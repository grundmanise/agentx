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
	return &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename one of your skills",
		Long: "Rename the skill called <old>, one of your own skills, to <new> on this machine,\n" +
			"placed where <old> was. The renamed skill is the same skill: it keeps its id and\n" +
			"its history, and a commit on top writes the new name into SKILL.md. Everything\n" +
			"the rename needs is checked before anything changes, so a merge pending or a\n" +
			"name that cannot be used change nothing.\n" +
			"Unpublished edits are recorded on the old branch first, and the renamed skill\n" +
			"keeps them, still unpublished. Files git ignores in the old worktree are deleted\n" +
			"with it. The rename stays on this machine until 'agentx skill publish <new>',\n" +
			"which pushes skills/<new> to the account remote and deletes skills/<old> there\n" +
			"when <new> holds everything it holds. Another machine keeps <old> until it is\n" +
			"removed there, and sees <new> as a skill to install.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillRename(cmd.Context(), args[0], args[1])
		},
	}
}

// skillRename renames old, one of your own skills, to newName on this
// machine. It is the fork of a fork followed by the fork's removal, each
// its own journaled mutation; the fork step writes no fork id, see
// makeFork, so newName keeps old's, and the next publish of newName finds
// old's branch on the account remote by it and deletes it, see
// dropRenamed. Before either step runs, everything either would refuse is
// asked: what skill fork refuses of old and of newName, see planFork, and
// what skill remove refuses of old, see judgeForkRemoval. Old's
// unpublished edits are recorded on its branch only then, as the fork is
// made, see makeFork, so a refused rename records nothing. A removal that
// fails once the fork is made says so, names both skills and the command
// that finishes the rename. Nothing is fetched or pushed.
func (inv *invocation) skillRename(ctx context.Context, old, newName string) error {
	if old == newName {
		return fail(exitUsage, sanitised(old)+" already has that name", "give the new name the skill should have")
	}
	fk, err := inv.planFork(ctx, old, newName, true)
	if err != nil {
		return err
	}
	if fk.src.kind != lineage.KindFork {
		return fail(exitRefused, sanitised(old)+" is not a skill of the account remote, so it cannot be renamed",
			"fork it under the new name with '"+skillCommand("fork", old, "--name", newName)+"', then remove it with '"+skillCommand("remove", old)+"'")
	}
	if err := inv.keepCopies(fk); err != nil {
		return err
	}
	r, err := inv.judgeForkRemoval(ctx, old, false)
	if err != nil {
		return err
	}
	// No fork of the name is a removal made meanwhile by another command.
	if r == nil || r.tip != fk.src.rec.Commit {
		return fail(exitRefused, sanitised(old)+" changed while it was being renamed, so nothing was changed", "run the command again")
	}
	if err := inv.makeFork(ctx, fk); err != nil {
		return err
	}
	// The fork recorded old's edits on its branch, see makeFork, so the
	// removal finds the branch at the tip that holds them.
	r.tip, r.guard, r.quiet = fk.src.rec.Commit, &forkGuard{site: fk.site, judged: fk.judged}, true
	made := sanitised(newName) + " was created"
	if err := inv.runForkRemoval(ctx, r); err != nil {
		f := failureOf(err)
		return fail(f.status, made+", but "+sanitised(old)+" could not be removed: "+f.message, renameFinish(old, newName, skillCommand("remove", old), err))
	}
	if fk.renamed != nil {
		fk.renamed()
	}
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
func renameFinish(old, newName, finish string, err error) string {
	carry := "run '" + skillCommand("remove", newName) + "' and '" + skillCommand("rename", old, newName) + "' again"
	switch {
	case errors.Is(err, errUncommitted):
		return "to keep the edits, " + carry + ", which records them; to drop them, run '" + finish + "'"
	case errors.Is(err, errMovedWhileRemoved):
		return sanitised(old) + "'s branch moved since " + sanitised(newName) + " was forked from it; to keep what it holds now, " +
			carry + "; to drop it, run '" + finish + "'"
	}
	return "run '" + finish + "' to finish the rename"
}
