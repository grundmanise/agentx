package cli

import (
	"context"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A rename stays on the machine that made it, see skillRename, until the
// renamed skill is published. The publish that creates the new name's
// branch on the account remote deletes the old name's branch there: the
// nearest name the renamed skill's own history records it was renamed
// from whose branch the account remote holds, see remoteForks.branchOf,
// once that branch is the same skill by its fork id and the new branch
// holds its tip. Only that publish does: every later one finds the new
// name's branch there and leaves the old name's be, so a branch put back
// on purpose stays put back. An old branch that holds changes the renamed
// skill lacks, another machine's, is kept, and a deletion git cannot make
// or the account remote rejects is not made: each warns once, naming the
// removal that deletes it, and neither fails the publish or is tried
// again. An old name this machine holds a skill of, or whose branch holds
// another skill, is not the renamed skill's old branch, and is left be
// without a word.

// renamedBranch is the name of the account remote's branch that the
// publish of the fork rec, the one that creates rec's branch there,
// deletes as rec's old name, see the comment above: remote is what the
// publish's fetch read, before its push, and records are the account
// repo's branches. "" when the account remote held rec's own name
// already, when it holds no former name of rec's that is the same skill,
// or when this machine holds a skill of its own under that name, whose
// branch it then is. Pure.
func renamedBranch(rec lineage.Record, records map[string]lineage.Record, remote remoteForks) string {
	if rec.Fork == nil || remote.tips[rec.Name] != "" {
		return ""
	}
	old := remote.branchOf(rec)
	if old == "" || records[old].Kind == lineage.KindFork {
		return ""
	}
	return old
}

// renamed is an old name's branch a publish deletes, see renamedBranch:
// that of old, which the skill called name, at tip on the account remote
// once pushed, was renamed from.
type renamed struct{ old, name, tip string }

// renamedDrops is the old names' branches the publish of list deletes
// from the account remote, sorted by old name: one for every skill of
// list the publish pushed whose branch it created there, see
// renamedBranch, records being the account repo's branches, with each
// fork's lineage, and remote what the fetch read of the account remote
// before the push. Each old name's branch is handled once, by the first
// skill by name that was renamed from it. Pure.
func renamedDrops(list []*publishing, records map[string]lineage.Record, remote remoteForks) []renamed {
	var drops []renamed
	for _, p := range list {
		if p.outcome != publishPushed || p.commit == "" {
			continue
		}
		if old := renamedBranch(records[p.name], records, remote); old != "" {
			drops = append(drops, renamed{old, p.name, p.commit})
		}
	}
	sort.Slice(drops, func(i, j int) bool {
		if drops[i].old != drops[j].old {
			return drops[i].old < drops[j].old
		}
		return drops[i].name < drops[j].name
	})
	var once []renamed
	for _, d := range drops {
		if len(once) == 0 || once[len(once)-1].old != d.old {
			once = append(once, d)
		}
	}
	return once
}

// dropRenamed deletes from the account remote, the git remote called
// account, the old names' branches of the publish of list, see
// renamedDrops.
func (inv *invocation) dropRenamed(ctx context.Context, gitDir, account string, records map[string]lineage.Record, remote remoteForks, list []*publishing) {
	for _, d := range renamedDrops(list, records, remote) {
		inv.dropRenamedBranch(ctx, gitDir, account, d.old, d.name, remote.tips[d.old], d.tip)
	}
}

// dropRenamedBranch deletes the account remote's branch of old, at
// oldTip, which the skill called newName was renamed from, when newTip,
// newName's branch there, holds it, and warns when it does not, when that
// cannot be told, or when the deletion is not made.
func (inv *invocation) dropRenamedBranch(ctx context.Context, gitDir, account, old, newName, oldTip, newTip string) {
	out := inv.out
	branch := forkBranch(old)
	held, err := inv.isAncestor(ctx, gitDir, oldTip, newTip)
	switch {
	case err != nil:
		out.warnWith("cannot tell whether "+sanitised(newName)+" holds the account remote's "+branch+", which it was renamed from, so it was kept: "+trimGit(err.Error()),
			"install it beside "+sanitised(newName)+" with '"+accountAddCommand(old)+"', or "+removeRenamed(old))
		return
	case !held:
		out.warnWith("the account remote's "+branch+" holds changes "+sanitised(newName)+" lacks, so it was kept",
			"install it beside "+sanitised(newName)+" with '"+accountAddCommand(old)+"', or "+removeRenamed(old))
		return
	}
	s, err := inv.git.DeleteRemoteBranch(ctx, gitDir, account, strings.TrimPrefix(lineage.ForkRef(old), "refs/heads/"), oldTip)
	switch {
	case err != nil:
		out.warnWith(remoteStillHolds(old, "")+", which "+sanitised(newName)+" was renamed from: "+trimGit(err.Error()), removeRenamed(old))
	case s.Rejected():
		message, hint := renameDeleteWarning(old, newName, s)
		out.warnWith(message, hint)
	default:
		out.done("deleted " + out.paint(heading, branch) + " from the account remote (renamed to " + sanitised(newName) + ")")
	}
}

// renameDeleteWarning is the warning, its message and its hint, of a
// deletion of the account remote's branch of old, which newName was
// renamed from, that the account remote rejected, s being what git push
// reported of it: the reason, as a removal says it, see
// remoteDeleteRefusal, and the removal that deletes it, since the
// deletion is never retried: once the account remote's default branch is
// another one when that is why, and with the install beside newName first
// when another machine moved it since the fetch. Pure.
func renameDeleteWarning(old, newName string, s gitx.PushStatus) (string, string) {
	hint := removeRenamed(old)
	switch why := s.Why(); {
	case strings.Contains(why, "current branch"):
		hint = "make another branch the account remote's default branch on its hosting service, then " + hint
	case strings.Contains(why, "stale info"):
		hint = "another machine moved it since the fetch: install it beside " + sanitised(newName) + " with '" + accountAddCommand(old) + "', or " + hint
	}
	return remoteDeleteRefusal(old, "", s).message, hint
}

// removeRenamed is the hint of an old name's branch a publish did not
// delete, which no later publish retries.
func removeRenamed(old string) string {
	return "delete it with '" + skillCommand("remove", old, "--remote") + "'"
}

// isAncestor is whether the commit a is b or one b's history holds, as
// git merge-base --is-ancestor says.
func (inv *invocation) isAncestor(ctx context.Context, gitDir, a, b string) (bool, error) {
	_, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", "--is-ancestor", a, b)
	return err == nil && status == 0, err
}
