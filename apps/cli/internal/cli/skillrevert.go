package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

func newSkillRevertCommand(inv *invocation) *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "revert <name>",
		Short: "Put a managed skill back to its base version, or discard a fork's edits",
		Long: "Put the library directory of a managed skill back to its base version, the\n" +
			"version it was installed at, discarding every edit made to it since: files added\n" +
			"are deleted, files changed or deleted are restored, and files git ignores are kept.\n" +
			"A copy placement that holds the edited content is put back too; a copy edited on\n" +
			"its own is kept and named. Run 'agentx skill diff <name>' first to see what the\n" +
			"revert discards.\n\n" +
			"For a fork, discard its uncommitted edits, putting its skill directory back to\n" +
			"the last commit of its branch; nothing is committed. With --to <commit>, restore\n" +
			"the skill directory from that earlier commit of its history as a new commit on\n" +
			"top; history is never rewritten, and a fork with uncommitted edits is refused.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("to") && strings.TrimSpace(to) == "" {
				return fail(exitUsage, "--to needs a commit", "name a commit by its id, such as one 'agentx skill history' lists")
			}
			return inv.skillRevert(cmd.Context(), args[0], to)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "restore a fork from this earlier commit, as a new commit on top")
	return cmd
}

// skillRevert puts a managed skill's library directory back to its base
// version: the import commit's tree is laid out in a hidden directory beside
// the library directory, which the mutation then retains and replaces, so a
// file the base does not hold is gone afterwards, and the copies that held
// the edited content are refreshed with it. The files git ignores in the
// library directory are carried into the new one, as git checkout keeps
// them.
//
// Reverting discards, so what is discarded is what the directory held when
// the command began and nothing later. That content is captured first, by
// the fingerprint the journal compares, and read again under the lock: an
// edit made in the meantime, by any tool, refuses the revert rather than
// being discarded with the rest. The journal's remove step carries the same
// fingerprint, and the content it retains is dropped only when it still
// hashes to it, so an edit made while the mutation runs is kept as well.
//
// A skill an update left a merge pending for is refused under the lock:
// the merge holds the library directory as it was, and a revert would
// leave it merging edits that are gone.
//
// A branch an earlier agentx wrote over a source's own tree, one that
// stores a mode git no longer writes, is never current against any
// directory, so a revert that left it would leave the skill modified. The
// same mutation moves it, from the commit it holds, to the commit an
// install of that version writes today, which is all a revert of a library
// directory that already holds the base's files does.
//
// A fork is reverted to its last commit, or to the commit to names, see
// forkRevert. Either way, a journal an earlier command left unfinished is
// finished first, so that what the revert reads is what that command
// left.
func (inv *invocation) skillRevert(ctx context.Context, name, to string) error {
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	gitDir, rec, held, err := inv.accountRecord(ctx, name)
	if err != nil {
		return err
	}
	if held && rec.Kind == lineage.KindFork {
		return inv.forkRevert(ctx, gitDir, rec, to)
	}
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return inv.noLibrarySkill(name)
	}
	if to != "" {
		return notAForkRefusal(name, "revert to", "revert", held)
	}
	if err := managedRefusal(name, "revert to", rec, held); err != nil {
		return err
	}
	libPath := inv.libraryPath(name)
	captured, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	edited, err := inv.readLibraryTree(lib.Path)
	if err != nil {
		return err
	}
	if len(edited.Unrecordable) > 0 {
		return unrecordableRefusal(name, libPath, edited.Unrecordable, "a revert", "revert")
	}
	against := "its base version at " + short(rec.Import.Commit)
	j, err := inv.judgeDir(ctx, gitDir, lib.ResolvedPath, edited, baseVersion(rec), true)
	if err != nil {
		return accountRepoFailure(err)
	}
	if j.holds {
		return inv.reportReverted(ctx, name, against, revertNothing, placements{})
	}
	base, err := lineage.ReadBase(ctx, inv.git, gitDir, rec)
	if err != nil {
		return accountRepoFailure(err)
	}
	target := base.ID() // the tree the base has laid out on disk
	restore := j.written != target
	// A library entry that is a symlink leads to the directory the user
	// edits, which is not the library's to replace: the mutation replaces
	// the entry itself, so it would drop the link, leave every edit where
	// the link led, and report the skill as reverted. Laying the base out
	// where the link leads instead would stage and sweep in a directory
	// outside the library, perhaps one another tool keeps.
	if target, isLink := home.LinkTarget(captured); isLink && restore {
		return fail(exitRefused, fmt.Sprintf("%s is a symlink to %s; a revert replaces the library directory and would drop the link without touching the files it leads to", quotedPath(libPath), quotedPath(target)),
			"replace the link with the directory it points to, then run '"+skillCommand("revert", name)+"' again, or put the files back by hand: '"+skillCommand("diff", name)+"' shows what differs")
	}
	var lay func(dest string) error // lays the base out
	if restore {
		bodies, err := source.ReadBlobs(ctx, inv.git, gitDir, baseBlobs(base))
		if err != nil {
			return accountRepoFailure(err)
		}
		lay = func(dest string) error { return materialise(dest, base, bodies) }
	}
	// The branch stays at its commit, unless that commit stores the base
	// in a form no directory is current against: then it moves to the one
	// an install writes today, held by a staging ref of this run's own
	// until the journal that names it is applied, as an install's is.
	recorded, run := rec.Commit, ""
	if !rec.Canonical(base) {
		run = lineage.NewRun()
		if recorded, err = lineage.Rewrite(ctx, inv.git, gitDir, run, rec, base); err != nil {
			inv.dropImporting(ctx, gitDir, run, 1)
			return accountRepoFailure(err)
		}
	}
	var done placements
	journaled := false
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		// Every input is read again under the lock: the branch the base came
		// from, and the directory the user asked to discard.
		refs, err := inv.lineageRefs(ctx, gitDir, name)
		if err != nil {
			return err
		}
		if refs[lineage.ManagedRef(name)] != rec.Commit || refs[lineage.ForkRef(name)] != "" {
			return fail(exitRefused, "the import branch "+lineage.ManagedRef(name)+" moved while "+name+" was being reverted, so nothing was discarded",
				"run '"+skillCommand("diff", name)+"' to see the base version now, then revert again")
		}
		if inv.mergePending(name) {
			return pendingMergeRefusal(name, "reverted")
		}
		m := home.NewMutation(inv.dirs.Home)
		// The ref step comes first and is the only step when the library
		// already holds the base's files. When the branch does not move, the
		// step holds the journal to the base the content came from, so that
		// a revert finished by recovery puts back the version the branch
		// still names and no other.
		m.Ref(gitDir, lineage.ManagedRef(name), rec.Commit, recorded)
		if restore {
			if err := inv.stageRevert(ctx, m, gitDir, name, libPath, captured, rec, target, lay, j, &done); err != nil {
				m.Discard()
				return err
			}
		}
		applied := m.Apply(inv.refs(ctx))
		journaled = m.Journaled()
		return applied
	})
	// The staging ref goes once the branch holds the commit, or when no
	// journal that recovery could finish names it.
	if run != "" && (err == nil || !journaled) {
		inv.dropImporting(ctx, gitDir, run, 1)
	}
	if err != nil {
		return mutationFailure(err)
	}
	if !restore {
		return inv.reportReverted(ctx, name, against, revertBranch, done)
	}
	return inv.reportReverted(ctx, name, against, revertLibrary, done)
}

// stageRevert records, under the lock, the steps that put the library
// directory back to the base, whose tree on disk is target, and refresh
// the copies that held it: the directory is read again first, and one that
// changed since it was captured refuses the revert, so an edit made in the
// meantime is never discarded with the rest. j is how the directory
// compared with the base, the files git ignores in it included.
func (inv *invocation) stageRevert(ctx context.Context, m *home.Mutation, gitDir, name, libPath, captured string, rec lineage.Record, target string, lay func(string) error, j judged, done *placements) error {
	live, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	if live != captured {
		return fail(exitRefused, name+" changed while it was being reverted, so nothing was discarded",
			"run '"+skillCommand("diff", name)+"' to see the change, then revert again to discard it too")
	}
	// A revert killed before its journal was written left what it staged
	// with nothing to name it: beside the library directory, and beside
	// each copy it was refreshing. All of it is swept before anything is
	// staged, since a sweep of a directory two configurations share would
	// take a sibling this plan staged a moment earlier.
	edit, err := inv.beginSettings()
	if err != nil {
		return err
	}
	recorded := edit.copiesOf(name)
	sweepStaged(inv.dirs.Library)
	for _, t := range inv.detectedTargets() {
		if !t.readsLibrary && slices.Contains(recorded, t.id) {
			sweepStaged(t.dir)
		}
	}
	*done = placements{}
	// What is laid out is the base as a directory on disk holds it, even
	// from a branch an earlier agentx stored in another form.
	laidOut := version{load: baseVersion(rec).load, holds: func(id string) bool { return id == target }}
	staged := m.Sibling(libPath, "staged")
	fingerprint, err := stageVersion(staged, lay, laidOut, libPath, j.ignored)
	if err != nil {
		_ = home.RemoveTree(staged)
		return libraryFailure(inv.dirs.Library, err)
	}
	m.Remove(libPath, captured)
	m.Publish(libPath, staged, fingerprint)
	inv.refreshCopies(ctx, m, gitDir, name, laidOut, []version{treeVersion(j.written)}, lay, recorded, done)
	return nil
}

// revertOutcome is what a revert changed.
type revertOutcome int

const (
	revertNothing revertOutcome = iota // the library held the base, and the branch stored it as git writes it
	revertBranch                       // the library held the base's files; the branch was stored again
	revertLibrary                      // the library directory was put back, and the branch too when it had to be
)

// reportReverted reads the machine again and reports the skill as it now
// stands. done is what the mutation did to the copies, which only a revert
// that put the library directory back touched.
func (inv *invocation) reportReverted(ctx context.Context, name, against string, outcome revertOutcome, done placements) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return fail(exitInternal, "the library holds no "+name+" after reverting it", "run 'agentx doctor' and check the library it names")
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	inv.out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
	out := inv.out
	switch outcome {
	case revertNothing:
		inv.summary = name + " already matches " + against + "; nothing was reverted"
		out.print(out.paint(heading, sanitised(name)), " already matches ", against, "; nothing was reverted")
		return nil
	case revertBranch:
		const stored = "; nothing was reverted, and its import branch now stores that version as git writes it today"
		inv.summary = name + " already matches " + against + stored
		out.done(out.paint(heading, sanitised(name)) + " already matches " + against + stored)
		return nil
	}
	note, painted := copiesNote(out, len(done.copies), len(done.skipped))
	inv.summary = "reverted " + name + " to " + against + note
	out.done("reverted " + out.paint(heading, sanitised(name)) + " to " + against + painted)
	return nil
}

// unrecordableRefusal refuses to replace the library directory of the skill
// called name, at libPath, while it holds paths git cannot record, a
// repository nested in it say, relative to the directory: what replaces
// the directory would discard them with no record of them anywhere. what
// is the replacement in words, "a revert", and verb the skill command to
// run again once they are moved out.
func unrecordableRefusal(name, libPath string, unrecordable []string, what, verb string) *failure {
	paths := make([]string, len(unrecordable))
	for i, p := range unrecordable {
		paths[i] = quotedPath(filepath.Join(libPath, filepath.FromSlash(p)))
	}
	return refuse(exitRefused, fmt.Sprintf("%s holds %s, which git cannot record", name, strings.Join(paths, ", ")),
		what+" would discard it with no record of it anywhere; move it out of the skill, then run '"+skillCommand(verb, name)+"' again")
}
