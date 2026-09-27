package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
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
	for _, dir := range filepath.SplitList(inv.env["PATH"]) {
		if info, err := os.Stat(filepath.Join(dir, "code")); dir != "" && err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return "code --wait", true
		}
	}
	return "", false
}

// edit resolves files of a pending merge in the user's editor. Under the
// lock, the unfinished journals finished first, the merge ref is read
// again and every file to edit is written into a directory of its own in
// the operating system's temporary directory, see layOutForEditor, as
// merge-file writes it: in zdiff3 style, with git's seven-character markers
// or longer ones for a file whose own lines start with as many marker
// characters. The directory is a plain one: nothing is registered in the
// account repo, and nothing is written where an agent reads. The lock is
// released before the editor starts, so that every other command, and the
// scans of the app, go on while it is open, and SIGINT is left to the
// editor while it runs, as git leaves it.
//
// Once the editor exits 0, each file is read back. One in which a marker
// of the size it was written with is left, alone on its line or followed by
// a space, is not resolved, or keeps the way an earlier run resolved it,
// and is named in a warning; every other one is resolved to what it holds,
// and written into the merge as a resolve writes a file, see resolve, only
// while the merge ref still holds the commit the files were written from:
// one that another run moved while the editor was open is refused with
// nothing written. A session that resolved nothing reads the merge ref
// again before it reports the merge, and one given up or moved meanwhile
// is refused rather than reported as it was.
//
// The directory is removed once what the editor left is written or found
// to have nothing to write. When the editor fails, or the run refuses to
// write, it is kept, marked so in its owner file, and a warning names it,
// so that nothing typed there is lost: a later session leaves it alone, and
// only completing the merge or giving it up removes it, which a run that
// finds the merge completed or given up meanwhile does at once. A
// completion that merged the library directory again and left the merge
// pending, exit code 4, wrote every resolution that still applies, and
// keeps the directory only for a file the editor resolved that conflicts
// anew, which a warning names.
func (r *pendingRun) edit(ctx context.Context, s *editorSession) error {
	inv, name := r.inv, r.rec.Name
	see := "run '" + skillCommand("resolve", name) + "' to see every file left to resolve"
	var files []*conflictFile
	if s.file != "" {
		p := filepath.ToSlash(filepath.Clean(s.file))
		f := r.files[p]
		switch {
		case f == nil:
			return fail(exitUsage, fmt.Sprintf("%s does not conflict in the merge of %s", quotedPath(p), name), see)
		case f.text == nil:
			return fail(exitUsage, fmt.Sprintf("%s conflicts as a whole file, so there is no text to edit", quotedPath(p)),
				fmt.Sprintf("choose a side for it with --hunk %s:1=mine or --hunk %s:1=theirs", p, p))
		}
		files = append(files, f)
	} else {
		for _, f := range r.unresolved(nil) {
			if f.text != nil {
				files = append(files, r.files[f.Path])
			}
		}
		if len(files) == 0 {
			return fail(exitRefused, "every file left to resolve in the merge of "+name+" conflicts as a whole file, so there is no text to edit",
				"choose a side for each with --hunk <file>:1=mine or --hunk <file>:1=theirs; "+see)
		}
	}
	var dir string
	var paths []string
	err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error {
		values, err := inv.lineageRefs(ctx, r.gitDir, name)
		if err != nil {
			return err
		}
		if values[lineage.MergeRef(name)] != r.pending.Commit {
			return mergeMovedFailure(name, "the files were being opened")
		}
		inv.pruneEditorDirs(name, pruneGone)
		dir, paths, err = inv.layOutForEditor(name, r.pending.Commit, files)
		return err
	})
	if err != nil {
		return mutationFailure(err)
	}
	// kept keeps the directory of a run that writes nothing of what the
	// editor left, and names it in a warning. One whose merge was completed
	// or given up meanwhile goes instead, as completing or giving the merge
	// up removes it: what was typed there can no longer be written anywhere.
	kept := func() {
		if values, err := inv.lineageRefs(ctx, r.gitDir, name); err == nil && values[lineage.MergeRef(name)] == "" {
			os.RemoveAll(dir)
			return
		}
		keepEditorDir(dir)
		inv.out.warn("the files you edited are kept in " + dir + " until the merge of " + name + " completes or is given up; " +
			"a new session opens the files afresh, so copy what you typed from there")
	}

	release := interrupt.Hold(ctx)
	err = inv.runEditor(ctx, s, paths)
	release()
	if err != nil {
		kept()
		return fail(exitRefused, fmt.Sprintf("the editor %s %s, so nothing was written", s.command, err),
			"run '"+skillCommand("resolve", name, "--editor")+"' again once the editor works, or set GIT_EDITOR or EDITOR to another")
	}

	var rs []resolution
	var left []string
	for i, f := range files {
		body, err := os.ReadFile(paths[i])
		if err != nil || holdsMarkers(string(body), f.text.size) {
			left = append(left, f.Path)
			continue
		}
		content := string(body)
		rs = append(rs, resolution{path: f.Path, entry: &source.TreeEntry{Mode: resolvedMode(f, nil)}, body: &content})
	}
	if len(rs) == 0 {
		if err := r.stillPending(ctx, "the editor was open"); err != nil {
			kept()
			return err
		}
		os.RemoveAll(dir)
	}
	for _, p := range left {
		if r.resolved[p] {
			inv.out.warn(quotedPath(p) + " still holds conflict markers, or is gone, so it keeps the way it was resolved before")
			continue
		}
		inv.out.warn(quotedPath(p) + " still holds conflict markers, or is gone, so it was left unresolved")
	}
	if len(rs) == 0 {
		return r.reportUnchanged()
	}
	err = r.resolve(ctx, rs)
	switch {
	case err == nil:
		os.RemoveAll(dir)
		return nil
	case r.carried == nil:
		kept()
		return err
	}
	// The merge was merged again and written, every resolution that still
	// applies carried into it, before the run was refused.
	var anew []string
	for _, res := range rs {
		if !r.carried[res.path] {
			anew = append(anew, res.path)
		}
	}
	if len(anew) == 0 {
		os.RemoveAll(dir)
		return err
	}
	keepEditorDir(dir)
	for _, p := range anew {
		inv.out.warn(quotedPath(p) + " conflicts anew now that the merge was merged again, so what you typed for it is kept in " + dir +
			" until the merge of " + name + " completes or is given up")
	}
	return err
}

// stillPending reads the merge ref again, for a run that is about to
// report the merge as it read it, and refuses one another run gave up,
// completed or moved while what was doing, so that nothing reports a merge
// that is no longer the one pending.
func (r *pendingRun) stillPending(ctx context.Context, what string) error {
	name := r.rec.Name
	values, err := r.inv.lineageRefs(ctx, r.gitDir, name)
	if err != nil {
		return err
	}
	switch held := values[lineage.MergeRef(name)]; {
	case held == "":
		return fail(exitRefused, name+" has no merge pending: it was given up or completed while "+what,
			"run 'agentx skill list' to see which skills have one")
	case held != r.pending.Commit:
		return mergeMovedFailure(name, what)
	}
	return nil
}

// reportUnchanged reports an editor session that resolved nothing: the
// conflict event of the merge as it stands, every file left in it, and the
// line that says so. The merge stays pending, which is no failure.
func (r *pendingRun) reportUnchanged() error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
	r.warnBaseMoved()
	files := r.unresolved(nil)
	out.emit(conflictOfSkill(name, r.pending.Merge, files))
	rest := plural(len(files), "file") + " left to resolve"
	inv.summary = "resolved nothing in the merge of " + name + ": " + rest
	out.print("Resolved nothing in the merge of ", out.paint(heading, sanitised(name)), ": ", out.paint(noteStyle, rest), ".")
	return nil
}

// editorDirPrefix starts the name of every directory an editor session
// lays files out in, in the operating system's temporary directory.
const editorDirPrefix = "agentx-resolve-"

// editorOwner is the file of an editor session's directory that says whose
// it is, one per line: the process id of the run that made it, or the word
// in editorKept once that run kept it, the agentx home it resolves a merge
// of, and the pending merge commit its files were written from. It sits
// beside the skill's directory and never in it, so no file of the skill can
// be taken for it, and its name is a hidden one, which the library never
// gives a skill, so the skill's directory cannot be taken for it either.
const editorOwner = ".owner"

// editorKept stands in for the process id in the owner file of a directory
// its run kept, so that nothing typed there is lost: no later session
// removes it, since its run is gone by design and not because it was
// killed.
const editorKept = "kept"

// keepEditorDir marks the directory an editor session laid its files out
// in as kept, see editorKept. A directory whose owner file cannot be
// rewritten is left as it is; it is then taken for one whose run was
// killed.
func keepEditorDir(dir string) {
	path := filepath.Join(dir, editorOwner)
	owner, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_, rest, _ := strings.Cut(string(owner), "\n")
	_ = os.WriteFile(path, []byte(editorKept+"\n"+rest), 0o600)
}

// tempDir is the operating system's temporary directory, read from the
// environment the CLI was given: TMPDIR, else /tmp.
func (inv *invocation) tempDir() string {
	if dir := inv.env["TMPDIR"]; dir != "" {
		return dir
	}
	return "/tmp"
}

// layOutForEditor makes the directory an editor session opens files in,
// agentx-resolve-<random> in the temporary directory: the owner file, which
// names commit, the pending merge commit the files are written from, and a
// directory named after the skill with each file at its path relative to
// the skill's directory, so that the editor shows what it is. Every file is
// written as merge-file wrote it, see editorText. It returns the directory
// and each file's path, in the order of files.
func (inv *invocation) layOutForEditor(name, commit string, files []*conflictFile) (string, []string, error) {
	dir, err := os.MkdirTemp(inv.tempDir(), editorDirPrefix)
	if err != nil {
		return "", nil, err
	}
	owner := fmt.Sprintf("%d\n%s\n%s\n", os.Getpid(), inv.dirs.Home, commit)
	if err := os.WriteFile(filepath.Join(dir, editorOwner), []byte(owner), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	paths := make([]string, len(files))
	for i, f := range files {
		if !lineage.FilePath(f.Path) {
			os.RemoveAll(dir)
			return "", nil, fmt.Errorf("%s is no path an editor can open inside %s", quotedPath(f.Path), dir)
		}
		paths[i] = filepath.Join(dir, name, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(paths[i]), 0o700); err != nil {
			os.RemoveAll(dir)
			return "", nil, err
		}
		if err := os.WriteFile(paths[i], []byte(editorText(f.text)), 0o600); err != nil {
			os.RemoveAll(dir)
			return "", nil, err
		}
	}
	return dir, paths, nil
}

// editorText is what an editor is given of a text file that conflicts:
// what merge-file wrote, with markers of the size it chose and the three
// versions named mine, base and theirs on them. A file merge-file merged
// with no hunk left although merge-tree found it conflicting is written as
// the one hunk it is, the whole of each version between the same markers.
func editorText(t *textMerge) string {
	if !t.whole {
		return t.out
	}
	line := func(text string) string {
		if text != "" && !strings.HasSuffix(text, "\n") {
			return text + "\n"
		}
		return text
	}
	return strings.Repeat("<", t.size) + " mine\n" + line(t.mine) +
		strings.Repeat("|", t.size) + " base\n" + line(t.base) +
		strings.Repeat("=", t.size) + "\n" + line(t.theirs) +
		strings.Repeat(">", t.size) + " theirs\n"
}

// holdsMarkers reports whether text holds a conflict marker of size: a
// line of that many <, |, = or >, alone or followed by a space.
func holdsMarkers(text string, size int) bool {
	for _, line := range strings.SplitAfter(text, "\n") {
		for _, c := range []string{"<", "|", "=", ">"} {
			if isMarkerLine(line, c, size) {
				return true
			}
		}
	}
	return false
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
// exits as it did.
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
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return errors.New("was stopped")
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
		return errors.New("was stopped")
	}
	if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return fmt.Errorf("was stopped by %s", unix.SignalName(status.Signal()))
	}
	return fmt.Errorf("could not be run: %w", err)
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
		i := strings.IndexByte(string(l.buf), '\n')
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

// prune says which of the directories editor sessions of a skill left
// pruneEditorDirs removes.
type prune int

const (
	// pruneGone removes those whose run is gone without keeping them, a run
	// killed while its editor was open, as a new session does.
	pruneGone prune = iota
	// pruneKept removes the kept ones too, as completing the merge does:
	// the merge what was typed there was for is complete.
	pruneKept
	// pruneEvery removes every one, a session still open included, as
	// giving the merge up does: what was typed there can no longer be
	// written anywhere.
	pruneEvery
)

// pruneEditorDirs removes the directories editor sessions of the skill
// called name laid files out in for this agentx home, which says. A
// directory whose owner file is missing or names another home is left
// alone: it is not this merge's.
func (inv *invocation) pruneEditorDirs(name string, which prune) {
	tmp := inv.tempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), editorDirPrefix) {
			continue
		}
		dir := filepath.Join(tmp, e.Name())
		owner, err := os.ReadFile(filepath.Join(dir, editorOwner))
		if err != nil {
			continue
		}
		lines := strings.Split(string(owner), "\n")
		if len(lines) < 2 || lines[1] != inv.dirs.Home {
			continue
		}
		if info, err := os.Lstat(filepath.Join(dir, name)); err != nil || !info.IsDir() {
			continue
		}
		switch pid := lines[0]; {
		case which == pruneEvery:
		case pid == editorKept:
			if which != pruneKept {
				continue
			}
		default:
			if n, err := strconv.Atoi(pid); err == nil && running(n) {
				continue
			}
		}
		os.RemoveAll(dir)
	}
}

// running reports whether a process with the id pid is running, as far as
// this user can tell: one it may not signal is running too.
func running(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
