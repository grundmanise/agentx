// Package lineage writes and reads what the account repo knows about where
// a skill came from: the parentless import commit of one upstream version,
// its trailers, and the branches that point at them.
package lineage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The trailers of an import commit. There are four and no more: an import
// commit is a pure function of the version it holds and the coordinates it
// came from, so that two machines that accept the same upstream version
// write the same commit. Nothing about a machine, a fork or an account
// enters it.
const (
	TrailerSource   = "Agentx-Source"
	TrailerPath     = "Agentx-Path"
	TrailerCommit   = "Agentx-Upstream-Commit"
	TrailerHash     = "Agentx-Content-Hash"
	trailerRootPath = "." // the subpath of a skill at the repository root
)

// Import is the lineage of one upstream version: the source it came from,
// the directory inside it, the commit it was taken at and the content hash
// of that version.
type Import struct {
	Source string // the canonical URL of the source
	Path   string // the skill directory from the repository root, "" for the root
	Commit string // the upstream commit the version was read from
	Hash   string // the content hash of the version
}

// ErrTrailer is the error of a message that is not an import commit's.
var ErrTrailer = errors.New("not an import commit")

var (
	objectID    = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`) // sha1, or sha256 for a repository written that way
	contentHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Message is the commit message of the import commit: one subject naming
// the four things that decide the commit, then the trailers a reader parses.
// The subject never carries the frontmatter name the library directory
// takes, which would make the commit id depend on something the content
// hash already covers.
func (i Import) Message() string {
	where := i.Source
	if i.Path != "" {
		where += "/" + i.Path
	}
	return fmt.Sprintf("import %s at %s content %s\n\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n",
		where, abbrev(i.Commit), abbrev(i.Hash),
		TrailerSource, i.Source,
		TrailerPath, i.pathTrailer(),
		TrailerCommit, i.Commit,
		TrailerHash, i.Hash)
}

// pathTrailer writes the subpath, a skill at the repository root as ".": a
// trailer with an empty value would be a trailer a parser cannot tell from
// a missing one.
func (i Import) pathTrailer() string {
	if i.Path == "" {
		return trailerRootPath
	}
	return i.Path
}

func abbrev(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// Parse reads the four trailers of an import commit message and refuses
// anything else: a missing trailer, one given twice, an Agentx- trailer
// that is not one of the four, a source that is not a canonical source URL,
// which is what tells a source from a plugin coordinate such as
// name@marketplace, a path that leaves the repository, and an upstream
// commit or content hash that is not an object id.
//
// A fifth Agentx- trailer is refused rather than ignored. The contract says
// there are four and no more, and it means it: an import commit is a pure
// function of the version it holds and the coordinates it came from, so a
// message carrying anything else about a machine, a fork or an account was
// not written as an import commit, and reading it as one would credit a
// version to a commit that does not hold it.
func Parse(message string) (Import, error) {
	var i Import
	seen := map[string]bool{}
	for _, line := range strings.Split(trailerBlock(message), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !strings.HasPrefix(key, "Agentx-") {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if seen[key] {
			return Import{}, fmt.Errorf("%w: the trailer %s is given twice", ErrTrailer, key)
		}
		seen[key] = true
		switch key {
		case TrailerSource:
			i.Source = value
		case TrailerPath:
			i.Path = value
		case TrailerCommit:
			i.Commit = value
		case TrailerHash:
			i.Hash = value
		default:
			return Import{}, fmt.Errorf("%w: %s is not one of the four trailers", ErrTrailer, key)
		}
	}
	for _, t := range []struct {
		name, value string
	}{{TrailerSource, i.Source}, {TrailerPath, i.Path}, {TrailerCommit, i.Commit}, {TrailerHash, i.Hash}} {
		if t.value == "" {
			return Import{}, fmt.Errorf("%w: no %s trailer", ErrTrailer, t.name)
		}
	}
	src, err := source.Parse(i.Source)
	if err != nil || src.URL != i.Source || src.Subpath != "" || src.Ref != "" {
		return Import{}, fmt.Errorf("%w: %s %q is not the canonical URL of a source", ErrTrailer, TrailerSource, i.Source)
	}
	if i.Path == trailerRootPath {
		i.Path = ""
	} else if clean := path.Clean(i.Path); clean != i.Path || strings.HasPrefix(i.Path, "/") || strings.HasPrefix(i.Path, "../") || i.Path == ".." {
		return Import{}, fmt.Errorf("%w: %s %q is not a directory of the source", ErrTrailer, TrailerPath, i.Path)
	}
	if !objectID.MatchString(i.Commit) {
		return Import{}, fmt.Errorf("%w: %s %q is not a commit id", ErrTrailer, TrailerCommit, i.Commit)
	}
	if !contentHash.MatchString(i.Hash) {
		return Import{}, fmt.Errorf("%w: %s %q is not a content hash", ErrTrailer, TrailerHash, i.Hash)
	}
	return i, nil
}

// trailerBlock is the last paragraph of a commit message, which is where
// git keeps trailers and the only place an import commit carries them. A
// message of one paragraph has no trailer block at all: its trailers would
// be its subject.
//
// Reading the whole message instead would let any commit whose body happens
// to quote an Agentx-Source line be read as lineage, and a fork's tip is
// read for lineage on every listing while its message is the user's own.
func trailerBlock(message string) string {
	lines := strings.Split(strings.TrimRight(message, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return ""
}

// Version is one upstream version about to be imported: the entries of the
// skill's tree as the source stores them, the directory name the import
// tree holds them under, which is the upstream's own name and not the
// frontmatter's, and the upstream committer time the commit is dated with.
type Version struct {
	Import  Import
	Dir     string // the single entry of the import tree
	Tree    string // the tree id of the skill directory in the source
	Entries []source.TreeEntry
	When    string // the upstream committer time as "<epoch> +0000"
}

// Write writes the import commit of v into gitDir and returns its id. The
// tree is written over the objects the source already put in the repository:
// the skill's own tree is reused whole unless an entry that is neither a
// regular file nor a directory has to be left out, and only the directories
// above such an entry are written anew. The commit is parentless, carries
// the fixed agentx identity and the upstream committer time as epoch
// seconds with +0000, so its id depends on nothing but the version and its
// coordinates.
func Write(ctx context.Context, r *gitx.Runner, gitDir string, v Version) (string, error) {
	tree, err := writeTree(ctx, r, gitDir, v)
	if err != nil {
		return "", err
	}
	return r.IsolatedAt(ctx, gitDir, v.When, "commit-tree", tree, "-m", v.Import.Message())
}

// writeTree builds the import tree: one entry, the upstream directory, and
// under it the skill's regular files. A skill whose tree holds only regular
// files and directories, which is nearly every skill, reuses that tree whole
// and costs one mktree; only the directories above an entry that has to be
// left out are written anew, deepest first, one mktree per level.
func writeTree(ctx context.Context, r *gitx.Runner, gitDir string, v Version) (string, error) {
	root, err := skillTree(ctx, r, gitDir, v)
	if err != nil {
		return "", err
	}
	if root == "" {
		return "", fmt.Errorf("%w: the skill has no regular file to import", ErrTrailer)
	}
	return mktree(ctx, r, gitDir, []string{entryLine(source.DirMode, root, v.Dir)})
}

// skillTree is the tree of the skill directory as the import holds it: the
// tree the source stores when every entry below it is a regular file or a
// directory, and otherwise a tree written without the entries an import
// leaves out. An empty result means every entry was left out.
func skillTree(ctx context.Context, r *gitx.Runner, gitDir string, v Version) (string, error) {
	children := map[string][]source.TreeEntry{}
	byDepth := map[int][]string{}
	maxDepth := 0
	ids := map[string]string{"": v.Tree}
	for _, e := range v.Entries {
		parent := path.Dir(e.Path)
		if parent == "." {
			parent = ""
		}
		children[parent] = append(children[parent], e)
		if e.Mode == source.DirMode {
			ids[e.Path] = e.OID
			depth := strings.Count(e.Path, "/") + 1
			byDepth[depth] = append(byDepth[depth], e.Path)
			maxDepth = max(maxDepth, depth)
		}
	}
	rewritten := map[string]bool{}
	for depth := maxDepth; depth >= 0; depth-- {
		dirs := byDepth[depth]
		if depth == 0 {
			dirs = []string{""}
		}
		sort.Strings(dirs)
		var level []string // the directories written at this level, in order
		var defs []string  // one mktree definition each
		for _, dir := range dirs {
			if !needsWriting(dir, children, rewritten) {
				continue
			}
			lines := entryLines(dir, children, ids)
			rewritten[dir] = true
			if len(lines) == 0 {
				ids[dir] = "" // every entry left out: the directory goes with them
				continue
			}
			level = append(level, dir)
			defs = append(defs, treeInput(lines))
		}
		if len(defs) == 0 {
			continue
		}
		// The trees of one level are written in one batch, each definition
		// a run of NUL-terminated records and the boundary between two of
		// them an empty record.
		out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(strings.Join(defs, "\x00")), "mktree", "-z", "--batch")
		if err != nil {
			return "", err
		}
		written := strings.Fields(out)
		if len(written) != len(defs) {
			return "", fmt.Errorf("git mktree wrote %d trees for %d directories", len(written), len(defs))
		}
		for i, dir := range level {
			ids[dir] = written[i]
		}
	}
	return ids[""], nil
}

// entryLines are the mktree lines of one directory: its regular files as
// they are, and the directories below it at the id they ended up with, an
// emptied one left out.
func entryLines(dir string, children map[string][]source.TreeEntry, ids map[string]string) []string {
	var lines []string
	for _, e := range children[dir] {
		switch {
		case e.Mode == source.DirMode:
			if id := ids[e.Path]; id != "" {
				lines = append(lines, entryLine(source.DirMode, id, path.Base(e.Path)))
			}
		case source.IsFileMode(e.Mode):
			lines = append(lines, entryLine(e.Mode, e.OID, path.Base(e.Path)))
		}
	}
	return lines
}

// needsWriting reports whether dir has to be written anew: it does when one
// of its entries is neither a regular file nor a directory, which an import
// leaves out, or when a directory below it was written anew.
func needsWriting(dir string, children map[string][]source.TreeEntry, rewritten map[string]bool) bool {
	for _, e := range children[dir] {
		switch {
		case e.Mode == source.DirMode:
			if rewritten[e.Path] {
				return true
			}
		case !source.IsFileMode(e.Mode):
			return true
		}
	}
	return false
}

// mktree writes one tree from its entries. Its answer comes back as git
// wrote it, one id and a newline whether or not the input was terminated
// with NUL, so it is trimmed before it names an object.
func mktree(ctx context.Context, r *gitx.Runner, gitDir string, lines []string) (string, error) {
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(treeInput(lines)), "mktree", "-z")
	return strings.TrimSpace(out), err
}

// treeInput is the mktree input of one tree: every record terminated with
// NUL rather than with a newline. mktree is given -z for this, and must be:
// its line-based reader ends a record at the first newline and C-unquotes
// any name that starts with a double quote, so a source holding a file
// called `"weird".md` would import as `weird` and one holding a newline in
// a name would not import at all. The names come from ls-tree -r -t -z as
// the source's own bytes, and nothing quotes them on the way back.
func treeInput(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte(0)
	}
	return b.String()
}

// entryLine is one record of mktree input: the ls-tree format it reads.
func entryLine(mode, oid, name string) string {
	typ := "blob"
	if mode == source.DirMode {
		typ = "tree"
	}
	return mode + " " + typ + " " + oid + "\t" + name
}

// Dropped names the entries of a skill's tree an import leaves out: a
// symlink or a submodule, which the import tree, holding regular files
// alone, has no room for. The caller warns about each one, since the
// library then differs from the upstream directory.
func Dropped(entries []source.TreeEntry) []string {
	var dropped []string
	for _, e := range entries {
		if e.Mode != source.DirMode && !source.IsFileMode(e.Mode) {
			dropped = append(dropped, e.Path)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// UpstreamDate turns the upstream commit's committer time, epoch seconds as
// git prints them, into the date an import commit carries: the same seconds
// with a zero offset, so that the commit id does not depend on the time zone
// of the machine that wrote it or of the one that made the upstream commit.
func UpstreamDate(epoch string) (string, error) {
	if epoch == "" || strings.TrimLeft(epoch, "0123456789") != "" {
		return "", fmt.Errorf("%w: %q is not a committer time", ErrTrailer, epoch)
	}
	return epoch + " +0000", nil
}
