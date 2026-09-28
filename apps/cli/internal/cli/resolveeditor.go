package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
)

// editorSession is what --editor asks of a run: the editor command, as the
// environment names it, the file to open, "" for every text file left to
// resolve, and what the editor reads its input from.
type editorSession struct {
	command string
	file    string
	stdin   io.Reader
}

// editorCommand is the command that opens files for editing, the one git
// would take from the same environment before its own configuration:
// GIT_EDITOR, then EDITOR, then VS Code's 'code --wait' when there is a
// code command on PATH. Each is read from the environment the CLI was
// given, and none is ever set. ok is false when there is none.
func (inv *invocation) editorCommand() (command string, ok bool) {
	for _, name := range []string{"GIT_EDITOR", "EDITOR"} {
		if command := inv.env[name]; strings.TrimSpace(command) != "" {
			return command, true
		}
	}
	if _, ok := gitx.LookPath(inv.env, "code"); ok {
		return "code --wait", true
	}
	return "", false
}

// resolveInEditor is the command that opens file in an editor session of
// the skill called name, or every text file left to resolve when file is
// "".
func resolveInEditor(name, file string) string {
	command := skillCommand("resolve", name, "--editor")
	switch {
	case file == "":
	case strings.HasPrefix(file, "-") && !strings.HasPrefix(name, "-"):
		// skillCommand puts -- before a name that starts with a dash, and
		// a file that does needs one as much.
		command += " -- " + shellWord(file)
	default:
		command += " " + shellWord(file)
	}
	return command
}

// runEditor runs the editor on files and waits for it to exit, the way git
// runs one: through sh, the command followed by the paths as arguments of
// their own, so that the command may carry arguments and a path needs no
// quoting. It gets the environment the CLI was given and the input the run
// reads. In text it has the run's stdout and stderr, a terminal's editor
// drawing on them; with --json, where each stream carries nothing but
// events, every line it prints is an info log event instead. A command that
// cannot be run, exits otherwise than 0 or is killed by a signal is an
// error that says how. One that exits 0 has succeeded, even when a process
// it left running, as a wrapper that starts an editor server does, still
// holds its output: that is read for a second longer, and then dropped.
//
// A terminal's Ctrl-C and Ctrl-\ reach its whole foreground process group,
// and the sh that runs the editor is in it: a sh that does not exec the
// command it runs last, as dash does not, would die of them and leave the
// editor running with nobody waiting for it. So the sh catches both with
// a trap that does nothing, which a command it starts does not inherit:
// the editor answers them as it always does, and the sh waits for it and
// exits as it did. The sh is then no process the editor replaced, so a run
// stopped by SIGTERM, which reaches agentx alone when a supervisor or the
// app sends it, sends SIGTERM to the editor and every process it started,
// and then to the sh, see stopEditor; the sh is killed a second later if it
// has not exited by then. The error of a run stopped while the editor was
// open is errEditorStopped.
func (inv *invocation) runEditor(ctx context.Context, s *editorSession, files []string) error {
	script := "trap : INT QUIT; " + s.command + ` "$@"`
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", script, s.command}, files...)...)
	env := make([]string, 0, len(inv.env))
	for k, v := range inv.env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	cmd.Stdin = s.stdin
	cmd.Stdout, cmd.Stderr = inv.out.stdout, inv.out.stderr
	if inv.out.json {
		relay := &lineRelay{emit: inv.out.info}
		defer relay.flush()
		cmd.Stdout, cmd.Stderr = relay, relay
	}
	cmd.Cancel = func() error { return stopEditor(cmd.Process) }
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return errEditorStopped
	case errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success():
		// The editor exited 0, and only a process it left running, a server
		// it started say, still holds its output, which is dropped.
		return nil
	case !errors.As(err, &exit):
		return fmt.Errorf("could not be run: %w", err)
	case exit.ExitCode() >= 0:
		return fmt.Errorf("exited with status %d", exit.ExitCode())
	}
	// A signal a terminal or a supervisor sent the whole process group can
	// end the editor before it reaches the run, which then answers for
	// that stop.
	if interrupt.Settle(ctx) {
		return errEditorStopped
	}
	if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return fmt.Errorf("was stopped by %s", unix.SignalName(status.Signal()))
	}
	return fmt.Errorf("could not be run: %w", err)
}

// errEditorStopped is the error of an editor the stop of its run ended,
// which may still be running, see runEditor.
var errEditorStopped = errors.New("was stopped")

// stopEditor stops the editor that sh, the process runEditor started,
// runs: SIGTERM to every process sh started, the editor and what it started
// in turn, and then to sh itself. sh does not exec the editor, see
// runEditor, so a signal to it alone would leave the editor running, still
// drawing on the terminal, after the run is over. A process that cannot be
// listed is left out, and sh is then all that is signalled.
func stopEditor(sh *os.Process) error {
	children := map[int][]int{}
	for pid, parent := range processParents() {
		children[parent] = append(children[parent], pid)
	}
	queue := slices.Clone(children[sh.Pid])
	for len(queue) > 0 {
		pid := queue[0]
		queue = append(queue[1:], children[pid]...)
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	return sh.Signal(syscall.SIGTERM)
}

// processParents is the parent of every process this user can see, by
// process id: read from /proc where it lists them, as on Linux, and from
// ps otherwise, as on macOS. It is empty when neither can be read.
func processParents() map[int]int {
	parents := map[int]int{}
	if entries, err := os.ReadDir("/proc"); err == nil {
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil {
				continue
			}
			stat, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
			if err != nil {
				continue
			}
			// The command's name, in parentheses, may hold anything, so the
			// fields are read after the last closing one: the state, then
			// the parent's id.
			i := bytes.LastIndexByte(stat, ')')
			if fields := strings.Fields(string(stat[i+1:])); i >= 0 && len(fields) > 1 {
				if parent, err := strconv.Atoi(fields[1]); err == nil {
					parents[pid] = parent
				}
			}
		}
		if len(parents) > 0 {
			return parents
		}
	}
	for _, ps := range []string{"/bin/ps", "/usr/bin/ps"} {
		out, err := exec.Command(ps, "-A", "-o", "pid=,ppid=").Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			if fields := strings.Fields(line); len(fields) == 2 {
				pid, err := strconv.Atoi(fields[0])
				parent, err2 := strconv.Atoi(fields[1])
				if err == nil && err2 == nil {
					parents[pid] = parent
				}
			}
		}
		break
	}
	return parents
}

// lineRelay hands every line written to it to emit as it completes, and a
// last line with no newline on flush. exec copies both of a command's
// streams into one writer from goroutines of its own when they are the
// same writer, and never at once, so it takes no lock.
type lineRelay struct {
	emit func(string)
	buf  []byte
}

func (l *lineRelay) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		l.emit(strings.TrimSuffix(string(l.buf[:i]), "\r"))
		l.buf = l.buf[i+1:]
	}
}

func (l *lineRelay) flush() {
	if len(l.buf) > 0 {
		l.emit(string(l.buf))
		l.buf = nil
	}
}
