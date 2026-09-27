package lineage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// TestPendingMessageRoundTrips: the message a pending merge commit carries
// reads back as the three versions it names, the count of files its
// subject says are unresolved, one file and several alike, and the files
// its body lists as resolved, sorted and each once, whatever bytes their
// paths hold: a space at either end, a double quote, a backslash, a
// newline and a byte that is not UTF-8.
func TestPendingMessageRoundTrips(t *testing.T) {
	t.Parallel()
	m := Merge{Base: commitID, Mine: strings.Repeat("a", 40), Theirs: strings.Repeat("b", 40)}
	for _, c := range []struct {
		unresolved int
		resolved   []string
		want       []string
	}{
		{unresolved: 0},
		{unresolved: 1},
		{unresolved: 2, resolved: []string{"notes.md"}, want: []string{"notes.md"}},
		{unresolved: 12, resolved: []string{"z.md", " a b .md", "docs/\"q\".md", "back\\slash", "new\nline", "\xff.bin", "z.md"},
			want: []string{" a b .md", "back\\slash", "docs/\"q\".md", "new\nline", "z.md", "\xff.bin"}},
	} {
		message := m.Message("alpha", c.unresolved, c.resolved)
		got := ParsePending("c0ffee", message)
		want := PendingMerge{Commit: "c0ffee", Merge: m, Readable: true, Unresolved: c.unresolved, Resolved: c.want}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ParsePending(%q) = %+v, want %+v", message, got, want)
		}
	}
	if subject, _, _ := strings.Cut(m.Message("alpha", 1, nil), "\n"); subject != "pending merge of alpha: 1 file unresolved" {
		t.Errorf("the subject of one file is %q", subject)
	}
	if got, want := m.Message("alpha", 1, []string{"notes.md", "logo.bin"}),
		"pending merge of alpha: 1 file unresolved\n\nresolved \"logo.bin\"\nresolved \"notes.md\"\n\n"+
			TrailerMergeBase+": "+m.Base+"\n"+TrailerMergeMine+": "+m.Mine+"\n"+TrailerMergeTheirs+": "+m.Theirs+"\n"; got != want {
		t.Errorf("the message is\n%s\nwant\n%s", got, want)
	}
}

// TestParsePendingRefusesAnythingElse: a message that is not one agentx
// writes names no three versions, and one whose subject does not count
// its files says nothing about them. The merge is pending all the same,
// which is the ref's to say, so the commit is kept.
func TestParsePendingRefusesAnythingElse(t *testing.T) {
	t.Parallel()
	good := Merge{Base: commitID, Mine: strings.Repeat("a", 40), Theirs: strings.Repeat("b", 40)}.Message("alpha", 2, []string{"notes.md"})
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
		{name: "a resolved file not quoted", message: strings.Replace(good, `resolved "notes.md"`, "resolved notes.md", 1), unresolved: 2},
		{name: "a resolved file with an escape git does not write", message: strings.Replace(good, `resolved "notes.md"`, `resolved "notes\x.md"`, 1), unresolved: 2},
		{name: "a resolved file that walks out", message: strings.Replace(good, `resolved "notes.md"`, `resolved "../notes.md"`, 1), unresolved: 2},
		{name: "a resolved file with no path", message: strings.Replace(good, `resolved "notes.md"`, `resolved ""`, 1), unresolved: 2},
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

// TestWriteResolvedWritesWhatAResolveLeaves: a merged version whose entries
// a resolve changed is written as the tree they make, wrapped in the
// upstream directory: a file replaced, one deleted, a directory left with
// nothing dropped with it, a symlink kept and a file added in a directory
// that did not exist; a directory nothing changed is kept as git stored
// it, and every id git answers with is the one computed in process.
func TestWriteResolvedWritesWhatAResolveLeaves(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	r, gitDir := newRepo(t)
	base := skillTree(t, ctx, r, gitDir, map[string]string{
		"SKILL.md": "---\nname: alpha\n---\n", "notes.md": "notes\n", "gone/only.md": "only\n", "kept/a.md": "a\n",
	}, "run")
	entries := map[string]source.TreeEntry{}
	for _, e := range base.entries {
		entries[e.Path] = e
	}
	blob := func(body string) string {
		t.Helper()
		oid, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(body), "hash-object", "-t", "blob", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(oid)
	}
	var changed []source.TreeEntry
	for _, e := range base.entries {
		switch e.Path {
		case "notes.md":
			e.OID = blob("notes, resolved\n")
		case "gone", "gone/only.md":
			continue
		}
		changed = append(changed, e)
	}
	changed = append(changed, source.TreeEntry{Path: "added", Mode: source.DirMode},
		source.TreeEntry{Path: "added/new.md", Mode: source.ExecutableMode, OID: blob("new\n")})

	root, err := WriteResolved(ctx, r, gitDir, "alpha-dir", Base{Tree: base.tree, Entries: changed})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Isolated(ctx, gitDir, "ls-tree", "-r", root, "--format=%(objectmode) %(path)")
	if err != nil {
		t.Fatal(err)
	}
	if want := "100644 alpha-dir/SKILL.md\n100755 alpha-dir/added/new.md\n100644 alpha-dir/kept/a.md\n100644 alpha-dir/notes.md\n120000 alpha-dir/run"; out != want {
		t.Errorf("the tree holds\n%s\nwant\n%s", out, want)
	}
	kept, err := r.Isolated(ctx, gitDir, "rev-parse", root+":alpha-dir/kept")
	if err != nil || kept != entries["kept"].OID {
		t.Errorf("the directory nothing changed is %s, %v; want %s", kept, err, entries["kept"].OID)
	}
	if got, err := r.Isolated(ctx, gitDir, "cat-file", "blob", root+":alpha-dir/notes.md"); err != nil || got != "notes, resolved" {
		t.Errorf("notes.md holds %q, %v", got, err)
	}
	if _, err := WriteResolved(ctx, r, gitDir, "alpha-dir", Base{Tree: base.tree}); err == nil {
		t.Error("a version with no file at all was written")
	}
}

// TestReadImportReadsTheLineageOfACommit: the lineage an import commit
// carries is read back from the commit alone, and a commit that is no
// import commit is refused.
func TestReadImportReadsTheLineageOfACommit(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	r, gitDir := newRepo(t)
	imp := Import{Source: "https://github.com/example/skills", Path: "skills/alpha-dir", Commit: commitID, Hash: hashID}
	base := skillTree(t, ctx, r, gitDir, map[string]string{"SKILL.md": "---\nname: alpha\n---\n"}, "")
	rec := importCommit(t, ctx, r, gitDir, imp, imp.Dir(), base.tree, "1700000000 +0000")
	got, err := ReadImport(ctx, r, gitDir, rec.Commit)
	if err != nil || got != imp {
		t.Errorf("ReadImport = %+v, %v; want %+v", got, err, imp)
	}
	other, err := r.Isolated(ctx, gitDir, "commit-tree", rec.Tree, "-m", "no lineage")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadImport(ctx, r, gitDir, other); !errors.Is(err, ErrTrailer) {
		t.Errorf("a commit with no lineage reads as %v", err)
	}
}
