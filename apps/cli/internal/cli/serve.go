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
	"strconv"
	"sync/atomic"
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
	Scope      string `json:"scope"`      // checkScopeAll or checkScopeHub
	CheckedAt  string `json:"checked_at"` // when the check ended, RFC 3339 in UTC
	OK         bool   `json:"ok"`         // false when the check failed as a whole or could not reach the hub
	Fetched    bool   `json:"fetched"`    // whether the check fetched anything
}

// The scopes of an update_check event: the full check serve runs at start,
// and the check of the hub alone it repeats while it runs.
const (
	checkScopeAll = "all"
	checkScopeHub = "hub"
)

// hubCheckInterval is how long the serve child waits, once the report
// of one check is made, before it checks the hub again, see
// serveChecks.hub. hubCheckEvery is what serve waits, which a test
// shortens.
const hubCheckInterval = 10 * time.Minute

var hubCheckEvery = hubCheckInterval

// reportQuietPeriod is how long the machine must have been quiet, no scan
// ending, before the serve child writes this machine's inventory report,
// see serveChecks.report. reportQuietFor is what serve waits, which a test
// shortens.
const reportQuietPeriod = time.Minute

var reportQuietFor = reportQuietPeriod

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

// serveScan is one scan of serve, the library read as library says, see
// scanMachine. It checks git first: serve starts without one, see needsGit,
// so that it stays up to say what is wrong, and the scan after git is
// installed finds it.
func (inv *invocation) serveScan(ctx context.Context, library libraryRead) (inventory scan.Inventory, unlisted *failure, err error) {
	if _, err := inv.gitVersion(ctx); err != nil {
		return scan.Inventory{}, nil, err
	}
	return inv.scanMachine(ctx, 0, "", false, library)
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
	scan := func(ctx context.Context) (scan.Inventory, error) {
		inventory, _, err := inv.serveScan(ctx, libraryEmitted)
		return inventory, err
	}
	return serve.Run(ctx, serve.Options{Scan: scan, Once: true, Inventory: inv.inventoryReporter(), Warn: inv.out.warn})
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
		Scan:         checks.scan,
		Index:        inv.sourceIndex,
		Watch:        dirs,
		Trees:        trees,
		Requests:     methods.requests,
		Check:        checks.startup,
		Recheck:      checks.hub,
		RecheckEvery: hubCheckEvery,
		Quiet:        checks.report,
		QuietAfter:   reportQuietFor,
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

// serveChecks is what the serve child does with its sources and the hub in
// the background: the full update check it runs once, as soon as its
// initial inventory is out, see startup, the check of the hub alone it
// repeats while it runs, see hub, each of which reads the hub's machines
// too, see machines, and the inventory report of this machine it writes
// once the machine has been quiet, see report. serve runs one of the three
// at a time and makes each report on its loop's goroutine before it starts
// the next, so none of this needs a lock but scanned, which the loop writes
// while one of them runs.
type serveChecks struct {
	inv *invocation
	// failing is set while the checks fail to reach the hub, or
	// fail as a whole: the check that starts failing warns, and those after
	// it stay quiet until one succeeds again.
	failing bool
	// fleet is the commit every machine branch of the hub held when the
	// last fleet event was emitted, by branch; nil before the first.
	fleet map[string]string
	// scanned is the last inventory serve scanned, which the report is
	// built from; nil before the first.
	scanned atomic.Pointer[scannedInventory]
	// reported is the report the hub's branch of this machine held when the
	// last report was written or found written already; nil before the
	// first, after one that failed and once the machine no longer reports.
	reported []byte
	// reportFailing is failing for the reports: set while they fail, until
	// one is written or the machine no longer reports. A hub check that
	// reached the hub writes the report again while it is set.
	reportFailing bool
}

// fleetRead is what one read of the hub's machines by the serve child came
// to: the commit each machine branch holds, the fleet they make, nil when
// they are the commits the last fleet event was read at, and the failure
// that kept the machines from being read. The zero value is no read.
type fleetRead struct {
	tips  map[string]string
	fleet *fleet
	err   error
}

// startup is the serve child's update check at start: the check skill
// check-updates runs, the hub included, over every source of
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
//
// Then it reads the hub's machines, see hubMachines, unless the check just
// found the hub out of reach, and its report carries the fleet event of
// that read before its update_check.
func (c *serveChecks) startup(ctx context.Context) func() {
	rep, err := c.inv.checkUpdates(ctx, true, "")
	var machines fleetRead
	if err == nil && !rep.hubFailed {
		machines = c.hubMachines(ctx)
	}
	at := time.Now()
	return func() {
		c.reportCheck(checkScopeAll, at, rep, err, machines, err == nil && len(rep.refreshed) > 0)
	}
}

// hub is the check the serve child repeats while it runs, see
// hubCheckEvery: what another machine published to hub skills
// since the last fetch of the hub, and what the hub holds of every
// machine. With no hub set, or no store, it runs no git and reports
// nothing. Otherwise one git ls-remote asks the hub for its fork branches
// and its machine branches, see checkHubRemote. When the fork branches are
// what the remote-tracking branches the last fetch left hold, read in one
// for-each-ref, it fetches none of them, takes no lock and writes nothing.
// When they differ, it runs the
// update check of the hub alone, see checkUpdates: one fetch of
// it, outside the lock, the comparison of hub skills placed here with
// what it holds, and one write of its last_fetched, under the lock, waited
// for as the check at start waits. Its report is that check's: one
// update_available per update, the fleet event when the hub's machines
// changed, see machines, then its update_check. A failure is a
// warning prefixed "update check: " when the checks start failing, and
// none while they go on failing, see serveChecks.failing. A check that
// reached the hub while the reports of this machine fail writes the report
// again, see report: no scan follows a hub that is back in reach.
func (c *serveChecks) hub(ctx context.Context) func() {
	inv := c.inv
	entry, remote, set, err := inv.hubSource()
	if err == nil && !set {
		return nil
	}
	if err == nil {
		if _, err := os.Stat(gitx.StorePath(inv.dirs.Home)); err != nil {
			return nil // no store yet: nothing was ever fetched into one
		}
	}
	var rep checkReport
	var machines fleetRead
	if err == nil {
		rep, machines, err = c.checkHubRemote(ctx, entry, remote)
	}
	at := time.Now()
	var again func()
	if c.reportFailing && err == nil && !rep.hubFailed && machines.err == nil {
		again = c.report(ctx)
	}
	return func() {
		c.reportCheck(checkScopeHub, at, rep, err, machines, err == nil && rep.hubFetched)
		if again != nil {
			again()
		}
	}
}

// reportCheck makes the report of one check of scope that ended at at, on
// the loop's goroutine: what the update check found, rep or err, see
// reportServeCheck, then the fleet event of the read of the hub's
// machines, see reportFleet, then the check's update_check. A check that
// could not reach the hub, failed as a whole or could not read the hub's
// machines is not ok: it warns when the checks start failing and gives none
// of its warnings while they go on failing, see serveChecks.failing. A
// check that had nothing of the sources to check emits no update_check, and
// reports nothing at all when it read no machines either.
func (c *serveChecks) reportCheck(scope string, at time.Time, rep checkReport, err error, machines fleetRead, fetched bool) {
	inv := c.inv
	idle := err == nil && rep.idle
	if idle && machines.tips == nil && machines.err == nil {
		return // nothing to check, and nothing was
	}
	reached := err == nil && !rep.hubFailed
	if !idle && (reached || !c.failing) {
		inv.reportServeCheck(rep, err)
	}
	if machines.err != nil && !c.failing {
		inv.out.warn("update check: " + machines.err.Error())
	}
	ok := reached && machines.err == nil
	c.failing = !ok
	c.reportFleet(machines)
	if !idle {
		inv.reportUpdateCheck(scope, at, ok, fetched)
	}
}

// checkHubRemote is the hub check of the serve child, see
// serveChecks.hub, of the hub entry names, the git remote
// called remote in the store. Its one ls-remote asks for the fork branches
// and for the machine branches. The report is empty, neither idle nor
// fetched, when the hub holds the fork branches its
// remote-tracking branches hold, and otherwise the report of the update
// check of the hub alone. An ls-remote that fails is reported
// as a fetch of the hub that failed. The hub's machines are read from the
// same answer, see machines, unless that update check found the hub out of
// reach or no longer set.
func (c *serveChecks) checkHubRemote(ctx context.Context, entry home.Source, remote string) (checkReport, fleetRead, error) {
	inv := c.inv
	url := entry.URL
	tips, err := inv.git.TipsAt(ctx, url, append([]string{gitx.ForkBranches}, machineBranches()...)...)
	if err != nil {
		f := uncheckedHub(url, fmt.Errorf("%w: %v", source.ErrUnreachable, err))
		return checkReport{hubFailed: true, failures: []checkFailure{{source: url, fetch: true, f: f}}}, fleetRead{}, nil
	}
	theirs, machines := map[string]string{}, map[string]string{}
	for branch, tip := range tips {
		if name, fork := gitx.ForkName(branch); fork {
			theirs[name] = tip
		} else {
			machines[branch] = tip
		}
	}
	ours, err := lineage.ListRemote(ctx, inv.git, gitx.StorePath(inv.dirs.Home), remote)
	if err != nil {
		return checkReport{}, fleetRead{}, storeFailure(err)
	}
	var rep checkReport
	if !maps.Equal(theirs, ours) {
		if rep, err = inv.checkUpdates(ctx, true, url); err != nil || rep.idle || rep.hubFailed {
			return rep, fleetRead{}, err
		}
	}
	return rep, c.machines(ctx, entry, remote, machines), nil
}

// hubMachines reads the hub's machines for the check at start, which
// fetches the fork branches without asking for them first: one ls-remote
// of its own asks for the machine branches alone, see machines. With no
// hub set, or no store, it runs no git and is no read. A hub the ls-remote
// cannot reach is the failure the hub check gives for one.
func (c *serveChecks) hubMachines(ctx context.Context) fleetRead {
	inv := c.inv
	entry, remote, set, err := inv.hubSource()
	if err != nil || !set {
		return fleetRead{}
	}
	if _, err := os.Stat(gitx.StorePath(inv.dirs.Home)); err != nil {
		return fleetRead{}
	}
	tips, err := inv.git.TipsAt(ctx, entry.URL, machineBranches()...)
	if err != nil {
		return fleetRead{err: uncheckedHub(entry.URL, fmt.Errorf("%w: %v", source.ErrUnreachable, err))}
	}
	return c.machines(ctx, entry, remote, tips)
}

// machines reads the hub's machines, every machine's report and the cloud
// machine's snapshot, from tips: the commit each machine branch of the hub
// entry names holds, as a check's ls-remote found them. Only the branches
// that moved are fetched, in one fetch, and none when none did, see
// heldBranches. The hub's remote is brought in line with its entry first,
// under the lock, waited for, as the update check brings it, and only ahead
// of a fetch. The files are read only when a branch holds another commit
// than the last fleet event was read at, see fleetHeld.
func (c *serveChecks) machines(ctx context.Context, entry home.Source, remote string, tips map[string]string) fleetRead {
	inv := c.inv
	gitDir := gitx.StorePath(inv.dirs.Home)
	align := func() error {
		return inv.holdLock(ctx, true, false, func() error { return inv.alignRemotes(ctx, gitDir, []fetchTarget{target(entry)}) })
	}
	held, err := inv.heldBranches(ctx, gitDir, remote, entry.URL, tips, align, machineBranches()...)
	if err != nil {
		return fleetRead{err: err}
	}
	return c.fleetHeld(ctx, gitDir, remote, entry.URL, held)
}

// fleetHeld is the read of the hub's machines from held, what the store's
// remote-tracking refs hold of the machine branches. It reaches no remote.
// When every branch holds the commit the last fleet event was read at,
// nothing changed and no file is read. The first read always makes a
// fleet, of no machine when the hub holds none.
func (c *serveChecks) fleetHeld(ctx context.Context, gitDir, remote, url string, held map[string]gitx.Tracked) fleetRead {
	tips := make(map[string]string, len(held))
	for branch, t := range held {
		tips[branch] = t.Commit
	}
	if c.fleet != nil && maps.Equal(tips, c.fleet) {
		return fleetRead{tips: tips}
	}
	f, err := c.inv.fleetOf(ctx, gitDir, remote, url, held)
	if err != nil {
		return fleetRead{err: err}
	}
	return fleetRead{tips: tips, fleet: &f}
}

// reportFleet emits the fleet event of a read that found the hub's
// machines changed, the type machine list emits, with its line without
// --json and a warning for each branch no view can use, and keeps what it
// was read at for the next read. A read that found nothing changed, or
// failed, emits nothing.
func (c *serveChecks) reportFleet(read fleetRead) {
	if read.fleet == nil {
		return
	}
	out := c.inv.out
	out.emit(read.fleet.event())
	out.print(out.paint(heading, "machines"), ": ", strconv.Itoa(len(read.fleet.machines)), " on the hub")
	for _, w := range read.fleet.warnings {
		out.warn(w)
	}
	c.fleet = read.tips
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
// commit made with git, moves only the hub's branch as the
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
		// The hub holds forks rather than skills on one branch,
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
