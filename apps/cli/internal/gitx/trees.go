package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// TreeEdit is one file a tree is written with: its path from the tree's
// root, with / as the separator, its mode, 100644 or 100755, and its bytes.
type TreeEdit struct {
	Path    string
	Mode    string
	Content []byte
}

// EditTree writes the tree base holds, "" for the empty tree, with each of
// edits written into it, and returns the new tree's id. The files are
// stored as they are, with no filter or line-ending conversion, through an
// index of its own in a temporary directory, so that nothing of any
// worktree's is touched.
func (r *Runner) EditTree(ctx context.Context, gitDir, base string, edits []TreeEdit) (string, error) {
	tmp, err := os.MkdirTemp("", "agentx-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	indexed := func(stdin []byte, args ...string) (string, error) {
		c := call{isolated: true, env: map[string]string{"GIT_INDEX_FILE": filepath.Join(tmp, "index")}}
		if stdin != nil {
			c.stdin = bytes.NewReader(stdin)
		}
		out, err := r.run(ctx, c, isolatedArgs(gitDir, args)...)
		return strings.TrimSpace(out), err
	}
	if base != "" {
		if _, err := indexed(nil, "read-tree", base); err != nil {
			return "", err
		}
	}
	for _, e := range edits {
		oid, err := indexed(e.Content, "hash-object", "-w", "--no-filters", "--stdin")
		if err != nil {
			return "", err
		}
		if _, err := indexed(nil, "update-index", "--add", "--cacheinfo", e.Mode+","+oid+","+e.Path); err != nil {
			return "", err
		}
	}
	return indexed(nil, "write-tree")
}

// ReplaceEntry is the root tree of the commit parent with its entry dir
// pointing at the tree subtree and every other entry kept, and, with no
// parent, the tree holding dir alone. A fork's branch holds its skill
// directory as the one entry of its root, and every commit agentx writes
// on it replaces that entry this way: a file a user committed with git
// beside the skill directory stays in the branch rather than being deleted
// by agentx's next commit. One ls-tree and one mktree, git's answer held to
// the id computed in process when the result holds dir alone.
func (r *Runner) ReplaceEntry(ctx context.Context, gitDir, parent, dir, subtree string) (string, error) {
	wrapped := treeid.Wrap(dir, subtree)
	if parent == "" {
		return r.mktree(ctx, gitDir, []string{treeid.DirMode + " tree " + subtree + "\t" + dir}, wrapped)
	}
	out, err := r.Isolated(ctx, gitDir, "ls-tree", "-z", parent)
	if err != nil {
		return "", err
	}
	records := []string{treeid.DirMode + " tree " + subtree + "\t" + dir}
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		if _, name, _ := strings.Cut(record, "\t"); name == dir {
			continue
		}
		records = append(records, record)
	}
	if len(records) == 1 {
		return r.mktree(ctx, gitDir, records, wrapped)
	}
	return r.mktree(ctx, gitDir, records, "")
}

// mktree writes the tree of the NUL-terminated ls-tree records given and,
// when want is not "", holds git's answer to it.
func (r *Runner) mktree(ctx context.Context, gitDir string, records []string, want string) (string, error) {
	var b strings.Builder
	for _, record := range records {
		b.WriteString(record)
		b.WriteByte(0)
	}
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "mktree", "-z")
	if err != nil {
		return "", err
	}
	tree := strings.TrimSpace(out)
	if want != "" && tree != want {
		return "", fmt.Errorf("git wrote the tree as %s, not %s", tree, want)
	}
	return tree, nil
}
