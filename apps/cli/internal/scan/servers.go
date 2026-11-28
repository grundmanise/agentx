package scan

import (
	"encoding/json"
	"os"
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

// NoSignature marks a declaration that was never handshaken. A handshake
// replaces it with the signature hash and sets Handshake on the occurrence.
const NoSignature = "none"

// declaration is one server declared in one configuration file. Its node is
// built when the inventory is composed, since the physical identity depends
// on the handshake.
type declaration struct {
	conf     Configuration
	file     string
	server   mcp.Server
	logical  string
	plugin   string // the plugin declaring it, "" for a configuration's own server
	owner    string // that plugin's physical id, for the provides edge
	disabled bool   // the client records the server as turned off
	fresh    *mcp.Result
	failed   *HandshakeError // why this scan's handshake failed, if it did
	at       string          // when fresh was taken, RFC 3339
}

// addServers records every server declared in cfg inside conf, owned by
// plugin when the source belongs to one, marking as disabled those named in
// disabled or by cfg, those whose declaration turns them off, and all of
// them when off. A missing file declares nothing; an unreadable or malformed one is a
// warning.
func (s *Scan) addServers(conf Configuration, cfg MCPConfig, plugin, owner string, disabled []string, off bool) {
	data := cfg.Data
	if data == nil {
		var err error
		if data, err = os.ReadFile(cfg.Path); err != nil {
			if !os.IsNotExist(err) {
				s.warn(err.Error() + ", skipped")
			}
			return
		}
	}
	declared, err := mcp.Parse(cfg.Format, data)
	if err != nil {
		s.warn(cfg.Path + ": " + err.Error() + ", skipped")
		return
	}
	for _, server := range declared {
		server.Expand(cfg.Vars)
		server.SetRoot(cfg.Root, cfg.RunInRoot)
		s.declared = append(s.declared, &declaration{
			conf:     conf,
			file:     cfg.Path,
			server:   server,
			logical:  s.serverLogicalID(server),
			plugin:   plugin,
			owner:    owner,
			disabled: off || server.Disabled || slices.Contains(disabled, server.Name) || cfg.Disabled != nil && cfg.Disabled(server.Name),
		})
	}
}

// serverLogicalID is what the server is, wherever it was configured: its
// normalised URL for a remote server, its registry and package for a local
// server that resolves to one, else its command line on this machine, with
// the home directory replaced by ~ like every other hashed path.
func (s *Scan) serverLogicalID(server mcp.Server) string {
	if u, ok := mcp.NormalizeURL(server.URL); ok {
		return id("server", "url", u)
	}
	if registry, pkg, ok := mcp.Package(server.Command, server.Args); ok {
		return id("server", registry, pkg)
	}
	parts := []string{"machine", s.MachineID, s.portable(server.Command), server.URL}
	for _, a := range server.Args {
		parts = append(parts, s.portable(a))
	}
	return id("server", parts...)
}

// composeServers builds the server nodes from the declarations, one per
// logical server: each declaration's version is this scan's handshake, else
// the one stored for it, else none, and its handshake error is this scan's
// failure, else the one stored for it. The handshakes this scan took are
// returned by occurrence id for the handshakes file, so two declarations of
// one server keep their own answers: an answer whole, and a failure beside
// the version the declaration last answered with.
func (s *Scan) composeServers() map[string]home.Handshake {
	fresh := map[string]home.Handshake{}
	for _, d := range s.declared {
		physical := id("server", d.logical, s.MachineID)
		occurrence := id("occurrence", d.conf.PhysicalID, physical, s.portable(d.file), d.server.Name)
		signature, tools, at, failed := NoSignature, []home.Tool(nil), "", d.failed
		stored, _ := s.storedFor(occurrence, d.logical)
		switch {
		case d.fresh != nil:
			signature, tools, at = d.fresh.Signature, s.tools(d.logical, d.fresh.Items), d.at
			fresh[occurrence] = home.Handshake{Signature: signature, Tools: tools, At: at}
		case stored.Signature != "":
			signature, tools, at = stored.Signature, keyed(d.logical, stored.Tools), stored.At
		}
		switch {
		case d.failed != nil:
			stored.Error = &home.HandshakeFailure{Reason: d.failed.Reason, Message: d.failed.Message}
			if stored.Tools == nil {
				stored.Tools = []home.Tool{}
			}
			fresh[occurrence] = stored
		case d.fresh == nil && stored.Error != nil:
			failed = &HandshakeError{Reason: stored.Error.Reason, Message: stored.Error.Message, Hint: mcp.Hint(stored.Error.Reason, d.server), Stored: true}
		}
		node, ok := s.servers[physical]
		if !ok {
			node = &Server{PhysicalID: physical, LogicalID: d.logical, Name: d.server.Name, Versions: []ServerVersion{}}
			s.servers[physical] = node
		}
		if signature != NoSignature {
			node.addVersion(ServerVersion{Signature: signature, SignatureAt: at, Tools: tools})
		}
		occ := ServerOccurrence{
			Name:          d.server.Name,
			Configuration: d.conf.ID,
			ConfigFile:    d.file,
			Command:       d.server.Command,
			Args:          d.server.Args,
			EnvKeys:       d.server.EnvKeys,
			URL:           d.server.URL,
			HeaderKeys:    d.server.HeaderKeys,
			Transport:     d.server.Transport,
			Signature:     signature,
			Handshake:     d.fresh != nil,
			HandshakeErr:  failed,
			Plugin:        d.plugin,
		}
		if d.disabled {
			occ.Enabled = new(bool)
		}
		occ.ID = occurrence
		if !s.seen[occ.ID] {
			s.seen[occ.ID] = true
			node.Occurrences = append(node.Occurrences, occ)
		}
		s.edge(d.conf.PhysicalID, physical)
		if d.owner != "" {
			s.edge(d.owner, physical)
		}
	}
	return fresh
}

// storedFor is the handshake stored for the declaration with this
// occurrence id, else the one an earlier agentx stored for its logical
// server, which kept one entry per server.
func (s *Scan) storedFor(occurrence, logical string) (home.Handshake, bool) {
	if h, ok := s.stored[occurrence]; ok {
		return h, true
	}
	h, ok := s.stored[logical]
	return h, ok
}

// addVersion records v on the server, once per signature, keeping the
// latest time it was taken.
func (node *Server) addVersion(v ServerVersion) {
	for i, have := range node.Versions {
		if have.Signature == v.Signature {
			if v.SignatureAt > have.SignatureAt {
				node.Versions[i].SignatureAt = v.SignatureAt
			}
			return
		}
	}
	node.Versions = append(node.Versions, v)
}

// ToolsOf is what the server exposed to the declaration o: the tools of the
// version o answered with, none when it never answered.
func (node Server) ToolsOf(o ServerOccurrence) []home.Tool {
	for _, v := range node.Versions {
		if v.Signature == o.Signature {
			return v.Tools
		}
	}
	return nil
}

// tools are the nodes of what a server exposed, one per identity.
func (s *Scan) tools(logical string, items []mcp.Item) []home.Tool {
	tools := make([]home.Tool, 0, len(items))
	for _, it := range items {
		tools = append(tools, home.Tool{
			Kind: it.Kind, Name: it.Name, URI: it.URI, Description: it.Description,
			InputSchema: raw(it.InputSchema), OutputSchema: raw(it.OutputSchema),
			Hash: it.Hash, SchemaHash: it.SchemaHash,
		})
	}
	return keyed(logical, tools)
}

// keyed gives every tool its identity and keeps the first of those sharing
// one, in the order the handshake sorted them. The identity is computed
// afresh, so a stored entry an earlier agentx keyed by its hash takes this
// one too, except a resource stored with no URI, whose name may not be
// unique: it keeps its stored id until a handshake gives its URI.
func keyed(logical string, tools []home.Tool) []home.Tool {
	out := []home.Tool{}
	seen := map[string]bool{}
	for _, t := range tools {
		if !legacyResource(t) {
			t.ID = toolID(logical, t)
		}
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	return out
}

// toolID is what a tool is, whatever its definition: its server's logical
// id, its kind and its key, the URI of a resource or a resource template and
// the name of anything else, which the protocol makes unique within a
// server. It is the same on every machine the server's logical id is.
func toolID(logical string, t home.Tool) string {
	key := t.Name
	if t.URI != "" && (t.Kind == mcp.KindResource || t.Kind == mcp.KindResourceTemplate) {
		key = t.URI
	}
	return id("tool", logical, t.Kind, key)
}

// legacyResource is a resource or resource template an earlier agentx
// stored, with an id and no URI.
func legacyResource(t home.Tool) bool {
	return t.ID != "" && t.URI == "" && (t.Kind == mcp.KindResource || t.Kind == mcp.KindResourceTemplate)
}

// raw is canonical JSON as a value to write, nil when absent.
func raw(canonical string) json.RawMessage {
	if canonical == "" {
		return nil
	}
	return json.RawMessage(canonical)
}

// addPlugin records p inside conf with an edge from conf, then the skills
// and servers it provides with an edge from the plugin to each.
func (s *Scan) addPlugin(conf Configuration, p InstalledPlugin) {
	node := Plugin{
		LogicalID:     id("plugin", p.Marketplace, p.Name),
		Name:          p.Name,
		Marketplace:   p.Marketplace,
		Version:       p.Version,
		Configuration: conf.ID,
		Path:          p.Path,
		Enabled:       p.Enabled,
		Hooks:         p.Hooks,
		Icon:          p.Icon,
		URL:           p.URL,
	}
	if node.Hooks == nil {
		node.Hooks = []Hook{}
	}
	node.PhysicalID = id("plugin", node.LogicalID, s.MachineID, conf.PhysicalID, p.Version)
	if !s.plugins[node.PhysicalID] {
		s.plugins[node.PhysicalID] = true
		s.inventory.Plugins = append(s.inventory.Plugins, node)
	}
	s.edge(conf.PhysicalID, node.PhysicalID)
	for _, dir := range p.Skills {
		for _, placement := range s.pluginSkills(dir) {
			skill := s.add(conf, placement, ScopeUser, p.Name)
			s.edge(node.PhysicalID, skill.PhysicalID)
		}
	}
	if p.Servers.Root == "" {
		p.Servers.Root = p.Path
	}
	s.addServers(conf, p.Servers, p.Name, node.PhysicalID, p.DisabledServers, p.Enabled != nil && !*p.Enabled)
}
