package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// TestServeRunsMaintenanceOnceADay starts serve three times over one home
// whose account repo holds an install. The first serve finds no record of
// a maintenance and runs it off its loop, after its snapshot: the
// loose-objects and incremental-repack tasks, then the pack-refs task,
// and never git maintenance start. The second, within the day, runs none.
// The third finds the last maintenance a day old and runs it again.
func TestServeRunsMaintenanceOnceADay(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
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
	maintenance := func(from int) []string {
		b, _ := os.ReadFile(log)
		lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if len(b) == 0 || from >= len(lines) {
			return nil
		}
		return lines[from:]
	}
	serveUntilMaintained := func(since time.Time) {
		t.Helper()
		p := h.serve(t, "--json")
		p.next("snapshot")
		awaitTrue(t, "maintenance", func() bool {
			last, ok := home.Maintained(h.agentx)
			return ok && !last.Before(since.Truncate(time.Second))
		})
		equal(t, "exit", p.close(), 0)
	}
	want := "run --task=loose-objects\nrun --task=incremental-repack\nrun --task=pack-refs"

	serveUntilMaintained(time.Now())
	equal(t, "the first serve's maintenance", strings.Join(maintenance(0), "\n"), want)

	n := len(maintenance(0))
	p := h.serve(t, "--json")
	p.next("snapshot")
	p.send(`{"type":"refresh","request_id":"r1"}`)
	p.next("refresh_complete")
	equal(t, "exit", p.close(), 0)
	equal(t, "the second serve's maintenance", strings.Join(maintenance(n), "\n"), "")

	if err := home.SetMaintained(h.agentx, time.Now().Add(-maintenanceEvery)); err != nil {
		t.Fatal(err)
	}
	n = len(maintenance(0))
	serveUntilMaintained(time.Now())
	equal(t, "the third serve's maintenance", strings.Join(maintenance(n), "\n"), want)
}
