//go:build darwin && cgo

package serve

import (
	"path/filepath"
	"slices"

	"github.com/fsnotify/fsevents"
)

// fseventsBackend watches through FSEvents, which is recursive and costs no
// descriptor per file, unlike kqueue: one stream over every root that exists
// now, restarted when that set changes. The stream reports the directory a
// change happened in; accept keeps the same directories the fsnotify
// backend would watch.
type fseventsBackend struct {
	sig   signals
	flat  []string // real paths of the directories watched now
	trees []string // real paths of the directories watched with everything below now
	es    *fsevents.EventStream
	done  chan struct{} // closed to end the goroutine reading es.Events
}

func newBackend(sig signals) (backend, error) { return &fseventsBackend{sig: sig}, nil }

func (b *fseventsBackend) sync(dirs, trees []string) error {
	flat, err := roots(dirs)
	if err != nil {
		return err
	}
	treeRoots, err := roots(trees)
	if err != nil {
		return err
	}
	// FSEvents does not follow symlinks, so a directory a symlink below a
	// tree leads out of it is a root of its own, as the fsnotify backend
	// watches it.
	treeRoots = extend(flat, treeRoots)
	if b.es != nil && slices.Equal(flat, b.flat) && slices.Equal(treeRoots, b.trees) {
		return nil
	}
	b.close()
	b.flat, b.trees = flat, treeRoots
	paths := slices.Clone(flat)
	for _, t := range treeRoots {
		if !slices.Contains(paths, t) {
			paths = append(paths, t)
		}
	}
	if len(paths) == 0 {
		return nil // nothing exists yet; the next sync looks again
	}
	es := &fsevents.EventStream{
		Events: make(chan []fsevents.Event, 64),
		Paths:  paths,
		Flags:  fsevents.NoDefer | fsevents.WatchRoot,
	}
	if err := es.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case events := <-es.Events:
				for _, e := range events {
					if e.Flags&(fsevents.MustScanSubDirs|fsevents.RootChanged) != 0 || accept(filepath.Clean(e.Path), flat, treeRoots) {
						b.sig.changed()
						break
					}
				}
			}
		}
	}()
	b.es, b.done = es, done
	return nil
}

// close stops the stream before it ends the goroutine, so a callback still
// delivering a batch finds a reader; a callback after Stop finds the stream
// gone from the package's registry and delivers nothing.
func (b *fseventsBackend) close() {
	if b.es != nil {
		b.es.Stop()
		close(b.done)
		b.es, b.done = nil, nil
	}
}
