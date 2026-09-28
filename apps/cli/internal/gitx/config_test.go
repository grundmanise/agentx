package gitx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestAddRemoteConfigWritesWhatGitConfigWrites: a remote added to the
// account repo in one write leaves the config byte for byte, mode and all,
// as the git config calls it replaces leave it, on the git of the machine
// the test runs on; and wherever it cannot be sure of that, it writes
// nothing and leaves the keys to git config.
func TestAddRemoteConfigWritesWhatGitConfigWrites(t *testing.T) {
	t.Parallel()
	r := realGit(t)
	ctx := context.Background()
	account := func() string {
		t.Helper()
		gitDir, _, err := OpenAccountRepo(ctx, r, t.TempDir())
		if err != nil {
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
	remote := func(id, url string) [][2]string {
		return [][2]string{
			{"url", url}, {"fetch", "+HEAD:refs/agentx/fetching/" + id}, {"tagOpt", "--no-tags"},
			{"promisor", "true"}, {"partialclonefilter", "blob:none"},
		}
	}
	remotes := []struct {
		name  string
		pairs [][2]string
	}{
		{"src-0123456789abcdef", remote("0123456789abcdef", "https://github.com/someone/skills.git")},
		{"src-fedcba9876543210", remote("fedcba9876543210", "file:///tmp/a%20path/skills")},
	}

	byGit, inOneWrite := account(), account()
	for _, rm := range remotes {
		for _, kv := range rm.pairs {
			if _, err := r.Isolated(ctx, byGit, "config", "remote."+rm.name+"."+kv[0], kv[1]); err != nil {
				t.Fatal(err)
			}
		}
		if !AddRemoteConfig(inOneWrite, rm.name, rm.pairs) {
			t.Fatalf("AddRemoteConfig of %s did not write", rm.name)
		}
		want, wantMode := read(byGit)
		got, gotMode := read(inOneWrite)
		if got != want || gotMode != wantMode {
			t.Fatalf("config with %s written in one go, %v:\n%s\nwant, as git config writes it, %v:\n%s", rm.name, gotMode, got, wantMode, want)
		}
	}
	if _, err := os.Stat(filepath.Join(inOneWrite, "config.lock")); !os.IsNotExist(err) {
		t.Errorf("config.lock left behind: %v", err)
	}

	// Each of these is git config's to write, and nothing changes here.
	held := account()
	if err := os.WriteFile(filepath.Join(held, "config.lock"), []byte("another writer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edited := account()
	if _, err := r.Isolated(ctx, edited, "config", "user.name", "someone"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		gitDir string
		remote string
		pairs  [][2]string
	}{
		{"a remote already there", inOneWrite, remotes[0].name, remotes[0].pairs},
		{"a config.lock another writer holds", held, remotes[0].name, remotes[0].pairs},
		{"a config agentx does not write", edited, remotes[0].name, remotes[0].pairs},
		{"a value git quotes", account(), remotes[0].name, remote("0123456789abcdef", "file:///tmp/a;b")},
		{"a value with a trailing space", account(), remotes[0].name, remote("0123456789abcdef", "file:///tmp/a ")},
		{"a key agentx does not write", account(), remotes[0].name, [][2]string{{"mirror", "true"}}},
		{"a remote name in capitals", account(), "SRC-0123", remotes[0].pairs},
	} {
		before, _ := read(c.gitDir)
		lock, _ := os.ReadFile(filepath.Join(c.gitDir, "config.lock"))
		if AddRemoteConfig(c.gitDir, c.remote, c.pairs) {
			t.Errorf("%s: AddRemoteConfig wrote", c.name)
		}
		if after, _ := read(c.gitDir); after != before {
			t.Errorf("%s: the config changed:\n%s", c.name, after)
		}
		if after, _ := os.ReadFile(filepath.Join(c.gitDir, "config.lock")); string(after) != string(lock) {
			t.Errorf("%s: config.lock went from %q to %q", c.name, lock, after)
		}
	}
}
