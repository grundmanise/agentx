package home

import (
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
		err = f.Sync()
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
	d, err := os.Open(filepath.Dir(to))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
