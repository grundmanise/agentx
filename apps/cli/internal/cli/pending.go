package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
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

// mergeStart is what a pending merge is set up from: its three versions,
// commits of the account repo whose trees wrap the skill in its upstream
// directory, the message the commit that completes it is to carry, and
// the size of its conflict markers, see markerSize.
type mergeStart struct {
	base, mine, theirs string
	message            string
	size               int
}

// startMerge sets up the pending merge of the skill called name, under the
// lock: the checkout is added, detached at mine and locked, and the merge
// runs in it, see mergeIn. When anything fails, the run being stopped
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

// mergeIn merges in the checkout at dir, whose HEAD is in.mine, as git
// merges any two commits over a base given: one merge-recursive with
// in.base as the merge base, with the lines paired by histogram diff and
// git's detection of a renamed directory off as merge-tree had them, the
// conflicts written in zdiff3 style, so that each carries the base between
// mine and theirs, with markers of in.size, which an attributes file of
// its own asks for in place of the null device the isolated environment
// names. The skill's own .gitattributes applies in the checkout, so a
// merge that comes out clean there is not the merge merge-tree judged,
// and fails. Then git's own merge state is written where git keeps it for
// the checkout, MERGE_MSG and then MERGE_HEAD, the candidate, so that the
// checkout is a merge in progress git knows how to complete.
func (inv *invocation) mergeIn(ctx context.Context, dir string, in mergeStart) error {
	attrs, err := os.CreateTemp("", "agentx-attributes-")
	if err != nil {
		return err
	}
	defer os.Remove(attrs.Name())
	_, err = fmt.Fprintf(attrs, "* conflict-marker-size=%d\n", in.size)
	if closeErr := attrs.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	_, status, err := inv.git.InCheckoutStatus(ctx, dir, 1, "-c", "merge.directoryRenames=false", "-c", "merge.conflictStyle=zdiff3",
		"-c", "core.attributesFile="+attrs.Name(), "merge-recursive", "--diff-algorithm=histogram", in.base, "--", in.mine, in.theirs)
	if err != nil {
		return err
	}
	if status == 0 {
		return errors.New("git merged the skill cleanly in the checkout, where its own .gitattributes applies")
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
// from to the candidate to: what it did, and the trailer naming the import
// commit the skill's branch moves to then. Nothing about the machine
// enters it.
func updateMergeMessage(name string, from, to lineage.Record) string {
	return fmt.Sprintf("update %s from %s to %s, keeping its edits\n\n%s: %s\n",
		name, short(from.Import.Commit), short(to.Import.Commit), lineage.TrailerBase, to.Commit)
}

// readConflicts reads the files that conflict in the merge in progress in
// the checkout at dir, whose tree holds the skill under skillDir, sorted
// by path relative to the skill: the unmerged paths git's index lists, the
// blob of each stage of each, read in one cat-file, and the hunks of a
// text file from the marker blocks the checkout's file holds, see
// hunksIn. A file conflicts whole as conflictOf says.
func (inv *invocation) readConflicts(ctx context.Context, gitDir, dir, skillDir string) ([]conflictFile, error) {
	out, err := inv.git.InCheckout(ctx, dir, "ls-files", "-u", "-z")
	if err != nil {
		return nil, err
	}
	stages := map[string]map[string]staged{}
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		meta, path, ok := strings.Cut(entry, "\t")
		parts := strings.Fields(meta)
		if !ok || len(parts) != 3 {
			return nil, fmt.Errorf("git ls-files: cannot read the unmerged entry %q", entry)
		}
		rel, inside := strings.CutPrefix(path, skillDir+"/")
		if !inside {
			continue
		}
		if stages[rel] == nil {
			stages[rel] = map[string]staged{}
		}
		stages[rel][parts[2]] = staged{mode: parts[0], oid: parts[1]}
	}
	var ids []string
	paths := make([]string, 0, len(stages))
	renamed := false // a file moved to two places leaves its old path with the base alone
	for p, versions := range stages {
		paths = append(paths, p)
		for _, v := range versions {
			if source.IsFileMode(v.mode) {
				ids = append(ids, v.oid)
			}
		}
		if _, ok := versions[gitStageBase]; ok && len(versions) == 1 {
			renamed = true
		}
	}
	sort.Strings(paths)
	bodies, err := source.ReadBlobs(ctx, inv.git, gitDir, ids)
	if err != nil {
		return nil, err
	}
	files := []conflictFile{}
	for _, p := range paths {
		f, text := conflictOf(p, stages[p], bodies, renamed)
		if text {
			if f.Hunks, f.why, err = hunksIn(filepath.Join(dir, skillDir, filepath.FromSlash(p)), stages[p], bodies); err != nil {
				return nil, err
			}
		}
		files = append(files, f)
	}
	return files, nil
}

// hunksIn reads the hunks of one text file that conflicts out of the file
// at path, where the merge wrote it: the marker blocks of the size
// markerSizeIn finds, so that no line of the file's own is taken for a
// marker, each with mine, the base and theirs as the file's three versions
// hold them there. A file with no such block conflicts whole: why says
// how.
func hunksIn(path string, versions map[string]staged, bodies map[string]string) ([]conflictHunk, string, error) {
	text := func(stage string) string {
		if v, ok := versions[stage]; ok {
			return bodies[v.oid]
		}
		return ""
	}
	mine, base, theirs := text(gitStageMine), text(gitStageBase), text(gitStageTheirs)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	file := string(b)
	var hunks []conflictHunk
	if size := markerSizeIn(file, mine, base, theirs); size > 0 {
		var around []string
		if around, hunks, err = splitMerged(file, size); err != nil {
			hunks = nil
		} else if len(hunks) > 0 && around[len(around)-1] == "" {
			h := &hunks[len(hunks)-1]
			h.Mine, h.Base, h.Theirs = asAtEnd(h.Mine, mine), asAtEnd(h.Base, base), asAtEnd(h.Theirs, theirs)
		}
	}
	_, based := versions[gitStageBase]
	switch {
	case len(hunks) > 0:
		return hunks, "", nil
	case !based && versions[gitStageMine].mode != versions[gitStageTheirs].mode:
		return []conflictHunk{}, "added here and by the update, executable on one side only", nil
	}
	return []conflictHunk{}, "changed here and by the update, with no conflict markers agentx can tell from its lines", nil
}

// pruneMerges removes what an interrupted command left of a pending merge,
// under the lock and once every journal is finished: a directory under
// the merges directory that is no checkout git knows, its .git file gone
// or naming a git directory that is not there, and the registration of a
// checkout under the merges directory whose directory is gone. A checkout
// anywhere else, a fork's worktree say, is never touched. It runs no git
// unless there is something to remove.
func (inv *invocation) pruneMerges(ctx context.Context, gitDir string) error {
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
	admins, err := os.ReadDir(filepath.Join(gitDir, "worktrees"))
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
		admin := filepath.Join(gitDir, "worktrees", a.Name())
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
			if err := inv.git.RemoveCheckout(ctx, gitDir, path); err != nil {
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
