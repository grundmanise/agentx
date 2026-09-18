// Package mcp reads the MCP servers declared in agent client configuration
// files. Environment and header values never leave the parser: a Server
// carries key names only.
package mcp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Server is one declaration in a configuration file.
type Server struct {
	Name       string
	Command    string
	Args       []string
	EnvKeys    []string // sorted environment variable names
	URL        string
	HeaderKeys []string // sorted header names
	Transport  string   // stdio, sse or streamable-http
}

// Format is the shape of a configuration file.
type Format int

const (
	// JSON is `{"mcpServers": {<name>: {...}}}`: command, args and env for a
	// local server; url or serverUrl and headers for a remote one; type or
	// transport "sse" marks an SSE endpoint, any other URL is streamable HTTP.
	JSON Format = iota
	// GeminiJSON is JSON where url is an SSE endpoint and httpUrl a
	// streamable HTTP one.
	GeminiJSON
	// CodexTOML is `[mcp_servers.<name>]` tables: command, args and env for a
	// local server; url, http_headers and env_http_headers for a remote one.
	CodexTOML
)

// Parse reads every server in data. Unknown keys are ignored; an entry that
// is not an object or declares neither a command nor a URL is skipped. The
// result is sorted by name.
func Parse(f Format, data []byte) ([]Server, error) {
	var entries map[string]any
	if f == CodexTOML {
		var doc struct {
			Servers map[string]any `toml:"mcp_servers"`
		}
		if err := toml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		entries = doc.Servers
	} else {
		var doc struct {
			Servers map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		entries = doc.Servers
	}
	var servers []Server
	for _, name := range sortedKeys(entries) {
		entry, ok := entries[name].(map[string]any)
		if !ok {
			continue
		}
		if s := server(f, name, entry); s.Command != "" || s.URL != "" {
			servers = append(servers, s)
		}
	}
	return servers, nil
}

func server(f Format, name string, e map[string]any) Server {
	s := Server{
		Name:    name,
		Command: str(e["command"]),
		Args:    strs(e["args"]),
		EnvKeys: keys(e["env"]),
	}
	kind := str(e["type"])
	if kind == "" {
		kind = str(e["transport"])
	}
	sse := strings.EqualFold(kind, "sse")
	if f == CodexTOML {
		s.URL = str(e["url"])
		s.HeaderKeys = append(keys(e["http_headers"]), keys(e["env_http_headers"])...)
		sort.Strings(s.HeaderKeys)
	} else {
		s.URL = str(e["httpUrl"])
		if s.URL == "" {
			s.URL = str(e["url"])
			sse = sse || f == GeminiJSON && s.URL != "" && kind == ""
		}
		if s.URL == "" {
			s.URL = str(e["serverUrl"])
		}
		s.HeaderKeys = keys(e["headers"])
	}
	switch {
	case s.Command != "":
		s.Transport = "stdio"
	case sse:
		s.Transport = "sse"
	default:
		s.Transport = "streamable-http"
	}
	return s
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func strs(v any) []string {
	out := []string{}
	if list, ok := v.([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// keys lists the keys of an object; its values are never read.
func keys(v any) []string {
	m, _ := v.(map[string]any)
	return sortedKeys(m)
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
