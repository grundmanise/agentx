package gitx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
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

// AddForkWorktree adds path as a linked worktree of the repository at
// gitDir on branch, the short name of a fork's branch such as skills/pdf,
// locked with ForkReason and with nothing checked out: the fork's content
// is laid out in it by the command, and ResetIndex then aligns the index.
// The branch is passed by its short name, since git detaches HEAD at a full
// ref name.
//
// The path is absent, an empty directory, or holds nothing but a .git file.
// What a fork's worktree removed by hand, or an add killed part way, left
// of path is cleared first, see clearForkWorktree. Registrations whose
// directory is gone are then pruned, which a locked worktree survives. A
// branch checked out at another existing path is refused, as git refuses
// it.
func (r *Runner) AddForkWorktree(ctx context.Context, gitDir, path, branch string) error {
	if err := clearForkWorktree(gitDir, path); err != nil {
		return err
	}
	list, err := r.Worktrees(ctx, gitDir)
	if err != nil {
		return err
	}
	prune := false
	for _, w := range list {
		switch {
		case w.Prunable:
			prune = true
		case w.Bare:
		case w.Branch == "refs/heads/"+branch && !gone(w.Path):
			return fmt.Errorf("%s is checked out at %s already", branch, w.Path)
		}
	}
	if prune {
		if err := r.PruneWorktrees(ctx, gitDir); err != nil {
			return err
		}
	}
	_, err = r.Isolated(ctx, gitDir, "worktree", "add", "--no-checkout", "--lock", "--reason", ForkReason, path, branch)
	return err
}

// clearForkWorktree removes what an earlier worktree of path left, with
// files alone, since git refuses to clear most of it. git worktree add
// writes, in order, the admin directory and its lock, the worktree's
// directory, the admin directory's gitdir, the worktree's .git file, then
// HEAD and commondir, and a SIGKILL or a power loss can stop it after any
// of them. git worktree remove then fails validation, and a plain add
// refuses the locked registration or the .git file. So:
//
//   - a .git file that is all path holds goes;
//   - a registration whose gitdir names path goes, which is also what a
//     fork's worktree removed by hand leaves, locked so that a prune keeps
//     it;
//   - an admin directory with no gitdir goes when it holds nothing else but
//     a fork's lock, since it can only be an add stopped before it wrote
//     the gitdir, and nothing else would ever remove it: git skips it when
//     it lists or prunes worktrees, and the next add takes another name.
func clearForkWorktree(gitDir, path string) error {
	if entries, err := os.ReadDir(path); err == nil && len(entries) == 1 && entries[0].Name() == ".git" && entries[0].Type().IsRegular() {
		if err := os.Remove(filepath.Join(path, ".git")); err != nil {
			return err
		}
	}
	admins := filepath.Join(gitDir, "worktrees")
	entries, err := os.ReadDir(admins)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		admin := filepath.Join(admins, e.Name())
		if e.IsDir() && (home.NamesBack(admin, path) || unwritten(admin)) {
			if err := os.RemoveAll(admin); err != nil {
				return err
			}
		}
	}
	return nil
}

// unwritten reports whether the admin directory is one a fork's worktree
// add stopped in before it wrote the gitdir: empty, or holding a fork's
// lock alone.
func unwritten(admin string) bool {
	entries, err := os.ReadDir(admin)
	if err != nil || len(entries) > 1 {
		return false
	}
	if len(entries) == 0 {
		return true
	}
	b, err := os.ReadFile(filepath.Join(admin, "locked"))
	return err == nil && strings.TrimRight(string(b), "\n") == ForkReason
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

// CheckBranchName asks git whether name is a valid branch name, as git
// check-ref-format --branch answers it, which needs no repository.
func (r *Runner) CheckBranchName(ctx context.Context, name string) error {
	_, err := r.run(ctx, call{isolated: true}, append(isolatedConfig(), "check-ref-format", "--branch", name)...)
	return err
}
