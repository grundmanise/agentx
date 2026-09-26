package serve

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// TestTheCheckRunsOffTheLoopAndNeverOverlaps drives Run with a check that
// holds on until the test lets it go, on a timer far shorter than it runs.
// The first check starts once the initial snapshot is out; the ticks that
// come while it runs are skipped rather than queued, so no second check
// starts beside it; its report is made on the loop's goroutine once it
// ends, and the next tick starts the next check. Stopping serve stops a
// check still running, and Run returns only once it has.
func TestTheCheckRunsOffTheLoopAndNeverOverlaps(t *testing.T) {
	t.Parallel()
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinW.Close()
	defer stdinR.Close()

	var snapshots, started, running, overlapped atomic.Int32
	release := make(chan struct{})
	reported := make(chan int32, 4)
	ended := make(chan struct{})
	var end sync.Once
	stopped := func() { end.Do(func() { close(ended) }) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Scan:  func(context.Context) (scan.Snapshot, error) { return scan.Snapshot{}, nil },
			Stdin: stdinR,
			Snapshot: func(scan.Snapshot) {
				snapshots.Add(1)
			},
			RefreshComplete: func(string, int, error) {},
			BadRequest:      func(string, string) {},
			Warn:            func(string) {},
			CheckEvery:      5 * time.Millisecond,
			Check: func(ctx context.Context) func() {
				n := started.Add(1)
				if snapshots.Load() == 0 {
					t.Error("a check started before the initial snapshot was out")
				}
				if running.Add(1) > 1 {
					overlapped.Add(1)
				}
				defer running.Add(-1)
				if n <= 2 {
					select {
					case <-release:
					case <-ctx.Done():
						stopped()
						return nil
					}
				} else {
					<-ctx.Done()
					stopped()
					return nil
				}
				return func() { reported <- n }
			},
		})
	}()

	// Twenty ticks go by while the first check holds on.
	time.Sleep(100 * time.Millisecond)
	if n := started.Load(); n != 1 {
		t.Fatalf("%d checks started while the first one ran, want 1", n)
	}
	release <- struct{}{}
	select {
	case n := <-reported:
		if n != 1 {
			t.Errorf("the report of check %d came first, want the first", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first check's report was never made")
	}
	release <- struct{}{} // the second check, which the next tick starts
	select {
	case n := <-reported:
		if n != 2 {
			t.Errorf("the report of check %d came second, want the second", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no second check")
	}

	// A third check starts and holds on until serve stops, which stops it.
	deadline := time.After(10 * time.Second)
	for started.Load() < 3 {
		select {
		case <-deadline:
			t.Fatal("no third check")
		case <-time.After(time.Millisecond):
		}
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
	select {
	case <-ended:
	default:
		t.Error("Run returned before the check it was running ended")
	}
	if n := overlapped.Load(); n != 0 {
		t.Errorf("checks overlapped %d times", n)
	}
}
