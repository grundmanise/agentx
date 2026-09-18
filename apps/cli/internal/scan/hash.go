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
	h := sha256.New()
	h.Write([]byte(fm.name))
	h.Write([]byte{0})
	h.Write([]byte(fm.description))
	h.Write([]byte{0})
	w := &walker{root: root, warn: warn}
	w.walk(root, "", []string{root})
	sort.Slice(w.files, func(i, j int) bool { return w.files[i].rel < w.files[j].rel })
	for _, f := range w.files {
		hashFile(h, f.rel, f.real, warn)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func hashFile(h hash.Hash, rel, real string, warn func(string)) {
	b, err := os.ReadFile(real)
	if err != nil {
		warn(real + ": " + err.Error() + ", skipped")
		return
	}
	h.Write([]byte(rel))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(len(b))))
	h.Write([]byte{0})
	h.Write(b)
}

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
		w.warn(dir + ": " + err.Error() + ", skipped")
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
			w.warn(path + ": " + err.Error() + ", skipped")
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
