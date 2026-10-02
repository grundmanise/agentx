package cli

import (
	"context"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/serve"
)

// maintenanceEvery is how often the account repo is maintained: once a
// day. A serve child lives as long as the desktop app that holds it open,
// often far less than a day, so the time of the last run is kept in agentx
// home, see home.Maintained, and every serve child asks it at start and
// then once a day.
const maintenanceEvery = 24 * time.Hour

// maintenanceTick is the serve child's maintenance of the account repo, on
// a timer of its own: at start and then once a day, see serveMaintenance.
// What it could not do is warned about once per cause, as a source the
// update check cannot fetch is: the same failure on the next day's run is a
// debug line.
func (inv *invocation) maintenanceTick() serve.Tick {
	var last string // the cause of the last run's failure, "" after one that worked
	return serve.Tick{Every: maintenanceEvery, AtStart: true, Run: func(ctx context.Context) func() {
		err := inv.serveMaintenance(ctx)
		return func() {
			cause := ""
			if err != nil {
				cause = failureCause(err.Error())
			}
			switch cause {
			case "":
			case last:
				inv.out.debugf("maintenance: %s", err)
			default:
				inv.out.warn("maintenance: " + err.Error())
			}
			last = cause
		}
	}}
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
func (inv *invocation) serveMaintenance(ctx context.Context) error {
	if !inv.maintenanceDue(time.Now()) {
		return nil
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil || !exists {
		return err
	}
	if err := inv.git.PackObjects(ctx, gitDir); err != nil {
		return err
	}
	return home.MutateQuietWaiting(ctx, inv.dirs.Home, inv.refs(ctx), func() error {
		if err := inv.git.PackRefs(ctx, gitDir); err != nil {
			return err
		}
		return home.SetMaintained(inv.dirs.Home, time.Now())
	})
}

// maintenanceDue reports whether the account repo is due for maintenance
// at now: the file that says when it was last maintained is missing, does
// not hold a time, or holds one a day or more before now. A time in the
// future, as a clock set back leaves, is due too, so a wrong clock never
// stops maintenance for good.
func (inv *invocation) maintenanceDue(now time.Time) bool {
	last, ok := home.Maintained(inv.dirs.Home)
	return !ok || now.Sub(last) >= maintenanceEvery || last.After(now)
}
