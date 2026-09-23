package source

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// RefPrefix is where the last fetched state of every source lives in the
// account repo: one ref per source id.
const RefPrefix = "refs/agentx/sources/"

// StagingRefPrefix is where a fetch in flight holds the commit it is still
// filling in, one ref per source id. A fetch reaches a source over two
// network round trips — the commit and its trees, then the SKILL.md blobs —
// and between them there is a commit the account repo can read whose blobs
// are not here yet. Staging keeps that commit off RefPrefix until it is
// whole, so nothing that reads a source ref can see a half fetched one.
const StagingRefPrefix = "refs/agentx/fetching/"

// RemoteName is the account repo remote of the source with id.
func RemoteName(id string) string { return "src-" + id }

// Ref is the account repo ref that holds the last fetched commit of the
// source with id.
func Ref(id string) string { return RefPrefix + id }

// StagingRef is the ref a fetch of the source with id stages on.
func StagingRef(id string) string { return StagingRefPrefix + id }

// Errors a fetch or a listing can report; the CLI maps them to exit codes.
var (
	ErrUnreachable = errors.New("cannot fetch the source")          // network, authentication or not a repository
	ErrRefNotFound = errors.New("ref not found in the source")      // the pin names no branch, tag or commit
	ErrNotFetched  = errors.New("source not fetched")               // the account repo holds no ref for it
	ErrNoSubpath   = errors.New("subpath not in the source")        // the subpath is not a directory at the fetched commit
	ErrIncomplete  = errors.New("the fetched source is incomplete") // an object the listing needs is not in the account repo
	ErrUnsafePath  = errors.New("unsafe path in the source")        // a tree entry that could be laid out outside its directory
)

// Skill is one installable skill of a source at its fetched commit.
type Skill struct {
	Subpath     string // the skill directory from the repository root, "" for the root
	Name        string // the frontmatter name, else the directory name
	Description string
	Tree        string // the tree id of the skill directory
}

// Listing is what a source holds at its fetched commit under a subpath.
// Previous is the commit the source ref held before this fetch, empty when
// it held none and when the listing came from List.
type Listing struct {
	Commit   string
	Previous string
	Skills   []Skill // sorted by subpath
}

// Refspec is what remote.src-<id>.fetch holds for the source: its pinned
// ref, or the remote HEAD when it follows the remote's default branch,
// onto the source's staging ref. The left side is the record of the pin.
// The right side is the staging ref and never the source ref, so that
// refs/agentx/sources/<id> is written by the update-ref at the end of a
// fetch and by nothing else: not by a fetch this code did not make, and
// not by a refspec left behind by an older or interrupted run.
func Refspec(s Source) string {
	src := "HEAD"
	if s.Ref != "" {
		src = s.Ref
	}
	return "+" + src + ":" + StagingRef(s.ID())
}

// Configure writes the source's remote into the account repo: its
// canonical URL, the refspec above, no tags, and the promisor and
// blob:none filter settings of a partial clone. It is safe to repeat and
// updates an existing remote.
func Configure(ctx context.Context, r *gitx.Runner, gitDir string, s Source) error {
	name := RemoteName(s.ID())
	for _, kv := range [][2]string{
		{"url", s.URL},
		{"fetch", Refspec(s)},
		{"tagOpt", "--no-tags"},
		{"promisor", "true"},
		{"partialclonefilter", "blob:none"},
	} {
		if _, err := r.Isolated(ctx, gitDir, "config", "remote."+name+"."+kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// SetRefspec writes the source's fetch refspec alone, for a remote whose
// other settings are already right. It is what brings a remote back in
// line with the pin the settings hold.
func SetRefspec(ctx context.Context, r *gitx.Runner, gitDir string, s Source) error {
	_, err := r.Isolated(ctx, gitDir, "config", "remote."+RemoteName(s.ID())+".fetch", Refspec(s))
	return err
}

// Remote is what the account repo's config records about one source: the
// URL it is fetched from and the refspec that records its pin. Either can
// be empty, since a run killed part way through writing a remote leaves
// what it had written.
type Remote struct {
	URL     string
	Refspec string
}

// Remotes maps the id of every source remote of the account repo to what
// the config records about it, read in one config --get-regexp so that a
// run over many sources checks them all in one git process. An account repo
// with no source remote at all makes that command exit 1, which is the
// empty map and not a failure; a config git really cannot read fails the
// write or the fetch that follows.
func Remotes(ctx context.Context, r *gitx.Runner, gitDir string) map[string]Remote {
	remotes := map[string]Remote{}
	out, err := r.Isolated(ctx, gitDir, "config", "--get-regexp", `^remote\.src-[0-9a-f]+\.(url|fetch)$`)
	if err != nil {
		return remotes
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		id, field, ok := strings.Cut(strings.TrimPrefix(key, "remote.src-"), ".")
		if !ok {
			continue
		}
		remote := remotes[id]
		switch field {
		case "url":
			remote.URL = value
		case "fetch":
			remote.Refspec = value
		}
		remotes[id] = remote
	}
	return remotes
}

// Fetch fetches the source's ref into the account repo without blobs, in
// the user's git environment, then the SKILL.md blobs of every skill under
// the subpath in one batch, and returns the listing built from them. A
// server that cannot filter or serve single objects gets one full fetch
// instead. The remote must be configured.
//
// Nothing of this lands on the source ref until all of it has arrived. The
// two fetches write a staging ref of this source alone, and the source ref
// is moved onto the fetched object in one update-ref at the end, once every
// SKILL.md blob of the new commit is in the account repo. A fetch that
// fails anywhere therefore leaves the source ref exactly where the last
// complete fetch left it, and no reader of a source ref — the serve child
// rebuilding its index, a concurrent listing, the next command after a
// crash — ever sees a commit whose skills cannot be listed.
func Fetch(ctx context.Context, r *gitx.Runner, gitDir string, s Source) (Listing, error) {
	id := s.ID()
	name := RemoteName(id)
	staging := StagingRef(id)
	previous, _ := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", Ref(id)+"^{commit}")
	// The refspec is built from the pin this call was given, which is the
	// pin the settings hold, rather than left to remote.<name>.fetch: a
	// remote a run was interrupted before it could write tracks a ref the
	// settings do not name, and a fetch that followed it would report one
	// pin while fetching another. --refmap= is what makes the refspec here
	// the only one, since a refspec on the command line does not replace
	// the configured one — git also updates that one opportunistically.
	fetchArgs := baseFetchArgs()
	refspec := Refspec(s)
	// The staging ref belongs to this fetch and goes with it, whether it
	// finished or failed, so a failure leaves no ref behind and the objects
	// it brought fall to the next maintenance. Only a crash can leak one,
	// and the next fetch of the source overwrites it, source remove deletes
	// it, and nothing reads it in between. The deletion outlives a cancelled
	// context: it is a local ref write, and leaving the ref is worse.
	defer func() {
		_, _ = r.Isolated(context.WithoutCancel(ctx), gitDir, "update-ref", "-d", staging)
	}()
	if _, err := r.User(ctx, gitDir, append(fetchArgs, "--filter=blob:none", name, refspec)...); err != nil {
		if strings.Contains(err.Error(), "couldn't find remote ref") {
			return Listing{}, fmt.Errorf("%w: %v", ErrRefNotFound, err)
		}
		return Listing{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	fetched, commit, err := staged(ctx, r, gitDir, staging)
	if err != nil || commit == "" {
		// The fetch landed, so this is the ref itself: a pin that names a
		// tag pointing at something other than a commit, never a network or
		// credential problem.
		return Listing{}, fmt.Errorf("%w: the fetched ref does not name a commit: %v", ErrRefNotFound, err)
	}
	// The SKILL.md blobs of the whole repository are fetched, not only those
	// under this command's subpath. A subpath scopes a listing and is never
	// stored, so a later listing of another part of the source, or of the
	// whole of it, must find every blob it needs already here: nothing after
	// an add reads the network.
	all, err := skillEntries(ctx, r, gitDir, commit, "", false)
	if err != nil {
		return Listing{}, err
	}
	if len(all) > 0 {
		if err := fetchBlobs(ctx, r, gitDir, name, all); err != nil {
			// Filtered, but single objects are refused: take everything
			// once. No stock git server does this, since
			// uploadpack.allowFilter implies allow-any-sha1-in-want and
			// setting allowAnySHA1InWant false does not take it back, so
			// this is a hosting service's own policy layer, or a ref that
			// moved and was reclaimed between the two fetches.
			if _, err := r.User(ctx, gitDir, append(fetchArgs, "--refetch", "--no-filter", name, refspec)...); err != nil {
				return Listing{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
			}
		}
		// What the source ref promises is checked before it is made: a
		// server that answers the batch without sending every object it was
		// asked for would otherwise leave a readable commit whose skills no
		// later listing can read.
		if dir, err := missingBlob(ctx, r, gitDir, all); err != nil {
			return Listing{}, err
		} else if dir != "" {
			return Listing{}, fmt.Errorf("%w: %s/SKILL.md did not arrive with the fetch", ErrIncomplete, dir)
		}
	}
	var entries []skillEntry
	if s.Subpath == "" {
		entries = filterSkipped(all)
	} else if entries, err = skillEntries(ctx, r, gitDir, commit, s.Subpath, true); err != nil {
		return Listing{}, err
	}
	skills, err := readSkills(ctx, r, gitDir, s.URL, entries)
	if err != nil {
		return Listing{}, err
	}
	// Whole at last: one update-ref publishes the fetch, and every reader
	// goes from the previous complete commit to this one with nothing in
	// between. The object read from the staging ref is used rather than the
	// ref itself, so that a second fetch of the same source that staged a
	// newer commit meanwhile cannot be published here unverified.
	if _, err := r.Isolated(ctx, gitDir, "update-ref", Ref(id), fetched); err != nil {
		return Listing{}, err
	}
	return Listing{Commit: commit, Previous: previous, Skills: skills}, nil
}

// staged reads what a fetch put on the staging ref in one for-each-ref: the
// object the ref names, which is what the source ref must end up holding,
// since a pin naming an annotated tag puts the tag object there and every
// reader peels it, and the commit it peels to, which is what the trees are
// walked from. The commit is empty for a ref that names no commit, an
// annotated tag on a tree included, and for a ref that is not there.
func staged(ctx context.Context, r *gitx.Runner, gitDir, ref string) (object, commit string, err error) {
	out, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(objectname) %(objecttype) %(*objectname) %(*objecttype)", ref)
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(out)
	switch {
	case len(fields) >= 4 && fields[1] == "tag" && fields[3] == "commit": // an annotated tag: the ref holds it, the listing reads what it peels to
		return fields[0], fields[2], nil
	case len(fields) >= 2 && fields[1] == "commit":
		return fields[0], fields[0], nil
	}
	return "", "", nil
}

// List builds the listing of an already fetched source from the account
// repo alone: no network.
func List(ctx context.Context, r *gitx.Runner, gitDir string, s Source) (Listing, error) {
	// The ref is read with for-each-ref rather than with rev-parse --verify
	// --quiet, which exits non-zero both for a ref that is not there and for
	// a repository git cannot read and so cannot tell the two apart. The
	// difference is what the reader is told: a source that was never fetched
	// is exit code 5 with a hint to add it, while an account repo git cannot
	// read is exit code 8, and a listing that reported the second as the
	// first would send the reader to a command that fails differently again.
	// for-each-ref prints nothing and succeeds for an absent ref, and fails
	// only when git could not read the refs at all.
	_, commit, err := staged(ctx, r, gitDir, Ref(s.ID()))
	if err != nil {
		return Listing{}, err // the account repo itself, not a source that was never fetched
	}
	if commit == "" {
		return Listing{}, fmt.Errorf("%w: %s", ErrNotFetched, s.URL)
	}
	entries, err := skillEntries(ctx, r, gitDir, commit, s.Subpath, true)
	if err != nil {
		return Listing{}, err
	}
	skills, err := readSkills(ctx, r, gitDir, s.URL, entries)
	if err != nil {
		return Listing{}, err
	}
	return Listing{Commit: commit, Skills: skills}, nil
}

// Configured reports whether the account repo holds the remote of the
// source with id. A settings entry whose remote is gone, which an
// interrupted removal leaves behind, cannot be fetched at all: git answers
// a fetch through it by naming the remote it cannot find, which is agentx's
// own name for the source and not the source.
func Configured(ctx context.Context, r *gitx.Runner, gitDir, id string) bool {
	_, err := r.Isolated(ctx, gitDir, "config", "--get", "remote."+RemoteName(id)+".url")
	return err == nil
}

// Present reports whether the account repo holds a remote or a ref for the
// source with id, which it can do without a settings entry when an add was
// cut short.
func Present(ctx context.Context, r *gitx.Runner, gitDir, id string) bool {
	if Configured(ctx, r, gitDir, id) {
		return true
	}
	out, err := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", Ref(id))
	return err == nil && out != ""
}

// Remove deletes the source's remote and ref from the account repo, and the
// staging ref a fetch killed mid-flight can leave behind, so that removing a
// source leaves nothing of it under refs/agentx. The objects stay until
// maintenance reclaims them.
func Remove(ctx context.Context, r *gitx.Runner, gitDir, id string) error {
	if _, err := r.Isolated(ctx, gitDir, "config", "--get", "remote."+RemoteName(id)+".url"); err == nil {
		if _, err := r.Isolated(ctx, gitDir, "remote", "remove", RemoteName(id)); err != nil {
			return err
		}
	}
	if _, err := r.Isolated(ctx, gitDir, "update-ref", "-d", StagingRef(id)); err != nil {
		return err
	}
	_, err := r.Isolated(ctx, gitDir, "update-ref", "-d", Ref(id))
	return err
}

// Commits maps the id of every fetched source to its commit, read in one
// for-each-ref.
func Commits(ctx context.Context, r *gitx.Runner, gitDir string) (map[string]string, error) {
	out, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname:strip=3) %(*objectname) %(objectname)", RefPrefix)
	if err != nil {
		return nil, err
	}
	commits := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		switch len(fields) {
		case 3: // an annotated tag: the peeled id comes first
			commits[fields[0]] = fields[1]
		case 2:
			commits[fields[0]] = fields[1]
		}
	}
	return commits, nil
}

// TreeEntry is one entry of a tree in the account repo: its path below the
// tree read, its mode as git writes it (100644 and 100755 for a file,
// 040000 for a directory, 120000 for a symlink, 160000 for a submodule) and
// its object id.
type TreeEntry struct {
	Path string
	Mode string
	OID  string
}

// The tree entry modes agentx reads: the two a regular file has and the
// one a directory has. A symlink (120000) and a submodule (160000) are
// neither, and a skill is imported without them.
const (
	FileMode       = "100644"
	ExecutableMode = "100755"
	DirMode        = "040000"
)

// IsFileMode reports whether an entry of a tree is a regular file.
func IsFileMode(mode string) bool { return mode == FileMode || mode == ExecutableMode }

// TreeArgs are the arguments ReadTree runs, for a caller that runs this
// read beside another independent one.
func TreeArgs(tree string) []string { return []string{"ls-tree", "-r", "-t", "-z", tree} }

// ReadTree lists every entry below tree, directories included, in one
// ls-tree. The entries come back in git's own order, which is bytewise by
// path within each directory.
func ReadTree(ctx context.Context, r *gitx.Runner, gitDir, tree string) ([]TreeEntry, error) {
	out, err := r.Isolated(ctx, gitDir, TreeArgs(tree)...)
	if err != nil {
		return nil, err
	}
	return ParseTree(out)
}

// ParseTree reads the output of the ls-tree TreeArgs names.
func ParseTree(out string) ([]TreeEntry, error) {
	var entries []TreeEntry
	for _, line := range strings.Split(out, "\x00") {
		if line == "" {
			continue
		}
		meta, name, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("git ls-tree: cannot parse %q", line)
		}
		entries = append(entries, TreeEntry{Path: name, Mode: fields[0], OID: fields[2]})
	}
	return entries, nil
}

// CheckPath refuses a tree entry path, as ParseTree returns it, that could
// be laid out outside the directory it is read below, or inside git's own:
// an empty or absolute path, one holding a backslash or a NUL, or one with
// an empty, ".", ".." or ".git" component, the last in any case. git never
// writes such an entry into a tree it checks out, but a source is any
// repository a user adds and a fetch does not check what it receives, so
// the names are the source's own bytes until this says otherwise. Every
// other name, one git would quote included, is carried through unchanged.
func CheckPath(p string) error {
	bad := p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00")
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.EqualFold(seg, ".git") {
			bad = true
		}
	}
	if bad {
		return fmt.Errorf("%w: %q", ErrUnsafePath, p)
	}
	return nil
}

// MissingArgs are the arguments of the read that lists the objects under
// tree the account repo does not hold, which is how an install learns what
// to fetch without asking git for an object that is not there: a read of a
// missing object in a partial clone either reaches for the network or
// fails, and every local read forbids the lazy fetch.
func MissingArgs(tree string) []string {
	return []string{"rev-list", "--objects", "--missing=print", "--no-object-names", tree}
}

// ParseMissing reads the output of the rev-list MissingArgs names: the
// objects that are missing are the lines marked with a question mark.
func ParseMissing(out string) []string {
	var missing []string
	for _, line := range strings.Split(out, "\n") {
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "?"); ok && id != "" {
			missing = append(missing, id)
		}
	}
	return missing
}

// ReadBlobs reads the blobs with ids from the account repo in one cat-file
// batch and returns their content by id. An id the repository does not hold
// is left out rather than fetched: every local read forbids the lazy fetch
// of a missing object.
func ReadBlobs(ctx context.Context, r *gitx.Runner, gitDir string, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id + "\n")
	}
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "cat-file", "--batch")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIncomplete, err)
	}
	return parseBatch(out)
}

// FetchObjects fetches the objects with ids from the source's remote in one
// batch, by object id on git's standard input, the way git itself fills a
// partial clone. A server that serves a filtered fetch but refuses single
// objects gets one full fetch instead, which is the same fallback a listing
// makes.
func FetchObjects(ctx context.Context, r *gitx.Runner, gitDir string, s Source, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	remote := RemoteName(s.ID())
	if err := fetchIDs(ctx, r, gitDir, remote, ids); err == nil {
		return nil
	}
	args := append(baseFetchArgs(), "--refetch", "--no-filter", remote)
	if _, err := r.User(ctx, gitDir, args...); err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return nil
}

// skillEntry is one SKILL.md found in the fetched tree.
type skillEntry struct {
	dir  string // the skill directory from the repository root
	blob string // the SKILL.md blob id
	tree string // the directory's tree id
}

// skillEntries walks the tree under subpath at commit, tree objects only,
// and returns every directory holding a SKILL.md. With skip, hidden
// directories and node_modules below the subpath are left out, which is what
// a listing shows; without it every one is returned, which is what the blob
// fetch needs so that a later listing of a directory named outright still
// reads from the account repo alone.
func skillEntries(ctx context.Context, r *gitx.Runner, gitDir, commit, subpath string, skip bool) ([]skillEntry, error) {
	treeish := commit + "^{tree}"
	if subpath != "" {
		treeish = commit + ":" + subpath
	}
	root, err := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", treeish)
	if err != nil || root == "" {
		return nil, fmt.Errorf("%w: %q", ErrNoSubpath, subpath)
	}
	if typ, err := r.Isolated(ctx, gitDir, "cat-file", "-t", root); err != nil || typ != "tree" {
		return nil, fmt.Errorf("%w: %q is not a directory", ErrNoSubpath, subpath)
	}
	listed, err := ReadTree(ctx, r, gitDir, root)
	if err != nil {
		return nil, err
	}
	trees := map[string]string{"": root}
	var entries []skillEntry
	for _, e := range listed {
		switch {
		case CheckPath(e.Path) != nil:
			// Not a directory a skill can be in: a SKILL.md below it would be
			// listed under a subpath that names another directory once cleaned.
			// An install reads the skill's own tree and refuses such an entry.
		case e.Mode == DirMode:
			trees[e.Path] = e.OID
		case path.Base(e.Path) == "SKILL.md" && IsFileMode(e.Mode):
			dir := path.Dir(e.Path)
			if dir == "." {
				dir = ""
			}
			if skip && skipped(dir) {
				continue
			}
			entries = append(entries, skillEntry{dir: path.Join(subpath, dir), blob: e.OID, tree: trees[dir]})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].dir < entries[j].dir })
	return entries, nil
}

// filterSkipped drops the entries a listing does not show, for the listing
// of a whole source built from the entries the blob fetch collected.
func filterSkipped(entries []skillEntry) []skillEntry {
	kept := make([]skillEntry, 0, len(entries))
	for _, e := range entries {
		if !skipped(e.dir) {
			kept = append(kept, e)
		}
	}
	return kept
}

// skipped reports whether a directory below the subpath is hidden or
// node_modules, at any level.
func skipped(dir string) bool {
	for _, seg := range strings.Split(dir, "/") {
		if strings.HasPrefix(seg, ".") || seg == "node_modules" {
			return true
		}
	}
	return false
}

// baseFetchArgs are the flags every fetch of a source carries: quiet, no
// tags, no FETCH_HEAD, no submodules, no forced-update report, and no
// refmap, so that a fetch writes the ref its own refspec names and never
// the one remote.<name>.fetch configures, which git would otherwise update
// opportunistically alongside it.
func baseFetchArgs() []string {
	return []string{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--no-show-forced-updates", "--refmap="}
}

// fetchBlobs fetches the SKILL.md blobs of entries from the remote in one
// batch, by object id, the way git itself fills a partial clone.
func fetchBlobs(ctx context.Context, r *gitx.Runner, gitDir, remote string, entries []skillEntry) error {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.blob
	}
	return fetchIDs(ctx, r, gitDir, remote, ids)
}

// fetchIDs asks the remote for the objects with ids in one batch.
func fetchIDs(ctx context.Context, r *gitx.Runner, gitDir, remote string, ids []string) error {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id + "\n")
	}
	// The flags are the ref fetch's, without --no-show-forced-updates: this
	// fetch names objects rather than refs and updates none, so there is no
	// forced update for git to work out and none to suppress.
	_, err := r.UserInput(ctx, gitDir, strings.NewReader(b.String()), "-c", "fetch.negotiationAlgorithm=noop",
		"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--refmap=", "--filter=blob:none", "--stdin", remote)
	return err
}

// missingBlob names the directory of the first entry whose SKILL.md the
// account repo does not hold, and "" when it holds them all. One
// cat-file --batch-check reads the ids alone, never the content, and the
// promisor remote makes a miss an error rather than a "missing" line, since
// GIT_NO_LAZY_FETCH forbids the fetch git would otherwise run; both are a
// source that did not arrive whole.
func missingBlob(ctx context.Context, r *gitx.Runner, gitDir string, entries []skillEntry) (string, error) {
	var ids strings.Builder
	for _, e := range entries {
		ids.WriteString(e.blob + "\n")
	}
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(ids.String()), "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrIncomplete, err)
	}
	here := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == "blob" {
			here[fields[0]] = true
		}
	}
	for _, e := range entries {
		if !here[e.blob] {
			return e.dir, nil
		}
	}
	return "", nil
}

// readSkills reads the SKILL.md blobs of entries from the account repo in
// one cat-file batch and builds the skills from their frontmatter.
func readSkills(ctx context.Context, r *gitx.Runner, gitDir, canonical string, entries []skillEntry) ([]Skill, error) {
	skills := make([]Skill, 0, len(entries))
	if len(entries) == 0 {
		return skills, nil
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.blob
	}
	blobs, err := ReadBlobs(ctx, r, gitDir, ids)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		content, ok := blobs[e.blob]
		if !ok {
			return nil, fmt.Errorf("%w: %s/SKILL.md is not in the account repo", ErrIncomplete, e.dir)
		}
		name, description, _ := scan.SkillFrontmatter(content) // an unusable frontmatter names the skill after its directory
		if name == "" {
			name = path.Base(e.dir)
			if e.dir == "" {
				name = RepoName(canonical)
			}
		}
		skills = append(skills, Skill{Subpath: e.dir, Name: name, Description: description, Tree: e.tree})
	}
	return skills, nil
}

// parseBatch reads cat-file --batch output: "<id> <type> <size>\n" followed
// by the object's bytes and a newline, or "<id> missing\n".
func parseBatch(out string) (map[string]string, error) {
	blobs := map[string]string{}
	for out != "" {
		header, rest, ok := strings.Cut(out, "\n")
		if !ok {
			return nil, fmt.Errorf("git cat-file: truncated output at %q", header)
		}
		fields := strings.Fields(header)
		if len(fields) == 2 && fields[1] == "missing" {
			out = rest
			continue
		}
		if len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file: cannot parse %q", header)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size > len(rest) {
			return nil, fmt.Errorf("git cat-file: truncated object %s", fields[0])
		}
		blobs[fields[0]] = rest[:size]
		out = strings.TrimPrefix(rest[size:], "\n")
	}
	return blobs, nil
}
