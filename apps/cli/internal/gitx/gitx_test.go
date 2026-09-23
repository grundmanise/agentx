package gitx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubGit puts a git script on a fresh PATH that prints its arguments on the
// first line and then its environment, so a test sees what git would see.
func stubGit(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho \"$@\"\n/usr/bin/env\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"PATH":            dir,
		"HOME":            "/home/someone",
		"GIT_AUTHOR_NAME": "Someone",
		"GIT_SSH_COMMAND": "ssh -i /home/someone/key",
	}
}

func lines(out string) map[string]bool {
	set := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		set[l] = true
	}
	return set
}

func expect(t *testing.T, what, out string, present []string, absent []string) {
	t.Helper()
	got := lines(out)
	for _, l := range present {
		if !got[l] {
			t.Errorf("%s: missing %q in\n%s", what, l, out)
		}
	}
	for _, l := range absent {
		if got[l] {
			t.Errorf("%s: unexpected %q in\n%s", what, l, out)
		}
	}
}

func TestEnvironments(t *testing.T) {
	t.Setenv("AGENTX_LEAK", "from the process")
	env := stubGit(t)
	logf := func(string, ...any) {}
	ctx := context.Background()

	out, err := New(env, false, logf).Isolated(ctx, "/repo.git", "commit")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, "isolated", out,
		[]string{
			"-c core.autocrlf=false -c commit.gpgsign=false -c core.hooksPath=/dev/null --git-dir=/repo.git commit",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_LAZY_FETCH=1",
			"GIT_AUTHOR_NAME=agentx", "GIT_AUTHOR_EMAIL=agentx@localhost", "GIT_AUTHOR_DATE=946684800 +0000",
			"GIT_COMMITTER_NAME=agentx", "GIT_COMMITTER_EMAIL=agentx@localhost", "GIT_COMMITTER_DATE=946684800 +0000",
			"HOME=/home/someone",
		},
		[]string{"GIT_AUTHOR_NAME=Someone", "GIT_SSH_COMMAND=ssh -i /home/someone/key", "AGENTX_LEAK=from the process", "GIT_TERMINAL_PROMPT=0"})

	out, err = New(env, false, logf).User(ctx, "/repo.git", "fetch", "origin")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, "user", out,
		[]string{"--git-dir=/repo.git fetch origin", "GIT_AUTHOR_NAME=Someone", "GIT_SSH_COMMAND=ssh -i /home/someone/key", "HOME=/home/someone"},
		[]string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_NO_LAZY_FETCH=1", "AGENTX_LEAK=from the process", "GIT_ASKPASS=/bin/false"})

	serve := New(env, true, logf)
	out, err = serve.run(ctx, call{}, "fetch")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, "serve user", out,
		[]string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -i /home/someone/key -o BatchMode=yes", "GIT_ASKPASS=/bin/false"},
		nil)
	out, err = serve.Isolated(ctx, "/repo.git", "commit")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, "serve isolated", out,
		[]string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "GIT_ASKPASS=/bin/false", "GIT_CONFIG_GLOBAL=/dev/null"},
		nil)
}

func TestVersion(t *testing.T) {
	tests := []struct {
		out     string
		want    string
		atFloor bool
		err     bool
	}{
		{"git version 2.43.0", "2.43.0", true, false},
		{"git version 2.39.3 (Apple Git-146)", "2.39.3", false, false},
		{"git version 2.100.0", "2.100.0", true, false},
		{"git version 2.4.1", "2.4.1", false, false},
		{"git version 2.40", "2.40.0", true, false},
		{"git version 2.45.2.windows.1", "2.45.2", true, false},
		{"git version", "", false, true},
		{"not git", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.out, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\necho '"+tt.out+"'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			r := New(map[string]string{"PATH": dir}, false, func(string, ...any) {})
			v, err := r.Version(context.Background())
			if (err != nil) != tt.err {
				t.Fatalf("err = %v, want error %v", err, tt.err)
			}
			if err != nil {
				return
			}
			if v.String() != tt.want {
				t.Errorf("version = %s, want %s", v, tt.want)
			}
			if v.AtLeast(2, 40) != tt.atFloor {
				t.Errorf("AtLeast(2, 40) = %v, want %v", !tt.atFloor, tt.atFloor)
			}
		})
	}
}

func TestMissingGit(t *testing.T) {
	r := New(map[string]string{"PATH": t.TempDir() + string(os.PathListSeparator) + t.TempDir()}, false, func(string, ...any) {})
	if _, err := r.Version(context.Background()); !errors.Is(err, ErrMissing) {
		t.Errorf("err = %v, want ErrMissing", err)
	}
}

func TestOpenAccountRepoCreatesOnceWithAgentxConfig(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}
	r := New(env, false, func(string, ...any) {})
	ctx := context.Background()

	gitDir, exists, err := CheckAccountRepo(ctx, r, home)
	if err != nil || exists {
		t.Fatalf("CheckAccountRepo before creation = %v, %v", exists, err)
	}
	if _, err := os.Stat(gitDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("CheckAccountRepo created %s: %v", gitDir, err)
	}

	gitDir, created, err := OpenAccountRepo(ctx, r, home)
	if err != nil || !created || gitDir != filepath.Join(home, "account.git") {
		t.Fatalf("OpenAccountRepo = %q, %v, %v", gitDir, created, err)
	}
	v, err := r.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"core.bare": "true", "gc.auto": "0", "core.logAllRefUpdates": "true", "merge.conflictStyle": "zdiff3"}
	if v.AtLeast(2, 48) {
		want["worktree.useRelativePaths"] = "true"
	}
	for key, value := range want {
		if got, err := r.Isolated(ctx, gitDir, "config", "--get", key); err != nil || got != value {
			t.Errorf("%s = %q, %v; want %q", key, got, err, value)
		}
	}

	if _, created, err := OpenAccountRepo(ctx, r, home); err != nil || created {
		t.Errorf("second OpenAccountRepo = created %v, %v", created, err)
	}
	if _, exists, err := CheckAccountRepo(ctx, r, home); err != nil || !exists {
		t.Errorf("CheckAccountRepo after creation = %v, %v", exists, err)
	}
}
