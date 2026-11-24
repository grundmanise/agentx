// Package serve is the long-running child the desktop app holds open: it
// scans on start, rescans when the machine changes, and answers what the
// doors of the process ask through its Requests.
package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

const (
	debounce = 100 * time.Millisecond // quiet time after the last change before a rescan
	maxWait  = 500 * time.Millisecond // a rescan happens this long after the first change at the latest
)

// ErrWatch wraps the error of a directory that cannot be watched: the
// watcher cannot be created, or a watch cannot be added. Serve then stops
// watching and scans only when it is asked to.
var ErrWatch = errors.New("cannot watch for changes")

// Options wires one serve loop to the command that runs it: how to scan and
// how to report. Check, Recheck and Quiet run on a goroutine of their own,
// one of the three at a time, and the report each returns is called from
// the loop's goroutine, as every other report function is.
type Options struct {
	Scan  func(ctx context.Context) (scan.Inventory, error)                              // one whole scan; ctx bounds its wait for the lock
	Index func(ctx context.Context, prev *source.Index) (*source.Index, []string, error) // the source index after a scan, prev being the last one built; nil indexes nothing
	Watch []string                                                                       // directories to watch, in order; a missing one is retried before each rescan
	Trees []string                                                                       // directories whose subdirectories, present or added later, are watched too
	Once  bool                                                                           // scan once, emit and return

	// Requests is what the doors of the process ask through; nil for none.
	Requests *Requests

	// Check is the update check, run once, off the loop's goroutine, after
	// the first inventory; it returns what reports its outcome, nil for
	// nothing to report. nil checks nothing, and so does Once. It starts
	// with the jobs, see Job.
	Check Job
	// Recheck is the check serve repeats while it runs, off the loop's
	// goroutine as Check is: RecheckEvery after the report of Check is
	// made, or after the first inventory when Check is nil, and again
	// RecheckEvery after each report of its own. So it never runs beside
	// itself or beside Check. nil repeats nothing, and so does Once.
	Recheck      Job
	RecheckEvery time.Duration
	// Quiet is the job serve runs once the machine has been quiet, off the
	// loop's goroutine as a check is: QuietAfter after a scan ended, when
	// no other scan ended since, whether or not the inventory changed, and
	// never before the first inventory. A run of scans, each within
	// QuietAfter of the one before, so leads to one run of it, after the
	// last. It never runs beside itself or beside a check: one that is due
	// while a check or a run of its own goes on starts once that one's
	// report is made, and a check that is due while it runs does the same.
	// A scan that ends while it runs starts the wait again, so another run
	// follows. nil runs nothing, and so does Once.
	Quiet      Job
	QuietAfter time.Duration
	// Jobs are the other jobs serve runs once at start, such as the home
	// repo's maintenance.
	Jobs []Job

	Inventory func(scan.Inventory) // a changed whole inventory, counter set
	// Fail reports what goes wrong that serve goes on from: the initial
	// scan failing, and watching failing, at start or later, wrapped in
	// ErrWatch. Under Once the initial scan's error is returned instead.
	Fail func(err error)
	Warn func(message string)
}

// Requests is how the doors of the serve process, its standard streams and
// its WebSocket, ask the loop for a scan and search the index it built. Its
// methods are called from any goroutine, before and while Run serves; one
// Requests serves one Run, or none, see Refuse.
type Requests struct {
	refreshes chan func(counter int, err error)
	loop      atomic.Pointer[loop]
	scanned   chan struct{} // closed by Run once its first scan has ended, with the index it built
	refused   chan struct{} // closed by Refuse, once refusal is set
	refusal   error
}

// NewRequests makes the Requests of one Run, to be passed in its Options.
func NewRequests() *Requests {
	return &Requests{refreshes: make(chan func(int, error)), scanned: make(chan struct{}), refused: make(chan struct{})}
}

// Refuse says that no Run will answer: every Refresh waiting, and every
// later one, returns err at once. It is called at most once, by a serve
// that cannot serve, such as one that found another serve holding its lock.
func (r *Requests) Refuse(err error) {
	r.refusal = err
	close(r.refused)
}

// Refresh asks for a scan and waits for it: the counter it returns is that
// of the current inventory, changed or not, after a scan that began once
// Refresh was called. The error is the scan's, the one Refuse was given, or
// ctx's when ctx is done first. Run answers no request once it has
// returned, so ctx must be done by then.
func (r *Requests) Refresh(ctx context.Context) (counter int, err error) {
	type outcome struct {
		counter int
		err     error
	}
	done := make(chan outcome, 1)
	select {
	case r.refreshes <- func(counter int, err error) { done <- outcome{counter, err} }:
	case <-r.refused:
		return 0, r.refusal
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	select {
	case o := <-done:
		return o.counter, o.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// Search answers a search from the index in memory: it takes no lock and
// runs no git. It waits for the first scan of Run to end, so that a search
// sent while serve starts is answered from the index that scan built, and
// never waits after that: it is answered while a scan is in progress. One
// that Refuse answers, or that came before a first scan that failed, finds
// nothing. The error is ctx's when ctx is done first.
func (r *Requests) Search(ctx context.Context, query string) ([]source.Match, error) {
	select {
	case <-r.scanned:
	case <-r.refused:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var idx *source.Index
	if l := r.loop.Load(); l != nil {
		idx = l.index.Load()
	}
	return idx.Search(query), nil
}

// Job is one job serve runs off the loop's goroutine, once the first
// inventory is out: each of Jobs once, beside each other, the checks, see
// Check and Recheck, and the quiet job, see Quiet. The report a run
// returns, nil for nothing to report, is made on the loop's goroutine,
// where every other report is. Under Once no job runs, and Run waits for a
// run still going, stopped by its context, before it returns.
type Job func(ctx context.Context) (report func())

// Run serves until ctx is done or, under Once, the initial inventory is
// emitted. Under Once it returns the error of the initial scan. Otherwise
// it reports what fails through Fail and goes on: a scan that failed is
// tried again on the next change or refresh, and once watching fails serve
// scans only when it is asked to. A cancelled ctx is a clean end.
func Run(ctx context.Context, o Options) error {
	ctx, cancel := context.WithCancel(ctx) // ends the jobs when Run returns
	l := &loop{Options: o}
	var refreshes chan func(counter int, err error) // nil without Requests: never ready
	if o.Requests != nil {
		refreshes = o.Requests.refreshes
		o.Requests.loop.Store(l)
	}
	var checking sync.WaitGroup
	defer func() {
		cancel()
		// A check still running is stopped by the cancel and waited for, so
		// that no git of it outlives serve and no hold of the lock does
		// either; its report is dropped.
		checking.Wait()
	}()
	if o.Once {
		return l.initial(ctx)
	}

	// The watcher is made before the initial scan, so a change during it is
	// not missed. Without one the watch channels are nil, which are never
	// ready, and serve scans only when it is asked to.
	var events <-chan struct{}
	var watchErrors <-chan error
	w, err := newWatcher(o.Watch, o.Trees)
	if err != nil {
		o.Fail(fmt.Errorf("%w: %w", ErrWatch, err))
	} else {
		events, watchErrors = w.events, w.errors
	}
	defer func() {
		if w != nil {
			w.close()
		}
	}()

	// The update check and every other job run off this goroutine, since a
	// check waits on the network for as long as its sources take and a
	// rescan may not wait for it: each once, as soon as the first inventory
	// is out, which is after the initial scan unless that one failed. The
	// report of one that ended comes back over ended and is made here,
	// where every other report is. The checks, Check and then Recheck over
	// and over, report over checked instead, which is what times the next
	// one: one check at a time, the next timed from the report of the last.
	// The quiet job reports over quieted.
	ended, checked, quieted := make(chan func()), make(chan func()), make(chan func())
	start := func(job Job, done chan<- func()) {
		checking.Add(1)
		go func() {
			defer checking.Done()
			report := job(ctx)
			select {
			case done <- report:
			case <-ctx.Done():
			}
		}()
	}
	// A check and the quiet job run one at a time, so that what they share
	// needs no lock: busy is set while one runs, and one whose time comes
	// meanwhile is due, and starts once the report of the one running is
	// made, the quiet job first.
	var recheckC, quietC <-chan time.Time
	busy, recheckDue, quietDue := false, false, false
	startDue := func() {
		switch {
		case busy:
		case quietDue:
			busy, quietDue = true, false
			start(o.Quiet, quieted)
		case recheckDue:
			busy, recheckDue = true, false
			start(o.Recheck, checked)
		}
	}
	jobsStarted := false
	startJobs := func() {
		if jobsStarted || l.counter == 0 {
			return
		}
		jobsStarted = true
		switch {
		case o.Check != nil:
			busy = true
			start(o.Check, checked)
		case o.Recheck != nil:
			recheckC = time.After(o.RecheckEvery)
		}
		for _, job := range o.Jobs {
			if job != nil {
				start(job, ended)
			}
		}
	}
	// scanned follows every scan, whatever it found: once an inventory is
	// out, the wait for the quiet job starts again.
	scanned := func() {
		startJobs()
		if o.Quiet != nil && l.counter > 0 {
			quietC = time.After(o.QuietAfter)
		}
	}
	if err := l.scan(ctx); err != nil && ctx.Err() == nil {
		o.Fail(err)
	}
	if o.Requests != nil {
		close(o.Requests.scanned)
	}
	scanned()

	// A change starts the debounce timer and, unless one is running, the
	// deadline timer; whichever fires first triggers the rescan.
	var debounceC, deadlineC <-chan time.Time
	schedule := func() {
		if deadlineC == nil {
			deadlineC = time.After(maxWait)
		}
		debounceC = time.After(debounce)
	}
	// rescan brings the watches in line first, so a directory the scan finds
	// is watched from then on; a directory that cannot be watched is
	// reported, and serve stops watching.
	rescan := func() {
		debounceC, deadlineC = nil, nil
		if w != nil {
			if err := w.sync(); err != nil {
				o.Fail(fmt.Errorf("%w: %w", ErrWatch, err))
				w.close()
				w, events, watchErrors = nil, nil, nil
			}
		}
		if err := l.scan(ctx); err != nil && ctx.Err() == nil {
			o.Warn("scan failed: " + err.Error())
		}
		scanned()
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case done := <-refreshes:
			l.pending = append(l.pending, done)
			schedule()
		case <-events:
			schedule()
		case err := <-watchErrors:
			o.Warn("watch: " + err.Error())
			schedule()
		case <-debounceC:
			rescan()
		case <-deadlineC:
			rescan()
		case report := <-ended:
			if report != nil {
				report()
			}
		case report := <-checked:
			busy = false
			if report != nil {
				report()
			}
			if o.Recheck != nil {
				recheckC = time.After(o.RecheckEvery)
			}
			startDue()
		case report := <-quieted:
			busy = false
			if report != nil {
				report()
			}
			startDue()
		case <-recheckC:
			recheckC, recheckDue = nil, true
			startDue()
		case <-quietC:
			quietC, quietDue = nil, true
			startDue()
		}
	}
}

// loop is the state one serve child keeps between scans.
type loop struct {
	Options
	last    []byte // canonical bytes of the last emitted inventory
	counter int    // scan_counter of the last emitted inventory; 0 before the first
	// pending are the refresh requests received before the next scan, each
	// as what acknowledges it.
	pending []func(counter int, err error)

	index atomic.Pointer[source.Index] // what searches are answered from; replaced whole after a scan
}

// initial is the one scan of Once, whose failure ends serve unless serve
// is ending anyway.
func (l *loop) initial(ctx context.Context) error {
	if err := l.scan(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// scan runs one scan and emits the inventory when it changed, then one
// acknowledgement per refresh request received before the scan began. The
// first scan that succeeds always emits. Nothing is emitted once ctx is
// done.
func (l *loop) scan(ctx context.Context) error {
	pending := l.pending
	l.pending = nil
	inventory, err := l.Scan(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		for _, acknowledge := range pending {
			acknowledge(0, err)
		}
		return err
	}
	b := canonical(inventory)
	if l.counter == 0 || !bytes.Equal(b, l.last) {
		l.counter++
		l.last = b
		inventory.ScanCounter = l.counter
		l.Inventory(inventory)
	}
	l.reindex(ctx)
	for _, acknowledge := range pending {
		acknowledge(l.counter, nil)
	}
	return nil
}

// reindex rebuilds the source index after a scan has emitted its inventory
// and before that scan's acknowledgements go out, so that a refresh
// acknowledged after a source was added, fetched again or removed is
// answered from the new index, while the inventory never waits for the lock
// this takes. A rebuild that fails leaves the last index in force. Nothing
// is indexed under Once, which answers no requests.
func (l *loop) reindex(ctx context.Context) {
	if l.Index == nil || l.Once {
		return
	}
	idx, warnings, err := l.Index(ctx, l.index.Load())
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		l.Warn("source index: " + err.Error())
		return
	}
	for _, w := range warnings {
		l.Warn(w)
	}
	l.index.Store(idx)
}

// canonical serialises an inventory without the counter, the one field that
// differs between identical inventories of one serve process.
func canonical(inventory scan.Inventory) []byte {
	inventory.ScanCounter = 0
	b, err := json.Marshal(inventory)
	if err != nil {
		panic(err) // an inventory is plain structs; marshalling cannot fail
	}
	return b
}
