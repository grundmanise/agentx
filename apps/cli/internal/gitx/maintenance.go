package gitx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// PackObjects runs the object tasks of git maintenance over the repository
// at gitDir, in the isolated environment: loose-objects, which packs the
// loose objects and drops the ones a pack already holds, then
// incremental-repack, which gathers small packs behind a multi-pack-index.
// Neither drops an object nothing refers to, so a commit a command wrote
// before the lock and no ref holds yet survives them. incremental-repack
// fails on a repository with no pack at all, as a new one is, so it runs
// only once a pack is there.
func (r *Runner) PackObjects(ctx context.Context, gitDir string) error {
	if _, err := r.Isolated(ctx, gitDir, "maintenance", "run", "--task=loose-objects"); err != nil {
		return err
	}
	if !hasPack(gitDir) {
		return nil
	}
	_, err := r.Isolated(ctx, gitDir, "maintenance", "run", "--task=incremental-repack")
	return err
}

// PackRefs gathers the loose refs of the repository at gitDir into its
// packed-refs file, as the pack-refs task of git maintenance does. A git
// older than that task, which answers that it is not a valid task, runs
// git pack-refs --all instead, which is what the task runs. It takes the
// repository's packed-refs lock, so the caller holds agentx's own lock,
// under which every ref write of agentx is made.
func (r *Runner) PackRefs(ctx context.Context, gitDir string) error {
	_, err := r.Isolated(ctx, gitDir, "maintenance", "run", "--task=pack-refs")
	if err != nil && strings.Contains(err.Error(), "not a valid task") {
		_, err = r.Isolated(ctx, gitDir, "pack-refs", "--all")
	}
	return err
}

// hasPack reports whether the repository at gitDir holds a pack file.
func hasPack(gitDir string) bool {
	matches, _ := filepath.Glob(filepath.Join(gitDir, "objects", "pack", "*.pack"))
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}
