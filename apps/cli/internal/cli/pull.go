package cli

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A pull brings a fork up to date with what another machine published to
// the account remote, by a plain Git merge on the ancestry the two
// histories share: nothing when the account remote holds nothing this
// machine lacks, a fast-forward when this machine holds nothing the
// account remote lacks, and otherwise a merge of the two tips, git finding
// their merge base. A clean merge is committed by the fork commit writer,
// with the local tip and the remote tip as its parents and the base the
// two sides agree on, see lineage.PickBase, and the branch and the
// worktree move to it in one journal, as an update's do. A merge that
// conflicts becomes a pending merge in a checkout under agentx home, as an
// update's does: the fork's worktree and branch stay as they are, and the
// next pull or update of the fork completes it once it is resolved there
// with git. A pull refuses a remote branch of the same name that is
// another fork, by its fork id, and, while the fork has edits nobody
// committed, anything that would move its branch.

// pullEvent is what one pull did to one fork.
type pullEvent struct {
	event
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Commit  string `json:"commit,omitempty"` // the fork's new tip, when the pull moved it
	Base    string `json:"base,omitempty"`   // the base the merge records, when the pull merged
}

// The outcomes of a pull, one per fork.
const (
	pullUpToDate    = "up to date"       // the account remote holds nothing the fork lacks
	pullFastForward = "fast-forward"     // the fork's branch moved to the remote tip
	pullMerged      = "merged"           // the two tips were merged, or a merge left pending was completed
	pullConflict    = "conflict"         // the merge is pending
	pullRefused     = "refused"          // see the error
	pullNoBranch    = "no remote branch" // the account remote holds no branch of the fork's name
)

func newPullCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "pull [<name>]",
		Short: "Take in what another machine published of a fork, or of every fork",
		Long: "Fetch the account remote once, then bring the fork called <name>, or every fork\n" +
			"placed on this machine, up to date with its branch there, by a plain Git merge:\n" +
			"a fast-forward when this machine has nothing of its own, a merge committed on the\n" +
			"fork's branch otherwise. A merge that conflicts waits in a Git worktree under\n" +
			"agentx home, as an update's does, and the fork stays as it is until you resolve it\n" +
			"with git and pull again, or give it up with 'agentx skill update <name> --abort'.\n" +
			"Commit a fork's uncommitted edits before it can take anything in. One\n" +
			"fork that cannot be pulled does not stop the others.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return inv.pull(cmd.Context(), name)
		},
	}
}

// pullCommand is the command line that pulls the fork called name.
func pullCommand(name string) string { return remoteCommand("pull", name) }

// remoteCommand is the command line of the top-level command verb for the
// fork called name, as a hint names it.
func remoteCommand(verb, name string) string {
	if strings.HasPrefix(name, "-") {
		return "agentx " + verb + " -- " + shellWord(name)
	}
	return "agentx " + verb + " " + shellWord(name)
}

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

// forkSync is what a pull, a publish or the account step of an update made
// of one fork: its outcome, see the pull outcomes, the update it applied
// or left pending, and why it was refused.
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

// syncFork pulls the fork whose branch is rec, see judgePull, and applies
// what the pull found, in a hold of the lock and a journal of its own, see
// applyForkUpdate. doing is what the command does, as its refusals say it:
// pulled, published, or updated for the account step of an update.
func (inv *invocation) syncFork(ctx context.Context, gitDir string, rec lineage.Record, remote remoteForks, doing string) forkSync {
	s := forkSync{name: rec.Name}
	s.outcome, s.u, s.f = inv.judgePull(ctx, gitDir, rec, remote, doing)
	if s.f != nil {
		s.outcome = pullRefused
		return s
	}
	if s.u == nil || s.u.merged.conflicted && !s.u.fork.start {
		if s.u != nil {
			s.f = forkConflictFailure(s.u, inv.checkoutPath(s.name))
		}
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

// judgePull decides, before the lock, what pulling the fork whose branch is
// rec takes in from remote, and merges it. A fork with a merge pending is
// judged by its checkout, see judgeForkCompletion: the pull completes it, or
// reports it again. Otherwise, in this order: a fork the account remote
// holds no branch of, or one that holds what the fork holds already, is
// left as it is; a remote branch whose history names another fork id than
// the fork's, or one of the two that names none, is refused, exit code 6,
// since two forks of one name are never tangled; then a fork whose
// worktree git cannot work in, see worktreeHealth, and one with
// uncommitted edits or a repository nested in it that no ignore rule
// covers, see cleanSite, is refused, exit code 6, before anything is
// written. A fork the account remote has nothing new for is up to date
// whatever its worktree holds.
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
	if inv.mergePending(name) {
		u, f := inv.judgeForkCompletion(ctx, gitDir, rec, true)
		switch {
		case f != nil:
			return "", nil, f
		case u.merged.conflicted && !u.fork.start:
			return pullConflict, u, nil
		}
		u.fork.doing = doing
		return pullMerged, u, nil
	}
	switch theirs {
	case "":
		return pullNoBranch, nil, nil
	case rec.Commit:
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

// pull fetches the account remote once and pulls the fork called name, or,
// with no name, every fork placed on this machine, one after the other,
// see syncFork. A fork that cannot be pulled, whose merge conflicts or is
// still pending, or that holds uncommitted edits, is reported and the run
// goes on with the others; the run then answers for all of them, see
// refusals. An unfinished journal is finished before anything is read.
func (inv *invocation) pull(ctx context.Context, name string) error {
	gitDir, entry, account, err := inv.accountRemote(ctx)
	if err != nil {
		return err
	}
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	if err := inv.fetchRemote(ctx, gitDir, account, entry.URL); err != nil {
		return err
	}
	records, err := inv.forkRecords(ctx, gitDir)
	if err != nil {
		return err
	}
	names, err := inv.remoteSelection(ctx, account, name, records, true, "pull")
	if err != nil {
		return err
	}
	if len(names) == 0 {
		inv.summary = "no fork to pull: no fork is placed on this machine"
		inv.out.print("No fork to pull: no fork is placed on this machine.")
		return nil
	}
	remote, err := inv.readRemoteForks(ctx, gitDir, account, records, names)
	if err != nil {
		return err
	}
	run := refusals{verb: "pulled", noun: "fork", mixed: "run 'agentx skill list' to see the state of each fork, then pull the rest one at a time"}
	var moved []string
	counts := map[string]int{}
	for _, n := range names {
		s := inv.syncFork(ctx, gitDir, records[n], remote, "pulled")
		inv.reportSync(s)
		counts[s.outcome]++
		if s.moved() {
			moved = append(moved, n)
		}
		if s.f != nil {
			run.add(n, s.f)
			if len(names) > 1 {
				inv.out.warn(namedReason(n, s.f.message))
			}
		}
	}
	if err := inv.reportForks(ctx, moved); err != nil {
		return err
	}
	inv.summary = pullSummary(names, counts)
	if f := run.failure(len(names), len(names)-len(run.broken)); f != nil {
		return f
	}
	return nil
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

// remoteSelection is the forks a pull or a publish covers, in name order:
// the one name it was given, which has to be a fork of this machine, or
// with none every fork, only those placed on this machine, a worktree or a
// library entry of theirs there, when placed is set. verb is the command,
// as its refusals name it, and remote the account remote's git remote.
func (inv *invocation) remoteSelection(ctx context.Context, remote, name string, records map[string]lineage.Record, placed bool, verb string) ([]string, error) {
	if name == "" {
		var names []string
		for n, rec := range records {
			if rec.Kind == lineage.KindFork && (!placed || lexists(inv.worktreeRoot(n)) || lexists(inv.libraryPath(n))) {
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
			return nil, fail(exitNotFound, sanitised(name)+" is a fork of the account remote that this machine has not installed, so there is nothing of it here to "+verb,
				"install it with '"+fromAccountCommand(name)+"'")
		}
		return nil, inv.noLibrarySkill(name)
	}
	what := " is not a fork"
	if ok {
		what = " is managed, not a fork"
	}
	refusal := sanitised(name) + what + ", so it is never " + verb + "ed: only forks travel through the account remote"
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

// remoteHolds reports whether the account remote, the git remote called
// remote, held a fork called name at the last fetch, by its remote-tracking
// branch; a read that fails says it does not.
func (inv *invocation) remoteHolds(ctx context.Context, remote, name string) bool {
	tips, err := lineage.ListRemote(ctx, inv.git, gitx.AccountRepoPath(inv.dirs.Home), remote)
	return err == nil && tips[name] != ""
}

// reportSync reports what a pull did to one fork: a pull event, a line,
// and for a merge left pending the conflict, see printConflicts.
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
	case pullUpToDate:
		out.print(name, " is up to date with the account remote")
	case pullNoBranch:
		out.print(name, " has no branch on the account remote; run ", out.paint(label, publishCommand(s.name)), " to publish it")
	case pullFastForward:
		out.done("pulled " + name + ": fast-forward to " + short(ev.Commit))
	case pullMerged:
		how := "merged the account remote"
		if s.u.checkout != "" {
			how = "applied the merge you resolved"
		}
		out.done("pulled " + name + ": " + how + ", committed as " + short(ev.Commit))
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

// pullSummary is what a pull says it did, by outcome, as in "pulled 3
// forks: 1 fast-forward, 1 merged, 1 up to date".
func pullSummary(names []string, counts map[string]int) string {
	var parts []string
	for _, o := range []string{pullFastForward, pullMerged, pullUpToDate, pullNoBranch, pullConflict, pullRefused} {
		if counts[o] > 0 {
			parts = append(parts, strconv.Itoa(counts[o])+" "+o)
		}
	}
	what := sanitised(names[0])
	if len(names) > 1 {
		what = plural(len(names), "fork")
	}
	return "pulled " + what + ": " + strings.Join(parts, ", ")
}
