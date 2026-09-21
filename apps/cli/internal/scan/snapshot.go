package scan

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// Options is what one scan needs besides the filesystem.
type Options struct {
	Dirs       home.Dirs
	MachineID  string
	Label      string
	InstanceID string
	Project    string              // absolute project root; empty scans user scope only
	Disabled   []string            // configuration ids the user disabled
	CopyMode   map[string][]string // skill directory name to the configuration ids holding a copy
}

// Snapshot is the inventory of one machine, the snapshot event without its envelope.
type Snapshot struct {
	InstanceID     string          `json:"instance_id"`
	ScanCounter    int             `json:"scan_counter"`
	Machine        Machine         `json:"machine"`
	Configurations []Configuration `json:"configurations"`
	Skills         []Skill         `json:"skills"`
	MCPServers     []Server        `json:"mcp_servers"`
	Plugins        []Plugin        `json:"plugins"`
	Edges          []Edge          `json:"edges"`
	Warnings       []string        `json:"warnings"`
}

type Machine struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Configuration struct {
	ID           string `json:"id"`
	Client       string `json:"client"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Enabled      bool   `json:"enabled"`
	ReadsLibrary bool   `json:"reads_library"`
	PhysicalID   string `json:"physical_id"`
	LogicalID    string `json:"logical_id"`
}

type Skill struct {
	PhysicalID  string       `json:"physical_id"`
	LogicalID   string       `json:"logical_id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	ContentHash string       `json:"content_hash"`
	Occurrences []Occurrence `json:"occurrences"`
}

type Occurrence struct {
	ID            string `json:"id"`
	Configuration string `json:"configuration"`
	Path          string `json:"path"`
	ResolvedPath  string `json:"resolved_path"`
	Kind          string `json:"kind"` // symlink, copy or directory
	Scope         string `json:"scope"`
	Plugin        string `json:"plugin,omitempty"` // the plugin the placement belongs to
}

// Server is one MCP server in one signature version; the same server
// declared in several configurations is one node with several occurrences.
type Server struct {
	PhysicalID  string             `json:"physical_id"`
	LogicalID   string             `json:"logical_id"`
	Name        string             `json:"name"`
	Signature   string             `json:"signature"`              // the handshake signature, or noSignature
	SignatureAt string             `json:"signature_at,omitempty"` // when the signature was taken, RFC 3339
	Tools       []home.Tool        `json:"tools,omitempty"`        // what the server exposed, sorted by kind then name
	Occurrences []ServerOccurrence `json:"occurrences"`
}

// ServerOccurrence is one declaration of a server in one configuration file.
// Environment and header values are never recorded, only their names.
type ServerOccurrence struct {
	ID            string          `json:"id"`
	Configuration string          `json:"configuration"`
	ConfigFile    string          `json:"config_file"`
	Command       string          `json:"command"`
	Args          []string        `json:"args"`
	EnvKeys       []string        `json:"env_keys"`
	URL           string          `json:"url"`
	HeaderKeys    []string        `json:"header_keys"`
	Transport     string          `json:"transport"`
	Handshake     bool            `json:"handshake"` // whether this scan handshook the server
	HandshakeErr  *HandshakeError `json:"handshake_error,omitempty"`
	Plugin        string          `json:"plugin,omitempty"`
	Enabled       *bool           `json:"enabled,omitempty"` // false only when the client records the server as turned off
}

// HandshakeError is why this scan's handshake of a declaration failed.
type HandshakeError struct {
	Reason  string `json:"reason"` // one of the mcp reasons
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

// Plugin is one plugin bundle installed in one configuration, in one version.
type Plugin struct {
	PhysicalID    string `json:"physical_id"`
	LogicalID     string `json:"logical_id"`
	Name          string `json:"name"`
	Marketplace   string `json:"marketplace"`
	Version       string `json:"version"`
	Configuration string `json:"configuration"`
	Path          string `json:"path"`
	Enabled       *bool  `json:"enabled,omitempty"` // only for a client that records it
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Read performs the local reads of one scan under the caller's shared lock.
// Nothing is written; what cannot be read becomes a warning. Handshake the
// declared servers outside the lock, then compose with Snapshot.
func Read(o Options) *Scan {
	b := &Scan{
		Options: o,
		scanner: newScanner(),
		nodes:   map[string]*Skill{},
		servers: map[string]*Server{},
		plugins: map[string]bool{},
		seen:    map[string]bool{},
		snap: Snapshot{
			InstanceID:     o.InstanceID,
			ScanCounter:    1,
			Machine:        Machine{ID: o.MachineID, Label: o.Label},
			Configurations: []Configuration{},
			Skills:         []Skill{},
			MCPServers:     []Server{},
			Plugins:        []Plugin{},
			Edges:          []Edge{},
		},
	}
	for _, c := range Detect(o.Dirs) {
		dirs := c.SkillsDirs(o.Dirs)
		conf := Configuration{
			ID:           c.Slug(),
			Client:       c.Slug(),
			Name:         c.Name(),
			Path:         c.ConfigDir(o.Dirs),
			Enabled:      !slices.Contains(o.Disabled, c.Slug()),
			ReadsLibrary: slices.Contains(dirs, o.Dirs.Library),
		}
		conf.PhysicalID = id("configuration", o.MachineID, conf.ID, b.portable(conf.Path))
		conf.LogicalID = conf.PhysicalID
		b.snap.Configurations = append(b.snap.Configurations, conf)
		b.snap.Edges = append(b.snap.Edges, Edge{From: o.MachineID, To: conf.PhysicalID})
		for _, dir := range dirs {
			for _, p := range b.skillsIn(dir) {
				b.add(conf, p, "user", "")
			}
		}
		if o.Project != "" {
			for _, rel := range c.ProjectSkillsDirs() {
				for _, p := range b.skillsIn(filepath.Join(o.Project, rel)) {
					b.add(conf, p, "project", "")
				}
			}
		}
		for _, cfg := range c.MCPConfigs(o.Dirs) {
			b.addServers(conf, cfg, "", "", nil, false)
		}
		for _, p := range c.Plugins(o.Dirs, b.warn) {
			b.addPlugin(conf, p)
		}
	}
	var err error
	if b.stored, err = home.LoadHandshakes(o.Dirs.Home); err != nil {
		b.warn(err.Error() + ", ignored")
	}
	return b
}

// Scan is one scan between its local reads and its snapshot.
type Scan struct {
	Options
	*scanner
	nodes    map[string]*Skill  // by content hash
	servers  map[string]*Server // by physical id
	plugins  map[string]bool    // physical ids already recorded
	seen     map[string]bool    // occurrence and edge ids already recorded
	declared []*declaration
	stored   map[string]home.Handshake // from the handshakes file, by logical id
	snap     Snapshot
}

// Snapshot composes the inventory, once per Scan, and returns it with the
// handshakes this scan took, by server logical id, for the handshakes file.
func (s *Scan) Snapshot() (Snapshot, map[string]home.Handshake) {
	fresh := s.composeServers()
	return s.finish(), fresh
}

// add records one placement of a skill inside conf, owned by plugin when
// the placement is inside one, and returns the skill's node.
func (s *Scan) add(conf Configuration, p placement, scope, plugin string) *Skill {
	node, ok := s.nodes[p.info.contentHash]
	if !ok {
		node = &Skill{
			LogicalID:   id("skill", "content", p.info.contentHash),
			Name:        p.info.name,
			Description: p.info.description,
			ContentHash: p.info.contentHash,
		}
		node.PhysicalID = id("skill", node.LogicalID, s.MachineID, p.info.contentHash)
		s.nodes[p.info.contentHash] = node
	}
	occ := Occurrence{
		Configuration: conf.ID,
		Path:          p.path,
		ResolvedPath:  p.resolved,
		Kind:          s.kind(conf, p, scope),
		Scope:         scope,
		Plugin:        plugin,
	}
	occ.ID = id("occurrence", conf.PhysicalID, node.PhysicalID, scope, s.portable(p.path))
	if !s.seen[occ.ID] {
		s.seen[occ.ID] = true
		node.Occurrences = append(node.Occurrences, occ)
	}
	s.edge(conf.PhysicalID, node.PhysicalID)
	return node
}

func (s *Scan) edge(from, to string) {
	if key := from + ">" + to; !s.seen[key] {
		s.seen[key] = true
		s.snap.Edges = append(s.snap.Edges, Edge{From: from, To: to})
	}
}

func (s *Scan) kind(conf Configuration, p placement, scope string) string {
	switch {
	case p.symlink:
		return "symlink"
	case scope == "user" && slices.Contains(s.CopyMode[filepath.Base(p.path)], conf.ID):
		return "copy"
	}
	return "directory"
}

// portable replaces the user's HOME with ~ so an identity does not depend
// on where the home directory is.
func (s *Scan) portable(path string) string {
	if path == s.Dirs.User {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, s.Dirs.User+string(filepath.Separator)); ok {
		return "~/" + filepath.ToSlash(rest)
	}
	return path
}

// finish orders everything so two scans of one machine are byte-identical.
func (s *Scan) finish() Snapshot {
	for _, node := range s.nodes {
		sort.Slice(node.Occurrences, func(i, j int) bool { return node.Occurrences[i].ID < node.Occurrences[j].ID })
		s.snap.Skills = append(s.snap.Skills, *node)
	}
	sort.Slice(s.snap.Skills, func(i, j int) bool { return s.snap.Skills[i].PhysicalID < s.snap.Skills[j].PhysicalID })
	for _, node := range s.servers {
		sort.Slice(node.Occurrences, func(i, j int) bool { return node.Occurrences[i].ID < node.Occurrences[j].ID })
		s.snap.MCPServers = append(s.snap.MCPServers, *node)
	}
	sort.Slice(s.snap.MCPServers, func(i, j int) bool { return s.snap.MCPServers[i].PhysicalID < s.snap.MCPServers[j].PhysicalID })
	sort.Slice(s.snap.Plugins, func(i, j int) bool { return s.snap.Plugins[i].PhysicalID < s.snap.Plugins[j].PhysicalID })
	sort.Slice(s.snap.Edges, func(i, j int) bool {
		if s.snap.Edges[i].From != s.snap.Edges[j].From {
			return s.snap.Edges[i].From < s.snap.Edges[j].From
		}
		return s.snap.Edges[i].To < s.snap.Edges[j].To
	})
	s.snap.Warnings = s.sortedWarnings()
	return s.snap
}
