package scan

import (
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

type windsurf struct{}

func (windsurf) Slug() string                 { return "windsurf" }
func (windsurf) Name() string                 { return "Windsurf" }
func (windsurf) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".codeium", "windsurf") }
func (c windsurf) SkillsDirs(d home.Dirs) []string {
	return []string{filepath.Join(c.ConfigDir(d), "skills")}
}
func (windsurf) ProjectSkillsDirs() []string { return []string{".windsurf/skills"} }
func (c windsurf) MCPConfigs(d home.Dirs) []MCPConfig {
	return []MCPConfig{{Path: filepath.Join(c.ConfigDir(d), "mcp_config.json"), Format: mcp.JSON}}
}
func (windsurf) Plugins(home.Dirs, func(string)) []InstalledPlugin { return nil }
