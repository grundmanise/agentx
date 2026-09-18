package scan

import (
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

type codex struct{}

func (codex) Slug() string                 { return "codex" }
func (codex) Name() string                 { return "Codex" }
func (codex) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".codex") }
func (c codex) SkillsDirs(d home.Dirs) []string {
	return []string{filepath.Join(c.ConfigDir(d), "skills"), d.Library}
}
func (codex) ProjectSkillsDirs() []string { return []string{".codex/skills", ".agents/skills"} }
func (c codex) MCPConfigs(d home.Dirs) []MCPConfig {
	return []MCPConfig{{Path: filepath.Join(c.ConfigDir(d), "config.toml"), Format: mcp.CodexTOML}}
}

// Plugins: Codex has plugins, but no documented on-disk layout to read them from.
func (codex) Plugins(home.Dirs, func(string)) []InstalledPlugin { return nil }
