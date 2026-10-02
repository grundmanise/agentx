package gitx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMaintenanceLeavesNoLockBehind: a maintenance git stopped part way is
// sent SIGTERM, on which git removes its lock files, never SIGKILL; one
// whose maintenance lock is taken is not run, since git would skip its work
// and exit 0; and the locks left are those older than the time asked.
func TestMaintenanceLeavesNoLockBehind(t *testing.T) {
	t.Parallel()
	dir, gitDir := t.TempDir(), t.TempDir()
	started, stopped := filepath.Join(dir, "started"), filepath.Join(dir, "stopped")
	script := "#!/bin/sh\ntrap 'touch " + stopped + "; exit 143' TERM\ntouch " + started + "\nwhile :; do sleep 0.05; done\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(map[string]string{"PATH": dir + string(os.PathListSeparator) + os.Getenv("PATH")}, false, func(string, ...any) {})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	go func() {
		for {
			if _, err := os.Stat(started); err == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	if err := r.PackObjects(ctx, gitDir); err == nil {
		t.Fatal("a stopped maintenance answered no error")
	}
	if _, err := os.Stat(stopped); err != nil {
		t.Error("the maintenance git was not stopped with SIGTERM")
	}

	lock := filepath.Join(gitDir, "objects", "maintenance.lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(started); err != nil {
		t.Fatal(err)
	}
	if err := r.PackRefs(context.Background(), gitDir); !errors.Is(err, ErrMaintenanceRunning) {
		t.Errorf("err = %v with the maintenance lock taken, want ErrMaintenanceRunning", err)
	}
	if _, err := os.Stat(started); err == nil {
		t.Error("git ran with the maintenance lock taken")
	}

	if left := LeftLocks(gitDir, time.Now().Add(-time.Hour)); len(left) != 0 {
		t.Errorf("a fresh lock is left behind: %v", left)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if left := LeftLocks(gitDir, time.Now().Add(-time.Hour)); len(left) != 1 || left[0] != lock {
		t.Errorf("left = %v, want %s", left, lock)
	}
}
