package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// conflictEvent is one skill whose edits and whose update conflict: the
// three versions the merge took, each a commit of the account repo and the
// base, HEAD and MERGE_HEAD of the merge in progress in the skill's
// checkout, and every file that conflicts there. It is emitted by an update
// that leaves a merge pending, and names the blobs a front end feeds to a
// diff tool to show a file's versions, without reading the account repo.
type conflictEvent struct {
	event
	Name   string         `json:"name"`
	Kind   string         `json:"kind"`   // managed, the one kind that merges its update today
	Base   string         `json:"base"`   // the import commit the skill's branch points at
	Mine   string         `json:"mine"`   // the library directory as git records it, files it ignores aside, committed on base
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
// nothing else to merge from, git's rules for files added, deleted or
// renamed on either side, and conflicts written in zdiff3 style, so that
// each carries the base between mine and theirs. git's detection of a
// renamed directory is off, so that a file added on one side inside a
// directory the other side renamed stays where it was added rather than
// being moved, or conflicting, on a guess.
func mergeVersions(ctx context.Context, r *gitx.Runner, gitDir string, m lineage.Merge) (mergeResult, error) {
	out, status, err := r.IsolatedStatus(ctx, gitDir, 1, "-c", "merge.directoryRenames=false", "-c", "merge.conflictStyle=zdiff3",
		"merge-tree", "--write-tree", "-z", "--no-messages", "--merge-base="+m.Base, m.Mine, m.Theirs)
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

// conflictOfSkill is the conflict event of the skill called name, whose
// merge of m left files conflicting.
func conflictOfSkill(name string, m lineage.Merge, files []conflictFile) conflictEvent {
	return conflictEvent{event: newEvent("conflict"), Name: name, Kind: lineage.KindManaged, Base: m.Base, Mine: m.Mine, Theirs: m.Theirs, Files: files}
}

// printConflicts reports a merge left pending: one conflict event, and in
// the text a line naming the skill, the two upstream commits and the files
// that conflict, then every file with what conflicts in it.
func (inv *invocation) printConflicts(ev conflictEvent, from, to string) {
	out := inv.out
	out.emit(ev)
	out.print(out.paint(heading, sanitised(ev.Name)), " conflicts with its update from ", from, " to ", to, " in ",
		out.paint(noteStyle, plural(len(ev.Files), "file")))
	for _, f := range ev.Files {
		out.print(out.paint(label, quotedPath(f.Path)), ": ", f.status())
	}
}

// pendingMergeRefusal refuses to do what to the skill called name while an
// update left a merge pending for it: the merge holds the library
// directory as it was and the update it was merged with, and updating,
// reverting or removing the skill would leave it merging versions that are
// no longer there. It is exit code 4, answered under the lock by
// everything that replaces a skill's content or takes it away, and the
// hint names the way out that keeps the library directory as it is.
// Whether a merge is pending is whether the skill's checkout is there, see
// mergePending.
func pendingMergeRefusal(name, what string) *failure {
	return refuse(exitPendingMerge, name+" has a merge with its update pending, so it cannot be "+what+" until the merge is resolved or given up",
		"run '"+skillCommand("update", name, "--abort")+"' to give the merge up; the library directory stays as it is")
}
