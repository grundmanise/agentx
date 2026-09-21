// Package scan inventories one machine: the agent configurations on it and
// the skills, MCP servers and plugins each one can see.
package scan

import (
	"os"
	"sort"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

// Client is one agent client the registry knows. A client contributes path
// data and, where it has them, its plugin records; the scanner does the
// reading. Add a client by adding one file with a value of this interface
// and listing it in registry.go.
type Client interface {
	// Slug is the stable lowercase id, such as "claude-code"; it names the
	// configuration in settings and output.
	Slug() string
	// Name is the display name, such as "Claude Code".
	Name() string
	// ConfigDir is the user-scope configuration directory. The client is
	// detected when it exists.
	ConfigDir(d home.Dirs) string
	// SkillsDirs are the user-scope skills directories the client reads, its
	// own first, then other clients' directories and the library where the
	// client reads them. A client that lists the library reads it directly,
	// so a library skill needs no placement in its own directory.
	SkillsDirs(d home.Dirs) []string
	// ProjectSkillsDirs are the project-scope skills directories, relative
	// to a project root.
	ProjectSkillsDirs() []string
	// MCPConfigs are the user-scope files that declare MCP servers; nil for
	// a client whose files agentx does not parse.
	MCPConfigs(d home.Dirs) []MCPConfig
	// Plugins lists the plugins installed in the configuration; nil for a
	// client without plugins. What cannot be read is reported through warn.
	Plugins(d home.Dirs, warn func(string)) []InstalledPlugin
}

// MCPConfig is one source of MCP server declarations: a file, or, with
// Data set, content already read from Path (a manifest that declares its
// servers inline). Vars are expanded in the declarations as `${<name>}`.
// Root is what a relative cwd resolves against, the plugin's directory for
// a plugin's servers; with RunInRoot, a server without cwd runs there too.
// Disabled, when set, names the servers the client has turned off.
type MCPConfig struct {
	Path      string
	Format    mcp.Format
	Data      []byte
	Vars      map[string]string
	Root      string
	RunInRoot bool
	Disabled  func(name string) bool
}

// InstalledPlugin is one plugin bundle found on disk. Its skills are the
// children of its Skills directories; its servers are declared in Servers,
// a file that may not exist, and DisabledServers names those of them the
// client records as turned off.
type InstalledPlugin struct {
	Name            string
	Marketplace     string // where it was installed from; empty when unknown
	Version         string
	Path            string
	Skills          []string
	Servers         MCPConfig
	DisabledServers []string
	Enabled         *bool // nil for a client that records no enabled state
}

// Detect returns the registered clients whose configuration directory
// exists, sorted by slug.
func Detect(d home.Dirs) []Client {
	var detected []Client
	for _, c := range registry {
		if info, err := os.Stat(c.ConfigDir(d)); err == nil && info.IsDir() {
			detected = append(detected, c)
		}
	}
	sort.Slice(detected, func(i, j int) bool { return detected[i].Slug() < detected[j].Slug() })
	return detected
}

// UserSkillsDirs are the user-scope skills directories the registered
// clients read, in registry order and each once. Detection is left out on
// purpose: these are the paths serve watches, and a client whose
// configuration appears while serve runs must have its skills directory
// watched from the sync that first sees it. A directory no detected client
// reads costs a rescan that finds the inventory unchanged.
func UserSkillsDirs(d home.Dirs) []string {
	var dirs []string
	seen := map[string]bool{}
	for _, c := range registry {
		for _, dir := range c.SkillsDirs(d) {
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}
	return dirs
}
