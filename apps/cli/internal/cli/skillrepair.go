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
			"from the library is never deleted without one of them, and one that holds the\n" +
			"directory a library entry is a symlink to is never replaced at all.\n\n" +
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
	holds      string // the library entry whose directory the directory is or holds, see holdsLibraryDirectory
	linksInto  string // a symlink in the directory, relative to it, that leads into a path the repair replaces, see linkIntoReplaced
	leadsInto  string // the path the repair replaces that linksInto leads into
	err        error  // what kept this machine from reading the place
}

// repairPlan is everything a repair judged, read once before the lock and
// again under it, where the two have to agree: the library entry, what the
// library directory holds, and every place drift finds missing or
// displaced, in the order the configurations were detected.
type repairPlan struct {
	lib      scan.LibrarySkill
	libState string      // the library entry, in the words a journal records
	libTree  treeid.Tree // what the library directory holds, as git would record it
	libBytes string      // what the library directory holds byte for byte, read only when git cannot record all of it
	copies   []string    // the configurations copy_mode records a copy of the skill for
	places   []repairPlace
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
// place is no drift, and a repair never touches it.
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
// only stands guard. A displaced directory that holds such a directory
// deeper down, this skill's or another's, is still planned, and names the
// entry in holds: the repair refuses it, since replacing it would delete
// that skill's content, see refusal.
//
// A directory holds what the library directory holds when git would record
// the two the same way. A tree leaves out what git cannot record, a nested
// repository or a named pipe, so where either holds any of it the two are
// compared byte for byte instead, as the journal fingerprints them.
func (inv *invocation) planRepair(lib scan.LibrarySkill, targets []placeTarget, disabled, copies []string) (repairPlan, error) {
	plan := repairPlan{lib: lib, copies: copies}
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
		if place.err == nil && home.IsDir(place.state) {
			place.holds, _ = holdsLibraryDirectory(p.path, inv.dirs.Library)
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
	// A directory whose content can become the library's is copied into
	// the library link for link. A link in it that leads into a path
	// --keep-placement replaces, the library directory, another displaced
	// directory or a copy copy_mode records, would lead once copied into
	// the library's new content, often to itself, and what it led to would
	// be discarded with the rest; see refusal.
	replaced := []string{lib.Path}
	for _, place := range plan.places {
		if place.err == nil && home.IsDir(place.state) {
			replaced = append(replaced, place.path)
		}
	}
	for _, t := range targets {
		if !t.readsLibrary && slices.Contains(copies, t.id) {
			replaced = append(replaced, t.ownPlace(inv.dirs.Library, lib.Name))
		}
	}
	for i, place := range plan.places {
		if place.err == nil && home.IsDir(place.state) && !place.same {
			plan.places[i].linksInto, plan.places[i].leadsInto = linkIntoReplaced(place.path, replaced)
		}
	}
	return plan, nil
}

// linkIntoReplaced names the first symlink in the real directory dir, as a
// path relative to it, that leads into one of replaced, and the one of
// replaced it leads into; "" and "" when none does. A link is followed on
// the machine as it stands, from where it sits, and where it ends up is
// walked up to the root and compared with each of replaced as files, not
// as paths, as isLibraryDirectory compares them, so no spelling hides it.
// A link that leads somewhere inside dir itself is its own content and
// nothing replaced, and one that does not resolve leads nowhere.
func linkIntoReplaced(dir string, replaced []string) (string, string) {
	self, err := os.Stat(dir)
	if err != nil {
		return "", ""
	}
	var paths []string
	var infos []os.FileInfo
	for _, r := range replaced {
		if info, err := os.Stat(r); err == nil && !os.SameFile(info, self) {
			paths, infos = append(paths, r), append(infos, info)
		}
	}
	var link, into string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&os.ModeSymlink == 0 {
			return nil // a directory this machine cannot read failed the tree read already
		}
		at, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil
		}
		for {
			if info, err := os.Stat(at); err == nil {
				if os.SameFile(info, self) {
					return nil
				}
				for i := range infos {
					if os.SameFile(info, infos[i]) {
						link, _ = filepath.Rel(dir, path)
						into = paths[i]
						return fs.SkipAll
					}
				}
			}
			parent := filepath.Dir(at)
			if parent == at {
				return nil
			}
			at = parent
		}
	})
	return link, into
}

// signature is everything the plan was judged from, as one string: two
// reads of the machine that give the same signature would be repaired the
// same way.
func (plan repairPlan) signature() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00%d\x00%s\x00%s\n", plan.libState, plan.libTree.ID, len(plan.libTree.Unrecordable), plan.libBytes, strings.Join(plan.copies, ","))
	for _, p := range plan.places {
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00%t\x00%s\x00%s\x00%s\x00%t\x00%t\x00%s\x00%s\n",
			p.path, p.word, p.state, p.tree, p.recordable, p.skill, p.same, p.holds, p.linksInto, p.leadsInto, p.copied, p.err != nil, strings.Join(p.enabled, ","), strings.Join(p.paths, ","))
	}
	return b.String()
}

// differing are the displaced directories whose content is not the
// library's: the places a repair has to be told what to keep of.
func (plan repairPlan) differing() []repairPlace {
	var found []repairPlace
	for _, p := range plan.places {
		if p.err == nil && home.IsDir(p.state) && !p.same {
			found = append(found, p)
		}
	}
	return found
}

// refusal is why the plan cannot be carried out as chosen, nil when it can.
// It is decided before the lock is taken, from what the first read found,
// and nothing has been changed when it refuses. The read under the lock
// holds the plan to it: every input a refusal turns on, the library entry
// in libState among them, is part of the signature that read has to give
// again, so a library entry made a symlink meanwhile refuses the run there.
//
// A displaced directory that holds the directory a library entry leads to
// is refused whatever the flags: every repair of a directory replaces it
// with the symlink, and that would delete the skill's content with it.
// With --keep-placement, so is a directory holding a link into a path the
// repair replaces: its content, copied into the library, would lose what
// the link leads to.
func (plan repairPlan) refusal(name string, choice repairChoice) error {
	for _, p := range plan.places {
		if p.holds != "" {
			return fail(exitRefused, fmt.Sprintf("%s holds the directory the library entry %s leads to, and replacing it would delete that skill's content, so nothing was repaired", quotedPath(p.path), quotedPath(p.holds)),
				"replace the link "+quotedPath(p.holds)+" with the directory it leads to, moving that out of "+quotedPath(p.path)+", then run '"+skillCommand("repair", name)+"' again")
		}
	}
	differ := plan.differing()
	if len(differ) == 0 {
		return nil
	}
	paths := make([]string, len(differ))
	for i, p := range differ {
		paths[i] = quotedPath(p.path)
	}
	them, is, their := "it", "is a directory whose content differs", "its"
	if len(differ) > 1 {
		them, is, their = "them", "are directories whose content differs", "their"
	}
	keepLibrary := "'" + skillCommand("repair", name, "--keep-library") + "'"
	keepPlacement := "'" + skillCommand("repair", name, "--keep-placement") + "'"
	switch choice {
	case chooseNone:
		return fail(exitRefused, fmt.Sprintf("%s %s from the library's %s, so nothing was repaired", strings.Join(paths, ", "), is, name),
			"to keep the library's content and discard "+them+", run "+keepLibrary+"; to make "+their+" content the library's, run "+keepPlacement)
	case choosePlacement:
		libPath := quotedPath(plan.lib.Path)
		if target, isLink := home.LinkTarget(plan.libState); isLink {
			return fail(exitRefused, fmt.Sprintf("%s is a symlink to %s; keeping the placement replaces the library directory and would drop the link without touching the files it leads to", libPath, quotedPath(target)),
				"replace the link with the directory it points to, then run "+keepPlacement+" again, or keep the library's content with "+keepLibrary)
		}
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
			if p.linksInto != "" {
				return fail(exitRefused, fmt.Sprintf("%s in %s is a symlink into %s, which keeping the placement replaces, so its content cannot become the library's", quotedPath(p.linksInto), quotedPath(p.path), quotedPath(p.leadsInto)),
					"replace the link "+quotedPath(p.linksInto)+" with the files it leads to, then run "+keepPlacement+" again, or keep the library's content with "+keepLibrary)
			}
		}
	}
	return nil
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
	p := libraryPlaceable(plan.lib)
	if differ := plan.differing(); choice == choosePlacement && len(differ) > 0 {
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
		if entry, held := holdsLibraryDirectory(place.path, inv.dirs.Library); held {
			// Neither read found a library entry leading into the
			// directory, or the run would have refused. One made a symlink
			// into it since is found here, and the directory is left:
			// removing it would take that skill's content with it.
			done.skipped = append(done.skipped, place.path)
			inv.out.warn(place.path + " holds the directory the library entry " + entry + " leads to; it was left as it is")
			return
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
