package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// conflictEvent is one skill whose edits and whose update conflict: the
// three versions the merge took, each a commit of the account repo and the
// base, HEAD and MERGE_HEAD of the merge in progress in the skill's
// checkout, and every file that conflicts there. It is emitted by an update
// that leaves a merge pending, or finds files of one still unmerged, and
// names the blobs a front end feeds to a diff tool to show a file's
// versions, without reading the account repo.
type conflictEvent struct {
	event
	Name   string         `json:"name"`
	Kind   string         `json:"kind"`   // managed or fork
	Base   string         `json:"base"`   // the merge base: the import commit a managed skill's branch, or a fork's history, names
	Mine   string         `json:"mine"`   // the library directory as git records it, committed on base; a fork's tip
	Theirs string         `json:"theirs"` // the update candidate
	Files  []conflictFile `json:"files"`
}

// conflictFile is one file of a merge that conflicts, an unmerged path at
// its path relative to the skill's directory: the blob each of the three
// versions holds for it, null for a version that has no file there.
type conflictFile struct {
	Path   string  `json:"path"`
	Base   *string `json:"base"`
	Mine   *string `json:"mine"`
	Theirs *string `json:"theirs"`
}

// status is what conflicts in the file, in git status's own words for the
// versions it has: us is the library directory, them the update.
func (f conflictFile) status() string {
	switch {
	case f.Mine != nil && f.Theirs != nil && f.Base != nil:
		return "both modified"
	case f.Mine != nil && f.Theirs != nil:
		return "both added"
	case f.Mine != nil && f.Base != nil:
		return "deleted by them"
	case f.Theirs != nil && f.Base != nil:
		return "deleted by us"
	case f.Mine != nil:
		return "added by us"
	case f.Theirs != nil:
		return "added by them"
	}
	return "both deleted"
}

// mergeResult is what git made of the three versions of a merge: the root
// tree merge-tree wrote, which wraps the skill in its upstream directory as
// the three versions do, conflict markers and all for a merge that
// conflicts, and for one that does the stages of its conflicted files as
// merge-tree lists them, "<mode> <blob> <stage>\t<path>" each. Both set the
// merge up in the skill's checkout, see startMerge.
type mergeResult struct {
	tree       string
	conflicted bool
	stages     []string
}

// mergeVersions merges the three versions m names in the isolated
// environment: one merge-tree with the base given, so that git finds
// nothing else to merge from, or, for a merge of two histories of one fork
// with no base given, with the merge base git finds in them; git's rules
// for files added, deleted or renamed on either side; and conflicts
// written in zdiff3 style, so that each carries the base between mine and
// theirs. git's detection of a renamed directory is off, so that a file
// added on one side inside a directory the other side renamed stays where
// it was added rather than being moved, or conflicting, on a guess.
//
// The conflict markers are sized to the files: a merge that conflicts in a
// file holding a line that starts with a run of seven or more of the
// marker characters, as a Markdown rule or a quoted conflict does, is
// merged once more with markers one longer than the longest such run, the
// conflict-marker-size attribute git documents, so that every marker git
// writes tells itself apart from every line of the skill, which stays as
// it is. A merge with no such line, the common case, costs one merge-tree.
func mergeVersions(ctx context.Context, r *gitx.Runner, gitDir string, m lineage.Merge) (mergeResult, error) {
	res, err := mergeTree(ctx, r, gitDir, m)
	if err != nil || !res.conflicted {
		return res, err
	}
	size, err := conflictMarkerSize(ctx, r, gitDir, res.stages)
	if err != nil || size == defaultMarkerSize {
		return res, err
	}
	attributes, err := os.CreateTemp("", "agentx-attributes-*")
	if err != nil {
		return mergeResult{}, err
	}
	defer func() { _ = os.Remove(attributes.Name()) }()
	_, err = fmt.Fprintf(attributes, "* conflict-marker-size=%d\n", size)
	if err = errors.Join(err, attributes.Close()); err != nil {
		return mergeResult{}, err
	}
	// The attributes file given here comes after the isolated
	// environment's, which reads none, and the last one given is the one
	// git reads.
	return mergeTree(ctx, r, gitDir, m, "-c", "core.attributesFile="+attributes.Name())
}

// mergeTree is the one merge-tree of mergeVersions, with config, more -c
// settings, given after its own.
func mergeTree(ctx context.Context, r *gitx.Runner, gitDir string, m lineage.Merge, config ...string) (mergeResult, error) {
	args := append([]string{"-c", "merge.directoryRenames=false", "-c", "merge.conflictStyle=zdiff3"}, config...)
	args = append(args, "merge-tree", "--write-tree", "-z", "--no-messages")
	if m.Base != "" {
		args = append(args, "--merge-base="+m.Base)
	}
	out, status, err := r.IsolatedStatus(ctx, gitDir, 1, append(args, m.Mine, m.Theirs)...)
	if err != nil {
		return mergeResult{}, err
	}
	fields := strings.Split(out, "\x00")
	res := mergeResult{tree: fields[0], conflicted: status == 1}
	for _, f := range fields[1:] {
		if f != "" {
			res.stages = append(res.stages, f)
		}
	}
	return res, nil
}

// defaultMarkerSize is how long git makes a conflict marker unless told
// otherwise.
const defaultMarkerSize = 7

// conflictMarkerSize is how long the markers of a merge that conflicts in
// the files of stages, as merge-tree lists them, have to be: see
// markerSize, over every version of every regular file that conflicts,
// read in one cat-file.
func conflictMarkerSize(ctx context.Context, r *gitx.Runner, gitDir string, stages []string) (int, error) {
	var ids []string
	seen := map[string]bool{}
	for _, entry := range stages {
		meta, _, _ := strings.Cut(entry, "\t")
		if parts := strings.Fields(meta); len(parts) == 3 && source.IsFileMode(parts[0]) && !seen[parts[1]] {
			seen[parts[1]] = true
			ids = append(ids, parts[1])
		}
	}
	if len(ids) == 0 {
		return defaultMarkerSize, nil
	}
	bodies, err := source.ReadBlobs(ctx, r, gitDir, ids)
	if err != nil {
		return 0, err
	}
	contents := make([]string, 0, len(bodies))
	for _, id := range ids {
		contents = append(contents, bodies[id])
	}
	return markerSize(contents), nil
}

// markerSize is how long conflict markers in files holding contents have
// to be for none of their lines to read as one: one more than the longest
// run of one marker character, <, =, > or |, that starts a line, and never
// shorter than git's own seven. Pure.
func markerSize(contents []string) int {
	size := defaultMarkerSize
	for _, c := range contents {
		for _, line := range strings.Split(c, "\n") {
			if line == "" || !strings.ContainsRune("<=>|", rune(line[0])) {
				continue
			}
			run := len(line) - len(strings.TrimLeft(line, line[:1]))
			size = max(size, run+1)
		}
	}
	return size
}

// conflictFiles are the files that conflict, read from the stages git lists
// for them, "<mode> <blob> <stage>\t<path>" each, as merge-tree and
// ls-files -u list them, a path's stages together and the paths sorted:
// one file per path below the upstream directory dir, relative to the
// skill's directory, stage 1 the base, 2 mine and 3 theirs.
func conflictFiles(stages []string, dir string) ([]conflictFile, error) {
	files := []conflictFile{}
	for _, entry := range stages {
		meta, path, ok := strings.Cut(entry, "\t")
		parts := strings.Fields(meta)
		if !ok || len(parts) != 3 {
			return nil, fmt.Errorf("git: cannot read the conflicted file %q", entry)
		}
		path = strings.TrimPrefix(path, dir+"/")
		if len(files) == 0 || files[len(files)-1].Path != path {
			files = append(files, conflictFile{Path: path})
		}
		f, oid := &files[len(files)-1], parts[1]
		switch parts[2] {
		case "1":
			f.Base = &oid
		case "2":
			f.Mine = &oid
		case "3":
			f.Theirs = &oid
		}
	}
	return files, nil
}

// conflictOfSkill is the conflict event of the skill called name, of kind
// managed or fork, whose merge of m left files conflicting.
func conflictOfSkill(name, kind string, m lineage.Merge, files []conflictFile) conflictEvent {
	return conflictEvent{event: newEvent("conflict"), Name: name, Kind: kind, Base: m.Base, Mine: m.Mine, Theirs: m.Theirs, Files: files}
}

// printConflicts reports a merge left pending: one conflict event, and in
// the text a line naming the skill, what it conflicts with, such as its
// update from one upstream commit to another, and the files that
// conflict, then every file with what conflicts in it.
func (inv *invocation) printConflicts(ev conflictEvent, with string) {
	out := inv.out
	out.emit(ev)
	out.print(out.paint(heading, sanitised(ev.Name)), " conflicts with ", with, " in ",
		out.paint(noteStyle, plural(len(ev.Files), "file")))
	for _, f := range ev.Files {
		out.print(out.paint(label, quotedPath(f.Path)), ": ", f.status())
	}
}

// pendingMergeRefusal refuses to do what to the skill called name while an
// update left a merge pending for it: the merge holds the library
// directory as it was and the update it was merged with, and updating
// or removing the skill would leave it merging versions that are
// no longer there. It is exit code 4, answered under the lock by
// everything that replaces a skill's content or takes it away, and the
// hint names the way out that keeps the library directory as it is.
// Whether a merge is pending is whether the skill's checkout is there, see
// mergePending.
func pendingMergeRefusal(name, what string) *failure {
	return refuse(exitPendingMerge, name+" has a merge with its update pending, so it cannot be "+what+" until the merge is resolved or given up",
		"run '"+skillCommand("update", name, "--abort")+"' to give the merge up; the library directory stays as it is")
}
