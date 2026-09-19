//go:build !darwin || !cgo

package serve

import (
	"fmt"
	"io/fs"
	"os"
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
	go func() { // until Close closes both channels
		for w.Events != nil || w.Errors != nil {
			select {
			case _, ok := <-w.Events:
				if !ok {
					w.Events = nil
					continue
				}
				sig.changed()
			case err, ok := <-w.Errors:
				if !ok {
					w.Errors = nil
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

// subdirs appends the real path of every directory below dir, following
// symlinks, to want, skipping hidden directories and node_modules as
// discovery does; seen keeps each real path once, which also ends a symlink
// loop. A directory that cannot be read or resolved is left out: the scan
// reports it.
func subdirs(dir string, want []string, seen map[string]bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return want
	}
	for _, e := range entries {
		name := e.Name()
		if skipped(name) {
			continue
		}
		if !e.IsDir() && e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(dir, name))
		if err != nil || seen[real] {
			continue
		}
		if info, err := os.Stat(real); err != nil || !info.IsDir() {
			continue
		}
		seen[real] = true
		want = append(want, real)
		want = subdirs(real, want, seen)
	}
	return want
}
