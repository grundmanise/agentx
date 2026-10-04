package cli

import (
	"context"
	"errors"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// A command that moves a fork's branch, an update of it or a fork of it,
// records the unpublished edits its skill directory holds first, as a
// commit of their own on the branch, see recordFirst, and then works on
// the branch with them: they are never merged over or lost, and they stay
// unpublished until the next publish. Under the lock, it refuses a merge
// pending, exit code 4, and then edits that appeared after the recording,
// exit code 6, each before anything changes, as git merge refuses over a
// dirty work tree. A command that works on the edits themselves,
// publishing, comparing or discarding them, is not refused for having
// them.

// forkPendingRefusal refuses to do what to the fork called name while it
// has a merge pending: the merge holds the fork's tip as it was, and
// moving the branch meanwhile would leave it merging a version that is no
// longer there.
func forkPendingRefusal(name, what string) *failure {
	return refuse(exitPendingMerge, sanitised(name)+" has a merge pending, so it cannot be "+what+" until the merge is resolved or given up",
		"run '"+skillCommand("update", name, "--abort")+"' to give the merge up; the fork's worktree and branch stay as they are")
}

// uncommittedRefusal refuses to do what to the fork called name when its
// skill directory holds edits its branch does not record once the command
// recorded the ones it found, see recordFirst: edits made while it ran,
// which nobody judged. A file git ignores is no such edit, since no commit
// would record it either. The refusal wraps errUncommitted, see
// renameFinish.
func uncommittedRefusal(name, what string) *failure {
	return refuse(exitRefused, sanitised(name)+" changed while it was being "+what, "run the command again").wrap(errUncommitted)
}

// errUncommitted is what a refusal for edits made while a command ran
// wraps, so that a rename whose removal of the old fork it refused can say
// how to carry the edits over, see renameFinish.
var errUncommitted = errors.New("unpublished edits")

// recordFirst is the first step of a command that moves the branch of the
// fork at site, before it judges anything against the branch's tip: the
// edits its skill directory holds, judged against the tip, are recorded as
// one commit on the branch, with a generated subject, in a journal of their
// own, see writeRecord and applyRecord, so the command works on a tip that
// holds them and keeps them. They stay unpublished, as edits a publish
// whose push failed recorded do, until the next publish. It returns the
// site with its branch at the new tip, which is the old one when there was
// nothing to record, and the directory as judged against it.
func (inv *invocation) recordFirst(ctx context.Context, w **forkWriter, site forkSite, what string, lost func([]string) *failure) (forkSite, siteJudged, *failure) {
	c, j, f := inv.writeRecord(ctx, w, site, lost)
	if f != nil || c == nil {
		return site, j, f
	}
	return inv.applyRecord(ctx, c, what, j)
}

// writeRecord judges the edits of the fork at site against its tip and
// writes the commit that records them, without moving the branch: nothing
// points at it until applyRecord does, so a command that can still refuse
// once it knows what the commit holds, as skill fork does, records nothing
// when it refuses. Before anything is written, a repository nested in the
// directory that no ignore rule covers is refused, see nestedRepoRefusal,
// and so is what git cannot record and no ignore rule covers when lost is
// given, which answers for it: laying a new tip out over the directory
// would discard it. *w is the fork commit writer: when it is nil and there
// are edits, one is read into it, for the caller to write its own commits
// with. The commit is nil when the directory holds the tip.
func (inv *invocation) writeRecord(ctx context.Context, w **forkWriter, site forkSite, lost func([]string) *failure) (*committing, siteJudged, *failure) {
	j, err := inv.judgeSite(ctx, site, false)
	switch {
	case err != nil:
		return nil, siteJudged{}, failureOf(err)
	case len(j.exposed) > 0:
		return nil, siteJudged{}, failureOf(nestedRepoRefusal(site.name, j.exposed))
	}
	if _, gone := splitUnrecordable(j.forkJudged); len(gone) > 0 && lost != nil {
		return nil, siteJudged{}, lost(gone)
	}
	if j.clean {
		return nil, j, nil
	}
	if *w == nil {
		if *w, err = inv.newForkWriter(ctx, site.gitDir); err != nil {
			return nil, siteJudged{}, failureOf(err)
		}
	}
	c := &committing{site: site, captured: j.captured, root: j.written}
	if err := inv.writeCommits(ctx, *w, site.gitDir, []*committing{c}, ""); err != nil {
		return nil, siteJudged{}, failureOf(err)
	}
	return c, j, nil
}

// applyRecord moves the fork's branch to the commit c writeRecord wrote,
// in a journal of its own, see applyCommits, refusing in the words of a
// command that was being what when the skill changed since it was judged,
// j. It returns the site with its branch at the commit and the directory
// as judged against it.
func (inv *invocation) applyRecord(ctx context.Context, c *committing, what string, j siteJudged) (forkSite, siteJudged, *failure) {
	site := c.site
	var dropped *failure
	again := func(string) string { return "run the command again" }
	if what == "updated" {
		again = func(name string) string {
			return "run '" + skillCommand("update", name) + "' again to update it as it is now"
		}
	}
	_, err := inv.applyCommits(ctx, site.gitDir, []*committing{c}, recordWords{what: what, again: again}, func(_ string, f *failure) { dropped = f })
	switch {
	case err != nil:
		return site, siteJudged{}, failureOf(err)
	case dropped != nil:
		return site, siteJudged{}, dropped
	}
	site.rec.Commit, site.rec.Tree = c.commit, c.root
	j.clean, j.captured = true, c.captured
	return site, j, nil
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
		return siteJudged{}, skillDirMissing(f)
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

// skillDirMissing refuses a fork whose worktree holds no skill directory,
// which git restores from the branch.
func skillDirMissing(f forkSite) error {
	return fail(exitRefused, sanitised(f.name)+"'s skill directory "+quotedPath(f.skillDir)+" is missing",
		"run 'git -C "+shellWord(f.root)+" restore "+shellWord(f.dir)+"' to put it back as the branch holds it")
}

// forkGuards runs, under the lock, the refusals of a command that would do
// what to the fork f: a merge pending first, and then edits the command
// did not record, see uncommittedRefusal. pre is how judgeSite found the skill directory
// before the lock; a directory that changed since is judged again, as pre
// was. It returns the directory as it is now: the fingerprint the
// command's journal expects, and, for a caller that asked for them, the
// ignored files it is to carry over.
func (inv *invocation) forkGuards(ctx context.Context, f forkSite, pre siteJudged, what string) (siteJudged, error) {
	if inv.mergePending(f.name) {
		return siteJudged{}, forkPendingRefusal(f.name, what)
	}
	return inv.siteNow(ctx, f, pre, what)
}

// siteNow is forkGuards with no merge pending to refuse: the skill
// directory of f as it is now, judged again when it changed since pre,
// and refused when it holds edits its tip does not record,
// made since the command recorded the ones it found. Completing a fork's
// pending merge asks it, since the merge it completes is the one pending.
func (inv *invocation) siteNow(ctx context.Context, f forkSite, pre siteJudged, what string) (siteJudged, error) {
	live, err := home.State(f.skillDir)
	if err != nil {
		return siteJudged{}, libraryFailure(f.root, err)
	}
	now := pre
	if live != pre.captured {
		if now, err = inv.judgeSite(ctx, f, pre.wantIgnored); err != nil {
			return siteJudged{}, err
		}
	}
	if !now.clean {
		return siteJudged{}, uncommittedRefusal(f.name, what)
	}
	return now, nil
}
