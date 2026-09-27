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
// them at least: what was carried over leaves the directory it came from,
// and every one that still holds something typed is kept and named in a
// warning that says this session did not open it.
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
// The directory is removed once what the editor left is written, or found
// to hold nothing typed, and never otherwise: agentx deletes nothing typed.
// Anything typed there that is not written keeps it: every file, when the
// editor fails or the run refuses to write, a file left with markers and
// something typed, a file the editor resolved that a merge done again, a
// completion of an edited library directory or a run on a moved import
// branch, makes conflict anew or merge cleanly, git's merge of it written
// in place of what was typed, and anything saved under another name, a
// swap file say; the files the run wrote into the merge leave it first.
// While the merge is pending it is marked kept, and the next session of
// the same conflict opens what was typed there again, see reopen, which a
// warning says, see kept; one whose merge was given up while the editor
// was open, once an update leaves it pending anew, see keptGivenUp. Once
// the merge is complete, completed by this run or by another, it is given
// up to the user instead, see releaseEditorDir, and the warning says so.
// Which of these it is, is read under the lock, waited for, see keep.
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
	var unopened, carried map[string]bool
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
		var mods []time.Time
		texts, from, mods = reopen(earlier, files, r.resolved, r.pending.Commit)
		if dir, paths, records, err = inv.layOutForEditor(name, r.pending.Commit, files, texts, mods); err != nil {
			return err
		}
		if editorLayoutHook != nil {
			editorLayoutHook("laid out")
		}
		laid := map[string]laidFile{}
		for i, f := range files {
			laid[f.Path] = laidFile{record: records[i], text: texts[i]}
		}
		unopened, carried = settle(earlier, name, laid)
		return nil
	})
	if err != nil {
		return mutationFailure(err)
	}
	for i, f := range files {
		if from[i] != "" {
			inv.out.info(quotedPath(f.Path) + " opens with what you typed in an earlier session, carried over from " + from[i])
		}
	}
	for _, d := range slices.Sorted(maps.Keys(unopened)) {
		what := "what you typed there"
		if carried[d] {
			what = "what else you typed there"
		}
		inv.out.warn("the files you edited in an earlier session are kept in " + d + keptNotOpened(what))
	}
	// keep settles the directory of a run that did not write everything
	// typed there, and reports whether it stays, and whether it is given up
	// to the user, or kept for a merge given up meanwhile, rather than kept
	// for the merge pending. One whose merge was completed or given up
	// meanwhile goes when nothing was typed there: there is nothing to open
	// again. One whose merge was completed meanwhile, the import branch
	// moved on from where the run read it, is given up to the user, see
	// releaseEditorDir: the conflict it was typed for cannot come back.
	// Every other one is kept, marked so, for the next session of the same
	// conflict to open again, one whose merge was given up meanwhile once an
	// update leaves it pending anew, which givenUp says, since no session
	// opens it before.
	//
	// It reads the refs and settles the directory under the lock, so that
	// its warning tells the merge as the last command left it, and waits for
	// a held lock rather than giving up, since a run whose write a held lock
	// refused comes here too: a completion another run makes meanwhile
	// either finishes first, and the directory is given up here, or starts
	// once it is kept, and gives it up in turn, see clearEditorDirs. A wait
	// that fails, the run stopped say, keeps it.
	keep := func() (stays, released, givenUp bool) {
		stays = true
		inv.out.debugf("taking the lock to settle %s, waiting while another command holds it", dir)
		err := home.MutateQuietWaiting(ctx, inv.dirs.Home, inv.refs(ctx), func() error {
			values, err := inv.lineageRefs(ctx, r.gitDir, name)
			switch {
			case err != nil:
				return err
			case values[lineage.MergeRef(name)] != "":
				keepEditorDir(dir)
			case untouched(dir, name, records):
				os.RemoveAll(dir)
				stays = false
			case values[lineage.ManagedRef(name)] != r.rec.Commit:
				releaseEditorDir(dir)
				released = true
			default:
				keepEditorDir(dir)
				givenUp = true
			}
			return nil
		})
		if err != nil {
			keepEditorDir(dir)
			return true, false, false
		}
		return stays, released, givenUp
	}
	// kept settles the directory of a run that writes nothing of what the
	// editor left, and names it in a warning that says what opens it again.
	// A merge given up meanwhile is worded as giving it up words it, see
	// keptGivenUp, the merge given up taken for the last one the run read,
	// the one it laid the files out from. A directory that holds nothing
	// typed but under another name, a swap file an editor left say, which
	// no session opens, promises nothing opens again, see keptUnopened. A
	// session of one file the merge records as resolved, which only one
	// named after --editor is, is opened again only by a session that names
	// it, and what holds no marker only while the merge is the one it was
	// typed in, see reopen, which the warning says. Any other session whose
	// merge another run moved meanwhile, resolving one of its files
	// perhaps, promises no more than keptMoved does.
	kept := func() {
		stays, released, givenUp := keep()
		k := keptOf(sessionDir(dir, name, records), records)
		moved := func() bool {
			// A merge ref that cannot be read, in a run stopped say, is not
			// taken for one that changed.
			values, err := inv.lineageRefs(ctx, r.gitDir, name)
			return err == nil && values[lineage.MergeRef(name)] != r.pending.Commit
		}
		end := opensAgain(name, "", "them")
		switch {
		case released:
			end = givenUpToYou(name)
		case givenUp:
			end = keptGivenUp(name, k)
		case !stays:
			return
		case !k.reopens && k.rest:
			end = keptUnopened()
		case s.file != "" && r.resolved[files[0].Path]:
			p := files[0].Path
			end = opensAgain(name, p, "them")
			if body, err := os.ReadFile(paths[0]); err == nil && !holdsMarkers(string(body), files[0].text.size) {
				end = keptWhileUnchanged(name, p, moved())
			}
		case moved():
			end = keptMoved(name)
		}
		inv.out.warn("the files you edited are kept in " + dir + end)
	}

	release := interrupt.Hold(ctx)
	err = inv.runEditor(ctx, s, paths)
	release()
	if err != nil {
		kept()
		named := ""
		if s.file != "" {
			named = files[0].Path
		}
		return fail(exitRefused, fmt.Sprintf("the editor %s %s, so nothing was written", s.command, err),
			"run '"+resolveInEditor(name, named)+"' again once the editor works, or set GIT_EDITOR or EDITOR to another")
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
	if len(rs) > 0 && editorLayoutHook != nil {
		editorLayoutHook("written")
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
	// session of the same conflict opens again, a file the editor resolved
	// that conflicts anew or merges cleanly now, and anything else, a file
	// saved under another name, a swap file an editor left or a symlink say,
	// see keptOf, which no session opens. While the merge is pending it is
	// marked kept; a run that completed the merge gives it up to the user,
	// and so does one that finds the merge completed by another run
	// meanwhile, see keep. pending says it is kept for the merge pending,
	// whose next session of the same conflict opens it again: that of every
	// file left, or, for a file the merge records as resolved, the one that
	// names it. elsewhere ends the warning that names it where only
	// something under another name keeps it, and given that of a file whose
	// typed text no session opens again, which says more only once the
	// directory is given up to the user.
	named := slices.ContainsFunc(left, func(p string) bool { return typed[p] }) || len(anew)+len(clean) > 0
	held, given, again, elsewhere := "", "", opensAgain(name, "", "it"), keptUnopened()
	pending := false
	switch {
	case !named && !keptOf(sessionDir(dir, name, records), records).rest:
		os.RemoveAll(dir)
	case r.completed:
		releaseEditorDir(dir)
		held, given = dir, givenUpToYou(name)
		again, elsewhere = given, given
	default:
		// What the run wrote into the merge is there now, so its file leaves
		// the directory before the directory is kept: a kept directory holds
		// nothing typed but what was not written, and a later session opens
		// such a file as the merge holds it, never what the merge replaced
		// since. Its line stays in the owner file, and reads as a file
		// deleted, with nothing typed in it, see spent.
		for _, res := range rs {
			if r.carried == nil || r.carried[res.path] {
				_ = os.Remove(filepath.Join(dir, name, filepath.FromSlash(res.path)))
			}
		}
		switch stays, released, givenUp := keep(); {
		case released:
			held, given = dir, givenUpToYou(name)
			again, elsewhere = given, given
		case givenUp:
			// The merge given up is taken for the one the run wrote: the one
			// it laid the files out from, or the one it merged again.
			conflicts := records
			if r.carried != nil {
				conflicts = r.remerged
			}
			held, again = dir, keptGivenUp(name, keptOf(sessionDir(dir, name, records), conflicts))
		case stays:
			held, pending = dir, true
		}
	}
	for _, p := range left {
		why, how, stays := " still holds conflict markers", ", so it was left unresolved", again
		if gone[p] {
			why = " is gone"
		}
		switch {
		case r.carried != nil && !r.left[p]:
			how, stays = ", and merges cleanly now that the merge was merged again, so git's merge of it was written", given
		case r.resolved[p]:
			how = ", so it keeps the way it was resolved before"
			if pending {
				stays = opensAgain(name, p, "it")
			}
		}
		warning := quotedPath(p) + why + how
		if typed[p] && held != "" {
			warning += "; what you typed is kept in " + held + stays
		}
		inv.out.warn(warning)
	}
	if held != "" {
		for _, p := range anew {
			inv.out.warn(quotedPath(p) + " conflicts anew now that the merge was merged again, so what you typed for it is kept in " + held + given)
		}
		for _, p := range clean {
			inv.out.warn(quotedPath(p) + " merges cleanly now that the merge was merged again, so what you typed for it was not written and is kept in " + held + given)
		}
		if !named {
			inv.out.warn("the files you edited are kept in " + held + elsewhere)
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

// editorLayingOut is the directory beside the owner file that an editor
// session writes its files in first, see layOutForEditor, and renames to
// the skill's name once every one of them is whole, so that no run ever
// finds the skill's directory with a file in it cut short. Its name is a
// hidden one, which the library never gives a skill.
const editorLayingOut = ".laying-out"

// editorLayoutHook is nil but in the crash tests of skill resolve, which
// set it in a process of their own to kill that process at a step of
// laying out an editor session's files: "staged", once every file is
// written in editorLayingOut, and "laid out", once they are in place and
// before the directories of earlier sessions are settled, see edit; or at
// the step of keeping a directory, "keeping", once keepEditorDir has
// written the owner file anew beside the old one and before it renames it
// over it; and in a test that runs alone, never in parallel, which holds
// the run at "written", once it has written what the editor resolved and
// before it settles its directory, see edit, while another run completes
// the merge or gives it up. No command the run starts falls between those
// steps, so nothing else lets a test stop the run there.
var editorLayoutHook func(step string)

// editorKept stands in for the process id in the owner file of a directory
// kept so that nothing typed there is lost: by its own run, which did not
// write what was typed there, or by a later run that found its run gone
// and something typed there. A later session of the same conflict opens
// what was typed there again, and removes it only once everything typed
// there is carried into its own directory; every other session of the
// skill names it, see settle. Completing a merge of the skill gives it up
// to the user, see clearEditorDirs: agentx never removes it with something
// typed in it.
const editorKept = "kept"

// keepEditorDir marks the directory an editor session laid its files out
// in as kept, see editorKept. The owner file is written anew beside the
// old one and renamed over it, so that a run killed on the way leaves the
// old one whole, never one cut short, which would leave the directory to
// no run: what is left beside it then is no file typed in, see
// ownerBeingKept. A directory whose owner file cannot be rewritten is left
// as it is; it is then taken for one whose run was killed.
func keepEditorDir(dir string) {
	path := filepath.Join(dir, editorOwner)
	owner, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_, rest, _ := strings.Cut(string(owner), "\n")
	f, err := os.CreateTemp(dir, editorOwner+".")
	if err != nil {
		return
	}
	_, err = f.WriteString(editorKept + "\n" + rest)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if editorLayoutHook != nil {
			editorLayoutHook("keeping")
		}
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
}

// ownerBeingKept reports whether e, an entry of the directory an editor
// session laid its files out in, is what a run killed as it kept the
// directory leaves: the owner file keepEditorDir wrote anew and had not
// yet renamed over the old one, a regular file named after the owner file
// and a dot.
func ownerBeingKept(e fs.DirEntry) bool {
	return strings.HasPrefix(e.Name(), editorOwner+".") && e.Type().IsRegular()
}

// releaseEditorDir gives the directory an editor session laid its files out
// in up to the user, once the merge what was typed there was for is
// complete: its owner file goes, so that no run takes it for a session's,
// see earlierDirs, and agentx never removes it.
func releaseEditorDir(dir string) {
	_ = os.Remove(filepath.Join(dir, editorOwner))
}

// leftToYou is the warning that names the directory of an earlier session
// that holds something typed, once completing the merge gives it up to the
// user, see clearEditorDirs.
func leftToYou(dir, name string) string {
	return "the files you edited in an earlier session are kept in " + dir + givenUpToYou(name)
}

// givenUpToYou ends a warning that names a directory given up to the user
// once the merge of the skill called name is complete, see
// releaseEditorDir: no session opens what was typed there again, agentx
// never removes it, and the operating system may clean it.
func givenUpToYou(name string) string {
	return ", which agentx leaves to you now that the merge of " + name + " is complete; the folder is in your temporary directory, which your system may clean, so copy what you need from it and delete it"
}

// opensAgain ends a warning that names a directory kept for what was typed
// in it, it or them: the next editor session of the same conflict opens
// what was typed there again. That is the session of every file left to
// resolve, or, for file, one the merge records as resolved, which no such
// session opens, the one that names it.
func opensAgain(name, file, what string) string {
	return ", and the next '" + resolveInEditor(name, file) + "' of the same conflict opens " + what + " again"
}

// keptWhileUnchanged ends the warning that names a directory kept for what
// was typed in file, one the merge records as resolved, with no marker left
// in it: the session that names file opens it again only while the merge
// is the one it was typed in, see reopen, since closed unchanged it
// replaces the resolution. changed says the merge has changed already, so
// that it is not opened again while file stays resolved.
func keptWhileUnchanged(name, file string, changed bool) string {
	if changed {
		return "; what you typed there is not opened again while " + quotedPath(file) + " stays resolved, since the merge of " + name + " changed while your editor was open, so copy what you need from it"
	}
	return ", and '" + resolveInEditor(name, file) + "' opens them again if it runs before anything else changes the merge of " + name + "; otherwise copy what you need from it"
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

// keptNotOpened ends the warning that names the directory of an earlier
// session a session keeps as it starts, see settle: what, what was typed
// there or, where something typed there was carried over, what else was,
// was not opened, with every reason there can be, so that the user copies
// what they need from it rather than wait for it to come back, which it
// may never do.
func keptNotOpened(what string) string {
	return "; " + what + " was not opened, because its conflict changed, its file is resolved or was not named, a newer copy was opened, or it is saved under another name, so copy what you need from it"
}

// keptGivenUp ends the warning that names a directory giving the merge of
// the skill called name up keeps, see keepEditorDirs, or that a session
// whose merge was given up while its editor was open keeps, see edit, k
// telling what it holds, see keptOf: where it holds something typed for a
// conflict of the merge given up, the next editor session of the same
// conflict opens that again once an update leaves the merge pending anew.
// What was typed for a conflict that changed before, which an update of
// the merge given up does not make again, and what it holds under any
// other name, a swap file an editor left say, are opened by no session,
// and the warning says so. Nothing reads the directory until then, and the
// operating system may clean its temporary directory meanwhile, so the
// warning says to copy what is worth keeping.
func keptGivenUp(name string, k keptDir) string {
	const changed = "its conflict changed before the merge was given up"
	switch {
	case !k.reopens && !k.changed:
		return keptUnopened()
	case !k.reopens && k.rest:
		return "; what you typed there does not open again, because " + changed + " or it is saved under another name, such as a swap file an editor left, so copy what you need from it"
	case !k.reopens:
		return "; what you typed there does not open again, because " + changed + ", so copy what you need from it"
	}
	again := ": when an update of " + name + " conflicts the same way, '" + skillCommand("resolve", name, "--editor") + "' opens what you typed again"
	if k.changed {
		again += "; what you typed for a conflict that changed before the merge was given up does not open again"
	}
	if k.rest {
		again += "; a file saved there under another name, such as a swap file an editor left, does not open again"
	}
	return again + "; the folder is in your temporary directory, which your system may clean, so copy what you want to keep, and delete it if you do not need it"
}

// keptUnopened ends the warning that names a directory kept for nothing
// but what is saved there under another name, a swap file an editor left
// say, which no session opens, see keptOf: nothing there opens again.
func keptUnopened() string {
	return "; what you typed there does not open again, because it is saved under another name, such as a swap file an editor left, so copy what you need from it"
}

// keptMoved ends the warning that names the directory of a session that
// writes nothing once another run moved the merge of the skill called name
// while its editor was open: the next session of every file left to
// resolve opens again what was typed for each such file with the same
// conflict. A file the other run resolved is not, since that session does
// not lay it out, and the session that names it opens what holds no marker
// only while the merge is the one it was typed in, see reopen.
func keptMoved(name string) string {
	return "; the next '" + resolveInEditor(name, "") + "' opens what you typed again for each file still left to resolve with the same conflict, but not for a file resolved while your editor was open, so copy what you need from it"
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
	return sessionDir(dir, name, records).spent()
}

// sessionDir is the directory an editor session of the skill called name
// laid its files out in, dir, as a later run finds it: records are the
// lines of its owner file that record the files, see editorRecord.
func sessionDir(dir, name string, records []string) earlierDir {
	d := earlierDir{path: dir, records: records, byPath: recordsByPath(records)}
	d.files, d.other = savedFiles(dir, name)
	return d
}

// earlierDir is a directory an earlier editor session of a skill laid its
// files out in for this agentx home, as a run finds it once that session's
// run is gone or kept it: what its owner file records and what it holds.
type earlierDir struct {
	path    string
	gone    bool                 // its run is gone without keeping it, killed or hung up on say; otherwise the directory is kept
	commit  string               // the pending merge commit its files were laid out from, as its owner file names it
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

// keptDir is a directory of an earlier editor session that giving a merge
// up keeps, see keepEditorDirs, or of a session whose merge was given up
// while its editor was open, see edit, and what it holds: something typed
// in a file its owner file records for a conflict of the merge given up,
// which the next editor session of the same conflict opens again once an
// update leaves it pending anew, see reopen, something typed for a
// conflict the merge given up no longer has, and anything else, which no
// session opens.
type keptDir struct {
	path                   string
	reopens, changed, rest bool
}

// keptOf is what d holds, as the warning that names it once the merge is
// given up tells it, see keptGivenUp: given are the lines an owner file
// records the conflicts of that merge with, see conflictRecords, nil when
// they are not known, and nothing typed is then promised to open again.
func keptOf(d earlierDir, given []string) keptDir {
	k := keptDir{path: d.path, rest: d.other || d.byPath == nil}
	for p, f := range d.files {
		switch record := d.byPath[p]; {
		case record == "":
			k.rest = true
		case editorRecord(p, []byte(f.body)) == record:
		case slices.Contains(given, record):
			k.reopens = true
		default:
			k.changed = true
		}
	}
	return k
}

// conflictRecords is the line an editor session's owner file records each
// text file of files with, see editorRecord: the conflicts a session of a
// merge that conflicts in files lays out, whether a file is resolved in it
// or not.
func conflictRecords(files []conflictFile) []string {
	var records []string
	for _, f := range files {
		if f.text != nil {
			records = append(records, editorRecord(f.Path, []byte(editorText(f.text))))
		}
	}
	return records
}

// savedFiles reads what the directory an editor session of the skill called
// name laid its files out in holds: every regular file under the skill's
// directory, by its path relative to it, and other when there is anything
// else, the owner file aside, and one a run killed as it kept the
// directory left beside it, see keepEditorDir. A directory that cannot be
// read through is taken for one that holds something else.
func savedFiles(dir, name string) (files map[string]savedFile, other bool) {
	files = map[string]savedFile{}
	root := filepath.Join(dir, name)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case p == dir, p == root, p == filepath.Join(dir, editorOwner), filepath.Dir(p) == dir && ownerBeingKept(d):
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

// spent reports whether nothing typed in d is lost when it goes: it holds
// nothing but files, each as merge-file wrote it, see editorRecord. Nothing
// else is there: no swap or backup file an editor writes beside the one it
// opens, no file saved under another name. A recorded file that is not
// there was deleted, or carried into a newer session's directory, see
// settle, and holds nothing typed; one cut short by a run killed as it laid
// them out is never there, see layOutForEditor. An owner file that records
// no file tells nothing, and its directory is taken for one that holds
// something typed.
func (d earlierDir) spent() bool {
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
		if !written[editorRecord(p, []byte(f.body))] {
			return false
		}
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
// that holds no file the editor was given, as a run killed as it laid the
// files out leaves it, see laidOutNothing, names no skill and goes,
// whichever skill's run finds it: nothing was typed there. One that holds
// no directory of the skill is some other skill's, or holds something
// saved beside its owner file, and is left as it is. So is one that is not
// this user's own, see ownDir.
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
		if !ownDir(dir) {
			continue
		}
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
			if gone && laidOutNothing(dir) {
				os.RemoveAll(dir)
			}
			continue
		}
		var records []string
		if len(lines) > 3 {
			for _, line := range lines[3:] {
				if line != "" {
					records = append(records, line)
				}
			}
		}
		d := sessionDir(dir, name, records)
		d.gone = gone
		if len(lines) > 2 {
			d.commit = lines[2]
		}
		dirs = append(dirs, d)
	}
	return dirs
}

// ownDir reports whether dir, a directory an editor session could have
// made, is this user's own and no other user can write in it, as every one
// a session makes is: one another user made, or could have put a file in,
// is never read, opened from or removed.
func ownDir(dir string) bool {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// reopen is what each of files is laid out with: what was typed for it in
// an earlier session, where the directory of one records the file for the
// same conflict, see editorRecord, and holds a regular file at its path
// with something else in it, the most recently saved where several do; and
// otherwise the file as merge-file wrote it, see editorText. from is the
// directory each comes from, "" for a file laid out afresh, and mods when
// what was typed there was saved, which the file laid out keeps, so that
// the most recently saved is still told by when it was typed. The pending
// merge commit a directory's owner file names plays no part in which
// conflict it is: a merge given up and left again by an update, or
// rewritten by a resolve since, conflicts the same way in a file whose
// three versions are the same.
//
// What was typed for a file the merge records as resolved, which only a
// file named after --editor is, is opened while it holds a marker: closed
// unchanged, it keeps the way the file was resolved. What holds none would
// replace that resolution, so it is opened only where the directory was
// laid out from the merge as it is now, pending, the commit its owner file
// names: typed once the file was resolved, as the session that names it
// types it, by a run whose editor failed say, and meant to replace the
// resolution it was typed against. What was typed before the file was
// resolved, or before any other change to the merge, is not opened.
func reopen(dirs []earlierDir, files []*conflictFile, resolved map[string]bool, pending string) (texts, from []string, mods []time.Time) {
	texts, from, mods = make([]string, len(files)), make([]string, len(files)), make([]time.Time, len(files))
	for i, f := range files {
		fresh := editorText(f.text)
		record := editorRecord(f.Path, []byte(fresh))
		texts[i] = fresh
		for _, d := range dirs {
			saved, ok := d.files[f.Path]
			switch {
			case !ok, d.byPath[f.Path] != record, saved.body == fresh:
				continue
			case resolved[f.Path] && !holdsMarkers(saved.body, f.text.size) && d.commit != pending:
				continue
			}
			if from[i] == "" || saved.mod.After(mods[i]) || saved.mod.Equal(mods[i]) && d.path > from[i] {
				texts[i], from[i], mods[i] = saved.body, d.path, saved.mod
			}
		}
	}
	return texts, from, mods
}

// settle settles the directories of earlier sessions of the skill called
// name, dirs, once a session has laid out laid, by path, and written every
// file of it: never before, so that a run killed in between leaves what
// was typed in one directory at least, and the next session opens it from
// whichever holds it. First each file the session laid out leaves every
// directory that records it for the same conflict and holds it just as the
// session laid it out, what was typed there and carried over included, the
// directory it came from among them, whether its run is gone or it is
// kept: the new directory holds it now, and a later session opens the file
// as this one leaves it, or as the merge holds it once this one writes it,
// never what that replaced. Its line stays in the owner file, and reads as
// a file deleted, with nothing typed in it, see spent. Then each directory
// is settled as giving the merge up settles it, see keepTyped: one left
// with nothing typed in it goes, and one that holds something else, typed
// for a conflict that changed or for a file the session does not lay out,
// what lost to a newer copy of the same file, or a file of any other name,
// a swap file an editor left say, is kept, marked so when its run is gone,
// and returned in named, so that the run names it, saying that what was
// typed there was not opened, see keptNotOpened, or what else was where
// something typed there was carried over, which carried says of every
// directory it holds. Every session names every such directory, whether or
// not an earlier run named it, so that each session tells where everything
// typed that it does not open is. A copy that lost to a newer one stays
// where it is, and a later session of the same conflict may open it again
// once no directory holds a newer copy, as after the newer one is written
// into the merge and the merge given up; the warning that named its
// directory said it was not opened.
func settle(dirs []earlierDir, name string, laid map[string]laidFile) (named, carried map[string]bool) {
	named, carried = map[string]bool{}, map[string]bool{}
	for _, d := range dirs {
		for p, f := range d.files {
			l, ok := laid[p]
			if !ok || d.byPath[p] != l.record || f.body != l.text {
				continue
			}
			if err := os.Remove(filepath.Join(d.path, name, filepath.FromSlash(p))); err != nil && !errors.Is(err, fs.ErrNotExist) {
				continue
			}
			delete(d.files, p)
			if editorRecord(p, []byte(l.text)) != l.record {
				carried[d.path] = true
			}
		}
		if keepTyped(d) {
			named[d.path] = true
		}
	}
	return named, carried
}

// keepTyped settles d, a directory of an earlier editor session, once
// nothing more is to be carried out of it: one that holds nothing typed,
// see spent, goes, since nothing typed is lost by that, and every other
// one is kept, marked so when its run is gone, see keepEditorDir, and
// reported to stay, so that the run names it.
func keepTyped(d earlierDir) (stays bool) {
	if d.spent() {
		os.RemoveAll(d.path)
		return false
	}
	if d.gone {
		keepEditorDir(d.path)
	}
	return true
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
// it, and which keeps the time it was saved, its mod, where that is not
// zero. The files are written in editorLayingOut first, which is renamed to
// the skill's name once every one is whole: a run killed on the way leaves
// no directory of the skill, so no later run takes a file cut short for
// what was typed, and nothing there is anything but what agentx wrote,
// which goes, see laidOutNothing. It returns the directory, each file's
// path and the line recording each, in the order of files.
func (inv *invocation) layOutForEditor(name, commit string, files []*conflictFile, texts []string, mods []time.Time) (dir string, paths, records []string, err error) {
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
	staging := filepath.Join(dir, editorLayingOut)
	if err := os.Mkdir(staging, 0o700); err != nil {
		os.RemoveAll(dir)
		return "", nil, nil, err
	}
	paths = make([]string, len(files))
	for i, f := range files {
		if !lineage.FilePath(f.Path) {
			os.RemoveAll(dir)
			return "", nil, nil, fmt.Errorf("%s is no path an editor can open inside %s", quotedPath(f.Path), dir)
		}
		staged := filepath.Join(staging, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(staged), 0o700); err != nil {
			os.RemoveAll(dir)
			return "", nil, nil, err
		}
		if err := os.WriteFile(staged, []byte(texts[i]), 0o600); err != nil {
			os.RemoveAll(dir)
			return "", nil, nil, err
		}
		if !mods[i].IsZero() {
			// Only which of two copies was saved last rides on it.
			_ = os.Chtimes(staged, mods[i], mods[i])
		}
		paths[i] = filepath.Join(dir, name, filepath.FromSlash(f.Path))
	}
	if editorLayoutHook != nil {
		editorLayoutHook("staged")
	}
	if err := os.Rename(staging, filepath.Join(dir, name)); err != nil {
		os.RemoveAll(dir)
		return "", nil, nil, err
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
// complete, as the run that completed it does, under the hold of the lock
// that deleted the merge ref: the merge what was typed there was for is
// done. One that holds nothing typed, see spent, goes. Every other one,
// kept or its run gone, is given up to the user, see releaseEditorDir, and
// returned, so that the run names it: agentx never removes what was typed.
// A session still open is left alone: it settles its own directory once
// its editor exits, see edit.
func (inv *invocation) clearEditorDirs(name string) (released []string) {
	for _, d := range inv.earlierDirs(name) {
		if d.spent() {
			os.RemoveAll(d.path)
			continue
		}
		releaseEditorDir(d.path)
		released = append(released, d.path)
	}
	return released
}

// keepEditorDirs settles the directories earlier editor sessions of the
// skill called name left for this home, see earlierDirs, once its merge is
// given up, as the run that gave it up does, and as a session settles them
// as it starts, see keepTyped. One that holds nothing but the files as
// merge-file wrote them goes, see spent; every other one is kept, marked
// so when its run is gone, and returned, so that the run names it: what
// was typed there in a file the owner file records is opened again by the
// next editor session of the same conflict, once an update of the skill
// leaves one pending again, and what it holds under any other name, a swap
// file an editor left say, never is, which the warning says, see
// keptGivenUp, as it does of what was typed for a conflict the merge
// given up no longer has, given being the lines an owner file records its
// conflicts with, see keptOf. A session still open is left alone: it finds
// the merge gone once its editor exits, see edit.
func (inv *invocation) keepEditorDirs(name string, given []string) (kept []keptDir) {
	for _, d := range inv.earlierDirs(name) {
		if keepTyped(d) {
			kept = append(kept, keptOf(d, given))
		}
	}
	return kept
}

// laidOutNothing reports whether the directory an editor session laid its
// files out in holds no file the editor was given: its owner file, and
// nothing beside it but what a run killed as it laid the files out leaves,
// editorLayingOut with the files written so far, the last perhaps cut
// short, see layOutForEditor, and an owner file being kept, see
// keepEditorDir. The editor is started only once the files are in place,
// and the directories of earlier sessions they were opened from are
// settled after that, so nothing typed is lost when it goes.
func laidOutNothing(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	owner := false
	for _, e := range entries {
		switch {
		case e.Name() == editorOwner && e.Type().IsRegular():
			owner = true
		case e.Name() == editorLayingOut && e.IsDir(), ownerBeingKept(e):
		default:
			return false
		}
	}
	return owner
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
