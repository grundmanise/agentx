package mcp

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// field is where a declared value goes, which some clients expand
// differently.
type field int

const (
	fieldLocal  field = iota // command, arguments
	fieldCwd                 // working directory
	fieldEnv                 // environment value
	fieldURL                 // URL of a remote server
	fieldHeader              // header value
)

// expander replaces the placeholders of one declared value with values from
// the environment, as the client that wrote the declaration does when it
// starts or connects to the server.
type expander func(value string, env map[string]string, f field) string

// expanders are the placeholder syntaxes by format; Codex expands nothing.
var expanders = map[Format]expander{
	JSON:         claudeExpand,
	CursorJSON:   cursorExpand,
	GeminiJSON:   geminiExpand,
	WindsurfJSON: windsurfExpand,
	CopilotJSON:  copilotExpand,
}

// expanded is s with its command, arguments, environment, working
// directory, URL and headers expanded from env, and its working directory
// located, see SetRoot. The result goes to the handshake alone: a snapshot
// records the declaration as written.
func (s Server) expanded(env map[string]string) Server {
	if x := expanders[s.format]; x != nil {
		s.Command = x(s.Command, env, fieldLocal)
		s.Args = slices.Clone(s.Args)
		for i, a := range s.Args {
			s.Args[i] = x(a, env, fieldLocal)
		}
		s.env = maps.Clone(s.env)
		for k, v := range s.env {
			s.env[k] = x(v, env, fieldEnv)
		}
		s.cwd = x(s.cwd, env, fieldCwd)
		s.URL = x(s.URL, env, fieldURL)
		s.headers = maps.Clone(s.headers)
		for k, v := range s.headers {
			s.headers[k] = x(v, env, fieldHeader)
		}
	}
	switch {
	case s.cwd == "" && s.inRoot:
		s.cwd = s.root
	case s.cwd != "" && s.root != "" && !filepath.IsAbs(s.cwd):
		s.cwd = filepath.Join(s.root, s.cwd)
	}
	return s
}

// shellVar is `${VAR}`, `${VAR:-default}` and, where a client takes it,
// `$VAR`.
var shellVar = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-[^}]*)?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// shellExpand gives a set variable its value, even an empty one, and an
// unset one its default; one without a default stays as written, or is
// empty with unsetEmpty. With bare, `$VAR` counts too.
func shellExpand(value string, env map[string]string, bare, unsetEmpty bool, blank func(name string) bool) string {
	return shellVar.ReplaceAllStringFunc(value, func(m string) string {
		sub := shellVar.FindStringSubmatch(m)
		name := sub[1] + sub[3]
		switch {
		case sub[3] != "" && !bare:
			return m
		case blank != nil && blank(name):
			return ""
		}
		if v, ok := env[name]; ok {
			return v
		}
		if sub[2] != "" {
			return sub[2][len(":-"):]
		}
		if unsetEmpty {
			return ""
		}
		return m
	})
}

// claudeExpand is Claude Code's `${VAR}` and `${VAR:-default}`. In a remote
// server's URL and headers, its credential variables are empty, set or not.
func claudeExpand(value string, env map[string]string, f field) string {
	var blank func(string) bool
	if f == fieldURL || f == fieldHeader {
		blank = func(name string) bool { return claudeCredential(name, env[name]) }
	}
	return shellExpand(value, env, false, false, blank)
}

// geminiExpand is Gemini CLI's `$VAR`, `${VAR}` and `${VAR:-default}`,
// resolved in every value when the settings load, an unset variable staying
// as written; an environment or header value is expanded once more when the
// server starts, where what is still unset becomes empty.
func geminiExpand(value string, env map[string]string, f field) string {
	return shellExpand(value, env, true, f == fieldEnv || f == fieldHeader, nil)
}

// copilotExpand is Copilot CLI's `$VAR`, `${VAR}` and `${VAR:-default}`,
// an unset variable staying as written, and `~` at the start of cwd.
func copilotExpand(value string, env map[string]string, f field) string {
	value = shellExpand(value, env, true, false, nil)
	if f == fieldCwd && (value == "~" || strings.HasPrefix(value, "~/")) {
		value = env["HOME"] + value[1:]
	}
	return value
}

// Withheld names the variables the handshake of s leaves empty though its
// declaration refers to them: the credentials a Claude Code declaration
// names in a remote server's URL or headers.
func Withheld(s Server, env map[string]string) []string {
	if s.format != JSON {
		return nil
	}
	var names []string
	for _, v := range append([]string{s.URL}, slices.Collect(maps.Values(s.headers))...) {
		for _, sub := range shellVar.FindAllStringSubmatch(v, -1) {
			if name := sub[1]; name != "" && claudeCredential(name, env[name]) && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return names
}

// cursorVar is one of Cursor's variables.
var cursorVar = regexp.MustCompile(`\$\{(env:[A-Za-z_][A-Za-z0-9_]*|userHome|pathSeparator|/|workspaceFolder|workspaceFolderBasename)\}`)

// cursorExpand is Cursor's `${env:NAME}`, `${userHome}`, `${pathSeparator}`
// and `${/}`. An unset variable stays as written, and so do the workspace
// variables, since a user-scope declaration has no workspace.
func cursorExpand(value string, env map[string]string, _ field) string {
	return cursorVar.ReplaceAllStringFunc(value, func(m string) string {
		switch name := m[2 : len(m)-1]; name {
		case "userHome":
			return env["HOME"]
		case "pathSeparator", "/":
			return string(filepath.Separator)
		case "workspaceFolder", "workspaceFolderBasename":
			return m
		default:
			if v, ok := env[name[len("env:"):]]; ok {
				return v
			}
			return m
		}
	})
}

// windsurfVar is Windsurf's `${env:NAME}` and `${file:/path}`.
var windsurfVar = regexp.MustCompile(`\$\{(env|file):([^}]+)\}`)

// windsurfExpand is Windsurf's expansion: an unset variable is empty, and a
// file gives its contents trimmed, or stays as written when it cannot be
// read.
func windsurfExpand(value string, env map[string]string, _ field) string {
	return windsurfVar.ReplaceAllStringFunc(value, func(m string) string {
		sub := windsurfVar.FindStringSubmatch(m)
		if sub[1] == "env" {
			return env[sub[2]]
		}
		b, err := os.ReadFile(sub[2])
		if err != nil {
			return m
		}
		return strings.TrimSpace(string(b))
	})
}
