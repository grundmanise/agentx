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
	flat  []string // real paths of the flat directories watched now
	trees []string // real paths of the trees watched now
	es    *fsevents.EventStream
	done  chan struct{} // closed to end the goroutine reading es.Events
}

func newBackend(sig signals) (backend, error) { return &fseventsBackend{sig: sig}, nil }

func (b *fseventsBackend) sync(dirs, trees []string) error {
	var flatDirs []string
	for _, dir := range dirs {
		if !slices.Contains(trees, dir) {
			flatDirs = append(flatDirs, dir)
		}
	}
	flat, err := roots(flatDirs)
	if err != nil {
		return err
	}
	treeRoots, err := roots(trees)
	if err != nil {
		return err
	}
	if b.es != nil && slices.Equal(flat, b.flat) && slices.Equal(treeRoots, b.trees) {
		return nil
	}
	b.close()
	b.flat, b.trees = flat, treeRoots
	paths := append(slices.Clone(flat), treeRoots...)
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

func (b *fseventsBackend) close() {
	if b.es != nil {
		b.es.Stop()
		close(b.done)
		b.es, b.done = nil, nil
	}
}
