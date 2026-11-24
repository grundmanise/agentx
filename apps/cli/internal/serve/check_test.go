package serve

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// TestTheChecksRunOneAtATime drives Run with a check that reports at once
// and a recheck repeated every few milliseconds: the check starts once the
// initial inventory is out, its report is made, and it never runs again,
// however many refreshes serve answers after it; the recheck starts only
// once the check's report is made, and runs again and again, never beside
// itself or the check. The quiet job, due a millisecond after each scan,
// runs between them and never beside one. A second job holds on until serve
// stops, which stops it, and Run returns only once it has.
func TestTheChecksRunOneAtATime(t *testing.T) {
	t.Parallel()
	requests := NewRequests()
	var inventories, checks, rechecks, quiets, running atomic.Int32
	var overlapped, early atomic.Bool // a check ran beside another; the recheck ran before the check's report
	var checkReported atomic.Bool
	var held atomic.Bool           // the second job, still running when serve stops, ended
	started := make(chan int32, 1) // the inventories out when the check started
	reported := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Scan:      func(context.Context) (scan.Inventory, error) { return scan.Inventory{}, nil },
			Requests:  requests,
			Inventory: func(scan.Inventory) { inventories.Add(1) },
			Fail:      func(err error) { t.Errorf("Fail(%v)", err) },
			Warn:      func(string) {},
			Check: func(context.Context) func() {
				if running.Add(1) > 1 {
					overlapped.Store(true)
				}
				defer running.Add(-1)
				if checks.Add(1) == 1 {
					started <- inventories.Load()
				}
				return func() {
					checkReported.Store(true)
					reported <- struct{}{}
				}
			},
			Recheck: func(context.Context) func() {
				if running.Add(1) > 1 {
					overlapped.Store(true)
				}
				defer running.Add(-1)
				if !checkReported.Load() {
					early.Store(true)
				}
				time.Sleep(time.Millisecond) // long enough for a recheck started beside it to show
				rechecks.Add(1)
				return nil
			},
			RecheckEvery: time.Millisecond,
			Quiet: func(context.Context) func() {
				if running.Add(1) > 1 {
					overlapped.Store(true)
				}
				defer running.Add(-1)
				time.Sleep(time.Millisecond)
				quiets.Add(1)
				return nil
			},
			QuietAfter: time.Millisecond,
			Jobs: []Job{func(ctx context.Context) func() {
				<-ctx.Done()
				held.Store(true)
				return nil
			}},
		})
	}()

	select {
	case n := <-started:
		if n == 0 {
			t.Error("the check started before the initial inventory was out")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no check started at launch")
	}
	select {
	case <-reported:
	case <-time.After(10 * time.Second):
		t.Fatal("the check's report was never made")
	}
	for range 2 {
		refreshCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		_, err := requests.Refresh(refreshCtx)
		stop()
		if err != nil {
			t.Fatalf("a refresh was answered %v", err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for rechecks.Load() < 3 || quiets.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("%d rechecks and %d runs of the quiet job within 10s, want at least 3 and 1", rechecks.Load(), quiets.Load())
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
	if !held.Load() {
		t.Error("Run returned before the job it was running ended")
	}
	if n := checks.Load(); n != 1 {
		t.Errorf("%d checks ran, want the one at launch", n)
	}
	if overlapped.Load() {
		t.Error("two of the checks and the quiet job ran at once")
	}
	if early.Load() {
		t.Error("the recheck ran before the check's report was made")
	}
}

// TestTheQuietJobWaitsForTheScansToStop drives Run with a quiet job due
// 150 ms after a scan. A scan a refresh asks for within that time starts
// the wait again: the job starts no sooner than 150 ms after that scan
// ended. A scan that ends while the job runs leads to another run, which
// starts only once the first has ended.
func TestTheQuietJobWaitsForTheScansToStop(t *testing.T) {
	t.Parallel()
	const quiet = 150 * time.Millisecond
	var scanned atomic.Int64 // when the last scan ended, in nanoseconds
	var running atomic.Int32
	var overlapped atomic.Bool
	runs := make(chan time.Time, 8) // when each run of the quiet job started
	release := make(chan struct{})
	r := start(t, Options{
		Scan: func(context.Context) (scan.Inventory, error) {
			defer func() { scanned.Store(time.Now().UnixNano()) }()
			return scan.Inventory{}, nil
		},
		Quiet: func(ctx context.Context) func() {
			if running.Add(1) > 1 {
				overlapped.Store(true)
			}
			defer running.Add(-1)
			runs <- time.Now()
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		},
		QuietAfter: quiet,
	})
	next := func() time.Time {
		t.Helper()
		select {
		case at := <-runs:
			return at
		case <-time.After(10 * time.Second):
			t.Fatal("the quiet job did not run")
			return time.Time{}
		}
	}
	if _, err := r.refresh(); err != nil {
		t.Fatal(err)
	}
	last := time.Unix(0, scanned.Load())
	started := next()
	for !started.After(last) { // on a machine slow enough for a run between the two scans
		release <- struct{}{}
		started = next()
	}
	if waited := started.Sub(last); waited < quiet {
		t.Errorf("the quiet job started %s after the last scan, want %s or more", waited, quiet)
	}

	if _, err := r.refresh(); err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	next()
	if overlapped.Load() {
		t.Error("the quiet job ran beside itself")
	}
}
