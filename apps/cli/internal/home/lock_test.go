package home

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
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

// TestAMutationWaitsForReadersAndNotForWriters: a lock only readers hold is
// waited for, up to a bound, and one another mutation holds is refused after
// the quick retries, however long that bound. It measures time, so it does
// not run in parallel.
func TestAMutationWaitsForReadersAndNotForWriters(t *testing.T) {
	for _, tt := range []struct {
		name    string
		how     int           // the lock the other command holds
		letGo   time.Duration // when it lets go; 0 holds it for the whole test
		readers time.Duration // how long the mutation waits for readers
		want    error
	}{
		{"a reader that lets go", syscall.LOCK_SH, 200 * time.Millisecond, readerWait, nil},
		{"a reader that outlasts the wait", syscall.LOCK_SH, 0, 200 * time.Millisecond, ErrLocked},
		{"a writer", syscall.LOCK_EX, 0, readerWait, ErrLocked},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := createHome(dir); err != nil {
				t.Fatal(err)
			}
			held, err := flock(LockPath(dir), tt.how)
			if err != nil {
				t.Fatal(err)
			}
			var once sync.Once
			release := func() { once.Do(func() { Unlock(held) }) }
			t.Cleanup(release)
			if tt.letGo > 0 {
				time.AfterFunc(tt.letGo, release)
			}

			start := time.Now()
			f, err := takeLockWithin(dir, tt.readers)
			took := time.Since(start)
			if f != nil {
				Unlock(f)
			}
			if !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Fatalf("err = %v after %s, want %v", err, took, tt.want)
			}
			if tt.how == syscall.LOCK_EX && took > readerWait/5 {
				t.Errorf("refused after %s: a writer is not waited for", took)
			}
		})
	}
}
