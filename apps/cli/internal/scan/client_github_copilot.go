package scan

import (
	"path/filepath"
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

type githubCopilot struct{}

func (githubCopilot) Slug() string                 { return "github-copilot" }
func (githubCopilot) Name() string                 { return "GitHub Copilot" }
func (githubCopilot) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".copilot") }
func (c githubCopilot) SkillsDirs(d home.Dirs) []string {
	return []string{filepath.Join(c.ConfigDir(d), "skills")}
}
func (githubCopilot) ProjectSkillsDirs() []string {
	return []string{".github/skills", ".agents/skills"}
}

// MCPConfigs is mcp-config.json; the servers `/mcp disable` turned off are
// listed in settings.json under disabledMcpServers.
func (c githubCopilot) MCPConfigs(d home.Dirs) []MCPConfig {
	var settings struct {
		Disabled []string `json:"disabledMcpServers"`
	}
	readQuiet(filepath.Join(c.ConfigDir(d), "settings.json"), &settings)
	return []MCPConfig{{
		Path:     filepath.Join(c.ConfigDir(d), "mcp-config.json"),
		Format:   mcp.CopilotJSON,
		Disabled: func(name string) bool { return slices.Contains(settings.Disabled, name) },
	}}
}
func (githubCopilot) Plugins(home.Dirs, func(string)) []InstalledPlugin { return nil }
