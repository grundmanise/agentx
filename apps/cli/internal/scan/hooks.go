package scan

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Hook is one hook a plugin declares: the event it runs on and a name for
// it, never its command line. No client's format names a hook, so the
// name is derived, see hookName.
type Hook struct {
	Event string `json:"event"`
	Name  string `json:"name"`
}

// defaultHooksFile is where every client looks for a plugin's hooks when
// its manifest names no other place.
const defaultHooksFile = "hooks/hooks.json"

// hookHandler is what the three clients' hook configurations have in
// common per handler: Claude Code and Codex keep handlers under a matcher
// group as `{"matcher", "hooks": [{"type", "command", "statusMessage"}]}`,
// Cursor keeps them flat as `{"command", "matcher"}`. The command and the
// matcher are read for the name only and never reported.
type hookHandler struct {
	Type          string `json:"type"`
	Command       string `json:"command"`
	StatusMessage string `json:"statusMessage"`
	Server        string `json:"server"`
	Tool          string `json:"tool"`
}

// manifestHooks reads the hooks a plugin at dir declares: raw is the
// manifest's hooks field, a path to a hooks file, an inline configuration
// or a list of either; a path is kept under the rules of manifestPath. The
// default file is read as well when merge is set, as Claude Code does, else
// only when the manifest declares nothing, as Codex and Cursor do. The
// hooks are sorted by event, then name.
func manifestHooks(raw json.RawMessage, manifest, dir string, bare, merge bool, warn func(string)) []Hook {
	hooks := []Hook{}
	var items []json.RawMessage
	if len(raw) > 0 && raw[0] == '[' {
		_ = json.Unmarshal(raw, &items) // a malformed list declares nothing
	} else if len(raw) > 0 {
		items = []json.RawMessage{raw}
	}
	declared := false
	for _, item := range items {
		var rel string
		switch {
		case json.Unmarshal(item, &rel) == nil:
			declared = true
			if manifestPath(rel, manifest, "hooks", bare, warn) {
				hooks = append(hooks, hooksFile(filepath.Join(dir, rel), warn)...)
			}
		case len(item) > 0 && item[0] == '{':
			declared = true
			hooks = append(hooks, parseHooks(item, manifest, warn)...)
		}
	}
	if merge || !declared {
		hooks = append(hooks, hooksFile(filepath.Join(dir, defaultHooksFile), warn)...)
	}
	slices.SortStableFunc(hooks, func(a, b Hook) int {
		if a.Event != b.Event {
			return strings.Compare(a.Event, b.Event)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return hooks
}

// hooksFile reads the hooks file at path. A missing file declares nothing;
// one that cannot be read or parsed is a warning naming it.
func hooksFile(path string, warn func(string)) []Hook {
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			warn(err.Error() + ", skipped")
		}
		return nil
	}
	return parseHooks(b, path, warn)
}

// parseHooks reads one hook configuration, data, from file: the event map
// under a top-level "hooks" key, as a hooks file wraps it, or the event
// map itself, as an inline configuration is written. Under each event, an
// entry is a matcher group holding handlers under "hooks", or one flat
// handler. Anything else there is ignored: a hooks file carries a version
// and a description beside its events.
func parseHooks(data []byte, file string, warn func(string)) []Hook {
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		warn(file + ": invalid JSON, skipped")
		return nil
	}
	events := top
	if raw, ok := top["hooks"]; ok && len(raw) > 0 && raw[0] == '{' {
		events = map[string]json.RawMessage{}
		_ = json.Unmarshal(raw, &events)
	}
	var hooks []Hook
	for _, event := range slices.Sorted(maps.Keys(events)) {
		var entries []json.RawMessage
		if json.Unmarshal(events[event], &entries) != nil {
			continue
		}
		for _, entry := range entries {
			var group struct {
				Hooks []json.RawMessage `json:"hooks"`
			}
			handlers := []json.RawMessage{entry}
			if json.Unmarshal(entry, &group) == nil && group.Hooks != nil {
				handlers = group.Hooks
			}
			for _, raw := range handlers {
				var h hookHandler
				if json.Unmarshal(raw, &h) != nil {
					continue
				}
				hooks = append(hooks, Hook{Event: event, Name: hookName(h)})
			}
		}
	}
	return hooks
}

// hookName is the name a hook is reported under, since no client's format
// gives a hook one: the handler's statusMessage, the one label a hook
// carries for a person, when it has one; else, for a command hook, the
// file name of the program the command runs, taken from the first word of
// the command that is a path, or its first word when none is, with any
// quotes dropped; else, for an mcp_tool hook, its tool; else the handler's
// type. A handler without a type is a command hook, as Cursor writes them.
func hookName(h hookHandler) string {
	if msg := strings.TrimSpace(h.StatusMessage); msg != "" {
		return msg
	}
	typ := h.Type
	if typ == "" {
		typ = "command"
	}
	switch typ {
	case "command":
		words := strings.Fields(h.Command)
		if len(words) == 0 {
			return typ
		}
		word := words[0]
		for _, w := range words {
			if strings.ContainsAny(w, `/\`) {
				word = w
				break
			}
		}
		word = strings.NewReplacer(`"`, "", `'`, "").Replace(word)
		if i := strings.LastIndexAny(word, `/\`); i >= 0 {
			word = word[i+1:]
		}
		if word == "" {
			return typ
		}
		return word
	case "mcp_tool":
		if h.Tool != "" {
			return h.Tool
		}
	}
	return typ
}
