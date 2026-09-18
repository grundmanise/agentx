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
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Run scans the machine described by o and returns its snapshot. Nothing is
// written; what cannot be read becomes a warning.
func Run(o Options) Snapshot {
	b := &builder{
		Options: o,
		scanner: newScanner(),
		nodes:   map[string]*Skill{},
		seen:    map[string]bool{},
		snap: Snapshot{
			InstanceID:     o.InstanceID,
			ScanCounter:    1,
			Machine:        Machine{ID: o.MachineID, Label: o.Label},
			Configurations: []Configuration{},
			Skills:         []Skill{},
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
				b.add(conf, p, "user")
			}
		}
		if o.Project != "" {
			for _, rel := range c.ProjectSkillsDirs() {
				for _, p := range b.skillsIn(filepath.Join(o.Project, rel)) {
					b.add(conf, p, "project")
				}
			}
		}
	}
	return b.finish()
}

type builder struct {
	Options
	*scanner
	nodes map[string]*Skill // by content hash
	seen  map[string]bool   // occurrence and edge ids already recorded
	snap  Snapshot
}

// add records one placement of a skill inside conf.
func (b *builder) add(conf Configuration, p placement, scope string) {
	node, ok := b.nodes[p.info.contentHash]
	if !ok {
		node = &Skill{
			LogicalID:   id("skill", "content", p.info.contentHash),
			Name:        p.info.name,
			Description: p.info.description,
			ContentHash: p.info.contentHash,
		}
		node.PhysicalID = id("skill", node.LogicalID, b.MachineID, p.info.contentHash)
		b.nodes[p.info.contentHash] = node
	}
	occ := Occurrence{
		Configuration: conf.ID,
		Path:          p.path,
		ResolvedPath:  p.resolved,
		Kind:          b.kind(conf, p, scope),
		Scope:         scope,
	}
	occ.ID = id("occurrence", conf.PhysicalID, node.PhysicalID, scope, b.portable(p.path))
	if !b.seen[occ.ID] {
		b.seen[occ.ID] = true
		node.Occurrences = append(node.Occurrences, occ)
	}
	edge := Edge{From: conf.PhysicalID, To: node.PhysicalID}
	if key := edge.From + ">" + edge.To; !b.seen[key] {
		b.seen[key] = true
		b.snap.Edges = append(b.snap.Edges, edge)
	}
}

func (b *builder) kind(conf Configuration, p placement, scope string) string {
	switch {
	case p.symlink:
		return "symlink"
	case scope == "user" && slices.Contains(b.CopyMode[filepath.Base(p.path)], conf.ID):
		return "copy"
	}
	return "directory"
}

// portable replaces the user's HOME with ~ so an identity does not depend
// on where the home directory is.
func (b *builder) portable(path string) string {
	if path == b.Dirs.User {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, b.Dirs.User+string(filepath.Separator)); ok {
		return "~/" + filepath.ToSlash(rest)
	}
	return path
}

// finish orders everything so two scans of one machine are byte-identical.
func (b *builder) finish() Snapshot {
	for _, node := range b.nodes {
		sort.Slice(node.Occurrences, func(i, j int) bool { return node.Occurrences[i].ID < node.Occurrences[j].ID })
		b.snap.Skills = append(b.snap.Skills, *node)
	}
	sort.Slice(b.snap.Skills, func(i, j int) bool { return b.snap.Skills[i].PhysicalID < b.snap.Skills[j].PhysicalID })
	sort.Slice(b.snap.Edges, func(i, j int) bool {
		if b.snap.Edges[i].From != b.snap.Edges[j].From {
			return b.snap.Edges[i].From < b.snap.Edges[j].From
		}
		return b.snap.Edges[i].To < b.snap.Edges[j].To
	})
	b.snap.Warnings = b.sortedWarnings()
	return b.snap
}
