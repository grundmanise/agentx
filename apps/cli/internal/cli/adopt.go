package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
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

// The reasons a record's skill cannot be adopted for, as an inventory names
// them, see adoptable: what asItIsRefusal found in the way of adopting a
// skill whose record names nowhere agentx can install it from.
const (
	refusedNameTaken   = "name_taken"    // agentx manages a skill of its name, whose library folder is missing
	refusedSymlink     = "symlink"       // this machine holds it as a symlink and no folder
	refusedFile        = "file"          // the library holds a file under its name
	refusedNoSkillFile = "no_skill_file" // its library folder holds no SKILL.md
	refusedTracked     = "tracked"       // it is a repository Skillshare tracks as a whole
	refusedUnplaced    = "unplaced"      // only that tool's own folder holds it, where no agent client looks
	refusedInvalidName = "invalid_name"  // its folder has a name agentx cannot give a skill
	refusedRepository  = "repository"    // its folder is a Git repository of its own
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
// but look, adopt the skills named, or adopt every one recorded, of the
// tools named or of both.
type adoptSelection struct {
	// tools are the tools --from named, whose records alone are read; none
	// for the records of both and the skills no tool recorded.
	tools map[string]bool
	names []string
	all   bool
	force bool // replace a client's folder of a named skill that holds other files
}

// reads are the tools whose records the run reads: the ones named, or both.
func (s adoptSelection) reads() map[string]bool {
	if len(s.tools) > 0 {
		return s.tools
	}
	return map[string]bool{toolVercel: true, toolSkillshare: true}
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

// fromTools is the --from of each of tools, in the order their records are
// read, for the commands a preview suggests: "" for a run that named none.
func fromTools(tools map[string]bool) string {
	flags := ""
	for _, arg := range []string{"skills-cli", "skillshare"} {
		if tools[adoptTools[arg]] {
			flags += " --from " + arg
		}
	}
	return flags
}

func newSkillImportCommand(inv *invocation) *cobra.Command {
	var sel adoptSelection
	var from []string
	cmd := &cobra.Command{
		Use:         "import [<name>...] [--from skills-cli|skillshare]...",
		Short:       "Import skills another tool installed, and unmanaged skills",
		Annotations: map[string]string{annotationSection: "skills", annotationSynopsis: "[<name>...]"},
		Example: examples(
			"agentx skill import", "preview what would be imported; nothing is written",
			"agentx skill import --all", "import every unmanaged skill",
			"agentx skill import --from skillshare --all", "import every skill Skillshare installed",
			"agentx skill import --from skills-cli --from skillshare --all", "import every skill either tool installed",
			"agentx skill import pdf xlsx", "import two skills by name",
			"agentx skill import notes", "import an unmanaged skill no tool installed",
		),
		Long: "Import the unmanaged skills on this machine, so that agentx manages them: the\n" +
			"skills the Vercel skills CLI or Skillshare installed, and those no tool installed.\n" +
			"Run it with no arguments to see what it would import; nothing is written and the\n" +
			"other tool's files are never touched. With --from skills-cli or --from\n" +
			"skillshare, look at the skills of that tool alone; give --from once for each\n" +
			"tool to look at the skills of both and at none that no tool installed.\n\n" +
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
			for _, arg := range from {
				tool, err := adoptTool(arg)
				if err != nil {
					return err
				}
				if sel.tools == nil {
					sel.tools = map[string]bool{}
				}
				sel.tools[tool] = true
			}
			sel.names = args
			return inv.adopt(cmd.Context(), sel)
		},
	}
	cmd.Flags().StringArrayVar(&from, "from", nil, "read the install records of this tool alone: skills-cli or skillshare; give it again to read those of both")
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
	records, err := inv.readRecords(sel.reads(), inv.out.warn)
	if err != nil {
		return err
	}
	lineage, err := inv.lineageRecords(ctx)
	if err != nil {
		return err
	}
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	cands := inv.adoptCandidates(records, lineage, s, inv.out.warn)
	var loose []looseSkill
	if len(sel.tools) == 0 && (sel.all || !sel.adopting()) {
		if loose, err = inv.looseSkills(cands, lineage); err != nil {
			return err
		}
	}
	if !sel.adopting() {
		inv.previewAdoption(cands, loose, len(records), sel.tools)
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

// readRecords reads the install records of tools, in name order. A name
// both tools record is one warning, and the Vercel skills CLI's record is
// the one used: its directory is the library entry itself. warn is told
// what the files say that a reader cannot use.
func (inv *invocation) readRecords(tools map[string]bool, warn func(string)) ([]record, error) {
	var records []record
	if tools[toolVercel] {
		entries, warnings, err := vercel.Read(vercel.LockPaths(inv.env, inv.dirs.User))
		for _, w := range warnings {
			warn(w)
		}
		if err != nil {
			return nil, recordFailure(err)
		}
		for _, e := range entries {
			records = append(records, vercelRecord(e))
		}
	}
	if !tools[toolSkillshare] {
		return records, nil
	}
	cfg, err := inv.skillshareConfig()
	if err != nil {
		return nil, recordFailure(err)
	}
	skills := cfg.Skills
	stored, warnings, err := skillshare.Read(skills)
	for _, w := range warnings {
		warn(w)
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
			warn(warning)
		}
		if r.name == "" {
			continue
		}
		if have, ok := byName[r.name]; ok {
			warn(fmt.Sprintf("%s is recorded in %s and in %s; the record in %s is the one used", sanitised(r.name), have.file, r.file, have.file))
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
	listed scan.RefusedAdoption // why it is refused, as an inventory lists it, see adoptable
	fail   *failure             // the refusal, which is how a run asked to adopt it answers
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
// candidate at all, since the user removed what that tool installed. They
// are judged against the lineage and the settings s, and warn is told of a
// record that is left out.
func (inv *invocation) adoptCandidates(records []record, lineage map[string]lineage.Record, s home.Settings, warn func(string)) []*candidate {
	sites := inv.siteFinder()
	var cands []*candidate
	for _, r := range records {
		if why := nameRefusal(r.name); why != "" {
			// The name would have to become a library directory and a
			// branch, and it is refused in the words of whichever cannot
			// hold it. Nothing is looked up under it: a name that walks out
			// of the library may not be joined to a path.
			warn(fmt.Sprintf("%q in %s %s and is left out", sanitised(r.name), r.file, why))
			continue
		}
		if r.dir == "" {
			r.dir = inv.libraryPath(r.name)
		}
		exists, err := entryExists(r.dir)
		if err != nil {
			warn(r.dir + " could not be read and is left out: " + err.Error())
			continue
		}
		if !exists {
			continue // the record names it, this machine no longer has it
		}
		c := &candidate{rec: r, state: adoptCandidate}
		inv.judge(c, lineage, s, sites, warn)
		cands = append(cands, c)
	}
	return cands
}

// judge decides what can be done with one candidate from what is on this
// machine alone: what the store already knows about the name, what
// the record rules out, and whether it names a source agentx can fetch. A
// name that is a hub skill, or a managed skill the
// library holds, is managed already. A record that names no source agentx
// can install from leaves the folder this machine holds of the skill to be
// adopted as it is, see judgeAsItIs, which sites finds the folders for.
func (inv *invocation) judge(c *candidate, records map[string]lineage.Record, s home.Settings, sites siteFinder, warn func(string)) {
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
		inv.judgeAsItIs(c, known, sites)
		return
	}
	src, f := adoptSource(c.rec)
	if f != nil {
		inv.judgeAsItIs(c, known, sites)
		return
	}
	if src.Stripped {
		warn("the user or token in the URL of " + sanitised(c.rec.name) + " was dropped: agentx never stores credentials; put them in a git credential helper (git config credential.helper) or an SSH key")
		src.Stripped = false
	}
	if !lineage.ValidPath(c.rec.subpath) {
		// A directory a trailer cannot carry names nothing agentx can install.
		inv.judgeAsItIs(c, known, sites)
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
func (inv *invocation) judgeAsItIs(c *candidate, managed bool, sites siteFinder) {
	listed, reason, f := inv.asItIsRefusal(c.rec, managed, sites)
	if f == nil {
		c.asItIs = true
		return
	}
	c.refuse(f)
	c.reason, c.listed = reason, listed
}

// adoptable is what an inventory says of the skills the other tools
// installed: for each tool and each source its records name, the Vercel
// skills CLI first and then by source, the records that name no source
// agentx can install from before the others, the names an import can adopt,
// the ones agentx manages already and the ones an import cannot adopt, each
// with why. A record whose skill this machine no longer holds is in none.
// The records are judged as
// a preview judges them, see adoptCandidates, against the lineage and the
// settings the scan read under its lock: the record files and the machine
// are read, and no git runs. What a preview warns of in a record file is the
// preview's to say. A file that is no record file is one warning of the
// scan, and no skill of either tool is listed.
func (inv *invocation) adoptable(lineage map[string]lineage.Record, s home.Settings, warn func(string)) []scan.Adoptable {
	quiet := func(string) {}
	listed := []scan.Adoptable{}
	records, err := inv.readRecords(adoptSelection{}.reads(), quiet)
	if err != nil {
		warn(err.Error() + "; the skills another tool installed are not listed")
		return listed
	}
	for _, c := range inv.adoptCandidates(records, lineage, s, quiet) {
		i := slices.IndexFunc(listed, func(of scan.Adoptable) bool { return of.Tool == c.rec.tool && of.Source == c.src.URL })
		if i < 0 {
			i, listed = len(listed), append(listed, scan.Adoptable{Tool: c.rec.tool, Source: c.src.URL, Skills: []string{}})
		}
		switch of := &listed[i]; c.state {
		case adoptManaged:
			of.Managed = append(of.Managed, c.rec.name)
		case adoptRefused:
			of.Refused = append(of.Refused, c.listed)
		default:
			of.Skills = append(of.Skills, c.rec.name)
		}
	}
	slices.SortStableFunc(listed, func(a, b scan.Adoptable) int {
		return cmp.Or(cmp.Compare(toolOrder(a.Tool), toolOrder(b.Tool)), cmp.Compare(a.Source, b.Source))
	})
	return listed
}

// toolOrder is where a tool's skills come in an inventory: the Vercel
// skills CLI first.
func toolOrder(tool string) int {
	return slices.Index([]string{toolVercel, toolSkillshare}, tool)
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
// every name is sorted. With a tool named, only the records of the tools
// named are read, and a name none of them holds fails the run before anything is
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
		case len(sel.tools) == 0:
			own = append(own, name)
		default:
			return nil, nil, noAdoptionNamed(name, cands, records, sel.tools)
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

// noAdoptionNamed answers a name the records of the tools named do not
// place: none may name it, or one may name it for a directory that is
// gone, which are different things to fix.
func noAdoptionNamed(name string, cands []*candidate, records []record, tools map[string]bool) error {
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
	return fail(exitNotFound, fmt.Sprintf("no skill called %q was installed by %s", sanitised(name), toolNames(tools, "or")), hint)
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
// one: the source is fetched when the settings do not hold it, the version
// at its pin is read out of the store, the import commits are written, and
// the library directories, the import branches and the placements land as
// one journaled mutation. One whose record names none, and one no tool
// recorded, is adopted with its files as they are, each in a journal of
// its own, in name order, before the installs. All of it lands in one hold
// of the lock, see adoptTogether. A skill agentx already manages is not
// acted on.
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
	kept, summaries, err := inv.adoptTogether(ctx, b, asItIs, ready)
	if err != nil {
		return err
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

// landing is what one run of agentx skill import changes on this machine:
// made ready before the lock, and landed in one hold of it, see land.
type landing struct {
	fetched  []*fetchedSource // the sources the run fetched, whose settings entries are not written yet
	forks    []*forking       // the skills adopted with their files as they are, their creation commits written
	versions []*imported      // the versions to install, their import commits written
	gitDir   string
	targets  []placeTarget

	// What the hold of the lock did.
	recorded  []*fetchedSource
	landed    []landedFork
	dones     []*installed
	journaled bool // the journal of the installs was written
}

// landedFork is one skill adopted with its files as they are, and the
// placements its adoption made.
type landedFork struct {
	fk   *forking
	done placements
}

// adoptTogether adopts each skill of names with its files as they are, see
// planAdoption, and installs each of ready from the source its record
// names, as one change of this machine. What needs the network or writes
// objects alone comes first, outside the lock: the sources the settings do
// not hold are fetched, the versions read, and the creation commits and the
// import commits written. Then one hold of the lock records the sources,
// makes the hub skills and installs the versions, see land, and ends with
// the one change signal of the run. One rescan follows, which every skill
// is reported from. It returns the names adopted with their files as they
// are and what the result says of the run, part by part.
//
// A run that ends before that hold takes the remotes of the sources it
// fetched back, so that it leaves the machine as it found it.
func (inv *invocation) adoptTogether(ctx context.Context, b *batch, names []string, ready []*candidate) (kept map[string]bool, summaries []string, err error) {
	targets, err := inv.placementTargets(nil)
	if err != nil {
		return nil, nil, err
	}
	l := &landing{targets: targets}
	defer func() {
		for _, f := range l.fetched {
			if !f.settled {
				err = f.takeBack(err)
			}
		}
	}()
	// A skill adopted with its files as they are takes no step of the
	// batch's budget, which counts the steps of an install.
	b.total -= stepsPerSkill * len(names)
	if l.versions, l.gitDir, err = inv.adoptVersions(ctx, b, ready, &l.fetched); err != nil {
		return nil, nil, err
	}
	if l.forks, err = inv.writeAdoptions(ctx, b, names); err != nil {
		return nil, nil, err
	}
	run, imports := lineage.NewRun(), len(l.versions)
	if imports > 0 {
		if err := inv.writeImports(ctx, b, l.gitDir, run, l.versions); err != nil {
			return nil, nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err // a run stopped before its hold of the lock lands nothing
	}
	if len(l.fetched)+len(l.forks)+len(l.versions) > 0 {
		err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error { return inv.land(ctx, b, l) })
		if errors.Is(err, errNothingInstalled) {
			err = nil // every skill was refused; the caller answers for them
		}
	}
	// Once a journal exists, recovery needs the commits the staging refs
	// of the import hold.
	if imports > 0 && (err == nil || !l.journaled) {
		inv.dropImporting(ctx, l.gitDir, run, imports)
	}
	// What landed is reported whatever ended the hold.
	kept, summaries, reportErr := inv.reportLanded(ctx, b, l)
	if err != nil {
		return nil, nil, mutationFailure(err)
	}
	return kept, summaries, reportErr
}

// writeAdoptions plans the adoption of each skill of names with its files
// as they are and writes its creation commit, in name order, before the
// lock: the folder this machine holds of it becomes a hub skill once land
// applies the plan. One that is refused is dropped from the batch with its
// refusal.
func (inv *invocation) writeAdoptions(ctx context.Context, b *batch, names []string) ([]*forking, error) {
	var forks []*forking
	for _, name := range names {
		fk, err := inv.planAdoption(ctx, name, adoptChanged(name))
		if err == nil {
			err = inv.writeFork(ctx, fk)
		}
		var f *failure
		switch {
		case err == nil:
			forks = append(forks, fk)
		case errors.As(err, &f):
			b.drop(name, 0, f)
		default:
			return nil, err
		}
	}
	return forks, nil
}

// land is the one hold of the lock of a run that adopts, under the lock
// its caller took: the settings entry of each source the run fetched, then
// each skill adopted with its files as they are, in a journal of its own,
// then the installs, every skill of them in one journal. A source that
// cannot be recorded costs its own skills, and a skill that is refused
// costs the run that skill and no other; both are dropped from the batch.
// A hold that changed nothing is errNothingInstalled, which signals
// nothing. Any other error ends the hold where it is, and what the hold
// changed before it is signalled all the same.
func (inv *invocation) land(ctx context.Context, b *batch, l *landing) error {
	changed := false
	ended := func(err error) error {
		if changed {
			_ = home.BumpVersion(inv.dirs.Home)
		}
		return err
	}
	for _, f := range l.fetched {
		if err := f.record(); err != nil {
			refusal, kept := failureOf(err), l.versions[:0]
			for _, v := range l.versions {
				if v.imp.Source == f.entry.URL {
					b.drop(v.name, stepsPerSkill-2, refusal) // the blobs and the import already ended
					continue
				}
				kept = append(kept, v)
			}
			l.versions = kept
			continue
		}
		l.recorded = append(l.recorded, f)
		changed = true
	}
	for _, fk := range l.forks {
		var done placements
		err := inv.landFork(ctx, fk, &done)
		var f *failure
		switch {
		case err == nil:
			l.landed = append(l.landed, landedFork{fk, done})
			changed = true
		case errors.As(err, &f):
			b.drop(fk.target, 0, f)
		default:
			return ended(err)
		}
	}
	if len(l.versions) > 0 {
		dones, journaled, err := inv.installLocked(ctx, b, l.gitDir, l.versions, l.targets, false)
		l.journaled = journaled
		switch {
		case errors.Is(err, errNothingInstalled):
		case err != nil:
			return ended(err)
		default:
			l.dones, b.done, changed = dones, dones, true
		}
	}
	if !changed {
		return errNothingInstalled
	}
	return nil
}

// reportLanded reports what land did, from one rescan: each source it
// recorded, as source add reports one, each skill adopted with its files
// as they are, and the installs. It returns the names of the former and
// what the result says of each part.
func (inv *invocation) reportLanded(ctx context.Context, b *batch, l *landing) (kept map[string]bool, summaries []string, err error) {
	kept = map[string]bool{}
	for _, f := range l.recorded {
		f.report()
	}
	if len(l.dones) == 0 {
		b.total-- // the rescan that ends an install
	}
	if len(l.landed)+len(l.dones) == 0 {
		return kept, nil, nil
	}
	for _, done := range l.dones {
		b.step(phaseInstall, done.v.name)
	}
	inventory, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return nil, nil, err
	}
	if len(l.landed) > 0 {
		sc, err := inv.skillContext(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, landed := range l.landed {
			if err := inv.reportAdoptedOwnIn(ctx, inventory, sc, landed.fk, landed.done); err != nil {
				return nil, nil, err
			}
			kept[landed.fk.target] = true
			summaries = append(summaries, inv.summary)
		}
	}
	if len(l.dones) > 0 {
		if err := inv.reportInstalledIn(ctx, b, inventory, l.dones); err != nil {
			return nil, nil, err
		}
		summaries = append(summaries, "imported "+strings.TrimPrefix(strings.TrimPrefix(inv.summary, "installed "), "imported "))
	}
	return kept, summaries, nil
}

// adoptVersions reads the version each candidate is adopted at: the skill
// at its subpath in its source, at the pin this machine holds, the source
// fetched first when the settings do not hold it, and added to fetched. A
// source that cannot be fetched or read costs its own skills and no
// others. It returns the versions, filled, and the store they were read
// from.
func (inv *invocation) adoptVersions(ctx context.Context, b *batch, ready []*candidate, fetched *[]*fetchedSource) ([]*imported, string, error) {
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
		listing, last, f, err := inv.adoptListing(ctx, b, url, cands, fetched)
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
		dropped := len(b.broken)
		read, err := inv.readVersions(ctx, b, gitDir, src, listing.Commit, skills)
		if f := (*failure)(nil); errors.As(err, &f) && f.status == exitSource && ctx.Err() == nil {
			// The source cannot be reached for the files of its skills,
			// which costs the ones the read did not give up on already.
			gone := map[string]bool{}
			for _, broken := range b.broken[dropped:] {
				gone[broken.subject] = true
			}
			for _, c := range owners {
				if !gone[c.rec.name] {
					b.drop(c.rec.name, stepsPerSkill, f)
				}
			}
			continue
		}
		if err != nil {
			return nil, "", err
		}
		for i, v := range read {
			v.fetched, v.replace, v.links = last, true, owners[i].rec.links
		}
		versions = append(versions, read...)
	}
	return versions, gitDir, nil
}

// adoptListing is what one source holds at its pin, and when it was last
// fetched: the listing of the fetch of a source the settings do not hold,
// which is added to fetched for land to record, else the listing of the
// store as it was last fetched. A source the run cannot read is the
// failure returned, which costs its skills; an error is the run's.
func (inv *invocation) adoptListing(ctx context.Context, b *batch, url string, cands []*candidate, fetched *[]*fetchedSource) (listing source.Listing, last string, f *failure, err error) {
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
		src, err := inv.fetchSource(ctx, source.Source{URL: url, Ref: pin})
		if err != nil {
			b.total--
			return listing, "", failureOf(err), nil
		}
		b.step(phaseFetch, url)
		*fetched = append(*fetched, src)
		return src.listing, src.entry.LastFetched, nil, nil
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
func (inv *invocation) previewAdoption(cands []*candidate, loose []looseSkill, records int, tools map[string]bool) {
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
	if len(tools) > 0 {
		installed = " installed by " + toolNames(tools, "or")
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
		from := fromTools(tools)
		t := &table{}
		t.add(cell{text: "agentx skill import" + from + " --all", style: label}, cell{text: all, style: muted})
		t.add(cell{text: "agentx skill import <name>" + from, style: label}, cell{text: "one skill", style: muted})
		out.render(t, "  ")
	case len(refused) > 0:
		out.print("Nothing to import.")
	case len(tools) == 0:
		out.print("Nothing to import: this machine has no unmanaged skill.")
	case len(managed) > 0:
		out.print("Nothing to import: agentx already manages every skill ", toolNames(tools, "or"), " installed.")
	default:
		out.print("Nothing to import: no skill installed by ", toolNames(tools, "or"), " is on this machine.")
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
