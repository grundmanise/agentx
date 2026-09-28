package lineage

import (
	"context"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// TestCommitDirWrapsTheTreeGitWrote: the mine of a merge is the tree git
// wrote of the library directory, wrapped in the upstream directory of the
// import commit it is committed on, which is its one parent. A second
// commit of the same tree on the same parent is the same commit.
func TestCommitDirWrapsTheTreeGitWrote(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	r, gitDir := newRepo(t)
	imp := Import{Source: "https://github.com/example/skills", Path: "skills/alpha-dir", Commit: commitID, Hash: hashID}
	base := skillTree(t, ctx, r, gitDir, map[string]string{"SKILL.md": "---\nname: alpha\n---\n"}, "")
	rec := importCommit(t, ctx, r, gitDir, imp, imp.Dir(), base.tree, "1700000000 +0000")
	edited := skillTree(t, ctx, r, gitDir, map[string]string{"SKILL.md": "---\nname: alpha\n---\nedited\n", "notes.md": "notes\n"}, "")

	mine, err := CommitDir(ctx, r, gitDir, imp.Dir(), edited.tree, rec.Commit, "library directory of alpha\n")
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := r.Isolated(ctx, gitDir, args...)
		if err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
		return out
	}
	if got, want := git("rev-parse", mine+"^{tree}"), treeid.Wrap("alpha-dir", edited.tree); got != want {
		t.Errorf("mine's tree is %s, want %s", got, want)
	}
	if got := git("rev-parse", mine+"^@"); got != rec.Commit {
		t.Errorf("mine's parents are %q, want %s", got, rec.Commit)
	}
	again, err := CommitDir(ctx, r, gitDir, imp.Dir(), edited.tree, rec.Commit, "library directory of alpha\n")
	if err != nil || again != mine {
		t.Errorf("a second commit is %s, %v; want %s", again, err, mine)
	}
}
