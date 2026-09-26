package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/serve"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

type refreshCompleteEvent struct {
	event
	RequestID   string `json:"request_id"`
	InstanceID  string `json:"instance_id"`
	OK          bool   `json:"ok"`
	ScanCounter int    `json:"scan_counter,omitempty"` // absent when the scan failed: no freshness is claimed
	Error       string `json:"error,omitempty"`
}

// searchEvent answers one search request: every skill of every source
// whose name or description matches, each naming its source by the
// canonical URL, which is what source list prints and what an install
// takes.
type searchEvent struct {
	event
	RequestID  string         `json:"request_id"`
	InstanceID string         `json:"instance_id"`
	Query      string         `json:"query"`
	Results    []source.Match `json:"results"` // empty, never absent, when nothing matched
}

// driftEvent says that one skill of the library is no longer in the state
// the last snapshot showed it in: its state, its drift, or both changed
// between two snapshots of one serve process. It is a notification and
// nothing more: the snapshot before it carries the skill as it now is, and
// is what a consumer applies. It names the snapshot it follows by instance
// and counter, so that a consumer that ignores a snapshot ignores its drift
// events with it.
type driftEvent struct {
	event
	InstanceID    string   `json:"instance_id"`
	ScanCounter   int      `json:"scan_counter"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	State         string   `json:"state,omitempty"`
	Drift         []string `json:"drift"` // [] when none
	PreviousState string   `json:"previous_state,omitempty"`
	PreviousDrift []string `json:"previous_drift"` // [] when none
}

// driftTransitions are the drift events of one snapshot against the library
// of the snapshot before it, one per skill in both whose state or drift
// changed, in the library's order, which is by name. Nothing is a
// transition against no snapshot at all: the first snapshot of a serve is
// where every state starts, not a change of one. A skill that arrives or
// leaves the library is not one either: the snapshot says so itself.
func driftTransitions(prev map[string]scan.LibraryEntry, snap scan.Snapshot) []driftEvent {
	if prev == nil {
		return nil
	}
	var events []driftEvent
	for _, e := range snap.Library {
		p, ok := prev[e.Name]
		if !ok || p.State == e.State && slices.Equal(p.Drift, e.Drift) {
			continue
		}
		events = append(events, driftEvent{
			event: newEvent("drift"), InstanceID: snap.InstanceID, ScanCounter: snap.ScanCounter,
			Name: e.Name, Kind: e.Kind, State: e.State, Drift: nonNil(e.Drift),
			PreviousState: p.State, PreviousDrift: nonNil(p.Drift),
		})
	}
	return events
}

// libraryByName indexes a snapshot's library for the next comparison.
func libraryByName(entries []scan.LibraryEntry) map[string]scan.LibraryEntry {
	byName := make(map[string]scan.LibraryEntry, len(entries))
	for _, e := range entries {
		byName[e.Name] = e
	}
	return byName
}

func nonNil(words []string) []string {
	if words == nil {
		return []string{}
	}
	return words
}

// checkInterval is how often the serve child runs the update check, the
// first time once its initial snapshot is out: thirty minutes, or
// AGENTX_CHECK_INTERVAL, a duration such as 10m, for tests and diagnosis.
const checkInterval = 30 * time.Minute

// checkEvery is the interval of the update check: checkInterval, or the
// duration AGENTX_CHECK_INTERVAL names, which is read as
// AGENTX_HANDSHAKE_TIMEOUT is and refused the same way when it is none.
func (inv *invocation) checkEvery() (time.Duration, error) {
	v := inv.env["AGENTX_CHECK_INTERVAL"]
	if v == "" {
		return checkInterval, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fail(exitUsage, "AGENTX_CHECK_INTERVAL "+v+" is not a duration", "set it like 30m or 90s, or unset it")
	}
	return d, nil
}

func newServeCommand(inv *invocation) *cobra.Command {
	var once bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Watch for changes and stream a snapshot on each one, until stdin closes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var every time.Duration // a single pass runs no check, and reads no interval
			if !once {
				var err error
				if every, err = inv.checkEvery(); err != nil {
					return err
				}
			}
			lock, err := home.TakeServeLock(inv.dirs.Home)
			if errors.Is(err, home.ErrServing) {
				return fail(exitRefused, err.Error(), "stop the agentx serve holding "+home.ServeLockPath(inv.dirs.Home)+" first")
			}
			if err != nil {
				return err
			}
			defer lock.Close()
			inv.instanceID() // fixed here, before two goroutines report it
			dirs, trees := inv.watchedDirs()
			// The library of the last snapshot emitted, which the next one's
			// drift is told against. Snapshots are reported from the loop's
			// goroutine alone, so nothing else touches it.
			var library map[string]scan.LibraryEntry
			// Whether an update check has anything to look at, which the
			// library of the last snapshot says without a git process: a
			// managed skill whose source the settings hold. A check runs on
			// a goroutine of its own, hence the atomic. Without --json the
			// snapshot lists no library, and a check runs whenever the
			// settings hold a source, which it reads for itself.
			var checkable atomic.Bool
			checkable.Store(!inv.out.json)
			err = serve.Run(cmd.Context(), serve.Options{
				Scan:  func(ctx context.Context) (scan.Snapshot, error) { return inv.snapshot(ctx, 0, "", false) },
				Index: inv.sourceIndex,
				Watch: dirs,
				Trees: trees,
				Once:  once,
				Stdin: cmd.InOrStdin(),
				Check: func(ctx context.Context) func() {
					if !checkable.Load() {
						return nil
					}
					return inv.serveCheck(ctx)
				},
				CheckEvery: every,
				Snapshot: func(snap scan.Snapshot) {
					inv.out.emit(snapshotEvent{event: newEvent("snapshot"), Snapshot: snap})
					inv.out.print(inv.out.paint(heading, fmt.Sprintf("snapshot %d", snap.ScanCounter)), ": ",
						plural(len(snap.Configurations), "configuration"), ", ", plural(len(snap.Skills), "skill"))
					for _, ev := range driftTransitions(library, snap) {
						inv.out.emit(ev)
					}
					library = libraryByName(snap.Library)
					if inv.out.json {
						checkable.Store(hasCheckable(snap.Library))
					}
				},
				RefreshComplete: func(id string, counter int, err error) {
					ev := refreshCompleteEvent{event: newEvent("refresh_complete"), RequestID: id, InstanceID: inv.instanceID(), OK: err == nil, ScanCounter: counter}
					if err != nil {
						ev.Error = err.Error()
						inv.out.print(inv.out.paint(heading, "refresh "+id), ": ", inv.out.paint(failStyle, "failed"), ": ", err.Error())
					} else {
						inv.out.print(inv.out.paint(heading, "refresh "+id), ": ", inv.out.paint(okStyle, "ok"), fmt.Sprintf(", snapshot %d", counter))
					}
					inv.out.emit(ev)
				},
				Search: func(id, query string, results []source.Match) {
					inv.out.emit(searchEvent{event: newEvent("search"), RequestID: id, InstanceID: inv.instanceID(), Query: query, Results: results})
					inv.out.print(inv.out.paint(heading, "search "+id), ": ", plural(len(results), "result"))
				},
				BadRequest: func(message, hint string) {
					inv.out.fail(&failure{status: exitUsage, message: message, hint: hint})
				},
				Warn: inv.out.warn,
			})
			if errors.Is(err, serve.ErrWatch) {
				return fail(exitRefused, err.Error(), "raise the system's watch limit: fs.inotify.max_user_watches on Linux, open files (ulimit -n) on macOS")
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "scan once, emit the snapshot and exit")
	return cmd
}

// serveCheck is the update check of the serve child, run off its loop on
// the timer: the check skill check runs, waiting for the lock rather than
// giving up on it, since it runs in the background and a command holding
// the lock for a moment is no reason to drop what it fetched. It reports
// nothing as it goes and never ends serve: the report it returns is made on
// the loop's goroutine and carries one update_available per update, with
// this process's instance id, and one warning per source or skill it could
// not check, which serve logs on every check. The rescan its write of the
// version file sets off brings the candidates and markers it wrote into the
// next snapshot.
func (inv *invocation) serveCheck(ctx context.Context) func() {
	rep, err := inv.checkUpdates(ctx, true, false)
	return func() {
		if err != nil {
			inv.out.warn("update check: " + err.Error())
			return
		}
		for _, cf := range rep.failures {
			inv.out.warn("update check: " + cf.warning())
		}
		for _, note := range rep.notes {
			inv.out.warn("update check: " + note)
		}
		for _, ev := range rep.updates {
			ev.InstanceID = inv.instanceID()
			inv.out.emit(ev)
			inv.out.print(inv.out.paint(heading, "update "+sanitised(ev.Name)), ": ",
				short(ev.UpstreamCommit), " -> ", short(ev.CandidateUpstreamCommit), ", ", plural(len(ev.Files), "file"))
		}
	}
}

// hasCheckable reports whether a snapshot's library holds a skill an update
// check looks at: a managed skill whose source the settings still hold.
func hasCheckable(entries []scan.LibraryEntry) bool {
	for _, e := range entries {
		if e.Kind == lineage.KindManaged && e.Source != "" && !slices.Contains(e.Drift, driftSourceRemoved) {
			return true
		}
	}
	return false
}

// watchedDirs are the directories a change signal can come from, most
// important first: agentx home holds the version file every mutation
// rewrites; the account repo, the worktrees, the library and every agent
// client's user-scope skills directory hold content. Everything but agentx
// home and the account repo is a tree: an edit anywhere inside a skill is a
// signal too, wherever the scan reads that skill from. A skills directory a
// client shares with another, and the library itself, are watched once.
func (inv *invocation) watchedDirs() (dirs, trees []string) {
	h := inv.dirs.Home
	trees = []string{filepath.Join(h, "worktrees"), inv.dirs.Library}
	for _, dir := range scan.UserSkillsDirs(inv.dirs) {
		if !slices.Contains(trees, dir) {
			trees = append(trees, dir)
		}
	}
	return append([]string{h, gitx.AccountRepoPath(h)}, trees...), trees
}

// sourceIndex builds the index serve answers searches from: the sources of
// the machine settings, each listed from its ref in the account repo. The
// settings are read under the shared lock, as a scan reads them, so a
// source being added or removed is never seen half written; the listing
// itself reads objects, which no mutation rewrites. A search reads the
// index this returns and takes no lock at all.
func (inv *invocation) sourceIndex(ctx context.Context, prev *source.Index) (idx *source.Index, warnings []string, err error) {
	err = home.ReadLocked(ctx, inv.dirs.Home, func() error {
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		urls := make([]string, 0, len(s.Sources))
		for _, src := range s.Sources {
			urls = append(urls, src.URL)
		}
		idx, warnings, err = source.BuildIndex(ctx, inv.git, gitx.AccountRepoPath(inv.dirs.Home), urls, prev)
		return err
	})
	return idx, warnings, err
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
