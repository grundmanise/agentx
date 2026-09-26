package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

func newSkillUpdateCommand(inv *invocation) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "update [<name>]",
		Short: "Apply the update the last check found to a managed skill",
		Long: "Replace the library directory of a managed skill with the newer upstream version\n" +
			"'agentx skill check' found for it, and record that version as the one the skill\n" +
			"is at. A copy placement that holds the version replaced is refreshed; a copy\n" +
			"edited on its own is kept and named. Pass --all instead of a name to update every\n" +
			"managed skill the last check found an update for. A skill edited since it was\n" +
			"installed is not updated: 'agentx skill diff <name>' shows the edits and\n" +
			"'agentx skill revert <name>' discards them. Read an update before you apply it\n" +
			"with 'agentx skill diff <name> --upstream'.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			const hint = "name the skill to update, or run 'agentx skill update --all' to update every skill the last check found an update for"
			switch {
			case all && len(args) > 0:
				return fail(exitUsage, "skill update takes a skill name or --all, not both", hint)
			case !all && len(args) == 0:
				return fail(exitUsage, "no skill to update", hint)
			}
			name := ""
			if !all {
				name = args[0]
			}
			return inv.skillUpdate(cmd.Context(), name)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "update every managed skill the last check found an update for")
	return cmd
}

// errEdited marks the refusal of a managed skill that was edited since it
// was installed, which a run over every skill skips rather than counts.
var errEdited = errors.New("edited")

// errNothingApplied ends the hold of the lock of a run whose every skill
// was refused under it, so that the version file is not bumped for a
// mutation that changed nothing. It never leaves the run.
var errNothingApplied = errors.New("nothing applied")

// updating is one skill an update applies its candidate to: what was read
// of it before the lock, to be read again under it, and what the mutation
// did to its copies.
type updating struct {
	name         string
	rec          lineage.Record // the import branch at the version the library holds
	next         lineage.Record // the import branch as the update leaves it, at the candidate
	libPath      string
	captured     string       // the library entry when the run began, in the words a journal records
	held         string       // the tree id of what the library directory holds, which is the base version's
	base         lineage.Base // the candidate's version, the one laid out in the library
	upstreamName string       // the name the candidate's SKILL.md gives the skill, when it is not name
	done         placements   // the copies the mutation refreshed or kept
}

// updateRun is one run of agentx skill update, for one name or for --all:
// the skills it applies an update to, those it skipped as modified and
// those it gave up on, each reported on its own while the run goes on.
type updateRun struct {
	refusals
	inv      *invocation
	gitDir   string
	all      bool
	selected int               // the skills the run set out to update
	edited   []string          // the skills --all skipped as modified, by name
	ready    []*updating       // the skills judged ready to update, by name
	bodies   map[string]string // what the files of every version staged hold, by blob id
	applied  []*updating       // the skills whose update the mutation applied, by name
}

// refuse gives up on one skill. A run over every skill says so in a
// warning naming it and goes on with the rest; the error and the result
// name every skill it gave up on, see refusals.
func (r *updateRun) refuse(name string, f *failure) {
	r.add(name, f)
	if r.all && r.selected > 1 {
		r.inv.out.warn(name + ": " + f.message)
	}
}

// failure is how the run answers for the skills it gave up on: nil when it
// gave up on none, which a typed nil would hide from the caller.
func (r *updateRun) failure() error {
	if f := r.refusals.failure(r.selected, len(r.applied)); f != nil {
		return f
	}
	return nil
}

// skillUpdate applies the update candidate of the managed skill called
// name, or with an empty name the candidate of every managed skill that
// has one, to a skill that was not edited since it was installed. That
// skill updates by replacement, one journaled mutation for the whole run:
// the import branch moves from the version the library holds to the
// candidate, the library directory is retained and replaced by the
// candidate's version laid out beside it, the copies that held the version
// replaced are refreshed with it, and the candidate ref is deleted last.
//
// Everything the mutation replaces is read before the lock and again under
// it: the import branch, the candidate, the settings entry of the source
// and the library directory, whose content is captured as the fingerprint
// the journal compares. A change to any of them in between refuses the
// skill rather than updating something nobody judged, so an edit made
// meanwhile is never replaced with the rest; the journal's remove step
// carries the same fingerprint, and the content it retains is dropped only
// when it still hashes to it.
//
// A modified skill is refused, and skipped with a warning by --all: its
// update would have to merge the edit into the newer version, which is not
// offered yet, and replacing it would discard the edit.
func (inv *invocation) skillUpdate(ctx context.Context, name string) error {
	r := &updateRun{
		refusals: refusals{verb: "updated", mixed: "run 'agentx skill list' to see which skills have an update, then update the rest one at a time"},
		inv:      inv,
		all:      name == "",
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	r.gitDir = gitDir
	records := map[string]lineage.Record{}
	if exists {
		if records, err = lineage.List(ctx, inv.git, gitDir); err != nil {
			return accountRepoFailure(fmt.Errorf("account repo %s: %w", gitDir, err))
		}
	}
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	sources := sourceURLs(s)

	names, libs := r.selection(name, records)
	r.selected = len(names)
	if r.all && len(names) == 0 {
		inv.summary = "nothing to update: no managed skill has an update as of the last update check; run 'agentx skill check' to look again"
		inv.out.print("Nothing to update: no managed skill has an update as of the last update check. Run ",
			inv.out.paint(label, "agentx skill check"), " to look again.")
		return nil
	}

	for _, n := range names {
		lib, held := libs[n]
		rec, managed := records[n]
		u, f := inv.judgeUpdate(n, rec, managed, lib, held, sources)
		switch {
		case f != nil && r.all && errors.Is(f, errEdited):
			r.edited = append(r.edited, n)
			inv.out.warnWith(n+" was edited since it was installed, so it was not updated: "+mergeNotYet,
				"run '"+skillCommand("diff", n)+"' to see the edits, or '"+skillCommand("revert", n)+"' to discard them and update it")
		case f != nil:
			r.refuse(n, f)
		case u == nil: // a name the last check found no update for, which only a run of one name asks about
			inv.summary = n + " is up to date as of the last update check; run 'agentx skill check' to look again"
			inv.out.print(inv.out.paint(heading, sanitised(n)), " is up to date as of the last update check; run ",
				inv.out.paint(label, "agentx skill check"), " to look again")
			return nil
		default:
			r.ready = append(r.ready, u)
		}
	}
	if err := r.read(ctx); err != nil {
		return err
	}
	if len(r.ready) > 0 {
		if err := r.apply(ctx); err != nil {
			return err
		}
	}
	if len(r.applied) > 0 {
		if err := r.report(ctx); err != nil {
			return err
		}
	} else if r.all && len(r.broken) == 0 {
		inv.summary = "no skill was updated, " + plural(len(r.edited), "modified skill") + " skipped"
		inv.out.print(inv.summary)
	}
	return r.failure()
}

// selection is the skills a run sets out to update, in name order, with
// what the library holds of each: the one name it was given, or with --all
// every managed skill with an update, a candidate agentx can read that its
// branch does not already hold (see lineage.Record.AtCandidate). A skill
// with no candidate has nothing to update, an upstream-removed one
// included, since the check that marks a skill deletes its candidate.
func (r *updateRun) selection(name string, records map[string]lineage.Record) ([]string, map[string]scan.LibrarySkill) {
	libs := map[string]scan.LibrarySkill{}
	if !r.all {
		if lib, ok := librarySkill(r.inv.dirs.Library, name); ok {
			libs[name] = lib
		}
		return []string{name}, libs
	}
	var names []string
	for n, rec := range records {
		if _, ok := rec.AtCandidate(); ok && rec.Kind == lineage.KindManaged {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		libs = librarySkills(r.inv.dirs.Library)
	}
	return names, libs
}

// mergeNotYet is why a modified skill is not updated.
const mergeNotYet = "merging a modified skill with its update is not supported yet"

// judgeUpdate decides, before the lock, whether the skill called name can
// be updated, and reads what the mutation replaces. It refuses, in this
// order: a name the library does not hold and no lineage names, an
// unmanaged skill, a fork, a managed skill whose library directory is gone
// and one whose import branch agentx cannot read; a skill whose source was
// removed from this machine, which is exit code 5 as installing from it
// is; a skill the last update check found its source no longer holds,
// which is kept as it is and never updated; and, having found an update
// for it, a skill that holds something git cannot record, which the update
// would discard with no record of it and which a revert refuses too, one
// that was edited since it was installed, and one whose library entry is a
// symlink. A skill with no update is neither: u and f are then both nil.
func (inv *invocation) judgeUpdate(name string, rec lineage.Record, managed bool, lib scan.LibrarySkill, held bool, sources map[string]bool) (*updating, *failure) {
	again := "run '" + skillCommand("update", name) + "' again"
	switch {
	case !held && !managed:
		return nil, failureOf(inv.noLibrarySkill(name))
	case !managed:
		return nil, refuse(exitRefused, name+" is not managed by agentx, so it has no upstream to update from",
			"run 'agentx skill list' to see which skills are managed")
	case rec.Kind == lineage.KindFork:
		return nil, refuse(exitRefused, name+" is a fork on this machine",
			"a fork's versions are its own history; this command works on a managed skill")
	case !held:
		sc := skillContext{records: map[string]lineage.Record{name: rec}}
		what, wayOut := sc.absentNotice(inv, name)
		return nil, refuse(exitRefused, what+", so there is nothing to update", wayOut)
	case !rec.HasImport:
		return nil, refuse(exitRefused, fmt.Sprintf("the import branch %s records no version agentx can read", rec.Ref),
			"run 'agentx doctor' and check the account repo it names")
	case !sources[rec.Import.Source]:
		return nil, removedSourceRefusal(name, rec.Import.Source)
	case rec.UpstreamRemoved != "":
		return nil, upstreamRemovedRefusal(name)
	}
	next, ok := rec.AtCandidate()
	if !ok {
		return nil, nil
	}
	libPath := inv.libraryPath(name)
	captured, err := home.State(libPath)
	if err != nil {
		return nil, failureOf(libraryFailure(inv.dirs.Library, err))
	}
	tree, err := inv.readLibraryTree(lib.Path)
	if err != nil {
		return nil, failureOf(err)
	}
	if len(tree.Unrecordable) > 0 {
		return nil, unrecordableRefusal(name, libPath, tree.Unrecordable, "an update", "update")
	}
	if !rec.Current(tree) {
		return nil, refuse(exitRefused, name+" was edited since it was installed, and "+mergeNotYet,
			"run '"+skillCommand("diff", name)+"' to see the edits, or '"+skillCommand("revert", name)+"' to discard them and then '"+
				skillCommand("update", name)+"'").wrap(errEdited)
	}
	// A library entry that is a symlink leads to a directory of the user's.
	// The update replaces the entry itself, so it would drop the link and
	// leave the directory it led to as it was.
	if target, isLink := home.LinkTarget(captured); isLink {
		return nil, refuse(exitRefused, fmt.Sprintf("%s is a symlink to %s; an update replaces the library directory and would drop the link without touching the files it leads to", quotedPath(libPath), quotedPath(target)),
			"replace the link with the directory it points to, then "+again)
	}
	return &updating{name: name, rec: rec, next: next, libPath: libPath, captured: captured, held: tree.ID}, nil
}

// removedSourceRefusal refuses a skill whose source is gone from this
// machine, in the words the refusal of that source's id uses, and with the
// command that adds it again: the update the last check found for the
// skill stays, and applies once the source is back.
func removedSourceRefusal(name, url string) *failure {
	return refuse(exitNotFound, name+" was installed from "+url+", which was removed from this machine, so it is not updated",
		"run 'agentx source add "+sourceAddArg(url, "")+"' to add it again")
}

// upstreamRemovedRefusal refuses a skill the last update check found its
// source no longer holds: it is kept as it is and never updated.
func upstreamRemovedRefusal(name string) *failure {
	return refuse(exitRefused, "the last update check found that the source of "+name+" no longer holds it, so it is kept as it is and never updated",
		"run 'agentx skill check' once the source holds it again, or '"+skillCommand("remove", name)+"' to remove it")
}

// read reads, outside the lock, the version each skill is updated to: the
// entries of its candidate's upstream directory, one ls-tree each, and the
// files of all of them in one cat-file. A candidate that stores its
// version in a form an import does not write, which no library directory
// is current against, is refused for its skill alone: a check writes it
// again, as an install would write it.
func (r *updateRun) read(ctx context.Context) error {
	var ready []*updating
	var ids []string
	for _, u := range r.ready {
		base, err := lineage.ReadBase(ctx, r.inv.git, r.gitDir, u.next)
		if err != nil {
			r.refuse(u.name, failureOf(accountRepoFailure(err)))
			continue
		}
		if !u.next.Canonical(base) {
			r.refuse(u.name, refuse(exitAccountRepo, "the update candidate "+lineage.CandidateRef(u.name)+" stores its version in a form agentx does not write",
				"run 'agentx skill check' to pin the update again"))
			continue
		}
		u.base = base
		ids = append(ids, baseBlobs(base)...)
		ready = append(ready, u)
	}
	r.ready = ready
	if len(ready) == 0 {
		return nil
	}
	bodies, err := source.ReadBlobs(ctx, r.inv.git, r.gitDir, ids)
	if err != nil {
		return accountRepoFailure(err)
	}
	r.bodies = bodies
	for _, u := range ready {
		u.upstreamName = upstreamNameOf(u, bodies)
	}
	return nil
}

// upstreamNameOf is the name an install of the candidate's version would
// give the skill, its SKILL.md's frontmatter name or else the upstream's
// own directory name, when it is not the skill's library name, and ""
// otherwise. The skill keeps its library name, branch and placements
// through an update, and the SKILL.md is never rewritten.
func upstreamNameOf(u *updating, bodies map[string]string) string {
	name := ""
	for _, e := range u.base.Entries {
		if e.Path == "SKILL.md" && source.IsFileMode(e.Mode) {
			name, _, _ = scan.SkillFrontmatter(bodies[e.OID])
		}
	}
	if name == "" {
		name = u.next.Import.Dir()
	}
	if name == u.name {
		return ""
	}
	return name
}

// apply reads every skill's inputs again under the lock and applies the
// update of each one they still hold as one journaled mutation. A skill
// whose import branch, candidate, source or library directory changed since
// it was judged is refused on its own and the others go on; a run whose
// every skill was refused here writes no journal.
func (r *updateRun) apply(ctx context.Context) error {
	inv := r.inv
	var refs []string
	for _, u := range r.ready {
		refs = append(refs, lineage.ManagedRef(u.name), lineage.ForkRef(u.name), lineage.CandidateRef(u.name), lineage.UpstreamRemovedRef(u.name))
	}
	var staged []*updating
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		values, err := inv.git.Refs(ctx).RefValues(r.gitDir, refs)
		if err != nil {
			return accountRepoFailure(err)
		}
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		sources := sourceURLs(edit.s)
		var live []*updating
		for _, u := range r.ready {
			if f := inv.recheckUpdate(u, values, sources); f != nil {
				r.refuse(u.name, f)
				continue
			}
			live = append(live, u)
		}
		if len(live) == 0 {
			return errNothingApplied
		}
		// An update killed before its journal was written left what it
		// staged with nothing to name it: beside the library directory, and
		// beside each copy it was refreshing. All of it is swept before
		// anything is staged, since a sweep of a directory two
		// configurations share would take a sibling this plan staged a
		// moment earlier.
		targets := inv.detectedTargets()
		sweepStaged(inv.dirs.Library)
		swept := map[string]bool{}
		for _, u := range live {
			for _, t := range targets {
				if !t.readsLibrary && !swept[t.dir] && slices.Contains(edit.copiesOf(u.name), t.id) {
					swept[t.dir] = true
					sweepStaged(t.dir)
				}
			}
		}
		m := home.NewMutation(inv.dirs.Home)
		for _, u := range live {
			if err := inv.stageUpdate(m, r.gitDir, u, r.bodies, edit.copiesOf(u.name)); err != nil {
				m.Discard()
				return err
			}
		}
		staged = live
		return m.Apply(inv.refs(ctx))
	})
	switch {
	case errors.Is(err, errNothingApplied):
		return nil
	case err != nil:
		return mutationFailure(err)
	}
	r.applied = staged
	return nil
}

// recheckUpdate reads again, under the lock, everything the update of one
// skill replaces or depends on, and refuses the skill when any of it is no
// longer what judgeUpdate read: the import branch, which a fork of the name
// would supersede; the upstream-removed marker and the candidate, which a
// check may have written meanwhile; the settings entry of the source; and
// the library directory, whose content an edit made since it was captured
// would otherwise be replaced unseen.
func (inv *invocation) recheckUpdate(u *updating, values map[string]string, sources map[string]bool) *failure {
	name := u.name
	again := "run '" + skillCommand("update", name) + "' again"
	switch {
	case values[lineage.ManagedRef(name)] != u.rec.Commit || values[lineage.ForkRef(name)] != "":
		return refuse(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved while "+name+" was being updated, so nothing was changed", again)
	case values[lineage.UpstreamRemovedRef(name)] != "":
		return upstreamRemovedRefusal(name)
	case values[lineage.CandidateRef(name)] != u.next.Commit:
		return refuse(exitRefused, "the update candidate "+lineage.CandidateRef(name)+" moved while "+name+" was being updated, so nothing was changed",
			again+" to apply the update the last check found")
	case !sources[u.rec.Import.Source]:
		return removedSourceRefusal(name, u.rec.Import.Source)
	}
	live, err := home.State(u.libPath)
	if err != nil {
		return failureOf(libraryFailure(inv.dirs.Library, err))
	}
	if live != u.captured {
		return refuse(exitRefused, name+" changed while it was being updated, so nothing was changed",
			"run '"+skillCommand("diff", name)+"' to see the change; a skill edited since it was installed is not updated")
	}
	return nil
}

// stageUpdate records, under the lock, the steps of one skill's update, in
// the order the journal applies them: the import branch moved from the
// version the library holds to the candidate, with the tip read before the
// lock as its expected old value; the library directory retained and
// replaced by the candidate's version, laid out beside it and read back as
// git would record it; the copies that held the version replaced refreshed
// with the new one, see refreshCopies; and the candidate ref deleted, with
// the candidate as its expected old value, which the journal runs after
// every path step, so that a refusal on the way leaves it in place.
func (inv *invocation) stageUpdate(m *home.Mutation, gitDir string, u *updating, bodies map[string]string, recorded []string) error {
	target := u.base.ID()
	staged := m.Sibling(u.libPath, "staged")
	fingerprint, err := stageBase(staged, u.base, target, bodies)
	if err != nil {
		os.RemoveAll(staged)
		return libraryFailure(inv.dirs.Library, err)
	}
	u.done = placements{}
	m.Ref(gitDir, lineage.ManagedRef(u.name), u.rec.Commit, u.next.Commit)
	m.Remove(u.libPath, u.captured)
	m.Publish(u.libPath, staged, fingerprint)
	inv.refreshCopies(m, u.name, target, []string{u.held}, "", staged, recorded, &u.done)
	m.Ref(gitDir, lineage.CandidateRef(u.name), u.next.Commit, "")
	return nil
}

// report reads the machine again and reports every skill the run updated
// as it now stands, in name order: a warning for each whose new version
// names it otherwise, one library_skill event each, carrying every
// placement as skill list reports them, and the line that says what the
// run did.
func (r *updateRun) report(ctx context.Context) error {
	inv := r.inv
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	libs := librarySkills(inv.dirs.Library)
	for _, u := range r.applied {
		if u.upstreamName != "" {
			inv.out.warn(fmt.Sprintf("%s: the update names the skill %q; updating it keeps the name %s", u.name, u.upstreamName, u.name))
		}
	}
	for _, u := range r.applied {
		lib, ok := libs[u.name]
		if !ok {
			return fail(exitInternal, "the library holds no "+u.name+" after updating it", "run 'agentx doctor' and check the library it names")
		}
		inv.out.emit(sc.librarySkillEventFor(inv, snap, lib, nil))
	}
	out := inv.out
	if !r.all {
		u := r.applied[0]
		moved := " from " + short(u.rec.Import.Commit) + " to " + short(u.next.Import.Commit)
		plain, painted := copiesNote(out, len(u.done.copies), len(u.done.skipped))
		inv.summary = "updated " + u.name + moved + plain
		out.done("updated " + out.paint(heading, sanitised(u.name)) + moved + painted)
		return nil
	}
	refreshed, skipped := 0, 0
	for _, u := range r.applied {
		refreshed += len(u.done.copies)
		skipped += len(u.done.skipped)
	}
	note, painted := copiesNote(out, refreshed, skipped)
	inv.summary = "updated " + plural(len(r.applied), "skill") + note
	line := "updated " + plural(len(r.applied), "skill") + painted
	if n := len(r.edited); n > 0 {
		inv.summary += ", " + plural(n, "modified skill") + " skipped"
		line += ", " + out.paint(warnStyle, plural(n, "modified skill")+" skipped")
	}
	out.done(line)
	t := &table{}
	for _, u := range r.applied {
		notes, _ := copiesNote(out, len(u.done.copies), len(u.done.skipped))
		t.add(c(sanitised(u.name), heading), c(short(u.rec.Import.Commit)+" -> "+short(u.next.Import.Commit), plain), c(strings.TrimPrefix(notes, ", "), noteStyle))
	}
	out.render(t, "  ")
	return nil
}

// copiesNote is what an update did to copy placements, as its line and its
// result say it, whether of one skill or added up over a run of several:
// how many were refreshed and how many were skipped, each after a comma,
// or nothing when neither.
func copiesNote(out *writer, refreshed, skipped int) (plain, painted string) {
	if refreshed > 0 {
		note := plural(refreshed, "copy placement") + " refreshed"
		plain += ", " + note
		painted += ", " + out.paint(noteStyle, note)
	}
	if skipped > 0 {
		note := plural(skipped, "placement") + " skipped"
		plain += ", " + note
		painted += ", " + out.paint(warnStyle, note)
	}
	return plain, painted
}
