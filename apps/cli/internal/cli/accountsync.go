package cli

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// The account step brings a fork up to date with what another machine
// published to the account remote, by a plain Git merge on the ancestry the
// two histories share: nothing when the account remote holds nothing this
// machine lacks, a fast-forward when this machine holds nothing the account
// remote lacks, and otherwise a merge of the two tips, git finding their
// merge base. It is the first step of a fork's update, see
// updateRun.accountStep, and what a publish takes in first. A clean merge
// is committed by the fork commit writer, with the local tip and the remote
// tip as its parents and the base the two sides agree on, see
// lineage.PickBase, and the branch and the worktree move to it in one
// journal, as an update's do. A merge that conflicts becomes a pending
// merge in a checkout under agentx home, as an update's does: the fork's
// worktree and branch stay as they are, and the next update of the fork
// completes it once it is resolved there with git. The account step
// refuses a remote branch of the same name that is another fork, by its
// fork id, and, while the fork has edits nobody committed, anything that
// would move its branch.

// pullEvent is what the account step did to one fork.
type pullEvent struct {
	event
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Commit  string `json:"commit,omitempty"` // the fork's new tip, when the account step moved it
	Base    string `json:"base,omitempty"`   // the base the merge records, when the account step merged
}

// The outcomes of the account step, one per fork. An up to date fork is
// never reported: the step took nothing in.
const (
	pullUpToDate    = "up to date"   // the account remote holds nothing the fork lacks
	pullFastForward = "fast-forward" // the fork's branch moved to the remote tip
	pullMerged      = "merged"       // the two tips were merged
	pullConflict    = "conflict"     // the merge is pending
	pullRefused     = "refused"      // see the error
)

// remoteForks is what the account remote held of the forks a run covers,
// as its fetch left the remote-tracking branches: each fork's remote tip,
// by name, and the lineage of every remote tip the fork's own branch does
// not hold already, read in one walk.
type remoteForks struct {
	remote string // the account remote's git remote, whose remote-tracking branches were read
	tips   map[string]string
	walked map[string]lineage.ForkLineage
}

// readRemoteForks reads the remote-tracking branches of the git remote
// called remote, then walks the remote tips of names that differ from the
// local ones, all in two git processes.
func (inv *invocation) readRemoteForks(ctx context.Context, gitDir, remote string, records map[string]lineage.Record, names []string) (remoteForks, error) {
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return remoteForks{}, accountRepoFailure(err)
	}
	return inv.walkRemoteForks(ctx, gitDir, remote, tips, records, names)
}

// walkRemoteForks is readRemoteForks for a run that read the
// remote-tracking branches already: tips, by name. It walks the remote
// tips of names that differ from the local ones, in one git process.
func (inv *invocation) walkRemoteForks(ctx context.Context, gitDir, remote string, tips map[string]string, records map[string]lineage.Record, names []string) (remoteForks, error) {
	var walk []string
	for _, n := range names {
		if tip := tips[n]; tip != "" && tip != records[n].Commit {
			walk = append(walk, tip)
		}
	}
	walked, err := lineage.Walk(ctx, inv.git, gitDir, walk)
	if err != nil {
		return remoteForks{}, accountRepoFailure(err)
	}
	return remoteForks{remote: remote, tips: tips, walked: walked}, nil
}

// forkSync is what the account step of an update, or of a publish, made of
// one fork: its outcome, see the outcomes of the account step, the update
// it applied or left pending, and why it was refused.
type forkSync struct {
	name    string
	outcome string
	u       *updating
	f       *failure
}

// moved reports whether the sync moved the fork's branch.
func (s forkSync) moved() bool {
	return s.outcome == pullFastForward || s.outcome == pullMerged
}

// syncFork runs the account step for the fork whose branch is rec, see
// judgePull, and applies what it found, in a hold of the lock and a journal
// of its own, see applyForkUpdate. doing is what the command does, as its
// refusals say it: published, or updated for the account step of an update.
func (inv *invocation) syncFork(ctx context.Context, gitDir string, rec lineage.Record, remote remoteForks, doing string) forkSync {
	s := forkSync{name: rec.Name}
	s.outcome, s.u, s.f = inv.judgePull(ctx, gitDir, rec, remote, doing)
	if s.f != nil {
		s.outcome = pullRefused
		return s
	}
	if s.u == nil {
		return s
	}
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error { return inv.applyForkUpdate(ctx, gitDir, s.u) })
	var f *failure
	switch {
	case errors.Is(err, home.ErrMovedBeforeApply):
		again, verb := s.u.fork.again(s.name)
		s.f = refuse(exitRefused, sanitised(s.name)+" changed while it was being "+s.u.fork.what()+", so nothing was changed",
			"run '"+again+"' again to "+verb+" it as it is now")
	case errors.As(err, &f):
		s.f = f
	case err != nil:
		s.f = failureOf(mutationFailure(err))
	case s.u.merged.conflicted:
		s.outcome, s.f = pullConflict, forkConflictFailure(s.u, inv.checkoutPath(s.name))
	}
	if s.f != nil && s.outcome != pullConflict {
		s.outcome = pullRefused
	}
	return s
}

// judgePull decides, before the lock, what the account step of the fork
// whose branch is rec takes in from remote, and merges it. Its callers
// never give it a fork with a merge pending: the update completes that one,
// see judgeForkCompletion, and a publish refuses it. In this order: a fork
// the account remote holds no branch of, or one that holds what the fork
// holds already, is left as it is, up to date; a remote branch whose
// history names another fork id than the fork's, or one of the two that
// names none, is refused, exit code 6, since two forks of one name are
// never tangled; then a fork whose worktree git cannot work in, see
// worktreeHealth, and one with uncommitted edits or a repository nested in
// it that no ignore rule covers, see cleanSite, is refused, exit code 6,
// before anything is written. A fork the account remote has nothing new for
// is up to date whatever its worktree holds.
//
// A fork whose tip the remote tip descends from is fast-forwarded to it.
// Any other is merged with it by merge-tree, with git's own merge base: a
// clean merge is committed here by the fork commit writer, with the local
// tip and the remote tip as its parents and the base lineage.PickBase
// chooses as its Agentx-Base; one that conflicts is left pending under the
// lock, with that base in its MERGE_MSG.
func (inv *invocation) judgePull(ctx context.Context, gitDir string, rec lineage.Record, remote remoteForks, doing string) (string, *updating, *failure) {
	name := rec.Name
	theirs := remote.tips[name]
	fork := &forkUpdate{pull: true, doing: doing, theirsRef: lineage.RemoteForkRef(remote.remote, name), theirs: theirs, with: "the account remote"}
	if theirs == "" || theirs == rec.Commit {
		return pullUpToDate, nil, nil
	}
	there := remote.walked[theirs]
	if f := sameForkRefusal(rec, there, doing); f != nil {
		return "", nil, f
	}
	out, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", rec.Commit, theirs)
	base := strings.TrimSpace(out)
	switch {
	case err != nil:
		return "", nil, failureOf(accountRepoFailure(err))
	case status != 0:
		return "", nil, refuse(exitRefused, sanitised(name)+" and the account remote's "+strings.TrimPrefix(rec.Ref, "refs/heads/")+" share no commit, so they cannot be merged",
			"run '"+skillCommand("history", name)+"' to read the history of yours")
	case base == theirs:
		return pullUpToDate, nil, nil
	}
	site, err := inv.forkSiteOf(ctx, gitDir, rec)
	if err != nil {
		return "", nil, failureOf(err)
	}
	fork.site = site
	u := &updating{name: name, libPath: site.libPath, fork: fork}
	if base == rec.Commit {
		judged, f := inv.cleanSite(ctx, site, fork)
		if f != nil {
			return "", nil, f
		}
		fork.judged, fork.base = judged, there.Base
		fork.stale = inv.staleCandidate(ctx, gitDir, rec, there.Base, there.Import)
		if f := inv.layFork(ctx, u, theirs); f != nil {
			return "", nil, f
		}
		return pullFastForward, u, nil
	}
	// The writer reads the user's core.excludesFile with their identity,
	// before git reads the skill directory with the ignore rules it names.
	w, err := inv.newForkWriter(ctx, gitDir)
	if err != nil {
		return "", nil, failureOf(err)
	}
	judged, f := inv.cleanSite(ctx, site, fork)
	if f != nil {
		return "", nil, f
	}
	fork.judged = judged
	u.merge = lineage.Merge{Mine: rec.Commit, Theirs: theirs}
	if u.merged, err = mergeVersions(ctx, inv.git, gitDir, u.merge); err != nil {
		return "", nil, failureOf(accountRepoFailure(err))
	}
	u.merge.Base = base
	if rec.Fork != nil {
		fork.base = lineage.PickBase(ctx, inv.git, gitDir, *rec.Fork, there)
	}
	subject := accountMergeSubject(name, w.label)
	if u.merged.conflicted {
		files, err := conflictFiles(u.merged.stages, site.dir)
		if err == nil {
			fork.message, err = w.mergeMessage(subject, fork.base)
		}
		if err != nil {
			return "", nil, failureOf(accountRepoFailure(err))
		}
		u.conflict, fork.start = conflictOfSkill(name, lineage.KindFork, u.merge, files), true
		return pullConflict, u, nil
	}
	// The base the merge records is the fork's own or the remote's, which
	// is the one that can pass the candidate.
	fork.stale = inv.staleCandidate(ctx, gitDir, rec, fork.base, there.Import)
	commit, err := w.commit(ctx, u.merged.tree, []string{rec.Commit, theirs}, forkMessage{subject: subject, trailers: lineage.ForkTrailers{Base: fork.base}})
	if err != nil {
		return "", nil, failureOf(accountRepoFailure(err))
	}
	if f := inv.layFork(ctx, u, commit); f != nil {
		return "", nil, f
	}
	return pullMerged, u, nil
}

// sameForkRefusal refuses to take in, or to publish over, a remote branch
// whose history there says is not the fork rec: its nearest fork id is
// another one, or one of the two names none, so nothing can tell that
// they are one fork. Two forks of one name are never tangled; the way on
// is to give this machine's another name, under which it publishes as a
// fork of its own. nil when the ids agree.
func sameForkRefusal(rec lineage.Record, there lineage.ForkLineage, doing string) *failure {
	name := sanitised(rec.Name)
	branch := strings.TrimPrefix(rec.Ref, "refs/heads/")
	rename := "rename yours with '" + skillCommand("rename", rec.Name, "<new>") + "', then publish that one"
	here := ""
	if rec.Fork != nil {
		here = rec.Fork.ID
	}
	switch {
	case here != "" && there.ID != "" && here != there.ID:
		return refuse(exitRefused, "the account remote's "+branch+" is a different fork than "+name+" on this machine, so "+name+" cannot be "+doing, rename)
	case here == "" || there.ID == "":
		return refuse(exitRefused, "the history of "+name+" here or of the account remote's "+branch+" records no fork id, so nothing tells that they are one fork and "+name+" cannot be "+doing, rename)
	}
	return nil
}

// accountMergeSubject is the subject of the commit that merges what the
// account remote holds of a fork into it: the skill and the label of the
// machine it was merged on, as in "pdf: merge the account remote (laptop)".
func accountMergeSubject(name, label string) string {
	return name + ": merge the account remote (" + sanitised(label) + ")"
}

// forkRecords is the lineage of the account repo with every fork's
// history read, see lineage.ReadForks.
func (inv *invocation) forkRecords(ctx context.Context, gitDir string) (map[string]lineage.Record, error) {
	records, err := inv.listLineage(ctx, gitDir)
	if err == nil {
		err = lineage.ReadForks(ctx, inv.git, gitDir, records, inv.forkWalks)
	}
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	return records, nil
}

// remoteSelection is the forks a publish covers, in name order:
// the one name it was given, which has to be a fork of this machine, or
// with none every fork. remote is the account remote's git remote.
func (inv *invocation) remoteSelection(ctx context.Context, remote, name string, records map[string]lineage.Record) ([]string, error) {
	if name == "" {
		var names []string
		for n, rec := range records {
			if rec.Kind == lineage.KindFork {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		return names, nil
	}
	rec, ok := records[name]
	if ok && rec.Kind == lineage.KindFork {
		return []string{name}, nil
	}
	_, held := librarySkill(inv.dirs.Library, name)
	if !ok && !held {
		if inv.remoteHolds(ctx, remote, name) {
			return nil, fail(exitNotFound, sanitised(name)+" is a fork of the account remote that this machine has not installed, so there is nothing of it here to publish",
				"install it with '"+fromAccountCommand(name)+"'")
		}
		return nil, inv.noLibrarySkill(name)
	}
	what := " is not a fork"
	if ok {
		what = " is managed, not a fork"
	}
	refusal := sanitised(name) + what + ", so it is never published: only forks travel through the account remote"
	// A fork the account remote holds of the name takes the place of what
	// is here; a fork of it made here would be another fork of the name,
	// which a publish refuses. A symlink at the library path is never
	// moved into the fork, so it has to go first.
	if inv.remoteHolds(ctx, remote, name) {
		libPath := inv.libraryPath(name)
		if state, err := home.State(libPath); err == nil && home.IsLink(state) {
			return nil, fail(exitRefused, refusal, "remove the link "+quotedPath(libPath)+", then install the account remote's fork in its place with '"+fromAccountCommand(name)+"'")
		}
		var flags []string
		if !ok && isDir(libPath) {
			flags = []string{"--keep-local"}
		}
		return nil, fail(exitRefused, refusal, "install the account remote's fork in its place with '"+fromAccountCommand(name, flags...)+"'")
	}
	return nil, fail(exitRefused, refusal, "fork it first with '"+skillCommand("fork", name)+"'")
}

// forkPlaced reports whether the fork called name is placed on this
// machine: a worktree or a library entry of its is there.
func (inv *invocation) forkPlaced(name string) bool {
	return lexists(inv.worktreeRoot(name)) || lexists(inv.libraryPath(name))
}

// remoteHolds reports whether the account remote, the git remote called
// remote, held a fork called name at the last fetch, by its remote-tracking
// branch; a read that fails says it does not.
func (inv *invocation) remoteHolds(ctx context.Context, remote, name string) bool {
	tips, err := lineage.ListRemote(ctx, inv.git, gitx.AccountRepoPath(inv.dirs.Home), remote)
	return err == nil && tips[name] != ""
}

// reportSync reports what the account step did to one fork it took
// something in for, or refused: a pull event, a line, and for a merge left
// pending the conflict, see printConflicts.
func (inv *invocation) reportSync(s forkSync) {
	out := inv.out
	ev := pullEvent{event: newEvent("pull"), Name: s.name, Outcome: s.outcome}
	if s.u != nil && s.moved() {
		ev.Commit, ev.Base = s.u.fork.commit, s.u.fork.base
		if s.outcome == pullFastForward {
			ev.Base = ""
		}
	}
	out.emit(ev)
	name := out.paint(heading, sanitised(s.name))
	switch s.outcome {
	case pullFastForward:
		out.done("pulled " + name + ": fast-forward to " + short(ev.Commit))
	case pullMerged:
		out.done("pulled " + name + ": merged the account remote, committed as " + short(ev.Commit))
	case pullConflict:
		inv.printConflicts(s.u.conflict, s.u.conflictsWith())
	}
}

// reportForks reports each fork of names as it now stands, one
// library_skill event each, after one scan of the machine; a fork whose
// library entry is elsewhere has nothing in the library to report.
func (inv *invocation) reportForks(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	libs := librarySkills(inv.dirs.Library)
	for _, n := range names {
		if lib, ok := libs[n]; ok {
			inv.out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
		}
	}
	return nil
}
