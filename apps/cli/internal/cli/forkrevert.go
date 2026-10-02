package cli

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A fork is reverted in one of two ways. Without a commit, its uncommitted
// edits are discarded: the skill directory is put back to the branch's tip
// and nothing is committed, which is the one way agentx drops edits, and
// only when asked to. With one, the skill directory is restored from that
// commit, an earlier one of the fork's own history, as a new commit on top
// of the tip, written by the fork commit writer: history is never
// rewritten, and the commit is an ordinary one, which a later merge keeps,
// or conflicts with where the other side changed the same lines, as git
// does with any commit. Either way the files git ignores in the skill
// directory stay, as git checkout keeps them, but for a file the restored
// content holds at an ignored path, which replaces the local file as git
// checkout replaces it; the journal retains the directory as it was until
// the command completes.

// revertTo is what a revert to an earlier commit refuses for as the words
// of the refusals that name it: the fork "cannot be reverted to an earlier
// commit".
const revertTo = "reverted to an earlier commit"

// forkReverting is one revert of a fork as it is planned before the lock.
type forkReverting struct {
	site   forkSite
	judged siteJudged // the skill directory, captured and judged with its ignored files
	// to is the commit a revert to an earlier commit restores and commit
	// the commit it writes; both are "" for a revert of the edits.
	to, commit, subject string
	laid                version // what the skill directory becomes: the tip's, or the new commit's
	lay                 func(dest string) error
	// placed is the commit whose skill directory a copy placement holds
	// when it holds what the library held: the tip for a revert to an
	// earlier commit, since the fork has no edits then, and "" for a
	// revert of the edits, whose copies hold the edits, see judged.
	placed string
}

// forkRevert reverts the fork whose branch is rec: its uncommitted edits,
// or, when to names a commit, to that commit, see forkRevertTo. The edits
// a revert discards are those the skill directory held when the command
// began: they are captured first, as the fingerprint a journal compares,
// and read again under the lock, where an edit made meanwhile, by any
// tool, refuses the revert rather than being discarded with the rest. The
// journal's remove step carries the same fingerprint, and the content it
// retains is dropped only when it still hashes to it, so an edit made while
// the mutation runs is kept as well.
//
// A fork with a merge pending is refused first, exit code 4, before its
// worktree or directory is read, and even when it has no edits to discard:
// the merge holds the fork's tip as it was. A directory holding something
// git cannot record that no ignore rule covers, such as a repository
// nested in it, is refused too, since the revert would discard it with no
// record of it anywhere; one an ignore rule covers stays where it is, as
// any ignored file does.
func (inv *invocation) forkRevert(ctx context.Context, gitDir string, rec lineage.Record, to string) error {
	if inv.mergePending(rec.Name) {
		return forkPendingRefusal(rec.Name, revertRefusal(to))
	}
	f, err := inv.forkSiteOf(ctx, gitDir, rec)
	if err != nil {
		return err
	}
	r := &forkReverting{site: f, to: to}
	if to != "" {
		if err := inv.forkRevertTo(ctx, r); err != nil {
			return err
		}
		if r.commit == "" {
			return inv.reportForkReverted(ctx, r, placements{})
		}
	} else {
		if r.judged, err = inv.judgeSite(ctx, f, true); err != nil {
			return err
		}
		if r.judged.clean {
			return inv.reportForkReverted(ctx, r, placements{})
		}
		if _, lost := splitUnrecordable(r.judged.forkJudged); len(lost) > 0 {
			return unrecordableRefusal(f.name, f.skillDir, lost, "a revert", "revert")
		}
		if r.laid, r.lay, err = inv.forkLayout(ctx, gitDir, rec.Commit, f.dir); err != nil {
			return accountRepoFailure(err)
		}
	}
	var done placements
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error { return inv.applyForkRevert(ctx, r, &done) })
	if errors.Is(err, home.ErrMovedBeforeApply) {
		return fail(exitRefused, sanitised(f.name)+" changed while it was being reverted, so nothing was changed", "run the command again")
	}
	if err != nil {
		return mutationFailure(err)
	}
	return inv.reportForkReverted(ctx, r, done)
}

// revertRefusal is what a revert, to the commit to names or of the edits
// when to is "", refuses for as the words of the refusals that name it.
func revertRefusal(to string) string {
	if to != "" {
		return revertTo
	}
	return "reverted"
}

// splitUnrecordable sorts the paths of a fork's skill directory, as j
// judged it, that git cannot record: the .git of a repository nested in
// the directory that an ignore rule covers is no part of the fork and is
// carried over into the reverted directory, kept, as every file git
// ignores is; anything else, a repository no rule covers or a named pipe,
// is lost, which replacing the directory would discard with no record of
// it anywhere. Pure.
func splitUnrecordable(j forkJudged) (kept, lost []string) {
	for _, p := range j.unrecordable {
		if hasGitComponent(p) && !slices.Contains(j.exposed, p) {
			kept = append(kept, p)
			continue
		}
		lost = append(lost, p)
	}
	return kept, lost
}

// forkRevertTo plans a revert of the fork to the commit r.to names, before
// the lock, once forkRevert has refused a merge pending: the commit has to
// be the tip or one of its ancestors and hold the fork's skill directory;
// the fork has to have no uncommitted edits, exit code 6; and the new
// commit is written, the tip's tree with the skill directory that commit
// holds, the tip its one parent. A commit whose directory the tip holds
// already leaves nothing to commit, r.commit "".
func (inv *invocation) forkRevertTo(ctx context.Context, r *forkReverting) error {
	f := r.site
	c, err := inv.resolveForkCommit(ctx, f, r.to, true)
	if err != nil {
		return err
	}
	r.to = c.id
	// The writer reads the user's identity and their core.excludesFile in
	// one read of their configuration, before git reads the skill
	// directory, so the ignore rules the judgement uses come with it.
	w, err := inv.newForkWriter(ctx, f.gitDir)
	if err != nil {
		return err
	}
	if r.judged, err = inv.judgeSite(ctx, f, true); err != nil {
		return err
	}
	switch {
	case len(r.judged.exposed) > 0:
		return nestedRepoRefusal(f.name, r.judged.exposed)
	case !r.judged.clean:
		return uncommittedRefusal(f.name, revertTo)
	}
	if _, lost := splitUnrecordable(r.judged.forkJudged); len(lost) > 0 {
		return unrecordableRefusal(f.name, f.skillDir, lost, "a revert", "revert")
	}
	root, err := inv.git.ReplaceEntry(ctx, f.gitDir, f.rec.Commit, f.dir, c.sub)
	if err != nil {
		return accountRepoFailure(err)
	}
	if root == f.rec.Tree {
		return nil
	}
	r.subject = revertSubject(f.name, w.label, c.id)
	if r.commit, err = w.commit(ctx, root, []string{f.rec.Commit}, forkMessage{subject: r.subject}); err != nil {
		return accountRepoFailure(err)
	}
	if r.laid, r.lay, err = inv.forkLayout(ctx, f.gitDir, r.commit, f.dir); err != nil {
		return accountRepoFailure(err)
	}
	r.placed = f.rec.Commit
	return nil
}

// revertSubject is the subject of the commit a revert to an earlier commit
// writes: the skill, the commit it restores and the label of the machine
// it was made on, as in "notes: revert to 1a2b3c4 (laptop)".
func revertSubject(name, label, commit string) string {
	return name + ": revert to " + short(commit) + " (" + sanitised(label) + ")"
}

// applyForkRevert records and applies a revert under the lock, once
// everything it was planned from is read again: no merge may be pending,
// the branch has to hold the tip the revert was planned on, the worktree
// has to be one git can work in, and the skill directory has to hold what
// it held when it was judged, which for a revert to an earlier commit is
// its tip, so an edit made meanwhile refuses it as uncommitted. For a
// revert of the edits, a directory that changed since is judged again, and
// only a change to what git records refuses it: a file git ignores that
// appeared or changed meanwhile, such as a .DS_Store, is carried over as
// any ignored file is.
//
// It is one journal, see layTipJournal, which moves the branch to the new
// commit or, for a revert of the edits, holds it at its tip, and lays that
// commit out in the worktree.
func (inv *invocation) applyForkRevert(ctx context.Context, r *forkReverting, done *placements) error {
	f := r.site
	if inv.mergePending(f.name) {
		return forkPendingRefusal(f.name, revertRefusal(r.to))
	}
	values, err := inv.git.Refs(ctx).RefValues(f.gitDir, []string{f.rec.Ref})
	if err != nil {
		return accountRepoFailure(err)
	}
	if values[f.rec.Ref] != f.rec.Commit {
		return fail(exitRefused, sanitised(f.name)+" changed while it was being reverted, so nothing was changed",
			"run '"+skillCommand("history", f.name)+"' to see what changed, then revert again")
	}
	if err := worktreeHealth(f.name, f.root, f.branch); err != nil {
		return err
	}
	now := r.judged
	if r.to != "" {
		if now, err = inv.forkGuards(ctx, f, r.judged, revertTo, true); err != nil {
			return err
		}
	} else {
		live, err := home.State(f.skillDir)
		if err != nil {
			return libraryFailure(f.root, err)
		}
		if live != r.judged.captured {
			// The edits are compared as git wrote them, so a change to an
			// ignored file alone leaves the tree it wrote as it was.
			if now, err = inv.judgeSite(ctx, f, true); err != nil {
				return err
			}
			if now.clean || now.written == "" || now.written != r.judged.written {
				return fail(exitRefused, sanitised(f.name)+" changed while it was being reverted, so nothing was discarded",
					"run '"+skillCommand("diff", f.name)+"' to see the change, then revert again to discard it too")
			}
		}
	}
	kept, lost := splitUnrecordable(now.forkJudged)
	if len(lost) > 0 {
		return unrecordableRefusal(f.name, f.skillDir, lost, "a revert", "revert")
	}
	moved := r.commit
	if moved == "" {
		moved = f.rec.Commit // held at its tip
	}
	m, _, err := inv.layTipJournal(ctx, f, tipLaying{
		commit: moved, laid: r.laid, lay: r.lay, now: now, kept: kept, done: done,
		placed: func() ([]version, error) { return inv.placedFork(ctx, r, now) },
	})
	if err != nil {
		return err
	}
	back := reenterReplaced(f.skillDir)
	defer back()
	return m.Apply(inv.refs(ctx))
}

// placedFork is what a copy placement of the fork holds when it holds what
// the library held before the revert: the tip's skill directory for a
// revert to an earlier commit, and the directory with its edits, as git
// wrote it when it judged the directory, for a revert of the edits. A copy
// holding it is refreshed with the reverted content, and one holding
// anything else was edited where it is and is kept.
func (inv *invocation) placedFork(ctx context.Context, r *forkReverting, now siteJudged) ([]version, error) {
	treeish := r.placed + ":" + r.site.dir
	if r.placed == "" {
		if now.written == "" {
			return nil, nil
		}
		treeish = now.written + ":" + r.site.dir
	}
	sub, err := inv.git.Isolated(ctx, r.site.gitDir, "rev-parse", "--verify", "--quiet", treeish)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	return []version{treeVersion(strings.TrimSpace(sub))}, nil
}

// reportForkReverted reads the machine again and reports the fork as it
// now stands: its library_skill event, and a line saying what the revert
// did, or that there was nothing to revert, which is said without a mark
// since nothing changed.
func (inv *invocation) reportForkReverted(ctx context.Context, r *forkReverting, done placements) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	f, out := r.site, inv.out
	if lib, ok := librarySkill(inv.dirs.Library, f.name); ok {
		out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
	}
	name := sanitised(f.name)
	switch {
	case r.to == "" && r.judged.clean:
		inv.summary = name + " has no uncommitted edits; nothing was reverted"
		out.print(out.paint(heading, name), " has no uncommitted edits; nothing was reverted")
		return nil
	case r.to != "" && r.commit == "":
		inv.summary = name + " already holds what " + short(r.to) + " holds; nothing was reverted"
		out.print(out.paint(heading, name), " already holds what ", short(r.to), " holds; nothing was reverted")
		return nil
	}
	note, painted := copiesNote(out, len(done.copies), len(done.skipped))
	if r.to == "" {
		what := " to its last commit " + short(f.rec.Commit)
		inv.summary = "reverted " + name + what + note
		out.done("reverted " + out.paint(heading, name) + what + painted)
		return nil
	}
	what := " to " + short(r.to) + " as " + short(r.commit) + ": " + sanitised(r.subject)
	inv.summary = "reverted " + name + what + note
	out.done("reverted " + out.paint(heading, name) + what + painted)
	return nil
}
