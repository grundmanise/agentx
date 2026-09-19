package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watcher signals changes in the directories serve cares about: through
// events and errors while the filesystem watcher works, through tick once
// it does not. Reading a nil channel blocks forever, so the loop selects on
// all three.
type watcher struct {
	w       *fsnotify.Watcher // nil once watching failed
	dirs    []string          // directories to watch, in order
	trees   []string          // directories whose subdirectories are watched too
	watched map[string]bool   // real paths with a watch
	ticker  *time.Ticker

	events <-chan fsnotify.Event
	errors <-chan error
	tick   <-chan time.Time
}

// newWatcher watches dirs and every directory below each of trees. On error
// the watcher is still usable: it ticks instead.
func newWatcher(dirs, trees []string) (*watcher, error) {
	ws := &watcher{dirs: dirs, trees: trees, watched: map[string]bool{}}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		ws.fail()
		return ws, err
	}
	ws.w, ws.events, ws.errors = w, w.Events, w.Errors
	return ws, ws.sync()
}

// sync brings the watches in line with the directories that exist now: one
// watch per real path after resolving symlinks, added for a directory that
// appeared and dropped for one that disappeared. A watch fsnotify has
// already dropped with its directory is dropped again without complaint.
// The error is fatal: watching stops and the periodic rescan takes over.
func (ws *watcher) sync() error {
	if ws.w == nil {
		return nil // watching failed earlier and was reported then
	}
	var want []string
	seen := map[string]bool{}
	for _, dir := range ws.dirs {
		real, err := filepath.EvalSymlinks(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			ws.fail()
			return fmt.Errorf("watch %s: %w", dir, err)
		}
		if !seen[real] {
			seen[real] = true
			want = append(want, real)
		}
	}
	for _, tree := range ws.trees {
		want = subdirs(tree, want, seen)
	}
	for real := range ws.watched {
		if !seen[real] {
			_ = ws.w.Remove(real) // ErrNonExistentWatch when the directory went away first
			delete(ws.watched, real)
		}
	}
	for _, real := range want {
		if ws.watched[real] {
			continue
		}
		if err := ws.w.Add(real); err != nil {
			ws.fail()
			return fmt.Errorf("watch %s: %w", real, err)
		}
		ws.watched[real] = true
	}
	return nil
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
		if strings.HasPrefix(name, ".") || name == "node_modules" {
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

// fail closes the filesystem watcher and starts the periodic rescan.
func (ws *watcher) fail() {
	ws.close()
	ws.events, ws.errors = nil, nil
	ws.ticker = time.NewTicker(fallback)
	ws.tick = ws.ticker.C
}

func (ws *watcher) close() {
	if ws.w != nil {
		ws.w.Close()
		ws.w = nil
	}
	if ws.ticker != nil {
		ws.ticker.Stop()
	}
}
