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
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
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
			"and its newer version. The merge waits in a hidden Git checkout, merges/<name> in\n" +
			"agentx home, where plain git works too; agents never see a half-merged file. With no\n" +
			"flag the files left to resolve are shown. --hunk <file>:<index>=mine|theirs|both\n" +
			"chooses one part of a file, numbered as shown, and can be given once per part: every\n" +
			"part of a file is chosen in the same run, and a file left with no conflict markers is\n" +
			"staged, as in Git. --editor opens every text file left to resolve, or the one <file>\n" +
			"names, in your editor, in place: GIT_EDITOR, else EDITOR, else 'code --wait'; <file>\n" +
			"is taken with --editor alone. Once no file is left the merge completes: the library\n" +
			"directory takes the merged version and the skill is at its update. --abort gives the\n" +
			"merge up, as 'git merge --abort' would, and leaves the library directory as it is.",
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
// for the managed skill called name, in its checkout, see checkoutState.
// An unfinished journal is finished first, as an update finishes one: a
// completion stopped part way through moves the import branch before
// anything else. Then the skill is judged, in this order: a name the
// library does not hold and no lineage names is exit code 5; a fork and a
// skill with no merge pending are exit code 6. --abort gives up any merge
// whose checkout is there, whatever else is true of it; everything else
// reads the checkout first, see readCheckout.
func (inv *invocation) skillResolve(ctx context.Context, name string, act resolveAction) error {
	inv.leaveCheckout(name)
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
	case !managed || !inv.mergePending(name):
		return noMergePending(name)
	case act.abort:
		return inv.abortMerge(ctx, gitDir, name)
	}
	r := &resolveRun{inv: inv, gitDir: gitDir, rec: rec}
	if r.p, err = inv.readCheckout(ctx, gitDir, rec); err != nil {
		return err
	}
	switch {
	case act.editor != nil:
		return r.edit(ctx, act.editor)
	case len(act.choices) > 0:
		return r.resolve(ctx, act.choices)
	}
	return r.show(ctx)
}

// leaveCheckout moves the process out of the checkout of the skill called
// name when the run starts in it, as it does once the merge is resolved
// there with plain git: completing or giving up the merge removes the
// checkout, and every git process started after that would inherit a
// working directory that is gone.
func (inv *invocation) leaveCheckout(name string) {
	path := inv.checkoutPath(name)
	if wd, err := os.Getwd(); err == nil && (samePath(wd, path) || inside(wd, path)) {
		_ = os.Chdir(inv.mergesDir())
	}
}

// noMergePending refuses a run on a skill with no merge pending.
func noMergePending(name string) *failure {
	return refuse(exitRefused, name+" has no merge pending",
		"a merge is left pending by '"+skillCommand("update", name)+"' when your edits conflict with the update; run 'agentx skill list' to see which skills have one")
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

// checkoutState is the pending merge of one skill as its checkout holds
// it: a merge in progress, or one the user or a completion stopped part
// way through committed, which completing does not commit again.
type checkoutState struct {
	path     string         // the checkout
	dir      string         // the upstream directory every tree of the merge wraps the skill in
	head     string         // HEAD
	mine     string         // HEAD, or its first parent once the merge is committed
	base     string         // mine's parent
	theirs   string         // MERGE_HEAD, or HEAD's second parent once the merge is committed
	root     string         // the commit HEAD's first parents lead back to: the tip of the import branch the merge started from
	target   string         // the import commit the merge's message names, which the import branch moves to
	next     lineage.Import // its lineage
	mineTree string         // the tree of the skill's directory in mine
	merging  bool           // the merge is in progress, not committed
	files    []conflictFile // the unmerged files
}

// resolveRun is one run of skill resolve on a skill whose merge is pending.
type resolveRun struct {
	inv    *invocation
	gitDir string
	rec    lineage.Record // the import branch as the run read it
	p      *checkoutState
}

// readCheckout reads the pending merge of the skill rec names from its
// checkout: file reads and a few git processes, with no lock of its own.
// A merge in progress has MERGE_HEAD, and its message in MERGE_MSG; one
// that is committed has none, and a HEAD with two parents whose message
// is the merge's. The first parents of HEAD lead back to the tip of the
// import branch the merge started from, an import commit having no
// parent: mine's parent is that tip, and after a restart, see restart, the
// mine before it. Anything else, a merge given up inside the checkout
// with plain git say, a message that names no import commit, or one whose
// lineage cannot be read or holds the skill under another directory, is
// no longer a merge agentx can complete: exit code 6, with the hint that
// gives it up.
func (inv *invocation) readCheckout(ctx context.Context, gitDir string, rec lineage.Record) (*checkoutState, error) {
	name := rec.Name
	if !inv.mergePending(name) {
		return nil, noMergePending(name)
	}
	p := &checkoutState{path: inv.checkoutPath(name), dir: rec.Import.Dir()}
	gone := refuse(exitRefused, "the pending merge of "+name+" in "+p.path+" is no longer a merge in progress",
		"run '"+skillCommand("resolve", name, "--abort")+"' to give it up")
	state, err := inv.gitPaths(ctx, p.path, "MERGE_HEAD", "MERGE_MSG")
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	chain, err := inv.git.InCheckout(ctx, p.path, "log", "--first-parent", "--format=%H %P", "HEAD")
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	commits := strings.Split(strings.TrimSpace(chain), "\n")
	heads := strings.Fields(commits[0])
	mergeHead, err := os.ReadFile(state[0])
	p.merging = err == nil
	var message string
	switch {
	case p.merging && len(heads) == 2 && lineage.IsObjectID(strings.TrimSpace(string(mergeHead))):
		p.head, p.mine, p.base, p.theirs = heads[0], heads[0], heads[1], strings.TrimSpace(string(mergeHead))
		b, err := os.ReadFile(state[1])
		if err != nil {
			return nil, gone
		}
		message = string(b)
	case !p.merging && len(heads) == 3 && len(commits) > 1 && len(strings.Fields(commits[1])) > 1:
		p.head, p.mine, p.theirs, p.base = heads[0], heads[1], heads[2], strings.Fields(commits[1])[1]
		if message, err = inv.git.InCheckout(ctx, p.path, "log", "-1", "--format=%B", "HEAD"); err != nil {
			return nil, accountRepoFailure(err)
		}
	default:
		return nil, gone
	}
	p.root = strings.Fields(commits[len(commits)-1])[0]
	var ok bool
	if p.target, ok = lineage.BaseOf(message); !ok {
		return nil, gone
	}
	if p.next, err = lineage.ReadImport(ctx, inv.git, gitDir, p.target); err != nil || p.next.Dir() != p.dir {
		return nil, gone
	}
	if p.mineTree, err = inv.git.Isolated(ctx, gitDir, "rev-parse", "--verify", "-q", p.mine+":"+p.dir); err != nil {
		return nil, gone
	}
	if p.files, err = inv.readConflicts(ctx, gitDir, p.path, p.dir); err != nil {
		return nil, accountRepoFailure(err)
	}
	return p, nil
}

// from and to are the upstream commits the merge is between, short.
func (r *resolveRun) from() string { return short(r.rec.Import.Commit) }
func (r *resolveRun) to() string   { return short(r.p.next.Commit) }

// conflict is the conflict event of the merge with files left unmerged:
// its three versions are the checkout's HEAD^, HEAD and MERGE_HEAD.
func (r *resolveRun) conflict(files []conflictFile) conflictEvent {
	return conflictOfSkill(r.rec.Name, lineage.Merge{Base: r.p.base, Mine: r.p.mine, Theirs: r.p.theirs}, files)
}

// movedBranchFailure refuses to complete a merge whose import branch no
// longer points at the tip the merge started from: something outside
// agentx moved it, and the merge is of versions the skill is no longer
// between. The checkout stays, for --abort.
func movedBranchFailure(name string) *failure {
	return refuse(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved outside agentx while the merge was pending, so it cannot be completed",
		"run '"+skillCommand("resolve", name, "--abort")+"' to give it up")
}

// show reports the merge as it stands: one conflict event with every file
// left unmerged and every hunk of it, and in the text the same lines an
// update prints under a line that says how many files are left, with a
// warning first when the import branch moved since the merge started,
// which keeps it from completing. A merge with no file left unmerged, one
// resolved with plain git say, is completed instead.
func (r *resolveRun) show(ctx context.Context) error {
	inv, out, name, p := r.inv, r.inv.out, r.rec.Name, r.p
	if len(p.files) == 0 {
		return r.completeNow(ctx)
	}
	if r.rec.Commit != p.root {
		f := movedBranchFailure(name)
		out.warn(f.message + "; " + f.hint)
	}
	count := plural(len(p.files), "file") + " unresolved"
	inv.printConflicts(r.conflict(p.files),
		out.paint(heading, sanitised(name)), " has a merge pending with its update from ", r.from(), " to ", r.to(), ": ", out.paint(noteStyle, count))
	inv.summary = name + " has a merge pending with its update from " + r.from() + " to " + r.to() + ": " + count
	return nil
}

// completeNow completes a merge that has no file left unmerged, under the
// lock, reading the checkout again there; one merged anew meanwhile is
// shown instead.
func (r *resolveRun) completeNow(ctx context.Context) error {
	inv := r.inv
	var c *completed
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		p, err := inv.readCheckout(ctx, r.gitDir, r.rec)
		if err != nil {
			return err
		}
		if r.p = p; len(p.files) > 0 {
			return nil
		}
		c, err = r.complete(ctx)
		return err
	})
	switch {
	case err != nil:
		return mutationFailure(err)
	case c == nil:
		return r.show(ctx)
	}
	return r.reportDone(ctx, c)
}

// choice is how --hunk resolves one file: a text file to body, every part
// of it chosen, or a file that conflicts whole to side, which keeps the
// version that side has, or deletes the file where it has none.
type choice struct {
	path string
	body *string
	text *conflictText
	side string
	keep bool
}

// choose turns the --hunk choices into how each file they name is
// resolved, refusing with exit code 1, before anything is written, a
// choice the merge cannot take: a file of the skill that is not unmerged,
// a part a file does not have, two sides for one part, both sides of a
// file that conflicts whole, and a file some of whose parts were given no
// side.
//
// A file that conflicts whole has one part, 1, and takes one side. A text
// file is assembled from what the merge wrote in the checkout, every part
// replaced by the side chosen for it and both by mine then theirs, see
// assemble.
func (r *resolveRun) choose(choices []hunkChoice) ([]choice, error) {
	name := r.rec.Name
	see := "run '" + skillCommand("resolve", name) + "' to see every file left to resolve and its parts"
	files := map[string]*conflictFile{}
	for i, f := range r.p.files {
		files[f.Path] = &r.p.files[i]
	}
	sides := map[string]map[int]string{}
	for _, c := range choices {
		f := files[c.path]
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
	var plan []choice
	for _, p := range paths {
		f := files[p]
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
		if len(f.Hunks) == 0 {
			side := sides[p][1]
			plan = append(plan, choice{path: p, side: side, keep: side == sideMine && f.Mine != nil || side == sideTheirs && f.Theirs != nil})
			continue
		}
		chosen := make([]string, parts)
		for i := range chosen {
			chosen[i] = sides[p][i+1]
		}
		body := assemble(f.text, chosen)
		plan = append(plan, choice{path: p, body: &body, text: f.text})
	}
	return plan, nil
}

// joinWords joins words as a sentence lists them: "1", "1 and 2", "1, 2
// and 3".
func joinWords(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// assemble is the content of a text file whose parts are resolved to
// sides, one per hunk in order: what the merge wrote, every hunk replaced
// by the side chosen for it, both being mine then theirs. A hunk that ends
// the file ends as the version chosen ends it, with a newline or without
// one, and where both are chosen mine keeps the line ending the merge
// gives its last line, so that theirs starts a line of its own.
func assemble(t *conflictText, sides []string) string {
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
				first += "\n" // a whole file with no newline at its end, which the merge never ended
			}
			b.WriteString(first + theirs)
		}
	}
	b.WriteString(t.around[n])
	return b.String()
}

// resolve resolves the files the choices name in the checkout, under the
// lock, which the run reads again there, and in path order: a text file is
// written in place, assembled from its parts, and staged once it holds no
// marker; a file that conflicts whole takes the side chosen with git
// checkout --ours or --theirs and is staged, or is removed with git rm
// where that side has no file. Staged is what resolved means, as in git.
// When no file is left unmerged the merge is completed in the same hold of
// the lock, see complete; otherwise what is left is reported.
func (r *resolveRun) resolve(ctx context.Context, choices []hunkChoice) error {
	if _, err := r.choose(choices); err != nil {
		return err
	}
	inv := r.inv
	var resolved []string
	var left []conflictFile
	var c *completed
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		p, err := inv.readCheckout(ctx, r.gitDir, r.rec)
		if err != nil {
			return err
		}
		r.p = p
		plan, err := r.choose(choices)
		if err != nil {
			return err
		}
		if err := r.stage(ctx, plan); err != nil {
			return err
		}
		for _, c := range plan {
			resolved = append(resolved, c.path)
		}
		if left, err = inv.readConflicts(ctx, r.gitDir, p.path, p.dir); err != nil {
			return accountRepoFailure(err)
		}
		if len(left) > 0 {
			return nil
		}
		p.files = nil
		c, err = r.complete(ctx)
		return err
	})
	switch {
	case err != nil:
		return mutationFailure(err)
	case c != nil:
		return r.reportDone(ctx, c)
	}
	return r.reportResolved(resolved, left)
}

// stage writes and stages what plan resolves the files to, with one git
// checkout per side, one git add and one git rm, their paths taken as
// they are and never as patterns.
func (r *resolveRun) stage(ctx context.Context, plan []choice) error {
	p := r.p
	var add, rm []string
	sides := map[string][]string{}
	for _, c := range plan {
		spec := p.dir + "/" + c.path
		switch {
		case c.body != nil:
			if err := rewrite(filepath.Join(p.path, filepath.FromSlash(spec)), *c.body); err != nil {
				return accountRepoFailure(err)
			}
			if !holdsMarkers(*c.body, c.text.mine, c.text.base, c.text.theirs) {
				add = append(add, spec)
			}
		case c.keep:
			sides[c.side] = append(sides[c.side], spec)
			add = append(add, spec)
		default:
			rm = append(rm, spec)
		}
	}
	var calls [][]string
	for _, side := range []struct{ name, flag string }{{sideMine, "--ours"}, {sideTheirs, "--theirs"}} {
		if len(sides[side.name]) > 0 {
			calls = append(calls, append([]string{"checkout", side.flag, "--"}, sides[side.name]...))
		}
	}
	if len(add) > 0 {
		calls = append(calls, append([]string{"add", "--"}, add...))
	}
	if len(rm) > 0 {
		calls = append(calls, append([]string{"rm", "-q", "--"}, rm...))
	}
	for _, args := range calls {
		if _, err := r.inv.git.InCheckout(ctx, p.path, append([]string{"--literal-pathspecs"}, args...)...); err != nil {
			return accountRepoFailure(err)
		}
	}
	return nil
}

// rewrite replaces the file at path with body, as a file written beside it
// and renamed over it, with the permissions the file has.
func rewrite(path, body string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agentx-resolve-")
	if err != nil {
		return err
	}
	_, err = tmp.WriteString(body)
	if err == nil {
		err = tmp.Chmod(info.Mode().Perm())
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// reportResolved reports a run that resolved the files at paths and left
// the merge pending with left still unmerged: a conflict event with those
// files and every hunk of them, and the line that says what the run did.
func (r *resolveRun) reportResolved(paths []string, left []conflictFile) error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
	out.emit(r.conflict(left))
	what := plural(len(paths), "file")
	if len(paths) == 1 {
		what = quotedPath(paths[0])
	}
	rest := ", " + plural(len(left), "file") + " left to resolve"
	inv.summary = "resolved " + what + " in the merge of " + name + rest
	out.done("resolved " + what + " in the merge of " + out.paint(heading, sanitised(name)) + out.paint(noteStyle, rest))
	return nil
}

// reportUnchanged reports an editor session that staged nothing: the
// conflict event of the merge as it stands, every file left in it, and the
// line that says so. The merge stays pending, which is no failure.
func (r *resolveRun) reportUnchanged(left []conflictFile) error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
	out.emit(r.conflict(left))
	rest := plural(len(left), "file") + " left to resolve"
	inv.summary = "resolved nothing in the merge of " + name + ": " + rest
	out.print("Resolved nothing in the merge of ", out.paint(heading, sanitised(name)), ": ", out.paint(noteStyle, rest), ".")
	return nil
}

// edit resolves text files of the merge in the user's editor, in place in
// the checkout: the one the run names, or every text file left unmerged.
// The lock is taken before, without waiting, to finish what an earlier
// command left and to see that the merge is still pending, and released
// before the editor starts, so that every other command, and the scans of
// the app, go on while it is open, and SIGINT is left to the editor while
// it runs, as git leaves it. Once the editor exits 0 the lock is taken
// again, without waiting, and every file it was given that is still
// unmerged and holds no marker, see holdsMarkers, is staged; a merge with
// no file left unmerged is then completed in the same hold, see complete.
// What the editor saved stays in the checkout whatever happens: an editor
// that fails or a run stopped stages nothing, and neither does one whose
// lock is busy afterwards, and a merge given up while the editor was open
// is reported as such, with nothing written.
func (r *resolveRun) edit(ctx context.Context, s *editorSession) error {
	inv, name, p := r.inv, r.rec.Name, r.p
	if len(p.files) == 0 {
		return r.completeNow(ctx)
	}
	see := "run '" + skillCommand("resolve", name) + "' to see every file left to resolve"
	var files []string
	if s.file != "" {
		file := filepath.ToSlash(filepath.Clean(s.file))
		i := slices.IndexFunc(p.files, func(f conflictFile) bool { return f.Path == file })
		switch {
		case i < 0:
			return fail(exitUsage, fmt.Sprintf("%s does not conflict in the merge of %s", quotedPath(file), name), see)
		case p.files[i].text == nil:
			return fail(exitUsage, fmt.Sprintf("%s conflicts as a whole file, so there is no text to edit", quotedPath(file)),
				fmt.Sprintf("choose a side for it with --hunk %s:1=mine or --hunk %s:1=theirs", file, file))
		}
		files = []string{file}
	} else {
		for _, f := range p.files {
			if f.text != nil {
				files = append(files, f.Path)
			}
		}
		if len(files) == 0 {
			return fail(exitRefused, "every file left to resolve in the merge of "+name+" conflicts as a whole file, so there is no text to edit",
				"choose a side for each with --hunk <file>:1=mine or --hunk <file>:1=theirs; "+see)
		}
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(p.path, p.dir, filepath.FromSlash(f))
	}
	err := home.MutateQuiet(inv.dirs.Home, inv.refs(ctx), func() error {
		if !inv.mergePending(name) {
			return noMergePending(name)
		}
		return nil
	})
	if err != nil {
		return mutationFailure(err)
	}

	release := interrupt.Hold(ctx)
	err = inv.runEditor(ctx, s, paths)
	release()
	if err != nil {
		return fail(exitRefused, fmt.Sprintf("the editor %s %s, so nothing was staged; what it saved stays in the merge", s.command, err),
			"run '"+resolveInEditor(name, s.file)+"' again once the editor works, or set GIT_EDITOR or EDITOR to another")
	}

	var staged []string
	var left []conflictFile
	var c *completed
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		if !inv.mergePending(name) {
			return fail(exitRefused, name+" has no merge pending: it was given up or completed while the editor was open, so nothing was staged",
				"run 'agentx skill list' to see which skills have one")
		}
		p, err := inv.readCheckout(ctx, r.gitDir, r.rec)
		if err != nil {
			return err
		}
		r.p = p
		var add []string
		for _, file := range files {
			i := slices.IndexFunc(p.files, func(f conflictFile) bool { return f.Path == file })
			if i < 0 || p.files[i].text == nil {
				continue
			}
			t := p.files[i].text
			body, err := os.ReadFile(filepath.Join(p.path, p.dir, filepath.FromSlash(file)))
			if err != nil || holdsMarkers(string(body), t.mine, t.base, t.theirs) {
				continue
			}
			staged, add = append(staged, file), append(add, p.dir+"/"+file)
		}
		if len(add) > 0 {
			if _, err := inv.git.InCheckout(ctx, p.path, append([]string{"--literal-pathspecs", "add", "--"}, add...)...); err != nil {
				return accountRepoFailure(err)
			}
		}
		if left, err = inv.readConflicts(ctx, r.gitDir, p.path, p.dir); err != nil {
			return accountRepoFailure(err)
		}
		if len(left) > 0 {
			return nil
		}
		p.files = nil
		c, err = r.complete(ctx)
		return err
	})
	switch {
	case errors.Is(err, home.ErrLocked):
		return fail(exitLocked, err.Error()+", so what the editor saved stays in the merge, unstaged",
			"run '"+resolveInEditor(name, s.file)+"' again to stage it once that command is done")
	case err != nil:
		return mutationFailure(err)
	case c != nil:
		return r.reportDone(ctx, c)
	case len(staged) > 0:
		return r.reportResolved(staged, left)
	}
	return r.reportUnchanged(left)
}

// mineCheck tells whether a skill still holds the mine of its pending
// merge, the content the merge started from, and how it differs when it
// does not. A managed skill's is its library directory, see libraryMine.
type mineCheck interface {
	holdsMine(ctx context.Context, mineTree string) (judged, error)
}

// libraryMine is the mineCheck of a managed skill: its library directory,
// read as tree at its real path dir, judged as drift judges it, the fast
// path first, then git. The index git compares it with is loaded from
// mine rather than the base version, so a file mine holds counts whatever
// an ignore rule says of it. The files git ignores are read, since a
// completion carries them over.
type libraryMine struct {
	inv    *invocation
	gitDir string
	dir    string
	tree   treeid.Tree
}

func (l libraryMine) holdsMine(ctx context.Context, mineTree string) (judged, error) {
	return l.inv.judgeDir(ctx, l.gitDir, l.dir, l.tree, treeVersion(mineTree), true)
}

// completed is what a completion did, for the report the run makes once
// the lock is released: the merge completed, or merged again and started
// anew in the checkout, restarted naming the files it left unmerged.
type completed struct {
	remerged     bool
	newer        bool
	upstreamName string
	done         placements
	restarted    []conflictFile
}

// complete completes a merge that has no file left unmerged, under the
// hold of the lock that read its checkout, which is p.
//
// The import branch must still be at the tip the merge started from, or
// the merge is refused, see movedBranchFailure. The library directory is
// then judged as an update judges it, one that is gone, is a symlink or
// holds something git cannot record being refused, and compared with
// mine, see mineCheck. A refusal writes nothing and keeps the checkout as
// it is.
//
// A merge in progress is committed in the checkout, as the user, see
// gitx.UserAuthor, with the message the update wrote: git commit, run as
// any merge is completed. What it resolves to must hold a SKILL.md and
// nothing the library cannot keep apart, see refuseVersion, before it is
// committed. When the library still holds mine, what the commit holds is
// applied. When it was edited meanwhile, by any tool, the edit is merged
// with the finished merge rather than lost: the library directory is
// committed on mine and merged with the finished merge, mine the merge
// base, and a clean result is applied; a conflict starts a new merge in
// the checkout, see restart. What is applied goes in as one journaled
// mutation, see apply.
func (r *resolveRun) complete(ctx context.Context) (*completed, error) {
	inv, git, gitDir, p, name := r.inv, r.inv.git, r.gitDir, r.p, r.rec.Name
	values, err := inv.lineageRefs(ctx, gitDir, name)
	if err != nil {
		return nil, err
	}
	if values[lineage.ManagedRef(name)] != p.root || r.rec.Commit != p.root {
		return nil, movedBranchFailure(name)
	}
	lib, held := librarySkill(inv.dirs.Library, name)
	if !held {
		sc := skillContext{records: map[string]lineage.Record{name: r.rec}}
		what, wayOut := sc.absentNotice(inv, name)
		return nil, fail(exitRefused, what+", so its merge cannot be completed",
			"run '"+skillCommand("resolve", name, "--abort")+"' to give the merge up first; then "+wayOut)
	}
	libPath := inv.libraryPath(name)
	captured, err := home.State(libPath)
	if err != nil {
		return nil, libraryFailure(inv.dirs.Library, err)
	}
	if target, isLink := home.LinkTarget(captured); isLink {
		return nil, fail(exitRefused, fmt.Sprintf("%s is a symlink to %s; completing the merge replaces the library directory and would drop the link without touching the files it leads to", quotedPath(libPath), quotedPath(target)),
			"replace the link with the directory it points to, then run '"+skillCommand("resolve", name)+"' again")
	}
	tree, err := inv.readLibraryTree(lib.Path)
	if err != nil {
		return nil, err
	}
	if len(tree.Unrecordable) > 0 {
		return nil, unrecordableRefusal(name, libPath, tree.Unrecordable, "completing the merge", "resolve")
	}
	var mine mineCheck = libraryMine{inv: inv, gitDir: gitDir, dir: lib.ResolvedPath, tree: tree}
	j, err := mine.holdsMine(ctx, p.mineTree)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	read := func(treeish string) (lineage.Base, error) {
		v, err := lineage.ReadMerged(ctx, git, gitDir, treeish, p.dir)
		if err != nil {
			return lineage.Base{}, accountRepoFailure(err)
		}
		if f := r.refuseVersion(v, lib.ResolvedPath, tree, libPath); f != nil {
			return lineage.Base{}, f
		}
		return v, nil
	}
	var v lineage.Base
	finished := p.head
	if p.merging {
		written, err := git.InCheckout(ctx, p.path, "write-tree")
		if err != nil {
			return nil, accountRepoFailure(err)
		}
		if v, err = read(strings.TrimSpace(written)); err != nil {
			return nil, err
		}
		author, err := git.UserAuthor(ctx, gitDir)
		if err != nil {
			return nil, accountRepoFailure(err)
		}
		if _, err := git.InCheckoutAs(ctx, p.path, author, "commit", "-q", "--no-edit"); err != nil {
			return nil, accountRepoFailure(err)
		}
		if finished, err = git.InCheckout(ctx, p.path, "rev-parse", "HEAD"); err != nil {
			return nil, accountRepoFailure(err)
		}
		finished = strings.TrimSpace(finished)
	}
	c := &completed{}
	placedMine := j.written
	switch {
	case j.holds && p.merging:
		placedMine = p.mineTree
	case j.holds:
		placedMine = p.mineTree
		if v, err = read(finished); err != nil {
			return nil, err
		}
	default:
		edited, err := lineage.CommitDir(ctx, git, gitDir, p.dir, j.written, p.mine, "library directory of "+name+"\n")
		if err != nil {
			return nil, accountRepoFailure(err)
		}
		res, err := mergeVersions(ctx, git, gitDir, p.dir, lineage.Merge{Base: p.mine, Mine: edited, Theirs: finished})
		if err != nil {
			return nil, accountRepoFailure(err)
		}
		if res.conflicted {
			return r.restart(ctx, edited, finished, res.size)
		}
		if v, err = read(res.tree); err != nil {
			return nil, err
		}
		c.remerged = true
	}
	return c, r.apply(ctx, c, applying{v: v, libPath: libPath, captured: captured, tree: tree, ignored: j.ignored,
		placed: []version{treeVersion(placedMine), baseVersion(r.rec)}, candidate: values[lineage.CandidateRef(name)]})
}

// refuseVersion refuses to complete a merge into v, the version it
// resolves to: one with no SKILL.md, which resolving SKILL.md to the side
// that deleted it leaves, and which laid out would take the skill out of
// every client, and one that holds two paths differing in case alone where
// the library's file system, at root and read as tree, cannot keep them
// apart, as an update refuses a clean merge that does. Nothing is written,
// and the merge stays as it is.
func (r *resolveRun) refuseVersion(v lineage.Base, root string, tree treeid.Tree, libPath string) *failure {
	name := r.rec.Name
	giveUp := "run '" + skillCommand("resolve", name, "--abort") + "' to give the merge up"
	if len(skillFileBlob(v)) == 0 {
		return refuse(exitRefused, "the merge of "+name+" resolves to a directory with no SKILL.md, which no client would read as a skill, so nothing was changed",
			"put SKILL.md back in the merge in "+quotedPath(r.p.path)+" with git, or "+giveUp)
	}
	if a, b, ok := caseClash(v.Entries); ok && foldsCase(root, tree) {
		return refuse(exitRefused, fmt.Sprintf("the merge of %s holds both %s and %s, which this file system cannot keep apart, so nothing was changed", name, quotedPath(a), quotedPath(b)),
			giveUp+" and rename the one in "+quotedPath(libPath)+" before you update again")
	}
	return nil
}

// restart starts the merge anew in the checkout, once the finished merge
// conflicts with an edit made to the library meanwhile, committed as
// edited: the checkout is moved to edited and merged with finished, the
// mine the merge started from as the merge base, as an update sets a merge
// up, see mergeIn, with the finished merge's message, so that completing
// it still moves the import branch to the same update. Only where the edit
// overlaps the finished merge conflicts. The library, the refs and the
// candidate are left as they are, so no journal is written.
func (r *resolveRun) restart(ctx context.Context, edited, finished string, size int) (*completed, error) {
	inv, p := r.inv, r.p
	message, err := inv.git.InCheckout(ctx, p.path, "log", "-1", "--format=%B", finished)
	if err == nil {
		_, err = inv.git.InCheckout(ctx, p.path, "checkout", "-q", "--detach", "--force", edited)
	}
	if err == nil {
		err = inv.mergeIn(ctx, r.gitDir, p.path, mergeStart{base: p.mine, mine: edited, theirs: finished, message: message, size: size})
	}
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	files, err := inv.readConflicts(ctx, r.gitDir, p.path, p.dir)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	p.base, p.mine, p.theirs = p.mine, edited, finished
	return &completed{restarted: files}, nil
}

// applying is what a completion lays out and replaces: the version v the
// merge resolves to, the library directory as it was read and captured,
// the files git ignores in it, the versions a copy may hold that agentx
// placed there, and the candidate as it was read under the lock.
type applying struct {
	v         lineage.Base
	libPath   string
	captured  string
	tree      treeid.Tree
	ignored   []string
	placed    []version
	candidate string
}

// apply completes a merge as one journaled mutation, as an update applies
// a clean merge: the import branch moves from the tip the merge started
// from to the update the merge's message names, and never to a newer
// candidate a check may have found meanwhile; the library directory is
// retained and replaced by v, laid out beside it with the files git
// ignores in it carried over; the copies that held what agentx placed
// there are refreshed and every other copy is kept, see refreshCopies;
// the candidate is deleted only when it still is that update, a newer one
// staying as the skill's next update; and the checkout is removed last,
// then its registration. What the library directory holds unchanged keeps
// its permissions, see keepPerms.
func (r *resolveRun) apply(ctx context.Context, c *completed, a applying) error {
	inv, git, gitDir, p, name := r.inv, r.inv.git, r.gitDir, r.p, r.rec.Name
	next := lineage.Record{Name: name, Kind: r.rec.Kind, Ref: r.rec.Ref, Commit: p.target, Import: p.next, HasImport: true}
	theirs, err := lineage.ReadBase(ctx, git, gitDir, next)
	if err != nil {
		return accountRepoFailure(err)
	}
	bodies, err := source.ReadBlobs(ctx, git, gitDir, append(baseBlobs(a.v), skillFileBlob(theirs)...))
	if err != nil {
		return accountRepoFailure(err)
	}
	c.upstreamName = upstreamRename(name, skillName(theirs, bodies), p.next.Dir())
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
	target := treeVersion(a.v.ID())
	lay := func(dest string) error {
		if err := materialise(dest, a.v, bodies); err != nil {
			return err
		}
		return keepPerms(dest, a.libPath, a.tree, a.v)
	}
	m := home.NewMutation(inv.dirs.Home)
	staged := m.Sibling(a.libPath, "staged")
	fingerprint, err := stageVersion(staged, lay, target, a.libPath, a.ignored)
	if err != nil {
		os.RemoveAll(staged)
		return libraryFailure(inv.dirs.Library, err)
	}
	if live, err := home.State(a.libPath); err != nil || live != a.captured {
		os.RemoveAll(staged)
		return changedWhileCompleting(name)
	}
	checkout, err := home.State(p.path)
	if err != nil {
		os.RemoveAll(staged)
		return err
	}
	m.Ref(gitDir, lineage.ManagedRef(name), p.root, p.target)
	m.Remove(a.libPath, a.captured)
	m.Publish(a.libPath, staged, fingerprint)
	inv.refreshCopies(ctx, m, gitDir, name, target, a.placed, lay, recorded, &c.done)
	m.Remove(p.path, checkout)
	switch a.candidate {
	case p.target:
		m.Ref(gitDir, lineage.CandidateRef(name), a.candidate, "")
	case "":
	default:
		c.newer = true
	}
	if err := m.Apply(inv.refs(ctx)); err != nil {
		return err
	}
	if err := git.RemoveCheckout(ctx, gitDir, p.path); err != nil {
		inv.out.debugf("cannot remove the registration of %s, which the next command prunes: %v", p.path, err)
	}
	return nil
}

// changedWhileCompleting refuses a completion whose library directory
// changed after the run read it: what the merge would replace is no
// longer what it judged, and an edit made meanwhile is never replaced.
func changedWhileCompleting(name string) *failure {
	return refuse(exitRefused, name+" changed while its merge was being completed, so nothing was changed",
		"run the command again to complete the merge with the skill as it is now")
}

// reportDone reports what a completion did, once the lock is released: a
// merge started anew is reported as an update reports one it leaves
// pending, and exits 4; a completed one is reported as the machine now
// has it, see reportCompleted.
func (r *resolveRun) reportDone(ctx context.Context, c *completed) error {
	if c.restarted == nil {
		return r.reportCompleted(ctx, c)
	}
	out, name := r.inv.out, r.rec.Name
	files := plural(len(c.restarted), "file")
	r.inv.printConflicts(r.conflict(c.restarted), out.paint(heading, sanitised(name)),
		" was edited while its merge was pending, and merging your edit into the finished merge conflicts in ", out.paint(noteStyle, files))
	return fail(exitPendingMerge, name+" was edited while its merge was pending, and merging the edit into the finished merge conflicts in "+files+", so the merge is still pending",
		"run '"+skillCommand("resolve", name)+"' to see what is left")
}

// reportCompleted reads the machine again and reports the skill as the
// completed merge leaves it: a warning when the update names it otherwise,
// its library_skill event with every placement, as skill list reports
// them, and the line that says what the run did.
func (r *resolveRun) reportCompleted(ctx context.Context, c *completed) error {
	inv, out, name := r.inv, r.inv.out, r.rec.Name
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
	if c.upstreamName != "" {
		out.warn(renameWarning(name, c.upstreamName))
	}
	out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
	how := " and updated it"
	if c.remerged {
		how = ", merged the edits made to it meanwhile and updated it"
	}
	moved := " from " + r.from() + " to " + r.to()
	plain, painted := copiesNote(out, len(c.done.copies), len(c.done.skipped))
	var later string
	if c.newer {
		later = "; the newer update the last check found stays for the next update"
	}
	inv.summary = "resolved " + name + how + moved + plain + later
	out.done("resolved " + out.paint(heading, sanitised(name)) + how + moved + painted + later)
	return nil
}

// abortMerge gives up the merge pending for the skill called name, as git
// merge --abort gives one up, whatever state its checkout is in: one
// journaled mutation retains the checkout and removes it, and then git's
// registration of it goes. The library directory, the import branch and
// the candidate are left exactly as they were, and so is every placement;
// what was resolved or typed in the checkout goes with it. The skill is
// reported as it now stands.
func (inv *invocation) abortMerge(ctx context.Context, gitDir, name string) error {
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		if !inv.mergePending(name) {
			return noMergePending(name)
		}
		path := inv.checkoutPath(name)
		state, err := home.State(path)
		if err != nil {
			return err
		}
		m := home.NewMutation(inv.dirs.Home)
		m.Remove(path, state)
		if err := m.Apply(inv.refs(ctx)); err != nil {
			return err
		}
		if err := inv.git.RemoveCheckout(ctx, gitDir, path); err != nil {
			inv.out.debugf("cannot remove the registration of %s, which the next command prunes: %v", path, err)
		}
		return nil
	})
	if err != nil {
		return mutationFailure(err)
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
		inv.out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
	}
	inv.out.done("gave up the merge of " + inv.out.paint(heading, sanitised(name)) + kept)
	return nil
}
