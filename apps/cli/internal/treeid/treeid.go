// Package treeid computes, in process, the git tree id a directory on disk
// would have: the id git gives the same files, modes and symlinks, so that a
// directory can be compared with a tree of the account repo without running
// git and without writing anything.
//
// It reads the directory the way git records one and in no other way: every
// entry is read with lstat and never followed, a regular file is a blob of
// mode 100644, or 100755 when its owner may execute it, a symlink is a blob
// of mode 120000 holding the link's target, a directory is a tree, and a
// directory holding nothing git records is left out, as git leaves it out.
// Nothing is ignored and nothing is filtered: no .gitignore, no attributes,
// no line-ending conversion. A git that ran add over the same directory
// could give another id, which is exactly why this package exists: what is
// compared, written and diffed is the directory as it is.
//
// What git cannot record is named rather than dropped silently: a path with
// a .git component, which git refuses in any case and which marks a
// repository nested in the skill, and anything that is neither a file, a
// directory nor a symlink, such as a named pipe or a socket.
package treeid

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The modes of the entries a tree records, as ls-tree prints them and
// mktree reads them.
const (
	FileMode       = "100644"
	ExecutableMode = "100755"
	SymlinkMode    = "120000"
	DirMode        = "040000"
)

// EmptyTree is the id of the tree that holds nothing, which git knows
// without storing it.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Tree is one directory read as git would record it.
type Tree struct {
	ID           string   // the tree id of the directory; EmptyTree when it records nothing
	Dirs         []Dir    // every directory recorded, each after the ones below it, the root last
	Blobs        []Blob   // every file and symlink recorded, in the order they were read
	Unrecordable []string // the paths git cannot record, relative to the directory, sorted
}

// Dir is one directory of a Tree with the entries it records.
type Dir struct {
	Path    string  // relative to the directory read, with / as the separator; "" for the directory itself
	ID      string  // its tree id
	Entries []Entry // in git's order
}

// Entry is one record of a tree: a name, a mode and an object id.
type Entry struct {
	Name string
	Mode string
	OID  string
}

// Blob is one file or symlink of a Tree.
type Blob struct {
	Path string // relative to the directory read, with / as the separator
	OID  string
	Link bool   // a symlink, whose blob is its target
	Data string // the target of a symlink; empty for a file, which is read again from disk to be written
}

// Read reads the directory at root, which must be a directory and not a
// symlink to one, as git would record it. An entry that cannot be read is
// an error: a tree id that left it out would name content that is not
// there.
func Read(root string) (Tree, error) {
	w := &walker{root: root}
	id, _, err := w.dir("")
	if err != nil {
		return Tree{}, err
	}
	sort.Strings(w.unrecordable)
	return Tree{ID: id, Dirs: w.dirs, Blobs: w.blobs, Unrecordable: w.unrecordable}, nil
}

type walker struct {
	root         string
	dirs         []Dir
	blobs        []Blob
	unrecordable []string
}

// dir reads the directory at rel and returns its tree id and whether it
// records anything at all.
func (w *walker) dir(rel string) (string, bool, error) {
	abs := filepath.Join(w.root, filepath.FromSlash(rel))
	list, err := os.ReadDir(abs)
	if err != nil {
		return "", false, err
	}
	var entries []Entry
	for _, e := range list {
		name := e.Name()
		child := path.Join(rel, name)
		if strings.EqualFold(name, ".git") {
			w.unrecordable = append(w.unrecordable, child)
			continue
		}
		info, err := os.Lstat(filepath.Join(abs, name))
		if err != nil {
			return "", false, err
		}
		switch mode := info.Mode(); {
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(filepath.Join(abs, name))
			if err != nil {
				return "", false, err
			}
			oid := BlobID([]byte(target))
			w.blobs = append(w.blobs, Blob{Path: child, OID: oid, Link: true, Data: target})
			entries = append(entries, Entry{Name: name, Mode: SymlinkMode, OID: oid})
		case mode.IsDir():
			id, any, err := w.dir(child)
			if err != nil {
				return "", false, err
			}
			if any {
				entries = append(entries, Entry{Name: name, Mode: DirMode, OID: id})
			}
		case mode.IsRegular():
			b, err := os.ReadFile(filepath.Join(abs, name))
			if err != nil {
				return "", false, err
			}
			oid := BlobID(b)
			w.blobs = append(w.blobs, Blob{Path: child, OID: oid})
			entries = append(entries, Entry{Name: name, Mode: fileMode(mode), OID: oid})
		default:
			w.unrecordable = append(w.unrecordable, child)
		}
	}
	if len(entries) == 0 {
		return EmptyTree, false, nil
	}
	Sort(entries)
	id := TreeID(entries)
	w.dirs = append(w.dirs, Dir{Path: rel, ID: id, Entries: entries})
	return id, true, nil
}

// fileMode is the mode git records a regular file with: executable when its
// owner may execute it, which is the one bit git looks at.
func fileMode(mode os.FileMode) string {
	if mode&0o100 != 0 {
		return ExecutableMode
	}
	return FileMode
}

// Sort puts entries in git's order: bytewise by name, a directory's name
// compared as if it ended in a slash.
func Sort(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool { return sortKey(entries[i]) < sortKey(entries[j]) })
}

func sortKey(e Entry) string {
	if e.Mode == DirMode {
		return e.Name + "/"
	}
	return e.Name
}

// BlobID is the id git gives a blob holding data.
func BlobID(data []byte) string {
	return objectID("blob", data)
}

// TreeID is the id git gives a tree holding entries, which must be in git's
// order and hold sha1 ids.
func TreeID(entries []Entry) string {
	var body bytes.Buffer
	for _, e := range entries {
		raw, err := hex.DecodeString(e.OID)
		if err != nil {
			panic(fmt.Sprintf("treeid: %q is not an object id", e.OID)) // every id here was computed by this package or read from git
		}
		body.WriteString(strings.TrimPrefix(e.Mode, "0"))
		body.WriteByte(' ')
		body.WriteString(e.Name)
		body.WriteByte(0)
		body.Write(raw)
	}
	return objectID("tree", body.Bytes())
}

// Wrap is the id of a tree holding one entry, the directory tree under name,
// which is the shape of an import commit's tree: the skill's directory under
// the upstream's own name.
func Wrap(name, tree string) string {
	return TreeID([]Entry{{Name: name, Mode: DirMode, OID: tree}})
}

func objectID(kind string, data []byte) string {
	h := sha1.New()
	h.Write([]byte(kind + " " + strconv.Itoa(len(data)) + "\x00"))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
