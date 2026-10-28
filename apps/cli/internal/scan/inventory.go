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
	Library    bool                // also list the library's own directories, for an inventory that reports them
}

// Inventory is the complete result of one scan of one machine, the
// inventory event without its envelope.
type Inventory struct {
	InstanceID     string          `json:"instance_id"`
	ScanCounter    int             `json:"scan_counter"`
	Machine        Machine         `json:"machine"`
	Configurations []Configuration `json:"configurations"`
	Skills         []Skill         `json:"skills"`
	Library        []LibraryEntry  `json:"library"` // filled in by the caller, which reads the lineage
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

// The kinds of an occurrence: what is at its placement path.
const (
	KindSymlink   = "symlink"
	KindCopy      = "copy"
	KindDirectory = "directory"
)

// The scopes of an occurrence: a client's user-scope skills directories,
// or a project's under scan --project.
const (
	ScopeUser    = "user"
	ScopeProject = "project"
)

type Occurrence struct {
	ID            string `json:"id"`
	Configuration string `json:"configuration"`
	Path          string `json:"path"`
	ResolvedPath  string `json:"resolved_path"`
	Kind          string `json:"kind"` // symlink, copy or directory
	Scope         string `json:"scope"`
	Plugin        string `json:"plugin,omitempty"` // the plugin the placement belongs to
}

// LibraryEntry is one skill directory of the library as agentx reports it:
// what it is called, what agentx knows it as, where it came from, how it
// stands against the version it came from and where it is seen from. It is
// what the library_skill event carries and what an inventory lists under
// library, one shape in both, so that the desktop app reading inventories and
// a script reading skill list are told the same thing about a skill. A skill
// node is something else: one content version found anywhere on the
// machine, which knows nothing of lineage.
type LibraryEntry struct {
	Name            string             `json:"name"`
	Kind            string             `json:"kind"`                       // managed or unmanaged
	ForkID          string             `json:"fork_id,omitempty"`          // one of your own skills' permanent id, from the commit that created it
	Source          string             `json:"source,omitempty"`           // the canonical URL of the source it is published to
	Subpath         *string            `json:"subpath,omitempty"`          // the directory in the source, "" for its root
	Upstream        string             `json:"upstream,omitempty"`         // the canonical URL a forked skill came from
	UpstreamSubpath *string            `json:"upstream_subpath,omitempty"` // the directory in the upstream, "" for its root
	UpstreamCommit  string             `json:"upstream_commit,omitempty"`
	BaseHash        string             `json:"base_hash,omitempty"`     // the content hash of the base version
	ContentHash     string             `json:"content_hash"`            // what the library holds now
	State           string             `json:"state,omitempty"`         // current or modified, for a managed skill whose directory is its checkout
	Drift           []string           `json:"drift,omitempty"`         // the drift states beside state, for a managed skill
	Candidate       *LibraryCandidate  `json:"candidate,omitempty"`     // the update the last check found, from its source or its upstream
	PendingMerge    bool               `json:"pending_merge,omitempty"` // an update left a merge pending, read from <agentx home>/merges/<name>
	Placements      []LibraryPlacement `json:"placements"`
	Universal       []string           `json:"universal"` // every detected universal client, which sees the skill whatever its placements
}

// LibraryCandidate is the update the last update check found for a managed
// skill or a fork: the newer upstream version its candidate ref pins, by
// the upstream commit and the content hash that version's import commit
// records.
type LibraryCandidate struct {
	UpstreamCommit string `json:"upstream_commit"`
	ContentHash    string `json:"content_hash"`
}

// LibraryPlacement is one way a configuration sees a library skill: mode is
// what agentx keeps for it, kind is what is on disk now.
type LibraryPlacement struct {
	Configuration string `json:"configuration"`
	Path          string `json:"path"`
	Mode          string `json:"mode"` // symlink, copy or library
	Kind          string `json:"kind"` // symlink, copy, directory or library
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
	Hooks         []Hook `json:"hooks"`             // the hooks it declares, sorted by event then name
	Icon          string `json:"icon,omitempty"`    // its own icon as a data URL, when its manifest names one inside the plugin
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Read performs the local reads of one scan under the caller's shared lock.
// Nothing is written; what cannot be read becomes a warning. Handshake the
// declared servers outside the lock, then compose with Inventory.
func Read(o Options) *Scan {
	b := &Scan{
		Options: o,
		scanner: newScanner(),
		nodes:   map[string]*Skill{},
		servers: map[string]*Server{},
		plugins: map[string]bool{},
		seen:    map[string]bool{},
		inventory: Inventory{
			InstanceID:     o.InstanceID,
			ScanCounter:    1,
			Machine:        Machine{ID: o.MachineID, Label: o.Label},
			Configurations: []Configuration{},
			Skills:         []Skill{},
			Library:        []LibraryEntry{},
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
			ReadsLibrary: readsLibrary(dirs, o.Dirs.Library),
		}
		conf.PhysicalID = id("configuration", o.MachineID, conf.ID, b.portable(conf.Path))
		conf.LogicalID = conf.PhysicalID
		b.inventory.Configurations = append(b.inventory.Configurations, conf)
		b.inventory.Edges = append(b.inventory.Edges, Edge{From: o.MachineID, To: conf.PhysicalID})
		for _, dir := range dirs {
			for _, p := range b.skillsIn(dir) {
				b.add(conf, p, ScopeUser, "")
			}
		}
		if o.Project != "" {
			for _, rel := range c.ProjectSkillsDirs() {
				for _, p := range b.skillsIn(filepath.Join(o.Project, rel)) {
					b.add(conf, p, ScopeProject, "")
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
	if o.Library {
		b.library = b.librarySkills(o.Dirs.Library)
	}
	var err error
	if b.stored, err = home.LoadHandshakes(o.Dirs.Home); err != nil {
		b.warn(err.Error() + ", ignored")
	}
	return b
}

// Scan is one scan between its local reads and its inventory.
type Scan struct {
	Options
	*scanner
	nodes     map[string]*Skill  // by content hash
	servers   map[string]*Server // by physical id
	plugins   map[string]bool    // physical ids already recorded
	seen      map[string]bool    // occurrence and edge ids already recorded
	declared  []*declaration
	stored    map[string]home.Handshake // from the handshakes file, by logical id
	library   []LibrarySkill            // the library's own directories, read only when Options.Library asks
	inventory Inventory
}

// Library is the skills the library held when Read ran, sorted by name, or
// nothing when Options.Library did not ask for them. They are read by the
// scan's own reads, under the lock Read runs under and through the same
// cache: a library a client reads directly is listed once and each of its
// skills hashed once, however many times the scan comes across it, and what
// could not be read in it is one of the scan's warnings.
func (s *Scan) Library() []LibrarySkill { return s.library }

// Warn adds a warning of the caller's own to the inventory, for something it
// reads beside the scan, under the same lock, and could not. It is sorted in
// with the scan's own warnings when Inventory composes them and, like them,
// never fails the scan.
func (s *Scan) Warn(message string) { s.warn(message) }

// Inventory composes the inventory, once per Scan, and returns it with the
// handshakes this scan took, by server logical id, for the handshakes file.
func (s *Scan) Inventory() (Inventory, map[string]home.Handshake) {
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
		s.inventory.Edges = append(s.inventory.Edges, Edge{From: from, To: to})
	}
}

func (s *Scan) kind(conf Configuration, p placement, scope string) string {
	switch {
	case p.symlink:
		return KindSymlink
	case scope == ScopeUser && slices.Contains(s.CopyMode[filepath.Base(p.path)], conf.ID):
		return KindCopy
	}
	return KindDirectory
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
func (s *Scan) finish() Inventory {
	for _, node := range s.nodes {
		sort.Slice(node.Occurrences, func(i, j int) bool { return node.Occurrences[i].ID < node.Occurrences[j].ID })
		s.inventory.Skills = append(s.inventory.Skills, *node)
	}
	sort.Slice(s.inventory.Skills, func(i, j int) bool { return s.inventory.Skills[i].PhysicalID < s.inventory.Skills[j].PhysicalID })
	for _, node := range s.servers {
		sort.Slice(node.Occurrences, func(i, j int) bool { return node.Occurrences[i].ID < node.Occurrences[j].ID })
		s.inventory.MCPServers = append(s.inventory.MCPServers, *node)
	}
	sort.Slice(s.inventory.MCPServers, func(i, j int) bool {
		return s.inventory.MCPServers[i].PhysicalID < s.inventory.MCPServers[j].PhysicalID
	})
	sort.Slice(s.inventory.Plugins, func(i, j int) bool { return s.inventory.Plugins[i].PhysicalID < s.inventory.Plugins[j].PhysicalID })
	sort.Slice(s.inventory.Edges, func(i, j int) bool {
		if s.inventory.Edges[i].From != s.inventory.Edges[j].From {
			return s.inventory.Edges[i].From < s.inventory.Edges[j].From
		}
		return s.inventory.Edges[i].To < s.inventory.Edges[j].To
	})
	s.inventory.Warnings = s.sortedWarnings()
	return s.inventory
}
