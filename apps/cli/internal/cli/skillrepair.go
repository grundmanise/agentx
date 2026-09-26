package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

func newSkillRepairCommand(inv *invocation) *cobra.Command {
	var keepLibrary, keepPlacement bool
	cmd := &cobra.Command{
		Use:   "repair <name>",
		Short: "Put back the missing and displaced placements of a managed skill",
		Long: "Put back the placements of a managed skill that 'agentx skill list' shows as\n" +
			"missing or displaced. A missing placement is made again: a copy where copy_mode\n" +
			"records one, a symlink everywhere else. The library's symlink where a copy is\n" +
			"recorded is replaced by a copy. A real directory where the symlink belongs is\n" +
			"replaced by the symlink when it holds the library's content; when it holds\n" +
			"anything else, choose what survives: --keep-library discards the directory,\n" +
			"--keep-placement makes its content the library's. A directory that differs\n" +
			"from the library is never deleted without one of them.\n\n" +
			"A repair changes nothing, whatever the flags, where symlinks make what it\n" +
			"changes overlap what it must leave alone: the skill's library entry must be a\n" +
			"real directory, and no path the repair changes may be, lie inside or hold the\n" +
			"library, a directory a library entry leads to, another path it changes or\n" +
			"another client's place of the skill. A repair that removes or replaces\n" +
			"anything also refuses while the skill's library directory holds a symlink,\n" +
			"and --keep-placement while the directory it keeps holds one.\n\n" +
			"To place the skill in one more client, run 'agentx skill place <name> --to <configuration>'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if keepLibrary && keepPlacement {
				return fail(exitUsage, "--keep-library and --keep-placement cannot both be given",
					"give --keep-library to keep the library's content, or --keep-placement to keep the directory's")
			}
			choice := chooseNone
			switch {
			case keepLibrary:
				choice = chooseLibrary
			case keepPlacement:
				choice = choosePlacement
			}
			return inv.skillRepair(cmd.Context(), args[0], choice)
		},
	}
	cmd.Flags().BoolVar(&keepLibrary, "keep-library", false,
		"replace a displaced directory whose content differs from the library with the symlink, discarding the directory")
	cmd.Flags().BoolVar(&keepPlacement, "keep-placement", false,
		"make a displaced directory's content the library's, then replace the directory with the symlink")
	return cmd
}

// repairChoice is what a repair keeps of a displaced directory whose
// content is not the library's: nothing unless told, since either way one
// of the two is discarded, the library's content, or the directory's.
type repairChoice int

const (
	chooseNone repairChoice = iota
	chooseLibrary
	choosePlacement
)

// The ways a repair puts one place back, each the word its row names.
const (
	repairPlaced   = "placed"   // nothing was there, and the placement was made
	repairRelinked = "relinked" // a real directory was there, and the symlink replaced it
	repairCopied   = "copied"   // the library's symlink was where a copy belongs, and a copy replaced it
)

// repairPlace is one place of a skill that drift finds missing or
// displaced, and what it held when it was judged.
type repairPlace struct {
	placeSite
	word       string // missing or displaced
	state      string // what the path holds, in the words a journal records
	tree       string // a real directory's tree id, as git would record it
	recordable bool   // the directory holds nothing git cannot record
	skill      bool   // the directory holds a SKILL.md, so it can be a library directory
	same       bool   // the directory holds exactly what the library directory holds
	err        error  // what kept this machine from reading the place
}

// removed reports whether a repair of the place removes a real directory,
// as it does every directory it repairs, whatever it keeps of it.
func (p repairPlace) removed() bool { return p.err == nil && home.IsDir(p.state) }

// repairPlan is everything a repair judged, read once before the lock and
// again under it, where the two have to agree: the library entry, what the
// library directory holds, and every place drift finds missing or
// displaced, in the order the configurations were detected.
type repairPlan struct {
	lib       scan.LibrarySkill
	library   string      // the library, as this machine names it
	libState  string      // the library entry, in the words a journal records
	libTree   treeid.Tree // what the library directory holds, as git would record it
	libBytes  string      // what the library directory holds byte for byte, read only when git cannot record all of it
	copies    []string    // the configurations copy_mode records a copy of the skill for
	copyPaths []string    // where those copies are, each a path --keep-placement may refresh
	sites     []string    // every detected configuration's own place of the skill, enabled or not, but a universal client's
	places    []repairPlace
}

// repaired is what a repair did, for the report that follows the mutation.
type repaired struct {
	rows      []repairRow // one per configuration put back, in detection order
	skipped   []string    // the places left as they were, each with a warning saying why
	refreshed placements  // the copies a new library content refreshed or kept; see refreshCopies
	kept      string      // with --keep-placement, the directory whose content the library now holds
	discarded []string    // with --keep-library, the directories whose content was discarded
}

// repairRow is one configuration a repair put back: how, and the placement
// it now has.
type repairRow struct {
	id, action, mode, path string
}

// skillRepair puts back the placements of a managed skill that drift finds
// missing or displaced, and nothing else: every place is judged by the one
// classifier drift reads, so what a repair touches is exactly what the
// listing says is wrong, and a skill with nothing wrong is left as it is.
//
//   - A missing placement is made the way skill place makes one, a copy
//     where copy_mode records one and a symlink everywhere else, and so is
//     a copy where the library's own symlink displaced it.
//   - A real directory where the symlink belongs is replaced by the symlink
//     when it holds exactly what the library directory holds, as git would
//     record the two: nothing of the user's is lost. When it holds
//     anything else, one of the two contents has to go, and the run
//     refuses unless told which: --keep-library replaces the directory
//     with the symlink and discards it, and --keep-placement first makes
//     its content the library's, which leaves a skill whose content is not
//     its base version modified.
//
// A link of the user's, a copy edited where it is and anything else at a
// place is no drift, and a repair never touches it. Where symlinks make
// what it would change overlap what it must leave alone, it refuses
// whatever the flags, see overlap.
//
// What a repair discards is what the paths held when it judged them. Every
// place and the library are read first, and read again under the lock: a
// change in between refuses the repair rather than being discarded with
// the rest. The journal's remove steps carry what was judged, and the
// content they retain is dropped only when it still hashes to it, so a
// change made while the mutation runs is kept as well.
func (inv *invocation) skillRepair(ctx context.Context, name string, choice repairChoice) error {
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	lib, rec, err := inv.repairRecord(sc, name)
	if err != nil {
		return err
	}
	plan, err := inv.planRepair(lib, sc.targets, sc.disabled, sc.modes[name])
	if err != nil {
		return err
	}
	if len(plan.places) == 0 {
		return inv.reportRepaired(ctx, name, nil)
	}
	if err := plan.refusal(name, choice); err != nil {
		return err
	}
	gitDir := gitx.AccountRepoPath(inv.dirs.Home)
	// A repair that makes a displaced directory's content the library's
	// refreshes every copy that holds what agentx placed there: what the
	// library directory held, or the base version, which a copy placed
	// before the library was edited still holds. The base is read here,
	// before the lock, and the branch it comes from is read again under it.
	var baseTree string
	if choice == choosePlacement && len(plan.differing()) > 0 {
		base, err := lineage.ReadBase(ctx, inv.git, gitDir, rec)
		if err != nil {
			return accountRepoFailure(err)
		}
		baseTree = base.ID() // the tree the base has laid out on disk, as a copy of it holds it
	}
	again := "run '" + skillCommand("repair", name) + "' again"
	var done repaired
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		// Every input is read again under the lock: the branch the skill is
		// managed by, the settings the places are judged against, and every
		// path the repair would change.
		refs, err := inv.lineageRefs(ctx, gitDir, name)
		if err != nil {
			return err
		}
		if refs[lineage.ManagedRef(name)] != rec.Commit || refs[lineage.ForkRef(name)] != "" {
			return fail(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved while "+name+" was being repaired, so nothing was changed", again)
		}
		changed := fail(exitRefused, name+" or its placements changed while it was being repaired, so nothing was changed", again)
		now, ok := librarySkill(inv.dirs.Library, name)
		if !ok {
			return changed
		}
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		targets := inv.detectedTargets()
		live, err := inv.planRepair(now, targets, edit.s.DisabledConfigurations, edit.copiesOf(name))
		if err != nil {
			return err
		}
		if live.signature() != plan.signature() {
			return changed
		}
		// Where links make paths overlap is no part of the signature, so it
		// is read again, before the sweep changes anything.
		if err := live.overlap(choice); err != nil {
			return err
		}
		// A repair killed before its journal was written left what it staged
		// with nothing to name it. All of it is swept before anything is
		// staged, since a sweep of a directory two configurations share would
		// take a sibling this plan staged a moment earlier.
		for _, place := range live.places {
			sweepStaged(filepath.Dir(place.path))
		}
		if choice == choosePlacement {
			sweepStaged(inv.dirs.Library)
			for _, t := range targets {
				if !t.readsLibrary && slices.Contains(live.copies, t.id) {
					sweepStaged(t.dir)
				}
			}
		}
		// And once more, the last read before anything is staged.
		if err := live.overlap(choice); err != nil {
			return err
		}
		done = repaired{}
		m := home.NewMutation(inv.dirs.Home)
		if err := inv.stageRepair(m, live, choice, baseTree, &done); err != nil {
			m.Discard()
			return err
		}
		done.inDetectionOrder(targets)
		if m.Empty() {
			return nil // every place was skipped, each with its warning
		}
		// The branch does not move: the step holds the journal to the
		// version the repair was judged against, so that recovery finishes
		// it only while the branch still names that version.
		m.Ref(gitDir, lineage.ManagedRef(name), rec.Commit, rec.Commit)
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return mutationFailure(err)
	}
	return inv.reportRepaired(ctx, name, &done)
}

// repairRecord is the library skill a repair works on and its lineage.
// Drift is judged for a managed skill alone, so a repair refuses every
// other name, each for its own reason: a name the library does not hold, a
// managed skill whose library directory is gone, an unmanaged skill, a
// fork and a branch agentx cannot read.
func (inv *invocation) repairRecord(sc skillContext, name string) (scan.LibrarySkill, lineage.Record, error) {
	rec, ok := sc.records[name]
	lib, held := librarySkill(inv.dirs.Library, name)
	var refusal error
	switch {
	case ok && rec.Kind == lineage.KindFork:
		refusal = fail(exitRefused, name+" is a fork on this machine",
			"this command repairs the placements of a managed skill; a fork's are not judged for drift")
	case !held && ok:
		what, wayOut := sc.absentNotice(inv, name)
		refusal = fail(exitRefused, what+", so there is no skill to place", wayOut)
	case !held:
		refusal = inv.noLibrarySkill(name)
	case !ok:
		refusal = fail(exitRefused, name+" is not managed by agentx, so its placements are not judged for drift",
			"place it in a configuration with '"+skillCommand("place", name, "--to", "<configuration>")+"'")
	case !rec.HasImport:
		refusal = fail(exitRefused, fmt.Sprintf("the import branch %s records no version agentx can read", rec.Ref),
			"run 'agentx doctor' and check the account repo it names")
	}
	return lib, rec, refusal
}

// planRepair judges the library skill lib and its places against the
// detected configurations, the ones the settings disable and the ones
// copy_mode records a copy for. A place that holds the placement agentx
// keeps, or anything that is no drift, is not in the plan.
//
// Nor is a place that is the library directory itself: one a skills
// directory made a symlink to the library, or the library made one to a
// skills directory, makes of it, and one the library entry, itself a
// symlink into a client's skills directory, leads to. The client reads the
// library there, and replacing that directory with the symlink would
// replace the skill's only content with a link to itself. Detection counts
// a client of the first two as reading the library, see scan.ReadsLibrary,
// and drift finds nothing at the third, see isLibraryDirectory, so this
// only stands guard. Every other overlap of the library and the places is
// planned as it is and refused, see overlap.
//
// A directory holds what the library directory holds when git would record
// the two the same way. A tree leaves out what git cannot record, a nested
// repository or a named pipe, so where either holds any of it the two are
// compared byte for byte instead, as the journal fingerprints them.
func (inv *invocation) planRepair(lib scan.LibrarySkill, targets []placeTarget, disabled, copies []string) (repairPlan, error) {
	plan := repairPlan{lib: lib, library: inv.dirs.Library, copies: copies}
	var err error
	if plan.libState, err = home.State(lib.Path); err != nil {
		return repairPlan{}, libraryFailure(inv.dirs.Library, err)
	}
	if plan.libTree, err = inv.readLibraryTree(lib.Path); err != nil {
		return repairPlan{}, err
	}
	if len(plan.libTree.Unrecordable) > 0 {
		real, err := filepath.EvalSymlinks(lib.Path)
		if err == nil {
			plan.libBytes, err = home.State(real)
		}
		if err != nil {
			return repairPlan{}, libraryFailure(inv.dirs.Library, err)
		}
	}
	for _, p := range ownPlaces(targets, inv.dirs.Library, lib.Name, disabled, copies) {
		if !p.asked() || isLibraryDirectory(p.path, lib.Path) {
			continue
		}
		word := p.drift(lib.Path)
		if word == "" {
			continue
		}
		place := repairPlace{placeSite: p, word: word}
		place.state, place.err = home.State(p.path)
		if place.removed() {
			var tree treeid.Tree
			if tree, place.err = treeid.Read(p.path); place.err == nil {
				place.tree, place.recordable = tree.ID, len(tree.Unrecordable) == 0
				recordable := place.recordable && len(plan.libTree.Unrecordable) == 0
				place.same = tree.ID == plan.libTree.ID && (recordable || place.state == plan.libBytes)
				place.skill = holdsSkillFile(p.path)
			}
		}
		plan.places = append(plan.places, place)
	}
	for _, t := range targets {
		if t.readsLibrary {
			continue
		}
		place := t.ownPlace(inv.dirs.Library, lib.Name)
		plan.sites = append(plan.sites, place)
		if slices.Contains(copies, t.id) {
			plan.copyPaths = append(plan.copyPaths, place)
		}
	}
	return plan, nil
}

// signature is everything the plan was judged from, as one string: two
// reads of the machine that give the same signature would be repaired the
// same way.
func (plan repairPlan) signature() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s\n", plan.libState, plan.libTree.ID, len(plan.libTree.Unrecordable), plan.libBytes,
		strings.Join(plan.copies, ","), strings.Join(plan.copyPaths, ","), strings.Join(plan.sites, ","))
	for _, p := range plan.places {
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00%t\x00%t\x00%t\x00%s\x00%s\n",
			p.path, p.word, p.state, p.tree, p.recordable, p.skill, p.same, p.copied, p.err != nil, strings.Join(p.enabled, ","), strings.Join(p.paths, ","))
	}
	return b.String()
}

// differing are the displaced directories whose content is not the
// library's: the places a repair has to be told what to keep of.
func (plan repairPlan) differing() []repairPlace {
	var found []repairPlace
	for _, p := range plan.places {
		if p.removed() && !p.same {
			found = append(found, p)
		}
	}
	return found
}

// refusal is why the plan cannot be carried out as chosen, nil when it can.
// It is decided before the lock is taken, from what the first read found,
// and nothing has been changed when it refuses. Where symlinks make what
// the repair changes overlap what it must leave alone it refuses first,
// whatever the flags, see overlap. The read under the lock holds the plan
// to the rest: every input they turn on is part of the signature that read
// has to give again.
func (plan repairPlan) refusal(name string, choice repairChoice) error {
	if err := plan.overlap(choice); err != nil {
		return err
	}
	differ := plan.differing()
	if len(differ) == 0 {
		return nil
	}
	keepLibrary := "'" + skillCommand("repair", name, "--keep-library") + "'"
	keepPlacement := "'" + skillCommand("repair", name, "--keep-placement") + "'"
	paths := make([]string, len(differ))
	for i, p := range differ {
		paths[i] = quotedPath(p.path)
	}
	them, is, their := "it", "is a directory whose content differs", "its"
	if len(differ) > 1 {
		them, is, their = "them", "are directories whose content differs", "their"
	}
	switch choice {
	case chooseNone:
		return fail(exitRefused, fmt.Sprintf("%s %s from the library's %s, so nothing was repaired", strings.Join(paths, ", "), is, name),
			"to keep the library's content and discard "+them+", run "+keepLibrary+"; to make "+their+" content the library's, run "+keepPlacement)
	case choosePlacement:
		for _, p := range differ {
			if !p.skill {
				return fail(exitRefused, fmt.Sprintf("%s holds no SKILL.md, so its content cannot become the library's %s", quotedPath(p.path), name),
					"keep the library's content with "+keepLibrary+", or move "+quotedPath(p.path)+" aside and run '"+skillCommand("repair", name)+"' again")
			}
			if !p.recordable {
				return fail(exitRefused, fmt.Sprintf("%s holds what git cannot record, so its content cannot become the library's", quotedPath(p.path)),
					"move the repository or special file out of "+quotedPath(p.path)+", then run "+keepPlacement+" again")
			}
			if p.tree != differ[0].tree {
				return fail(exitRefused, fmt.Sprintf("--keep-placement keeps the content of one directory, and %s hold different content", strings.Join(paths, ", ")),
					"move aside every directory but the one to keep, then run "+keepPlacement+" again, or keep the library's content with "+keepLibrary)
			}
		}
	}
	return nil
}

// touchedPath is a path a repair changes, as the plan names it and where it
// really is, every symlink on the way to it resolved, see canonicalPath.
// library marks the library directory that --keep-placement replaces.
type touchedPath struct {
	path, real string
	library    bool
}

// touched are the paths the plan changes as chosen, each once however many
// configurations name it: when --keep-placement replaces the library
// directory, that directory first and every copy copy_mode records, which
// the new content may refresh, and every place the plan removes or writes,
// a displaced directory, a missing place and the library's symlink where a
// copy belongs. A place this machine cannot read is skipped, not changed.
// replaces reports whether the plan removes or replaces anything at all.
func (plan repairPlan) touched(choice repairChoice) (paths []touchedPath, replaces bool) {
	libraryReplaced := choice == choosePlacement && len(plan.differing()) > 0
	if libraryReplaced {
		paths = append(paths, touchedPath{plan.lib.Path, canonicalPath(plan.lib.Path), true})
	}
	// A place is never merged into the library directory: one that is it
	// has to be found inside the library, see overlap.
	add := func(path string) {
		real := canonicalPath(path)
		if !slices.ContainsFunc(paths, func(t touchedPath) bool { return !t.library && samePath(t.real, real) }) {
			paths = append(paths, touchedPath{path: path, real: real})
		}
	}
	for _, p := range plan.places {
		if p.err == nil {
			add(p.path)
			replaces = replaces || p.word == driftDisplaced
		}
	}
	if libraryReplaced {
		for _, c := range plan.copyPaths {
			add(c)
		}
	}
	return paths, replaces || libraryReplaced
}

// overlap is why the plan cannot be carried out safely, nil when it can. A
// repair removes and writes whole directories, and a symlink can make any
// of them part of something else, so rather than follow links to find out
// what, it refuses, whatever the flags, wherever links make what it changes
// overlap what it must leave alone:
//
//   - The skill's library entry is not a real directory. agentx installs
//     one, and replacing a link would drop it without touching what it
//     leads to.
//   - A path the repair changes, see touched, is, lies inside or holds the
//     library, or the directory a library entry leads to, each compared
//     where it really is. The library directory --keep-placement replaces
//     is compared with the other entries alone.
//   - Two paths the repair changes, or one of them and another
//     configuration's own place of the skill, lie one inside the other:
//     removing the outer one takes the inner one with it, and writing the
//     inner one changes what the outer one was judged to hold.
//   - A repair that removes or replaces anything finds a symlink anywhere
//     in the skill's library directory. An import never holds one, so it
//     is a hand edit whose link may lead into what the repair replaces.
//     With --keep-placement, so does a symlink anywhere in a directory
//     whose content becomes the library's, which would be copied into the
//     library link for link.
//
// A symlink elsewhere on disk that leads into what the repair changes is
// the user's, and no disk is searched for one. The rule is read afresh at
// every call: before the lock, under it and right before staging.
func (plan repairPlan) overlap(choice repairChoice) error {
	name := plan.lib.Name
	again := "run '" + skillCommand("repair", name) + "' again"
	info, err := os.Lstat(plan.lib.Path)
	if err != nil {
		return libraryFailure(plan.library, err)
	}
	if !info.IsDir() {
		target, _ := os.Readlink(plan.lib.Path)
		return fail(exitRefused, fmt.Sprintf("%s is a symlink to %s, not the directory agentx installed, so nothing was repaired", quotedPath(plan.lib.Path), quotedPath(target)),
			"replace the link with the directory it leads to, then "+again)
	}
	root, err := filepath.EvalSymlinks(plan.library)
	if err != nil {
		return libraryFailure(plan.library, err)
	}
	entries, err := linkedEntries(root, plan.library)
	if err != nil {
		return libraryFailure(plan.library, err)
	}
	touched, replaces := plan.touched(choice)
	joined := func(message string) error {
		return fail(exitRefused, message+", so nothing was repaired",
			"replace the symlink that joins them with the files it leads to, or move one of them elsewhere, then "+again)
	}
	for _, t := range touched {
		if !t.library {
			if rel := relation(t.real, root); rel != "" {
				return joined(fmt.Sprintf("%s %s the library %s, and repairing it would change what the library holds", quotedPath(t.path), rel, quotedPath(plan.library)))
			}
		}
		for _, e := range entries {
			switch rel := relation(t.real, e.real); rel {
			case "":
			case "is":
				return joined(fmt.Sprintf("the library entry %s leads to %s, and repairing it would change what the library holds", quotedPath(e.path), quotedPath(t.path)))
			default:
				return joined(fmt.Sprintf("%s %s %s, where the library entry %s leads, and repairing it would change what the library holds", quotedPath(t.path), rel, quotedPath(e.real), quotedPath(e.path)))
			}
		}
	}
	// Every touched path comes first, so a pair holds one of them.
	all := slices.Clone(touched)
	for _, s := range plan.sites {
		real := canonicalPath(s)
		if !slices.ContainsFunc(all, func(t touchedPath) bool { return samePath(t.real, real) }) {
			all = append(all, touchedPath{path: s, real: real})
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
			return joined(fmt.Sprintf("%s lies inside %s, and repairing one would change the other", quotedPath(inner.path), quotedPath(outer.path)))
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
		return fail(exitRefused, fmt.Sprintf("the library directory %s holds the symlink %s, which no version agentx installs holds, so nothing was repaired", quotedPath(plan.lib.Path), quotedPath(link)),
			"replace the link with the files it leads to, or see what changed with '"+skillCommand("diff", name)+"' and go back to the installed version with '"+skillCommand("revert", name)+"', then "+again)
	}
	if choice != choosePlacement {
		return nil
	}
	for _, p := range plan.differing() {
		link, err := firstSymlink(p.path)
		if err != nil {
			return fail(exitRefused, fmt.Sprintf("%s cannot be read whole, so its content cannot become the library's: %v", quotedPath(p.path), err),
				"make "+quotedPath(p.path)+" readable, then run '"+skillCommand("repair", name, "--keep-placement")+"' again")
		}
		if link != "" {
			return fail(exitRefused, fmt.Sprintf("%s holds the symlink %s, so its content cannot become the library's", quotedPath(p.path), quotedPath(link)),
				"replace the link with the files it leads to, then run '"+skillCommand("repair", name, "--keep-placement")+"' again, or keep the library's content with '"+skillCommand("repair", name, "--keep-library")+"'")
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

// stageRepair plans, into m, the whole of a repair plan the lock-time read
// agreed with. With --keep-placement the library directory is replaced
// first, so every placement made after it holds what the library is to
// hold, and the copies copy_mode records are refreshed or kept as a revert
// refreshes or keeps them: a copy holding what the library directory held
// or the base version, whose tree is base, is agentx's and is refreshed.
// Where the library directory holds something git cannot record, a copy
// placed from it holds that too, and is judged byte for byte against it,
// as planRepair judges a displaced directory.
func (inv *invocation) stageRepair(m *home.Mutation, plan repairPlan, choice repairChoice, base string, done *repaired) error {
	name, libPath := plan.lib.Name, plan.lib.Path
	differ := plan.differing()
	libraryReplaced := choice == choosePlacement && len(differ) > 0
	p := libraryPlaceable(plan.lib)
	if libraryReplaced {
		kept := differ[0]
		staged, fingerprint, err := stageRefresh(m, libPath, kept.path, kept.tree)
		if err != nil {
			return libraryFailure(inv.dirs.Library, err)
		}
		hash := contentHashAt(staged)
		if hash == "" {
			os.RemoveAll(staged)
			return libraryFailure(inv.dirs.Library, fmt.Errorf("the content of %s staged at %s holds no SKILL.md", kept.path, staged))
		}
		m.Remove(libPath, plan.libState)
		m.Publish(libPath, staged, fingerprint)
		inv.refreshCopies(m, name, kept.tree, []string{plan.libTree.ID, base}, plan.libBytes, staged, plan.copies, &done.refreshed)
		p = placeable{name: name, hash: hash, stage: func(dest string) error { return copyTreeTo(staged, dest) }}
		done.kept = kept.path
	}
	for _, place := range plan.places {
		inv.stageRepairPlace(m, p, place, libPath, plan.copies, choice, done)
	}
	return nil
}

// stageRepairPlace plans the repair of one place. A place this machine
// cannot read or cannot write is skipped with a warning and counted, as a
// placement that cannot be made is: one client agentx cannot reach is not
// a reason to leave every other one unrepaired.
func (inv *invocation) stageRepairPlace(m *home.Mutation, p placeable, place repairPlace, libPath string, copies []string, choice repairChoice, done *repaired) {
	t := place.representative(copies)
	switch {
	case place.err != nil:
		done.skipped = append(done.skipped, place.path)
		inv.out.warn("cannot repair " + place.path + ": " + place.err.Error() + "; it was left as it is")
	case home.IsDir(place.state):
		if !place.same && choice == chooseNone {
			return // refused before the lock; never planned
		}
		// Only the symlink is written, and it is written where the directory
		// was, so what can fail is the directory it sits in: checked now,
		// before a step of this place is recorded, as a placement checks it.
		var scratch placements
		if _, _, err := inv.placementContent(m, p, t, place.path, false); err != nil {
			inv.skipPlacement(&scratch, t, place.path, err)
			done.skipped = append(done.skipped, scratch.skipped...)
			return
		}
		m.Remove(place.path, place.state)
		m.Link(place.path, libPath)
		done.put(place, repairRelinked, modeSymlink)
		if !place.same && choice == chooseLibrary {
			done.discarded = append(done.discarded, place.path)
		}
	default:
		// Nothing at the place, or the library's own link where a copy
		// belongs: the placement skill place would make, made by the same
		// staging and refused for the same reasons.
		var one placements
		inv.stagePlacement(m, p, t, libPath, place.copied, copies, &one)
		done.skipped = append(done.skipped, one.skipped...)
		if len(one.placed) == 0 {
			return
		}
		mode := modeSymlink
		if len(one.copies) > 0 {
			mode = modeCopy
		}
		action := repairPlaced
		if place.word == driftDisplaced {
			action = repairCopied
		}
		done.put(place, action, mode)
	}
}

// representative is the configuration a place is repaired as: the first
// enabled one copy_mode records a copy for, so that a copy placed at a
// place two configurations share is the one the settings already name, and
// the first enabled one otherwise.
func (p repairPlace) representative(copies []string) placeTarget {
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

// put records that the place was put back, action saying how, with one
// row for each enabled configuration whose own place it is, at the path
// that configuration names it by.
func (d *repaired) put(place repairPlace, action, mode string) {
	for i, t := range place.targets {
		if slices.Contains(place.enabled, t.id) {
			d.rows = append(d.rows, repairRow{id: t.id, action: action, mode: mode, path: place.paths[i]})
		}
	}
}

// inDetectionOrder puts the rows in the order the configurations were
// detected, targets being every detected one: they are recorded place by
// place, and a place two configurations share records both together.
func (d *repaired) inDetectionOrder(targets []placeTarget) {
	at := make(map[string]int, len(targets))
	for i, t := range targets {
		at[t.id] = i
	}
	slices.SortStableFunc(d.rows, func(a, b repairRow) int { return at[a.id] - at[b.id] })
}

// reportRepaired reads the machine again and reports the skill as it now
// stands. done is what the mutation did, nil when drift found nothing to
// repair and nothing was changed.
func (inv *invocation) reportRepaired(ctx context.Context, name string, done *repaired) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return fail(exitInternal, "the library holds no "+name+" after repairing it", "run 'agentx doctor' and check the library it names")
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	inv.out.emit(sc.librarySkillEventFor(inv, snap, lib, nil))
	out := inv.out
	if done == nil {
		inv.summary = name + " has no missing or displaced placement; nothing was repaired"
		out.print(out.paint(heading, sanitised(name)), " has no missing or displaced placement; nothing was repaired")
		return nil
	}
	skipped := len(done.skipped) + len(done.refreshed.skipped)
	inv.summary = fmt.Sprintf("repaired %s in %s", name, plural(len(done.rows), "configuration"))
	line := "repaired " + out.paint(heading, sanitised(name)) + " in " + out.paint(noteStyle, plural(len(done.rows), "configuration"))
	if n := len(done.refreshed.copies); n > 0 {
		inv.summary += ", " + plural(n, "copy placement") + " refreshed"
		line += ", " + out.paint(noteStyle, plural(n, "copy placement")+" refreshed")
	}
	if skipped > 0 {
		inv.summary += ", " + plural(skipped, "placement") + " skipped"
		line += ", " + out.paint(warnStyle, plural(skipped, "placement")+" skipped")
	}
	if done.kept != "" {
		inv.summary += "; the library now holds what " + done.kept + " held"
	}
	if len(done.discarded) > 0 {
		inv.summary += "; discarded what " + strings.Join(done.discarded, ", ") + " held"
	}
	out.done(line)
	t := &table{}
	for _, r := range done.rows {
		path := quotedPath(r.path)
		if r.mode == modeSymlink {
			path += out.paint(muted, " -> "+quotedPath(lib.Path))
		}
		t.add(c("  "+r.id, label), c(r.action, noteStyle), c(r.mode, muted), c(path, plain))
	}
	out.render(t, "")
	if done.kept != "" {
		out.print("  ", out.paint(muted, "the library now holds what "+quotedPath(done.kept)+" held"))
	}
	for _, p := range done.discarded {
		out.print("  ", out.paint(muted, "discarded what "+quotedPath(p)+" held"))
	}
	return nil
}
