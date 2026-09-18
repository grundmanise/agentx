package home

import (
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

func LockPath(dir string) string { return filepath.Join(dir, "lock") }

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
// in progress is waited for briefly; one that outlasts the wait is ErrLocked.
func ReadLocked(dir string, fn func() error) error {
	lock, err := takeSharedLock(dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	return fn()
}

// takeLock takes the exclusive advisory lock of agentx home without waiting;
// a held lock is ErrLocked at once. It creates agentx home, ops directory
// included, on first use, since the lock file lives there.
func takeLock(dir string) (*os.File, error) {
	return flock(dir, syscall.LOCK_EX, 1)
}

// takeSharedLock takes the shared advisory lock, retrying for up to a second
// while a mutation holds the exclusive one.
func takeSharedLock(dir string) (*os.File, error) {
	return flock(dir, syscall.LOCK_SH, 20)
}

func flock(dir string, how, attempts int) (*os.File, error) {
	if err := os.MkdirAll(filepath.Join(dir, "ops"), 0o755); err != nil {
		return nil, err
	}
	path := LockPath(dir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	for i := 1; ; i++ {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if i == attempts {
			f.Close()
			return nil, ErrLocked
		}
		time.Sleep(50 * time.Millisecond)
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
