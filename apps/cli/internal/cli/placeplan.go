package cli

import (
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

// plannedPlace is one place of a skill that skill place covers, and what it
// held when it was judged.
type plannedPlace struct {
	placeSite
	word   string // missing or displaced, as drift names the place, or "" for neither
	writes bool   // the run writes the place: it is missing or displaced, or --copy replaces the library's symlink there
	state  string // what the path holds, in the words a journal records
	tree   string // a displaced directory's tree id, as git would record it
	same   bool   // the displaced directory holds exactly what the library directory holds
	err    error  // what kept this machine from reading the place
}

// removed reports whether placing the skill removes a real directory at
// the place, as it does every displaced directory it replaces.
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
		if !t.readsLibrary {
			plan.sites = append(plan.sites, t.ownPlace(inv.dirs.Library, lib.Name))
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
			place.tree = tree.ID
			recordable := len(tree.Unrecordable) == 0 && len(plan.libTree.Unrecordable) == 0
			place.same = tree.ID == plan.libTree.ID && (recordable || place.state == plan.libBytes)
		}
	}
	return plan, nil
}

// signature is everything the plan was judged from, as one string: two
// reads of the machine that give the same signature would be placed the
// same way.
func (plan placePlan) signature() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%t\x00%s\x00%t\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\n", plan.managed, plan.rec.Commit, plan.asCopy, strings.Join(plan.asked, ","),
		plan.libState, plan.libTree.ID, len(plan.libTree.Unrecordable), plan.libBytes,
		strings.Join(plan.copies, ","), strings.Join(plan.sites, ","))
	for _, p := range plan.places {
		fmt.Fprintf(&b, "%s\x00%s\x00%t\x00%s\x00%s\x00%t\x00%t\x00%t\x00%s\x00%s\n",
			p.path, p.word, p.writes, p.state, p.tree, p.same, p.copied, p.err != nil, strings.Join(p.enabled, ","), strings.Join(p.paths, ","))
	}
	return b.String()
}

// differing are the displaced directories whose content is not the
// library's: the places skill place replaces only with --force.
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

// refusal is why the plan cannot be carried out, nil when it can. It is
// decided before the lock is taken, from what the first read found, and
// nothing has been changed when it refuses. Where symlinks make what the
// run changes overlap what it must leave alone it refuses first, with or
// without --force, see overlap. A displaced directory whose content is not
// the library's is replaced only with --force, since replacing it discards
// what it holds; the hint says so, and how to keep it instead. The read
// under the lock holds the plan to the rest: every input they turn on is
// part of the signature that read has to give again.
func (plan placePlan) refusal(force bool) error {
	if err := plan.overlap(); err != nil {
		return err
	}
	differ := plan.differing()
	if len(differ) == 0 || force {
		return nil
	}
	paths := make([]string, len(differ))
	for i, p := range differ {
		paths[i] = quotedPath(p.path)
	}
	is, it, holds := "is a directory whose content differs", "it", "it holds"
	if len(differ) > 1 {
		is, it, holds = "are directories whose content differs", "them", "they hold"
	}
	return fail(exitRefused, fmt.Sprintf("%s %s from the library's %s, so nothing was placed", strings.Join(paths, ", "), is, plan.lib.Name),
		"to replace "+it+" with the library's version and delete what "+holds+", run "+plan.command("--force")+"; to keep "+it+", move "+it+" elsewhere first")
}

// touchedPath is a path skill place changes, as the plan names it and where
// it really is, every symlink on the way to it resolved, see placeKey.
type touchedPath struct {
	path string
	placeKey
}

// touched are the paths the plan changes, each once however many
// configurations name it: every place the plan writes, a displaced
// directory, a missing place and the library's symlink where a copy
// belongs. A place this machine cannot read is skipped, not changed, and a
// place that already holds what it should is not written. replaces reports
// whether the plan removes a displaced directory.
func (plan placePlan) touched() (paths []touchedPath, replaces bool) {
	// Two places that are one place, as drift tells places apart, are one
	// path, which the run changes once.
	for _, p := range plan.places {
		if p.err != nil || !p.writes {
			continue
		}
		key := keyOf(p.path)
		if !slices.ContainsFunc(paths, func(t touchedPath) bool { return t.is(key) }) {
			paths = append(paths, touchedPath{path: p.path, placeKey: key})
		}
		replaces = replaces || p.removed()
	}
	return paths, replaces
}

// overlap is why the plan cannot be carried out safely, nil when it can.
// skill place removes and writes whole directories, and a symlink can make
// any of them part of something else, so rather than follow links to find
// out what, it refuses, with or without --force, wherever links make what
// it changes overlap what it must leave alone:
//
//   - A run that removes a displaced directory finds the skill's library
//     entry is not a real directory. agentx installs one, so a link there is
//     a hand edit that may lead into what the run removes. A run that only
//     writes placements writes links to the entry and copies of what it
//     leads to, so a skill linked into the library by hand can still have
//     them made.
//   - A path the run changes, see touched, is, lies inside or holds the
//     library, or the directory a library entry leads to, each compared
//     where it really is.
//   - Two paths the run changes, or one of them and another
//     configuration's own place of the skill, lie one inside the other:
//     removing the outer one takes the inner one with it, and writing the
//     inner one changes what the outer one was judged to hold.
//   - A run that removes a displaced directory finds a symlink anywhere in
//     the skill's library directory. An import never holds one, so it is a
//     hand edit whose link may lead into what the run removes.
//
// A symlink elsewhere on disk that leads into what the run changes is the
// user's, and no disk is searched for one. The rule is read afresh at
// every call: before the lock, under it and right before staging.
func (plan placePlan) overlap() error {
	again := "run " + plan.command() + " again"
	root, err := filepath.EvalSymlinks(plan.library)
	if err != nil {
		return libraryFailure(plan.library, err)
	}
	entries, err := linkedEntries(root, plan.library)
	if err != nil {
		return libraryFailure(plan.library, err)
	}
	touched, replaces := plan.touched()
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
		if rel := relation(t.real, root); rel != "" {
			return joined(fmt.Sprintf("%s %s the library %s, and placing it would change what the library holds", quotedPath(t.path), rel, quotedPath(plan.library)))
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
// agreed with. A covered configuration whose client reads the library has
// its placement already, the library entry, and each place is then planned
// on its own, see stagePlaceAt.
func (inv *invocation) stagePlace(m *home.Mutation, plan placePlan, force bool, done *placements) {
	p := libraryPlaceable(plan.lib)
	done.placed = append(done.placed, plan.universal...)
	for _, place := range plan.places {
		inv.stagePlaceAt(m, p, place, plan.lib.Path, plan.copies, plan.asCopy, force, done)
	}
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
// only with --force, which discards it. Every other place gets the
// placement stagePlacement makes, with its refusals: a missing one is
// made, the library's symlink where a copy belongs is replaced by one, and
// a link of the user's or a copy edited where it is is left as it is and
// counted as skipped.
//
// A place two configurations share is placed once, and counts as placed
// for each of them the run covers.
func (inv *invocation) stagePlaceAt(m *home.Mutation, p placeable, place plannedPlace, libPath string, copies []string, asCopy, force bool, done *placements) {
	t := place.representative(copies)
	switch {
	case place.err != nil:
		inv.skipPlacement(done, t, place.path, place.err)
	case place.removed():
		if !place.same && !force {
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
		if place.same {
			done.adoptions = append(done.adoptions, place.path)
		} else {
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
// not hold, and a managed skill whose library directory is gone, which has
// no content to place. A fork is found by its branch alone, whatever its
// library entry is: putting that entry back is part of placing it, see
// placeFork.
func (inv *invocation) placeRecord(sc skillContext, name string) (lib scan.LibrarySkill, rec lineage.Record, managed bool, err error) {
	rec, ok := sc.records[name]
	if ok && rec.Kind == lineage.KindFork {
		return lib, rec, false, nil
	}
	lib, held := librarySkill(inv.dirs.Library, name)
	managed = ok && rec.Kind == lineage.KindManaged
	switch {
	case !held && managed:
		what, wayOut := sc.absentNotice(inv, name)
		err = fail(exitRefused, what+", so there is no skill to place", wayOut)
	case !held:
		err = inv.noLibrarySkill(name)
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
