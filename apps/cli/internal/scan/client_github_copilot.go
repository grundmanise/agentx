package scan

import (
	"path/filepath"

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
func (c githubCopilot) MCPConfigs(d home.Dirs) []MCPConfig {
	return []MCPConfig{{Path: filepath.Join(c.ConfigDir(d), "mcp-config.json"), Format: mcp.JSON}}
}
func (githubCopilot) Plugins(home.Dirs, func(string)) []InstalledPlugin { return nil }
