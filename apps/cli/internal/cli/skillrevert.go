package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

func newSkillRevertCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "revert <name>",
		Short: "Put a managed skill back to the version it was installed at",
		Long: "Put the library directory of a managed skill back to its base version, the\n" +
			"version it was installed at, discarding every edit made to it since: files added\n" +
			"are deleted, files changed or deleted are restored. A copy placement that holds\n" +
			"the edited content is put back too; a copy edited on its own is kept and named.\n" +
			"Run 'agentx skill diff <name>' first to see what the revert discards.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillRevert(cmd.Context(), args[0])
		},
	}
}

// skillRevert puts a managed skill's library directory back to its base
// version: the import commit's tree is laid out in a hidden directory beside
// the library directory, which the mutation then retains and replaces, so a
// file the base does not hold is gone afterwards, and the copies that held
// the edited content are refreshed with it.
//
// Reverting discards, so what is discarded is what the directory held when
// the command began and nothing later. That content is captured first, by
// the fingerprint the journal compares, and read again under the lock: an
// edit made in the meantime, by any tool, refuses the revert rather than
// being discarded with the rest. The journal's remove step carries the same
// fingerprint, and the content it retains is dropped only when it still
// hashes to it, so an edit made while the mutation runs is kept as well.
//
// A branch an earlier agentx wrote over a source's own tree, one that
// stores a mode git no longer writes, is never current against any
// directory, so a revert that left it would leave the skill modified. The
// same mutation moves it, from the commit it holds, to the commit an
// install of that version writes today, which is all a revert of a library
// directory that already holds the base's files does.
func (inv *invocation) skillRevert(ctx context.Context, name string) error {
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return inv.noLibrarySkill(name)
	}
	gitDir, rec, err := inv.managedRecord(ctx, name, "revert to")
	if err != nil {
		return err
	}
	libPath := inv.libraryPath(name)
	captured, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	edited, err := inv.readLibraryTree(lib.Path)
	if err != nil {
		return err
	}
	if len(edited.Unrecordable) > 0 {
		paths := make([]string, len(edited.Unrecordable))
		for i, p := range edited.Unrecordable {
			paths[i] = quotedPath(filepath.Join(libPath, filepath.FromSlash(p)))
		}
		return fail(exitRefused, fmt.Sprintf("%s holds %s, which git cannot record", name, strings.Join(paths, ", ")),
			"a revert would discard it with no record of it anywhere; move it out of the skill, then run '"+skillCommand("revert", name)+"' again")
	}
	against := "its base version at " + short(rec.Import.Commit)
	if rec.Current(edited) {
		return inv.reportReverted(ctx, name, against, revertNothing, placements{})
	}
	base, err := lineage.ReadBase(ctx, inv.git, gitDir, rec)
	if err != nil {
		return accountRepoFailure(err)
	}
	target := base.ID() // the tree the base has laid out on disk
	restore := !base.HeldBy(edited)
	// A library entry that is a symlink leads to the directory the user
	// edits, which is not the library's to replace: the mutation replaces
	// the entry itself, so it would drop the link, leave every edit where
	// the link led, and report the skill as reverted. Laying the base out
	// where the link leads instead would stage and sweep in a directory
	// outside the library, perhaps one another tool keeps.
	if target, isLink := home.LinkTarget(captured); isLink && restore {
		return fail(exitRefused, fmt.Sprintf("%s is a symlink to %s; a revert replaces the library directory and would drop the link without touching the files it leads to", quotedPath(libPath), quotedPath(target)),
			"replace the link with the directory it points to, then run '"+skillCommand("revert", name)+"' again, or put the files back by hand: '"+skillCommand("diff", name)+"' shows what differs")
	}
	var bodies map[string]string
	if restore {
		if bodies, err = source.ReadBlobs(ctx, inv.git, gitDir, baseBlobs(base)); err != nil {
			return accountRepoFailure(err)
		}
	}
	// The branch stays at its commit, unless that commit stores the base
	// in a form no directory is current against: then it moves to the one
	// an install writes today, held by a staging ref of this run's own
	// until the journal that names it is applied, as an install's is.
	recorded, run := rec.Commit, ""
	if !rec.Canonical(base) {
		run = lineage.NewRun()
		if recorded, err = lineage.Rewrite(ctx, inv.git, gitDir, run, rec, base); err != nil {
			inv.dropImporting(ctx, gitDir, run, 1)
			return accountRepoFailure(err)
		}
	}
	var done placements
	journaled := false
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		// Every input is read again under the lock: the branch the base came
		// from, and the directory the user asked to discard.
		refs, err := inv.lineageRefs(ctx, gitDir, name)
		if err != nil {
			return err
		}
		if refs[lineage.ManagedRef(name)] != rec.Commit || refs[lineage.ForkRef(name)] != "" {
			return fail(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved while "+name+" was being reverted, so nothing was discarded",
				"run '"+skillCommand("diff", name)+"' to see the base version now, then revert again")
		}
		m := home.NewMutation(inv.dirs.Home)
		// The ref step comes first and is the only step when the library
		// already holds the base's files. When the branch does not move, the
		// step holds the journal to the base the content came from, so that
		// a revert finished by recovery puts back the version the branch
		// still names and no other.
		m.Ref(gitDir, lineage.ManagedRef(name), rec.Commit, recorded)
		if restore {
			if err := inv.stageRevert(m, name, libPath, captured, base, target, bodies, edited.ID, &done); err != nil {
				m.Discard()
				return err
			}
		}
		applied := m.Apply(inv.refs(ctx))
		journaled = m.Journaled()
		return applied
	})
	// The staging ref goes once the branch holds the commit, or when no
	// journal that recovery could finish names it.
	if run != "" && (err == nil || !journaled) {
		inv.dropImporting(ctx, gitDir, run, 1)
	}
	if err != nil {
		return mutationFailure(err)
	}
	if !restore {
		return inv.reportReverted(ctx, name, against, revertBranch, done)
	}
	return inv.reportReverted(ctx, name, against, revertLibrary, done)
}

// stageRevert records, under the lock, the steps that put the library
// directory back to the base and refresh the copies that held it: the
// directory is read again first, and one that changed since it was
// captured refuses the revert, so an edit made in the meantime is never
// discarded with the rest.
func (inv *invocation) stageRevert(m *home.Mutation, name, libPath, captured string, base lineage.Base, target string, bodies map[string]string, edited string, done *placements) error {
	live, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	if live != captured {
		return fail(exitRefused, name+" changed while it was being reverted, so nothing was discarded",
			"run '"+skillCommand("diff", name)+"' to see the change, then revert again to discard it too")
	}
	// A revert killed before its journal was written left what it staged
	// with nothing to name it: beside the library directory, and beside
	// each copy it was refreshing. All of it is swept before anything is
	// staged, since a sweep of a directory two configurations share would
	// take a sibling this plan staged a moment earlier.
	edit, err := inv.beginSettings()
	if err != nil {
		return err
	}
	recorded := edit.copiesOf(name)
	sweepStaged(inv.dirs.Library)
	for _, t := range inv.detectedTargets() {
		if !t.readsLibrary && slices.Contains(recorded, t.id) {
			sweepStaged(t.dir)
		}
	}
	*done = placements{}
	staged := m.Sibling(libPath, "staged")
	fingerprint, err := stageBase(staged, base, target, bodies)
	if err != nil {
		os.RemoveAll(staged)
		return libraryFailure(inv.dirs.Library, err)
	}
	m.Remove(libPath, captured)
	m.Publish(libPath, staged, fingerprint)
	inv.refreshCopies(m, name, target, []string{edited}, "", staged, recorded, done)
	return nil
}

// stageBase lays the base version out at staged and reads it back as git
// would record it: a directory that is not the base version, whose tree is
// target, never gets published. It returns the fingerprint the publish
// step expects.
func stageBase(staged string, base lineage.Base, target string, bodies map[string]string) (string, error) {
	if err := materialise(staged, base, bodies); err != nil {
		return "", err
	}
	if err := home.SyncTree(staged); err != nil {
		return "", err
	}
	tree, err := treeid.Read(staged)
	if err != nil {
		return "", err
	}
	if tree.ID != target {
		return "", fmt.Errorf("the base version staged at %s holds tree %s, not %s", staged, tree.ID, target)
	}
	return home.Fingerprint(staged)
}

// revertOutcome is what a revert changed.
type revertOutcome int

const (
	revertNothing revertOutcome = iota // the library held the base, and the branch stored it as git writes it
	revertBranch                       // the library held the base's files; the branch was stored again
	revertLibrary                      // the library directory was put back, and the branch too when it had to be
)

// reportReverted reads the machine again and reports the skill as it now
// stands. done is what the mutation did to the copies, which only a revert
// that put the library directory back touched.
func (inv *invocation) reportReverted(ctx context.Context, name, against string, outcome revertOutcome, done placements) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return fail(exitInternal, "the library holds no "+name+" after reverting it", "run 'agentx doctor' and check the library it names")
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	inv.out.emit(sc.librarySkillEventFor(inv, snap, lib, nil))
	out := inv.out
	switch outcome {
	case revertNothing:
		inv.summary = name + " already matches " + against + "; nothing was reverted"
		out.print(out.paint(heading, sanitised(name)), " already matches ", against, "; nothing was reverted")
		return nil
	case revertBranch:
		const stored = "; nothing was reverted, and its import branch now stores that version as git writes it today"
		inv.summary = name + " already matches " + against + stored
		out.done(out.paint(heading, sanitised(name)) + " already matches " + against + stored)
		return nil
	}
	inv.summary = "reverted " + name + " to " + against
	line := "reverted " + out.paint(heading, sanitised(name)) + " to " + against
	if n := len(done.copies); n > 0 {
		inv.summary += ", " + plural(n, "copy placement") + " refreshed"
		line += ", " + out.paint(noteStyle, plural(n, "copy placement")+" refreshed")
	}
	if n := len(done.skipped); n > 0 {
		inv.summary += ", " + plural(n, "placement") + " skipped"
		line += ", " + out.paint(warnStyle, plural(n, "placement")+" skipped")
	}
	out.done(line)
	return nil
}
