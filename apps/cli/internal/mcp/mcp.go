// Package mcp reads the MCP servers declared in agent client configuration
// files and handshakes them. Environment and header values never leave the
// package: a Server exports key names only and hands the values to the
// server process or request alone.
package mcp

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
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

	env        map[string]string // the declared environment, for the handshake only
	headers    map[string]string // the declared headers, for the handshake only
	envHeaders map[string]string // header name to the environment variable holding its value (Codex env_http_headers)
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
	// CodexJSON is a Codex plugin's server file: the CodexTOML fields as JSON,
	// under `mcpServers` or as a bare map of name to declaration.
	CodexJSON
)

// Parse reads every server in data. Unknown keys are ignored; an entry that
// is not an object or declares neither a command nor a URL is skipped. The
// result is sorted by name. The error for a malformed file is fixed text:
// a decoder's message can quote the file, and the file can hold secrets.
func Parse(f Format, data []byte) ([]Server, error) {
	var entries map[string]any
	switch f {
	case CodexTOML:
		var doc struct {
			Servers map[string]any `toml:"mcp_servers"`
		}
		if toml.Unmarshal(data, &doc) != nil {
			return nil, errors.New("invalid TOML")
		}
		entries = doc.Servers
	case CodexJSON:
		if json.Unmarshal(data, &entries) != nil {
			return nil, errors.New("invalid JSON")
		}
		if wrapped, ok := entries["mcpServers"].(map[string]any); ok {
			entries = wrapped
		}
	default:
		var doc struct {
			Servers map[string]any `json:"mcpServers"`
		}
		if json.Unmarshal(data, &doc) != nil {
			return nil, errors.New("invalid JSON")
		}
		entries = doc.Servers
	}
	var servers []Server
	for _, name := range slices.Sorted(maps.Keys(entries)) {
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
		env:     values(e["env"]),
	}
	kind := str(e["type"])
	if kind == "" {
		kind = str(e["transport"])
	}
	sse := strings.EqualFold(kind, "sse")
	if f == CodexTOML || f == CodexJSON {
		s.URL = str(e["url"])
		s.HeaderKeys = append(keys(e["http_headers"]), keys(e["env_http_headers"])...)
		sort.Strings(s.HeaderKeys)
		s.headers = values(e["http_headers"])
		s.envHeaders = values(e["env_http_headers"])
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
		s.headers = values(e["headers"])
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

// Expand replaces `${<name>}` for every name in vars in the command, the
// arguments and the environment values, for a declaration that refers to
// where its plugin lives.
func (s *Server) Expand(vars map[string]string) {
	var pairs []string
	for k, v := range vars {
		pairs = append(pairs, "${"+k+"}", v)
	}
	r := strings.NewReplacer(pairs...)
	s.Command = r.Replace(s.Command)
	for i, a := range s.Args {
		s.Args[i] = r.Replace(a)
	}
	for k, v := range s.env {
		s.env[k] = r.Replace(v)
	}
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

// keys lists the keys of an object, sorted; never nil, so JSON shows [].
func keys(v any) []string {
	m, _ := v.(map[string]any)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// values is the string values of an object by key; a value of another type
// is dropped.
func values(v any) map[string]string {
	m, _ := v.(map[string]any)
	out := map[string]string{}
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}
