package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

func newSkillRenameCommand(inv *invocation) *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:   "rename <old> <new> [--remote]",
		Short: "Rename a fork: fork it under the new name, then remove it",
		Long: "Rename the fork called <old> to <new> by running what you could run yourself:\n" +
			"'agentx skill fork <old> --name <new>', then 'agentx skill remove <old>', with\n" +
			"--remote passed on to the removal. The renamed skill is a new fork, with a fork\n" +
			"id of its own, whose history holds the old one's commits, placed where the old\n" +
			"one was. Both steps are checked before either runs, so a merge pending, edits\n" +
			"nobody committed or a name that cannot be used change nothing. Files git ignores\n" +
			"in the old fork's worktree are deleted with it. Another machine sees the renamed\n" +
			"fork as a new fork to install, and keeps the old one until it is removed there.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillRename(cmd.Context(), args[0], args[1], remote)
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "remove the old fork's branch from the account remote too")
	return cmd
}

// skillRename renames the fork called old to newName. It is the fork of a
// fork followed by the fork's removal, each its own journaled mutation, and
// nothing else: no rename is recorded anywhere, and the new fork has a
// fork id of its own. Before either step runs, everything either would
// refuse is asked: what skill fork refuses of old and of newName, see
// planFork, and what skill remove refuses of old, see judgeForkRemoval. A
// removal that fails once the fork is made says so, names both skills and
// the command that finishes the rename.
func (inv *invocation) skillRename(ctx context.Context, old, newName string, remote bool) error {
	if old == newName {
		return fail(exitUsage, sanitised(old)+" already has that name", "give the new name the fork should have")
	}
	fk, err := inv.planFork(ctx, old, newName)
	if err != nil {
		return err
	}
	if fk.src.kind != lineage.KindFork {
		return fail(exitRefused, sanitised(old)+" is not a fork, so it cannot be renamed",
			"fork it under the new name with '"+skillCommand("fork", old, "--name", newName)+"', then remove it with '"+skillCommand("remove", old)+"'")
	}
	r, err := inv.judgeForkRemoval(ctx, old, remote)
	if err != nil {
		return err
	}
	if r.tip != fk.src.rec.Commit {
		return fail(exitRefused, sanitised(old)+" changed while it was being renamed, so nothing was changed", "run the command again")
	}
	r.guard = &forkGuard{site: fk.site, judged: fk.judged}
	if err := inv.makeFork(ctx, fk); err != nil {
		return err
	}
	made := sanitised(newName) + " was created"
	if err := inv.runForkRemoval(ctx, r, made); err != nil {
		f := failureOf(err)
		if r.remote && f.status == exitSource {
			return f // the fork is made and old is removed here; the failure says so
		}
		finish := skillCommand("remove", old)
		if remote {
			finish = skillCommand("remove", old, "--remote")
		}
		return fail(f.status, made+", but "+sanitised(old)+" could not be removed: "+f.message, "run '"+finish+"' to finish the rename")
	}
	inv.summary = "renamed " + sanitised(old) + " to " + sanitised(newName) + "; " + inv.summary
	inv.out.done("renamed " + inv.out.paint(heading, sanitised(old)) + " to " + inv.out.paint(heading, sanitised(newName)))
	return nil
}

func newSkillUnforkCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:    "unfork <name>",
		Short:  "Reserved for a later version",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "<name>"
			if len(args) > 0 {
				name = args[0]
			}
			return fail(exitRefused, "skill unfork is reserved for a later version",
				"remove the fork with '"+skillCommand("remove", name)+"' and install the upstream again with 'agentx skill add'")
		},
	}
}
