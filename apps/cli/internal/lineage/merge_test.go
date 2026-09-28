package lineage

import (
	"context"
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
