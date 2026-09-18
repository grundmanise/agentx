package scan

import (
	"os"
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

type geminiCLI struct{}

func (geminiCLI) Slug() string                 { return "gemini-cli" }
func (geminiCLI) Name() string                 { return "Gemini CLI" }
func (geminiCLI) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".gemini") }
func (c geminiCLI) SkillsDirs(d home.Dirs) []string {
	return []string{filepath.Join(c.ConfigDir(d), "skills"), d.Library}
}
func (geminiCLI) ProjectSkillsDirs() []string { return []string{".gemini/skills", ".agents/skills"} }

func (c geminiCLI) MCPConfigs(d home.Dirs) []MCPConfig {
	return []MCPConfig{{Path: filepath.Join(c.ConfigDir(d), "settings.json"), Format: mcp.GeminiJSON}}
}

// Plugins are Gemini's extensions: every ~/.gemini/extensions/<dir> holding a
// gemini-extension.json, which names the extension and declares its servers.
// The marketplace is the source recorded by the install metadata next to it,
// or empty for an extension without one.
func (c geminiCLI) Plugins(d home.Dirs, warn func(string)) []InstalledPlugin {
	root := filepath.Join(c.ConfigDir(d), "extensions")
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			warn(err.Error() + ", skipped")
		}
		return nil
	}
	var plugins []InstalledPlugin
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue // Gemini keeps its own files next to the extensions
		}
		manifest := filepath.Join(dir, "gemini-extension.json")
		var ext struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if !readJSON(manifest, &ext, warn) {
			continue
		}
		var install struct {
			Source string `json:"source"`
		}
		readJSON(filepath.Join(dir, ".gemini-extension-install.json"), &install, warn)
		p := InstalledPlugin{
			Name:        ext.Name,
			Marketplace: install.Source,
			Version:     ext.Version,
			Path:        dir,
			Servers:     MCPConfig{Path: manifest, Format: mcp.GeminiJSON},
		}
		if p.Name == "" {
			p.Name = e.Name()
		}
		plugins = append(plugins, p)
	}
	return plugins
}
