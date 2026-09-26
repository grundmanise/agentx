package lineage

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// MergePrefix is where a pending merge of a managed skill waits: the merge
// commit an update leaves when the edits of the library directory and the
// update candidate conflict, one ref per skill. Its tree is what git made of
// the three versions, conflict markers and all, and it is never laid out in
// the library: agents read the library, and a half-merged file is never
// what they read. It is read with the lineage, in the same for-each-ref, so
// a listing shows it without a git process of its own, and it stays until
// the merge is resolved or given up. The import branch, the candidate and
// the library directory are left as they were while it waits.
const MergePrefix = "refs/agentx/merge/"

// MergeRef is the pending merge of the skill called name.
func MergeRef(name string) string { return MergePrefix + name }

// The trailers of a pending merge commit, which name the three versions it
// merges, and nothing else: nothing about the machine that wrote it enters
// the commit, so the same three versions merge to the same commit anywhere.
const (
	TrailerMergeBase   = "Agentx-Merge-Base"
	TrailerMergeMine   = "Agentx-Merge-Mine"
	TrailerMergeTheirs = "Agentx-Merge-Theirs"
)

// Merge is the three versions of a skill a merge takes, each a commit of
// the account repo whose tree wraps the skill in its upstream directory, as
// an import tree does: the base version the import branch pointed at, the
// library directory as it was, committed on top of it, and the update
// candidate. Running merge-tree on the three again gives the same result,
// which is how the conflicts of a pending merge are read back.
type Merge struct {
	Base   string // the import commit the skill's branch pointed at
	Mine   string // the library directory, committed with Base as its parent
	Theirs string // the update candidate
}

// PendingMerge is what a merge ref holds: the pending merge commit and,
// when its message is one agentx writes, the three versions it merges and
// how many of its files are not resolved yet.
type PendingMerge struct {
	Commit     string
	Merge      Merge
	Readable   bool // its trailers name the three versions
	Unresolved int  // the files not resolved yet, as its subject counts them; -1 when it does not
}

// unresolvedSubject is the subject of a pending merge commit, which counts
// the files of the merge that are not resolved yet. It is the one thing of
// a pending merge a listing shows that git would otherwise have to merge
// again to tell, so it is written wherever the commit is.
var unresolvedSubject = regexp.MustCompile(`^pending merge of .+: ([0-9]+) files? unresolved$`)

// Message is the message of a pending merge commit of the skill called
// name, with unresolved files left to resolve: a subject that counts them,
// then the three trailers a reader parses.
func (m Merge) Message(name string, unresolved int) string {
	files := "files"
	if unresolved == 1 {
		files = "file"
	}
	return fmt.Sprintf("pending merge of %s: %d %s unresolved\n\n%s: %s\n%s: %s\n%s: %s\n",
		name, unresolved, files,
		TrailerMergeBase, m.Base,
		TrailerMergeMine, m.Mine,
		TrailerMergeTheirs, m.Theirs)
}

// ParsePending reads the message of the commit a merge ref holds: the three
// trailers, which must all be there, once each and each an object id, and
// the count of files its subject says are unresolved, -1 when it says none.
// A message that is not one agentx writes is refused, and the merge is then
// pending all the same, since the ref is there: it is only not readable.
func ParsePending(commit, message string) PendingMerge {
	p := PendingMerge{Commit: commit, Unresolved: -1}
	subject, _, _ := strings.Cut(message, "\n")
	if match := unresolvedSubject.FindStringSubmatch(subject); match != nil {
		if n, err := strconv.Atoi(match[1]); err == nil {
			p.Unresolved = n
		}
	}
	values := map[string]string{}
	for _, line := range strings.Split(trailerBlock(message), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !strings.HasPrefix(key, "Agentx-") {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if _, seen := values[key]; seen {
			return p
		}
		values[key] = value
	}
	m := Merge{Base: values[TrailerMergeBase], Mine: values[TrailerMergeMine], Theirs: values[TrailerMergeTheirs]}
	if len(values) != 3 || !objectID.MatchString(m.Base) || !objectID.MatchString(m.Mine) || !objectID.MatchString(m.Theirs) {
		return p
	}
	p.Merge, p.Readable = m, true
	return p
}

// WriteMine writes what the library directory at root holds, as tree read
// it, into the object store of gitDir as the "mine" of a merge: a commit
// whose tree wraps the directory in the upstream directory of the import
// commit rec names, as an import tree wraps a version, and whose parent is
// that commit, so that merge-tree compares the directory with its base
// version path for path. The directory goes in as WriteDir writes one,
// never through an index, and the one-entry tree around it in the same
// mktree; the commit has the fixed identity and date of every commit the
// isolated environment writes, so the same directory on the same base is
// the same commit. Three git processes, whatever the directory holds.
func WriteMine(ctx context.Context, r *gitx.Runner, gitDir, root string, tree treeid.Tree, rec Record) (string, error) {
	if !rec.HasImport {
		return "", fmt.Errorf("%w: %s carries no lineage", ErrTrailer, rec.Ref)
	}
	if len(tree.Dirs) == 0 {
		return "", fmt.Errorf("%w: the library directory of %s holds nothing git records", ErrChanged, rec.Name)
	}
	dir := rec.Import.Dir()
	wrapped, err := writeDir(ctx, r, gitDir, root, tree, dir)
	if err != nil {
		return "", err
	}
	message := "library directory of " + rec.Name + "\n"
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(message), "commit-tree", wrapped, "-p", rec.Commit)
	return strings.TrimSpace(out), err
}

// CommitPending writes the pending merge commit of the skill called name:
// tree, what merge-tree made of the three versions of m, with Mine and
// Theirs as its parents and the message Message writes. Its identity and
// date are the isolated environment's, and it names nothing about the
// machine, so the same merge is the same commit wherever it is written.
func CommitPending(ctx context.Context, r *gitx.Runner, gitDir, tree, name string, m Merge, unresolved int) (string, error) {
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(m.Message(name, unresolved)),
		"commit-tree", tree, "-p", m.Mine, "-p", m.Theirs)
	return strings.TrimSpace(out), err
}

// ReadMerged reads the version a merge wrote out of tree, the root tree
// merge-tree answered with, as ReadBase reads the one an import commit
// holds: everything below the upstream directory dir, with that name taken
// off every path.
func ReadMerged(ctx context.Context, r *gitx.Runner, gitDir, tree, dir string) (Base, error) {
	return readVersion(ctx, r, gitDir, tree, dir, "the merged tree "+tree)
}
