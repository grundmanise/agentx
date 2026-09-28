package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// mcpServer is the fixture MCP server built by TestMain; mcpServerErr says
// why it is missing.
var (
	mcpServer    string
	mcpServerErr error
)

// TestMain sets how many tests run at once, turns off the flushes to the
// disk that agentx and git make, and builds the fixture server once per
// test run. Tests that need the server skip when go is not on PATH.
func TestMain(m *testing.M) {
	setParallel()
	home.SkipFlushesInTests()
	gitx.SkipFlushesInTests()
	skipRaceExitSleep()
	os.Exit(func() int {
		if _, err := exec.LookPath("go"); err != nil {
			mcpServerErr = err
			return m.Run()
		}
		dir, err := os.MkdirTemp("", "agentx-mcpserver")
		if err != nil {
			mcpServerErr = err
			return m.Run()
		}
		defer os.RemoveAll(dir)
		mcpServer = filepath.Join(dir, "mcpserver")
		if out, err := exec.Command("go", "build", "-o", mcpServer, "./testdata/mcpserver").CombinedOutput(); err != nil {
			mcpServerErr = fmt.Errorf("build the fixture server: %v\n%s", err, out)
		}
		return m.Run()
	}())
}

// skipRaceExitSleep passes GORACE=atexit_sleep_ms=0 on to every run of the
// test binary that a test starts as a child process. A binary built with
// -race that exits with status 0 while other goroutines still run sleeps
// for a second first, in case one of them races with the exit, and no test
// needs that second. The race runtime reads GORACE as the process starts, so
// setting it here reaches only the children; `make test` sets it for the
// test binaries go test starts. A GORACE that names atexit_sleep_ms itself
// is left as it is.
func skipRaceExitSleep() {
	if gorace := os.Getenv("GORACE"); !strings.Contains(gorace, "atexit_sleep_ms") {
		_ = os.Setenv("GORACE", strings.TrimSpace(gorace+" atexit_sleep_ms=0"))
	}
}
