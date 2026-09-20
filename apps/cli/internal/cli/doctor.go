package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

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
			d := &doctor{inv: inv}
			err := d.run(cmd.Context())
			d.print()
			return err
		},
	}
}

type doctor struct {
	inv  *invocation
	rows []doctorRow
}

// doctorRow is one reported check, kept until print groups it by section.
type doctorRow struct {
	check, status, detail, hint string
}

// The sections of the text report, in print order. A check belongs to the
// first section whose prefix matches, or to the last one.
var doctorSections = []struct {
	title  string
	checks []string
}{
	{"System", []string{"git", "merge_tree", "relative_worktree_paths", "isolated_commit", "home"}},
	{"App", []string{"lock", "mutations", "settings", "account_repo", "library"}},
	{"Clients", nil}, // client:<id> and clients
}

// row reports one check as a doctor event and keeps it for the text report.
func (d *doctor) row(check, status, detail, hint string) {
	d.inv.out.emit(doctorEvent{event: newEvent("doctor"), Check: check, Status: status, Detail: detail, Hint: hint})
	d.rows = append(d.rows, doctorRow{check, status, detail, hint})
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

// print writes the text report: one block per section with a glyph, the
// check, its status and the detail on each row, then the warnings and
// failures again under Issues with their hints, or one line saying there
// are none.
func (d *doctor) print() {
	out := d.inv.out
	if out.json {
		return
	}
	t := &table{}
	for i, section := range doctorSections {
		rows := d.section(i)
		if len(rows) == 0 {
			continue
		}
		if len(t.rows) > 0 {
			t.add(c("", plain))
		}
		t.add(c(out.paint(heading, section.title), plain))
		for _, r := range rows {
			glyph, st := statusStyle(r.status)
			t.add(c("  "+out.paint(st, glyph)+" "+r.check, plain), c(r.status, st), c(r.detail, plain))
		}
	}
	out.render(t, "")
	out.print("")
	var issues []doctorRow
	for _, r := range d.rows {
		if r.status == "warn" || r.status == "fail" {
			issues = append(issues, r)
		}
	}
	if len(issues) == 0 {
		out.print(out.paint(okStyle, glyphOK), " No issues detected")
		return
	}
	t = &table{}
	t.add(c(out.paint(heading, plural(len(issues), "issue")), plain))
	for _, r := range issues {
		glyph, st := statusStyle(r.status)
		t.add(c("  "+out.paint(st, glyph)+" "+out.paint(heading, r.check), plain), c(r.detail, plain))
		if r.hint != "" {
			t.add(c("", plain), c(out.paint(warnStyle, "hint:")+" "+r.hint, plain))
		}
	}
	out.render(t, "")
}

// section returns the rows of section i in the order they were reported.
func (d *doctor) section(i int) []doctorRow {
	var rows []doctorRow
	for _, r := range d.rows {
		if sectionOf(r.check) == i {
			rows = append(rows, r)
		}
	}
	return rows
}

func sectionOf(check string) int {
	for i, section := range doctorSections {
		if slices.Contains(section.checks, check) {
			return i
		}
	}
	return len(doctorSections) - 1
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
		d.row("client:"+c.Slug(), "info", c.Name()+": "+c.ConfigDir(inv.dirs), "")
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
