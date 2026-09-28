package lineage

import (
	"context"
	"fmt"
	"path"
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

// HoldsID reports whether a directory whose tree id is id holds exactly
// the base version rec records: the id, wrapped the way an import tree
// wraps it under the upstream's own name, is the tree of the import
// commit. It runs no git: the tree came with the lineage, in the one
// for-each-ref that read it.
func (rec Record) HoldsID(id string) bool {
	return rec.HasImport && treeid.Wrap(rec.Import.Dir(), id) == rec.Tree
}

// Canonical reports whether the import commit rec names stores base, the
// version read out of it, as git writes a tree today, which is the only
// form a directory on disk can be compared equal to. Every import writes
// it that way; an earlier agentx reused a source's own tree whole, and a
// source may store a mode git reads but no longer writes, such as 100664.
// HoldsID is never true against a branch that fails this, whatever the
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

// ReadBase reads the base version of a managed skill out of its import
// commit in one ls-tree: the commit's one entry, the upstream directory,
// and everything below it with that directory's name taken off, so that
// every path a command shows or writes is relative to the skill's own
// directory, whatever the upstream called it.
func ReadBase(ctx context.Context, r *gitx.Runner, gitDir string, rec Record) (Base, error) {
	if !rec.HasImport {
		return Base{}, fmt.Errorf("%w: %s carries no lineage", ErrTrailer, rec.Ref)
	}
	entries, err := source.ReadTree(ctx, r, gitDir, rec.Commit)
	if err != nil {
		return Base{}, err
	}
	dir := rec.Import.Dir()
	var base Base
	for _, e := range entries {
		switch rest, below := strings.CutPrefix(e.Path, dir+"/"); {
		case e.Path == dir && e.Mode == source.DirMode:
			base.Tree = e.OID
		case below:
			e.Path = rest
			base.Entries = append(base.Entries, e)
		default:
			return Base{}, fmt.Errorf("%w: %s holds %q beside %s", ErrTrailer, rec.Ref, e.Path, dir)
		}
	}
	if base.Tree == "" {
		return Base{}, fmt.Errorf("%w: %s holds no directory %s", ErrTrailer, rec.Ref, dir)
	}
	return base, nil
}
