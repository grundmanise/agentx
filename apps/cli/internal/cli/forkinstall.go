package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// A fork another machine published is installable here once a fetch of the
// account remote has written its remote-tracking branch and this machine
// has no branch of its own by that name. skill list --remote lists them,
// each with the provenance its own commits record, and skill add
// --from-account installs one: the local branch is created at the
// remote-tracking branch's commit, so the fork is the same fork, with the
// same fork id and history, and what is committed here is published back
// to the same branch. Nothing is copied outside git: the worktree is added
// for the branch and its skill directory laid out from the commit, as a
// fork put back by skill place is.

// installableForkEvent is one fork the account remote holds that this
// machine has no branch of, as skill list --remote lists it.
type installableForkEvent struct {
	event
	Name           string  `json:"name"`
	ForkID         string  `json:"fork_id,omitempty"` // the fork's permanent id, "" when its history records none
	Commit         string  `json:"commit"`            // the remote tip
	Source         string  `json:"source,omitempty"`  // the canonical URL of the upstream, for a fork with a base version
	Subpath        *string `json:"subpath,omitempty"`
	UpstreamCommit string  `json:"upstream_commit,omitempty"`
	BaseHash       string  `json:"base_hash,omitempty"`
}

// installRemoteRow is what the human listing says of an installable fork,
// in the state column the library's rows use.
const installRemoteRow = "installable"

// installableForks reads the forks the account remote holds that records,
// the account repo's branches, hold no fork branch of, sorted by name, with
// the provenance of each read by one walk of their remote tips. A branch
// whose name agentx would never give a fork is left out, with a warning
// each: installing it could not create the local branch.
func (inv *invocation) installableForks(ctx context.Context, gitDir string, records map[string]lineage.Record) ([]installableForkEvent, []string, error) {
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir)
	if err != nil {
		return nil, nil, accountRepoFailure(err)
	}
	var names, warnings []string
	var walk []string
	for name, tip := range tips {
		switch {
		case records[name].Kind == lineage.KindFork:
		case forkNameRefusal(name) != "":
			warnings = append(warnings, "the account remote's branch skills/"+sanitised(name)+" is not listed, since it cannot be installed: "+forkNameRefusal(name))
		default:
			names = append(names, name)
			walk = append(walk, tip)
		}
	}
	sort.Strings(names)
	sort.Strings(warnings)
	walked, err := lineage.Walk(ctx, inv.git, gitDir, walk)
	if err != nil {
		return nil, nil, accountRepoFailure(err)
	}
	events := make([]installableForkEvent, 0, len(names))
	for _, name := range names {
		l := walked[tips[name]]
		ev := installableForkEvent{event: newEvent("installable_fork"), Name: name, ForkID: l.ID, Commit: tips[name]}
		if l.Base != "" && l.Problem == "" {
			subpath := l.Import.Path
			ev.Source, ev.Subpath, ev.UpstreamCommit, ev.BaseHash = l.Import.Source, &subpath, l.Import.Commit, l.Import.Hash
		}
		events = append(events, ev)
	}
	return events, warnings, nil
}

// installableRow is one line of the human listing for a fork the account
// remote holds and this machine does not: its name, the kind and state it
// is listed with, and where it came from.
func installableRow(out *writer, ev installableForkEvent) []cell {
	upstream := c("(none)", muted)
	if ev.Source != "" {
		where := ev.Source
		if ev.Subpath != nil && *ev.Subpath != "" {
			where += "/" + *ev.Subpath
		}
		upstream = c(sanitised(where), plain)
	}
	return []cell{c("  "+sanitised(ev.Name), heading), c(lineage.KindFork, muted), c(installRemoteRow, infoStyle), upstream}
}

// fromAccountCommand is the command line that installs the fork called
// name from the account remote, with flags after it.
func fromAccountCommand(name string, flags ...string) string {
	return strings.Join(append([]string{"agentx", "skill", "add", "--from-account", shellWord(name)}, flags...), " ")
}

// libraryHolds is what the library path of a fork being installed from
// the account remote holds, as fromAccountPlan decides on it.
type libraryHolds int

const (
	holdsNothing    libraryHolds = iota // nothing, or a symlink into the worktrees directory that leads nowhere
	holdsCopy                           // a managed skill of the fork's upstream, its library directory holding the base version
	holdsEditedCopy                     // a managed skill of the fork's upstream whose library directory was edited
	holdsDirectory                      // any other directory: an unmanaged skill, or a managed skill of another upstream
	holdsLink                           // a symlink that is not a fork's whose worktree is gone
	holdsFile                           // a file, or anything else that is neither a directory nor a symlink
)

// accountAction is what an install from the account remote does at the
// library path.
type accountAction int

const (
	installFresh     accountAction = iota // lay the fork out and link the library path to it
	installSupersede                      // as installFresh, the managed copy's directory retired and its files git ignores carried in
	installKeepLocal                      // move the directory into the worktree, its content then uncommitted edits of the fork
)

// fromAccountPlan decides, with no I/O, what installing the fork called
// name from the account remote does with what its library path, libPath,
// holds, and refuses what it cannot take: a managed copy of the fork's
// upstream that still holds its base version is superseded, with or
// without --keep-local; a directory of anything else, an edited copy
// included, is refused unless --keep-local keeps it as the fork's
// uncommitted edits; and a symlink or a file is refused either way, since
// moving one in would make the fork's skill directory a link or no
// directory at all.
func fromAccountPlan(name, libPath string, holds libraryHolds, keepLocal bool) (accountAction, *failure) {
	again := "run '" + fromAccountCommand(name, "--keep-local") + "'"
	switch holds {
	case holdsNothing:
		return installFresh, nil
	case holdsCopy:
		return installSupersede, nil
	case holdsEditedCopy:
		if !keepLocal {
			return 0, refuse(exitRefused, sanitised(name)+" holds edits at "+quotedPath(libPath)+" that installing the fork would lose, so nothing was changed",
				again+" to keep them as uncommitted edits of the fork, or '"+skillCommand("revert", name)+"' to discard them first")
		}
		return installKeepLocal, nil
	case holdsDirectory:
		if !keepLocal {
			return 0, refuse(exitRefused, "the library already holds "+quotedPath(libPath)+", so "+sanitised(name)+" was not installed",
				again+" to keep its content as uncommitted edits of the fork, or move it aside and run the command again")
		}
		return installKeepLocal, nil
	case holdsLink:
		return 0, refuse(exitRefused, "the library already holds "+quotedPath(libPath)+", a symlink, so "+sanitised(name)+" was not installed",
			"remove the link, then run '"+fromAccountCommand(name)+"' again")
	}
	return 0, refuse(exitRefused, quotedPath(libPath)+" is neither a directory nor a symlink, so "+sanitised(name)+" was not installed",
		"move it aside, then run '"+fromAccountCommand(name)+"' again")
}

// accountInstall is one fork being installed from the account remote, as
// the command found it before the lock and checks again under it.
type accountInstall struct {
	site      forkSite            // the fork as it will be: rec is its branch at the remote tip
	beside    []string            // what the tip holds at its root beside the skill directory
	there     lineage.ForkLineage // the remote tip's lineage
	action    accountAction
	managed   *lineage.Record // the managed skill of the name the fork takes the place of, nil for none
	libState  string          // what the library path held, as captured before it was judged
	ignored   []string        // the files git ignores in a superseded copy, carried into the worktree
	keepLocal bool
}

// installFromAccount is skill add --from-account: it fetches the account
// remote, then installs the fork called name it holds, see accountInstall,
// in one journaled mutation: the local branch at the remote tip, its
// worktree, the skill directory laid out from the tip, or the directory at
// the library path moved in with --keep-local, the library symlink, and
// the placements of a fork new to this machine. A managed skill the fork
// takes the place of keeps its placements, which lead to the library path
// and so to the fork, and its import branch, update candidate and
// upstream-removed marker are deleted last. The fork's upstream source is
// added first when the settings lack it, as source add adds one, so that
// update checks cover the fork here as on the machine that published it.
// The branch is set to track the account remote's just before the journal
// is written.
func (inv *invocation) installFromAccount(ctx context.Context, name string, to []string, asCopy, keepLocal bool) error {
	if refusal := forkNameRefusal(name); refusal != "" {
		return fail(exitRefused, refusal, "run 'agentx skill list --remote' to see the forks the account remote holds")
	}
	targets, err := inv.placementTargets(to)
	if err != nil {
		return err
	}
	gitDir, url, err := inv.accountRemote(ctx)
	if err != nil {
		return err
	}
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	if err := inv.fetchRemote(ctx, gitDir, url); err != nil {
		return err
	}
	if err := home.SyncExclude(gitDir, inv.systemFilesIgnored()); err != nil {
		return accountRepoFailure(err)
	}
	in, err := inv.judgeAccountInstall(ctx, gitDir, name, keepLocal)
	if err != nil {
		return err
	}
	if in.managed != nil && (len(to) > 0 || asCopy) {
		inv.out.warn(sanitised(name) + " takes the place of the managed skill of that name and keeps its placements, so --to and --copy place nothing")
	}
	inv.addForkSource(ctx, name, in.there)
	var done placements
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		return inv.applyAccountInstall(ctx, in, targets, asCopy, &done)
	})
	switch {
	case errors.Is(err, home.ErrMovedBeforeApply):
		return fail(exitRefused, sanitised(name)+" changed while it was being installed, so nothing was changed", "run '"+fromAccountCommand(name, installFlags(in)...)+"' again")
	case err != nil:
		return mutationFailure(err)
	}
	return inv.reportAccountInstall(ctx, in, done)
}

// installFlags is --keep-local when the install was given it, for a hint
// that runs the command again.
func installFlags(in *accountInstall) []string {
	if in.keepLocal {
		return []string{"--keep-local"}
	}
	return nil
}

// judgeAccountInstall reads what installing the fork called name from the
// account remote takes, after the fetch and before the lock, and refuses
// what it cannot do, changing nothing: a fork the account remote does not
// hold, exit code 5; one this machine has a branch of already, a name
// another branch holds whatever its case, a worktree directory in the way,
// and a library path fromAccountPlan refuses, exit code 6; and a managed
// skill it would take the place of whose update merge is pending, exit
// code 4.
func (inv *invocation) judgeAccountInstall(ctx context.Context, gitDir, name string, keepLocal bool) (*accountInstall, error) {
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	tip := tips[name]
	if tip == "" {
		return nil, fail(exitNotFound, "the account remote holds no fork called "+sanitised(name),
			"run 'agentx skill list --remote' to see the forks it holds")
	}
	records, err := inv.listLineage(ctx, gitDir)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	in := &accountInstall{keepLocal: keepLocal}
	if rec, ok := records[name]; ok {
		if rec.Kind == lineage.KindFork {
			return nil, inv.installedRefusal(gitDir, rec)
		}
		in.managed = &rec
		// A pending merge comes before what the library holds: until it is
		// finished or aborted, neither --keep-local nor a revert can help.
		if inv.mergePending(name) {
			return nil, pendingMergeRefusal(name, "replaced by the fork")
		}
	}
	if err := inv.checkForkName(ctx, records, name, name, "remove it first, then run '"+fromAccountCommand(name)+"' again"); err != nil {
		return nil, err
	}
	walked, err := lineage.Walk(ctx, inv.git, gitDir, []string{tip})
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	in.there = walked[tip]
	rec := lineage.Record{Name: name, Kind: lineage.KindFork, Ref: lineage.ForkRef(name), Commit: tip, Fork: &in.there}
	f := inv.forkPlace(gitDir, rec)
	if f.dir, err = inv.tipDir(ctx, gitDir, rec); err != nil {
		return nil, err
	}
	f.skillDir = filepath.Join(f.root, f.dir)
	in.site = f
	if lexists(f.root) {
		return nil, fail(exitRefused, quotedPath(f.root)+" already exists, so "+sanitised(name)+" was not installed",
			"move it aside, then run '"+fromAccountCommand(name, installFlags(in)...)+"' again")
	}
	if in.beside, err = inv.besideSkillDir(ctx, gitDir, tip, f.dir); err != nil {
		return nil, err
	}
	holds, err := inv.judgeAccountLibrary(ctx, gitDir, in)
	if err != nil {
		return nil, err
	}
	if in.managed != nil && holds == holdsNothing {
		return nil, fail(exitRefused, sanitised(name)+" is managed on this machine, and the library no longer holds it",
			"run '"+skillCommand("remove", name)+"' to stop managing it, then run '"+fromAccountCommand(name)+"' again")
	}
	action, f2 := fromAccountPlan(name, f.libPath, holds, keepLocal)
	if f2 != nil {
		return nil, f2
	}
	in.action = action
	if action == installKeepLocal {
		if err := inv.movableLibrary(in); err != nil {
			return nil, err
		}
	}
	return in, nil
}

// installedRefusal refuses to install a fork this machine has a branch of
// already, exit code 6: a pull takes in what the account remote holds of
// it, and skill place lays out one that was never placed here.
func (inv *invocation) installedRefusal(gitDir string, rec lineage.Record) error {
	name := rec.Name
	if classifyFork(inv.forkFactsOf(inv.forkPlace(gitDir, rec))).outcome == outcomeInstallable {
		return fail(exitRefused, sanitised(name)+" is already installed on this machine, but not placed",
			"run '"+skillCommand("place", name)+"' to place it")
	}
	return fail(exitRefused, sanitised(name)+" is already installed on this machine",
		"run '"+pullCommand(name)+"' to take in what the account remote holds of it")
}

// tipDir is the skill directory a fork's tip holds at its root: the one
// directory there, read with one ls-tree. A tip holding none or several is
// the account remote's to sort out, exit code 8, as for a fork placed here.
func (inv *invocation) tipDir(ctx context.Context, gitDir string, rec lineage.Record) (string, error) {
	out, err := inv.git.Isolated(ctx, gitDir, "ls-tree", "-z", "-d", "--name-only", rec.Commit)
	if err != nil {
		return "", accountRepoFailure(err)
	}
	var dirs []string
	for _, d := range strings.Split(out, "\x00") {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) != 1 {
		return "", fail(exitAccountRepo, fmt.Sprintf("the account remote's branch %s holds %d directories at its root, not the one skill directory a fork's branch holds", strings.TrimPrefix(rec.Ref, "refs/heads/"), len(dirs)),
			"fix the branch on the account remote, then run '"+fromAccountCommand(rec.Name)+"' again")
	}
	return dirs[0], nil
}

// judgeAccountLibrary reads what the library path of the fork in.site
// holds, see libraryHolds, capturing it into in first. A managed skill of
// the fork's upstream, the same source and subpath its base version
// records, is judged against its own base version under Git's ignore
// rules, as drift judges it, and the files git ignores in it are kept for
// the worktree.
func (inv *invocation) judgeAccountLibrary(ctx context.Context, gitDir string, in *accountInstall) (libraryHolds, error) {
	libPath := in.site.libPath
	state, err := home.State(libPath)
	if err != nil {
		return 0, libraryFailure(inv.dirs.Library, err)
	}
	in.libState = state
	switch {
	case home.IsAbsent(state):
		return holdsNothing, nil
	case home.IsLink(state):
		if home.IsDangling(libPath) && inv.intoWorktrees(libPath) {
			return holdsNothing, nil
		}
		return holdsLink, nil
	case !home.IsDir(state):
		return holdsFile, nil
	}
	rec := in.managed
	if rec == nil || !rec.HasImport || !sameUpstream(rec.Import, in.there) {
		return holdsDirectory, nil
	}
	t, err := treeid.Read(libPath)
	if err != nil {
		return 0, libraryFailure(inv.dirs.Library, err)
	}
	j, err := inv.judgeDir(ctx, gitDir, libPath, t, baseVersion(*rec), true)
	if err != nil {
		return 0, accountRepoFailure(err)
	}
	if !j.holds {
		return holdsEditedCopy, nil
	}
	in.ignored = j.ignored
	return holdsCopy, nil
}

// sameUpstream reports whether a managed skill's import names the upstream
// the fork whose lineage is there is based on: the same source and the
// same subpath, whatever version of it each holds.
func sameUpstream(imp lineage.Import, there lineage.ForkLineage) bool {
	return there.Base != "" && there.Problem == "" && imp.Source == there.Import.Source && imp.Path == there.Import.Path
}

// movableLibrary refuses a library directory --keep-local cannot move into
// the fork's worktree by a rename: one that is itself a Git repository,
// which git run in the fork would find instead of the fork's branch, and
// one on another file system than agentx home.
func (inv *invocation) movableLibrary(in *accountInstall) error {
	f := in.site
	again := "then run '" + fromAccountCommand(f.name, "--keep-local") + "' again"
	if own := filepath.Join(f.libPath, ".git"); lexists(own) {
		return fail(exitRefused, quotedPath(f.libPath)+" is itself a Git repository, at "+quotedPath(own)+", which git would take for the fork",
			"move its .git out of the directory, "+again)
	}
	same, err := home.SameDevice(filepath.Dir(f.libPath), existingAncestor(inv.worktreesDir()))
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	if !same {
		return fail(exitRefused, "the library and agentx home are on different file systems, so "+quotedPath(f.libPath)+" cannot be moved into "+sanitised(f.name)+"'s worktree",
			"keep AGENTX_HOME and AGENTX_LIBRARY on one file system, or move "+quotedPath(f.libPath)+" aside, "+again)
	}
	return nil
}

// addForkSource adds the upstream source of the fork whose remote lineage
// is there when the settings do not hold it, as source add adds one, so
// that update checks on this machine cover the fork and a later merge can
// prove which upstream version is newer. A source that cannot be added
// does not stop the install: a warning names the command that adds it.
func (inv *invocation) addForkSource(ctx context.Context, name string, there lineage.ForkLineage) {
	if there.Base == "" || there.Problem != "" {
		return
	}
	url := there.Import.Source
	s, err := inv.loadSettings()
	if err != nil || s.FindSource(url) >= 0 {
		return
	}
	if _, _, err := inv.addSource(ctx, source.Source{URL: url}); err != nil {
		reason := err.Error()
		var f *failure
		if errors.As(err, &f) {
			reason = f.message
		}
		inv.out.warn(sanitised(name) + "'s upstream " + sanitised(url) + " could not be fetched, so it was not added: " + reason +
			"; run 'agentx source add " + shellWord(url) + "' to receive its updates")
	}
}

// applyAccountInstall records and applies, under the lock, the install
// judged in in. Every input is read again first, and anything that moved
// refuses with nothing changed: the branches, for a fork branch created
// meanwhile or a managed skill that moved, the worktree directory and the
// library path.
func (inv *invocation) applyAccountInstall(ctx context.Context, in *accountInstall, targets []placeTarget, asCopy bool, done *placements) error {
	f := in.site
	again := "run '" + fromAccountCommand(f.name, installFlags(in)...) + "' again"
	records, err := inv.listLineage(ctx, f.gitDir)
	if err != nil {
		return accountRepoFailure(err)
	}
	if err := takenRefusal(records, f.name, f.name, again); err != nil {
		return err
	}
	rec, held := records[f.name]
	switch {
	case held && rec.Kind == lineage.KindFork:
		return fail(exitRefused, sanitised(f.name)+" was installed by another command meanwhile, so nothing was changed", "run '"+pullCommand(f.name)+"' to take in what the account remote holds of it")
	case held != (in.managed != nil), held && rec.Commit != in.managed.Commit:
		return fail(exitRefused, sanitised(f.name)+"'s import branch changed while the fork was being installed, so nothing was changed", again)
	}
	if in.managed != nil {
		in.managed = &rec // its candidate and marker as they are now
		if inv.mergePending(f.name) {
			return pendingMergeRefusal(f.name, "replaced by the fork")
		}
	}
	if lexists(f.root) {
		return fail(exitRefused, quotedPath(f.root)+" appeared while "+sanitised(f.name)+" was being installed, so nothing was changed", "move it aside, then "+again)
	}
	if state, err := home.State(f.libPath); err != nil {
		return libraryFailure(inv.dirs.Library, err)
	} else if state != in.libState {
		return fail(exitRefused, quotedPath(f.libPath)+" changed while "+sanitised(f.name)+" was being installed, so nothing was changed", again)
	}
	for _, parent := range []string{inv.dirs.Library, inv.worktreesDir()} {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return libraryFailure(parent, err)
		}
	}
	sweepStaged(inv.worktreesDir())
	sweepStaged(inv.dirs.Library)
	place := in.managed == nil
	if place {
		for _, t := range targets {
			if !t.readsLibrary {
				sweepStaged(t.dir)
			}
		}
	}
	link, err := inv.forkLink(f.libPath, f.skillDir)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	m := home.NewMutation(inv.dirs.Home)
	m.Ref(f.gitDir, f.rec.Ref, "", f.rec.Commit)
	m.Worktree(f.gitDir, f.root, f.branch, in.beside...)
	from := f.libPath // where the fork's content is before the journal is applied
	if in.action == installKeepLocal {
		fp, _ := home.DirFingerprint(in.libState)
		m.Move(f.libPath, f.skillDir, fp)
	} else {
		// The content is staged beside the worktree, in the worktrees
		// directory, never inside it, where git would see it as a file of
		// the branch.
		staged := m.Sibling(f.root, "staged")
		carried := ""
		if in.action == installSupersede {
			carried = f.libPath
		}
		fp, err := inv.stageForkContent(ctx, f.gitDir, f.rec.Commit, f.dir, staged, carried, in.ignored)
		if err != nil {
			_ = home.RemoveTree(staged)
			return accountRepoFailure(err)
		}
		m.Publish(f.skillDir, staged, fp)
		if !home.IsAbsent(in.libState) {
			m.Remove(f.libPath, in.libState) // the superseded copy, retained until the journal completes, or a link that led nowhere
		}
		from = staged
	}
	m.Link(f.libPath, link)
	if !holdsSkillFile(from) {
		m.Discard()
		return noAccountSkill(f, in, from)
	}
	if place {
		edit, err := inv.beginSettings()
		if err != nil {
			m.Discard()
			return err
		}
		*done = placements{}
		p := dirPlaceable(f.name, from)
		for _, t := range targets {
			inv.stagePlacement(m, p, t, f.libPath, asCopy, edit.copiesOf(f.name), done)
		}
		edit.addCopies(f.name, done.copies)
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
	} else {
		// The managed skill goes once everything else is in place: its
		// import branch, the update its last check found and the marker of
		// an upstream that no longer holds it, all deleted last.
		m.Ref(f.gitDir, in.managed.Ref, in.managed.Commit, "")
		if c := in.managed.Candidate; c != nil {
			m.Ref(f.gitDir, lineage.CandidateRef(f.name), c.Commit, "")
		}
		if in.managed.UpstreamRemoved != "" {
			m.Ref(f.gitDir, lineage.UpstreamRemovedRef(f.name), in.managed.UpstreamRemoved, "")
		}
	}
	// The branch tracks the account remote's, for git status in the
	// worktree. It is set before the journal is written, so that an
	// install a later command finishes tracks it too; a section left for a
	// branch the journal never makes is harmless and set again by the next
	// install. agentx reads the remote-tracking branch by its name and never
	// needs it, so a failure is a warning and the install goes on.
	if err := inv.git.SetTracking(ctx, f.gitDir, f.branch); err != nil {
		inv.out.warn("could not set " + f.branch + " to track the account remote's branch: " + trimGit(err.Error()))
	}
	return m.Apply(inv.refs(ctx))
}

// noAccountSkill refuses to install the fork f when the directory it would
// be installed from, dir, holds no SKILL.md: no listing would show it as a
// skill. Nothing is changed.
func noAccountSkill(f forkSite, in *accountInstall, dir string) error {
	if in.action == installKeepLocal {
		return fail(exitNotFound, quotedPath(f.libPath)+" holds no SKILL.md, so "+sanitised(f.name)+" was not installed and nothing was changed",
			"add a SKILL.md to "+quotedPath(f.libPath)+", then run '"+fromAccountCommand(f.name, "--keep-local")+"' again")
	}
	return fail(exitNotFound, "the account remote's branch "+sanitised(f.branch)+" holds no SKILL.md in "+sanitised(f.dir)+", so there is no skill to install and nothing was changed",
		"commit a SKILL.md to "+sanitised(f.dir)+" on the machine that published "+sanitised(f.name)+" and publish it, then run '"+fromAccountCommand(f.name)+"' again")
}

// reportAccountInstall reads the machine again and reports the installed
// fork: its library_skill event, a confirmation that says what became of
// what the library path held, and for a fork new to this machine one row
// per configuration it was placed in.
func (inv *invocation) reportAccountInstall(ctx context.Context, in *accountInstall, done placements) error {
	f := in.site
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	lib, ok := librarySkill(inv.dirs.Library, f.name)
	if !ok {
		return fail(exitInternal, "the library holds no "+f.name+" after installing it", "run 'agentx doctor' and check the library it names")
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	var covered []string
	if in.managed == nil {
		covered = targetIDs(done.placed)
	}
	ev := sc.librarySkillEventFor(ctx, inv, snap, lib, covered)
	inv.out.emit(ev)
	out, name := inv.out, sanitised(f.name)
	line := "installed " + out.paint(heading, name) + " from the account remote at " + short(f.rec.Commit)
	inv.summary = "installed " + name + " from the account remote"
	var kept string
	switch {
	case in.action == installKeepLocal:
		kept = quotedPath(f.libPath) + " was moved into its worktree, and its content is uncommitted edits of the fork"
	case in.managed != nil:
		kept = "the fork replaces the managed skill wherever it was"
	}
	if in.managed != nil {
		out.done(line + "; " + kept)
		inv.summary += "; " + kept
		return nil
	}
	rows := inv.ownPlacements(f.name, done.placed, ev.Placements)
	line += ": " + out.paint(noteStyle, plural(len(rows), "placement"))
	inv.summary += " in " + plural(len(done.placed), "configuration")
	if n := len(done.skipped); n > 0 {
		line += ", " + out.paint(warnStyle, plural(n, "placement")+" skipped")
		inv.summary += ", " + plural(n, "placement") + " skipped"
	}
	if kept != "" {
		line += "; " + kept
		inv.summary += "; " + kept
	}
	out.done(line)
	inv.printPlacementRows(f.name, rows)
	inv.printUniversal(ev.Universal)
	inv.summary += universalClause(ev.Universal)
	return nil
}
