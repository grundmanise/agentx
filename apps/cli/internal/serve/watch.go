package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// watcher signals changes in the directories serve cares about, through
// events and errors. Watching is a precondition of serve: when it cannot be
// set up, or a directory cannot be watched later, the error ends serve.
type watcher struct {
	b     backend
	dirs  []string // directories to watch, in order
	trees []string // directories whose subdirectories are watched too

	events <-chan struct{}
	errors <-chan error
}

// backend is one platform's way of watching: one fsnotify watch per
// directory everywhere, one FSEvents stream over the whole set on macOS
// built with cgo. Both deliver through signals.
type backend interface {
	// sync brings the watches in line with the directories that exist now.
	sync(dirs, trees []string) error
	close()
}

// signals is where a backend delivers: at most one pending change signal
// and one pending error, since the loop coalesces changes anyway and warns
// once per error it reads.
type signals struct {
	events chan<- struct{}
	errors chan<- error
}

func (s signals) changed() {
	select {
	case s.events <- struct{}{}:
	default:
	}
}

func (s signals) failed(err error) {
	select {
	case s.errors <- err:
	default:
	}
}

// newWatcher watches dirs and every directory below each of trees.
func newWatcher(dirs, trees []string) (*watcher, error) {
	events := make(chan struct{}, 1)
	errs := make(chan error, 1)
	b, err := newBackend(signals{events: events, errors: errs})
	if err != nil {
		return nil, err
	}
	ws := &watcher{b: b, dirs: dirs, trees: trees, events: events, errors: errs}
	if err := ws.sync(); err != nil {
		ws.close()
		return nil, err
	}
	return ws, nil
}

// sync brings the watches in line with the directories that exist now: one
// that appeared is watched from now on, one that disappeared is dropped.
func (ws *watcher) sync() error { return ws.b.sync(ws.dirs, ws.trees) }

func (ws *watcher) close() { ws.b.close() }

// roots resolves dirs to the real paths that exist now, in order and each
// once; a directory that does not exist is left out and tried again on the
// next sync. Any other failure to resolve one is an error.
func roots(dirs []string) ([]string, error) {
	var real []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		r, err := filepath.EvalSymlinks(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("watch %s: %w", dir, err)
		}
		if !seen[r] {
			seen[r] = true
			real = append(real, r)
		}
	}
	return real, nil
}

// skipped reports whether a directory entry is left out of a tree, as
// discovery leaves it out: hidden entries and node_modules.
func skipped(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

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

// extend appends to trees the directories below them that a recursive watch
// over them misses because it does not follow symlinks: every real path
// subdirs reaches that accept does not already cover, so one reached through
// a symlink out of every tree or through a skipped directory, in order and
// each once. A subdirectory of one is below it and left out. flat and trees
// are real paths; the result is a fresh slice.
func extend(flat, trees []string) []string {
	seen := map[string]bool{}
	for _, r := range flat {
		seen[r] = true
	}
	for _, t := range trees {
		seen[t] = true
	}
	all := slices.Clone(trees)
	for _, t := range trees {
		for _, dir := range subdirs(t, nil, seen) {
			if !accept(dir, flat, all) {
				all = append(all, dir)
			}
		}
	}
	return all
}

// accept reports whether a change in the directory dir is a change signal:
// dir is one of the flat directories, or one of the trees, or below a tree
// without a skipped directory on the way. flat and trees are real paths.
func accept(dir string, flat, trees []string) bool {
	for _, f := range flat {
		if dir == f {
			return true
		}
	}
	for _, t := range trees {
		rel, err := filepath.Rel(t, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			continue
		}
		if rel == "." {
			return true
		}
		ok := true
		for _, name := range strings.Split(rel, string(filepath.Separator)) {
			if skipped(name) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
