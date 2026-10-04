package serve

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// TestTicksRunEachOnItsOwn drives Run with two ticks beside the update
// check: one that starts with serve and holds on, and one that waits a
// whole interval first. The held tick does not keep the second from
// running beside it, and the second does not run at start. That a tick
// never overlaps itself and reports on the loop's goroutine is the update
// check's test, see TestTheCheckRunsOffTheLoopAndNeverOverlaps, since the
// check is a tick too. It measures how long the second tick waited, so it
// runs on its own.
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

// equalTick reports a value of the test that is not what it should be.
func equalTick[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}
