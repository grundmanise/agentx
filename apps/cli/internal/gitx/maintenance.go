package gitx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	if err := r.maintain(ctx, gitDir, "maintenance", "run", "--task=loose-objects"); err != nil {
		return err
	}
	if !hasPack(gitDir) {
		return nil
	}
	return r.maintain(ctx, gitDir, "maintenance", "run", "--task=incremental-repack")
}

// PackRefs gathers the loose refs of the repository at gitDir into its
// packed-refs file, as the pack-refs task of git maintenance does. A git
// older than that task, which answers that it is not a valid task, runs
// git pack-refs --all instead, which is what the task runs. It takes the
// repository's packed-refs lock, so the caller holds agentx's own lock,
// under which every ref write of agentx is made.
func (r *Runner) PackRefs(ctx context.Context, gitDir string) error {
	err := r.maintain(ctx, gitDir, "maintenance", "run", "--task=pack-refs")
	if err != nil && strings.Contains(err.Error(), "not a valid task") {
		err = r.maintain(ctx, gitDir, "pack-refs", "--all")
	}
	return err
}

// ErrMaintenanceRunning is the answer of a maintenance task to a
// repository whose maintenance lock is taken: git would skip the task and
// exit 0 having done nothing, so the task is not run, and is not done.
var ErrMaintenanceRunning = errors.New("git maintenance is already running in the account repo")

// maintain runs one maintenance git over the repository at gitDir, in the
// isolated environment, unless git maintenance holds the repository's
// maintenance lock, see ErrMaintenanceRunning. A cancelled context stops
// it with SIGTERM, on which git removes the lock files it took: a lock
// left behind by a git killed outright would make every later maintenance
// skip its work, and a packed-refs lock would fail every ref deletion.
func (r *Runner) maintain(ctx context.Context, gitDir string, args ...string) error {
	if _, err := os.Lstat(filepath.Join(gitDir, "objects", "maintenance.lock")); err == nil {
		return ErrMaintenanceRunning
	}
	_, err := r.run(ctx, call{isolated: true, terminate: true}, isolatedArgs(gitDir, args)...)
	return err
}

// LeftLocks are the lock files maintenance takes in the repository at
// gitDir, its maintenance lock and its packed-refs lock, that were last
// modified before before: ones no git still running holds, but a git
// killed outright, which had no chance to remove them, left behind.
func LeftLocks(gitDir string, before time.Time) []string {
	var left []string
	for _, lock := range []string{filepath.Join(gitDir, "objects", "maintenance.lock"), filepath.Join(gitDir, "packed-refs.lock")} {
		if info, err := os.Lstat(lock); err == nil && info.ModTime().Before(before) {
			left = append(left, lock)
		}
	}
	return left
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
