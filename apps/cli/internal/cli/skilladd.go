package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

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

func newSkillAddCommand(inv *invocation) *cobra.Command {
	var names, to []string
	var asCopy, fetch bool
	cmd := &cobra.Command{
		Use:   "add <source>[/<subpath>]",
		Short: "Install a skill from a source into the library and place it",
		Long: "Install a skill from a source into the library and place it in every enabled\n" +
			"configuration. A source URL this machine has not added yet is added first, as\n" +
			"'agentx source add' would; an added source is installed from as it was last\n" +
			"fetched, unless --fetch fetches it again. Name the skill with --skill when the\n" +
			"source or the path holds more than one.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillAdd(cmd.Context(), args[0], names, to, asCopy, fetch)
		},
	}
	cmd.Flags().StringArrayVar(&names, "skill", nil, "the skill to install, by its name in the source")
	cmd.Flags().StringArrayVar(&to, "to", nil, "the configuration to place it in, instead of every enabled one")
	cmd.Flags().BoolVar(&asCopy, "copy", false, "place copies instead of symlinks")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "fetch an added source again before installing")
	return cmd
}

// skillAdd installs one skill of a source: it adds or fetches the source
// when it has to, reads the version out of the account repo, writes its
// import commit, then publishes the library directory, the import branch
// and the placements as one journaled mutation, and reads the affected
// configurations again.
func (inv *invocation) skillAdd(ctx context.Context, arg string, names, to []string, asCopy, fetch bool) error {
	// A selection the command cannot take is refused before anything else,
	// a source this run would add included.
	if err := checkSkillNames(names); err != nil {
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
	sk, err := selectSkill(listing, names, src)
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
	total := stepsPerSkill + 1
	v, err := inv.readVersion(ctx, gitDir, src, listing.Commit, sk)
	if err != nil {
		return err
	}
	v.fetched = entry.LastFetched
	inv.progress(phaseBlobs, v.name, 1, total)
	if v.commit, err = lineage.Write(ctx, inv.git, gitDir, v.version()); err != nil {
		return accountRepoFailure(err)
	}
	inv.progress(phaseImport, v.name, 2, total)
	done, err := inv.install(ctx, gitDir, v, targets, asCopy)
	if err != nil {
		return err
	}
	inv.progress(phaseInstall, v.name, 3, total)
	return inv.reportInstalled(ctx, v, done, total)
}

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

// readVersion reads the skill's tree and its upstream commit in one round
// of independent reads, fetches the blobs it does not hold yet in one batch
// and computes the content hash in process, never laying the version out on
// disk to do it.
func (inv *invocation) readVersion(ctx context.Context, gitDir string, src source.Source, tip string, sk source.Skill) (*imported, error) {
	// Three reads of the account repo that do not depend on each other: what
	// the skill's tree holds, the upstream commit with its committer time,
	// and which of those objects this machine does not have yet.
	out, err := inv.git.IsolatedAll(ctx, gitDir, [][]string{
		source.TreeArgs(sk.Tree),
		upstreamArgs(tip, sk.Subpath),
		source.MissingArgs(sk.Tree),
	})
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	entries, err := source.ParseTree(out[0])
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	// Every path is checked before any of it is read or written: an entry
	// is laid out below a staging directory by its path, and one that
	// climbs out of it would be written wherever it points.
	for _, e := range entries {
		if err := source.CheckPath(e.Path); err != nil {
			return nil, fail(exitRefused, fmt.Sprintf("the skill %q in %s%s holds an entry agentx will not lay out: %q", sk.Name, src.URL, underPath(sk.Subpath), e.Path),
				"an empty, absolute, '.', '..' or '.git' path, a backslash or a NUL could be written outside the skill's directory; install another skill of the source")
		}
	}
	commit, epoch, _ := strings.Cut(strings.TrimSpace(out[1]), " ")
	if commit == "" {
		return nil, accountRepoFailure(fmt.Errorf("no commit of %s touches %s", short(tip), sk.Subpath))
	}
	when, err := lineage.UpstreamDate(epoch)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	v := &imported{skill: sk, name: sk.Name, dir: upstreamDir(src, sk), entries: entries, when: when, dropped: lineage.Dropped(entries)}
	if !usableName(v.name) {
		return nil, fail(exitRefused, fmt.Sprintf("%q is not a name agentx can hold as a library directory and an import branch", v.name),
			"a name cannot be empty or hidden, or carry a separator, a space or any of ~^:?*[; fix it in the skill's SKILL.md frontmatter upstream, or install another skill")
	}
	bodies, err := inv.readBlobs(ctx, gitDir, src, entries, source.ParseMissing(out[2]))
	if err != nil {
		return nil, err
	}
	var files []scan.File
	for _, e := range entries {
		if !source.IsFileMode(e.Mode) {
			continue
		}
		v.files = append(v.files, treeFile{path: e.Path, mode: e.Mode, body: bodies[e.OID]})
		files = append(files, scan.File{Path: e.Path, Content: bodies[e.OID]})
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
	v.imp = lineage.Import{Source: src.URL, Path: sk.Subpath, Commit: commit, Hash: v.hash}
	for _, d := range v.dropped {
		inv.out.warn(path.Join(sk.Subpath, d) + " is not a regular file and is left out of the import")
	}
	return v, nil
}

// upstreamArgs are the arguments of the read that finds a skill's upstream
// commit: the last commit reachable from tip that touched the skill's
// directory, and the tip itself for a skill at the repository root, with
// its committer time. That commit, not the tip, is what the import commit
// records and takes its dates from, so that a commit elsewhere in the
// source, or a fetch at another time, leaves the import commit of an
// unchanged skill as it was. The walk compares trees alone: renames are not
// followed, since that would read blobs a blobless clone does not hold, and
// the subpath, which the source names, is matched literally rather than as
// a pattern.
func upstreamArgs(tip, subpath string) []string {
	args := []string{"log", "-1", "--no-renames", "--format=%H %ct", tip}
	if subpath != "" {
		args = append(args, "--", ":(literal)"+subpath)
	}
	return args
}

// readBlobs reads the version's file contents from the account repo, which
// holds the trees of a source but only its SKILL.md blobs, fetching in one
// batch the blobs of this skill that are not here yet. A version whose
// blobs are all here, which is how the same version installed twice looks,
// costs no network at all.
func (inv *invocation) readBlobs(ctx context.Context, gitDir string, src source.Source, entries []source.TreeEntry, absent []string) (map[string]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, e := range entries {
		if source.IsFileMode(e.Mode) && !seen[e.OID] {
			seen[e.OID] = true
			ids = append(ids, e.OID)
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
		return nil, sourceFailure(err, src)
	}
	for _, id := range ids {
		if _, ok := bodies[id]; !ok {
			return nil, sourceFailure(fmt.Errorf("%w: the source did not serve %s", source.ErrIncomplete, id), src)
		}
	}
	return bodies, nil
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

// checkSkillNames refuses more than one --skill: installing several at
// once is what the bulk flags are for.
func checkSkillNames(names []string) error {
	if len(names) > 1 {
		return fail(exitUsage, "agentx skill add installs one skill at a time", "give one --skill and run it again for the next")
	}
	return nil
}

// selectSkill picks the one skill of the listing to install. A source or a
// path that holds one skill needs no --skill; anything else has to name it.
// A name matches a skill's frontmatter name, or its directory name when the
// frontmatter has none, case-insensitively, and the first in listing order
// when two share it. The names have passed checkSkillNames already.
func selectSkill(listing source.Listing, names []string, src source.Source) (source.Skill, error) {
	switch {
	case len(names) == 1:
		for _, sk := range listing.Skills {
			if strings.EqualFold(sk.Name, names[0]) {
				return sk, nil
			}
		}
		return source.Skill{}, fail(exitNotFound, fmt.Sprintf("%s has no skill called %q", src.URL, names[0]), skillNamesHint(listing))
	case len(listing.Skills) == 1:
		return listing.Skills[0], nil
	case len(listing.Skills) == 0:
		return source.Skill{}, fail(exitNotFound, "no skill in "+src.URL+underPath(src.Subpath), "run 'agentx source skills "+src.URL+"' to see what it holds")
	}
	return source.Skill{}, fail(exitUsage, fmt.Sprintf("%s%s holds %s", src.URL, underPath(src.Subpath), plural(len(listing.Skills), "skill")), skillNamesHint(listing))
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
	if len(names) > 12 {
		names = append(names[:12], "...")
	}
	return "name one with --skill: " + strings.Join(names, ", ")
}

// placeTarget is one configuration an install places the skill in.
type placeTarget struct {
	id           string
	dir          string // the client's own skills directory
	readsLibrary bool   // the library entry is the placement; a symlink would list the skill twice
}

// placementTargets are the configurations the install places into: every
// enabled one, or exactly those --to names, whatever their enabled state,
// since naming one is asking for it.
func (inv *invocation) placementTargets(to []string) ([]placeTarget, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	detected := scan.Detect(inv.dirs)
	var targets []placeTarget
	for _, c := range detected {
		t := placeTarget{id: c.Slug(), dir: scan.PlacementDir(c, inv.dirs), readsLibrary: scan.ReadsLibrary(c, inv.dirs)}
		if t.dir == "" && !t.readsLibrary {
			continue // a client the registry gives no skills directory has nowhere to place into
		}
		if len(to) > 0 {
			if !containsString(to, t.id) {
				continue
			}
		} else if containsString(s.DisabledConfigurations, t.id) {
			continue
		}
		targets = append(targets, t)
	}
	for _, id := range to {
		if !hasTarget(targets, id) {
			return nil, inv.detectedConfiguration(id)
		}
	}
	return targets, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func hasTarget(targets []placeTarget, id string) bool {
	for _, t := range targets {
		if t.id == id {
			return true
		}
	}
	return false
}

// installed is what the mutation did, for the report that follows it.
type installed struct {
	adopted   bool          // the library already held this version
	placed    []placeTarget // the configurations that now see the skill
	copies    []string      // the configurations that hold a copy
	adoptions []string      // the placement paths that were a directory of this version
	skipped   []string      // the placement paths something else holds
}

// install publishes the library directory, the import branch and the
// placements as one journaled mutation under the exclusive lock. Every
// input is read again under the lock before anything live changes, and
// nothing live changes before the journal that describes all of it is on
// disk.
func (inv *invocation) install(ctx context.Context, gitDir string, v *imported, targets []placeTarget, asCopy bool) (*installed, error) {
	done := &installed{}
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		if err := os.MkdirAll(inv.dirs.Library, 0o755); err != nil {
			return libraryFailure(inv.dirs.Library, err)
		}
		sweepStaged(inv.dirs.Library)
		for _, t := range targets {
			if !t.readsLibrary {
				sweepStaged(t.dir)
			}
		}
		m := home.NewMutation(inv.dirs.Home)
		if err := inv.stage(ctx, m, gitDir, v, targets, asCopy, done); err != nil {
			// A refusal reached after something was staged takes its
			// staging directories with it: no journal names them, so the
			// sweep of the next install into the same directories is the
			// only thing that would ever remove them, and a copy staged in
			// a client directory no later install targets would stay for good.
			m.Discard()
			return err
		}
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return nil, installFailure(err)
	}
	return done, nil
}

// stage plans the whole mutation, in the order the contract prescribes and
// recovery finishes it in: the import branch, the library directory, the
// placements, the settings write. The ref goes first so that a process
// stopped between two steps never leaves the library holding a skill
// directory that no lineage branch names, which a scan would then read as a
// new unmanaged skill — what the mutation safety spec forbids. Nothing here
// touches a live path: it all becomes the journal Apply then runs.
func (inv *invocation) stage(ctx context.Context, m *home.Mutation, gitDir string, v *imported, targets []placeTarget, asCopy bool, done *installed) error {
	libPath := inv.libraryPath(v.name)
	if err := inv.stageRef(ctx, m, gitDir, v); err != nil {
		return err
	}
	if err := inv.stageLibrary(m, v, libPath, done); err != nil {
		return err
	}
	for _, t := range targets {
		inv.stagePlacement(m, v, t, libPath, asCopy, done)
	}
	if len(done.copies) > 0 {
		return inv.stageCopyMode(m, v.name, done.copies)
	}
	return nil
}

// libraryFailure is the refusal of a library this machine cannot use. The
// library is not optional, as a placement is: without it there is no skill
// to place, so this ends the install rather than being skipped. It is an
// unmet precondition and not an internal error, and doctor's library row
// says the same thing in more detail.
func libraryFailure(library string, err error) error {
	return fail(exitRefused, err.Error(), "run 'agentx doctor' and check the library it names, or make "+library+" writable")
}

// stageLibrary decides what the library does with the version: hold it for
// the first time, adopt a directory that already holds exactly it, or
// refuse a directory that holds something else. A dangling symlink into the
// agentx worktrees directory counts as absent: it is a fork's placement
// whose worktree is gone.
func (inv *invocation) stageLibrary(m *home.Mutation, v *imported, libPath string, done *installed) error {
	state, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	switch {
	case home.IsAbsent(state):
	case home.IsDangling(libPath) && inv.intoWorktrees(libPath):
		m.Remove(libPath, state)
	default:
		hash := contentHashAt(libPath)
		if hash == v.hash {
			done.adopted = true // the same version already: write the branch, copy nothing
			return nil
		}
		return fail(exitRefused, fmt.Sprintf("the library already holds %s at %s", v.name, inv.dirs.Library),
			"remove "+libPath+" and install again, or install the skill under another name by forking it")
	}
	staged := m.Sibling(libPath, "staged")
	if err := inv.writeStaged(staged, v); err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	fingerprint, err := home.Fingerprint(staged)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	m.Publish(libPath, staged, fingerprint)
	return nil
}

// stageRef records the import branch: created with an expected old value of
// empty, so that two commands cannot both claim the name. A branch already
// at this commit is the same version installed again; one at another commit
// is a version this command does not replace.
func (inv *invocation) stageRef(ctx context.Context, m *home.Mutation, gitDir string, v *imported) error {
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		return accountRepoFailure(err)
	}
	rec, ok := records[v.name]
	switch {
	case !ok:
		m.Ref(gitDir, lineage.ManagedRef(v.name), "", v.commit)
	case rec.Kind == lineage.KindFork:
		return fail(exitRefused, fmt.Sprintf("%s is a fork on this machine", v.name),
			"install the skill under another name, or remove the fork first")
	case rec.Commit == v.commit: // the same version again: nothing to move
	default:
		return fail(exitRefused, fmt.Sprintf("%s is already managed at another version", v.name),
			"remove "+inv.libraryPath(v.name)+" and the branch "+lineage.ManagedRef(v.name)+", then install again")
	}
	return nil
}

// stagePlacement plans one configuration's placement. A configuration whose
// client reads the library needs none: the library entry is the placement,
// and a second entry would make that client list the skill twice.
//
// A placement this machine cannot make — a skills directory owned by
// somebody else, one on a read-only mount, one macOS has not granted
// access to, a path that is a file rather than a directory — is skipped
// with a warning and counted in the result, exactly as a path something
// else holds is. One client agentx cannot reach is not a reason to leave
// the library, the branch and every other placement undone, and an install
// that aborted here would leave a journal no later command could finish.
func (inv *invocation) stagePlacement(m *home.Mutation, v *imported, t placeTarget, libPath string, asCopy bool, done *installed) {
	if t.readsLibrary {
		done.placed = append(done.placed, t)
		return
	}
	placePath := filepath.Join(t.dir, v.name)
	state, err := home.State(placePath)
	if err != nil {
		inv.skipPlacement(done, t, placePath, err)
		return
	}
	var displace, adopt bool
	switch {
	case home.IsAbsent(state):
	case sameTarget(placePath, libPath):
		// A symlink at the library directory, dangling until this install
		// publishes it or not: the placement is already what it should be,
		// unless copies were asked for, and a link holds nothing to keep.
		if !asCopy {
			done.placed = append(done.placed, t)
			return
		}
		displace = true
	case contentHashAt(placePath) == v.hash:
		// A real directory holding exactly this version: nothing of the
		// user's is lost by replacing it, and with --copy it is the copy.
		if asCopy && home.IsDir(state) {
			done.placed = append(done.placed, t)
			done.copies = append(done.copies, t.id)
			return
		}
		displace, adopt = true, true
	default:
		done.skipped = append(done.skipped, placePath)
		inv.out.warn(placePath + " is not this skill and was left as it is; no placement was made for " + t.id)
		return
	}
	// Everything that can fail runs before the first step of this placement
	// is recorded, so that a placement that cannot be made leaves neither a
	// step in the journal nor content on disk.
	staged, fingerprint, err := inv.placementContent(m, v, t, placePath, asCopy)
	if err != nil {
		inv.skipPlacement(done, t, placePath, err)
		return
	}
	if displace {
		m.Remove(placePath, state)
		if adopt {
			done.adoptions = append(done.adoptions, placePath)
		}
	}
	if asCopy {
		m.Publish(placePath, staged, fingerprint)
		done.copies = append(done.copies, t.id)
	} else {
		m.Link(placePath, libPath)
	}
	done.placed = append(done.placed, t)
}

// placementContent prepares on disk what one placement needs: a directory
// to hold it, which the install creates when it is missing, and for --copy
// the staged copy itself, read back the way a scan reads it. The directory
// is checked for being writable rather than only for existing, since the
// symlink a placement usually is writes nothing until the journal runs it,
// and a directory agentx cannot write would otherwise be found out only
// then, with the journal already on disk.
func (inv *invocation) placementContent(m *home.Mutation, v *imported, t placeTarget, placePath string, asCopy bool) (staged, fingerprint string, err error) {
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return "", "", err
	}
	if err := unix.Access(t.dir, unix.W_OK|unix.X_OK); err != nil {
		return "", "", &fs.PathError{Op: "access", Path: t.dir, Err: err}
	}
	if !asCopy {
		return "", "", nil
	}
	staged = m.Sibling(placePath, "staged")
	if err := inv.writeStaged(staged, v); err != nil {
		os.RemoveAll(staged)
		return "", "", err
	}
	if fingerprint, err = home.Fingerprint(staged); err != nil {
		os.RemoveAll(staged)
		return "", "", err
	}
	return staged, fingerprint, nil
}

// skipPlacement leaves one configuration without a placement and says why,
// counting it where the contract counts a placement path something else
// holds: the run still succeeds and the result says how many were skipped.
func (inv *invocation) skipPlacement(done *installed, t placeTarget, placePath string, err error) {
	done.skipped = append(done.skipped, placePath)
	inv.out.warn("cannot place " + placePath + ": " + err.Error() + "; no placement was made for " + t.id)
}

// stageCopyMode records in the machine settings which configurations hold a
// copy of this skill, in one write for the whole install.
func (inv *invocation) stageCopyMode(m *home.Mutation, name string, configs []string) error {
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	modes, err := s.CopyModes()
	if err != nil {
		return fail(exitInternal, "parse "+home.SettingsPath(inv.dirs.Home)+": copy_mode must map skill names to configuration ids", "fix copy_mode in the settings file")
	}
	for _, id := range configs {
		if !containsString(modes[name], id) {
			modes[name] = append(modes[name], id)
		}
	}
	sort.Strings(modes[name])
	if err := s.SetCopyModes(modes); err != nil {
		return err
	}
	b, err := home.MarshalSettings(s)
	if err != nil {
		return err
	}
	return m.ReplaceFile(home.SettingsPath(inv.dirs.Home), b)
}

// writeStaged lays the version out at a hidden directory beside the one it
// will become, then reads it back the way a scan does: a directory whose
// content hash is not the version's never gets published.
func (inv *invocation) writeStaged(staged string, v *imported) error {
	if err := os.MkdirAll(staged, 0o755); err != nil {
		return err
	}
	for _, f := range v.files {
		full := filepath.Join(staged, filepath.FromSlash(f.path))
		// readVersion refused such a path already; nothing is written
		// outside the staging directory whatever reaches this far.
		if rel, err := filepath.Rel(staged, full); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%q is not a path inside %s", f.path, staged)
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
	if err := home.SyncTree(staged); err != nil {
		return err
	}
	if hash, _ := scan.ContentHashAt(staged); hash != v.hash {
		return fmt.Errorf("the staged copy of %s hashes to %s, not %s", v.name, hash, v.hash)
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

// sameTarget reports whether the symlink at path names the library
// directory, whether or not it resolves: a placement made before the
// library held the skill dangles until the install publishes it, and is
// still the placement the install would make.
func sameTarget(path, libPath string) bool {
	link, err := os.Readlink(path)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(path), link)
	}
	if filepath.Clean(link) == filepath.Clean(libPath) {
		return true
	}
	a, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	b, err := filepath.EvalSymlinks(libPath)
	return err == nil && a == b
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

// installFailure keeps the refusals of the install as they are and maps a
// journal that could not be finished to the recovery exit code.
func installFailure(err error) error {
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

// reportInstalled reads the affected configurations again and reports the
// skill as the machine now has it: the rescan is what the placements in the
// event are read from, so the report says what is there rather than what
// the install meant to do.
func (inv *invocation) reportInstalled(ctx context.Context, v *imported, done *installed, total int) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	inv.progress(phaseRescan, "", total, total)
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	modes, err := s.CopyModes()
	if err != nil {
		modes = map[string][]string{}
	}
	lib, found := librarySkill(inv.dirs.Library, v.name)
	if !found {
		return fail(exitInternal, "the library holds no "+v.name+" after installing it", "run 'agentx doctor' and check the library it names")
	}
	places := inv.placements(snap, lib, modes)
	inv.summary = installSummary(v, done)
	rec := lineage.Record{Name: v.name, Kind: lineage.KindManaged, Ref: lineage.ManagedRef(v.name), Commit: v.commit, Import: v.imp, HasImport: true}
	ev := skillFromLibrary(lib, rec, true, filterPlacements(places, done))
	inv.out.emit(ev)
	inv.printInstalled(v, ev, done)
	return nil
}

// installSummary is what the result event says the run did: the skill, where
// it came from, how many configurations now see it and what was left alone.
func installSummary(v *imported, done *installed) string {
	what := "installed"
	if done.adopted {
		what = "adopted"
	}
	summary := fmt.Sprintf("%s %s from %s%s in %s", what, v.name, v.imp.Source, underPath(v.imp.Path), plural(len(done.placed), "configuration"))
	if n := len(done.copies); n > 0 {
		summary += fmt.Sprintf(", %s as %s", plural(n, "placement"), modeCopy)
	}
	if n := len(done.adoptions); n > 0 {
		summary += fmt.Sprintf(", %s adopted", plural(n, "placement"))
	}
	if n := len(done.skipped); n > 0 {
		summary += fmt.Sprintf(", %s skipped", plural(n, "placement"))
	}
	return summary + fetchedAt(v)
}

// fetchedAt says when the source the version was read from was last
// fetched, so that an install from an old fetch says so.
func fetchedAt(v *imported) string {
	if v.fetched == "" {
		return ""
	}
	return ", source fetched " + v.fetched
}

// filterPlacements keeps the placements of the configurations this install
// covered, which is what a targeted rescan reports on; the library entry of
// a client that reads the library is one of them.
func filterPlacements(places []placementEvent, done *installed) []placementEvent {
	kept := []placementEvent{}
	for _, p := range places {
		for _, t := range done.placed {
			if p.Configuration == t.id {
				kept = append(kept, p)
				break
			}
		}
	}
	return kept
}

// librarySkill reads one directory of the library.
func librarySkill(library, name string) (scan.LibrarySkill, bool) {
	skills, _ := scan.ReadLibrary(library)
	for _, s := range skills {
		if s.Name == name {
			return s, true
		}
	}
	return scan.LibrarySkill{}, false
}

// printInstalled writes the confirmation and one row per placement.
func (inv *invocation) printInstalled(v *imported, ev librarySkillEvent, done *installed) {
	out := inv.out
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
	t := &table{}
	for _, p := range ev.Placements {
		path := p.Path
		if p.Kind == modeSymlink {
			path += out.paint(muted, " -> "+inv.libraryPath(v.name))
		}
		t.add(c("  "+p.Configuration, label), c(p.Mode, muted), c(path, plain))
	}
	out.render(t, "")
	if len(done.adoptions) > 0 {
		out.print("  ", out.paint(muted, "adopted "+strings.Join(done.adoptions, ", ")))
	}
}
