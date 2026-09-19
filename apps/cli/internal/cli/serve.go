package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/serve"
)

type refreshCompleteEvent struct {
	event
	RequestID   string `json:"request_id"`
	InstanceID  string `json:"instance_id"`
	OK          bool   `json:"ok"`
	ScanCounter int    `json:"scan_counter,omitempty"` // absent when the scan failed: no freshness is claimed
	Error       string `json:"error,omitempty"`
}

func newServeCommand(inv *invocation) *cobra.Command {
	var once bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Watch this machine and stream a snapshot whenever it changes, until stdin closes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lock, err := home.TakeServeLock(inv.dirs.Home)
			if errors.Is(err, home.ErrServing) {
				return fail(exitRefused, err.Error(), "stop the agentx serve holding "+home.ServeLockPath(inv.dirs.Home)+" first")
			}
			if err != nil {
				return err
			}
			defer lock.Close()
			return serve.Run(cmd.Context(), serve.Options{
				Scan:  func(ctx context.Context) (scan.Snapshot, error) { return inv.scan(ctx, 0, "", false) },
				Watch: inv.watchedDirs(),
				Once:  once,
				Stdin: cmd.InOrStdin(),
				Snapshot: func(snap scan.Snapshot) {
					inv.out.emit(snapshotEvent{event: newEvent("snapshot"), Snapshot: snap})
					inv.out.printf("snapshot %d: %s, %s\n", snap.ScanCounter,
						plural(len(snap.Configurations), "configuration"), plural(len(snap.Skills), "skill"))
				},
				RefreshComplete: func(id string, counter int, err error) {
					ev := refreshCompleteEvent{event: newEvent("refresh_complete"), RequestID: id, InstanceID: inv.instanceID(), OK: err == nil, ScanCounter: counter}
					if err != nil {
						ev.Error = err.Error()
						inv.out.printf("refresh %s: failed: %s\n", id, err)
					} else {
						inv.out.printf("refresh %s: ok, snapshot %d\n", id, counter)
					}
					inv.out.emit(ev)
				},
				BadRequest: func(message, hint string) {
					inv.out.fail(&failure{status: exitUsage, message: message, hint: hint})
				},
				Warn: inv.out.warn,
			})
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "scan once, emit the snapshot and exit")
	return cmd
}

// watchedDirs are the directories a change signal can come from, most
// important first: agentx home holds the version file every mutation
// rewrites; the account repo, the worktrees and the library hold content.
func (inv *invocation) watchedDirs() []string {
	h := inv.dirs.Home
	return []string{h, gitx.AccountRepoPath(h), filepath.Join(h, "worktrees"), inv.dirs.Library}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
