package cli

import (
	"context"
	"errors"
	"os"
	"slices"

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
			"one was. Both steps are checked before either runs, so a merge pending or a name\n" +
			"that cannot be used change nothing. Unpublished edits are recorded on the old\n" +
			"fork's branch first, and the renamed fork starts from them, reading current\n" +
			"until it is edited again; publish it to push them. Files git ignores\n" +
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
// planFork, and what skill remove refuses of old, see judgeForkRemoval.
// Old's unpublished edits are recorded on its branch only then, as the
// fork is made, see makeFork, so a refused rename records nothing. A
// removal that fails once the fork is made says so, names both skills and
// the command that finishes the rename.
func (inv *invocation) skillRename(ctx context.Context, old, newName string, remote bool) error {
	if old == newName {
		return fail(exitUsage, sanitised(old)+" already has that name", "give the new name the fork should have")
	}
	fk, err := inv.planFork(ctx, old, newName, true)
	if err != nil {
		return err
	}
	if fk.src.kind != lineage.KindFork {
		return fail(exitRefused, sanitised(old)+" is not a fork, so it cannot be renamed",
			"fork it under the new name with '"+skillCommand("fork", old, "--name", newName)+"', then remove it with '"+skillCommand("remove", old)+"'")
	}
	if err := inv.keepCopies(fk); err != nil {
		return err
	}
	renameHere := skillCommand("rename", old, newName)
	r, err := inv.judgeForkRemoval(ctx, old, remote, "rename it on this machine alone with '"+renameHere+"'")
	if err != nil {
		return err
	}
	// No fork of the name, with no --remote asked for, is a removal made
	// meanwhile by another command.
	if r == nil || r.tip != fk.src.rec.Commit {
		return fail(exitRefused, sanitised(old)+" changed while it was being renamed, so nothing was changed", "run the command again")
	}
	// The renamed fork holds the old one's history as this machine has it:
	// commits another machine published that this one never took in would
	// go with the remote branch and be in neither fork.
	if r.remote && r.there != "" && r.there != r.tip {
		held, err := inv.isAncestor(ctx, r.gitDir, r.there, r.tip)
		if err != nil {
			return accountRepoFailure(err)
		}
		if !held {
			return fail(exitRefused, "the account remote's "+forkBranch(old)+" holds commits "+sanitised(old)+" on this machine lacks, which removing it from the account remote would delete",
				"run '"+skillCommand("update", old)+"' first, or rename it without --remote with '"+renameHere+"'")
		}
	}
	if err := inv.makeFork(ctx, fk); err != nil {
		return err
	}
	// The fork recorded old's edits on its branch, see makeFork, so the
	// removal finds the branch at the tip that holds them.
	r.tip, r.guard = fk.src.rec.Commit, &forkGuard{site: fk.site, judged: fk.judged}
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
		return fail(f.status, made+", but "+sanitised(old)+" could not be removed: "+f.message, renameFinish(old, newName, finish, remote, err))
	}
	inv.summary = "renamed " + sanitised(old) + " to " + sanitised(newName) + "; " + inv.summary
	inv.out.done("renamed " + inv.out.paint(heading, sanitised(old)) + " to " + inv.out.paint(heading, sanitised(newName)))
	return nil
}

// keepCopies adds to the configurations the new fork fk plans is placed
// into every one where copy_mode records a copy of the fork it is made
// from, and that copy is still there. The fork goes where its source is,
// see planBeside, and a copy only counts as a placement while it holds the
// library's content, which it stops doing at the fork's next commit; the
// rename removes the copy all the same, so the new fork takes its place
// as a copy, up to date.
func (inv *invocation) keepCopies(fk *forking) error {
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	modes, err := inv.copyModes(s)
	if err != nil {
		return err
	}
	planned := map[string]bool{}
	for _, t := range fk.targets {
		planned[t.id] = true
	}
	var targets []placeTarget
	for _, t := range inv.detectedTargets() {
		if !planned[t.id] && !t.readsLibrary && slices.Contains(modes[fk.src.name], t.id) {
			_, err := os.Lstat(t.ownPlace(inv.dirs.Library, fk.src.name))
			planned[t.id] = err == nil
		}
		if planned[t.id] {
			targets = append(targets, t)
		}
	}
	fk.targets = targets
	return nil
}

// renameFinish is the hint of a rename whose fork of old was made as
// newName but whose removal of old failed with err; finish removes old.
// Most failures leave old as it was forked, and finish completes the
// rename. Unpublished edits and a branch that moved since the fork
// are work newName lacks, which finish would delete: the hint says how to
// carry it over first, by removing newName and renaming again. Pure.
func renameFinish(old, newName, finish string, remote bool, err error) string {
	again := skillCommand("rename", old, newName)
	if remote {
		again = skillCommand("rename", old, newName, "--remote")
	}
	carry := "run '" + skillCommand("remove", newName) + "' and '" + again + "' again"
	switch {
	case errors.Is(err, errUncommitted):
		return "to keep the edits, " + carry + ", which records them; to drop them, run '" + finish + "'"
	case errors.Is(err, errMovedWhileRemoved):
		return sanitised(old) + "'s branch moved since " + sanitised(newName) + " was forked from it; to keep what it holds now, " +
			carry + "; to drop it, run '" + finish + "'"
	}
	return "run '" + finish + "' to finish the rename"
}

// isAncestor is whether the commit a is b or one b's history holds, as
// git merge-base --is-ancestor says.
func (inv *invocation) isAncestor(ctx context.Context, gitDir, a, b string) (bool, error) {
	_, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", "--is-ancestor", a, b)
	return err == nil && status == 0, err
}
