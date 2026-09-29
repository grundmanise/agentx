package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
