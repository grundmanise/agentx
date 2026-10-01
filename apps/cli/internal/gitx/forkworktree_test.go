package gitx

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

func TestParseWorktrees(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		out  string
		want []Worktree
	}{
		{"bare", "worktree /a.git\x00bare\x00\x00", []Worktree{{Path: "/a.git", Bare: true}}},
		{"on a branch, locked with a reason",
			"worktree /w/pdf\x00HEAD abc\x00branch refs/heads/skills/pdf\x00locked agentx fork\x00\x00",
			[]Worktree{{Path: "/w/pdf", Head: "abc", Branch: "refs/heads/skills/pdf", Locked: true, LockReason: "agentx fork"}}},
		{"detached, locked without a reason, prunable",
			"worktree /m/pdf\x00HEAD def\x00detached\x00locked\x00prunable gitdir file points to non-existent location\x00\x00",
			[]Worktree{{Path: "/m/pdf", Head: "def", Detached: true, Locked: true, Prunable: true}}},
		{"two", "worktree /a.git\x00bare\x00\x00worktree /w/x\x00HEAD 1\x00branch refs/heads/skills/x\x00\x00",
			[]Worktree{{Path: "/a.git", Bare: true}, {Path: "/w/x", Head: "1", Branch: "refs/heads/skills/x"}}},
		{"none", "", nil},
	} {
		got, err := ParseWorktrees(tc.out)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: ParseWorktrees = %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
	if _, err := ParseWorktrees("HEAD abc\x00"); err == nil {
		t.Error("an attribute outside a record parsed")
	}
}

// TestForkWorktreeAddsResetsAndComesBack adds a fork's worktree, locked and
// with nothing checked out, aligns its index, and adds it again once its
// directory was removed by hand, which leaves a locked registration git
// keeps through a prune.
func TestForkWorktreeAddsResetsAndComesBack(t *testing.T) {
	t.Parallel()
	r, gitDir, commit := checkoutRepo(t)
	ctx := context.Background()
	if _, err := r.Isolated(ctx, gitDir, "update-ref", "refs/heads/skills/notes", commit); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "worktrees", "notes")
	for _, round := range []string{"first", "after the directory was removed"} {
		if err := r.AddForkWorktree(ctx, gitDir, path, "skills/notes"); err != nil {
			t.Fatalf("%s add: %v", round, err)
		}
		if entries, _ := os.ReadDir(path); len(entries) != 1 || entries[0].Name() != ".git" {
			t.Errorf("%s add checked out %d entries, want the .git file alone", round, len(entries))
		}
		list, err := r.Worktrees(ctx, gitDir)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, w := range list {
			if samePath(w.Path, path) {
				found = w.Branch == "refs/heads/skills/notes" && w.Locked && w.LockReason == ForkReason
			}
		}
		if !found {
			t.Errorf("%s add: the worktree is not listed on its branch, locked as a fork's: %+v", round, list)
		}
		if err := r.ResetIndex(ctx, path); err != nil {
			t.Fatalf("%s reset: %v", round, err)
		}
		if out, err := r.InCheckout(ctx, path, "ls-files"); err != nil || out != "notes.md\n" {
			t.Errorf("%s: the index holds %q, %v; want the tip's file", round, out, err)
		}
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.AddForkWorktree(ctx, gitDir, path, "skills/notes"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddForkWorktree(ctx, gitDir, filepath.Join(t.TempDir(), "other"), "skills/notes"); err == nil || !strings.Contains(err.Error(), "checked out") {
		t.Errorf("a branch checked out at an existing path was added again: %v", err)
	}
}

// TestRepairWorktreesNamesItsPaths: a repair always names the worktrees it
// repairs, and keeps their paths relative only where git can and the
// account repo is set to.
func TestRepairWorktreesNamesItsPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"*--version*) echo \"git version $VER\" ;;\n" +
		"*\"config --type=bool --get worktree.useRelativePaths\"*) [ -n \"$REL\" ] && echo true && exit 0; exit 1 ;;\n" +
		"*) echo \"$*\" >> \"$LOG\" ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tc := range []struct {
		version, relative string
		want              bool
	}{{"2.43.0", "1", false}, {"2.48.0", "", false}, {"2.48.1", "1", true}} {
		os.Remove(log)
		r := New(map[string]string{"PATH": dir, "VER": tc.version, "REL": tc.relative, "LOG": log}, false, func(string, ...any) {})
		if err := r.RepairWorktrees(ctx, "/a.git", nil); err == nil {
			t.Errorf("%s: a repair naming no worktree ran", tc.version)
		}
		if err := r.RepairWorktrees(ctx, "/a.git", []string{"/w/x"}); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(log)
		got := string(b)
		if !strings.Contains(got, "worktree repair") || !strings.HasSuffix(strings.TrimSpace(got), " /w/x") {
			t.Errorf("%s: ran %q, want a repair of /w/x", tc.version, got)
		}
		if strings.Contains(got, "--relative-paths") != tc.want {
			t.Errorf("%s with the setting %q: ran %q, relative paths want %v", tc.version, tc.relative, got, tc.want)
		}
	}
}

// TestCommitTreeAsWritesTheUsersIdentity reads the user's identity and
// excludes file in one read, a ~ in the latter expanded as git expands it,
// and writes a commit as them, dated when it is told.
func TestCommitTreeAsWritesTheUsersIdentity(t *testing.T) {
	t.Parallel()
	r, gitDir, commit := checkoutRepo(t)
	ctx := context.Background()
	home := r.env["HOME"]
	config := "[user]\n\tname = Ada <Lovelace>\n\temail = ada@example.com\n[core]\n\texcludesFile = ~/my-ignore\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	r.env["GIT_CONFIG_NOSYSTEM"] = "1"
	c := r.ReadUserConfig(ctx, gitDir)
	if c.Ident.Name != "Ada <Lovelace>" || c.Ident.Email != "ada@example.com" || c.ExcludesFile != filepath.Join(home, "my-ignore") {
		t.Fatalf("ReadUserConfig = %+v", c)
	}
	id := c.Ident.Sanitised()
	if id.Name != "Ada Lovelace" {
		t.Errorf("sanitised name = %q", id.Name)
	}
	tree, err := r.Isolated(ctx, gitDir, "rev-parse", commit+"^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	when := "1700000000 +0200"
	made, err := r.CommitTreeAs(ctx, gitDir, id, when, tree, []string{commit}, "two\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Isolated(ctx, gitDir, "log", "-1", "--format=%an <%ae>|%cn <%ce>|%ad|%P|%s", "--date=raw", made)
	if want := "Ada Lovelace <ada@example.com>|Ada Lovelace <ada@example.com>|" + when + "|" + commit + "|two"; err != nil || got != want {
		t.Errorf("the commit reads %q, %v; want %q", got, err, want)
	}
	if err := os.Remove(filepath.Join(home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}
	if c := r.ReadUserConfig(ctx, gitDir); c != (UserConfig{}) {
		t.Errorf("with nothing configured ReadUserConfig = %+v", c)
	}
}

// TestReplaceEntryKeepsWhatElseTheRootHolds writes a skill's file into a
// tree, then replaces its directory in a root that holds a file beside it,
// which stays, and in no root at all.
func TestReplaceEntryKeepsWhatElseTheRootHolds(t *testing.T) {
	t.Parallel()
	r, gitDir, commit := checkoutRepo(t)
	ctx := context.Background()
	root, err := r.EditTree(ctx, gitDir, "", []TreeEdit{{Path: "pdf/SKILL.md", Mode: treeid.FileMode, Content: []byte("one\r\n")}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.Isolated(ctx, gitDir, "cat-file", "blob", root+":pdf/SKILL.md"); err != nil || got != "one\r" {
		t.Errorf("the edited file holds %q, %v; want its bytes as written", got, err)
	}
	sub, err := r.Isolated(ctx, gitDir, "rev-parse", root+":pdf")
	if err != nil {
		t.Fatal(err)
	}
	alone, err := r.ReplaceEntry(ctx, gitDir, "", "pdf", sub)
	if err != nil || alone != root || alone != treeid.Wrap("pdf", sub) {
		t.Errorf("ReplaceEntry with no parent = %s, %v; want %s", alone, err, root)
	}
	beside, err := r.ReplaceEntry(ctx, gitDir, commit, "pdf", sub)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.Isolated(ctx, gitDir, "ls-tree", "--name-only", beside); err != nil || got != "notes.md\npdf" {
		t.Errorf("the root holds %q, %v; want the file beside the skill kept", got, err)
	}
}
