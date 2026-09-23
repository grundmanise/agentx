package cli

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// adoptHistoryCommits is how far back an adoption looks through the history
// of one skill's directory for the version the lock file recorded. A skill
// changes a handful of times a year, so a version this does not reach is
// one the source rewrote past rather than one the user installed.
const adoptHistoryCommits = 500

// basePlan is where one skill's base version was established: the commit of
// the source the version is read from, the tree that holds it, and how it
// was established, which the run reports because it is the whole question
// the command turns on.
//
// commit starts as the commit the version was read at, and upstreamCommits
// makes it the commit an install of that version records: the last commit
// reachable from it that touched the skill's directory, which holds the
// same tree. when is that commit's committer time, which the import
// commit's dates take.
//
// confirm marks the one route that is not established from the source
// alone: the directory is taken to hold the version the source has now, and
// that is true only while the two hash alike, which is checked before the
// version is read and again under the lock.
type basePlan struct {
	commit  string
	tree    string
	when    string
	confirm bool
	how     string
}

// How a base was established, which the run says of every skill it adopts:
// the question of where a version came from is the one the command exists
// to answer, so its answer is never left implied.
const (
	howRecorded = "the version the lock file recorded, found in the source"
	howCurrent  = "the version the source holds at that commit, which the directory holds unchanged"
	howChosen   = "the version named with --base"
)

// treeRequest is one <commit>:<subpath> a batch of tree lookups asks about.
type treeRequest struct {
	commit  string
	subpath string
}

// treeish names a skill's directory at one commit the way git reads it.
func treeish(commit, subpath string) string {
	if subpath == "" {
		return commit + "^{tree}"
	}
	return commit + ":" + subpath
}

// readAdoptBases establishes the base version of every skill the run is
// about to adopt and reads that version out of the account repo.
//
// The base is an upstream version and never what the directory happens to
// hold. It is established one of three ways, tried as a cascade in this
// order, each route handing on what it could not reach:
//
//  1. --base names a commit, a branch or a tag of the source outright,
//     which is the explicit choice offered when nothing else can establish
//     one.
//  2. The folder hash the lock file recorded is the id of the tree the
//     source holds at that subpath, at the source's fetched commit or at
//     one behind it. Git's own content addressing is the verification: an
//     id that names that tree names that content and nothing else.
//  3. The directory holds exactly the version the source has now, which
//     makes the on-disk content an upstream version rather than an edit of
//     one. This is the same adoption an install makes for a library
//     directory whose content hash is the version's.
//
// A skill none of them reaches is left unmanaged with the reason, since
// recording what is on disk as the version it came from would turn every
// edit into upstream content. That reason names every route that was
// tried, because a refusal says that no route established a version and
// never that the first one did not.
func (inv *invocation) readAdoptBases(ctx context.Context, run *adoptRun, ready []*candidate, sel adoptSelection) ([]*candidate, error) {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	if !exists {
		return nil, fail(exitNotFound, "this machine has fetched no source", "run 'agentx source add <url>' for the source the skills came from")
	}
	commits, err := source.Commits(ctx, inv.git, gitDir)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	var fetched []*candidate
	for _, c := range ready {
		tip := commits[c.src.ID()]
		if tip == "" {
			run.drop(c, refuse(exitNotFound, "the account repo holds no ref for "+c.src.URL,
				"run 'agentx source fetch "+c.src.URL+"' and adopt again"))
			continue
		}
		c.tip = tip
		fetched = append(fetched, c)
	}
	if len(fetched) == 0 {
		return nil, nil
	}
	if sel.base != "" {
		if err := inv.chosenBase(ctx, run, gitDir, fetched[0], sel.base); err != nil {
			return nil, err
		}
	} else if err := inv.establishBases(ctx, run, gitDir, fetched); err != nil {
		return nil, err
	}
	var established []*candidate
	for _, c := range fetched {
		if c.base != nil {
			established = append(established, c)
		}
	}
	if len(established) == 0 {
		return nil, nil
	}
	established, err = inv.upstreamCommits(ctx, run, gitDir, established)
	if err != nil || len(established) == 0 {
		return nil, err
	}
	return inv.readAdoptVersions(ctx, run, gitDir, established)
}

// establishBases works routes 2 and 3 as one cascade: the tree the lock
// file's folder hash names, found anywhere in the history of that skill's
// directory, and otherwise the source's current version for a directory
// that holds exactly it. Only a candidate no route reached is refused.
func (inv *invocation) establishBases(ctx context.Context, run *adoptRun, gitDir string, cands []*candidate) error {
	pairs := make([]treeRequest, len(cands))
	for i, c := range cands {
		pairs[i] = treeRequest{commit: c.tip, subpath: c.subpath()}
	}
	trees, err := inv.treesAt(ctx, gitDir, pairs)
	if err != nil {
		return err
	}
	var searching []*candidate
	for i, c := range cands {
		// The tree at the fetched commit is what the directory is compared
		// with below. It is empty for an upstream-removed skill, whose
		// directory the source no longer has, and that is no reason to stop:
		// the version the lock file recorded is behind the tip, which is
		// exactly what the history search is for.
		c.tipTree = trees[i]
		if plausibleTreeID(c.entry.FolderHash) {
			searching = append(searching, c)
		}
	}
	if err := inv.searchHistory(ctx, gitDir, searching); err != nil {
		return err
	}
	for _, c := range cands {
		switch {
		case c.base != nil: // the lock file named the version and the source holds it
		case c.tipTree != "":
			// The folder hash named no version of this source: the lock file
			// recorded none, or recorded one in that tool's own algorithm,
			// which is not comparable with a git object id, or recorded one
			// this source has never held. What is left is the one case where
			// the directory itself establishes a version, and it does so only
			// while it matches the source exactly, which is checked before the
			// version is read and again under the lock.
			c.base = &basePlan{commit: c.tip, tree: c.tipTree, confirm: true, how: howCurrent}
		default:
			run.drop(c, c.noVersion())
		}
	}
	return nil
}

// searchHistory looks back through the history of each skill's directory
// for the commit at which it took the content the lock file recorded, which
// is the version that tool installed. The search is one git process per
// skill and one for all their commits together, and it stops at
// adoptHistoryCommits.
//
// The commit it reports is the newest one of the first-parent line at which
// the directory holds that content, whether or not the directory still
// holds it at the fetched commit. That is where the version is present and
// not yet the commit it is dated by: a version that reached the branch
// through a merge is found at the merge. upstreamCommits then records the
// commit an install of that version records, read from the source and not
// from what this machine happens to have fetched, which is what keeps two
// machines adopting the same version at the same commit, and so writing
// the same import commit for it.
func (inv *invocation) searchHistory(ctx context.Context, gitDir string, cands []*candidate) error {
	if len(cands) == 0 {
		return nil
	}
	calls := make([][]string, len(cands))
	for i, c := range cands {
		c.searched = true
		args := []string{"rev-list", "--first-parent", "--max-count=" + strconv.Itoa(adoptHistoryCommits), c.tip}
		if c.subpath() != "" {
			args = append(args, "--", ":(literal)"+c.subpath())
		}
		calls[i] = args
	}
	outs, err := inv.git.IsolatedAll(ctx, gitDir, calls)
	if err != nil {
		return accountRepoFailure(err)
	}
	var pairs []treeRequest
	var owner []*candidate
	for i, c := range cands {
		for _, line := range strings.Split(outs[i], "\n") {
			if commit := strings.TrimSpace(line); commit != "" {
				pairs = append(pairs, treeRequest{commit: commit, subpath: c.subpath()})
				owner = append(owner, c)
			}
		}
	}
	trees, err := inv.treesAt(ctx, gitDir, pairs)
	if err != nil {
		return err
	}
	for i, c := range owner {
		// rev-list prints the newest commit first, so the first match is the
		// newest commit at which the directory took that content.
		if c.base == nil && trees[i] == c.entry.FolderHash {
			c.base = &basePlan{commit: pairs[i].commit, tree: trees[i], how: howRecorded}
		}
	}
	return nil
}

// upstreamCommits moves every base from the commit its version was read
// at, the one the history search found, the fetched commit or the one
// --base named, to the commit an install of that version records: the last
// commit reachable from it that touched the skill's directory, found by the
// very reads an install makes. The directory holds the same tree at both,
// so the version is unchanged; what changes is that the import commit no
// longer depends on when this machine fetched, or on which line of a merge
// the history search walked. Every commit a base is read at is reachable
// from the source's fetched commit, so the bases of one source share one
// history walk from it, however many skills the run adopts and whichever
// route established each, and that walk dates each commit too.
//
// A directory no commit of the walk touched holds no entry at all, since
// every path a commit holds was added by one, so there is nothing to
// import for it. That costs the one skill, which is refused as one with no
// regular file is, and never the rest of the run: the subpath is the lock
// file's and the tree at it is whatever the source holds.
func (inv *invocation) upstreamCommits(ctx context.Context, run *adoptRun, gitDir string, cands []*candidate) ([]*candidate, error) {
	var tips []string
	at := map[string][]treeRequest{}
	for _, c := range cands {
		if at[c.tip] == nil {
			tips = append(tips, c.tip)
		}
		at[c.tip] = append(at[c.tip], treeRequest{commit: c.base.commit, subpath: c.subpath()})
	}
	var calls [][]string
	counts := make([]int, len(tips))
	for i, tip := range tips {
		reads := upstreamReadsAt(tip, at[tip])
		counts[i] = len(reads)
		calls = append(calls, reads...)
	}
	outs, err := inv.git.IsolatedAll(ctx, gitDir, calls)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	found := map[treeRequest]upstream{}
	for i, tip := range tips {
		ups, err := foundUpstreams(tip, at[tip], outs[:counts[i]])
		if err != nil {
			return nil, accountRepoFailure(err)
		}
		outs = outs[counts[i]:]
		for r, up := range ups {
			found[r] = up
		}
	}
	var kept []*candidate
	for _, c := range cands {
		up, ok := found[treeRequest{commit: c.base.commit, subpath: c.subpath()}]
		if !ok {
			run.drop(c, refuse(exitRefused, c.entry.Name+" has no regular file to import",
				fmt.Sprintf("%s holds no file at %s in %s; leave it unmanaged", c.subpath(), short(c.base.commit), c.src.URL)))
			continue
		}
		c.base.commit, c.base.when = up.commit, up.when
		kept = append(kept, c)
	}
	return kept, nil
}

// noVersion is the refusal of a candidate every route left without a base.
// It says what each route tried, in the order they were tried, since the
// skill is left unmanaged because nothing established a version for it and
// not because one particular lookup missed. The two ways on are the ones
// the contract names: choose the version, or fork the directory.
func (c *candidate) noVersion() *failure {
	where := c.src.URL + " at " + short(c.tip)
	var byHash, byDirectory string
	switch {
	case c.searched:
		byHash = fmt.Sprintf("no version of %s in %s, or in the %d commits of it behind that, has the folder hash %s recorded",
			dirOrRoot(c.subpath()), where, adoptHistoryCommits, c.entry.File)
	case c.entry.FolderHash == "":
		byHash = c.entry.File + " records no folder hash for it"
	default:
		byHash = c.entry.File + " records a folder hash that no git object id can be"
	}
	if c.tipTree == "" {
		byDirectory = where + " has no " + dirOrRoot(c.subpath())
	} else {
		byDirectory = "the directory is not the version " + where + " holds"
	}
	return refuse(exitRefused,
		fmt.Sprintf("the version %s was installed at cannot be established: %s, and %s", c.entry.Name, byHash, byDirectory),
		"leave it unmanaged, or name the version it came from with 'agentx adopt --skill "+c.entry.Name+" --base <commit, branch or tag>'")
}

// chosenBase is --base: the user names the version of the source to record
// as the base, which is the explicit choice offered for a skill whose own
// version cannot be established. It is still an upstream version, read from
// the source at the commit they named; what the directory holds is left
// alone and differs from it or does not.
func (inv *invocation) chosenBase(ctx context.Context, run *adoptRun, gitDir string, c *candidate, base string) error {
	commit, err := inv.baseCommit(ctx, gitDir, c, base)
	if err != nil {
		return err
	}
	if commit == "" {
		run.drop(c, refuse(exitNotFound, fmt.Sprintf("%s has no commit, branch or tag %q", c.src.URL, sanitised(base)),
			"name a branch, a tag or a commit id of the source; 'git ls-remote "+c.src.URL+"' lists what it publishes"))
		return nil
	}
	// The commit has to be one of this source's own history: an object id
	// another source put in the account repo would otherwise be recorded as
	// a version of this one. A ref of the source this machine has not
	// fetched the history of is refused here too, which is the same answer:
	// it is no version of what this machine follows.
	if _, err := inv.git.Isolated(ctx, gitDir, "merge-base", "--is-ancestor", commit, c.tip); err != nil {
		run.drop(c, refuse(exitRefused, fmt.Sprintf("%s is not in the history of %s that this machine fetched", short(commit), c.src.URL),
			"name a commit of the ref the source follows, or add the source at the ref you want with 'agentx source add "+c.src.URL+"#<ref>'"))
		return nil
	}
	trees, err := inv.treesAt(ctx, gitDir, []treeRequest{{commit: commit, subpath: c.subpath()}})
	if err != nil {
		return err
	}
	if trees[0] == "" {
		run.drop(c, refuse(exitNotFound, fmt.Sprintf("%s has no %s at %s", c.src.URL, dirOrRoot(c.subpath()), short(commit)),
			"name a commit at which the skill's directory exists"))
		return nil
	}
	c.base = &basePlan{commit: commit, tree: trees[0], how: howChosen}
	return nil
}

// baseCommit is the commit --base names, empty for a name the source does
// not have. A commit id is read from the account repo, which holds the
// history of the source it fetched. A branch or a tag is resolved against
// the source itself: a fetch writes one ref per source and brings no tags
// at all, so the names a user knows a version by — the ones the refusals,
// the contract and the help center all offer — are in the source alone and
// nowhere in this repository.
//
// An object the account repo does not hold is still returned: it is a ref
// of the source outside the history this machine fetched, and the ancestor
// check answers that for what it is rather than as a name that is not there.
func (inv *invocation) baseCommit(ctx context.Context, gitDir string, c *candidate, base string) (string, error) {
	if commit, err := inv.git.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err == nil && commit != "" {
		return commit, nil
	}
	named, err := source.ResolveRef(ctx, inv.git, gitDir, c.src, base)
	if err != nil {
		return "", sourceFailure(err, c.src)
	}
	if named == "" {
		return "", nil
	}
	// The peeled commit when this machine holds the object, which is the
	// usual case for a ref of the history it fetched; the id the source
	// named otherwise, for the ancestor check to refuse.
	if commit, err := inv.git.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", named+"^{commit}"); err == nil && commit != "" {
		return commit, nil
	}
	return named, nil
}

// treesAt resolves every <commit>:<subpath> in one cat-file batch and
// returns the tree id of each, empty for one that is not a directory of
// that commit. One git process answers a whole run, however many skills and
// commits it asks about.
func (inv *invocation) treesAt(ctx context.Context, gitDir string, pairs []treeRequest) ([]string, error) {
	trees := make([]string, len(pairs))
	if len(pairs) == 0 {
		return trees, nil
	}
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(treeish(p.commit, p.subpath) + "\n")
	}
	out, err := inv.git.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	// One line per request, in the order they were asked: a request git
	// cannot resolve is answered with the request and the word missing, so
	// the answers are read by position and never by name.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := range pairs {
		if i >= len(lines) {
			break
		}
		if fields := strings.Fields(lines[i]); len(fields) == 2 && fields[1] == "tree" {
			trees[i] = fields[0]
		}
	}
	return trees, nil
}

// plausibleTreeID reports whether the folder hash a lock file recorded
// could be a git object id at all. The vercel CLI writes a tree id there
// when it read one from the forge and its own digest of the folder when it
// did not, and the second is not comparable with anything in a repository;
// a value that cannot even be an object id is not worth a history search.
func plausibleTreeID(hash string) bool {
	if len(hash) != 40 && len(hash) != 64 {
		return false
	}
	return strings.TrimLeft(hash, "0123456789abcdef") == ""
}

// dirOrRoot names a subpath for a message, the repository root included.
func dirOrRoot(subpath string) string {
	if subpath == "" {
		return "a skill at its root"
	}
	return subpath
}

// readAdoptVersions reads the established versions out of the account repo:
// the tree of each, the blobs this machine does not hold yet in one batch
// per source, and the content hash of every version computed in process, as
// an install reads a version it is about to import. Nothing is laid out on
// disk: the library directory is the user's and is not written.
func (inv *invocation) readAdoptVersions(ctx context.Context, run *adoptRun, gitDir string, ready []*candidate) ([]*candidate, error) {
	trees := distinctTrees(ready)
	calls := make([][]string, len(trees))
	for i, tree := range trees {
		calls[i] = source.TreeArgs(tree)
	}
	outs, err := inv.git.IsolatedAll(ctx, gitDir, calls)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	entries := map[string][]source.TreeEntry{}
	for i, tree := range trees {
		listed, err := source.ParseTree(outs[i])
		if err != nil {
			return nil, accountRepoFailure(err)
		}
		entries[tree] = listed
	}
	missing, err := inv.git.Isolated(ctx, gitDir, source.MissingArgs(trees...)...)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	ready = inv.fetchAdoptBlobs(ctx, run, gitDir, ready, entries, source.ParseMissing(missing))
	if len(ready) == 0 {
		return nil, nil
	}
	bodies, err := source.ReadBlobs(ctx, inv.git, gitDir, fileIDs(ready, entries))
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	var kept []*candidate
	for _, c := range ready {
		if f := inv.fillAdoption(c, entries[c.base.tree], bodies); f != nil {
			run.drop(c, f)
			continue
		}
		kept = append(kept, c)
	}
	return kept, nil
}

// fillAdoption builds the version one candidate is adopted at and decides
// what the library directory is against it. A base that only the directory
// establishes is confirmed here: the two must hash alike, or the version
// the skill was installed at is not established at all and the skill is
// left unmanaged rather than given the directory's content as its upstream.
func (inv *invocation) fillAdoption(c *candidate, listed []source.TreeEntry, bodies map[string]string) *failure {
	v := &imported{
		skill:   source.Skill{Subpath: c.subpath(), Name: c.entry.Name, Tree: c.base.tree},
		name:    c.entry.Name,
		dir:     upstreamDir(c.src, source.Skill{Subpath: c.subpath()}),
		entries: listed,
		when:    c.base.when,
		dropped: lineage.Dropped(listed),
	}
	if f := inv.usable(v, map[string]string{}, c.src); f != nil {
		return f
	}
	if f := v.fill(bodies, c.src.URL); f != nil {
		return f
	}
	if c.base.confirm && v.hash != c.hash {
		// The last route is spent: the directory is not the version the
		// source has, so nothing establishes the one it was installed at.
		return c.noVersion()
	}
	v.imp = lineage.Import{Source: c.src.URL, Path: c.subpath(), Commit: c.base.commit, Hash: v.hash}
	for _, d := range v.dropped {
		inv.out.warn(path.Join(c.subpath(), d) + " is not a regular file and is left out of the import")
	}
	c.imported, c.modified = v, v.hash != c.hash
	return nil
}

// distinctTrees are the trees the run has to read, sorted so that two runs
// over the same skills read them in the same order.
func distinctTrees(cands []*candidate) []string {
	seen := map[string]bool{}
	var trees []string
	for _, c := range cands {
		if !seen[c.base.tree] {
			seen[c.base.tree] = true
			trees = append(trees, c.base.tree)
		}
	}
	sort.Strings(trees)
	return trees
}

// fileIDs are the blob ids of every version the run reads, once each.
func fileIDs(cands []*candidate, entries map[string][]source.TreeEntry) []string {
	seen := map[string]bool{}
	var ids []string
	for _, c := range cands {
		for _, e := range entries[c.base.tree] {
			if source.IsFileMode(e.Mode) && !seen[e.OID] {
				seen[e.OID] = true
				ids = append(ids, e.OID)
			}
		}
	}
	return ids
}

// fetchAdoptBlobs brings the file contents the account repo does not hold
// yet, one batch per source, in the user's git environment as every fetch
// is. A version whose blobs are all here, which is what a skill installed
// from the source's current commit looks like, costs no network at all.
func (inv *invocation) fetchAdoptBlobs(ctx context.Context, run *adoptRun, gitDir string, ready []*candidate, entries map[string][]source.TreeEntry, absent []string) []*candidate {
	if len(absent) == 0 {
		return ready
	}
	gone := map[string]bool{}
	for _, id := range absent {
		gone[id] = true
	}
	perSource := map[string][]string{}
	srcOf := map[string]source.Source{}
	for _, c := range ready {
		srcOf[c.src.URL] = c.src
		seen := map[string]bool{}
		for _, e := range entries[c.base.tree] {
			if source.IsFileMode(e.Mode) && gone[e.OID] && !seen[e.OID] {
				seen[e.OID] = true
				perSource[c.src.URL] = append(perSource[c.src.URL], e.OID)
			}
		}
	}
	failed := map[string]*failure{}
	for _, url := range sortedURLs(perSource) {
		if err := source.FetchObjects(ctx, inv.git, gitDir, srcOf[url], perSource[url]); err != nil {
			f, ok := sourceFailure(err, srcOf[url]).(*failure)
			if !ok {
				f = refuse(exitSource, err.Error(), "run 'agentx source fetch "+url+"' and adopt again")
			}
			failed[url] = f
		}
	}
	if len(failed) == 0 {
		return ready
	}
	var kept []*candidate
	for _, c := range ready {
		if f, bad := failed[c.src.URL]; bad {
			run.drop(c, f)
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

func sortedURLs(m map[string][]string) []string {
	urls := make([]string, 0, len(m))
	for url := range m {
		urls = append(urls, url)
	}
	sort.Strings(urls)
	return urls
}
