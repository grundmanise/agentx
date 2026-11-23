package scan

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
	"github.com/grundmanise/agentx/apps/cli/internal/weburl"
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
// record instead of a list in the older layout. A record without an install
// path is a warning. The version falls back to the plugin's manifest. A
// plugin declares servers in .mcp.json at its root, and its hooks in
// hooks/hooks.json and in the hooks its manifest declares, both read; the
// manifest's icon names its icon. Its URL is the manifest's, else its
// marketplace's, see marketplaceURLs. It is on or off as enabledPlugins in
// the user settings, ~/.claude/settings.json, sets it:
// `{"<name>@<marketplace>": true}` or false. A plugin with no boolean there
// is reported without an enabled state: Claude Code then follows a default
// the plugin and its marketplace give, which is not read.
func (c claudeCode) Plugins(d home.Dirs, warn func(string)) []InstalledPlugin {
	path := filepath.Join(c.ConfigDir(d), "plugins", "installed_plugins.json")
	var file struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if !readJSON(path, &file, warn) {
		return nil
	}
	var settings struct {
		Enabled map[string]any `json:"enabledPlugins"`
	}
	readJSON(filepath.Join(c.ConfigDir(d), "settings.json"), &settings, warn)
	var marketplaces map[string]string // read once, for the first plugin with no URL of its own
	var plugins []InstalledPlugin
	for _, key := range slices.Sorted(maps.Keys(file.Plugins)) {
		type record struct {
			InstallPath string `json:"installPath"`
			Version     string `json:"version"`
		}
		var records []record
		if raw := file.Plugins[key]; len(raw) > 0 && raw[0] == '[' {
			_ = json.Unmarshal(raw, &records) // a malformed record yields no plugins, reported below
		} else {
			var one record
			_ = json.Unmarshal(raw, &one)
			records = []record{one}
		}
		name, marketplace, _ := strings.Cut(key, "@")
		for _, r := range records {
			if r.InstallPath == "" {
				warn(path + ": plugin " + key + " has no installPath, skipped")
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
				Skills:      []string{filepath.Join(r.InstallPath, "skills")},
				Servers: MCPConfig{
					Path:   filepath.Join(r.InstallPath, ".mcp.json"),
					Format: mcp.JSON,
					Vars:   map[string]string{"CLAUDE_PLUGIN_ROOT": r.InstallPath},
				},
			}
			if on, ok := settings.Enabled[key].(bool); ok {
				p.Enabled = &on
			}
			var m pluginManifest
			manifest := filepath.Join(r.InstallPath, ".claude-plugin", "plugin.json")
			readJSON(manifest, &m, warn)
			if p.Version == "" {
				p.Version = m.Version
			}
			p.Hooks = manifestHooks(m.Hooks, manifest, r.InstallPath, false, true, warn)
			p.Icon = pluginIcon(m.Icon, manifest, "icon", r.InstallPath, false, warn)
			if p.URL = m.webURL(); p.URL == "" && marketplace != "" {
				if marketplaces == nil {
					marketplaces = marketplaceURLs(filepath.Join(c.ConfigDir(d), "plugins", "known_marketplaces.json"))
				}
				p.URL = marketplaces[marketplace]
			}
			plugins = append(plugins, p)
		}
	}
	return plugins
}

// marketplaceURLs reads the marketplaces Claude Code knows of, in
// known_marketplaces.json, `{"<name>": {"source": {"source": "github",
// "repo": "<owner>/<repo>"}}}` or a "git" source with a "url", into the web
// URL of each one's repository. A marketplace of another kind, a
// directory or a URL of its marketplace.json, has none, and so has one
// whose value is not of that shape. A file that is missing or unreadable
// is quietly none at all: what it would add is only a link.
func marketplaceURLs(path string) map[string]string {
	var file map[string]json.RawMessage
	readQuiet(path, &file)
	urls := map[string]string{}
	for name, raw := range file {
		var entry struct {
			Source struct {
				Source string `json:"source"`
				Repo   string `json:"repo"`
				URL    string `json:"url"`
			} `json:"source"`
		}
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		var u string
		switch src := entry.Source; src.Source {
		case "github":
			u = weburl.GitHub(src.Repo)
		case "git":
			u = weburl.Of(src.URL)
		}
		if u != "" {
			urls[name] = u
		}
	}
	return urls
}
