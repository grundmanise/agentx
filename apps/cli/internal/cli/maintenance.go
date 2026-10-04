package cli

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// maintenanceEvery is how often the account repo is maintained: at most
// once a day. The time of the last run is kept in agentx home, see
// home.Maintained, and every serve child asks it once, at start, so a run
// falls due on the first start after the day has passed.
//
// maintenanceLeft is how old a lock file of git maintenance is when no git
// still holds it, see clearLeftLocks.
const (
	maintenanceEvery = 24 * time.Hour
	maintenanceLeft  = time.Hour
)

// maintenanceJob is the serve child's maintenance of the account repo, run
// once at start; it maintains the repo when that is due, see
// serveMaintenance. What it could not do is a warning.
func (inv *invocation) maintenanceJob(ctx context.Context) func() {
	err := inv.serveMaintenance(ctx)
	return func() {
		if err != nil {
			inv.out.warn("maintenance: " + err.Error())
		}
	}
}

// serveMaintenance maintains the account repo when the last maintenance is
// a day old or more, or never ran: git maintenance run with the
// loose-objects and incremental-repack tasks, which pack what commands
// wrote loose and gather small packs, then the pack-refs task, which packs
// the branches and refs. None of them drops an object, reachable or not.
// git maintenance start is never run: it would install a job of its own
// with the system's scheduler, and fetch on a timer of its own.
//
// The object tasks run without agentx's lock: git writes a pack whole
// before it drops what the pack holds, and a command reading or writing
// objects meanwhile finds every object in one place or the other. pack-refs
// takes the repository's packed-refs lock, which a ref write of a command
// would fail on, so it runs under agentx's lock, waiting for it as the
// update check does and without the change signal, since packed refs hold
// what loose ones held. A machine with no account repo has nothing to
// maintain and runs no git.
//
// Serve stopping stops a run part way with SIGTERM, on which git removes
// its lock files, and the run is due again at the next start. A run that
// finds git's maintenance lock taken is not done either: git would skip
// its work and say it succeeded.
func (inv *invocation) serveMaintenance(ctx context.Context) error {
	if last, ok := home.Maintained(inv.dirs.Home); !maintenanceDue(last, ok, time.Now()) {
		return nil
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil || !exists {
		return err
	}
	if err := inv.clearLeftLocks(ctx, gitDir); err != nil {
		return err
	}
	err = inv.git.PackObjects(ctx, gitDir)
	if err == nil {
		err = home.MutateQuietWaiting(ctx, inv.dirs.Home, inv.refs(ctx), func() error {
			if err := inv.git.PackRefs(ctx, gitDir); err != nil {
				return err
			}
			return home.SetMaintained(inv.dirs.Home, time.Now())
		})
	}
	if errors.Is(err, gitx.ErrMaintenanceRunning) {
		// Another serve child's maintenance, most likely: the run is
		// still due, and the next ask makes it once that one is done.
		inv.out.debugf("maintenance: %v", err)
		return nil
	}
	return err
}

// clearLeftLocks removes the lock files maintenance takes in the account
// repo at gitDir that are older than maintenanceLeft, under agentx's lock:
// a git maintenance killed outright, by a crash or a forced quit, has no
// chance to remove its own, and git would then skip every later run and
// fail every ref deletion. No maintenance runs that long, and agentx's
// lock keeps every ref write of agentx's away meanwhile.
func (inv *invocation) clearLeftLocks(ctx context.Context, gitDir string) error {
	if len(gitx.LeftLocks(gitDir, time.Now().Add(-maintenanceLeft))) == 0 {
		return nil
	}
	return home.MutateQuietWaiting(ctx, inv.dirs.Home, inv.refs(ctx), func() error {
		for _, lock := range gitx.LeftLocks(gitDir, time.Now().Add(-maintenanceLeft)) {
			if err := os.Remove(lock); err != nil && !os.IsNotExist(err) {
				return err
			}
			inv.out.debugf("maintenance: removed %s, left behind by a git that was killed", lock)
		}
		return nil
	})
}

// maintenanceDue reports whether the account repo is due for maintenance
// at now, last being when it was last maintained, as home.Maintained reads
// it, and ok false when the file that says so is missing or does not hold
// a time: then, or when last is a day or more before now. A time in the
// future, as a clock set back leaves, is due too, so a wrong clock never
// stops maintenance for good.
func maintenanceDue(last time.Time, ok bool, now time.Time) bool {
	return !ok || now.Sub(last) >= maintenanceEvery || last.After(now)
}
