package cli

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A fork takes a newer upstream version by merging it, always, whether or
// not it holds commits of its own: git merges the fork's tip with the
// update's import commit, with the fork's base version, the import its
// history names, given as the merge base, never one git computes or one
// grafted on. So the fork's own changes are kept, a line it reverted to an
// older upstream text stays reverted, and what changed upstream alone comes
// in clean. A clean merge is committed by the fork commit writer, with the
// tip and the update as its parents and the update named as the fork's new
// base, the branch moves to it and the worktree's skill directory is laid
// out from it, all in one journal. A merge that conflicts becomes a
// pending merge, as a managed skill's does, in a checkout under agentx
// home: the fork's worktree and branch stay as they are until the merge is
// resolved there with git and the next update of the fork completes it,
// see judgeForkCompletion, or it is given up.
//
// The update records the fork's unpublished edits first, as a commit of
// their own on its branch, see recordFirst, and merges the update into the
// tip that holds them, so they are never merged over or lost. They stay
// unpublished until the next publish.

// forkUpdate is what the update of a fork reads before the lock besides
// what every update reads, see updating, and applies under it.
type forkUpdate struct {
	site forkSite
	// laidTip is the tip the branch held before the command recorded the
	// skill directory's edits on it, see recordFirst: what agentx last laid
	// out, which a copy placement of the fork may still hold.
	laidTip string
	judged  siteJudged // the skill directory, captured and judged with its ignored files
	// commit is the commit the branch moves to, the merge of the update or
	// the merge completed, "" for a merge that conflicts.
	commit string
	base   string // the import the commit, or the merge left pending, records as the fork's base
	laid   version
	lay    func(dest string) error
	// start is a merge that conflicts and is to be left pending under the
	// lock: a new one, or, when remerge, the merge completed merged again
	// in its checkout with commits the fork's branch gained meanwhile.
	start   bool
	remerge bool
	message string // the message of the commit that completes a merge left pending, its MERGE_MSG
	// theirsRef is the ref the commit the fork takes in was read from, which
	// has to hold theirs still under the lock: the update candidate for an
	// update, the remote-tracking branch for the account step; "" for a
	// merge pending being completed, which takes in what it merged,
	// whatever the ref holds now.
	theirsRef, theirs string
	// with is what a merge left pending merges, as the line that reports it
	// names it, see updating.conflictsWith; "" for an update from upstream.
	with string
	// pull is set for the account step of an update, which takes in what
	// the account remote holds and says so in its refusals; an update from
	// upstream leaves it unset.
	pull bool
	// stale is the update candidate the fork's new base passed: when the
	// commit moves the base to another import than the candidate, a
	// candidate the source history does not prove newer than that import
	// would take the fork back to an older version, so it is deleted with
	// the rest, see staleCandidate. "" when the candidate stays.
	stale string
}

// staleCandidate is the update candidate of the fork rec that a commit
// recording base, the import commit imp names, as the fork's new base
// leaves behind, see forkUpdate.stale: "" when the base stays as it is,
// when the fork has no candidate agentx can read, when the candidate is
// the new base, which the update deletes anyway, and when the source
// history proves the candidate newer, see lineage.Newer, which is then
// still an update. Anything else, an older version, one on another branch
// of the source or one whose order nothing proves, is the candidate's
// commit: the next update check pins the update again against the new
// base.
func (inv *invocation) staleCandidate(ctx context.Context, gitDir string, rec lineage.Record, base string, imp lineage.Import) string {
	c := rec.Candidate
	switch {
	case base == "" || c == nil || !c.HasImport || c.Commit == base:
		return ""
	case rec.Fork != nil && rec.Fork.Base == base:
		return ""
	case lineage.Newer(ctx, inv.git, gitDir, imp, c.Import):
		return ""
	}
	return c.Commit
}

// forkBaseRecord is the record of the fork rec's base version, see
// lineage.Record.ForkBase, refusing a fork that has none to update from:
// one whose history does not say, which is the account repo's to sort
// out, exit code 8, and one with no upstream at all, exit code 6.
func forkBaseRecord(rec lineage.Record) (lineage.Record, *failure) {
	if base, ok := rec.ForkBase(); ok {
		return base, nil
	}
	name := sanitised(rec.Name)
	if rec.Fork != nil && rec.Fork.Problem != "" {
		return lineage.Record{}, refuse(exitAccountRepo, "the history of "+name+" does not say which upstream version it is based on: "+sanitised(rec.Fork.Problem),
			"run '"+skillCommand("history", rec.Name)+"' to read it, and 'agentx doctor' to check the account repo")
	}
	return lineage.Record{}, refuse(exitRefused, name+" has no upstream to update from",
		"a skill created with 'agentx skill new', or forked from an unmanaged or a plugin's skill, has no upstream version; its versions are its own commits")
}

// forkBaseOf is forkBaseRecord for a fork whose lineage was not read yet:
// its history is walked first, in one git log.
func (inv *invocation) forkBaseOf(ctx context.Context, gitDir string, rec lineage.Record) (lineage.Record, error) {
	if rec.Fork == nil {
		recs := map[string]lineage.Record{rec.Name: rec}
		if err := lineage.ReadForks(ctx, inv.git, gitDir, recs, inv.forkWalks); err != nil {
			return lineage.Record{}, accountRepoFailure(err)
		}
		rec = recs[rec.Name]
	}
	base, f := forkBaseRecord(rec)
	if f != nil {
		return lineage.Record{}, f
	}
	return base, nil
}

// judgeForkUpdate decides, before the lock, whether the fork whose branch
// is rec can be updated, and merges its update. A fork with a merge
// pending is judged by its checkout instead, see judgeForkCompletion. It
// refuses, in this order: a fork with no base version to update from, see
// forkBaseRecord; one whose base came from a source removed from this
// machine, exit code 5 as for a managed skill; a candidate that holds the
// skill under another source or upstream directory than the base, which
// the account repo's check never pins, exit code 8; a worktree git cannot
// work in, see worktreeHealth; and a skill directory holding a repository
// no ignore rule covers, or what git cannot record that the new layout
// would discard, exit code 6. A fork with no candidate, or with its base
// as its candidate, is up to date: u and f are then both nil. Then the
// unpublished edits of the skill directory are recorded, see recordFirst,
// and the fork is updated from the tip that holds them.
//
// The merge is merge-tree's, as a managed skill's is, see mergeVersions:
// mine the fork's tip, theirs the candidate, and the base the import the
// fork's history names, given explicitly. A clean one is committed here,
// outside the lock, by the fork commit writer, with the tip and the
// candidate as its parents and the candidate as its Agentx-Base, and is
// laid out under the lock, see applyFork.
func (inv *invocation) judgeForkUpdate(ctx context.Context, gitDir string, rec lineage.Record, sources map[string]bool) (*updating, *failure) {
	name := rec.Name
	if inv.mergePending(name) {
		return inv.judgeForkCompletion(ctx, gitDir, rec)
	}
	base, f := forkBaseRecord(rec)
	if f != nil {
		return nil, f
	}
	if !sources[base.Import.Source] {
		return nil, removedSourceRefusal(name, base.Import.Source)
	}
	next, ok := base.AtCandidate()
	if !ok {
		return nil, nil
	}
	if next.Import.Source != base.Import.Source || next.Import.Path != base.Import.Path {
		return nil, refuse(exitAccountRepo, "the update candidate "+lineage.CandidateRef(name)+" holds "+sanitised(name)+" under another source or directory than its base version",
			"run '"+checkUpdatesCommand+"' to pin the update again")
	}
	site, err := inv.forkSiteOf(ctx, gitDir, rec)
	if err != nil {
		return nil, failureOf(err)
	}
	if site.dir != base.Import.Dir() {
		return nil, refuse(exitAccountRepo, "the branch "+rec.Ref+" holds "+sanitised(name)+" under "+quotedPath(site.dir)+", not under "+quotedPath(base.Import.Dir())+" as its base version does",
			"run '"+skillCommand("history", name)+"' and 'agentx doctor' to see how it got there")
	}
	// The writer reads the user's core.excludesFile with their identity,
	// before git reads the skill directory with the ignore rules it names.
	w, err := inv.newForkWriter(ctx, gitDir)
	if err != nil {
		return nil, failureOf(err)
	}
	fork := &forkUpdate{site: site, base: next.Commit, theirsRef: lineage.CandidateRef(name), theirs: next.Commit}
	// Edits are recorded first, and the merge takes the tip that holds them.
	fork.laidTip = site.rec.Commit
	if site, _, f = inv.recordFirst(ctx, &w, site, "updated", func(lost []string) *failure { return fork.unrecordable(site, lost) }); f != nil {
		return nil, f
	}
	rec, fork.site = site.rec, site
	pre, f := inv.cleanSite(ctx, site, fork)
	if f != nil {
		return nil, f
	}
	fork.judged = pre
	u := &updating{
		name: name, rec: base, next: next, libPath: site.libPath,
		merge: lineage.Merge{Base: base.Commit, Mine: rec.Commit, Theirs: next.Commit},
		fork:  fork,
	}
	if u.merged, err = mergeVersions(ctx, inv.git, gitDir, u.merge); err != nil {
		return nil, failureOf(accountRepoFailure(err))
	}
	subject := upstreamMergeSubject(name, w.label, next.Import.Commit)
	if u.merged.conflicted {
		files, err := conflictFiles(u.merged.stages, site.dir)
		if err == nil {
			u.fork.message, err = w.mergeMessage(subject, next.Commit)
		}
		if err != nil {
			return nil, failureOf(accountRepoFailure(err))
		}
		u.conflict, u.fork.start = conflictOfSkill(name, u.merge, files), true
		return u, nil
	}
	commit, err := w.commit(ctx, u.merged.tree, []string{rec.Commit, next.Commit}, forkMessage{subject: subject, trailers: lineage.ForkTrailers{Base: next.Commit}})
	if err != nil {
		return nil, failureOf(accountRepoFailure(err))
	}
	if f := inv.layFork(ctx, u, commit); f != nil {
		return nil, f
	}
	return u, nil
}

// cleanSite judges the skill directory of the fork site before a command
// that lays a new tip out over it, with the files git ignores in it, which
// the new layout carries over: a repository nested in it that no ignore
// rule covers, anything else git cannot record that no rule covers, and
// edits made since the command recorded the ones it found, see
// recordFirst, are refused, exit code 6. It never records anything: an
// update judges what it does with the tip before it gets here, and a
// commit recorded after that judgement would be laid over.
func (inv *invocation) cleanSite(ctx context.Context, site forkSite, fork *forkUpdate) (siteJudged, *failure) {
	pre, err := inv.judgeSite(ctx, site, true)
	switch {
	case err != nil:
		return siteJudged{}, failureOf(err)
	case len(pre.exposed) > 0:
		return siteJudged{}, failureOf(nestedRepoRefusal(site.name, pre.exposed))
	case !pre.clean:
		return siteJudged{}, uncommittedRefusal(site.name, "updated")
	}
	if _, lost := splitUnrecordable(pre.forkJudged); len(lost) > 0 {
		return siteJudged{}, fork.unrecordable(site, lost)
	}
	return pre, nil
}

// unrecordable refuses the fork of site, whose skill directory holds lost,
// what git cannot record and no ignore rule covers, which laying a new tip
// out over it would discard, see unrecordableAt.
func (f *forkUpdate) unrecordable(site forkSite, lost []string) *failure {
	command := skillCommand("update", site.name)
	what := "an update"
	if f.pull {
		what = "taking in what the account remote holds"
	}
	return unrecordableAt(site.name, site.skillDir, lost, what, command)
}

// layFork sets what u's fork is updated to: commit, its branch's new tip,
// and the skill directory that commit holds, as the journal lays it out.
func (inv *invocation) layFork(ctx context.Context, u *updating, commit string) *failure {
	var err error
	u.fork.commit = commit
	if u.fork.laid, u.fork.lay, err = inv.forkLayout(ctx, u.fork.site.gitDir, commit, u.fork.site.dir); err != nil {
		return failureOf(accountRepoFailure(err))
	}
	return nil
}

// applyFork applies the update of one fork, in a hold of the lock of its
// own, once everything it was judged from is read again: whether a merge
// is pending, exit code 4, unless the update completes it; the branch,
// which has to hold the tip the update was judged on, and the candidate;
// the worktree, which git has to be able to work in; and the skill
// directory, which has to hold no edits the update did not record, judged
// again when it changed, exit code 6. A refusal drops the fork alone, as a managed
// skill's does, and the run goes on.
//
// A merge that conflicts is left pending, see startMerge, with nothing
// journaled: the fork's worktree and branch stay as they are. One that
// completed a pending merge and conflicts with commits made meanwhile is
// set up again in the same checkout, see remergeIn. Anything else is one
// journal, see layTipJournal: the branch moved from the tip to the new
// commit, the commit laid out in the worktree and every copy placement
// that held the tip refreshed; then the checkout of a pending merge it
// completes removed; and the candidate ref deleted, last, when it still
// names the import the new commit records as the fork's base, or the
// candidate that base passed, see forkUpdate.stale.
func (r *updateRun) applyFork(ctx context.Context, u *updating) error {
	inv := r.inv
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error { return inv.applyForkUpdate(ctx, r.gitDir, u) })
	var f *failure
	switch {
	case errors.Is(err, home.ErrMovedBeforeApply):
		r.drop(u.name, refuse(exitRefused, sanitised(u.name)+" changed while it was being updated, so nothing was changed",
			"run '"+skillCommand("update", u.name)+"' again to update it as it is now"))
	case errors.As(err, &f):
		r.drop(u.name, f)
	case err != nil:
		return mutationFailure(err)
	case u.merged.conflicted:
		r.pending = append(r.pending, u)
	default:
		r.applied = append(r.applied, u)
	}
	return nil
}

// applyForkUpdate is applyFork's work under the lock.
func (inv *invocation) applyForkUpdate(ctx context.Context, gitDir string, u *updating) error {
	site, name := u.fork.site, u.name
	candidate := lineage.CandidateRef(name)
	switch pending := inv.mergePending(name); {
	case u.checkout == "" && pending:
		return forkPendingRefusal(name, "updated")
	case u.checkout != "" && !pending:
		return refuse(exitRefused, "the merge of "+sanitised(name)+" was given up while it was being applied, so nothing was changed",
			"run '"+skillCommand("update", name)+"' to merge it again")
	}
	refs := []string{site.rec.Ref, candidate}
	if u.fork.theirsRef != "" && u.fork.theirsRef != candidate {
		refs = append(refs, u.fork.theirsRef)
	}
	values, err := inv.git.Refs(ctx).RefValues(gitDir, refs)
	if err != nil {
		return accountRepoFailure(err)
	}
	if values[site.rec.Ref] != site.rec.Commit {
		return refuse(exitRefused, sanitised(name)+" changed while it was being updated, so nothing was changed",
			"run '"+skillCommand("update", name)+"' again to update it as it is now")
	}
	switch {
	case u.checkout != "" || u.fork.theirsRef == "" || values[u.fork.theirsRef] == u.fork.theirs:
	case u.fork.theirsRef == candidate:
		return refuse(exitRefused, "the update candidate "+candidate+" moved while "+sanitised(name)+" was being updated, so nothing was changed",
			"run '"+skillCommand("update", name)+"' again to apply the update the last check found")
	default:
		return refuse(exitRefused, "the account remote's "+site.branch+" moved while "+sanitised(name)+" was being updated, so nothing was changed",
			"run '"+skillCommand("update", name)+"' again to update it as it is now")
	}
	if err := worktreeHealth(name, site.root, site.branch); err != nil {
		return err
	}
	now, err := inv.siteNow(ctx, site, u.fork.judged, "updated")
	if err != nil {
		return err
	}
	kept, lost := splitUnrecordable(now.forkJudged)
	if len(lost) > 0 {
		return u.fork.unrecordable(site, lost)
	}
	if u.merged.conflicted {
		start := mergeStart{mine: u.merge.Mine, theirs: u.merge.Theirs, merged: u.merged, message: u.fork.message}
		if u.fork.remerge {
			return inv.remergeIn(ctx, u.checkout, start)
		}
		return inv.startMerge(ctx, gitDir, name, start)
	}
	m, discard, err := inv.layTipJournal(ctx, site, tipLaying{
		commit: u.fork.commit, laid: u.fork.laid, lay: u.fork.lay, now: now, kept: kept, done: &u.done,
		// A copy that holds the tip's skill directory, or the one of the tip
		// the edits this command recorded were recorded on, holds what agentx
		// placed there, and is refreshed; one holding anything else was
		// edited where it is, and is kept.
		placed: func() ([]version, error) {
			tips := []string{site.rec.Commit}
			if laid := u.fork.laidTip; laid != "" && laid != site.rec.Commit {
				tips = append(tips, laid)
			}
			var held []version
			for _, tip := range tips {
				sub, err := inv.git.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", tip+":"+site.dir)
				if err != nil {
					return nil, accountRepoFailure(err)
				}
				held = append(held, treeVersion(strings.TrimSpace(sub)))
			}
			return held, nil
		},
	})
	if err != nil {
		return err
	}
	if u.checkout != "" {
		inv.leaveCheckout(u.checkout)
		checkout, err := home.State(u.checkout)
		if err != nil {
			discard()
			return err
		}
		m.Remove(u.checkout, checkout)
	}
	if now := values[candidate]; now != "" && (now == u.fork.base || now == u.fork.stale) {
		m.Ref(gitDir, candidate, now, "")
	}
	back := reenterReplaced(site.skillDir)
	err = m.Apply(inv.refs(ctx))
	back()
	if err != nil {
		return err
	}
	if u.checkout != "" {
		if err := inv.git.RemoveCheckout(ctx, gitDir, u.checkout); err != nil {
			inv.out.debugf("cannot remove the registration of %s, which the next command prunes: %v", u.checkout, err)
		}
	}
	return nil
}

// forkConflictFailure is how an update answers for a fork whose merge it
// left pending, checkout being the merge's checkout: exit code 4, the
// fork's worktree and branch left as they are, and the ways on from
// there, as for a managed skill, see conflictFailure.
func forkConflictFailure(u *updating, checkout string) *failure {
	with := "its update"
	switch {
	case u.fork.with != "":
		with = u.fork.with
	case u.fork.remerge:
		with = "the commits made while its merge was pending"
	}
	name := u.name
	return refuse(exitPendingMerge, sanitised(name)+" conflicts with "+with+" in "+plural(len(u.conflict.Files), "file")+", so the merge is pending and the fork's worktree and branch were left as they are",
		conflictHintAt(name, filepath.Join(checkout, u.fork.site.dir)))
}

// splitUnrecordable sorts the paths of a fork's skill directory, as j
// judged it, that git cannot record: the .git of a repository nested in
// the directory that an ignore rule covers is no part of the fork and is
// carried over into the directory that replaces it, kept, as every file git
// ignores is; anything else, a repository no rule covers or a named pipe,
// is lost, which replacing the directory would discard with no record of
// it anywhere. Pure.
func splitUnrecordable(j forkJudged) (kept, lost []string) {
	for _, p := range j.unrecordable {
		if hasGitComponent(p) && !slices.Contains(j.exposed, p) {
			kept = append(kept, p)
			continue
		}
		lost = append(lost, p)
	}
	return kept, lost
}
