package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/skillshare"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/vercel"
)

// The tools whose install records agentx skill import reads.
const (
	toolVercel     = "vercel"     // the Vercel skills CLI, which installs into the library and keeps a lock file
	toolSkillshare = "skillshare" // Skillshare, which installs into a folder of its own and keeps a metadata store
)

// The states one record is reported in. A preview reports candidate,
// managed and refused; a run that adopts reports adopted in place of
// candidate.
const (
	adoptCandidate = "candidate" // it can be adopted
	adoptAdopted   = "adopted"   // this run installed it
	adoptManaged   = "managed"   // agentx already knows the skill
	adoptRefused   = "refused"   // it cannot be adopted, and why
)

// adoptionEvent is one skill another tool installed, as that tool recorded
// it: where the record says it came from, and what agentx did or would do
// about it.
type adoptionEvent struct {
	event
	Name    string  `json:"name"`
	Tool    string  `json:"tool"`              // the tool that installed it: vercel or skillshare
	File    string  `json:"file"`              // the file the record was read from
	Source  string  `json:"source,omitempty"`  // the canonical URL of the source it names
	Subpath *string `json:"subpath,omitempty"` // the skill's directory in that source, "" for its root
	State   string  `json:"state"`             // candidate, adopted, managed or refused
	Reason  string  `json:"reason,omitempty"`  // why it was refused, or what an adoption would still have to do
}

// phaseFetch is the step a run reports for one source it fetches: the one
// agentx skill import adds, and the ones source fetch, check-updates, publish and
// the hub commands fetch.
const phaseFetch = "fetch"

// adoptSelection is what one run of agentx skill import was asked to do: nothing
// but look, adopt the skills named, or adopt every one recorded, of one tool
// or of both.
type adoptSelection struct {
	tool  string // the tool whose records are read: vercel, skillshare, or "" for both
	names []string
	all   bool
	force bool // replace a client's folder of a named skill that holds other files
}

// adoptTools maps the value of --from that names a tool to that tool.
var adoptTools = map[string]string{"skills-cli": toolVercel, "skillshare": toolSkillshare}

// adoptTool is the tool the value of --from names. Anything else is a
// usage error, and one that looks like a skill name is pointed at the
// argument that names a skill.
func adoptTool(arg string) (string, error) {
	if tool, ok := adoptTools[arg]; ok {
		return tool, nil
	}
	return "", fail(exitUsage, fmt.Sprintf("%q is not a tool agentx imports from", sanitised(arg)),
		"name skills-cli or skillshare; to import a skill by name, run 'agentx skill import "+shellWord(sanitised(arg))+"'")
}

// toolArg is the value of --from that names tool, for the commands a
// preview suggests.
func toolArg(tool string) string {
	for arg, t := range adoptTools {
		if t == tool {
			return arg
		}
	}
	return ""
}

func newSkillImportCommand(inv *invocation) *cobra.Command {
	var sel adoptSelection
	var from string
	cmd := &cobra.Command{
		Use:         "import [<name>...] [--from skills-cli|skillshare]",
		Short:       "Import skills another tool installed, and unmanaged skills",
		Annotations: map[string]string{annotationSection: "skills", annotationSynopsis: "[<name>...]"},
		Example: examples(
			"agentx skill import", "preview what would be imported; nothing is written",
			"agentx skill import --all", "import every unmanaged skill",
			"agentx skill import --from skillshare --all", "import every skill Skillshare installed",
			"agentx skill import pdf xlsx", "import two skills by name",
			"agentx skill import notes", "import an unmanaged skill no tool installed",
		),
		Long: "Import the unmanaged skills on this machine, so that agentx manages them: the\n" +
			"skills the Vercel skills CLI or Skillshare installed, and those no tool installed.\n" +
			"Run it with no arguments to see what it would import; nothing is written and the\n" +
			"other tool's files are never touched. With --from skills-cli or --from\n" +
			"skillshare, look at the skills of that tool alone.\n\n" +
			"Importing a skill installs it from its source, as 'agentx skill add' does, at\n" +
			"the version the source holds now, and places it in every enabled configuration.\n" +
			"Whatever the library holds under the skill's name gives way to that version, so\n" +
			"an edit made to the other tool's copy is gone. A skill installed from a place\n" +
			"agentx cannot install from, such as a local folder, is imported with its files\n" +
			"as they are: it becomes a hub skill and gets no updates.\n\n" +
			"An unmanaged skill no tool installed is imported too, unless --from is given:\n" +
			"one the library holds, or a folder in an agent client's own skills directory.\n" +
			"It is imported with its files as they are and becomes a hub skill: a\n" +
			"branch of its own, skills/<name>, in the store, checked out as a\n" +
			"worktree in agentx home, and a symlink to it in the library. A folder in a\n" +
			"client's skills directory moves there and a symlink to the library takes its\n" +
			"place, so every client that reads the library sees the skill too.\n\n" +
			"A skill in the library whose folder in an agent client's skills directory holds\n" +
			"other files is not imported; --force replaces that folder with the imported\n" +
			"skill.\n\n" +
			"--all imports every skill that can be imported, and lists the ones that cannot,\n" +
			"each with what to do about it.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("from") {
				tool, err := adoptTool(from)
				if err != nil {
					return err
				}
				sel.tool = tool
			}
			sel.names = args
			return inv.adopt(cmd.Context(), sel)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "read the install records of this tool alone: skills-cli or skillshare")
	cmd.Flags().BoolVar(&sel.all, "all", false, "import every unmanaged skill that can be imported; with --from, every skill that tool installed")
	cmd.Flags().BoolVar(&sel.force, "force", false, "with a skill named, replace an agent's folder of the skill that holds other files with the imported skill; its files are lost")
	return cmd
}

// adopting reports whether the run was asked to change anything.
func (s adoptSelection) adopting() bool { return s.all || len(s.names) > 0 }

// check refuses a combination of flags and arguments that asks for two
// things at once.
func (s adoptSelection) check() error {
	if s.all && len(s.names) > 0 {
		return fail(exitUsage, "--all and a skill name cannot both be given",
			"--all imports every skill that can be imported; drop it to import the ones you name")
	}
	if s.force && len(s.names) == 0 {
		return fail(exitUsage, "--force needs a skill name",
			"name the skills whose agents' folders --force may replace")
	}
	return nil
}

// adopt reads what the other tools installed and reports, or adopts, the
// skills they recorded, see adoptChosen. A run that names no tool also
// covers the unmanaged skills no tool recorded, see looseSkills: the
// preview lists them and --all adopts them with the recorded ones. With
// --all, a skill that cannot be adopted is listed and fails nothing, see
// adoptChosen; one named as an argument is the run's to answer for. A skill
// named as an argument that no record covers is one of those, adopted as a
// hub skill with its files as they are, see planAdoption; every one named
// is planned before anything changes and made first, so that the answer of
// the batch, which may adopt some and refuse others, is the run's.
//
// Without a flag it is a preview and writes nothing at all: it takes no
// lock, creates neither agentx home nor the store, and reaches for
// no network. The other tools' files are never written, on any path.
func (inv *invocation) adopt(ctx context.Context, sel adoptSelection) error {
	if err := sel.check(); err != nil {
		return err
	}
	inv.adoptForce = sel.force
	records, err := inv.readRecords(sel.tool)
	if err != nil {
		return err
	}
	lineage, err := inv.lineageRecords(ctx)
	if err != nil {
		return err
	}
	cands, err := inv.adoptCandidates(records, lineage)
	if err != nil {
		return err
	}
	var loose []looseSkill
	if sel.tool == "" && (sel.all || !sel.adopting()) {
		if loose, err = inv.looseSkills(cands, lineage); err != nil {
			return err
		}
	}
	if !sel.adopting() {
		inv.previewAdoption(cands, loose, len(records), sel.tool)
		return nil
	}
	chosen, own, err := inv.chooseAdoptions(cands, records, sel)
	if err != nil {
		return err
	}
	var plans []*forking
	for _, name := range own {
		fk, err := inv.planAdoption(ctx, name, notAdoptable(name, records))
		if err != nil {
			return err
		}
		plans = append(plans, fk)
	}
	var summaries []string
	for _, fk := range plans {
		if err := inv.makeFork(ctx, fk); err != nil {
			return err
		}
		summaries = append(summaries, inv.summary)
	}
	if len(chosen) > 0 || len(plans) == 0 {
		if err := inv.adoptChosen(ctx, chosen, loose, sel.all); err != nil {
			return err
		}
		summaries = append(summaries, inv.summary)
	}
	inv.summary = strings.Join(summaries, "; ")
	return nil
}

// record is one skill another tool installed, as that tool recorded it:
// what an install from its source needs, and where that tool put it.
type record struct {
	tool    string
	name    string   // the library directory name
	file    string   // the file the record was read from
	dir     string   // the directory that tool installed the skill into; "" for the library entry, built once the name is known safe
	links   string   // the folder that tool keeps skills in and links into clients' folders, "" for a tool that links nothing
	urls    []string // the forms the record names its source in, tried in order
	ref     string   // the branch or tag it was installed from, "" for the default branch
	subpath string   // the skill's directory inside the source, "" for its root
	foreign bool     // it was installed from somewhere that is no git repository, which agentx cannot install from
	tracked bool     // it is a repository Skillshare tracks as a whole, with skills inside it, and no skill itself
}

// readRecords reads the install records of tool, or of every tool when it
// is "", in name order. A name both tools record is one warning, and the
// Vercel skills CLI's record is the one used: its directory is the library
// entry itself.
func (inv *invocation) readRecords(tool string) ([]record, error) {
	var records []record
	if tool != toolSkillshare {
		entries, warnings, err := vercel.Read(vercel.LockPaths(inv.env, inv.dirs.User))
		for _, w := range warnings {
			inv.out.warn(w)
		}
		if err != nil {
			return nil, recordFailure(err)
		}
		for _, e := range entries {
			records = append(records, vercelRecord(e))
		}
	}
	if tool == toolVercel {
		return records, nil
	}
	cfg, err := inv.skillshareConfig()
	if err != nil {
		return nil, recordFailure(err)
	}
	skills := cfg.Skills
	stored, warnings, err := skillshare.Read(skills)
	for _, w := range warnings {
		inv.out.warn(w)
	}
	if err != nil {
		return nil, recordFailure(err)
	}
	byName := map[string]record{}
	for _, r := range records {
		byName[r.name] = r
	}
	for _, e := range stored {
		r, warning := skillshareRecord(skills, e)
		if warning != "" {
			inv.out.warn(warning)
		}
		if r.name == "" {
			continue
		}
		if have, ok := byName[r.name]; ok {
			inv.out.warn(fmt.Sprintf("%s is recorded in %s and in %s; the record in %s is the one used", sanitised(r.name), have.file, r.file, have.file))
			continue
		}
		records = append(records, r)
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].name < records[j].name })
	return records, nil
}

// skillshareConfig reads Skillshare's configuration on this machine.
func (inv *invocation) skillshareConfig() (skillshare.Config, error) {
	return skillshare.ReadConfig(skillshare.Locate(inv.env, inv.dirs.Config), inv.dirs.User)
}

// recordFailure answers a record file that is not one: the same exit code
// an unreadable settings file has, since both are a file agentx reads whole
// and can make nothing of.
func recordFailure(err error) error {
	if errors.Is(err, vercel.ErrLock) || errors.Is(err, skillshare.ErrStore) {
		return fail(exitInternal, err.Error(), "fix the file or move it aside")
	}
	return fail(exitInternal, err.Error(), "check that the file is readable")
}

// vercelRecord is the record of one entry of the Vercel skills lock file.
// That tool installs into the library, so the directory is the library
// entry. The URL the entry was installed from is read first and its
// normalised identifier second, so that a URL agentx does not accept, a
// GitHub blob page among them, still leaves owner/repo to fall back on.
func vercelRecord(e vercel.Entry) record {
	r := record{tool: toolVercel, name: e.Name, file: e.File, ref: e.Ref, subpath: e.Subpath, urls: nonEmpty(e.SourceURL, e.Source)}
	switch e.SourceType {
	case "github", "gitlab", "git", "":
	default:
		r.foreign = true
	}
	return r
}

// skillshareRecord is the record of one entry of Skillshare's store, whose
// directory is below the folder Skillshare installs into. An agent is no
// skill and is passed over without a word; a key that would reach outside
// that folder is left out with the warning returned.
func skillshareRecord(skills string, e skillshare.Entry) (record, string) {
	if e.Kind == "agent" {
		return record{}, ""
	}
	if err := source.CheckPath(e.Name); err != nil {
		return record{}, fmt.Sprintf("%q in %s is not a directory of %s and is left out", sanitised(e.Name), e.File, skills)
	}
	r := record{
		tool: toolSkillshare, name: skillshare.LibraryName(e.Name), file: e.File,
		dir: filepath.Join(skills, filepath.FromSlash(e.Name)), links: skills,
		ref: e.Branch, subpath: e.Subdir, urls: nonEmpty(e.RepoURL, e.Source),
	}
	switch kind := strings.TrimSuffix(e.Type, "-subdir"); {
	case e.Tracked:
		r.tracked = true
	case kind == "github", kind == "git-https", kind == "git-ssh", kind == "":
	default:
		r.foreign = true
	}
	return r, ""
}

func nonEmpty(values ...string) []string {
	var kept []string
	for _, v := range values {
		if v != "" {
			kept = append(kept, v)
		}
	}
	return kept
}

// candidate is one record whose directory exists, with what agentx makes
// of it.
type candidate struct {
	rec    record
	src    source.Source // the source the record names, empty when it names none agentx can use
	entry  home.Source   // the settings entry of that source, when added
	added  bool          // the settings already hold that source
	state  string
	reason string
	fail   *failure // the refusal, which is how a run asked to adopt it answers
	// asItIs says the record names nowhere agentx can install the skill
	// from, and this machine holds a folder of it to keep: adopting it
	// makes that folder a hub skill, see planAdoption.
	asItIs bool
}

// refuse records why the candidate cannot be adopted.
func (c *candidate) refuse(f *failure) {
	c.state, c.reason, c.fail = adoptRefused, f.message, f
}

// adoptCandidates turns the records into candidates: one per record whose
// directory exists, in name order. A record whose directory is gone is no
// candidate at all, since the user removed what that tool installed.
func (inv *invocation) adoptCandidates(records []record, lineage map[string]lineage.Record) ([]*candidate, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	var cands []*candidate
	for _, r := range records {
		if why := nameRefusal(r.name); why != "" {
			// The name would have to become a library directory and a
			// branch, and it is refused in the words of whichever cannot
			// hold it. Nothing is looked up under it: a name that walks out
			// of the library may not be joined to a path.
			inv.out.warn(fmt.Sprintf("%q in %s %s and is left out", sanitised(r.name), r.file, why))
			continue
		}
		if r.dir == "" {
			r.dir = inv.libraryPath(r.name)
		}
		exists, err := entryExists(r.dir)
		if err != nil {
			inv.out.warn(r.dir + " could not be read and is left out: " + err.Error())
			continue
		}
		if !exists {
			continue // the record names it, this machine no longer has it
		}
		c := &candidate{rec: r, state: adoptCandidate}
		inv.judge(c, lineage, s)
		cands = append(cands, c)
	}
	return cands, nil
}

// judge decides what can be done with one candidate from what is on this
// machine alone: what the store already knows about the name, what
// the record rules out, and whether it names a source agentx can fetch. A
// name that is a hub skill, or a managed skill the
// library holds, is managed already. A record that names no source agentx
// can install from leaves the folder this machine holds of the skill to be
// adopted as it is, see judgeAsItIs.
func (inv *invocation) judge(c *candidate, records map[string]lineage.Record, s home.Settings) {
	rec, known := records[c.rec.name]
	if known && (rec.Kind == lineage.KindFork || isRealDirectory(inv.libraryPath(c.rec.name))) {
		// Left alone, and still shown with the source the record names
		// when it names one agentx can read.
		c.state, c.reason = adoptManaged, "agentx already manages it"
		if src, f := adoptSource(c.rec); f == nil && !c.rec.foreign && !c.rec.tracked {
			src.Stripped = false
			c.src = src
		}
		return
	}
	if c.rec.foreign || c.rec.tracked {
		inv.judgeAsItIs(c, known)
		return
	}
	src, f := adoptSource(c.rec)
	if f != nil {
		inv.judgeAsItIs(c, known)
		return
	}
	if src.Stripped {
		inv.out.warn("the user or token in the URL of " + sanitised(c.rec.name) + " was dropped: agentx never stores credentials; put them in a git credential helper (git config credential.helper) or an SSH key")
		src.Stripped = false
	}
	if !lineage.ValidPath(c.rec.subpath) {
		// A directory a trailer cannot carry names nothing agentx can install.
		inv.judgeAsItIs(c, known)
		return
	}
	c.src = src
	if i := s.FindSource(src.URL); i >= 0 {
		c.added, c.entry = true, s.Sources[i]
	} else {
		c.reason = reasonNotAdded
	}
	if known {
		// Managed, but the library no longer holds it as a directory: the
		// other tool took it back, or the user removed it. Adopting again
		// installs it again, and moves the branch when the source moved on.
		c.reason = "agentx manages it but the library no longer holds it; importing installs it again"
	}
}

// judgeAsItIs judges a candidate whose record names nowhere agentx can
// install the skill from: one this machine holds a folder of is adopted
// as it is, and any other is refused for what is in the way of that, see
// asItIsRefusal. managed says an import branch holds the name.
func (inv *invocation) judgeAsItIs(c *candidate, managed bool) {
	reason, f := inv.asItIsRefusal(c.rec, managed)
	if f == nil {
		c.asItIs = true
		return
	}
	c.refuse(f)
	c.reason = reason
}

// reasonNotAdded is the reason of a candidate whose source the settings do
// not hold. The adoption event carries it; the preview leaves it out, since
// adding the source is part of every adoption.
const reasonNotAdded = "its source is not added yet; importing adds and fetches it"

// isRealDirectory reports whether path is a real directory: what the library
// holds for a managed skill, as against nothing or another tool's link.
func isRealDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// entryExists says whether anything is at path, a symlink included: what
// another tool installed is there whatever shape it took.
func entryExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	}
	return true, nil
}

// adoptSource is the source a record names, canonicalised the way every
// source agentx stores is. A user or token in it is dropped before the URL
// is used anywhere, as it is for 'agentx source add'.
func adoptSource(r record) (source.Source, *failure) {
	for _, raw := range r.urls {
		src, err := source.Parse(raw)
		if err != nil {
			continue
		}
		// The subpath and the ref of the record decide, not the ones a
		// tree URL happens to carry: the record says what was installed,
		// and the URL may name the whole repository.
		ref := src.Ref
		if r.ref != "" && source.ValidRef(r.ref) {
			ref = r.ref
		}
		src.Subpath, src.Ref = "", ref
		return src, nil
	}
	return source.Source{}, refuse(exitRefused,
		fmt.Sprintf("%s names no source agentx can read", r.name),
		"accepted forms: "+source.Forms)
}

// chooseAdoptions picks what a run covers: every candidate with --all, or
// exactly the names given as arguments. A name a candidate holds is chosen. Any
// other name is own: an unmanaged skill no record names, looked for once
// every name is sorted. With a tool named, only that tool's records are
// read, and a name none of them holds fails the run before anything is
// fetched, the way a --skill that names no skill of a source does. Both
// are returned in name order, whatever order the flags were given in.
func (inv *invocation) chooseAdoptions(cands []*candidate, records []record, sel adoptSelection) (chosen []*candidate, own []string, err error) {
	if sel.all {
		return cands, nil, nil
	}
	byName := map[string]*candidate{}
	for _, c := range cands {
		byName[c.rec.name] = c
	}
	seen := map[string]bool{}
	for _, name := range sel.names {
		if seen[name] {
			continue // a name given twice adopts one skill
		}
		seen[name] = true
		c, ok := byName[name]
		switch {
		case ok:
			chosen = append(chosen, c)
		case sel.tool == "":
			own = append(own, name)
		default:
			return nil, nil, noAdoptionNamed(name, cands, records, sel.tool)
		}
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].rec.name < chosen[j].rec.name })
	sort.Strings(own)
	return chosen, own, nil
}

// goneRecord answers a name a record names for a directory that is
// gone, nil when no record names it.
func goneRecord(name string, records []record) error {
	for _, r := range records {
		if r.name == name {
			return fail(exitNotFound, fmt.Sprintf("%s says %q was installed, and this machine no longer holds it", r.file, name),
				"run 'agentx skill import' to see what can be imported")
		}
	}
	return nil
}

// notAdoptable answers a name that names nothing this machine holds to
// adopt: a record may name it for a directory that is gone, or nothing may
// name or hold it at all, which are different things to fix.
func notAdoptable(name string, records []record) error {
	if err := goneRecord(name, records); err != nil {
		return err
	}
	return fail(exitNotFound, fmt.Sprintf("no skill called %q can be imported: no record names it, and neither the library nor an agent client's skills directory holds it", sanitised(name)),
		"run 'agentx skill import' to see what another tool installed, and 'agentx skill list' to see the library")
}

// noAdoptionNamed answers a name the records of the tool named do not
// place: none may name it, or one may name it for a directory that is
// gone, which are different things to fix.
func noAdoptionNamed(name string, cands []*candidate, records []record, tool string) error {
	if err := goneRecord(name, records); err != nil {
		return err
	}
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		names = append(names, sanitised(c.rec.name))
	}
	hint := "run 'agentx skill import' to see what can be imported"
	if len(names) > 0 {
		if len(names) > namesInAHint {
			names = append(names[:namesInAHint], "...")
		}
		hint = "the records name: " + strings.Join(names, ", ")
	}
	return fail(exitNotFound, fmt.Sprintf("no skill called %q was installed by %s", sanitised(name), toolNames(toolSet(tool), "or")), hint)
}

// unadopted is one skill a run that adopts every skill could not adopt: a
// row of the list it ends with.
type unadopted struct {
	name   string
	tool   string // the tool that installed it, "" for one no tool recorded
	reason string // what it is and what to do, as a preview says it of a skill it cannot adopt
}

// adoptChosen adopts every chosen skill, and every one of loose that can
// be adopted, the unmanaged skills no tool recorded that --all covers. One
// whose record names a source is installed from it, as skill add installs
// one: the source is added and fetched when the settings do not hold it,
// the version at its pin is read out of the store, the import
// commits are written, and the library directories, the import branches
// and the placements land as one journaled mutation. One whose record
// names none, and one no tool recorded, is adopted with its files as they
// are, see adoptAsItIs, each in a mutation of its own, in name order,
// before the installs. A skill agentx already manages is not acted on.
//
// A skill that cannot be adopted costs the run that skill and no other.
// One the run was asked for by name fails it. With all, the run was asked
// for whatever can be adopted, so a skill that cannot be, as it was judged
// or as a refusal found out while it was adopted, fails nothing: it is
// listed once the run ends, see reportUnadopted. Any other failure, a
// source that cannot be fetched among them, is the run's either way.
func (inv *invocation) adoptChosen(ctx context.Context, covered []*candidate, loose []looseSkill, all bool) error {
	var ready []*candidate
	var asItIs []string
	var judged []*candidate // the ones judged not adoptable, which a run by name answers for
	var left []unadopted
	tools := map[string]string{}
	for _, c := range covered {
		tools[c.rec.name] = c.rec.tool
		switch {
		case c.state == adoptManaged:
		case c.fail != nil && all:
			left = append(left, unadopted{c.rec.name, c.rec.tool, c.reason})
		case c.fail != nil:
			judged = append(judged, c)
		case c.asItIs:
			asItIs = append(asItIs, c.rec.name)
		default:
			ready = append(ready, c)
		}
	}
	for _, l := range loose {
		if reason := l.reason(); reason != "" {
			left = append(left, unadopted{name: l.name, reason: reason})
		} else {
			asItIs = append(asItIs, l.name)
		}
	}
	sort.Strings(asItIs)
	mixed := len(toolsOf(covered)) > 1
	selected := len(ready) + len(asItIs) + len(judged)
	if selected == 0 {
		inv.reportAdopted(nil, covered, nil)
		if len(left) == 0 {
			inv.out.print("Already imported.")
			inv.summary = "already imported"
			return nil
		}
		inv.reportUnadopted(left, 0, mixed)
		return nil
	}
	b := newBatch(inv, selected)
	b.verb, b.mixed = "imported", "run 'agentx skill import' to see what is left, and import the rest one at a time"
	b.listsRefused = all
	inv.publishHintOnce = all
	for _, c := range judged {
		b.drop(c.rec.name, stepsPerSkill, c.fail)
	}
	kept, summaries, err := inv.adoptAsItIs(ctx, b, asItIs)
	if err != nil {
		return err
	}
	installed, err := inv.adoptFromSources(ctx, b, ready)
	if err != nil {
		return err
	}
	if installed {
		summaries = append(summaries, inv.summary)
	}
	inv.summary = strings.Join(summaries, "; ")
	if inv.publishHintDue {
		out := inv.out
		out.print("Imported skills stay on this machine until you publish them: run ", out.paint(label, "'agentx source add <url> --hub'"),
			", then ", out.paint(label, "'agentx skill publish <name>'"), ".")
	}
	inv.reportAdopted(b, covered, kept)
	adopted := len(b.done) + len(kept)
	if all {
		// A refusal is a skill that cannot be adopted, which is listed; what
		// is left is what went wrong.
		var failed []brokenSkill
		for _, broken := range b.broken {
			if broken.fail.status != exitRefused {
				failed = append(failed, broken)
				continue
			}
			left = append(left, unadopted{broken.subject, tools[broken.subject], adoptRow(broken.fail)})
			selected--
		}
		b.broken = failed
		if len(left) > 0 {
			inv.reportUnadopted(left, adopted, mixed)
		}
	}
	if f := b.refusals.failure(selected, adopted); f != nil {
		return f
	}
	return nil // a typed nil would be an error the caller cannot see through
}

// adoptRow is how the list a run that adopts every skill ends with shows
// the refusal f: its own row, see failure.row, and for one that has none,
// a refusal of an install among them, its message and what to do.
func adoptRow(f *failure) string {
	switch {
	case f.row != "":
		return f.row
	case f.hint == "":
		return f.message
	}
	return f.message + ". To import it, " + f.hint
}

// toolsOf are the tools whose records cands are.
func toolsOf(cands []*candidate) map[string]bool {
	tools := map[string]bool{}
	for _, c := range cands {
		tools[c.rec.tool] = true
	}
	return tools
}

// reportUnadopted ends a run that adopts every skill with the ones it
// could not adopt, in name order: a list in the shape of the preview's,
// each with what it is and what to do, then a line that counts them and
// the adopted skills, which is the result's summary too. In JSON mode each
// is a warning. mixed says the records of both tools were covered, which
// shows the tool of each.
func (inv *invocation) reportUnadopted(left []unadopted, adopted int, mixed bool) {
	out := inv.out
	sort.SliceStable(left, func(i, j int) bool { return left[i].name < left[j].name })
	if adopted > 0 {
		out.print()
	}
	out.print(out.paint(heading, fmt.Sprintf("Could not import (%d)", len(left))))
	t := &table{wrap: out.columns(out.stdout)}
	for _, u := range left {
		t.add(refusedRow(u.name, u.tool, u.reason, mixed)...)
		if out.json {
			out.warn(sanitised(u.name) + " could not be imported: " + u.reason)
		}
	}
	out.render(t, "  ")
	inv.summary = "imported " + plural(adopted, "skill")
	if adopted == 0 {
		inv.summary = "no skills imported"
	}
	inv.summary += fmt.Sprintf("; %d could not be imported", len(left))
	out.print()
	out.print(strings.ToUpper(inv.summary[:1]), inv.summary[1:], ".")
}

// adoptAsItIs adopts each skill of names with its files as they are, see
// planAdoption: the folder this machine holds of it becomes a hub skill.
// Each is reported as it lands. One that is refused is dropped from the
// batch with its refusal, and takes no step of the batch's budget, which
// counts the steps of an install. It returns the names that landed and what
// the result says of each.
func (inv *invocation) adoptAsItIs(ctx context.Context, b *batch, names []string) (kept map[string]bool, summaries []string, err error) {
	kept = map[string]bool{}
	for _, name := range names {
		fk, err := inv.planAdoption(ctx, name, adoptChanged(name))
		if err == nil {
			err = inv.makeFork(ctx, fk)
		}
		var f *failure
		switch {
		case err == nil:
			b.total -= stepsPerSkill
			kept[name] = true
			summaries = append(summaries, inv.summary)
		case errors.As(err, &f):
			b.drop(name, stepsPerSkill, f)
		default:
			return nil, nil, err
		}
	}
	return kept, summaries, nil
}

// adoptFromSources installs each of ready from the source its record
// names, and reports whether any landed, which the summary then names.
func (inv *invocation) adoptFromSources(ctx context.Context, b *batch, ready []*candidate) (bool, error) {
	targets, err := inv.placementTargets(nil)
	if err != nil {
		return false, err
	}
	versions, gitDir, err := inv.adoptVersions(ctx, b, ready)
	if err != nil {
		return false, err
	}
	if len(versions) == 0 {
		b.total-- // the rescan that ends an install
		return false, nil
	}
	run := lineage.NewRun()
	if err := inv.writeImports(ctx, b, gitDir, run, versions); err != nil {
		return false, err
	}
	dones, journaled, err := inv.install(ctx, b, gitDir, versions, targets, false)
	if err != nil {
		if !journaled {
			inv.dropImporting(ctx, gitDir, run, len(versions))
		}
		return false, err
	}
	inv.dropImporting(ctx, gitDir, run, len(versions))
	if len(dones) == 0 {
		b.total--
		return false, nil
	}
	if err := inv.reportInstalled(ctx, b, dones); err != nil {
		return false, err
	}
	inv.summary = "imported " + strings.TrimPrefix(strings.TrimPrefix(inv.summary, "installed "), "imported ")
	return true, nil
}

// adoptVersions reads the version each candidate is adopted at: the skill
// at its subpath in its source, at the pin this machine holds, the source
// added and fetched first when the settings do not hold it. A source that
// cannot be added or read costs its own skills and no others. It returns
// the versions, filled, and the store they were read from.
func (inv *invocation) adoptVersions(ctx context.Context, b *batch, ready []*candidate) ([]*imported, string, error) {
	if len(ready) == 0 {
		return nil, "", nil
	}
	// The lock is taken and released before the network, as an install
	// takes it: it recovers an unfinished journal and refuses a run that
	// could not finish anyway.
	if err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error { return nil }); err != nil {
		return nil, "", err
	}
	var urls []string
	bySource := map[string][]*candidate{}
	for _, c := range ready {
		if bySource[c.src.URL] == nil {
			urls = append(urls, c.src.URL)
		}
		bySource[c.src.URL] = append(bySource[c.src.URL], c)
	}
	var versions []*imported
	var gitDir string
	for _, url := range urls {
		cands := bySource[url]
		dropAll := func(f *failure) {
			for _, c := range cands {
				b.drop(c.rec.name, stepsPerSkill, f)
			}
		}
		listing, fetched, f, err := inv.adoptListing(ctx, b, url, cands)
		if err != nil {
			return nil, "", err
		}
		if f != nil {
			dropAll(f)
			continue
		}
		if gitDir == "" {
			if gitDir, _, err = gitx.CheckStore(ctx, inv.git, inv.dirs.Home); err != nil {
				return nil, "", storeFailure(err)
			}
		}
		var skills []source.Skill
		var owners []*candidate
		for _, c := range cands {
			sk, ok := skillAt(listing, c.rec.subpath)
			if !ok {
				b.drop(c.rec.name, stepsPerSkill, refuse(exitNotFound, fmt.Sprintf("%s holds no skill at %s now", url, dirOrRoot(c.rec.subpath)),
					"the source moved or removed it; leave the skill unmanaged, or install another with 'agentx skill add'"))
				continue
			}
			// The library keeps the name the skill was installed under,
			// whatever its SKILL.md calls it, as an update keeps it.
			sk.Name = c.rec.name
			skills = append(skills, sk)
			owners = append(owners, c)
		}
		if len(skills) == 0 {
			continue
		}
		src := source.Source{URL: url}
		read, err := inv.readVersions(ctx, b, gitDir, src, listing.Commit, skills)
		if err != nil {
			return nil, "", err
		}
		for i, v := range read {
			v.fetched, v.replace, v.links = fetched, true, owners[i].rec.links
		}
		versions = append(versions, read...)
	}
	return versions, gitDir, nil
}

// adoptListing is what one source holds at its pin: the listing of the
// fetch that adds it when the settings do not hold it, else the listing of
// the store as it was last fetched. A source the run cannot read is
// the failure returned, which costs its skills; an error is the run's.
func (inv *invocation) adoptListing(ctx context.Context, b *batch, url string, cands []*candidate) (listing source.Listing, fetched string, f *failure, err error) {
	if !cands[0].added {
		pin, recorded := adoptPin(cands)
		if len(recorded) > 1 {
			// One source has one pin on a machine, and it governs every
			// later install from it, so records of two refs of it cannot
			// be answered in silence.
			inv.out.warn(fmt.Sprintf("%s was installed at more than one ref (%s); it is added pinned to %s. To change the pin, run 'agentx source add %s#<ref>' and import again",
				url, strings.Join(pinNames(recorded), ", "), pinName(pin), url))
		}
		b.total++ // the fetch step of the source
		listing, entry, err := inv.addSource(ctx, source.Source{URL: url, Ref: pin})
		if err != nil {
			b.total--
			return listing, "", failureOf(err), nil
		}
		b.step(phaseFetch, url)
		return listing, entry.LastFetched, nil, nil
	}
	entry := cands[0].entry
	pinned := source.Source{URL: url, Ref: entry.Pin}
	gitDir, exists, err := gitx.CheckStore(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return listing, "", nil, storeFailure(err)
	}
	if !exists {
		return listing, "", failureOf(sourceFailure(source.NotFetched(url), pinned)), nil
	}
	if listing, err = source.List(ctx, inv.git, gitDir, source.Source{URL: url}); err != nil {
		return listing, "", failureOf(sourceFailure(err, pinned)), nil
	}
	return listing, entry.LastFetched, nil, nil
}

// skillAt is the skill the listing holds at subpath.
func skillAt(listing source.Listing, subpath string) (source.Skill, bool) {
	for _, sk := range listing.Skills {
		if sk.Subpath == subpath {
			return sk, true
		}
	}
	return source.Skill{}, false
}

// dirOrRoot names a subpath for a message, the repository root included.
func dirOrRoot(subpath string) string {
	if subpath == "" {
		return "its root"
	}
	return subpath
}

// adoptPin is the ref a source is added at and every ref the candidates
// recorded for it, in name order. A tool records a ref per skill and a
// machine holds one pin per source, so the first candidate in name order
// decides and the caller says so when they disagree.
func adoptPin(cands []*candidate) (pin string, recorded []string) {
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c.src.Ref] {
			continue
		}
		seen[c.src.Ref] = true
		recorded = append(recorded, c.src.Ref)
	}
	if len(recorded) > 0 {
		pin = recorded[0]
	}
	return pin, recorded
}

// pinName is how a pin reads in a message; another tool wrote it, so it is
// sanitised like everything else that came from there.
func pinName(ref string) string {
	if ref == "" {
		return "the branch it follows"
	}
	return sanitised(ref)
}

func pinNames(refs []string) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, pinName(ref))
	}
	return names
}

// reportAdopted emits one adoption event per candidate the run covered, in
// name order, as the run left it: adopted for a skill that landed, one of
// kept, adopted as it is, included; refused with its reason for one the
// run gave up on, which is the reason the candidate was judged with when
// that is why; managed for one it never acted on. The skills that landed
// were reported as an install or an adoption as it is reports them already.
func (inv *invocation) reportAdopted(b *batch, covered []*candidate, kept map[string]bool) {
	landed := map[string]bool{}
	refused := map[string]*failure{}
	if b != nil {
		for _, done := range b.done {
			landed[done.v.name] = true
		}
		for _, broken := range b.broken {
			refused[broken.subject] = broken.fail
		}
	}
	for _, c := range covered {
		switch f := refused[c.rec.name]; {
		case landed[c.rec.name], kept[c.rec.name]:
			c.state, c.reason = adoptAdopted, ""
		case f != nil && f != c.fail:
			c.refuse(f)
		}
		inv.out.emit(c.eventOf())
	}
}

// eventOf is the adoption event of one candidate.
func (c *candidate) eventOf() adoptionEvent {
	ev := adoptionEvent{event: newEvent("adoption"), Name: c.rec.name, Tool: c.rec.tool, File: c.rec.file, State: c.state, Reason: c.reason}
	if c.src.URL != "" {
		subpath := c.rec.subpath
		ev.Source, ev.Subpath = c.src.URL, &subpath
	}
	return ev
}

// previewAdoption reports what an adoption would do and changes nothing.
// The skills are grouped by what an adoption would do with them: the ones
// it would adopt first, as a list of names; then the ones it cannot, each
// with the reason; then the ones another tool installed that agentx
// manages already, as a list of names, since there is nothing to do about
// them. loose are the unmanaged skills no tool recorded, which are listed
// with the recorded ones and have no adoption event. The first line counts
// the unmanaged skills, the ones of the first two groups.
func (inv *invocation) previewAdoption(cands []*candidate, loose []looseSkill, records int, tool string) {
	out := inv.out
	var ready, managed []string
	var refused [][]cell
	for _, c := range cands {
		out.emit(c.eventOf())
	}
	mixed := len(toolsOf(cands)) > 1
	for _, c := range cands {
		switch c.state {
		case adoptCandidate:
			ready = append(ready, c.rec.name)
		case adoptRefused:
			refused = append(refused, refusedRow(c.rec.name, c.rec.tool, c.reason, mixed))
		case adoptManaged:
			managed = append(managed, c.rec.name)
		}
	}
	for _, l := range loose {
		if reason := l.reason(); reason != "" {
			refused = append(refused, refusedRow(l.name, "", reason, mixed))
		} else {
			ready = append(ready, l.name)
		}
	}
	sort.Strings(ready)
	sort.SliceStable(refused, func(i, j int) bool { return refused[i][0].text < refused[j][0].text })
	inv.summary = previewSummary(len(ready), len(refused), len(managed), records-len(cands))

	installed := ""
	if tool != "" {
		installed = " installed by " + toolNames(toolSet(tool), "or")
	}
	unmanaged := len(ready) + len(refused)
	if unmanaged > 0 {
		out.print("Found ", out.paint(heading, plural(unmanaged, "unmanaged skill")), installed, ".")
	}
	inv.printNames("Ready to import", ready, plain, unmanaged > 0)
	if len(refused) > 0 {
		out.print()
		out.print(out.paint(heading, fmt.Sprintf("Cannot import (%d)", len(refused))))
		t := &table{wrap: out.columns(out.stdout)}
		for _, row := range refused {
			t.add(row...)
		}
		out.render(t, "  ")
	}
	inv.printNames("Already managed by agentx", managed, muted, unmanaged > 0)

	if unmanaged > 0 || len(managed) > 0 {
		out.print()
	}
	switch n := len(ready); {
	case n > 0:
		all := "all " + plural(n, "skill")
		if n == 1 {
			all = "the one skill"
		}
		out.print("To import:")
		from := ""
		if tool != "" {
			from = " --from " + toolArg(tool)
		}
		t := &table{}
		t.add(cell{text: "agentx skill import" + from + " --all", style: label}, cell{text: all, style: muted})
		t.add(cell{text: "agentx skill import <name>" + from, style: label}, cell{text: "one skill", style: muted})
		out.render(t, "  ")
	case len(refused) > 0:
		out.print("Nothing to import.")
	case tool == "":
		out.print("Nothing to import: this machine has no unmanaged skill.")
	case len(managed) > 0:
		out.print("Nothing to import: agentx already manages every skill ", toolNames(toolSet(tool), "or"), " installed.")
	default:
		out.print("Nothing to import: no skill installed by ", toolNames(toolSet(tool), "or"), " is on this machine.")
	}
}

// printNames prints one group of a preview as its title, with how many it
// holds, and their names, separated by commas and wrapped to the terminal;
// nothing for a group that holds none. spaced says a line precedes it, to
// set it apart from.
func (inv *invocation) printNames(title string, names []string, st style, spaced bool) {
	if len(names) == 0 {
		return
	}
	out := inv.out
	if spaced {
		out.print()
	}
	out.print(out.paint(heading, fmt.Sprintf("%s (%d)", title, len(names))))
	shown := make([]string, len(names))
	for i, name := range names {
		shown[i] = sanitised(name)
	}
	for _, line := range wrapWords(strings.Join(shown, ", "), out.columns(out.stdout)-2) {
		out.print("  ", out.paint(st, line))
	}
}

// toolSet is the tools a run reads: the one named, or both.
func toolSet(tool string) map[string]bool {
	if tool != "" {
		return map[string]bool{tool: true}
	}
	return map[string]bool{toolVercel: true, toolSkillshare: true}
}

// toolNames names tools for a message, joined by conj.
func toolNames(tools map[string]bool, conj string) string {
	var names []string
	if tools[toolVercel] {
		names = append(names, "the Vercel skills CLI")
	}
	if tools[toolSkillshare] {
		names = append(names, "Skillshare")
	}
	return strings.Join(names, " "+conj+" ")
}

// refusedRow is one row of the skills a preview cannot adopt, or a run that
// adopts every skill could not, see reportUnadopted: the name and why. The tool that installed it,
// none for a skill no tool recorded, is shown only when the records of
// both tools are listed. The name is sanitised: it is text agentx did not
// write.
func refusedRow(name, tool, reason string, mixed bool) []cell {
	row := []cell{{text: sanitised(name), style: heading}}
	if mixed {
		row = append(row, cell{text: tool, style: muted})
	}
	return append(row, cell{text: reason, style: muted})
}

// previewSummary is what the result event says a preview found: how many
// unmanaged skills, the ones it would adopt and the ones it cannot, then
// the skills another tool installed that agentx manages already and the
// ones its records name that are no longer on this machine. Pure.
func previewSummary(ready, refused, managed, gone int) string {
	parts := []string{"no unmanaged skill"}
	if n := ready + refused; n > 0 {
		parts = []string{plural(n, "unmanaged skill")}
	}
	for _, what := range []struct {
		n    int
		text string
	}{
		{ready, "to import"},
		{refused, "that cannot be imported"},
		{managed, "already managed"},
		{gone, "no longer on this machine"},
	} {
		if what.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", what.n, what.text))
		}
	}
	if len(parts) == 1 && ready+refused == 0 {
		return "nothing to import"
	}
	return strings.Join(parts, ", ")
}
