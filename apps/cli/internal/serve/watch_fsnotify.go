//go:build !darwin || !cgo

package serve

import (
	"fmt"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// fsnotifyBackend keeps one non-recursive watch per directory, since the
// watcher library is not recursive: on Linux one inotify watch each, on
// macOS without cgo one kqueue descriptor per directory and per file in it.
type fsnotifyBackend struct {
	w       *fsnotify.Watcher
	watched map[string]bool // real paths with a watch
}

func newBackend(sig signals) (backend, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	events, errs := w.Events, w.Errors
	go func() { // until Close closes both channels
		for events != nil || errs != nil {
			select {
			case _, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				sig.changed()
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				sig.failed(err)
			}
		}
	}()
	return &fsnotifyBackend{w: w, watched: map[string]bool{}}, nil
}

// sync adds a watch for every directory that exists now and lacks one, and
// drops the watches of directories that disappeared. A watch fsnotify has
// already dropped with its directory is dropped again without complaint.
func (b *fsnotifyBackend) sync(dirs, trees []string) error {
	want, err := roots(dirs)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, r := range want {
		seen[r] = true
	}
	for _, tree := range trees {
		real, err := filepath.EvalSymlinks(tree)
		if err != nil {
			continue // missing: not in want either
		}
		want = subdirs(real, want, seen)
	}
	for real := range b.watched {
		if !seen[real] {
			_ = b.w.Remove(real) // ErrNonExistentWatch when the directory went away first
			delete(b.watched, real)
		}
	}
	for _, real := range want {
		if b.watched[real] {
			continue
		}
		if err := b.w.Add(real); err != nil {
			return fmt.Errorf("watch %s: %w", real, err)
		}
		b.watched[real] = true
	}
	return nil
}

func (b *fsnotifyBackend) close() { _ = b.w.Close() }
