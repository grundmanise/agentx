package serve

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/crash"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// TestTheChecksRunOneAtATime drives Run with a check that reports at once
// and a recheck repeated every few milliseconds: the check starts once the
// initial inventory is out, its report is made, and it never runs again,
// however many refreshes serve answers after it; the recheck starts only
// once the check's report is made, and runs again and again, never beside
// itself or the check. A second job holds on until serve stops, which stops
// it, and Run returns only once it has.
func TestTheChecksRunOneAtATime(t *testing.T) {
	t.Parallel()
	requests := NewRequests()
	var inventories, checks, rechecks, running atomic.Int32
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
	for rechecks.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("%d rechecks within 10s, want at least 3", rechecks.Load())
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
		t.Error("two checks ran at once")
	}
	if early.Load() {
		t.Error("the recheck ran before the check's report was made")
	}
}

// TestAPanicInAJobEndsServe drives Run with a job that panics: the panic
// ends Run, which returns it as its error instead of the process ending.
func TestAPanicInAJobEndsServe(t *testing.T) {
	t.Parallel()
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{
			Scan:      func(context.Context) (scan.Inventory, error) { return scan.Inventory{}, nil },
			Inventory: func(scan.Inventory) {},
			Fail:      func(err error) { t.Errorf("Fail(%v)", err) },
			Warn:      func(string) {},
			Jobs:      []Job{func(context.Context) func() { panic("job failed") }},
		})
	}()
	select {
	case err := <-done:
		var p *crash.Panic
		if !errors.As(err, &p) || p.Value != "job failed" {
			t.Errorf("Run returned %v, want the job's panic", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
}
