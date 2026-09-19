package gitx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// AccountRepoPath is the git directory of the account repo in agentx home.
func AccountRepoPath(homeDir string) string { return filepath.Join(homeDir, "account.git") }

// OpenAccountRepo returns the git directory of the account repo in agentx
// home, creating the repo under the lock when it does not exist yet. A
// present repo that git cannot read is an error; so is one that cannot be
// created. created reports whether this call made it.
func OpenAccountRepo(ctx context.Context, r *Runner, homeDir string) (gitDir string, created bool, err error) {
	gitDir = AccountRepoPath(homeDir)
	_, err = os.Stat(gitDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = home.Mutate(homeDir, func() error {
			if _, err := os.Stat(gitDir); err == nil {
				return nil // created meanwhile by another command
			}
			created = true
			return createAccountRepo(ctx, r, gitDir)
		})
		return gitDir, created, err
	case err != nil:
		return gitDir, false, fmt.Errorf("account repo %s: %w", gitDir, err)
	}
	if out, err := r.Isolated(ctx, gitDir, "rev-parse", "--is-bare-repository"); err != nil || out != "true" {
		if err == nil {
			err = errors.New("not a bare repository")
		}
		return gitDir, false, fmt.Errorf("account repo %s: %w", gitDir, err)
	}
	return gitDir, false, nil
}

// createAccountRepo initialises the bare repo with the configuration agentx
// needs, in a temporary directory that is renamed into place only once every
// step succeeded, so a failure leaves no half-configured repo behind.
func createAccountRepo(ctx context.Context, r *Runner, gitDir string) error {
	v, err := r.Version(ctx)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(gitDir), ".account.git.*")
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
	if v.AtLeast(2, 48) {
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
		return fmt.Errorf("account repo %s: %w", gitDir, err)
	}
	return nil
}
