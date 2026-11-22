// Package serve is the long-running child the desktop app holds open: it
// scans on start, rescans when the machine changes, and answers requests
// read from stdin and, through its Requests, from another door of the
// process.
package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// ErrWatch wraps the error that ends serve because a directory cannot be
// watched: the watcher cannot be created, or a watch cannot be added.
var ErrWatch = errors.New("cannot watch for changes")

// Options wires one serve loop to the command that runs it: how to scan and
// how to report. Search, and BadRequest for a search, are called from the
// goroutine that reads stdin, so that a search is answered while a scan
// runs; the two are never called at once, and neither is called once Run
// has returned. Check and Recheck run on a goroutine of their own, and the
// report each returns is called from the loop's goroutine, as every other
// report function is.
type Options struct {
	Scan  func(ctx context.Context) (scan.Inventory, error)                              // one whole scan; ctx bounds its wait for the lock
	Index func(ctx context.Context, prev *source.Index) (*source.Index, []string, error) // the source index after a scan, prev being the last one built; nil indexes nothing
	Watch []string                                                                       // directories to watch, in order; a missing one is retried before each rescan
	Trees []string                                                                       // directories whose subdirectories, present or added later, are watched too
	Once  bool                                                                           // scan once, emit and return
	Stdin io.Reader                                                                      // request lines

	// Requests is what another door of the process asks through; nil for
	// none.
	Requests *Requests

	// Check is the update check, run once, off the loop's goroutine, after
	// the initial inventory; it returns what reports its outcome, nil for
	// nothing to report. nil checks nothing, and so does Once. It starts
	// with the jobs, see Job.
	Check Job
	// Recheck is the check serve repeats while it runs, off the loop's
	// goroutine as Check is: RecheckEvery after the report of Check is
	// made, or after the initial inventory when Check is nil, and again
	// RecheckEvery after each report of its own. So it never runs beside
	// itself or beside Check. nil repeats nothing, and so does Once.
	Recheck      Job
	RecheckEvery time.Duration
	// Jobs are the other jobs serve runs once at start, such as the account
	// repo's maintenance.
	Jobs []Job

	Inventory       func(scan.Inventory)                                  // a changed whole inventory, counter set
	RefreshComplete func(requestID string, counter int, err error)        // the acknowledgement of one refresh request
	Search          func(requestID, query string, results []source.Match) // the answer to one search request
	BadRequest      func(message, hint string)                            // a stdin line that is not a request
	Warn            func(message string)
}

// Requests is how another door of the serve process, such as its
// WebSocket, asks for what stdin's requests ask for. Its methods are
// called from any goroutine, before and while Run serves; one Requests
// serves one Run.
type Requests struct {
	refreshes chan func(counter int, err error)
	loop      atomic.Pointer[loop]
}

// NewRequests makes the Requests of one Run, to be passed in its Options.
func NewRequests() *Requests {
	return &Requests{refreshes: make(chan func(int, error))}
}

// Refresh asks for a scan as a refresh request does, and waits for it: the
// counter it returns is that of the current inventory, changed or not,
// after a scan that began once Refresh was called. The error is the
// scan's, or ctx's when ctx is done first. Run answers no request once it
// has returned, so ctx must be done by then.
func (r *Requests) Refresh(ctx context.Context) (counter int, err error) {
	type outcome struct {
		counter int
		err     error
	}
	done := make(chan outcome, 1)
	select {
	case r.refreshes <- func(counter int, err error) { done <- outcome{counter, err} }:
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

// Search answers a search as a search request is answered: from the index
// in memory, while a scan is in progress. Before the first index is built
// it finds nothing.
func (r *Requests) Search(query string) []source.Match {
	var idx *source.Index
	if l := r.loop.Load(); l != nil {
		idx = l.index.Load()
	}
	return idx.Search(query)
}

// Job is one job serve runs off the loop's goroutine, once the initial
// inventory is out: each of Jobs once, beside each other, and the checks,
// see Check and Recheck. The report a run returns, nil
// for nothing to report, is made on the loop's goroutine, where every other
// report is. Under Once no job runs, and Run waits for a run still going,
// stopped by its context, before it returns.
type Job func(ctx context.Context) (report func())

const (
	requestHint = `send one JSON object per line, such as {"type":"refresh","request_id":"<unique id>"}`
	searchHint  = `send {"type":"search","request_id":"<unique id>","query":"<text>"}`
	typesHint   = "request types: refresh, search"
)

// Run serves until ctx is done, stdin closes or, under Once, the initial
// inventory is emitted. It returns an error when the initial scan fails or
// when watching fails, at start or later, wrapped in ErrWatch; a cancelled
// ctx is a clean end.
func Run(ctx context.Context, o Options) error {
	ctx, cancel := context.WithCancel(ctx) // ends the stdin reader when Run returns for another reason, and the scan in progress when stdin closes
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
		// either; its report is dropped. A search being answered in the
		// reader's goroutine finishes first, so that nothing is reported
		// after Run returned.
		checking.Wait()
		l.mu.Lock()
		l.closed = true
		l.mu.Unlock()
	}()
	if o.Once {
		return l.initial(ctx)
	}

	w, err := newWatcher(o.Watch, o.Trees) // before the initial scan, so a change during it is not missed
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWatch, err)
	}
	defer w.close()
	if err := l.initial(ctx); err != nil {
		return err
	}
	reqs := readRequests(ctx, cancel, o.Stdin, l.answer)

	// The update check and every other job run off this goroutine, since a
	// check waits on the network for as long as its sources take and a
	// rescan may not wait for it: each once, now that the initial inventory
	// is out. The report of one that ended comes back over ended and is made
	// here, where every other report is. The checks, Check and then Recheck
	// over and over, report over checked instead, which is what times the
	// next one: one check at a time, the next timed from the report of the
	// last.
	ended, checked := make(chan func()), make(chan func())
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
	var recheckC <-chan time.Time
	switch {
	case o.Check != nil:
		start(o.Check, checked)
	case o.Recheck != nil:
		recheckC = time.After(o.RecheckEvery)
	}
	for _, job := range o.Jobs {
		if job != nil {
			start(job, ended)
		}
	}

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
	// is watched from then on; a directory that cannot be watched ends serve.
	rescan := func() error {
		debounceC, deadlineC = nil, nil
		if err := w.sync(); err != nil {
			return fmt.Errorf("%w: %w", ErrWatch, err)
		}
		if err := l.scan(ctx); err != nil && ctx.Err() == nil {
			o.Warn("scan failed: " + err.Error())
		}
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case r, ok := <-reqs:
			if !ok {
				return nil
			}
			switch {
			case r.err != nil:
				o.BadRequest(r.err.Error(), requestHint)
			case r.Type != "refresh":
				o.BadRequest(fmt.Sprintf("unknown request type %q", r.Type), typesHint)
			case r.RequestID == "":
				o.BadRequest("refresh request has no request_id", requestHint)
			default:
				l.pending = append(l.pending, func(counter int, err error) { o.RefreshComplete(r.RequestID, counter, err) })
				schedule()
			}
		case done := <-refreshes:
			l.pending = append(l.pending, done)
			schedule()
		case <-w.events:
			schedule()
		case err := <-w.errors:
			o.Warn("watch: " + err.Error())
			schedule()
		case <-debounceC:
			if err := rescan(); err != nil {
				return err
			}
		case <-deadlineC:
			if err := rescan(); err != nil {
				return err
			}
		case report := <-ended:
			if report != nil {
				report()
			}
		case report := <-checked:
			if report != nil {
				report()
			}
			if o.Recheck != nil {
				recheckC = time.After(o.RecheckEvery)
			}
		case <-recheckC:
			recheckC = nil
			start(o.Recheck, checked)
		}
	}
}

// loop is the state one serve child keeps between scans.
type loop struct {
	Options
	last    []byte // canonical bytes of the last emitted inventory
	counter int    // scan_counter of the last emitted inventory; 0 before the first
	// pending are the refresh requests received before the next scan, each
	// as what acknowledges it: RefreshComplete for one read from stdin.
	pending []func(counter int, err error)

	index  atomic.Pointer[source.Index] // what searches are answered from; replaced whole after a scan
	mu     sync.Mutex                   // held while a search is answered, and to close
	closed bool                         // Run has returned: nothing is reported any more
}

// answer takes a search request and answers it in the goroutine that read
// it, from the index in memory, so that it is answered while a scan is in
// progress: no lock is taken and no git runs. It reports whether the
// request was a search; every other request goes on to the loop.
func (l *loop) answer(r request) bool {
	if r.err != nil || r.Type != "search" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return true
	}
	switch {
	case r.RequestID == "":
		l.BadRequest("search request has no request_id", searchHint)
	case r.Query == "":
		l.BadRequest("search request has no query", searchHint)
	default:
		l.Search(r.RequestID, r.Query, l.index.Load().Search(r.Query))
	}
	return true
}

// initial is the first scan, whose failure ends serve unless serve is
// ending anyway.
func (l *loop) initial(ctx context.Context) error {
	if err := l.scan(ctx); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// scan runs one scan and emits the inventory when it changed, then one
// acknowledgement per refresh request received before the scan began. The
// initial scan always emits. Nothing is emitted once ctx is done.
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
// is indexed under Once, which reads no requests.
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

// request is one stdin line. err is set when the line is not a JSON object.
type request struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Query     string `json:"query"`
	err       error
}

// readRequests parses stdin line by line into a channel that is closed at
// EOF; a request answer takes is answered there and not sent on. Blank lines
// are skipped. Once ctx is done nothing more is delivered, but a read
// blocked on stdin cannot be interrupted: that goroutine outlives Run until
// stdin closes, which for the serve process means until it exits.
//
// At EOF it also calls end, which cancels ctx: a scan in progress is not
// waited for, since it can wait on the lock of a command that serve, once
// it ends, stops. Without that the scan waits for the command and the
// command for serve to end.
func readRequests(ctx context.Context, end context.CancelFunc, stdin io.Reader, answer func(request) bool) <-chan request {
	reqs := make(chan request)
	go func() {
		defer close(reqs)
		defer end()
		r := bufio.NewReader(stdin)
		for {
			line, err := r.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				req := parseRequest(line)
				if ctx.Err() != nil {
					return
				}
				if !answer(req) {
					select {
					case reqs <- req:
					case <-ctx.Done():
						return
					}
				}
			}
			if err != nil {
				return // EOF, or stdin is broken: either way serve is done
			}
		}
	}()
	return reqs
}

func parseRequest(line []byte) request {
	var r request
	if err := json.Unmarshal(line, &r); err != nil {
		return request{err: fmt.Errorf("request line is not a JSON object: %w", err)}
	}
	return r
}
