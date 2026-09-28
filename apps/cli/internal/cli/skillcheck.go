package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

func newSkillCheckCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Look for newer upstream versions of the managed skills",
		Long: "Fetch every added source a managed skill came from and report which skills have a\n" +
			"newer upstream version, with the files each one changes; skills from a source you\n" +
			"removed are skipped. Nothing is applied: read an update with\n" +
			"'agentx skill diff <name> --upstream'.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return inv.skillCheck(cmd.Context()) },
	}
}

// updateAvailableEvent is one managed skill whose source holds a newer
// version than its base version: where it came from, the version it is at,
// the version the check pinned as its candidate and every file that
// version changes, each path relative to the skill's directory. The serve
// child emits the same event, carrying its instance id.
type updateAvailableEvent struct {
	event
	InstanceID              string       `json:"instance_id,omitempty"` // serve only
	Name                    string       `json:"name"`
	Kind                    string       `json:"kind"`
	Source                  string       `json:"source"`
	Subpath                 string       `json:"subpath"`
	BaseHash                string       `json:"base_hash"`
	UpstreamCommit          string       `json:"upstream_commit"`
	Candidate               string       `json:"candidate"`
	CandidateHash           string       `json:"candidate_hash"`
	CandidateUpstreamCommit string       `json:"candidate_upstream_commit"`
	Files                   []updateFile `json:"files"`
	UpstreamName            string       `json:"upstream_name,omitempty"` // present when the upstream names the skill otherwise
}

// updateFile is one file an update changes, with what it does to it, in
// the words of a diff event.
type updateFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// verdict is what a check found for one managed skill in the source commit
// it fetched.
type verdict int

const (
	verdictCurrent verdict = iota // the source holds the base version, as an import would take it
	verdictUpdate                 // the source holds another version, which the candidate pins
	verdictRemoved                // the source holds no skill at the skill's subpath
	verdictPresent                // the source holds the skill, but its newer version was not read or cannot be imported
)

// finding is one managed skill's verdict, with what the check writes for it
// under the lock: nothing for a skill whose branch no longer names tip, the
// import commit the comparison was made against, since that skill was
// compared with something it no longer is.
type finding struct {
	name      string
	source    string // the canonical URL of the source the skill came from
	tip       string
	verdict   verdict
	v         *imported // verdictUpdate: the version the candidate is the import commit of
	candidate string    // verdictUpdate: that commit
	marker    string    // verdictRemoved: the source commit that holds no skill at the subpath
}

// checkFailure is one thing a check could not check: a source it could not
// fetch or read, with the managed skills that came from it, or one skill
// whose newer version cannot be imported.
type checkFailure struct {
	source string   // the canonical URL of the source, the one the skill came from for a skill's failure
	skill  bool     // the failure is the one skill's that skills names, not its source's
	skills []string // the skills left unchecked, by name
	f      *failure
}

// warning is the line a failure is reported with, naming what it left
// unchecked: the source and its skills, or the skill.
func (cf checkFailure) warning() string {
	if cf.skill {
		return cf.skills[0] + ": " + cf.f.message
	}
	return cf.f.message + "; not checked: " + strings.Join(cf.skills, ", ")
}

// removedSkill is a managed skill whose source no longer holds it, with the
// source commit that does not.
type removedSkill struct {
	name, source, subpath, commit string
}

// checkReport is what one update check found and did, for skill check to
// print and for the serve child to emit.
type checkReport struct {
	idle     bool // no managed skill comes from a source this machine has, or every one the check set out to fetch was removed while it ran
	fetched  int  // the sources that were fetched and that the settings still hold
	checked  int  // the managed skills the check recorded a verdict for
	updates  []updateAvailableEvent
	removed  []removedSkill
	failures []checkFailure
	notes    []string // what a candidate the check moved leaves out or calls otherwise
}

// skillCheck is agentx skill check: the update check, printed, and every
// source or skill it could not check turned into the refusal a run over
// several sources answers with. What it could check is reported and pinned
// all the same.
func (inv *invocation) skillCheck(ctx context.Context) error {
	rep, err := inv.checkUpdates(ctx, false, true)
	if err != nil {
		return err
	}
	out := inv.out
	if rep.idle {
		inv.summary = "nothing to check: no managed skill comes from a source added on this machine"
		out.print("Nothing to check: no managed skill comes from a source added on this machine.")
		return nil
	}
	for _, cf := range rep.failures {
		out.warn(cf.warning())
	}
	for _, note := range rep.notes {
		out.warn(note)
	}
	for _, ev := range rep.updates {
		out.emit(ev)
	}
	inv.printCheck(rep)
	inv.summary = rep.summary()
	if len(rep.failures) > 0 {
		return checkRefusal(rep.failures)
	}
	return nil
}

// summary is the line that says what the check found, the one its text
// output starts with.
func (rep checkReport) summary() string {
	line := fmt.Sprintf("checked %s from %s: ", plural(rep.checked, "skill"), plural(rep.fetched, "source"))
	switch n := len(rep.updates); n {
	case 0:
		line += "no update available"
	case 1:
		line += "1 update available"
	default:
		line += fmt.Sprintf("%d updates available", n)
	}
	if n := len(rep.removed); n > 0 {
		line += fmt.Sprintf(", %d upstream removed", n)
	}
	return line
}

// printCheck writes the text of a check: the summary, then one line per
// skill with an update, the files it changes under it, and one line per
// skill whose upstream removed it. A check that fetched no source because
// every fetch failed has no summary to give, and says so through its
// warnings alone.
func (inv *invocation) printCheck(rep checkReport) {
	out := inv.out
	if rep.fetched > 0 || len(rep.failures) == 0 {
		out.done(rep.summary())
	}
	for _, ev := range rep.updates {
		out.print("  ", out.paint(heading, sanitised(ev.Name)), "  ", out.paint(warnStyle, updateAvailable), "  ",
			short(ev.UpstreamCommit), " -> ", short(ev.CandidateUpstreamCommit), "  ", out.paint(noteStyle, plural(len(ev.Files), "file")))
		t := &table{}
		for _, f := range ev.Files {
			t.add(c("    "+f.Status, muted), c(quotedPath(f.Path), plain))
		}
		out.render(t, "")
	}
	for _, r := range rep.removed {
		where := r.source
		if r.subpath != "" {
			where += "/" + r.subpath
		}
		out.print("  ", out.paint(heading, sanitised(r.name)), "  ", out.paint(warnStyle, driftUpstreamRemoved), "  ",
			sanitised(where), " at ", short(r.commit))
	}
	if len(rep.updates) > 0 {
		out.print("Read an update with ", out.paint(label, "agentx skill diff <name> --upstream"), ".")
	}
}

// checkRefusal is how a check ends when something it set out to check could
// not be: the code every failure agrees on, as a fetch of several sources
// answers, and the source-level refusal when they disagree. It names every
// source with the skills it left unchecked, and every skill. The hint a
// fetch of several sources gives goes with it only when every failure is a
// source's, since a skill refused is none of what that hint says to check.
func checkRefusal(failures []checkFailure) error {
	st := failures[0].f.status
	sources := true
	for _, cf := range failures {
		if cf.f.status != st {
			st = exitSource
		}
		sources = sources && !cf.skill
	}
	if len(failures) == 1 {
		return fail(st, "could not check "+failures[0].warning(), failures[0].f.hint)
	}
	named := make([]string, len(failures))
	for i, cf := range failures {
		named[i] = cf.skills[0]
		if !cf.skill {
			named[i] = cf.source + " (" + strings.Join(cf.skills, ", ") + ")"
		}
	}
	hint := "the warnings name each failure"
	switch st {
	case exitSource, exitNotFound, exitAccountRepo: // what a run of several sources that failed that way says
		if sources {
			hint += "; " + refusalHint(st)
		}
	}
	return fail(st, "could not check "+strings.Join(named, ", "), hint)
}

// checkUpdates is the update check, which skill check runs and the serve
// child runs on its timer. It fetches every source a managed skill came
// from, in the user's git environment and outside the lock, compares each
// skill's base version with what its source holds now by tree id, writes
// the import commit of every newer version outside the lock, then records
// what it found in one mutation: the candidate and upstream-removed refs
// and last_fetched. Nothing is applied to the library. wait takes the lock
// as the serve child does, and progress reports a progress event per
// source.
//
// A source the settings no longer hold is not fetched and its skills are
// left as they are, candidate and marker included: nothing names it to
// fetch, and adding it again is the user's to decide. A source that could
// not be fetched, and a skill whose newer version cannot be imported, are
// failures in the report and cost nothing else: every other source is still
// fetched, compared and recorded.
func (inv *invocation) checkUpdates(ctx context.Context, wait, progress bool) (checkReport, error) {
	var rep checkReport
	s, err := inv.loadSettings()
	if err != nil {
		return rep, err
	}
	if len(s.Sources) == 0 { // nothing can be checked, and nothing is spawned to find that out
		rep.idle = true
		return rep, nil
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return rep, accountRepoFailure(err)
	}
	if !exists {
		rep.idle = true
		return rep, nil
	}
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		return rep, accountRepoFailure(fmt.Errorf("account repo %s: %w", gitDir, err))
	}
	bySource := inv.checkable(records, s)
	var targets []fetchTarget
	for _, entry := range s.Sources {
		if len(bySource[entry.URL]) > 0 {
			targets = append(targets, target(entry))
		}
	}
	if len(targets) == 0 {
		rep.idle = true
		return rep, nil
	}
	results, err := inv.fetchSources(ctx, gitDir, targets, wait, progress)
	if err != nil {
		return rep, err
	}
	run := &checkRun{inv: inv, gitDir: gitDir, owners: map[*imported][]lineage.Record{}}
	fetched := map[string]bool{}
	for _, res := range results {
		recs := bySource[res.Source.URL]
		if res.Err != nil {
			// What the fetch of that source alone would fail with; a lock or
			// a recovery is no one source's, and fetchSources has refused
			// those already.
			run.failures = append(run.failures, checkFailure{source: res.Source.URL, skills: skillNamesOf(recs), f: fetchRefused(res)})
			continue
		}
		fetched[res.Source.URL] = true
		run.compare(ctx, res, recs)
	}
	importing := lineage.NewRun()
	if err := run.writeCandidates(ctx, importing); err != nil {
		return rep, err
	}
	live, added, moved, journaled, err := inv.recordCheck(ctx, gitDir, wait, run.findings, fetched, records)
	if !journaled || err == nil {
		// Recovery of a journal that could not be finished needs the
		// commits the staging refs hold; otherwise they are what the
		// candidate refs hold by now, or what no ref will ever hold.
		inv.dropImporting(ctx, gitDir, importing, len(run.versions))
	}
	if err != nil {
		return rep, err
	}
	// A source removed while the check ran is not one it checked, and
	// neither is a fetch or a read of it that failed: the removal takes the
	// source's remote and staging refs away, which is often what made it
	// fail, and the check leaves the skills of a removed source alone.
	for url := range fetched {
		if added[url] {
			rep.fetched++
		}
	}
	for _, cf := range run.failures {
		if added[cf.source] {
			rep.failures = append(rep.failures, cf)
		}
	}
	if rep.fetched == 0 && len(rep.failures) == 0 { // every source it set out to check was removed meanwhile
		rep.idle = true
		return rep, nil
	}
	checked := map[string]bool{} // the skills the check recorded a verdict for
	for _, fd := range run.findings {
		rec, ok := live[fd.name]
		if !ok || rec.Commit != fd.tip || !added[fd.source] {
			continue // the branch moved, or the source went, while the check ran: it recorded nothing for the skill
		}
		if fd.verdict == verdictPresent {
			continue // a failure names the skill as not checked; the write only cleared its marker
		}
		checked[fd.name] = true
		if fd.verdict == verdictRemoved {
			rep.removed = append(rep.removed, removedSkill{name: fd.name, source: rec.Import.Source, subpath: rec.Import.Path, commit: fd.marker})
		}
		if fd.verdict == verdictUpdate && moved[fd.name] {
			for _, d := range fd.v.dropped {
				rep.notes = append(rep.notes, path.Join(fd.v.skill.Subpath, d)+" is not a regular file and is left out of the update of "+fd.name)
			}
		}
	}
	rep.checked = len(checked)
	var announced []lineage.Record
	for name := range checked {
		if rec := live[name]; rec.Candidate != nil && rec.Candidate.HasImport {
			announced = append(announced, rec)
		}
	}
	sort.Slice(announced, func(i, j int) bool { return announced[i].Name < announced[j].Name })
	sort.Slice(rep.removed, func(i, j int) bool { return rep.removed[i].name < rep.removed[j].name })
	if rep.updates, err = inv.describeCandidates(ctx, gitDir, announced); err != nil {
		return rep, err
	}
	for _, ev := range rep.updates {
		if ev.UpstreamName != "" && moved[ev.Name] {
			rep.notes = append(rep.notes, fmt.Sprintf("%s: the update names the skill %q; updating it keeps the name %s", ev.Name, ev.UpstreamName, ev.Name))
		}
	}
	return rep, nil
}

// checkable are the managed skills an update check covers, by the canonical
// URL of their source, each source's by name: every managed skill whose
// lineage agentx can read, whose source the settings hold and whose
// directory the library holds. One whose source was removed is left as it
// is, since nothing names its source to fetch; one whose directory is gone
// has nothing to update, and skill list names it in a warning instead. A
// fork is not checked: its base version is the last one merged into it,
// which its own history holds and this check does not read.
func (inv *invocation) checkable(records map[string]lineage.Record, s home.Settings) map[string][]lineage.Record {
	added := sourceURLs(s)
	bySource := map[string][]lineage.Record{}
	for _, rec := range records {
		if rec.Kind != lineage.KindManaged || !rec.HasImport || !added[rec.Import.Source] || !inv.holdsSkill(rec.Name) {
			continue
		}
		bySource[rec.Import.Source] = append(bySource[rec.Import.Source], rec)
	}
	for _, recs := range bySource {
		sort.Slice(recs, func(i, j int) bool { return recs[i].Name < recs[j].Name })
	}
	return bySource
}

// holdsSkill reports whether the library holds a skill directory called
// name, one with a SKILL.md, the rule a listing lists a skill by. It reads
// one file's metadata and runs no git.
func (inv *invocation) holdsSkill(name string) bool {
	info, err := os.Stat(filepath.Join(inv.libraryPath(name), "SKILL.md"))
	return err == nil && info.Mode().IsRegular()
}

// skillNamesOf are the names of recs, in their order.
func skillNamesOf(recs []lineage.Record) []string {
	out := make([]string, len(recs))
	for i, rec := range recs {
		out[i] = rec.Name
	}
	return out
}

// checkRun is what one check collects between its fetches and its locked
// write: what it found for each skill, the versions it writes candidates
// for with the skills each one is for, and what it could not check.
type checkRun struct {
	inv      *invocation
	gitDir   string
	findings []finding
	versions []*imported
	owners   map[*imported][]lineage.Record
	failures []checkFailure
}

// compare decides what the commit a fetch brought holds for every managed
// skill of that source, by tree id and never by reading a file.
//
// A skill whose directory, or whose SKILL.md, the commit does not hold is
// upstream removed. One whose directory's tree is the base version's, the
// tree the import branch holds under the upstream's directory name, is
// current: the listing of the fetch carries every skill directory's tree,
// and the lineage the tree of every branch, so this costs no git at all,
// however many skills the source has.
//
// The others are read the way an install reads them, the whole commit
// listed once and one history walk for all of them, and the import tree
// each would get is computed in process: a directory whose only difference
// is what an import leaves out, a symlink or a submodule, has the base
// version's import tree, and is current too. What is left differs in what
// an import keeps, and its files are read, fetching the blobs this machine
// does not hold in one batch, so that the candidate is the import commit an
// install of that version writes.
func (c *checkRun) compare(ctx context.Context, res source.Result, recs []lineage.Record) {
	listing, src := res.Listing, res.Source
	bySubpath := map[string][]lineage.Record{}
	var differing []source.Skill
	for _, rec := range recs {
		tree, ok := listing.Trees[rec.Import.Path]
		switch {
		case !ok:
			c.found(rec, verdictRemoved, func(fd *finding) { fd.marker = listing.Commit })
		case treeid.Wrap(rec.Import.Dir(), tree) == rec.Tree:
			c.found(rec, verdictCurrent, nil)
		default:
			if _, seen := bySubpath[rec.Import.Path]; !seen {
				differing = append(differing, source.Skill{Subpath: rec.Import.Path, Name: rec.Name, Tree: tree})
			}
			bySubpath[rec.Import.Path] = append(bySubpath[rec.Import.Path], rec)
		}
	}
	if len(differing) == 0 {
		return
	}
	// A read that fails leaves every skill it was reading for unchecked,
	// and the source's other skills stand: their verdicts need nothing it
	// would have read. The source still holds every skill it was reading
	// for, so none of them is upstream removed any longer.
	unchecked := func(subpaths []string, err error) {
		var f *failure
		if !errors.As(err, &f) {
			f = refuse(exitAccountRepo, err.Error(), "run 'agentx doctor' and check the account repo it names")
		}
		var left []string
		for _, p := range subpaths {
			for _, rec := range bySubpath[p] {
				c.found(rec, verdictPresent, nil)
			}
			left = append(left, skillNamesOf(bySubpath[p])...)
		}
		sort.Strings(left)
		c.failures = append(c.failures, checkFailure{source: src.URL, skills: left, f: f})
	}
	versions, missing, err := c.inv.listVersions(ctx, c.gitDir, src, listing.Commit, differing)
	if err != nil {
		var subpaths []string
		for _, sk := range differing {
			subpaths = append(subpaths, sk.Subpath)
		}
		unchecked(subpaths, err)
		return
	}
	var wanted []*imported
	for _, v := range versions {
		tree := v.version().ImportTree()
		var stale []lineage.Record
		for _, rec := range bySubpath[v.skill.Subpath] {
			if tree == rec.Tree {
				c.found(rec, verdictCurrent, nil)
			} else {
				stale = append(stale, rec)
			}
		}
		if len(stale) == 0 {
			continue
		}
		if f := c.inv.importable(v, nil, src); f != nil {
			c.refused(stale, f)
			continue
		}
		c.owners[v] = stale
		wanted = append(wanted, v)
	}
	if len(wanted) == 0 {
		return
	}
	ready, err := c.inv.fillVersions(ctx, c.gitDir, src, wanted, missing, func(v *imported, f *failure) {
		if f != nil {
			c.refused(c.owners[v], f)
		}
	})
	if err != nil {
		var subpaths []string
		for _, v := range wanted {
			subpaths = append(subpaths, v.skill.Subpath)
		}
		unchecked(subpaths, err)
		return
	}
	c.versions = append(c.versions, ready...)
}

// found records the verdict for one skill against the tip it was compared
// with; set fills in what the verdict needs besides.
func (c *checkRun) found(rec lineage.Record, v verdict, set func(*finding)) {
	fd := finding{name: rec.Name, source: rec.Import.Source, tip: rec.Commit, verdict: v}
	if set != nil {
		set(&fd)
	}
	c.findings = append(c.findings, fd)
}

// refused records that the newer version of every skill of recs cannot be
// imported, for the reason f gives. Nothing about that version is recorded
// for them, and a candidate an earlier check pinned stays where it is; the
// source holds each of them all the same, so an upstream-removed marker
// goes.
func (c *checkRun) refused(recs []lineage.Record, f *failure) {
	for _, rec := range recs {
		c.found(rec, verdictPresent, nil)
		c.failures = append(c.failures, checkFailure{source: rec.Import.Source, skill: true, skills: []string{rec.Name}, f: f})
	}
}

// writeCandidates writes the import commit of every newer version the check
// found, all of them through one fast-import onto the staging refs of run,
// outside the lock, as an install writes its own. Each one is then the
// verdict of every skill it is newer than.
func (c *checkRun) writeCandidates(ctx context.Context, run string) error {
	if len(c.versions) == 0 {
		return nil
	}
	list := make([]lineage.Version, len(c.versions))
	for i, v := range c.versions {
		list[i] = v.version()
	}
	commits, trees, err := lineage.WriteAll(ctx, c.inv.git, c.gitDir, run, list)
	if err != nil {
		return accountRepoFailure(err)
	}
	for i, v := range c.versions {
		v.commit, v.tree = commits[i], trees[i]
		for _, rec := range c.owners[v] {
			c.found(rec, verdictUpdate, func(fd *finding) { fd.v, fd.candidate = v, v.commit })
		}
	}
	return nil
}

// recordCheck writes what the check found in one mutation under the lock,
// the only hold of it after the network, and returns the lineage as the
// mutation left it, the sources the settings hold once the fetches are
// done, and the skills whose candidate it moved.
//
// The lineage is read again under the lock. A skill whose branch no longer
// names the import commit it was compared with, or that is gone, gets
// nothing: it was compared with something it no longer is. Every other
// skill gets its candidate ref and its upstream-removed marker, each
// written, moved or deleted with the value it holds now as its expected old
// value, so that a ref something else moved is refused rather than
// overwritten: an update of the skill's own, or a removal, deletes the
// candidate with its value just the same. The settings get last_fetched for
// every source that fetched, and the write bumps the version file once.
func (inv *invocation) recordCheck(ctx context.Context, gitDir string, wait bool, findings []finding, fetched map[string]bool, records map[string]lineage.Record) (
	live map[string]lineage.Record, added, moved map[string]bool, journaled bool, err error,
) {
	live, moved = records, map[string]bool{}
	if len(fetched) == 0 {
		// Every fetch failed: nothing was compared, and there is nothing to
		// write. The sources the settings hold are read again all the same,
		// a plain read of a file only ever replaced whole, so that a source
		// removed while its fetch ran is not reported as one that failed.
		s, err := inv.loadSettings()
		if err != nil {
			return nil, nil, nil, false, err
		}
		return live, sourceURLs(s), moved, false, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	err = inv.holdLock(ctx, wait, true, func() error {
		var err error
		if live, err = lineage.List(ctx, inv.git, gitDir); err != nil {
			return accountRepoFailure(fmt.Errorf("account repo %s: %w", gitDir, err))
		}
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		m := home.NewMutation(inv.dirs.Home)
		stamped := make(map[string]bool, len(fetched))
		for url := range fetched {
			stamped[url] = true
		}
		if err := inv.stampFetched(ctx, m, gitDir, s, stamped, now); err != nil {
			m.Discard()
			return err
		}
		added = sourceURLs(s)
		sort.Slice(findings, func(i, j int) bool { return findings[i].name < findings[j].name })
		for _, fd := range findings {
			rec, ok := live[fd.name]
			if !ok || rec.Kind != lineage.KindManaged || rec.Commit != fd.tip || !added[fd.source] {
				continue
			}
			candidate, marker := rec.CandidateCommit(), rec.UpstreamRemoved
			setCandidate, setMarker := "", ""
			switch fd.verdict {
			case verdictUpdate:
				setCandidate = fd.candidate
			case verdictRemoved:
				setMarker = fd.marker
			case verdictPresent: // the candidate stays as it is, whatever else was found for the skill
				setCandidate = candidate
			}
			if candidate != setCandidate {
				m.Ref(gitDir, lineage.CandidateRef(fd.name), candidate, setCandidate)
				rec.Candidate = nil
				if setCandidate != "" {
					rec.Candidate = &lineage.Candidate{Commit: setCandidate, Tree: fd.v.tree, Import: fd.v.imp, HasImport: true}
					moved[fd.name] = true
				}
			}
			if marker != setMarker {
				m.Ref(gitDir, lineage.UpstreamRemovedRef(fd.name), marker, setMarker)
				rec.UpstreamRemoved = setMarker
			}
			live[fd.name] = rec
		}
		applied := m.Apply(inv.refs(ctx))
		journaled = m.Journaled()
		return applied
	})
	if err != nil {
		return nil, nil, nil, journaled, mutationFailure(err)
	}
	return live, added, moved, journaled, nil
}

// describeCandidates builds the update_available event of every record,
// each holding a candidate, in the order given: the two versions, and what
// the candidate does to each file of the base version. Both reads cover
// every candidate at once, so that a check that finds a hundred updates
// costs what one that finds one costs: one diff-tree reading every pair of
// trees from its standard input, each base version's against its
// candidate's, and one cat-file of every candidate's SKILL.md, whose name
// is the one an install of that version would take.
func (inv *invocation) describeCandidates(ctx context.Context, gitDir string, recs []lineage.Record) ([]updateAvailableEvent, error) {
	if len(recs) == 0 {
		return nil, nil
	}
	var pairs, blobs strings.Builder
	seen := map[string]bool{}
	for _, rec := range recs {
		if pair := rec.Tree + " " + rec.Candidate.Tree; !seen[pair] {
			seen[pair] = true
			pairs.WriteString(pair + "\n")
		}
		blobs.WriteString(rec.Candidate.Commit + ":" + rec.Candidate.Import.Dir() + "/SKILL.md\n")
	}
	out, err := inv.git.IsolatedInput(ctx, gitDir, strings.NewReader(pairs.String()), "diff-tree", "--stdin", "-r", "-z", "--no-renames", "--name-status")
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	changes, err := parsePairDiffs(out)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	out, err = inv.git.IsolatedInput(ctx, gitDir, strings.NewReader(blobs.String()), "cat-file", "--batch")
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	bodies, err := batchInOrder(out, len(recs))
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	events := make([]updateAvailableEvent, len(recs))
	for i, rec := range recs {
		c := rec.Candidate
		ev := updateAvailableEvent{
			event: newEvent("update_available"), Name: rec.Name, Kind: rec.Kind,
			Source: rec.Import.Source, Subpath: rec.Import.Path, BaseHash: rec.Import.Hash, UpstreamCommit: rec.Import.Commit,
			Candidate: c.Commit, CandidateHash: c.Import.Hash, CandidateUpstreamCommit: c.Import.Commit, Files: []updateFile{},
		}
		prefix := rec.Import.Dir() + "/"
		for _, f := range changes[rec.Tree+" "+c.Tree] {
			f.Path = strings.TrimPrefix(f.Path, prefix)
			ev.Files = append(ev.Files, f)
		}
		sort.Slice(ev.Files, func(i, j int) bool { return ev.Files[i].Path < ev.Files[j].Path })
		// The name an install of the version would give the skill: its
		// frontmatter's, else the upstream's own directory name.
		name, _, _ := scan.SkillFrontmatter(bodies[i])
		if name == "" {
			name = c.Import.Dir()
		}
		if name != rec.Name {
			ev.UpstreamName = name
		}
		events[i] = ev
	}
	return events, nil
}

// parsePairDiffs reads what diff-tree --stdin -z --name-status prints for
// pairs of trees: for each pair its two ids on a line of their own, then a
// status and a path per file, each ended by a NUL. A status is a capital
// letter and an object id starts with a lower-case hex digit, which is how
// the next pair is told from the next file.
func parsePairDiffs(out string) (map[string][]updateFile, error) {
	changes := map[string][]updateFile{}
	for out != "" {
		pair, rest, ok := strings.Cut(out, "\n")
		if !ok || strings.Count(pair, " ") != 1 {
			return nil, fmt.Errorf("git diff-tree: cannot read the pair of trees at %q", pair)
		}
		out = rest
		files := []updateFile{}
		for out != "" && !isHexDigit(out[0]) {
			status, rest, ok := strings.Cut(out, "\x00")
			if !ok {
				return nil, fmt.Errorf("git diff-tree: truncated status at %q", out)
			}
			file, rest, ok := strings.Cut(rest, "\x00")
			if !ok {
				return nil, fmt.Errorf("git diff-tree: truncated path after status %q", status)
			}
			f := updateFile{Path: file, Status: diffModified}
			switch status {
			case "A":
				f.Status = diffAdded
			case "D":
				f.Status = diffDeleted
			}
			files = append(files, f)
			out = rest
		}
		changes[pair] = files
	}
	return changes, nil
}

func isHexDigit(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' }

// batchInOrder reads cat-file --batch output for n requests into their
// bodies, in the order they were asked for: an object that is missing,
// which names no object this request could find, has an empty body.
func batchInOrder(out string, n int) ([]string, error) {
	bodies := make([]string, 0, n)
	for out != "" {
		header, rest, ok := strings.Cut(out, "\n")
		if !ok {
			return nil, fmt.Errorf("git cat-file: truncated output at %q", header)
		}
		fields := strings.Fields(header)
		size := -1
		if len(fields) == 3 {
			if v, err := strconv.Atoi(fields[2]); err == nil {
				size = v
			}
		}
		if size < 0 { // "<name> missing", or another answer that carries no object
			bodies = append(bodies, "")
			out = rest
			continue
		}
		if size > len(rest) {
			return nil, fmt.Errorf("git cat-file: truncated object %s", fields[0])
		}
		bodies = append(bodies, rest[:size])
		out = strings.TrimPrefix(rest[size:], "\n")
	}
	if len(bodies) != n {
		return nil, fmt.Errorf("git cat-file answered %d of %d requests", len(bodies), n)
	}
	return bodies, nil
}
