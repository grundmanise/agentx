package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/serve"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

type refreshCompleteEvent struct {
	event
	RequestID   string `json:"request_id"`
	InstanceID  string `json:"instance_id"`
	OK          bool   `json:"ok"`
	ScanCounter int    `json:"scan_counter,omitempty"` // absent when the scan failed: no freshness is claimed
	Error       string `json:"error,omitempty"`
}

// searchEvent answers one search request: every skill of every source
// whose name or description matches, each naming its source by the
// canonical URL, which is what source list prints and what an install
// takes.
type searchEvent struct {
	event
	RequestID  string         `json:"request_id"`
	InstanceID string         `json:"instance_id"`
	Query      string         `json:"query"`
	Results    []source.Match `json:"results"` // empty, never absent, when nothing matched
}

func newServeCommand(inv *invocation) *cobra.Command {
	var once bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Watch for changes and stream a snapshot on each one, until stdin closes",
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
			inv.instanceID() // fixed here, before two goroutines report it
			dirs, trees := inv.watchedDirs()
			err = serve.Run(cmd.Context(), serve.Options{
				Scan:  func(ctx context.Context) (scan.Snapshot, error) { return inv.scan(ctx, 0, "", false) },
				Index: inv.sourceIndex,
				Watch: dirs,
				Trees: trees,
				Once:  once,
				Stdin: cmd.InOrStdin(),
				Snapshot: func(snap scan.Snapshot) {
					inv.out.emit(snapshotEvent{event: newEvent("snapshot"), Snapshot: snap})
					inv.out.print(inv.out.paint(heading, fmt.Sprintf("snapshot %d", snap.ScanCounter)), ": ",
						plural(len(snap.Configurations), "configuration"), ", ", plural(len(snap.Skills), "skill"))
				},
				RefreshComplete: func(id string, counter int, err error) {
					ev := refreshCompleteEvent{event: newEvent("refresh_complete"), RequestID: id, InstanceID: inv.instanceID(), OK: err == nil, ScanCounter: counter}
					if err != nil {
						ev.Error = err.Error()
						inv.out.print(inv.out.paint(heading, "refresh "+id), ": ", inv.out.paint(failStyle, "failed"), ": ", err.Error())
					} else {
						inv.out.print(inv.out.paint(heading, "refresh "+id), ": ", inv.out.paint(okStyle, "ok"), fmt.Sprintf(", snapshot %d", counter))
					}
					inv.out.emit(ev)
				},
				Search: func(id, query string, results []source.Match) {
					inv.out.emit(searchEvent{event: newEvent("search"), RequestID: id, InstanceID: inv.instanceID(), Query: query, Results: results})
					inv.out.print(inv.out.paint(heading, "search "+id), ": ", plural(len(results), "result"))
				},
				BadRequest: func(message, hint string) {
					inv.out.fail(&failure{status: exitUsage, message: message, hint: hint})
				},
				Warn: inv.out.warn,
			})
			if errors.Is(err, serve.ErrWatch) {
				return fail(exitRefused, err.Error(), "raise the system's watch limit: fs.inotify.max_user_watches on Linux, open files (ulimit -n) on macOS")
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "scan once, emit the snapshot and exit")
	return cmd
}

// watchedDirs are the directories a change signal can come from, most
// important first: agentx home holds the version file every mutation
// rewrites; the account repo, the worktrees, the library and every agent
// client's user-scope skills directory hold content. Everything but agentx
// home and the account repo is a tree: an edit anywhere inside a skill is a
// signal too, wherever the scan reads that skill from. A skills directory a
// client shares with another, and the library itself, are watched once.
func (inv *invocation) watchedDirs() (dirs, trees []string) {
	h := inv.dirs.Home
	trees = []string{filepath.Join(h, "worktrees"), inv.dirs.Library}
	for _, dir := range scan.UserSkillsDirs(inv.dirs) {
		if !slices.Contains(trees, dir) {
			trees = append(trees, dir)
		}
	}
	return append([]string{h, gitx.AccountRepoPath(h)}, trees...), trees
}

// sourceIndex builds the index serve answers searches from: the sources of
// the machine settings, each listed from its ref in the account repo. The
// settings are read under the shared lock, as a scan reads them, so a
// source being added or removed is never seen half written; the listing
// itself reads objects, which no mutation rewrites. A search reads the
// index this returns and takes no lock at all.
func (inv *invocation) sourceIndex(ctx context.Context, prev *source.Index) (idx *source.Index, warnings []string, err error) {
	err = home.ReadLocked(ctx, inv.dirs.Home, func() error {
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		urls := make([]string, 0, len(s.Sources))
		for _, src := range s.Sources {
			urls = append(urls, src.URL)
		}
		idx, warnings, err = source.BuildIndex(ctx, inv.git, gitx.AccountRepoPath(inv.dirs.Home), urls, prev)
		return err
	})
	return idx, warnings, err
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
