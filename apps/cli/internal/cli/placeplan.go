package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// keepChoice is what skill place keeps of a displaced directory whose
// content is not the library's: nothing unless told, since either way one
// of the two is discarded, the library's content, or the directory's.
type keepChoice int

const (
	keepNeither keepChoice = iota
	keepLibrary
	keepPlacement
)

// plannedPlace is one place of a skill that skill place covers, and what it
// held when it was judged.
type plannedPlace struct {
	placeSite
	word       string // missing or displaced, as drift names the place, or "" for neither
	writes     bool   // the run writes the place: it is missing or displaced, or --copy replaces the library's symlink there
	state      string // what the path holds, in the words a journal records
	tree       string // a displaced directory's tree id, as git would record it
	recordable bool   // the displaced directory holds nothing git cannot record
	skill      bool   // the displaced directory holds a SKILL.md, so it can be a library directory
	same       bool   // the displaced directory holds exactly what the library directory holds
	err        error  // what kept this machine from reading the place
}

// removed reports whether placing the skill removes a real directory at
// the place, as it does every displaced directory it replaces, whatever it
// keeps of it.
func (p plannedPlace) removed() bool {
	return p.err == nil && p.word == driftDisplaced && home.IsDir(p.state)
}

// placePlan is everything skill place judged, read once before the lock
// and again under it, where the two have to agree: the library entry, what
// the library directory holds, and every place of the configurations the
// run covers, in the order the configurations were detected.
type placePlan struct {
	lib       scan.LibrarySkill
	rec       lineage.Record // the skill's branch, the zero record for a skill agentx does not manage
	managed   bool           // the skill is managed by an import branch
	asCopy    bool           // --copy was given
	asked     []string       // the configurations the run covers, sorted
	flags     []string       // the --to and --copy the run was given, for the commands a refusal names
	library   string         // the library, as this machine names it
	libState  string         // the library entry, in the words a journal records, read only beside a displaced directory
	libTree   treeid.Tree    // what the library directory holds, as git would record it, read only beside a displaced directory
	libBytes  string         // what the library directory holds byte for byte, read only when git cannot record all of it
	copies    []string       // the configurations copy_mode records a copy of the skill for
	copyPaths []string       // where those copies are, each a path --keep-placement may refresh
	sites     []string       // every detected configuration's own place of the skill, covered or not, but a universal client's
	universal []placeTarget  // the covered configurations whose client reads the library, whose library entry is the placement
	places    []plannedPlace
}

// planPlace judges the library skill lib and its places against the
// detected configurations, the ones the run covers, asked, and the ones
// copy_mode records a copy of the skill for. Every covered configuration's
// own place is in the plan, judged by the classifier drift reads, see
// ownPlaces and placeSite.drift, one judgement per path: a place two
// configurations share is planned once. copies is what copy_mode records
// and nothing more: --copy changes what is written at a place, not whether
// what is there is displaced.
//
// A place that is the library directory itself is planned as it is, with
// no drift: one a skills directory made a symlink to the library, or the
// library made one to a skills directory, makes of it, and one the library
// entry, itself a symlink into a client's skills directory, leads to. The
// client reads the library there, and replacing that directory with the
// symlink would replace the skill's only content with a link to itself.
// Detection counts a client of the first two as reading the library, see
// scan.ReadsLibrary, and drift finds nothing at the third, see
// isLibraryDirectory, so placing it counts it as placed and writes nothing.
// Every other overlap of the library and the places is planned as it is
// and refused, see overlap.
//
// A displaced directory holds what the library directory holds when git
// would record the two the same way. A tree leaves out what git cannot
// record, a nested repository or a named pipe, so where either holds any of
// it the two are compared byte for byte instead, as the journal
// fingerprints them. Nothing is skipped by either comparison: a directory
// is replaced without a flag only when nothing git would see is lost.
func (inv *invocation) planPlace(lib scan.LibrarySkill, detected []placeTarget, asked, copies []string, asCopy bool) (placePlan, error) {
	plan := placePlan{lib: lib, asCopy: asCopy, asked: slices.Sorted(slices.Values(asked)), library: inv.dirs.Library, copies: copies}
	var notAsked []string
	for _, t := range detected {
		if !slices.Contains(asked, t.id) {
			notAsked = append(notAsked, t.id)
		} else if t.readsLibrary {
			plan.universal = append(plan.universal, t)
		}
	}
	for _, p := range ownPlaces(detected, inv.dirs.Library, lib.Name, notAsked, copies) {
		if !p.asked() {
			continue
		}
		place := plannedPlace{placeSite: p, word: p.drift(lib.Path)}
		place.writes = place.word != "" || asCopy && sameTarget(p.path, lib.Path)
		place.state, place.err = home.State(p.path)
		plan.places = append(plan.places, place)
	}
	for _, t := range detected {
		if t.readsLibrary {
			continue
		}
		place := t.ownPlace(inv.dirs.Library, lib.Name)
		plan.sites = append(plan.sites, place)
		if slices.Contains(copies, t.id) {
			plan.copyPaths = append(plan.copyPaths, place)
		}
	}
	if !slices.ContainsFunc(plan.places, plannedPlace.removed) {
		return plan, nil
	}
	// The library is read whole only where a displaced directory is judged
	// against it: a run that only makes placements copies nothing it has not
	// checked, see stageCopy.
	var err error
	if plan.libState, err = home.State(lib.Path); err != nil {
		return placePlan{}, libraryFailure(inv.dirs.Library, err)
	}
	if plan.libTree, err = inv.readLibraryTree(lib.Path); err != nil {
		return placePlan{}, err
	}
	if len(plan.libTree.Unrecordable) > 0 {
		real, err := filepath.EvalSymlinks(lib.Path)
		if err == nil {
			plan.libBytes, err = home.State(real)
		}
		if err != nil {
			return placePlan{}, libraryFailure(inv.dirs.Library, err)
		}
	}
	for i := range plan.places {
		place := &plan.places[i]
		if !place.removed() {
			continue
		}
		var tree treeid.Tree
		if tree, place.err = treeid.Read(place.path); place.err == nil {
			place.tree, place.recordable = tree.ID, len(tree.Unrecordable) == 0
			recordable := place.recordable && len(plan.libTree.Unrecordable) == 0
			place.same = tree.ID == plan.libTree.ID && (recordable || place.state == plan.libBytes)
			place.skill = holdsSkillFile(place.path)
		}
	}
	return plan, nil
}

// signature is everything the plan was judged from, as one string: two
// reads of the machine that give the same signature would be placed the
// same way.
func (plan placePlan) signature() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%t\x00%s\x00%t\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s\n", plan.managed, plan.rec.Commit, plan.asCopy, strings.Join(plan.asked, ","),
		plan.libState, plan.libTree.ID, len(plan.libTree.Unrecordable), plan.libBytes,
		strings.Join(plan.copies, ","), strings.Join(plan.copyPaths, ","), strings.Join(plan.sites, ","))
	for _, p := range plan.places {
		fmt.Fprintf(&b, "%s\x00%s\x00%t\x00%s\x00%s\x00%t\x00%t\x00%t\x00%t\x00%t\x00%s\x00%s\n",
			p.path, p.word, p.writes, p.state, p.tree, p.recordable, p.skill, p.same, p.copied, p.err != nil, strings.Join(p.enabled, ","), strings.Join(p.paths, ","))
	}
	return b.String()
}

// differing are the displaced directories whose content is not the
// library's: the places skill place has to be told what to keep of.
func (plan placePlan) differing() []plannedPlace {
	var found []plannedPlace
	for _, p := range plan.places {
		if p.removed() && !p.same {
			found = append(found, p)
		}
	}
	return found
}

// command is the skill place command a hint names, quoted as a line of the
// hint quotes it: the command that was run, with the --to and --copy it
// was given, and extra after them.
func (plan placePlan) command(extra ...string) string {
	return "'" + skillCommand("place", plan.lib.Name, slices.Concat(plan.flags, extra)...) + "'"
}

// refusal is why the plan cannot be carried out as chosen, nil when it can.
// It is decided before the lock is taken, from what the first read found,
// and nothing has been changed when it refuses. Where symlinks make what
// the run changes overlap what it must leave alone it refuses first,
// whatever the flags, see overlap. The read under the lock holds the plan
// to the rest: every input they turn on is part of the signature that read
// has to give again.
func (plan placePlan) refusal(choice keepChoice) error {
	if err := plan.overlap(choice); err != nil {
		return err
	}
	differ := plan.differing()
	if len(differ) == 0 {
		return nil
	}
	name := plan.lib.Name
	withLibrary, withPlacement := plan.command("--keep-library"), plan.command("--keep-placement")
	paths := make([]string, len(differ))
	for i, p := range differ {
		paths[i] = quotedPath(p.path)
	}
	them, is, their := "it", "is a directory whose content differs", "its"
	if len(differ) > 1 {
		them, is, their = "them", "are directories whose content differs", "their"
	}
	switch choice {
	case keepNeither:
		return fail(exitRefused, fmt.Sprintf("%s %s from the library's %s, so nothing was placed", strings.Join(paths, ", "), is, name),
			"to keep the library's content and discard "+them+", run "+withLibrary+"; to make "+their+" content the library's, run "+withPlacement)
	case keepPlacement:
		for _, p := range differ {
			if !p.skill {
				return fail(exitRefused, fmt.Sprintf("%s holds no SKILL.md, so its content cannot become the library's %s", quotedPath(p.path), name),
					"keep the library's content with "+withLibrary+", or move "+quotedPath(p.path)+" aside and run "+plan.command()+" again")
			}
			if !p.recordable {
				return fail(exitRefused, fmt.Sprintf("%s holds what git cannot record, so its content cannot become the library's", quotedPath(p.path)),
					"move the repository or special file out of "+quotedPath(p.path)+", then run "+withPlacement+" again")
			}
			if p.tree != differ[0].tree {
				return fail(exitRefused, fmt.Sprintf("--keep-placement keeps the content of one directory, and %s hold different content", strings.Join(paths, ", ")),
					"move aside every directory but the one to keep, then run "+withPlacement+" again, or keep the library's content with "+withLibrary)
			}
		}
	}
	return nil
}

// touchedPath is a path skill place changes, as the plan names it and where
// it really is, every symlink on the way to it resolved, see placeKey.
// library marks the library directory that --keep-placement replaces.
type touchedPath struct {
	path string
	placeKey
	library bool
}

// touched are the paths the plan changes as chosen, each once however many
// configurations name it: when --keep-placement replaces the library
// directory, that directory first and every copy copy_mode records, which
// the new content may refresh, and every place the plan writes, a
// displaced directory, a missing place and the library's symlink where a
// copy belongs. A place this machine cannot read is skipped, not changed,
// and a place that already holds what it should is not written. replaces
// reports whether the plan removes a displaced directory or replaces the
// library directory.
func (plan placePlan) touched(choice keepChoice) (paths []touchedPath, replaces bool) {
	libraryReplaced := choice == keepPlacement && len(plan.differing()) > 0
	if libraryReplaced {
		paths = append(paths, touchedPath{plan.lib.Path, keyOf(plan.lib.Path), true})
	}
	// A place is never merged into the library directory: one that is it
	// has to be found inside the library, see overlap. Two that are one
	// place, as drift tells places apart, are one path, which the run
	// changes once.
	add := func(path string) {
		key := keyOf(path)
		if !slices.ContainsFunc(paths, func(t touchedPath) bool { return !t.library && t.is(key) }) {
			paths = append(paths, touchedPath{path: path, placeKey: key})
		}
	}
	for _, p := range plan.places {
		if p.err == nil && p.writes {
			add(p.path)
			replaces = replaces || p.removed()
		}
	}
	if libraryReplaced {
		for _, c := range plan.copyPaths {
			add(c)
		}
	}
	return paths, replaces || libraryReplaced
}

// overlap is why the plan cannot be carried out safely, nil when it can.
// skill place removes and writes whole directories, and a symlink can make
// any of them part of something else, so rather than follow links to find
// out what, it refuses, whatever the flags, wherever links make what it
// changes overlap what it must leave alone:
//
//   - A run that removes a displaced directory or replaces the library
//     directory finds the skill's library entry is not a real directory.
//     agentx installs one, and replacing a link would drop it without
//     touching what it leads to. A run that only writes placements writes
//     links to the entry and copies of what it leads to, so a skill linked
//     into the library by hand can still have them made.
//   - A path the run changes, see touched, is, lies inside or holds the
//     library, or the directory a library entry leads to, each compared
//     where it really is. The library directory --keep-placement replaces
//     is compared with the other entries alone.
//   - Two paths the run changes, or one of them and another
//     configuration's own place of the skill, lie one inside the other:
//     removing the outer one takes the inner one with it, and writing the
//     inner one changes what the outer one was judged to hold.
//   - A run that removes a displaced directory or replaces the library
//     directory finds a symlink anywhere in the skill's library directory.
//     An import never holds one, so it is a hand edit whose link may lead
//     into what the run replaces. With --keep-placement, so does a symlink
//     anywhere in a directory whose content becomes the library's, which
//     would be copied into the library link for link.
//
// A symlink elsewhere on disk that leads into what the run changes is the
// user's, and no disk is searched for one. The rule is read afresh at
// every call: before the lock, under it and right before staging.
func (plan placePlan) overlap(choice keepChoice) error {
	again := "run " + plan.command() + " again"
	root, err := filepath.EvalSymlinks(plan.library)
	if err != nil {
		return libraryFailure(plan.library, err)
	}
	entries, err := linkedEntries(root, plan.library)
	if err != nil {
		return libraryFailure(plan.library, err)
	}
	touched, replaces := plan.touched(choice)
	if replaces {
		info, err := os.Lstat(plan.lib.Path)
		if err != nil {
			return libraryFailure(plan.library, err)
		}
		if !info.IsDir() {
			target, _ := os.Readlink(plan.lib.Path)
			return fail(exitRefused, fmt.Sprintf("%s is a symlink to %s, not the directory agentx installed, so nothing was placed", quotedPath(plan.lib.Path), quotedPath(target)),
				"replace the link with the directory it leads to, then "+again)
		}
	}
	joined := func(message string) error {
		return fail(exitRefused, message+", so nothing was placed",
			"replace the symlink that joins them with the files it leads to, or move one of them elsewhere, then "+again)
	}
	for _, t := range touched {
		if !t.library {
			if rel := relation(t.real, root); rel != "" {
				return joined(fmt.Sprintf("%s %s the library %s, and placing it would change what the library holds", quotedPath(t.path), rel, quotedPath(plan.library)))
			}
		}
		for _, e := range entries {
			switch rel := relation(t.real, e.real); rel {
			case "":
			case "is":
				return joined(fmt.Sprintf("the library entry %s leads to %s, and placing it would change what the library holds", quotedPath(e.path), quotedPath(t.path)))
			default:
				return joined(fmt.Sprintf("%s %s %s, where the library entry %s leads, and placing it would change what the library holds", quotedPath(t.path), rel, quotedPath(e.real), quotedPath(e.path)))
			}
		}
	}
	// Every touched path comes first, so a pair holds one of them.
	all := slices.Clone(touched)
	for _, s := range plan.sites {
		key := keyOf(s)
		if !slices.ContainsFunc(all, func(t touchedPath) bool { return t.is(key) }) {
			all = append(all, touchedPath{path: s, placeKey: key})
		}
	}
	for i, a := range touched {
		for _, b := range all[i+1:] {
			inner, outer := a, b
			if inside(b.real, a.real) {
				inner, outer = b, a
			} else if !inside(a.real, b.real) {
				continue
			}
			return joined(fmt.Sprintf("%s lies inside %s, and placing one would change the other", quotedPath(inner.path), quotedPath(outer.path)))
		}
	}
	if !replaces {
		return nil
	}
	link, err := firstSymlink(plan.lib.Path)
	if err != nil {
		return libraryFailure(plan.library, err)
	}
	if link != "" {
		return fail(exitRefused, fmt.Sprintf("the library directory %s holds the symlink %s, which no version agentx installs holds, so nothing was placed", quotedPath(plan.lib.Path), quotedPath(link)),
			"replace the link with the files it leads to, or see what changed with '"+skillCommand("diff", plan.lib.Name)+"' and go back to the installed version with '"+skillCommand("revert", plan.lib.Name)+"', then "+again)
	}
	if choice != keepPlacement {
		return nil
	}
	withPlacement := plan.command("--keep-placement")
	for _, p := range plan.differing() {
		link, err := firstSymlink(p.path)
		if err != nil {
			return fail(exitRefused, fmt.Sprintf("%s cannot be read whole, so its content cannot become the library's: %v", quotedPath(p.path), err),
				"make "+quotedPath(p.path)+" readable, then run "+withPlacement+" again")
		}
		if link != "" {
			return fail(exitRefused, fmt.Sprintf("%s holds the symlink %s, so its content cannot become the library's", quotedPath(p.path), quotedPath(link)),
				"replace the link with the files it leads to, then run "+withPlacement+" again, or keep the library's content with "+plan.command("--keep-library"))
		}
	}
	return nil
}

// linkedEntry is one entry of the library that is a symlink: its path as
// the library names it, and where it really leads.
type linkedEntry struct{ path, real string }

// linkedEntries are the entries of the library, whose real directory is
// root and which this machine names library, that are symlinks, each with
// where it leads, every link on the way resolved. A link that leads
// nowhere is taken where it names, so a place written there is still found
// to be the entry's. An entry that is a real directory lies inside the
// library and needs no entry of its own.
func linkedEntries(root, library string) ([]linkedEntry, error) {
	dirents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var entries []linkedEntry
	for _, d := range dirents {
		if d.Type()&os.ModeSymlink == 0 {
			continue
		}
		at := filepath.Join(root, d.Name())
		real, err := filepath.EvalSymlinks(at)
		if err != nil {
			target, err := os.Readlink(at)
			if err != nil {
				return nil, err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(root, target)
			}
			real = canonicalPath(target)
		}
		entries = append(entries, linkedEntry{filepath.Join(library, d.Name()), real})
	}
	return entries, nil
}

// firstSymlink is the first symlink inside the real directory dir, at any
// depth, as dir names it, "" when it holds none.
func firstSymlink(dir string) (string, error) {
	var found string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}

// relation is how the real path a stands to the real path b, in the words
// of a refusal: "is", "lies inside" or "holds"; "" when neither is the other
// or lies inside it.
func relation(a, b string) string {
	switch {
	case samePath(a, b):
		return "is"
	case inside(a, b):
		return "lies inside"
	case inside(b, a):
		return "holds"
	}
	return ""
}

// samePath reports whether the real paths a and b are one path: spelled
// the same, or the same file where both are there, so a disk that ignores
// case hides nothing.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ai, err := os.Lstat(a)
	if err != nil {
		return false
	}
	bi, err := os.Lstat(b)
	return err == nil && os.SameFile(ai, bi)
}

// inside reports whether the real path inner lies beneath the real
// directory outer, not being it: spelled beneath it, whatever outer is, or
// with a directory on the way up from it that is outer as a file, which
// takes outer to be a real directory, since a symlink there holds nothing.
// Neither need exist.
func inside(inner, outer string) bool {
	if strings.HasPrefix(inner, strings.TrimSuffix(outer, string(filepath.Separator))+string(filepath.Separator)) {
		return true
	}
	o, err := os.Lstat(outer)
	if err != nil || !o.IsDir() {
		return false
	}
	for dir := filepath.Dir(inner); ; dir = filepath.Dir(dir) {
		if info, err := os.Lstat(dir); err == nil && os.SameFile(info, o) {
			return true
		}
		if filepath.Dir(dir) == dir {
			return false
		}
	}
}

// stagePlace plans, into m, the whole of a place plan the lock-time read
// agreed with. With --keep-placement the library directory is replaced
// first, so every placement made after it holds what the library is to
// hold, and the copies copy_mode records follow it, see stageKept. A
// covered configuration whose client reads the library has its placement
// already, the library entry, and each place is then planned on its own,
// see stagePlaceAt, but a copy the replacement already judged: it counts as
// placed unless it was kept as it is.
func (inv *invocation) stagePlace(ctx context.Context, m *home.Mutation, gitDir string, plan placePlan, choice keepChoice, done *placements) error {
	p := libraryPlaceable(plan.lib)
	replaced := choice == keepPlacement && len(plan.differing()) > 0
	var left []string // the copies the replacement left as they were
	if replaced {
		kept, skipped, err := inv.stageKept(ctx, m, gitDir, plan, done)
		if err != nil {
			return err
		}
		p, left = kept, skipped
	}
	done.placed = append(done.placed, plan.universal...)
	for _, place := range plan.places {
		if replaced && place.holdsCopy() {
			key := keyOf(place.path)
			if !slices.ContainsFunc(left, func(path string) bool { return keyOf(path).is(key) }) {
				done.placedAt(place)
			}
			continue
		}
		inv.stagePlaceAt(m, p, place, plan.lib.Path, plan.copies, plan.asCopy, choice, done)
	}
	return nil
}

// holdsCopy reports whether the place holds a directory copy_mode records
// as a copy: no drift, and what the copies of a replaced library directory
// are judged by, see refreshCopies.
func (p plannedPlace) holdsCopy() bool {
	return p.err == nil && p.word == "" && p.copied && home.IsDir(p.state)
}

// stageKept plans the library directory's replacement by the content of
// the displaced directory --keep-placement keeps, and returns that content
// as what every placement of the run is made of, and the copies it left as
// they were. The files git ignores in the library directory are carried
// into the new one, as git checkout keeps them and as a revert carries
// them, unless the content kept holds a file at the same path. The copies
// copy_mode records follow it as a revert makes them follow the base: a
// copy holding what the library directory held or the base version of a
// managed skill, which a copy placed before the library was edited still
// holds, is agentx's and is refreshed, and any other copy is kept.
func (inv *invocation) stageKept(ctx context.Context, m *home.Mutation, gitDir string, plan placePlan, done *placements) (placeable, []string, error) {
	name, libPath := plan.lib.Name, plan.lib.Path
	kept := plan.differing()[0]
	// What the library directory held is judged against the base version of
	// a managed skill, so a file the base holds is content, not a file to
	// carry, and against the empty tree for a skill agentx does not manage.
	before := emptyVersion()
	if plan.managed {
		before = baseVersion(plan.rec)
	}
	j, err := inv.judgeDir(ctx, gitDir, plan.lib.ResolvedPath, plan.libTree, before, true)
	if err != nil {
		return placeable{}, nil, accountRepoFailure(err)
	}
	target := rawVersion(kept.tree)
	lay := func(dest string) error { return copyTreeTo(kept.path, dest) }
	staged := m.Sibling(libPath, "staged")
	fingerprint, err := stageVersion(staged, lay, target, plan.lib.ResolvedPath, j.ignored)
	if err != nil {
		os.RemoveAll(staged)
		return placeable{}, nil, libraryFailure(inv.dirs.Library, err)
	}
	hash := contentHashAt(staged)
	if hash == "" {
		os.RemoveAll(staged)
		return placeable{}, nil, libraryFailure(inv.dirs.Library, fmt.Errorf("the content of %s staged at %s holds no SKILL.md", kept.path, staged))
	}
	m.Remove(libPath, plan.libState)
	m.Publish(libPath, staged, fingerprint)
	var placed []version
	if j.written != "" {
		placed = append(placed, treeVersion(j.written))
	}
	if plan.managed {
		placed = append(placed, baseVersion(plan.rec))
	}
	var copies placements
	inv.refreshCopies(ctx, m, gitDir, name, target, placed, lay, plan.copies, &copies)
	done.refreshed = append(done.refreshed, copies.copies...)
	done.skipped = append(done.skipped, copies.skipped...)
	done.kept = kept.path
	return placeable{name: name, hash: hash, stage: func(dest string) error { return copyTreeTo(staged, dest) }}, copies.skipped, nil
}

// stagePlaceAt plans the placement at one place, made of p. A place this
// machine cannot read or cannot write is skipped with a warning and
// counted, as a placement that cannot be made is: one client agentx cannot
// reach is not a reason to leave every other one unplaced.
//
// A displaced directory is replaced by the expected placement: a symlink,
// or a copy where --copy asks for one. One holding the library's content
// loses nothing to it, is replaced all the same and is reported as
// adopted, as an install adopts it. One holding anything else is replaced
// only as the choice says: --keep-library discards it, and --keep-placement
// already made its content the library's. Every other place gets the
// placement stagePlacement makes, with its refusals: a missing one is
// made, the library's symlink where a copy belongs is replaced by one, and
// a link of the user's or a copy edited where it is is left as it is and
// counted as skipped.
//
// A place two configurations share is placed once, and counts as placed
// for each of them the run covers.
func (inv *invocation) stagePlaceAt(m *home.Mutation, p placeable, place plannedPlace, libPath string, copies []string, asCopy bool, choice keepChoice, done *placements) {
	t := place.representative(copies)
	switch {
	case place.err != nil:
		inv.skipPlacement(done, t, place.path, place.err)
	case place.removed():
		if !place.same && choice == keepNeither {
			return // refused before the lock; never planned
		}
		// What can fail is the copy staged beside the place, or the
		// directory the place sits in, where the symlink is written: both
		// are checked now, before a step of this place is recorded, as a
		// placement checks them.
		staged, fingerprint, err := inv.placementContent(m, p, t, place.path, asCopy)
		if err != nil {
			inv.skipPlacement(done, t, place.path, err)
			return
		}
		m.Remove(place.path, place.state)
		if asCopy {
			m.Publish(place.path, staged, fingerprint)
			done.copiedAt(place)
		} else {
			m.Link(place.path, libPath)
		}
		switch {
		case place.same:
			done.adoptions = append(done.adoptions, place.path)
		case choice == keepLibrary:
			done.discarded = append(done.discarded, place.path)
		}
		done.placedAt(place)
	default:
		// Nothing at the place, the library's own link where a copy
		// belongs, or anything that is no drift: the placement skill add
		// would make, made by the same staging and refused for the same
		// reasons.
		var one placements
		inv.stagePlacement(m, p, t, libPath, asCopy || place.copied, copies, &one)
		if asCopy && len(one.copies) > 0 {
			done.copiedAt(place)
		} else {
			done.copies = append(done.copies, one.copies...)
		}
		done.adoptions = append(done.adoptions, one.adoptions...)
		done.skipped = append(done.skipped, one.skipped...)
		if len(one.placed) > 0 {
			done.placedAt(place)
		}
	}
}

// representative is the configuration a place is placed as: the first
// covered one copy_mode records a copy for, so that a copy placed at a
// place two configurations share is the one the settings already name, and
// the first covered one otherwise.
func (p plannedPlace) representative(copies []string) placeTarget {
	var first *placeTarget
	for i, t := range p.targets {
		if !slices.Contains(p.enabled, t.id) {
			continue
		}
		if slices.Contains(copies, t.id) {
			return t
		}
		if first == nil {
			first = &p.targets[i]
		}
	}
	return *first
}

// placedAt records that the place holds the placement now, for every
// configuration the run covers whose own place it is.
func (d *placements) placedAt(place plannedPlace) {
	for _, t := range place.targets {
		if slices.Contains(place.enabled, t.id) {
			d.placed = append(d.placed, t)
		}
	}
}

// copiedAt records that the place holds the copy --copy asked for, for
// every configuration the run covers whose own place it is, as an install
// that names each of them records it.
func (d *placements) copiedAt(place plannedPlace) {
	for _, t := range place.targets {
		if slices.Contains(place.enabled, t.id) {
			d.copies = append(d.copies, t.id)
		}
	}
}

// placeRecord is the library skill skill place works on, its lineage, and
// whether an import branch manages it. It refuses a name the library does
// not hold, a managed skill whose library directory is gone, which has no
// content to place, and a fork given --keep-library or --keep-placement: a
// fork's placements are placed as they always were, and never judged
// against a library content to keep.
func (inv *invocation) placeRecord(sc skillContext, name string, choice keepChoice, flags []string) (lib scan.LibrarySkill, rec lineage.Record, managed bool, err error) {
	rec, ok := sc.records[name]
	lib, held := librarySkill(inv.dirs.Library, name)
	managed = ok && rec.Kind == lineage.KindManaged
	switch {
	case !held && managed:
		what, wayOut := sc.absentNotice(inv, name)
		err = fail(exitRefused, what+", so there is no skill to place", wayOut)
	case !held:
		err = inv.noLibrarySkill(name)
	case ok && rec.Kind == lineage.KindFork && choice != keepNeither:
		err = fail(exitRefused, name+" is a fork on this machine, which --keep-library and --keep-placement do not apply to",
			"place it without them with '"+skillCommand("place", name, flags...)+"'")
	}
	return lib, rec, managed, err
}

// placeFlags are the --to and --copy a run of skill place was given, as a
// command a hint names repeats them.
func placeFlags(to []string, asCopy bool) []string {
	var flags []string
	for _, id := range to {
		flags = append(flags, "--to", shellWord(id))
	}
	if asCopy {
		flags = append(flags, "--copy")
	}
	return flags
}
