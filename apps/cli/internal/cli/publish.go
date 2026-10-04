package cli

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A publish pushes a fork's commits to the account remote, and nothing but
// commits: edits nobody committed stay where they are, and the result names
// every fork that holds some. It pushes only the fork's own branch, to the
// branch of the same name, never forced. A publish never merges: when the
// account remote holds commits the fork lacks, another machine's, the fork
// is not pushed, and an update takes them in, see updateRun.accountStep. A
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
	// publishMoved is a fork the account remote holds commits of that it
	// lacks, published from another machine, while it holds commits of its
	// own the remote lacks: the two histories diverged, and an update has
	// to take them in before it can be published. See the error.
	publishMoved = "moved"
	// publishBehind is a fork whose tip the account remote's branch holds
	// in its history, with commits another machine published on top: it
	// has nothing to publish, and is not refused.
	publishBehind = "behind"
)

func newSkillPublishCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "publish [<name>]",
		Short: "Push the commits of a fork, or of every fork, to the account remote",
		Long: "Push the commits of the fork called <name>, or with no name of every fork with\n" +
			"commits the account remote lacks, to the fork's branch on the account remote.\n" +
			"Publishing pushes commits only: commit a fork's edits first with 'agentx skill\n" +
			"commit'; the result names every fork whose edits were left out. When the account\n" +
			"remote holds commits another machine published, the fork is not pushed, and\n" +
			"publishing fails when the fork holds commits of its own: run 'agentx skill\n" +
			"update <name>' to take them in first, then publish again. A push the account\n" +
			"remote rejects is reported, never forced, and a branch of the same name that is\n" +
			"another fork is never pushed over.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return inv.publish(cmd.Context(), name)
		},
	}
}

// publishCommand is the command line that publishes the fork called name.
func publishCommand(name string) string { return skillCommand("publish", name) }

// publishing is one fork of a publish: what was found of it and what the
// publish did with it.
type publishing struct {
	name        string
	outcome     string
	commit      string
	uncommitted bool
	push        bool // its branch goes in the push
	f           *failure
}

// publish fetches the account remote once and publishes the fork called
// name, or with no name every fork of this machine. Each fork is judged
// against its branch on the account remote, as the fetch left it: a fork
// the remote holds no branch of, or holds an ancestor of its tip on, is
// pushed; one whose tip the remote holds already is up to date; one whose
// remote branch is another fork is refused, exit code 6; one whose tip the
// remote branch descends from is behind, and not pushed; and one the
// remote holds commits of that it lacks while it holds commits of its own
// is refused, exit code 6, since a publish never merges. Then every fork
// with something to push goes in one push, see gitx.Push, and a ref the
// remote rejects is reported, exit code 6, never forced. One progress event
// per fork follows, then its publish event, which says whether it holds
// uncommitted edits, which publishing leaves where they are, and the result
// names them.
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
	names, err := inv.remoteSelection(ctx, account, name, records)
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
	return inv.reportPublished(list)
}

// judgePublish decides what the publish does with the fork whose branch is
// rec, see publish. It writes nothing: what the account remote holds that
// the fork lacks is an update's to take in, see updateRun.accountStep.
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
	// Is the remote tip in the fork's history, or the fork's tip in the
	// remote's? Neither is a divergence.
	ahead, err := inv.isAncestor(ctx, gitDir, theirs, rec.Commit)
	behind := false
	if err == nil && !ahead {
		behind, err = inv.isAncestor(ctx, gitDir, rec.Commit, theirs)
	}
	switch {
	case err != nil:
		p.outcome, p.f = publishRefused, failureOf(accountRepoFailure(err))
	case ahead: // the account remote holds nothing the fork lacks
		p.push = true
	case behind: // the fork holds nothing the account remote lacks
		p.outcome, p.commit = publishBehind, theirs
	default:
		p.outcome, p.f = publishMoved, movedRefusal(n)
	}
	return p
}

// movedRefusal refuses to publish the fork called name while the account
// remote holds commits of it that it lacks, published from another
// machine, and it holds commits of its own the remote lacks: a publish
// never merges, and an update takes them in first.
func movedRefusal(name string) *failure {
	return refuse(exitRefused, "the account remote holds changes to "+sanitised(name)+" that this machine lacks, published from another machine",
		"run '"+skillCommand("update", name)+"' to take them in, then publish again")
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
				"run '"+skillCommand("update", p.name)+"' to take in what it holds, then publish again; agentx never forces a push")
		case statuses[i].Flag == '=':
			p.outcome, p.commit = publishUpToDate, tips[p.name]
		default:
			p.outcome, p.commit = publishPushed, tips[p.name]
		}
	}
	return nil
}

// reportPublished reports every fork of the publish, in name order: one
// progress event each, its publish event and its line, or for a fork
// behind the account remote a warning, then a warning for each fork with
// uncommitted edits, and the result, which answers for every fork that
// was not published, see refusals.
func (inv *invocation) reportPublished(list []*publishing) error {
	out := inv.out
	run := refusals{verb: "published", noun: "fork", mixed: "run 'agentx skill list' to see the state of each fork, then publish the rest one at a time"}
	var dirty []string
	pushed := 0
	for i, p := range list {
		out.emit(progressEvent{event: newEvent("progress"), Phase: "publish", Subject: p.name, Current: i + 1, Total: len(list)})
		out.emit(publishEvent{event: newEvent("publish"), Name: p.name, Outcome: p.outcome, Commit: p.commit, Uncommitted: p.uncommitted})
		name := out.paint(heading, sanitised(p.name))
		switch p.outcome {
		case publishPushed:
			pushed++
			out.done("published " + name + " as " + short(p.commit))
		case publishUpToDate:
			out.print(name, " is up to date on the account remote")
		case publishBehind:
			out.warnWith(behindMessage(p.name), "run '"+skillCommand("update", p.name)+"' to take them in")
		}
		if p.uncommitted {
			dirty = append(dirty, p.name)
		}
		if p.f != nil {
			run.add(p.name, p.f)
			if len(list) > 1 {
				out.warn(namedReason(p.name, p.f.message))
			}
		}
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
	case len(list) == 1 && list[0].outcome == publishBehind:
		inv.summary = behindMessage(list[0].name)
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

// behindMessage is the warning for the fork called name, which the account
// remote is ahead of: it has nothing to publish.
func behindMessage(name string) string {
	return sanitised(name) + " has nothing to publish; the account remote holds changes published from another machine"
}

// sanitisedAll is every name of names sanitised.
func sanitisedAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = sanitised(n)
	}
	return out
}
