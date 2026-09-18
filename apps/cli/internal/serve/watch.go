package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watcher signals changes in the directories serve cares about: through
// events and errors while the filesystem watcher works, through tick once
// it does not. Reading a nil channel blocks forever, so the loop selects on
// all three.
type watcher struct {
	w      *fsnotify.Watcher // nil once watching failed
	todo   []string          // directories not watched yet because they do not exist
	ticker *time.Ticker

	events <-chan fsnotify.Event
	errors <-chan error
	tick   <-chan time.Time
}

// newWatcher watches dirs. On error the watcher is still usable: it ticks
// instead.
func newWatcher(dirs []string) (*watcher, error) {
	ws := &watcher{todo: dirs}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		ws.fail()
		return ws, err
	}
	ws.w, ws.events, ws.errors = w, w.Events, w.Errors
	return ws, ws.add()
}

// add watches every directory in todo that exists now, resolving symlinks so
// the real directory is watched; one that does not exist stays in todo. The
// error is fatal: watching stops and the periodic rescan takes over.
func (ws *watcher) add() error {
	if ws.w == nil {
		return nil // watching failed earlier and was reported then
	}
	var todo []string
	for _, dir := range ws.todo {
		real, err := filepath.EvalSymlinks(dir)
		if errors.Is(err, fs.ErrNotExist) {
			todo = append(todo, dir)
			continue
		}
		if err == nil {
			err = ws.w.Add(real)
		}
		if err != nil {
			ws.fail()
			return fmt.Errorf("watch %s: %w", dir, err)
		}
	}
	ws.todo = todo
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
	if ws.w != nil {
		ws.w.Close()
		ws.w = nil
	}
	if ws.ticker != nil {
		ws.ticker.Stop()
	}
}
