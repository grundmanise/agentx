package cli

import (
	"context"
	"errors"
	"fmt"
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
		Use:   "resolve <name> [--hunk <file>:<index>=mine|theirs|both]... [--editor [<file>]] [--abort]",
		Short: "Resolve, or give up, the merge an update left pending for a skill you edited",
		Long: "Show and resolve the conflicts an update found between the edits of a managed skill\n" +
			"and its newer version. With no flag the files left to resolve are shown and nothing\n" +
			"changes. --hunk <file>:<index>=mine|theirs|both chooses one part of a file, numbered\n" +
			"as shown, and can be given once per part: every part of a file is chosen in the same\n" +
			"run. --editor opens every text file left to resolve, or the one named, in your editor\n" +
			"with conflict markers: GIT_EDITOR, else EDITOR, else 'code --wait'. Once no file is\n" +
			"left the merge completes: the library directory takes the merged version and the\n" +
			"skill is at its update. --abort gives the merge up and leaves the library directory\n" +
			"as it is. Agents never see a half-merged file: the merge waits in the account repo.",
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
	cmd.Flags().BoolVar(&editor, "editor", false, "open the files left to resolve, or the one named after it, in your editor")
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
		case clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/"):
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
// as one whose base moved. Then the skill is judged, in this order: a name
// the library does not hold and no lineage names is exit code 5; a fork,
// a skill with no import branch and one with no merge pending are exit
// code 6. --abort gives up any merge the ref holds, whatever else is true
// of it; everything else reads the merge first, see readPending.
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
	inv      *invocation
	gitDir   string
	rec      lineage.Record           // the import branch, at the merge's base
	pending  lineage.PendingMerge     // what the merge ref held
	dir      string                   // the upstream directory every tree of the merge wraps the skill in
	res      mergeResult              // the merge of the three versions, run again
	files    map[string]*conflictFile // the files that conflict, by path
	resolved map[string]bool          // the files the pending merge commit records as resolved
	theirs   lineage.Import           // the lineage of the update merged
}

// readPending reads the merge a skill's merge ref holds, refusing one that
// cannot be resolved with exit code 6 and the hint that gives it up: an
// import branch or a pending merge commit agentx cannot read, and a merge
// whose base is no longer the version the import branch points at, which
// only something outside agentx does, since every command that moves the
// branch is refused while the merge is pending. Its three versions are
// then merged again, which is what the conflicts, their numbering and which
// files are left are read from.
func (inv *invocation) readPending(ctx context.Context, gitDir string, rec lineage.Record) (*pendingRun, error) {
	name, p := rec.Name, *rec.PendingMerge
	abandon := "run '" + skillCommand("resolve", name, "--abort") + "' to give the merge up; the library directory stays as it is"
	switch {
	case !rec.HasImport:
		return nil, fail(exitRefused, fmt.Sprintf("the import branch %s records no version agentx can read", rec.Ref), abandon)
	case !p.Readable:
		return nil, fail(exitRefused, "the pending merge "+lineage.MergeRef(name)+" names no versions agentx can read", abandon)
	case rec.Commit != p.Merge.Base:
		return nil, baseMovedFailure(name)
	}
	r := &pendingRun{inv: inv, gitDir: gitDir, rec: rec, pending: p, dir: rec.Import.Dir(), resolved: map[string]bool{}}
	res, err := mergeVersions(ctx, inv.git, gitDir, r.dir, p.Merge)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	if len(res.files) == 0 {
		return nil, fail(exitRefused, "the versions the pending merge of "+name+" names no longer conflict when merged again", abandon+", then run '"+skillCommand("update", name)+"' again")
	}
	r.res, r.files = res, map[string]*conflictFile{}
	for i := range res.files {
		r.files[res.files[i].Path] = &r.res.files[i]
	}
	for _, path := range p.Resolved {
		if r.files[path] != nil {
			r.resolved[path] = true
		}
	}
	if c := rec.Candidate; c != nil && c.Commit == p.Merge.Theirs && c.HasImport {
		r.theirs = c.Import
	} else if r.theirs, err = lineage.ReadImport(ctx, inv.git, gitDir, p.Merge.Theirs); err != nil {
		return nil, accountRepoFailure(fmt.Errorf("the update %s the pending merge of %s names: %w", p.Merge.Theirs, name, err))
	}
	return r, nil
}

// baseMovedFailure refuses a merge whose base, the version the import
// branch pointed at when the update merged it, is no longer the one the
// branch points at: what the merge would complete to is no longer an update
// of what the skill is at.
func baseMovedFailure(name string) *failure {
	return refuse(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved since the merge of "+name+" was left pending, so the merge cannot be resolved",
		"run '"+skillCommand("resolve", name, "--abort")+"' to give it up, then '"+skillCommand("update", name)+"' to merge again")
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
// left.
func (r *pendingRun) show() error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
	left := r.unresolved(nil)
	count := fmt.Sprintf("%d of %s unresolved", len(left), plural(len(r.res.files), "file"))
	inv.printConflicts(conflictOfSkill(name, r.pending.Merge, left),
		out.paint(heading, sanitised(name)), " has a merge pending with its update from ", r.from(), " to ", r.to(), ": ", out.paint(noteStyle, count))
	inv.summary = name + " has a merge pending with its update from " + r.from() + " to " + r.to() + ": " + count
	return nil
}

// resolution is how one file of a pending merge is resolved: what the
// merge's tree holds at the file's path once it is. A text file's parts
// are assembled here, and its content is written before its blob is known.
type resolution struct {
	path  string
	entry *source.TreeEntry // the file or symlink put at the path, by mode and blob; nil for nothing
	keep  bool              // the directory the tree holds at the path stays, as one side has it
	body  *string           // what a text file resolved here holds, written into the account repo as entry's blob
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
		rs = append(rs, resolution{path: p, entry: &source.TreeEntry{Mode: resolvedMode(f, chosen)}, body: &body})
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
		return resolution{path: f.Path, entry: &source.TreeEntry{Mode: v.mode, OID: v.oid}}
	}
	return resolution{path: f.Path, keep: len(f.aside) > 0}
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
// The content of every text file assembled or edited here is written into
// the account repo first, outside the lock. A merge that still has a file
// left to resolve is then rewritten: the tree of the merge with every
// resolution in it and the commit over it are written, see commit, and the
// merge ref is moved to it, see publish. One that has none left is
// completed in the same run, see complete.
func (r *pendingRun) resolve(ctx context.Context, rs []resolution) error {
	git, gitDir := r.inv.git, r.gitDir
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
	held, err := lineage.ReadMerged(ctx, git, gitDir, r.pending.Commit, r.dir)
	if err != nil {
		return accountRepoFailure(err)
	}
	v := applyResolutions(held, r.files, rs)
	now := map[string]bool{}
	for _, res := range rs {
		now[res.path] = true
	}
	resolved := make([]string, 0, len(r.resolved)+len(now))
	for _, f := range r.res.files {
		if r.resolved[f.Path] || now[f.Path] {
			resolved = append(resolved, f.Path)
		}
	}
	left := r.unresolved(now)
	if len(left) == 0 {
		return r.complete(ctx, v)
	}
	commit, err := r.commit(ctx, v, r.pending.Merge, len(left), resolved)
	if err != nil {
		return err
	}
	if err := r.publish(ctx, commit, "it was being resolved"); err != nil {
		return err
	}
	return r.reportResolved(rs, left)
}

// applyResolutions is the version a pending merge holds, as ReadMerged read
// it out of its tree, with rs written into it, in path order: every path
// git moved a version of a resolved file aside to goes, and the path
// itself, with anything below it, takes the version the file is resolved
// to, a directory it needs to sit in included, or keeps the directory it
// holds. files are the files of the merge the tree comes from, by path.
func applyResolutions(v lineage.Base, files map[string]*conflictFile, rs []resolution) lineage.Base {
	entries := map[string]source.TreeEntry{}
	for _, e := range v.Entries {
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
	sorted := slices.Clone(rs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })
	for _, res := range sorted {
		if f := files[res.path]; f != nil {
			for _, aside := range f.aside {
				drop(aside)
			}
		}
		if res.keep {
			continue
		}
		drop(res.path)
		if res.entry == nil {
			continue
		}
		for dir := path.Dir(res.path); dir != "."; dir = path.Dir(dir) {
			if e, ok := entries[dir]; !ok || e.Mode != source.DirMode {
				entries[dir] = source.TreeEntry{Path: dir, Mode: source.DirMode}
			}
		}
		e := *res.entry
		e.Path = res.path
		entries[res.path] = e
	}
	out := lineage.Base{Tree: v.Tree, Entries: make([]source.TreeEntry, 0, len(entries))}
	for _, e := range entries {
		out.Entries = append(out.Entries, e)
	}
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Path < out.Entries[j].Path })
	return out
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
// import branch something moved, refuses the run with nothing written.
// what says what the run was doing, for the refusal.
func (r *pendingRun) publish(ctx context.Context, commit, what string) error {
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
			return mergeMovedFailure(name, what)
		case values[lineage.ManagedRef(name)] != r.pending.Merge.Base || values[lineage.ForkRef(name)] != "":
			return baseMovedFailure(name)
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
// the merge pending with left still to resolve: a conflict event with those
// files and every hunk of them, and the line that says what the run did.
func (r *pendingRun) reportResolved(rs []resolution, left []conflictFile) error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
	out.emit(conflictOfSkill(name, r.pending.Merge, left))
	what := plural(len(rs), "file")
	if len(rs) == 1 {
		what = quotedPath(rs[0].path)
	}
	rest := ", " + plural(len(left), "file") + " left to resolve"
	inv.summary = "resolved " + what + " in the merge of " + name + rest
	out.done("resolved " + what + " in the merge of " + out.paint(heading, sanitised(name)) + out.paint(noteStyle, rest))
	return nil
}

// complete completes a merge that has no file left to resolve once it holds
// v, the skill's directory as the merge resolves it, in the same run that
// resolved its last file.
//
// First the library directory is judged, outside the lock, as an update
// judges it: one that is gone, is a symlink, or holds something git cannot
// record is refused with nothing written, and the run can be made again
// once that is put right. Then it is compared with mine, the directory as
// the update read it, by tree id: the directory's own, wrapped in the
// upstream directory, against the tree of the commit Agentx-Merge-Mine
// names. A directory edited while the merge was pending, by any tool, is
// merged again, see remerge, and that edit is never lost. The merge is then
// applied, see apply.
func (r *pendingRun) complete(ctx context.Context, v lineage.Base) error {
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
	mine, err := inv.git.Isolated(ctx, r.gitDir, "rev-parse", r.pending.Merge.Mine+"^{tree}")
	if err != nil {
		return accountRepoFailure(err)
	}
	c := completion{v: v, m: r.pending.Merge, libPath: libPath, captured: captured, tree: tree}
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
	captured string      // the library entry, in the words a journal records
	tree     treeid.Tree // what the library directory holds, as git would record it
	remerged bool        // the directory was edited while the merge was pending, and merged again
}

// remerge merges again a merge whose library directory was edited while it
// was pending: the directory is written into the account repo as mine, as
// the update wrote it, on the same base, and merged with the same update.
// A file that conflicts as it did, its base, mine and theirs the versions
// they were, keeps the way it was resolved, whether this run or an earlier
// one resolved it; every other file that conflicts, one the edit changed
// or a new one, is left to resolve. When none is, the merge completes with
// the new mine, and done is false; when some are, the merge is rewritten
// with the new mine as its first parent and in its trailer, and with the
// resolutions it kept, and the run reports those files alone and exits 4.
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
	m := lineage.Merge{Base: r.pending.Merge.Base, Mine: mine, Theirs: r.pending.Merge.Theirs}
	res, err := mergeVersions(ctx, git, gitDir, r.dir, m)
	if err != nil {
		return false, accountRepoFailure(err)
	}
	merged, err := lineage.ReadMerged(ctx, git, gitDir, res.tree, r.dir)
	if err != nil {
		return false, accountRepoFailure(err)
	}
	files := map[string]*conflictFile{}
	var kept []resolution
	var resolved []string
	left := []conflictFile{}
	for i, f := range res.files {
		files[f.Path] = &res.files[i]
		if was := r.files[f.Path]; was != nil && sameStages(was.stages, f.stages) {
			kept = append(kept, resolutionIn(c.v, f.Path))
			resolved = append(resolved, f.Path)
			continue
		}
		left = append(left, f)
	}
	v := applyResolutions(merged, files, kept)
	if len(left) == 0 {
		c.v, c.m, c.remerged = v, m, true
		return false, nil
	}
	commit, err := r.commit(ctx, v, m, len(left), resolved)
	if err != nil {
		return false, err
	}
	if err := r.publish(ctx, commit, "it was being completed"); err != nil {
		return false, err
	}
	out := inv.out
	inv.printConflicts(conflictOfSkill(name, m, left),
		out.paint(heading, sanitised(name)), " was edited while its merge was pending, and merging it again with its update from ",
		r.from(), " to ", r.to(), " conflicts in ", out.paint(noteStyle, plural(len(left), "file")))
	return true, refuse(exitPendingMerge, name+" was edited while its merge was pending, and merging it again conflicts in "+plural(len(left), "file")+", so the merge is still pending",
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
// keepPerms.
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
			return baseMovedFailure(name)
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
	inv.pruneEditorDirs(name, false)
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
// every placement. The files an editor had open for the merge go too, see
// pruneEditorDirs, and the skill is reported as it now stands.
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
	inv.pruneEditorDirs(name, true)
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
