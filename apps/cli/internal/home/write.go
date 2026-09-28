package home

import (
	"io/fs"
	"os"
	"path/filepath"
)

// writeAtomic replaces path with data by temp file, fsync, rename and
// directory fsync, so a reader sees either the old content or the new,
// never a partial file, and the new file survives a crash.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = Sync(f)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = renameSynced(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// renameSynced renames from over to and fsyncs to's directory, so the
// rename is durable.
func renameSynced(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return err
	}
	return syncDir(filepath.Dir(to))
}

// SyncTree flushes every directory of a staged tree, so that the names it
// holds are as durable as the bytes in its files before the rename that
// publishes it.
func SyncTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return syncDir(path)
	})
}

// syncDir fsyncs a directory, so that a name created or removed in it
// survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return Sync(d)
}

// flushes says whether a write is flushed to the disk before it counts as
// made. Only tests turn it off, see SkipFlushesInTests.
var flushes = true

// SkipFlushesInTests stops every flush to the disk that Sync makes, for the
// rest of the process. A test stops a process at most, never the machine,
// and what a stopped process wrote stays in the page cache either way,
// while on macOS each flush drains the disk's own cache (F_FULLFSYNC), and
// the CLI's tests make tens of thousands of them. Call it before any test
// runs.
func SkipFlushesInTests() {
	flushes = false
}

// Sync flushes f to the disk, file or directory, so that what was written
// to it, or the names created or removed in it, survive a crash.
func Sync(f *os.File) error {
	if !flushes {
		return nil
	}
	return f.Sync()
}
