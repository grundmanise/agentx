package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// forkPlacing is what skill place does to a fork beside its placements:
// the repairs reconciliation would make, see classifyFork, and, with
// --force, the adoption of what is in the way.
type forkPlacing struct {
	v forkVerdict // the repairs: the worktree, the skill directory, the library symlink
	// adoptRoot adopts the directory at the worktree that git does not
	// register: its skill directory is moved aside, the worktree added in
	// its place, and the skill directory moved back, its content then the
	// fork's uncommitted edits.
	adoptRoot bool
	// adoptLib moves the directory at the library entry into the worktree
	// as the fork's skill directory, its content then the fork's
	// uncommitted edits.
	adoptLib bool
	// retire is what the worktree's skill directory holds when adoptLib
	// replaces it, "" when there is none to replace: it holds the branch
	// tip, as judged, and is retained until the journal completes.
	retire   string
	libState string // what the library entry holds, as judged
	dirState string // what the skill directory of an orphaned worktree holds, as judged
}

// changes reports whether the plan changes anything of the fork itself,
// beside its placements.
func (p forkPlacing) changes() bool { return p.v.repairs() || p.adoptRoot || p.adoptLib }

// contentAt is the directory the placements of the fork f are made from
// under the plan, as it is before the plan is carried out, or "" when the
// plan lays the content out from the branch tip, which is only on disk once
// it is staged.
func (p forkPlacing) contentAt(f forkSite) string {
	switch {
	case p.adoptLib:
		return f.libPath
	case p.v.content:
		return ""
	}
	return f.skillDir
}

// noForkSkill refuses to place the fork f when the directory its placements
// would be made from, dir, holds no SKILL.md: it is no skill by the rule the
// library lists one by, so a placement of it would be one no listing shows.
// When the plan lays the content out from the branch tip, dir is where it
// was staged, and the branch is what lacks the file.
func noForkSkill(f forkSite, plan forkPlacing, dir string, flags []string) error {
	if plan.adoptRoot || plan.adoptLib {
		flags = append(slices.Clip(flags), "--force")
	}
	name, again := sanitised(f.name), "run '"+skillCommand("place", f.name, flags...)+"' again"
	if plan.v.content {
		return fail(exitNotFound, name+"'s branch "+sanitised(f.branch)+" holds no SKILL.md in "+sanitised(f.dir)+", so there is no skill to place and nothing was changed",
			"commit a SKILL.md to "+sanitised(f.dir)+" on "+sanitised(f.branch)+" in "+quotedPath(f.gitDir)+", then "+again)
	}
	return fail(exitNotFound, quotedPath(dir)+" holds no SKILL.md, so "+name+" is no skill to place and nothing was changed",
		"add a SKILL.md to "+quotedPath(dir)+", then "+again)
}

// placeFork places a fork, and first puts back what it needs to be placed
// on this machine, as reconciliation at serve start does: a worktree whose
// pointers moved with agentx home is repaired with git, and a missing
// worktree, skill directory or library symlink is put back from the
// branch, the tip laid out as the account repo holds it. A fork never
// placed on this machine, whose branch the account repo holds, is checked
// out the same way. Then each configuration of targets gets its placement,
// as an install places one, skipping what it finds at a place that is not
// the placement.
//
// What is in the way of the fork, an adopt candidate, is refused, since it
// is the user's, unless --force says to adopt it, which keeps every file:
// a directory at the worktree that git does not register keeps its
// content in place and becomes the fork's worktree, and a directory at the
// library entry is moved into the worktree as the fork's skill directory,
// replacing one there only when it holds the branch tip. Either content
// then reads as uncommitted edits of the fork, which skill commit keeps.
// A symlink of the user's at the library entry
// holds nothing, and --force replaces it. --force with nothing in the way
// is refused, as it always was for a fork.
func (inv *invocation) placeFork(ctx context.Context, rec lineage.Record, targets []placeTarget, asCopy, force bool, flags []string) error {
	gitDir := gitx.AccountRepoPath(inv.dirs.Home)
	f := inv.forkPlace(gitDir, rec)
	if err := inv.findForkDir(ctx, &f); err != nil {
		return err
	}
	// A worktree whose pointers moved is repaired under the lock before it
	// is judged: what it holds is known only once git can read it.
	if facts := inv.forkFactsOf(f); facts.root != rootMoved {
		plan, err := inv.planForkPlace(ctx, f, facts, force, flags)
		if err != nil {
			return err
		}
		if dir := plan.contentAt(f); dir != "" && !holdsSkillFile(dir) {
			return noForkSkill(f, plan, dir, flags)
		}
	}
	var done placements
	var plan forkPlacing
	repointed := false // the worktree's pointers were repaired
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		values, err := inv.git.Refs(ctx).RefValues(gitDir, []string{rec.Ref})
		if err != nil {
			return accountRepoFailure(err)
		}
		if values[rec.Ref] != rec.Commit {
			return fail(exitRefused, rec.Name+" changed while it was being placed, so nothing was changed", "run '"+skillCommand("place", rec.Name, flags...)+"' again")
		}
		facts := inv.forkFactsOf(f)
		if facts.root == rootMoved {
			// git's own repair, safe to repeat, so it runs outside the
			// journal; one git cannot make leaves the worktree as it is.
			if err := inv.git.RepairWorktrees(ctx, gitDir, []string{f.root}); err != nil {
				inv.out.debugf("worktree repair: %v", err)
			}
			repointed = !home.PointersMoved(f.root)
			if facts = inv.forkFactsOf(f); facts.root == rootMoved {
				facts.root = rootOrphan // git could not repair it, so it is a directory of the user's
			}
		}
		if plan, err = inv.planForkPlace(ctx, f, facts, force, flags); err != nil {
			return err
		}
		sweepStaged(inv.worktreesDir())
		for _, t := range targets {
			if !t.readsLibrary {
				sweepStaged(t.dir)
			}
		}
		m := home.NewMutation(inv.dirs.Home)
		from, err := inv.stageForkPlace(ctx, m, f, plan)
		if err != nil {
			m.Discard()
			return err
		}
		if !holdsSkillFile(from) {
			m.Discard()
			return noForkSkill(f, plan, from, flags)
		}
		edit, err := inv.beginSettings()
		if err != nil {
			m.Discard()
			return err
		}
		done = placements{}
		p := dirPlaceable(rec.Name, from)
		for _, t := range targets {
			inv.stagePlacement(m, p, t, f.libPath, asCopy, edit.copiesOf(rec.Name), &done)
		}
		edit.addCopies(rec.Name, done.copies)
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return mutationFailure(err)
	}
	if err := inv.reportPlaced(ctx, rec.Name, targets, done); err != nil {
		return err
	}
	inv.reportForkPlaced(f, plan, repointed)
	return nil
}

// planForkPlace decides what skill place does to the fork f, whose facts
// were just read, beside its placements, and refuses what it cannot do:
// an adopt candidate without --force, --force with nothing in the way, and
// what even --force does not adopt. It reads the directories it would
// move, and judges a skill directory an adopted library directory would
// replace against the branch tip.
func (inv *invocation) planForkPlace(ctx context.Context, f forkSite, facts forkFacts, force bool, flags []string) (forkPlacing, error) {
	name, again := sanitised(f.name), "run '"+skillCommand("place", f.name, flags...)+"' again"
	v := classifyFork(facts)
	if v.outcome == outcomeInstallable {
		// Never placed on this machine: placing it checks it out.
		v = forkVerdict{outcome: outcomeWorktreeMissing, worktree: true, content: true, link: true}
	}
	var plan forkPlacing
	switch {
	case v.outcome != outcomeAdoptCandidate && force:
		return plan, fail(exitRefused, name+" is a fork with nothing in the way of its worktree or its library symlink, so --force has nothing to adopt",
			"place it without --force with '"+skillCommand("place", f.name, flags...)+"'")
	case v.outcome == outcomeAdoptCandidate && !force:
		what, wayOut := adoptRefusal(f, v, facts)
		return plan, fail(exitRefused, what+", so nothing was placed", wayOut)
	case v.outcome == outcomeWorktreeMissing && !v.repairs():
		return plan, worktreeHealth(f.name, f.root, f.branch)
	case v.outcome != outcomeAdoptCandidate:
		plan.v = v
		return plan, nil
	}
	var err error
	if plan.libState, err = home.State(f.libPath); err != nil {
		return plan, libraryFailure(inv.dirs.Library, err)
	}
	// What the library entry is decides the rest: a symlink of the user's
	// is replaced, a directory is moved into the worktree, and either way
	// the worktree is then judged as if the entry were not there.
	rest := facts
	rest.lib = libAbsent
	switch facts.lib {
	case libForeign:
		if !home.IsLink(plan.libState) {
			return plan, fail(exitRefused, quotedPath(f.libPath)+" is neither a directory nor a symlink, so "+name+" cannot adopt it", "move it aside, then "+again)
		}
	case libDir:
		plan.adoptLib = true
		if err := inv.adoptableLibrary(f); err != nil {
			return plan, err
		}
		switch {
		case facts.root == rootOrphan:
			return plan, fail(exitRefused, quotedPath(f.libPath)+" and "+quotedPath(f.root)+" are both in the way of "+name+", and only one of them can be adopted",
				"move one of them aside, then "+again)
		case facts.root == rootWorktree && (!facts.onBranch || facts.busy):
			// Adding the worktree again aligns its index with the
			// branch, which a git stopped part way in it would lose.
			return plan, worktreeHealth(f.name, f.root, f.branch)
		case facts.root == rootWorktree && facts.skillDir:
			if plan.retire, err = inv.retirable(ctx, f, again); err != nil {
				return plan, err
			}
		}
		plan.v = forkVerdict{outcome: outcomeAdoptCandidate, worktree: true, link: true}
		return plan, nil
	}
	rv := classifyFork(rest)
	if rv.outcome != outcomeAdoptCandidate {
		if rv.outcome == outcomeWorktreeMissing && !rv.repairs() {
			return plan, worktreeHealth(f.name, f.root, f.branch)
		}
		if rv.outcome == outcomeInstallable {
			rv = forkVerdict{outcome: outcomeWorktreeMissing, worktree: true, content: true}
		}
		rv.link = true
		plan.v = rv
		return plan, nil
	}
	plan.adoptRoot = true
	if plan.dirState, err = inv.adoptableRoot(f, again); err != nil {
		return plan, err
	}
	plan.v = forkVerdict{outcome: outcomeAdoptCandidate, worktree: true, link: facts.lib != libOwn}
	return plan, nil
}

// adoptableLibrary refuses a library directory the fork f cannot take into
// its worktree by a rename: one that is itself a Git repository, which git
// run in the fork would find instead of the fork's branch, and one on
// another file system than agentx home.
func (inv *invocation) adoptableLibrary(f forkSite) error {
	again := "then run '" + skillCommand("place", f.name, "--force") + "' again"
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

// retirable is what the skill directory of the fork f holds when it holds
// the branch tip, as git status in the worktree would say, and nothing git
// does not record, so that a library directory adopted in its place loses
// nothing. One with edits of its own is refused, since only one of the two
// can be the fork's, and so is one holding files git ignores, such as a
// local .env, or cannot record, each named: they would go with the
// directory it replaces. The files ignore_system_files ignores while it is
// on, a .DS_Store Finder left, are no user's and go with it.
func (inv *invocation) retirable(ctx context.Context, f forkSite, again string) (string, error) {
	state, err := home.State(f.skillDir)
	if err != nil {
		return "", libraryFailure(inv.worktreesDir(), err)
	}
	t, err := treeid.Read(f.skillDir)
	if err != nil {
		return "", libraryFailure(inv.worktreesDir(), err)
	}
	j, err := inv.judgeTip(ctx, f, t, true)
	if err != nil {
		return "", accountRepoFailure(err)
	}
	ignored := j.ignored
	if inv.systemFilesIgnored() {
		ignored = slices.DeleteFunc(slices.Clone(ignored), inSystemFile)
	}
	if kept := append(ignored, j.unrecordable...); j.clean && len(kept) > 0 {
		named := make([]string, len(kept))
		for i, p := range kept {
			named[i] = quotedPath(filepath.Join(f.skillDir, filepath.FromSlash(p)))
		}
		return "", fail(exitRefused, sanitised(f.name)+"'s skill directory "+quotedPath(f.skillDir)+" holds what git does not record, "+strings.Join(named, ", ")+", and adopting "+quotedPath(f.libPath)+" in its place would delete it",
			"move what you want to keep into "+quotedPath(f.libPath)+" or elsewhere, then "+again)
	}
	if !j.clean {
		return "", fail(exitRefused, sanitised(f.name)+"'s worktree holds uncommitted edits at "+quotedPath(f.skillDir)+" and "+quotedPath(f.libPath)+" holds a directory of its own, and only one of them can be the fork's",
			"commit the fork's edits with '"+skillCommand("commit", f.name)+"', or move "+quotedPath(f.libPath)+" aside, then "+again)
	}
	return state, nil
}

// inSystemFile reports whether the slash-separated path p is, or lies in,
// one of the files an operating system or an editor leaves on its own, see
// home.SystemFiles.
func inSystemFile(p string) bool {
	return slices.ContainsFunc(strings.Split(p, "/"), home.IsSystemFile)
}

// adoptableRoot is what the skill directory of the orphaned worktree of
// the fork f holds, when the worktree can be adopted in place: it holds
// that directory and at most a .git file beside it, a file whose
// registration is gone, as a worktree whose registration was removed by
// hand leaves it. Anything else at the worktree is refused, named, even
// with --force: git adds a worktree only into a directory that holds
// nothing, and a journal moves directories alone. What the fork's branch
// holds there, such as a committed .gitignore, the adoption checks out
// from the branch once it is moved aside.
func (inv *invocation) adoptableRoot(f forkSite, again string) (string, error) {
	entries, err := os.ReadDir(f.root)
	if err != nil {
		return "", libraryFailure(inv.worktreesDir(), err)
	}
	var other []string
	for _, e := range entries {
		switch {
		case e.Name() == f.dir && e.IsDir():
			if own := filepath.Join(f.skillDir, ".git"); lexists(own) {
				return "", fail(exitRefused, quotedPath(f.skillDir)+" is itself a Git repository, at "+quotedPath(own)+", which git would take for the fork",
					"move its .git out of the directory, then "+again)
			}
		case e.Name() == ".git" && e.Type().IsRegular():
			if holder, held := home.HeldBy(f.root); held {
				return "", fail(exitRefused, quotedPath(filepath.Join(f.root, ".git"))+" names the registration of the worktree "+quotedPath(holder)+", so "+quotedPath(f.root)+" is a copy of it and cannot be adopted",
					"move "+quotedPath(f.root)+" or "+quotedPath(holder)+" aside, then "+again)
			}
			if admin, ok := home.AdminDirOf(f.root); ok && lexists(admin) {
				return "", fail(exitRefused, quotedPath(filepath.Join(f.root, ".git"))+" names "+quotedPath(admin)+", a registration git could not repair, so "+quotedPath(f.root)+" cannot be adopted",
					"run 'git --git-dir="+shellWord(f.gitDir)+" worktree repair "+shellWord(f.root)+"' to see why, then "+again)
			}
		default:
			other = append(other, quotedPath(filepath.Join(f.root, e.Name())))
		}
	}
	if len(other) > 0 {
		return "", fail(exitRefused, quotedPath(f.root)+" holds "+strings.Join(other, ", ")+" beside "+sanitised(f.name)+"'s skill directory "+sanitised(f.dir)+", so it cannot be adopted",
			"move them out of it, then "+again+"; what the fork's branch holds beside its skill directory is checked out from the branch")
	}
	state, err := home.State(f.skillDir)
	if err != nil {
		return "", libraryFailure(inv.worktreesDir(), err)
	}
	return state, nil
}

// stageForkPlace plans into m what skill place does to the fork f beside
// its placements, see forkPlacing, and returns the directory the fork's
// content is at before the journal is applied, for a copy placed from it.
//
// An adopted worktree's skill directory is moved out to a hidden directory
// of the worktrees directory, the worktree added in its place, which git
// refuses into a directory that holds anything, and the skill directory
// moved back: what is left of the old worktree, a .git file at most, is
// what adding the worktree clears. An adopted library directory is moved
// into the worktree, a skill directory there that holds the tip retained
// first. Every move is a rename: no file is copied, and none is lost to a
// run stopped part way, which recovery finishes.
func (inv *invocation) stageForkPlace(ctx context.Context, m *home.Mutation, f forkSite, plan forkPlacing) (string, error) {
	if plan.v.content {
		// The branch does not move: the step holds the journal to the tip
		// the content is laid out from.
		m.Ref(f.gitDir, f.rec.Ref, f.rec.Commit, f.rec.Commit)
	}
	switch {
	case plan.adoptRoot:
		fp, _ := home.DirFingerprint(plan.dirState)
		aside := m.Sibling(f.root, "adopting")
		m.Move(f.skillDir, aside, fp)
		if err := inv.stageForkWorktree(ctx, m, f, true); err != nil {
			return "", err
		}
		m.Move(aside, f.skillDir, fp)
		if plan.v.link {
			if err := inv.stageForkLink(m, f); err != nil {
				return "", err
			}
		}
		return f.skillDir, nil
	case plan.adoptLib:
		if err := os.MkdirAll(inv.worktreesDir(), 0o755); err != nil {
			return "", libraryFailure(inv.worktreesDir(), err)
		}
		link, err := inv.forkLink(f.libPath, f.skillDir)
		if err != nil {
			return "", libraryFailure(inv.dirs.Library, err)
		}
		if err := inv.stageForkWorktree(ctx, m, f, true); err != nil {
			return "", err
		}
		if plan.retire != "" {
			if err := m.RemoveInto(f.skillDir, plan.retire, inv.worktreesDir()); err != nil {
				return "", libraryFailure(inv.worktreesDir(), err)
			}
		}
		fp, _ := home.DirFingerprint(plan.libState)
		m.Move(f.libPath, f.skillDir, fp)
		m.Link(f.libPath, link)
		return f.libPath, nil
	}
	return inv.stageForkRepair(ctx, m, f, plan.v)
}

// dirPlaceable is the content at dir, for a placement of the skill called
// name: a copy is laid out from that directory and validated by its
// content hash before it is published.
func dirPlaceable(name, dir string) placeable {
	hash, _ := scan.ContentHashAt(dir)
	return placeable{name: name, hash: hash, stage: func(dest string) error { return copyTreeTo(dir, dest) }}
}

// reportForkPlaced says, after the placements, what skill place put back
// or adopted of the fork itself, and adds it to the result's summary.
func (inv *invocation) reportForkPlaced(f forkSite, plan forkPlacing, repointed bool) {
	var what string
	switch {
	case repointed && !plan.changes():
		what = fmt.Sprintf("repaired the pointers of %s's worktree", sanitised(f.name))
	case plan.adoptRoot:
		what = fmt.Sprintf("adopted %s as %s's worktree; its content is uncommitted edits of the fork", quotedPath(f.root), sanitised(f.name))
	case plan.adoptLib:
		what = fmt.Sprintf("moved %s into %s's worktree; its content is uncommitted edits of the fork", quotedPath(f.libPath), sanitised(f.name))
	case plan.v.worktree && plan.v.content:
		what = fmt.Sprintf("checked %s's worktree out again from its branch", sanitised(f.name))
	case plan.v.content:
		what = fmt.Sprintf("laid %s's skill directory out again from its branch", sanitised(f.name))
	case plan.v.link:
		what = fmt.Sprintf("put %s's library symlink back", sanitised(f.name))
	default:
		return
	}
	inv.out.done(what)
	inv.summary += "; " + what
}
