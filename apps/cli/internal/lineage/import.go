// Package lineage writes and reads what the account repo knows about where
// a skill came from: the parentless import commit of one upstream version,
// its trailers, and the branches that point at them.
package lineage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
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
	} else if !ValidPath(i.Path) {
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

// IsObjectID reports whether s has the shape of a git object id, sha1 or
// sha256, which is what a reader outside this package checks a commit it
// was handed with.
func IsObjectID(s string) bool { return objectID.MatchString(s) }

// Unrecordable reports why an import commit carrying i would not be read
// back as the lineage i is, and "" when it would be. It is the write side
// of Parse, and it is Parse itself that answers, on the message the commit
// would carry, so that the two sides of an import commit cannot drift
// apart.
//
// They have to agree, because lineage a reader refuses is lineage the
// import commit no longer provides: the skill installs, then lists as
// managed with no source, no subpath, no upstream commit and no state, and
// nothing can update or revert it again. Lineage a reader reads back as
// something else is worse still, the skill being credited to a directory
// it did not come from. Neither is reported to anyone, which is why the
// answer is to refuse the version rather than to record it.
//
// A predicate per coordinate is not enough on its own, and this is why a
// whole message is written and read instead: a trailer is one line and its
// value is that line with the surrounding whitespace trimmed off, so what
// makes a coordinate unrecordable is not a property of the coordinate
// alone but of the message it would sit in.
func Unrecordable(i Import) string {
	got, err := Parse(i.Message())
	if err != nil {
		return strings.TrimPrefix(err.Error(), ErrTrailer.Error()+": ")
	}
	for _, t := range []struct{ name, wrote, read string }{
		{TrailerSource, i.Source, got.Source},
		{TrailerPath, i.Path, got.Path},
		{TrailerCommit, i.Commit, got.Commit},
		{TrailerHash, i.Hash, got.Hash},
	} {
		if t.wrote != t.read {
			return fmt.Sprintf("%s %q would be read back as %q", t.name, t.wrote, t.read)
		}
	}
	return ""
}

// ValidPath reports whether p is a directory of a repository, "" being its
// root: the subpath an import commit records, and the subpath another
// tool's lock file names. A path that is not already clean, that is
// absolute, that walks out of the repository, that carries a control
// character or that a trailer would not carry back unchanged is none. The
// last two are what keep a subpath one line of a commit message and one
// line of git's batch input, neither of which a reader could split back,
// and what keep the line it is read back from the directory it named: a
// trailer's value is its line with the surrounding whitespace trimmed off,
// so a directory whose name begins or ends in a space, which a source may
// perfectly well hold, would be read back as a different directory of that
// same source.
func ValidPath(p string) bool {
	switch {
	case p == "":
		return true
	case p == "." || p == "..", strings.HasPrefix(p, "/"), strings.HasPrefix(p, "../"):
		return false
	case path.Clean(p) != p, strings.TrimSpace(p) != p:
		return false
	}
	return strings.IndexFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
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

// ImportingPrefix is where the commits of a run wait between the
// fast-import that writes them and the mutation journal that points the
// import branches at them: one staging ref per skill, under a namespace of
// this run's own, so that the objects stay reachable while the journal is
// written and two runs never share a ref. The refs are dropped when the run
// ends, on a context a stop signal does not reach, so only a run that was
// killed leaves them. Those are what `agentx doctor` reports as
// `staged_imports`: an import commit no branch holds is reachable from
// nothing else, and once the source it came from is removed its trees and
// blobs are pinned by that ref alone. Nothing sweeps them, because the
// staging refs of a run that is still fetching look exactly the same and no
// lock is held while either exists.
const ImportingPrefix = "refs/agentx/importing/"

// ImportingRef is the staging ref of the nth version of run.
func ImportingRef(run string, n int) string {
	return fmt.Sprintf("%s%s/%d", ImportingPrefix, run, n)
}

// NewRun names one run's staging namespace.
func NewRun() string {
	var id [8]byte
	_, _ = rand.Read(id[:])
	return hex.EncodeToString(id[:])
}

// WriteAll writes the import commit of every version into gitDir, through
// one fast-import whatever the count, and returns their ids, and the ids of
// the trees they hold, in the order the versions were given. Each commit
// points at a tree this call has already written or the source already
// held, is parentless, carries the fixed agentx identity and the upstream
// committer time as epoch seconds with +0000, and so depends on nothing but
// the version and its coordinates: a skill installed alone and the same
// skill installed in a batch of thirty end at the same commit.
//
// fast-import is fed the tree rather than the files: the commit's tree is
// exactly the one writeTrees built, set with a filemodify of the tree root,
// so that nothing about how a batch is streamed can reach the commit id.
// The ids come back through get-mark rather than a marks file, which costs
// no temporary file and keeps the whole import to one git process.
func WriteAll(ctx context.Context, r *gitx.Runner, gitDir, run string, versions []Version) (commits, trees []string, err error) {
	if len(versions) == 0 {
		return nil, nil, nil
	}
	trees, err = writeTrees(ctx, r, gitDir, versions)
	if err != nil {
		return nil, nil, err
	}
	var b strings.Builder
	for i, v := range versions {
		msg := v.Import.Message()
		fmt.Fprintf(&b, "commit %s\nmark :%d\n", ImportingRef(run, i), i+1)
		fmt.Fprintf(&b, "author %s %s\ncommitter %s %s\n", gitx.Identity, v.When, gitx.Identity, v.When)
		// The count is bytes and not characters, and the message is written
		// as it stands: a commit-tree of the same message ends in the same
		// newline, and one byte either way is another commit id.
		fmt.Fprintf(&b, "data %d\n%s", len(msg), msg)
		// An empty path is fast-import's way of replacing the tree root, so
		// the commit takes the tree whole instead of having it assembled.
		fmt.Fprintf(&b, "M %s %s \n\n", source.DirMode, trees[i])
	}
	for i := range versions {
		fmt.Fprintf(&b, "get-mark :%d\n", i+1)
	}
	b.WriteString("done\n")
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "fast-import", "--quiet", "--done")
	if err != nil {
		return nil, nil, err
	}
	ids := strings.Fields(out)
	if len(ids) != len(versions) {
		return nil, nil, fmt.Errorf("git fast-import wrote %d commits for %d versions", len(ids), len(versions))
	}
	return ids, trees, nil
}

// Rewrite writes the import commit of the version rec records once more,
// as an import writes one today, onto the first staging ref of run: the
// same trailers and message, the same dates, and the tree base gets from
// a git of today. It is for a branch whose commit fails Canonical, which
// an earlier agentx wrote over a source's own tree, legacy modes and all:
// no directory on disk is ever current against that commit, while the one
// this returns is the commit an install of the same version writes now,
// on this machine or any other.
//
// The date is read off the commit rec names, which carries the upstream
// commit's committer time as every import commit does, and is held to the
// rule an install holds that time to.
func Rewrite(ctx context.Context, r *gitx.Runner, gitDir, run string, rec Record, base Base) (string, error) {
	if !rec.HasImport {
		return "", fmt.Errorf("%w: %s carries no lineage", ErrTrailer, rec.Ref)
	}
	out, err := r.Isolated(ctx, gitDir, "log", "-1", "--format=%ct", rec.Commit)
	if err != nil {
		return "", err
	}
	when, err := UpstreamDate(strings.TrimSpace(out))
	if err != nil {
		return "", err
	}
	dir := rec.Import.Dir()
	commits, trees, err := WriteAll(ctx, r, gitDir, run, []Version{{
		Import: rec.Import, Dir: dir, Tree: base.Tree, Entries: base.Entries, When: when,
	}})
	if err != nil {
		return "", err
	}
	// An import holds regular files alone, so a base holding anything
	// else was not written by one, and the commit written now would hold
	// another version than the one a revert lays out.
	if want := treeid.Wrap(dir, base.ID()); trees[0] != want {
		return "", fmt.Errorf("%w: %s holds entries no import writes", ErrTrailer, rec.Ref)
	}
	return commits[0], nil
}

// DropImporting removes the staging refs of run, in one transaction. It is
// cleanup: the refs hold commits the import branches now hold too, and a
// deletion of a ref that is not there succeeds.
func DropImporting(ctx context.Context, r *gitx.Runner, gitDir, run string, n int) error {
	if n == 0 {
		return nil
	}
	var b strings.Builder
	for i := range n {
		b.WriteString("delete " + ImportingRef(run, i) + "\n")
	}
	_, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "update-ref", "--stdin")
	return err
}

// writeTrees builds the import tree of every version: one entry, the
// upstream directory, and under it the skill's regular files, every tree
// exactly as git writes one today. A directory the source already stores
// that way, which is nearly every directory of nearly every skill, is
// reused whole. The rest are written anew, deepest first: a directory that
// holds an entry an import leaves out, one above a directory written anew,
// and one the source stores in a form git reads but no longer writes, such
// as a file of mode 100664 or a directory mode padded with a zero. git
// reads such a mode back as the canonical one, so a listing of the tree
// shows nothing amiss, but the tree keeps an id of its own, and no
// directory on disk, whose id is computed with canonical modes, could ever
// be compared equal to it.
//
// So the id every directory ends at is known before anything is written,
// computed in process the way treeid computes a directory's, and every id
// git answers with is held to it: the tree an import commit holds is the
// tree Holds compares a library directory with. Every version is written
// together, one mktree per level of the deepest of them and one for their
// roots, so that a batch of thirty skills costs the tree writes of one.
func writeTrees(ctx context.Context, r *gitx.Runner, gitDir string, versions []Version) ([]string, error) {
	plans := make([]*treePlan, len(versions))
	maxDepth := 0
	for i, v := range versions {
		plans[i] = planTrees(v, false)
		maxDepth = max(maxDepth, plans[i].maxDepth)
	}
	// A directory is written once every directory below it is, so the levels
	// go deepest first and every version's level is written in the same pass.
	for depth := maxDepth; depth >= 0; depth-- {
		var defs []string
		var at []plannedDir // what each definition writes, in the same order
		for _, p := range plans {
			for _, dir := range p.level(depth) {
				at = append(at, plannedDir{plan: p, dir: dir})
				defs = append(defs, treeInput(entryLines(p.entries[dir])))
			}
		}
		written, err := mktree(ctx, r, gitDir, defs)
		if err != nil {
			return nil, err
		}
		for i, d := range at {
			if want := d.plan.ids[d.dir]; written[i] != want {
				return nil, fmt.Errorf("git wrote the directory %q of %s as %s, not %s", d.dir, d.plan.dir, written[i], want)
			}
		}
	}
	defs := make([]string, 0, len(versions))
	for i, v := range versions {
		root := plans[i].ids[""]
		if root == "" {
			// Unreachable through an install: HasFileToImport answers the
			// same question per skill, before the run stages anything.
			return nil, fmt.Errorf("%w: %s has no regular file to import", ErrTrailer, v.Dir)
		}
		defs = append(defs, treeInput([]string{entryLine(source.DirMode, root, v.Dir)}))
	}
	roots, err := mktree(ctx, r, gitDir, defs)
	if err != nil {
		return nil, err
	}
	for i, v := range versions {
		if want := treeid.Wrap(v.Dir, plans[i].ids[""]); roots[i] != want {
			return nil, fmt.Errorf("git wrote the import tree of %s as %s, not %s", v.Dir, roots[i], want)
		}
	}
	return roots, nil
}

// treePlan is one version's import tree: what each directory holds once an
// import has left out what it leaves out, the id each directory ends at,
// the id the source stores it under, and which directories sit at which
// depth.
type treePlan struct {
	dir      string                    // the upstream directory, for an error to name
	entries  map[string][]treeid.Entry // by directory, "" the skill's own, in git's order
	ids      map[string]string         // the id each directory ends at; "" for one left with nothing
	stored   map[string]string         // the id the source stores each directory under
	byDepth  map[int][]string
	maxDepth int
}

// plannedDir names one directory of one version, for holding the ids of a
// level's mktree to the ones the plan computed.
type plannedDir struct {
	plan *treePlan
	dir  string
}

// planTrees reads the entries of one version into its directories and
// computes, deepest first, the id each one ends at: its regular files as
// git lists them, which is with their canonical modes, and the directories
// below it at the ids they end at, one left with nothing left out. links
// keeps the symlinks too, which an import leaves out and a version laid
// out on disk holds.
func planTrees(v Version, links bool) *treePlan {
	p := &treePlan{
		dir:     v.Dir,
		entries: map[string][]treeid.Entry{},
		ids:     map[string]string{},
		stored:  map[string]string{"": v.Tree},
		byDepth: map[int][]string{},
	}
	files := map[string][]treeid.Entry{}
	subdirs := map[string][]string{}
	for _, e := range v.Entries {
		parent := path.Dir(e.Path)
		if parent == "." {
			parent = ""
		}
		switch {
		case e.Mode == source.DirMode:
			p.stored[e.Path] = e.OID
			subdirs[parent] = append(subdirs[parent], e.Path)
			depth := strings.Count(e.Path, "/") + 1
			p.byDepth[depth] = append(p.byDepth[depth], e.Path)
			p.maxDepth = max(p.maxDepth, depth)
		case source.IsFileMode(e.Mode), links && e.Mode == source.SymlinkMode:
			files[parent] = append(files[parent], treeid.Entry{Name: path.Base(e.Path), Mode: e.Mode, OID: e.OID})
		}
	}
	for depth := p.maxDepth; depth >= 0; depth-- {
		for _, dir := range p.dirsAt(depth) {
			entries := files[dir]
			for _, sub := range subdirs[dir] {
				if id := p.ids[sub]; id != "" {
					entries = append(entries, treeid.Entry{Name: path.Base(sub), Mode: source.DirMode, OID: id})
				}
			}
			if len(entries) == 0 {
				p.ids[dir] = "" // every entry left out: the directory goes with them
				continue
			}
			treeid.Sort(entries)
			p.entries[dir] = entries
			p.ids[dir] = treeid.TreeID(entries)
		}
	}
	return p
}

// dirsAt names the directories of this version at depth, in a fixed order.
func (p *treePlan) dirsAt(depth int) []string {
	if depth == 0 {
		return []string{""}
	}
	dirs := p.byDepth[depth]
	sort.Strings(dirs)
	return dirs
}

// level names the directories of this version at depth that have to be
// written anew: every one whose id is not the one the source stores it
// under, and that is left with anything at all.
func (p *treePlan) level(depth int) []string {
	var needed []string
	for _, dir := range p.dirsAt(depth) {
		if id := p.ids[dir]; id != "" && id != p.stored[dir] {
			needed = append(needed, dir)
		}
	}
	return needed
}

// entryLines are the mktree lines of one directory's entries.
func entryLines(entries []treeid.Entry) []string {
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = entryLine(e.Mode, e.OID, e.Name)
	}
	return lines
}

// mktree writes the trees defs defines, one definition per tree, in one git
// process, and returns their ids in the same order. Every definition is a
// run of NUL-terminated records, so the boundary between two of them is an
// empty record and joining them with one more NUL is what separates them.
// It is the only place a tree is written: one writer means one place where
// the records are framed, and framing them any other way silently renames
// a file.
func mktree(ctx context.Context, r *gitx.Runner, gitDir string, defs []string) ([]string, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(strings.Join(defs, "\x00")), "mktree", "-z", "--batch")
	if err != nil {
		return nil, err
	}
	written := strings.Fields(out)
	if len(written) != len(defs) {
		return nil, fmt.Errorf("git mktree wrote %d trees for %d directories", len(written), len(defs))
	}
	return written, nil
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

// HasFileToImport reports whether a skill's tree holds anything an import
// tree can carry: one regular file, at any depth. A directory of nothing
// but symlinks and submodules has none, and an import of it would be a
// commit with an empty tree.
//
// It is one predicate on purpose. writeTrees discovers the same thing, by
// finding nothing left to put in the root tree, but by then it can only
// fail the whole run, where the contract says a skill that cannot be
// installed costs only itself. So the caller asks this first and refuses
// that one skill, and the two cannot drift apart into two conditions.
func HasFileToImport(entries []source.TreeEntry) bool {
	for _, e := range entries {
		if source.IsFileMode(e.Mode) {
			return true
		}
	}
	return false
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
//
// It is what git could have written and nothing wider: a run of digits that
// does not fit a signed 64-bit integer is not a committer time git stores,
// and fast-import, unlike commit-tree, would take it and write a commit
// dated somewhere no reader can put it.
func UpstreamDate(epoch string) (string, error) {
	if epoch == "" || strings.TrimLeft(epoch, "0123456789") != "" {
		return "", fmt.Errorf("%w: %q is not a committer time", ErrTrailer, epoch)
	}
	if _, err := strconv.ParseInt(epoch, 10, 64); err != nil {
		return "", fmt.Errorf("%w: %q is not a committer time git could have written", ErrTrailer, epoch)
	}
	return epoch + " +0000", nil
}
