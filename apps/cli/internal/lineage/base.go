package lineage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// UpstreamDir is the directory name an import tree holds a version under:
// the last segment of the subpath, and the repository's name for a skill at
// the root, which is what the source listing calls it too. It is not the
// library directory name, which the frontmatter decides. Every writer of an
// import commit and every reader of one takes it from here, so that a base
// version is always found where it was put.
func UpstreamDir(sourceURL, subpath string) string {
	if subpath == "" {
		return source.RepoName(sourceURL)
	}
	return path.Base(subpath)
}

// Dir is the upstream directory the import commit of i holds its version
// under.
func (i Import) Dir() string { return UpstreamDir(i.Source, i.Path) }

// Current reports whether the directory read as lib holds exactly the base
// version rec records: the tree id of the directory, wrapped the way an
// import tree wraps it under the upstream's own name, is the tree of the
// import commit. It runs no git: the tree came with the lineage, in the one
// for-each-ref that read it. A directory holding anything git cannot
// record is never current, since the base version holds none of it.
func (rec Record) Current(lib treeid.Tree) bool {
	return rec.HasImport && Holds(lib, rec.Import.Dir(), rec.Tree)
}

// Holds reports whether lib, a directory read as git would record it, is
// exactly the version an import tree holds under the upstream directory
// dir: the one comparison a directory and a base version are told apart
// by, whichever command makes it.
func Holds(lib treeid.Tree, dir, importTree string) bool {
	return len(lib.Unrecordable) == 0 && treeid.Wrap(dir, lib.ID) == importTree
}

// Canonical reports whether the import commit rec names stores base, the
// version read out of it, as git writes a tree today, which is the only
// form a directory on disk can be compared equal to. Every import writes
// it that way; an earlier agentx reused a source's own tree whole, and a
// source may store a mode git reads but no longer writes, such as 100664.
// Current is never true against a branch that fails this, whatever the
// library holds: a diff says so, and a revert writes the branch again, see
// Rewrite.
func (rec Record) Canonical(base Base) bool {
	return rec.HasImport && treeid.Wrap(rec.Import.Dir(), base.ID()) == rec.Tree
}

// Base is the base version of a managed skill as the account repo holds it:
// the tree of the skill's directory and every entry below it, their paths
// relative to that directory.
type Base struct {
	Tree    string
	Entries []source.TreeEntry
}

// ID is the id git gives the base version's directory as it writes a tree
// today, computed in process from every entry it holds: its files, and
// the symlinks no import writes but a directory laid out from the base
// would hold all the same. For a base an import writes now it is Tree
// itself, since every import tree is written that way. A base an earlier
// agentx reused whole from a source that stores a mode git no longer
// writes, such as 100664, keeps an id of its own in Tree, while the
// directory laid out from it, like any directory on disk, has this one.
// So a command that lays the base out holds what it laid out to ID, and a
// diff, which git reads with canonical modes, compares with Tree.
func (b Base) ID() string {
	return planTrees(Version{Tree: b.Tree, Entries: b.Entries}, true).ids[""]
}

// HeldBy reports whether lib, a directory read as git would record it,
// holds exactly the files, modes and links of the base, however the
// account repo stores them. Against a branch that is Canonical it is
// Current; against one that is not, which Current never matches, it is
// what tells a directory holding the version from one holding another.
func (b Base) HeldBy(lib treeid.Tree) bool {
	return len(lib.Unrecordable) == 0 && lib.ID == b.ID()
}

// ReadBase reads the base version of a managed skill out of its import
// commit in one ls-tree: the commit's one entry, the upstream directory,
// and everything below it with that directory's name taken off, so that
// every path a command shows or writes is relative to the skill's own
// directory, whatever the upstream called it.
func ReadBase(ctx context.Context, r *gitx.Runner, gitDir string, rec Record) (Base, error) {
	if !rec.HasImport {
		return Base{}, fmt.Errorf("%w: %s carries no lineage", ErrTrailer, rec.Ref)
	}
	return readVersion(ctx, r, gitDir, rec.Commit, rec.Import.Dir(), rec.Ref)
}

// readVersion reads the version of a skill a tree of the account repo holds
// under the upstream directory dir, the tree's one entry, as ReadBase says.
// what names the tree in an error.
func readVersion(ctx context.Context, r *gitx.Runner, gitDir, treeish, dir, what string) (Base, error) {
	entries, err := source.ReadTree(ctx, r, gitDir, treeish)
	if err != nil {
		return Base{}, err
	}
	var base Base
	for _, e := range entries {
		switch rest, below := strings.CutPrefix(e.Path, dir+"/"); {
		case e.Path == dir && e.Mode == source.DirMode:
			base.Tree = e.OID
		case below:
			e.Path = rest
			base.Entries = append(base.Entries, e)
		default:
			return Base{}, fmt.Errorf("%w: %s holds %q beside %s", ErrTrailer, what, e.Path, dir)
		}
	}
	if base.Tree == "" {
		return Base{}, fmt.Errorf("%w: %s holds no directory %s", ErrTrailer, what, dir)
	}
	return base, nil
}

// WriteDir writes what the directory at root holds, as tree read it, into
// the object store of gitDir, and returns the id of its tree. The blobs go
// in through one hash-object with --no-filters, so that no attribute and no
// line-ending rule of anybody's changes a byte, and the trees through one
// mktree, each tree after the ones below it: two git processes whatever the
// directory holds, and never an index or a work tree, which would apply the
// ignore rules and lose what they name. What git cannot record, which tree
// names, is not written, and the caller says so.
//
// Every id git answers with is held to the id tree computed in process: a
// file that changed between the read and the write would otherwise be
// written as a version nobody read.
func WriteDir(ctx context.Context, r *gitx.Runner, gitDir, root string, tree treeid.Tree) (string, error) {
	if len(tree.Dirs) == 0 {
		return treeid.EmptyTree, nil
	}
	return writeDir(ctx, r, gitDir, root, tree, "")
}

// writeDir is WriteDir for a directory that records something, with the
// tree wrapped in one more, holding it under the name wrap, written in the
// same mktree when wrap is not "", and the id of that tree returned
// instead: the shape of an import tree, which a merge compares a library
// directory in.
func writeDir(ctx context.Context, r *gitx.Runner, gitDir, root string, tree treeid.Tree, wrap string) (string, error) {
	if err := writeBlobs(ctx, r, gitDir, root, tree.Blobs); err != nil {
		return "", err
	}
	defs := make([]string, len(tree.Dirs), len(tree.Dirs)+1)
	for i, d := range tree.Dirs {
		lines := make([]string, len(d.Entries))
		for j, e := range d.Entries {
			lines[j] = entryLine(e.Mode, e.OID, e.Name)
		}
		defs[i] = treeInput(lines)
	}
	if wrap != "" {
		defs = append(defs, treeInput([]string{entryLine(source.DirMode, tree.ID, wrap)}))
	}
	written, err := mktree(ctx, r, gitDir, defs)
	if err != nil {
		return "", err
	}
	for i, d := range tree.Dirs {
		if written[i] != d.ID {
			return "", fmt.Errorf("%w: git wrote the directory %q as %s, not %s", ErrChanged, d.Path, written[i], d.ID)
		}
	}
	if wrap == "" {
		return tree.ID, nil
	}
	if want := treeid.Wrap(wrap, tree.ID); written[len(written)-1] != want {
		return "", fmt.Errorf("git wrote the tree around %s as %s, not %s", wrap, written[len(written)-1], want)
	}
	return written[len(written)-1], nil
}

// WriteContent writes each of bodies into the object store of gitDir as a
// blob, byte for byte, through the one hash-object every blob of a
// directory is written through, and returns their ids in the same order:
// the content a resolve assembled of a file, or an editor saved. Each id is
// held to the one computed in process.
func WriteContent(ctx context.Context, r *gitx.Runner, gitDir string, bodies []string) ([]string, error) {
	blobs := make([]treeid.Blob, len(bodies))
	ids := make([]string, len(bodies))
	for i, body := range bodies {
		// A blob written from what it holds rather than from a path of the
		// directory is a symlink's to writeBlobs: its Data goes into a file
		// of its own, which is what a body needs.
		ids[i] = treeid.BlobID([]byte(body))
		blobs[i] = treeid.Blob{Path: fmt.Sprint(i), OID: ids[i], Link: true, Data: body}
	}
	if err := writeBlobs(ctx, r, gitDir, "", blobs); err != nil {
		return nil, err
	}
	return ids, nil
}

// ErrChanged is the error of a directory that changed while it was written.
var ErrChanged = errors.New("the directory changed while it was read")

// writeBlobs writes every blob of a tree through one hash-object reading
// paths from its standard input. A file is named by its path on disk, a
// symlink by a temporary file holding its target, since hash-object reads
// what a path leads to and the blob of a link is the link itself. Every
// path is C-quoted, which is how hash-object reads a line that starts with
// a double quote, so that a name holding a newline is one path and not two.
func writeBlobs(ctx context.Context, r *gitx.Runner, gitDir, root string, blobs []treeid.Blob) error {
	if len(blobs) == 0 {
		return nil
	}
	var links string
	defer func() {
		if links != "" {
			os.RemoveAll(links)
		}
	}()
	var b strings.Builder
	for i, blob := range blobs {
		file := filepath.Join(root, filepath.FromSlash(blob.Path))
		if blob.Link {
			if links == "" {
				dir, err := os.MkdirTemp("", "agentx-links-")
				if err != nil {
					return err
				}
				links = dir
			}
			file = filepath.Join(links, fmt.Sprint(i))
			if err := os.WriteFile(file, []byte(blob.Data), 0o600); err != nil {
				return err
			}
		}
		b.WriteString(cQuoted(file))
		b.WriteByte('\n')
	}
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "hash-object", "-w", "--no-filters", "--stdin-paths")
	if err != nil {
		// A file that is gone, or is no longer a file, since the tree was
		// read is the directory changing, not the account repo failing.
		for _, blob := range blobs {
			if blob.Link {
				continue
			}
			info, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(blob.Path)))
			if statErr != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("%w: %q is no longer the file that was read", ErrChanged, blob.Path)
			}
		}
		return err
	}
	ids := strings.Fields(out)
	if len(ids) != len(blobs) {
		return fmt.Errorf("git hash-object wrote %d blobs for %d files", len(ids), len(blobs))
	}
	for i, blob := range blobs {
		if ids[i] != blob.OID {
			return fmt.Errorf("%w: git wrote %q as %s, not %s", ErrChanged, blob.Path, ids[i], blob.OID)
		}
	}
	return nil
}

// cQuoted is path in double quotes with a backslash before every double
// quote and backslash and every control byte written as an octal escape,
// which is the quoting git reads back to the same bytes.
func cQuoted(path string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(path); i++ {
		switch c := path[i]; {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
