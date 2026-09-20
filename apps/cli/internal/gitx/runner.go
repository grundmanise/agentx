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
)

// ErrMissing is returned when no git executable is found in PATH.
var ErrMissing = errors.New("git not found in PATH")

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
	out, err := r.run(ctx, false, nil, "--version")
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
	out, err := r.run(ctx, true, nil, isolatedArgs(gitDir, args)...)
	return strings.TrimRight(out, "\n"), err
}

// IsolatedInput is Isolated with stdin fed to git, for the commands that
// read object ids from it. It returns stdout as is, since a batch of
// objects ends how it ends.
func (r *Runner) IsolatedInput(ctx context.Context, gitDir string, stdin io.Reader, args ...string) (string, error) {
	return r.run(ctx, true, stdin, isolatedArgs(gitDir, args)...)
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
	out, err := r.run(ctx, false, nil, append([]string{"--git-dir=" + gitDir}, args...)...)
	return strings.TrimRight(out, "\n"), err
}

// UserInput is User with stdin fed to git.
func (r *Runner) UserInput(ctx context.Context, gitDir string, stdin io.Reader, args ...string) (string, error) {
	return r.run(ctx, false, stdin, append([]string{"--git-dir=" + gitDir}, args...)...)
}

// run executes git with args; isolated false is the user's own environment,
// in which credential helpers, SSH configuration and URL rewrites apply.
// stdout is returned as is.
func (r *Runner) run(ctx context.Context, isolated bool, stdin io.Reader, args ...string) (string, error) {
	git, err := r.lookPath()
	if err != nil {
		return "", err
	}
	r.logf("git %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Env = r.childEnv(isolated)
	cmd.Stdin = stdin
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
// both fail instead of prompting.
func (r *Runner) childEnv(isolated bool) []string {
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
		for _, who := range []string{"AUTHOR", "COMMITTER"} {
			env["GIT_"+who+"_NAME"] = "agentx"
			env["GIT_"+who+"_EMAIL"] = "agentx@localhost"
			env["GIT_"+who+"_DATE"] = "946684800 +0000"
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
