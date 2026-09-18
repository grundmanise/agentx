package scan

import (
	"os"
	"path/filepath"

	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

// noSignature marks a server that was never handshaken. A handshake replaces
// it with the signature hash and sets Handshake on the occurrence.
const noSignature = "none"

// addServers records every server declared in cfg inside conf, owned by
// plugin when the file belongs to one, and returns their nodes. A missing
// file declares nothing; an unreadable or malformed one is a warning.
func (b *builder) addServers(conf Configuration, cfg MCPConfig, plugin string) []*Server {
	data, err := os.ReadFile(cfg.Path)
	if err != nil {
		if !os.IsNotExist(err) {
			b.warn(err.Error() + ", skipped")
		}
		return nil
	}
	declared, err := mcp.Parse(cfg.Format, data)
	if err != nil {
		b.warn(cfg.Path + ": " + err.Error() + ", skipped")
		return nil
	}
	var nodes []*Server
	for _, s := range declared {
		nodes = append(nodes, b.addServer(conf, cfg.Path, s, plugin))
	}
	return nodes
}

func (b *builder) addServer(conf Configuration, file string, s mcp.Server, plugin string) *Server {
	logical := b.serverLogicalID(s)
	physical := id("server", logical, b.MachineID, noSignature)
	node, ok := b.servers[physical]
	if !ok {
		node = &Server{PhysicalID: physical, LogicalID: logical, Name: s.Name, Signature: noSignature}
		b.servers[physical] = node
	}
	occ := ServerOccurrence{
		Configuration: conf.ID,
		ConfigFile:    file,
		Command:       s.Command,
		Args:          s.Args,
		EnvKeys:       s.EnvKeys,
		URL:           s.URL,
		HeaderKeys:    s.HeaderKeys,
		Transport:     s.Transport,
		Plugin:        plugin,
	}
	occ.ID = id("occurrence", conf.PhysicalID, physical, b.portable(file), s.Name)
	if !b.seen[occ.ID] {
		b.seen[occ.ID] = true
		node.Occurrences = append(node.Occurrences, occ)
	}
	b.edge(conf.PhysicalID, physical)
	return node
}

// serverLogicalID is what the server is, wherever it was configured: its
// normalised URL for a remote server, its registry and package for a local
// server that resolves to one, else its command line on this machine.
func (b *builder) serverLogicalID(s mcp.Server) string {
	if u, ok := mcp.NormalizeURL(s.URL); ok {
		return id("server", "url", u)
	}
	if registry, pkg, ok := mcp.Package(s.Command, s.Args); ok {
		return id("server", registry, pkg)
	}
	return id("server", append([]string{"machine", b.MachineID, s.Command, s.URL}, s.Args...)...)
}

// addPlugin records p inside conf with an edge from conf, then the skills
// and servers it provides with an edge from the plugin to each.
func (b *builder) addPlugin(conf Configuration, p InstalledPlugin) {
	node := Plugin{
		LogicalID:     id("plugin", p.Marketplace, p.Name),
		Name:          p.Name,
		Marketplace:   p.Marketplace,
		Version:       p.Version,
		Configuration: conf.ID,
		Path:          p.Path,
	}
	node.PhysicalID = id("plugin", node.LogicalID, b.MachineID, conf.PhysicalID, p.Version)
	if !b.plugins[node.PhysicalID] {
		b.plugins[node.PhysicalID] = true
		b.snap.Plugins = append(b.snap.Plugins, node)
	}
	b.edge(conf.PhysicalID, node.PhysicalID)
	for _, placement := range b.skillsIn(filepath.Join(p.Path, "skills")) {
		skill := b.add(conf, placement, "user", p.Name)
		b.edge(node.PhysicalID, skill.PhysicalID)
	}
	for _, server := range b.addServers(conf, p.Servers, p.Name) {
		b.edge(node.PhysicalID, server.PhysicalID)
	}
}
