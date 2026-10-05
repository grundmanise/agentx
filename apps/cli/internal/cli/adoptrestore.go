package cli

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// leftOutByInstaller reports whether the vercel skills CLI leaves the file
// at p, a path inside a skill directory, out of the copy it installs. It
// judges every name on the way down, a directory's as much as the file's,
// as that tool's copy does. No version copies metadata.json, none before
// 1.4.1 copied README.md, and none before 1.4.5 copied a name starting
// with _. The lock file does not say which version installed a skill, so
// a file any of them leaves out counts.
func leftOutByInstaller(p string) bool {
	for _, name := range strings.Split(p, "/") {
		if name == "README.md" || name == "metadata.json" || strings.HasPrefix(name, "_") {
			return true
		}
	}
	return false
}

// installerLeftOut are the files of the version v that the installer left
// out of dir, the library directory it installed v into: those it never
// copies and dir does not hold. A file the directory holds, edited or not,
// is the user's, and so is a path under a file or a symlink the directory
// holds where the version has a directory.
func installerLeftOut(dir string, v *imported) []treeFile {
	var left []treeFile
	for _, f := range v.files {
		if leftOutByInstaller(f.path) && absentUnder(dir, f.path) {
			left = append(left, f)
		}
	}
	return left
}

// absentUnder reports whether the file p is missing from dir, with every
// directory on the way to it that is there a real directory, so that
// writing it would write inside dir and replace nothing.
func absentUnder(dir, p string) bool {
	names := strings.Split(p, "/")
	at := dir
	for i, name := range names {
		at = filepath.Join(at, name)
		info, err := os.Lstat(at)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return true
		case err != nil, i == len(names)-1, !info.IsDir():
			return false
		}
	}
	return false
}

// paths are the paths of files, sorted, for a message and an event.
func paths(files []treeFile) []string {
	ps := make([]string, 0, len(files))
	for _, f := range files {
		ps = append(ps, f.path)
	}
	sort.Strings(ps)
	return ps
}

// hashWithout is the content hash of v with files left out: what a library
// directory holds that the installer copied v into, and nobody edited.
func (v *imported) hashWithout(files []treeFile) string {
	gone := map[string]bool{}
	for _, f := range files {
		gone[f.path] = true
	}
	var kept []treeFile
	for _, f := range v.files {
		if !gone[f.path] {
			kept = append(kept, f)
		}
	}
	return contentHashOf(kept)
}

// stageRestore lays out beside the library directory dir a copy of it with
// the files the installer left out written in, for a mutation to publish in
// its place. It returns the staged directory and the fingerprint a publish
// of it expects; on an error nothing is left behind.
func stageRestore(m *home.Mutation, dir string, left []treeFile) (string, string, error) {
	staged := m.Sibling(dir, "staged")
	fingerprint, err := func() (string, error) {
		if err := copyTreeTo(dir, staged); err != nil {
			return "", err
		}
		for _, f := range left {
			full := filepath.Join(staged, filepath.FromSlash(path.Clean(f.path)))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return "", err
			}
			mode := os.FileMode(0o644)
			if f.mode == source.ExecutableMode {
				mode = 0o755
			}
			if err := writeSynced(full, []byte(f.body), mode); err != nil {
				return "", err
			}
		}
		if err := home.SyncTree(staged); err != nil {
			return "", err
		}
		return home.Fingerprint(staged)
	}()
	if err != nil {
		_ = home.RemoveTree(staged)
		return "", "", err
	}
	return staged, fingerprint, nil
}
