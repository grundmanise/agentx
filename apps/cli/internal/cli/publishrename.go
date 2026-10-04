package cli

import (
	"context"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A rename stays on the machine that made it, see skillRename, until the
// renamed skill is published: the push creates the new name's branch on
// the account remote, and the publish then deletes the old name's branch,
// which it finds by the fork id the renamed skill kept and by the rename
// its history records, once the renamed skill holds all of it. An old
// branch that holds changes the renamed skill lacks, another machine's, is
// kept, with a warning. A deletion the account remote refuses warns too,
// and every later publish tries again; neither fails the publish.

// renamedBranches pairs the account remote's branches this machine holds
// no skill of, remoteOnly, name to fork id, with the skills of a publish,
// selected, name to fork id: a branch whose fork id exactly one skill of
// the publish holds may be that skill's old name, old to new, which the
// new name's history then tells, see renamesFrom. A fork id two skills of
// the publish hold, as a machine that installed the renamed skill beside
// the old one does, pairs nothing, nor does an empty one, nor a name the
// publish itself holds. Pure.
func renamedBranches(selected, remoteOnly map[string]string) map[string]string {
	holders := map[string][]string{}
	for name, id := range selected {
		if id != "" {
			holders[id] = append(holders[id], name)
		}
	}
	pairs := map[string]string{}
	for old, id := range remoteOnly {
		if _, held := selected[old]; held || id == "" || len(holders[id]) != 1 {
			continue
		}
		pairs[old] = holders[id][0]
	}
	return pairs
}

// dropRenamed deletes from the account remote, the git remote called
// account, the old name's branch of every skill of list that was pushed or
// is up to date and was renamed on this machine, see renamedBranches and
// renamesFrom. records are the account repo's branches, with each fork's
// lineage, and remote what the fetch read of the account remote, whose
// branches no skill here holds are walked once for their fork ids. An old
// branch whose tip the renamed skill's branch there holds is deleted,
// leased on that tip; one that holds changes the renamed skill lacks is
// kept, and a deletion git cannot make or the account remote rejects is
// not made: each warns, and the publish goes on.
func (inv *invocation) dropRenamed(ctx context.Context, gitDir, account string, records map[string]lineage.Record, remote remoteForks, list []*publishing) {
	selected := map[string]string{}
	tips := map[string]string{}
	for _, p := range list {
		rec := records[p.name]
		if (p.outcome == publishPushed || p.outcome == publishUpToDate) && p.commit != "" && rec.Fork != nil {
			selected[p.name], tips[p.name] = rec.Fork.ID, p.commit
		}
	}
	if len(selected) == 0 {
		return
	}
	var walk []string
	for name, tip := range remote.tips {
		if records[name].Kind != lineage.KindFork {
			walk = append(walk, tip)
		}
	}
	walked, err := lineage.Walk(ctx, inv.git, gitDir, walk)
	if err != nil {
		inv.out.debugf("renamed skills: %v", err)
		return
	}
	remoteOnly := map[string]string{}
	for name, tip := range remote.tips {
		if records[name].Kind != lineage.KindFork {
			remoteOnly[name] = walked[tip].ID
		}
	}
	pairs := renamedBranches(selected, remoteOnly)
	olds := make([]string, 0, len(pairs))
	for old := range pairs {
		olds = append(olds, old)
	}
	sort.Strings(olds)
	for _, old := range olds {
		inv.dropRenamedBranch(ctx, gitDir, account, old, pairs[old], remote.tips[old], tips[pairs[old]])
	}
}

// dropRenamedBranch deletes the account remote's branch of old, at
// oldTip, which the skill called newName was renamed from, when newTip,
// newName's branch there, holds it, and warns when it does not or when
// the deletion is not made.
func (inv *invocation) dropRenamedBranch(ctx context.Context, gitDir, account, old, newName, oldTip, newTip string) {
	out := inv.out
	branch := forkBranch(old)
	// Only the skill renamed from old pairs with it: a machine that still
	// holds old pairs the renamed skill's branch with its old, whose
	// history records no rename, and leaves it be.
	subjects, err := inv.git.Isolated(ctx, gitDir, "log", "--first-parent", "--format=%s", oldTip+".."+newTip)
	if err != nil {
		out.debugf("%s: %v", old, err)
		return
	}
	if !renamesFrom(subjects, old) {
		return
	}
	held, err := inv.isAncestor(ctx, gitDir, oldTip, newTip)
	if err != nil {
		inv.out.debugf("%s: %v", old, err)
		return
	}
	if !held {
		out.warnWith("the account remote's "+branch+" holds changes "+sanitised(newName)+" lacks, so it was kept",
			"install it beside "+sanitised(newName)+" with '"+accountAddCommand(old)+"', or delete it with '"+skillCommand("remove", old, "--remote")+"'")
		return
	}
	s, err := inv.git.DeleteRemoteBranch(ctx, gitDir, account, strings.TrimPrefix(lineage.ForkRef(old), "refs/heads/"), oldTip)
	switch {
	case err != nil:
		out.warnWith(remoteStillHolds(old, "")+", which "+sanitised(newName)+" was renamed from: "+trimGit(err.Error()), renameAgain(newName))
	case s.Rejected():
		message, hint := renameDeleteWarning(old, newName, s)
		out.warnWith(message, hint)
	default:
		out.done("deleted " + out.paint(heading, branch) + " from the account remote (renamed to " + sanitised(newName) + ")")
	}
}

// renamesFrom reports whether subjects, one commit subject a line, as git
// log lists a skill's history since the account remote's branch of old,
// holds the commit skill rename writes of a rename from old, see
// lineage.RenameSubject. Pure.
func renamesFrom(subjects, old string) bool {
	for _, line := range strings.Split(subjects, "\n") {
		if lineage.RenamedFrom(line) == old {
			return true
		}
	}
	return false
}

// renameDeleteWarning is the warning, its message and its hint, of a
// deletion of the account remote's branch of old, which newName was
// renamed from, that the account remote rejected, s being what git push
// reported of it: the reason, as a removal says it, see
// remoteDeleteRefusal, and that the next publish of newName tries again,
// once the account remote's default branch is another one when that is
// why. Pure.
func renameDeleteWarning(old, newName string, s gitx.PushStatus) (string, string) {
	hint := renameAgain(newName)
	if strings.Contains(s.Why(), "current branch") {
		hint = "make another branch the account remote's default branch on its hosting service; " + hint
	}
	return remoteDeleteRefusal(old, "", s).message, hint
}

// renameAgain is the hint of an old name's branch a publish of newName
// did not delete.
func renameAgain(newName string) string {
	return "the next '" + publishCommand(newName) + "' tries again"
}

// isAncestor is whether the commit a is b or one b's history holds, as
// git merge-base --is-ancestor says.
func (inv *invocation) isAncestor(ctx context.Context, gitDir, a, b string) (bool, error) {
	_, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", "--is-ancestor", a, b)
	return err == nil && status == 0, err
}
