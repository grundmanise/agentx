package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
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

// updateCheckEvent ends the report of one update check of the serve child,
// after its update_available events: which check it was, when it ended,
// whether it reached what it set out to check, and whether it fetched.
type updateCheckEvent struct {
	event
	InstanceID string `json:"instance_id"`
	Scope      string `json:"scope"`      // checkScopeAll or checkScopeAccount
	CheckedAt  string `json:"checked_at"` // when the check ended, RFC 3339 in UTC
	OK         bool   `json:"ok"`         // false when the check failed as a whole or could not reach the account remote
	Fetched    bool   `json:"fetched"`    // whether the check fetched anything
}

// The scopes of an update_check event: the full check serve runs at start,
// and the check of the account remote alone it repeats while it runs.
const (
	checkScopeAll     = "all"
	checkScopeAccount = "account"
)

// accountCheckInterval is how long the serve child waits, once the report
// of one check is made, before it checks the account remote again, see
// serveChecks.account. accountCheckEvery is what serve waits, which a test
// shortens.
const accountCheckInterval = 10 * time.Minute

var accountCheckEvery = accountCheckInterval

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
// the last inventory showed it in: its state, its drift, or both changed
// between two inventories of one serve process. It is a notification and
// nothing more: the inventory before it carries the skill as it now is, and
// is what a consumer applies. It names the inventory it follows by instance
// and counter, so that a consumer that ignores an inventory ignores its drift
// events with it.
type driftEvent struct {
	event
	InstanceID    string   `json:"instance_id"`
	ScanCounter   int      `json:"scan_counter"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"` // managed or unmanaged, as the inventory's library entry has it
	State         string   `json:"state,omitempty"`
	Drift         []string `json:"drift"` // [] when none
	PreviousState string   `json:"previous_state,omitempty"`
	PreviousDrift []string `json:"previous_drift"` // [] when none
}

// driftTransitions are the drift events of one inventory against the library
// of the inventory before it, one per skill in both whose state or drift
// changed, in the library's order, which is by name. Nothing is a
// transition against no inventory at all: the first inventory of a serve is
// where every state starts, not a change of one. A skill that arrives or
// leaves the library is not one either: the inventory says so itself.
func driftTransitions(prev map[string]scan.LibraryEntry, inventory scan.Inventory) []driftEvent {
	if prev == nil {
		return nil
	}
	var events []driftEvent
	for _, e := range inventory.Library {
		p, ok := prev[e.Name]
		if !ok || p.State == e.State && slices.Equal(p.Drift, e.Drift) {
			continue
		}
		events = append(events, driftEvent{
			event: newEvent("drift"), InstanceID: inventory.InstanceID, ScanCounter: inventory.ScanCounter,
			Name: e.Name, Kind: e.Kind, State: e.State, Drift: nonNil(e.Drift),
			PreviousState: p.State, PreviousDrift: nonNil(p.Drift),
		})
	}
	return events
}

// libraryByName indexes an inventory's library for the next comparison.
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
	var (
		once    bool
		listen  string
		origins []string
	)
	cmd := &cobra.Command{
		Use:         "serve",
		Short:       "Stream an inventory on every change, for the desktop app",
		Annotations: map[string]string{annotationSection: "diagnostics"},
		Args:        cobra.NoArgs,
		Example: examples(
			"agentx serve --json", "the desktop app's mode: inventories until stdin closes",
			"agentx serve --once", "scan once, emit the inventory and exit",
			"agentx serve --json --listen localhost --origin http://localhost:1420", "answer the frontend in a browser on this machine too",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			var address string
			switch {
			case listen != "" && once:
				return fail(exitUsage, "--listen and --once cannot both be given", "--once scans once and exits, so it has no door to open")
			case listen == "" && len(origins) > 0:
				return fail(exitUsage, "--origin needs --listen", "--origin names who may connect to the door --listen opens")
			case listen != "":
				var err error
				if address, err = listenAddress(listen); err != nil {
					return fail(exitUsage, err.Error(), listenHint)
				}
				for _, origin := range origins {
					if err := webOrigin(origin); err != nil {
						return fail(exitUsage, err.Error(), originHint)
					}
				}
			}
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
			inv.forksWarned = true // reconciliation and every inventory name them
			// The door opens before reconciliation and the first scan, so
			// that whoever started serve learns at once where it is; a
			// client that connects meanwhile is answered once serve scans.
			var requests *serve.Requests
			if address != "" {
				door, err := inv.openDoor(address, origins)
				if err != nil {
					return err
				}
				defer door.close()
				requests = door.requests
			}
			if !once {
				// Reconciliation reports and repairs before the first scan,
				// so the inventory shows what it put back. One that cannot
				// run leaves serve to report what the scan finds.
				if err := inv.serveReconcile(cmd.Context()); err != nil {
					if cmd.Context().Err() != nil {
						return nil
					}
					inv.out.warn("reconcile: " + err.Error())
				}
			}
			dirs, trees := inv.watchedDirs()
			// The library of the last inventory emitted, which the next one's
			// drift is told against. Inventories are reported from the loop's
			// goroutine alone, so nothing else touches it.
			var library map[string]scan.LibraryEntry
			checks := &serveChecks{inv: inv}
			err = serve.Run(cmd.Context(), serve.Options{
				Scan:         func(ctx context.Context) (scan.Inventory, error) { return inv.inventory(ctx, 0, "", false) },
				Index:        inv.sourceIndex,
				Watch:        dirs,
				Trees:        trees,
				Once:         once,
				Stdin:        cmd.InOrStdin(),
				Requests:     requests,
				Check:        checks.startup,
				Recheck:      checks.account,
				RecheckEvery: accountCheckEvery,
				Jobs:         []serve.Job{inv.maintenanceJob},
				Inventory: func(inventory scan.Inventory) {
					inv.out.emit(inventoryEvent{event: newEvent("inventory"), Inventory: inventory})
					inv.out.print(inv.out.paint(heading, fmt.Sprintf("inventory %d", inventory.ScanCounter)), ": ",
						plural(len(inventory.Configurations), "configuration"), ", ", plural(len(inventory.Skills), "skill"))
					for _, ev := range driftTransitions(library, inventory) {
						inv.out.emit(ev)
					}
					library = libraryByName(inventory.Library)
				},
				RefreshComplete: func(id string, counter int, err error) {
					ev := refreshCompleteEvent{event: newEvent("refresh_complete"), RequestID: id, InstanceID: inv.instanceID(), OK: err == nil, ScanCounter: counter}
					if err != nil {
						ev.Error = err.Error()
						inv.out.print(inv.out.paint(heading, "refresh "+id), ": ", inv.out.paint(failStyle, "failed"), ": ", err.Error())
					} else {
						inv.out.print(inv.out.paint(heading, "refresh "+id), ": ", inv.out.paint(okStyle, "ok"), fmt.Sprintf(", inventory %d", counter))
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
	cmd.Flags().BoolVar(&once, "once", false, "scan once, emit the inventory and exit")
	cmd.Flags().StringVar(&listen, "listen", "", "also answer JSON-RPC on a WebSocket at this address of this machine, such as localhost or 127.0.0.1:4319")
	cmd.Flags().StringArrayVar(&origins, "origin", nil, "a web origin that may connect to --listen, such as http://localhost:1420 (repeatable)")
	return cmd
}

// serveChecks are the update checks of the serve child: the full check it
// runs once, as soon as its initial inventory is out, see startup, and the
// check of the account remote alone it repeats while it runs, see account.
// serve runs one at a time and makes each report on its loop's goroutine,
// so failing needs no lock.
type serveChecks struct {
	inv *invocation
	// failing is set while the checks fail to reach the account remote, or
	// fail as a whole: the check that starts failing warns, and those after
	// it stay quiet until one succeeds again.
	failing bool
}

// startup is the serve child's update check at start: the check skill
// check-updates runs, the account remote included, over every source of
// the settings rather than only the sources a managed skill came from, so
// that one pass fetches each source once, whether or not a skill was
// installed from it, and the source index follows what they hold now. It
// waits for the lock rather than giving up on it, since it runs in the
// background and a command holding the lock for a moment is no reason to
// drop what it fetched. It reports nothing as it goes and never ends serve:
// the report it returns is made on the loop's goroutine and carries one
// update_available per update, with this process's instance id, a warning
// for each thing the check could not check, and its update_check. A
// machine with no source runs no git at all, and neither does one with no
// account repo; a source an import wrote stays a source not fetched,
// account repo or not, until source add adds it. The rescan its write of
// the version file sets off brings the candidates and markers it wrote
// into the next inventory, and every source ref it moved into the source
// index.
func (c *serveChecks) startup(ctx context.Context) func() {
	rep, err := c.inv.checkUpdates(ctx, true, "")
	at := time.Now()
	return func() {
		if err == nil && rep.idle {
			return // nothing to check, and nothing was
		}
		c.inv.reportServeCheck(rep, err)
		ok := err == nil && !rep.accountFailed
		c.failing = !ok
		c.inv.reportUpdateCheck(checkScopeAll, at, ok, err == nil && len(rep.refreshed) > 0)
	}
}

// account is the check the serve child repeats while it runs, see
// accountCheckEvery: what another machine published to your own skills
// since the last fetch of the account remote. With no account remote set,
// or no account repo, it runs no git and reports nothing. Otherwise one
// git ls-remote asks the account remote for its fork branches, see
// gitx.Runner.ForkTips, and when they are what the remote-tracking
// branches the last fetch left hold, read in one for-each-ref, it fetches
// nothing, takes no lock and writes nothing. When they differ, it runs the
// update check of the account remote alone, see checkUpdates: one fetch of
// it, outside the lock, the comparison of your own skills placed here with
// what it holds, and one write of its last_fetched, under the lock, waited
// for as the check at start waits. Its report is that check's: one
// update_available per update, then its update_check. A failure is a
// warning prefixed "update check: " when the checks start failing, and
// none while they go on failing, see serveChecks.failing.
func (c *serveChecks) account(ctx context.Context) func() {
	inv := c.inv
	entry, remote, set, err := inv.accountSource()
	if err == nil && !set {
		return nil
	}
	if err == nil {
		if _, err := os.Stat(gitx.AccountRepoPath(inv.dirs.Home)); err != nil {
			return nil // no account repo yet: nothing was ever fetched into one
		}
	}
	var rep checkReport
	if err == nil {
		rep, err = inv.checkAccountRemote(ctx, entry.URL, remote)
	}
	at := time.Now()
	return func() {
		if err == nil && rep.idle {
			return
		}
		ok := err == nil && !rep.accountFailed
		if ok || !c.failing {
			inv.reportServeCheck(rep, err)
		}
		c.failing = !ok
		inv.reportUpdateCheck(checkScopeAccount, at, ok, err == nil && rep.accountFetched)
	}
}

// checkAccountRemote is the account check of the serve child, see
// serveChecks.account, of the account remote at url, the git remote
// called remote in the account repo: an empty report, neither idle nor
// fetched, when the account remote holds the fork branches its
// remote-tracking branches hold, and otherwise the report of the update
// check of the account remote alone. An ls-remote that fails is reported
// as a fetch of the account remote that failed.
func (inv *invocation) checkAccountRemote(ctx context.Context, url, remote string) (checkReport, error) {
	theirs, err := inv.git.ForkTips(ctx, url)
	if err != nil {
		f := uncheckedAccount(url, fmt.Errorf("%w: %v", source.ErrUnreachable, err))
		return checkReport{accountFailed: true, failures: []checkFailure{{source: url, fetch: true, f: f}}}, nil
	}
	ours, err := lineage.ListRemote(ctx, inv.git, gitx.AccountRepoPath(inv.dirs.Home), remote)
	if err != nil {
		return checkReport{}, accountRepoFailure(err)
	}
	if maps.Equal(theirs, ours) {
		return checkReport{}, nil
	}
	return inv.checkUpdates(ctx, true, url)
}

// reportServeCheck reports what an update check of the serve child found,
// every warning prefixed "update check: ": the failure that ended the whole
// check, or each source or skill it could not check, each note, and then
// one update_available per update.
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

// reportUpdateCheck emits the update_check event that ends the report of
// a check of scope that ended at at, and its line without --json.
func (inv *invocation) reportUpdateCheck(scope string, at time.Time, ok, fetched bool) {
	inv.out.emit(updateCheckEvent{
		event: newEvent("update_check"), InstanceID: inv.instanceID(), Scope: scope,
		CheckedAt: at.UTC().Format(time.RFC3339), OK: ok, Fetched: fetched,
	})
	status, style := "ok", okStyle
	if !ok {
		status, style = "failed", failStyle
	}
	line := []string{inv.out.paint(heading, "check "+scope), ": ", inv.out.paint(style, status)}
	if fetched {
		line = append(line, ", fetched")
	}
	inv.out.print(line...)
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
