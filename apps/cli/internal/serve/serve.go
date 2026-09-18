// Package serve is the long-running child the desktop app holds open: it
// scans on start, rescans when the machine changes, and answers requests
// read from stdin.
package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

const (
	debounce = 100 * time.Millisecond // quiet time after the last change before a rescan
	maxWait  = 500 * time.Millisecond // a rescan happens this long after the first change at the latest
	fallback = 2 * time.Second        // rescan period when watching is impossible
)

// Options wires one serve loop to the command that runs it: how to scan and
// how to report. Every report function is called from the loop's goroutine.
type Options struct {
	Scan       func(ctx context.Context) (scan.Snapshot, error) // one whole scan; ctx bounds its wait for the lock
	Watch      []string                                         // directories to watch, in order; a missing one is retried after each scan
	Once       bool                                             // scan once, emit and return
	Stdin      io.Reader                                        // request lines
	InstanceID string

	Snapshot        func(scan.Snapshot)                            // a changed whole snapshot, counter set
	RefreshComplete func(requestID string, counter int, err error) // the acknowledgement of one refresh request
	BadRequest      func(message, hint string)                     // a stdin line that is not a request
	Warn            func(message string)
}

const requestHint = `send one JSON object per line, such as {"type":"refresh","request_id":"<unique id>"}`

// Run serves until ctx is done, stdin closes or, under Once, the initial
// snapshot is emitted. It returns an error only when the initial scan fails;
// a cancelled ctx is a clean end.
func Run(ctx context.Context, o Options) error {
	ctx, cancel := context.WithCancel(ctx) // ends the stdin reader when Run returns for another reason
	defer cancel()
	l := &loop{Options: o}
	if o.Once {
		return l.initial(ctx)
	}

	warnWatch := func(err error) {
		if err != nil {
			o.Warn(fmt.Sprintf("cannot watch for changes, rescanning every %s: %v", fallback, err))
		}
	}
	w, err := newWatcher(o.Watch) // before the initial scan, so a change during it is not missed
	warnWatch(err)
	defer w.close()
	if err := l.initial(ctx); err != nil {
		return err
	}
	reqs := readRequests(ctx, o.Stdin)

	// A change starts the debounce timer and, unless one is running, the
	// deadline timer; whichever fires first triggers the rescan.
	var debounceC, deadlineC <-chan time.Time
	schedule := func() {
		if deadlineC == nil {
			deadlineC = time.After(maxWait)
		}
		debounceC = time.After(debounce)
	}
	rescan := func() {
		debounceC, deadlineC = nil, nil
		if err := l.scan(ctx); err != nil && ctx.Err() == nil {
			o.Warn("scan failed: " + err.Error())
		}
		warnWatch(w.add())
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
				o.BadRequest(fmt.Sprintf("unknown request type %q", r.Type), "request types: refresh")
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
		case <-w.tick:
			rescan()
		case <-debounceC:
			rescan()
		case <-deadlineC:
			rescan()
		}
	}
}

// loop is the state one serve child keeps between scans.
type loop struct {
	Options
	last    []byte   // canonical bytes of the last emitted snapshot
	counter int      // scan_counter of the last emitted snapshot; 0 before the first
	pending []string // refresh request ids received before the next scan
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
		snap.InstanceID = l.InstanceID
		snap.ScanCounter = l.counter
		l.Snapshot(snap)
	}
	for _, id := range pending {
		l.RefreshComplete(id, l.counter, nil)
	}
	return nil
}

// canonical serialises a snapshot without the fields that differ between
// identical inventories: the instance id and the counter.
func canonical(snap scan.Snapshot) []byte {
	snap.InstanceID = ""
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
	err       error
}

// readRequests parses stdin line by line into a channel that is closed at
// EOF. Blank lines are skipped. The reader stops when ctx is done.
func readRequests(ctx context.Context, stdin io.Reader) <-chan request {
	reqs := make(chan request)
	go func() {
		defer close(reqs)
		r := bufio.NewReader(stdin)
		for {
			line, err := r.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				select {
				case reqs <- parseRequest(line):
				case <-ctx.Done():
					return
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
