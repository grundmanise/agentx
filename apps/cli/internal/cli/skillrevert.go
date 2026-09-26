package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		return inv.reportReverted(ctx, name, against, nil)
	}
	base, err := lineage.ReadBase(ctx, inv.git, gitDir, rec)
	if err != nil {
		return accountRepoFailure(err)
	}
	bodies, err := source.ReadBlobs(ctx, inv.git, gitDir, baseBlobs(base))
	if err != nil {
		return accountRepoFailure(err)
	}
	var done placements
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
		live, err := home.State(libPath)
		if err != nil {
			return libraryFailure(inv.dirs.Library, err)
		}
		if live != captured {
			return fail(exitRefused, name+" changed while it was being reverted, so nothing was discarded",
				"run '"+skillCommand("diff", name)+"' to see the change, then revert again to discard it too")
		}
		sweepStaged(inv.dirs.Library)
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		done = placements{}
		m := home.NewMutation(inv.dirs.Home)
		staged := m.Sibling(libPath, "staged")
		fingerprint, err := stageBase(staged, base, bodies)
		if err != nil {
			os.RemoveAll(staged)
			return libraryFailure(inv.dirs.Library, err)
		}
		// The branch does not move: the step holds the journal to the base
		// the content came from, so that a revert finished by recovery puts
		// back the version the branch still names and no other.
		m.Ref(gitDir, lineage.ManagedRef(name), rec.Commit, rec.Commit)
		m.Remove(libPath, captured)
		m.Publish(libPath, staged, fingerprint)
		inv.refreshCopies(m, name, base.Tree, []string{edited.ID}, staged, edit.copiesOf(name), &done)
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return mutationFailure(err)
	}
	return inv.reportReverted(ctx, name, against, &done)
}

// stageBase lays the base version out at staged and reads it back as git
// would record it: a directory that is not the base version never gets
// published. It returns the fingerprint the publish step expects.
func stageBase(staged string, base lineage.Base, bodies map[string]string) (string, error) {
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
	if tree.ID != base.Tree {
		return "", fmt.Errorf("the base version staged at %s holds tree %s, not %s", staged, tree.ID, base.Tree)
	}
	return home.Fingerprint(staged)
}

// reportReverted reads the machine again and reports the skill as it now
// stands. done is what the mutation did to the copies, nil when the library
// already held the base version and nothing was changed.
func (inv *invocation) reportReverted(ctx context.Context, name, against string, done *placements) error {
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
	if done == nil {
		inv.summary = name + " already matches " + against + "; nothing was reverted"
		out.print(out.paint(heading, sanitised(name)), " already matches ", against, "; nothing was reverted")
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
