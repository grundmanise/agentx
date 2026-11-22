package serve

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// TestStdinClosingStopsAScan closes stdin while a scan waits, as one does
// on the lock of a command serve started: Run returns at once rather than
// once the scan does, which would be once that command ends, and the
// command ends only once serve has. A request that came in during the scan,
// which the loop cannot take yet, does not keep stdin's end from being read.
func TestStdinClosingStopsAScan(t *testing.T) {
	t.Parallel()
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdinR.Close()
	scans := 0
	waiting := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{
			Scan: func(ctx context.Context) (scan.Inventory, error) {
				if scans++; scans == 1 {
					return scan.Inventory{}, nil // the initial scan
				}
				close(waiting)
				<-ctx.Done() // a lock that is never let go
				return scan.Inventory{}, ctx.Err()
			},
			Stdin:           stdinR,
			Inventory:       func(scan.Inventory) {},
			RefreshComplete: func(string, int, error) {},
			BadRequest:      func(string, string) {},
			Warn:            func(string) {},
		})
	}()
	if _, err := stdinW.WriteString(`{"type":"refresh","request_id":"r1"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("the refresh never started a scan")
	}
	if _, err := stdinW.WriteString(`{"type":"refresh","request_id":"r2"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	stdinW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return once stdin closed")
	}
}
