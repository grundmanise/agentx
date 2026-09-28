package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// conflictEvent is one skill whose edits and whose update conflict: the
// three versions the merge took, each a commit of the account repo and the
// base, HEAD and MERGE_HEAD of the merge in progress in the skill's
// checkout, and every file that conflicts there, with every hunk of it. It
// is emitted by an update that leaves a merge pending, and says everything
// a hunk chooser needs without reading the account repo.
type conflictEvent struct {
	event
	Name   string         `json:"name"`
	Kind   string         `json:"kind"`   // managed, the one kind that merges its update today
	Base   string         `json:"base"`   // the import commit the skill's branch points at
	Mine   string         `json:"mine"`   // the library directory as git records it, files it ignores aside, committed on base
	Theirs string         `json:"theirs"` // the update candidate
	Files  []conflictFile `json:"files"`
}

// conflictFile is one file of a merge that conflicts, an unmerged path of
// the checkout at its path relative to the skill's directory: the blob
// each of the three versions holds for it, null for a version that has no
// file there, and its hunks. A file that is binary in any of them, or that
// is anything but a file in one, that one side deleted, or that holds no
// conflict marker agentx can tell from its own lines, conflicts whole and
// has no hunk; any other has one at least.
type conflictFile struct {
	Path   string         `json:"path"`
	Base   *string        `json:"base"`
	Mine   *string        `json:"mine"`
	Theirs *string        `json:"theirs"`
	Binary bool           `json:"binary"`
	Hunks  []conflictHunk `json:"hunks"`
	why    string         // what conflicts, for a file that conflicts whole: the words the text says it in
	text   *conflictText  // for a text file in every version that holds it, what a resolve rewrites it from; nil for any other
}

// conflictText is one text file of a merge as a resolve rewrites it: what
// its versions hold, as the index keeps them, and the file the merge
// wrote, split at its marker blocks, see splitMerged. A file with no block
// agentx can tell has no raw hunk and conflicts whole.
type conflictText struct {
	mine, base, theirs string
	around             []string       // the text between the blocks, one piece more than there are
	raw                []conflictHunk // the blocks, as the file holds them
}

// conflictHunk is one region of a file that conflicts, one marker block of
// the checkout's file: what each of the three versions holds there, as
// text, every line with the ending it has, and a last line of the file
// without one as the file has it. Hunks are numbered from 1 in the order
// they come in the file.
type conflictHunk struct {
	Index  int    `json:"index"`
	Mine   string `json:"mine"`
	Base   string `json:"base"`
	Theirs string `json:"theirs"`
}

// mergeResult is what git made of the three versions of a merge: the root
// tree merge-tree wrote, which wraps the skill in its upstream directory as
// the three versions do, whether the merge conflicts, and for one that does
// the size its conflict markers are to have, see markerSize. The tree of a
// merge that conflicts is never read: the merge is set up again in the
// skill's checkout, see startMerge.
type mergeResult struct {
	tree       string
	conflicted bool
	size       int
}

// The stages git names the versions of a conflicted file by.
const (
	gitStageBase   = "1"
	gitStageMine   = "2"
	gitStageTheirs = "3"
)

// staged is one version of a conflicted file as git's index lists it.
type staged struct {
	mode, oid string
}

// mergeVersions merges the three versions m names, whose trees wrap the
// skill in the upstream directory dir, in the isolated environment: one
// merge-tree with the base given, so that git finds nothing else to merge
// from, and git's rules for files added, deleted or renamed on either side.
// git's detection of a renamed directory is off, on every run, so that a
// file added on one side inside a directory the other side renamed stays
// where it was added rather than being moved, or conflicting, on a guess.
// A merge that conflicts is only decided here: the stages merge-tree lists
// for its conflicted files name the blobs, all of them read in one
// cat-file, that the size of its conflict markers is judged from.
func mergeVersions(ctx context.Context, r *gitx.Runner, gitDir, dir string, m lineage.Merge) (mergeResult, error) {
	out, status, err := r.IsolatedStatus(ctx, gitDir, 1, "-c", "merge.directoryRenames=false",
		"merge-tree", "--write-tree", "-z", "--no-messages", "--merge-base="+m.Base, m.Mine, m.Theirs)
	if err != nil {
		return mergeResult{}, err
	}
	fields := strings.Split(out, "\x00")
	res := mergeResult{tree: fields[0], conflicted: status == 1}
	if !lineage.IsObjectID(res.tree) {
		return mergeResult{}, fmt.Errorf("git merge-tree: cannot read the tree of %q", out)
	}
	var ids []string
	entries := 0
	for _, f := range fields[1:] {
		if f == "" {
			continue
		}
		entries++
		meta, path, ok := strings.Cut(f, "\t")
		parts := strings.Fields(meta)
		if !ok || len(parts) != 3 {
			return mergeResult{}, fmt.Errorf("git merge-tree: cannot read the conflicted file %q", f)
		}
		if !strings.HasPrefix(path, dir+"/") {
			return mergeResult{}, fmt.Errorf("git merge-tree: the conflicted file %q is outside %s", path, dir)
		}
		if source.IsFileMode(parts[0]) {
			ids = append(ids, parts[1])
		}
	}
	switch {
	case !res.conflicted && entries > 0:
		return mergeResult{}, fmt.Errorf("git merge-tree: a clean merge names %d conflicted files", entries)
	case res.conflicted && entries == 0:
		return mergeResult{}, fmt.Errorf("git merge-tree: a merge that conflicts names no conflicted file")
	case !res.conflicted:
		return res, nil
	}
	bodies, err := source.ReadBlobs(ctx, r, gitDir, ids)
	if err != nil {
		return mergeResult{}, err
	}
	texts := make([]string, 0, len(bodies))
	for _, body := range bodies {
		texts = append(texts, body)
	}
	res.size = markerSize(texts...)
	return res, nil
}

// conflictOf is the conflicted file at path as its stages name it, and
// whether its hunks can be read: the file is in both versions merged and
// is a text file in every version that holds it. Anything else conflicts
// whole, and why says how in the words the text output prints. renamed
// says that the merge moved a file to two places.
func conflictOf(path string, versions map[string]staged, bodies map[string]string, renamed bool) (conflictFile, bool) {
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
	mine, theirs := entryKind(versions[gitStageMine].mode), entryKind(versions[gitStageTheirs].mode)
	// A rename that conflicts, a file moved to two places say, lists each
	// path with the stages of the sides that have a file there, and no base
	// where the file was not before, and leaves the path it was moved from
	// with the base alone.
	switch {
	case f.Mine != nil && f.Theirs != nil && mine != theirs:
		f.why = mine + " here, " + theirs + " in the update"
	case f.Mine == nil && f.Theirs != nil && f.Base == nil && renamed:
		f.why = "moved here by the update, and moved or deleted here"
	case f.Mine == nil && f.Theirs != nil && f.Base == nil:
		f.why = "added by the update, and in the way of what is here"
	case f.Mine == nil && f.Theirs != nil:
		f.why = "deleted here, changed by the update"
	case f.Theirs == nil && f.Mine != nil && f.Base == nil && renamed:
		f.why = "moved here, and moved or deleted by the update"
	case f.Theirs == nil && f.Mine != nil && f.Base == nil:
		f.why = "added here, and in the way of the update"
	case f.Theirs == nil && f.Mine != nil:
		f.why = "changed here, deleted by the update"
	case f.Mine == nil || f.Theirs == nil:
		f.why = "moved or deleted on both sides"
	case f.Binary && f.Base == nil:
		f.why = "binary, added here and by the update"
	case f.Binary:
		f.why = "binary, changed here and by the update"
	case !files && f.Base == nil:
		f.why = mine + ", added here and by the update"
	case !files:
		f.why = mine + ", changed here and by the update"
	default:
		return f, true
	}
	return f, false
}

// entryKind names the kind of entry a version of a conflicted file is, by
// its mode, as the text says it. An import leaves submodules out, so a
// version is a file or a symlink.
func entryKind(mode string) string {
	if mode == source.SymlinkMode {
		return "a symlink"
	}
	return "a file"
}

// isBinary is git's own test of whether content is binary, the one a merge
// refuses to merge line by line: a NUL byte among its first 8000.
func isBinary(body string) bool {
	return strings.IndexByte(body[:min(len(body), 8000)], 0) >= 0
}

// markerSize is the size of the conflict markers of a merge of the
// versions texts: git's own seven, or one more than the longest run of one
// of the four marker characters at the start of a line of any of them, so
// that no line of the versions' own is ever taken for a marker.
func markerSize(texts ...string) int {
	return max(7, markerRun(texts...)+1)
}

// markerRun is the longest run of one of the four marker characters, <, |,
// = and >, at the start of a line of any of texts.
func markerRun(texts ...string) int {
	longest := 0
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			if line == "" || !strings.ContainsRune("<=|>", rune(line[0])) {
				continue
			}
			longest = max(longest, len(line)-len(strings.TrimLeft(line, line[:1])))
		}
	}
	return longest
}

// markerSizeIn is the size of the conflict markers the merged file holds,
// whose versions are blobs: the length of the run of < starting the first
// line whose run is longer than any run of a marker character starting a
// line of the blobs, and is followed by a space or the end of the line. No
// line of the versions' own can be taken for one, whatever size the markers
// were written at. It is 0 for a file with no such line.
func markerSizeIn(file string, blobs ...string) int {
	longest := markerRun(blobs...)
	for _, line := range strings.Split(file, "\n") {
		n := len(line) - len(strings.TrimLeft(line, "<"))
		if rest := strings.TrimSuffix(line[n:], "\r"); n > longest && (rest == "" || rest[0] == ' ') {
			return n
		}
	}
	return 0
}

// holdsMarkers reports whether file, a text file of a merge whose versions
// are blobs, still holds a conflict marker: a start marker markerSizeIn
// finds, whatever size it was written at, or a line starting with a run of
// < or > at least as long as the markers of a merge of those versions,
// see markerSize, followed by a space or the end of the line. No line of
// the versions' own is taken for one, and neither is a quote, a line
// starting with "> ", that a resolution added.
func holdsMarkers(file string, blobs ...string) bool {
	if markerSizeIn(file, blobs...) > 0 {
		return true
	}
	size := markerSize(blobs...)
	for _, line := range strings.Split(file, "\n") {
		if line == "" || (line[0] != '<' && line[0] != '>') {
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, line[:1]))
		if rest := strings.TrimSuffix(line[n:], "\r"); n >= size && (rest == "" || rest[0] == ' ') {
			return true
		}
	}
	return false
}

// splitMerged reads the marker blocks of size out of a merged file: every
// region from a line of that many < to one of that many >, with the base
// after the | line and theirs after the = line, as zdiff3 writes them, each
// a hunk, and around them the text between the blocks, one piece more than
// there are hunks, the first before the first block and the last after the
// last. A block with no end is an error.
func splitMerged(file string, size int) (around []string, hunks []conflictHunk, err error) {
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
	var h conflictHunk
	var piece strings.Builder
	at := outside
	for _, line := range strings.SplitAfter(file, "\n") {
		if line == "" {
			continue
		}
		switch {
		case at == outside && isMarker(line, "<"):
			around = append(around, piece.String())
			piece.Reset()
			h, at = conflictHunk{Index: len(hunks) + 1}, inMine
		case at == inMine && isMarker(line, "|"):
			at = inBase
		case (at == inMine || at == inBase) && isMarker(line, "="):
			at = inTheirs
		case at == inTheirs && isMarker(line, ">"):
			hunks, at = append(hunks, h), outside
		case at == inMine:
			h.Mine += line
		case at == inBase:
			h.Base += line
		case at == inTheirs:
			h.Theirs += line
		default:
			piece.WriteString(line)
		}
	}
	if at != outside {
		return nil, nil, fmt.Errorf("a conflict with no end")
	}
	return append(around, piece.String()), hunks, nil
}

// asAtEnd is the text of a hunk that ends the file whose content is file,
// with the line ending git added to the last line, for the marker after it,
// taken off again when the file has none there.
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

// printConflicts reports a merge pending: one conflict event, and in the
// text the headline, which names the skill and says how many files are
// left, then every hunk of every file under its file and number,
// file:index as the hunk is named, between markers git's own size with the
// three versions' names on them, and every file that conflicts whole with
// what conflicts in it. Every line of a hunk holds the file's own bytes,
// so it is printed as a line of a diff is.
func (inv *invocation) printConflicts(ev conflictEvent, headline ...string) {
	out := inv.out
	out.emit(ev)
	out.print(headline...)
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
// no longer there. It is exit code 4, answered once the command knows the
// skill as a managed skill whose branch it can read, before anything it
// would find wrong with what the skill holds or with its update, and the
// hint names the way out that keeps the library directory as it is.
// Everything that replaces a skill's content or takes it away calls it
// under the lock, and before it too wherever it judges the skill first.
// Whether a merge is pending is whether the skill's checkout is there, see
// mergePending.
func pendingMergeRefusal(name, what string) *failure {
	return refuse(exitPendingMerge, name+" has a merge with its update pending, so it cannot be "+what+" until the merge is resolved or given up",
		"run '"+skillCommand("resolve", name, "--abort")+"' to give the merge up; the library directory stays as it is")
}
