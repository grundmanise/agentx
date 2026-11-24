package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/crash"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// testPanic makes scan panic, with a message that names HOME and the
// agentx home, when AGENTX_TEST_PANIC is 1: it is how the tests reach a
// crash through the seam.
func (inv *invocation) testPanic() {
	if inv.env["AGENTX_TEST_PANIC"] != "1" {
		return
	}
	panic(fmt.Errorf("test panic: open %s and %s", home.SettingsPath(inv.dirs.Home), filepath.Join(inv.dirs.User, "project")))
}

// reportCrash sends p to PostHog's error tracking when usage data is sent.
// The client's BeforeSend removes every path from it, and the end of the
// run gives it its second to go.
func (inv *invocation) reportCrash(ctx context.Context, p *crash.Panic) {
	if c, id := inv.telemetryClient(ctx); c != nil {
		_ = c.Enqueue(p.Exception(id, time.Now()))
	}
}
