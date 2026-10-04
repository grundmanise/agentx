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
// leaves the version file alone. u finishes the ref steps of an unfinished
// journal of an earlier install; every command that can run git passes one.
func Mutate(dir string, u RefUpdater, fn func() error) error {
	return mutate(dir, u, quick(dir), true, fn)
}

// MutateQuiet is Mutate without the change signal, for a step that nothing
// watching agentx home needs to see because the mutation that completes the
// command follows it. It still serialises against every other mutation, so
// two commands cannot write the same file at once.
func MutateQuiet(dir string, u RefUpdater, fn func() error) error {
	return mutate(dir, u, quick(dir), false, fn)
}

// MutateQuietWaiting is MutateQuiet for a mutation that may not give up:
// one command taking back what an earlier hold of the lock already wrote.
// It waits for a held lock until ctx is done rather than reporting it held,
// because the lock is exactly what such a command needs to clean up after
// itself, and losing it is often what made the command fail in the first
// place. Everything else is MutateQuiet: the same journal recovery, and no
// change signal, since taking a change back leaves agentx home as the last
// signalled version already describes it. The update check of the serve
// child takes its hold before the network this way too, for the reason
// MutateWaiting gives.
func MutateQuietWaiting(ctx context.Context, dir string, u RefUpdater, fn func() error) error {
	return mutate(dir, u, func() (*os.File, error) { return waitLock(ctx, dir, syscall.LOCK_EX) }, false, fn)
}

// MutateWaiting is Mutate for the update check of the serve child, which
// runs in the background once at start: it waits for a held lock until ctx is
// done rather than giving up, since a command that happens to hold the lock
// at that moment is no reason to drop what the check fetched, and nobody is
// there to run it again. Waiting blocks nobody else: every other command
// still gives up on a lock it finds held rather than queueing behind this.
// A publish to a shared source finishes this way too, bounded: the source
// took its commit already, so a held lock is no reason to drop recording it.
func MutateWaiting(ctx context.Context, dir string, u RefUpdater, fn func() error) error {
	return mutate(dir, u, func() (*os.File, error) { return waitLock(ctx, dir, syscall.LOCK_EX) }, true, fn)
}

// acquire takes the exclusive lock of agentx home, either way a mutation
// can ask for it: without waiting, or waiting for the holder.
type acquire func() (*os.File, error)

func quick(dir string) acquire { return func() (*os.File, error) { return takeLock(dir) } }

// Pruner removes what an interrupted command left outside every journal,
// once the journals are finished: the pending merges of agentx home that
// setting up stopped part way through, say. A command whose RefUpdater
// implements it has it run under the lock before its own change.
type Pruner interface {
	Prune() error
}

// mutate takes the exclusive lock the way take asks for it, recovers the
// unfinished journals of earlier mutations, prunes what u prunes, runs fn
// and, with bump, rewrites the version file.
func mutate(dir string, u RefUpdater, take acquire, bump bool, fn func() error) error {
	lock, err := take()
	if err != nil {
		return err
	}
	defer Unlock(lock)
	if err := recoverJournals(dir, u); err != nil {
		return err
	}
	if p, ok := u.(Pruner); ok {
		if err := p.Prune(); err != nil {
			return err
		}
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
	defer Unlock(lock)
	return fn()
}

// TakeServeLock takes the lock one serve child holds for its lifetime; a
// second serve for the same home gets ErrServing at once. Unlock releases it.
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
// ErrLocked after a few quick retries. A holder that finishes frees the lock
// at once through Unlock; the retries cover one killed while a child process
// it had forked was not yet exec'd, which keeps the lock until its exec. It
// creates agentx home, mutations and ops directories included, on first use,
// since the lock file lives there.
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

// waitLock takes the advisory lock how (shared for a scan's reads,
// exclusive for recovery and for a take-back), retrying every 50 ms while
// it is held, until ctx is done.
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

// Unlock releases the advisory lock f holds and closes f. Closing alone is
// not enough: a child process another goroutine has forked but not yet
// exec'd shares f's open file description, and with it the lock, which would
// stay held until that child execs, long enough under load for the next
// command to find it held. Releasing through f frees it for every copy.
func Unlock(f *os.File) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) // closing f still releases it if this fails
	f.Close()
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

// BumpVersion rewrites the version file of agentx home dir, the change
// signal every mutation ends with, for a hold of the lock taken without it
// that turned out to change something. Call it under the exclusive lock.
func BumpVersion(dir string) error { return bumpVersion(dir) }

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
// retries as takeLock, so the instant a killed holder's child keeps the lock
// is not mistaken for a holder.
func LockHeld(dir string) (held bool, pid string, err error) {
	path := LockPath(dir)
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	defer Unlock(f)
	for attempt := 1; ; attempt++ {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return false, "", nil // Unlock releases the lock again
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
