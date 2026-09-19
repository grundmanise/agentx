package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// pluginManifest is what a plugin.json manifest gives every client that
// reads one; Codex also reads its skills and mcpServers.
type pluginManifest struct {
	Schema     string          `json:"$schema"`
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Skills     json.RawMessage `json:"skills"`
	MCPServers json.RawMessage `json:"mcpServers"`
}

// subdirs lists the directories directly under dir by name, skipping hidden
// ones; a missing dir has none, another error is a warning.
func subdirs(dir string, warn func(string)) []string {
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		warn(err.Error() + ", skipped")
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names
}

// firstFile is the first of the names that is a regular file under dir, or "".
func firstFile(dir string, names ...string) string {
	for _, name := range names {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// readJSON decodes path into v and reports whether it could. A missing file
// is silently false; anything else is a warning naming the path, never the
// content.
func readJSON(path string, v any, warn func(string)) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			warn(err.Error() + ", skipped")
		}
		return false
	}
	if json.Unmarshal(b, v) != nil {
		warn(path + ": invalid JSON, skipped")
		return false
	}
	return true
}
