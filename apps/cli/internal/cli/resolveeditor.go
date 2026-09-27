package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
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
	if _, ok := gitx.LookPath(inv.env, "code"); ok {
		return "code --wait", true
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
// What was typed in an earlier session comes back: a file that an earlier
// session of the skill left typed in, for the same conflict, is laid out
// with what was typed rather than with markers afresh, see reopen, and a
// note says so before the editor takes the terminal. The directories of
// earlier sessions are then settled, see settle, once the files are
// written, so that a run killed in between leaves what was typed in one of
// them at least.
//
// Once the editor exits 0, each file is read back. One that is gone, or in
// which a marker of the size it was written with is left, alone on its line
// or followed by a space, is not resolved, or keeps the way an earlier run
// resolved it, and is named in a warning; every other one is resolved to
// what it holds, and written into the merge as a resolve writes a file, see
// resolve, only while the merge ref still holds the commit the files were
// written from: one that another run moved while the editor was open is
// refused with nothing written. A session that resolved nothing reads the
// merge ref again before it reports the merge, and one given up or moved
// meanwhile is refused rather than reported as it was.
//
// The directory is removed once what the editor left is written or found
// to have nothing to write. When the editor fails, or the run refuses to
// write, it is kept, marked so in its owner file, and a warning names it,
// so that nothing typed there is lost: the next session of the same
// conflict opens what was typed there again, and completing a merge of the
// skill removes it. So is a directory that holds a file left with markers
// in it and something typed besides, whose warning says so. So is one
// whose merge was completed or given up while the editor was open, once
// something was typed there, which the next session of the same conflict
// opens again when an update leaves it pending anew; one with nothing typed
// in it goes at once. A run that merged the merge again, a completion of an
// edited library directory or a run on a moved import branch, wrote every
// resolution that still applies, and keeps the directory only for a file
// the editor resolved that conflicts anew, or that merges cleanly now,
// git's merge of it written in place of what was typed, which a warning
// names. When that run completed the merge, which is what removes a kept
// directory, the directory is given up to the user instead, see
// releaseEditorDir: agentx never removes it.
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
	var paths, from, records []string
	err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error {
		values, err := inv.lineageRefs(ctx, r.gitDir, name)
		if err != nil {
			return err
		}
		if values[lineage.MergeRef(name)] != r.pending.Commit {
			return mergeMovedFailure(name, "the files were being opened")
		}
		earlier := inv.earlierDirs(name)
		var texts []string
		texts, from = reopen(earlier, files)
		if dir, paths, records, err = inv.layOutForEditor(name, r.pending.Commit, files, texts); err != nil {
			return err
		}
		laid := map[string]laidFile{}
		for i, f := range files {
			laid[f.Path] = laidFile{record: records[i], text: texts[i]}
		}
		r.turned = settle(earlier, laid)
		return nil
	})
	if err != nil {
		return mutationFailure(err)
	}
	for i, f := range files {
		if from[i] != "" {
			inv.out.info(quotedPath(f.Path) + " opens with what you typed in an earlier session, taken from " + from[i])
		}
	}
	for _, d := range slices.Sorted(maps.Keys(r.turned)) {
		inv.out.warn("the files you edited in an earlier session that did not finish are kept in " + d + keptUntil(name, "them"))
	}
	// keep keeps the directory, marked so, and reports whether it did. One
	// whose merge was completed or given up meanwhile goes instead when
	// nothing was typed there: there is nothing to open again once an update
	// conflicts the same way. kept keeps the directory of a run that writes
	// nothing of what the editor left, and names it in a warning.
	keep := func() bool {
		if values, err := inv.lineageRefs(ctx, r.gitDir, name); err == nil && values[lineage.MergeRef(name)] == "" && untouched(dir, name, records) {
			os.RemoveAll(dir)
			return false
		}
		keepEditorDir(dir)
		return true
	}
	kept := func() {
		if keep() {
			inv.out.warn("the files you edited are kept in " + dir + keptUntil(name, "them"))
		}
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
	var left []string          // the files left as they were resolved, or unresolved
	gone := map[string]bool{}  // of those, the ones the editor left no file at
	typed := map[string]bool{} // and the ones that hold markers and something typed, which is kept
	for i, f := range files {
		body, err := os.ReadFile(paths[i])
		switch {
		case err != nil:
			left, gone[f.Path] = append(left, f.Path), true
		case holdsMarkers(string(body), f.text.size):
			left, typed[f.Path] = append(left, f.Path), string(body) != editorText(f.text)
		default:
			content := string(body)
			rs = append(rs, resolution{path: f.Path, entry: &source.TreeEntry{Mode: resolvedMode(f, nil)}, body: &content})
		}
	}
	var anew, clean []string // the files the editor resolved that conflict anew, and that merge cleanly now, what was typed for them kept
	if len(rs) == 0 {
		if err = r.stillPending(ctx, "the editor was open"); err != nil {
			kept()
			return err
		}
	} else if err = r.resolve(ctx, rs); err != nil && r.carried == nil {
		kept()
		return err
	}
	if r.carried != nil {
		// The merge was merged again and written, or completed, every
		// resolution that still applies carried into it. A file the editor
		// resolved that is left to resolve conflicts anew; any other one
		// merges cleanly now, and git's merge of it was written in place
		// of what was typed.
		for _, res := range rs {
			switch {
			case r.carried[res.path]:
			case r.left[res.path]:
				anew = append(anew, res.path)
			default:
				clean = append(clean, res.path)
			}
		}
	}
	// The directory stays while it holds something typed that was not
	// written: a file left with markers in it and more, which the next
	// session of the same conflict opens again, and a file the editor
	// resolved that conflicts anew or merges cleanly now. While the merge is
	// pending it is marked kept, and goes once a merge of the skill
	// completes; a run that completed the merge gives it up to the user,
	// since agentx would not remove it for them any more.
	held, until, again := "", " until a merge of "+name+" completes", keptUntil(name, "it")
	switch {
	case !slices.ContainsFunc(left, func(p string) bool { return typed[p] }) && len(anew)+len(clean) == 0:
		os.RemoveAll(dir)
	case r.completed:
		releaseEditorDir(dir)
		held, until = dir, ", which agentx leaves for you to delete now that the merge of "+name+" is complete"
		again = until
	case keep():
		held = dir
	}
	for _, p := range left {
		why, how, stays := " still holds conflict markers", ", so it was left unresolved", again
		if gone[p] {
			why = " is gone"
		}
		switch {
		case r.carried != nil && !r.left[p]:
			how, stays = ", and merges cleanly now that the merge was merged again, so git's merge of it was written", until
		case r.resolved[p]:
			how = ", so it keeps the way it was resolved before"
		}
		warning := quotedPath(p) + why + how
		if typed[p] && held != "" {
			warning += "; what you typed is kept in " + held + stays
		}
		inv.out.warn(warning)
	}
	if held != "" {
		for _, p := range anew {
			inv.out.warn(quotedPath(p) + " conflicts anew now that the merge was merged again, so what you typed for it is kept in " + held + until)
		}
		for _, p := range clean {
			inv.out.warn(quotedPath(p) + " merges cleanly now that the merge was merged again, so what you typed for it was not written and is kept in " + held + until)
		}
	}
	if len(rs) == 0 {
		return r.reportUnchanged()
	}
	return err
}

// stillPending reads the merge ref again, for a run that is about to
// report the merge as it read it, and refuses one another run gave up,
// completed or moved while what was going on, "the editor was open" say,
// so that nothing reports a merge that is no longer the one pending.
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
// in editorKept once it is kept, the agentx home it resolves a merge of,
// the pending merge commit its files were written from, and then each file
// as merge-file wrote it, see editorRecord, which tells the conflict it was
// written for, even where what was typed for it earlier was laid out in
// its place, see reopen. It sits beside the skill's directory and never in
// it, so no file of the skill can be taken for it, and its name is a
// hidden one, which the library never gives a skill, so the skill's
// directory cannot be taken for it either.
const editorOwner = ".owner"

// editorKept stands in for the process id in the owner file of a directory
// kept so that nothing typed there is lost: by its own run, which did not
// write what was typed there, or by a later run that found its run gone
// and something typed there. A later session of the same conflict opens
// what was typed there again, and removes it only once everything typed
// there is carried into its own directory, see settle; completing a merge
// of the skill removes it, which every warning that names it says, see
// clearEditorDirs.
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

// releaseEditorDir gives the directory an editor session laid its files out
// in up to the user, once the merge what was typed there was for is
// complete: its owner file goes, so that no run takes it for a session's,
// see earlierDirs, and agentx never removes it.
func releaseEditorDir(dir string) {
	_ = os.Remove(filepath.Join(dir, editorOwner))
}

// leftToYou is the warning that names the directory an earlier session
// whose run is gone left something saved in, once completing the merge
// gives it up to the user, see clearEditorDirs.
func leftToYou(dir, name string) string {
	return "the files you edited in an earlier session that did not finish are kept in " + dir + ", which agentx leaves for you to delete now that the merge of " + name + " is complete"
}

// keptUntil ends a warning that names a directory kept for what was typed
// in it, it or them: how long it stays, and that the next editor session
// of the same conflict opens what was typed there again.
func keptUntil(name, what string) string {
	return " until a merge of " + name + " completes, and the next '" + skillCommand("resolve", name, "--editor") + "' of the same conflict opens " + what + " again"
}

// editorRecord is the line of an editor session's owner file that records
// a file as merge-file wrote it for the editor, see editorText: the SHA-256
// of what it holds, in hex, then its path relative to the skill's
// directory, quoted as git quotes a path, so that no path breaks the line.
// Two sessions that record a file alike laid out the same conflict of it.
func editorRecord(path string, body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]) + " " + gitQuoted(path)
}

// untouched reports whether the directory an editor session of the skill
// called name laid its files out in holds nothing but them as merge-file
// wrote them, records being the lines of its owner file that record them,
// see editorRecord and spent.
func untouched(dir, name string, records []string) bool {
	d := earlierDir{path: dir, records: records}
	d.files, d.other = savedFiles(dir, name)
	return d.spent(nil)
}

// earlierDir is a directory an earlier editor session of a skill laid its
// files out in for this agentx home, as a run finds it once that session's
// run is gone or kept it: what its owner file records and what it holds.
type earlierDir struct {
	path    string
	gone    bool                 // its run is gone without keeping it, killed or hung up on say; otherwise the directory is kept
	records []string             // the lines of its owner file that record the files as merge-file wrote them, see editorRecord
	byPath  map[string]string    // those lines by the path they record; nil when one of them records no file of a skill, and nothing is opened again from it
	files   map[string]savedFile // every regular file under the skill's directory, by its path relative to it
	other   bool                 // it holds anything else: something beside the skill's directory, a symlink or a file of another kind, which is never followed or read
}

// savedFile is what a file of an earlier session's directory holds, and
// when it was last written.
type savedFile struct {
	body string
	mod  time.Time
}

// laidFile is a file as a session lays it out: the line its owner file
// records it with, for the conflict as it is now, and what is written.
type laidFile struct {
	record, text string
}

// savedFiles reads what the directory an editor session of the skill called
// name laid its files out in holds: every regular file under the skill's
// directory, by its path relative to it, and other when there is anything
// else, the owner file aside. A directory that cannot be read through is
// taken for one that holds something else.
func savedFiles(dir, name string) (files map[string]savedFile, other bool) {
	files = map[string]savedFile{}
	root := filepath.Join(dir, name)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case p == dir, p == root, p == filepath.Join(dir, editorOwner):
			return nil
		case !strings.HasPrefix(p, root+string(filepath.Separator)), !d.IsDir() && !d.Type().IsRegular():
			other = true
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.IsDir():
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files[filepath.ToSlash(p[len(root)+1:])] = savedFile{body: string(body), mod: info.ModTime()}
		return nil
	})
	return files, other || err != nil
}

// spent reports whether nothing typed in d is lost when it goes once a
// session has laid out laid, by path: it holds nothing but files, each as
// merge-file wrote it or, for the same conflict, see editorRecord, as the
// session laid it out, with what was typed in it carried over, see reopen.
// Nothing else is there: no swap or backup file an editor writes beside the
// one it opens, no file saved under another name. A recorded file that is
// not there holds nothing typed, one deleted or one a run killed as it laid
// them out never wrote, see layOutForEditor, which records every file
// before it writes any. An owner file that records no file tells nothing,
// and its directory is taken for one that holds something typed.
func (d earlierDir) spent(laid map[string]laidFile) bool {
	written := map[string]bool{}
	for _, line := range d.records {
		if line != "" {
			written[line] = true
		}
	}
	if d.other || len(written) == 0 {
		return false
	}
	for p, f := range d.files {
		if written[editorRecord(p, []byte(f.body))] {
			continue
		}
		if l, ok := laid[p]; ok && d.byPath[p] == l.record && f.body == l.text {
			continue
		}
		return false
	}
	return true
}

// recordsByPath is the lines of an owner file that record files, records,
// by the path each records, or nil when one of them is no line editorRecord
// writes for a file of a skill.
func recordsByPath(records []string) map[string]string {
	byPath := map[string]string{}
	for _, line := range records {
		sum, quoted, ok := strings.Cut(line, " ")
		if _, err := hex.DecodeString(sum); !ok || err != nil || len(sum) != 2*sha256.Size || strings.ToLower(sum) != sum {
			return nil
		}
		p, ok := gitUnquoted(quoted)
		if !ok || !lineage.FilePath(p) {
			return nil
		}
		byPath[p] = line
	}
	return byPath
}

// earlierDirs is every directory an earlier editor session of the skill
// called name laid its files out in for this agentx home whose run is gone
// or kept it, in the order of their names. A directory whose owner file is
// missing or names another home is left alone: it is not this merge's, or
// it was given up to the user. So is one whose run is still going: a
// session still open is never read or touched. One whose run is gone and
// that holds nothing but its owner file, as a run killed after it wrote
// that file and before it made the skill's directory leaves it, names no
// skill and goes, whichever skill's run finds it: nothing was typed there.
// One that holds no directory of the skill is some other skill's, or holds
// something saved beside its owner file, and is left as it is.
func (inv *invocation) earlierDirs(name string) []earlierDir {
	tmp := inv.tempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return nil
	}
	var dirs []earlierDir
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
		pid := lines[0]
		n, err := strconv.Atoi(pid)
		gone := pid != editorKept && (err != nil || !running(n))
		if !gone && pid != editorKept {
			continue
		}
		if info, err := os.Lstat(filepath.Join(dir, name)); err != nil || !info.IsDir() {
			if gone && onlyOwner(dir) {
				os.RemoveAll(dir)
			}
			continue
		}
		d := earlierDir{path: dir, gone: gone}
		if len(lines) > 3 {
			for _, line := range lines[3:] {
				if line != "" {
					d.records = append(d.records, line)
				}
			}
		}
		d.byPath = recordsByPath(d.records)
		d.files, d.other = savedFiles(dir, name)
		dirs = append(dirs, d)
	}
	return dirs
}

// reopen is what each of files is laid out with: what was typed for it in
// an earlier session, where the directory of one records the file for the
// same conflict, see editorRecord, and holds a regular file at its path
// with something else in it, the most recently saved where several do; and
// otherwise the file as merge-file wrote it, see editorText. from is the
// directory each comes from, "" for a file laid out afresh. The pending merge commit a
// directory's owner file names plays no part: a merge given up and left
// again by an update, or rewritten by a resolve since, conflicts the same
// way in a file whose three versions are the same.
func reopen(dirs []earlierDir, files []*conflictFile) (texts, from []string) {
	texts, from = make([]string, len(files)), make([]string, len(files))
	for i, f := range files {
		fresh := editorText(f.text)
		record := editorRecord(f.Path, []byte(fresh))
		texts[i] = fresh
		var newest time.Time
		for _, d := range dirs {
			saved, ok := d.files[f.Path]
			if !ok || d.byPath[f.Path] != record || saved.body == fresh {
				continue
			}
			if from[i] == "" || saved.mod.After(newest) || saved.mod.Equal(newest) && d.path > from[i] {
				texts[i], from[i], newest = saved.body, d.path, saved.mod
			}
		}
	}
	return texts, from
}

// settle settles the directories of earlier sessions, dirs, once a session
// has laid out laid, by path, and written every file of it: never before,
// so that a run killed in between leaves what was typed in one directory
// at least, and the next session opens it from whichever holds it. A
// directory with nothing typed in it that the session did not carry over
// goes, see spent. One whose run is gone and that holds something else,
// typed for a conflict that changed or for a file the session does not
// lay out, or a file of any other name, is kept, marked so, and returned,
// so that the run names it; a kept one stays as it is, named when it was
// kept.
func settle(dirs []earlierDir, laid map[string]laidFile) (turned map[string]bool) {
	turned = map[string]bool{}
	for _, d := range dirs {
		switch {
		case d.spent(laid):
			os.RemoveAll(d.path)
		case d.gone:
			keepEditorDir(d.path)
			turned[d.path] = true
		}
	}
	return turned
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
// names commit, the pending merge commit the files are written from, and
// records every file as merge-file wrote it, see editorText, before any is
// written, and a directory named after the skill with each file at its
// path relative to the skill's directory, so that the editor shows what it
// is. Each file holds its text, in the order of files: what merge-file
// wrote, or what was typed for the same conflict in an earlier session,
// see reopen, which the record, of what merge-file wrote, then tells from
// it. It returns the directory, each file's path and the line recording
// each, in the order of files.
func (inv *invocation) layOutForEditor(name, commit string, files []*conflictFile, texts []string) (dir string, paths, records []string, err error) {
	dir, err = os.MkdirTemp(inv.tempDir(), editorDirPrefix)
	if err != nil {
		return "", nil, nil, err
	}
	owner := fmt.Sprintf("%d\n%s\n%s\n", os.Getpid(), inv.dirs.Home, commit)
	records = make([]string, len(files))
	for i, f := range files {
		records[i] = editorRecord(f.Path, []byte(editorText(f.text)))
		owner += records[i] + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, editorOwner), []byte(owner), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", nil, nil, err
	}
	paths = make([]string, len(files))
	for i, f := range files {
		if !lineage.FilePath(f.Path) {
			os.RemoveAll(dir)
			return "", nil, nil, fmt.Errorf("%s is no path an editor can open inside %s", quotedPath(f.Path), dir)
		}
		paths[i] = filepath.Join(dir, name, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(paths[i]), 0o700); err != nil {
			os.RemoveAll(dir)
			return "", nil, nil, err
		}
		if err := os.WriteFile(paths[i], []byte(texts[i]), 0o600); err != nil {
			os.RemoveAll(dir)
			return "", nil, nil, err
		}
	}
	return dir, paths, records, nil
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
// exits as it did. The sh is then no process the editor replaced, so a run
// stopped by SIGTERM, which reaches agentx alone when a supervisor or the
// app sends it, sends SIGTERM to the editor and every process it started,
// and then to the sh, see stopEditor; the sh is killed a second later if it
// has not exited by then.
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

// clearEditorDirs settles the directories earlier editor sessions of the
// skill called name left for this home, see earlierDirs, once its merge is
// complete, as the run that completed it does: the merge what was typed
// there was for is done. A kept one goes, as every warning that named it
// said it would, but for one this run kept itself as its session started,
// turned, of which no warning said so before its editor took the terminal.
// That one, and one whose run is gone that holds anything but the files as
// merge-file wrote them, see spent, are given up to the user instead, see
// releaseEditorDir, and returned, so that the run names them; every other
// one goes.
func (inv *invocation) clearEditorDirs(name string, turned map[string]bool) (released []string) {
	for _, d := range inv.earlierDirs(name) {
		if !d.spent(nil) && (d.gone || turned[d.path]) {
			releaseEditorDir(d.path)
			released = append(released, d.path)
			continue
		}
		os.RemoveAll(d.path)
	}
	return released
}

// keepEditorDirs settles the directories earlier editor sessions of the
// skill called name left for this home, see earlierDirs, once its merge is
// given up, as the run that gave it up does. One that holds nothing but the
// files as merge-file wrote them goes, see spent; every other one is kept,
// marked so when its run is gone, and returned, so that the run names it:
// what was typed there is opened again by the next editor session of the
// same conflict, once an update of the skill leaves one pending again. A
// session still open is left alone: it finds the merge gone once its editor
// exits, see edit.
func (inv *invocation) keepEditorDirs(name string) (kept []string) {
	for _, d := range inv.earlierDirs(name) {
		if d.spent(nil) {
			os.RemoveAll(d.path)
			continue
		}
		if d.gone {
			keepEditorDir(d.path)
		}
		kept = append(kept, d.path)
	}
	return kept
}

// onlyOwner reports whether the directory an editor session laid its files
// out in holds nothing but its owner file, as a run killed after it wrote
// that file and before it made the skill's directory leaves it, see
// layOutForEditor.
func onlyOwner(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) == 1 && entries[0].Name() == editorOwner && entries[0].Type().IsRegular()
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
