package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// TestServeRunsMaintenance starts serve over a home whose account repo
// holds an install, has no record of a maintenance and holds a maintenance
// lock a killed git left behind. It removes the lock and runs one off its
// loop, after its snapshot: the loose-objects and incremental-repack
// tasks, then the pack-refs task, and never git maintenance start. When a
// run is due is maintenanceDue's, see TestMaintenanceIsDueOnceADay; every
// other serve of the tests finds a fresh record and runs none.
func TestServeRunsMaintenance(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	h.env["AGENTX_CHECK_INTERVAL"] = "1h"
	h.maintain = true
	// The wrapper logs each git maintenance alone, in one write, so that
	// gits run at once beside it never share its line.
	log := filepath.Join(t.TempDir(), "maintenance")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, "#!/bin/sh\nPATH="+os.Getenv("PATH")+"\ncase \" $* \" in *\" maintenance \"*) all=\"$*\"; echo \"${all##* maintenance }\" >> "+log+" ;; esac\nexec "+real+" \"$@\"\n")
	// A lock a killed maintenance left behind would make git skip its
	// work; one an hour old is no running git's, and serve removes it.
	lock := filepath.Join(gitx.AccountRepoPath(h.agentx), "objects", "maintenance.lock")
	writeFile(t, lock, "")
	left := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lock, left, left); err != nil {
		t.Fatal(err)
	}
	since := time.Now()
	p := h.serve(t, "--json")
	p.next("snapshot")
	awaitTrue(t, "maintenance", func() bool {
		last, ok := home.Maintained(h.agentx)
		return ok && !last.Before(since.Truncate(time.Second))
	})
	equal(t, "exit", p.close(), 0)
	if lexists(lock) {
		t.Error("the lock a killed maintenance left behind is still there")
	}
	b, _ := os.ReadFile(log)
	equal(t, "maintenance", strings.TrimSuffix(string(b), "\n"), "run --task=loose-objects\nrun --task=incremental-repack\nrun --task=pack-refs")
}

// TestMaintenanceIsDueOnceADay is when serve maintains the account repo:
// with no record of a run, or one a day old or more, and, so that a clock
// set back never stops it for good, one in the future.
func TestMaintenanceIsDueOnceADay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		what string
		last time.Time
		ok   bool
		want bool
	}{
		{"no record", time.Time{}, false, true},
		{"an hour old", now.Add(-time.Hour), true, false},
		{"a minute short of a day", now.Add(-maintenanceEvery + time.Minute), true, false},
		{"a day old", now.Add(-maintenanceEvery), true, true},
		{"in the future", now.Add(time.Hour), true, true},
	} {
		if got := maintenanceDue(tc.last, tc.ok, now); got != tc.want {
			t.Errorf("%s: due = %v, want %v", tc.what, got, tc.want)
		}
	}
}
