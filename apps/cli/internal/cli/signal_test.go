package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// The variables the child process of the stop-signal tests reads: it runs
// one agentx command through Main, the seam a real process enters by, so
// the signals it answers are the ones the released binary answers.
const (
	signalChildEnv  = "AGENTX_TEST_SIGNAL_CHILD"
	signalChildArgs = "AGENTX_TEST_SIGNAL_ARGS"
)

// TestSignalChildProcess is not a test: it is the body of the process the
// tests below start and signal. It does nothing when the variable that
// marks that process is not set.
func TestSignalChildProcess(t *testing.T) {
	if os.Getenv(signalChildEnv) == "" {
		t.Skip("not the signal child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Main(strings.Fields(os.Getenv(signalChildArgs)), env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// stopRun is one agentx command to run in a child process and stop: the
// file whose appearance says the run has reached the call to stop it at,
// the signals to send it one after another, whether they go to the whole
// process group as a terminal's Ctrl-C does or to the process alone as a
// supervisor's does, and the command line.
type stopRun struct {
	ready string
	sigs  []syscall.Signal
	group bool
	args  []string
}

// signalled runs r and returns the exit code – negative for a process a
// signal killed, as exec reports it – and what the command wrote to stderr,
// which is where its text output goes. The child gets a process group of
// its own, so a group signal reaches it and its git and never the test
// binary that sent it.
func signalled(t *testing.T, h *harness, r stopRun) (int, string) {
	t.Helper()
	ready, sigs := r.ready, r.sigs
	child := exec.Command(os.Args[0], "-test.run=^TestSignalChildProcess$")
	child.Env = append(os.Environ(), signalChildEnv+"=1", signalChildArgs+"="+strings.Join(r.args, " "))
	for k, v := range h.env {
		child.Env = append(child.Env, k+"="+v)
	}
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if !waitFor(ready) {
		// The buffers belong to the goroutines exec copies with until Wait
		// returns, so the run is ended before anything it wrote is read.
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatalf("the run never reached %s:\n%s%s", filepath.Base(ready), stdout.String(), stderr.String())
	}
	for _, sig := range sigs {
		target := child.Process.Pid
		if r.group {
			target = -target // the whole process group, git included
		}
		if err := syscall.Kill(target, sig); err != nil {
			t.Fatal(err)
		}
		// Far enough apart that the second signal is answered as a second
		// one and not raced with the first.
		time.Sleep(200 * time.Millisecond)
	}
	err := child.Wait()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
	default:
		t.Fatal(err)
	}
	code := child.ProcessState.ExitCode() // -1 for a process a signal killed
	if status, ok := child.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		code = -int(status.Signal())
	}
	return code, stderr.String()
}

// waitFor blocks until path holds something, so a test signals a run that
// has reached the call it means to stop rather than one still starting. The
// file is written and read by different processes, so an empty one is a
// write still in flight and not an answer.
func waitFor(path string) bool {
	deadline := time.Now().Add(30 * time.Second)
	for {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// hangingGit puts a git on the harness PATH that never answers the
// subcommand it is given: it writes its own process id to ready and then
// becomes a sleep, so the test can both wait for the run to reach that call
// and check afterwards whether the child was torn down or orphaned. Every
// other call goes to the real git.
func hangingGit(t *testing.T, h *harness, sub, ready string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// The stub is alone on the harness PATH, so the script reaches nothing
	// but shell builtins and the absolute paths written into it.
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" = `+sub+` ] && [ ! -s `+ready+` ]; then
		echo $$ > `+ready+`
		exec `+sleeper+` 300
	fi
done
exec `+real+` "$@"
`)
}

// TestASignalStopsAFetchAndTearsDownItsGit is the question a long fetch
// raises: a run that is waiting on the network has to answer a Ctrl-C at
// once, say so, and take its git child with it. Before there was a handler
// the process died where it stood, printing nothing, and the git it had
// started was reparented to init and went on fetching.
func TestASignalStopsAFetchAndTearsDownItsGit(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)

	ready := filepath.Join(t.TempDir(), "fetching")
	hangingGit(t, h, "fetch", ready)
	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM},
		args: []string{"source", "fetch", "--all", "--color", "off"}})

	equal(t, "the exit code of an interrupted fetch", code, exitInterrupted.exit)
	if !strings.Contains(stderr, "error: interrupted") {
		t.Errorf("the run said nothing about the stop:\n%s", stderr)
	}
	if !strings.Contains(stderr, "hint: ") {
		t.Errorf("the stop came with no hint:\n%s", stderr)
	}
	gitPID := readPID(t, ready)
	if err := syscall.Kill(gitPID, 0); err == nil {
		_ = syscall.Kill(gitPID, syscall.SIGKILL) // do not leave it behind either
		t.Errorf("the git child %d outlived the run it belonged to", gitPID)
	} else if !errors.Is(err, syscall.ESRCH) {
		t.Errorf("git %d: %v", gitPID, err)
	}
}

// TestASignalDuringAnAddStillTakesTheRemoteBack: `source add` writes the
// remote under one hold of the lock and the settings entry under another,
// and takes the remote back when it cannot reach the entry, because a
// remote no settings entry names is one nothing will ever name again. A
// stop may not be the one way to defeat that. The take-back runs on a
// context the stop does not reach, so a Ctrl-C during the fetch leaves the
// account repo as it found it and doctor has nothing to report.
func TestASignalDuringAnAddStillTakesTheRemoteBack(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, _, _ := h.standardSource(true)
	ready := filepath.Join(t.TempDir(), "fetching")
	hangingGit(t, h, "fetch", ready)

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM},
		args: []string{"source", "add", s.url, "--color", "off"}})

	equal(t, "the exit code of an interrupted add", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	// git config exits non-zero when nothing matches, which is the answer
	// this test wants, so the error is the empty case and not a failure.
	if left, err := h.accountGitErr("config", "--get-regexp", `^remote\.src-`); err == nil && left != "" {
		t.Errorf("the stopped add left its remote behind:\n%s", left)
	}
	rows, _ := doctorRows(t, h.events(h.mustRun("--json", "doctor").stdout))
	equal(t, "source_remotes.status after the stopped add", rows["source_remotes"]["status"], "ok")
	killLeftover(t, ready)
}

// TestASecondSignalKillsTheRun is the way out of a run that will not stop,
// and it is stopped here at the one place a first signal deliberately does
// not reach: the ref step of a journal already on disk, which runs on a
// context the stop is taken off. The first signal cancels the run and the
// update-ref goes on regardless; the second restores the default and raises
// it again, so the process dies exactly as it did before there was a
// handler. That is the crash the mutation journal is built for, and this
// test holds the line that the handler never takes the escape away.
func TestASecondSignalKillsTheRun(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	ready := filepath.Join(t.TempDir(), "updating")
	hangingGit(t, h, "update-ref", ready)

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGINT, syscall.SIGINT},
		args: []string{"skill", "add", s.url, "--skill", "alpha", "--color", "off"}})

	equal(t, "the exit status after a second signal", code, -int(syscall.SIGINT))
	if stderr != "" {
		t.Errorf("a killed run still reported something:\n%s", stderr)
	}
	// It was killed inside the journal, which is what the first signal let
	// it stay in, and the journal is what the next command recovers from.
	equal(t, "journals left by the killed run", journalCount(t, h), 1)
	killLeftover(t, ready)
}

// TestASignalDuringTheJournalLetsTheMutationFinish is the whole point of
// stopping between the steps of a mutation rather than inside one. The git
// wrapper signals the install just as the journal's ref step reaches git:
// the journal is on disk by then, so a stop that cancelled it would leave
// the machine with a branch and no library directory and the next command
// with a recovery. The mutation runs to the end instead, on a context the
// stop does not reach, and only then does the run answer for the stop.
func TestASignalDuringTheJournalLetsTheMutationFinish(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ready, done := filepath.Join(dir, "signalled"), filepath.Join(dir, "updated")
	// The wrapper stops the run that started it, its parent, as the
	// journal's refs go in, then hands that same update-ref to the real git.
	// It does so once: the marker keeps a later update-ref, the run's own or
	// another command's, from being signalled too.
	stubGit(t, h, `#!/bin/sh
for arg in "$@"; do
	if [ "$arg" = update-ref ] && [ ! -s `+ready+` ]; then
		echo $PPID > `+ready+`
		kill -TERM $PPID
		`+real+` "$@"
		status=$?
		echo applied > `+done+`
		exit $status
	fi
done
exec `+real+` "$@"
`)
	code, stderr := signalled(t, h, stopRun{ready: ready,
		args: []string{"skill", "add", s.url, "--skill", "alpha", "--color", "off"}})

	if _, err := os.Stat(done); err != nil {
		t.Fatalf("the journal's ref step never ran, so the test did not stop the run where it means to:\n%s", stderr)
	}
	// The stop arrived after the last thing that could fail, so the run
	// answers for what it did: the whole of it. An interrupted run reports
	// a stop, never a failure it did not have.
	equal(t, "the exit code of an install the stop arrived too late for", code, exitOK.exit)

	// The mutation is whole: branch, library directory and placements, and
	// no journal left for the next command to finish.
	if head := h.accountGit("for-each-ref", "--format=%(objectname)", "refs/heads/managed/alpha"); head == "" {
		t.Error("the import branch was not written")
	}
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("the library directory was not published: %v", err)
	}
	for _, rel := range []string{".claude/skills/alpha", ".cursor/skills/alpha"} {
		if _, err := os.Readlink(filepath.Join(h.home, rel)); err != nil {
			t.Errorf("the placement %s was not made: %v", rel, err)
		}
	}
	equal(t, "journals left by the interrupted install", journalCount(t, h), 0)
	// And the run still took its own staging refs away, which it does on a
	// context the stop does not reach: leaving them would hand the next
	// doctor a warning for a run that had already decided about them.
	if left := h.accountGit("for-each-ref", "--format=%(refname)", lineage.ImportingPrefix); left != "" {
		t.Errorf("the interrupted install left its staging refs behind:\n%s", left)
	}
}

// killLeftover takes away the git the test left hanging, for a run that was
// killed before it could.
func killLeftover(t *testing.T, ready string) {
	t.Helper()
	if pid := readPID(t, ready); syscall.Kill(pid, 0) == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// TestATerminalCtrlCDuringTheJournalIsTheCrashBoundary is the one case the
// shielded context cannot cover, and is not meant to. A terminal signals
// the whole foreground process group, so the git of a ref step is killed
// directly whatever context agentx ran it on. Nothing is lost by that: the
// ref updates of a journal are one transaction that commits or does not,
// the journal stays on disk, the run still reports the stop, and the next
// command that takes the lock finishes the install. It is the crash
// boundary the journal is built for, reached by a different route.
func TestATerminalCtrlCDuringTheJournalIsTheCrashBoundary(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	ready := filepath.Join(t.TempDir(), "updating")
	hangingGit(t, h, "update-ref", ready)

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGINT}, group: true,
		args: []string{"skill", "add", s.url, "--skill", "alpha", "--color", "off"}})

	equal(t, "the exit code of a run a terminal stopped", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")

	// The transaction did not commit and the journal says what is left.
	if head := h.accountGit("for-each-ref", "--format=%(objectname)", lineage.ManagedRef("alpha")); head != "" {
		t.Errorf("the ref transaction committed although its git was killed: %s", head)
	}
	equal(t, "journals left by the killed run", journalCount(t, h), 1)

	// And the next command that takes the lock finishes the install, which
	// is the guarantee the graceful stop was never allowed to weaken.
	h.mustRun("config", "set", "label", "recovered")
	equal(t, "journals after recovery", journalCount(t, h), 0)
	if head := h.accountGit("for-each-ref", "--format=%(objectname)", lineage.ManagedRef("alpha")); head == "" {
		t.Error("the import branch was not recovered")
	}
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("the library directory was not recovered: %v", err)
	}
	killLeftover(t, ready)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}
