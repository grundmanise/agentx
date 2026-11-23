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
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// snapshotSchemaVersion is the version of the snapshot agentx snapshot save
// writes. It is the version of that document alone: the settings object
// inside it carries the settings schema version, and an event carries the
// output schema version. Version 1 was the export document of lineage
// records an earlier agentx wrote, and a reader refuses it like any other
// version but this one and readableSnapshotVersions.
const snapshotSchemaVersion = 4

// readableSnapshotVersions are the earlier versions a reader takes as well.
// They differ only in their servers, which an import never reads: version
// 2 lacks a server's logical id and a tool's id and hashes, which version 3
// added, and both hold one signature and its tools on a server where
// version 4 holds its versions, so a reader moves those into one version.
var readableSnapshotVersions = []int{2, 3}

// settingsSchemaVersion is the version of the machine settings file this
// CLI writes, which is what an import may restore.
const settingsSchemaVersion = home.SettingsSchemaVersion

// snapshot is what agentx snapshot save writes and snapshot apply reads: the
// inventory of this machine, saved with the machine settings, without what
// only describes the machine it was taken on. It keeps the agent
// configurations by client id, every skill of the library with where it is
// published to, the version it is at and the configurations it is placed
// in, by configuration id, and the MCP servers and plugins as the scan found
// them. It leaves out the absolute paths, the state and drift, the scan's
// counters and warnings, and every id of the inventory's graph that names
// this machine: the physical ids, the logical ids of configurations and
// skills, the occurrence ids and the edges. It keeps a server's logical id
// and its tools' ids and hashes, the same on every machine the server is, so
// that the tools of one server can be compared across machines. Nothing it
// holds is the content of a skill.
//
// A snapshot is one machine's whole state, so it always holds the settings.
// An export writes every part, with every skill it can name exactly; any
// other writer may write a partial snapshot, see readExport: the machine,
// the configurations, the servers and the plugins may be left out, and so
// may each skill's version, an entry with none following the latest
// version.
//
// The snapshot is a pure function of the machine's state: two exports of
// an unchanged machine are byte-identical, so two snapshots can be diffed.
type snapshot struct {
	SchemaVersion  int                     `json:"schema_version"`
	Machine        *exportMachine          `json:"machine,omitempty"`
	Settings       home.Settings           `json:"settings"`
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
// the hub, with no subpath, for hub skills, absent while no
// hub is set. Upstream and UpstreamSubpath are where a forked
// skill came from. Commit, UpstreamCommit and BaseHash are its version: the
// commit its branch points at and the upstream version it is based on, the
// source's or the upstream's, the last two absent for a skill based on no
// such version, as one skill new made has none. An entry with no commit
// follows the latest version; one with a commit is exact. Placements are
// the configurations the skill is placed in, by id, sorted, the library
// entry of a universal client included.
type snapshotSkill struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`              // managed or unmanaged; an entry that leaves it out is managed
	ForkID          string   `json:"fork_id,omitempty"` // a hub skill's permanent id
	Commit          string   `json:"commit,omitempty"`  // what the branch points at; absent for an entry that follows
	Source          string   `json:"source,omitempty"`
	Subpath         *string  `json:"subpath,omitempty"` // "" for a skill at the source's root
	Upstream        string   `json:"upstream,omitempty"`
	UpstreamSubpath *string  `json:"upstream_subpath,omitempty"`
	UpstreamCommit  string   `json:"upstream_commit,omitempty"`
	BaseHash        string   `json:"base_hash,omitempty"`
	Placements      []string `json:"placements"`
}

// snapshotServer is one MCP server node of the inventory, without its
// physical id and when its versions were taken.
type snapshotServer struct {
	LogicalID   string                     `json:"logical_id,omitempty"` // absent from a version 2 snapshot
	Name        string                     `json:"name"`
	Versions    []snapshotVersion          `json:"versions,omitempty"`
	Occurrences []snapshotServerOccurrence `json:"occurrences"`

	// What a version 2 or 3 snapshot holds in place of versions, which a
	// reader moves into them; never written.
	LegacySignature string         `json:"signature,omitempty"`
	LegacyTools     []snapshotTool `json:"tools,omitempty"`
}

// snapshotVersion is one version of a server: its signature and what it
// exposed.
type snapshotVersion struct {
	Signature string         `json:"signature"`
	Tools     []snapshotTool `json:"tools,omitempty"`
}

// snapshotTool is one tool, prompt, resource or resource template a server
// exposed, as the inventory has it, without its schemas: they can be large,
// and schema_hash is what compares them across machines. A version 2
// snapshot holds its kind, name and description alone.
type snapshotTool struct {
	ID          string `json:"id,omitempty"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	URI         string `json:"uri,omitempty"`
	Description string `json:"description"`
	Hash        string `json:"hash,omitempty"`
	SchemaHash  string `json:"schema_hash,omitempty"`
}

// snapshotServerOccurrence is one declaration of a server, without its id,
// the file it is declared in and what this scan's handshake of it found.
type snapshotServerOccurrence struct {
	Name          string   `json:"name,omitempty"` // absent from a version 2 or 3 snapshot
	Configuration string   `json:"configuration"`
	Command       string   `json:"command"`
	Args          []string `json:"args"`
	EnvKeys       []string `json:"env_keys"`
	URL           string   `json:"url"`
	HeaderKeys    []string `json:"header_keys"`
	Transport     string   `json:"transport"`
	Signature     string   `json:"signature,omitempty"` // absent from a version 2 or 3 snapshot until read
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

// exportEvent reports the snapshot agentx snapshot save wrote. The entries
// themselves are in the file or on the branch; the event says where it is
// and how many skills it holds: a file by its path, or a machine's branch
// on the hub by the machine id and the commit that holds it.
type exportEvent struct {
	event
	Path    string `json:"path,omitempty"`    // the file, absent for a machine's branch
	Machine string `json:"machine,omitempty"` // the machine whose branch holds it, absent for a file
	Commit  string `json:"commit,omitempty"`  // the tip of machines/<machine> now, absent for a file
	Skills  int    `json:"skills"`
}

// defaultSnapshot is the name of the file agentx snapshot save writes when no name
// is given: snapshot.json in the library, the name a machine's branch holds
// it under too.
const defaultSnapshot = "snapshot"

// newSnapshotCommand groups the commands that save a snapshot of this
// machine and apply one to it.
func newSnapshotCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "snapshot",
		Short:       "Save or apply a snapshot: settings, skills and placements",
		Annotations: map[string]string{annotationGroup: "true", annotationSection: "machines"},
		Args:        cobra.NoArgs,
		Example: examples(
			"agentx snapshot save", "write snapshot.json to the library",
			"agentx snapshot save --machine cloud", "seed the cloud machine with this one's skills",
			"agentx snapshot apply --machine cloud", "apply the cloud machine's snapshot",
		),
		RunE: needSubcommand(inv, "no snapshot command given", "run 'agentx snapshot --help' to list commands"),
	}
	cmd.AddCommand(newSnapshotSaveCommand(inv))
	cmd.AddCommand(newSnapshotApplyCommand(inv))
	return cmd
}

func newSnapshotSaveCommand(inv *invocation) *cobra.Command {
	var force bool
	var machine string
	cmd := &cobra.Command{
		Use:         "save [<name> | --machine <id>]",
		Short:       "Save a snapshot to the library or the hub",
		Annotations: map[string]string{annotationSection: "machines", annotationSynopsis: "[<name>]|--machine <id>"},
		Example: examples(
			"agentx snapshot save", "write snapshot.json to the library",
			"agentx snapshot save work-laptop --force", "write work-laptop.json, replacing one that is there",
			"agentx snapshot save --machine cloud", "seed the cloud machine with this one's skills",
			"agentx snapshot save --machine <id>", "a backup on the remote; 'agentx machine' prints the id",
		),
		Args: func(cmd *cobra.Command, args []string) error {
			switch {
			case len(args) > 1:
				return fail(exitUsage, "snapshot save takes at most one argument, the snapshot's name",
					"run 'agentx snapshot save --help' for usage")
			case machine != "" && len(args) == 1:
				return fail(exitUsage, "a name and --machine cannot both be given",
					"give the name, or --machine to save the snapshot to the hub")
			case machine != "":
				return machineArg(machine)
			case len(args) == 1:
				return snapshotName(args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if machine != "" {
				if force {
					return fail(exitUsage, "--force overwrites a file, and --machine writes no file",
						"drop --force: a machine's branch takes a new commit on the one it holds")
				}
				return inv.exportToMachine(cmd.Context(), machine)
			}
			name := defaultSnapshot
			if len(args) == 1 {
				name = args[0]
			}
			return inv.export(cmd.Context(), inv.snapshotPath(name), force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace the file when one of that name is there")
	cmd.Flags().StringVar(&machine, "machine", "", "write to the branch machines/<id> of the hub instead: cloud, or a computer's machine id")
	// The long help names this machine's id and the file the export writes,
	// which are known before the command runs: help is printed before the
	// root's PersistentPreRunE resolves the directories, so it resolves them
	// itself.
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		cmd.Long = inv.exportHelp()
		inv.help(cmd, args)
	})
	return cmd
}

// exportHelp is the long help of export, with the file a plain export
// writes and this machine's id filled in. Each is read only when it can
// be: without a home directory the help names the library in words, and a
// machine whose id is still to be generated is sent to 'agentx machine',
// which generates it.
func (inv *invocation) exportHelp() string {
	file := "  snapshot.json in the library\n"
	id := "'agentx machine' prints it"
	dirs := inv.dirs
	if dirs.Home == "" {
		dirs, _ = home.Resolve(inv.env)
	}
	if dirs.Library != "" {
		file = "  " + filepath.Join(dirs.Library, defaultSnapshot+".json") + "\n"
	}
	if dirs.Home != "" {
		if known, err := home.KnownMachineID(dirs.Home, inv.env); err == nil && known != "" {
			id = known
		}
	}
	return "Save a snapshot of this machine: its settings, agent configurations, skill\n" +
		"list, versions and placements, and its MCP servers and plugins. Use 'agentx\n" +
		"snapshot apply' to restore from the snapshot.\n\n" +
		"The snapshot goes to the library, as snapshot.json or as <name>.json:\n" +
		file + "\n" +
		"With --machine <id>, it is committed to the branch machines/<id> of the hub.\n" +
		"Use this machine's id (" + id + ") to keep a\n" +
		"backup on the remote. Use 'cloud', or another computer's id, to sync this\n" +
		"machine with another.\n\n" +
		"The snapshot keeps each MCP server's command, arguments and URL as written,\n" +
		"which can hold a token. Keep it private."
}

// snapshotName holds the name of a snapshot to a file name the library can
// hold beside its skills: no path, so the file stays in the library, and no
// extension, since export adds .json. A hidden name, which is one with a
// leading dot, has an extension by that rule.
func snapshotName(name string) error {
	switch {
	case name == "" || name == "." || name == "..",
		strings.ContainsAny(name, `/\`),
		filepath.Ext(name) != "":
		return fail(exitUsage, clipped(sanitised(name))+" is not a snapshot name",
			"give a file name without an extension or a slash: snapshot save writes <name>.json to the library")
	}
	return nil
}

// snapshotPath is the file export writes the snapshot named name to:
// <name>.json in the library, beside the skills.
func (inv *invocation) snapshotPath(name string) string {
	return filepath.Join(inv.dirs.Library, name+".json")
}

// importArgs holds import to one place a snapshot is read from: a file,
// the one argument, or a machine's branch, named with --machine, and never
// both.
func importArgs(args []string, machine string) error {
	switch {
	case machine != "" && len(args) > 0:
		return fail(exitUsage, "a file and --machine cannot both be given",
			"give the file, or --machine to apply the snapshot from the hub")
	case machine != "":
		return machineArg(machine)
	case len(args) != 1:
		return fail(exitUsage, "snapshot apply takes one argument, the file to read, or --machine <id>",
			"run 'agentx snapshot apply --help' for usage")
	}
	return nil
}

// export writes the snapshot to path, a file in the library.
func (inv *invocation) export(ctx context.Context, path string, force bool) error {
	snap, err := inv.snapshot(ctx)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fail(exitInternal, err.Error(), "")
	}
	// The library is where the file goes, and a machine that installed
	// nothing yet has none.
	if err := os.MkdirAll(inv.dirs.Library, 0o755); err != nil {
		return fail(exitInternal, err.Error(), "")
	}
	if err := writeExport(path, append(b, '\n'), force); err != nil {
		return err
	}
	inv.out.emit(exportEvent{event: newEvent("export"), Path: path, Skills: len(snap.Skills)})
	out := inv.out
	out.done("saved the snapshot of the settings and " + out.paint(noteStyle, plural(len(snap.Skills), "skill")) +
		" to " + out.paint(heading, path))
	inv.summary = fmt.Sprintf("saved the snapshot of the settings and %s to %s", plural(len(snap.Skills), "skill"), path)
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
// The lineage is read as skill list reads it, so that a home repo git
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
		Settings:       s,
		Configurations: snapshotConfigurations(inventory),
		Skills:         inv.snapshotSkills(sc, inventory),
		MCPServers:     snapshotServers(inventory),
		Plugins:        snapshotPlugins(inventory),
	}, nil
}

// snapshotSkills is one entry per skill of the library, sorted by name, as
// the inventory lists the library, with the commit each one's branch
// points at. What it cannot read of the library is a warning, as skill
// list gives it, so a library it cannot list is said out loud rather than
// written as a snapshot with no skills and a silent success.
func (inv *invocation) snapshotSkills(sc skillContext, inventory scan.Inventory) []snapshotSkill {
	libs, warnings := readLibrary(inv.dirs.Library)
	for _, w := range warnings {
		inv.out.warn(w)
	}
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
			// source, and an entry with no source is a hub skill
			// to the reader, which would look for it under skills/. The
			// snapshot cannot say what it is, so it is left out and said out
			// loud, as a name the library cannot hold is.
			inv.out.warn("left " + sanitised(rec.Ref) + " out of the snapshot: its commit names no source it was imported from")
			continue
		}
		entry := skillFromLibrary(lib, rec, ok, sc.sources, sc.hub, inv.placements(inventory, lib, sc.modes), nil, observation{}).LibraryEntry
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
		srv := snapshotServer{LogicalID: node.LogicalID, Name: node.Name, Versions: []snapshotVersion{}, Occurrences: []snapshotServerOccurrence{}}
		for _, v := range node.Versions {
			version := snapshotVersion{Signature: v.Signature}
			for _, tool := range v.Tools {
				version.Tools = append(version.Tools, snapshotTool{ID: tool.ID, Kind: tool.Kind, Name: tool.Name, URI: tool.URI, Description: tool.Description, Hash: tool.Hash, SchemaHash: tool.SchemaHash})
			}
			srv.Versions = append(srv.Versions, version)
		}
		for _, o := range node.Occurrences {
			srv.Occurrences = append(srv.Occurrences, snapshotServerOccurrence{
				Name: o.Name, Configuration: o.Configuration, Command: o.Command, Args: o.Args, EnvKeys: o.EnvKeys,
				URL: o.URL, HeaderKeys: o.HeaderKeys, Transport: o.Transport, Signature: o.Signature, Plugin: o.Plugin, Enabled: o.Enabled,
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
		return fail(exitRefused, path+" is a directory", "give another name")
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, 0o600)
	switch {
	case errors.Is(err, fs.ErrExist):
		return fail(exitRefused, path+" is already there", "give another name, or pass --force to overwrite it")
	case errors.Is(err, fs.ErrNotExist):
		return fail(exitNotFound, "the directory "+filepath.Dir(path)+" does not exist", "create it")
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
