package home

import (
	"context"
	"errors"
	"fmt"
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
func Mutate(dir string, fn func() error) error {
	lock, err := takeLock(dir)
	if err != nil {
		return err
	}
	defer lock.Close() // closing releases the flock
	if err := fn(); err != nil {
		return err
	}
	return bumpVersion(dir)
}

// ReadLocked runs fn, a scan's local reads, while holding the shared lock of
// agentx home dir, so no mutation lands halfway through the reads. A mutation
// in progress is waited for until ctx is done; then the error is ErrLocked
// wrapping ctx's error.
func ReadLocked(ctx context.Context, dir string, fn func() error) error {
	lock, err := takeSharedLock(ctx, dir)
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

// takeLock takes the exclusive advisory lock of agentx home without waiting;
// a held lock is ErrLocked at once. It creates agentx home, ops directory
// included, on first use, since the lock file lives there.
func takeLock(dir string) (*os.File, error) {
	if err := createHome(dir); err != nil {
		return nil, err
	}
	return flock(LockPath(dir), syscall.LOCK_EX)
}

// takeSharedLock takes the shared advisory lock, retrying every 50 ms while
// a mutation holds the exclusive one, until ctx is done.
func takeSharedLock(ctx context.Context, dir string) (*os.File, error) {
	if err := createHome(dir); err != nil {
		return nil, err
	}
	for {
		f, err := flock(LockPath(dir), syscall.LOCK_SH)
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
	return os.MkdirAll(filepath.Join(dir, "ops"), 0o755)
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
		// The holder's pid lets doctor name it; it is informational, so a failed write is ignored.
		if err := f.Truncate(0); err == nil {
			f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
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

// LockHeld reports whether another command holds the lock, without waiting,
// and the pid that command wrote into the lock file.
func LockHeld(dir string) (held bool, pid string, err error) {
	f, err := takeLock(dir)
	if errors.Is(err, ErrLocked) {
		b, _ := os.ReadFile(LockPath(dir))
		return true, strings.TrimSpace(string(b)), nil
	}
	if err != nil {
		return false, "", err
	}
	f.Close()
	return false, "", nil
}
