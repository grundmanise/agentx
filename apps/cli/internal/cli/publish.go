package cli

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A publish pushes a fork's commits to the account remote, and nothing but
// commits: edits nobody committed stay where they are, and the result
// names every fork that holds some. It pushes only the fork's own branch,
// to the branch of the same name, never forced. When the account remote
// holds commits the fork lacks, another machine's, the publish takes them
// in first exactly as a pull does, see judgePull, and pushes the merge; a
// merge that conflicts is left pending, and that fork is not pushed. A
// branch of the same name that is another fork, by its fork id, is never
// pushed over.

// publishEvent is what one publish did with one fork.
type publishEvent struct {
	event
	Name        string `json:"name"`
	Outcome     string `json:"outcome"`
	Commit      string `json:"commit,omitempty"` // what the account remote's branch holds now, when the publish knows it
	Uncommitted bool   `json:"uncommitted"`      // the fork holds edits nobody committed, which were not published
}

// The outcomes of a publish, one per fork.
const (
	publishPushed   = "pushed"     // the account remote's branch now holds the fork's tip
	publishUpToDate = "up to date" // it held it already
	publishRejected = "rejected"   // the account remote refused the push; see the error
	publishRefused  = "refused"    // see the error
	publishConflict = "conflict"   // taking in what the account remote holds conflicts, and the merge is pending
)

func newPublishCommand(inv *invocation) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "publish [<name>] [--all]",
		Short: "Push the commits of a fork, or of every fork, to the account remote",
		Long: "Push the commits of the fork called <name>, or with --all of every fork with\n" +
			"commits the account remote lacks, to the fork's branch on the account remote.\n" +
			"Publishing pushes commits only: commit a fork's edits first with 'agentx skill\n" +
			"commit'; the result names every fork whose edits were left out. When the account\n" +
			"remote holds commits another machine published, they are taken in first, as\n" +
			"'agentx pull' takes them in, and the merge is pushed. A push the account remote\n" +
			"rejects is reported, never forced, and a branch of the same name that is another\n" +
			"fork is never pushed over.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			const hint = "name the fork to publish, or run 'agentx publish --all' to publish every fork"
			switch {
			case all && len(args) > 0:
				return fail(exitUsage, "publish takes a fork name or --all, not both", hint)
			case !all && len(args) == 0:
				return fail(exitUsage, "publish needs a fork name or --all", hint)
			case all:
				return inv.publish(cmd.Context(), "")
			}
			return inv.publish(cmd.Context(), args[0])
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "publish every fork with commits the account remote lacks")
	return cmd
}

// publishing is one fork of a publish: what was found of it and what the
// publish did with it.
type publishing struct {
	name        string
	outcome     string
	commit      string
	uncommitted bool
	push        bool // its branch goes in the push
	f           *failure
	sync        *forkSync // what taking in the account remote's commits did, when they were taken in
}

// publish fetches the account remote once and publishes the fork called
// name, or with no name every fork of this machine. Each fork is judged
// against its branch on the account remote, as the fetch left it: a fork
// the remote holds no branch of, or holds an ancestor of its tip on, is
// pushed; one whose tip the remote holds already is up to date; one whose
// remote branch is another fork is refused, exit code 6; and one the
// remote holds commits of that it lacks takes them in first, see syncFork,
// refused while a merge is pending, exit code 4, or while it holds
// uncommitted edits, exit code 6. Then every fork with something to push
// goes in one push, see gitx.Push, and a ref the remote rejects is
// reported, exit code 6, never forced. One progress event per fork follows,
// then its publish event, which says whether it holds uncommitted edits,
// which publishing leaves where they are, and the result names them.
func (inv *invocation) publish(ctx context.Context, name string) error {
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
	names, err := inv.remoteSelection(ctx, account, name, records, false, "publish")
	if err != nil {
		return err
	}
	if len(names) == 0 {
		inv.summary = "no fork to publish"
		inv.out.print("No fork to publish. Create one with ", inv.out.paint(label, "agentx skill new <name>"), " or ", inv.out.paint(label, "agentx skill fork <name>"), ".")
		return nil
	}
	remote, err := inv.readRemoteForks(ctx, gitDir, account, records, names)
	if err != nil {
		return err
	}
	list := make([]*publishing, 0, len(names))
	var branches []string
	for _, n := range names {
		p := inv.judgePublish(ctx, gitDir, records[n], remote)
		if p.push {
			branches = append(branches, strings.TrimPrefix(records[n].Ref, "refs/heads/"))
		}
		list = append(list, p)
	}
	if len(branches) > 0 {
		if err := inv.pushForks(ctx, gitDir, account, entry.URL, list, branches); err != nil {
			return err
		}
	}
	return inv.reportPublished(ctx, list)
}

// judgePublish decides what the publish does with the fork whose branch is
// rec, and takes in what the account remote holds of it when the remote
// holds commits it lacks, see publish.
func (inv *invocation) judgePublish(ctx context.Context, gitDir string, rec lineage.Record, remote remoteForks) *publishing {
	n := rec.Name
	p := &publishing{name: n, uncommitted: inv.forkUncommitted(ctx, gitDir, rec)}
	theirs := remote.tips[n]
	switch theirs {
	case "":
		p.push = true
		return p
	case rec.Commit:
		p.outcome, p.commit = publishUpToDate, theirs
		return p
	}
	if f := sameForkRefusal(rec, remote.walked[theirs], "published"); f != nil {
		p.outcome, p.f = publishRefused, f
		return p
	}
	_, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", "--is-ancestor", theirs, rec.Commit)
	switch {
	case err != nil:
		p.outcome, p.f = publishRefused, failureOf(accountRepoFailure(err))
		return p
	case status == 0: // the account remote holds nothing the fork lacks
		p.push = true
		return p
	case inv.mergePending(n):
		// The skill directory in the checkout, as the conflict that left
		// the merge pending named it.
		checkout := inv.checkoutPath(n)
		if dir, err := inv.forkDir(ctx, gitDir, rec); err == nil {
			checkout = filepath.Join(checkout, dir)
		}
		p.outcome, p.f = publishRefused, publishPendingRefusal(n, checkout)
		return p
	}
	s := inv.syncFork(ctx, gitDir, rec, remote, "published")
	p.sync = &s
	switch s.outcome {
	case pullFastForward:
		p.outcome, p.commit = publishUpToDate, theirs
	case pullMerged:
		p.push = true
	case pullConflict:
		p.outcome, p.f = publishConflict, s.f
	default:
		p.outcome, p.f = publishRefused, s.f
	}
	return p
}

// publishPendingRefusal refuses to publish the fork called name while it
// has a merge pending and the account remote holds commits it lacks: the
// publish would have to take them in, and it never completes a pending
// merge, resolved or not. A pull completes it once it is resolved in its
// checkout, at checkout.
func publishPendingRefusal(name, checkout string) *failure {
	return refuse(exitPendingMerge, sanitised(name)+" has a merge pending, so it cannot be published until the merge is completed or given up",
		"resolve it with git in "+quotedPath(checkout)+" and run '"+pullCommand(name)+"' to complete it, then publish again, or run '"+
			skillCommand("update", name, "--abort")+"' to give the merge up")
}

// forkUncommitted reports whether the fork whose branch is rec holds edits
// nobody committed, as its drift reads them. A fork whose worktree git
// cannot work in is not read, and holds none as far as a publish says.
func (inv *invocation) forkUncommitted(ctx context.Context, gitDir string, rec lineage.Record) bool {
	site, err := inv.forkSiteOf(ctx, gitDir, rec)
	if err != nil {
		inv.out.debugf("%s: %v", rec.Name, err)
		return false
	}
	j, err := inv.judgeSite(ctx, site, false)
	if err != nil {
		inv.out.debugf("%s: %v", rec.Name, err)
		return false
	}
	return !j.clean
}

// pushForks pushes branches in one push to the account remote, the git
// remote called remote at url, and reads what it answered for each, see
// gitx.Push, then what its branches hold now. A push git could not make at
// all is exit code 3, as a fetch is.
func (inv *invocation) pushForks(ctx context.Context, gitDir, remote, url string, list []*publishing, branches []string) error {
	statuses, err := inv.git.Push(ctx, gitDir, remote, branches)
	if err != nil {
		return unreachableRemote(url, err)
	}
	answered := map[string]int{}
	for i, s := range statuses {
		answered[strings.TrimPrefix(s.To, "refs/heads/")] = i
	}
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return accountRepoFailure(err)
	}
	for _, p := range list {
		if !p.push {
			continue
		}
		branch := strings.TrimPrefix(lineage.ForkRef(p.name), "refs/heads/")
		i, ok := answered[branch]
		switch {
		case !ok:
			p.outcome, p.f = publishRejected, refuse(exitSource, "git push said nothing of "+branch+", so whether the account remote took it is not known",
				"run '"+publishCommand(p.name)+"' again")
		case statuses[i].Rejected():
			p.outcome, p.f = publishRejected, refuse(exitRefused, "the account remote rejected "+branch+": "+sanitised(statuses[i].Why()),
				"run '"+pullCommand(p.name)+"' to take in what it holds, then publish again; agentx never forces a push")
		case statuses[i].Flag == '=':
			p.outcome, p.commit = publishUpToDate, tips[p.name]
		default:
			p.outcome, p.commit = publishPushed, tips[p.name]
		}
	}
	return nil
}

// reportPublished reports every fork of the publish, in name order: one
// progress event each, a merge it made first as a pull reports it, its
// publish event and its line, then one library_skill event for each fork
// the merge moved, a warning for each fork with uncommitted edits but one
// refused for them, whose refusal names them, and the result, which
// answers for every fork that was not published, see refusals.
func (inv *invocation) reportPublished(ctx context.Context, list []*publishing) error {
	out := inv.out
	run := refusals{verb: "published", noun: "fork", mixed: "run 'agentx skill list' to see the state of each fork, then publish the rest one at a time"}
	var moved, dirty []string
	pushed := 0
	for i, p := range list {
		out.emit(progressEvent{event: newEvent("progress"), Phase: "publish", Subject: p.name, Current: i + 1, Total: len(list)})
		if p.sync != nil && p.sync.outcome != pullRefused {
			inv.reportSync(*p.sync)
			if p.sync.moved() {
				moved = append(moved, p.name)
			}
		}
		out.emit(publishEvent{event: newEvent("publish"), Name: p.name, Outcome: p.outcome, Commit: p.commit, Uncommitted: p.uncommitted})
		name := out.paint(heading, sanitised(p.name))
		switch p.outcome {
		case publishPushed:
			pushed++
			out.done("published " + name + " as " + short(p.commit))
		case publishUpToDate:
			out.print(name, " is up to date on the account remote")
		}
		// A fork refused for its uncommitted edits says so in its refusal;
		// any other fork that holds some is named with them.
		if p.uncommitted && (p.f == nil || !errors.Is(p.f, errUncommitted)) {
			dirty = append(dirty, p.name)
		}
		if p.f != nil {
			run.add(p.name, p.f)
			if len(list) > 1 {
				out.warn(namedReason(p.name, p.f.message))
			}
		}
	}
	if err := inv.reportForks(ctx, moved); err != nil {
		return err
	}
	for _, n := range dirty {
		out.warnWith(sanitised(n)+" has uncommitted edits, which were not published",
			"run '"+skillCommand("commit", n)+"' to commit them, then publish again")
	}
	switch {
	case len(list) == 1 && pushed == 1:
		inv.summary = "published " + sanitised(list[0].name)
	case len(list) == 1 && list[0].outcome == publishUpToDate:
		inv.summary = sanitised(list[0].name) + " is up to date on the account remote"
	default:
		inv.summary = "published " + strconv.Itoa(pushed) + " of " + plural(len(list), "fork")
	}
	if len(dirty) > 0 {
		inv.summary += "; uncommitted edits were not published: " + strings.Join(sanitisedAll(dirty), ", ")
	}
	if f := run.failure(len(list), len(list)-len(run.broken)); f != nil {
		return f
	}
	return nil
}

// sanitisedAll is every name of names sanitised.
func sanitisedAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = sanitised(n)
	}
	return out
}
