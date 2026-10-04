package serve

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// TestTheCheckRunsOnceAtLaunch drives Run with a check that reports at
// once: it starts once the initial snapshot is out, its report is made, and
// no other check follows it, however many refreshes serve answers after it.
func TestTheCheckRunsOnceAtLaunch(t *testing.T) {
	t.Parallel()
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinW.Close()
	defer stdinR.Close()

	var snapshots, checks atomic.Int32
	started := make(chan int32, 1) // the snapshots out when the check started
	reported := make(chan struct{}, 1)
	acked := make(chan string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Scan:            func(context.Context) (scan.Snapshot, error) { return scan.Snapshot{}, nil },
			Stdin:           stdinR,
			Snapshot:        func(scan.Snapshot) { snapshots.Add(1) },
			RefreshComplete: func(id string, _ int, _ error) { acked <- id },
			BadRequest:      func(string, string) {},
			Warn:            func(string) {},
			Check: func(context.Context) func() {
				if checks.Add(1) == 1 {
					started <- snapshots.Load()
				}
				return func() { reported <- struct{}{} }
			},
		})
	}()

	select {
	case n := <-started:
		if n == 0 {
			t.Error("the check started before the initial snapshot was out")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no check started at launch")
	}
	select {
	case <-reported:
	case <-time.After(10 * time.Second):
		t.Fatal("the check's report was never made")
	}
	for _, id := range []string{"r1", "r2"} {
		if _, err := stdinW.WriteString(`{"type":"refresh","request_id":"` + id + `"}` + "\n"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-acked:
		case <-time.After(10 * time.Second):
			t.Fatalf("refresh %s was never acknowledged", id)
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
	if n := checks.Load(); n != 1 {
		t.Errorf("%d checks ran, want the one at launch", n)
	}
}
