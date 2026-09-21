package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// pluginManifest is what a plugin.json manifest gives every client that
// reads one; Codex and Cursor also read its skills and mcpServers.
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
// readQuiet decodes the JSON file at path into v, leaving v as it is when
// the file is missing or cannot be read.
func readQuiet(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

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

// manifestSkills is where the plugin at dir keeps its skills: the paths its
// manifest names, else skills. bare is whether a path may omit the ./.
func manifestSkills(m pluginManifest, manifest, dir string, bare bool, warn func(string)) []string {
	var dirs []string
	for _, rel := range manifestPaths(m.Skills, manifest, "skills", bare, warn) {
		dirs = append(dirs, filepath.Join(dir, rel))
	}
	if dirs == nil {
		dirs = []string{filepath.Join(dir, "skills")}
	}
	return dirs
}

// manifestServers is where the plugin at dir declares its servers: the
// manifest itself when its mcpServers is an object, returned as the data to
// parse in the `{"mcpServers": ...}` shape, else the file the manifest
// names, else the first of the client's two default files that exists,
// else the first default.
func manifestServers(m pluginManifest, manifest, dir string, bare bool, warn func(string), first, second string) (path string, data []byte) {
	if len(m.MCPServers) > 0 && m.MCPServers[0] == '{' {
		return manifest, append(append([]byte(`{"mcpServers":`), m.MCPServers...), '}')
	}
	if named := manifestPaths(m.MCPServers, manifest, "mcpServers", bare, warn); len(named) == 1 {
		return filepath.Join(dir, named[0]), nil
	}
	if path = firstFile(dir, first, second); path == "" {
		path = filepath.Join(dir, first)
	}
	return path, nil
}

// manifestPaths reads a manifest path field, one string or a list, keeping
// the paths that start with ./ and stay inside the bundle; another path is
// a warning naming it. With bare, a relative path without the ./ is kept
// too, as Cursor's own marketplace plugins write them.
func manifestPaths(raw json.RawMessage, manifest, field string, bare bool, warn func(string)) []string {
	var one string
	var list []string
	if json.Unmarshal(raw, &one) == nil {
		list = []string{one}
	} else {
		_ = json.Unmarshal(raw, &list) // neither a string nor a list names no path
	}
	var paths []string
	for _, rel := range list {
		rule := "must start with ./ and stay inside the plugin"
		ok := strings.HasPrefix(rel, "./")
		if bare {
			rule = "must be relative and stay inside the plugin"
			ok = rel != "" && !strings.HasPrefix(rel, "/")
		}
		if !ok || slices.Contains(strings.Split(rel, "/"), "..") {
			warn(manifest + ": " + field + " path " + strconv.Quote(rel) + " " + rule + ", skipped")
			continue
		}
		paths = append(paths, rel)
	}
	return paths
}
