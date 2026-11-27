// Package proc runs one command as a child process for the serve child's
// door: it hands over the lines the command writes as it writes them, and
// stops the command and everything it started when asked to.
package proc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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
// the grace period for such a process to let go of the command's output,
// and then hands over what the output still holds.
//
// The error is set only when the command could not be started.
func Run(ctx context.Context, c Command, line func(stream string, text []byte)) (Result, error) {
	grace := c.Grace
	if grace == 0 {
		grace = Grace
	}
	var mu sync.Mutex
	stdout, err := newOutput(&lines{stream: Stdout, mu: &mu, line: line})
	if err != nil {
		return Result{}, err
	}
	defer stdout.close()
	stderr, err := newOutput(&lines{stream: Stderr, mu: &mu, line: line})
	if err != nil {
		return Result{}, err
	}
	defer stderr.close()

	cmd := exec.Command(c.Path, c.Args...)
	cmd.Env = c.Env
	cmd.Stdout = stdout.w
	cmd.Stderr = stderr.w
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = cmd.Start()
	stdout.started()
	stderr.started()
	if err != nil {
		return Result{}, err
	}
	var reading sync.WaitGroup
	reading.Go(stdout.read)
	reading.Go(stderr.read)
	pid := cmd.Process.Pid

	var res Result
	ended := make(chan struct{})
	closed := make(chan struct{})
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
		case <-closed:
		case <-t.C:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()

	// Wait's error says again what the state says: how the command ended.
	_ = cmd.Wait()
	close(ended)
	stdout.end(grace)
	stderr.end(grace)
	reading.Wait()
	if stdout.eof && stderr.eof {
		close(closed)
	}
	<-stopped
	stdout.lines.flush()
	stderr.lines.flush()

	// A command a signal ended has no exit code.
	if code := cmd.ProcessState.ExitCode(); code >= 0 {
		res.ExitCode = &code
	}
	return res, nil
}

// output is the pipe one stream of the command is written to.
type output struct {
	r, w  *os.File
	lines *lines
	eof   bool // every process that held the pipe closed it
}

func newOutput(l *lines) (*output, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	return &output{r: r, w: w, lines: l}, nil
}

// started lets go of the write end the command was given, so that the pipe
// ends once the command and what it started have closed theirs.
func (o *output) started() {
	o.w.Close()
}

// read hands over what the pipe holds until it ends, or until end's grace
// period is over and what is left in it is read.
func (o *output) read() {
	buf := make([]byte, 32<<10)
	for {
		n, err := o.r.Read(buf)
		if n > 0 {
			o.lines.add(buf[:n])
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			o.drain(buf)
		}
		if err != nil {
			o.eof = errors.Is(err, io.EOF)
			return
		}
	}
}

// drain reads what the pipe holds now, up to the most a pipe can hold: a
// process left running may write on.
func (o *output) drain(buf []byte) {
	raw, err := o.r.SyscallConn()
	if err != nil || o.r.SetReadDeadline(time.Time{}) != nil {
		return
	}
	_ = raw.Read(func(fd uintptr) bool {
		for left := pipeMax; left > 0; {
			n, err := unix.Read(int(fd), buf[:min(left, len(buf))])
			if n <= 0 || err != nil {
				break
			}
			o.lines.add(buf[:n])
			left -= n
		}
		return true
	})
}

// pipeMax is the most a pipe can hold: the size Linux lets a program raise
// its pipe to.
const pipeMax = 1 << 20

// end gives a command that has ended the grace period for what it left
// running to close the pipe.
func (o *output) end(grace time.Duration) {
	_ = o.r.SetReadDeadline(time.Now().Add(grace))
}

func (o *output) close() {
	o.r.Close()
	o.w.Close()
}

// lines splits what a command writes on one stream into lines.
type lines struct {
	stream string
	mu     *sync.Mutex // shared by both streams: one call of line at a time
	line   func(stream string, text []byte)
	rest   []byte // the start of a line whose newline has not been written yet
}

func (l *lines) add(p []byte) {
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
