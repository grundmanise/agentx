package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/vercel"
)

// The states one entry of the lock file is reported in. A preview reports
// candidate, managed and refused; a run that adopts reports adopted in
// place of candidate.
const (
	adoptCandidate = "candidate" // it can be adopted
	adoptAdopted   = "adopted"   // this run wrote its import branch
	adoptManaged   = "managed"   // agentx already knows the skill
	adoptRefused   = "refused"   // it cannot be adopted, and why
)

// The steps an adoption reports: one per source it has to add, which is
// the fetch of 'agentx source add', and one per skill it adopts.
const (
	phaseFetch = "fetch"
	phaseAdopt = "adopt"
)

// adoptionEvent is one entry of the vercel skills lock file whose library
// directory exists: where the entry says it came from, what the library
// holds under that name now, and what agentx did or would do about it.
type adoptionEvent struct {
	event
	Name           string  `json:"name"`
	Lock           string  `json:"lock"`                      // the lock file the entry was read from
	Source         string  `json:"source,omitempty"`          // the canonical URL of the source it names
	Subpath        *string `json:"subpath,omitempty"`         // the skill's directory in that source, "" for its root
	State          string  `json:"state"`                     // candidate, adopted, managed or refused
	UpstreamCommit string  `json:"upstream_commit,omitempty"` // the last commit that touched the skill's directory, reachable from the commit the base version was established at
	BaseHash       string  `json:"base_hash,omitempty"`       // the content hash of the base version
	ContentHash    string  `json:"content_hash,omitempty"`    // what the library directory holds now
	Modified       *bool   `json:"modified,omitempty"`        // whether the directory differs from the base
	Reason         string  `json:"reason,omitempty"`          // why it was refused, or what an adoption would still have to do
}

// adoptSelection is what one run of agentx adopt was asked to do: nothing
// but look, adopt the skills named, or adopt every one the lock file holds.
type adoptSelection struct {
	names []string
	all   bool
	base  string // the ref or commit an explicit base selection names
}

func newAdoptCommand(inv *invocation) *cobra.Command {
	var sel adoptSelection
	cmd := &cobra.Command{
		Use:   "adopt",
		Short: "Adopt skills another tool installed into the library",
		Long: "Take over the skills the vercel skills CLI installed into the library, so that\n" +
			"agentx knows where each one came from and can update and revert it. Run it with\n" +
			"no flags to see what it would adopt; nothing is written and the lock file is\n" +
			"never touched.\n\n" +
			"The base version recorded for a skill is the upstream version it was installed\n" +
			"at, read from the source and verified against the lock file. The directory on\n" +
			"disk is left exactly as it is, so an edit made to it stays an edit. A skill whose\n" +
			"upstream version cannot be established is left unmanaged rather than have what is\n" +
			"on disk recorded as if it came from upstream.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return inv.adopt(cmd.Context(), sel) },
	}
	cmd.Flags().StringArrayVar(&sel.names, "skill", nil, "the skill to adopt, by its name in the library; give it again for each")
	cmd.Flags().BoolVar(&sel.all, "all", false, "adopt every skill of the lock file that can be adopted")
	cmd.Flags().StringVar(&sel.base, "base", "", "with one --skill, the commit id, branch or tag of the source to record as its base version")
	return cmd
}

// adopting reports whether the run was asked to change anything.
func (s adoptSelection) adopting() bool { return s.all || len(s.names) > 0 }

// check refuses a combination of flags that asks for two things at once.
func (s adoptSelection) check() error {
	switch {
	case s.all && len(s.names) > 0:
		return fail(exitUsage, "--all and --skill cannot both be given",
			"--all adopts every skill of the lock file; drop it to adopt the ones you name")
	case s.base != "" && len(s.names) != 1:
		return fail(exitUsage, "--base chooses the base version of one skill",
			"give --skill once beside --base, since a base names one version of one skill")
	case s.base != "" && !source.ValidRef(s.base):
		return fail(exitUsage, fmt.Sprintf("--base %q is not a commit or ref git accepts", s.base),
			"name a commit, a branch or a tag of the source")
	}
	return nil
}

// adopt reads the vercel skills lock file and reports, or adopts, the
// skills it names that the library holds.
//
// Without a flag it is a preview and writes nothing at all: it takes no
// lock, creates neither agentx home nor the account repo, and reaches for
// no network. The lock file is never written, on any path.
func (inv *invocation) adopt(ctx context.Context, sel adoptSelection) error {
	if err := sel.check(); err != nil {
		return err
	}
	entries, warnings, err := vercel.Read(vercel.LockPaths(inv.env, inv.dirs.User))
	for _, w := range warnings {
		inv.out.warn(w)
	}
	if err != nil {
		return lockFailure(err)
	}
	records, err := inv.lineageRecords(ctx)
	if err != nil {
		return err
	}
	cands, err := inv.adoptCandidates(entries, records)
	if err != nil {
		return err
	}
	if !sel.adopting() {
		inv.previewAdoption(cands, len(entries))
		return nil
	}
	chosen, err := inv.chooseAdoptions(cands, entries, sel)
	if err != nil {
		return err
	}
	return inv.adoptChosen(ctx, chosen, sel)
}

// lockFailure answers a lock file that is not one: the same exit code an
// unreadable settings file has, since both are a file agentx reads whole
// and can make nothing of.
func lockFailure(err error) error {
	if errors.Is(err, vercel.ErrLock) {
		return fail(exitInternal, err.Error(), "fix the file or move it aside; agentx never writes it")
	}
	return fail(exitInternal, err.Error(), "check that the lock file is readable")
}

// candidate is one entry of the lock file whose library directory exists,
// with what agentx makes of it.
type candidate struct {
	entry vercel.Entry
	path  string // the library directory
	hash  string // the content hash of that directory now

	src   source.Source // the source the entry names, empty when it names none agentx can use
	added bool          // the settings already hold that source

	state  string
	reason string
	fail   *failure // the refusal, which is how a run asked to adopt it answers

	tip      string // the commit this machine fetched the source at
	tipTree  string // the tree of the skill's directory there, empty when the source no longer has it
	searched bool   // the folder hash was looked for in the history of that directory

	base     *basePlan // where the base version was established
	imported *imported // the version itself, once read
	modified bool      // the directory differs from the base
}

// refuse records why the candidate cannot be adopted.
func (c *candidate) refuse(f *failure) {
	c.state, c.reason, c.fail = adoptRefused, f.message, f
}

// subpath is the skill's directory inside its source.
func (c *candidate) subpath() string { return c.entry.Subpath }

// adoptCandidates turns the lock file's entries into candidates: one per
// entry whose library directory exists, in the order the entries are
// sorted, which is by name. An entry the library does not hold is no
// candidate at all, since there is nothing on this machine to adopt.
func (inv *invocation) adoptCandidates(entries []vercel.Entry, records map[string]lineage.Record) ([]*candidate, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	var cands []*candidate
	for _, e := range entries {
		if why := nameRefusal(e.Name); why != "" {
			// The name would have to become a library directory and a
			// branch, and it is refused in the words of whichever cannot
			// hold it. Nothing is looked up under it: a name that walks out
			// of the library may not be joined to a path.
			inv.out.warn(fmt.Sprintf("%q in %s %s and is left out", sanitised(e.Name), e.File, why))
			continue
		}
		c := &candidate{entry: e, path: inv.libraryPath(e.Name), state: adoptCandidate}
		exists, dir, err := libraryEntry(c.path)
		if err != nil {
			inv.out.warn(c.path + " could not be read and is left out: " + err.Error())
			continue
		}
		if !exists {
			continue // the lock file names it, this machine does not have it
		}
		if dir {
			c.hash = contentHashAt(c.path)
		}
		inv.judge(c, dir, records, s.FindSource)
		cands = append(cands, c)
	}
	return cands, nil
}

// judge decides what can be done with one candidate from what is on this
// machine alone: what the library directory is, what the account repo
// already knows about the name, and whether the entry names a source agentx
// can fetch.
func (inv *invocation) judge(c *candidate, dir bool, records map[string]lineage.Record, findSource func(string) int) {
	switch rec, known := records[c.entry.Name]; {
	case !dir:
		c.refuse(refuse(exitRefused, c.path+" is not a directory the library holds",
			"a skill agentx manages is a real directory in the library; leave this one as it is"))
		return
	case c.hash == "":
		c.refuse(refuse(exitRefused, c.path+" holds no SKILL.md and is no skill of the library",
			"a directory without a SKILL.md is not a skill; leave it as it is or remove it"))
		return
	case known && rec.Kind == lineage.KindFork:
		c.refuse(refuse(exitRefused, c.entry.Name+" is a fork on this machine",
			"a fork has a history of its own; adopting would take the place of it"))
		return
	case known:
		c.state, c.reason = adoptManaged, "agentx already manages it"
		return
	}
	src, f := adoptSource(c.entry)
	if f != nil {
		c.refuse(f)
		return
	}
	if src.Stripped {
		inv.out.warn("the user or token in the URL of " + sanitised(c.entry.Name) + " was dropped: agentx never stores credentials; put them in a git credential helper (git config credential.helper) or an SSH key")
		src.Stripped = false
	}
	if !lineage.ValidPath(c.entry.Subpath) {
		// Quoted and not sanitised: a directory a trailer cannot carry is
		// one padded with spaces as often as one holding a control
		// character, and turning either into a space would name a
		// directory the lock file does not.
		c.refuse(refuse(exitRefused, fmt.Sprintf("%s names %q, which is not a directory of %s", c.entry.File, c.entry.Subpath, src.URL),
			"fix the entry in the lock file, or install the skill with 'agentx skill add'"))
		return
	}
	c.src, c.added = src, findSource(src.URL) >= 0
	if !c.added {
		c.reason = "its source is not added yet; adopting adds and fetches it"
	}
}

// libraryEntry says what the library holds at path: whether anything is
// there, and whether it is a real directory. The journal's own reading of a
// path is not used here: it fingerprints a directory, which costs a full
// read of every file in it, and an adoption needs only to tell a directory
// from a link.
func libraryEntry(path string) (exists, dir bool, err error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, false, nil
	case err != nil:
		return false, false, err
	}
	return true, info.IsDir(), nil
}

// adoptSource is the source a lock entry names, canonicalised the way every
// source agentx stores is. The URL the entry was installed from is read
// first and its normalised identifier second, so that a URL agentx does not
// accept, a GitHub blob page among them, still leaves owner/repo to fall
// back on. A user or token in either is dropped before the URL is used
// anywhere, as it is for 'agentx source add'.
func adoptSource(e vercel.Entry) (source.Source, *failure) {
	switch e.SourceType {
	case "github", "gitlab", "git", "":
	default:
		return source.Source{}, refuse(exitRefused,
			fmt.Sprintf("%s was installed from a %s source, which is not a git repository agentx can fetch", e.Name, sanitised(e.SourceType)),
			"install it again with 'agentx skill add <git source>', or leave it unmanaged")
	}
	for _, raw := range []string{e.SourceURL, e.Source} {
		if raw == "" {
			continue
		}
		src, err := source.Parse(raw)
		if err != nil {
			continue
		}
		// The subpath and the ref of the entry decide, not the ones a tree
		// URL happens to carry: skillPath is what that tool recorded about
		// this skill, and the URL may name the whole repository.
		ref := src.Ref
		if e.Ref != "" && source.ValidRef(e.Ref) {
			ref = e.Ref
		}
		src.Subpath, src.Ref = "", ref
		return src, nil
	}
	return source.Source{}, refuse(exitRefused,
		fmt.Sprintf("%s names no source agentx can read", e.Name),
		"accepted forms: "+source.Forms)
}

// chooseAdoptions picks the entries a run covers: every candidate with
// --all, or exactly those --skill names. A name the lock file does not hold
// at all fails the run before anything is fetched, the way a --skill that
// names no skill of a source does.
//
// What it returns is in name order, whatever order the flags were given in,
// because the run reports in the order it acts: one order for a run over
// the same skills however they were asked for.
func (inv *invocation) chooseAdoptions(cands []*candidate, entries []vercel.Entry, sel adoptSelection) ([]*candidate, error) {
	if sel.all {
		return cands, nil
	}
	byName := map[string]*candidate{}
	for _, c := range cands {
		byName[c.entry.Name] = c
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
			return nil, inv.noAdoptionNamed(name, cands, entries)
		}
		chosen = append(chosen, c)
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].entry.Name < chosen[j].entry.Name })
	return chosen, nil
}

// noAdoptionNamed answers a --skill the run cannot place: the lock file may
// not name it at all, or may name it for a directory the library does not
// hold, which are different things to fix.
func (inv *invocation) noAdoptionNamed(name string, cands []*candidate, entries []vercel.Entry) error {
	for _, e := range entries {
		if e.Name == name {
			return fail(exitNotFound, fmt.Sprintf("the library holds no %q at %s, which %s says was installed there", name, inv.libraryPath(name), e.File),
				"run 'agentx adopt' to see what can be adopted")
		}
	}
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		names = append(names, sanitised(c.entry.Name))
	}
	hint := "run 'agentx adopt' to see what can be adopted"
	if len(names) > 0 {
		if len(names) > namesInAHint {
			names = append(names[:namesInAHint], "...")
		}
		hint = "the lock file names: " + strings.Join(names, ", ")
	}
	return fail(exitNotFound, fmt.Sprintf("no skill called %q in the vercel skills lock file", sanitised(name)), hint)
}

// adoptRun is one run of agentx adopt that changes something: what it set
// out to do, how many steps it has reported, what it adopted and the skills
// it had to leave unmanaged.
type adoptRun struct {
	refusals
	inv      *invocation
	selected int
	current  int
	total    int
	done     []*candidate
}

func newAdoptRun(inv *invocation, selected, sources int) *adoptRun {
	return &adoptRun{
		refusals: refusals{verb: "adopted", mixed: "run 'agentx adopt' to see what is left, and adopt the rest one at a time"},
		inv:      inv,
		selected: selected,
		total:    selected + sources,
	}
}

// step reports one finished step of the run.
func (r *adoptRun) step(phase, subject string) {
	r.current++
	r.inv.progress(phase, subject, r.current, r.total)
}

// drop gives up on one skill: the step it will no longer run leaves the
// budget, so a run that ends still counts up to its total.
func (r *adoptRun) drop(c *candidate, f *failure) {
	if c.state != adoptRefused {
		c.refuse(f)
	}
	r.total--
	r.add(c.entry.Name, f)
	if r.selected > 1 {
		r.inv.out.warn(c.entry.Name + ": " + f.message)
	}
}

func (r *adoptRun) failure() *failure { return r.refusals.failure(r.selected, len(r.done)) }

// adoptChosen adds the sources the covered skills came from, establishes
// the base version of each from the source itself, writes the import
// commits and points the import branches at them as one journaled mutation.
//
// The library directories are not touched: adoption records where a skill
// came from and changes nothing on disk, so an edit made to a directory
// stays an edit against the version it was installed at.
func (inv *invocation) adoptChosen(ctx context.Context, covered []*candidate, sel adoptSelection) error {
	var chosen []*candidate // what the run acts on: a skill agentx already manages is not acted on
	for _, c := range covered {
		if c.state != adoptManaged {
			chosen = append(chosen, c)
		}
	}
	if len(chosen) == 0 {
		inv.reportAdopted(nil, covered, "nothing left to adopt")
		return nil
	}
	toAdd := sourcesToAdd(chosen)
	run := newAdoptRun(inv, len(chosen), len(toAdd))
	var ready []*candidate
	for _, c := range chosen {
		if c.fail != nil {
			run.drop(c, c.fail)
			continue
		}
		ready = append(ready, c)
	}
	ready = inv.addAdoptSources(ctx, run, ready, toAdd)
	if len(ready) > 0 {
		var err error
		if ready, err = inv.readAdoptBases(ctx, run, ready, sel); err != nil {
			return err
		}
	}
	if len(ready) > 0 {
		if err := inv.writeAdoptions(ctx, run, ready); err != nil {
			return err
		}
	}
	inv.reportAdopted(run, covered, "")
	if f := run.failure(); f != nil {
		return f
	}
	return nil
}

// sourcesToAdd are the canonical URLs of the chosen skills' sources that
// the settings do not hold yet, in the order the skills name them, so that
// a run adds each source once.
func sourcesToAdd(chosen []*candidate) []string {
	var urls []string
	seen := map[string]bool{}
	for _, c := range chosen {
		if c.fail != nil || c.added || c.src.URL == "" || seen[c.src.URL] {
			continue
		}
		seen[c.src.URL] = true
		urls = append(urls, c.src.URL)
	}
	return urls
}

// addAdoptSources adds every source the run needs and does not have, which
// is the whole of 'agentx source add' and not a fetch of its own. A source
// that cannot be added costs its own skills and no others.
func (inv *invocation) addAdoptSources(ctx context.Context, run *adoptRun, ready []*candidate, urls []string) []*candidate {
	if len(urls) == 0 {
		return ready
	}
	failed := map[string]*failure{}
	for _, url := range urls {
		pin, recorded := adoptPin(ready, url)
		src := source.Source{URL: url, Ref: pin}
		if len(recorded) > 1 {
			// One source has one pin on a machine, and it governs every later
			// install from it, so a lock file that records two refs of it
			// cannot be answered in silence: the skills of the refs not taken
			// are looked for in the history of the one that was.
			inv.out.warn(fmt.Sprintf("%s was installed at more than one ref (%s); it is added pinned to %s. Add it at another ref with 'agentx source add %s#<ref>' and adopt again",
				url, strings.Join(pinNames(recorded), ", "), pinName(pin), url))
		}
		if _, _, err := inv.addSource(ctx, src); err != nil {
			var f *failure
			if !errors.As(err, &f) {
				f = refuse(exitInternal, err.Error(), "run 'agentx doctor' and check what it names")
			}
			failed[url] = f
			run.total-- // the fetch step this source will not report leaves the budget
			continue
		}
		run.step(phaseFetch, url)
	}
	if len(failed) == 0 {
		return ready
	}
	var kept []*candidate
	for _, c := range ready {
		if f, bad := failed[c.src.URL]; bad {
			run.drop(c, f)
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

// adoptPin is the ref a source is added at and every ref the covered
// entries recorded for it, in name order. A lock file records a ref per
// skill and a machine holds one pin per source, so the first entry in name
// order decides and the caller says so when they disagree.
func adoptPin(ready []*candidate, url string) (pin string, recorded []string) {
	seen := map[string]bool{}
	for _, c := range ready {
		if c.src.URL != url || seen[c.src.Ref] {
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

// pinName is how a pin reads in a message; the lock file wrote it, so it is
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

// reportAdopted emits one event per entry the run covered, in name order,
// and prints what it did.
func (inv *invocation) reportAdopted(run *adoptRun, covered []*candidate, nothing string) {
	for _, c := range covered {
		inv.out.emit(c.eventOf())
		if c.state != adoptAdopted {
			continue
		}
		v := c.imported
		inv.out.done("adopted " + inv.out.paint(heading, sanitised(c.entry.Name)) + " from " + inv.out.paint(heading, v.imp.Source) +
			underShown(v.imp.Path) + " at " + short(v.imp.Commit))
		inv.out.print("  ", inv.out.paint(muted, c.baseLine()))
	}
	if nothing != "" {
		inv.out.print(strings.ToUpper(nothing[:1]) + nothing[1:] + ".")
		inv.summary = nothing
		return
	}
	inv.summary = adoptSummary(run.done)
}

// baseLine says what the library directory holds against the base the
// adoption recorded: the whole point of the command, since a directory that
// differs is a modification of that version and not a version of its own.
func (c *candidate) baseLine() string {
	line := "base " + short(c.imported.hash) + ": " + c.base.how
	if c.modified {
		line += "; the directory differs from it, so what differs is a local modification"
	}
	return line
}

// adoptSummary is what the result event says the run did.
func adoptSummary(done []*candidate) string {
	if len(done) == 0 {
		return "nothing was adopted"
	}
	names := make([]string, 0, len(done))
	modified := 0
	for _, c := range done {
		names = append(names, c.entry.Name)
		if c.modified {
			modified++
		}
	}
	summary := "adopted " + strings.Join(names, ", ")
	if len(done) == 1 {
		v := done[0].imported
		summary += " from " + v.imp.Source + underPath(v.imp.Path) + " at " + short(v.imp.Commit)
	} else {
		summary += " from the vercel skills lock file"
	}
	if modified > 0 {
		summary += fmt.Sprintf(", %s modified since it was installed", plural(modified, "skill"))
	}
	return summary
}

// eventOf is the adoption event of one candidate.
func (c *candidate) eventOf() adoptionEvent {
	ev := adoptionEvent{event: newEvent("adoption"), Name: c.entry.Name, Lock: c.entry.File,
		State: c.state, ContentHash: c.hash, Reason: c.reason}
	if c.src.URL != "" {
		subpath := c.subpath()
		ev.Source, ev.Subpath = c.src.URL, &subpath
	}
	if c.imported != nil {
		modified := c.modified
		ev.UpstreamCommit, ev.BaseHash, ev.Modified = c.imported.imp.Commit, c.imported.hash, &modified
	}
	return ev
}

// previewAdoption reports what an adoption would do and changes nothing.
func (inv *invocation) previewAdoption(cands []*candidate, entries int) {
	out := inv.out
	if len(cands) == 0 {
		out.print("No skill of the vercel skills lock file is in the library at ", out.paint(label, inv.dirs.Library), ".")
		inv.summary = "no skill of the vercel skills lock file is in the library"
		return
	}
	counts := map[string]int{}
	out.print(out.paint(heading, plural(len(cands), "skill")), " of the vercel skills lock file in the library")
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
	inv.summary = previewSummary(counts, entries)
}

// rowOf is one row of the preview listing. Every field the lock file
// supplied is sanitised: it is text agentx did not write.
func (a *candidate) rowOf() []cell {
	state := c(a.state, muted)
	switch a.state {
	case adoptCandidate:
		state = c(a.state, okStyle)
	case adoptRefused:
		state = c(a.state, warnStyle)
	}
	where := c("(none)", muted)
	if a.src.URL != "" {
		text := a.src.URL
		if a.subpath() != "" {
			text += "/" + a.subpath()
		}
		where = c(sanitised(text), plain)
	}
	return []cell{c("  "+sanitised(a.entry.Name), heading), state, where, c(sanitised(a.reason), muted)}
}

// previewSummary is what the result event says a preview found.
func previewSummary(counts map[string]int, entries int) string {
	parts := []string{fmt.Sprintf("%s of the vercel skills lock file in the library", plural(counts[adoptCandidate]+counts[adoptManaged]+counts[adoptRefused], "skill"))}
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
	if entries > counts[adoptCandidate]+counts[adoptManaged]+counts[adoptRefused] {
		parts = append(parts, fmt.Sprintf("%d not in the library", entries-counts[adoptCandidate]-counts[adoptManaged]-counts[adoptRefused]))
	}
	return strings.Join(parts, ", ")
}

// writeAdoptions writes the import commits of the established versions and
// points the import branches at them as one journaled mutation, under the
// exclusive lock, with every input read again under it. Nothing but refs
// changes: the library directories are the user's and stay as they are.
func (inv *invocation) writeAdoptions(ctx context.Context, run *adoptRun, ready []*candidate) error {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	if !exists {
		return fail(exitNotFound, "no source has been added on this machine", "run 'agentx source add <url>' for the source the skills came from")
	}
	versions := make([]lineage.Version, 0, len(ready))
	for _, c := range ready {
		versions = append(versions, c.imported.version())
	}
	runID := lineage.NewRun()
	commits, trees, err := lineage.WriteAll(ctx, inv.git, gitDir, runID, versions)
	if err != nil {
		return accountRepoFailure(err)
	}
	for i, c := range ready {
		c.imported.commit, c.imported.tree = commits[i], trees[i]
	}
	journaled := false
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		records, err := lineage.List(ctx, inv.git, gitDir)
		if err != nil {
			return accountRepoFailure(err)
		}
		m := home.NewMutation(inv.dirs.Home)
		for _, c := range ready {
			f, err := inv.stageAdoption(m, gitDir, c, records)
			switch {
			case err != nil:
				m.Discard()
				return err
			case f != nil:
				run.drop(c, f)
				continue
			}
			run.done = append(run.done, c)
		}
		if len(run.done) == 0 {
			m.Discard()
			return errNothingAdopted
		}
		journaled = !m.Empty()
		return m.Apply(inv.refs(ctx))
	})
	if err != nil && !journaled {
		// Nothing was written that recovery could need, so the run takes its
		// staging refs with it and leaves the account repo as it was.
		inv.dropImporting(ctx, gitDir, runID, len(versions))
	} else if err == nil {
		inv.dropImporting(ctx, gitDir, runID, len(versions))
	}
	if errors.Is(err, errNothingAdopted) {
		run.done = nil
		return nil // every skill was refused; the run answers for them
	}
	if err != nil {
		run.done = nil
		return mutationFailure(err)
	}
	for _, c := range run.done {
		c.state, c.reason = adoptAdopted, ""
		run.step(phaseAdopt, c.entry.Name)
	}
	return nil
}

// errNothingAdopted ends the mutation of a run whose every skill was
// refused under the lock, so that the lock is released without the version
// file being rewritten: nothing changed, and nothing watching agentx home
// has anything to read again.
var errNothingAdopted = errors.New("no skill of the run could be adopted")

// stageAdoption records the one thing an adoption changes, the import
// branch, after reading under the lock what the branch and the library
// directory hold now. A base established from what the directory holds is
// checked again here: a directory edited between the read and the lock
// would otherwise be adopted at a version it no longer holds.
func (inv *invocation) stageAdoption(m *home.Mutation, gitDir string, c *candidate, records map[string]lineage.Record) (*failure, error) {
	exists, dir, err := libraryEntry(c.path)
	if err != nil {
		return nil, err
	}
	// What the event reports of the directory is what this read found,
	// refused or not: content_hash is what the library holds now, and a
	// candidate this leaves unadopted has no base version to be modified
	// against.
	if !exists || !dir {
		c.hash, c.imported = "", nil
		return refuse(exitRefused, c.path+" is no longer a directory the library holds",
			"run 'agentx adopt' again to see what is there now"), nil
	}
	hash := contentHashAt(c.path)
	if c.base.confirm && hash != c.imported.hash {
		c.hash, c.imported = hash, nil
		return refuse(exitRefused, c.path+" changed while it was being adopted",
			"run 'agentx adopt' again: its base version has to be established from what the directory holds now"), nil
	}
	// Modified is what skill list will say of the skill: the directory's
	// tree against the import tree, modes and links included.
	c.hash, c.modified = hash, !holdsVersion(c.path, c.imported)
	create, f := refPlan(c.imported, records, c.path, false)
	if f != nil {
		return f, nil
	}
	if create {
		m.Ref(gitDir, lineage.ManagedRef(c.entry.Name), "", c.imported.commit)
	}
	return nil, nil
}
