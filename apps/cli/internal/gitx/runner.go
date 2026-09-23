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
	return append([]string{
		"-c", "core.autocrlf=false",
		"-c", "commit.gpgsign=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"--git-dir=" + gitDir,
	}, args...)
}

// User runs git against gitDir in the user's own environment, in which
// credential helpers, SSH configuration and URL rewrites apply. Network
// commands use it. It returns stdout without its trailing newline.
func (r *Runner) User(ctx context.Context, gitDir string, args ...string) (string, error) {
	out, err := r.run(ctx, call{}, append([]string{"--git-dir=" + gitDir}, args...)...)
	return strings.TrimRight(out, "\n"), err
}

// UserInput is User with stdin fed to git.
func (r *Runner) UserInput(ctx context.Context, gitDir string, stdin io.Reader, args ...string) (string, error) {
	return r.run(ctx, call{stdin: stdin}, append([]string{"--git-dir=" + gitDir}, args...)...)
}

// call is what one git process needs besides its arguments: the
// environment to build, the dates to fix in it and what to feed its stdin.
type call struct {
	isolated bool
	dates    string // GIT_AUTHOR_DATE and GIT_COMMITTER_DATE, isolated only; the fixed date stands when empty
	stdin    io.Reader
}

// run executes git with args; a call that is not isolated runs in the
// user's own environment, in which credential helpers, SSH configuration
// and URL rewrites apply. stdout is returned as is.
func (r *Runner) run(ctx context.Context, c call, args ...string) (string, error) {
	git, err := r.lookPath()
	if err != nil {
		return "", err
	}
	r.logf("git %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Env = r.childEnv(c.isolated, c.dates)
	cmd.Stdin = c.stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if stderr.Len() > 0 {
		r.logf("git stderr: %s", strings.TrimRight(stderr.String(), "\n"))
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", subcommand(args), detail)
	}
	return stdout.String(), nil
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
// fixes configuration, author and committer, and forbids the lazy fetch of
// a missing object (git 2.45 and newer honour the variable), since it never
// touches the network; the user environment is the map as is. Under serve,
// both fail instead of prompting. dates, when it is not empty, replaces the
// fixed author and committer dates for this one process.
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
