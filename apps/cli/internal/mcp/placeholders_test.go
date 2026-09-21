package mcp

import (
	"slices"
	"testing"
)

func TestExpanders(t *testing.T) {
	t.Parallel()
	env := map[string]string{
		"HOME": "/home/me", "KEY": "k", "EMPTY": "", "ANTHROPIC_API_KEY": "a", "GITHUB_TOKEN": "g",
		"OPENAI_BASE_URL": "https://user:pw@api.example.com", "EXAMPLE_BASE_URL": "https://api.example.com",
		"ANTHROPIC_FUTURE_SECRET": "f", "CLAUDE_ANYTHING": "c",
	}
	for _, c := range []struct {
		name string
		x    expander
		in   string
		f    field
		want string
	}{
		{"claude set", claudeExpand, "Bearer ${KEY}", fieldHeader, "Bearer k"},
		{"claude default", claudeExpand, "${NONE:-d}", fieldLocal, "d"},
		{"claude empty default", claudeExpand, "${NONE:-}", fieldHeader, ""},
		{"claude set but empty", claudeExpand, "${EMPTY:-d}", fieldLocal, ""},
		{"claude unset", claudeExpand, "${NONE}", fieldEnv, "${NONE}"},
		{"claude bare", claudeExpand, "$KEY", fieldLocal, "$KEY"},
		{"claude credential remote", claudeExpand, "${ANTHROPIC_API_KEY}", fieldHeader, ""},
		{"claude credential default ignored", claudeExpand, "${ANTHROPIC_API_KEY:-d}", fieldURL, ""},
		{"claude credential local", claudeExpand, "${ANTHROPIC_API_KEY}", fieldEnv, "a"},
		{"claude token not listed", claudeExpand, "${GITHUB_TOKEN}", fieldHeader, "g"},
		{"claude input prefix, any case", claudeExpand, "${input_anthropic_api_key}", fieldHeader, ""},
		{"claude pattern", claudeExpand, "${OTEL_EXPORTER_OTLP_HEADERS}", fieldHeader, ""},
		{"claude base url with credentials", claudeExpand, "${OPENAI_BASE_URL}", fieldURL, ""},
		{"claude base url without", claudeExpand, "${EXAMPLE_BASE_URL}/v1", fieldURL, "https://api.example.com/v1"},
		{"claude anthropic prefix", claudeExpand, "Bearer ${ANTHROPIC_FUTURE_SECRET}", fieldHeader, "Bearer "},
		{"claude claude prefix, any case", claudeExpand, "?k=${claude_anything}", fieldURL, "?k="},
		{"claude prefix local", claudeExpand, "${CLAUDE_ANYTHING}", fieldLocal, "c"},
		{"cursor env", cursorExpand, "${env:KEY}", fieldHeader, "k"},
		{"cursor unset", cursorExpand, "${env:NONE}", fieldHeader, "${env:NONE}"},
		{"cursor home", cursorExpand, "${userHome}${/}bin", fieldLocal, "/home/me/bin"},
		{"cursor workspace", cursorExpand, "${workspaceFolder}", fieldCwd, "${workspaceFolder}"},
		{"cursor shell syntax", cursorExpand, "${KEY}", fieldLocal, "${KEY}"},
		{"gemini bare", geminiExpand, "$KEY/x", fieldLocal, "k/x"},
		{"gemini unset local", geminiExpand, "$NONE", fieldLocal, "$NONE"},
		{"gemini unset header", geminiExpand, "Bearer ${NONE}", fieldHeader, "Bearer "},
		{"windsurf env", windsurfExpand, "${env:KEY}", fieldURL, "k"},
		{"windsurf unset", windsurfExpand, "${env:NONE}", fieldURL, ""},
		{"windsurf unreadable file", windsurfExpand, "${file:/nonexistent/key}", fieldHeader, "${file:/nonexistent/key}"},
		{"copilot tilde in cwd", copilotExpand, "~/bin", fieldCwd, "/home/me/bin"},
		{"copilot tilde elsewhere", copilotExpand, "~/bin", fieldLocal, "~/bin"},
		{"copilot unset", copilotExpand, "${NONE}", fieldEnv, "${NONE}"},
	} {
		if got := c.x(c.in, env, c.f); got != c.want {
			t.Errorf("%s: %q = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestWithheld(t *testing.T) {
	t.Parallel()
	servers, err := Parse(JSON, []byte(`{"mcpServers": {
  "remote": {"type": "http", "url": "https://x.example/${CLAUDE_TOKEN}?k=${KEY}", "headers": {"A": "${NPM_TOKEN} ${AWS_SESSION_TOKEN:-none}", "B": "$ANTHROPIC_API_KEY ${GITHUB_TOKEN}"}},
  "local": {"command": "run", "env": {"T": "${ANTHROPIC_API_KEY}"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := Parse(CursorJSON, []byte(`{"mcpServers": {"c": {"url": "https://x.example", "headers": {"A": "${env:ANTHROPIC_API_KEY}"}}}}`))
	for _, c := range []struct {
		s    Server
		want []string
	}{
		{servers[1], []string{"AWS_SESSION_TOKEN", "CLAUDE_TOKEN", "NPM_TOKEN"}}, // bare $VAR is not Claude syntax
		{servers[0], nil}, // local values are never withheld
		{cursor[0], nil},  // only Claude Code withholds
	} {
		if got := Withheld(c.s, map[string]string{}); !slices.Equal(got, c.want) {
			t.Errorf("%s withheld %v, want %v", c.s.Name, got, c.want)
		}
	}
}
