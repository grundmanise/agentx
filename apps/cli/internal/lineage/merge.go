package lineage

import (
	"context"
	"fmt"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// Merge is the three versions of a skill a merge takes, each a commit of
// the account repo whose tree wraps the skill in its upstream directory, as
// an import tree does: the base version the import branch pointed at, the
// library directory as git records it, committed on top of it, and the
// update candidate. A merge that conflicts is left as a git merge in
// progress with the three as its base, HEAD and MERGE_HEAD.
type Merge struct {
	Base   string // the import commit the skill's branch pointed at
	Mine   string // the library directory, committed with Base as its parent
	Theirs string // the update candidate
}

// CommitDir commits tree, a tree git wrote of a library directory, as the
// "mine" of a merge: the commit's tree wraps it in one entry named dir, the
// upstream directory, as an import tree wraps a version, and its one
// parent is parent, so that merge-tree compares the directory with its
// base version path for path. One mktree and one commit-tree, in the
// isolated environment, whose fixed identity and date make the same tree
// on the same parent the same commit.
func CommitDir(ctx context.Context, r *gitx.Runner, gitDir, dir, tree, parent, message string) (string, error) {
	wrapped, err := mktree(ctx, r, gitDir, []string{treeInput([]string{entryLine(source.DirMode, tree, dir)})})
	if err != nil {
		return "", err
	}
	if want := treeid.Wrap(dir, tree); wrapped[0] != want {
		return "", fmt.Errorf("git wrote the tree around %s as %s, not %s", dir, wrapped[0], want)
	}
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(message), "commit-tree", wrapped[0], "-p", parent)
	return strings.TrimSpace(out), err
}

// ReadMerged reads the version a merge wrote out of tree, the root tree
// merge-tree answered with, as ReadBase reads the one an import commit
// holds: everything below the upstream directory dir, with that name taken
// off every path.
func ReadMerged(ctx context.Context, r *gitx.Runner, gitDir, tree, dir string) (Base, error) {
	return readVersion(ctx, r, gitDir, tree, dir, "the merged tree "+tree)
}

// At is the record of rec's skill with its import branch at commit, an
// import commit no ref of the skill's own may name any more, read in one
// git log: the update a pending merge merges, which a check may have moved
// the candidate on from since.
func (rec Record) At(ctx context.Context, r *gitx.Runner, gitDir, commit string) (Record, error) {
	out, err := r.Isolated(ctx, gitDir, "log", "-1", "--format=%T%n%B", commit)
	if err != nil {
		return Record{}, err
	}
	tree, message, _ := strings.Cut(out, "\n")
	imp, err := Parse(message)
	if err != nil {
		return Record{}, err
	}
	return Record{Name: rec.Name, Kind: rec.Kind, Ref: rec.Ref, Commit: commit, Tree: tree, Import: imp, HasImport: true}, nil
}
