package cli

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// Reconciliation matches the branches of the account repo against what is
// on disk, so that agentx installed again over an existing agentx home, or
// a fork's worktree deleted by mistake, comes back as it was: every skill
// is recognised from its branch and its library entry, a fork whose
// worktree went is checked out again from its branch, and nothing is
// deleted or overwritten. The outcomes, one per skill name, are these.
const (
	// outcomeRestored is a skill found as its branch leaves it: a managed
	// skill whose library directory holds its base version, or a fork
	// whose library symlink leads into its registered worktree.
	outcomeRestored = "restored"
	// outcomeRepaired is a fork whose worktree, skill directory or library
	// symlink was missing, and was put back from its branch.
	outcomeRepaired = "repaired"
	// outcomeWorktreeMissing is a fork that was placed on this machine and
	// whose worktree, the skill directory in it, or the library symlink
	// into it, is gone or leads nowhere, and was not put back. It is not
	// drift's missing, which is a configuration's place with nothing at it.
	outcomeWorktreeMissing = "worktree missing"
	// outcomeAdoptCandidate is a fork with something of its own in the
	// way: a directory where its worktree belongs that is not registered as
	// one, or a directory or a foreign symlink where its library symlink
	// belongs. It is reported and left as it is.
	outcomeAdoptCandidate = "adopt candidate"
	// outcomeModified is a managed skill whose library directory does not
	// hold its base version.
	outcomeModified = "modified"
	// outcomeInstallable is a skill the account repo holds and the library
	// does not: a managed skill whose library directory is gone, a fork
	// never placed on this machine, and a fork only the account remote's
	// branches hold.
	outcomeInstallable = "installable"
	// outcomeUnmanaged is a skill of the library no branch names.
	outcomeUnmanaged = "unmanaged"
)

// reconcileEvent is the outcome of one skill name, emitted by a
// long-running serve before its first snapshot.
type reconcileEvent struct {
	event
	Name    string `json:"name"`
	Kind    string `json:"kind"` // managed or unmanaged, see eventKind
	Outcome string `json:"outcome"`
	Path    string `json:"path,omitempty"` // what is in the way of an adopt candidate, the worktree of one whose worktree is missing
}

// libKind is what a fork's library entry is.
type libKind int

const (
	libAbsent   libKind = iota // nothing
	libOwn                     // a symlink leading into the fork's own worktree
	libDangling                // a symlink into the worktrees directory that leads nowhere
	libDir                     // a real directory
	libForeign                 // anything else: a symlink to elsewhere, a file
)

// rootKind is what is at a fork's worktree.
type rootKind int

const (
	rootAbsent   rootKind = iota // nothing
	rootEmpty                    // an empty directory, or one holding nothing but a .git file that is not registered: what a worktree add stopped part way leaves
	rootWorktree                 // a worktree the account repo registers, whatever branch it is on
	rootMoved                    // a .git file whose pointers no longer meet, as moving agentx home leaves them; git can repair it
	rootOrphan                   // a directory holding more than a lone .git file that is not a registered worktree
)

// forkFacts are what reconciliation reads of one fork, from files alone.
type forkFacts struct {
	lib        libKind
	root       rootKind
	registered bool // a registration of the account repo names the worktree, whatever is there now
	onBranch   bool // rootWorktree: its HEAD is the fork's branch
	skillDir   bool // rootWorktree: it holds the skill's directory
	busy       bool // rootWorktree: a git command the user ran stopped part way in it, or a running git holds its index
}

// forkVerdict is what reconciliation makes of a fork's facts: the outcome
// before any repair, what is in the way of an adopt candidate, and what a
// repair does. A fork with anything to repair is worktree missing until
// the repair is made, and repaired once it is.
type forkVerdict struct {
	outcome  string
	inWay    string // "worktree" or "library symlink": what an adopt candidate's path is in the way of
	pointers bool   // the worktree's pointers are repaired with git first, and the fork read again
	worktree bool   // the worktree is added, or its index aligned with the branch
	content  bool   // the skill's directory is laid out from the branch tip
	link     bool   // the library symlink is written, a dangling one removed first
}

// repairs reports whether the verdict has anything to repair.
func (v forkVerdict) repairs() bool { return v.pointers || v.worktree || v.content || v.link }

// classifyFork is the rule reconciliation, the listing's warnings and skill
// place share. Something of the user's in the way, a directory or a
// foreign symlink at the library entry, or a directory at the worktree
// that is not registered, makes an adopt candidate, which only skill place
// --force adopts. A fork with neither a worktree, nor a registration of
// one, nor a library symlink into the worktrees directory was never placed
// on this machine, and is installable. Every other fork is restored when
// its library symlink leads into its registered worktree, and worktree
// missing otherwise, with what a repair puts back: the worktree and the
// skill directory, laid out from the branch tip, and the library symlink.
// A registered worktree on another branch than the fork's keeps whatever
// it holds: agentx lays nothing out where git would compare it with
// another branch. Nor is anything laid out in one where a git command the
// user ran stopped part way, such as a merge with conflicts, or that a
// running git holds: aligning its index with the branch would lose the
// command's state.
func classifyFork(f forkFacts) forkVerdict {
	switch {
	case f.lib == libDir || f.lib == libForeign:
		return forkVerdict{outcome: outcomeAdoptCandidate, inWay: "library symlink"}
	case f.root == rootOrphan:
		return forkVerdict{outcome: outcomeAdoptCandidate, inWay: "worktree"}
	case f.root == rootMoved:
		return forkVerdict{outcome: outcomeWorktreeMissing, pointers: true}
	case f.root == rootAbsent && f.lib == libAbsent && !f.registered:
		return forkVerdict{outcome: outcomeInstallable}
	case f.root == rootAbsent || f.root == rootEmpty:
		return forkVerdict{outcome: outcomeWorktreeMissing, worktree: true, content: true, link: true}
	case !f.skillDir && (!f.onBranch || f.busy):
		return forkVerdict{outcome: outcomeWorktreeMissing}
	}
	v := forkVerdict{outcome: outcomeRestored, worktree: !f.skillDir, content: !f.skillDir, link: f.lib != libOwn}
	if v.repairs() {
		v.outcome = outcomeWorktreeMissing
	}
	return v
}

// forkFactsOf reads the facts of the fork f from files alone. Its skill
// directory is looked for only when f names it: a listing, which reads no
// git, leaves it unnamed when the library symlink does not lead into the
// worktree.
func (inv *invocation) forkFactsOf(f forkSite) forkFacts {
	var facts forkFacts
	info, err := os.Lstat(f.libPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		facts.lib = libAbsent
	case err != nil:
		facts.lib = libForeign
	case info.Mode()&os.ModeSymlink != 0:
		real, err := filepath.EvalSymlinks(f.libPath)
		_, own := inv.placedForkDir(f.name, real)
		switch {
		case err == nil && own:
			facts.lib = libOwn
		case err != nil && inv.intoWorktrees(f.libPath):
			facts.lib = libDangling
		default:
			facts.lib = libForeign
		}
	case info.IsDir():
		facts.lib = libDir
	default:
		facts.lib = libForeign
	}
	facts.registered = home.Registered(f.gitDir, f.root)
	switch {
	case !lexists(f.root):
		facts.root = rootAbsent
	case home.PointersMoved(f.root) && repairable(f.gitDir, f.root):
		facts.root = rootMoved
	case home.RegisteredIn(f.gitDir, f.root):
		facts.root = rootWorktree
		facts.onBranch = home.WorktreeAt(f.root, f.branch)
		facts.busy = home.Unfinished(f.root) != "" || home.IndexLock(f.root) != ""
		facts.skillDir = f.dir == "" || isDir(f.skillDir)
	case loneGitFile(f.root):
		facts.root = rootEmpty
	default:
		facts.root = rootOrphan
	}
	return facts
}

// repairable reports whether git can repair the pointers of the worktree
// at root, which no longer meet, as git worktree repair finds the
// registration: the admin directory its .git file names, whose gitdir then
// names another path, or, when that directory is not there, as moving
// agentx home as a whole leaves it, the one of the same name in the account
// repo at gitDir. A worktree whose registration is gone either way, as one
// removed by hand leaves it, is a directory git does not know, and so is a
// copy of another worktree, whose registration names that worktree, which
// names it back: repairing the copy would take the registration from it.
func repairable(gitDir, root string) bool {
	admin, ok := home.AdminDirOf(root)
	if !ok {
		return false
	}
	if _, held := home.HeldBy(root); held {
		return false
	}
	return lexists(admin) || lexists(filepath.Join(gitDir, "worktrees", filepath.Base(admin)))
}

// loneGitFile reports whether the directory at path is empty, or holds
// nothing but a .git file: what a worktree add stopped part way, or a
// worktree emptied by hand, leaves, which adding the worktree again
// clears.
func loneGitFile(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0 || len(entries) == 1 && entries[0].Name() == ".git" && entries[0].Type().IsRegular()
}

// isDir reports whether path is a directory, not following a symlink.
func isDir(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// forkWarning is what a listing says of a fork reconciliation would not
// call restored or installable, "" when it says nothing: the one line
// that names what is wrong and the skill place that puts it right. It
// reads files alone.
func (inv *invocation) forkWarning(f forkSite) string {
	facts := inv.forkFactsOf(f)
	v := classifyFork(facts)
	switch {
	case v.outcome == outcomeAdoptCandidate:
		what, wayOut := adoptRefusal(f, v, facts)
		return what + "; " + wayOut
	case v.outcome != outcomeWorktreeMissing:
		return ""
	}
	return worktreeMissingWarning(f, facts)
}

// worktreeMissingWarning names what is missing of a fork whose worktree is
// missing, and the skill place that puts it back.
func worktreeMissingWarning(f forkSite, facts forkFacts) string {
	name, place := sanitised(f.name), "run '"+skillCommand("place", f.name)+"'"
	switch {
	case facts.root == rootMoved:
		return name + "'s worktree " + quotedPath(f.root) + " needs repair; " + place + " to repair it"
	case facts.root == rootAbsent || facts.root == rootEmpty:
		return name + "'s worktree " + quotedPath(f.root) + " is missing; " + place + " to check it out again from its branch"
	case facts.busy && !facts.skillDir:
		where := name + "'s skill directory " + quotedPath(f.skillDir) + " is missing from its worktree, "
		if command := home.Unfinished(f.root); command != "" {
			return where + "where a git " + command + " stopped part way; finish it with git, or run 'git -C " + shellWord(f.root) + " " + command + " --abort' to give it up, then " + place
		}
		return where + "where git is running; " + place + " once it finishes; if no git is running, remove " + quotedPath(home.IndexLock(f.root))
	case !facts.onBranch:
		return name + "'s worktree " + quotedPath(f.root) + " holds no skill directory and is not on its branch " + f.branch + "; run 'git -C " + shellWord(f.root) + " switch " + f.branch + "', then " + place
	case !facts.skillDir:
		return name + "'s skill directory " + quotedPath(f.skillDir) + " is missing from its worktree; " + place + " to lay it out again from its branch"
	}
	return name + "'s library symlink " + quotedPath(f.libPath) + " is missing or leads nowhere; " + place + " to put it back"
}

// adoptRefusal is what skill place says of an adopt candidate it was not
// given --force for, and how a listing warns of one: what is in the way of
// what, and how to adopt it. Adopting keeps every file: a directory's
// content becomes the fork's unpublished edits, and a symlink, which
// holds nothing, is replaced.
func adoptRefusal(f forkSite, v forkVerdict, facts forkFacts) (what, wayOut string) {
	path := f.root
	if v.inWay == "library symlink" {
		path = f.libPath
	}
	what = quotedPath(path) + " is in the way of " + sanitised(f.name) + "'s " + v.inWay
	force := "run '" + skillCommand("place", f.name, "--force") + "'"
	if facts.lib == libForeign && v.inWay == "library symlink" {
		return what, force + " to replace it with " + sanitised(f.name) + "'s library symlink"
	}
	return what, force + " to adopt it: its content becomes the skill's unpublished edits"
}

// forkWarnings are the warnings a listing gives of the forks of records
// but the one called except, sorted by name: one per fork whose worktree
// is missing or that has something in the way, see forkWarning. A fork's
// skill directory is named only when its library symlink leads into its
// worktree, or names a directory at the worktree's root that is gone,
// since finding it otherwise takes git, and no listing runs git for a
// warning.
func (inv *invocation) forkWarnings(records map[string]lineage.Record, except string) []string {
	gitDir := gitx.AccountRepoPath(inv.dirs.Home)
	var warnings []string
	for _, name := range slices.Sorted(maps.Keys(records)) {
		rec := records[name]
		if rec.Kind != lineage.KindFork || name == except {
			continue
		}
		f := inv.forkPlace(gitDir, rec)
		if dir, ok := inv.linkedForkDir(name); ok {
			f.dir, f.skillDir = dir, filepath.Join(f.root, dir)
		}
		if w := inv.forkWarning(f); w != "" {
			warnings = append(warnings, w)
		}
	}
	return warnings
}

// linkedForkDir is the skill directory the library symlink of the fork
// called name leads to in the fork's worktree, read from the filesystem
// alone, and false when it leads elsewhere: where it leads, or, when that
// is gone, as a skill directory deleted from the worktree leaves it, what
// the symlink names, when that is a directory at the worktree's root.
func (inv *invocation) linkedForkDir(name string) (string, bool) {
	libPath := inv.libraryPath(name)
	if real, err := filepath.EvalSymlinks(libPath); err == nil {
		return inv.placedForkDir(name, real)
	}
	link, err := os.Readlink(libPath)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(canonicalPath(libPath)), link)
	}
	link = filepath.Clean(link)
	root := inv.worktreeRoot(name)
	if dir := filepath.Dir(link); dir != root && dir != canonicalPath(root) {
		return "", false
	}
	return filepath.Base(link), true
}

// listLineage is lineage.List for a command, which reads the branches of
// the account repo at gitDir, and, the first time this run reads them,
// warns of every fork the listing would warn of, see forkWarnings, so that
// a fork whose worktree is missing is named by whatever command runs next,
// not only by skill list. It reads files alone for the warnings. A command
// whose own output carries them, skill list, a snapshot and serve, or that
// puts the fork back, skill place, sets forksWarned first.
func (inv *invocation) listLineage(ctx context.Context, gitDir string) (map[string]lineage.Record, error) {
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err == nil && !inv.forksWarned {
		inv.forksWarned = true
		for _, w := range inv.forkWarnings(records, "") {
			inv.out.warn(w)
		}
	}
	return records, err
}

// serveReconcile reconciles agentx home when a long-running serve starts,
// before its first scan, and emits one reconcile event per skill name, by
// name. It holds the lock throughout, waiting for it as the update check
// does: the recovery and pruning every mutation starts with, then git's
// registrations of worktrees whose directory is gone, which a locked
// worktree, a fork's or a pending merge's, survives; then the pointers of
// every fork's worktree that moved with agentx home, repaired with git,
// each path named; then the classification, see classifyFork, and one
// journal that repairs every fork whose worktree is missing. Adopt
// candidates are reported and left as they are. The version file is
// rewritten only when something was repaired. A repair that fails is a
// warning, and its forks stay worktree missing.
//
// A pending merge's checkout under merges/ is never classified or repaired
// as a fork: the recovery every mutation starts with prunes a checkout
// that is not registered, and a registration with no checkout.
func (inv *invocation) serveReconcile(ctx context.Context) error {
	var events []reconcileEvent
	err := home.MutateQuietWaiting(ctx, inv.dirs.Home, inv.refs(ctx), func() (err error) {
		events, err = inv.reconcile(ctx)
		return err
	})
	if err != nil {
		return err
	}
	for _, ev := range events {
		inv.out.emit(ev)
		if ev.Outcome != outcomeRestored {
			style := noteStyle
			if ev.Outcome == outcomeAdoptCandidate || ev.Outcome == outcomeWorktreeMissing {
				style = warnStyle
			}
			inv.out.print(inv.out.paint(heading, "reconcile "+sanitised(ev.Name)), ": ", inv.out.paint(style, ev.Outcome))
		}
	}
	return nil
}

// reconcile is serveReconcile's work under the lock: what it repairs, and
// the events it reports.
func (inv *invocation) reconcile(ctx context.Context) ([]reconcileEvent, error) {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return nil, err
	}
	remote, repointed := map[string]bool{}, map[string]bool{}
	if exists {
		var roots []string
		for _, name := range slices.Sorted(maps.Keys(sc.records)) {
			if sc.records[name].Kind == lineage.KindFork {
				roots = append(roots, inv.worktreeRoot(name))
			}
		}
		if remote, repointed, err = inv.prepareWorktrees(ctx, gitDir, roots); err != nil {
			return nil, err
		}
	}
	libs := librarySkills(inv.dirs.Library)
	names := map[string]bool{}
	for name := range sc.records {
		names[name] = true
	}
	for name := range libs {
		names[name] = true
	}
	for name := range remote {
		names[name] = true
	}
	var events []reconcileEvent
	var repairs []forkSite
	for _, name := range slices.Sorted(maps.Keys(names)) {
		ev := reconcileEvent{event: newEvent("reconcile"), Name: name}
		rec, held := sc.records[name]
		lib, inLibrary := libs[name]
		switch {
		case held && rec.Kind == lineage.KindFork:
			f, v := inv.reconcileFork(ctx, gitDir, rec)
			ev.Kind, ev.Outcome, ev.Path = lineage.KindManaged, v.outcome, verdictPath(f, v)
			if v.outcome == outcomeRestored && repointed[name] {
				ev.Outcome = outcomeRepaired
			}
			if v.repairs() && !v.pointers {
				repairs = append(repairs, f)
			}
		case held:
			ev.Kind = lineage.KindManaged
			switch {
			case !inLibrary:
				ev.Outcome = outcomeInstallable
			case sc.observe(ctx, inv, lib).modified:
				ev.Outcome = outcomeModified
			default:
				ev.Outcome = outcomeRestored
			}
		case remote[name] && !inLibrary:
			ev.Kind, ev.Outcome = lineage.KindManaged, outcomeInstallable
		default:
			ev.Kind, ev.Outcome = lineage.KindUnmanaged, outcomeUnmanaged
		}
		events = append(events, ev)
	}
	repaired := map[string]bool{}
	if len(repairs) > 0 {
		if repaired, err = inv.repairForks(ctx, repairs); err != nil {
			inv.out.warn("reconcile: " + err.Error() + "; run 'agentx skill place <name>' for each of your skills whose worktree is missing")
		}
	}
	for i := range events {
		if repaired[events[i].Name] {
			events[i].Outcome, events[i].Path = outcomeRepaired, ""
		}
	}
	if len(repaired) == 0 && len(repointed) == 0 {
		return events, nil
	}
	return events, home.BumpVersion(inv.dirs.Home)
}

// reconcileFork finds and classifies the fork whose branch is rec. Its
// skill directory is where the library symlink leads or, when the symlink
// leads elsewhere, the one directory its tip holds; a tip that does not
// tell is no reason to keep serve from starting, so the fork is then
// classified without one, and repairForks leaves it as it is. A worktree
// whose pointers still do not meet is one git could not repair, a
// directory of the user's in the way.
func (inv *invocation) reconcileFork(ctx context.Context, gitDir string, rec lineage.Record) (forkSite, forkVerdict) {
	f := inv.forkPlace(gitDir, rec)
	if err := inv.findForkDir(ctx, &f); err != nil {
		inv.out.debugf("reconcile %s: %v", rec.Name, err)
	}
	facts := inv.forkFactsOf(f)
	if facts.root == rootMoved {
		facts.root = rootOrphan // prepareWorktrees asked git to repair it, and git could not
	}
	return f, classifyFork(facts)
}

// verdictPath is the path a reconcile event names: what is in the way of
// an adopt candidate, and the worktree of a fork whose worktree is
// missing.
func verdictPath(f forkSite, v forkVerdict) string {
	switch {
	case v.inWay == "library symlink":
		return f.libPath
	case v.outcome == outcomeAdoptCandidate || v.outcome == outcomeWorktreeMissing:
		return f.root
	}
	return ""
}

// prepareWorktrees readies git's worktrees of the account repo at gitDir
// for reconciliation, and, while an account remote is set, reads the fork
// branches it holds, by name: registrations whose directory is gone are
// pruned, which every locked worktree survives, and the pointers of every
// fork's worktree of roots that no longer meet, as moving agentx home
// leaves them, are repaired, each path named; repointed are the names of
// the worktrees the repair put right. A directory of the worktrees
// directory that is no fork's worktree is never repaired, nor is a copy of
// another worktree, see repairable: git would hand it that worktree's
// registration. A repair git cannot make leaves the worktree as it is, for
// the classification to find.
func (inv *invocation) prepareWorktrees(ctx context.Context, gitDir string, roots []string) (remote, repointed map[string]bool, err error) {
	list, err := inv.git.Worktrees(ctx, gitDir)
	if err != nil {
		return nil, nil, accountRepoFailure(err)
	}
	if slices.ContainsFunc(list, func(w gitx.Worktree) bool { return w.Prunable }) {
		if err := inv.git.PruneWorktrees(ctx, gitDir); err != nil {
			return nil, nil, accountRepoFailure(err)
		}
	}
	var moved []string
	for _, root := range roots {
		if isDir(root) && home.PointersMoved(root) && repairable(gitDir, root) {
			moved = append(moved, root)
		}
	}
	repointed = map[string]bool{}
	if len(moved) > 0 {
		if err := inv.git.RepairWorktrees(ctx, gitDir, moved); err != nil {
			inv.out.debugf("worktree repair: %v", err)
		}
		for _, root := range moved {
			if !home.PointersMoved(root) {
				repointed[filepath.Base(root)] = true
			}
		}
	}
	remote = map[string]bool{}
	_, account, ok, err := inv.accountSource()
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return remote, repointed, nil
	}
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, account)
	if err != nil {
		return nil, nil, accountRepoFailure(err)
	}
	for name := range tips {
		remote[name] = true
	}
	return remote, repointed, nil
}

// repairForks repairs, in one journal under the lock its caller holds,
// every fork of sites whose worktree is missing, read again first, and
// returns the names of the forks it repaired: the worktree added, the
// skill's directory laid out from the branch tip and the library symlink
// written, as stageForkRepair plans them. Placements are not touched: they
// lead to the library entry, which the symlink puts back.
func (inv *invocation) repairForks(ctx context.Context, sites []forkSite) (map[string]bool, error) {
	sweepStaged(inv.worktreesDir())
	m := home.NewMutation(inv.dirs.Home)
	repaired := map[string]bool{}
	for _, f := range sites {
		v := classifyFork(inv.forkFactsOf(f))
		if v.outcome != outcomeWorktreeMissing || v.pointers || f.dir == "" {
			continue
		}
		if _, err := inv.stageForkRepair(ctx, m, f, v); err != nil {
			m.Discard()
			return nil, err
		}
		repaired[f.name] = true
	}
	if err := m.Apply(inv.refs(ctx)); err != nil {
		return nil, mutationFailure(err)
	}
	return repaired, nil
}

// stageForkRepair plans into m the repair of the fork f, as v says: the
// worktree added, or its index aligned with the branch, the skill's
// directory laid out from the branch tip, staged in the worktrees
// directory, and the library symlink written, a dangling one removed
// first. A worktree added, or one that holds nothing but its .git file,
// also gets back what the branch tip holds beside the skill directory, see
// stageForkWorktree. It returns the directory the fork's content is at
// before the journal is applied, for a copy placed from it: the staged
// content when it lays the directory out, and the skill directory
// otherwise.
func (inv *invocation) stageForkRepair(ctx context.Context, m *home.Mutation, f forkSite, v forkVerdict) (string, error) {
	from := f.skillDir
	if v.worktree || v.content && loneGitFile(f.root) {
		if err := inv.stageForkWorktree(ctx, m, f, v.worktree); err != nil {
			return "", err
		}
	}
	if v.content {
		staged := m.Sibling(f.root, "staged")
		fp, err := inv.stageForkContent(ctx, f.gitDir, f.rec.Commit, f.dir, staged, "", nil)
		if err != nil {
			_ = home.RemoveTree(staged)
			return "", accountRepoFailure(err)
		}
		m.Publish(f.skillDir, staged, fp)
		from = staged
	}
	if v.link {
		if err := inv.stageForkLink(m, f); err != nil {
			return "", err
		}
	}
	return from, nil
}

// stageForkWorktree plans into m the worktree step of the fork f, which
// adds its worktree or aligns its index with the branch. A worktree that
// is added, or that holds nothing but its .git file, has the entries the
// branch tip holds at its root beside the skill directory checked out with
// it: a commit made with git keeps files there, such as a .gitignore whose
// rules the skill's files are judged by, and a worktree put back without
// them would show them as deleted and stop ignoring what they ignore. When
// the worktree is there already and the tip holds nothing beside the
// skill directory, the step is planned only when always is set.
func (inv *invocation) stageForkWorktree(ctx context.Context, m *home.Mutation, f forkSite, always bool) error {
	if err := os.MkdirAll(inv.worktreesDir(), 0o755); err != nil {
		return libraryFailure(inv.worktreesDir(), err)
	}
	var beside []string
	if !home.WorktreeAt(f.root, f.branch) || loneGitFile(f.root) {
		names, err := inv.besideSkillDir(ctx, f.gitDir, f.rec.Commit, f.dir)
		if err != nil {
			return err
		}
		beside = names
	}
	if always || len(beside) > 0 {
		m.Worktree(f.gitDir, f.root, f.branch, beside...)
	}
	return nil
}

// besideSkillDir is the names of the entries commit holds at its root
// beside the skill directory dir, read in one ls-tree.
func (inv *invocation) besideSkillDir(ctx context.Context, gitDir, commit, dir string) ([]string, error) {
	out, err := inv.git.Isolated(ctx, gitDir, "ls-tree", "-z", "--name-only", commit)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	var names []string
	for _, name := range strings.Split(out, "\x00") {
		if name != "" && name != dir {
			names = append(names, name)
		}
	}
	return names, nil
}

// stageForkLink plans into m the library symlink of the fork f, the entry
// at the library path removed first when it is a symlink: one that leads
// nowhere, or a foreign one skill place --force replaces. Neither holds
// anything.
func (inv *invocation) stageForkLink(m *home.Mutation, f forkSite) error {
	state, err := home.State(f.libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	if err := os.MkdirAll(inv.dirs.Library, 0o755); err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	link, err := inv.forkLink(f.libPath, f.skillDir)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	if home.IsLink(state) {
		m.Remove(f.libPath, state)
	}
	m.Link(f.libPath, link)
	return nil
}
