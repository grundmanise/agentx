package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
// candidate stay as they were until the merge is resolved or given up.
// The checkout is the pending merge, so whether a skill has one is read
// from the directory alone, with no git process.

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
// whether its checkout is there.
func (inv *invocation) mergePending(name string) bool {
	_, err := os.Lstat(inv.checkoutPath(name))
	return err == nil
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
// it for the checkout, MERGE_MSG and then MERGE_HEAD, the candidate, so
// that the checkout is a merge in progress git knows how to complete.
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
	out, err := inv.git.InCheckout(ctx, dir, "rev-parse", "--git-path", "MERGE_MSG", "--git-path", "MERGE_HEAD")
	if err != nil {
		return err
	}
	paths := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(paths) != 2 {
		return fmt.Errorf("git rev-parse: cannot read where the merge state goes in %q", out)
	}
	for i, p := range paths {
		if !filepath.IsAbs(p) {
			paths[i] = filepath.Join(dir, p)
		}
	}
	if err := os.WriteFile(paths[0], []byte(in.message), 0o644); err != nil {
		return err
	}
	return os.WriteFile(paths[1], []byte(in.theirs+"\n"), 0o644)
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
// or naming a git directory that is not there, and the registration of a
// checkout under the merges directory whose directory is gone, its
// directory under the account repo's worktrees, as git's own pruning
// removes one. A checkout anywhere else, a fork's worktree say, is never
// touched. It runs no git.
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
	worktrees, err := filepath.Abs(filepath.Join(gitDir, "worktrees"))
	if err != nil {
		return err
	}
	admins, err := os.ReadDir(worktrees)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// git records where a checkout is by its real path, or by one relative
	// to its registration, so the directory holding a checkout is compared
	// with the merges directory by the real path of the directory above,
	// which is agentx home, there whatever else is not.
	home, err := filepath.Abs(inv.dirs.Home)
	if err != nil {
		return err
	}
	if home, err = filepath.EvalSymlinks(home); err != nil {
		return err
	}
	inMerges := func(path string) bool {
		dir := filepath.Dir(path)
		if above, err := filepath.EvalSymlinks(filepath.Dir(dir)); err == nil {
			dir = filepath.Join(above, filepath.Base(dir))
		}
		return dir == filepath.Join(home, "merges")
	}
	for _, a := range admins {
		admin := filepath.Join(worktrees, a.Name())
		b, err := os.ReadFile(filepath.Join(admin, "gitdir"))
		if err != nil {
			continue
		}
		path := strings.TrimSuffix(string(b), "\n")
		if !filepath.IsAbs(path) {
			path = filepath.Join(admin, path)
		}
		path = filepath.Dir(path)
		if !inMerges(path) {
			continue
		}
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
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
