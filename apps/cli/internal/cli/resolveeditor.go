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
// a space, is not resolved and is named in a warning; every other one is
// resolved to what it holds, and written into the merge as a resolve
// writes a file, see resolve, only while the merge ref still holds the
// commit the files were written from: one that another run moved while the
// editor was open is refused with nothing written. The directory is
// removed once what the editor left is written or found to have nothing to
// write; when the editor fails, or the run refuses to write, it is kept,
// and a warning names it, so that nothing typed there is lost.
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
		inv.pruneEditorDirs(name, false)
		dir, paths, err = inv.layOutForEditor(name, files)
		return err
	})
	if err != nil {
		return mutationFailure(err)
	}
	kept := func() { inv.out.warn("the files you edited are kept in " + dir) }

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
	for _, p := range left {
		inv.out.warn(quotedPath(p) + " still holds conflict markers, or is gone, so it was left unresolved")
	}
	if len(rs) == 0 {
		os.RemoveAll(dir)
		return r.reportUnchanged(left)
	}
	if err := r.resolve(ctx, rs); err != nil {
		kept()
		return err
	}
	os.RemoveAll(dir)
	return nil
}

// reportUnchanged reports an editor session that resolved nothing: the
// conflict event of the merge as it stands, every file left in it, and the
// line that says so. The merge stays pending, which is no failure.
func (r *pendingRun) reportUnchanged(left []string) error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
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
// it is: the process id of the run that made it and the agentx home it
// resolves a merge of, one per line. It sits beside the skill's directory
// and never in it, so no file of the skill can be taken for it.
const editorOwner = "owner"

// tempDir is the operating system's temporary directory, read from the
// environment the CLI was given: TMPDIR, else /tmp.
func (inv *invocation) tempDir() string {
	if dir := inv.env["TMPDIR"]; dir != "" {
		return dir
	}
	return "/tmp"
}

// layOutForEditor makes the directory an editor session opens files in,
// agentx-resolve-<random> in the temporary directory: the owner file, and
// a directory named after the skill with each file at its path relative
// to the skill's directory, so that the editor shows what it is. Every
// file is written as merge-file wrote it, see editorText. It returns the
// directory and each file's path, in the order of files.
func (inv *invocation) layOutForEditor(name string, files []*conflictFile) (string, []string, error) {
	dir, err := os.MkdirTemp(inv.tempDir(), editorDirPrefix)
	if err != nil {
		return "", nil, err
	}
	owner := fmt.Sprintf("%d\n%s\n", os.Getpid(), inv.dirs.Home)
	if err := os.WriteFile(filepath.Join(dir, editorOwner), []byte(owner), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, err
	}
	paths := make([]string, len(files))
	for i, f := range files {
		if !safeRelPath(f.Path) {
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

// safeRelPath reports whether p stays inside the directory it is joined
// to: not empty, not absolute, and no empty, "." or ".." component.
func safeRelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
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
// cannot be run or exits otherwise than 0 is an error that says how.
func (inv *invocation) runEditor(ctx context.Context, s *editorSession, files []string) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", s.command + ` "$@"`, s.command}, files...)...)
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
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		return fmt.Errorf("exited with status %d", exit.ExitCode())
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

// pruneEditorDirs removes the directories editor sessions of the skill
// called name laid files out in for this agentx home: every one with all,
// as giving the merge up does, since what was typed there can no longer be
// written anywhere, and otherwise only those whose run is gone, a run
// killed or ended with the editor's files kept, as a new session and the
// completion of the merge do. A directory whose owner file is missing or
// names another home is left alone: it is not this merge's.
func (inv *invocation) pruneEditorDirs(name string, all bool) {
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
		pid, homeDir, _ := strings.Cut(strings.TrimSuffix(string(owner), "\n"), "\n")
		if homeDir != inv.dirs.Home {
			continue
		}
		if info, err := os.Lstat(filepath.Join(dir, name)); err != nil || !info.IsDir() {
			continue
		}
		if n, err := strconv.Atoi(pid); !all && err == nil && running(n) {
			continue
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
