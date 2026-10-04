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

// TestTicksRunEachOnItsOwn drives Run with two ticks beside the update
// check: one that starts with serve and holds on, and one that waits a
// whole interval first. The held tick does not keep the second from
// running beside it, and the second does not run at start. That a tick
// never overlaps itself and reports on the loop's goroutine is
// TestATickRunsOffTheLoopAndNeverOverlaps. It measures how long the second
// tick waited, so it runs on its own.
func TestTicksRunEachOnItsOwn(t *testing.T) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinW.Close()
	defer stdinR.Close()

	var held, later atomic.Int32
	release := make(chan struct{})
	reported := make(chan string, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// report is one tick's report: a send that never blocks the loop.
	report := func(what string) func() {
		return func() {
			select {
			case reported <- what:
			default:
			}
		}
	}
	start := time.Now()
	var laterAfter atomic.Int64 // how long after start the later tick first ran
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Scan:            func(context.Context) (scan.Snapshot, error) { return scan.Snapshot{}, nil },
			Stdin:           stdinR,
			Snapshot:        func(scan.Snapshot) {},
			RefreshComplete: func(string, int, error) {},
			BadRequest:      func(string, string) {},
			Warn:            func(string) {},
			Ticks: []Tick{
				{Every: time.Hour, AtStart: true, Run: func(ctx context.Context) func() {
					held.Add(1)
					select {
					case <-release:
					case <-ctx.Done():
						return nil
					}
					return report("held")
				}},
				{Every: 20 * time.Millisecond, Run: func(context.Context) func() {
					if later.Add(1) == 1 {
						laterAfter.Store(int64(time.Since(start)))
						return report("later")
					}
					return nil
				}},
			},
		})
	}()

	// The held tick runs at start and is still holding on when the later
	// one, a whole interval on, reports.
	select {
	case got := <-reported:
		equalTick(t, "the first report", got, "later")
	case <-time.After(10 * time.Second):
		t.Fatal("the later tick never reported")
	}
	equalTick(t, "runs of the held tick so far", held.Load(), int32(1))
	if d := time.Duration(laterAfter.Load()); d < 20*time.Millisecond {
		t.Errorf("the later tick first ran %s after serve started, before its interval", d)
	}
	close(release)
	select {
	case got := <-reported:
		equalTick(t, "the next report", got, "held")
	case <-time.After(10 * time.Second):
		t.Fatal("the held tick never reported")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestATickRunsOffTheLoopAndNeverOverlaps drives Run with a tick that
// holds on until the test lets it go, on a timer far shorter than it runs.
// The first run starts once the initial snapshot is out; the ticks that
// come while it runs are skipped rather than queued, so no second run
// starts beside it; its report is made on the loop's goroutine once it
// ends, and the next tick starts the next run. Stopping serve stops a run
// still going, and Run returns only once it has.
func TestATickRunsOffTheLoopAndNeverOverlaps(t *testing.T) {
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
			Ticks: []Tick{{Every: 5 * time.Millisecond, AtStart: true, Run: func(ctx context.Context) func() {
				n := started.Add(1)
				if snapshots.Load() == 0 {
					t.Error("a run started before the initial snapshot was out")
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
			}}},
		})
	}()

	// The first run starts, and twenty ticks go by while it holds on.
	deadline := time.After(10 * time.Second)
	for started.Load() < 1 {
		select {
		case <-deadline:
			t.Fatal("no first run")
		case <-time.After(time.Millisecond):
		}
	}
	time.Sleep(100 * time.Millisecond)
	if n := started.Load(); n != 1 {
		t.Fatalf("%d runs started while the first one ran, want 1", n)
	}
	release <- struct{}{}
	select {
	case n := <-reported:
		if n != 1 {
			t.Errorf("the report of run %d came first, want the first", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the first run's report was never made")
	}
	release <- struct{}{} // the second run, which the next tick starts
	select {
	case n := <-reported:
		if n != 2 {
			t.Errorf("the report of run %d came second, want the second", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no second run")
	}

	// A third run starts and holds on until serve stops, which stops it.
	deadline = time.After(10 * time.Second)
	for started.Load() < 3 {
		select {
		case <-deadline:
			t.Fatal("no third run")
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
		t.Error("Run returned before the run it was making ended")
	}
	if n := overlapped.Load(); n != 0 {
		t.Errorf("runs overlapped %d times", n)
	}
}

// equalTick reports a value of the test that is not what it should be.
func equalTick[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}
