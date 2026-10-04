package gitx

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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
			"-c core.autocrlf=false -c commit.gpgsign=false -c core.hooksPath=/dev/null -c core.attributesFile=/dev/null --git-dir=/repo.git commit",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1", "GIT_NO_LAZY_FETCH=1",
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
		[]string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1", "GIT_NO_LAZY_FETCH=1", "AGENTX_LEAK=from the process", "GIT_ASKPASS=/bin/false", "SSH_ASKPASS_REQUIRE=never"})

	serve := New(env, true, logf)
	out, err = serve.run(ctx, call{}, "fetch")
	if err != nil {
		t.Fatal(err)
	}
	// The serve child never prompts, and leaves the user's SSH command as
	// it is: appending options to it would break a command that is not ssh
	// itself, and replacing it would drop a core.sshCommand of theirs.
	unattended := []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false", "SSH_ASKPASS=/bin/false", "SSH_ASKPASS_REQUIRE=never", "GCM_INTERACTIVE=never"}
	expect(t, "serve user", out, append([]string{"GIT_SSH_COMMAND=ssh -i /home/someone/key"}, unattended...), nil)
	out, err = serve.Isolated(ctx, "/repo.git", "commit")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, "serve isolated", out, append([]string{"GIT_CONFIG_GLOBAL=/dev/null"}, unattended...), nil)
	if strings.Contains(out, "GIT_SSH_COMMAND=") {
		t.Errorf("serve isolated: an SSH command was set:\n%s", out)
	}
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
	want := map[string]string{"core.bare": "true", "gc.auto": "0", "core.logAllRefUpdates": "true", "merge.conflictStyle": "zdiff3"}
	if v, err := r.Version(ctx); err == nil && v.AtLeast(2, 48) {
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

// TestIsolatedReadsNoAttributesOfTheUsers: an attributes file of the
// user's, the one git reads under XDG_CONFIG_HOME when no configuration
// names one, sets nothing for a path in the isolated environment, where a
// merge driver it named would decide how files merge; the user's own
// environment still reads it.
func TestIsolatedReadsNoAttributesOfTheUsers(t *testing.T) {
	t.Parallel()
	config := t.TempDir()
	if err := os.MkdirAll(filepath.Join(config, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "git", "attributes"), []byte("*.md merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir(), "XDG_CONFIG_HOME": config}
	r := New(env, false, func(string, ...any) {})
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "repo.git")
	if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Isolated(ctx, gitDir, "check-attr", "merge", "--", "notes.md"); err != nil || got != "notes.md: merge: unspecified" {
		t.Errorf("isolated check-attr = %q, %v; want the attribute unspecified", got, err)
	}
	if got, err := r.User(ctx, gitDir, "check-attr", "merge", "--", "notes.md"); err != nil || got != "notes.md: merge: union" {
		t.Errorf("user check-attr = %q, %v; want the user's attribute", got, err)
	}
}

// TestIsolatedStatusAnswersWithTheExitStatus: a git that exits with a
// status up to the bound the caller names answers with it and with what it
// wrote, as merge-tree does for a merge that conflicts; one that exits
// above it, or that is killed, is an error as any failure of Isolated is.
func TestIsolatedStatusAnswersWithTheExitStatus(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nfor last; do :; done\necho written\nif [ \"$last\" = kill ]; then kill -9 $$; fi\nexit $last\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(map[string]string{"PATH": dir}, false, func(string, ...any) {})
	ctx := context.Background()
	for _, c := range []struct {
		exit   string
		status int
		fails  bool
	}{
		{exit: "0"},
		{exit: "1", status: 1},
		{exit: "2", status: 2},
		{exit: "3", fails: true},
		{exit: "kill", fails: true},
	} {
		out, status, err := r.IsolatedStatus(ctx, "/repo.git", 2, "merge-tree", c.exit)
		switch {
		case c.fails && err == nil:
			t.Errorf("exit %s: no error, status %d", c.exit, status)
		case c.fails:
		case err != nil:
			t.Errorf("exit %s: %v", c.exit, err)
		case status != c.status || out != "written\n":
			t.Errorf("exit %s: status %d and %q, want %d and %q", c.exit, status, out, c.status, "written\n")
		}
	}
}

// TestAddCheckoutLocksWithTheReason: a checkout is added detached at the
// commit given and locked with the reason, which git worktree list shows,
// so that git's own pruning never takes it, and a registration whose
// directory is gone is pruned first.
func TestAddCheckoutLocksWithTheReason(t *testing.T) {
	t.Parallel()
	r, gitDir, commit := checkoutRepo(t)
	ctx := context.Background()
	stale := filepath.Join(t.TempDir(), "stale")
	if _, err := r.Isolated(ctx, gitDir, "worktree", "add", "--detach", stale, commit); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(stale); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "merges", "alpha")
	if err := r.AddCheckout(ctx, gitDir, path, commit, "a reason of its own"); err != nil {
		t.Fatal(err)
	}
	list, err := r.Isolated(ctx, gitDir, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if want := "HEAD " + commit + "\ndetached\nlocked a reason of its own"; !strings.Contains(list, want) {
		t.Errorf("worktree list says\n%s\nwant it to hold %q", list, want)
	}
	if strings.Contains(list, stale) {
		t.Errorf("worktree list still names the stale registration:\n%s", list)
	}
	if top, err := r.InCheckout(ctx, path, "rev-parse", "HEAD"); err != nil || strings.TrimSpace(top) != commit {
		t.Errorf("HEAD in the checkout = %q, %v; want %s", top, err, commit)
	}
}

// TestGitRunsFromADeletedDirectory: a process whose working directory was
// removed, a shell left in a merge checkout an update completed, still reads
// the repository it names. Not parallel: it changes the working directory.
func TestGitRunsFromADeletedDirectory(t *testing.T) {
	r, gitDir, commit := checkoutRepo(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if out, err := r.Isolated(context.Background(), gitDir, "rev-parse", commit); err != nil || strings.TrimSpace(out) != strings.TrimSpace(commit) {
		t.Errorf("rev-parse from a deleted directory = %q, %v; want %s", out, err, commit)
	}
}

// TestCancellingAnUnattendedGitStopsItsTransport: an unattended git runs in
// a session of its own, out of reach of a terminal's Ctrl-C, so a cancelled
// run has to stop what git started as well as git: an ssh waiting on a host
// that does not answer would otherwise outlive the run by minutes. The
// stand-in transport holds the write end of a FIFO, so the reader sees its
// end exactly when the transport is gone.
func TestCancellingAnUnattendedGitStopsItsTransport(t *testing.T) {
	t.Parallel()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	fifo, ready := filepath.Join(dir, "transport"), filepath.Join(dir, "ready")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	writeShim(t, filepath.Join(dir, "git"), "#!/bin/sh\n[ \"$1\" = --version ] && exit 0\n"+
		sleeper+" 300 > "+fifo+" &\necho $! > "+ready+"\nwait\n")
	r := New(map[string]string{"PATH": dir, "HOME": dir}, false, func(string, ...any) {})

	opened, ended := make(chan struct{}), make(chan error, 1)
	go func() {
		f, err := os.Open(fifo) // returns once the transport has opened its end
		close(opened)
		if err != nil {
			ended <- err
			return
		}
		defer f.Close()
		_, err = io.Copy(io.Discard, f)
		ended <- err
	}()
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan error, 1)
	go func() {
		_, err := r.run(ctx, call{unattended: true}, "fetch")
		ran <- err
	}()
	<-opened
	// The transport's pid, for a failure to clean up after, is written
	// once it has started.
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if info, err := os.Stat(ready); (err == nil && info.Size() > 0) || time.Now().After(deadline) {
			break
		}
	}
	cancel()
	if err := <-ran; err == nil {
		t.Error("a cancelled git answered no error")
	}
	select {
	case err := <-ended:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		if b, err := os.ReadFile(ready); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		t.Fatal("the transport git started outlived the cancelled run")
	}
}

// TestRemoveCheckoutOfALockedCheckout: a locked checkout is removed, its
// directory and its registration, and so is one whose directory is already
// gone; removing one git knows nothing of is no error.
func TestRemoveCheckoutOfALockedCheckout(t *testing.T) {
	t.Parallel()
	r, gitDir, commit := checkoutRepo(t)
	ctx := context.Background()
	for _, gone := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "alpha")
		if err := r.AddCheckout(ctx, gitDir, path, commit, "locked"); err != nil {
			t.Fatal(err)
		}
		if gone {
			if err := os.RemoveAll(path); err != nil {
				t.Fatal(err)
			}
		}
		if err := r.RemoveCheckout(ctx, gitDir, path); err != nil {
			t.Fatalf("directory gone %v: %v", gone, err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("directory gone %v: the checkout is still there: %v", gone, err)
		}
		if list, err := r.Isolated(ctx, gitDir, "worktree", "list", "--porcelain"); err != nil || strings.Contains(list, "locked") {
			t.Errorf("directory gone %v: worktree list says\n%s, %v", gone, list, err)
		}
		if err := r.RemoveCheckout(ctx, gitDir, path); err != nil {
			t.Errorf("directory gone %v: removing it again: %v", gone, err)
		}
	}
}

// checkoutRepo is a bare repository holding one commit, for a checkout of
// it to be added at.
func checkoutRepo(t *testing.T) (*Runner, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	r := New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(string, ...any) {})
	if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	blob, err := r.IsolatedInput(ctx, gitDir, strings.NewReader("notes\n"), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := r.IsolatedInput(ctx, gitDir, strings.NewReader("100644 blob "+strings.TrimSpace(blob)+"\tnotes.md\n"), "mktree")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := r.Isolated(ctx, gitDir, "commit-tree", strings.TrimSpace(tree), "-m", "one")
	if err != nil {
		t.Fatal(err)
	}
	return r, gitDir, commit
}

// writeShim writes a git shell script a parallel test is about to run and
// waits until it can be run. The suite forks in parallel with itself and a
// fork duplicates the write descriptor of a file being written, so running
// the script can fail with ETXTBSY until the child that inherited that
// descriptor execs; it is run with --version until it starts, which every
// script given answers at once.
func writeShim(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for {
		if err := exec.Command(path, "--version").Run(); !errors.Is(err, syscall.ETXTBSY) {
			return
		}
		runtime.Gosched()
	}
}

// TestChildEnv checks what one git process is given: the variables that
// locate a repository are dropped in both environments, so that a GIT_DIR
// of a hook or a shell agentx runs under reaches no git of agentx's, while
// the user's own configuration, credentials, SSH and proxy settings stay in
// the user environment, and a call's own variables apply on top of either.
func TestChildEnv(t *testing.T) {
	t.Parallel()
	user := map[string]string{
		"HOME":                             "/home/someone",
		"HTTPS_PROXY":                      "http://proxy:3128",
		"GIT_SSH_COMMAND":                  "ssh -i key",
		"GIT_CONFIG_GLOBAL":                "/home/someone/.gitconfig-work",
		"GIT_DIR":                          "/elsewhere/.git",
		"GIT_WORK_TREE":                    "/elsewhere",
		"GIT_INDEX_FILE":                   "/elsewhere/.git/index",
		"GIT_OBJECT_DIRECTORY":             "/elsewhere/.git/objects",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": "/shared/objects",
		"GIT_COMMON_DIR":                   "/elsewhere/.git",
		"GIT_NAMESPACE":                    "ns",
		"GIT_CEILING_DIRECTORIES":          "/home",
		"GIT_DISCOVERY_ACROSS_FILESYSTEM":  "1",
		"GIT_PREFIX":                       "sub/",
	}
	dropped := func(except ...string) []string {
		var keys []string
		for _, k := range repoLocation {
			if !slices.Contains(except, k) {
				keys = append(keys, k)
			}
		}
		return keys
	}
	for _, tc := range []struct {
		name   string
		c      call
		want   map[string]string
		absent []string
	}{
		{
			name:   "user environment",
			c:      call{},
			want:   map[string]string{"HOME": "/home/someone", "HTTPS_PROXY": "http://proxy:3128", "GIT_SSH_COMMAND": "ssh -i key", "GIT_CONFIG_GLOBAL": "/home/someone/.gitconfig-work"},
			absent: dropped(),
		},
		{
			name:   "user environment, a call's own index",
			c:      call{env: map[string]string{"GIT_INDEX_FILE": "/tmp/agentx/index", "GIT_NO_LAZY_FETCH": "1"}},
			want:   map[string]string{"GIT_INDEX_FILE": "/tmp/agentx/index", "GIT_NO_LAZY_FETCH": "1", "GIT_SSH_COMMAND": "ssh -i key"},
			absent: dropped("GIT_INDEX_FILE"),
		},
		{
			name:   "isolated, a call's own index and identity",
			c:      call{isolated: true, env: map[string]string{"GIT_INDEX_FILE": "/tmp/agentx/index", "GIT_AUTHOR_NAME": "Someone"}},
			want:   map[string]string{"GIT_INDEX_FILE": "/tmp/agentx/index", "GIT_AUTHOR_NAME": "Someone", "GIT_COMMITTER_NAME": IdentityName},
			absent: dropped("GIT_INDEX_FILE"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := map[string]string{}
			for _, kv := range New(user, false, func(string, ...any) {}).childEnv(tc.c, false) {
				k, v, _ := strings.Cut(kv, "=")
				if _, twice := got[k]; twice {
					t.Errorf("%s is set twice", k)
				}
				got[k] = v
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
			for _, k := range tc.absent {
				if v, ok := got[k]; ok {
					t.Errorf("%s = %q, want it dropped", k, v)
				}
			}
		})
	}
}
