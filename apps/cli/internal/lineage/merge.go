package lineage

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
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
// when its message is one agentx writes, the three versions it merges,
// which resolving the merge reads back to merge them again, the files of
// it resolved so far and how many are not resolved yet, which a listing
// shows.
type PendingMerge struct {
	Commit     string
	Merge      Merge
	Readable   bool     // its trailers name the three versions, and its body lists nothing it cannot read
	Unresolved int      // the files not resolved yet, as its subject counts them; -1 when it does not
	Resolved   []string // the files resolved so far, relative to the skill's directory, sorted
}

// unresolvedSubject is the subject of a pending merge commit, which counts
// the files of the merge that are not resolved yet. It is the one thing of
// a pending merge a listing shows that git would otherwise have to merge
// again to tell, so it is written wherever the commit is.
var unresolvedSubject = regexp.MustCompile(`^pending merge of .+: ([0-9]+) files? unresolved$`)

// resolvedLine starts the line of a pending merge commit's body that names
// one file of the merge resolved so far, its path quoted after it.
const resolvedLine = "resolved "

// Message is the message of a pending merge commit of the skill called
// name, with unresolved files left to resolve and the files resolved so
// far: a subject that counts the ones left, a paragraph with one line per
// file resolved, `resolved "<path>"`, the path relative to the skill's
// directory and quoted as git quotes a path, and then the three trailers a
// reader parses, the same whatever was resolved. A merge nothing of which
// is resolved yet has no such paragraph.
//
// Which files are resolved is recorded rather than read off the tree: a
// file that conflicts whole is written into the tree as the version of one
// side, a binary file as mine's and a file one side deleted as the other
// side's, so a file resolved to that side holds exactly what it held
// unresolved, and only the record tells the two apart.
func (m Merge) Message(name string, unresolved int, resolved []string) string {
	files := "files"
	if unresolved == 1 {
		files = "file"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "pending merge of %s: %d %s unresolved\n\n", name, unresolved, files)
	if len(resolved) > 0 {
		for _, p := range sortedPaths(resolved) {
			b.WriteString(resolvedLine + cQuoted(p) + "\n")
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%s: %s\n%s: %s\n%s: %s\n",
		TrailerMergeBase, m.Base,
		TrailerMergeMine, m.Mine,
		TrailerMergeTheirs, m.Theirs)
	return b.String()
}

// sortedPaths is paths sorted bytewise, each once.
func sortedPaths(paths []string) []string {
	sorted := slices.Clone(paths)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

// ParsePending reads the message of the commit a merge ref holds: the three
// trailers, which must all be there, once each and each an object id, the
// count of files its subject says are unresolved, -1 when it says none,
// and the files its body lists as resolved. A message that is not one
// agentx writes is refused, and the merge is then pending all the same,
// since the ref is there: it is only not readable.
func ParsePending(commit, message string) PendingMerge {
	p := PendingMerge{Commit: commit, Unresolved: -1}
	subject, body, _ := strings.Cut(message, "\n")
	if match := unresolvedSubject.FindStringSubmatch(subject); match != nil {
		if n, err := strconv.Atoi(match[1]); err == nil {
			p.Unresolved = n
		}
	}
	resolved, ok := resolvedPaths(body)
	if !ok {
		return p
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
	p.Merge, p.Readable, p.Resolved = m, true, resolved
	return p
}

// resolvedPaths reads the files the body of a pending merge commit's
// message lists as resolved: every line of it before the trailer block
// that starts with the word, each path quoted as Message quotes it. A line
// that starts with the word and holds no path it can read makes the whole
// list unreadable, and ok false: a merge whose resolved files cannot be
// told is not one to resolve further.
func resolvedPaths(body string) (paths []string, ok bool) {
	trailers := trailerBlock(body)
	for _, line := range strings.Split(strings.TrimSuffix(strings.TrimRight(body, "\n"), trailers), "\n") {
		quoted, listed := strings.CutPrefix(line, resolvedLine)
		if !listed {
			continue
		}
		p, err := cUnquoted(quoted)
		if err != nil || !filePath(p) {
			return nil, false
		}
		paths = append(paths, p)
	}
	return sortedPaths(paths), true
}

// cUnquoted reads back a path cQuoted wrote: the bytes between the double
// quotes, a backslash followed by a double quote or a backslash standing
// for that character and one followed by three octal digits for the byte
// they name. Anything else is refused.
func cUnquoted(quoted string) (string, error) {
	inner, ok := strings.CutPrefix(quoted, `"`)
	if ok {
		inner, ok = strings.CutSuffix(inner, `"`)
	}
	if !ok {
		return "", fmt.Errorf("%q is not a quoted path", quoted)
	}
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		switch c := inner[i]; {
		case c == '"':
			return "", fmt.Errorf("%q holds a double quote it does not escape", quoted)
		case c != '\\':
			b.WriteByte(c)
		case i+1 < len(inner) && (inner[i+1] == '"' || inner[i+1] == '\\'):
			b.WriteByte(inner[i+1])
			i++
		case i+3 < len(inner) && isOctal(inner[i+1]) && isOctal(inner[i+2]) && isOctal(inner[i+3]):
			n, err := strconv.ParseUint(inner[i+1:i+4], 8, 8)
			if err != nil {
				return "", fmt.Errorf("%q holds an escape that is no byte", quoted)
			}
			b.WriteByte(byte(n))
			i += 3
		default:
			return "", fmt.Errorf("%q holds an escape git does not write", quoted)
		}
	}
	return b.String(), nil
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// filePath reports whether p can be the path of a file of a skill, relative
// to its directory, as a tree holds it: not empty, no NUL, and no empty,
// "." or ".." component, so none that leaves the directory.
func filePath(p string) bool {
	if p == "" || strings.ContainsRune(p, 0) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
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
// tree, what merge-tree made of the three versions of m with the files
// resolved so far written into it, with Mine and Theirs as its parents and
// the message Message writes. Its identity and date are the isolated
// environment's, and it names nothing about the machine, so the same
// merge, resolved alike, is the same commit wherever it is written.
func CommitPending(ctx context.Context, r *gitx.Runner, gitDir, tree, name string, m Merge, unresolved int, resolved []string) (string, error) {
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(m.Message(name, unresolved, resolved)),
		"commit-tree", tree, "-p", m.Mine, "-p", m.Theirs)
	return strings.TrimSpace(out), err
}

// WriteResolved writes v, the version a pending merge holds once files of
// it were resolved, into the object store of gitDir as the tree it is,
// wrapped in the upstream directory dir as every tree of a merge is, and
// returns the id of that root tree. Its entries are the ones ReadMerged
// read, directories with the ids git stored them under, with what the
// resolving changed: a directory whose entries changed is written again,
// every other one is kept, and a directory left with nothing goes with
// its entries. Every id is computed in process first, symlinks included,
// and every one git answers with is held to it; the trees are written in
// one mktree, each after the ones below it. Its blobs must be written
// already.
func WriteResolved(ctx context.Context, r *gitx.Runner, gitDir, dir string, v Base) (string, error) {
	p := planTrees(Version{Dir: dir, Tree: v.Tree, Entries: v.Entries}, true)
	root := p.ids[""]
	if root == "" {
		return "", fmt.Errorf("the merge of %s resolves to no file at all", dir)
	}
	var defs, want []string
	for depth := p.maxDepth; depth >= 0; depth-- {
		for _, d := range p.level(depth) {
			defs = append(defs, treeInput(entryLines(p.entries[d])))
			want = append(want, p.ids[d])
		}
	}
	defs = append(defs, treeInput([]string{entryLine(source.DirMode, root, dir)}))
	want = append(want, treeid.Wrap(dir, root))
	written, err := mktree(ctx, r, gitDir, defs)
	if err != nil {
		return "", err
	}
	for i := range want {
		if written[i] != want[i] {
			return "", fmt.Errorf("git wrote a tree of the merge of %s as %s, not %s", dir, written[i], want[i])
		}
	}
	return written[len(written)-1], nil
}

// ReadImport reads the lineage the import commit commit carries, for a
// version no ref of the skill's own names any more, as the update a merge
// merged once the check moved the candidate on.
func ReadImport(ctx context.Context, r *gitx.Runner, gitDir, commit string) (Import, error) {
	out, err := r.Isolated(ctx, gitDir, "log", "-1", "--format=%B", commit)
	if err != nil {
		return Import{}, err
	}
	return Parse(out)
}

// ReadMerged reads the version a merge wrote out of tree, the root tree
// merge-tree answered with, as ReadBase reads the one an import commit
// holds: everything below the upstream directory dir, with that name taken
// off every path.
func ReadMerged(ctx context.Context, r *gitx.Runner, gitDir, tree, dir string) (Base, error) {
	return readVersion(ctx, r, gitDir, tree, dir, "the merged tree "+tree)
}
