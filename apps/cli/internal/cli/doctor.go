package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

type doctorEvent struct {
	event
	Check  string `json:"check"`
	Status string `json:"status"` // ok, warn, fail or info
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

func newDoctorCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that this machine can run agentx: git, agentx home and the account repo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d := &doctor{inv: inv, table: inv.out.table()}
			err := d.run(cmd.Context())
			if flushErr := d.table.Flush(); err == nil {
				err = flushErr
			}
			return err
		},
	}
}

type doctor struct {
	inv   *invocation
	table *tabwriter.Writer
}

// row reports one check as a doctor event and a table line.
func (d *doctor) row(check, status, detail, hint string) {
	d.inv.out.emit(doctorEvent{event: newEvent("doctor"), Check: check, Status: status, Detail: detail, Hint: hint})
	fmt.Fprintf(d.table, "%s\t%s\t%s", check, status, detail)
	if hint != "" {
		fmt.Fprintf(d.table, "\t%s", hint)
	}
	fmt.Fprintln(d.table)
}

// run performs the checks in a fixed order. A missing or too-old git ends
// the run with exit 2, since nothing after it can be checked; an unusable
// account repo is exit 8 after every row; anything else is reported and
// exits 0.
func (d *doctor) run(ctx context.Context) error {
	inv := d.inv
	v, err := inv.gitVersion(ctx)
	if err != nil {
		f := failureOf(err)
		d.row("git", "fail", f.message, f.hint)
		return err
	}
	d.row("git", "ok", "git "+v.String(), "")

	commit, mergeErr, probeErr := gitx.Probe(ctx, inv.git)
	switch {
	case probeErr != nil:
		d.row("merge_tree", "fail", "cannot set up the probe repository: "+probeErr.Error(), "run with --verbose to see the failing git command")
	case mergeErr != nil:
		d.row("merge_tree", "fail", mergeErr.Error(), "install git 2.40 or newer")
	default:
		d.row("merge_tree", "ok", "merge-tree --write-tree --merge-base merges two branches", "")
	}
	if v.AtLeast(2, 48) {
		d.row("relative_worktree_paths", "info", "available: git "+v.String()+" is 2.48 or newer", "")
	} else {
		d.row("relative_worktree_paths", "info", "not available: git "+v.String()+" is older than 2.48, so worktree paths stay absolute", "")
	}
	switch {
	case commit == "" && probeErr != nil:
		d.row("isolated_commit", "fail", "cannot set up the probe repository: "+probeErr.Error(), "run with --verbose to see the failing git command")
	case commit != gitx.FixedCommit:
		d.row("isolated_commit", "fail", "the isolated environment produced commit "+commit+", not "+gitx.FixedCommit, "run with --verbose to see the git commands")
	default:
		d.row("isolated_commit", "ok", "commit "+commit, "")
	}

	if err := writable(inv.dirs.Home); err != nil {
		d.row("home", "fail", err.Error(), "make "+inv.dirs.Home+" writable")
	} else {
		d.row("home", "ok", inv.dirs.Home+" is writable", "")
	}

	lock := home.LockPath(inv.dirs.Home)
	switch held, err := home.LockHeld(inv.dirs.Home); {
	case err != nil:
		d.row("lock", "fail", err.Error(), "")
	case held:
		d.row("lock", "warn", "held: another agentx command holds "+lock, "wait for it to finish")
	default:
		d.row("lock", "ok", "free: "+lock, "")
	}

	settings := home.SettingsPath(inv.dirs.Home)
	switch _, err := inv.loadSettings(); {
	case err != nil:
		f := failureOf(err)
		d.row("settings", "fail", f.message, f.hint)
	case !exists(settings):
		d.row("settings", "ok", "defaults: "+settings+" is not written yet", "")
	default:
		d.row("settings", "ok", settings+" parses", "")
	}

	var repoErr error
	switch gitDir, created, err := gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home); {
	case errors.Is(err, home.ErrLocked):
		d.row("account_repo", "fail", "cannot create "+gitDir+": "+err.Error(), "")
		repoErr = err
	case err != nil:
		d.row("account_repo", "fail", err.Error(), "")
		repoErr = fail(exitAccountRepo, err.Error(), "check that "+gitDir+" is a bare repository this git can read, or move it aside to have doctor create a new one")
	case created:
		d.row("account_repo", "ok", "created "+gitDir, "")
	default:
		d.row("account_repo", "ok", gitDir+" opens", "")
	}

	if exists(inv.dirs.Library) {
		d.row("library", "ok", inv.dirs.Library, "")
	} else {
		d.row("library", "warn", "missing: "+inv.dirs.Library, "create it with mkdir -p "+inv.dirs.Library)
	}
	return repoErr
}

// writable creates dir when missing and proves a file can be written in it.
func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".doctor.*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// failureOf returns the failure behind err; a plain error becomes an internal one.
func failureOf(err error) *failure {
	var f *failure
	if !errors.As(err, &f) {
		f = &failure{status: exitInternal, message: err.Error()}
	}
	return f
}
