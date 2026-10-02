package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A pending merge is what an update of a modified skill leaves when its
// edits and its update conflict: an ordinary git merge in progress, in a
// linked worktree of the account repo at <agentx home>/merges/<name>, the
// skill's checkout. It is detached at mine, the library directory as git
// records it committed on the base version, with the update candidate as
// MERGE_HEAD, the merged files, conflict markers and all, in its work tree
// and their three versions in its index, so git status, git diff and git
// commit work in it as in any merge. Nothing of it is ever laid out where
// an agent reads: the library directory, the import branch and the
// candidate stay as they were until the user resolves the merge there
// with git and the next update of the skill applies it, see judgePending,
// or it is given up, see abortMerge. The checkout is the pending merge, so
// whether a skill has one is read from the directory alone, with no git
// process.

// pendingReason is the reason every checkout of a pending merge is locked
// with, which git worktree list shows.
const pendingReason = "agentx pending merge"

// mergesDir is where the checkouts of pending merges are.
func (inv *invocation) mergesDir() string { return filepath.Join(inv.dirs.Home, "merges") }

// checkoutPath is the checkout of the pending merge of the skill called
// name.
func (inv *invocation) checkoutPath(name string) string {
	return filepath.Join(inv.mergesDir(), name)
}

// mergePending reports whether the skill called name has a merge pending:
// whether the merges directory holds its checkout. A name that is not one
// entry of that directory, such as "..", "." or one with a separator, has
// none.
func (inv *invocation) mergePending(name string) bool {
	merges, _ := inv.pendingMerges()
	return merges[name]
}

// pendingMerges are the names of the skills with a merge pending, read in
// one read of the merges directory, which is none when there is no such
// directory. A name starting with a dot is what a journal keeps beside a
// checkout, and no skill's.
func (inv *invocation) pendingMerges() (map[string]bool, error) {
	entries, err := os.ReadDir(inv.mergesDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	merges := map[string]bool{}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			merges[e.Name()] = true
		}
	}
	return merges, nil
}

// mergeStart is what a pending merge is set up from: mine, the commit its
// checkout is detached at, theirs, the candidate it merges, what
// merge-tree made of them, see mergeResult, and the message the commit
// that completes it is to carry.
type mergeStart struct {
	mine, theirs string
	merged       mergeResult
	message      string
}

// startMerge sets up the pending merge of the skill called name, under the
// lock: the checkout is added, detached at mine and locked, and the merge
// is set up in it, see mergeIn. When anything fails, the run being stopped
// included, the checkout is removed again, so that a merge is pending only
// once it is set up whole.
func (inv *invocation) startMerge(ctx context.Context, gitDir, name string, in mergeStart) error {
	if err := os.MkdirAll(inv.mergesDir(), 0o755); err != nil {
		return err
	}
	path := inv.checkoutPath(name)
	err := inv.git.AddCheckout(ctx, gitDir, path, in.mine, pendingReason)
	if err == nil {
		err = inv.mergeIn(ctx, path, in)
	}
	if err != nil {
		return errors.Join(err, inv.git.RemoveCheckout(interrupt.Uninterruptible(ctx), gitDir, path))
	}
	return nil
}

// mergeIn sets the merge up in the checkout at dir, whose HEAD is in.mine,
// as merge-tree found it when the update was judged: its tree is read into
// the checkout's index and work tree, and the stages it listed for each
// conflicted file take the place of that file in the index, as git's own
// merge leaves them. Then git's own merge state is written where git keeps
// it for the checkout, MERGE_MSG, ORIG_HEAD, mine, as git merge writes it,
// and then MERGE_HEAD, the candidate, so that the checkout is a merge in
// progress git knows how to complete. A fork's completion reads mine from
// ORIG_HEAD, since a fork's tip may itself be a merge, which HEAD's
// parents would not tell from the merge committed.
func (inv *invocation) mergeIn(ctx context.Context, dir string, in mergeStart) error {
	if _, err := inv.git.InCheckout(ctx, dir, "read-tree", "--reset", "-u", in.merged.tree); err != nil {
		return err
	}
	// A path takes its stages once its merged entry is taken out, by a line
	// of mode 0, as git update-index documents.
	var removed, stages strings.Builder
	seen := map[string]bool{}
	for _, f := range in.merged.stages {
		_, path, _ := strings.Cut(f, "\t")
		if !seen[path] {
			seen[path] = true
			fmt.Fprintf(&removed, "0 %s\t%s\x00", strings.Repeat("0", len(in.mine)), path)
		}
		stages.WriteString(f + "\x00")
	}
	if _, err := inv.git.InCheckoutInput(ctx, dir, strings.NewReader(removed.String()+stages.String()), "update-index", "-z", "--index-info"); err != nil {
		return err
	}
	paths, err := inv.gitPaths(ctx, dir, "MERGE_MSG", "ORIG_HEAD", "MERGE_HEAD")
	if err != nil {
		return err
	}
	if err := os.WriteFile(paths[0], []byte(in.message), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(paths[1], []byte(in.mine+"\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(paths[2], []byte(in.theirs+"\n"), 0o644)
}

// gitPaths are where git keeps the files called names for the checkout at
// dir, as absolute paths.
func (inv *invocation) gitPaths(ctx context.Context, dir string, names ...string) ([]string, error) {
	var args []string
	for _, n := range names {
		args = append(args, "--git-path", n)
	}
	out, err := inv.git.InCheckout(ctx, dir, append([]string{"rev-parse"}, args...)...)
	if err != nil {
		return nil, err
	}
	paths := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(paths) != len(names) {
		return nil, fmt.Errorf("git rev-parse: cannot read where %s are in %q", strings.Join(names, " and "), out)
	}
	for i, p := range paths {
		if !filepath.IsAbs(p) {
			paths[i] = filepath.Join(dir, p)
		}
	}
	return paths, nil
}

// pendingState is the pending merge of one skill as its checkout holds it
// now, resolved there with git or not: mine, the commit the merge started
// from, with its root tree and its parent, the base, and theirs, the
// update, which is MERGE_HEAD while the merge is in progress and the
// second parent of HEAD once it is committed; the stages of the files
// still unmerged, as ls-files -u lists them; and, when none is left and
// the merge is in progress or committed, the tree of the index, the merge
// as it was resolved.
type pendingState struct {
	mine, mineTree, base, theirs string
	unmerged                     []string
	tree                         string
}

// readMerge reads the merge in the checkout at dir, see pendingState, in
// four git processes at most and one file read.
func (inv *invocation) readMerge(ctx context.Context, dir string) (pendingState, error) {
	var s pendingState
	out, err := inv.git.InCheckout(ctx, dir, "ls-files", "-u", "-z")
	if err != nil {
		return s, err
	}
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			s.unmerged = append(s.unmerged, f)
		}
	}
	// HEAD and its first parent, each as "<commit> <tree> <parents>": mine
	// is HEAD, or its first parent once HEAD is the merge, committed.
	out, err = inv.git.InCheckout(ctx, dir, "log", "-2", "--first-parent", "--format=%H %T %P", "HEAD")
	if err != nil {
		return s, err
	}
	commits := strings.Split(strings.TrimSpace(out), "\n")
	mine := strings.Fields(commits[0])
	if len(mine) == 4 && len(commits) == 2 {
		s.theirs, mine = mine[3], strings.Fields(commits[1])
	}
	if len(mine) > 2 {
		s.mine, s.mineTree, s.base = mine[0], mine[1], mine[2]
	}
	paths, err := inv.gitPaths(ctx, dir, "MERGE_HEAD")
	if err != nil {
		return s, err
	}
	if b, err := os.ReadFile(paths[0]); err == nil {
		s.theirs = strings.TrimSpace(string(b))
	}
	if len(s.unmerged) == 0 && s.theirs != "" {
		out, err = inv.git.InCheckout(ctx, dir, "write-tree")
		s.tree = strings.TrimSpace(out)
	}
	return s, err
}

// abortMerge gives up the merge pending for the skill called name, under
// the lock: git removes its checkout, the directory and git's registration
// of it, whatever was resolved there. The library directory, every
// placement, the import branch and the candidate stay exactly as they
// were, and so do a fork's worktree and branch, so nothing is journaled; a removal stopped part way leaves what
// the next command that changes anything prunes, see pruneMerges. The
// skill is reported as it now stands.
func (inv *invocation) abortMerge(ctx context.Context, name string) error {
	gitDir, _, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	fork := false
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		if !inv.mergePending(name) {
			return refuse(exitRefused, name+" has no merge pending",
				"a merge is left pending by '"+skillCommand("update", name)+"' when your edits conflict with the update; run 'agentx skill list' to see which skills have one")
		}
		values, err := inv.git.Refs(ctx).RefValues(gitDir, []string{lineage.ForkRef(name)})
		if err != nil {
			return accountRepoFailure(err)
		}
		fork = values[lineage.ForkRef(name)] != ""
		path := inv.checkoutPath(name)
		inv.leaveCheckout(path)
		if err := inv.git.RemoveCheckout(ctx, gitDir, path); err != nil {
			return accountRepoFailure(err)
		}
		return nil
	})
	if err != nil {
		return mutationFailure(err)
	}
	kept := "; the library directory is as it was"
	if fork {
		kept = "; the fork's worktree and branch are as they were"
	}
	inv.summary = "gave up the merge of " + name + kept
	if lib, ok := librarySkill(inv.dirs.Library, name); ok {
		snap, err := inv.scan(ctx, lockWait, "", false)
		if err != nil {
			return err
		}
		sc, err := inv.skillContext(ctx)
		if err != nil {
			return err
		}
		inv.out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
	}
	inv.out.done("gave up the merge of " + inv.out.paint(heading, sanitised(name)) + kept)
	return nil
}

// leaveCheckout moves the process out of the checkout at path when it runs
// in it, as it does once the merge was resolved there with git: applying
// or giving up the merge removes the checkout, and every git started after
// that would inherit a working directory that is gone.
func (inv *invocation) leaveCheckout(path string) {
	if wd, err := os.Getwd(); err == nil && (samePath(wd, path) || inside(wd, path)) {
		_ = os.Chdir(inv.mergesDir())
	}
}

// reenterReplaced notes the process's working directory when it is the
// directory at path or one in it, and returns what moves the process back
// there, by path, once a mutation replaced the directory, as a revert or an
// update replaces a skill's directory. The replacement leaves the process
// in the directory it displaced, which the mutation removes once it is
// complete, and every git started after that would inherit a working
// directory that is gone. The working directory is judged on its real
// path, as a shell in a fork's library entry works in the fork's skill
// directory, and entered again on the path it was reached by. A directory
// the new content does not hold leaves the process in the nearest one
// above it that is there.
func reenterReplaced(path string) func() {
	wd, err := os.Getwd()
	if err != nil {
		return func() {}
	}
	real, err := filepath.EvalSymlinks(wd)
	if err != nil || !samePath(real, path) && !inside(real, path) {
		return func() {}
	}
	return func() {
		for dir := wd; ; dir = filepath.Dir(dir) {
			if os.Chdir(dir) == nil || dir == filepath.Dir(dir) {
				return
			}
		}
	}
}

// updateMergeMessage is the message of the commit that completes the
// pending merge of an update of the skill called name from the version
// from to the candidate to. Nothing about the machine enters it.
func updateMergeMessage(name string, from, to lineage.Record) string {
	return fmt.Sprintf("update %s from %s to %s, keeping its edits\n", name, short(from.Import.Commit), short(to.Import.Commit))
}

// pruneMerges removes what an interrupted command left of a pending merge,
// under the lock and once every journal is finished: a directory under
// the merges directory that is no checkout git knows, its .git file gone
// or naming a git directory that is not there, and git's registration of a
// checkout locked with agentx's reason whose directory is gone, its
// directory under the account repo's worktrees, as git's own pruning
// removes one. A checkout agentx did not lock, a fork's worktree say, is
// never touched. It runs no git.
func (inv *invocation) pruneMerges(gitDir string) error {
	entries, err := os.ReadDir(inv.mergesDir())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(inv.mergesDir(), e.Name())
		if !strings.HasPrefix(e.Name(), ".") && !isCheckout(path) {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	worktrees := filepath.Join(gitDir, "worktrees")
	admins, err := os.ReadDir(worktrees)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, a := range admins {
		admin := filepath.Join(worktrees, a.Name())
		reason, err := os.ReadFile(filepath.Join(admin, "locked"))
		if err != nil || strings.TrimSuffix(string(reason), "\n") != pendingReason {
			continue
		}
		b, err := os.ReadFile(filepath.Join(admin, "gitdir"))
		if err != nil {
			continue
		}
		// git records the checkout's .git file by its path, or by one
		// relative to the registration.
		path := strings.TrimSuffix(string(b), "\n")
		if !filepath.IsAbs(path) {
			path = filepath.Join(admin, path)
		}
		if _, err := os.Lstat(filepath.Dir(path)); errors.Is(err, fs.ErrNotExist) {
			if err := os.RemoveAll(admin); err != nil {
				return err
			}
		}
	}
	return nil
}

// isCheckout reports whether the directory at path is a checkout git
// knows: its .git file names a git directory that is there.
func isCheckout(path string) bool {
	b, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	admin, ok := strings.CutPrefix(strings.TrimSuffix(string(b), "\n"), "gitdir: ")
	if !ok {
		return false
	}
	if !filepath.IsAbs(admin) {
		admin = filepath.Join(path, admin)
	}
	_, err = os.Stat(admin)
	return err == nil
}
