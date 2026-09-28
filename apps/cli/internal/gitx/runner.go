// Package gitx runs the system git as a subprocess in the two environments
// agentx needs, and holds what agentx keeps in git: the account repo.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ErrMissing is returned when no git executable is found in PATH.
var ErrMissing = errors.New("git not found in PATH")

// FixedDate is the author and committer date of every isolated call that
// does not name one, so that a commit agentx writes has the same id on
// every machine.
const FixedDate = "946684800 +0000"

// The author and committer of every commit agentx writes. A stream that
// names them itself, as fast-import does, writes Identity, which is the
// same two fields in one git identity line: a commit must come out the same
// whichever of the two writes it.
const (
	IdentityName  = "agentx"
	IdentityEmail = "agentx@localhost"
	Identity      = IdentityName + " <" + IdentityEmail + ">"
)

// Runner runs git. Every call names its git directory explicitly and builds
// the child environment from the environment map it was given; nothing is
// inherited from the process.
type Runner struct {
	env     map[string]string
	serve   bool // the serve child: git must fail instead of prompting
	logf    func(format string, args ...any)
	version *Version // cached after the first Version call
	// stopped records that a stop signal killed one of this runner's git
	// children without agentx having cancelled it, which is what a terminal
	// does: Ctrl-C goes to the whole foreground process group. It is the
	// run's evidence that a signal is on its way to the run itself. Calls
	// run in parallel, so it is atomic.
	stopped atomic.Bool
}

// New returns a runner over env. Under serve every git call has terminal
// prompts disabled, SSH in batch mode and an askpass that fails. logf receives
// every command line and its stderr.
func New(env map[string]string, serve bool, logf func(format string, args ...any)) *Runner {
	return &Runner{env: env, serve: serve, logf: logf}
}

// Version is a git version by its numeric components.
type Version struct {
	Major, Minor, Patch int
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// AtLeast reports whether v is major.minor or newer.
func (v Version) AtLeast(major, minor int) bool {
	return v.Major > major || v.Major == major && v.Minor >= minor
}

// Version runs git --version once and caches the answer.
func (r *Runner) Version(ctx context.Context) (Version, error) {
	if r.version != nil {
		return *r.version, nil
	}
	out, err := r.run(ctx, call{}, "--version")
	if err != nil {
		return Version{}, err
	}
	v, err := parseVersion(strings.TrimRight(out, "\n"))
	if err != nil {
		return Version{}, err
	}
	r.version = &v
	return v, nil
}

// parseVersion reads "git version 2.43.0", tolerating a suffix such as
// " (Apple Git-146)" or a fourth component.
func parseVersion(out string) (Version, error) {
	fields := strings.Fields(strings.TrimPrefix(out, "git version "))
	var parts []string
	if len(fields) > 0 {
		parts = strings.Split(fields[0], ".")
	}
	var v Version
	nums := []*int{&v.Major, &v.Minor, &v.Patch}
	if len(parts) < 2 {
		return v, fmt.Errorf("cannot parse git version %q", out)
	}
	for i, p := range parts {
		if i == len(nums) {
			break
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, fmt.Errorf("cannot parse git version %q", out)
		}
		*nums[i] = n
	}
	return v, nil
}

// Isolated runs git against gitDir in the isolated environment: no global or
// system configuration, a fixed author and committer, no lazy fetch of
// missing objects, and the settings agentx needs. Object writes and merges
// use it so that ids match on every machine. It returns stdout without its
// trailing newline.
func (r *Runner) Isolated(ctx context.Context, gitDir string, args ...string) (string, error) {
	out, err := r.run(ctx, call{isolated: true}, isolatedArgs(gitDir, args)...)
	return strings.TrimRight(out, "\n"), err
}

// IsolatedAt is Isolated with the author and committer dates of this one
// call set to when, an epoch with an offset such as "1700000000 +0000".
// An import commit takes the upstream committer's time this way, so that
// the commit is a pure function of the version it holds; everything else
// the isolated environment fixes still holds.
func (r *Runner) IsolatedAt(ctx context.Context, gitDir, when string, args ...string) (string, error) {
	out, err := r.run(ctx, call{isolated: true, dates: when}, isolatedArgs(gitDir, args)...)
	return strings.TrimRight(out, "\n"), err
}

// IsolatedInput is Isolated with stdin fed to git, for the commands that
// read object ids from it. It returns stdout as is, since a batch of
// objects ends how it ends.
func (r *Runner) IsolatedInput(ctx context.Context, gitDir string, stdin io.Reader, args ...string) (string, error) {
	return r.run(ctx, call{isolated: true, stdin: stdin}, isolatedArgs(gitDir, args)...)
}

// IsolatedStatus is Isolated for a git whose exit status is part of its
// answer: merge-tree exits 1 for a merge that conflicts, having written the
// whole of its result to stdout first. An exit status from 1 to upTo is
// returned with stdout as is and no error; any other failure is an error as
// it is for Isolated, a status above upTo included, which is git's own way
// of saying it could not do the work at all.
func (r *Runner) IsolatedStatus(ctx context.Context, gitDir string, upTo int, args ...string) (string, int, error) {
	return r.runStatus(ctx, call{isolated: true}, upTo, isolatedArgs(gitDir, args)...)
}

// InCheckout runs git in the isolated environment inside dir, a linked
// worktree of a repository, as git runs in any checkout: dir is git's
// working directory and no --git-dir is passed, so git finds the
// worktree's own git directory through its .git file. The worktree records
// file modes and symlinks as they are, whatever the repository's own
// configuration says of them. It returns stdout as is.
func (r *Runner) InCheckout(ctx context.Context, dir string, args ...string) (string, error) {
	return r.InCheckoutInput(ctx, dir, nil, args...)
}

// InCheckoutInput is InCheckout with stdin fed to git.
func (r *Runner) InCheckoutInput(ctx context.Context, dir string, stdin io.Reader, args ...string) (string, error) {
	full := append(isolatedConfig(), "-c", "core.fileMode=true", "-c", "core.symlinks=true")
	return r.run(ctx, call{isolated: true, dir: dir, stdin: stdin}, append(full, args...)...)
}

// AddCheckout adds a linked worktree of the repository at gitDir at path,
// detached at commit and locked with reason, so that git's own pruning of
// worktrees never takes it, whatever becomes of its directory.
func (r *Runner) AddCheckout(ctx context.Context, gitDir, path, commit, reason string) error {
	_, err := r.Isolated(ctx, gitDir, "worktree", "add", "--detach", "--lock", "--reason", reason, path, commit)
	return err
}

// RemoveCheckout removes the linked worktree at path, its directory and its
// registration, locked or not and whether its directory is still there or
// not: the second -f is what removes a locked one. A path git knows no
// worktree at has nothing to remove. It never prunes, which would take
// other worktrees too.
func (r *Runner) RemoveCheckout(ctx context.Context, gitDir, path string) error {
	_, err := r.Isolated(ctx, gitDir, "worktree", "remove", "-f", "-f", path)
	if err != nil && strings.Contains(err.Error(), "is not a working tree") {
		return nil
	}
	return err
}

// Workers is how many git processes agentx runs at once. A read is mostly
// the cost of starting git and reading its answer, so a few in flight hide
// each other's latency, while the bound keeps a command from spawning a
// process per thing it reads. It is the count the fetches of a source and
// the handshakes of a scan use, for the same reason.
const Workers = 4

// IsolatedAll runs calls, which must be independent read-only isolated
// calls against gitDir, at most Workers at a time, and returns their
// outputs in the order the calls were given: the work is parallel, what a
// caller reads from it is not. The first failing call by that order is the
// error, and every call still runs, so one failure leaves no goroutine
// behind. Runner keeps no state per call, so the processes share nothing
// but the repository, which they only read.
func (r *Runner) IsolatedAll(ctx context.Context, gitDir string, calls [][]string) ([]string, error) {
	out := make([]string, len(calls))
	errs := make([]error, len(calls))
	slots := make(chan struct{}, Workers)
	var wg sync.WaitGroup
	for i, args := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			out[i], errs[i] = r.Isolated(ctx, gitDir, args...)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func isolatedArgs(gitDir string, args []string) []string {
	return append(append(isolatedConfig(), "--git-dir="+gitDir), args...)
}

// isolatedConfig is the configuration every isolated call sets on its
// command line. Attributes come from nowhere but the files git reads them
// from in a work tree: the user's own attributes file is never read.
func isolatedConfig() []string {
	return []string{
		"-c", "core.autocrlf=false",
		"-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.attributesFile=" + os.DevNull,
	}
}

// User runs git against gitDir in the user's own environment, in which
// credential helpers, SSH configuration and URL rewrites apply. Network
// commands, and the read of the user's core.excludesFile, use it. It
// returns stdout without its trailing newline.
func (r *Runner) User(ctx context.Context, gitDir string, args ...string) (string, error) {
	out, err := r.run(ctx, call{}, append([]string{"--git-dir=" + gitDir}, args...)...)
	return strings.TrimRight(out, "\n"), err
}

// UserInput is User with stdin fed to git.
func (r *Runner) UserInput(ctx context.Context, gitDir string, stdin io.Reader, args ...string) (string, error) {
	return r.run(ctx, call{stdin: stdin}, append([]string{"--git-dir=" + gitDir}, args...)...)
}

// call is what one git process needs besides its arguments: the
// environment to build, the dates to fix in it, what to feed its stdin,
// variables of its own and the directory it runs in.
type call struct {
	isolated bool
	dates    string // GIT_AUTHOR_DATE and GIT_COMMITTER_DATE, isolated only; the fixed date stands when empty
	stdin    io.Reader
	env      map[string]string // set on top of the environment built, such as GIT_INDEX_FILE
	dir      string            // git's working directory; "" keeps the process's
}

// run executes git with args; a call that is not isolated runs in the
// user's own environment, in which credential helpers, SSH configuration
// and URL rewrites apply. stdout is returned as is.
func (r *Runner) run(ctx context.Context, c call, args ...string) (string, error) {
	out, _, err := r.runStatus(ctx, c, 0, args...)
	return out, err
}

// runStatus is run that answers an exit status from 1 to upTo with stdout
// and that status rather than with an error, for a git that exits non-zero
// to say what it found. A git killed by a signal has no such status and is
// an error whatever upTo is.
func (r *Runner) runStatus(ctx context.Context, c call, upTo int, args ...string) (string, int, error) {
	git, err := r.lookPath()
	if err != nil {
		return "", 0, err
	}
	r.logf("git %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Env = r.childEnv(c.isolated, c.dates)
	for k, v := range c.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Dir = c.dir
	cmd.Stdin = c.stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// A cancelled context kills git, but git's own children – the transport
	// of a fetch, the ssh it starts – hold the pipes agentx reads its output
	// through, and waiting for those to close is waiting for a network call
	// nobody is reading any more. WaitDelay closes them instead, so that a
	// run stopped during a fetch ends now rather than when the far end times
	// out. It never truncates the output of a git that finished: by then the
	// process has exited and only a child it left behind can still hold a
	// pipe open.
	cmd.WaitDelay = waitDelay
	err = cmd.Run()
	if stderr.Len() > 0 {
		r.logf("git stderr: %s", strings.TrimRight(stderr.String(), "\n"))
	}
	if err != nil {
		if ctx.Err() != nil {
			// The child was killed because the run is stopping, so its own
			// report, "signal: killed", says nothing true about git.
			return "", 0, fmt.Errorf("git %s: interrupted", subcommand(args))
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 1 && exitErr.ExitCode() <= upTo {
			return stdout.String(), exitErr.ExitCode(), nil
		}
		if stoppedBySignal(err) {
			// Nothing here cancelled it, so the signal came from outside:
			// a terminal signalling the whole foreground process group,
			// which is about to signal this process too. Recording it lets
			// the run answer for the stop rather than for the git it killed.
			r.stopped.Store(true)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", 0, fmt.Errorf("git %s: %s", subcommand(args), detail)
	}
	return stdout.String(), 0, nil
}

// waitDelay is how long a git that has been killed, or has exited leaving a
// child of its own behind, may keep agentx waiting on its pipes.
const waitDelay = 2 * time.Second

// StoppedChild reports whether a stop signal killed one of this runner's
// git children that agentx did not cancel. A run reads it when it is
// failing, to tell a git that died of the user's Ctrl-C from one that
// failed on its own.
func (r *Runner) StoppedChild() bool { return r.stopped.Load() }

// stoppedBySignal reports whether the child was killed by a signal that
// asks a process to stop. Any other death, a crash included, is git's own
// answer and is reported as it stands.
func stoppedBySignal(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return false
	}
	switch status.Signal() {
	case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
		return true
	}
	return false
}

// subcommand is the first argument that is not a global option, so an error
// names "commit" rather than a -c setting or a --git-dir.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-c":
			i++
		case strings.HasPrefix(args[i], "-"):
		default:
			return args[i]
		}
	}
	return strings.Join(args, " ")
}

// lookPath finds git in the PATH of the environment map, never the process's.
func (r *Runner) lookPath() (string, error) {
	for _, dir := range filepath.SplitList(r.env["PATH"]) {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, "git")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", ErrMissing
}

// childEnv builds the environment of one git process from the environment
// map. The isolated environment drops every GIT_ variable of the user's,
// fixes configuration, author and committer, reads no attributes of the
// user's or the system's, so that no merge driver, filter or marker size
// of theirs changes what git writes (isolatedConfig names no attributes file
// in place of the one git reads under XDG_CONFIG_HOME when configuration
// names none, and GIT_ATTR_NOSYSTEM drops the system's), and forbids the
// lazy fetch of a missing object (git 2.45 and newer honour the variable),
// since it never touches the network; the user environment is the map as
// is. Under serve, both fail instead of prompting. dates, when it is not
// empty, replaces the fixed author and committer dates for this one
// process.
func (r *Runner) childEnv(isolated bool, dates string) []string {
	env := make(map[string]string, len(r.env)+12)
	for k, v := range r.env {
		if isolated && strings.HasPrefix(k, "GIT_") {
			continue
		}
		env[k] = v
	}
	if isolated {
		env["GIT_CONFIG_GLOBAL"] = os.DevNull
		env["GIT_CONFIG_NOSYSTEM"] = "1"
		env["GIT_ATTR_NOSYSTEM"] = "1"
		env["GIT_NO_LAZY_FETCH"] = "1"
		when := FixedDate
		if dates != "" {
			when = dates
		}
		for _, who := range []string{"AUTHOR", "COMMITTER"} {
			env["GIT_"+who+"_NAME"] = IdentityName
			env["GIT_"+who+"_EMAIL"] = IdentityEmail
			env["GIT_"+who+"_DATE"] = when
		}
	}
	if r.serve {
		ssh := env["GIT_SSH_COMMAND"]
		if ssh == "" {
			ssh = "ssh"
		}
		env["GIT_TERMINAL_PROMPT"] = "0"
		env["GIT_SSH_COMMAND"] = ssh + " -o BatchMode=yes"
		env["GIT_ASKPASS"] = "/bin/false"
	}
	list := make([]string, 0, len(env))
	for k, v := range env {
		list = append(list, k+"="+v)
	}
	return list
}
