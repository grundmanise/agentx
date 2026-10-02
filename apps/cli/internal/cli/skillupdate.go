package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

func newSkillUpdateCommand(inv *invocation) *cobra.Command {
	var all, abort bool
	cmd := &cobra.Command{
		Use:   "update [<name>] [--abort]",
		Short: "Apply the update the last check found to a managed skill",
		Long: "Replace the library directory of a managed skill with the newer upstream version\n" +
			"'agentx skill check' found for it, and record that version as the one the skill\n" +
			"is at. A skill edited since it was installed keeps its edits: they are merged\n" +
			"into the newer version. When they conflict with it, the library is left as it is\n" +
			"and the conflicting files are listed: the merge waits, an ordinary Git merge in\n" +
			"progress in a Git worktree under agentx home, never in the library, for you to\n" +
			"resolve with git. Run the update again once it is resolved to apply it, or pass\n" +
			"--abort to give it up. Files git ignores in the skill, such as a .DS_Store or an\n" +
			"ignored build directory, are not edits, and stay. A copy placement that holds the\n" +
			"version replaced is refreshed; a copy edited on its own is kept and named. Pass\n" +
			"--all instead of a name to update every managed skill the last check found an\n" +
			"update for. Read an update before you apply it with\n" +
			"'agentx skill diff <name> --update'.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			const hint = "name the skill to update, or run 'agentx skill update --all' to update every skill the last check found an update for"
			switch {
			case all && len(args) > 0:
				return fail(exitUsage, "skill update takes a skill name or --all, not both", hint)
			case all && abort:
				return fail(exitUsage, "--abort gives up the merge of one skill, not of --all", "run 'agentx skill update <name> --abort' for each skill whose merge to give up")
			case !all && len(args) == 0:
				return fail(exitUsage, "no skill to update", hint)
			case abort:
				return inv.abortMerge(cmd.Context(), args[0])
			}
			name := ""
			if !all {
				name = args[0]
			}
			return inv.skillUpdate(cmd.Context(), name)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "update every managed skill the last check found an update for")
	cmd.Flags().BoolVar(&abort, "abort", false, "give up the merge pending for the skill; the library directory stays as it is")
	return cmd
}

// errSourceRemoved marks the refusal of a managed skill whose source was
// removed from this machine, which a run over every skill skips rather than
// counts, see updateRun.drop.
var errSourceRemoved = errors.New("source removed")

// errNothingApplied ends the hold of the lock of a run whose every skill
// was dropped under it, so that the version file is not bumped for a
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
	edited       bool         // the library directory is not the base version, so the update merges
	written      string       // the tree git wrote of the library directory, when it was edited
	ignored      []string     // the files git ignores in the library directory, which the new one keeps
	theirs       lineage.Base // the candidate's version
	base         lineage.Base // the version laid out in the library: the candidate's, or the merge of it with the edits
	target       version      // base, as the copies are compared with it
	placed       []version    // the versions a copy holds that agentx could have placed there, which the update refreshes
	merge        lineage.Merge
	mine         version       // merge.Mine, as the copies are compared with it
	merged       mergeResult   // what merge-tree made of merge; one that conflicts the update leaves pending
	checkout     string        // the checkout of the merge pending for the skill, which the update applies once it is resolved; "" for none
	conflict     conflictEvent // the conflicts of the merge left pending
	upstreamName string        // the name the candidate's SKILL.md gives the skill, when it is not name
	done         placements    // the copies the mutation refreshed or kept
}

// updateRun is one run of agentx skill update, for one name or for --all:
// the skills it applies an update to, those it skipped because their
// source was removed and those it gave up on, each reported on its own
// while the run goes on.
type updateRun struct {
	refusals
	inv        *invocation
	gitDir     string
	all        bool
	selected   int               // the skills the run set out to update
	sourceless []string          // the skills --all skipped because their source was removed, by name
	ready      []*updating       // the skills judged ready to update, by name
	bodies     map[string]string // what the files of every version staged hold, by blob id
	applied    []*updating       // the skills whose update the mutation applied, by name
	pending    []*updating       // the skills whose merge the mutation left pending, by name
}

// drop gives up on one skill. A run over every skill goes on with the rest,
// with a warning naming the skill when there are others it does not apply
// to; the error and the result name every skill it gave up on, see
// refusals.
//
// A skill whose source was removed from this machine is skipped by a run
// over every skill instead, whether that was found before the lock or
// under it: removing a source is the user's own choice, and the update a
// check found for the skill stays for as long as the source is gone, so
// counting it would fail every later run over every skill too. The warning
// says so and how to add the source again.
func (r *updateRun) drop(name string, f *failure) {
	if r.all && errors.Is(f, errSourceRemoved) {
		r.sourceless = append(r.sourceless, name)
		r.inv.out.warnWith(f.message, f.hint)
		return
	}
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
// has one, in one journaled mutation for the whole run. A skill that was
// not edited since it was installed updates by replacement: the import
// branch moves from the version the library holds to the candidate, the
// library directory is retained and replaced by the candidate's version
// laid out beside it, with the files git ignores in it kept, the copies
// that held the version replaced are refreshed with it, and the candidate
// ref is deleted last.
//
// A modified skill updates by a three-way merge, see updateRun.merge: the
// library directory as git records it, committed on its base version,
// merged with the candidate. A clean merge applies exactly as a
// replacement does, with the merged version laid out in place of the
// candidate's, and the skill stays modified against its new base. A merge
// that conflicts changes neither the library nor the import branch nor
// the candidate: under the same hold of the lock, once the journal is
// applied, it is left pending in the skill's checkout, see startMerge, the
// conflicts are reported, and the run ends with exit code 4. Once the
// merge is resolved in the checkout with git, the next update of the skill
// applies it, see judgePending, as a clean merge applies, the checkout
// going with the rest; until then it reports what is left unmerged with
// that same code.
//
// Everything the mutation replaces is read before the lock and again under
// it: the import branch, the candidate, the settings entry of the source
// and the library directory, whose content is captured as the fingerprint
// the journal compares, before git reads what a merge merges. A change to
// any of them in between refuses the skill rather than updating something
// nobody judged, so an edit made meanwhile is never replaced with the rest;
// the journal's remove step carries the same fingerprint, and the content
// it retains is dropped only when it still hashes to it.
//
// A skill whose source was removed from this machine is refused, and
// skipped with a warning by --all, see updateRun.drop.
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
	// An unfinished journal is finished before anything is read. A stopped
	// update's journal moves the import branch to the candidate first, so
	// until it is finished the skill reads as already updated while the
	// library still holds the version replaced, or holds no directory once
	// that was retained, and the run would answer for a machine halfway
	// through its own change.
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	records := map[string]lineage.Record{}
	if exists {
		if records, err = inv.listLineage(ctx, gitDir); err != nil {
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
		u, f := inv.judgeUpdate(ctx, gitDir, n, rec, managed, lib, held, sources)
		switch {
		case f != nil:
			r.drop(n, f)
		case u == nil: // a name the last check found no update for, which only a run of one name asks about
			inv.summary = n + " is up to date as of the last update check; run 'agentx skill check' to look again"
			inv.out.print(inv.out.paint(heading, sanitised(n)), " is up to date as of the last update check; run ",
				inv.out.paint(label, "agentx skill check"), " to look again")
			return nil
		case u.merged.conflicted: // a merge pending with files still to resolve, left as it is
			r.pending = append(r.pending, u)
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
	if len(r.applied) > 0 || len(r.pending) > 0 {
		if err := r.report(ctx); err != nil {
			return err
		}
	} else if r.all && len(r.broken) == 0 {
		skips, _ := r.skippedNote(inv.out)
		inv.summary = "no skill was updated" + skips
		inv.out.print(inv.summary)
	}
	return r.failure()
}

// finishJournals finishes any unfinished journal before a command reads
// what it works on, for a command whose reads would otherwise answer for a
// machine halfway through an earlier change. The lock is taken for it only
// when there is a journal to finish, and nothing is written beyond what
// the recovery writes, its own bump of the version file included, as for
// the recovery a scan runs.
func (inv *invocation) finishJournals(ctx context.Context) error {
	switch journals, err := home.Journals(inv.dirs.Home); {
	case err != nil:
		return mutationFailure(err)
	case len(journals) > 0:
		inv.out.debugf("recovering %s", strings.Join(journals, ", "))
		if err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error { return nil }); err != nil {
			return mutationFailure(err)
		}
	}
	return nil
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

// judgeUpdate decides, before the lock, whether the skill called name can
// be updated, and reads what the mutation replaces. It refuses, in this
// order: a name the library does not hold and no lineage names, an
// unmanaged skill, a fork, a managed skill whose library directory is gone
// and one whose import branch agentx cannot read; a skill whose source was
// removed from this machine, which is exit code 5 as installing from it
// is; a skill the last update check found its source no longer holds,
// which is kept as it is and never updated; and, having found an update
// for it, a skill whose library entry is a symlink, whatever it leads to,
// and one that holds something git cannot record, which the update would
// discard with no record of it and which a revert refuses too. A skill
// with no update is neither: u and f are then both nil. A skill edited
// since it was installed is no refusal: its update merges the edits, and
// u says so. A skill with a merge pending is judged by its checkout, see
// judgePending, and its library directory must still hold the mine the
// merge started from.
//
// Edited is decided as drift decides it, by git over a throwaway index
// loaded from the base version, with the in-process tree id as the fast
// path, so a file git ignores is no edit. The files git ignores, which the
// update keeps, are read here, outside the lock: they still hold under it
// for as long as the fingerprint recheckUpdate compares does, since that
// covers every byte of the directory.
func (inv *invocation) judgeUpdate(ctx context.Context, gitDir, name string, rec lineage.Record, managed bool, lib scan.LibrarySkill, held bool, sources map[string]bool) (*updating, *failure) {
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
	libPath := inv.libraryPath(name)
	u := &updating{name: name, rec: rec, libPath: libPath}
	v := baseVersion(rec)
	if inv.mergePending(name) {
		switch f := inv.judgePending(ctx, gitDir, u); {
		case f != nil:
			return nil, f
		case u.merged.conflicted:
			return u, nil
		}
		v = u.mine
	} else if next, ok := rec.AtCandidate(); ok {
		u.next = next
	} else {
		return nil, nil
	}
	captured, err := home.State(libPath)
	if err != nil {
		return nil, failureOf(libraryFailure(inv.dirs.Library, err))
	}
	// A library entry that is a symlink leads to a directory of the user's.
	// The update replaces the entry itself, so it would drop the link and
	// leave the directory it led to as it was, whatever that holds: the
	// link is what is refused, before what it leads to is judged, since a
	// revert of an edit there refuses the link too.
	if target, isLink := home.LinkTarget(captured); isLink {
		return nil, refuse(exitRefused, fmt.Sprintf("%s is a symlink to %s; an update replaces the library directory and would drop the link without touching the files it leads to", quotedPath(libPath), quotedPath(target)),
			"replace the link with the directory it points to, then "+again)
	}
	tree, err := inv.readLibraryTree(lib.Path)
	if err != nil {
		return nil, failureOf(err)
	}
	if len(tree.Unrecordable) > 0 {
		return nil, unrecordableRefusal(name, libPath, tree.Unrecordable, "an update", "update")
	}
	j, err := inv.judgeDir(ctx, gitDir, lib.ResolvedPath, tree, v, true)
	if err != nil {
		return nil, failureOf(accountRepoFailure(err))
	}
	if u.checkout != "" && !j.holds {
		return nil, refuse(exitRefused, name+" was edited while its merge was pending, so the merge cannot be applied",
			"run '"+skillCommand("update", name, "--abort")+"' and update again")
	}
	u.captured, u.written, u.ignored = captured, j.written, j.ignored
	u.edited = !j.holds || u.checkout != "" // a resolved merge is laid out as a clean one is
	return u, nil
}

// judgePending reads, before the lock, the merge pending for u's skill in
// its checkout, as the user left it resolving it with git, see readMerge.
// A merge with files still unmerged, or no merge in progress at all, once
// git merge --abort ran there say, is left as it is: u's merge conflicts,
// with the files still unmerged. One with nothing left unmerged, in
// progress or committed, is applied as merged: its tree is the index's,
// and the update it moves the import branch to is the one it merged,
// whatever the candidate is now; the import branch must still be at the
// merge's base, or it is refused and the checkout kept, for --abort.
func (inv *invocation) judgePending(ctx context.Context, gitDir string, u *updating) *failure {
	u.checkout = inv.checkoutPath(u.name)
	s, err := inv.readMerge(ctx, u.checkout)
	if err != nil {
		return failureOf(accountRepoFailure(err))
	}
	if s.theirs == "" {
		s.theirs = u.rec.CandidateCommit() // no merge in progress: the update it would merge
	}
	if u.next, err = u.rec.At(ctx, inv.git, gitDir, s.theirs); err != nil {
		return failureOf(accountRepoFailure(err))
	}
	dir := u.rec.Import.Dir()
	u.merge = lineage.Merge{Base: s.base, Mine: s.mine, Theirs: s.theirs}
	u.mine = version{load: s.mine + ":" + dir, holds: func(id string) bool { return treeid.Wrap(dir, id) == s.mineTree }}
	if s.tree == "" {
		files, err := conflictFiles(s.unmerged, dir)
		if err != nil {
			return failureOf(accountRepoFailure(err))
		}
		u.merged.conflicted, u.conflict = true, conflictOfSkill(u.name, u.merge, files)
		return nil
	}
	if s.base != u.rec.Commit {
		return refuse(exitRefused, "the import branch "+lineage.ManagedRef(u.name)+" moved outside agentx while the merge was pending, so the merge cannot be applied",
			"run '"+skillCommand("update", u.name, "--abort")+"' to give it up")
	}
	u.merged.tree = s.tree
	return nil
}

// removedSourceRefusal refuses a skill whose source is gone from this
// machine, in the words the refusal of that source's id uses, and with the
// command that adds it again: the update the last check found for the
// skill stays, and applies once the source is back. A run over every skill
// skips the skill with the same words, see updateRun.drop.
func removedSourceRefusal(name, url string) *failure {
	return refuse(exitNotFound, name+" was installed from "+url+", which was removed from this machine, so it is not updated",
		"run 'agentx source add "+sourceAddArg(url, "")+"' to add it again").wrap(errSourceRemoved)
}

// upstreamRemovedRefusal refuses a skill the last update check found its
// source no longer holds: it is kept as it is and never updated.
func upstreamRemovedRefusal(name string) *failure {
	return refuse(exitRefused, "the last update check found that the source of "+name+" no longer holds it, so it is kept as it is and never updated",
		"run 'agentx skill check' once the source holds it again, or '"+skillCommand("remove", name)+"' to remove it")
}

// read reads, outside the lock, the version each skill is updated to: the
// entries of its candidate's upstream directory, one ls-tree each, and for
// a skill that was edited the merge of its edits with them, see merge; then
// the files of every version laid out, in one cat-file for the whole run.
// A candidate that stores its version in a form an import does not write,
// which no library directory is current against, is refused for its skill
// alone: a check writes it again, as an install would write it.
func (r *updateRun) read(ctx context.Context) error {
	var ready []*updating
	var ids []string
	for _, u := range r.ready {
		theirs, err := lineage.ReadBase(ctx, r.inv.git, r.gitDir, u.next)
		if err != nil {
			r.drop(u.name, failureOf(accountRepoFailure(err)))
			continue
		}
		if !u.next.Canonical(theirs) {
			r.drop(u.name, refuse(exitAccountRepo, "the update candidate "+lineage.CandidateRef(u.name)+" stores its version in a form agentx does not write",
				"run 'agentx skill check' to pin the update again"))
			continue
		}
		u.theirs, u.base = theirs, theirs
		u.target = version{load: baseVersion(u.next).load, holds: func(id string) bool { return id == theirs.ID() }}
		u.placed = []version{baseVersion(u.rec)}
		if u.edited {
			if f := r.merge(ctx, u); f != nil {
				r.drop(u.name, f)
				continue
			}
		}
		if !u.merged.conflicted {
			ids = append(ids, baseBlobs(u.base)...)
		}
		ids = append(ids, skillFileBlob(u.theirs)...)
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

// merge merges the edits of a modified skill with its update, outside the
// lock, as git merges three versions: the tree git wrote of the library
// directory, over the index judgeUpdate loaded from the base version, is
// wrapped in the upstream directory as an import tree is and committed on
// the base version as "mine"; then merge-tree merges it with the
// candidate, the base version given as the merge base, see mergeVersions.
// A file git ignores is not in mine, and the update carries it over as a
// replacement does.
//
// A clean merge is the version the update lays out, in place of the
// candidate's, and the skill keeps its edits on the new base. The copies
// agentx could have placed are then the ones holding the library directory
// as it was or the base version, since a copy placed before the edits holds
// the base. A merge that conflicts lays nothing out: the update leaves it
// pending as merge-tree found it, see startMerge, and the files that
// conflict are read from merge-tree's own list. A pending merge resolved
// with git, see judgePending, is merged already, and is laid out as a
// clean one is.
func (r *updateRun) merge(ctx context.Context, u *updating) *failure {
	git, gitDir := r.inv.git, r.gitDir
	dir := u.rec.Import.Dir()
	if u.next.Import.Dir() != dir {
		return refuse(exitAccountRepo, "the update candidate "+lineage.CandidateRef(u.name)+" holds "+u.name+" under another directory than its import branch",
			"run 'agentx skill check' to pin the update again")
	}
	if u.checkout == "" {
		mine, err := lineage.CommitDir(ctx, git, gitDir, dir, u.written, u.rec.Commit, "library directory of "+u.name+"\n")
		if err != nil {
			return failureOf(accountRepoFailure(err))
		}
		u.merge = lineage.Merge{Base: u.rec.Commit, Mine: mine, Theirs: u.next.Commit}
		u.mine = treeVersion(u.written)
		if u.merged, err = mergeVersions(ctx, git, gitDir, u.merge); err != nil {
			return failureOf(accountRepoFailure(err))
		}
		if u.merged.conflicted {
			files, err := conflictFiles(u.merged.stages, dir)
			if err != nil {
				return failureOf(accountRepoFailure(err))
			}
			u.conflict = conflictOfSkill(u.name, u.merge, files)
			return nil
		}
	}
	merged, err := lineage.ReadMerged(ctx, git, gitDir, u.merged.tree, dir)
	if err != nil {
		return failureOf(accountRepoFailure(err))
	}
	u.base = merged
	u.target = treeVersion(merged.ID())
	u.placed = []version{u.mine, baseVersion(u.rec)}
	return nil
}

// skillFileBlob is the blob of the SKILL.md a version holds, the one file
// of it the name of the skill is read from, as a list of none or one.
func skillFileBlob(v lineage.Base) []string {
	for _, e := range v.Entries {
		if e.Path == "SKILL.md" && source.IsFileMode(e.Mode) {
			return []string{e.OID}
		}
	}
	return nil
}

// changedWhileUpdating refuses a skill whose library directory changed
// after the update read it: what the update would replace, or merge, is
// no longer what it judged, and an edit made meanwhile is never replaced
// with the rest.
func changedWhileUpdating(name string) *failure {
	return refuse(exitRefused, name+" changed while it was being updated, so nothing was changed",
		"run '"+skillCommand("update", name)+"' again to update it as it is now")
}

// upstreamNameOf is the name an install of the candidate's version would
// give the skill, when it is not the skill's library name, and ""
// otherwise, see upstreamRename.
func upstreamNameOf(u *updating, bodies map[string]string) string {
	name := ""
	if ids := skillFileBlob(u.theirs); len(ids) > 0 {
		name, _, _ = scan.SkillFrontmatter(bodies[ids[0]])
	}
	return upstreamRename(u.name, name, u.next.Import.Dir())
}

// apply reads every skill's inputs again under the lock and applies the
// update of each one they still hold as one journaled mutation. A skill
// with a merge pending, or whose import branch, candidate, source or
// library directory changed since it was judged, is dropped on its own and
// the others go on; a run whose every skill was dropped here writes no
// journal. A skill whose merge conflicts has no step in the journal and
// nothing of it changes: once the journal is applied, and only then, its
// merge is left pending in its checkout, under the same hold of the lock.
// A skill whose merge cannot be set up is dropped with nothing of it left.
// The checkout of a pending merge the journal applied is removed by it,
// and git's registration of it once it is applied; one left behind is
// pruned by the next command that changes anything.
func (r *updateRun) apply(ctx context.Context) error {
	inv := r.inv
	var refs []string
	for _, u := range r.ready {
		refs = append(refs, lineage.ManagedRef(u.name), lineage.ForkRef(u.name), lineage.CandidateRef(u.name), lineage.UpstreamRemovedRef(u.name))
	}
	var staged, pending []*updating
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
		var live, conflicted []*updating
		for _, u := range r.ready {
			switch f := inv.recheckUpdate(u, values, sources); {
			case f != nil:
				r.drop(u.name, f)
			case u.merged.conflicted:
				conflicted = append(conflicted, u)
			default:
				live = append(live, u)
			}
		}
		if len(live)+len(conflicted) == 0 {
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
			if err := inv.stageUpdate(ctx, m, r.gitDir, u, r.bodies, edit.copiesOf(u.name)); err != nil {
				m.Discard()
				return err
			}
		}
		if len(live) > 0 {
			backs := make([]func(), 0, len(live))
			for _, u := range live {
				backs = append(backs, reenterReplaced(u.libPath))
			}
			err := m.Apply(inv.refs(ctx))
			for _, back := range backs {
				back()
			}
			if err != nil {
				return err
			}
		}
		for _, u := range live {
			if u.checkout == "" {
				continue
			}
			if err := inv.git.RemoveCheckout(ctx, r.gitDir, u.checkout); err != nil {
				inv.out.debugf("cannot remove the registration of %s, which the next command prunes: %v", u.checkout, err)
			}
		}
		staged = live
		for _, u := range conflicted {
			start := mergeStart{mine: u.merge.Mine, theirs: u.merge.Theirs, merged: u.merged, message: updateMergeMessage(u.name, u.rec, u.next)}
			if err := inv.startMerge(ctx, r.gitDir, u.name, start); err != nil {
				r.drop(u.name, failureOf(accountRepoFailure(err)))
				continue
			}
			pending = append(pending, u)
		}
		if len(staged)+len(pending) == 0 {
			return errNothingApplied
		}
		return nil
	})
	switch {
	case errors.Is(err, errNothingApplied):
		return nil
	case err != nil:
		return mutationFailure(err)
	}
	r.applied, r.pending = staged, append(r.pending, pending...)
	return nil
}

// recheckUpdate reads again, under the lock, everything the update of one
// skill replaces or depends on, and refuses the skill when any of it is no
// longer what judgeUpdate read: the import branch, which a fork of the name
// would supersede; whether a merge is pending for the skill, one pending
// now refusing an update that merges anew; the settings entry of the
// source; the upstream-removed marker and the candidate, which a check may
// have written meanwhile; and the library directory, whose content an edit
// made since it was captured would otherwise be replaced unseen: the
// fingerprint was captured before git read the directory, so it covers
// what the update merged too. A removed source is answered for before the
// marker, in the order judgeUpdate answers for them.
func (inv *invocation) recheckUpdate(u *updating, values map[string]string, sources map[string]bool) *failure {
	name := u.name
	again := "run '" + skillCommand("update", name) + "' again"
	switch {
	case values[lineage.ManagedRef(name)] != u.rec.Commit || values[lineage.ForkRef(name)] != "":
		return refuse(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved while "+name+" was being updated, so nothing was changed", again)
	case u.checkout == "" && inv.mergePending(name):
		return pendingMergeRefusal(name, "updated")
	case !sources[u.rec.Import.Source]:
		return removedSourceRefusal(name, u.rec.Import.Source)
	case values[lineage.UpstreamRemovedRef(name)] != "":
		return upstreamRemovedRefusal(name)
	case values[lineage.CandidateRef(name)] != u.rec.CandidateCommit():
		return refuse(exitRefused, "the update candidate "+lineage.CandidateRef(name)+" moved while "+name+" was being updated, so nothing was changed",
			again+" to apply the update the last check found")
	}
	live, err := home.State(u.libPath)
	if err != nil {
		return failureOf(libraryFailure(inv.dirs.Library, err))
	}
	if live != u.captured {
		return changedWhileUpdating(name)
	}
	return nil
}

// stageUpdate records, under the lock, the steps of one skill's update, in
// the order the journal applies them: the import branch moved from the
// version the library holds to the candidate, with the tip read before the
// lock as its expected old value; the library directory retained and
// replaced by the version the update lays out, the candidate's or its
// merge with the edits, laid out beside it and read back as git would
// record it; the copies that held what agentx could have placed there
// refreshed with the new one, see refreshCopies; the checkout of a pending
// merge the update applies removed; and the candidate ref deleted, with
// the candidate as its expected old value, which the journal runs after
// every path step, so that a refusal on the way leaves it in place. A
// candidate a check moved on while the merge was pending stays, as the
// skill's next update.
//
// The files git ignores in the library directory are carried into the new
// one, as git checkout keeps them, but for a path the new version holds,
// which keeps the new version's file. What they held before stays in the
// retained library directory until the update is complete.
func (inv *invocation) stageUpdate(ctx context.Context, m *home.Mutation, gitDir string, u *updating, bodies map[string]string, recorded []string) error {
	lay := func(dest string) error { return materialise(dest, u.base, bodies) }
	staged := m.Sibling(u.libPath, "staged")
	fingerprint, err := stageVersion(staged, lay, u.target, u.libPath, u.ignored)
	if err != nil {
		_ = home.RemoveTree(staged)
		return libraryFailure(inv.dirs.Library, err)
	}
	u.done = placements{}
	m.Ref(gitDir, lineage.ManagedRef(u.name), u.rec.Commit, u.next.Commit)
	m.Remove(u.libPath, u.captured)
	m.Publish(u.libPath, staged, fingerprint)
	inv.refreshCopies(ctx, m, gitDir, u.name, u.target, u.placed, lay, recorded, &u.done)
	if u.checkout != "" {
		inv.leaveCheckout(u.checkout)
		checkout, err := home.State(u.checkout)
		if err != nil {
			return err
		}
		m.Remove(u.checkout, checkout)
	}
	if u.rec.CandidateCommit() == u.next.Commit {
		m.Ref(gitDir, lineage.CandidateRef(u.name), u.next.Commit, "")
	}
	return nil
}

// report reports every skill the run updated and every merge it left
// pending. The machine is read again for the skills it updated, which are
// reported as they now stand, in name order: a warning for each whose new
// version names it otherwise, one library_skill event each, carrying every
// placement as skill list reports them, and the line that says what the
// run did. Every merge left pending is reported after them, in name order,
// see printConflicts, and costs its skill as a refusal does: the skill was
// not updated, and the run exits 4 for it.
func (r *updateRun) report(ctx context.Context) error {
	if len(r.applied) > 0 {
		if err := r.reportApplied(ctx); err != nil {
			return err
		}
	}
	slices.SortFunc(r.pending, func(a, b *updating) int { return strings.Compare(a.name, b.name) })
	for _, u := range r.pending {
		r.inv.printConflicts(u.conflict, short(u.rec.Import.Commit), short(u.next.Import.Commit))
		r.drop(u.name, conflictFailure(u.name, filepath.Join(r.inv.checkoutPath(u.name), u.rec.Import.Dir()), len(u.conflict.Files)))
	}
	return nil
}

// conflictFailure is how an update answers for a skill whose merge it left
// pending, path being the skill's directory in the checkout, which the
// files it lists are relative to: exit code 4, the library directory left
// as it is, and the ways on from there, all of them git's own but for the
// update that applies the merge once it is resolved and the one that gives
// it up.
func conflictFailure(name, path string, files int) *failure {
	return refuse(exitPendingMerge, name+" conflicts with its update in "+plural(files, "file")+", so the merge is pending and the library directory was left as it is",
		"resolve it with git in "+quotedPath(path)+" ('git add' each file you resolved, or 'git checkout --ours|--theirs <file>' then 'git add'; 'git commit' is optional), "+
			"then run '"+skillCommand("update", name)+"' again to apply it, or '"+skillCommand("update", name, "--abort")+"' to give it up")
}

// reportApplied is report's part for the skills the run updated.
func (r *updateRun) reportApplied(ctx context.Context) error {
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
			inv.out.warn(renameWarning(u.name, u.upstreamName))
		}
	}
	for _, u := range r.applied {
		lib, ok := libs[u.name]
		if !ok {
			return fail(exitInternal, "the library holds no "+u.name+" after updating it", "run 'agentx doctor' and check the library it names")
		}
		inv.out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
	}
	out := inv.out
	if !r.all {
		u := r.applied[0]
		moved := " from " + short(u.rec.Import.Commit) + " to " + short(u.next.Import.Commit)
		switch {
		case u.checkout != "":
			moved += " with the merge you resolved"
		case u.edited:
			moved += " and merged its edits cleanly"
		}
		plain, painted := copiesNote(out, len(u.done.copies), len(u.done.skipped))
		inv.summary = "updated " + u.name + moved + plain
		out.done("updated " + out.paint(heading, sanitised(u.name)) + moved + painted)
		return nil
	}
	refreshed, skipped, merged := 0, 0, 0
	for _, u := range r.applied {
		refreshed += len(u.done.copies)
		skipped += len(u.done.skipped)
		if u.edited && u.checkout == "" {
			merged++
		}
	}
	var mergedNote, paintedMerged string
	if merged > 0 {
		mergedNote = ", " + plural(merged, "edited skill") + " merged cleanly"
		paintedMerged = ", " + out.paint(noteStyle, plural(merged, "edited skill")+" merged cleanly")
	}
	note, painted := copiesNote(out, refreshed, skipped)
	skips, paintedSkips := r.skippedNote(out)
	inv.summary = "updated " + plural(len(r.applied), "skill") + mergedNote + note + skips
	out.done("updated " + plural(len(r.applied), "skill") + paintedMerged + painted + paintedSkips)
	t := &table{}
	for _, u := range r.applied {
		notes, _ := copiesNote(out, len(u.done.copies), len(u.done.skipped))
		switch {
		case u.checkout != "":
			notes = ", the merge you resolved" + notes
		case u.edited:
			notes = ", edits merged cleanly" + notes
		}
		t.add(c(sanitised(u.name), heading), c(short(u.rec.Import.Commit)+" -> "+short(u.next.Import.Commit), plain), c(strings.TrimPrefix(notes, ", "), noteStyle))
	}
	out.render(t, "  ")
	return nil
}

// skippedNote is what a run over every skill skipped, as its line and its
// result say it: how many skills whose source was removed, after a comma,
// or nothing when there were none.
func (r *updateRun) skippedNote(out *writer) (plain, painted string) {
	var notes []string
	switch n := len(r.sourceless); {
	case n == 1:
		notes = append(notes, "1 skill from a removed source skipped")
	case n > 1:
		notes = append(notes, fmt.Sprintf("%d skills from removed sources skipped", n))
	}
	for _, note := range notes {
		plain += ", " + note
		painted += ", " + out.paint(warnStyle, note)
	}
	return plain, painted
}
