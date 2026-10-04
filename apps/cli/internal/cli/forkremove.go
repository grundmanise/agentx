package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// Removing a fork takes it off this machine as one journaled mutation:
// every placement agentx made of it, the library symlink, the worktree
// with whatever it holds, unpublished edits and ignored files included,
// the fork's branch, its update candidate and its copy modes. The journal
// retains the worktree until the removal is complete, so a removal that
// stops part way loses nothing a recovery cannot put back. The worktree's
// registration in the account repo goes once the journal is applied, see
// pruneMerges for what an interrupted removal leaves of it. With --remote
// the account remote's branch is deleted as well, after the removal here,
// and nothing on any other machine changes either way: a machine that
// installed the fork keeps it until it is removed there too.

// forkRemoval is the removal of one fork, as it is judged before the lock.
type forkRemoval struct {
	name   string
	gitDir string
	tip    string // what the fork's branch held when it was judged; "" when this machine has no fork of the name
	remote bool   // the account remote's branch goes too
	url    string // the account remote's, with remote
	// account is the account remote's git remote in the account repo,
	// with remote.
	account string
	there   string // what the account remote's branch held at the fetch; "" when it holds none
	// guard is the fork's skill directory as it was judged clean, when the
	// removal must find it still clean under its lock: skill rename
	// removes the fork it has just forked, and an edit made in between
	// would be in neither.
	guard *forkGuard
}

// forkGuard is a fork's site and how its skill directory was judged.
type forkGuard struct {
	site   forkSite
	judged siteJudged
}

// removeFork removes the fork called name, see judgeForkRemoval, and
// reports it. handled is false when name is no fork of this machine and
// remote is not asked for: the removal is then the one of any other skill.
func (inv *invocation) removeFork(ctx context.Context, name string, remote bool) (handled bool, err error) {
	r, err := inv.judgeForkRemoval(ctx, name, remote, "remove it from this machine alone with '"+skillCommand("remove", name)+"'")
	if r == nil && err == nil {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return true, inv.runForkRemoval(ctx, r, "")
}

// judgeForkRemoval runs every refusal of a fork's removal before anything
// changes: a merge pending for the fork, exit code 4, and with remote a
// skill here that is no fork, see notOfTheAccountRemote, and no account
// remote, both exit code 6, an account remote git cannot reach, exit
// code 3, a name that is no fork here or there, and an account remote
// whose branch of the name is another fork, by its fork id, exit code 6,
// which a removal of this fork must not delete. That last refusal's hint
// is alone, what does the command's work on this machine alone. It returns
// nil and no error when name is no fork of this machine and remote is not
// asked for.
func (inv *invocation) judgeForkRemoval(ctx context.Context, name string, remote bool, alone string) (*forkRemoval, error) {
	gitDir, hasRepo, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	values := map[string]string{}
	if hasRepo {
		if values, err = inv.lineageRefs(ctx, gitDir, name); err != nil {
			return nil, err
		}
	}
	if values[lineage.ForkRef(name)] == "" && !remote {
		return nil, nil
	}
	// A journal an earlier command left is finished first, so that what
	// the removal judges is what the machine holds.
	if err := inv.finishJournals(ctx); err != nil {
		return nil, err
	}
	if hasRepo {
		if values, err = inv.lineageRefs(ctx, gitDir, name); err != nil {
			return nil, err
		}
	}
	r := &forkRemoval{name: name, gitDir: gitDir, tip: values[lineage.ForkRef(name)], remote: remote}
	if r.tip != "" && inv.mergePending(name) {
		return nil, forkPendingRefusal(name, "removed")
	}
	if !remote {
		return r, nil
	}
	if r.tip == "" {
		managed := values[lineage.ManagedRef(name)]
		if _, inLibrary := librarySkill(inv.dirs.Library, name); inLibrary || managed != "" {
			return nil, inv.notOfTheAccountRemote(ctx, gitDir, name, managed)
		}
	}
	var entry home.Source
	if gitDir, entry, r.account, err = inv.accountRemote(ctx); err != nil {
		return nil, err
	}
	r.url = entry.URL
	if err := inv.fetchRemote(ctx, gitDir, r.account, r.url); err != nil {
		return nil, err
	}
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, r.account)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	r.there = tips[name]
	switch {
	case r.tip == "" && r.there == "":
		return nil, fail(exitNotFound, "neither this machine nor the account remote holds a fork called "+sanitised(name),
			"run 'agentx skill list --remote' to see the forks of both")
	case r.tip != "" && r.there != "" && r.there != r.tip:
		records, err := inv.forkRecords(ctx, gitDir)
		if err != nil {
			return nil, err
		}
		walked, err := lineage.Walk(ctx, inv.git, gitDir, []string{r.there})
		if err != nil {
			return nil, accountRepoFailure(err)
		}
		if f := sameForkRefusal(records[name], walked[r.there], "removed from the account remote"); f != nil {
			f.hint = alone
			return nil, f
		}
	}
	return r, nil
}

// notOfTheAccountRemote refuses --remote for a skill this machine holds
// that is not one of the account remote's, exit code 6, before the account
// remote is looked up or fetched: a managed skill of a shared source, whose
// branch's tip is managed, which agentx never deletes anything from, named
// by the source that commit records, or an unmanaged skill, managed empty,
// which has no source at all. A tip whose message git cannot read or that
// records no source is still refused, naming no source.
func (inv *invocation) notOfTheAccountRemote(ctx context.Context, gitDir, name, managed string) error {
	alone := "remove it from this machine with '" + skillCommand("remove", name) + "'"
	if managed == "" {
		return fail(exitRefused, sanitised(name)+" has no source, so there is nothing to delete remotely", alone)
	}
	from := "a source other than the account remote"
	if message, err := inv.git.Isolated(ctx, gitDir, "log", "-1", "--format=%B", managed); err == nil {
		if imported, err := lineage.Parse(message); err == nil && imported.Source != "" {
			from = shownURL(imported.Source)
		}
	}
	return fail(exitRefused, sanitised(name)+" comes from "+from+", and agentx deletes skills only from the account remote", alone)
}

// runForkRemoval removes the fork here, when this machine has one, then,
// with remote, deletes the account remote's branch, and reports both.
// done, when it is not empty, is what a step before this one did, which a
// remote deletion that fails says came first; skill rename passes the fork
// it made.
func (inv *invocation) runForkRemoval(ctx context.Context, r *forkRemoval, done string) error {
	var plan *removalPlan
	if r.tip != "" {
		p, err := inv.applyForkRemoval(ctx, r)
		if err != nil {
			return err
		}
		plan = &p
		what := sanitised(r.name) + " was removed from this machine"
		if done != "" {
			what = done + " and " + sanitised(r.name) + " removed from this machine"
		}
		done = what
	}
	var deleted error
	if r.remote && r.there != "" {
		deleted = inv.deleteRemoteFork(ctx, r, done)
	}
	if plan != nil {
		plan.remote = r.remote && r.there != "" && deleted == nil
		if err := inv.reportRemoved(ctx, *plan, inv.detectedTargets()); err != nil {
			return err
		}
	} else if deleted == nil {
		branch := forkBranch(r.name)
		inv.summary = "removed " + branch + " from the account remote; this machine has no fork " + sanitised(r.name)
		inv.out.done("deleted " + inv.out.paint(heading, branch) + " from the account remote; this machine has no fork " + sanitised(r.name))
	}
	return deleted
}

// committedCopy reports, for a copy of the fork r removes that differs
// from the library, whether it holds what a commit of the fork's branch
// holds in its skill directory: a copy a later commit left behind, which is
// out of date but holds nothing the branch lacks. The branch's trees are
// read once, with two git runs, the first time a copy asks.
func (inv *invocation) committedCopy(ctx context.Context, r *forkRemoval) func(path string) bool {
	var trees map[string]bool
	return func(path string) bool {
		if trees == nil {
			trees = inv.branchTrees(ctx, r)
		}
		t, err := treeid.Read(path)
		return err == nil && fastHolds(t, inv.systemFilesIgnored(), version{holds: func(id string) bool { return trees[id] }})
	}
}

// branchTrees is the set of trees the skill directory of r's fork had in
// every commit of its branch. One that cannot be read is empty, and every
// copy that differs from the library is then said to.
func (inv *invocation) branchTrees(ctx context.Context, r *forkRemoval) map[string]bool {
	trees := map[string]bool{}
	dir, err := inv.forkDir(ctx, r.gitDir, lineage.Record{Name: r.name, Ref: lineage.ForkRef(r.name), Commit: r.tip})
	if err != nil {
		inv.out.debugf("%s: %v", r.name, err)
		return trees
	}
	commits, err := inv.git.Isolated(ctx, r.gitDir, "rev-list", r.tip)
	if err != nil {
		inv.out.debugf("%s: %v", r.name, err)
		return trees
	}
	var query strings.Builder
	for _, c := range strings.Fields(commits) {
		query.WriteString(c + ":" + dir + "\n")
	}
	out, err := inv.git.IsolatedInput(ctx, r.gitDir, strings.NewReader(query.String()), "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		inv.out.debugf("%s: %v", r.name, err)
		return trees
	}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == "tree" {
			trees[fields[0]] = true
		}
	}
	return trees
}

// forkBranch is the short name of the fork branch of the skill called
// name, as git push and a person name it: skills/<name>.
func forkBranch(name string) string {
	return strings.TrimPrefix(lineage.ForkRef(sanitised(name)), "refs/heads/")
}

// applyForkRemoval removes the fork r judged as one journaled mutation,
// once everything it was judged on is read again under the lock: the
// branch still holds the commit judged, no merge is pending since, no git
// holds the worktree's index, as a git commit waiting in an editor does,
// and, for skill rename, the skill directory is still as clean as it was. It
// records, in the order they apply: a step that holds the branch to that
// commit, which the journal's first transaction checks before any path
// changes, since git run in the worktree moves the branch without the
// lock; the placement agentx made in every detected configuration, by the
// rule every removal deletes by, see planRemoval; the library symlink,
// dangling or not, or the directory an install of the name made, when its
// import branch is left; the worktree, retained until the journal is complete;
// copy_mode; and last the branch, the import branch of the name if one is
// left, the update candidate and the upstream-removed marker, each with the
// value it holds now. Any other directory or file at the library path is
// not agentx's, as skill place and skill list say of it, and is left where
// it is and named. Once the journal is applied, and before the lock is
// released, git's registration of the worktree is dropped, and so is the
// branch's tracking configuration.
func (inv *invocation) applyForkRemoval(ctx context.Context, r *forkRemoval) (removalPlan, error) {
	targets := inv.detectedTargets()
	plan := removalPlan{name: r.name, whole: true, fork: true, from: targetIDs(targets)}
	libPath, root := inv.libraryPath(r.name), inv.worktreeRoot(r.name)
	// A shell working in the fork is left in the nearest directory above
	// it that is still there, so that the git run after the removal does
	// not inherit a directory that is gone.
	back := reenterReplaced(root)
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		values, err := inv.lineageRefs(ctx, r.gitDir, r.name)
		if err != nil {
			return err
		}
		if values[lineage.ForkRef(r.name)] != r.tip {
			return movedWhileRemoved(r.name)
		}
		if inv.mergePending(r.name) {
			return forkPendingRefusal(r.name, "removed")
		}
		if err := gitRunning(r.name, root); err != nil {
			return err
		}
		if r.guard != nil {
			if _, err := inv.forkGuards(ctx, r.guard.site, r.guard.judged, "removed", true); err != nil {
				return err
			}
		}
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		libState, err := home.State(libPath)
		if err != nil {
			return err
		}
		// The library entry is the fork's symlink, dangling or not, or,
		// when the name has an import branch left beside the fork's, the
		// directory that install made; anything else there is the user's.
		ours := home.IsAbsent(libState) || home.IsLink(libState) || (home.IsDir(libState) && values[lineage.ManagedRef(r.name)] != "")
		m := home.NewMutation(inv.dirs.Home)
		m.Ref(r.gitDir, lineage.ForkRef(r.name), r.tip, r.tip)
		plan.deleted, plan.kept, plan.library, plan.dropped = nil, nil, nil, nil
		judge := copyJudge{against: "the library"}
		lib, inLibrary := librarySkill(inv.dirs.Library, r.name)
		if inLibrary && ours && lib.ContentHash != "" {
			committed := inv.committedCopy(ctx, r)
			judge.differs = func(path string) bool { return contentHashAt(path) != lib.ContentHash && !committed(path) }
		}
		// A library entry that leads nowhere, its worktree gone, gave no
		// client the skill, so no client that reads the library is said to
		// lose it; nor does one that is not the fork's symlink, which
		// stays.
		plan.absent = !inLibrary || !ours
		copies := edit.copiesOf(r.name)
		gone := removalDeletes(targets, r.name, libPath, copies, ours)
		if err := inv.stageRemovals(m, &plan, targets, libPath, judge, copies, gone); err != nil {
			m.Discard()
			return err
		}
		if ours {
			if !home.IsAbsent(libState) {
				m.Remove(libPath, libState)
			}
		} else {
			step := removalStep{path: libPath, mode: modeLibrary, state: libState, why: libraryKept(libPath, libState, r.name, targets)}
			plan.kept = append(plan.kept, step)
			inv.out.warn(step.why)
		}
		state, err := home.State(root)
		if err != nil {
			m.Discard()
			return err
		}
		if plan.worktree = !home.IsAbsent(state); plan.worktree {
			m.Remove(root, state)
		}
		plan.branch = r.tip
		m.Ref(r.gitDir, lineage.ForkRef(r.name), r.tip, "")
		if commit := values[lineage.ManagedRef(r.name)]; commit != "" {
			plan.managed = commit
			m.Ref(r.gitDir, lineage.ManagedRef(r.name), commit, "")
		}
		dropCheckRefs(m, r.gitDir, r.name, values)
		edit.dropSkill(r.name)
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
		if err := m.Apply(inv.refs(ctx)); err != nil {
			return err
		}
		// Still under the lock, so that no worktree another command makes
		// at root meanwhile goes with it. What is left of the registration
		// goes with the next mutation, see pruneMerges; the tracking
		// configuration skill add wrote goes here alone.
		back()
		if err := inv.git.RemoveCheckout(ctx, r.gitDir, root); err != nil {
			inv.out.debugf("%s: %v", r.name, err)
		}
		if err := inv.git.UnsetTracking(ctx, r.gitDir, strings.TrimPrefix(lineage.ForkRef(r.name), "refs/heads/")); err != nil {
			inv.out.debugf("%s: %v", r.name, err)
		}
		return nil
	})
	back()
	if errors.Is(err, home.ErrMovedBeforeApply) {
		return plan, movedWhileRemoved(r.name)
	}
	if err != nil {
		return plan, mutationFailure(err)
	}
	return plan, nil
}

// movedWhileRemoved refuses the removal of the fork called name whose
// branch moved after it was judged, a commit made with git in its worktree
// for one: nothing was removed. It wraps errMovedWhileRemoved.
func movedWhileRemoved(name string) error {
	return refuse(exitRefused, sanitised(name)+" changed while it was being removed, so nothing was removed",
		"run '"+skillCommand("remove", name)+"' again").wrap(errMovedWhileRemoved)
}

// errMovedWhileRemoved is what movedWhileRemoved wraps, so that skill
// rename, whose new fork was made from the commit the branch held before
// it moved, can say what running the removal again would drop.
var errMovedWhileRemoved = errors.New("fork branch moved while it was being removed")

// libraryKept is the line a fork's removal reports a directory or a file
// at its library path with, state being what the path holds: it is not the
// symlink agentx made, and it stays. A directory is a skill to every
// client of targets that reads the library, which still sees it.
func libraryKept(libPath, state, name string, targets []placeTarget) string {
	if !home.IsDir(state) {
		return libPath + " is not " + sanitised(name) + "'s library symlink and was left as it is"
	}
	var readers []string
	for _, t := range targets {
		if t.readsLibrary {
			readers = append(readers, t.id)
		}
	}
	line := libPath + " is a directory agentx did not place there and was left as it is"
	switch len(readers) {
	case 0:
		return line
	case 1:
		return line + "; " + readers[0] + " still sees it"
	}
	return line + "; " + andList(readers) + " still see it"
}

// deleteRemoteFork deletes the account remote's branch of the fork r
// judged, leased on what the fetch read it at, see
// gitx.DeleteRemoteBranch. done is what was already done, which the
// failure says first. A push git could not make, and a deletion the remote
// refused, are exit code 3, as for a fetch.
func (inv *invocation) deleteRemoteFork(ctx context.Context, r *forkRemoval, done string) error {
	s, err := inv.git.DeleteRemoteBranch(ctx, r.gitDir, r.account, strings.TrimPrefix(lineage.ForkRef(r.name), "refs/heads/"), r.there)
	if err != nil {
		return refuse(exitSource, remoteStillHolds(r.name, done)+": "+trimGit(err.Error()),
			"check that you can reach "+shownURL(r.url)+" with git, then run '"+skillCommand("remove", r.name, "--remote")+"' again")
	}
	if s.Rejected() {
		return remoteDeleteRefusal(r.name, done, s)
	}
	return nil
}

// remoteStillHolds says that the account remote still holds the branch of
// the fork called name, after done, when something was.
func remoteStillHolds(name, done string) string {
	if done == "" {
		return "the account remote still holds " + forkBranch(name)
	}
	return done + ", but the account remote still holds " + forkBranch(name)
}

// remoteDeleteRefusal is the answer to a deletion of the fork called
// name's branch that the account remote rejected, s being what git push
// reported of it, after done. A hosting service refuses to delete a
// repository's default branch, and a remote that only ever received fork
// branches has the first of them as its default, so running the command
// again would be refused again: the hint says what to change first. A
// branch another machine moved since the fetch is rejected as stale, and
// running the command again fetches it anew. Pure.
func remoteDeleteRefusal(name, done string, s gitx.PushStatus) *failure {
	reason := s.Why()
	again := "run '" + skillCommand("remove", name, "--remote") + "' again"
	hint := again
	switch {
	case strings.Contains(reason, "current branch"):
		hint = "make another branch the account remote's default branch on its hosting service, then " + again
	case strings.Contains(reason, "stale info"):
		hint = "another machine moved it since the fetch; " + again + " to delete what it holds now"
	}
	return refuse(exitSource, remoteStillHolds(name, done)+": the account remote rejected its deletion: "+sanitised(reason), hint)
}
