package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// realGit is a runner over the git on PATH, with a home of its own.
func realGit(t *testing.T) *Runner {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	return New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(string, ...any) {})
}

// noGit is a runner that finds no git at all, so that whatever it answers
// was answered without one.
func noGit(t *testing.T) *Runner {
	return New(map[string]string{"PATH": t.TempDir()}, false, func(string, ...any) {})
}

// TestConfigureNewWritesWhatGitConfigWrites: the account repo gets its
// configuration in one write, and the file comes out byte for byte, mode
// and all, as the git config calls it replaces leave it, whatever the git
// init of this machine wrote first. A config that is no longer what git
// init wrote goes to git config.
func TestConfigureNewWritesWhatGitConfigWrites(t *testing.T) {
	t.Parallel()
	r := realGit(t)
	ctx := context.Background()
	fresh := func() string {
		t.Helper()
		gitDir := filepath.Join(t.TempDir(), "account.git")
		if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
			t.Fatal(err)
		}
		return gitDir
	}
	read := func(gitDir string) (string, os.FileMode) {
		t.Helper()
		path := filepath.Join(gitDir, "config")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b), info.Mode()
	}

	byGit := fresh()
	for _, kv := range accountConfig {
		if _, err := r.Isolated(ctx, byGit, "config", kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	inOneWrite := fresh()
	if err := configureNew(ctx, noGit(t), inOneWrite, accountConfig); err != nil {
		t.Fatalf("configureNew ran git: %v", err)
	}
	want, wantMode := read(byGit)
	got, gotMode := read(inOneWrite)
	if got != want || gotMode != wantMode {
		t.Errorf("config written in one go, %v:\n%s\nwant, as git config writes it, %v:\n%s", gotMode, got, wantMode, want)
	}

	// A key already there, a section git init did not write, a value that
	// needs quoting: each is git config's to write.
	for name, config := range map[string][][2]string{
		"a key git init wrote":  {{"core.bare", "true"}},
		"a value to quote":      {{"merge.conflictStyle", "a b"}},
		"a subsection":          {{"remote.x.url", "file:///x"}},
		"a section in capitals": {{"GC.auto", "0"}},
	} {
		if err := configureNew(ctx, noGit(t), fresh(), config); err == nil {
			t.Errorf("%s: configureNew wrote it without git", name)
		}
	}
	edited := fresh()
	if _, err := r.Isolated(ctx, edited, "config", "user.name", "someone"); err != nil {
		t.Fatal(err)
	}
	if err := configureNew(ctx, noGit(t), edited, accountConfig); err == nil {
		t.Error("configureNew wrote into a config git init did not write alone")
	}
	if err := configureNew(ctx, r, edited, accountConfig); err != nil {
		t.Fatal(err)
	}
	for _, kv := range append(accountConfig, [2]string{"user.name", "someone"}) {
		if got, err := r.Isolated(ctx, edited, "config", "--get", kv[0]); err != nil || got != kv[1] {
			t.Errorf("%s = %q, %v; want %q", kv[0], got, err, kv[1])
		}
	}
}
