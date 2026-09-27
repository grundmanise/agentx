package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

func newSkillUpdateCommand(inv *invocation) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "update [<name>]",
		Short: "Apply the update the last check found to a managed skill",
		Long: "Replace the library directory of a managed skill with the newer upstream version\n" +
			"'agentx skill check' found for it, and record that version as the one the skill\n" +
			"is at. A skill edited since it was installed keeps its edits: they are merged\n" +
			"into the newer version, and when they conflict with it nothing is changed and the\n" +
			"conflicts are shown, the merge waiting until it is resolved. A copy placement that\n" +
			"holds the version replaced is refreshed; a copy edited on its own is kept and\n" +
			"named. Pass --all instead of a name to update every managed skill the last check\n" +
			"found an update for. Read an update before you apply it with\n" +
			"'agentx skill diff <name> --upstream'.",
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
	tree         treeid.Tree  // what the library directory holds, as git would record it
	held         string       // its tree id: the base version's, unless the skill was edited
	edited       bool         // the library directory is not the base version, so the update merges
	theirs       lineage.Base // the candidate's version
	base         lineage.Base // the version laid out in the library: the candidate's, or the merge of it with the edits
	placed       []string     // the trees a copy holds that agentx could have placed there, which the update refreshes
	pending      string       // the pending merge commit of an edited skill whose merge conflicts
	conflict     conflictEvent
	upstreamName string     // the name the candidate's SKILL.md gives the skill, when it is not name
	done         placements // the copies the mutation refreshed or kept
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
// laid out beside it, the copies that held the version replaced are
// refreshed with it, and the candidate ref is deleted last.
//
// A modified skill updates by a three-way merge, see updateRun.merge: the
// library directory, committed on its base version, merged with the
// candidate. A clean merge applies exactly as a replacement does, with the
// merged version laid out in place of the candidate's, and the skill stays
// modified against its new base. A merge that conflicts changes neither
// the library nor the import branch nor the candidate: the mutation creates
// the skill's merge ref alone, the conflicts are reported, and the run ends
// with exit code 4. A skill with a merge pending is refused with that same
// code, whatever else is true of it, until the merge is resolved or given
// up.
//
// Everything the mutation replaces is read before the lock and again under
// it: the import branch, the candidate, the merge ref, the settings entry
// of the source and the library directory, whose content is captured as
// the fingerprint the journal compares and whose tree is what a merge
// merged. A change to any of them in between refuses the skill rather than
// updating something nobody judged, so an edit made meanwhile is never
// replaced with the rest; the journal's remove step carries the same
// fingerprint, and the content it retains is dropped only when it still
// hashes to it.
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
	// through its own change. Nothing is written beyond what the recovery
	// writes, its own bump of the version file included, as for the
	// recovery a scan runs.
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
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
		case f != nil:
			r.drop(n, f)
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
// and one whose import branch agentx cannot read; a skill with a merge
// pending, which is exit code 4 whether or not there is an update, since
// the merge has to be resolved or given up first; a skill whose source was
// removed from this machine, which is exit code 5 as installing from it
// is; a skill the last update check found its source no longer holds,
// which is kept as it is and never updated; and, having found an update
// for it, a skill whose library entry is a symlink, whatever it leads to,
// and one that holds something git cannot record, which the update would
// discard with no record of it and which a revert refuses too. A skill
// with no update is neither: u and f are then both nil. A skill edited
// since it was installed is no refusal: its update merges the edits, and
// u says so.
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
	case rec.PendingMerge != nil:
		return nil, pendingMergeRefusal(name, "updated")
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
	return &updating{name: name, rec: rec, next: next, libPath: libPath, captured: captured, tree: tree, held: tree.ID,
		edited: !rec.Current(tree), placed: []string{tree.ID}}, nil
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
		if u.edited {
			if f := r.merge(ctx, u); f != nil {
				r.drop(u.name, f)
				continue
			}
		}
		if u.pending == "" {
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
// lock, as git merges three versions: the library directory as it was read
// is written into the account repo as the tree it is, through the writer
// every tree of a directory takes, wrapped in the upstream directory as an
// import tree is and committed on the base version as "mine"; then
// merge-tree merges it with the candidate, the base version given as the
// merge base, see mergeVersions.
//
// A clean merge is the version the update lays out, in place of the
// candidate's, and the skill keeps its edits on the new base. The copies
// agentx could have placed are then the ones holding the library directory
// as it was or the base version, since a copy placed before the edits holds
// the base. A clean merge the library's file system cannot hold is refused
// instead, see caseClashRefusal. A merge that conflicts is committed as the
// skill's pending merge commit, its parents mine and the candidate and its
// trailers naming the three versions, and nothing is laid out: the
// mutation only points the merge ref at it.
//
// A directory that holds exactly the base version's files, which reads as
// modified only because an earlier agentx stored that version in a form git
// no longer writes, has no edits to merge, and updates by replacement.
//
// A directory that changed while it was written is refused, as it is under
// the lock: the merge would be of something nobody read.
func (r *updateRun) merge(ctx context.Context, u *updating) *failure {
	git, gitDir := r.inv.git, r.gitDir
	dir := u.rec.Import.Dir()
	if u.next.Import.Dir() != dir {
		return refuse(exitAccountRepo, "the update candidate "+lineage.CandidateRef(u.name)+" holds "+u.name+" under another directory than its import branch",
			"run 'agentx skill check' to pin the update again")
	}
	base, err := lineage.ReadBase(ctx, git, gitDir, u.rec)
	if err != nil {
		return failureOf(accountRepoFailure(err))
	}
	if base.HeldBy(u.tree) {
		u.edited = false
		return nil
	}
	root, err := filepath.EvalSymlinks(u.libPath)
	if err != nil {
		return failureOf(libraryFailure(r.inv.dirs.Library, err))
	}
	mine, err := lineage.WriteMine(ctx, git, gitDir, root, u.tree, u.rec)
	if errors.Is(err, lineage.ErrChanged) {
		return changedWhileUpdating(u.name)
	}
	if err != nil {
		return failureOf(accountRepoFailure(err))
	}
	m := lineage.Merge{Base: u.rec.Commit, Mine: mine, Theirs: u.next.Commit}
	res, err := mergeVersions(ctx, git, gitDir, dir, m)
	if err != nil {
		return failureOf(accountRepoFailure(err))
	}
	if len(res.files) > 0 {
		if u.pending, err = lineage.CommitPending(ctx, git, gitDir, res.tree, u.name, m, len(res.files), nil); err != nil {
			return failureOf(accountRepoFailure(err))
		}
		u.conflict = conflictOfSkill(u.name, m, res.files)
		return nil
	}
	merged, err := lineage.ReadMerged(ctx, git, gitDir, res.tree, dir)
	if err != nil {
		return failureOf(accountRepoFailure(err))
	}
	if f := caseClashRefusal(u, merged, root); f != nil {
		return f
	}
	u.base, u.placed = merged, []string{u.held, base.ID()}
	return nil
}

// caseClashRefusal refuses a clean merge that holds two paths differing in
// case alone when the file system of the library directory at root cannot
// keep them apart, as macOS and Windows by default cannot. git compares
// paths byte for byte, so a file the user added and one the update added,
// readme.md and README.md say, merge cleanly, into a version that laid out
// there would be one file and not the version merged. It is refused before
// the lock, for its skill alone, and names the path of the two the library
// directory holds as the one to rename; the update holds the other. A pair
// the update itself holds both of has nothing in the library to rename,
// and is only laid out once its source tells them apart.
func caseClashRefusal(u *updating, merged lineage.Base, root string) *failure {
	a, b, ok := caseClash(merged.Entries)
	if !ok {
		return nil
	}
	held := map[string]bool{}
	for _, d := range u.tree.Dirs {
		held[d.Path] = true
	}
	for _, blob := range u.tree.Blobs {
		held[blob.Path] = true
	}
	if (held[a] && held[b]) || !foldsCase(root, u.tree) {
		return nil // the file system keeps them apart
	}
	message := fmt.Sprintf("the update of %s merged with its edits holds both %s and %s, which this file system cannot keep apart, so nothing was changed",
		u.name, quotedPath(a), quotedPath(b))
	mine := a
	if held[b] {
		mine = b
	}
	if !held[mine] {
		return refuse(exitRefused, message,
			"the update itself holds both: once its source tells them apart, run 'agentx skill check', then '"+skillCommand("update", u.name)+"'")
	}
	return refuse(exitRefused, message,
		"rename "+quotedPath(mine)+" in "+quotedPath(u.libPath)+", then run '"+skillCommand("update", u.name)+"' again")
}

// caseClash finds two paths of a version, directories included, that
// differ in case alone, the first such pair in the order given. A
// directory is listed before what it holds, so two directories whose names
// differ in case clash as themselves, before any file below them does.
func caseClash(entries []source.TreeEntry) (a, b string, ok bool) {
	seen := map[string]string{}
	for _, e := range entries {
		key := foldedCase(e.Path)
		if other, found := seen[key]; found {
			return other, e.Path, true
		}
		seen[key] = e.Path
	}
	return "", "", false
}

// foldedCase is path with every letter replaced by the least of the
// letters Unicode folds it together with, so that two paths strings.EqualFold
// holds equal are the same string.
func foldedCase(path string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		return least
	}, path)
}

// foldsCase reports whether the file system of the directory at root, read
// as tree, finds a path whatever the case it is spelled in: whether a file
// the directory holds is found again, as the same file, with the case of
// every letter of its path swapped. A directory with no such path to try
// is judged by its operating system instead: macOS and Windows fold case
// by default.
func foldsCase(root string, tree treeid.Tree) bool {
	held := map[string]bool{}
	for _, b := range tree.Blobs {
		held[b.Path] = true
	}
	for _, b := range tree.Blobs {
		swapped := strings.Map(func(r rune) rune {
			if unicode.IsUpper(r) {
				return unicode.ToLower(r)
			}
			return unicode.ToUpper(r)
		}, b.Path)
		if swapped == b.Path || held[swapped] {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(b.Path)))
		if err != nil {
			continue
		}
		other, err := os.Lstat(filepath.Join(root, filepath.FromSlash(swapped)))
		return err == nil && os.SameFile(info, other)
	}
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
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
	return upstreamRename(u.name, skillName(u.theirs, bodies), u.next.Import.Dir())
}

// skillName is the name the SKILL.md of a version gives the skill, read
// out of bodies, "" when it gives none.
func skillName(v lineage.Base, bodies map[string]string) string {
	ids := skillFileBlob(v)
	if len(ids) == 0 {
		return ""
	}
	name, _, _ := scan.SkillFrontmatter(bodies[ids[0]])
	return name
}

// apply reads every skill's inputs again under the lock and applies the
// update of each one they still hold as one journaled mutation. A skill
// whose import branch, candidate, merge ref, source or library directory
// changed since it was judged is dropped on its own and the others go on,
// as is one whose version could not be laid out; a run whose every skill
// was dropped here writes no journal. A skill whose
// merge conflicts gets one step, the creation of its merge ref, which must
// not exist yet; everything else of it is left as it is.
func (r *updateRun) apply(ctx context.Context) error {
	inv := r.inv
	var refs []string
	for _, u := range r.ready {
		refs = append(refs, lineage.ManagedRef(u.name), lineage.ForkRef(u.name), lineage.CandidateRef(u.name),
			lineage.UpstreamRemovedRef(u.name), lineage.MergeRef(u.name))
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
				r.drop(u.name, f)
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
				if u.pending == "" && !t.readsLibrary && !swept[t.dir] && slices.Contains(edit.copiesOf(u.name), t.id) {
					swept[t.dir] = true
					sweepStaged(t.dir)
				}
			}
		}
		// stageUpdate records no step of a skill until its version is laid
		// out, and removes what it laid out when that fails, so a skill it
		// fails for is dropped with nothing of it in the mutation.
		m := home.NewMutation(inv.dirs.Home)
		for _, u := range live {
			if u.pending != "" {
				m.Ref(r.gitDir, lineage.MergeRef(u.name), "", u.pending)
			} else if err := inv.stageUpdate(m, r.gitDir, u, r.bodies, edit.copiesOf(u.name)); err != nil {
				r.drop(u.name, failureOf(err))
				continue
			}
			staged = append(staged, u)
		}
		if len(staged) == 0 {
			m.Discard()
			return errNothingApplied
		}
		return m.Apply(inv.refs(ctx))
	})
	switch {
	case errors.Is(err, errNothingApplied):
		return nil
	case err != nil:
		return mutationFailure(err)
	}
	for _, u := range staged {
		if u.pending != "" {
			r.pending = append(r.pending, u)
		} else {
			r.applied = append(r.applied, u)
		}
	}
	return nil
}

// recheckUpdate reads again, under the lock, everything the update of one
// skill replaces or depends on, and refuses the skill when any of it is no
// longer what judgeUpdate read: the import branch, which a fork of the name
// would supersede; a merge left pending meanwhile, by another update of the
// skill; the settings entry of the source; the upstream-removed marker and
// the candidate, which a check may have written meanwhile; and the library
// directory, whose content an edit made since it was captured would
// otherwise be replaced unseen, and whose tree has to be the one the update
// read, and merged when the skill was edited, so that what it replaces is
// what it judged however the directory changed in between. A removed source
// is answered for before the marker, in the order judgeUpdate answers for
// them.
func (inv *invocation) recheckUpdate(u *updating, values map[string]string, sources map[string]bool) *failure {
	name := u.name
	again := "run '" + skillCommand("update", name) + "' again"
	switch {
	case values[lineage.ManagedRef(name)] != u.rec.Commit || values[lineage.ForkRef(name)] != "":
		return refuse(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved while "+name+" was being updated, so nothing was changed", again)
	case values[lineage.MergeRef(name)] != "":
		return pendingMergeRefusal(name, "updated")
	case !sources[u.rec.Import.Source]:
		return removedSourceRefusal(name, u.rec.Import.Source)
	case values[lineage.UpstreamRemovedRef(name)] != "":
		return upstreamRemovedRefusal(name)
	case values[lineage.CandidateRef(name)] != u.next.Commit:
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
	tree, err := inv.readLibraryTree(u.libPath)
	if err != nil {
		return failureOf(err)
	}
	if tree.ID != u.held || len(tree.Unrecordable) > 0 {
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
// refreshed with the new one, see refreshCopies; and the candidate ref
// deleted, with the candidate as its expected old value, which the journal
// runs after every path step, so that a refusal on the way leaves it in
// place. What the library directory holds unchanged keeps its permissions,
// see keepPerms.
func (inv *invocation) stageUpdate(m *home.Mutation, gitDir string, u *updating, bodies map[string]string, recorded []string) error {
	target := u.base.ID()
	staged := m.Sibling(u.libPath, "staged")
	fingerprint, err := stageBase(staged, u.base, target, bodies)
	if err == nil {
		err = keepPerms(staged, u.libPath, u.tree, u.base)
	}
	if err != nil {
		os.RemoveAll(staged)
		return libraryFailure(inv.dirs.Library, err)
	}
	u.done = placements{}
	m.Ref(gitDir, lineage.ManagedRef(u.name), u.rec.Commit, u.next.Commit)
	m.Remove(u.libPath, u.captured)
	m.Publish(u.libPath, staged, fingerprint)
	inv.refreshCopies(m, u.name, target, u.placed, "", staged, recorded, &u.done)
	m.Ref(gitDir, lineage.CandidateRef(u.name), u.next.Commit, "")
	return nil
}

// keepPerms gives what an update laid out at staged, the version v, the
// permissions the library directory at libPath, read as tree, gives the
// same thing: a file the library holds at the same path with the same
// content, and executable exactly when git records it so, and a directory
// the library holds at the same path, the skill's own included, both as
// the library's tree was read, never through a symlink of it. A version is
// laid out with every file 0644, or 0755, and every directory 0755, so a
// file of the user's they made private, 0600 say, which the update does not
// change, would otherwise come out of it readable by everyone. The owner's
// execute bit of a file is never changed, so the tree staged and its
// fingerprint stay what they were; a file the update changes, and anything
// agentx could not read back or enter once it was changed, is left as it
// was laid out. The library directory was read again under the lock, so
// what it holds is what the update merged or replaced.
func keepPerms(staged, libPath string, tree treeid.Tree, v lineage.Base) error {
	held := map[string]string{}
	for _, b := range tree.Blobs {
		if !b.Link {
			held[b.Path] = b.OID
		}
	}
	dirs := map[string]bool{}
	for _, d := range tree.Dirs {
		dirs[d.Path] = true
	}
	keep := func(rel string, dir, executable bool) error {
		info, err := os.Lstat(filepath.Join(libPath, filepath.FromSlash(rel)))
		if err != nil {
			return nil
		}
		perm := info.Mode().Perm()
		switch {
		case dir && (!info.IsDir() || perm&0o700 != 0o700):
			return nil
		case !dir && (!info.Mode().IsRegular() || perm&0o400 == 0 || (perm&0o100 != 0) != executable):
			return nil
		}
		f, err := os.Open(filepath.Join(staged, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		defer f.Close()
		if laid, err := f.Stat(); err != nil || laid.Mode().Perm() == perm {
			return err
		}
		if err := f.Chmod(perm); err != nil {
			return err
		}
		return f.Sync()
	}
	if err := keep("", true, false); err != nil {
		return err
	}
	for _, e := range v.Entries {
		var err error
		switch {
		case e.Mode == source.DirMode && dirs[e.Path]:
			err = keep(e.Path, true, false)
		case source.IsFileMode(e.Mode) && held[e.Path] == e.OID:
			err = keep(e.Path, false, e.Mode == source.ExecutableMode)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// report reports every skill the run updated and every merge it left
// pending. The machine is read again for the skills it updated, which are
// reported as they now stand, in name order: a warning for each whose new
// version names it otherwise, one library_skill event each, carrying every
// placement as skill list reports them, and the line that says what the
// run did. Every merge left pending is reported after them, see
// printConflicts, and costs its skill as a refusal does: the skill was not
// updated, and the run exits 4 for it.
func (r *updateRun) report(ctx context.Context) error {
	if len(r.applied) > 0 {
		if err := r.reportApplied(ctx); err != nil {
			return err
		}
	}
	for _, u := range r.pending {
		out := r.inv.out
		r.inv.printConflicts(u.conflict, out.paint(heading, sanitised(u.name)), " conflicts with its update from ", short(u.rec.Import.Commit),
			" to ", short(u.next.Import.Commit), " in ", out.paint(noteStyle, plural(len(u.conflict.Files), "file")))
		r.drop(u.name, conflictFailure(u.name, len(u.conflict.Files)))
	}
	return nil
}

// conflictFailure is how an update answers for a skill whose merge it left
// pending: exit code 4, the library directory left as it is, and the ways
// on from there.
func conflictFailure(name string, files int) *failure {
	return refuse(exitPendingMerge, name+" conflicts with its update in "+plural(files, "file")+", so the merge is pending and the library directory was left as it is",
		"run '"+skillCommand("resolve", name)+"' to resolve the conflicts, or '"+skillCommand("resolve", name, "--abort")+"' to give the merge up")
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
		inv.out.emit(sc.librarySkillEventFor(inv, snap, lib, nil))
	}
	out := inv.out
	if !r.all {
		u := r.applied[0]
		moved := " from " + short(u.rec.Import.Commit) + " to " + short(u.next.Import.Commit)
		if u.edited {
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
		if u.edited {
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
		if u.edited {
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
