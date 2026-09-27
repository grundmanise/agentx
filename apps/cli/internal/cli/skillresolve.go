package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

func newSkillResolveCommand(inv *invocation) *cobra.Command {
	var hunks []string
	var editor, abort bool
	cmd := &cobra.Command{
		Use:   "resolve <name> [<file>] [--hunk <file>:<index>=mine|theirs|both]... [--editor] [--abort]",
		Short: "Resolve, or give up, the merge an update left pending for a skill you edited",
		Long: "Show and resolve the conflicts an update found between the edits of a managed skill\n" +
			"and its newer version. With no flag the files left to resolve are shown and nothing\n" +
			"changes. --hunk <file>:<index>=mine|theirs|both chooses one part of a file, numbered\n" +
			"as shown, and can be given once per part: every part of a file is chosen in the same\n" +
			"run. --editor opens every text file left to resolve, or the one <file> names, in your\n" +
			"editor with conflict markers: GIT_EDITOR, else EDITOR, else 'code --wait'; <file> is\n" +
			"taken with --editor alone. Once no file is left the merge completes: the library\n" +
			"directory takes the merged version and the skill is at its update. --abort gives the\n" +
			"merge up and leaves the library directory as it is. Agents never see a half-merged\n" +
			"file: the merge waits in the account repo.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			const hint = "run 'agentx skill resolve --help' to see how a merge is resolved"
			name, file := args[0], ""
			switch {
			case len(args) == 2 && !editor:
				return fail(exitUsage, "skill resolve takes one skill name; a file is named after --editor, or in --hunk <file>:<index>=<side>", hint)
			case abort && (len(hunks) > 0 || editor):
				return fail(exitUsage, "--abort gives the merge up and takes neither --hunk nor --editor", hint)
			case editor && len(hunks) > 0:
				return fail(exitUsage, "choose parts with --hunk or open the files with --editor, not both in one run", hint)
			case len(args) == 2:
				file = args[1]
			}
			choices, err := parseHunkChoices(hunks)
			if err != nil {
				return err
			}
			act := resolveAction{choices: choices, abort: abort}
			if editor {
				command, ok := inv.editorCommand()
				if !ok {
					return fail(exitUsage, "no editor to open the files in: GIT_EDITOR and EDITOR are not set and there is no code command on PATH",
						"set EDITOR to the editor to use, as in EDITOR=vim, or resolve with --hunk <file>:<index>=mine|theirs|both")
				}
				act.editor = &editorSession{command: command, file: file, stdin: cmd.InOrStdin()}
			}
			return inv.skillResolve(cmd.Context(), name, act)
		},
	}
	cmd.Flags().StringArrayVar(&hunks, "hunk", nil, "choose mine, theirs or both for one part of a file, as <file>:<index>=<side>; give every part of a file")
	cmd.Flags().BoolVar(&editor, "editor", false, "open the files left to resolve, or the one <file> names, in your editor")
	cmd.Flags().BoolVar(&abort, "abort", false, "give the merge up; the library directory stays as it is")
	return cmd
}

// resolveAction is what one run of skill resolve was asked to do: show the
// merge, which a run with no flag does, choose parts of its files, open
// files in an editor, or give the merge up.
type resolveAction struct {
	choices []hunkChoice
	editor  *editorSession
	abort   bool
}

// The sides a part of a file is resolved to: the library directory's
// version, the update's, or both, the library's first.
const (
	sideMine   = "mine"
	sideTheirs = "theirs"
	sideBoth   = "both"
)

// hunkChoice is one --hunk: the file, relative to the skill's directory,
// the part of it by the number the conflict event gives it, and the side
// chosen for it.
type hunkChoice struct {
	path  string
	index int
	side  string
}

// parseHunkChoices reads every --hunk as <file>:<index>=<side>. A path may
// hold a colon or an equals sign, so the side is what follows the last =
// and the index what lies between that and the last colon before it. The
// path is cleaned, "./notes.md" naming notes.md, and one that leaves the
// skill's directory is refused. Each malformed one is exit code 1, before
// anything is read.
func parseHunkChoices(args []string) ([]hunkChoice, error) {
	const hint = "name each part as <file>:<index>=mine, theirs or both, the file relative to the skill's directory and the index as 'agentx skill resolve <name>' numbers it"
	choices := make([]hunkChoice, 0, len(args))
	for _, arg := range args {
		left, side, ok := cutLast(arg, "=")
		file, index, ok2 := cutLast(left, ":")
		n, err := strconv.Atoi(index)
		clean := path.Clean(file)
		switch {
		case !ok || !ok2 || err != nil || n < 1 || file == "":
			return nil, fail(exitUsage, fmt.Sprintf("--hunk %q is not <file>:<index>=<side>", arg), hint)
		case side != sideMine && side != sideTheirs && side != sideBoth:
			return nil, fail(exitUsage, fmt.Sprintf("--hunk %q chooses %q, which is not mine, theirs or both", arg, side), hint)
		case !lineage.FilePath(clean):
			return nil, fail(exitUsage, fmt.Sprintf("--hunk %q names no file of the skill", arg), hint)
		}
		choices = append(choices, hunkChoice{path: clean, index: n, side: side})
	}
	return choices, nil
}

// cutLast is strings.Cut at the last sep rather than the first.
func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// skillResolve shows, resolves or gives up the merge an update left pending
// for the managed skill called name. An unfinished journal is finished
// first, as an update finishes one: a completion stopped part way through
// moves the import branch before anything else, and the merge would read
// as one whose branch moved to the very update it merges. Then the skill is
// judged, in this order: a name the library does not hold and no lineage
// names is exit code 5; a fork, a skill with no import branch and one with
// no merge pending are exit code 6. --abort gives up any merge the ref
// holds, whatever else is true of it; everything else reads the merge
// first, see readPending.
func (inv *invocation) skillResolve(ctx context.Context, name string, act resolveAction) error {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	records := map[string]lineage.Record{}
	if exists {
		if records, err = lineage.List(ctx, inv.git, gitDir); err != nil {
			return accountRepoFailure(fmt.Errorf("account repo %s: %w", gitDir, err))
		}
	}
	rec, managed := records[name]
	_, held := librarySkill(inv.dirs.Library, name)
	switch {
	case !held && !managed:
		return inv.noLibrarySkill(name)
	case managed && rec.Kind == lineage.KindFork:
		return fail(exitRefused, name+" is a fork on this machine",
			"a fork's versions are its own history; this command works on a managed skill")
	case !managed || rec.PendingMerge == nil:
		return fail(exitRefused, name+" has no merge pending",
			"a merge is left pending by '"+skillCommand("update", name)+"' when your edits conflict with the update; run 'agentx skill list' to see which skills have one")
	case act.abort:
		return inv.abortMerge(ctx, gitDir, name)
	}
	r, err := inv.readPending(ctx, gitDir, rec)
	if err != nil {
		return err
	}
	switch {
	case act.editor != nil:
		return r.edit(ctx, act.editor)
	case len(act.choices) > 0:
		rs, err := r.choose(act.choices)
		if err != nil {
			return err
		}
		return r.resolve(ctx, rs)
	}
	return r.show()
}

// finishJournals finishes every unfinished journal before a command reads
// what it is about to judge, taking the lock only when there is one to
// finish, and writing nothing beyond what the recovery writes. A stopped
// update or completion moves the import branch first, so until its journal
// is finished the skill reads as already at the version while the library
// still holds the one before, and the command would answer for a machine
// halfway through a change of its own.
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

// pendingRun is one run of skill resolve on a skill whose merge is pending:
// its lineage, what its merge ref held when the run read it, and the merge
// of the three versions the ref's commit names, run again, which gives the
// same files and the same hunks, numbered alike, as the update that left it.
type pendingRun struct {
	inv       *invocation
	gitDir    string
	rec       lineage.Record           // the import branch as the run read it, at the merge's base unless something moved it since
	pending   lineage.PendingMerge     // what the merge ref held
	dir       string                   // the upstream directory every tree of the merge wraps the skill in
	res       mergeResult              // the merge of the three versions, run again
	files     map[string]*conflictFile // the files that conflict, by path
	resolved  map[string]bool          // the files the pending merge commit records as resolved
	theirs    lineage.Import           // the lineage of the update merged
	carried   map[string]bool          // once the run merged the merge again and wrote or completed it: the files whose resolution it kept
	left      map[string]bool          // and the files it left to resolve, none once it completed it
	completed bool                     // the run completed the merge
	turned    map[string]bool          // the directories of earlier editor sessions whose run was gone that this run kept as its session started, see settle
}

// readPending reads the merge a skill's merge ref holds, refusing one that
// cannot be resolved with exit code 6 and the hint that gives it up: an
// import branch or a pending merge commit agentx cannot read, an import
// branch that moved to the very update the merge merges, which leaves
// nothing to merge, and one that moved to a version holding the skill
// under another upstream directory than the update does. Its three
// versions are then merged again, which is what the conflicts, their
// numbering and which files are left are read from.
//
// An import branch that moved to another version since the merge was left
// pending, which only something outside agentx does, a legacy import
// branch that skill add or adopt writes again in today's form included, is
// no refusal: what the run resolves is written into the merge as it was
// recorded, and the merge is then merged again on the version the branch
// points at now, see rebase, keeping every resolution that still applies.
func (inv *invocation) readPending(ctx context.Context, gitDir string, rec lineage.Record) (*pendingRun, error) {
	name, p := rec.Name, *rec.PendingMerge
	abandon := "run '" + skillCommand("resolve", name, "--abort") + "' to give the merge up; the library directory stays as it is"
	switch {
	case !rec.HasImport:
		return nil, fail(exitRefused, fmt.Sprintf("the import branch %s records no version agentx can read", rec.Ref), abandon)
	case !p.Readable:
		return nil, fail(exitRefused, "the pending merge "+lineage.MergeRef(name)+" names no versions agentx can read", abandon)
	case rec.Commit == p.Merge.Theirs:
		return nil, fail(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved to the update the merge of "+name+" merges, so there is nothing left to merge", abandon)
	}
	r := &pendingRun{inv: inv, gitDir: gitDir, rec: rec, pending: p, dir: rec.Import.Dir(), resolved: map[string]bool{}}
	var err error
	if c := rec.Candidate; c != nil && c.Commit == p.Merge.Theirs && c.HasImport {
		r.theirs = c.Import
	} else if r.theirs, err = lineage.ReadImport(ctx, inv.git, gitDir, p.Merge.Theirs); err != nil {
		return nil, accountRepoFailure(fmt.Errorf("the update %s the pending merge of %s names: %w", p.Merge.Theirs, name, err))
	}
	if r.baseMoved() && r.theirs.Dir() != r.dir {
		// The update refuses a candidate held under another directory than
		// the import branch, so only a branch moved since gets here.
		return nil, fail(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved since the merge of "+name+" was left pending, to a version that holds the skill under another directory than the update, so the merge cannot be resolved",
			"run '"+skillCommand("resolve", name, "--abort")+"' to give it up, then '"+skillCommand("update", name)+"' to merge again")
	}
	res, err := mergeVersions(ctx, inv.git, gitDir, inv.tempDir(), r.dir, p.Merge)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	if len(res.files) == 0 {
		return nil, fail(exitRefused, "the versions the pending merge of "+name+" names no longer conflict when merged again", abandon+", then run '"+skillCommand("update", name)+"' again")
	}
	r.adopt(res, p.Resolved)
	return r, nil
}

// adopt makes res the merge the run resolves, the files of it listed in
// resolved being the ones resolved so far.
func (r *pendingRun) adopt(res mergeResult, resolved []string) {
	r.res, r.files, r.resolved = res, map[string]*conflictFile{}, map[string]bool{}
	for i := range r.res.files {
		r.files[r.res.files[i].Path] = &r.res.files[i]
	}
	for _, path := range resolved {
		if r.files[path] != nil {
			r.resolved[path] = true
		}
	}
}

// baseMoved reports whether the import branch no longer points at the base
// of the merge the pending merge commit records.
func (r *pendingRun) baseMoved() bool { return r.rec.Commit != r.pending.Merge.Base }

// branchMovedFailure refuses a run that found the import branch moved
// under the lock, after it read it: what it would write is a merge on a
// version the skill is no longer at. Run again, it merges on the one it is.
// doing is what the run was doing to the merge, as "resolved".
func branchMovedFailure(name, doing string) *failure {
	return refuse(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved while the merge of "+name+" was being "+doing+", so nothing was written",
		"run the command again to merge it on the version the branch points at now")
}

// mergeMovedFailure refuses a run whose merge ref no longer holds the
// commit it read: another run resolved or gave up part of the merge
// meanwhile, and what this one would write is no longer what it judged.
func mergeMovedFailure(name, what string) *failure {
	return refuse(exitRefused, "the merge of "+name+" changed while "+what+", so nothing was written",
		"run '"+skillCommand("resolve", name)+"' to see the merge as it is now")
}

// unresolved is every file of the merge that is left to resolve once the
// ones in also are, in path order: the files that conflict, less the ones
// the pending merge commit records as resolved.
func (r *pendingRun) unresolved(also map[string]bool) []conflictFile {
	files := []conflictFile{}
	for _, f := range r.res.files {
		if !r.resolved[f.Path] && !also[f.Path] {
			files = append(files, f)
		}
	}
	return files
}

// from and to are the upstream commits the merge is between, short.
func (r *pendingRun) from() string { return short(r.rec.Import.Commit) }
func (r *pendingRun) to() string   { return short(r.theirs.Commit) }

// show reports the merge as it stands and changes nothing: one conflict
// event with every file left to resolve and every hunk of it, as the update
// that left it reported them, numbered alike, and in the text the same
// lines the update printed under a line that says how many files are
// left. The merge shown is the one the pending merge commit records, whose
// numbering a --hunk is read against, so a warning says when the import
// branch moved since and the next run that resolves anything merges again.
func (r *pendingRun) show() error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
	r.warnBaseMoved()
	left := r.unresolved(nil)
	count := fmt.Sprintf("%d of %s unresolved", len(left), plural(len(r.res.files), "file"))
	inv.printConflicts(conflictOfSkill(name, r.pending.Merge, left),
		out.paint(heading, sanitised(name)), " has a merge pending with its update from ", r.from(), " to ", r.to(), ": ", out.paint(noteStyle, count))
	inv.summary = name + " has a merge pending with its update from " + r.from() + " to " + r.to() + ": " + count
	return nil
}

// warnBaseMoved warns, for a run that reports the merge as its pending
// merge commit records it, that the import branch moved since the merge was
// left pending, so that the files shown may not be the ones left once it is
// merged again.
func (r *pendingRun) warnBaseMoved() {
	if r.baseMoved() {
		r.inv.out.warn("the import branch " + lineage.ManagedRef(r.rec.Name) + " moved since the merge of " + r.rec.Name +
			" was left pending; the next run that resolves a file merges it again on the version the branch points at, keeping every resolution that still applies")
	}
}

// resolution is how one file of a pending merge is resolved: what the
// merge's tree holds at the file's path once it is. A text file's parts
// are assembled here, and its content is written before its blob is known.
type resolution struct {
	path  string
	entry *source.TreeEntry // the file or symlink put at the path, by mode and blob; nil for nothing
	keep  bool              // the directory the tree holds at the path stays, as one side has it
	body  *string           // what a text file resolved here holds, written into the account repo as entry's blob
	side  string            // the side every part of the file was resolved to, "" for parts resolved otherwise or an editor's save
}

// choose turns the --hunk choices into how each file they name is
// resolved, refusing with exit code 1, before anything is written, a
// choice the merge cannot take: a file of the skill that does not
// conflict, a part a file does not have, two sides for one part, both
// sides of a file that conflicts whole, and a file some of whose parts
// were given no side. A file resolved already can be resolved again.
//
// A file that conflicts whole has one part, 1, and takes one side: that
// side's version at the path, which is a directory where one side moved a
// directory in and git moved the other's file aside, and nothing where the
// side deleted the file or moved it away. A text file is assembled from
// what merge-file wrote, every part replaced by the side chosen for it and
// both by mine then theirs, see assemble.
func (r *pendingRun) choose(choices []hunkChoice) ([]resolution, error) {
	name := r.rec.Name
	see := "run '" + skillCommand("resolve", name) + "' to see every file left to resolve and its parts"
	sides := map[string]map[int]string{}
	for _, c := range choices {
		f := r.files[c.path]
		if f == nil {
			return nil, fail(exitUsage, fmt.Sprintf("%s does not conflict in the merge of %s", quotedPath(c.path), name), see)
		}
		parts, whole := len(f.Hunks), len(f.Hunks) == 0
		if whole {
			parts = 1
		}
		switch {
		case c.index > parts && whole:
			return nil, fail(exitUsage, fmt.Sprintf("%s conflicts as a whole file, so its one part is 1, not %d", quotedPath(c.path), c.index), see)
		case c.index > parts:
			return nil, fail(exitUsage, fmt.Sprintf("%s has %s, and no hunk %d", quotedPath(c.path), plural(parts, "hunk"), c.index), see)
		case whole && c.side == sideBoth:
			return nil, fail(exitUsage, fmt.Sprintf("%s conflicts as a whole file, so it is resolved to mine or theirs, not both", quotedPath(c.path)), see)
		}
		if sides[c.path] == nil {
			sides[c.path] = map[int]string{}
		}
		if had, ok := sides[c.path][c.index]; ok && had != c.side {
			return nil, fail(exitUsage, fmt.Sprintf("%s:%d is given both %s and %s", quotedPath(c.path), c.index, had, c.side), see)
		}
		sides[c.path][c.index] = c.side
	}
	paths := make([]string, 0, len(sides))
	for p := range sides {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var rs []resolution
	for _, p := range paths {
		f := r.files[p]
		parts := max(len(f.Hunks), 1)
		var missing []string
		for i := 1; i <= parts; i++ {
			if _, ok := sides[p][i]; !ok {
				missing = append(missing, strconv.Itoa(i))
			}
		}
		if len(missing) > 0 {
			return nil, fail(exitUsage, fmt.Sprintf("%s has %s and --hunk chooses no side for %s", quotedPath(p), plural(parts, "hunk"), joinWords(missing)),
				fmt.Sprintf("choose a side for every hunk of a file in the same run, as in --hunk %s:%s=mine", p, missing[0]))
		}
		if f.text == nil {
			rs = append(rs, wholeResolution(f, sides[p][1]))
			continue
		}
		chosen := make([]string, parts)
		for i := range chosen {
			chosen[i] = sides[p][i+1]
		}
		body := assemble(f.text, chosen)
		res := resolution{path: p, entry: &source.TreeEntry{Mode: resolvedMode(f, chosen)}, body: &body}
		if !slices.ContainsFunc(chosen, func(s string) bool { return s != chosen[0] }) {
			res.side = chosen[0]
		}
		rs = append(rs, res)
	}
	return rs, nil
}

// joinWords joins words as a sentence lists them: "1", "1 and 2", "1, 2
// and 3".
func joinWords(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// wholeResolution resolves a file that conflicts whole to side: the
// version merge-tree lists for that side, the directory the tree holds at
// the path when that side put a directory there and git moved the other's
// file aside, and nothing when the side deleted the file or moved it away.
func wholeResolution(f *conflictFile, side string) resolution {
	stage := gitStageMine
	if side == sideTheirs {
		stage = gitStageTheirs
	}
	if v, ok := f.stages[stage]; ok {
		return resolution{path: f.Path, entry: &source.TreeEntry{Mode: v.mode, OID: v.oid}, side: side}
	}
	return resolution{path: f.Path, keep: len(f.aside) > 0, side: side}
}

// assemble is the content of a text file whose parts are resolved to
// sides, one per hunk in order: what merge-file wrote, every hunk replaced
// by the side chosen for it, both being mine then theirs. A hunk that ends
// the file ends as the version chosen ends it, with a newline or without
// one, and where both are chosen mine keeps the line ending merge-file
// gives its last line, so that theirs starts a line of its own.
func assemble(t *textMerge, sides []string) string {
	var b strings.Builder
	n := len(t.raw)
	for i, h := range t.raw {
		b.WriteString(t.around[i])
		atEnd := i == n-1 && t.around[n] == ""
		mine, theirs := h.Mine, h.Theirs
		if atEnd {
			mine, theirs = asAtEnd(h.Mine, t.mine), asAtEnd(h.Theirs, t.theirs)
		}
		switch sides[i] {
		case sideMine:
			b.WriteString(mine)
		case sideTheirs:
			b.WriteString(theirs)
		default:
			first := h.Mine
			if first != "" && !strings.HasSuffix(first, "\n") {
				first += "\n" // a whole file with no newline at its end, which merge-file never ended
			}
			b.WriteString(first + theirs)
		}
	}
	b.WriteString(t.around[n])
	return b.String()
}

// resolvedMode is the mode a text file resolved to sides is written with,
// nil sides standing for content an editor saved: the three versions'
// modes merged as git merges them, a mode one side changed and the other
// did not being the changed one. Two sides that changed it differently,
// which only a file added on both sides can, take mine's unless every part
// was resolved to theirs.
func resolvedMode(f *conflictFile, sides []string) string {
	base, mine, theirs := f.stages[gitStageBase].mode, f.stages[gitStageMine].mode, f.stages[gitStageTheirs].mode
	switch {
	case mine == theirs, base == theirs:
		return mine
	case base == mine:
		return theirs
	case len(sides) > 0 && !slices.ContainsFunc(sides, func(s string) bool { return s != sideTheirs }):
		return theirs
	}
	return mine
}

// resolve writes how the files rs name are resolved into the pending merge.
// A resolution that would put something inside a file the merge holds is
// refused first, with nothing written, see inFile. The content of every
// text file assembled or edited here is written into the account repo,
// outside the lock. The resolutions are written into the tree of the
// pending merge commit, see applyResolutions, and a merge whose import
// branch moved since is then merged again, see rebase. Otherwise a merge
// that still has a file left to resolve is rewritten: the tree of the merge
// with every resolution in it and the commit over it are written, see
// commit, and the merge ref is moved to it, see publish. One that has none
// left is completed in the same run, see complete.
func (r *pendingRun) resolve(ctx context.Context, rs []resolution) error {
	git, gitDir := r.inv.git, r.gitDir
	held, err := lineage.ReadMerged(ctx, git, gitDir, r.pending.Commit, r.dir)
	if err != nil {
		return accountRepoFailure(err)
	}
	if err := r.inFile(held, rs); err != nil {
		return err
	}
	var bodies []string
	for _, res := range rs {
		if res.body != nil {
			bodies = append(bodies, *res.body)
		}
	}
	ids, err := lineage.WriteContent(ctx, git, gitDir, bodies)
	if err != nil {
		return accountRepoFailure(err)
	}
	for i := range rs {
		if rs[i].body != nil {
			rs[i].entry.OID, ids = ids[0], ids[1:]
		}
	}
	// The tree merge-tree wrote is read only for a path resolved to the
	// directory one side has there, see applyResolutions.
	var fresh lineage.Base
	if slices.ContainsFunc(rs, func(res resolution) bool { return res.keep }) {
		if fresh, err = lineage.ReadMerged(ctx, git, gitDir, r.res.tree, r.dir); err != nil {
			return accountRepoFailure(err)
		}
	}
	v, reopened := applyResolutions(held, fresh, r.files, rs)
	for _, p := range reopened {
		delete(r.resolved, p)
	}
	now := map[string]bool{}
	for _, res := range rs {
		now[res.path] = true
	}
	if r.baseMoved() {
		return r.rebase(ctx, v, rs, now)
	}
	resolved := make([]string, 0, len(r.resolved)+len(now))
	for _, f := range r.res.files {
		if r.resolved[f.Path] || now[f.Path] {
			resolved = append(resolved, f.Path)
		}
	}
	left := r.unresolved(now)
	if len(left) == 0 {
		return r.complete(ctx, v, r.pending.Merge, nil)
	}
	commit, err := r.commit(ctx, v, r.pending.Merge, len(left), resolved)
	if err != nil {
		return err
	}
	if err := r.publish(ctx, commit, r.pending.Merge.Base, "resolved"); err != nil {
		return err
	}
	return r.reportResolved(rs, r.pending.Merge, left)
}

// inFile refuses, with exit code 1, a resolution that puts a file, a
// symlink or a directory inside a path the merge holds a file or a symlink
// at once rs is written into held, the pending merge's version: a path this
// run resolves so, or one an earlier run did and this one leaves alone.
// Written, it would make that path a directory, and the file resolved
// there would go with no word while the merge still listed it as resolved.
// A path resolved to the directory one side holds there, or to nothing,
// has room for it. The hint names the side that keeps a directory at the
// path, where one side does.
func (r *pendingRun) inFile(held lineage.Base, rs []resolution) error {
	now := map[string]resolution{}
	for _, res := range rs {
		now[res.path] = res
	}
	was := map[string]source.TreeEntry{}
	for _, e := range held.Entries {
		was[e.Path] = e
	}
	sorted := slices.Clone(rs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })
	for _, res := range sorted {
		if res.entry == nil && !res.keep {
			continue
		}
		for dir := path.Dir(res.path); dir != "."; dir = path.Dir(dir) {
			e, side := was[dir], ""
			if at, ok := now[dir]; ok {
				e, side = source.TreeEntry{Mode: source.DirMode}, at.side
				if at.entry != nil {
					e = *at.entry
				}
			}
			if e.Mode == "" || e.Mode == source.DirMode {
				continue
			}
			return r.fileInTheWay(dir, e, side, res)
		}
	}
	return nil
}

// fileInTheWay is inFile's refusal of res, whose path is inside dir, which
// the merge holds e at, the side it was resolved to being side, "" when the
// run did not resolve it and the side is then read from the versions of
// the file that conflicts there.
func (r *pendingRun) fileInTheWay(dir string, e source.TreeEntry, side string, res resolution) error {
	f := r.files[dir]
	stages := [][2]string{{gitStageMine, sideMine}, {gitStageTheirs, sideTheirs}}
	if side == "" && f != nil {
		for _, st := range stages {
			if v, ok := f.stages[st[0]]; ok && v.mode == e.Mode && v.oid == e.OID {
				side = st[1]
				break
			}
		}
	}
	held := entryKind(e.Mode)
	if side != "" {
		held = side + ", " + held
	}
	to := "a version of its own"
	if res.side != "" {
		to = res.side
	}
	message := fmt.Sprintf("%s is resolved to %s, so %s cannot be resolved to %s inside it", quotedPath(dir), held, quotedPath(res.path), to)
	// The side with no version at the path, where git moved the other
	// side's aside, is the one that keeps a directory there.
	var other string
	if f != nil && len(f.aside) > 0 {
		for _, st := range stages {
			if _, ok := f.stages[st[0]]; !ok {
				other = st[1]
				break
			}
		}
	}
	if other == "" {
		return fail(exitUsage, message, "run '"+skillCommand("resolve", r.rec.Name)+"' to see every file left to resolve and its parts")
	}
	example := fmt.Sprintf("--hunk %s:1=%s", dir, other)
	if g := r.files[res.path]; g != nil && g.text == nil && res.side != "" {
		example += fmt.Sprintf(" --hunk %s:1=%s", res.path, res.side)
	}
	return fail(exitUsage, message, fmt.Sprintf("resolve %s to %s, which keeps a directory there, first or in the same run, as in %s", quotedPath(dir), other, example))
}

// applyResolutions is the version a pending merge holds, as ReadMerged read
// it out of its tree, held, with rs written into it, in path order: every
// path git moved a version of a resolved file aside to goes, and the path
// itself, with anything below it, takes the version the file is resolved
// to, a directory it needs to sit in included, or keeps the directory the
// side resolved to holds there. files are the files of the merge the tree
// comes from, by path, and fresh the tree merge-tree wrote for it, before
// anything was resolved.
//
// A path resolved to a directory keeps the one held holds there. Where held
// holds anything else, the path having been resolved to the other side's
// file before, the directory is taken again from fresh, which holds it as
// git kept it for the side that has it; a file of the merge inside it
// whose resolution went with it when the file replaced the directory, and
// that rs does not resolve again, is left to resolve again, and reopened
// names it. So is a file of the merge inside a path resolved to a file, or
// to nothing, which takes everything below it away. inFile has refused
// anything rs puts inside a file already, so the directory a path needs
// to sit in never replaces one. A directory left with nothing below it,
// its last file resolved to the side that deleted it, goes too.
func applyResolutions(held, fresh lineage.Base, files map[string]*conflictFile, rs []resolution) (v lineage.Base, reopened []string) {
	entries := map[string]source.TreeEntry{}
	for _, e := range held.Entries {
		entries[e.Path] = e
	}
	drop := func(p string) {
		delete(entries, p)
		for k := range entries {
			if strings.HasPrefix(k, p+"/") {
				delete(entries, k)
			}
		}
	}
	parents := func(p string) {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if e, ok := entries[dir]; !ok || e.Mode != source.DirMode {
				entries[dir] = source.TreeEntry{Path: dir, Mode: source.DirMode}
			}
		}
	}
	sorted := slices.Clone(rs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })
	now := map[string]bool{}
	for _, res := range sorted {
		now[res.path] = true
	}
	reopen := func(dir string) {
		for p := range files {
			if strings.HasPrefix(p, dir+"/") && !now[p] {
				reopened = append(reopened, p)
			}
		}
	}
	for _, res := range sorted {
		if f := files[res.path]; f != nil {
			for _, aside := range f.aside {
				drop(aside)
			}
		}
		if res.keep {
			if e, ok := entries[res.path]; ok && e.Mode == source.DirMode {
				continue
			}
			drop(res.path)
			for _, e := range fresh.Entries {
				if e.Path == res.path || strings.HasPrefix(e.Path, res.path+"/") {
					entries[e.Path] = e
				}
			}
			parents(res.path)
			reopen(res.path)
			continue
		}
		drop(res.path)
		reopen(res.path)
		if res.entry == nil {
			continue
		}
		parents(res.path)
		e := *res.entry
		e.Path = res.path
		entries[res.path] = e
	}
	// A directory with no file left below it goes, as git records none and
	// no tree of the merge holds one: laid out, it would be a directory
	// neither side has.
	filled := map[string]bool{}
	for p, e := range entries {
		if e.Mode == source.DirMode {
			continue
		}
		for dir := path.Dir(p); dir != "." && !filled[dir]; dir = path.Dir(dir) {
			filled[dir] = true
		}
	}
	for p, e := range entries {
		if e.Mode == source.DirMode && !filled[p] {
			delete(entries, p)
		}
	}
	v = lineage.Base{Tree: held.Tree, Entries: make([]source.TreeEntry, 0, len(entries))}
	for _, e := range entries {
		v.Entries = append(v.Entries, e)
	}
	sort.Slice(v.Entries, func(i, j int) bool { return v.Entries[i].Path < v.Entries[j].Path })
	sort.Strings(reopened)
	return v, reopened
}

// mergedAgain is a pending merge merged again because one of its three
// versions moved since the pending merge commit was written: what
// merge-tree made of the three versions now, and the files of it.
type mergedAgain struct {
	m        lineage.Merge
	res      mergeResult
	v        lineage.Base   // the tree merge-tree wrote, every resolution kept written into it
	resolved []string       // the files whose resolution was kept, in path order
	left     []conflictFile // every other file that conflicts, in path order
	anew     bool           // a file left conflicts otherwise than it did in the merge the run read
}

// mergeAgain merges m, the merge the run read with one of its versions
// moved, the base or mine, and carries over what v resolved: a file that
// conflicts as it did, its base, mine and theirs the versions they were,
// keeps the way v resolves it when it is one of done, the files resolved
// by this run or an earlier one. Every other file that conflicts, one the
// move changed, a new one, or one not resolved yet, is left to resolve.
func (r *pendingRun) mergeAgain(ctx context.Context, v lineage.Base, m lineage.Merge, done map[string]bool) (mergedAgain, error) {
	git, gitDir := r.inv.git, r.gitDir
	res, err := mergeVersions(ctx, git, gitDir, r.inv.tempDir(), r.dir, m)
	if err != nil {
		return mergedAgain{}, accountRepoFailure(err)
	}
	merged, err := lineage.ReadMerged(ctx, git, gitDir, res.tree, r.dir)
	if err != nil {
		return mergedAgain{}, accountRepoFailure(err)
	}
	again := mergedAgain{m: m, res: res, left: []conflictFile{}}
	files := map[string]*conflictFile{}
	var kept []resolution
	for i, f := range res.files {
		files[f.Path] = &res.files[i]
		was := r.files[f.Path]
		same := was != nil && sameStages(was.stages, f.stages)
		if same && done[f.Path] {
			kept = append(kept, resolutionIn(v, f.Path))
			again.resolved = append(again.resolved, f.Path)
			continue
		}
		again.left = append(again.left, f)
		again.anew = again.anew || !same
	}
	again.v, _ = applyResolutions(merged, merged, files, kept)
	return again, nil
}

// write rewrites the pending merge as again merged it: the commit over its
// tree, which names its three versions and lists the files whose
// resolution it kept, and the merge ref moved to it, see publish. doing is
// what the run was doing to the merge, for a refusal.
func (r *pendingRun) write(ctx context.Context, again mergedAgain, doing string) error {
	commit, err := r.commit(ctx, again.v, again.m, len(again.left), again.resolved)
	if err != nil {
		return err
	}
	if err := r.publish(ctx, commit, again.m.Base, doing); err != nil {
		return err
	}
	r.note(again)
	return nil
}

// note records, once the run wrote the merge as again merged it or
// completed it so, which files kept their resolution and which are left to
// resolve, none for a merge completed: every other file of the merge the
// run read merges cleanly now, git's merge of it written, see edit.
func (r *pendingRun) note(again mergedAgain) {
	r.carried, r.left = map[string]bool{}, map[string]bool{}
	for _, p := range again.resolved {
		r.carried[p] = true
	}
	for _, f := range again.left {
		r.left[f.Path] = true
	}
}

// rebase merges again, on the version the import branch points at now, a
// merge whose base moved since it was left pending, once v holds what the
// run resolved, rs, now naming their files: the base is that version, and
// mine and theirs are the ones the merge names, see mergeAgain. When no
// file is left to resolve, the merge completes on that base. Otherwise it
// is rewritten, see write: a merge whose files left conflict as they did
// is reported as any rewritten merge is, and one where a file conflicts
// anew, or lost its resolution, emits a conflict event with every file left
// and exits 4, since what the run chose was read against a merge that is
// no longer the one pending.
func (r *pendingRun) rebase(ctx context.Context, v lineage.Base, rs []resolution, now map[string]bool) error {
	inv, name := r.inv, r.rec.Name
	done := maps.Clone(now)
	for p := range r.resolved {
		done[p] = true
	}
	m := lineage.Merge{Base: r.rec.Commit, Mine: r.pending.Merge.Mine, Theirs: r.pending.Merge.Theirs}
	again, err := r.mergeAgain(ctx, v, m, done)
	if err != nil {
		return err
	}
	if len(again.left) == 0 {
		r.adopt(again.res, again.resolved)
		return r.complete(ctx, again.v, m, &again)
	}
	if err := r.write(ctx, again, "resolved"); err != nil {
		return err
	}
	if !again.anew {
		return r.reportResolved(rs, m, again.left)
	}
	out := inv.out
	files := plural(len(again.left), "file")
	inv.printConflicts(conflictOfSkill(name, m, again.left),
		"The import branch of ", out.paint(heading, sanitised(name)), " moved while its merge was pending, and merging it again with its update from ",
		r.from(), " to ", r.to(), " leaves ", out.paint(noteStyle, files), " to resolve")
	return refuse(exitPendingMerge, "the import branch "+lineage.ManagedRef(name)+" moved while the merge of "+name+" was pending, and merging it again leaves "+files+" to resolve, so the merge is still pending",
		"run '"+skillCommand("resolve", name)+"' to see every file left to resolve")
}

// commit writes v as the tree of a pending merge of m, and the pending
// merge commit over it: m's Mine and Theirs its parents, the trailers
// naming m, the subject counting the files left and the body listing the
// ones resolved. Nothing holds it until publish points the merge ref at it.
func (r *pendingRun) commit(ctx context.Context, v lineage.Base, m lineage.Merge, left int, resolved []string) (string, error) {
	tree, err := lineage.WriteResolved(ctx, r.inv.git, r.gitDir, r.dir, v)
	if err != nil {
		return "", accountRepoFailure(err)
	}
	commit, err := lineage.CommitPending(ctx, r.inv.git, r.gitDir, tree, r.rec.Name, m, left, resolved)
	if err != nil {
		return "", accountRepoFailure(err)
	}
	return commit, nil
}

// publish points the merge ref at commit, under the lock and through the
// journal, with the commit the run read as its expected old value: one ref
// step, the whole of the rewrite. The merge ref and the import branch are
// read again first, and a merge ref another run moved meanwhile, or an
// import branch no longer at base, the base of the merge commit is of,
// refuses the run with nothing written. doing says what the run was doing
// to the merge, as "resolved", for the refusal.
func (r *pendingRun) publish(ctx context.Context, commit, base, doing string) error {
	inv, name := r.inv, r.rec.Name
	if commit == r.pending.Commit {
		return nil
	}
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		values, err := inv.lineageRefs(ctx, r.gitDir, name)
		if err != nil {
			return err
		}
		switch {
		case values[lineage.MergeRef(name)] != r.pending.Commit:
			return mergeMovedFailure(name, "it was being "+doing)
		case values[lineage.ManagedRef(name)] != base || values[lineage.ForkRef(name)] != "":
			return branchMovedFailure(name, doing)
		}
		m := home.NewMutation(inv.dirs.Home)
		m.Ref(r.gitDir, lineage.MergeRef(name), r.pending.Commit, commit)
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return mutationFailure(err)
	}
	return nil
}

// reportResolved reports a run that resolved the files rs name and left
// the merge of m pending with left still to resolve: a conflict event with
// those files and every hunk of them, and the line that says what the run
// did.
func (r *pendingRun) reportResolved(rs []resolution, m lineage.Merge, left []conflictFile) error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
	out.emit(conflictOfSkill(name, m, left))
	what := plural(len(rs), "file")
	if len(rs) == 1 {
		what = quotedPath(rs[0].path)
	}
	rest := ", " + plural(len(left), "file") + " left to resolve"
	inv.summary = "resolved " + what + " in the merge of " + name + rest
	out.done("resolved " + what + " in the merge of " + out.paint(heading, sanitised(name)) + out.paint(noteStyle, rest))
	return nil
}

// complete completes a merge of m that has no file left to resolve once it
// holds v, the skill's directory as the merge resolves it, in the same run
// that resolved its last file. m is the merge the pending merge commit
// names, or the one a moved import branch made of it, see rebase, merged
// being then that merge merged again, and nil otherwise.
//
// First the library directory is judged, outside the lock, as an update
// judges it: one that is gone, is a symlink, or holds something git cannot
// record is refused with nothing written, and the run can be made again
// once that is put right. Then it is compared with mine, the directory as
// the update read it, by tree id: the directory's own, wrapped in the
// upstream directory, against the tree of the commit m names as mine, the
// one Agentx-Merge-Mine names. A directory edited while the merge was
// pending, by any tool, is merged again, see remerge, and that edit is
// never lost. The merge is then applied, see apply.
func (r *pendingRun) complete(ctx context.Context, v lineage.Base, m lineage.Merge, merged *mergedAgain) error {
	inv, name := r.inv, r.rec.Name
	again := "run the command again"
	lib, held := librarySkill(inv.dirs.Library, name)
	if !held {
		sc := skillContext{records: map[string]lineage.Record{name: r.rec}}
		what, wayOut := sc.absentNotice(inv, name)
		return fail(exitRefused, what+", so its merge cannot be completed",
			"run '"+skillCommand("resolve", name, "--abort")+"' to give the merge up first; then "+wayOut)
	}
	libPath := inv.libraryPath(name)
	captured, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	if target, isLink := home.LinkTarget(captured); isLink {
		return fail(exitRefused, fmt.Sprintf("%s is a symlink to %s; completing the merge replaces the library directory and would drop the link without touching the files it leads to", quotedPath(libPath), quotedPath(target)),
			"replace the link with the directory it points to, then "+again)
	}
	tree, err := inv.readLibraryTree(lib.Path)
	if err != nil {
		return err
	}
	if len(tree.Unrecordable) > 0 {
		return unrecordableThen(name, libPath, tree.Unrecordable, "completing the merge", again)
	}
	mine, err := inv.git.Isolated(ctx, r.gitDir, "rev-parse", m.Mine+"^{tree}")
	if err != nil {
		return accountRepoFailure(err)
	}
	c := completion{v: v, m: m, libPath: libPath, captured: captured, tree: tree, again: merged}
	if treeid.Wrap(r.dir, tree.ID) != mine {
		done, err := r.remerge(ctx, &c)
		if done || err != nil {
			return err
		}
	}
	return r.apply(ctx, c)
}

// completion is what completing a merge lays out and replaces: the
// version the merge resolves to, the merge it is of, which is another one
// than the pending merge's when the library directory was merged again,
// and the library directory as it was read.
type completion struct {
	v        lineage.Base
	m        lineage.Merge
	libPath  string
	captured string       // the library entry, in the words a journal records
	tree     treeid.Tree  // what the library directory holds, as git would record it
	remerged bool         // the directory was edited while the merge was pending, and merged again
	again    *mergedAgain // the merge merged again that is completed, on a moved import branch or an edited directory; nil for the one the pending merge commit records
}

// remerge merges again a merge whose library directory was edited while it
// was pending: the directory is written into the account repo as mine, as
// the update wrote it, on the same base, and merged with the same update.
// A file that conflicts as it did, its base, mine and theirs the versions
// they were, keeps the way it was resolved, whether this run or an earlier
// one resolved it, see mergeAgain; every other file that conflicts, one
// the edit changed or a new one, is left to resolve. When none is, the
// merge completes with the new mine, and done is false; when some are, the
// merge is rewritten with the new mine as its first parent and in its
// trailer, and with the resolutions it kept, see write, and the run
// reports those files alone and exits 4.
func (r *pendingRun) remerge(ctx context.Context, c *completion) (done bool, err error) {
	inv, git, gitDir, name := r.inv, r.inv.git, r.gitDir, r.rec.Name
	root, err := filepath.EvalSymlinks(c.libPath)
	if err != nil {
		return false, libraryFailure(inv.dirs.Library, err)
	}
	mine, err := lineage.WriteMine(ctx, git, gitDir, root, c.tree, r.rec)
	if errors.Is(err, lineage.ErrChanged) {
		return false, changedWhileCompleting(name)
	}
	if err != nil {
		return false, accountRepoFailure(err)
	}
	m := lineage.Merge{Base: c.m.Base, Mine: mine, Theirs: c.m.Theirs}
	every := map[string]bool{} // a merge is completed only once every file of it is resolved
	for p := range r.files {
		every[p] = true
	}
	again, err := r.mergeAgain(ctx, c.v, m, every)
	if err != nil {
		return false, err
	}
	if len(again.left) == 0 {
		c.v, c.m, c.remerged, c.again = again.v, m, true, &again
		return false, nil
	}
	if err := r.write(ctx, again, "completed"); err != nil {
		return false, err
	}
	out, files := inv.out, plural(len(again.left), "file")
	inv.printConflicts(conflictOfSkill(name, m, again.left),
		out.paint(heading, sanitised(name)), " was edited while its merge was pending, and merging it again with its update from ",
		r.from(), " to ", r.to(), " conflicts in ", out.paint(noteStyle, files))
	return true, refuse(exitPendingMerge, name+" was edited while its merge was pending, and merging it again conflicts in "+files+", so the merge is still pending",
		"run '"+skillCommand("resolve", name)+"' to see every file left to resolve")
}

// sameStages reports whether a file conflicts with the same three versions
// in two merges, mode and blob of each.
func sameStages(a, b map[string]staged) bool {
	if len(a) != len(b) {
		return false
	}
	for stage, v := range a {
		if w, ok := b[stage]; !ok || w != v {
			return false
		}
	}
	return true
}

// resolutionIn is how the version v resolved the file at p: the file or
// symlink it holds there, the directory it holds there, or nothing.
func resolutionIn(v lineage.Base, p string) resolution {
	for _, e := range v.Entries {
		if e.Path != p {
			continue
		}
		if e.Mode == source.DirMode {
			return resolution{path: p, keep: true}
		}
		held := e
		return resolution{path: p, entry: &held}
	}
	return resolution{path: p}
}

// changedWhileCompleting refuses a completion whose library directory
// changed after the run read it: what the merge would replace is no
// longer what it judged, and an edit made meanwhile is never replaced.
func changedWhileCompleting(name string) *failure {
	return refuse(exitRefused, name+" changed while its merge was being completed, so nothing was changed",
		"run the command again to complete the merge with the skill as it is now")
}

// apply completes a merge as one journaled mutation, as an update applies
// a clean merge, the version laid out being the one the merge resolves to:
// the import branch moves from the base to the update the merge merged,
// the one Agentx-Merge-Theirs names and never the candidate a check may
// have moved on since, with the base as its expected old value; the
// library directory is retained and replaced by the version laid out
// beside it; the copies that held the library directory as the merge read
// it, or the base version, are refreshed and every other copy is kept; the
// candidate is deleted only when it still is that update, a newer one
// staying as the skill's next update; and the merge ref is deleted last,
// with the commit the run read as its expected old value.
//
// A version with no SKILL.md, which resolving SKILL.md to the side that
// deleted it leaves, is refused before anything is written: laid out, it
// would take the skill out of every client. Everything is read again under
// the lock first: the import branch, the merge ref and the library
// directory, whose content and tree must be the ones the run judged. What
// the library directory holds unchanged keeps its permissions, see
// keepPerms. Once it is written, the directories editor sessions of the
// skill left go, see clearEditorDirs, but for one an earlier session whose
// run is gone typed something in, which no warning named before this run,
// and which is given up to the user and named in a warning.
func (r *pendingRun) apply(ctx context.Context, c completion) error {
	inv, git, gitDir, name := r.inv, r.inv.git, r.gitDir, r.rec.Name
	target := c.v.ID()
	if len(skillFileBlob(c.v)) == 0 {
		return fail(exitRefused, "the merge of "+name+" resolves to a directory with no SKILL.md, which no client would read as a skill, so nothing was changed",
			"resolve SKILL.md to the side that keeps it, or run '"+skillCommand("resolve", name, "--abort")+"' to give the merge up")
	}
	if f := r.caseClash(c); f != nil {
		return f
	}
	base, err := lineage.ReadBase(ctx, git, gitDir, r.rec)
	if err != nil {
		return accountRepoFailure(err)
	}
	next := lineage.Record{Name: name, Kind: r.rec.Kind, Ref: r.rec.Ref, Commit: c.m.Theirs, Import: r.theirs, HasImport: true}
	theirs, err := lineage.ReadBase(ctx, git, gitDir, next)
	if err != nil {
		return accountRepoFailure(err)
	}
	bodies, err := source.ReadBlobs(ctx, git, gitDir, append(baseBlobs(c.v), skillFileBlob(theirs)...))
	if err != nil {
		return accountRepoFailure(err)
	}
	var done placements
	newer := false
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		values, err := inv.lineageRefs(ctx, gitDir, name)
		if err != nil {
			return err
		}
		switch {
		case values[lineage.MergeRef(name)] != r.pending.Commit:
			return mergeMovedFailure(name, "it was being completed")
		case values[lineage.ManagedRef(name)] != c.m.Base || values[lineage.ForkRef(name)] != "":
			return branchMovedFailure(name, "completed")
		}
		live, err := home.State(c.libPath)
		if err != nil {
			return libraryFailure(inv.dirs.Library, err)
		}
		if live != c.captured {
			return changedWhileCompleting(name)
		}
		now, err := inv.readLibraryTree(c.libPath)
		if err != nil {
			return err
		}
		if now.ID != c.tree.ID || len(now.Unrecordable) > 0 {
			return changedWhileCompleting(name)
		}
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		recorded := edit.copiesOf(name)
		// A completion killed before its journal was written left what it
		// staged with nothing to name it, beside the library directory and
		// beside each copy it was refreshing; all of it is swept first.
		sweepStaged(inv.dirs.Library)
		for _, t := range inv.detectedTargets() {
			if !t.readsLibrary && slices.Contains(recorded, t.id) {
				sweepStaged(t.dir)
			}
		}
		m := home.NewMutation(inv.dirs.Home)
		staged := m.Sibling(c.libPath, "staged")
		fingerprint, err := stageBase(staged, c.v, target, bodies)
		if err == nil {
			err = keepPerms(staged, c.libPath, c.tree, c.v)
		}
		if err != nil {
			os.RemoveAll(staged)
			return libraryFailure(inv.dirs.Library, err)
		}
		m.Ref(gitDir, lineage.ManagedRef(name), c.m.Base, c.m.Theirs)
		m.Remove(c.libPath, c.captured)
		m.Publish(c.libPath, staged, fingerprint)
		inv.refreshCopies(m, name, target, []string{c.tree.ID, base.ID()}, "", staged, recorded, &done)
		switch candidate := values[lineage.CandidateRef(name)]; candidate {
		case c.m.Theirs:
			m.Ref(gitDir, lineage.CandidateRef(name), candidate, "")
		case "":
		default:
			newer = true
		}
		m.Ref(gitDir, lineage.MergeRef(name), r.pending.Commit, "")
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return mutationFailure(err)
	}
	r.completed = true
	if c.again != nil {
		r.note(*c.again)
	}
	for _, dir := range inv.clearEditorDirs(name, r.turned) {
		inv.out.warn(leftToYou(dir, name))
	}
	return r.reportCompleted(ctx, c, newer, upstreamRename(name, skillName(theirs, bodies), next.Import.Dir()), done)
}

// caseClash refuses a completion whose version holds two paths that differ
// in case alone where the library's file system cannot keep them apart,
// as an update refuses a clean merge that does: laid out, one would
// overwrite the other. Nothing is written, and the hint says how to get out
// of it: the merge is given up, and the path of the two the library
// directory holds is renamed before the next update.
func (r *pendingRun) caseClash(c completion) *failure {
	a, b, ok := caseClash(c.v.Entries)
	if !ok {
		return nil
	}
	root, err := filepath.EvalSymlinks(c.libPath)
	if err != nil || !foldsCase(root, c.tree) {
		return nil
	}
	name := r.rec.Name
	return refuse(exitRefused, fmt.Sprintf("the merge of %s holds both %s and %s, which this file system cannot keep apart, so nothing was changed", name, quotedPath(a), quotedPath(b)),
		"resolve one of them to the other side, or run '"+skillCommand("resolve", name, "--abort")+"' to give the merge up and rename the one in "+quotedPath(c.libPath)+" before you update again")
}

// reportCompleted reads the machine again and reports the skill as the
// completed merge leaves it: a warning when the update names it otherwise,
// its library_skill event with every placement, as skill list reports
// them, and the line that says what the run did. newer says that a check
// found a newer update meanwhile, which stays.
func (r *pendingRun) reportCompleted(ctx context.Context, c completion, newer bool, upstreamName string, done placements) error {
	inv, name := r.inv, r.rec.Name
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return fail(exitInternal, "the library holds no "+name+" after completing its merge", "run 'agentx doctor' and check the library it names")
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	if upstreamName != "" {
		inv.out.warn(renameWarning(name, upstreamName))
	}
	inv.out.emit(sc.librarySkillEventFor(inv, snap, lib, nil))
	out := inv.out
	how := " and updated it"
	if c.remerged {
		how = ", merged the edits made to it meanwhile and updated it"
	}
	moved := " from " + r.from() + " to " + r.to()
	plain, painted := copiesNote(out, len(done.copies), len(done.skipped))
	var later string
	if newer {
		later = "; the newer update the last check found stays for the next update"
	}
	inv.summary = "resolved " + name + how + moved + plain + later
	out.done("resolved " + out.paint(heading, sanitised(name)) + how + moved + painted + later)
	return nil
}

// abortMerge gives up the merge pending for the skill called name, as one
// journaled mutation whose one step deletes the merge ref, with the commit
// it holds under the lock as its expected old value, whatever that commit
// is and whether or not agentx can read it. The library directory, the
// import branch and the candidate are left exactly as they were, and so is
// every placement. So is what was typed in an editor session of the skill:
// a directory of one holding anything typed is kept, and named in a
// warning, so that the next editor session of the same conflict opens it
// again once an update leaves the merge pending anew, see keepEditorDirs;
// one that holds nothing typed goes. The warning says that a kept one
// stays until a merge of the skill completes, which removes it as it
// removes every kept one, see clearEditorDirs. The skill is reported as it
// now stands.
func (inv *invocation) abortMerge(ctx context.Context, gitDir, name string) error {
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		values, err := inv.lineageRefs(ctx, gitDir, name)
		if err != nil {
			return err
		}
		held := values[lineage.MergeRef(name)]
		if held == "" {
			return fail(exitRefused, name+" has no merge pending", "run 'agentx skill list' to see which skills have one")
		}
		m := home.NewMutation(inv.dirs.Home)
		m.Ref(gitDir, lineage.MergeRef(name), held, "")
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return mutationFailure(err)
	}
	for _, dir := range inv.keepEditorDirs(name) {
		inv.out.warn("the files you edited are kept in " + dir + " until a merge of " + name + " completes: when an update of " + name +
			" conflicts the same way, '" + skillCommand("resolve", name, "--editor") + "' opens what you typed again; delete the folder if you do not need it")
	}
	const kept = "; the library directory is as it was"
	inv.summary = "gave up the merge of " + name + kept
	if lib, ok := librarySkill(inv.dirs.Library, name); ok {
		snap, err := inv.scan(ctx, lockWait, "", false)
		if err != nil {
			return err
		}
		sc, err := inv.skillContext(ctx)
		if err != nil {
			return err
		}
		inv.out.emit(sc.librarySkillEventFor(inv, snap, lib, nil))
	}
	inv.out.done("gave up the merge of " + inv.out.paint(heading, sanitised(name)) + kept)
	return nil
}
