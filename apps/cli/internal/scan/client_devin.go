package scan

import (
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

// devin is Devin Desktop, which was Windsurf until June 2026, and Devin for
// Terminal: both keep their user configuration in the same directory and
// share one skill discovery, so they are one agent to agentx.
type devin struct{}

func (devin) Slug() string                   { return "devin" }
func (devin) Name() string                   { return "Devin" }
func (devin) ConfigDir(d home.Dirs) string   { return filepath.Join(d.Config, "devin") }
func (c devin) SkillsDir(d home.Dirs) string { return filepath.Join(c.ConfigDir(d), "skills") }
func (devin) ProjectSkillsDir() string       { return ".devin/skills" }

// MCPConfigs is mcp_config.json, and the one Devin still reads from where
// Windsurf kept it.
func (c devin) MCPConfigs(d home.Dirs) []MCPConfig {
	return []MCPConfig{
		{Path: filepath.Join(c.ConfigDir(d), "mcp_config.json"), Format: mcp.DevinJSON},
		{Path: filepath.Join(windsurfDir(d), "mcp_config.json"), Format: mcp.DevinJSON},
	}
}
func (devin) Plugins(home.Dirs, func(string)) []InstalledPlugin { return nil }

// windsurfDir is the user directory Windsurf kept its configuration in.
func windsurfDir(d home.Dirs) string { return filepath.Join(d.User, ".codeium", "windsurf") }
