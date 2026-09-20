package scan

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

type cursor struct{}

func (cursor) Slug() string                 { return "cursor" }
func (cursor) Name() string                 { return "Cursor" }
func (cursor) ConfigDir(d home.Dirs) string { return filepath.Join(d.User, ".cursor") }

// SkillsDirs: Cursor also reads the Claude Code and Codex user skills directories.
func (c cursor) SkillsDirs(d home.Dirs) []string {
	return []string{
		filepath.Join(c.ConfigDir(d), "skills"),
		filepath.Join(claudeCode{}.ConfigDir(d), "skills"),
		filepath.Join(codex{}.ConfigDir(d), "skills"),
	}
}
func (cursor) ProjectSkillsDirs() []string {
	return []string{".cursor/skills", ".claude/skills", ".codex/skills", ".agents/skills"}
}
func (c cursor) MCPConfigs(d home.Dirs) []MCPConfig {
	return []MCPConfig{{Path: filepath.Join(c.ConfigDir(d), "mcp.json"), Format: mcp.JSON}}
}

// Plugins are the user-local plugins, one per directory under plugins/local
// (a symlink is followed only when it stays inside that directory), and the
// marketplace plugins the client cached under
// plugins/cache/<marketplace>/<name>/<version> once a .cache-complete marker
// says the copy finished; a version without one is a warning. Cursor keeps
// its install records in the account, so a cached plugin is installed, not
// necessarily enabled. The Claude Code plugins Cursor imports are reported
// under Claude Code only.
func (c cursor) Plugins(d home.Dirs, warn func(string)) []InstalledPlugin {
	root := filepath.Join(c.ConfigDir(d), "plugins")
	var plugins []InstalledPlugin
	local := filepath.Join(root, "local")
	realLocal, _ := filepath.EvalSymlinks(local)
	entries, err := os.ReadDir(local)
	if err != nil && !os.IsNotExist(err) {
		warn(err.Error() + ", skipped")
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(local, e.Name())
		if e.Type()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				warn(path + ": broken symlink, skipped")
				continue
			}
			if !strings.HasPrefix(real, realLocal+string(filepath.Separator)) {
				warn(path + ": symlink resolves outside " + local + ", skipped")
				continue
			}
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		plugins = append(plugins, cursorPlugin(e.Name(), "", "", path, warn))
	}
	cache := filepath.Join(root, "cache")
	for _, marketplace := range subdirs(cache, warn) {
		for _, name := range subdirs(filepath.Join(cache, marketplace), warn) {
			for _, version := range subdirs(filepath.Join(cache, marketplace, name), warn) {
				dir := filepath.Join(cache, marketplace, name, version)
				if firstFile(dir, ".cache-complete") == "" {
					warn(dir + ": incomplete plugin cache, skipped")
					continue
				}
				plugins = append(plugins, cursorPlugin(name, marketplace, version, dir, warn))
			}
		}
	}
	return plugins
}

// cursorPlugin reads the plugin at dir: the first of the .cursor-plugin,
// .claude-plugin and root manifests names and versions it, falling back to
// the directory names. Skills live in the manifest's skills paths, else
// under skills; servers are the manifest's mcpServers object or the file
// it names, else mcp.json, else .mcp.json, with ${CURSOR_PLUGIN_ROOT} and
// ${CLAUDE_PLUGIN_ROOT} standing for dir.
func cursorPlugin(name, marketplace, version, dir string, warn func(string)) InstalledPlugin {
	var m pluginManifest
	manifest := firstFile(dir, ".cursor-plugin/plugin.json", ".claude-plugin/plugin.json", "plugin.json")
	if manifest != "" {
		readJSON(manifest, &m, warn)
	}
	p := InstalledPlugin{
		Name:        m.Name,
		Marketplace: marketplace,
		Version:     m.Version,
		Path:        dir,
		Skills:      manifestSkills(m, manifest, dir, true, warn),
		Servers: MCPConfig{
			Format: mcp.JSON,
			Vars:   map[string]string{"CURSOR_PLUGIN_ROOT": dir, "CLAUDE_PLUGIN_ROOT": dir},
		},
	}
	p.Servers.Path, p.Servers.Data = manifestServers(m, manifest, dir, true, warn, "mcp.json", ".mcp.json")
	if p.Name == "" {
		p.Name = name
	}
	if p.Version == "" {
		p.Version = version
	}
	return p
}
