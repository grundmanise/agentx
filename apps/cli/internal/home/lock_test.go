package home

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
)

// shareLock stands in for a child process another goroutine has forked but
// not yet exec'd: such a child holds a copy of every descriptor, so it
// shares the open file description, and with it the flock, of the lock file
// at path. shareLock duplicates each descriptor open on that file, which
// shares it the same way, and closes the copies when the test ends.
func shareLock(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	lockFile := info.Sys().(*syscall.Stat_t)
	// Find them all before duplicating any: a copy takes the lowest free
	// descriptor, above the one it copies, and a scan that met its own
	// copies would copy them again, up to the end of the range, crowding a
	// parallel test's lock out of it.
	var open []int
	for fd := 0; fd < 1024; fd++ {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil && st.Dev == lockFile.Dev && st.Ino == lockFile.Ino {
			open = append(open, fd)
		}
	}
	shared := 0
	for _, fd := range open {
		// Close-on-exec, so that no process a parallel test starts keeps
		// the copy past its own exec.
		syscall.ForkLock.RLock()
		dup, err := syscall.Dup(fd)
		if err == nil {
			syscall.CloseOnExec(dup)
		}
		syscall.ForkLock.RUnlock()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { syscall.Close(dup) })
		shared++
	}
	if shared == 0 {
		t.Fatalf("no descriptor is open on %s", path)
	}
}

// TestTheLockIsFreeOnceItsHolderFinishes checks that whatever holds the
// lock of agentx home frees it as it finishes, even while a child process
// forked in the meantime still shares the lock's descriptor, so the next
// command neither finds it held nor waits for that child to exec.
func TestTheLockIsFreeOnceItsHolderFinishes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	holders := map[string]func(dir string, fn func() error) error{
		"a mutation":       func(dir string, fn func() error) error { return Mutate(dir, nil, fn) },
		"a quiet mutation": func(dir string, fn func() error) error { return MutateQuiet(dir, nil, fn) },
		"a take-back":      func(dir string, fn func() error) error { return MutateQuietWaiting(ctx, dir, nil, fn) },
		"a scan's reads":   func(dir string, fn func() error) error { return ReadLocked(ctx, dir, fn) },
	}
	for name, hold := range holders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := hold(dir, func() error { shareLock(t, LockPath(dir)); return nil }); err != nil {
				t.Fatal(err)
			}
			lock, err := takeLock(dir)
			if err != nil {
				t.Fatalf("the next command: %v", err)
			}
			Unlock(lock)
			if held, _, err := LockHeld(dir); err != nil || held {
				t.Errorf("doctor reports the lock held: %v, %v", held, err)
			}
		})
	}
	t.Run("a serve", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		lock, err := TakeServeLock(dir)
		if err != nil {
			t.Fatal(err)
		}
		shareLock(t, ServeLockPath(dir))
		Unlock(lock)
		next, err := TakeServeLock(dir)
		if errors.Is(err, ErrServing) {
			t.Fatal("the next serve finds the first one still running")
		}
		if err != nil {
			t.Fatal(err)
		}
		Unlock(next)
	})
}
