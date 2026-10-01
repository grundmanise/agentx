package cli

import (
	"context"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// A command that moves a fork's branch refuses while the fork has a merge
// pending or edits nobody committed, as git merge refuses over a dirty
// work tree: moving the branch lays the new tip out over the skill
// directory, and edits that are in no commit would be merged over or lost.
// The merge pending is asked about first, exit code 4, then the edits,
// exit code 6, each before anything changes. A command that works on the
// edits themselves, committing, comparing or discarding them, is not
// refused for having them.

// forkPendingRefusal refuses to do what to the fork called name while it
// has a merge pending: the merge holds the fork's tip as it was, and
// moving the branch meanwhile would leave it merging a version that is no
// longer there.
func forkPendingRefusal(name, what string) *failure {
	return refuse(exitPendingMerge, sanitised(name)+" has a merge pending, so it cannot be "+what+" until the merge is resolved or given up",
		"run '"+skillCommand("update", name, "--abort")+"' to give the merge up; the fork's worktree and branch stay as they are")
}

// uncommittedRefusal refuses to do what to the fork called name while its
// skill directory holds edits its branch does not record. A file git
// ignores is no such edit, since no commit would record it either.
func uncommittedRefusal(name, what string) *failure {
	return refuse(exitRefused, sanitised(name)+" has uncommitted edits, so it cannot be "+what+" until they are committed or reverted",
		"run '"+skillCommand("commit", name)+"' to keep them, or '"+skillCommand("revert", name)+"' to discard them, then run the command again")
}

// judgeSite captures what the fork's skill directory holds, as the
// fingerprint a journal compares, and then judges it against the fork's
// tip, in that order, so that an edit made while git reads the directory
// shows as a fingerprint that no longer holds.
func (inv *invocation) judgeSite(ctx context.Context, f forkSite, wantIgnored bool) (string, forkJudged, error) {
	captured, err := home.State(f.skillDir)
	if err != nil {
		return "", forkJudged{}, libraryFailure(f.root, err)
	}
	if home.IsAbsent(captured) {
		return "", forkJudged{}, fail(exitRefused, sanitised(f.name)+"'s skill directory "+quotedPath(f.skillDir)+" is missing",
			"run 'git -C "+shellWord(f.root)+" restore "+shellWord(f.dir)+"' to put it back as the branch holds it")
	}
	j, err := inv.judgeFork(ctx, f.gitDir, f.skillDir, f.version(), wantIgnored)
	if err != nil {
		return "", forkJudged{}, accountRepoFailure(err)
	}
	return captured, j, nil
}

// forkGuards runs, under the lock, the refusals of a command that would do
// what to the fork f: a merge pending first, and then, when the command
// moves the fork's branch, needsClean, edits nobody committed. pre is how
// the skill directory compared with the tip before the lock, when it held
// what captured fingerprints; a directory that changed since is judged
// again. It returns the fingerprint the directory holds now, which the
// command's journal expects.
func (inv *invocation) forkGuards(ctx context.Context, f forkSite, captured string, pre forkJudged, what string, needsClean bool) (string, error) {
	if inv.mergePending(f.name) {
		return "", forkPendingRefusal(f.name, what)
	}
	live, err := home.State(f.skillDir)
	if err != nil {
		return "", libraryFailure(f.root, err)
	}
	if !needsClean {
		return live, nil
	}
	j := pre
	if live != captured {
		if live, j, err = inv.judgeSite(ctx, f, false); err != nil {
			return "", err
		}
	}
	if !j.clean {
		return "", uncommittedRefusal(f.name, what)
	}
	return live, nil
}
