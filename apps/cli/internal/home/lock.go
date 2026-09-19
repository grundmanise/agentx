package home

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrLocked is returned when another agentx command holds the lock.
var ErrLocked = errors.New("another agentx command holds the lock")

// ErrServing is returned when another agentx serve runs for the same home.
var ErrServing = errors.New("another agentx serve is running for this agentx home")

func LockPath(dir string) string      { return filepath.Join(dir, "lock") }
func ServeLockPath(dir string) string { return filepath.Join(dir, "serve.lock") }

// Mutate runs fn while holding the lock of agentx home dir, then rewrites the
// version file as the change signal and releases the lock. A failing fn
// leaves the version file alone.
func Mutate(dir string, fn func() error) error { return mutate(dir, true, fn) }

// mutate takes the exclusive lock, recovers the unfinished journals of
// earlier mutations, runs fn and, with bump, rewrites the version file.
func mutate(dir string, bump bool, fn func() error) error {
	lock, err := takeLock(dir)
	if err != nil {
		return err
	}
	defer lock.Close() // closing releases the flock
	if err := recoverJournals(dir); err != nil {
		return err
	}
	if err := fn(); err != nil {
		return err
	}
	if !bump {
		return nil
	}
	return bumpVersion(dir)
}

// ReadLocked runs fn, a scan's local reads, while holding the shared lock of
// agentx home dir, so no mutation lands halfway through the reads. A mutation
// in progress is waited for until ctx is done; then the error is ErrLocked
// wrapping ctx's error.
func ReadLocked(ctx context.Context, dir string, fn func() error) error {
	lock, err := waitLock(ctx, dir, syscall.LOCK_SH)
	if err != nil {
		return err
	}
	defer lock.Close()
	return fn()
}

// TakeServeLock takes the lock one serve child holds for its lifetime; a
// second serve for the same home gets ErrServing at once. Close the file to
// release it.
func TakeServeLock(dir string) (*os.File, error) {
	if err := createHome(dir); err != nil {
		return nil, err
	}
	f, err := flock(ServeLockPath(dir), syscall.LOCK_EX)
	if errors.Is(err, ErrLocked) {
		return nil, ErrServing
	}
	return f, err
}

// takeLock takes the exclusive advisory lock of agentx home; a held lock is
// ErrLocked after a few quick retries, which cover a lock a child process
// inherited for the instant between its fork and its exec. It creates agentx
// home, mutations and ops directories included, on first use, since the
// lock file lives there.
func takeLock(dir string) (*os.File, error) {
	if err := createHome(dir); err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		f, err := flock(LockPath(dir), syscall.LOCK_EX)
		if !errors.Is(err, ErrLocked) || attempt == lockAttempts {
			return f, err
		}
		time.Sleep(lockRetry)
	}
}

// A held lock is reported after lockAttempts tries lockRetry apart.
const (
	lockAttempts = 5
	lockRetry    = 10 * time.Millisecond
)

// waitLock takes the advisory lock how (shared for a scan's reads, exclusive
// for recovery), retrying every 50 ms while it is held, until ctx is done.
func waitLock(ctx context.Context, dir string, how int) (*os.File, error) {
	if err := createHome(dir); err != nil {
		return nil, err
	}
	for {
		f, err := flock(LockPath(dir), how)
		if !errors.Is(err, ErrLocked) {
			return f, err
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %w", ErrLocked, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func createHome(dir string) error {
	for _, sub := range []string{"mutations", "ops"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// flock opens path and takes the advisory lock how on it without waiting; a
// held lock is ErrLocked.
func flock(path string, how int) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if how == syscall.LOCK_EX {
		// The holder's pid lets doctor name it; it is informational, so a
		// failed write is ignored. It is written only when it differs, so
		// that a serve child recovering a journal it cannot resolve does not
		// signal itself a change in agentx home on every attempt.
		pid := []byte(strconv.Itoa(os.Getpid()) + "\n")
		if b, err := os.ReadFile(path); err != nil || string(b) != string(pid) {
			if err := f.Truncate(0); err == nil {
				_, _ = f.WriteAt(pid, 0)
			}
		}
	}
	return f, nil
}

// bumpVersion increments the counter in the version file; a missing or
// unreadable file counts as 0.
func bumpVersion(dir string) error {
	path := filepath.Join(dir, "version")
	n := 0
	if b, err := os.ReadFile(path); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	return writeAtomic(path, []byte(strconv.Itoa(n+1)+"\n"))
}

// LockHeld reports whether another command holds the lock, and the pid that
// command wrote into the lock file. It creates nothing: a lock file that does
// not exist is free. A held lock is confirmed after the same few quick
// retries as takeLock, so a child process inheriting the lock for the instant
// between its fork and its exec is not mistaken for a holder.
func LockHeld(dir string) (held bool, pid string, err error) {
	path := LockPath(dir)
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	defer f.Close()
	for attempt := 1; ; attempt++ {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return false, "", nil // closing f releases the lock again
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return false, "", fmt.Errorf("lock %s: %w", path, err)
		}
		if attempt == lockAttempts {
			b, _ := os.ReadFile(path)
			return true, strings.TrimSpace(string(b)), nil
		}
		time.Sleep(lockRetry)
	}
}
