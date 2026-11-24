package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/crash"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// issuesURL is where a user reports a bug.
const issuesURL = "https://github.com/grundmanise/agentx/issues"

// testPanic makes scan panic, with a message that names HOME and the
// agentx home, when AGENTX_TEST_PANIC is 1: it is how the tests reach a
// crash through the seam.
func (inv *invocation) testPanic() {
	if inv.env["AGENTX_TEST_PANIC"] != "1" {
		return
	}
	panic(fmt.Errorf("test panic: open %s and %s", home.SettingsPath(inv.dirs.Home), filepath.Join(inv.dirs.User, "project")))
}

// crashPaths are the directories a crash report of this run names by a
// placeholder. They are read from the environment again, since a panic
// can come before the run resolved its directories.
func (inv *invocation) crashPaths() crash.Paths {
	dirs, _ := home.Resolve(inv.env)
	return crash.Paths{
		Home:    dirs.Home,
		Library: dirs.Library,
		Config:  dirs.Config,
		Temp:    inv.env["TMPDIR"],
		User:    dirs.User,
	}
}

// reportCrash spools p as an exception record when telemetry is
// recording: its type, its message and its frames, with every user path
// replaced. The run's own record follows it, and starts the flush that
// sends both.
func (inv *invocation) reportCrash(ctx context.Context, p *crash.Panic) {
	exceptionType, exceptionValue, frames := p.Report(inv.crashPaths())
	inv.spoolRecord(context.WithoutCancel(ctx), recordException, map[string]any{
		"exception_type":  exceptionType,
		"exception_value": exceptionValue,
		"frames":          frames,
	})
}

// exceptionProperties turns the fields of an exception record into the
// exception list an $exception event carries: one unhandled exception
// with its raw stack. Grouping is left to the server.
func exceptionProperties(props map[string]any) {
	props["$exception_list"] = []map[string]any{{
		"type":       props["exception_type"],
		"value":      props["exception_value"],
		"mechanism":  map[string]any{"handled": false},
		"stacktrace": map[string]any{"type": "raw", "frames": props["frames"]},
	}}
	delete(props, "exception_type")
	delete(props, "exception_value")
	delete(props, "frames")
}
