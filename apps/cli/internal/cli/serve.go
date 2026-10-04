package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

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
	Kind          string   `json:"kind"` // managed or unmanaged, as the snapshot's library entry has it
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

func newServeCommand(inv *invocation) *cobra.Command {
	var once bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Watch for changes and stream a snapshot on each one, until stdin closes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lock, err := home.TakeServeLock(inv.dirs.Home)
			if errors.Is(err, home.ErrServing) {
				return fail(exitRefused, err.Error(), "stop the agentx serve holding "+home.ServeLockPath(inv.dirs.Home)+" first")
			}
			if err != nil {
				return err
			}
			defer home.Unlock(lock)
			inv.instanceID() // fixed here, before two goroutines report it
			inv.verdicts = map[string]keptVerdict{}
			inv.forkWalks = lineage.NewWalkCache()
			inv.forksWarned = true // reconciliation and every snapshot name them
			if !once {
				// Reconciliation reports and repairs before the first scan,
				// so the snapshot shows what it put back. One that cannot
				// run leaves serve to report what the scan finds.
				if err := inv.serveReconcile(cmd.Context()); err != nil {
					if cmd.Context().Err() != nil {
						return nil
					}
					inv.out.warn("reconcile: " + err.Error())
				}
			}
			dirs, trees := inv.watchedDirs()
			// The library of the last snapshot emitted, which the next one's
			// drift is told against. Snapshots are reported from the loop's
			// goroutine alone, so nothing else touches it.
			var library map[string]scan.LibraryEntry
			err = serve.Run(cmd.Context(), serve.Options{
				Scan:  func(ctx context.Context) (scan.Snapshot, error) { return inv.snapshot(ctx, 0, "", false) },
				Index: inv.sourceIndex,
				Watch: dirs,
				Trees: trees,
				Once:  once,
				Stdin: cmd.InOrStdin(),
				Check: inv.serveCheck,
				Jobs:  []serve.Job{inv.maintenanceJob},
				Snapshot: func(snap scan.Snapshot) {
					inv.out.emit(snapshotEvent{event: newEvent("snapshot"), Snapshot: snap})
					inv.out.print(inv.out.paint(heading, fmt.Sprintf("snapshot %d", snap.ScanCounter)), ": ",
						plural(len(snap.Configurations), "configuration"), ", ", plural(len(snap.Skills), "skill"))
					for _, ev := range driftTransitions(library, snap) {
						inv.out.emit(ev)
					}
					library = libraryByName(snap.Library)
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

// serveCheck is the serve child's update check, run once, off its loop,
// as soon as its initial snapshot is out: after that, updates are checked
// for only when the user asks, with skill check-updates. It is the check
// skill check-updates runs, the account remote included, over every source
// of the settings rather than only the sources a managed skill came from,
// so that one pass fetches each source once, whether or not a skill was
// installed from it, and the source index follows what they hold now. It
// waits for the lock rather than giving up on it, since it runs in the
// background and a command holding the lock for a moment is no reason to
// drop what it fetched. It reports nothing as it goes and never ends serve:
// the report it returns is made on the loop's goroutine and carries one
// update_available per update, with this process's instance id, and a
// warning for each thing the check could not check. A machine with no source
// runs no git at all, and neither does one with no account repo; a source
// an import wrote stays a source not fetched, account repo or not, until
// source add adds it. The rescan its write of the version file sets off
// brings the candidates and markers it wrote into the next snapshot, and
// every source ref it moved into the source index.
func (inv *invocation) serveCheck(ctx context.Context) func() {
	rep, err := inv.checkUpdates(ctx, true, "")
	return func() { inv.reportServeCheck(rep, err) }
}

// reportServeCheck reports what the serve child's update check found, every
// warning prefixed "update check: ": the failure that ended the whole check,
// or each source or skill it could not check, each note, and then one
// update_available per update.
func (inv *invocation) reportServeCheck(rep checkReport, err error) {
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
		from, to := ev.versions()
		inv.out.print(inv.out.paint(heading, "update "+sanitised(ev.Name)), ": ",
			short(from), " -> ", short(to), ", ", plural(len(ev.Files), "file"))
	}
}

// watchedDirs are the directories a change signal can come from, most
// important first: agentx home holds the version file every mutation
// rewrites; the account repo, the worktrees, the library and every agent
// client's user-scope skills directory hold content. Everything but agentx
// home and the account repo is a tree: an edit anywhere inside a skill is a
// signal too, wherever the scan reads that skill from. A skills directory a
// client shares with another, and the library itself, are watched once.
//
// The reflogs of the fork branches are a tree too: a commit made with git
// in a fork's worktree writes nothing in the worktree or in agentx home,
// only in the account repo, where it appends to its branch's reflog, so
// that is how serve learns the fork moved. So are the reflogs of the
// remote-tracking branches: a publish with nothing to record, as of a
// commit made with git, moves only the account remote's branch as the
// account repo tracks it, which is what a fork's state is judged against,
// see unpublished. Those reflog trees appear with the first ref of their
// kind, a skill's first publish creating logs/refs/remotes and every
// directory below it at once, so logs and logs/refs are watched too: the
// directory appearing there is the signal, and a sync takes the new tree in.
// The worktrees' own admin directories are not watched: git writes an index
// there on every status, which would be a signal of its own.
func (inv *invocation) watchedDirs() (dirs, trees []string) {
	h := inv.dirs.Home
	logs := filepath.Join(gitx.AccountRepoPath(h), "logs")
	refs := filepath.Join(logs, "refs")
	trees = []string{filepath.Join(h, "worktrees"), inv.dirs.Library, filepath.Join(refs, "heads", "skills"), filepath.Join(refs, "remotes")}
	for _, dir := range scan.UserSkillsDirs(inv.dirs) {
		if !slices.Contains(trees, dir) {
			trees = append(trees, dir)
		}
	}
	return append([]string{h, gitx.AccountRepoPath(h), logs, refs}, trees...), trees
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
		// The account remote holds forks rather than skills on one branch,
		// and is never fetched as a shared source, so it has nothing to
		// search.
		shared := sharedSources(s.Sources)
		urls := make([]string, 0, len(shared))
		for _, src := range shared {
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
