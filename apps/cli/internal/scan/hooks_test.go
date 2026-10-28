package scan

import (
	"reflect"
	"testing"
)

// TestHookName covers the name a hook is reported under, which no client's
// format gives it: the handler's status message, else the program a command
// hook runs, else what the handler is.
func TestHookName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		h    hookHandler
		want string
	}{
		{"status message wins", hookHandler{Type: "command", Command: "./scripts/format.sh", StatusMessage: " Formatting "}, "Formatting"},
		{"program of a bare command", hookHandler{Type: "command", Command: "./scripts/format.sh"}, "format.sh"},
		{"quoted plugin root", hookHandler{Type: "command", Command: `"${CLAUDE_PLUGIN_ROOT}"/scripts/format.sh --write`}, "format.sh"},
		{"interpreter then script", hookHandler{Type: "command", Command: "python3 ${PLUGIN_ROOT}/hooks/session_start.py"}, "session_start.py"},
		{"no path at all", hookHandler{Type: "command", Command: "npx prettier --write"}, "npx"},
		{"windows separators", hookHandler{Command: `node "%CLAUDE_PLUGIN_ROOT%\scripts\check.js"`}, "check.js"},
		{"cursor handler has no type", hookHandler{Command: "./scripts/guard.sh"}, "guard.sh"},
		{"empty command", hookHandler{Command: "   "}, "command"},
		{"path ending in a slash", hookHandler{Command: "./scripts/"}, "command"},
		{"mcp tool by its tool", hookHandler{Type: "mcp_tool", Server: "srv", Tool: "audit"}, "audit"},
		{"mcp tool without a tool", hookHandler{Type: "mcp_tool", Server: "srv"}, "mcp_tool"},
		{"prompt", hookHandler{Type: "prompt", Command: "ignored"}, "prompt"},
		{"http", hookHandler{Type: "http"}, "http"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hookName(tt.h); got != tt.want {
				t.Errorf("hookName(%+v) = %q, want %q", tt.h, got, tt.want)
			}
		})
	}
}

// TestParseHooks covers the three shapes a hook configuration comes in: a
// hooks file with the event map under "hooks", an inline event map, and
// Cursor's flat handlers beside Claude Code's and Codex's matcher groups.
func TestParseHooks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want []Hook
		warn bool
	}{
		{
			name: "wrapped file with matcher groups",
			data: `{"description": "Formatting", "hooks": {"PostToolUse": [{"matcher": "Write|Edit", "hooks": [{"type": "command", "command": "./scripts/format.sh"}, {"type": "command", "command": "./scripts/lint.sh", "statusMessage": "Linting"}]}]}}`,
			want: []Hook{{"PostToolUse", "format.sh"}, {"PostToolUse", "Linting"}},
		},
		{
			name: "inline event map",
			data: `{"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "./check.sh"}]}], "SessionStart": [{"hooks": [{"type": "prompt", "prompt": "Say hello"}]}]}`,
			want: []Hook{{"PreToolUse", "check.sh"}, {"SessionStart", "prompt"}},
		},
		{
			name: "cursor file with flat handlers and a version",
			data: `{"version": 1, "hooks": {"afterFileEdit": [{"command": "./scripts/format-code.sh"}], "beforeShellExecution": [{"command": "./scripts/guard.sh", "matcher": "rm|curl"}]}}`,
			want: []Hook{{"afterFileEdit", "format-code.sh"}, {"beforeShellExecution", "guard.sh"}},
		},
		{
			name: "events sorted, odd values ignored",
			data: `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "./b.sh"}]}], "PreCompact": "not a list", "Notification": [42, {"hooks": "not a list"}]}}`,
			want: []Hook{{"Notification", "command"}, {"Stop", "b.sh"}},
		},
		{name: "invalid JSON", data: `{not json`, warn: true},
		{name: "no hooks", data: `{"hooks": {}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var warnings []string
			got := parseHooks([]byte(tt.data), "hooks.json", func(w string) { warnings = append(warnings, w) })
			if len(got) == 0 && len(tt.want) == 0 {
				got, tt.want = nil, nil
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseHooks = %v, want %v", got, tt.want)
			}
			if (len(warnings) > 0) != tt.warn {
				t.Errorf("warnings = %v, want a warning: %v", warnings, tt.warn)
			}
		})
	}
}
