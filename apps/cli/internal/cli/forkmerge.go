package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A fork's pending merge is a managed skill's, see startMerge: a checkout
// under agentx home, detached at the fork's tip as the merge found it,
// with the merge in progress in it, which git status, git diff and git
// commit work in as in any merge. Its MERGE_MSG is the message of the
// commit that completes it, written when the merge was started and naming
// the import the merge records as the fork's base, and its ORIG_HEAD is
// the tip it started from. Nothing else records it: no ref, no file of
// agentx's own. The next update of the fork completes it once it is
// resolved, see judgeForkCompletion, and skill update --abort gives it up.

// forkPending is the merge pending for a fork as its checkout holds it
// now, resolved there with git or not.
type forkPending struct {
	mine   string // the fork's tip the merge started from, its ORIG_HEAD
	theirs string // what it merges: MERGE_HEAD, or the second parent of the merge once committed
	// merged is the merge commit made in the checkout with git commit, ""
	// while the merge is in progress.
	merged   string
	message  string   // the message of the commit that completes it: MERGE_MSG, or the commit's own
	unmerged []string // the stages of the files still unmerged, as ls-files -u lists them
	tree     string   // the tree of the index, once nothing is unmerged and the merge is in progress
}

// readForkMerge reads the merge in the fork's checkout at dir, see
// forkPending, in four git processes at most and three file reads. A merge
// committed in the checkout is HEAD with ORIG_HEAD as its first parent.
// Neither a merge in progress nor one committed, as git merge --abort in
// the checkout leaves it, is read with no theirs.
func (inv *invocation) readForkMerge(ctx context.Context, dir string) (forkPending, error) {
	var p forkPending
	out, err := inv.git.InCheckout(ctx, dir, "ls-files", "-u", "-z")
	if err != nil {
		return p, err
	}
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			p.unmerged = append(p.unmerged, f)
		}
	}
	paths, err := inv.gitPaths(ctx, dir, "ORIG_HEAD", "MERGE_HEAD", "MERGE_MSG")
	if err != nil {
		return p, err
	}
	files := make([]string, len(paths))
	for i, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return p, err
		}
		files[i] = string(b)
	}
	out, err = inv.git.InCheckout(ctx, dir, "log", "-1", "--format=%H%x00%P%x00%B", "HEAD")
	if err != nil {
		return p, err
	}
	head := strings.SplitN(out, "\x00", 3)
	if len(head) != 3 {
		return p, errors.New("git log: cannot read the HEAD of " + dir)
	}
	parents := strings.Fields(head[1])
	p.mine = strings.TrimSpace(files[0])
	switch {
	case strings.TrimSpace(files[1]) != "":
		p.theirs, p.message = strings.TrimSpace(files[1]), files[2]
		if p.mine == "" {
			p.mine = head[0]
		}
	case len(parents) == 2 && parents[0] == p.mine:
		p.merged, p.theirs, p.message = head[0], parents[1], strings.TrimSuffix(head[2], "\n")
	}
	if len(p.unmerged) == 0 && p.theirs != "" && p.merged == "" {
		out, err = inv.git.InCheckout(ctx, dir, "write-tree")
		p.tree = strings.TrimSpace(out)
	}
	return p, err
}

// judgeForkCompletion judges, before the lock, the merge pending for the
// fork whose branch is rec, for an update or, when pull is set, a pull, as
// the user left it resolving it with git, see readForkMerge. A merge with
// files still unmerged, or none in progress, is left as it is and reported
// again, exit code 4. One resolved, in progress or committed, is
// completed, once the fork's skill directory is found to hold no
// uncommitted edits, exit code 6 otherwise, as for the update that started
// it: the commit that completes it is the user's own commit in the
// checkout, or the one the fork commit writer writes here, with the tree
// of the checkout's index, the tip the merge started from and what it
// merged as its parents, and the merge's MERGE_MSG as its message.
// A user's commit whose message names no base is written again the same
// way, with the base added to its message.
//
// When the fork's branch still holds that tip, the branch moves to the
// commit, see applyFork. When it moved on, because commits were made in
// the fork's worktree while the merge was pending, the completed merge is
// merged with the branch's tip in turn, on ordinary ancestry, the merge
// base being the tip the merge started from, so that only where the new
// commits and the completed merge overlap can conflict. That merge records
// the base the completed one records, unless the commits made meanwhile
// brought the fork a base of its own: then it records the newer of the two
// where the source history proves it, and the fork's own otherwise, as a
// merge of two histories does, see lineage.PickBase. A clean result is
// committed and applied; one that conflicts is set up again in the same
// checkout under the lock, showing only the overlap, and is completed in
// turn. A branch that no longer holds the tip the merge started from at
// all was moved outside agentx, and the merge cannot be applied, exit code
// 6, until it is given up.
func (inv *invocation) judgeForkCompletion(ctx context.Context, gitDir string, rec lineage.Record, pull bool) (*updating, *failure) {
	name := rec.Name
	site, err := inv.forkSiteOf(ctx, gitDir, rec)
	if err != nil {
		return nil, failureOf(err)
	}
	u := &updating{name: name, libPath: site.libPath, checkout: inv.checkoutPath(name), fork: &forkUpdate{site: site, pull: pull}}
	p, err := inv.readForkMerge(ctx, u.checkout)
	if err != nil {
		return nil, failureOf(accountRepoFailure(err))
	}
	if p.theirs == "" {
		p.theirs = rec.CandidateCommit() // no merge in progress: the update it would merge
	}
	recorded := ""
	if t, err := lineage.ParseFork(p.message); err == nil {
		recorded = t.Base
	}
	var tips []string
	for _, c := range []string{p.mine, p.theirs, recorded} {
		if c != "" && !slices.Contains(tips, c) {
			tips = append(tips, c)
		}
	}
	walked, err := lineage.Walk(ctx, inv.git, gitDir, tips)
	if err != nil {
		return nil, failureOf(accountRepoFailure(err))
	}
	mine := walked[p.mine]
	resolved := len(p.unmerged) == 0 && (p.tree != "" || p.merged != "")
	// A merge committed in the checkout with a message of the user's own, as
	// git commit -m writes it, or one whose MERGE_MSG was edited, names no
	// base. The base is then the one the merge was started to record: what
	// it merges, an import, or the base the completed merge it merges again
	// records. A merge of two histories of the fork, as a pull starts, or a
	// completed merge merged again records what the base rule picks of the
	// two sides' bases instead, see lineage.PickBase, as the merge's
	// MERGE_MSG did: a pull never takes the fork back to an older version
	// than the one this machine holds. The commit that completes it is
	// written again with that base added to the user's message, see
	// keptMessage, so that the fork does not stay on the old base and take
	// the same update, and conflict, again.
	next := walked[recorded]
	rewrite := resolved && recorded == "" && walked[p.theirs].Base != ""
	if rewrite {
		recorded, next = walked[p.theirs].Base, walked[p.theirs]
		if recorded != p.theirs {
			if base := lineage.PickBase(ctx, inv.git, gitDir, mine, next); base != recorded {
				recorded, next = base, mine
			}
		}
	}
	u.rec = lineage.Record{Name: name, Kind: lineage.KindFork, Commit: mine.Base, Import: mine.Import}
	u.next = lineage.Record{Name: name, Kind: lineage.KindFork, Commit: recorded, Import: next.Import}
	// A merge whose theirs is no import commit is a merge of two histories:
	// a completed merge merged again with the commits made while it was
	// pending, or a pull's merge with what the account remote held.
	u.fork.remerge = p.theirs != "" && walked[p.theirs].Base != p.theirs
	if !resolved {
		u.merge = lineage.Merge{Base: mine.Base, Mine: p.mine, Theirs: p.theirs}
		if u.fork.remerge {
			if u.merge.Base, err = inv.git.Isolated(ctx, gitDir, "merge-base", p.mine, p.theirs); err != nil {
				return nil, failureOf(accountRepoFailure(err))
			}
			// A completed merge is only ever written here, so a theirs the
			// remote-tracking branch holds is what a pull merged, whichever
			// command reports the merge again and however far the remote
			// moved on since.
			with, err := inv.mergedWithAccount(ctx, gitDir, name, p.theirs)
			if err != nil {
				return nil, failureOf(err)
			}
			if with {
				u.fork.with = "the account remote"
			}
		} else if p.theirs != "" {
			u.next = lineage.Record{Name: name, Kind: lineage.KindFork, Commit: p.theirs, Import: walked[p.theirs].Import}
		}
		files, err := conflictFiles(p.unmerged, site.dir)
		if err != nil {
			return nil, failureOf(accountRepoFailure(err))
		}
		u.merged.conflicted, u.conflict = true, conflictOfSkill(name, lineage.KindFork, u.merge, files)
		return u, nil
	}
	w, err := inv.newForkWriter(ctx, gitDir)
	if err != nil {
		return nil, failureOf(err)
	}
	var f *failure
	if u.fork.judged, f = inv.cleanSite(ctx, site, u.fork); f != nil {
		return nil, f
	}
	merged, tree, message := p.merged, p.tree, p.message
	if rewrite {
		merged = ""
		if tree == "" {
			tree, err = inv.git.Isolated(ctx, gitDir, "rev-parse", "--verify", p.merged+"^{tree}")
		}
		if err == nil {
			message, err = w.keptMessage(p.message, recorded)
		}
		if err != nil {
			return nil, failureOf(accountRepoFailure(err))
		}
	}
	if merged == "" {
		if merged, err = w.commitText(ctx, tree, []string{p.mine, p.theirs}, message); err != nil {
			return nil, failureOf(accountRepoFailure(err))
		}
	}
	u.fork.base = recorded
	if rec.Commit == p.mine {
		u.fork.stale = inv.staleCandidate(ctx, gitDir, rec, u.fork.base, u.next.Import)
		return u, inv.layFork(ctx, u, merged)
	}
	_, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", "--is-ancestor", p.mine, rec.Commit)
	switch {
	case err != nil:
		return nil, failureOf(accountRepoFailure(err))
	case status != 0:
		return nil, refuse(exitRefused, "the branch "+rec.Ref+" moved outside agentx while the merge of "+sanitised(name)+" was pending, so the merge cannot be applied",
			"run '"+skillCommand("update", name, "--abort")+"' to give it up, then update again")
	}
	if rec.Fork != nil && rec.Fork.Base != mine.Base {
		completed := lineage.ForkLineage{Base: u.fork.base, Import: u.next.Import}
		if base := lineage.PickBase(ctx, inv.git, gitDir, *rec.Fork, completed); base != u.fork.base {
			u.fork.base = base
			u.next = lineage.Record{Name: name, Kind: lineage.KindFork, Commit: base, Import: rec.Fork.Import}
		}
	}
	u.merge = lineage.Merge{Base: p.mine, Mine: rec.Commit, Theirs: merged}
	if u.merged, err = mergeVersions(ctx, inv.git, gitDir, u.merge); err != nil {
		return nil, failureOf(accountRepoFailure(err))
	}
	subject, _, _ := strings.Cut(message, "\n")
	if u.merged.conflicted {
		files, err := conflictFiles(u.merged.stages, site.dir)
		if err == nil {
			u.fork.message, err = w.mergeMessage(subject, u.fork.base)
		}
		if err != nil {
			return nil, failureOf(accountRepoFailure(err))
		}
		u.conflict = conflictOfSkill(name, lineage.KindFork, u.merge, files)
		u.fork.start, u.fork.remerge = true, true
		return u, nil
	}
	u.fork.stale = inv.staleCandidate(ctx, gitDir, rec, u.fork.base, u.next.Import)
	commit, err := w.commit(ctx, u.merged.tree, []string{rec.Commit, merged}, forkMessage{subject: subject, trailers: lineage.ForkTrailers{Base: u.fork.base}})
	if err != nil {
		return nil, failureOf(accountRepoFailure(err))
	}
	return u, inv.layFork(ctx, u, commit)
}

// mergedWithAccount reports whether theirs, what a pending merge of the
// fork called name merged, is on the account remote's remote-tracking
// branch of the fork, as a pull's merge is: false on a machine with no
// account remote.
func (inv *invocation) mergedWithAccount(ctx context.Context, gitDir, name, theirs string) (bool, error) {
	_, remote, ok, err := inv.accountSource()
	if err != nil || !ok {
		return false, err
	}
	on, err := inv.git.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)", "--contains", theirs, lineage.RemoteForkRef(remote, name))
	if err != nil {
		return false, accountRepoFailure(err)
	}
	return on != "", nil
}

// remergeIn sets a merge up again in the checkout at dir, under the lock:
// the completed merge, in.theirs, merged with the fork's tip, in.mine,
// which the checkout's HEAD is moved to, so that the checkout is that merge
// in progress, see mergeIn. The checkout holds a resolved merge until
// then, which the next update would complete, so it is made to hold none
// before anything else changes: MERGE_HEAD goes first, and ORIG_HEAD names
// the tip, which no commit the checkout's HEAD can be at has as its first
// parent. A re-merge stopped part way, by git failing on an index the
// user's own git holds or by the process being killed, then leaves a
// checkout with no merge in progress, whose conflict the next update
// reports again until the merge is given up, never one that reads as
// resolved with the new merge's conflict markers in its index. MERGE_HEAD
// is written again last, by mergeIn. Once started, it is not stopped by
// an interrupt, as startMerge's clean-up is not.
func (inv *invocation) remergeIn(ctx context.Context, dir string, in mergeStart) error {
	ctx = interrupt.Uninterruptible(ctx)
	paths, err := inv.gitPaths(ctx, dir, "MERGE_HEAD", "ORIG_HEAD")
	if err != nil {
		return accountRepoFailure(err)
	}
	if err := os.Remove(paths[0]); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.WriteFile(paths[1], []byte(in.mine+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := inv.git.InCheckout(ctx, dir, "update-ref", "--no-deref", "HEAD", in.mine); err != nil {
		return accountRepoFailure(err)
	}
	if err := inv.mergeIn(ctx, dir, in); err != nil {
		return accountRepoFailure(err)
	}
	return nil
}
