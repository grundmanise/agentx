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

// The tools whose install records agentx adopt reads.
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
// agentx adopt adds, and the ones source fetch, check-updates, publish and
// the account commands fetch.
const phaseFetch = "fetch"

// adoptSelection is what one run of agentx adopt was asked to do: nothing
// but look, adopt the skills named, or adopt every one recorded.
type adoptSelection struct {
	names []string
	all   bool
}

func newAdoptCommand(inv *invocation) *cobra.Command {
	var sel adoptSelection
	cmd := &cobra.Command{
		Use:   "adopt",
		Short: "Install the skills another tool installed, so that agentx manages them",
		Long: "Take over the skills the Vercel skills CLI or Skillshare installed, so that agentx\n" +
			"knows where each one came from and can update it. Run it with no flags to see\n" +
			"what it would adopt; nothing is written and the other tool's files are never\n" +
			"touched.\n\n" +
			"Adopting a skill installs it from its source, as 'agentx skill add' does, at the\n" +
			"version the source holds now, and places it in every enabled configuration.\n" +
			"Whatever the library holds under the skill's name gives way to that version, an\n" +
			"edit made to it included: fork the skill first to keep one.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return inv.adopt(cmd.Context(), sel) },
	}
	cmd.Flags().StringArrayVar(&sel.names, "skill", nil, "the skill to adopt, by its name in the library; give it again for each")
	cmd.Flags().BoolVar(&sel.all, "all", false, "adopt every recorded skill that can be adopted")
	return cmd
}

// adopting reports whether the run was asked to change anything.
func (s adoptSelection) adopting() bool { return s.all || len(s.names) > 0 }

// check refuses a combination of flags that asks for two things at once.
func (s adoptSelection) check() error {
	if s.all && len(s.names) > 0 {
		return fail(exitUsage, "--all and --skill cannot both be given",
			"--all adopts every recorded skill; drop it to adopt the ones you name")
	}
	return nil
}

// adopt reads what the other tools installed and reports, or adopts, the
// skills they recorded.
//
// Without a flag it is a preview and writes nothing at all: it takes no
// lock, creates neither agentx home nor the account repo, and reaches for
// no network. The other tools' files are never written, on any path.
func (inv *invocation) adopt(ctx context.Context, sel adoptSelection) error {
	if err := sel.check(); err != nil {
		return err
	}
	records, err := inv.readRecords()
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
	if !sel.adopting() {
		inv.previewAdoption(cands, len(records))
		return nil
	}
	chosen, err := inv.chooseAdoptions(cands, records, sel)
	if err != nil {
		return err
	}
	return inv.adoptChosen(ctx, chosen)
}

// record is one skill another tool installed, as that tool recorded it:
// what an install from its source needs, and where that tool put it.
type record struct {
	tool    string
	name    string   // the library directory name
	file    string   // the file the record was read from
	dir     string   // the directory that tool installed the skill into; "" for the library entry, built once the name is known safe
	urls    []string // the forms the record names its source in, tried in order
	ref     string   // the branch or tag it was installed from, "" for the default branch
	subpath string   // the skill's directory inside the source, "" for its root
	refusal *failure // what the record itself rules out, before any source is looked at
}

// readRecords reads the install records of every tool, in name order. A
// name both tools record is one warning, and the Vercel skills CLI's record
// is the one used: its directory is the library entry itself.
func (inv *invocation) readRecords() ([]record, error) {
	entries, warnings, err := vercel.Read(vercel.LockPaths(inv.env, inv.dirs.User))
	for _, w := range warnings {
		inv.out.warn(w)
	}
	if err != nil {
		return nil, recordFailure(err)
	}
	var records []record
	for _, e := range entries {
		records = append(records, vercelRecord(e))
	}
	paths := skillshare.Locate(inv.env, inv.dirs.Config)
	skills, err := skillshare.SkillsDir(paths, inv.dirs.User)
	if err != nil {
		return nil, recordFailure(err)
	}
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

// recordFailure answers a record file that is not one: the same exit code
// an unreadable settings file has, since both are a file agentx reads whole
// and can make nothing of.
func recordFailure(err error) error {
	if errors.Is(err, vercel.ErrLock) || errors.Is(err, skillshare.ErrStore) {
		return fail(exitInternal, err.Error(), "fix the file or move it aside; agentx never writes it")
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
		r.refusal = refuse(exitRefused,
			fmt.Sprintf("%s was installed from a %s source, which is not a git repository agentx can fetch", e.Name, sanitised(e.SourceType)),
			"install it again with 'agentx skill add <git source>', or leave it unmanaged")
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
		dir: filepath.Join(skills, filepath.FromSlash(e.Name)),
		ref: e.Branch, subpath: e.Subdir, urls: nonEmpty(e.RepoURL, e.Source),
	}
	switch kind := strings.TrimSuffix(e.Type, "-subdir"); {
	case e.Tracked:
		r.refusal = refuse(exitRefused, r.name+" is a repository Skillshare tracks as a whole, with skills inside it",
			"add the repository with 'agentx source add <url>' and install its skills with 'agentx skill add'")
	case kind == "github", kind == "git-https", kind == "git-ssh", kind == "":
	default:
		r.refusal = refuse(exitRefused,
			fmt.Sprintf("%s was installed from a %s source, which is not a git repository agentx can fetch", r.name, sanitised(kind)),
			"install it again with 'agentx skill add <git source>', or leave it unmanaged")
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
// machine alone: what the account repo already knows about the name, what
// the record rules out, and whether it names a source agentx can fetch.
func (inv *invocation) judge(c *candidate, records map[string]lineage.Record, s home.Settings) {
	switch rec, known := records[c.rec.name]; {
	case known && rec.Kind == lineage.KindFork:
		c.refuse(refuse(exitRefused, c.rec.name+" is a skill of the account remote on this machine",
			"it has a history of its own; adopting would take the place of it"))
		return
	case known:
		c.state, c.reason = adoptManaged, "agentx already manages it"
		return
	case c.rec.refusal != nil:
		c.refuse(c.rec.refusal)
		return
	}
	src, f := adoptSource(c.rec)
	if f != nil {
		c.refuse(f)
		return
	}
	if src.Stripped {
		inv.out.warn("the user or token in the URL of " + sanitised(c.rec.name) + " was dropped: agentx never stores credentials; put them in a git credential helper (git config credential.helper) or an SSH key")
		src.Stripped = false
	}
	if !lineage.ValidPath(c.rec.subpath) {
		// Quoted and not sanitised: a directory a trailer cannot carry is
		// one padded with spaces as often as one holding a control
		// character, and turning either into a space would name a
		// directory the record does not.
		c.refuse(refuse(exitRefused, fmt.Sprintf("%s names %q, which is not a directory of %s", c.rec.file, c.rec.subpath, src.URL),
			"fix the entry, or install the skill with 'agentx skill add'"))
		return
	}
	c.src = src
	if i := s.FindSource(src.URL); i >= 0 {
		c.added, c.entry = true, s.Sources[i]
		return
	}
	c.reason = "its source is not added yet; adopting adds and fetches it"
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

// chooseAdoptions picks the records a run covers: every candidate with
// --all, or exactly those --skill names. A name no record holds fails the
// run before anything is fetched, the way a --skill that names no skill of
// a source does. What it returns is in name order, whatever order the
// flags were given in.
func (inv *invocation) chooseAdoptions(cands []*candidate, records []record, sel adoptSelection) ([]*candidate, error) {
	if sel.all {
		return cands, nil
	}
	byName := map[string]*candidate{}
	for _, c := range cands {
		byName[c.rec.name] = c
	}
	var chosen []*candidate
	seen := map[string]bool{}
	for _, name := range sel.names {
		if seen[name] {
			continue // a name given twice adopts one skill
		}
		seen[name] = true
		c, ok := byName[name]
		if !ok {
			return nil, noAdoptionNamed(name, cands, records)
		}
		chosen = append(chosen, c)
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].rec.name < chosen[j].rec.name })
	return chosen, nil
}

// noAdoptionNamed answers a --skill the run cannot place: no record may
// name it, or one may name it for a directory that is gone, which are
// different things to fix.
func noAdoptionNamed(name string, cands []*candidate, records []record) error {
	for _, r := range records {
		if r.name == name {
			return fail(exitNotFound, fmt.Sprintf("%s says %q was installed, and this machine no longer holds it", r.file, name),
				"run 'agentx adopt' to see what can be adopted")
		}
	}
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		names = append(names, sanitised(c.rec.name))
	}
	hint := "run 'agentx adopt' to see what can be adopted"
	if len(names) > 0 {
		if len(names) > namesInAHint {
			names = append(names[:namesInAHint], "...")
		}
		hint = "the records name: " + strings.Join(names, ", ")
	}
	return fail(exitNotFound, fmt.Sprintf("no skill called %q was installed by the Vercel skills CLI or Skillshare", sanitised(name)), hint)
}

// adoptChosen installs every chosen skill from its source, as skill add
// installs one: the source is added and fetched when the settings do not
// hold it, the version at its pin is read out of the account repo, the
// import commits are written, and the library directories, the import
// branches and the placements land as one journaled mutation. A skill
// agentx already manages is not acted on.
func (inv *invocation) adoptChosen(ctx context.Context, covered []*candidate) error {
	var chosen []*candidate
	for _, c := range covered {
		if c.state != adoptManaged {
			chosen = append(chosen, c)
		}
	}
	if len(chosen) == 0 {
		inv.reportAdopted(nil, covered)
		inv.out.print("Already adopted.")
		inv.summary = "already adopted"
		return nil
	}
	targets, err := inv.placementTargets(nil)
	if err != nil {
		return err
	}
	b := newBatch(inv, len(chosen))
	b.verb, b.mixed = "adopted", "run 'agentx adopt' to see what is left, and adopt the rest one at a time"
	var ready []*candidate
	for _, c := range chosen {
		if c.fail != nil {
			b.drop(c.rec.name, stepsPerSkill, c.fail)
			continue
		}
		ready = append(ready, c)
	}
	versions, gitDir, err := inv.adoptVersions(ctx, b, ready)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		inv.reportAdopted(b, covered)
		return b.abandon()
	}
	run := lineage.NewRun()
	if err := inv.writeImports(ctx, b, gitDir, run, versions); err != nil {
		return err
	}
	dones, journaled, err := inv.install(ctx, b, gitDir, versions, targets, false)
	if err != nil {
		if !journaled {
			inv.dropImporting(ctx, gitDir, run, len(versions))
		}
		return err
	}
	inv.dropImporting(ctx, gitDir, run, len(versions))
	if len(dones) == 0 {
		inv.reportAdopted(b, covered)
		return b.abandon()
	}
	if err := inv.reportInstalled(ctx, b, dones); err != nil {
		return err
	}
	inv.summary = "adopted " + strings.TrimPrefix(strings.TrimPrefix(inv.summary, "installed "), "adopted ")
	inv.reportAdopted(b, covered)
	if f := b.failure(); f != nil {
		return f
	}
	return nil
}

// adoptVersions reads the version each candidate is adopted at: the skill
// at its subpath in its source, at the pin this machine holds, the source
// added and fetched first when the settings do not hold it. A source that
// cannot be added or read costs its own skills and no others. It returns
// the versions, filled, and the account repo they were read from.
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
			if gitDir, _, err = gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home); err != nil {
				return nil, "", accountRepoFailure(err)
			}
		}
		var skills []source.Skill
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
		}
		if len(skills) == 0 {
			continue
		}
		src := source.Source{URL: url}
		read, err := inv.readVersions(ctx, b, gitDir, src, listing.Commit, skills)
		if err != nil {
			return nil, "", err
		}
		for _, v := range read {
			v.fetched, v.replace = fetched, true
		}
		versions = append(versions, read...)
	}
	return versions, gitDir, nil
}

// adoptListing is what one source holds at its pin: the listing of the
// fetch that adds it when the settings do not hold it, else the listing of
// the account repo as it was last fetched. A source the run cannot read is
// the failure returned, which costs its skills; an error is the run's.
func (inv *invocation) adoptListing(ctx context.Context, b *batch, url string, cands []*candidate) (listing source.Listing, fetched string, f *failure, err error) {
	if !cands[0].added {
		pin, recorded := adoptPin(cands)
		if len(recorded) > 1 {
			// One source has one pin on a machine, and it governs every
			// later install from it, so records of two refs of it cannot
			// be answered in silence.
			inv.out.warn(fmt.Sprintf("%s was installed at more than one ref (%s); it is added pinned to %s. To change the pin, run 'agentx source add %s#<ref>' and adopt again",
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
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return listing, "", nil, accountRepoFailure(err)
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
// name order, as the run left it: adopted for a skill that landed, refused
// with its reason for one the run gave up on, managed for one it never
// acted on. The skills that landed were reported as an install reports
// them already.
func (inv *invocation) reportAdopted(b *batch, covered []*candidate) {
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
		switch {
		case landed[c.rec.name]:
			c.state, c.reason = adoptAdopted, ""
		case refused[c.rec.name] != nil:
			c.refuse(refused[c.rec.name])
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
func (inv *invocation) previewAdoption(cands []*candidate, records int) {
	out := inv.out
	if len(cands) == 0 {
		out.print("Nothing to adopt: no skill installed by the Vercel skills CLI or Skillshare is on this machine.")
		inv.summary = "nothing to adopt"
		return
	}
	counts := map[string]int{}
	out.print(out.paint(heading, plural(len(cands), "skill")), " installed by another tool")
	t := &table{}
	for _, c := range cands {
		out.emit(c.eventOf())
		counts[c.state]++
		t.add(c.rowOf()...)
	}
	out.render(t, "")
	if counts[adoptCandidate] > 0 {
		out.print("Adopt them with ", out.paint(label, "agentx adopt --all"), ".")
	}
	inv.summary = previewSummary(counts, records)
}

// rowOf is one row of the preview listing. Every field the record supplied
// is sanitised: it is text agentx did not write.
func (c *candidate) rowOf() []cell {
	state := cell{text: c.state, style: muted}
	switch c.state {
	case adoptCandidate:
		state.style = okStyle
	case adoptRefused:
		state.style = warnStyle
	}
	where := cell{text: "(none)", style: muted}
	if c.src.URL != "" {
		text := c.src.URL
		if c.rec.subpath != "" {
			text += "/" + c.rec.subpath
		}
		where = cell{text: sanitised(text), style: plain}
	}
	return []cell{{text: "  " + sanitised(c.rec.name), style: heading}, {text: c.rec.tool, style: muted}, state, where, {text: sanitised(c.reason), style: muted}}
}

// previewSummary is what the result event says a preview found.
func previewSummary(counts map[string]int, records int) string {
	found := counts[adoptCandidate] + counts[adoptManaged] + counts[adoptRefused]
	parts := []string{plural(found, "skill") + " installed by another tool"}
	for _, what := range []struct {
		n    int
		text string
	}{
		{counts[adoptCandidate], "to adopt"},
		{counts[adoptManaged], "already managed"},
		{counts[adoptRefused], "that cannot be adopted"},
	} {
		if what.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", what.n, what.text))
		}
	}
	if records > found {
		parts = append(parts, fmt.Sprintf("%d no longer on this machine", records-found))
	}
	return strings.Join(parts, ", ")
}
