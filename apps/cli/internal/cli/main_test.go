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

// cliChildEnv, set in the environment a harness gives serve, makes this
// test binary run as agentx itself. A serve that runs a command starts its
// own executable, which under test is this binary, with its own
// environment: the child finds the variable there and is the command.
const cliChildEnv = "AGENTX_TEST_IS_THE_CLI"

func processEnv() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}

// TestMain sets how many tests run at once, turns off the flushes to the
// disk that agentx and git make, and builds the fixture server once per
// test run. Tests that need the server skip when go is not on PATH.
func TestMain(m *testing.M) {
	home.SkipFlushesInTests()
	gitx.SkipFlushesInTests()
	if os.Getenv(cliChildEnv) != "" {
		os.Exit(Main(os.Args[1:], processEnv(), os.Stdin, os.Stdout, os.Stderr))
	}
	setParallel()
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
