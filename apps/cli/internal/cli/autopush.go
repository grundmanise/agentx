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
// ever fast-forwards a branch the account remote already holds: a fork
// published nowhere yet, or whose branch there was deleted, is left for
// an explicit publish, so that serve never puts back a branch another
// machine removed; and a fork whose branch there holds commits it lacks,
// another machine's, or is another fork, is left for an explicit publish
// too, which takes them in first or refuses, so that serve never starts a
// merge on its own.

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
// the forks it found nothing to push of, their remote branch holding the
// tip already, or could not push, as no fast-forward, as another fork or
// as rejected, by the tip and the remote tip it found them at, so that it
// neither fetches for them again nor pushes them until either moves, and
// the cause of each failure it last warned about. Runs of one tick never
// overlap, and the report of each is made before the next starts, so
// nothing else touches it. Auto-push takes no lock: what it shares with
// the scans and the update check running beside it is the fork lineages
// serve keeps, which lineage.WalkCache guards, and the account repo, whose
// refs git updates atomically.
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
// period, it has no merge pending, and its remote-tracking branch is there
// and lacks commits of that commit: a fork the account remote holds no
// branch of, as far as the last fetch knows, is published by agentx
// publish alone, and one whose branch there holds its tip, as a fetch of
// another machine's publish leaves it, has nothing to push.
// The account remote is fetched once when a fork is due. A due fork whose
// branch there is still there, is the same fork by its fork id and is an
// ancestor of its tip is pushed, every one in one push of the tip judged,
// leased on what the fetch read, see gitx.PushTips: a commit made
// meanwhile waits for a quiet period of its own, and a branch another
// machine moved or deleted since is rejected rather than overwritten or
// put back. One whose branch there now holds its tip is left alone
// quietly, and one whose branch there is another fork, holds commits it
// lacks and lacks some of its own, or that the remote rejected, is left
// alone with a warning naming what to run, each until its tip or its
// remote-tracking branch moves. It
// returns a publish event for every fork the account remote now holds the
// tip of.
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
		there := tracked[name]
		if st.seen[name] != rec.Commit || there == "" || there == rec.Commit || st.behind[name] == rec.Commit+" "+there || inv.mergePending(name) {
			continue
		}
		// A tip its remote-tracking branch already holds, as another
		// machine's publish fetched here leaves it, has nothing to push.
		switch lags, err := inv.isAncestor(ctx, gitDir, rec.Commit, there); {
		case err != nil:
			st.warn(inv, name, sanitised(name)+": "+err.Error())
		case lags:
			st.behind[name] = rec.Commit + " " + there
		default:
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
	ids, walked, err := inv.autoPushLineage(ctx, gitDir, due, seen, tracked)
	if err != nil {
		st.warn(inv, "account repo", err.Error())
		return nil
	}
	var tips []gitx.PushTip
	for _, name := range due {
		tip, there := seen[name], tracked[name]
		if there == "" || there == tip {
			continue // deleted or published meanwhile
		}
		if f := sameForkRefusal(ids[name], walked[there], "published"); f != nil {
			st.behind[name] = tip + " " + there
			st.warn(inv, name, f.message+"; "+f.hint)
			continue
		}
		ff, err := inv.isAncestor(ctx, gitDir, there, tip)
		lags := false
		if err == nil && !ff {
			lags, err = inv.isAncestor(ctx, gitDir, tip, there)
		}
		switch {
		case err != nil:
			st.warn(inv, name, sanitised(name)+": "+err.Error())
			continue
		case lags:
			st.behind[name] = tip + " " + there // published meanwhile, and more on top
			continue
		case !ff:
			st.behind[name] = tip + " " + there
			st.warn(inv, name, sanitised(name)+" was not pushed, since the account remote holds commits it lacks; run '"+publishCommand(name)+"' to take them in and publish it")
			continue
		}
		delete(st.behind, name)
		tips = append(tips, gitx.PushTip{Branch: strings.TrimPrefix(lineage.ForkRef(name), "refs/heads/"), Commit: tip, Expect: there})
	}
	if len(tips) == 0 {
		return nil
	}
	statuses, err := inv.git.PushTips(ctx, gitDir, tips)
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
			st.behind[name] = seen[name] + " " + tracked[name]
			st.warn(inv, name, "the account remote rejected "+forkBranch(name)+": "+sanitised(ps.Why())+"; run '"+publishCommand(name)+"'")
		case ps.Flag == '=':
		default:
			delete(st.warned, name)
			pushed = append(pushed, publishEvent{event: newEvent("publish"), Name: name, Outcome: publishPushed, Commit: seen[name],
				Uncommitted: inv.forkUncommitted(ctx, gitDir, records[name])})
		}
	}
	return pushed
}

// autoPushLineage reads what tells the due forks from another fork of
// their name on the account remote, as a publish does: every fork's record
// with its fork id, and the history of each remote branch, as the fetch
// left it, that is neither gone nor the fork's tip. Nothing is read when
// no due fork needs it.
func (inv *invocation) autoPushLineage(ctx context.Context, gitDir string, due []string, seen, tracked map[string]string) (map[string]lineage.Record, map[string]lineage.ForkLineage, error) {
	var tips []string
	for _, name := range due {
		if there := tracked[name]; there != "" && there != seen[name] {
			tips = append(tips, there)
		}
	}
	if len(tips) == 0 {
		return nil, nil, nil
	}
	records, err := inv.forkRecords(ctx, gitDir)
	if err != nil {
		return nil, nil, err
	}
	walked, err := lineage.Walk(ctx, inv.git, gitDir, tips)
	if err != nil {
		return nil, nil, accountRepoFailure(err)
	}
	return records, walked, nil
}

// isAncestor is whether the commit a is b or one b's history holds, as
// git merge-base --is-ancestor says.
func (inv *invocation) isAncestor(ctx context.Context, gitDir, a, b string) (bool, error) {
	_, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", "--is-ancestor", a, b)
	return err == nil && status == 0, err
}
