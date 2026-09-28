package gitx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlainlyBareNeverSaysMoreThanGit is the promise the account repo
// check rests on: whatever plainlyBare calls a bare repository, git calls
// one too. Each case changes a bare repository the way the files of a
// real one could differ, and git's own answer is asked every time. The
// repositories agentx makes and keeps are plain, so their check needs no
// git; the rest are left to git.
func TestPlainlyBareNeverSaysMoreThanGit(t *testing.T) {
	t.Parallel()
	r := realGit(t)
	ctx := context.Background()
	git := func(gitDir string, args ...string) string {
		t.Helper()
		out, err := r.Isolated(ctx, gitDir, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	editConfig := func(gitDir string, edit func(string) string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(gitDir, "config"))
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(gitDir, "config"), edit(string(b)))
	}
	// A source to fetch from with a filter, the way a source add does, so
	// that whatever git writes into the config then is part of the case.
	upstream := filepath.Join(t.TempDir(), "upstream.git")
	git(upstream, "init", "--bare", "--quiet", upstream)
	git(upstream, "config", "uploadpack.allowFilter", "true")
	blob := strings.TrimSpace(func() string {
		out, err := r.IsolatedInput(ctx, upstream, strings.NewReader("notes\n"), "hash-object", "-w", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}())
	tree, err := r.IsolatedInput(ctx, upstream, strings.NewReader("100644 blob "+blob+"\tSKILL.md\n"), "mktree")
	if err != nil {
		t.Fatal(err)
	}
	git(upstream, "update-ref", "refs/heads/main", git(upstream, "commit-tree", strings.TrimSpace(tree), "-m", "one"))
	git(upstream, "symbolic-ref", "HEAD", "refs/heads/main")

	for _, c := range []struct {
		name  string
		plain bool
		setup func(t *testing.T, gitDir string)
	}{
		{"as git init makes it", true, func(t *testing.T, gitDir string) {}},
		{"as agentx makes it", true, func(t *testing.T, gitDir string) {
			if err := configureNew(ctx, r, gitDir, accountConfig); err != nil {
				t.Fatal(err)
			}
		}},
		{"with a source remote fetched with a filter", true, func(t *testing.T, gitDir string) {
			for _, kv := range [][2]string{
				{"url", "file://" + upstream}, {"fetch", "+HEAD:refs/agentx/fetching/x"}, {"tagOpt", "--no-tags"},
				{"promisor", "true"}, {"partialclonefilter", "blob:none"},
			} {
				git(gitDir, "config", "remote.src-0123abcd."+kv[0], kv[1])
			}
			if _, err := r.User(ctx, gitDir, "fetch", "--quiet", "--filter=blob:none", "src-0123abcd", "+HEAD:refs/agentx/sources/x"); err != nil {
				t.Fatal(err)
			}
		}},
		{"with comments, blank lines and a section name in capitals, which git reads but never writes", false, func(t *testing.T, gitDir string) {
			editConfig(gitDir, func(s string) string {
				return "# written by hand\n\n" + strings.Replace(s, "[core]", "[CORE]\n  ; a comment", 1) + "\n"
			})
		}},
		{"a work tree's git directory", false, func(t *testing.T, gitDir string) {
			git(gitDir, "config", "core.bare", "false")
		}},
		{"core.bare said twice, false the last time", false, func(t *testing.T, gitDir string) {
			editConfig(gitDir, func(s string) string { return s + "[core]\n\tbare = false\n" })
		}},
		{"core.bare as git reads a key with no value", false, func(t *testing.T, gitDir string) {
			editConfig(gitDir, func(s string) string { return strings.Replace(s, "\tbare = true", "\tbare", 1) })
		}},
		{"core.bare in capitals", false, func(t *testing.T, gitDir string) {
			editConfig(gitDir, func(s string) string { return strings.Replace(s, "bare = true", "bare = TRUE", 1) })
		}},
		{"no repository format version", false, func(t *testing.T, gitDir string) {
			editConfig(gitDir, func(s string) string { return strings.Replace(s, "\trepositoryformatversion = 0\n", "", 1) })
		}},
		{"repository format version 1 with an extension", false, func(t *testing.T, gitDir string) {
			git(gitDir, "config", "core.repositoryformatversion", "1")
			git(gitDir, "config", "extensions.objectFormat", "sha1")
		}},
		{"an include", false, func(t *testing.T, gitDir string) {
			included := filepath.Join(t.TempDir(), "included")
			write(included, "[core]\n\tbare = false\n")
			git(gitDir, "config", "include.path", included)
		}},
		{"a work tree set", false, func(t *testing.T, gitDir string) {
			git(gitDir, "config", "core.worktree", t.TempDir())
		}},
		{"a core key git parses", false, func(t *testing.T, gitDir string) {
			git(gitDir, "config", "core.abbrev", "12")
		}},
		{"a section git reads", false, func(t *testing.T, gitDir string) {
			git(gitDir, "config", "user.name", "someone")
		}},
		{"a quoted value", false, func(t *testing.T, gitDir string) {
			git(gitDir, "config", "remote.src-0123abcd.url", "file:///a path;with a semicolon")
		}},
		{"a subsection of core", false, func(t *testing.T, gitDir string) {
			editConfig(gitDir, func(s string) string { return s + "[core \"x\"]\n\ty = z\n" })
		}},
		{"carriage returns", false, func(t *testing.T, gitDir string) {
			editConfig(gitDir, func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") })
		}},
		{"a detached HEAD", false, func(t *testing.T, gitDir string) {
			write(filepath.Join(gitDir, "HEAD"), strings.Repeat("0", 40)+"\n")
		}},
		{"a HEAD git cannot read", false, func(t *testing.T, gitDir string) {
			write(filepath.Join(gitDir, "HEAD"), "garbage\n")
		}},
		{"a symlinked HEAD", false, func(t *testing.T, gitDir string) {
			head := filepath.Join(gitDir, "HEAD")
			if err := os.Remove(head); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("refs/heads/master", head); err != nil {
				t.Fatal(err)
			}
		}},
		{"no objects", false, func(t *testing.T, gitDir string) {
			if err := os.RemoveAll(filepath.Join(gitDir, "objects")); err != nil {
				t.Fatal(err)
			}
		}},
		{"no refs", false, func(t *testing.T, gitDir string) {
			if err := os.RemoveAll(filepath.Join(gitDir, "refs")); err != nil {
				t.Fatal(err)
			}
		}},
		{"a common directory elsewhere", false, func(t *testing.T, gitDir string) {
			write(filepath.Join(gitDir, "commondir"), "..\n")
		}},
		{"no config", false, func(t *testing.T, gitDir string) {
			if err := os.Remove(filepath.Join(gitDir, "config")); err != nil {
				t.Fatal(err)
			}
		}},
		{"nothing at all", false, func(t *testing.T, gitDir string) {
			if err := os.RemoveAll(gitDir); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(gitDir, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			gitDir := filepath.Join(t.TempDir(), "account.git")
			git(gitDir, "init", "--bare", "--quiet", gitDir)
			c.setup(t, gitDir)
			plain := plainlyBare(gitDir)
			out, err := r.Isolated(ctx, gitDir, "rev-parse", "--is-bare-repository")
			if plain && (err != nil || out != "true") {
				t.Fatalf("plainly bare, but git answers %q, %v", out, err)
			}
			if plain != c.plain {
				t.Errorf("plainly bare = %v, want %v (git answers %q, %v)", plain, c.plain, out, err)
			}
		})
	}
}

// TestCheckAccountRepoAsksGitOnlyWhenItMust: an account repo as agentx
// makes it is checked without git, and one that is not plainly bare still
// gets git's own answer, word for word.
func TestCheckAccountRepoAsksGitOnlyWhenItMust(t *testing.T) {
	t.Parallel()
	r := realGit(t)
	ctx := context.Background()
	home := t.TempDir()
	gitDir, created, err := OpenAccountRepo(ctx, r, home)
	if err != nil || !created {
		t.Fatalf("OpenAccountRepo = %v, %v", created, err)
	}
	if _, exists, err := CheckAccountRepo(ctx, noGit(t), home); err != nil || !exists {
		t.Fatalf("CheckAccountRepo without git = %v, %v; want the repo found without asking git", exists, err)
	}

	if _, err := r.Isolated(ctx, gitDir, "config", "core.bare", "false"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CheckAccountRepo(ctx, noGit(t), home); !errors.Is(err, ErrMissing) {
		t.Errorf("CheckAccountRepo of a repo that is not plainly bare = %v, want it to ask git", err)
	}
	_, exists, err := CheckAccountRepo(ctx, r, home)
	if want := "account repo " + gitDir + ": not a bare repository"; !exists || err == nil || err.Error() != want {
		t.Errorf("CheckAccountRepo of a work tree's git directory = %v, %v; want %q", exists, err, want)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = CheckAccountRepo(ctx, r, home)
	if want := "account repo " + gitDir + ": git rev-parse: fatal: "; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("CheckAccountRepo of a broken HEAD = %v, want git's own error after %q", err, want)
	}
}
