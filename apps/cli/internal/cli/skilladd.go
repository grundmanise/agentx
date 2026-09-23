package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The steps one skill of an install reports, plus the rescan the run ends
// with. A step is reported when it ends, as every progress event is.
const (
	phaseBlobs    = "blobs"   // the version's files came out of the source
	phaseImport   = "import"  // the import commit is written
	phaseInstall  = "install" // the library, the branch and the placements are in place
	phaseRescan   = "rescan"  // the affected configurations were read again
	stepsPerSkill = 3
)

// namesInAHint is how many skill names a hint lists before it stops.
const namesInAHint = 12

func newSkillAddCommand(inv *invocation) *cobra.Command {
	var sel selection
	var to []string
	var asCopy, fetch bool
	cmd := &cobra.Command{
		Use:   "add <source>[/<subpath>]",
		Short: "Install skills from a source into the library and place them",
		Long: "Install skills from a source into the library and place them in every enabled\n" +
			"configuration. A source URL this machine has not added yet is added first, as\n" +
			"'agentx source add' would; an added source is installed from as it was last\n" +
			"fetched, unless --fetch fetches it again. Name the skills with --skill, once for\n" +
			"each, or take the whole source with --all and leave out what you do not want\n" +
			"with --except.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillAdd(cmd.Context(), args[0], sel, to, asCopy, fetch)
		},
	}
	cmd.Flags().StringArrayVar(&sel.names, "skill", nil, "the skill to install, by its name in the source; give it again for each")
	cmd.Flags().BoolVar(&sel.all, "all", false, "install every skill the source holds")
	cmd.Flags().StringArrayVar(&sel.except, "except", nil, "with --all, a skill to leave out; give it again for each")
	cmd.Flags().StringArrayVar(&to, "to", nil, "the configuration to place them in, instead of every enabled one")
	cmd.Flags().BoolVar(&asCopy, "copy", false, "place copies instead of symlinks")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch an added source again before installing")
	return cmd
}

// selection is which skills of a source an install takes: those named,
// every one, or every one but those excepted.
type selection struct {
	names  []string
	except []string
	all    bool
}

// skillAdd installs the skills of a source the selection names: it adds or
// fetches the source when it has to, reads their versions out of the
// account repo, writes their import commits through one fast-import, then
// publishes the library directories, the import branches and the
// placements as one journaled mutation, and reads the affected
// configurations again.
//
// A skill that cannot be installed does not cost the others: it is dropped
// with a warning naming it, the rest of the batch lands, and the run
// answers with what failed, the way a fetch of several sources does.
func (inv *invocation) skillAdd(ctx context.Context, arg string, sel selection, to []string, asCopy, fetch bool) error {
	// Flags that contradict each other are refused before anything else, a
	// source this run would add included.
	if err := sel.check(); err != nil {
		return err
	}
	src, entry, err := inv.findSource(arg)
	add := false
	switch {
	case err == nil:
		if src.Ref != "" && src.Ref != entry.Pin {
			return pinMismatch(src, entry)
		}
	case errors.Is(err, errNotAdded) && !source.IsID(arg):
		// A URL the settings do not hold is a source to add, exactly as
		// source add adds it. An id cannot be: it names no URL to fetch.
		add = true
	default:
		return err
	}
	targets, err := inv.placementTargets(to)
	if err != nil {
		return err
	}
	// A source this run adds, or fetches because --fetch asked, is fetched
	// once, outside the lock, and the install reads the listing that fetch
	// built. Its settings entry is a mutation of its own, so an install
	// that fails afterwards keeps the source. --fetch fetches at the pin the
	// settings hold, which the argument either named or left out.
	var listing source.Listing
	if add || fetch {
		at := src
		if !add {
			at.Ref = entry.Pin
		}
		if listing, entry, err = inv.addSource(ctx, at); err != nil {
			return err
		}
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	if !exists {
		return sourceFailure(fmt.Errorf("%w: %s", source.ErrNotFetched, src.URL), src)
	}
	if !add && !fetch {
		// The pin the settings hold decides the commit, resolved in the
		// clone: an install from an added source never reaches for the
		// network to find out what to install, and never reads a working
		// copy. The event says when that clone was fetched, as the one of a
		// fetch does.
		if listing, err = source.List(ctx, inv.git, gitDir, src); err != nil {
			return sourceFailure(err, src)
		}
		n := len(listing.Skills)
		inv.out.emit(sourceEvent{event: newEvent("source"), ID: src.ID(), URL: src.URL, Alias: entry.Alias, Pin: entry.Pin, Subpath: src.Subpath,
			LastFetched: entry.LastFetched, Commit: listing.Commit, Skills: &n})
	}
	skills, err := selectSkills(listing, sel, src)
	if err != nil {
		return err
	}
	// The lock is taken and released before the network: it recovers an
	// unfinished journal and refuses a run that could not finish anyway,
	// while the blobs are fetched outside it, so a slow network never
	// blocks a scan.
	if err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error { return nil }); err != nil {
		return err
	}
	b := newBatch(inv, len(skills))
	versions, err := inv.readVersions(ctx, b, gitDir, src, listing.Commit, skills)
	if err != nil {
		return err
	}
	for _, v := range versions {
		v.fetched = entry.LastFetched
	}
	if len(versions) == 0 {
		return b.abandon()
	}
	run := lineage.NewRun()
	if err := inv.writeImports(ctx, b, gitDir, run, versions); err != nil {
		return err
	}
	dones, journaled, err := inv.install(ctx, b, gitDir, versions, targets, asCopy)
	if err != nil {
		if !journaled {
			// Nothing was written that recovery could need, so the run takes
			// its staging refs with it and leaves the account repo as it was.
			inv.dropImporting(ctx, gitDir, run, len(versions))
		}
		return err
	}
	inv.dropImporting(ctx, gitDir, run, len(versions))
	if len(dones) == 0 {
		return b.abandon()
	}
	if err := inv.reportInstalled(ctx, b, dones); err != nil {
		return err
	}
	if f := b.failure(); f != nil {
		return f
	}
	return nil
}

// batch is one run of agentx skill add: how many steps it planned, how many
// it has reported, what it installed and the skills it had to give up on.
type batch struct {
	refusals
	inv      *invocation
	selected int
	current  int
	total    int
	done     []*installed // the skills that landed, in the order they were installed
}

func newBatch(inv *invocation, selected int) *batch {
	return &batch{
		refusals: refusals{verb: "installed", mixed: "run 'agentx skill list' to see what the library holds, then install the rest one at a time"},
		inv:      inv,
		selected: selected,
		total:    stepsPerSkill*selected + 1,
	}
}

// step reports one finished step of one skill.
func (b *batch) step(phase, subject string) {
	b.current++
	b.inv.progress(phase, subject, b.current, b.total)
}

// drop gives up on one skill: its remaining steps leave the budget, so a
// run that ends still counts up to its total, and the reason is a warning
// naming the skill when there are others it does not apply to.
func (b *batch) drop(subject string, remaining int, f *failure) {
	b.total -= remaining
	b.add(subject, f)
	if b.selected > 1 {
		b.inv.out.warn(subject + ": " + f.message)
	}
}

// abandon answers a run that installed nothing: the rescan it planned never
// happens either.
func (b *batch) abandon() error {
	b.total--
	if f := b.failure(); f != nil {
		return f
	}
	return nil // a typed nil would be an error the caller cannot see through
}

// failure is how a run with broken skills answers, which every run over
// several skills answers the same way; see refusals.
func (b *batch) failure() *failure { return b.refusals.failure(b.selected, len(b.done)) }

// imported is one upstream version read out of the account repo, in
// memory: the skill as the source lists it, its files and the coordinates
// the import commit records.
type imported struct {
	skill   source.Skill // subpath, name, description and tree in the source
	name    string       // the library directory name: the frontmatter name, else the upstream's
	dir     string       // the upstream's own directory name, the one entry of the import tree
	entries []source.TreeEntry
	files   []treeFile
	hash    string   // the content hash of this version
	dropped []string // entries left out of the import: symlinks and submodules
	when    string   // the upstream committer time as "<epoch> +0000"
	fetched string   // when the source was last fetched, as the settings record it
	imp     lineage.Import
	commit  string // the import commit, once written
}

// treeFile is one regular file of the version with its bytes and the mode
// the library directory gets.
type treeFile struct {
	path string
	mode string
	body string
}

func (v *imported) version() lineage.Version {
	return lineage.Version{Import: v.imp, Dir: v.dir, Tree: v.skill.Tree, Entries: v.entries, When: v.when}
}

// readVersions reads every selected skill's tree, each one's upstream
// commit and the objects this machine is missing in one round of
// independent reads, fetches the blobs it does not hold yet in one batch,
// reads them in one cat-file and computes the content hashes in process,
// never laying a version out on disk to do it. Nothing here costs a git
// process per skill: the whole commit is listed once and sliced, and one
// history walk finds the upstream commit of every selected skill, so a
// batch of thirty skills reads what one skill reads.
func (inv *invocation) readVersions(ctx context.Context, b *batch, gitDir string, src source.Source, tip string, skills []source.Skill) ([]*imported, error) {
	trees := make([]string, 0, len(skills))
	seen := map[string]bool{}
	subpaths := make([]string, 0, len(skills))
	for _, sk := range skills {
		if !seen[sk.Tree] {
			seen[sk.Tree] = true
			trees = append(trees, sk.Tree)
		}
		subpaths = append(subpaths, sk.Subpath)
	}
	// Reads of the account repo that do not depend on each other: what the
	// commit holds, which of the selected skills' objects this machine does
	// not have yet, and the upstream commit of each selected skill with its
	// committer time.
	reads := append([][]string{source.TreeArgs(tip), source.MissingArgs(trees...)}, upstreamReads(tip, subpaths)...)
	out, err := inv.git.IsolatedAll(ctx, gitDir, reads)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	listed, err := source.ParseTree(out[0])
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	ups, err := upstreams(tip, subpaths, out[2:])
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	taken := map[string]string{} // library name to the subpath that claimed it
	var kept []*imported
	for _, sk := range skills {
		entries := entriesUnder(listed, sk.Subpath)
		v := &imported{skill: sk, name: sk.Name, dir: upstreamDir(src, sk), entries: entries, when: ups[sk.Subpath].when, dropped: lineage.Dropped(entries)}
		if f := inv.usable(v, taken, src); f != nil {
			b.drop(v.name, stepsPerSkill, f)
			continue
		}
		taken[v.name] = sk.Subpath
		kept = append(kept, v)
	}
	if len(kept) == 0 {
		return nil, nil
	}
	bodies, err := inv.readBlobs(ctx, gitDir, src, kept, source.ParseMissing(out[1]))
	if err != nil {
		return nil, err
	}
	var ready []*imported
	for _, v := range kept {
		if f := v.fill(bodies, src.URL); f != nil {
			b.drop(v.name, stepsPerSkill, f)
			continue
		}
		v.imp = lineage.Import{Source: src.URL, Path: v.skill.Subpath, Commit: ups[v.skill.Subpath].commit, Hash: v.hash}
		for _, d := range v.dropped {
			inv.out.warn(path.Join(v.skill.Subpath, d) + " is not a regular file and is left out of the import")
		}
		ready = append(ready, v)
		b.step(phaseBlobs, v.name)
	}
	return ready, nil
}

// entriesUnder is the skill's own tree entries, taken out of the listing of the
// whole commit and named from the skill directory down, which is what
// listing that directory's tree on its own would have given.
func entriesUnder(listed []source.TreeEntry, subpath string) []source.TreeEntry {
	if subpath == "" {
		return listed
	}
	prefix := subpath + "/"
	var entries []source.TreeEntry
	for _, e := range listed {
		if rel, ok := strings.CutPrefix(e.Path, prefix); ok {
			e.Path = rel
			entries = append(entries, e)
		}
	}
	return entries
}

// upstream is the commit a skill's version is recorded at, with its
// committer time as the import commit's dates take it.
type upstream struct {
	commit string
	when   string
}

// upstreamReads are the reads that find the upstream commit of every
// selected skill: the last commit reachable from tip that touched the
// skill's directory, and tip itself for a skill at the repository root.
// That commit, not the tip, is what the import commit records and takes
// its dates from, so that a commit elsewhere in the source, or a fetch at
// another time, leaves the import commit of an unchanged skill as it was.
func upstreamReads(tip string, subpaths []string) [][]string {
	return upstreamReadsAt(tip, atTip(tip, subpaths))
}

// upstreamReadsAt are the reads that find the upstream commit of each
// subpath as seen from the commit named with it, which is tip or a commit
// reachable from it: the last commit reachable from that commit that
// touched the subpath, and that commit itself for the root.
//
// The subpaths below the root cost one walk of the source's history from
// tip together, however many they are and whichever commits they are seen
// from, and each gets the commit
// "git log -1 --no-renames <commit> -- :(literal)<subpath>" names for it
// alone, under git's default history simplification: a merge that took the
// skill's directory from one of its parents is not a change, and the walk
// follows that parent to the commit that changed it. So which skills a run
// selected never changes the commit of one of them. The root costs one read
// of the commits it is seen from.
func upstreamReadsAt(tip string, at []treeRequest) [][]string {
	var below, roots []string
	seen := map[treeRequest]bool{}
	for _, r := range at {
		// A subpath is walked once whichever commits it is seen from, and
		// the root is read once at each of its commits.
		key := treeRequest{subpath: r.subpath}
		if r.subpath == "" {
			key = treeRequest{commit: r.commit}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if r.subpath == "" {
			roots = append(roots, r.commit)
		} else {
			below = append(below, r.subpath)
		}
	}
	var reads [][]string
	if len(below) > 0 {
		reads = append(reads, historyRead(tip, below))
	}
	if len(roots) > 0 {
		reads = append(reads, append([]string{"log", "--no-walk", "--format=%x00%H %ct"}, roots...))
	}
	return reads
}

// atTip asks for the upstream of every subpath as seen from tip itself.
func atTip(tip string, subpaths []string) []treeRequest {
	at := make([]treeRequest, len(subpaths))
	for i, p := range subpaths {
		at[i] = treeRequest{commit: tip, subpath: p}
	}
	return at
}

// upstreams reads what upstreamReads printed into the upstream of every
// subpath, and fails for a subpath no commit of the walk touched.
func upstreams(tip string, subpaths []string, out []string) (map[string]upstream, error) {
	found, err := foundUpstreams(tip, atTip(tip, subpaths), out)
	if err != nil {
		return nil, err
	}
	ups := make(map[string]upstream, len(subpaths))
	for _, p := range subpaths {
		up, ok := found[treeRequest{commit: tip, subpath: p}]
		if !ok {
			return nil, fmt.Errorf("no commit of %s touches %s", short(tip), p)
		}
		ups[p] = up
	}
	return ups, nil
}

// foundUpstreams reads what upstreamReadsAt printed into the upstream of
// every request it can: git's last change of the subpath replayed on the
// walk from the request's commit, and that commit for the root. A subpath
// whose tree holds no entry at all, which no commit ever lists, is left out.
func foundUpstreams(tip string, at []treeRequest, out []string) (map[treeRequest]upstream, error) {
	found := make(map[treeRequest]upstream, len(at))
	add := func(r treeRequest, commit, epoch string) error {
		when, err := lineage.UpstreamDate(epoch)
		if err != nil {
			return err
		}
		found[r] = upstream{commit: commit, when: when}
		return nil
	}
	var below []string
	root := false
	for _, r := range at {
		if r.subpath == "" {
			root = true
		} else {
			below = append(below, r.subpath)
		}
	}
	if len(below) > 0 {
		h, err := parseHistory(out[0], below)
		if err != nil {
			return nil, err
		}
		for _, r := range at {
			if r.subpath == "" {
				continue
			}
			if commit, ok := h.lastChange(r.commit, r.subpath); ok {
				if err := add(r, commit, h.commits[commit].epoch); err != nil {
					return nil, err
				}
			}
		}
		out = out[1:]
	}
	if root {
		epochs := map[string]string{}
		for _, record := range strings.Split(out[0], "\x00") {
			if commit, epoch, ok := strings.Cut(strings.TrimSpace(record), " "); ok {
				epochs[commit] = epoch
			}
		}
		for _, r := range at {
			if r.subpath != "" {
				continue
			}
			epoch, ok := epochs[r.commit]
			if !ok {
				return nil, fmt.Errorf("the account repo does not date the commit %s", r.commit)
			}
			if err := add(r, r.commit, epoch); err != nil {
				return nil, err
			}
		}
	}
	return found, nil
}

// usable refuses a skill the machine cannot hold before anything is read
// for it: a name the library or the account repo cannot take, an entry
// whose path would lay it out outside the skill's directory, a directory
// with no regular file in it at all, and a name another skill of this same
// batch has already claimed.
func (inv *invocation) usable(v *imported, taken map[string]string, src source.Source) *failure {
	// Every path is checked before any of it is read or written: an entry
	// is laid out below a staging directory by its path, and one that
	// climbs out of it would be written wherever it points.
	for _, e := range v.entries {
		if err := source.CheckPath(e.Path); err != nil {
			return refuse(exitRefused, fmt.Sprintf("the skill %q in %s%s holds an entry agentx will not lay out: %q", v.skill.Name, src.URL, underPath(v.skill.Subpath), e.Path),
				"an empty, absolute, '.', '..' or '.git' path, a backslash or a NUL could be written outside the skill's directory; install another skill of the source")
		}
	}
	if why := nameRefusal(v.name); why != "" {
		return refuse(exitRefused, fmt.Sprintf("%q %s", v.name, why),
			"a name cannot be empty or hidden, or carry a separator, a space or any of ~^:?*[; fix it in the skill's SKILL.md frontmatter upstream, or install another skill")
	}
	if where, ok := taken[v.name]; ok {
		return refuse(exitRefused, fmt.Sprintf("%s is also the name of the skill under %s, which this run installs", v.name, where),
			"install one of the two, or fork one of them under another name")
	}
	// The same predicate the import tree is built on, so that the run
	// refuses such a skill here, where it costs only itself, rather than in
	// writeTrees, where it would cost the whole batch.
	if lineage.HasFileToImport(v.entries) {
		return nil
	}
	return refuse(exitRefused, v.name+" has no regular file to import",
		"the skill directory holds only symlinks or submodules, which an import leaves out")
}

// fill puts the file bodies of one version in place and computes its
// content hash, over the same byte sequence a scan hashes a directory with,
// so that the library directory and the hash agree once it is written.
func (v *imported) fill(bodies map[string]string, url string) *failure {
	var files []scan.File
	for _, e := range v.entries {
		if !source.IsFileMode(e.Mode) {
			continue
		}
		body, ok := bodies[e.OID]
		if !ok {
			// The same answer a listing gives for a source the account repo
			// holds incompletely, and for this skill alone: another skill of
			// the run whose blobs did arrive still installs.
			return refuse(exitNotFound, fmt.Sprintf("%s holds a file the source did not serve: %s", v.name, e.Path),
				"run 'agentx source fetch "+url+"' to fetch it again")
		}
		v.files = append(v.files, treeFile{path: e.Path, mode: e.Mode, body: body})
		files = append(files, scan.File{Path: e.Path, Content: body})
	}
	// The name and description the hash covers are the frontmatter's own,
	// empty when it has none; the directory name stands in for a missing
	// name afterwards, as it does in a scan, and never enters the hash.
	var name, description string
	for _, f := range v.files {
		if f.path == "SKILL.md" {
			name, description, _ = scan.SkillFrontmatter(f.body)
		}
	}
	v.hash = scan.ContentHash(name, description, files)
	return nil
}

// readBlobs reads the file contents of every selected version from the
// account repo, which holds the trees of a source but only its SKILL.md
// blobs, fetching in one batch the blobs of this whole run that are not
// here yet. A version whose blobs are all here, which is how the same
// version installed twice looks, costs no network at all.
func (inv *invocation) readBlobs(ctx context.Context, gitDir string, src source.Source, versions []*imported, absent []string) (map[string]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, v := range versions {
		for _, e := range v.entries {
			if source.IsFileMode(e.Mode) && !seen[e.OID] {
				seen[e.OID] = true
				ids = append(ids, e.OID)
			}
		}
	}
	var missing []string
	for _, id := range absent {
		if seen[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		if err := source.FetchObjects(ctx, inv.git, gitDir, src, missing); err != nil {
			return nil, sourceFailure(err, src)
		}
	}
	bodies, err := source.ReadBlobs(ctx, inv.git, gitDir, ids)
	if err != nil {
		// A blob the source did not serve is not reported as missing: the
		// account repo is a partial clone, so cat-file dies on the first
		// object it cannot get rather than printing a "missing" line for
		// it. Which ones they are is asked of rev-list, which walks what is
		// here and never reaches for the network, and the rest are read
		// again without them, so that a version whose blobs did not arrive
		// costs its own skill and not the run. Both reads happen only on
		// this path: a run whose blobs all arrive still costs one cat-file.
		kept := inv.present(ctx, gitDir, versions, ids)
		if kept == nil {
			return nil, sourceFailure(err, src)
		}
		if bodies, err = source.ReadBlobs(ctx, inv.git, gitDir, kept); err != nil {
			return nil, sourceFailure(err, src)
		}
	}
	return bodies, nil
}

// present are the ids of versions the account repo really holds, or nil
// when rev-list finds nothing missing and so does not explain the read that
// failed.
func (inv *invocation) present(ctx context.Context, gitDir string, versions []*imported, ids []string) []string {
	trees := make([]string, 0, len(versions))
	seen := map[string]bool{}
	for _, v := range versions {
		if !seen[v.skill.Tree] {
			seen[v.skill.Tree] = true
			trees = append(trees, v.skill.Tree)
		}
	}
	out, err := inv.git.Isolated(ctx, gitDir, source.MissingArgs(trees...)...)
	if err != nil {
		return nil
	}
	gone := map[string]bool{}
	for _, id := range source.ParseMissing(out) {
		gone[id] = true
	}
	if len(gone) == 0 {
		return nil
	}
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if !gone[id] {
			kept = append(kept, id)
		}
	}
	return kept
}

// writeImports writes the import commit of every version, all of them
// through one fast-import onto this run's staging refs, and reports the
// import step of each skill.
func (inv *invocation) writeImports(ctx context.Context, b *batch, gitDir, run string, versions []*imported) error {
	list := make([]lineage.Version, len(versions))
	for i, v := range versions {
		list[i] = v.version()
	}
	commits, err := lineage.WriteAll(ctx, inv.git, gitDir, run, list)
	if err != nil {
		return accountRepoFailure(err)
	}
	for i, v := range versions {
		v.commit = commits[i]
		b.step(phaseImport, v.name)
	}
	return nil
}

// dropImporting takes this run's staging refs away again. It is cleanup:
// the commits they held are on the import branches by now, and a failure to
// remove them costs nothing but a ref no command reads.
func (inv *invocation) dropImporting(ctx context.Context, gitDir, run string, n int) {
	if err := lineage.DropImporting(ctx, inv.git, gitDir, run, n); err != nil {
		inv.out.debugf("the staging refs of this import stay behind: %v", err)
	}
}

// upstreamDir is the directory name the import tree holds the version
// under: the last segment of the subpath, and the repository's name for a
// skill at the root, which is what the source listing calls it too. It is
// not the library directory name, which the frontmatter decides.
func upstreamDir(src source.Source, sk source.Skill) string {
	if sk.Subpath == "" {
		return source.RepoName(src.URL)
	}
	return path.Base(sk.Subpath)
}

// check refuses a selection whose flags contradict each other. The two
// forms do not mix: --all already names everything, and excepting from a
// list one wrote out is a contradiction.
func (sel selection) check() error {
	switch {
	case sel.all && len(sel.names) > 0:
		return fail(exitUsage, "--all and --skill cannot both be given", "--all installs every skill; drop it to install the ones you name")
	case !sel.all && len(sel.except) > 0:
		return fail(exitUsage, "--except needs --all", "name the skills you want with --skill, or take the rest with --all --except")
	}
	return nil
}

// selectSkills picks the skills of the listing to install, in the order the
// listing holds them, which is by subpath: a source or a path that holds
// one skill needs no --skill, --skill names one and may be given again for
// each, and --all takes every one with --except leaving some out. The
// selection has passed check already.
func selectSkills(listing source.Listing, sel selection, src source.Source) ([]source.Skill, error) {
	switch {
	case len(listing.Skills) == 0:
		return nil, fail(exitNotFound, "no skill in "+src.URL+underPath(src.Subpath), "run 'agentx source skills "+src.URL+"' to see what it holds")
	case sel.all:
		return everySkillBut(listing, sel.except, src)
	case len(sel.names) > 0:
		return namedSkills(listing, sel.names, src)
	case len(listing.Skills) == 1:
		return listing.Skills, nil
	}
	return nil, fail(exitUsage, fmt.Sprintf("%s%s holds %s", src.URL, underPath(src.Subpath), plural(len(listing.Skills), "skill")), skillNamesHint(listing))
}

// namedSkills resolves the --skill names against the listing, keeping the
// listing's order. A name matches a skill's frontmatter name, or its
// directory name when the frontmatter has none, case-insensitively. A name
// given twice installs one skill, and a name two directories of the source
// share resolves to the first of them, which is what naming a skill has
// always meant here.
func namedSkills(listing source.Listing, names []string, src source.Source) ([]source.Skill, error) {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[strings.ToLower(name)] = true
	}
	found := map[string]bool{}
	var skills []source.Skill
	for _, sk := range listing.Skills {
		key := strings.ToLower(sk.Name)
		if wanted[key] && !found[key] {
			found[key] = true
			skills = append(skills, sk)
		}
	}
	if missing := notFound(names, found); len(missing) > 0 {
		return nil, fail(exitNotFound, fmt.Sprintf("%s has no skill called %s", src.URL, quoted(missing)), skillNamesHint(listing))
	}
	return skills, nil
}

// everySkillBut is --all: every skill of the listing, less those --except
// names, matched as namedSkills matches them. An --except that names no
// skill of the source is a mistake worth saying so about, since the run
// would otherwise install more than asked.
func everySkillBut(listing source.Listing, except []string, src source.Source) ([]source.Skill, error) {
	unwanted := map[string]bool{}
	for _, name := range except {
		unwanted[strings.ToLower(name)] = true
	}
	found := map[string]bool{}
	var skills []source.Skill
	for _, sk := range listing.Skills {
		if key := strings.ToLower(sk.Name); unwanted[key] {
			found[key] = true // every skill of that name, not the first
			continue
		}
		skills = append(skills, sk)
	}
	if missing := notFound(except, found); len(missing) > 0 {
		return nil, fail(exitNotFound, fmt.Sprintf("%s has no skill called %s", src.URL, quoted(missing)), skillNamesHint(listing))
	}
	if len(skills) == 0 {
		return nil, fail(exitUsage, "--except left no skill to install", "drop one of the --except names")
	}
	return skills, nil
}

// notFound keeps the names the listing did not hold, in the order they were
// given on the command line and without repeating one given twice in any
// case. found is keyed by the lowercased name.
func notFound(given []string, found map[string]bool) []string {
	var names []string
	seen := map[string]bool{}
	for _, name := range given {
		if key := strings.ToLower(name); !found[key] && !seen[key] {
			seen[key] = true
			names = append(names, name)
		}
	}
	return names
}

func quoted(names []string) string {
	q := make([]string, len(names))
	for i, name := range names {
		q[i] = fmt.Sprintf("%q", name)
	}
	return strings.Join(q, ", ")
}

func underPath(subpath string) string {
	if subpath == "" {
		return ""
	}
	return " under " + subpath
}

func skillNamesHint(listing source.Listing) string {
	names := make([]string, 0, len(listing.Skills))
	for _, sk := range listing.Skills {
		names = append(names, sk.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "run 'agentx source skills' to see what the source holds"
	}
	if len(names) > namesInAHint {
		names = append(names[:namesInAHint], "...")
	}
	return "name one with --skill, or take them all with --all: " + strings.Join(names, ", ")
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// installed is what the mutation did to one skill, for the report that
// follows it: the version, whether the library already held it, and the
// placements the install made, which are the placements any command makes.
type installed struct {
	v       *imported
	adopted bool // the library already held this version
	placements
}

// install publishes the library directories, the import branches and the
// placements of the whole batch as one journaled mutation under the
// exclusive lock: thirty skills are one journal and not thirty, so an
// interrupted run is finished rather than half undone. Every input is read
// again under the lock before anything live changes, and nothing live
// changes before the journal that describes all of it is on disk.
//
// journaled says whether the journal was written, which is what decides
// whether the caller may take the staging refs of the import away: once a
// journal exists, recovery needs the commits they hold.
func (inv *invocation) install(ctx context.Context, b *batch, gitDir string, versions []*imported, targets []placeTarget, asCopy bool) (dones []*installed, journaled bool, err error) {
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		if err := os.MkdirAll(inv.dirs.Library, 0o755); err != nil {
			return libraryFailure(inv.dirs.Library, err)
		}
		sweepStaged(inv.dirs.Library)
		for _, t := range targets {
			if !t.readsLibrary {
				sweepStaged(t.dir)
			}
		}
		records, err := lineage.List(ctx, inv.git, gitDir)
		if err != nil {
			return accountRepoFailure(err)
		}
		// The copy modes the settings record decide what each placement is,
		// so they are read before any is planned, and the run's own copies
		// go into the same write.
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		m := home.NewMutation(inv.dirs.Home)
		for _, v := range versions {
			done, f, err := inv.stageSkill(m, gitDir, v, records, targets, asCopy, edit.copiesOf(v.name))
			switch {
			case err != nil:
				m.Discard()
				return err
			case f != nil:
				b.drop(v.name, stepsPerSkill-2, f) // the blobs and the import already ended
				continue
			}
			dones = append(dones, done)
		}
		if len(dones) == 0 {
			m.Discard()                // nothing live changed and no journal exists
			return errNothingInstalled // and nothing changed, so nothing is signalled either
		}
		if err := inv.stageCopyMode(m, edit, dones); err != nil {
			m.Discard()
			return err
		}
		// After the apply, not before it: a journal that could not be
		// written leaves nothing for recovery to need, and calling it
		// journaled would keep this run's staging refs in the account repo
		// for good.
		applied := m.Apply(inv.refs(ctx))
		journaled = m.Journaled()
		return applied
	})
	if errors.Is(err, errNothingInstalled) {
		return nil, false, nil // every skill was refused; the caller answers for them
	}
	if err != nil {
		return nil, journaled, mutationFailure(err)
	}
	b.done = dones
	return dones, journaled, nil
}

// errNothingInstalled ends the mutation of a run whose every skill was
// refused, so that the lock is released without the version file being
// rewritten: nothing changed, and nothing watching agentx home has anything
// to read again.
var errNothingInstalled = errors.New("no skill of the run could be installed")

// stageSkill records everything one skill of the batch does. What can be
// refused is decided first, from the live state and the branches the
// account repo holds, so that a skill the run gives up on leaves no step of
// its own in the journal: a branch without its library directory would be a
// version this machine claims to hold and does not.
func (inv *invocation) stageSkill(m *home.Mutation, gitDir string, v *imported, records map[string]lineage.Record, targets []placeTarget, asCopy bool, recordedCopies []string) (*installed, *failure, error) {
	libPath := inv.libraryPath(v.name)
	state, err := home.State(libPath)
	if err != nil {
		return nil, nil, libraryFailure(inv.dirs.Library, err)
	}
	lib, f := inv.libraryPlan(v, libPath, state)
	if f != nil {
		return nil, f, nil
	}
	ref, f := refPlan(v, records, libPath)
	if f != nil {
		return nil, f, nil
	}
	done := &installed{v: v, adopted: lib.adopt}
	if ref {
		m.Ref(gitDir, lineage.ManagedRef(v.name), "", v.commit)
	}
	if !lib.adopt {
		if lib.displace {
			m.Remove(libPath, state)
		}
		staged := m.Sibling(libPath, "staged")
		if err := inv.stageCopy(staged, v.placeable()); err != nil {
			return nil, nil, libraryFailure(inv.dirs.Library, err)
		}
		fingerprint, err := home.Fingerprint(staged)
		if err != nil {
			return nil, nil, libraryFailure(inv.dirs.Library, err)
		}
		m.Publish(libPath, staged, fingerprint)
	}
	for _, t := range targets {
		// A placement this machine cannot make is skipped and counted, not
		// a failure of the skill: stagePlacement decides that itself.
		inv.stagePlacement(m, v.placeable(), t, libPath, asCopy, recordedCopies, &done.placements)
	}
	return done, nil, nil
}

// libraryAction is what the library does with one version: hold it for the
// first time, hold it after taking a dangling link away, or adopt what is
// already there.
type libraryAction struct {
	adopt    bool
	displace bool
}

// libraryPlan decides what the library does with the version: hold it for

// libraryFailure is the refusal of a library this machine cannot use. The
// library is not optional, as a placement is: without it there is no skill
// to place, so this ends the install rather than being skipped. It is an
// unmet precondition and not an internal error, and doctor's library row
// says the same thing in more detail.
func libraryFailure(library string, err error) error {
	return fail(exitRefused, err.Error(), "run 'agentx doctor' and check the library it names, or make "+library+" writable")
}

// libraryPlan decides what the library does with the version: hold it for
// the first time, adopt a directory that already holds exactly it, or
// refuse a directory that holds something else. A dangling symlink into the
// agentx worktrees directory counts as absent: it is a fork's placement
// whose worktree is gone.
func (inv *invocation) libraryPlan(v *imported, libPath, state string) (libraryAction, *failure) {
	switch {
	case home.IsAbsent(state):
		return libraryAction{}, nil
	case home.IsDangling(libPath) && inv.intoWorktrees(libPath):
		return libraryAction{displace: true}, nil
	case contentHashAt(libPath) == v.hash:
		return libraryAction{adopt: true}, nil // write the branch, copy nothing
	}
	return libraryAction{}, refuse(exitRefused, fmt.Sprintf("the library already holds %s at %s", v.name, inv.dirs.Library),
		"remove "+libPath+" and install again, or install the skill under another name by forking it")
}

// refPlan decides the import branch, which is created with an expected old
// value of empty, so that two commands cannot both claim the name. A branch
// already at this commit is the same version installed again; one at
// another commit is a version this command does not replace.
func refPlan(v *imported, records map[string]lineage.Record, libPath string) (create bool, f *failure) {
	rec, ok := records[v.name]
	switch {
	case !ok:
		return true, nil
	case rec.Kind == lineage.KindFork:
		return false, refuse(exitRefused, fmt.Sprintf("%s is a fork on this machine", v.name),
			"install the skill under another name, or remove the fork first")
	case rec.Commit == v.commit: // the same version again: nothing to move
		return false, nil
	}
	return false, refuse(exitRefused, fmt.Sprintf("%s is already managed at another version", v.name),
		"remove "+libPath+" and the branch "+lineage.ManagedRef(v.name)+", then install again")
}

// stageCopyMode records in the machine settings which configurations hold a
// copy of which skill, in one write for the whole run however many skills
// it installed.
func (inv *invocation) stageCopyMode(m *home.Mutation, edit *settingsEdit, dones []*installed) error {
	for _, done := range dones {
		edit.addCopies(done.v.name, done.copies)
	}
	return edit.stage(m, inv.dirs.Home)
}

// placeable is the version an install places: its library name, its content
// hash and the files it imported, which is what a copy placement of this
// run is laid out from. The library directory is published by the same
// mutation, so it is not there to copy from yet.
func (v *imported) placeable() placeable {
	return placeable{name: v.name, hash: v.hash, stage: v.writeFiles}
}

// writeFiles lays the imported files out under dest with the modes the
// library directory gets.
func (v *imported) writeFiles(dest string) error {
	for _, f := range v.files {
		full := filepath.Join(dest, filepath.FromSlash(f.path))
		// usable refused such a path already; nothing is written
		// outside the staging directory whatever reaches this far.
		if rel, err := filepath.Rel(dest, full); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%q is not a path inside %s", f.path, dest)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.mode == source.ExecutableMode {
			mode = 0o755
		}
		if err := writeSynced(full, []byte(f.body), mode); err != nil {
			return err
		}
	}
	return nil
}

// writeSynced writes a file of staged content and flushes it, so that the
// rename that publishes it cannot leave an empty file behind.
func writeSynced(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// sweepStaged removes the staging directories of an install that stopped
// before its journal existed. It runs under the exclusive lock and after
// recovery, so nothing that is still wanted is there. Content a removal
// retained is never swept: it is the user's, and recovery keeps it when it
// could not be given back.
func sweepStaged(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentx-staged-") {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// contentHashAt is the content hash of the skill at path, empty when there
// is no skill there.
func contentHashAt(path string) string {
	if info, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	hash, _ := scan.ContentHashAt(path)
	return hash
}

// intoWorktrees reports whether the symlink at path points into the agentx
// worktrees directory, which is where a fork's placement points.
func (inv *invocation) intoWorktrees(path string) bool {
	link, err := os.Readlink(path)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(path), link)
	}
	worktrees := filepath.Join(inv.dirs.Home, "worktrees")
	return strings.HasPrefix(filepath.Clean(link), worktrees+string(filepath.Separator))
}

// mutationFailure keeps the refusals of a mutating skill command as they
// are and maps a journal that could not be finished to the recovery exit
// code. Installing, placing and removing all answer through it.
func mutationFailure(err error) error {
	var f *failure
	switch {
	case errors.As(err, &f):
		return err
	case errors.Is(err, home.ErrRecovery), errors.Is(err, home.ErrLocked):
		return err
	case errors.Is(err, gitx.ErrAccountRepo):
		return accountRepoFailure(err)
	}
	return fail(exitInternal, err.Error(), "run 'agentx doctor' and check the library and the account repo it names")
}

// progress reports one finished step of the run.
func (inv *invocation) progress(phase, subject string, current, total int) {
	inv.out.emit(progressEvent{event: newEvent("progress"), Phase: phase, Subject: subject, Current: current, Total: total})
}

// reportInstalled reads the affected configurations again and reports every
// skill as the machine now has it: the rescan is what the placements in the
// events are read from, so the report says what is there rather than what
// the install meant to do. One rescan covers the whole batch.
func (inv *invocation) reportInstalled(ctx context.Context, b *batch, dones []*installed) error {
	for _, done := range dones {
		b.step(phaseInstall, done.v.name)
	}
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	b.step(phaseRescan, "")
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	modes, err := s.CopyModes()
	if err != nil {
		modes = map[string][]string{}
	}
	// The library is read once for the whole run. Reading it content-hashes
	// every directory it holds, so reading it per installed skill costs a
	// batch of n skills n hashes of the whole library: a run of forty was
	// measured a third slower than one read.
	library := librarySkills(inv.dirs.Library)
	for _, done := range dones {
		lib, found := library[done.v.name]
		if !found {
			return fail(exitInternal, "the library holds no "+done.v.name+" after installing it", "run 'agentx doctor' and check the library it names")
		}
		places := inv.placements(snap, lib, modes)
		rec := lineage.Record{Name: done.v.name, Kind: lineage.KindManaged, Ref: lineage.ManagedRef(done.v.name), Commit: done.v.commit, Import: done.v.imp, HasImport: true}
		ev := skillFromLibrary(lib, rec, true, filterPlacements(places, targetIDs(done.placed)))
		inv.out.emit(ev)
		inv.printInstalled(done, ev)
	}
	inv.summary = installSummary(dones)
	return nil
}

// installSummary is what the result event says the run did: the skills,
// where they came from, how many configurations now see them and what was
// left alone. One skill reads as the one install it is; several are named
// together, since one line stands for the whole run.
func installSummary(dones []*installed) string {
	first := dones[0].v
	copies, adoptions, skipped, adopted := 0, 0, 0, 0
	names := make([]string, 0, len(dones))
	covered := map[string]bool{} // the configurations that see any of them
	for _, done := range dones {
		names = append(names, done.v.name)
		for _, t := range done.placed {
			covered[t.id] = true
		}
		copies += len(done.copies)
		adoptions += len(done.adoptions)
		skipped += len(done.skipped)
		if done.adopted {
			adopted++
		}
	}
	summary := fmt.Sprintf("installed %s from %s%s in %s", strings.Join(names, ", "), first.imp.Source, underPath(first.imp.Path), plural(len(covered), "configuration"))
	if len(dones) > 1 {
		summary = fmt.Sprintf("installed %s from %s in %s", strings.Join(names, ", "), first.imp.Source, plural(len(covered), "configuration"))
	}
	if len(dones) == 1 && adopted == 1 {
		summary = strings.Replace(summary, "installed ", "adopted ", 1)
	} else if adopted > 0 {
		summary += fmt.Sprintf(", %d already in the library", adopted)
	}
	for _, what := range []struct {
		n    int
		text string
	}{{copies, "as " + modeCopy}, {adoptions, "adopted"}, {skipped, "skipped"}} {
		if what.n > 0 {
			summary += fmt.Sprintf(", %s %s", plural(what.n, "placement"), what.text)
		}
	}
	return summary + fetchedAt(first)
}

// fetchedAt says when the source the version was read from was last
// fetched, so that an install from an old fetch says so.
func fetchedAt(v *imported) string {
	if v.fetched == "" {
		return ""
	}
	return ", source fetched " + v.fetched
}

// librarySkills reads the library once, by name. One read covers a whole
// run: reading it names every directory and content-hashes each one, which
// is work that belongs to the library and not to the skills a run installed.
func librarySkills(library string) map[string]scan.LibrarySkill {
	skills, _ := readLibrary(library)
	byName := make(map[string]scan.LibrarySkill, len(skills))
	for _, s := range skills {
		byName[s.Name] = s
	}
	return byName
}

// librarySkill reads one directory of the library, for the commands that
// work on a single name and have no batch to amortise a whole read over.
func librarySkill(library, name string) (scan.LibrarySkill, bool) {
	skills, _ := readLibrary(library)
	for _, s := range skills {
		if s.Name == name {
			return s, true
		}
	}
	return scan.LibrarySkill{}, false
}

// printInstalled writes the confirmation of one skill and one row per
// placement.
func (inv *invocation) printInstalled(done *installed, ev librarySkillEvent) {
	out, v := inv.out, done.v
	what := "installed"
	if done.adopted {
		what = "adopted"
	}
	line := what + " " + out.paint(heading, v.name) + " from " + out.paint(heading, v.imp.Source) + underPath(v.imp.Path) +
		" at " + short(v.imp.Commit) + out.paint(muted, fetchedAt(v)) + ": " + out.paint(noteStyle, plural(len(ev.Placements), "placement"))
	if n := len(done.skipped); n > 0 {
		line += ", " + out.paint(warnStyle, plural(n, "placement")+" skipped")
	}
	out.done(line)
	inv.printPlacementRows(v.name, ev.Placements)
	if len(done.adoptions) > 0 {
		out.print("  ", out.paint(muted, "adopted "+strings.Join(done.adoptions, ", ")))
	}
}
