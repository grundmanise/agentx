package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// conflictEvent is one skill whose edits and whose update conflict: the
// three versions the merge took, each a commit of the account repo, and
// every file that conflicts, with every hunk of it. It is emitted by an
// update that leaves a merge pending, and says everything a hunk chooser
// needs without reading the account repo.
type conflictEvent struct {
	event
	Name   string         `json:"name"`
	Kind   string         `json:"kind"`   // managed, the one kind that merges its update today
	Base   string         `json:"base"`   // the import commit the skill's branch points at
	Mine   string         `json:"mine"`   // the library directory, committed on base
	Theirs string         `json:"theirs"` // the update candidate
	Files  []conflictFile `json:"files"`
}

// conflictFile is one file of a merge that conflicts, at its path relative
// to the skill's directory: the blob each of the three versions holds for
// it, null for a version that has no file there, and its hunks. A file that
// is binary in any of them, or that is anything but a file in one, or that
// one side deleted, conflicts whole and has no hunk.
type conflictFile struct {
	Path   string         `json:"path"`
	Base   *string        `json:"base"`
	Mine   *string        `json:"mine"`
	Theirs *string        `json:"theirs"`
	Binary bool           `json:"binary"`
	Hunks  []conflictHunk `json:"hunks"`
	why    string         // what conflicts, for a file that conflicts whole: the words the text says it in
}

// conflictHunk is one region of a file that conflicts: what each of the
// three versions holds there, as text, every line with the ending it has,
// and a last line of the file without one as the file has it. Hunks are
// numbered from 1 in the order they come in the file.
type conflictHunk struct {
	Index  int    `json:"index"`
	Mine   string `json:"mine"`
	Base   string `json:"base"`
	Theirs string `json:"theirs"`
}

// mergeResult is what git made of the three versions of a merge: the root
// tree merge-tree wrote, which wraps the skill in its upstream directory as
// the three versions do, and the files that conflict, sorted by path, none
// for a merge that is clean. The tree of a merge that conflicts holds
// markers in the files that do, and is never laid out anywhere an agent
// reads.
type mergeResult struct {
	tree  string
	files []conflictFile
}

// The stages merge-tree names the versions of a conflicted file by.
const (
	gitStageBase   = "1"
	gitStageMine   = "2"
	gitStageTheirs = "3"
)

// staged is one version of a conflicted file as merge-tree names it.
type staged struct {
	mode, oid string
}

// mergeVersions merges the three versions m names, whose trees wrap the
// skill in the upstream directory dir, in the isolated environment: one
// merge-tree with the base given, so that git finds nothing else to merge
// from, and git's rules for files added, deleted or renamed on either side.
// Which files conflict is read from the stages merge-tree lists for them,
// and from nothing else. The hunks of each are read from the three blobs
// of those stages, all of them read in one cat-file, as git merge-file
// writes them, one merge-file per file: the tree merge-tree wrote is not
// read for them, since its markers are git's default size and a line of
// the file itself may look like one.
//
// The three versions decide everything here, so the same three merge to
// the same tree, the same files and the same hunks, numbered alike, on any
// later run: which is how a pending merge, whose commit names the three in
// its trailers, is read back.
func mergeVersions(ctx context.Context, r *gitx.Runner, gitDir, dir string, m lineage.Merge) (mergeResult, error) {
	out, status, err := r.IsolatedStatus(ctx, gitDir, 1,
		"merge-tree", "--write-tree", "-z", "--no-messages", "--merge-base="+m.Base, m.Mine, m.Theirs)
	if err != nil {
		return mergeResult{}, err
	}
	fields := strings.Split(out, "\x00")
	res := mergeResult{tree: fields[0]}
	if !lineage.IsObjectID(res.tree) {
		return mergeResult{}, fmt.Errorf("git merge-tree: cannot read the tree of %q", out)
	}
	stages := map[string]map[string]staged{}
	for _, f := range fields[1:] {
		if f == "" {
			continue
		}
		meta, path, ok := strings.Cut(f, "\t")
		parts := strings.Fields(meta)
		if !ok || len(parts) != 3 {
			return mergeResult{}, fmt.Errorf("git merge-tree: cannot read the conflicted file %q", f)
		}
		rel, inside := strings.CutPrefix(path, dir+"/")
		if !inside {
			return mergeResult{}, fmt.Errorf("git merge-tree: the conflicted file %q is outside %s", path, dir)
		}
		if stages[rel] == nil {
			stages[rel] = map[string]staged{}
		}
		stages[rel][parts[2]] = staged{mode: parts[0], oid: parts[1]}
	}
	switch {
	case status == 0 && len(stages) > 0:
		return mergeResult{}, fmt.Errorf("git merge-tree: a clean merge names %d conflicted files", len(stages))
	case status == 1 && len(stages) == 0:
		return mergeResult{}, fmt.Errorf("git merge-tree: a merge that conflicts names no conflicted file")
	case status == 0:
		return res, nil
	}
	var ids []string
	for _, versions := range stages {
		for _, v := range versions {
			if source.IsFileMode(v.mode) {
				ids = append(ids, v.oid)
			}
		}
	}
	bodies, err := source.ReadBlobs(ctx, r, gitDir, ids)
	if err != nil {
		return mergeResult{}, err
	}
	paths := make([]string, 0, len(stages))
	for p := range stages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var scratch string
	defer func() {
		if scratch != "" {
			os.RemoveAll(scratch)
		}
	}()
	for _, p := range paths {
		f, text := conflictOf(p, stages[p], bodies)
		if text {
			if scratch == "" {
				if scratch, err = os.MkdirTemp("", "agentx-merge-"); err != nil {
					return mergeResult{}, err
				}
			}
			if f.Hunks, err = hunksOf(ctx, r, gitDir, scratch, stages[p], bodies); err != nil {
				return mergeResult{}, err
			}
			// A file whose content merge-file merges with no hunk left
			// still conflicts in what else merge-tree decided about it,
			// its mode say, and conflicts whole.
			if len(f.Hunks) == 0 {
				f.why = "changed here and by the update"
			}
		}
		res.files = append(res.files, f)
	}
	return res, nil
}

// conflictOf is the conflicted file at path as its stages name it, and
// whether its hunks can be read: the file is in both versions merged and
// is a text file in every version that holds it. Anything else conflicts
// whole, and why says how in the words the text output prints.
func conflictOf(path string, versions map[string]staged, bodies map[string]string) (conflictFile, bool) {
	f := conflictFile{Path: path, Hunks: []conflictHunk{}}
	id := func(stage string) *string {
		if v, ok := versions[stage]; ok {
			oid := v.oid
			return &oid
		}
		return nil
	}
	f.Base, f.Mine, f.Theirs = id(gitStageBase), id(gitStageMine), id(gitStageTheirs)
	files := true
	for _, v := range versions {
		switch {
		case !source.IsFileMode(v.mode):
			files = false
		case isBinary(bodies[v.oid]):
			f.Binary = true
		}
	}
	switch {
	case f.Mine == nil && f.Theirs != nil:
		f.why = "deleted here, changed by the update"
	case f.Theirs == nil && f.Mine != nil:
		f.why = "changed here, deleted by the update"
	case f.Mine == nil || f.Theirs == nil:
		f.why = "moved or deleted on both sides"
	case f.Binary && f.Base == nil:
		f.why = "binary, added here and by the update"
	case f.Binary:
		f.why = "binary, changed here and by the update"
	case !files:
		f.why = "changed here and by the update, and not a file in every version"
	default:
		return f, true
	}
	return f, false
}

// isBinary is git's own test of whether content is binary, the one
// merge-file refuses to merge: a NUL byte among its first 8000.
func isBinary(body string) bool {
	return strings.IndexByte(body[:min(len(body), 8000)], 0) >= 0
}

// hunksOf reads the hunks of one text file that conflicts: its three
// versions are written into scratch, an empty file for a base that has
// none, as an add on both sides is, and git merge-file writes their merge
// in zdiff3 style with markers longer than any run of marker characters at
// the start of a line of any of the three, so that no line of the file is
// ever read as a marker. The hunks are read from the regions between those
// markers, in order.
func hunksOf(ctx context.Context, r *gitx.Runner, gitDir, scratch string, versions map[string]staged, bodies map[string]string) ([]conflictHunk, error) {
	text := func(stage string) string {
		if v, ok := versions[stage]; ok {
			return bodies[v.oid]
		}
		return ""
	}
	mine, base, theirs := text(gitStageMine), text(gitStageBase), text(gitStageTheirs)
	files := make([]string, 3)
	for i, body := range []string{mine, base, theirs} {
		files[i] = filepath.Join(scratch, fmt.Sprint(i))
		if err := os.WriteFile(files[i], []byte(body), 0o600); err != nil {
			return nil, err
		}
	}
	size := markerSize(mine, base, theirs)
	out, _, err := r.IsolatedStatus(ctx, gitDir, 127, "merge-file", "-p", "--zdiff3", fmt.Sprintf("--marker-size=%d", size),
		"-L", "mine", "-L", "base", "-L", "theirs", files[0], files[1], files[2])
	if err != nil {
		return nil, err
	}
	return parseHunks(out, size, mine, base, theirs)
}

// markerSize is the length of the markers merge-file is to write between
// the versions mine, base and theirs: git's own seven, or one more than the
// longest run of one of the four marker characters at the start of a line
// of any of them.
func markerSize(texts ...string) int {
	longest := 0
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			if line == "" || !strings.ContainsRune("<=|>", rune(line[0])) {
				continue
			}
			longest = max(longest, len(line)-len(strings.TrimLeft(line, line[:1])))
		}
	}
	return max(7, longest+1)
}

// parseHunks reads the hunks out of what merge-file wrote with markers of
// size: every region from a line of that many < to one of that many >,
// with the base after the | line and theirs after the = line, as zdiff3
// writes them. merge-file ends a last line with no newline with one when a
// marker follows it, so a hunk that ends the file gives each version whose
// file has no newline at its end its last line back as that file has it.
func parseHunks(out string, size int, mine, base, theirs string) ([]conflictHunk, error) {
	isMarker := func(line string, c string) bool {
		bare := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		run := strings.Repeat(c, size)
		return bare == run || strings.HasPrefix(bare, run+" ")
	}
	const (
		outside = iota
		inMine
		inBase
		inTheirs
	)
	var hunks []conflictHunk
	var h conflictHunk
	at, last := outside, false
	for _, line := range strings.SplitAfter(out, "\n") {
		if line == "" {
			continue
		}
		last = false
		switch {
		case at == outside && isMarker(line, "<"):
			h, at = conflictHunk{Index: len(hunks) + 1}, inMine
		case at == inMine && isMarker(line, "|"):
			at = inBase
		case (at == inMine || at == inBase) && isMarker(line, "="):
			at = inTheirs
		case at == inTheirs && isMarker(line, ">"):
			hunks, at, last = append(hunks, h), outside, true
		case at == inMine:
			h.Mine += line
		case at == inBase:
			h.Base += line
		case at == inTheirs:
			h.Theirs += line
		}
	}
	if at != outside {
		return nil, fmt.Errorf("git merge-file: a conflict with no end")
	}
	if last {
		h := &hunks[len(hunks)-1]
		h.Mine, h.Base, h.Theirs = asAtEnd(h.Mine, mine), asAtEnd(h.Base, base), asAtEnd(h.Theirs, theirs)
	}
	return hunks, nil
}

// asAtEnd is the text of a hunk that ends the file whose content is file,
// with the line ending merge-file added to the last line taken off again
// when the file has none there.
func asAtEnd(text, file string) string {
	switch {
	case file == "" || strings.HasSuffix(file, "\n"):
		return text
	case strings.HasSuffix(text, "\r\n") && !strings.HasSuffix(file, "\r"):
		return strings.TrimSuffix(text, "\r\n")
	}
	return strings.TrimSuffix(text, "\n")
}

// conflictOfSkill is the conflict event of the skill called name, whose
// merge of m left files conflicting.
func conflictOfSkill(name string, m lineage.Merge, files []conflictFile) conflictEvent {
	return conflictEvent{event: newEvent("conflict"), Name: name, Kind: lineage.KindManaged, Base: m.Base, Mine: m.Mine, Theirs: m.Theirs, Files: files}
}

// printConflicts reports a merge left pending: one conflict event, and in
// the text a line naming the skill, the two upstream commits and the files
// that conflict, then every hunk of every file under its file and number,
// file:index as the hunk is named, between markers git's own size with the
// three versions' names on them, and every file that conflicts whole with
// what conflicts in it. Every line of a hunk holds the file's own bytes,
// so it is printed as a line of a diff is.
func (inv *invocation) printConflicts(ev conflictEvent, from, to string) {
	out := inv.out
	out.emit(ev)
	out.print(out.paint(heading, sanitised(ev.Name)), " conflicts with its update from ", from, " to ", to, " in ",
		out.paint(noteStyle, plural(len(ev.Files), "file")))
	for _, f := range ev.Files {
		if len(f.Hunks) == 0 {
			out.print(out.paint(label, quotedPath(f.Path)), ": ", f.why)
			continue
		}
		for _, h := range f.Hunks {
			out.print(out.paint(label, fmt.Sprintf("%s:%d", quotedPath(f.Path), h.Index)))
			for _, section := range []struct{ marker, text string }{
				{"<<<<<<< mine", h.Mine}, {"||||||| base", h.Base}, {"=======", h.Theirs},
			} {
				out.print(out.paint(muted, section.marker))
				printLines(out, section.text)
			}
			out.print(out.paint(muted, ">>>>>>> theirs"))
		}
	}
}

// printLines prints text line by line, each as a line of a diff is.
func printLines(out *writer, text string) {
	for _, line := range strings.SplitAfter(text, "\n") {
		if line != "" {
			out.print(patchLine(strings.TrimSuffix(line, "\n")))
		}
	}
}

// pendingMergeRefusal refuses to do what to the skill called name while an
// update left a merge pending for it: the merge holds the library
// directory as it was and the update it was merged with, and updating,
// reverting or removing the skill would leave it merging versions that are
// no longer there. It is exit code 4, whatever else would be wrong with
// the skill, and the hint names the way out that keeps the library
// directory as it is. Everything that replaces a skill's content or takes
// it away calls it, under the lock as well as before it.
func pendingMergeRefusal(name, what string) *failure {
	return refuse(exitPendingMerge, name+" has a merge with its update pending, so it cannot be "+what+" until the merge is resolved or given up",
		"run '"+skillCommand("resolve", name, "--abort")+"' to give the merge up; the library directory stays as it is")
}
