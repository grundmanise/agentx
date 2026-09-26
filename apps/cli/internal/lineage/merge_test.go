package lineage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// TestPendingMessageRoundTrips: the message a pending merge commit carries
// reads back as the three versions it names and the count of files its
// subject says are unresolved, one file and several alike.
func TestPendingMessageRoundTrips(t *testing.T) {
	t.Parallel()
	m := Merge{Base: commitID, Mine: strings.Repeat("a", 40), Theirs: strings.Repeat("b", 40)}
	for _, n := range []int{0, 1, 2, 12} {
		message := m.Message("alpha", n)
		got := ParsePending("c0ffee", message)
		want := PendingMerge{Commit: "c0ffee", Merge: m, Readable: true, Unresolved: n}
		if got != want {
			t.Errorf("ParsePending(%q) = %+v, want %+v", message, got, want)
		}
	}
	if subject, _, _ := strings.Cut(m.Message("alpha", 1), "\n"); subject != "pending merge of alpha: 1 file unresolved" {
		t.Errorf("the subject of one file is %q", subject)
	}
}

// TestParsePendingRefusesAnythingElse: a message that is not one agentx
// writes names no three versions, and one whose subject does not count
// its files says nothing about them. The merge is pending all the same,
// which is the ref's to say, so the commit is kept.
func TestParsePendingRefusesAnythingElse(t *testing.T) {
	t.Parallel()
	good := Merge{Base: commitID, Mine: strings.Repeat("a", 40), Theirs: strings.Repeat("b", 40)}.Message("alpha", 2)
	for _, c := range []struct {
		name, message string
		readable      bool
		unresolved    int
	}{
		{name: "no message", message: "", unresolved: -1},
		{name: "a subject alone", message: "pending merge of alpha: 2 files unresolved\n", unresolved: 2},
		{name: "a trailer missing", message: strings.Replace(good, TrailerMergeMine+": "+strings.Repeat("a", 40)+"\n", "", 1), unresolved: 2},
		{name: "a trailer given twice", message: good + TrailerMergeBase + ": " + commitID + "\n", unresolved: 2},
		{name: "a fourth trailer", message: good + "Agentx-Merge-Resolved: notes.md\n", unresolved: 2},
		{name: "a trailer that is no commit id", message: strings.Replace(good, commitID, "HEAD", 1), unresolved: 2},
		{name: "a subject that counts nothing", message: strings.Replace(good, "2 files unresolved", "some files unresolved", 1), readable: true, unresolved: -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := ParsePending("c0ffee", c.message)
			if got.Readable != c.readable || got.Unresolved != c.unresolved || got.Commit != "c0ffee" {
				t.Errorf("ParsePending = %+v, want readable %t and unresolved %d", got, c.readable, c.unresolved)
			}
		})
	}
}

// TestWriteMineWrapsTheDirectoryAsAnImportTree: the mine of a merge is the
// library directory as the in-process reader recorded it, a symlink and an
// executable file included and nothing ignored, wrapped in the upstream
// directory of the import commit it is committed on, which is its one
// parent. A second write of the same directory is the same commit.
func TestWriteMineWrapsTheDirectoryAsAnImportTree(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	r, gitDir := newRepo(t)
	imp := Import{Source: "https://github.com/example/skills", Path: "skills/alpha-dir", Commit: commitID, Hash: hashID}
	base := skillTree(t, ctx, r, gitDir, map[string]string{"SKILL.md": "---\nname: alpha\n---\n"}, "")
	rec := importCommit(t, ctx, r, gitDir, imp, imp.Dir(), base.tree, "1700000000 +0000")
	rec.Name = "alpha"

	dir := t.TempDir()
	for path, body := range map[string]string{"SKILL.md": "---\nname: alpha\n---\nedited\n", ".gitignore": "*\n", "sub/run.sh": "#!/bin/sh\n"} {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(dir, "sub", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub/run.sh", filepath.Join(dir, "run")); err != nil {
		t.Fatal(err)
	}
	tree, err := treeid.Read(dir)
	if err != nil {
		t.Fatal(err)
	}

	mine, err := WriteMine(ctx, r, gitDir, dir, tree, rec)
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
	if got, want := git("rev-parse", mine+"^{tree}"), treeid.Wrap("alpha-dir", tree.ID); got != want {
		t.Errorf("mine's tree is %s, want %s", got, want)
	}
	if got := git("rev-parse", mine+"^@"); got != rec.Commit {
		t.Errorf("mine's parents are %q, want %s", got, rec.Commit)
	}
	if got := git("ls-tree", "-r", mine, "--format=%(objectmode) %(path)"); got != "100644 alpha-dir/.gitignore\n100644 alpha-dir/SKILL.md\n120000 alpha-dir/run\n100755 alpha-dir/sub/run.sh" {
		t.Errorf("mine holds\n%s", got)
	}
	again, err := WriteMine(ctx, r, gitDir, dir, tree, rec)
	if err != nil || again != mine {
		t.Errorf("a second write is %s, %v; want %s", again, err, mine)
	}
}
