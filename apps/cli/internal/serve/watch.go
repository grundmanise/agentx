package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// watcher signals changes in the directories serve cares about: through
// events and errors while the filesystem watcher works, through tick once
// it does not. Reading a nil channel blocks forever, so the loop selects on
// all three.
type watcher struct {
	b      backend  // nil once watching failed
	dirs   []string // directories to watch, in order
	trees  []string // directories whose subdirectories are watched too
	ticker *time.Ticker

	events <-chan struct{}
	errors <-chan error
	tick   <-chan time.Time
}

// backend is one platform's way of watching: one fsnotify watch per
// directory everywhere, one FSEvents stream over the whole set on macOS
// built with cgo. Both deliver through signals.
type backend interface {
	// sync brings the watches in line with the directories that exist now.
	// The error is fatal: watching stops and the periodic rescan takes over.
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

// newWatcher watches dirs and every directory below each of trees. On error
// the watcher is still usable: it ticks instead.
func newWatcher(dirs, trees []string) (*watcher, error) {
	events := make(chan struct{}, 1)
	errs := make(chan error, 1)
	ws := &watcher{dirs: dirs, trees: trees, events: events, errors: errs}
	b, err := newBackend(signals{events: events, errors: errs})
	if err != nil {
		ws.fail()
		return ws, err
	}
	ws.b = b
	return ws, ws.sync()
}

// sync brings the watches in line with the directories that exist now: one
// that appeared is watched from now on, one that disappeared is dropped.
func (ws *watcher) sync() error {
	if ws.b == nil {
		return nil // watching failed earlier and was reported then
	}
	if err := ws.b.sync(ws.dirs, ws.trees); err != nil {
		ws.fail()
		return err
	}
	return nil
}

// fail closes the filesystem watcher and starts the periodic rescan.
func (ws *watcher) fail() {
	ws.close()
	ws.events, ws.errors = nil, nil
	ws.ticker = time.NewTicker(fallback)
	ws.tick = ws.ticker.C
}

func (ws *watcher) close() {
	if ws.b != nil {
		ws.b.close()
		ws.b = nil
	}
	if ws.ticker != nil {
		ws.ticker.Stop()
	}
}

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
