package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/rpc"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/serve"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// updateCheckEvent ends the report of one update check of the serve child,
// after its update_available events: which check it was, when it ended,
// whether it reached what it set out to check, and whether it fetched.
type updateCheckEvent struct {
	event
	InstanceID string `json:"instance_id"`
	Scope      string `json:"scope"`      // checkScopeAll or checkScopeAccount
	CheckedAt  string `json:"checked_at"` // when the check ended, RFC 3339 in UTC
	OK         bool   `json:"ok"`         // false when the check failed as a whole or could not reach the account repo
	Fetched    bool   `json:"fetched"`    // whether the check fetched anything
}

// The scopes of an update_check event: the full check serve runs at start,
// and the check of the account repo alone it repeats while it runs.
const (
	checkScopeAll     = "all"
	checkScopeAccount = "account"
)

// accountCheckInterval is how long the serve child waits, once the report
// of one check is made, before it checks the account repo again, see
// serveChecks.account. accountCheckEvery is what serve waits, which a test
// shortens.
const accountCheckInterval = 10 * time.Minute

var accountCheckEvery = accountCheckInterval

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
			"agentx serve --json", "the desktop app's mode: JSON-RPC on stdin and stdout until stdin closes",
			"agentx serve --once", "scan once, emit the inventory and exit",
			"agentx serve --json --listen localhost --origin http://localhost:1420", "answer the frontend in a browser on this machine too",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout := inv.out.stdout
			if !once && inv.out.json {
				// Everything serve writes on stdout is a JSON-RPC message, so
				// that its client reads one protocol: its own events, the
				// error and the result it ends with among them, go out as
				// event notifications, and the answers to stdin go to
				// stdout as they are.
				inv.out.stdout = eventNotifications{out: stdout}
			}
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
			if once {
				return inv.serveOnce(cmd.Context())
			}
			return inv.serve(cmd.Context(), cmd.InOrStdin(), stdout, address, origins)
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "scan once, emit the inventory and exit")
	cmd.Flags().StringVar(&listen, "listen", "", "also answer JSON-RPC on a WebSocket at this address of this machine, such as localhost or 127.0.0.1:4319")
	cmd.Flags().StringArrayVar(&origins, "origin", nil, "a web origin that may connect to --listen, such as http://localhost:1420 (repeatable)")
	return cmd
}

// watchHint is what to do about a directory serve cannot watch.
const watchHint = "raise the system's watch limit: fs.inotify.max_user_watches on Linux, open files (ulimit -n) on macOS"

// serveLockRefusal is how serve answers err, another serve holding the
// serve lock of agentx home dir.
func serveLockRefusal(dir string, err error) *failure {
	return refuse(exitRefused, err.Error(), "stop the agentx serve holding "+home.ServeLockPath(dir)+" first")
}

// prepareServe sets what the scans of one serve process share.
func (inv *invocation) prepareServe() {
	inv.instanceID() // fixed here, before the goroutines that report it
	inv.verdicts = map[string]keptVerdict{}
	inv.forkWalks = lineage.NewWalkCache()
	inv.forksWarned = true // reconciliation and every inventory name them
}

// serveScan is one scan of serve. It checks git first: serve starts
// without one, see needsGit, so that it stays up to say what is wrong, and
// the scan after git is installed finds it.
func (inv *invocation) serveScan(ctx context.Context) (scan.Inventory, error) {
	if _, err := inv.gitVersion(ctx); err != nil {
		return scan.Inventory{}, err
	}
	return inv.inventory(ctx, 0, "", false)
}

// inventoryReporter reports each inventory serve emits, with the drift
// events against the inventory before it. It keeps the library of the last
// inventory emitted, which the next one's drift is told against;
// inventories are reported from the loop's goroutine alone, so nothing else
// touches it.
func (inv *invocation) inventoryReporter() func(scan.Inventory) {
	var library map[string]scan.LibraryEntry
	return func(inventory scan.Inventory) {
		inv.out.emit(inventoryEvent{event: newEvent("inventory"), Inventory: inventory})
		inv.out.print(inv.out.paint(heading, fmt.Sprintf("inventory %d", inventory.ScanCounter)), ": ",
			plural(len(inventory.Configurations), "configuration"), ", ", plural(len(inventory.Skills), "skill"))
		for _, ev := range driftTransitions(library, inventory) {
			inv.out.emit(ev)
		}
		library = libraryByName(inventory.Library)
	}
}

// serveOnce is serve --once: the initial scan and its inventory, and
// nothing else. It reads no stdin, and another serve holding the lock, or
// a scan that fails, ends it with that error.
func (inv *invocation) serveOnce(ctx context.Context) error {
	lock, err := home.TakeServeLock(inv.dirs.Home)
	if errors.Is(err, home.ErrServing) {
		return serveLockRefusal(inv.dirs.Home, err)
	}
	if err != nil {
		return err
	}
	defer home.Unlock(lock)
	inv.prepareServe()
	return serve.Run(ctx, serve.Options{Scan: inv.serveScan, Once: true, Inventory: inv.inventoryReporter(), Warn: inv.out.warn})
}

// serve is serve without --once: JSON-RPC on stdin and stdout, and on the
// WebSocket door when address is set, until stdin ends or ctx is done. It
// answers stdin before it does anything else, so that run is answered
// while serve takes its lock, reconciles and scans, and what keeps it from
// serving is an error event of its own rather than its end: another serve
// holding the lock, a scan that fails, as one does without git, and
// watching that fails.
func (inv *invocation) serve(ctx context.Context, stdin io.Reader, stdout io.Writer, address string, origins []string) error {
	inv.prepareServe()
	methods, err := inv.newServeMethods()
	if err != nil {
		return err
	}
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	stdio := rpc.ServeStdio(ctx, stdin, lockedWriter{w: inv.out, out: stdout}, methods.table())
	// The end of stdin ends serve once the runs it asked for have stopped,
	// and serve ending for another reason stops them and waits for them
	// too, so that no command it started outlives it.
	go func() {
		<-stdio.Done()
		stop()
	}()
	defer func() {
		stop()
		<-stdio.Done()
	}()
	// The door opens before the lock, reconciliation and the first scan,
	// so that whoever started serve learns at once where it is; a client
	// that connects meanwhile is answered as stdin is.
	if address != "" {
		door, err := inv.openDoor(address, origins, methods.table())
		if err != nil {
			return err
		}
		defer door.close()
	}
	lock, err := home.TakeServeLock(inv.dirs.Home)
	if errors.Is(err, home.ErrServing) {
		// Without the lock serve scans, watches and checks nothing, and
		// answers a refresh with the refusal, until stdin ends: the
		// client that started it reads why from the error event, and
		// can still run commands through it.
		f := serveLockRefusal(inv.dirs.Home, err)
		inv.out.fail(f)
		methods.requests.Refuse(err)
		<-ctx.Done()
		f.reported = true
		return f
	}
	if err != nil {
		return err
	}
	defer home.Unlock(lock)
	// Reconciliation reports and repairs before the first scan, so the
	// inventory shows what it put back. One that cannot run leaves serve
	// to report what the scan finds, and without git the scan says so.
	if _, err := inv.gitVersion(ctx); err == nil {
		if err := inv.serveReconcile(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			inv.out.warn("reconcile: " + err.Error())
		}
	}
	dirs, trees := inv.watchedDirs()
	checks := &serveChecks{inv: inv}
	return serve.Run(ctx, serve.Options{
		Scan:         inv.serveScan,
		Index:        inv.sourceIndex,
		Watch:        dirs,
		Trees:        trees,
		Requests:     methods.requests,
		Check:        checks.startup,
		Recheck:      checks.account,
		RecheckEvery: accountCheckEvery,
		Jobs:         []serve.Job{inv.maintenanceJob},
		Inventory:    inv.inventoryReporter(),
		Fail:         inv.serveFailed,
		Warn:         inv.out.warn,
	})
}

// serveFailed reports what serve goes on from as an error event of its
// own, see serve.Options.Fail: a directory it cannot watch is refused, and
// a scan that failed carries the code of its error, as the exit code of a
// command that failed the same way would.
func (inv *invocation) serveFailed(err error) {
	if errors.Is(err, serve.ErrWatch) {
		inv.out.fail(refuse(exitRefused, err.Error(), watchHint))
		return
	}
	f, ok := knownFailure(inv.dirs.Home, err)
	if !ok {
		f = &failure{status: exitInternal, message: err.Error()}
	}
	inv.out.fail(f)
}

// serveChecks are the update checks of the serve child: the full check it
// runs once, as soon as its initial inventory is out, see startup, and the
// check of the account repo alone it repeats while it runs, see account repo.
// serve runs one at a time and makes each report on its loop's goroutine,
// so failing needs no lock.
type serveChecks struct {
	inv *invocation
	// failing is set while the checks fail to reach the account repo, or
	// fail as a whole: the check that starts failing warns, and those after
	// it stay quiet until one succeeds again.
	failing bool
}

// startup is the serve child's update check at start: the check skill
// check-updates runs, the account repo included, over every source of
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
// store; a source an import wrote stays a source not fetched,
// store or not, until source add adds it. The rescan its write of
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
// accountCheckEvery: what another machine published to account skills
// since the last fetch of the account repo. With no account repo set,
// or no store, it runs no git and reports nothing. Otherwise one
// git ls-remote asks the account repo for its fork branches, see
// gitx.Runner.ForkTips, and when they are what the remote-tracking
// branches the last fetch left hold, read in one for-each-ref, it fetches
// nothing, takes no lock and writes nothing. When they differ, it runs the
// update check of the account repo alone, see checkUpdates: one fetch of
// it, outside the lock, the comparison of account skills placed here with
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
		if _, err := os.Stat(gitx.StorePath(inv.dirs.Home)); err != nil {
			return nil // no store yet: nothing was ever fetched into one
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

// checkAccountRemote is the account repo check of the serve child, see
// serveChecks.account, of the account repo at url, the git remote
// called remote in the store: an empty report, neither idle nor
// fetched, when the account repo holds the fork branches its
// remote-tracking branches hold, and otherwise the report of the update
// check of the account repo alone. An ls-remote that fails is reported
// as a fetch of the account repo that failed.
func (inv *invocation) checkAccountRemote(ctx context.Context, url, remote string) (checkReport, error) {
	theirs, err := inv.git.ForkTips(ctx, url)
	if err != nil {
		f := uncheckedAccount(url, fmt.Errorf("%w: %v", source.ErrUnreachable, err))
		return checkReport{accountFailed: true, failures: []checkFailure{{source: url, fetch: true, f: f}}}, nil
	}
	ours, err := lineage.ListRemote(ctx, inv.git, gitx.StorePath(inv.dirs.Home), remote)
	if err != nil {
		return checkReport{}, storeFailure(err)
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
// rewrites; the store, the worktrees, the library and every agent
// client's user-scope skills directory hold content. Everything but agentx
// home and the store is a tree: an edit anywhere inside a skill is a
// signal too, wherever the scan reads that skill from. A skills directory a
// client shares with another, and the library itself, are watched once.
//
// The reflogs of the fork branches are a tree too: a commit made with git
// in a fork's worktree writes nothing in the worktree or in agentx home,
// only in the store, where it appends to its branch's reflog, so
// that is how serve learns the fork moved. So are the reflogs of the
// remote-tracking branches: a publish with nothing to record, as of a
// commit made with git, moves only the account repo's branch as the
// store tracks it, which is what a fork's state is judged against,
// see unpublished. Those reflog trees appear with the first ref of their
// kind, a skill's first publish creating logs/refs/remotes and every
// directory below it at once, so logs and logs/refs are watched too: the
// directory appearing there is the signal, and a sync takes the new tree in.
// The worktrees' own admin directories are not watched: git writes an index
// there on every status, which would be a signal of its own.
func (inv *invocation) watchedDirs() (dirs, trees []string) {
	h := inv.dirs.Home
	logs := filepath.Join(gitx.StorePath(h), "logs")
	refs := filepath.Join(logs, "refs")
	trees = []string{filepath.Join(h, "worktrees"), inv.dirs.Library, filepath.Join(refs, "heads", "skills"), filepath.Join(refs, "remotes")}
	for _, dir := range scan.UserSkillsDirs(inv.dirs) {
		if !slices.Contains(trees, dir) {
			trees = append(trees, dir)
		}
	}
	return append([]string{h, gitx.StorePath(h), logs, refs}, trees...), trees
}

// sourceIndex builds the index serve answers searches from: the sources of
// the machine settings, each listed from its ref in the store. The
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
		// The account repo holds forks rather than skills on one branch,
		// and is never fetched as a shared source, so it has nothing to
		// search.
		shared := sharedSources(s.Sources)
		urls := make([]string, 0, len(shared))
		for _, src := range shared {
			urls = append(urls, src.URL)
		}
		idx, warnings, err = source.BuildIndex(ctx, inv.git, gitx.StorePath(inv.dirs.Home), urls, prev)
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
