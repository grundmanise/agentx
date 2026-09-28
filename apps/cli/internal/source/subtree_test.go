package source

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// TestSubtreeAnswersAsRevParseAndCatFileDo: the tree a listing starts from
// is found with one git process where it used to take two, rev-parse and
// then cat-file -t, and for every path a commit can hold or not – a
// directory, a file, a file whose object the account repo lacks, with a
// promisor remote or without, a submodule, nothing, a path through a file,
// a name with a newline – it answers what those two did, word for word.
func TestSubtreeAnswersAsRevParseAndCatFileDo(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	var calls atomic.Int32
	r := gitx.New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(format string, args ...any) {
		if strings.HasPrefix(fmt.Sprintf(format, args...), "git ") && !strings.HasPrefix(format, "git stderr") {
			calls.Add(1)
		}
	})
	ctx := context.Background()
	for _, promisor := range []bool{false, true} {
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
		if promisor {
			git("", "config", "remote.origin.url", "file:///nowhere")
			git("", "config", "remote.origin.promisor", "true")
		}
		blob := git("---\nname: alpha\n---\n", "hash-object", "-w", "--stdin")
		dir := git("100644 blob "+blob+"\tSKILL.md\n", "mktree")
		gone, other := strings.Repeat("1", 40), strings.Repeat("2", 40)
		root := git("040000 tree "+dir+"\tdir\x00"+
			"040000 tree "+dir+"\ta\nb\x00"+
			"100644 blob "+blob+"\tfile.md\x00"+
			"100644 blob "+gone+"\tgone.md\x00"+
			"160000 commit "+other+"\tsub\x00", "mktree", "-z", "--missing")
		commit := git("", "commit-tree", root, "-m", "one")

		// What the two reads answered, as skillEntries made them before.
		twoReads := func(subpath string) (string, error) {
			treeish := commit + "^{tree}"
			if subpath != "" {
				treeish = commit + ":" + subpath
			}
			id, err := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", treeish)
			if err != nil || id == "" {
				return "", fmt.Errorf("%w: %q", ErrNoSubpath, subpath)
			}
			if typ, err := r.Isolated(ctx, gitDir, "cat-file", "-t", id); err != nil || typ != "tree" {
				return "", fmt.Errorf("%w: %q is not a directory", ErrNoSubpath, subpath)
			}
			return id, nil
		}
		for _, c := range []struct {
			subpath string
			calls   int32 // the git processes subtree runs: one, unless batch-check cannot tell
		}{
			{"", 1}, {"dir", 1}, {"file.md", 1}, {"sub", 3}, {"gone.md", 3}, {"nope", 2}, {"dir/nope", 2},
			{"file.md/x", 2}, {"a\nb", 2},
		} {
			want, wantErr := twoReads(c.subpath)
			before := calls.Load()
			got, err := subtree(ctx, r, gitDir, commit, c.subpath)
			ran := calls.Load() - before
			if got != want || fmt.Sprint(err) != fmt.Sprint(wantErr) {
				t.Errorf("promisor %v, %q: subtree = %q, %v; rev-parse and cat-file -t answer %q, %v", promisor, c.subpath, got, err, want, wantErr)
			}
			if ran != c.calls {
				t.Errorf("promisor %v, %q: subtree ran %d git processes, want %d", promisor, c.subpath, ran, c.calls)
			}
		}
	}
}
