package gitx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
)

// StorePath is the git directory of the store in agentx home.
func StorePath(homeDir string) string { return filepath.Join(homeDir, "store.git") }

// CheckStore reports whether the store exists in agentx home
// and, when it does, that git reads it as a bare repository. It changes
// nothing.
func CheckStore(ctx context.Context, r *Runner, homeDir string) (gitDir string, exists bool, err error) {
	gitDir = StorePath(homeDir)
	_, err = os.Stat(gitDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return gitDir, false, nil
	case err != nil:
		return gitDir, false, fmt.Errorf("store %s: %w", gitDir, err)
	}
	if out, err := r.Isolated(ctx, gitDir, "rev-parse", "--is-bare-repository"); err != nil || out != "true" {
		if err == nil {
			err = errors.New("not a bare repository")
		}
		return gitDir, true, fmt.Errorf("store %s: %w", gitDir, err)
	}
	return gitDir, true, nil
}

// OpenStore returns the git directory of the store in agentx
// home, creating the repo under the lock when it does not exist yet. A
// present repo that git cannot read is an error; so is one that cannot be
// created. created reports whether this call made it.
func OpenStore(ctx context.Context, r *Runner, homeDir string) (gitDir string, created bool, err error) {
	return openStore(ctx, r, homeDir, func(u home.RefUpdater, fn func() error) error { return home.Mutate(homeDir, u, fn) })
}

// OpenStoreWaiting is OpenStore for the serve child, which waits for the
// lock until ctx is done rather than giving up on it.
func OpenStoreWaiting(ctx context.Context, r *Runner, homeDir string) (gitDir string, created bool, err error) {
	return openStore(ctx, r, homeDir, func(u home.RefUpdater, fn func() error) error {
		return home.MutateWaiting(ctx, homeDir, u, fn)
	})
}

func openStore(ctx context.Context, r *Runner, homeDir string, mutate func(home.RefUpdater, func() error) error) (gitDir string, created bool, err error) {
	gitDir, exists, err := CheckStore(ctx, r, homeDir)
	if err != nil || exists {
		return gitDir, false, err
	}
	// The recovery this Mutate runs before its own work finishes the journal
	// of an earlier command, whose ref steps a stop may not cut in half; the
	// creation itself keeps the command's context and gives up on a stop
	// like anything else, leaving the temporary directory behind it.
	err = mutate(r.Refs(interrupt.Uninterruptible(ctx)), func() error {
		if _, err := os.Stat(gitDir); err == nil {
			return nil // created meanwhile by another command
		}
		created = true
		return createStore(ctx, r, gitDir)
	})
	return gitDir, created, err
}

// createStore initialises the bare repo with the configuration agentx
// needs, in a temporary directory that is renamed into place only once every
// step succeeded, so a failure leaves no half-configured repo behind.
func createStore(ctx context.Context, r *Runner, gitDir string) error {
	tmp, err := os.MkdirTemp(filepath.Dir(gitDir), ".store.git.*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	config := [][2]string{
		{"gc.auto", "0"},                  // maintenance runs on the serve child's timer, never inside a command
		{"core.logAllRefUpdates", "true"}, // a bare repo has no reflogs by default
		{"merge.conflictStyle", "zdiff3"},
	}
	// A git that can write the worktree paths of the store relative
	// to it is told to: a fork's worktree then survives agentx home being
	// moved as a whole. An existing repo is never switched over, since the
	// first relative worktree marks the repo as one an older git cannot
	// read.
	if v, err := r.Version(ctx); err == nil && v.AtLeast(2, 48) {
		config = append(config, [2]string{"worktree.useRelativePaths", "true"})
	}
	if _, err := r.Isolated(ctx, tmp, "init", "--bare", "--quiet"); err != nil {
		return err
	}
	for _, kv := range config {
		if _, err := r.Isolated(ctx, tmp, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, gitDir); err != nil {
		return fmt.Errorf("store %s: %w", gitDir, err)
	}
	return nil
}
