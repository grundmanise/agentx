package cli

import (
	"context"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
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

// siteJudged is a fork's skill directory as judgeSite found it: the
// fingerprint captured before git read it, as a journal compares it, and
// how it compared with the fork's tip, with the files git ignores in it
// when the caller asked for them.
type siteJudged struct {
	forkJudged
	captured    string
	wantIgnored bool
}

// judgeSite captures what the fork's skill directory holds, as the
// fingerprint a journal compares, and then judges it against the fork's
// tip, in that order, so that an edit made while git reads the directory
// shows as a fingerprint that no longer holds. A caller that will lay a
// new tip out over the directory asks for its ignored files, wantIgnored,
// which the new layout carries over.
func (inv *invocation) judgeSite(ctx context.Context, f forkSite, wantIgnored bool) (siteJudged, error) {
	captured, err := home.State(f.skillDir)
	if err != nil {
		return siteJudged{}, libraryFailure(f.root, err)
	}
	if home.IsAbsent(captured) {
		return siteJudged{}, fail(exitRefused, sanitised(f.name)+"'s skill directory "+quotedPath(f.skillDir)+" is missing",
			"run 'git -C "+shellWord(f.root)+" restore "+shellWord(f.dir)+"' to put it back as the branch holds it")
	}
	t, err := treeid.Read(f.skillDir)
	if err != nil {
		return siteJudged{}, libraryFailure(f.root, err)
	}
	j, err := inv.judgeTip(ctx, f, t, wantIgnored)
	if err != nil {
		return siteJudged{}, accountRepoFailure(err)
	}
	return siteJudged{forkJudged: j, captured: captured, wantIgnored: wantIgnored}, nil
}

// forkGuards runs, under the lock, the refusals of a command that would do
// what to the fork f: a merge pending first, and then, when the command
// moves the fork's branch, needsClean, edits nobody committed. pre is how
// judgeSite found the skill directory before the lock; a directory that
// changed since is judged again, as pre was. It returns the directory as
// it is now: the fingerprint the command's journal expects, and, for a
// caller that asked for them, the ignored files it is to carry over.
func (inv *invocation) forkGuards(ctx context.Context, f forkSite, pre siteJudged, what string, needsClean bool) (siteJudged, error) {
	if inv.mergePending(f.name) {
		return siteJudged{}, forkPendingRefusal(f.name, what)
	}
	live, err := home.State(f.skillDir)
	if err != nil {
		return siteJudged{}, libraryFailure(f.root, err)
	}
	now := pre
	if live != pre.captured {
		if !needsClean && !pre.wantIgnored {
			now.captured = live
			return now, nil
		}
		if now, err = inv.judgeSite(ctx, f, pre.wantIgnored); err != nil {
			return siteJudged{}, err
		}
	}
	if needsClean && !now.clean {
		return siteJudged{}, uncommittedRefusal(f.name, what)
	}
	return now, nil
}
