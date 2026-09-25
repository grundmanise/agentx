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

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The three states one record of an export is listed with: the account repo
// of this machine holds that branch at that commit, holds no branch of that
// name, or holds one that is not this record's.
const (
	restorePresent   = "present"
	restoreMissing   = "missing"
	restoreDifferent = "different"
)

// importSkillEvent is one lineage record of an export document with what
// this machine's account repo holds for it. It is not a librarySkillEvent:
// that one is a directory of the library, while this one is a record of a
// document, of a skill that may be on no machine at all.
type importSkillEvent struct {
	event
	exportSkill
	State       string `json:"state"`                  // present, missing or different
	LocalKind   string `json:"local_kind,omitempty"`   // the kind the account repo holds, when it holds the name
	LocalCommit string `json:"local_commit,omitempty"` // the commit it holds it at
}

func newImportCommand(inv *invocation) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Restore the settings from an export and list what the account repo can restore",
		Long: "Replace this machine's settings with those of the export at <file>, after\n" +
			"confirming, and list every skill the export records against what the account\n" +
			"repo of this machine holds.\n\n" +
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

// importSettings restores the settings of an export document and reports
// what the account repo can restore of the skills it records.
//
// The document is read and validated whole before anything is locked, and
// the account repo is read before anything is written, so that a document
// agentx will not accept costs the settings on disk nothing at all. The one
// write is the settings file, through the mutation journal as every
// settings write is.
func (inv *invocation) importSettings(ctx context.Context, path string, yes bool, in io.Reader) error {
	doc, err := readExport(path)
	if err != nil {
		return err
	}
	records, err := inv.lineageRecords(ctx)
	if err != nil {
		return err
	}
	states := restoreStates(doc.Skills, records)
	if err := inv.confirmImport(path, doc, in, yes); err != nil {
		return err
	}
	if err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		return home.SaveSettings(inv.dirs.Home, doc.Settings)
	}); err != nil {
		return mutationFailure(err)
	}
	inv.emitSettings(doc.Settings)
	for _, st := range states {
		inv.out.emit(st)
	}
	inv.printImported(path, doc, states)
	inv.summary = importSummary(path, states)
	return nil
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

// readExport reads the document at path and refuses anything that is not
// one this agentx can restore. Every refusal happens here, before the lock
// and before any write: a file that is truncated, that is not JSON, that is
// of another schema version, that carries a field this version does not
// know or that holds settings agentx would not write itself leaves the
// settings on disk exactly as they were.
func readExport(path string) (exportDocument, error) {
	b, err := readDocument(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return exportDocument{}, fail(exitNotFound, path+" does not exist", "give the path of a file 'agentx export' wrote")
	case errors.Is(err, errTooLarge):
		return exportDocument{}, fail(exitRefused,
			fmt.Sprintf("%s is larger than %d bytes, which is more than any machine's settings and skills come to", path, documentLimit),
			"give the path of a file 'agentx export' wrote")
	case err != nil:
		return exportDocument{}, fail(exitRefused, "read "+path+": "+err.Error(), "give the path of a file 'agentx export' wrote")
	}
	// The version is read first and on its own: a document of a schema this
	// CLI does not know is refused for that reason rather than for whichever
	// field of it happens to have changed.
	var envelope struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return exportDocument{}, notAnExport(path, err.Error())
	}
	switch {
	case envelope.SchemaVersion == nil:
		return exportDocument{}, notAnExport(path, "it carries no schema_version")
	case *envelope.SchemaVersion != exportSchemaVersion:
		return exportDocument{}, fail(exitRefused,
			fmt.Sprintf("%s is an export of schema version %d, and this agentx reads version %d", path, *envelope.SchemaVersion, exportSchemaVersion),
			"export it again from that machine with this version of agentx, or upgrade agentx here")
	}
	var doc exportDocument
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields() // a field this version does not know is a document it cannot restore whole
	if err := dec.Decode(&doc); err != nil {
		return exportDocument{}, notAnExport(path, err.Error())
	}
	home.Normalise(&doc.Settings) // a document may leave out what a settings file agentx wrote always holds
	if err := validSettings(path, doc.Settings); err != nil {
		return exportDocument{}, err
	}
	if err := validRecords(path, doc.Skills); err != nil {
		return exportDocument{}, err
	}
	return doc, nil
}

// notAnExport refuses a file that is not a document this agentx reads. The
// reason often quotes the file, a field name or a character of it, so it is
// sanitised the way every value agentx did not write is before it is
// printed: a document from another machine may hold anything.
func notAnExport(path, why string) error {
	return fail(exitRefused, path+" is not an agentx export: "+sanitised(why), "give the path of a file 'agentx export' wrote")
}

// validSettings refuses a settings object agentx would not have written
// itself, since importing it would leave the machine with a settings file
// its own commands then refuse.
//
// Every field is checked, and not only the ones a command would notice.
// An import is the one route by which a string somebody else chose reaches
// the settings file, so a field nothing else in the CLI ever writes — the
// source `alias` is one — has this gate and no other in front of it, and
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
				"fix the export, or write it again from the machine it came from")
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
// is not what is checked — a document written on a machine with a client
// this build has no entry for names a configuration that is real there —
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

// badSources checks every field of every entry. url and alias are both
// URLs of a source and are held to the same rule; pin is a ref this
// machine will hand to git, which source.ValidRef exists to check before
// that happens; last_fetched is a date agentx wrote and nothing reads back.
// SetSource keeps the list sorted by url and holds one entry per source,
// so two entries for one source are a document agentx did not write and a
// pin or an alias silently lost: FindSource only ever sees the first.
func badSources(s home.Settings) string {
	seen := map[string]bool{}
	for _, src := range s.Sources {
		// url and alias are the same kind of thing and are held to the
		// same rule. url is always there, an entry being a source; alias
		// is absent until something maps a second URL onto it, which
		// nothing does yet, so an import is the only writer it has.
		urls := []string{src.URL}
		if src.Alias != "" {
			urls = append(urls, src.Alias)
		}
		for _, url := range urls {
			if why := badSourceURL(url); why != "" {
				return why
			}
		}
		switch {
		case src.Pin != "" && !source.ValidRef(src.Pin):
			return src.URL + " is pinned to " + clipped(src.Pin) + ", which is not a ref git accepts"
		case src.LastFetched != "" && !fetchTime(src.LastFetched):
			return src.URL + " was last fetched at " + clipped(src.LastFetched) + ", which is not RFC 3339 in UTC"
		case seen[src.URL]:
			return src.URL + " is listed twice"
		}
		seen[src.URL] = true
	}
	if !slices.IsSortedFunc(s.Sources, func(a, b home.Source) int { return strings.Compare(a.URL, b.URL) }) {
		return "the sources must be sorted by url"
	}
	return ""
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
// is empty — SetCopyModes drops that entry rather than writing it. A
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

// validRecords refuses a listing that is not one the account repo could
// have produced: a name no branch of the library namespace can carry, a
// kind that is neither, a commit that is not an object id, or lineage that
// the trailer reader would not accept. The trailers are checked by the
// gate every import commit passes before it is written, which is that
// reader itself on the message the commit would carry, so a record this
// command accepts is exactly a record a branch could hold.
func validRecords(path string, skills []exportSkill) error {
	refuse := func(why string) error {
		return fail(exitRefused, path+" lists a skill agentx cannot read: "+sanitised(why),
			"fix the export, or write it again from the machine it came from")
	}
	seen := map[string]bool{}
	for i, rec := range skills {
		switch {
		case nameRefusal(rec.Name) != "":
			// Not "no name a branch can carry": a branch carries
			// nested/deeper perfectly well, and it is the library, which
			// holds one directory per skill, that cannot.
			return refuse(fmt.Sprintf("the record at position %d is named %q, which is not a name the library and a branch can both hold", i+1, clipped(rec.Name)))
		case seen[rec.Name]:
			return refuse(rec.Name + " is listed twice")
		case rec.Kind != lineage.KindManaged && rec.Kind != lineage.KindFork:
			return refuse(rec.Name + " is neither " + lineage.KindManaged + " nor " + lineage.KindFork)
		case !lineage.IsObjectID(rec.Commit):
			return refuse(rec.Name + " does not name the commit its branch points at")
		}
		seen[rec.Name] = true
		if rec.Source == "" && rec.Subpath == nil && rec.UpstreamCommit == "" && rec.BaseHash == "" {
			continue // a branch with no imported version: a fork with nothing merged into it yet
		}
		var subpath string
		if rec.Subpath != nil {
			subpath = *rec.Subpath
		}
		if lineage.Unrecordable(lineage.Import{Source: rec.Source, Path: subpath, Commit: rec.UpstreamCommit, Hash: rec.BaseHash}) != "" {
			return refuse(rec.Name + " does not carry the lineage of one upstream version")
		}
	}
	return nil
}

// restoreStates compares every record of the export with what the account
// repo of this machine holds under that name. A branch of the same kind at
// the same commit is the version the export names; a name the repo does not
// hold at all is missing; a name it holds otherwise, at another commit or in
// the other namespace, is different, and what it holds is reported beside
// it. A machine with no account repo holds nothing, and creating one is not
// this command's to do, so every record is then missing.
func restoreStates(skills []exportSkill, records map[string]lineage.Record) []importSkillEvent {
	states := make([]importSkillEvent, 0, len(skills))
	for _, rec := range skills {
		st := importSkillEvent{event: newEvent("import_skill"), exportSkill: rec, State: restoreMissing}
		if have, ok := records[rec.Name]; ok {
			st.LocalKind, st.LocalCommit = have.Kind, have.Commit
			st.State = restoreDifferent
			if have.Kind == rec.Kind && have.Commit == rec.Commit {
				st.State = restorePresent
			}
		}
		states = append(states, st)
	}
	return states
}

// confirmImport asks before the settings are replaced. An import overwrites
// what only this machine decides, so it is never the default answer:
// without --yes the command asks, and where it cannot ask it refuses rather
// than assuming. It can ask only when a person is there to answer, which is
// when the run is not in JSON mode and both the stream the question goes to
// and the stream the answer comes from are terminals. The desktop app and
// every script are on the other side of that line and pass --yes.
func (inv *invocation) confirmImport(path string, doc exportDocument, in io.Reader, yes bool) error {
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
func (inv *invocation) askToImport(path string, doc exportDocument, in io.Reader) error {
	inv.out.prompt("Replace the settings in " + home.SettingsPath(inv.dirs.Home) + " with those of " +
		sanitised(doc.Machine.Label) + " from " + path + "? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	}
	return fail(exitRefused, "the import was declined: the settings were not changed",
		"run it again with --yes to import without asking")
}

// printImported writes the confirmation, then one row per record of the
// export with what the account repo has of it. Every value the document
// supplies is sanitised: agentx did not write it, and a file handed from
// another machine may hold anything.
func (inv *invocation) printImported(path string, doc exportDocument, states []importSkillEvent) {
	out := inv.out
	out.done("imported the settings of " + out.paint(heading, sanitised(doc.Machine.Label)) + " from " + out.paint(heading, path))
	if len(states) == 0 {
		out.print("No skills in the export.")
		// A machine that added sources and installed nothing still has
		// sources to add again, and this is the only place that says how.
		if len(doc.Settings.Sources) > 0 {
			printSourcesToAdd(out, doc.Settings.Sources)
		}
		return
	}
	present, missing, different := restoreCounts(states)
	out.print(out.paint(heading, plural(len(states), "skill")+" in the export"), ": ",
		fmt.Sprintf("%d in the account repo, %d missing, %d at a different version", present, missing, different))
	t := &table{}
	for _, st := range states {
		t.add(importRow(out, st)...)
	}
	out.render(t, "")
	printSourcesToAdd(out, doc.Settings.Sources)
}

// printSourcesToAdd writes the closing line of an import. An import
// restores the settings entry of a source and nothing of the source itself:
// no account repo is created and no remote configured, so on the machine an
// import just made, skill add answers that the source was never added.
// source add is the step that has to come first, and nothing said so. It is
// printed per source with the pin the settings now hold: source add writes
// the pin its argument names, so the bare URL would unpin the source this
// import just restored.
func printSourcesToAdd(out *writer, sources []home.Source) {
	if len(sources) == 0 {
		out.print("  ", out.paint(muted, "only the settings were written: install a missing skill with 'agentx skill add <source>'"))
		return
	}
	out.print("  ", out.paint(muted, "only the settings were written: add each source again, then install a missing skill with 'agentx skill add <source>'"))
	for _, src := range sources {
		out.print("    ", out.paint(label, "agentx source add "+sourceAddArg(src.URL, src.Pin)))
	}
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
// knows it as, what it has of it and where it came from.
func importRow(out *writer, st importSkillEvent) []cell {
	state := c(st.State, okStyle)
	if st.State != restorePresent {
		state = c(st.State, warnStyle)
	}
	upstream := c("(none)", muted)
	if st.Source != "" {
		where := st.Source
		if st.Subpath != nil && *st.Subpath != "" {
			where += "/" + *st.Subpath
		}
		upstream = c(sanitised(where), plain)
	}
	return []cell{c("  "+sanitised(st.Name), heading), c(st.Kind, muted), state, upstream}
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

// importSummary is what the result event says the import did.
func importSummary(path string, states []importSkillEvent) string {
	if len(states) == 0 {
		return "imported the settings from " + path + ": the export lists no skills"
	}
	present, missing, different := restoreCounts(states)
	return fmt.Sprintf("imported the settings from %s: %d of %s in the account repo, %d missing, %d at a different version",
		path, present, plural(len(states), "skill"), missing, different)
}
