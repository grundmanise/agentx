package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The three states one entry of a snapshot is listed with: the account
// repo of this machine holds that branch at that commit, holds no branch of
// that name, or holds one that is not this entry's.
const (
	restorePresent   = "present"
	restoreMissing   = "missing"
	restoreDifferent = "different"
)

// exportDocument is the version 1 export document, which agentx export
// wrote before it wrote a snapshot: the machine settings and one lineage
// record per branch of the account repo. An import still reads one, see
// readExport, each record an exact entry of a snapshot.
type exportDocument struct {
	SchemaVersion int           `json:"schema_version"`
	Machine       exportMachine `json:"machine"`
	Settings      home.Settings `json:"settings"`
	Skills        []exportSkill `json:"skills"`
}

// exportSkill is one lineage record of a version 1 export document: an
// entry of a snapshot, of kind managed and always with its commit, with
// placed, whether the exporting machine had a placement of it, in place of
// the configurations it was placed in.
type exportSkill struct {
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	Commit          string  `json:"commit"`
	Source          string  `json:"source,omitempty"`
	Subpath         *string `json:"subpath,omitempty"`
	Upstream        string  `json:"upstream,omitempty"`
	UpstreamSubpath *string `json:"upstream_subpath,omitempty"`
	UpstreamCommit  string  `json:"upstream_commit,omitempty"`
	BaseHash        string  `json:"base_hash,omitempty"`
	Placed          bool    `json:"placed"`
}

// importSkillEvent is one skill entry of a snapshot with what this
// machine's account repo holds for it. It is not a librarySkillEvent: that
// one is a directory of the library, while this one is an entry of a
// snapshot, of a skill that may be on no machine at all.
type importSkillEvent struct {
	event
	snapshotSkill
	Placed      bool   `json:"placed"`                 // whether the snapshot places it in any configuration
	State       string `json:"state"`                  // present, missing or different
	LocalCommit string `json:"local_commit,omitempty"` // the commit the account repo holds the name at, when it holds it
}

// importTarget is what an import reads a snapshot against on this machine:
// the account remote its settings set, which an entry of one of your own
// skills names when the snapshot holds no settings of its own, and the
// configurations detected here, which a version 1 record's placed flag
// stands for.
type importTarget struct {
	account  string
	detected []string
}

func newImportCommand(inv *invocation) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Restore the settings from a snapshot and list what the account repo can restore",
		Long: "Replace this machine's settings with those of the snapshot at <file>, after\n" +
			"confirming, and list every skill the snapshot records against what the account\n" +
			"repo of this machine holds. A snapshot that holds no settings leaves them as\n" +
			"they are.\n\n" +
			"It writes the settings and nothing else: it creates no account repo, fetches\n" +
			"nothing, installs nothing and places nothing. A skill the account repo already\n" +
			"holds is restored by placing it; one it is missing comes back by installing it\n" +
			"again from its source.\n\n" +
			"The command asks before it writes. Pass --yes to import without asking, which\n" +
			"is required whenever it cannot ask: with --json, or when either stream is not\n" +
			"a terminal.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.importSettings(cmd.Context(), args[0], yes, cmd.InOrStdin())
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "replace the settings without asking")
	return cmd
}

// importSettings restores the settings of a snapshot and reports what the
// account repo can restore of the skills it records.
//
// The snapshot is read and validated whole before anything is locked, and
// the account repo is read before anything is written, so that a snapshot
// agentx will not accept costs the settings on disk nothing at all. The one
// write is the settings file, through the mutation journal as every
// settings write is, and a snapshot that holds no settings writes nothing.
func (inv *invocation) importSettings(ctx context.Context, path string, yes bool, in io.Reader) error {
	here, err := inv.loadSettings()
	if err != nil {
		return err
	}
	target := importTarget{account: documentAccount(here)}
	for _, c := range scan.Detect(inv.dirs) {
		target.detected = append(target.detected, c.Slug())
	}
	doc, err := readExport(path, target)
	if err != nil {
		return err
	}
	records, err := inv.lineageRecords(ctx)
	if err != nil {
		return err
	}
	remote, err := inv.remoteTips(ctx)
	if err != nil {
		return err
	}
	account := snapshotAccount(doc, target)
	states := restoreStates(doc.Skills, records, remote, account)
	if doc.Settings != nil {
		if err := inv.confirmImport(path, doc, in, yes); err != nil {
			return err
		}
		forgetAccess(doc.Settings)
		if err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
			return home.SaveSettings(inv.dirs.Home, *doc.Settings)
		}); err != nil {
			return mutationFailure(err)
		}
		here = *doc.Settings
	}
	inv.emitSettings(here)
	for _, st := range states {
		inv.out.emit(st)
	}
	inv.printImported(path, doc, states, here)
	inv.summary = importSummary(path, doc.Settings != nil, states)
	return nil
}

// snapshotAccount is the account remote the entries of doc are read
// against: the one its settings set, or, for a snapshot that holds no
// settings, the one this machine's set, which an import then leaves as
// they are.
func snapshotAccount(doc snapshot, target importTarget) string {
	if doc.Settings != nil {
		return documentAccount(*doc.Settings)
	}
	return target.account
}

// forgetAccess takes out of s what another machine found it could do at
// each source. Rights are a machine's own, a matter of its credentials and
// SSH keys, so an import keeps every other field of a source and writes
// none of its access: this machine checks it again when it adds or fetches
// the source.
func forgetAccess(s *home.Settings) {
	for i := range s.Sources {
		s.Sources[i].Access, s.Sources[i].AccessChecked = "", ""
	}
}

// documentLimit bounds the file an import reads. A document is the settings
// plus a few hundred bytes per skill, so a machine with thousands of them
// is still far under this; a file above it does not describe a machine, it
// carries a value somebody chose to be large. Reading it would write a
// settings file that every later command then reads and writes whole, and
// export it again twice the size. No field bound has to be guessed at for
// a field nobody thought of: this one holds them all.
const documentLimit = 8 << 20

// errTooLarge marks a document above that bound.
var errTooLarge = errors.New("larger than the limit")

// readDocument reads at most documentLimit bytes of path. The bound is read
// rather than stat'ed so that nothing between the two can change, and so
// that a file that grows while it is read is refused rather than truncated.
func readDocument(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, documentLimit+1))
	if err != nil {
		return nil, err
	}
	if len(b) > documentLimit {
		return nil, errTooLarge
	}
	return b, nil
}

// readExport reads the snapshot at path, or a version 1 export document,
// and refuses anything that is not one this agentx can restore. Every
// refusal happens here, before the lock and before any write: a file that
// is truncated, that is not JSON, that is of another schema version, that
// carries a field this version does not know or that holds settings agentx
// would not write itself leaves the settings on disk exactly as they were.
//
// A version 1 document comes back as the snapshot it stands for: each
// lineage record an exact entry, and each placed flag every configuration
// detected on this machine that the document's settings leave enabled,
// see importTarget, since the document never said which configurations
// those were.
func readExport(path string, target importTarget) (snapshot, error) {
	b, err := readDocument(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return snapshot{}, fail(exitNotFound, path+" does not exist", "give the path of a snapshot 'agentx export' wrote")
	case errors.Is(err, errTooLarge):
		return snapshot{}, fail(exitRefused,
			fmt.Sprintf("%s is larger than %d bytes, which is more than any machine's settings and skills come to", path, documentLimit),
			"give the path of a snapshot 'agentx export' wrote")
	case err != nil:
		return snapshot{}, fail(exitRefused, "read "+path+": "+err.Error(), "give the path of a snapshot 'agentx export' wrote")
	}
	// The version is read first and on its own: a document of a schema this
	// CLI does not know is refused for that reason rather than for whichever
	// field of it happens to have changed.
	var envelope struct {
		SchemaVersion *int            `json:"schema_version"`
		Settings      json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return snapshot{}, notAnExport(path, err.Error())
	}
	var snap snapshot
	switch {
	case envelope.SchemaVersion == nil:
		return snapshot{}, notAnExport(path, "it carries no schema_version")
	case *envelope.SchemaVersion == 1:
		var doc exportDocument
		doc.Settings.IgnoreSystemFiles = true // the default, as for a settings file without the key
		if err := decodeStrictly(b, &doc); err != nil {
			return snapshot{}, notAnExport(path, err.Error())
		}
		snap = fromExportDocument(doc, target)
	case *envelope.SchemaVersion == snapshotSchemaVersion:
		if len(envelope.Settings) > 0 {
			snap.Settings = &home.Settings{IgnoreSystemFiles: true} // the default, as for a settings file without the key
		}
		if err := decodeStrictly(b, &snap); err != nil {
			return snapshot{}, notAnExport(path, err.Error())
		}
	default:
		return snapshot{}, fail(exitRefused,
			fmt.Sprintf("%s is a snapshot of schema version %d, and this agentx reads versions 1 and %d", path, *envelope.SchemaVersion, snapshotSchemaVersion),
			"export it again from that machine with this version of agentx, or upgrade agentx here")
	}
	if snap.Settings != nil {
		home.Normalise(snap.Settings) // a snapshot may leave out what a settings file agentx wrote always holds
		if err := validSettings(path, *snap.Settings); err != nil {
			return snapshot{}, err
		}
	}
	if err := validEntries(path, snap.Skills, snapshotAccount(snap, target), snap.SchemaVersion == 1); err != nil {
		return snapshot{}, err
	}
	for i := range snap.Skills {
		e := &snap.Skills[i]
		if e.Kind == "" {
			e.Kind = lineage.KindManaged
		}
		if snap.SchemaVersion != 1 {
			e.placed = len(e.Placements) > 0
		}
	}
	return snap, nil
}

// decodeStrictly decodes b into v, refusing a field v does not know, since
// a field this version does not know is a document it cannot restore
// whole. What follows the value is refused by the read of the version,
// which reads b whole.
func decodeStrictly(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// fromExportDocument is the snapshot a version 1 export document stands
// for, see readExport.
func fromExportDocument(doc exportDocument, target importTarget) snapshot {
	enabled := []string{}
	for _, id := range target.detected {
		if !slices.Contains(doc.Settings.DisabledConfigurations, id) {
			enabled = append(enabled, id)
		}
	}
	sort.Strings(enabled)
	snap := snapshot{SchemaVersion: doc.SchemaVersion, Machine: &doc.Machine, Settings: &doc.Settings, Skills: []snapshotSkill{}}
	for _, rec := range doc.Skills {
		e := snapshotSkill{
			Name: rec.Name, Kind: rec.Kind, Commit: rec.Commit,
			Source: rec.Source, Subpath: rec.Subpath, Upstream: rec.Upstream, UpstreamSubpath: rec.UpstreamSubpath,
			UpstreamCommit: rec.UpstreamCommit, BaseHash: rec.BaseHash,
			Placements: []string{}, placed: rec.Placed,
		}
		if rec.Placed {
			e.Placements = slices.Clone(enabled)
		}
		snap.Skills = append(snap.Skills, e)
	}
	return snap
}

// notAnExport refuses a file that is not a snapshot this agentx reads. The
// reason often quotes the file, a field name or a character of it, so it is
// sanitised the way every value agentx did not write is before it is
// printed: a snapshot from another machine may hold anything.
func notAnExport(path, why string) error {
	return fail(exitRefused, path+" is not an agentx snapshot: "+sanitised(why), "give the path of a snapshot 'agentx export' wrote")
}

// validSettings refuses a settings object agentx would not have written
// itself, since importing it would leave the machine with a settings file
// its own commands then refuse.
//
// Every field is checked, and not only the ones a command would notice.
// An import is the one route by which a string somebody else chose reaches
// the settings file, so a field nothing else in the CLI ever writes – the
// source `alias` is one – has this gate and no other in front of it, and
// the rule that guards the normal path has to guard this one too. The
// source URLs are why that matters beyond the schema: a URL carrying a
// user or a token never reaches the settings by any other route, and an
// import may not be the one that puts one there.
func validSettings(path string, s home.Settings) error {
	for _, check := range []func(home.Settings) string{
		badSchemaVersion, badLabel, badDisabledConfigurations, badSources, badCopyMode,
	} {
		if why := check(s); why != "" {
			return fail(exitRefused, path+" holds settings agentx cannot write: "+sanitised(why),
				"fix the snapshot, or export it again from the machine it came from")
		}
	}
	return nil
}

// Each check below answers with the reason the settings are not ones agentx
// would write, and "" when that part of them is sound.

func badSchemaVersion(s home.Settings) string {
	if s.SchemaVersion != settingsSchemaVersion {
		return fmt.Sprintf("their schema_version is %d, and this agentx writes %d", s.SchemaVersion, settingsSchemaVersion)
	}
	return ""
}

// badLabel holds the label to the rule config set label and machine rename
// are held to, so that a settings file agentx wrote round trips and one it
// would refuse to write is refused here.
func badLabel(s home.Settings) string {
	if s.Label == "" {
		return "" // absent until it is set, and the hostname stands in
	}
	if err := validLabel(s.Label); err != nil {
		return err.Error()
	}
	return ""
}

// badDisabledConfigurations: config disable writes a detected
// configuration id and keeps the list sorted. Membership of the registry
// is not what is checked – a document written on a machine with a client
// this build has no entry for names a configuration that is real there –
// but the shape and the order are.
func badDisabledConfigurations(s home.Settings) string {
	seen := map[string]bool{}
	for _, id := range s.DisabledConfigurations {
		switch {
		case !configurationID(id):
			return "disabled_configurations holds " + clipped(id) + ", which is not a configuration id"
		case seen[id]:
			return id + " is disabled twice"
		}
		seen[id] = true
	}
	if !slices.IsSorted(s.DisabledConfigurations) {
		return "disabled_configurations must be sorted"
	}
	return ""
}

// badSources holds every field of every entry, and the list as a whole, to
// what agentx writes, see sourcesRefusal. An SSH host is read as itself:
// an import reads no SSH configuration.
func badSources(s home.Settings) string {
	return sourcesRefusal(s.Sources, nil)
}

// badSourceURL holds one URL of a source entry to the canonical form. The
// reason never repeats what was given: an input carrying a user or a token
// is exactly what this refuses, and source.Parse redacts what it names, so
// the canonical URL it parsed to is what the message carries.
func badSourceURL(raw string) string {
	parsed, err := source.Parse(raw)
	if err != nil {
		return err.Error()
	}
	if parsed.URL != raw || parsed.Subpath != "" || parsed.Ref != "" {
		return parsed.URL + " must be stored as the canonical URL of a source alone"
	}
	return ""
}

// fetchTime reports whether v is the spelling source add and source fetch
// write: RFC 3339 in UTC, to the second.
func fetchTime(v string) bool {
	t, err := time.Parse(time.RFC3339, v)
	return err == nil && t.UTC().Format(time.RFC3339) == v
}

// badCopyMode checks what copy_mode says and how it is spelled. agentx
// writes it by marshalling the map it holds, so the object is sorted by
// key, holds no key twice and holds no skill whose list of configurations
// is empty: SetCopyModes drops that entry rather than writing it. A
// document that spells it otherwise, JSON null included, says something
// the settings file agentx writes cannot say, and copy_mode is kept as raw
// JSON precisely so that a write does not lose it: what is imported is
// read back and written out again for good.
func badCopyMode(s home.Settings) string {
	modes, err := s.CopyModes()
	if err != nil {
		return "copy_mode must map skill names to configuration ids"
	}
	names := make([]string, 0, len(modes))
	for name := range modes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if nameRefusal(name) != "" {
			return "copy_mode holds " + clipped(name) + ", which is no name a skill of the library has"
		}
		if len(modes[name]) == 0 {
			return "copy_mode holds " + name + " with no configuration, which agentx removes rather than writes"
		}
		for _, id := range modes[name] {
			if !configurationID(id) {
				return "copy_mode copies " + name + " to " + clipped(id) + ", which is not a configuration id"
			}
		}
	}
	canonical, err := json.Marshal(modes)
	if err != nil {
		return "copy_mode must map skill names to configuration ids"
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, s.CopyMode); err != nil || !bytes.Equal(compact.Bytes(), canonical) {
		return "copy_mode must be written the way agentx writes it: an object, sorted, each skill once"
	}
	return ""
}

// documentAccount is the URL of the account remote the settings of a
// document set, "" when they set none.
func documentAccount(s home.Settings) string {
	entry, _ := accountEntry(s)
	return entry.URL
}

// ownRecord reports whether e is one of your own skills, which a fork
// branch holds: a managed entry whose source is the account remote the
// snapshot is read against, account, or that has no source at all, as
// your own skills have none while no account remote is set. Every other
// managed entry is a skill of a shared source, whose import branch holds
// it.
func ownRecord(e snapshotSkill, account string) bool {
	return e.Kind != lineage.KindUnmanaged && (e.Source == "" || account != "" && e.Source == account)
}

// recordedImport is the upstream version an entry names, see
// snapshotSkill: the source's for a skill of a shared source, the
// upstream's for one of your own, and whether it names any field of one.
func recordedImport(e snapshotSkill, own bool) (lineage.Import, bool) {
	source, subpath := e.Source, e.Subpath
	if own {
		source, subpath = e.Upstream, e.UpstreamSubpath
	}
	i := lineage.Import{Source: source, Commit: e.UpstreamCommit, Hash: e.BaseHash}
	if subpath != nil {
		i.Path = *subpath
	}
	return i, source != "" || subpath != nil || e.UpstreamCommit != "" || e.BaseHash != ""
}

// validEntries refuses skill entries that are not ones agentx could act
// on: a name no branch of the library namespace can carry, a name given
// twice, a kind other than managed and unmanaged, placements that are not
// configuration ids as agentx writes them, a commit that is not an object
// id, or lineage that the trailer reader would not accept. account is the
// account remote the snapshot is read against, which tells your own skills
// from a shared source's, see ownRecord: one of your own carries no
// subpath and names its upstream version, if any, in its upstream fields;
// a shared source's carries its subpath and no upstream, and names its
// version in its source fields. An unmanaged skill carries no source and
// no version at all. An entry with no commit follows the latest version and
// names no other part of one. The trailers are checked by the gate every
// import commit passes before it is written, which is that reader itself on
// the message the commit would carry, so an exact entry this command
// accepts is exactly one a branch could hold.
//
// v1 holds the records of a version 1 export document to what that
// document always held: the kind managed and a commit. Its subpath was
// left out for a skill at the root of its source.
func validEntries(path string, skills []snapshotSkill, account string, v1 bool) error {
	refuse := func(why string) error {
		return fail(exitRefused, path+" lists a skill agentx cannot read: "+sanitised(why),
			"fix the snapshot, or export it again from the machine it came from")
	}
	seen := map[string]bool{}
	for i, e := range skills {
		switch {
		case nameRefusal(e.Name) != "":
			// Not "no name a branch can carry": a branch carries
			// nested/deeper perfectly well, and it is the library, which
			// holds one directory per skill, that cannot.
			return refuse(fmt.Sprintf("the entry at position %d is named %q, which is not a name the library and a branch can both hold", i+1, clipped(e.Name)))
		case seen[e.Name]:
			return refuse(e.Name + " is listed twice")
		case v1 && e.Kind != lineage.KindManaged:
			return refuse("the kind of " + e.Name + " is not " + lineage.KindManaged)
		case e.Kind != "" && e.Kind != lineage.KindManaged && e.Kind != lineage.KindUnmanaged:
			return refuse("the kind of " + e.Name + " is neither " + lineage.KindManaged + " nor " + lineage.KindUnmanaged)
		case v1 && !lineage.IsObjectID(e.Commit), e.Commit != "" && !lineage.IsObjectID(e.Commit):
			return refuse(e.Name + " does not name the commit its branch points at")
		}
		seen[e.Name] = true
		if why := placementsRefusal(e); why != "" {
			return refuse(why)
		}
		if e.Kind == lineage.KindUnmanaged {
			if e.ForkID != "" || e.Commit != "" || e.Source != "" || e.Subpath != nil || e.Upstream != "" ||
				e.UpstreamSubpath != nil || e.UpstreamCommit != "" || e.BaseHash != "" {
				return refuse(e.Name + " is unmanaged, which has no source and no version")
			}
			continue
		}
		own := ownRecord(e, account)
		switch {
		case own && e.Subpath != nil:
			return refuse(e.Name + " is one of your own skills, which have no subpath")
		case !own && (e.Upstream != "" || e.UpstreamSubpath != nil):
			return refuse(e.Name + " names an upstream, which only your own skills have")
		case !own && e.ForkID != "":
			return refuse(e.Name + " names a fork id, which only your own skills have")
		case e.ForkID != "" && !lineage.IsForkID(e.ForkID):
			return refuse(e.Name + " names a fork id that is not one")
		case !own && !v1 && e.Subpath == nil:
			return refuse(e.Name + " names its source and not its folder in it, the subpath")
		}
		imported, named := recordedImport(e, own)
		if e.Commit == "" {
			if e.UpstreamCommit != "" || e.BaseHash != "" || e.Upstream != "" || e.UpstreamSubpath != nil {
				return refuse(e.Name + " names no commit, so it follows the latest version, and names part of a version all the same")
			}
			// A source and a folder of it, held to the same reader as an
			// exact entry's, with a version that is only there to make
			// the message whole.
			imported.Commit, imported.Hash = strings.Repeat("0", 40), strings.Repeat("0", 64)
			if !own && lineage.Unrecordable(imported) != "" {
				return refuse(e.Name + " does not name the canonical URL of a source and a folder of it")
			}
			continue
		}
		if !named && own {
			continue // a branch with no imported version: a skill made by skill new
		}
		if lineage.Unrecordable(imported) != "" {
			return refuse(e.Name + " does not carry the lineage of one upstream version")
		}
	}
	return nil
}

// placementsRefusal holds the placements of an entry to what agentx
// writes: a list, [] for none, of configuration ids, sorted, each once.
// Membership of the registry is not what is checked – a snapshot of a
// machine with a client this build has no entry for names a configuration
// that is real there – but the shape and the order are.
func placementsRefusal(e snapshotSkill) string {
	if e.Placements == nil {
		return e.Name + " lists no placements: an entry names the configurations it is placed in, [] for none"
	}
	for i, id := range e.Placements {
		switch {
		case !configurationID(id):
			return e.Name + " is placed in " + clipped(id) + ", which is not a configuration id"
		case i > 0 && e.Placements[i-1] >= id:
			return "the placements of " + e.Name + " must be sorted, each configuration once"
		}
	}
	return ""
}

// restoreStates compares every entry of the snapshot with what the account
// repo of this machine holds under that name, in the namespace the entry
// belongs in: your own skills, see ownRecord, under skills/, and a shared
// source's under managed/. A branch in that namespace at the same commit
// is the version the entry names, and at any commit for an entry that
// follows the latest version, which only an update can tell apart; a name
// the repo does not hold at all is missing; a name it holds otherwise, at
// another commit or in the other namespace, is different, and the commit
// it holds is reported beside it. One of your own skills the repo holds no
// branch of is also held when the account remote holds it, as remote lists
// the remote-tracking branches by name: a machine that set the account
// remote and fetched it holds every skill published there, so such an
// entry reads present when its remote-tracking branch is at its commit and
// different when that is at another. An unmanaged skill has no branch
// anywhere: it is different when the repo holds a branch of its name and
// missing otherwise. A machine with no account repo holds nothing, and
// creating one is not this command's to do, so every entry is then
// missing.
func restoreStates(skills []snapshotSkill, records map[string]lineage.Record, remote map[string]string, account string) []importSkillEvent {
	states := make([]importSkillEvent, 0, len(skills))
	for _, e := range skills {
		st := importSkillEvent{event: newEvent("import_skill"), snapshotSkill: e, Placed: e.placed, State: restoreMissing}
		have, local := records[e.Name]
		if e.Kind == lineage.KindUnmanaged {
			if local {
				st.LocalCommit, st.State = have.Commit, restoreDifferent
			}
			states = append(states, st)
			continue
		}
		kind := lineage.KindManaged
		if ownRecord(e, account) {
			kind = lineage.KindFork
		}
		if there := remote[e.Name]; !local && there != "" && kind == lineage.KindFork {
			have, local = lineage.Record{Kind: lineage.KindFork, Commit: there}, true
		}
		if local {
			st.LocalCommit = have.Commit
			st.State = restoreDifferent
			if have.Kind == kind && (e.Commit == "" || have.Commit == e.Commit) {
				st.State = restorePresent
			}
		}
		states = append(states, st)
	}
	return states
}

// remoteTips is what the account remote held of every fork as the last
// fetch left it, see lineage.ListRemote: none on a machine with no account
// repo, or with no account remote set.
func (inv *invocation) remoteTips(ctx context.Context) (map[string]string, error) {
	_, remote, ok, err := inv.accountSource()
	if err != nil || !ok {
		return nil, err
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	if !exists {
		return nil, nil
	}
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	return tips, nil
}

// confirmImport asks before the settings are replaced. An import overwrites
// what only this machine decides, so it is never the default answer:
// without --yes the command asks, and where it cannot ask it refuses rather
// than assuming. It can ask only when a person is there to answer, which is
// when the run is not in JSON mode and both the stream the question goes to
// and the stream the answer comes from are terminals. The desktop app and
// every script are on the other side of that line and pass --yes.
func (inv *invocation) confirmImport(path string, doc snapshot, in io.Reader, yes bool) error {
	if yes {
		return nil
	}
	if inv.out.json || !isTerminal(in) || !isTerminal(inv.out.stdout) {
		return fail(exitRefused,
			"importing replaces the settings in "+home.SettingsPath(inv.dirs.Home)+", and there is no terminal to confirm at",
			"run it again with --yes to import without asking")
	}
	return inv.askToImport(path, doc, in)
}

// askToImport puts the question and reads the answer. Anything but yes is a
// refusal: the answer to a question about replacing a file is yes or it is
// no, and a stream that ends without one has not said yes. The read error
// is not looked at for the same reason: what was read stands on its own,
// and a line that ended in end-of-file rather than a newline is an answer
// like any other.
func (inv *invocation) askToImport(path string, doc snapshot, in io.Reader) error {
	whose := "those"
	if doc.Machine != nil && doc.Machine.Label != "" {
		whose += " of " + sanitised(doc.Machine.Label)
	}
	inv.out.prompt("Replace the settings in " + home.SettingsPath(inv.dirs.Home) + " with " + whose + " from " + path + "? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return fail(exitRefused, "the import was declined: the settings were not changed",
		"run it again with --yes to import without asking")
}

// printImported writes the confirmation, then one row per entry of the
// snapshot with what the account repo has of it. Every value the snapshot
// supplies is sanitised: agentx did not write it, and a file handed from
// another machine may hold anything. settings are the settings the machine
// now has, the snapshot's or, for one that holds none, its own.
func (inv *invocation) printImported(path string, doc snapshot, states []importSkillEvent, settings home.Settings) {
	out := inv.out
	switch {
	case doc.Settings == nil:
		out.done("read " + out.paint(heading, path) + ": it holds no settings, so the settings were left as they are")
	case doc.Machine != nil && doc.Machine.Label != "":
		out.done("imported the settings of " + out.paint(heading, sanitised(doc.Machine.Label)) + " from " + out.paint(heading, path))
	default:
		out.done("imported the settings from " + out.paint(heading, path))
	}
	closing := func() {
		if doc.Settings == nil {
			printNothingWritten(out, settings.Sources)
			return
		}
		printSourcesToAdd(out, settings.Sources)
	}
	if len(states) == 0 {
		out.print("No skills in the snapshot.")
		// A machine that added sources and installed nothing still has
		// sources to add again, and this is the only place that says how.
		if doc.Settings != nil && len(settings.Sources) > 0 {
			printSourcesToAdd(out, settings.Sources)
		}
		return
	}
	present, missing, different := restoreCounts(states)
	out.print(out.paint(heading, plural(len(states), "skill")+" in the snapshot"), ": ",
		fmt.Sprintf("%d present, %d missing, %d at a different version", present, missing, different))
	account := documentAccount(settings)
	t := &table{}
	for _, st := range states {
		t.add(importRow(out, st, account)...)
	}
	out.render(t, "")
	closing()
}

// printNothingWritten is the closing line of an import of a snapshot that
// holds no settings: nothing was written, and the sources this machine
// already has are the ones to install from.
func printNothingWritten(out *writer, sources []home.Source) {
	install := "install a missing skill with 'agentx skill add <source>'"
	if slices.ContainsFunc(sources, func(src home.Source) bool { return src.Account }) {
		install += ", or one of your own with 'agentx skill add --name <name>'"
	}
	out.print("  ", out.paint(muted, "nothing was written: "+install))
}

// printSourcesToAdd writes the closing line of an import. An import
// restores the settings entry of a source and nothing of the source itself:
// no account repo is created and no remote configured, so on the machine an
// import just made, skill add answers that the source was never added.
// source add is the step that has to come first, and nothing said so. It is
// printed per source as the settings now hold it: source add writes the pin
// its argument names, so the bare URL would unpin the source this import
// just restored. The account remote is attached again with source add
// --account, and your own skills are installed from it by name.
func printSourcesToAdd(out *writer, sources []home.Source) {
	if len(sources) == 0 {
		out.print("  ", out.paint(muted, "only the settings were written: install a missing skill with 'agentx skill add <source>'"))
		return
	}
	install := "install a missing skill with 'agentx skill add <source>'"
	for _, src := range sources {
		if src.Account {
			install += ", or one of your own with 'agentx skill add --name <name>'"
			break
		}
	}
	out.print("  ", out.paint(muted, "only the settings were written: add each source again, then "+install))
	for _, src := range sources {
		out.print("    ", out.paint(label, sourceAddLine(src)))
	}
}

// sourceAddLine is the command that adds src again as the settings hold
// it, every word of it quoted for a POSIX shell.
func sourceAddLine(src home.Source) string {
	line := "agentx source add " + sourceAddArg(src.URL, src.Pin)
	if src.Account {
		line = "agentx source add " + shellWord(sanitised(src.URL)) + " --account"
	}
	return line
}

// sourceAddArg is the argument of a source add that adds a source again as
// the settings hold it: the URL, with the pin when there is one, sanitised
// and quoted for a POSIX shell. Every line that tells the user to add a
// source the settings already hold writes it this way, so that following
// one keeps the pin.
func sourceAddArg(url, pin string) string {
	arg := sanitised(url)
	if pin != "" {
		arg += "#" + sanitised(pin)
	}
	return shellWord(arg)
}

// shellSafe is characters a POSIX shell takes literally wherever they stand
// in a word, # too but for the first place, where it opens a comment. The
// list is short on purpose: a character left off it costs a pair of quotes,
// one wrongly on it runs a command.
const shellSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/:@%+=,#-"

// shellWord makes text a line tells the user to run into the one word a
// POSIX shell reads back as that text. The text comes from an export or
// from the name of a library directory, and a URL, a pin and a name agentx
// accepts may still hold $, &, ; or a quote, so pasted bare it could run a
// command of its own or hand agentx another argument. Text of safe
// characters alone prints as it is; anything else is single quoted, the one
// quoting in which nothing is special, with each single quote it holds
// closed, escaped and reopened.
func shellWord(text string) string {
	if text != "" && text[0] != '#' && strings.IndexFunc(text, func(r rune) bool { return !strings.ContainsRune(shellSafe, r) }) < 0 {
		return text
	}
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

// importRow is one line of the listing: the name, what the account repo
// knows it as, what it has of it, where it is published to and the
// configurations the snapshot places it in, account being the account
// remote the snapshot is read against, to which an entry of one of your
// own skills, see ownRecord, is published. An unmanaged skill is published
// nowhere.
func importRow(out *writer, st importSkillEvent, account string) []cell {
	state := c(st.State, okStyle)
	if st.State != restorePresent {
		state = c(st.State, warnStyle)
	}
	source := c("(none)", muted)
	if st.Kind != lineage.KindUnmanaged {
		source = sourceCell(ownRecord(st.snapshotSkill, account), st.Source, st.Subpath, st.Upstream, st.UpstreamSubpath, account)
	}
	placed := c("not placed", muted)
	if len(st.Placements) > 0 {
		placed = c("placed in "+strings.Join(st.Placements, ", "), plain)
	}
	return []cell{c("  "+sanitised(st.Name), heading), c(st.Kind, muted), state, source, placed}
}

func restoreCounts(states []importSkillEvent) (present, missing, different int) {
	for _, st := range states {
		switch st.State {
		case restorePresent:
			present++
		case restoreMissing:
			missing++
		default:
			different++
		}
	}
	return present, missing, different
}

// importSummary is what the result event says the import did, settings
// being whether the snapshot held settings it wrote.
func importSummary(path string, settings bool, states []importSkillEvent) string {
	done := "imported the settings from " + path
	if !settings {
		done = "read " + path + ", which holds no settings"
	}
	if len(states) == 0 {
		return done + ": the snapshot lists no skills"
	}
	present, missing, different := restoreCounts(states)
	return fmt.Sprintf("%s: %d of %s present, %d missing, %d at a different version",
		done, present, plural(len(states), "skill"), missing, different)
}
