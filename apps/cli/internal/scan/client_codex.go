package scan

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

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

// Plugins joins the `[plugins."<name>@<marketplace>"]` tables of config.toml,
// each with its enabled state, with the bundles under
// plugins/cache/<marketplace>/<name>/<version>. A table without a bundle is
// a warning; a bundle without a table, which is how a remote install shows
// up, is reported without an enabled state.
func (c codex) Plugins(d home.Dirs, warn func(string)) []InstalledPlugin {
	configPath := filepath.Join(c.ConfigDir(d), "config.toml")
	var config struct {
		Plugins map[string]struct {
			Enabled *bool `toml:"enabled"`
		} `toml:"plugins"`
	}
	if b, err := os.ReadFile(configPath); err != nil {
		if !os.IsNotExist(err) {
			warn(err.Error() + ", skipped")
		}
	} else if toml.Unmarshal(b, &config) != nil {
		warn(configPath + ": invalid TOML, skipped")
	}
	cache := filepath.Join(c.ConfigDir(d), "plugins", "cache")
	bundles := map[string]string{} // <name>@<marketplace> to the active version's directory
	for _, marketplace := range subdirs(cache, warn) {
		for _, name := range subdirs(filepath.Join(cache, marketplace), warn) {
			if v := activeVersion(subdirs(filepath.Join(cache, marketplace, name), warn)); v != "" {
				bundles[name+"@"+marketplace] = filepath.Join(cache, marketplace, name, v)
			}
		}
	}
	enabled := map[string]*bool{}
	for key, table := range config.Plugins {
		name, marketplace, ok := pluginKey(key)
		if !ok {
			warn(configPath + ": plugin key " + strconv.Quote(key) + " is not <name>@<marketplace>, skipped")
			continue
		}
		if _, ok := bundles[key]; !ok {
			warn(filepath.Join(cache, marketplace, name) + ": plugin " + key + " is not installed there, skipped")
			continue
		}
		on := table.Enabled == nil || *table.Enabled
		enabled[key] = &on
	}
	var plugins []InstalledPlugin
	for _, key := range slices.Sorted(maps.Keys(bundles)) {
		name, marketplace, _ := pluginKey(key)
		p := codexPlugin(name, marketplace, bundles[key], warn)
		p.Enabled = enabled[key]
		plugins = append(plugins, p)
	}
	return plugins
}

// pluginKey splits a Codex plugin id on its last @ into the plugin and
// marketplace names, both non-empty.
func pluginKey(key string) (name, marketplace string, ok bool) {
	at := strings.LastIndex(key, "@")
	if at <= 0 || at == len(key)-1 {
		return "", "", false
	}
	return key[:at], key[at+1:], true
}

// codexPlugin reads the bundle at dir. The manifest is plugin.json at the
// root when it is in the agent-plugins format, else the first of the
// .codex-plugin, .claude-plugin and .cursor-plugin manifests. The version
// falls back to the directory name. Skills live in the manifest's skills
// paths, else under skills; servers are the manifest's mcpServers object or
// the file it names, else .mcp.json, else mcp.json.
func codexPlugin(name, marketplace, dir string, warn func(string)) InstalledPlugin {
	p := InstalledPlugin{Name: name, Marketplace: marketplace, Version: filepath.Base(dir), Path: dir}
	var m pluginManifest
	manifest := firstFile(dir, "plugin.json")
	if manifest == "" || !readJSON(manifest, &m, warn) || !strings.HasPrefix(m.Schema, "https://agent-plugins.org/schemas/") {
		m = pluginManifest{}
		if manifest = firstFile(dir, ".codex-plugin/plugin.json", ".claude-plugin/plugin.json", ".cursor-plugin/plugin.json"); manifest != "" {
			readJSON(manifest, &m, warn)
		}
	}
	if m.Version != "" {
		p.Version = m.Version
	}
	for _, rel := range manifestPaths(m.Skills) {
		p.Skills = append(p.Skills, filepath.Join(dir, rel))
	}
	if p.Skills == nil {
		p.Skills = []string{filepath.Join(dir, "skills")}
	}
	p.Servers = MCPConfig{Format: mcp.CodexJSON}
	switch named := manifestPaths(m.MCPServers); {
	case len(m.MCPServers) > 0 && m.MCPServers[0] == '{':
		p.Servers.Path, p.Servers.Data = manifest, m.MCPServers
	case len(named) == 1:
		p.Servers.Path = filepath.Join(dir, named[0])
	default:
		if p.Servers.Path = firstFile(dir, ".mcp.json", "mcp.json"); p.Servers.Path == "" {
			p.Servers.Path = filepath.Join(dir, ".mcp.json")
		}
	}
	return p
}

// manifestPaths reads a manifest path field, one string or a list, keeping
// the paths that start with ./ and stay inside the bundle.
func manifestPaths(raw json.RawMessage) []string {
	var one string
	var list []string
	if json.Unmarshal(raw, &one) == nil {
		list = []string{one}
	} else {
		json.Unmarshal(raw, &list)
	}
	var paths []string
	for _, rel := range list {
		if strings.HasPrefix(rel, "./") && !slices.Contains(strings.Split(rel, "/"), "..") {
			paths = append(paths, rel)
		}
	}
	return paths
}

// versionName is what Codex allows in a version directory name.
var versionName = regexp.MustCompile(`^[A-Za-z0-9.+_-]+$`)

// activeVersion is the version directory Codex loads: local when present,
// else the highest by semver, comparing as strings where either side is not
// one. Names that are not versions are ignored.
func activeVersion(names []string) string {
	best := ""
	for _, n := range names {
		if !versionName.MatchString(n) {
			continue
		}
		if n == "local" {
			return n
		}
		if best == "" || versionLess(best, n) {
			best = n
		}
	}
	return best
}

// versionLess orders two version names: numerically by semver when both
// parse, a pre-release before its release, else by bytes.
func versionLess(a, b string) bool {
	x, okA := semver(a)
	y, okB := semver(b)
	if !okA || !okB {
		return a < b
	}
	for i := range 3 {
		if x.core[i] != y.core[i] {
			return x.core[i] < y.core[i]
		}
	}
	if (x.pre == "") != (y.pre == "") {
		return x.pre != ""
	}
	return x.pre < y.pre
}

type version struct {
	core [3]int
	pre  string
}

// semver parses MAJOR.MINOR.PATCH with an optional -pre-release and +build.
func semver(s string) (version, bool) {
	var v version
	s, _, _ = strings.Cut(s, "+")
	s, v.pre, _ = strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || part != strconv.Itoa(n) {
			return v, false
		}
		v.core[i] = n
	}
	return v, true
}
