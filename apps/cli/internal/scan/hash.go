package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// id renders one graph identity: SHA-256 over the type and its parts, each
// preceded by NUL, prefixed with the type.
func id(typ string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(typ))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return typ + ":" + hex.EncodeToString(h.Sum(nil))
}

// contentHash is the hash over a skill's name, description and files, as
// documented in the CLI contract. root is the skill directory with every
// symlink resolved.
func contentHash(root string, fm frontmatter, warn func(string)) string {
	h := newContentHasher(fm.name, fm.description)
	w := &walker{root: root, warn: warn}
	w.walk(root, "", []string{root})
	sort.Slice(w.files, func(i, j int) bool { return w.files[i].rel < w.files[j].rel })
	for _, f := range w.files {
		b, err := os.ReadFile(f.real)
		if err != nil {
			warn(err.Error() + ", skipped")
			continue
		}
		h.file(f.rel, b)
	}
	return h.sum()
}

// File is one regular file of a skill read from somewhere other than the
// filesystem, today a tree in the account repo: its path relative to the
// skill directory with / as the separator, and its bytes.
type File struct {
	Path    string
	Content string
}

// ContentHash is the content hash of a skill whose files are already in
// hand, for an install that reads a version out of git and never lays it
// out on disk first. It is the same hash contentHash computes over a
// directory: the same name, description and files give the same hex.
func ContentHash(name, description string, files []File) string {
	sorted := append([]File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	h := newContentHasher(name, description)
	for _, f := range sorted {
		h.file(f.Path, []byte(f.Content))
	}
	return h.sum()
}

// contentHasher writes the byte sequence the CLI contract defines. Both the
// walk of a directory and the read of a tree feed it, so the two cannot
// drift apart.
type contentHasher struct{ h hash.Hash }

func newContentHasher(name, description string) *contentHasher {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write([]byte(description))
	h.Write([]byte{0})
	return &contentHasher{h: h}
}

// file adds one regular file: its relative path, its length in decimal and
// its bytes. Modes and times never contribute.
func (c *contentHasher) file(rel string, b []byte) {
	c.h.Write([]byte(rel))
	c.h.Write([]byte{0})
	c.h.Write([]byte(strconv.Itoa(len(b))))
	c.h.Write([]byte{0})
	c.h.Write(b)
}

func (c *contentHasher) sum() string { return hex.EncodeToString(c.h.Sum(nil)) }

type walker struct {
	root  string
	warn  func(string)
	files []struct{ rel, real string }
}

// walk lists the regular files under dir, itself a real directory, with
// their paths relative to the skill. A symlink is followed when it resolves
// inside the skill; stack holds the real directories being walked, so a
// symlink back into one of them is a loop.
func (w *walker) walk(dir, rel string, stack []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		w.warn(err.Error() + ", skipped")
		return
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		childRel := e.Name()
		if rel != "" {
			childRel = rel + "/" + e.Name()
		}
		real := path
		if e.Type()&os.ModeSymlink != 0 {
			real, err = filepath.EvalSymlinks(path)
			if err != nil {
				w.warn(path + ": broken symlink, skipped")
				continue
			}
			if !inside(w.root, real) {
				w.warn(path + ": symlink resolves outside the skill, skipped")
				continue
			}
		}
		info, err := os.Stat(real)
		if err != nil {
			w.warn(err.Error() + ", skipped")
			continue
		}
		switch {
		case info.IsDir():
			if loops(stack, real) {
				w.warn(path + ": symlink loops inside the skill, skipped")
				continue
			}
			w.walk(real, childRel, append(stack, real))
		case info.Mode().IsRegular():
			w.files = append(w.files, struct{ rel, real string }{childRel, real})
		}
	}
}

func inside(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

func loops(stack []string, dir string) bool {
	for _, s := range stack {
		if inside(dir, s) {
			return true
		}
	}
	return false
}
