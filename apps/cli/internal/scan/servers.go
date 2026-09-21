package scan

import (
	"os"
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

// noSignature marks a server that was never handshaken. A handshake replaces
// it with the signature hash and sets Handshake on the occurrence.
const noSignature = "none"

// declaration is one server declared in one configuration file. Its node is
// built when the snapshot is composed, since the physical identity depends
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

// composeServers builds the server nodes from the declarations: this scan's
// handshake gives the signature, else the stored one, else noSignature. The
// fresh handshakes are returned by logical id for the handshakes file.
func (s *Scan) composeServers() map[string]home.Handshake {
	fresh := map[string]home.Handshake{}
	for _, d := range s.declared {
		signature, tools, at := noSignature, []home.Tool(nil), ""
		if d.fresh != nil {
			signature, tools, at = d.fresh.Signature, s.tools(d.logical, d.fresh.Items), d.at
			fresh[d.logical] = home.Handshake{Signature: signature, Tools: tools, At: at}
		} else if h, ok := s.stored[d.logical]; ok {
			signature, tools, at = h.Signature, h.Tools, h.At
		}
		physical := id("server", d.logical, s.MachineID, signature)
		node, ok := s.servers[physical]
		if !ok {
			node = &Server{PhysicalID: physical, LogicalID: d.logical, Name: d.server.Name, Signature: signature, SignatureAt: at, Tools: tools}
			s.servers[physical] = node
		} else if d.fresh != nil {
			node.SignatureAt = at
		}
		occ := ServerOccurrence{
			Configuration: d.conf.ID,
			ConfigFile:    d.file,
			Command:       d.server.Command,
			Args:          d.server.Args,
			EnvKeys:       d.server.EnvKeys,
			URL:           d.server.URL,
			HeaderKeys:    d.server.HeaderKeys,
			Transport:     d.server.Transport,
			Handshake:     d.fresh != nil,
			HandshakeErr:  d.failed,
			Plugin:        d.plugin,
		}
		if d.disabled {
			occ.Enabled = new(bool)
		}
		occ.ID = id("occurrence", d.conf.PhysicalID, physical, s.portable(d.file), d.server.Name)
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

// tools are the nodes of what a server exposed, one per distinct item.
func (s *Scan) tools(logical string, items []mcp.Item) []home.Tool {
	tools := []home.Tool{}
	seen := map[string]bool{}
	for _, it := range items {
		tid := id("tool", logical, it.Hash)
		if seen[tid] {
			continue
		}
		seen[tid] = true
		tools = append(tools, home.Tool{ID: tid, Kind: it.Kind, Name: it.Name, Description: it.Description})
	}
	return tools
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
	}
	node.PhysicalID = id("plugin", node.LogicalID, s.MachineID, conf.PhysicalID, p.Version)
	if !s.plugins[node.PhysicalID] {
		s.plugins[node.PhysicalID] = true
		s.snap.Plugins = append(s.snap.Plugins, node)
	}
	s.edge(conf.PhysicalID, node.PhysicalID)
	for _, dir := range p.Skills {
		for _, placement := range s.pluginSkills(dir) {
			skill := s.add(conf, placement, "user", p.Name)
			s.edge(node.PhysicalID, skill.PhysicalID)
		}
	}
	if p.Servers.Root == "" {
		p.Servers.Root = p.Path
	}
	s.addServers(conf, p.Servers, p.Name, node.PhysicalID, p.DisabledServers, p.Enabled != nil && !*p.Enabled)
}
