package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// snapshotSchemaVersion is the version of the snapshot agentx export
// writes. It is the version of that document alone: the settings object
// inside it carries the settings schema version, and an event carries the
// output schema version. Version 1 was the export document of lineage
// records, which an import still reads, see exportDocument; a reader
// refuses any other version.
const snapshotSchemaVersion = 2

// settingsSchemaVersion is the version of the machine settings file this
// CLI writes, which is what an import may restore.
const settingsSchemaVersion = home.SettingsSchemaVersion

// snapshot is what agentx export writes and agentx import reads: the
// inventory of this machine, saved with the machine settings, without what
// only describes the machine it was taken on. It keeps the agent
// configurations by client id, every skill of the library with where it is
// published to, the version it is at and the configurations it is placed
// in, by configuration id, and the MCP servers and plugins as the scan found
// them. It leaves out the absolute paths, the state and drift, the scan's
// counters and warnings, and every id of the inventory's graph: the
// physical and logical ids, the occurrence ids and the edges. Nothing it
// holds is the content of a skill.
//
// An export writes every part, with every skill it can name exactly; any
// other writer may write a partial snapshot, see readExport: the machine,
// the settings, the configurations, the servers and the plugins may be
// left out, and so may each skill's version, an entry with none following
// the latest version.
//
// The snapshot is a pure function of the machine's state: two exports of
// an unchanged machine are byte-identical, so a snapshot can be diffed or
// kept under version control.
type snapshot struct {
	SchemaVersion  int                     `json:"schema_version"`
	Machine        *exportMachine          `json:"machine,omitempty"`
	Settings       *home.Settings          `json:"settings,omitempty"`
	Configurations []snapshotConfiguration `json:"configurations"`
	Skills         []snapshotSkill         `json:"skills"`
	MCPServers     []snapshotServer        `json:"mcp_servers"`
	Plugins        []snapshotPlugin        `json:"plugins"`
}

// exportMachine names the machine the snapshot was taken on. It is for a
// reader to know whose snapshot this is; an import restores nothing from
// it, the machine id of a machine being its own.
type exportMachine struct {
	ID    string `json:"id"`
	Label string `json:"label"` // the effective label: the setting, or the hostname until it is set
}

// snapshotConfiguration is one agent configuration of the inventory,
// without its path and its ids.
type snapshotConfiguration struct {
	ID           string `json:"id"`
	Client       string `json:"client"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	ReadsLibrary bool   `json:"reads_library"`
}

// snapshotSkill is one skill of the library: its library_skill entry in the
// inventory without what describes this machine alone, its content hash,
// its state, drift, update candidate and pending merge, and its placement
// paths, plus the commit its branch points at, which an inventory entry
// does not carry. Source and Subpath are where the skill is published to:
// the source its import commit names for a skill of a shared source, and
// the account remote, with no subpath, for your own skills, absent while no
// account remote is set. Upstream and UpstreamSubpath are where a forked
// skill came from. Commit, UpstreamCommit and BaseHash are its version: the
// commit its branch points at and the upstream version it is based on, the
// source's or the upstream's, the last two absent for a skill based on no
// such version, as one skill new made has none. An entry with no commit
// follows the latest version; one with a commit is exact. Placements are
// the configurations the skill is placed in, by id, sorted, the library
// entry of a universal client included.
//
// placed is what an import_skill event reports beside the placements:
// whether the skill is placed anywhere, which a version 1 export recorded
// in place of them.
type snapshotSkill struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`              // managed or unmanaged; an entry that leaves it out is managed
	ForkID          string   `json:"fork_id,omitempty"` // one of your own skills' permanent id
	Commit          string   `json:"commit,omitempty"`  // what the branch points at; absent for an entry that follows
	Source          string   `json:"source,omitempty"`
	Subpath         *string  `json:"subpath,omitempty"` // "" for a skill at the source's root
	Upstream        string   `json:"upstream,omitempty"`
	UpstreamSubpath *string  `json:"upstream_subpath,omitempty"`
	UpstreamCommit  string   `json:"upstream_commit,omitempty"`
	BaseHash        string   `json:"base_hash,omitempty"`
	Placements      []string `json:"placements"`
	placed          bool
}

// snapshotServer is one MCP server node of the inventory, without its ids
// and when its signature was taken.
type snapshotServer struct {
	Name        string                     `json:"name"`
	Signature   string                     `json:"signature"`
	Tools       []snapshotTool             `json:"tools,omitempty"`
	Occurrences []snapshotServerOccurrence `json:"occurrences"`
}

// snapshotTool is one tool, prompt, resource or resource template a server
// exposed, without its id.
type snapshotTool struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// snapshotServerOccurrence is one declaration of a server, without its id,
// the file it is declared in and what this scan's handshake of it found.
type snapshotServerOccurrence struct {
	Configuration string   `json:"configuration"`
	Command       string   `json:"command"`
	Args          []string `json:"args"`
	EnvKeys       []string `json:"env_keys"`
	URL           string   `json:"url"`
	HeaderKeys    []string `json:"header_keys"`
	Transport     string   `json:"transport"`
	Plugin        string   `json:"plugin,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
}

// snapshotPlugin is one plugin node of the inventory, without its ids and
// its directory.
type snapshotPlugin struct {
	Name          string `json:"name"`
	Marketplace   string `json:"marketplace"`
	Version       string `json:"version"`
	Configuration string `json:"configuration"`
	Enabled       *bool  `json:"enabled,omitempty"`
}

// exportEvent reports the snapshot agentx export wrote. The entries
// themselves are in the file; the event says where it is and how many
// skills it holds.
type exportEvent struct {
	event
	Path   string `json:"path"`
	Skills int    `json:"skills"`
}

func newExportCommand(inv *invocation) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "export <file>",
		Short: "Write a snapshot of this machine, with its settings, to a file",
		Long: "Write a snapshot of this machine to <file>: its settings, its agent\n" +
			"configurations, every skill of the library with its source, its version and the\n" +
			"configurations it is placed in, and the MCP servers and plugins as found.\n\n" +
			"The snapshot holds no skill content. Read it on another machine with\n" +
			"'agentx import'.\n\n" +
			"The file holds the machine settings, so it is written readable by you alone.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.export(cmd.Context(), args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file when it is already there")
	return cmd
}

// export writes the snapshot to path.
func (inv *invocation) export(ctx context.Context, path string, force bool) error {
	snap, err := inv.snapshot(ctx)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fail(exitInternal, err.Error(), "")
	}
	if err := writeExport(path, append(b, '\n'), force); err != nil {
		return err
	}
	inv.out.emit(exportEvent{event: newEvent("export"), Path: path, Skills: len(snap.Skills)})
	out := inv.out
	out.done("exported the settings and " + out.paint(noteStyle, plural(len(snap.Skills), "skill")) +
		" to " + out.paint(heading, path))
	out.print("  ", out.paint(muted, "the snapshot names each skill's source, version and placements, and holds no skill content"))
	inv.summary = fmt.Sprintf("exported the settings and %s to %s", plural(len(snap.Skills), "skill"), path)
	return nil
}

// snapshot reads what the snapshot holds: the settings file as it stands,
// the machine it is taken on, and its inventory without what only
// describes this machine.
//
// The settings are taken as the file holds them and not as a command
// reports them: the effective label a settings event carries is the
// hostname when the setting is empty, and writing that into a snapshot
// would give the machine that imports it the exporting machine's hostname.
//
// The lineage is read as skill list reads it, so that an account repo git
// cannot read is exit code 8 here rather than a snapshot that lists every
// managed skill as unmanaged; the scan that follows is the one skill list
// runs for the placements, which judges no drift, since a snapshot carries
// none.
func (inv *invocation) snapshot(ctx context.Context) (snapshot, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return snapshot{}, err
	}
	id, _, err := home.MachineID(inv.dirs.Home, inv.env, inv.refs(ctx))
	if err != nil {
		return snapshot{}, err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return snapshot{}, err
	}
	inventory, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{
		SchemaVersion:  snapshotSchemaVersion,
		Machine:        &exportMachine{ID: id, Label: inv.label(s)},
		Settings:       &s,
		Configurations: snapshotConfigurations(inventory),
		Skills:         inv.snapshotSkills(sc, inventory),
		MCPServers:     snapshotServers(inventory),
		Plugins:        snapshotPlugins(inventory),
	}, nil
}

// snapshotSkills is one entry per skill of the library, sorted by name, as
// the inventory lists the library, with the commit each one's branch
// points at.
func (inv *invocation) snapshotSkills(sc skillContext, inventory scan.Inventory) []snapshotSkill {
	libs, _ := readLibrary(inv.dirs.Library)
	skills := make([]snapshotSkill, 0, len(libs))
	for _, lib := range libs {
		if nameRefusal(lib.Name) != "" {
			// A directory of the library whose name no branch can carry is
			// no entry this very CLI's reader accepts, and that refusal costs
			// the reader the whole snapshot, the settings restore included,
			// over one directory. It is left out and said out loud instead,
			// here on the machine that has it and can do something about it.
			inv.out.warn("left " + clipped(sanitised(lib.Name)) + " out of the snapshot: it is not a name the library and a branch can both hold")
			continue
		}
		rec, ok := sc.records[lib.Name]
		if ok && rec.Kind == lineage.KindManaged && !rec.HasImport {
			// An import branch whose lineage cannot be read names no
			// source, and an entry with no source is one of your own skills
			// to the reader, which would look for it under skills/. The
			// snapshot cannot say what it is, so it is left out and said out
			// loud, as a name the library cannot hold is.
			inv.out.warn("left " + sanitised(rec.Ref) + " out of the snapshot: its commit names no source it was imported from")
			continue
		}
		entry := skillFromLibrary(lib, rec, ok, sc.sources, sc.account, inv.placements(inventory, lib, sc.modes), nil, observation{}).LibraryEntry
		e := snapshotSkill{
			Name: entry.Name, Kind: entry.Kind, ForkID: entry.ForkID,
			Source: entry.Source, Subpath: entry.Subpath, Upstream: entry.Upstream, UpstreamSubpath: entry.UpstreamSubpath,
			UpstreamCommit: entry.UpstreamCommit, BaseHash: entry.BaseHash,
			Placements: placementIDs(entry.Placements),
		}
		if ok {
			e.Commit = rec.Commit
		}
		skills = append(skills, e)
	}
	return skills
}

// placementIDs is the configurations of places, each once however many
// paths it sees the skill through, see placedIn, sorted.
func placementIDs(places []placementEvent) []string {
	ids := []string{}
	for _, p := range places {
		if !slices.Contains(ids, p.Configuration) {
			ids = append(ids, p.Configuration)
		}
	}
	sort.Strings(ids)
	return ids
}

// snapshotConfigurations are the inventory's configurations, sorted by id
// as it sorts them.
func snapshotConfigurations(inventory scan.Inventory) []snapshotConfiguration {
	confs := make([]snapshotConfiguration, 0, len(inventory.Configurations))
	for _, c := range inventory.Configurations {
		confs = append(confs, snapshotConfiguration{ID: c.ID, Client: c.Client, Name: c.Name, Enabled: c.Enabled, ReadsLibrary: c.ReadsLibrary})
	}
	return confs
}

// snapshotServers are the inventory's MCP servers, sorted by name and their
// declarations by configuration. The inventory sorts both by id, which the
// snapshot leaves out; the order it gives is kept between those that tie,
// so the order stays a function of the machine's state.
func snapshotServers(inventory scan.Inventory) []snapshotServer {
	servers := make([]snapshotServer, 0, len(inventory.MCPServers))
	for _, node := range inventory.MCPServers {
		srv := snapshotServer{Name: node.Name, Signature: node.Signature, Occurrences: []snapshotServerOccurrence{}}
		for _, tool := range node.Tools {
			srv.Tools = append(srv.Tools, snapshotTool{Kind: tool.Kind, Name: tool.Name, Description: tool.Description})
		}
		for _, o := range node.Occurrences {
			srv.Occurrences = append(srv.Occurrences, snapshotServerOccurrence{
				Configuration: o.Configuration, Command: o.Command, Args: o.Args, EnvKeys: o.EnvKeys,
				URL: o.URL, HeaderKeys: o.HeaderKeys, Transport: o.Transport, Plugin: o.Plugin, Enabled: o.Enabled,
			})
		}
		sort.SliceStable(srv.Occurrences, func(i, j int) bool {
			return srv.Occurrences[i].Configuration < srv.Occurrences[j].Configuration
		})
		servers = append(servers, srv)
	}
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return servers
}

// snapshotPlugins are the inventory's plugins, sorted by name, then by
// configuration, the inventory's order kept between those that tie.
func snapshotPlugins(inventory scan.Inventory) []snapshotPlugin {
	plugins := make([]snapshotPlugin, 0, len(inventory.Plugins))
	for _, p := range inventory.Plugins {
		plugins = append(plugins, snapshotPlugin{Name: p.Name, Marketplace: p.Marketplace, Version: p.Version, Configuration: p.Configuration, Enabled: p.Enabled})
	}
	sort.SliceStable(plugins, func(i, j int) bool {
		if plugins[i].Name != plugins[j].Name {
			return plugins[i].Name < plugins[j].Name
		}
		return plugins[i].Configuration < plugins[j].Configuration
	})
	return plugins
}

// writeExport writes the snapshot to path. A file that is already there is
// refused rather than overwritten, since the path is an argument and the
// content it would replace is not agentx's to lose; --force overwrites it.
// The file is created readable by its owner alone: it holds the machine
// settings, which never leave the machine except this way.
func writeExport(path string, b []byte, force bool) error {
	// A directory is answered before the open rather than after it: --force
	// cannot overwrite one, so offering the flag would send the reader after
	// a fix that does not exist, and with the flag the open fails as an
	// internal error over what is really a path that names the wrong thing.
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return fail(exitRefused, path+" is a directory", "give the path of the file to write")
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o600)
	switch {
	case errors.Is(err, fs.ErrExist):
		return fail(exitRefused, path+" is already there", "choose another path, or pass --force to overwrite it")
	case errors.Is(err, fs.ErrNotExist):
		return fail(exitNotFound, "the directory "+filepath.Dir(path)+" does not exist", "create it, or choose another path")
	case err != nil:
		return fail(exitInternal, err.Error(), "")
	}
	_, err = f.Write(b)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fail(exitInternal, err.Error(), "")
	}
	return nil
}
