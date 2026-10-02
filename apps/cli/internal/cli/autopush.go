package cli

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/serve"
)

// Auto-push is the machine setting auto_push, off by default: when it is
// on, the serve child publishes a fork's commits once its branch has stood
// still for the quiet period, so that a run of commits goes out as one
// push. It pushes commits and nothing else: it never commits, so edits
// nobody committed stay where they are, and it reads no identity. It only
// ever fast-forwards the account remote's branch: a fork whose branch
// there holds commits it lacks, another machine's or another fork's, is
// left for an explicit publish, which takes them in first, so that serve
// never starts a merge on its own.

// pushQuiet is how long a fork's branch has to stand still before serve
// pushes it: a minute, or AGENTX_PUSH_QUIET, a duration such as 30s, for
// tests and for a machine that wants another.
const pushQuiet = time.Minute

// pushQuietPeriod is the quiet period of auto-push: pushQuiet, or the
// duration AGENTX_PUSH_QUIET names, refused as AGENTX_CHECK_INTERVAL is
// when it is not a positive one.
func (inv *invocation) pushQuietPeriod() (time.Duration, error) {
	v := inv.env["AGENTX_PUSH_QUIET"]
	if v == "" {
		return pushQuiet, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fail(exitUsage, "AGENTX_PUSH_QUIET "+v+" is not a positive duration", "set it like 1m or 30s, or unset it")
	}
	return d, nil
}

// autoPushState is what the auto-push tick remembers from one run to the
// next, in the serve process alone: the tip of each fork the last run saw,
// the forks it could not push, as no fast-forward or as rejected, by the
// tip and the remote tip it found them at, so that it neither fetches for
// them again nor pushes them until either moves, and the cause of each
// failure it last warned about. Runs of one tick never overlap, and the report of each is made
// before the next starts, so nothing else touches it.
type autoPushState struct {
	seen    map[string]string // fork name -> tip at the last run
	behind  map[string]string // fork name -> "<tip> <remote tip>" it could not be pushed at
	warned  map[string]string // what the warning is about -> its cause
	pending []string          // warnings of the last run, made by its report
}

// autoPushTick is the serve child's auto-push, on a timer of its own every
// quiet period, the first run at start, which only notes where each fork
// stands. See autoPush for what each run does.
func (inv *invocation) autoPushTick(quiet time.Duration) serve.Tick {
	st := &autoPushState{seen: map[string]string{}, behind: map[string]string{}, warned: map[string]string{}}
	return serve.Tick{Every: quiet, AtStart: true, Run: func(ctx context.Context) func() {
		pushed := inv.autoPush(ctx, st)
		warnings := st.pending
		st.pending = nil
		return func() {
			for _, w := range warnings {
				inv.out.warn("auto-push: " + w)
			}
			for _, ev := range pushed {
				ev.InstanceID = inv.instanceID()
				inv.out.emit(ev)
				inv.out.print(inv.out.paint(heading, "auto-push "+sanitised(ev.Name)), ": ", ev.Outcome, " ", short(ev.Commit))
			}
		}
	}}
}

// warn notes a warning about what for the run's report, unless the last
// one about it had the same cause; the same failure again is a debug line,
// as a source the update check cannot fetch is.
func (st *autoPushState) warn(inv *invocation, what, message string) {
	cause := failureCause(message)
	if st.warned[what] == cause {
		inv.out.debugf("auto-push: %s", message)
		return
	}
	st.warned[what] = cause
	st.pending = append(st.pending, message)
}

// autoPush is one run of auto-push. With auto_push off, or no account
// remote, it does nothing and forgets what it saw, so that turning it on
// waits a whole quiet period. Otherwise a fork is due when its branch
// holds the commit the last run saw, so that it stood still for a quiet
// period, it has no merge pending, and its remote-tracking branch is not
// that commit. The account remote is fetched once when a fork is due, and
// a due fork whose branch there is gone or an ancestor of its tip is
// pushed, every one in one push, see gitx.Push; one whose branch there
// holds commits it lacks, or that the remote rejected, is left alone, with
// a warning naming the publish that takes them in, until its tip or its
// remote-tracking branch moves. It returns a publish event for every fork
// the account remote now holds the tip of.
func (inv *invocation) autoPush(ctx context.Context, st *autoPushState) []publishEvent {
	s, err := inv.loadSettings()
	if err != nil {
		st.warn(inv, "settings", err.Error())
		return nil
	}
	url := ""
	if s.AutoPush {
		if url, err = inv.remoteURL(ctx); err != nil {
			st.warn(inv, "remote", err.Error())
			return nil
		}
	}
	if url == "" {
		clear(st.seen)
		clear(st.behind)
		return nil
	}
	gitDir := gitx.AccountRepoPath(inv.dirs.Home)
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		st.warn(inv, "account repo", err.Error())
		return nil
	}
	tracked, err := lineage.ListRemote(ctx, inv.git, gitDir)
	if err != nil {
		st.warn(inv, "account repo", err.Error())
		return nil
	}
	seen := map[string]string{}
	var due []string
	for name, rec := range records {
		if rec.Kind != lineage.KindFork {
			continue
		}
		seen[name] = rec.Commit
		if st.seen[name] == rec.Commit && tracked[name] != rec.Commit && st.behind[name] != rec.Commit+" "+tracked[name] && !inv.mergePending(name) {
			due = append(due, name)
		}
	}
	st.seen = seen
	if len(due) == 0 {
		return nil
	}
	sort.Strings(due)
	if err := inv.git.FetchRemote(ctx, gitDir); err != nil {
		st.warn(inv, "fetch", unreachableRemote(url, err).message)
		return nil
	}
	delete(st.warned, "fetch")
	if tracked, err = lineage.ListRemote(ctx, inv.git, gitDir); err != nil {
		st.warn(inv, "account repo", err.Error())
		return nil
	}
	var branches []string
	for _, name := range due {
		tip, there := seen[name], tracked[name]
		switch {
		case there == tip:
			continue // published meanwhile
		case there != "":
			_, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", "--is-ancestor", there, tip)
			if err != nil {
				st.warn(inv, name, sanitised(name)+": "+err.Error())
				continue
			}
			if status != 0 {
				st.behind[name] = tip + " " + there
				st.warn(inv, name, sanitised(name)+" was not pushed, since the account remote holds commits it lacks; run '"+publishCommand(name)+"' to take them in and publish it")
				continue
			}
		}
		delete(st.behind, name)
		branches = append(branches, strings.TrimPrefix(lineage.ForkRef(name), "refs/heads/"))
	}
	if len(branches) == 0 {
		return nil
	}
	statuses, err := inv.git.Push(ctx, gitDir, branches)
	if err != nil {
		st.warn(inv, "push", unreachableRemote(url, err).message)
		return nil
	}
	delete(st.warned, "push")
	var pushed []publishEvent
	for _, ps := range statuses {
		name := strings.TrimPrefix(ps.To, "refs/heads/skills/")
		switch {
		case ps.Rejected():
			reason := ps.Summary
			if ps.Reason != "" {
				reason = ps.Reason
			}
			st.behind[name] = seen[name] + " " + tracked[name]
			st.warn(inv, name, "the account remote rejected "+forkBranch(name)+": "+sanitised(reason)+"; run '"+publishCommand(name)+"'")
		case ps.Flag == '=':
		default:
			delete(st.warned, name)
			pushed = append(pushed, publishEvent{event: newEvent("publish"), Name: name, Outcome: publishPushed, Commit: seen[name],
				Uncommitted: inv.forkUncommitted(ctx, gitDir, records[name])})
		}
	}
	return pushed
}
