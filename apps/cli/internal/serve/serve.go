// Package serve is the long-running child the desktop app holds open: it
// scans on start, rescans when the machine changes, and answers requests
// read from stdin.
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
// has returned. Every other report function is called from the loop's
// goroutine.
type Options struct {
	Scan  func(ctx context.Context) (scan.Snapshot, error)                               // one whole scan; ctx bounds its wait for the lock
	Index func(ctx context.Context, prev *source.Index) (*source.Index, []string, error) // the source index after a scan, prev being the last one built; nil indexes nothing
	Watch []string                                                                       // directories to watch, in order; a missing one is retried before each rescan
	Trees []string                                                                       // directories whose subdirectories, present or added later, are watched too
	Once  bool                                                                           // scan once, emit and return
	Stdin io.Reader                                                                      // request lines

	Snapshot        func(scan.Snapshot)                                   // a changed whole snapshot, counter set
	RefreshComplete func(requestID string, counter int, err error)        // the acknowledgement of one refresh request
	Search          func(requestID, query string, results []source.Match) // the answer to one search request
	BadRequest      func(message, hint string)                            // a stdin line that is not a request
	Warn            func(message string)
}

const (
	requestHint = `send one JSON object per line, such as {"type":"refresh","request_id":"<unique id>"}`
	searchHint  = `send {"type":"search","request_id":"<unique id>","query":"<text>"}`
	typesHint   = "request types: refresh, search"
)

// Run serves until ctx is done, stdin closes or, under Once, the initial
// snapshot is emitted. It returns an error when the initial scan fails or
// when watching fails, at start or later, wrapped in ErrWatch; a cancelled
// ctx is a clean end.
func Run(ctx context.Context, o Options) error {
	ctx, cancel := context.WithCancel(ctx) // ends the stdin reader when Run returns for another reason
	l := &loop{Options: o}
	defer func() {
		cancel()
		// A search being answered in the reader's goroutine finishes first,
		// so that nothing is reported after Run returned.
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
	reqs := readRequests(ctx, o.Stdin, l.answer)

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
				l.pending = append(l.pending, r.RequestID)
				schedule()
			}
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
		}
	}
}

// loop is the state one serve child keeps between scans.
type loop struct {
	Options
	last    []byte   // canonical bytes of the last emitted snapshot
	counter int      // scan_counter of the last emitted snapshot; 0 before the first
	pending []string // refresh request ids received before the next scan

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

// scan runs one scan and emits the snapshot when it changed, then one
// acknowledgement per refresh request received before the scan began. The
// initial scan always emits. Nothing is emitted once ctx is done.
func (l *loop) scan(ctx context.Context) error {
	pending := l.pending
	l.pending = nil
	snap, err := l.Scan(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		for _, id := range pending {
			l.RefreshComplete(id, 0, err)
		}
		return err
	}
	b := canonical(snap)
	if l.counter == 0 || !bytes.Equal(b, l.last) {
		l.counter++
		l.last = b
		snap.ScanCounter = l.counter
		l.Snapshot(snap)
	}
	l.reindex(ctx)
	for _, id := range pending {
		l.RefreshComplete(id, l.counter, nil)
	}
	return nil
}

// reindex rebuilds the source index after a scan has emitted its snapshot
// and before that scan's acknowledgements go out, so that a refresh
// acknowledged after a source was added, fetched again or removed is
// answered from the new index, while the snapshot never waits for the lock
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

// canonical serialises a snapshot without the counter, the one field that
// differs between identical inventories of one serve process.
func canonical(snap scan.Snapshot) []byte {
	snap.ScanCounter = 0
	b, err := json.Marshal(snap)
	if err != nil {
		panic(err) // a snapshot is plain structs; marshalling cannot fail
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
func readRequests(ctx context.Context, stdin io.Reader, answer func(request) bool) <-chan request {
	reqs := make(chan request)
	go func() {
		defer close(reqs)
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
