package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// A skill of the account remote, one another machine published, is
// installable here once a fetch of the account remote has written its
// remote-tracking branch and this machine has no branch of its own by that
// name. skill list --remote lists them, each with the provenance its own
// commits record, and skill add with no source installs them, named with
// --name or every one with --all: the local branch is created at the
// remote-tracking branch's commit, so the skill is the same fork, with the
// same fork id and history, and what is committed here is published back
// to the same branch. Nothing is copied outside git: the worktree is added
// for the branch and its skill directory laid out from the commit, as a
// fork put back by skill place is.

// installableSkillEvent is one of your own skills the account remote holds
// that this machine has no branch of, as skill list --remote lists it. Its
// source is the account remote; its upstream, a forked skill's, is where
// it came from.
type installableSkillEvent struct {
	event
	Name            string  `json:"name"`
	ForkID          string  `json:"fork_id"`            // the skill's permanent id; a branch whose history records none is not listed
	Commit          string  `json:"commit"`             // the remote tip
	Source          string  `json:"source"`             // the canonical URL of the account remote
	Upstream        string  `json:"upstream,omitempty"` // the canonical URL a forked skill came from
	UpstreamSubpath *string `json:"upstream_subpath,omitempty"`
	UpstreamCommit  string  `json:"upstream_commit,omitempty"`
	BaseHash        string  `json:"base_hash,omitempty"`
}

// installRemoteRow is what the human listing says, in the state column the
// library's rows use, of one of your own skills it can install from the
// account remote.
const installRemoteRow = "installable"

// installableSkills reads your own skills that the account remote, the
// settings entry account, holds and records, the account repo's branches,
// hold no branch of, sorted by name, with the provenance of each read by one
// walk of their remote tips. A branch whose name agentx would never give
// a fork, or whose history records no fork id, is left out, with a
// warning each: installing the one could not create the local branch, and
// the other could never be updated or published, since nothing would tell
// that the two branches are one fork. A branch that is a skill this
// machine holds under another name, see heldUnderAnotherName, is left out
// silently: it is the old name of a skill renamed here.
func (inv *invocation) installableSkills(ctx context.Context, gitDir string, account home.Source, records map[string]lineage.Record) ([]installableSkillEvent, []string, error) {
	remote := source.RemoteName(source.ID(account.URL))
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return nil, nil, accountRepoFailure(err)
	}
	var candidates, warnings []string
	var walk []string
	for name, tip := range tips {
		switch {
		case records[name].Kind == lineage.KindFork:
		case forkNameRefusal(name) != "":
			warnings = append(warnings, "the account remote's branch skills/"+sanitised(name)+" is left out, since it cannot be installed: "+forkNameRefusal(name))
		default:
			candidates = append(candidates, name)
			walk = append(walk, tip)
		}
	}
	walked, err := lineage.Walk(ctx, inv.git, gitDir, walk)
	if err != nil {
		return nil, nil, accountRepoFailure(err)
	}
	local := map[string][]string{}
	for _, rec := range records {
		if rec.Kind == lineage.KindFork && rec.Fork != nil && rec.Fork.ID != "" {
			local[rec.Fork.ID] = append(local[rec.Fork.ID], rec.Commit)
		}
	}
	ancestor := func(a, b string) bool {
		held, err := inv.isAncestor(ctx, gitDir, a, b)
		if err != nil {
			inv.out.debugf("%s: %v", a, err)
		}
		return held
	}
	var names []string
	for _, name := range candidates {
		id := walked[tips[name]].ID
		switch {
		case id == "":
			warnings = append(warnings, "the account remote's branch skills/"+sanitised(name)+" is left out, since its history records no fork id")
		case !heldUnderAnotherName(local, id, tips[name], ancestor):
			names = append(names, name)
		}
	}
	sort.Strings(names)
	sort.Strings(warnings)
	events := make([]installableSkillEvent, 0, len(names))
	for _, name := range names {
		l := walked[tips[name]]
		ev := installableSkillEvent{event: newEvent("installable_skill"), Name: name, ForkID: l.ID, Commit: tips[name]}
		_, ev.Source, _, ev.Upstream, ev.UpstreamSubpath = skillOrigin(lineage.Record{Kind: lineage.KindFork, Fork: &l}, account.URL)
		if l.Base != "" {
			ev.UpstreamCommit, ev.BaseHash = l.Import.Commit, l.Import.Hash
		}
		events = append(events, ev)
	}
	return events, warnings, nil
}

// heldUnderAnotherName reports whether the account remote's branch whose
// fork id is id and whose tip is tip is a skill this machine already holds
// under another name: one of its skills, whose tips local holds by fork
// id, holds id, and that skill's tip is tip or has it in its history, as
// ancestor(tip, local tip) says. That is the old name's branch of a skill
// renamed here, before or after a publish of the rename; a branch of the
// same fork id that holds changes none of them holds, as the renamed
// skill's branch does for a machine that still holds the old name, is
// not. Pure but for ancestor.
func heldUnderAnotherName(local map[string][]string, id, tip string, ancestor func(a, b string) bool) bool {
	for _, l := range local[id] {
		if l == tip || ancestor(tip, l) {
			return true
		}
	}
	return false
}

// installableRow is one line of the human listing for one of your own
// skills the account remote holds and this machine does not: its name, the
// kind and state it is listed with, and where it is published to, the
// account remote.
func installableRow(out *writer, ev installableSkillEvent) []cell {
	return []cell{c("  "+sanitised(ev.Name), heading), c(lineage.KindManaged, muted), c(installRemoteRow, infoStyle), sourceCell(true, ev.Source, nil, ev.Upstream, ev.UpstreamSubpath, ev.Source)}
}

// accountAddCommand is the command line that installs the skill called
// name from the account remote.
func accountAddCommand(name string) string {
	return "agentx skill add --name " + shellWord(name)
}

// listRemoteHint is the hint of a refusal to install a name the account
// remote does not offer.
const listRemoteHint = "run 'agentx skill list --remote' to see the skills the account remote holds"

// libraryHolds is what the library path of a skill being installed from
// the account remote holds, as fromAccountPlan decides on it.
type libraryHolds int

const (
	holdsNothing   libraryHolds = iota // nothing, or a symlink into the worktrees directory that leads nowhere
	holdsCopy                          // a managed skill of the fork's upstream, its library directory holding the base version
	holdsDirectory                     // any other directory: an unmanaged skill, an edited copy, or a managed skill of another upstream
	holdsLink                          // a symlink that is not a fork's whose worktree is gone
	holdsFile                          // a file, or anything else that is neither a directory nor a symlink
)

// accountAction is what an install from the account remote does at the
// library path.
type accountAction int

const (
	installFresh     accountAction = iota // lay the fork out and link the library path to it
	installSupersede                      // as installFresh, the managed copy's directory retired and its files git ignores carried in
)

// fromAccountPlan decides, with no I/O, what installing the skill called
// name from the account remote does with what its library path, libPath,
// holds, and refuses what it cannot take: a managed copy of the skill's
// upstream that still holds its base version is superseded; a directory of
// anything else, an edited copy included, a symlink and a file are refused,
// since installing would lose what they hold.
func fromAccountPlan(name, libPath string, holds libraryHolds) (accountAction, *failure) {
	switch holds {
	case holdsNothing:
		return installFresh, nil
	case holdsCopy:
		return installSupersede, nil
	case holdsDirectory:
		return 0, refuse(exitRefused, "the library already holds "+quotedPath(libPath)+", so "+sanitised(name)+" was not installed",
			"move it aside, or run '"+skillCommand("remove", name)+"', then run '"+accountAddCommand(name)+"' again")
	case holdsLink:
		return 0, refuse(exitRefused, "the library already holds "+quotedPath(libPath)+", a symlink, so "+sanitised(name)+" was not installed",
			"remove the link, then run '"+accountAddCommand(name)+"' again")
	}
	return 0, refuse(exitRefused, quotedPath(libPath)+" is neither a directory nor a symlink, so "+sanitised(name)+" was not installed",
		"move it aside, then run '"+accountAddCommand(name)+"' again")
}

// accountInstall is one skill being installed from the account remote, as
// the command found it before the lock and checks again under it.
type accountInstall struct {
	site     forkSite            // the fork as it will be: rec is its branch at the remote tip
	beside   []string            // what the tip holds at its root beside the skill directory
	there    lineage.ForkLineage // the remote tip's lineage
	action   accountAction
	managed  *lineage.Record // the managed skill of the name the fork takes the place of, nil for none
	libState string          // what the library path held, as captured before it was judged
	ignored  []string        // the files git ignores in a superseded copy, carried into the worktree
	remote   string          // the account remote's git remote, which the fork's branch tracks
}

// addFromAccount is skill add with no source: it fetches the account
// remote once, then installs the skills of it the selection names, or with
// --all every one this machine lacks but those --except names, one after
// the other, each in a journaled mutation of its own, see installOne. A
// skill that cannot be installed does not cost the others: the run goes
// on, and answers with what failed, as an install from a source does.
func (inv *invocation) addFromAccount(ctx context.Context, sel selection, to []string, asCopy, fetch bool) error {
	switch {
	case fetch:
		return fail(exitUsage, "--fetch applies to a source; the account remote is fetched every time", "run the command again without --fetch")
	case !sel.all && len(sel.names) == 0:
		return fail(exitUsage, "skill add needs a source, or --name or --all to install from the account remote", listRemoteHint)
	}
	var names []string
	for _, name := range sel.names {
		if containsString(names, name) {
			continue
		}
		if refusal := forkNameRefusal(name); refusal != "" {
			return fail(exitRefused, refusal, listRemoteHint)
		}
		names = append(names, name)
	}
	targets, err := inv.placementTargets(to)
	if err != nil {
		return err
	}
	gitDir, entry, remote, err := inv.accountRemote(ctx)
	if err != nil {
		return err
	}
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	if err := inv.fetchRemote(ctx, gitDir, remote, entry.URL); err != nil {
		return err
	}
	if err := home.SyncExclude(gitDir, inv.systemFilesIgnored()); err != nil {
		return accountRepoFailure(err)
	}
	if sel.all {
		if names, err = inv.everyAccountSkillBut(ctx, gitDir, entry, sel.except); err != nil || len(names) == 0 {
			return err
		}
	}
	run := refusals{verb: "installed", mixed: "run 'agentx skill list --remote' to see what the account remote holds, then install the rest one at a time"}
	var installed []string
	for _, name := range names {
		err := inv.installOne(ctx, gitDir, remote, name, targets, to, asCopy)
		var f *failure
		switch {
		case err == nil:
			installed = append(installed, sanitised(name))
			continue
		case ctx.Err() != nil || !errors.As(err, &f):
			return err
		}
		run.add(name, f)
		if len(names) > 1 {
			inv.out.warn(namedReason(name, f.message))
		}
	}
	if len(names) > 1 {
		inv.summary = "installed " + strconv.Itoa(len(installed)) + " of " + plural(len(names), "skill") + " from the account remote"
		if len(installed) > 0 {
			inv.summary += ": " + strings.Join(installed, ", ")
		}
	}
	if f := run.failure(len(names), len(installed)); f != nil {
		return f
	}
	return nil
}

// everyAccountSkillBut is --all with no source: every skill the account
// remote holds that this machine lacks, as skill list --remote lists them,
// less those except names, matched exactly. A branch that cannot be
// installed is left out with a warning. An --except naming no such skill is
// refused, exit code 5, even when the account remote holds nothing to
// install; otherwise such an account remote says so and installs nothing,
// which is no failure, and an --except that leaves nothing is exit code 1.
func (inv *invocation) everyAccountSkillBut(ctx context.Context, gitDir string, account home.Source, except []string) ([]string, error) {
	records, err := inv.forkRecords(ctx, gitDir)
	if err != nil {
		return nil, err
	}
	installable, warnings, err := inv.installableSkills(ctx, gitDir, account, records)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		inv.out.warn(w)
	}
	var unknown []string
	for _, name := range except {
		if !containsString(unknown, name) && !slices.ContainsFunc(installable, func(ev installableSkillEvent) bool { return ev.Name == name }) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return nil, fail(exitNotFound, "the account remote holds no skill this machine lacks called "+quoted(unknown), listRemoteHint)
	}
	if len(installable) == 0 {
		inv.out.print("The account remote holds no skill this machine lacks.")
		inv.summary = "the account remote holds no skill this machine lacks"
		return nil, nil
	}
	var names []string
	for _, ev := range installable {
		if !containsString(except, ev.Name) {
			names = append(names, ev.Name)
		}
	}
	if len(names) == 0 {
		return nil, fail(exitUsage, "--except left no skill to install", "drop one of the --except names")
	}
	return names, nil
}

// installOne installs the skill called name from the account remote, the
// git remote called remote, which the run has fetched, see accountInstall,
// in one journaled mutation: the local branch at the remote tip, its
// worktree, the skill directory laid out from the tip, the library
// symlink, and the placements of a skill new to this machine. A managed
// skill it takes the place of keeps its placements, which lead to the
// library path and so to the fork, and its import branch, update candidate
// and upstream-removed marker are deleted last. The skill's upstream source
// is added first when the settings lack it, as source add adds one, so that
// update checks cover it here as on the machine that published it. The
// branch is set to track the account remote's just before the journal is
// written.
func (inv *invocation) installOne(ctx context.Context, gitDir, remote, name string, targets []placeTarget, to []string, asCopy bool) error {
	in, err := inv.judgeAccountInstall(ctx, gitDir, remote, name)
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
		return fail(exitRefused, sanitised(name)+" changed while it was being installed, so nothing was changed", "run '"+accountAddCommand(name)+"' again")
	case err != nil:
		return mutationFailure(err)
	}
	return inv.reportAccountInstall(ctx, in, done)
}

// judgeAccountInstall reads what installing the skill called name from the
// account remote, the git remote called remote, takes, after the fetch and
// before the lock, and refuses what it cannot do, changing nothing: a skill
// the account remote does not hold, and a content to install that holds no
// SKILL.md, exit code 5; one this machine has a branch of already, a name
// another branch holds whatever its case, a remote history that records no
// fork id, a worktree directory in the way, and a library path
// fromAccountPlan refuses, exit code 6; and a managed skill it would take
// the place of whose update merge is pending, exit code 4. Every refusal
// comes before the fork's upstream source is added, which is a change of
// its own.
func (inv *invocation) judgeAccountInstall(ctx context.Context, gitDir, remote, name string) (*accountInstall, error) {
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	tip := tips[name]
	if tip == "" {
		return nil, fail(exitNotFound, "the account remote holds no skill called "+sanitised(name), listRemoteHint)
	}
	records, err := inv.listLineage(ctx, gitDir)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	in := &accountInstall{remote: remote}
	if rec, ok := records[name]; ok {
		if rec.Kind == lineage.KindFork {
			return nil, inv.installedRefusal(gitDir, rec)
		}
		in.managed = &rec
		// A pending merge comes before what the library holds: until it is
		// finished or aborted, the library directory is the merge's.
		if inv.mergePending(name) {
			return nil, pendingMergeRefusal(name, "replaced by the account remote's skill")
		}
	}
	if err := inv.checkForkName(ctx, records, name, name, "remove it first, then run '"+accountAddCommand(name)+"' again"); err != nil {
		return nil, err
	}
	walked, err := lineage.Walk(ctx, inv.git, gitDir, []string{tip})
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	in.there = walked[tip]
	if in.there.ID == "" {
		// The account step and a publish refuse a fork whose two histories
		// do not both record its id, so a fork installed from this branch
		// could never sync.
		return nil, fail(exitRefused, "the history of the account remote's skills/"+sanitised(name)+" records no fork id, so nothing tells which skill it is and "+sanitised(name)+" cannot be installed",
			"publish it from a skill agentx made, one 'agentx skill new' or 'agentx skill fork' creates, on the machine that holds it")
	}
	rec := lineage.Record{Name: name, Kind: lineage.KindFork, Ref: lineage.ForkRef(name), Commit: tip, Fork: &in.there}
	f := inv.forkPlace(gitDir, rec)
	if f.dir, err = inv.tipDir(ctx, gitDir, rec); err != nil {
		return nil, err
	}
	f.skillDir = filepath.Join(f.root, f.dir)
	in.site = f
	if lexists(f.root) {
		return nil, fail(exitRefused, quotedPath(f.root)+" already exists, so "+sanitised(name)+" was not installed",
			"move it aside, then run '"+accountAddCommand(name)+"' again")
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
			"run '"+skillCommand("remove", name)+"' to stop managing it, then run '"+accountAddCommand(name)+"' again")
	}
	action, f2 := fromAccountPlan(name, f.libPath, holds)
	if f2 != nil {
		return nil, f2
	}
	in.action = action
	if ok, err := inv.tipHoldsSkill(ctx, gitDir, tip, f.dir); err != nil {
		return nil, err
	} else if !ok {
		return nil, noAccountSkill(f)
	}
	return in, nil
}

// tipHoldsSkill reports whether the commit tip holds a SKILL.md file in
// its directory dir, read with one ls-tree: what makes the directory laid
// out from it a skill.
func (inv *invocation) tipHoldsSkill(ctx context.Context, gitDir, tip, dir string) (bool, error) {
	out, err := inv.git.Isolated(ctx, gitDir, "ls-tree", "-z", tip, "--", dir+"/SKILL.md")
	if err != nil {
		return false, accountRepoFailure(err)
	}
	meta, _, _ := strings.Cut(strings.TrimSuffix(out, "\x00"), "\t")
	fields := strings.Fields(meta)
	return len(fields) == 3 && fields[1] == "blob" && (fields[0] == "100644" || fields[0] == "100755"), nil
}

// installedRefusal refuses to install a fork this machine has a branch of
// already, exit code 6: an update takes in what the account remote holds of
// it, and skill place lays out one that was never placed here.
func (inv *invocation) installedRefusal(gitDir string, rec lineage.Record) error {
	name := rec.Name
	if classifyFork(inv.forkFactsOf(inv.forkPlace(gitDir, rec))).outcome == outcomeInstallable {
		return fail(exitRefused, sanitised(name)+" is already installed on this machine, but not placed",
			"run '"+skillCommand("place", name)+"' to place it")
	}
	return fail(exitRefused, sanitised(name)+" is already installed on this machine",
		"run '"+skillCommand("update", name)+"' to take in what the account remote holds of it")
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
		return "", fail(exitAccountRepo, fmt.Sprintf("the account remote's branch %s holds %d directories at its root, not the one skill directory a skill's branch holds", strings.TrimPrefix(rec.Ref, "refs/heads/"), len(dirs)),
			"fix the branch on the account remote, then run '"+accountAddCommand(rec.Name)+"' again")
	}
	return dirs[0], nil
}

// judgeAccountLibrary reads what the library path of the fork in.site
// holds, see libraryHolds, capturing it into in first. A managed skill of
// the fork's upstream, the same source and subpath its base version
// records, is judged against its own base version under Git's ignore
// rules, as drift judges it, and the files git ignores in it are kept for
// the worktree; one with edits is a directory like any other.
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
		return holdsDirectory, nil
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
		again := "; run 'agentx source add " + shellWord(url) + "' to receive its updates"
		if ctx.Err() != nil { // a stop, not the upstream, is what kept it out
			inv.out.warn(sanitised(name) + "'s upstream " + sanitised(url) + " was not added because the run was stopped" + again)
			return
		}
		reason := err.Error()
		var f *failure
		if errors.As(err, &f) {
			reason = f.message
		}
		inv.out.warn(sanitised(name) + "'s upstream " + sanitised(url) + " could not be fetched, so it was not added: " + reason + again)
	}
}

// applyAccountInstall records and applies, under the lock, the install
// judged in in. Every input is read again first, and anything that moved
// refuses with nothing changed: the branches, for a fork branch created
// meanwhile or a managed skill that moved, the worktree directory and the
// library path.
func (inv *invocation) applyAccountInstall(ctx context.Context, in *accountInstall, targets []placeTarget, asCopy bool, done *placements) error {
	f := in.site
	again := "run '" + accountAddCommand(f.name) + "' again"
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
		return fail(exitRefused, sanitised(f.name)+" was installed by another command meanwhile, so nothing was changed", "run '"+skillCommand("update", f.name)+"' to take in what the account remote holds of it")
	case held != (in.managed != nil), held && rec.Commit != in.managed.Commit:
		return fail(exitRefused, sanitised(f.name)+"'s import branch changed while it was being installed, so nothing was changed", again)
	}
	if in.managed != nil {
		in.managed = &rec // its candidate and marker as they are now
		if inv.mergePending(f.name) {
			return pendingMergeRefusal(f.name, "replaced by the account remote's skill")
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
	// The content is staged beside the worktree, in the worktrees
	// directory, never inside it, where git would see it as a file of the
	// branch.
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
	m.Link(f.libPath, link)
	if !holdsSkillFile(staged) { // judged before the lock, and read again from what was staged
		m.Discard()
		return noAccountSkill(f)
	}
	if place {
		edit, err := inv.beginSettings()
		if err != nil {
			m.Discard()
			return err
		}
		*done = placements{}
		p := dirPlaceable(f.name, staged)
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
	if err := inv.git.SetTracking(ctx, f.gitDir, in.remote, f.branch); err != nil {
		inv.out.warn("could not set " + f.branch + " to track the account remote's branch: " + trimGit(err.Error()))
	}
	return m.Apply(inv.refs(ctx))
}

// noAccountSkill refuses to install the skill f when what it would be
// installed from, the remote tip's skill directory, holds no SKILL.md: no
// listing would show it as a skill. Nothing is changed.
func noAccountSkill(f forkSite) error {
	return fail(exitNotFound, "the account remote's branch "+sanitised(f.branch)+" holds no SKILL.md in "+sanitised(f.dir)+", so there is no skill to install and nothing was changed",
		"add a SKILL.md to "+sanitised(f.dir)+" on the machine that published "+sanitised(f.name)+" and run '"+publishCommand(f.name)+"' there, then run '"+accountAddCommand(f.name)+"' again")
}

// reportAccountInstall reads the machine again and reports the installed
// skill: its library_skill event, a confirmation that says what became of
// a managed skill it replaced, and for a skill new to this machine one row
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
	if in.managed != nil {
		const kept = "it replaces the managed skill wherever it was"
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
	out.done(line)
	inv.printPlacementRows(f.name, rows)
	inv.printUniversal(ev.Universal)
	inv.summary += universalClause(ev.Universal)
	return nil
}
