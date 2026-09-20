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

// RemoteName is the account repo remote of the source with id.
func RemoteName(id string) string { return "src-" + id }

// Ref is the account repo ref that holds the last fetched commit of the
// source with id.
func Ref(id string) string { return RefPrefix + id }

// Errors a fetch or a listing can report; the CLI maps them to exit codes.
var (
	ErrUnreachable = errors.New("cannot fetch the source")     // network, authentication or not a repository
	ErrRefNotFound = errors.New("ref not found in the source") // the pin names no branch, tag or commit
	ErrNotFetched  = errors.New("source not fetched")          // the account repo holds no ref for it
	ErrNoSubpath   = errors.New("subpath not in the source")   // the subpath is not a directory at the fetched commit
)

// Skill is one installable skill of a source at its fetched commit.
type Skill struct {
	Subpath     string // the skill directory from the repository root, "" for the root
	Name        string // the frontmatter name, else the directory name
	Description string
	Tree        string // the tree id of the skill directory
}

// Listing is what a source holds at its fetched commit under a subpath.
type Listing struct {
	Commit string
	Skills []Skill // sorted by subpath
}

// Configure writes the source's remote into the account repo: its
// canonical URL, one refspec from the pinned ref (or the remote HEAD) onto
// the source ref, no tags, and the promisor and blob:none filter settings
// of a partial clone. It is safe to repeat and updates an existing remote.
func Configure(ctx context.Context, r *gitx.Runner, gitDir string, s Source) error {
	id := s.ID()
	name := RemoteName(id)
	src := "HEAD"
	if s.Ref != "" {
		src = s.Ref
	}
	for _, kv := range [][2]string{
		{"url", s.URL},
		{"fetch", "+" + src + ":" + Ref(id)},
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

// Fetch fetches the source's ref into the account repo without blobs, in
// the user's git environment, then the SKILL.md blobs of every skill under
// the subpath in one batch, and returns the listing built from them. A
// server that cannot filter or serve single objects gets one full fetch
// instead. The remote must be configured.
func Fetch(ctx context.Context, r *gitx.Runner, gitDir string, s Source) (Listing, error) {
	id := s.ID()
	name := RemoteName(id)
	fetchArgs := []string{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--no-show-forced-updates"}
	if _, err := r.User(ctx, gitDir, append(fetchArgs, "--filter=blob:none", name)...); err != nil {
		if strings.Contains(err.Error(), "couldn't find remote ref") {
			return Listing{}, fmt.Errorf("%w: %v", ErrRefNotFound, err)
		}
		return Listing{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	commit, err := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", Ref(id)+"^{commit}")
	if err != nil {
		return Listing{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	entries, err := skillEntries(ctx, r, gitDir, commit, s.Subpath)
	if err != nil {
		return Listing{}, err
	}
	if len(entries) > 0 {
		if err := fetchBlobs(ctx, r, gitDir, name, entries); err != nil {
			// Filtered, but single objects are refused: take everything once.
			if _, err := r.User(ctx, gitDir, append(fetchArgs, "--refetch", "--no-filter", name)...); err != nil {
				return Listing{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
			}
		}
	}
	skills, err := readSkills(ctx, r, gitDir, s.URL, entries)
	if err != nil {
		return Listing{}, err
	}
	return Listing{Commit: commit, Skills: skills}, nil
}

// List builds the listing of an already fetched source from the account
// repo alone: no network.
func List(ctx context.Context, r *gitx.Runner, gitDir string, s Source) (Listing, error) {
	commit, err := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", Ref(s.ID())+"^{commit}")
	if err != nil || commit == "" {
		return Listing{}, fmt.Errorf("%w: %s", ErrNotFetched, s.URL)
	}
	entries, err := skillEntries(ctx, r, gitDir, commit, s.Subpath)
	if err != nil {
		return Listing{}, err
	}
	skills, err := readSkills(ctx, r, gitDir, s.URL, entries)
	if err != nil {
		return Listing{}, err
	}
	return Listing{Commit: commit, Skills: skills}, nil
}

// Remove deletes the source's remote and ref from the account repo. The
// objects stay until maintenance reclaims them.
func Remove(ctx context.Context, r *gitx.Runner, gitDir, id string) error {
	if _, err := r.Isolated(ctx, gitDir, "config", "--get", "remote."+RemoteName(id)+".url"); err == nil {
		if _, err := r.Isolated(ctx, gitDir, "remote", "remove", RemoteName(id)); err != nil {
			return err
		}
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

// skillEntry is one SKILL.md found in the fetched tree.
type skillEntry struct {
	dir  string // the skill directory from the repository root
	blob string // the SKILL.md blob id
	tree string // the directory's tree id
}

// skillEntries walks the tree under subpath at commit, tree objects only,
// and returns every directory holding a SKILL.md, skipping hidden
// directories and node_modules below the subpath.
func skillEntries(ctx context.Context, r *gitx.Runner, gitDir, commit, subpath string) ([]skillEntry, error) {
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
	out, err := r.Isolated(ctx, gitDir, "ls-tree", "-r", "-t", "-z", root)
	if err != nil {
		return nil, err
	}
	trees := map[string]string{"": root}
	var entries []skillEntry
	for _, line := range strings.Split(out, "\x00") {
		if line == "" {
			continue
		}
		meta, name, ok := strings.Cut(line, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("git ls-tree: cannot parse %q", line)
		}
		mode, typ, oid := fields[0], fields[1], fields[2]
		switch {
		case typ == "tree":
			trees[name] = oid
		case typ == "blob" && path.Base(name) == "SKILL.md" && (mode == "100644" || mode == "100755"):
			dir := path.Dir(name)
			if dir == "." {
				dir = ""
			}
			if skipped(dir) {
				continue
			}
			entries = append(entries, skillEntry{dir: path.Join(subpath, dir), blob: oid, tree: trees[dir]})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].dir < entries[j].dir })
	return entries, nil
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

// fetchBlobs fetches the SKILL.md blobs of entries from the remote in one
// batch, by object id, the way git itself fills a partial clone.
func fetchBlobs(ctx context.Context, r *gitx.Runner, gitDir, remote string, entries []skillEntry) error {
	var ids strings.Builder
	for _, e := range entries {
		ids.WriteString(e.blob + "\n")
	}
	_, err := r.UserInput(ctx, gitDir, strings.NewReader(ids.String()),
		"-c", "fetch.negotiationAlgorithm=noop",
		"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--filter=blob:none", "--stdin", remote)
	return err
}

// readSkills reads the SKILL.md blobs of entries from the account repo in
// one cat-file batch and builds the skills from their frontmatter.
func readSkills(ctx context.Context, r *gitx.Runner, gitDir, canonical string, entries []skillEntry) ([]Skill, error) {
	skills := make([]Skill, 0, len(entries))
	if len(entries) == 0 {
		return skills, nil
	}
	var ids strings.Builder
	for _, e := range entries {
		ids.WriteString(e.blob + "\n")
	}
	out, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(ids.String()), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	blobs, err := parseBatch(out)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		content, ok := blobs[e.blob]
		if !ok {
			return nil, fmt.Errorf("git cat-file: blob %s of %s/SKILL.md is missing", e.blob, e.dir)
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
