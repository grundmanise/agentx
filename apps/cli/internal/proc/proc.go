// Package proc runs one command as a child process for the serve child's
// door: it hands over the lines the command writes as it writes them, and
// stops the command and everything it started when asked to.
package proc

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Grace is how long a stopped command has between SIGTERM and SIGKILL, the
// time the desktop app gives one.
const Grace = 2 * time.Second

// The streams a line is written on.
const (
	Stdout = "stdout"
	Stderr = "stderr"
)

// Command is one run.
type Command struct {
	Path string   // the executable
	Args []string // its arguments, the program's name left out
	Env  []string // its environment, as os/exec takes it

	Deadline time.Duration // stops the run once it passes; 0 for none
	Grace    time.Duration // between SIGTERM and SIGKILL; 0 for Grace
}

// Result is how a run ended.
type Result struct {
	ExitCode  *int // nil when a signal ended the command
	Cancelled bool // ctx was done before the command ended
	TimedOut  bool // the deadline passed before the command ended
}

// Run starts c and waits for it. line is given every line the command
// writes, without its newline, one call at a time, in the order the lines
// of one stream were written; a last line with no newline is given too.
//
// The command runs in a process group of its own. When ctx is done or the
// deadline passes, the group gets SIGTERM, and SIGKILL if it is still there
// after the grace period, so that what the command started ends with it.
// A process that left the group, as a git agentx runs unattended does, is
// not reached; once the command itself has ended, Run waits no longer than
// the grace period for such a process to let go of the command's output.
//
// The error is set only when the command could not be started.
func Run(ctx context.Context, c Command, line func(stream string, text []byte)) (Result, error) {
	grace := c.Grace
	if grace == 0 {
		grace = Grace
	}
	var mu sync.Mutex
	stdout := &lines{stream: Stdout, mu: &mu, line: line}
	stderr := &lines{stream: Stderr, mu: &mu, line: line}

	cmd := exec.Command(c.Path, c.Args...)
	cmd.Env = c.Env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = grace
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	pid := cmd.Process.Pid

	var res Result
	ended := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		var deadline <-chan time.Time
		if c.Deadline > 0 {
			t := time.NewTimer(c.Deadline)
			defer t.Stop()
			deadline = t.C
		}
		select {
		case <-ended:
			return
		case <-ctx.Done():
			res.Cancelled = true
		case <-deadline:
			res.TimedOut = true
		}
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		t := time.NewTimer(grace)
		defer t.Stop()
		select {
		case <-ended:
		case <-t.C:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()

	// Wait's error says again what the state says: how the command ended.
	_ = cmd.Wait()
	close(ended)
	<-stopped
	stdout.flush()
	stderr.flush()

	// A command a signal ended has no exit code.
	if code := cmd.ProcessState.ExitCode(); code >= 0 {
		res.ExitCode = &code
	}
	return res, nil
}

// lines splits what a command writes on one stream into lines.
type lines struct {
	stream string
	mu     *sync.Mutex // shared by both streams: one call of line at a time
	line   func(stream string, text []byte)
	rest   []byte // the start of a line whose newline has not been written yet
}

func (l *lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rest = append(l.rest, p...)
	for {
		i := bytes.IndexByte(l.rest, '\n')
		if i < 0 {
			break
		}
		l.line(l.stream, l.rest[:i])
		l.rest = l.rest[i+1:]
	}
	if len(l.rest) == 0 {
		l.rest = nil // lets go of a long line's buffer
	}
	return len(p), nil
}

// flush hands over a last line that ended without a newline.
func (l *lines) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.rest) > 0 {
		l.line(l.stream, l.rest)
		l.rest = nil
	}
}
