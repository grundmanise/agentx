package gitx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ForkReason is the lock reason of a fork's worktree. A pending merge's
// checkout is locked with a reason of its own, and what prunes those
// leftovers goes by the reason, so the two must differ.
const ForkReason = "agentx fork"

// Worktree is one entry of git worktree list.
type Worktree struct {
	Path       string
	Head       string
	Branch     string // the full ref, such as refs/heads/skills/pdf; "" when detached or bare
	Detached   bool
	Bare       bool
	Locked     bool
	LockReason string
	Prunable   bool
}

// ParseWorktrees reads what git worktree list --porcelain -z prints: one
// record per worktree, each a run of NUL-terminated attribute lines ending
// with an empty one.
func ParseWorktrees(out string) ([]Worktree, error) {
	var list []Worktree
	var cur *Worktree
	for _, line := range strings.Split(out, "\x00") {
		if line == "" {
			cur = nil
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		if key == "worktree" {
			list = append(list, Worktree{Path: value})
			cur = &list[len(list)-1]
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("git worktree list printed %q outside a worktree's record", line)
		}
		switch key {
		case "HEAD":
			cur.Head = value
		case "branch":
			cur.Branch = value
		case "detached":
			cur.Detached = true
		case "bare":
			cur.Bare = true
		case "locked":
			cur.Locked, cur.LockReason = true, value
		case "prunable":
			cur.Prunable = true
		}
	}
	return list, nil
}

// Worktrees lists the worktrees of the repository at gitDir.
func (r *Runner) Worktrees(ctx context.Context, gitDir string) ([]Worktree, error) {
	out, err := r.run(ctx, call{isolated: true}, isolatedArgs(gitDir, []string{"worktree", "list", "--porcelain", "-z"})...)
	if err != nil {
		return nil, err
	}
	return ParseWorktrees(out)
}

// AddForkWorktree adds path, absent or an empty directory, as a linked
// worktree of the repository at gitDir on branch, the short name of a
// fork's branch such as skills/pdf, locked with ForkReason and with nothing
// checked out: the fork's content is laid out in it by the command, and
// ResetIndex then aligns the index. The branch is passed by its short name,
// since git detaches HEAD at a full ref name.
//
// Registrations whose directory is gone are pruned first, which a locked
// worktree survives. A locked registration of path itself whose directory
// is gone, which is what a fork's worktree removed by hand leaves, is
// reused with the doubled -f git asks for. A branch checked out at another
// existing path is refused, as git refuses it.
func (r *Runner) AddForkWorktree(ctx context.Context, gitDir, path, branch string) error {
	list, err := r.Worktrees(ctx, gitDir)
	if err != nil {
		return err
	}
	force, prune := false, false
	for _, w := range list {
		switch {
		case w.Prunable:
			prune = true
		case w.Bare:
		case samePath(w.Path, path):
			force = w.Locked && gone(w.Path)
		case w.Branch == "refs/heads/"+branch && !gone(w.Path):
			return fmt.Errorf("%s is checked out at %s already", branch, w.Path)
		}
	}
	if prune {
		if err := r.PruneWorktrees(ctx, gitDir); err != nil {
			return err
		}
	}
	args := []string{"worktree", "add"}
	if force {
		args = append(args, "-f", "-f")
	}
	args = append(args, "--no-checkout", "--lock", "--reason", ForkReason, path, branch)
	_, err = r.Isolated(ctx, gitDir, args...)
	return err
}

// ResetIndex sets the index of the worktree at path to its branch tip and
// touches no file, which is git reset -q run in the worktree. It is how a
// worktree added with nothing checked out, or one whose branch agentx moved,
// comes to agree with its branch. It is safe to repeat.
func (r *Runner) ResetIndex(ctx context.Context, path string) error {
	_, err := r.InCheckout(ctx, path, "reset", "-q")
	return err
}

// PruneWorktrees drops the registrations of worktrees whose directory is
// gone. A locked worktree survives it, a fork's and a pending merge's
// checkout alike.
func (r *Runner) PruneWorktrees(ctx context.Context, gitDir string) error {
	_, err := r.Isolated(ctx, gitDir, "worktree", "prune")
	return err
}

// RepairWorktrees repairs the pointers between the repository at gitDir and
// the worktrees at paths, which must be named: git worktree repair with no
// path repairs whatever it finds, and agentx repairs only the worktrees it
// means to. When the repository writes relative worktree paths, which git
// 2.48 and newer can be configured to, the repair keeps them relative.
func (r *Runner) RepairWorktrees(ctx context.Context, gitDir string, paths []string) error {
	if len(paths) == 0 {
		return errors.New("no worktree to repair was named")
	}
	relative, err := r.relativeWorktrees(ctx, gitDir)
	if err != nil {
		return err
	}
	args := []string{"worktree", "repair"}
	if relative {
		args = append(args, "--relative-paths")
	}
	_, err = r.Isolated(ctx, gitDir, append(args, paths...)...)
	return err
}

// relativeWorktrees reports whether the repository at gitDir writes
// relative worktree paths: git 2.48 or newer, with worktree.useRelativePaths
// on. An older git has neither the setting nor the flag.
func (r *Runner) relativeWorktrees(ctx context.Context, gitDir string) (bool, error) {
	v, err := r.Version(ctx)
	if err != nil || !v.AtLeast(2, 48) {
		return false, err
	}
	out, _, err := r.IsolatedStatus(ctx, gitDir, 1, "config", "--type=bool", "--get", "worktree.useRelativePaths")
	return strings.TrimSpace(out) == "true", err
}

// gone reports whether nothing is at path.
func gone(path string) bool {
	_, err := os.Lstat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// samePath compares two spellings of one path, resolving symlinks in their
// directories when they differ as written.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(filepath.Dir(a))
	rb, errB := filepath.EvalSymlinks(filepath.Dir(b))
	return errA == nil && errB == nil && filepath.Join(ra, filepath.Base(a)) == filepath.Join(rb, filepath.Base(b))
}

// CheckBranchName asks git whether name is a valid branch name, as git
// check-ref-format --branch answers it, which needs no repository.
func (r *Runner) CheckBranchName(ctx context.Context, name string) error {
	_, err := r.run(ctx, call{isolated: true}, append(isolatedConfig(), "check-ref-format", "--branch", name)...)
	return err
}
