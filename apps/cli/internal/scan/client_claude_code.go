package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

type claudeCode struct{}

func (claudeCode) Slug() string                 { return "claude-code" }
func (claudeCode) Name() string                 { return "Claude Code" }
func (claudeCode) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".claude") }
func (c claudeCode) SkillsDirs(d home.Dirs) []string {
	return []string{filepath.Join(c.ConfigDir(d), "skills")}
}
func (claudeCode) ProjectSkillsDirs() []string { return []string{".claude/skills"} }

// MCPConfigs: user-scope servers live in ~/.claude.json, not in settings.json.
func (claudeCode) MCPConfigs(d home.Dirs) []MCPConfig {
	return []MCPConfig{{Path: filepath.Join(d.User, ".claude.json"), Format: mcp.JSON}}
}

// Plugins reads the install records in ~/.claude/plugins/installed_plugins.json:
// `{"plugins": {"<name>@<marketplace>": [{"installPath", "version"}]}}`, or one
// record instead of a list in the older layout. The version falls back to the
// plugin's manifest. A plugin declares servers in .mcp.json at its root.
func (c claudeCode) Plugins(d home.Dirs, warn func(string)) []InstalledPlugin {
	path := filepath.Join(c.ConfigDir(d), "plugins", "installed_plugins.json")
	var file struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if !readJSON(path, &file, warn) {
		return nil
	}
	var plugins []InstalledPlugin
	for _, key := range sortedKeys(file.Plugins) {
		type record struct {
			InstallPath string `json:"installPath"`
			Version     string `json:"version"`
		}
		var records []record
		if raw := file.Plugins[key]; len(raw) > 0 && raw[0] == '[' {
			json.Unmarshal(raw, &records)
		} else {
			var one record
			json.Unmarshal(raw, &one)
			records = []record{one}
		}
		name, marketplace, _ := strings.Cut(key, "@")
		for _, r := range records {
			if r.InstallPath == "" {
				continue
			}
			if info, err := os.Stat(r.InstallPath); err != nil || !info.IsDir() {
				warn(r.InstallPath + ": plugin " + key + " is not installed there, skipped")
				continue
			}
			p := InstalledPlugin{
				Name:        name,
				Marketplace: marketplace,
				Version:     r.Version,
				Path:        r.InstallPath,
				Servers:     MCPConfig{Path: filepath.Join(r.InstallPath, ".mcp.json"), Format: mcp.JSON},
			}
			if p.Version == "" {
				var manifest struct {
					Version string `json:"version"`
				}
				readJSON(filepath.Join(r.InstallPath, ".claude-plugin", "plugin.json"), &manifest, warn)
				p.Version = manifest.Version
			}
			plugins = append(plugins, p)
		}
	}
	return plugins
}

// readJSON decodes path into v and reports whether it could. A missing file
// is silently false; anything else is a warning.
func readJSON(path string, v any, warn func(string)) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			warn(err.Error() + ", skipped")
		}
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		warn(path + ": " + err.Error() + ", skipped")
		return false
	}
	return true
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
