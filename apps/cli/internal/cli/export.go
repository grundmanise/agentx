package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// exportSchemaVersion is the version of the export document. It is the
// version of that document alone: the settings object inside it carries the
// settings schema version, and an event carries the output schema version.
// A reader refuses a document of a version it does not know.
const exportSchemaVersion = 1

// settingsSchemaVersion is the version of the machine settings file this
// CLI writes, which is what an import may restore.
const settingsSchemaVersion = 1

// exportDocument is what agentx export writes and agentx import reads: the
// machine settings as the file holds them and one lineage record per branch
// of the account repo.
//
// It holds no snapshot, no tree and no file of any skill. A skill is
// restored by the account repo, which holds the content, or by installing
// it again from its source; an export carries the coordinates that name
// those versions and nothing they could be reconstructed from. That is what
// makes an export safe to hand to another machine, and it is why an import
// only lists.
//
// The document is a pure function of the machine's state: two exports of an
// unchanged machine are byte-identical, so an export can be diffed or kept
// under version control.
type exportDocument struct {
	SchemaVersion int           `json:"schema_version"`
	Machine       exportMachine `json:"machine"`
	Settings      home.Settings `json:"settings"`
	Skills        []exportSkill `json:"skills"`
}

// exportMachine names the machine the export came from. It is for a reader
// to know whose settings these are; an import restores nothing from it, the
// machine id of a machine being its own.
type exportMachine struct {
	ID    string `json:"id"`
	Label string `json:"label"` // the effective label: the setting, or the hostname until it is set
}

// exportSkill is one lineage record: what the account repo's branch says
// about one skill. Source, Subpath, UpstreamCommit and BaseHash are the
// four lineage trailers of the version the branch records, absent together
// when the branch carries none, as a fork with nothing imported into it yet
// does.
//
// It carries a base hash and no content hash, which is the rule and not an
// exception to it: content_hash is content that is on a machine now, and an
// export is about what the account repo can restore and reads no library
// directory at all.
type exportSkill struct {
	Name           string  `json:"name"`
	Kind           string  `json:"kind"`   // managed or fork
	Commit         string  `json:"commit"` // what the branch points at
	Source         string  `json:"source,omitempty"`
	Subpath        *string `json:"subpath,omitempty"` // "" for a skill at the source's root
	UpstreamCommit string  `json:"upstream_commit,omitempty"`
	BaseHash       string  `json:"base_hash,omitempty"`
	Placed         bool    `json:"placed"` // whether the exporting machine had a placement of it
}

// exportEvent reports the document agentx export wrote. The records
// themselves are in the file; the event says where it is and how many.
type exportEvent struct {
	event
	Path   string `json:"path"`
	Skills int    `json:"skills"`
}

func newExportCommand(inv *invocation) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "export <file>",
		Short: "Write the settings and a lineage record per skill to a file",
		Long: "Write this machine's settings and one lineage record per branch of the account\n" +
			"repo to <file> as one JSON document.\n\n" +
			"The document holds no snapshot and no skill content: a record names where a\n" +
			"skill came from, not what is in it. Restore the settings on another machine\n" +
			"with 'agentx import'; the content comes back from the account repo or from\n" +
			"installing the skill again.\n\n" +
			"The file holds the machine settings, so it is written readable by you alone.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.export(cmd.Context(), args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file when it is already there")
	return cmd
}

// export writes the export document to path.
func (inv *invocation) export(ctx context.Context, path string, force bool) error {
	doc, err := inv.exportDocument(ctx)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fail(exitInternal, err.Error(), "")
	}
	if err := writeExport(path, append(b, '\n'), force); err != nil {
		return err
	}
	inv.out.emit(exportEvent{event: newEvent("export"), Path: path, Skills: len(doc.Skills)})
	out := inv.out
	out.done("exported the settings and " + out.paint(noteStyle, plural(len(doc.Skills), "lineage record")) +
		" to " + out.paint(heading, path))
	out.print("  ", out.paint(muted, "the export holds no skill content: the account repo and 'agentx skill add' restore that"))
	inv.summary = fmt.Sprintf("exported the settings and %s to %s", plural(len(doc.Skills), "lineage record"), path)
	return nil
}

// exportDocument reads what the document holds: the settings file as it
// stands, the machine the export comes from and the lineage records.
//
// The settings are taken as the file holds them and not as a command
// reports them: the effective label a settings event carries is the
// hostname when the setting is empty, and writing that into an export would
// give the machine that imports it the exporting machine's hostname.
func (inv *invocation) exportDocument(ctx context.Context) (exportDocument, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return exportDocument{}, err
	}
	id, _, err := home.MachineID(inv.dirs.Home, inv.env, inv.refs(ctx))
	if err != nil {
		return exportDocument{}, err
	}
	skills, err := inv.exportRecords(ctx)
	if err != nil {
		return exportDocument{}, err
	}
	return exportDocument{
		SchemaVersion: exportSchemaVersion,
		Machine:       exportMachine{ID: id, Label: inv.label(s)},
		Settings:      s,
		Skills:        skills,
	}, nil
}

// exportRecords is one record per branch of the account repo, sorted by
// name. The branches are read exactly as skill list reads them, one
// for-each-ref over both namespaces, trailers and all; where skill list
// walks the library and looks a branch up for each directory, an export
// walks the branches, since what it lists is what the account repo knows
// and not what this machine happens to hold.
func (inv *invocation) exportRecords(ctx context.Context) ([]exportSkill, error) {
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(sc.records))
	for name := range sc.records {
		names = append(names, name)
	}
	sort.Strings(names)
	placed := map[string]bool{}
	if len(names) > 0 { // an account repo with no branch costs no scan
		if placed, err = inv.placedSkills(ctx, sc); err != nil {
			return nil, err
		}
	}
	records := make([]exportSkill, 0, len(names))
	for _, name := range names {
		rec := sc.records[name]
		if nameRefusal(name) != "" {
			// A branch under the two lineage namespaces whose name the
			// library cannot hold as a directory names no skill of this
			// machine. agentx never writes one, but those namespaces are
			// shared with the account remote, so a fetch, another machine
			// or a stray push can put one there. Writing it into the
			// document would write a record this very CLI's reader refuses,
			// and that refusal costs the reader the whole document, the
			// settings restore included, over a branch that names no skill.
			// It is left out and said out loud instead, here on the machine
			// that has the branch and can do something about it.
			inv.out.warn("left " + sanitised(rec.Ref) + " out of the export: " +
				clipped(sanitised(name)) + " is not a name the library and a branch can both hold")
			continue
		}
		e := exportSkill{Name: rec.Name, Kind: rec.Kind, Commit: rec.Commit, Placed: placed[name]}
		if rec.HasImport {
			subpath := rec.Import.Path
			e.Source, e.Subpath = rec.Import.Source, &subpath
			e.UpstreamCommit, e.BaseHash = rec.Import.Commit, rec.Import.Hash
		}
		records = append(records, e)
	}
	return records, nil
}

// placedSkills reports, per library directory, whether this machine has a
// placement of it, the placements being read the way skill list reads them:
// a scan of the machine, and every occurrence that resolves to the library
// directory, the library entry of a client that reads the library included.
// Only the answer, a boolean, reaches the export.
func (inv *invocation) placedSkills(ctx context.Context, sc skillContext) (map[string]bool, error) {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return nil, err
	}
	skills, _ := readLibrary(inv.dirs.Library)
	placed := make(map[string]bool, len(skills))
	for _, lib := range skills {
		placed[lib.Name] = len(inv.placements(snap, lib, sc.modes)) > 0
	}
	return placed, nil
}

// writeExport writes the document to path. A file that is already there is
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
