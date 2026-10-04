package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// TestStagedAndPreviousReadBothRefsAtOnce: the end of a fetch reads the
// staging ref and the source ref in one for-each-ref, and answers for the
// source ref what rev-parse --verify --quiet <ref>^{commit} answered before
// the two reads were one: the commit it holds or peels to, nothing when it
// is not there or peels to no commit, and a broken source ref still leaves
// the staging ref read, so that the fetch can repair it.
func TestStagedAndPreviousReadBothRefsAtOnce(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	r := gitx.New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(string, ...any) {})
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	git := func(stdin string, args ...string) string {
		t.Helper()
		out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(stdin), args...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	git("", "init", "--bare", "--quiet", gitDir)
	tree := git("", "mktree")
	commit := git("", "commit-tree", tree, "-m", "one")
	newer := git("", "commit-tree", tree, "-p", commit, "-m", "two")
	tag := func(name, object, typ string) string {
		return git("object "+object+"\ntype "+typ+"\ntag "+name+"\ntagger "+gitx.Identity+" 0 +0000\n\n"+name+"\n", "mktag")
	}
	onCommit := tag("v1", commit, "commit")
	onTag := tag("v2", onCommit, "tag")
	onTree := tag("v3", tree, "tree")

	const staging, source = "refs/agentx/fetching/run/0123456789abcdef", "refs/agentx/sources/0123456789abcdef"
	set := func(ref, value string) {
		t.Helper()
		if value == "" {
			git("", "update-ref", "-d", ref)
			return
		}
		git("", "update-ref", ref, value)
	}
	for _, c := range []struct {
		name                 string
		staged, held         string // what the two refs hold, "" for nothing
		broken               bool   // the source ref names an object the repo lacks
		object, commit, prev string
	}{
		{name: "a first fetch", staged: newer, object: newer, commit: newer},
		{name: "a commit held", staged: newer, held: commit, object: newer, commit: newer, prev: commit},
		{name: "a tag of a commit held", staged: newer, held: onCommit, object: newer, commit: newer, prev: commit},
		{name: "a tag of a tag held", staged: newer, held: onTag, object: newer, commit: newer, prev: commit},
		{name: "a tag of a tree held", staged: newer, held: onTree, object: newer, commit: newer},
		{name: "a tree held", staged: newer, held: tree, object: newer, commit: newer},
		{name: "a broken source ref", staged: newer, broken: true, object: newer, commit: newer},
		{name: "a tag staged", staged: onCommit, held: commit, object: onCommit, commit: commit, prev: commit},
		{name: "a tree staged", staged: tree, held: commit, prev: commit},
		{name: "nothing staged", held: commit, prev: commit},
	} {
		set(staging, c.staged)
		set(source, c.held)
		if c.broken {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(gitDir, source)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(gitDir, source), []byte(strings.Repeat("3", 40)+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		// What rev-parse answered for the source ref, read on its own.
		before, _ := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", source+"^{commit}")
		if before != c.prev {
			t.Fatalf("%s: rev-parse answers %q, the case says %q", c.name, before, c.prev)
		}
		object, commit, prev, held, err := stagedAndPrevious(ctx, r, gitDir, staging, source)
		if err != nil || object != c.object || commit != c.commit || prev != c.prev || held != c.held {
			t.Errorf("%s: stagedAndPrevious = %q, %q, %q, %q, %v; want %q, %q, %q, %q", c.name, object, commit, prev, held, err, c.object, c.commit, c.prev, c.held)
		}
		if err := os.RemoveAll(filepath.Join(gitDir, source)); err != nil {
			t.Fatal(err)
		}
	}
}
