package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
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
			d := &doctor{inv: inv, table: &table{}}
			err := d.run(cmd.Context())
			d.print()
			return err
		},
	}
}

type doctor struct {
	inv    *invocation
	table  *table
	counts map[string]int // rows per status
}

// row reports one check as a doctor event and a table line: a glyph, the
// check, its status and the detail, with the hint on a line of its own
// under the detail.
func (d *doctor) row(check, status, detail, hint string) {
	d.inv.out.emit(doctorEvent{event: newEvent("doctor"), Check: check, Status: status, Detail: detail, Hint: hint})
	if d.counts == nil {
		d.counts = map[string]int{}
	}
	d.counts[status]++
	glyph, st := statusStyle(status)
	out := d.inv.out
	d.table.add(c(out.paint(st, glyph)+" "+out.paint(heading, check), plain), c(status, st), c(detail, plain))
	if hint != "" {
		d.table.add(c("", plain), c("", plain), c(out.paint(warnStyle, "hint:")+" "+hint, plain))
	}
}

// statusStyle maps a doctor status to the glyph its row starts with and the
// style both the glyph and the status word are painted in.
func statusStyle(status string) (string, style) {
	switch status {
	case "ok":
		return glyphOK, okStyle
	case "warn":
		return glyphWarn, warnStyle
	case "fail":
		return glyphFail, failStyle
	}
	return glyphInfo, infoStyle
}

// print writes the table and then one summary line counting the rows by
// status, the failures first.
func (d *doctor) print() {
	out := d.inv.out
	if out.json {
		return
	}
	out.render(d.table, "")
	total := 0
	for _, n := range d.counts {
		total += n
	}
	var parts []string
	for _, s := range []struct{ status, word string }{{"fail", "failed"}, {"warn", "warning"}, {"ok", "ok"}, {"info", "info"}} {
		n := d.counts[s.status]
		if n == 0 {
			continue
		}
		_, st := statusStyle(s.status)
		count := fmt.Sprintf("%d %s", n, s.word)
		if s.status == "warn" {
			count = plural(n, s.word)
		}
		parts = append(parts, out.paint(st, count))
	}
	out.print("")
	out.print(out.paint(heading, plural(total, "check")), ": ", strings.Join(parts, ", "))
}

// run performs the checks in a fixed order and changes nothing in agentx
// home: it creates neither the directory, nor the lock file, nor the
// account repo. A missing or too-old git ends the run with exit 2, since
// nothing after it can be checked; an unusable account repo is exit 8 after
// every row; anything else is reported and exits 0.
func (d *doctor) run(ctx context.Context) error {
	inv := d.inv
	v, err := inv.gitVersion(ctx)
	if err != nil {
		f := failureOf(err)
		d.row("git", "fail", f.message, f.hint)
		return err
	}
	d.row("git", "ok", "git "+v.String(), "")

	const verbose = "run with --verbose to see the git commands"
	commit, mergeErr, probeErr := gitx.Probe(ctx, inv.git)
	switch {
	case probeErr != nil:
		d.row("merge_tree", "fail", "cannot set up the probe repository: "+probeErr.Error(), verbose)
	case mergeErr != nil:
		d.row("merge_tree", "fail", mergeErr.Error(), verbose)
	default:
		d.row("merge_tree", "ok", "merge-tree --write-tree --merge-base merges two branches", "")
	}
	if v.AtLeast(2, 48) {
		d.row("relative_worktree_paths", "info", "available: git "+v.String()+" is 2.48 or newer", "")
	} else {
		d.row("relative_worktree_paths", "info", "not available: git "+v.String()+" is older than 2.48", "")
	}
	switch {
	case commit == "":
		d.row("isolated_commit", "fail", "cannot set up the probe repository: "+probeErr.Error(), verbose)
	case commit != gitx.FixedCommit:
		d.row("isolated_commit", "fail", "the isolated environment produced commit "+commit+", not "+gitx.FixedCommit, verbose)
	default:
		d.row("isolated_commit", "ok", "commit "+commit, "")
	}

	h := inv.dirs.Home
	switch info, err := os.Stat(h); {
	case errors.Is(err, fs.ErrNotExist):
		d.row("home", "ok", "not created yet: "+h+"; the first scan creates it", "")
	case err != nil:
		d.row("home", "fail", err.Error(), "")
	case !info.IsDir():
		d.row("home", "fail", h+" is not a directory", "move it aside")
	case unix.Access(h, unix.W_OK) != nil:
		d.row("home", "fail", h+" is not writable", "make "+h+" writable")
	default:
		d.row("home", "ok", h+" is writable", "")
	}

	lock := home.LockPath(inv.dirs.Home)
	switch held, pid, err := home.LockHeld(inv.dirs.Home); {
	case err != nil:
		d.row("lock", "fail", err.Error(), "")
	case held && pid != "":
		d.row("lock", "warn", "held by process "+pid+": "+lock, "wait for it to finish")
	case held:
		d.row("lock", "warn", "held by another agentx command: "+lock, "wait for it to finish")
	default:
		d.row("lock", "ok", "free: "+lock, "")
	}

	// Reported, never recovered: doctor holds no lock.
	switch journals, err := home.Journals(inv.dirs.Home); {
	case err != nil:
		d.row("mutations", "fail", err.Error(), "")
	case len(journals) > 0:
		d.row("mutations", "warn", plural(len(journals), "unfinished mutation")+": "+journals[0], "run agentx scan to recover; when it refuses, restore the file it names or move the journal aside")
	default:
		d.row("mutations", "ok", "none unfinished: "+home.MutationsDir(inv.dirs.Home), "")
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
	switch gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home); {
	case err != nil:
		d.row("account_repo", "fail", err.Error(), "")
		repoErr = fail(exitAccountRepo, err.Error(), "check that "+gitDir+" is a bare repository this git can read, or move it aside")
	case !exists:
		d.row("account_repo", "ok", "not created yet: "+gitDir, "")
	default:
		d.row("account_repo", "ok", gitDir+" opens", "")
	}

	if exists(inv.dirs.Library) {
		d.row("library", "ok", inv.dirs.Library, "")
	} else {
		d.row("library", "warn", "missing: "+inv.dirs.Library, "create it with mkdir -p "+inv.dirs.Library)
	}

	detected := scan.Detect(inv.dirs)
	for _, c := range detected {
		d.row("client:"+c.Slug(), "ok", c.Name()+": "+c.ConfigDir(inv.dirs), "")
	}
	summary := fmt.Sprintf("%d of %d registered clients detected", len(detected), scan.Registered())
	if len(detected) == 0 {
		d.row("clients", "warn", summary, "install an agent client or check HOME")
	} else {
		d.row("clients", "info", summary, "")
	}
	return repoErr
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
