package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

func newSkillDiffCommand(inv *invocation) *cobra.Command {
	var update bool
	var commit string
	cmd := &cobra.Command{
		Use:         "diff <name>",
		Short:       "Show unpublished edits, or what an update changes",
		Annotations: map[string]string{annotationSection: "skills"},
		Long: "Show how the library directory of a managed skill differs from its base version,\n" +
			"the version it was installed at or last published, as one unified diff per file:\n" +
			"its edits not yet published, which 'agentx skill publish <name>' pushes to its\n" +
			"source. Every edit counts, whatever tool made it, a file made executable and a\n" +
			"file turned into a symlink included. Files git ignores do not. Nothing is\n" +
			"written to the library. With --update, show instead what\n" +
			"'agentx skill update <name>' would apply: what the update\n" +
			"'" + checkUpdatesCommand + "' found changes in the base version, and for a hub\n" +
			"skill, first, what another machine published to the hub as last fetched.\n\n" +
			"For a hub skill, show its unpublished edits: how its skill directory\n" +
			"differs from its last published version, the newest commit of its branch the\n" +
			"hub holds as last fetched, or from the commit that created it when the\n" +
			"hub holds none, so edits an update or a fork recorded show until they\n" +
			"are published; or, with --commit <id>, how it differs from that commit.",
		Example: examples(
			"agentx skill diff pdf", "edits not yet published",
			"agentx skill diff pdf --update", "what an update would apply",
			"agentx skill diff release-notes --commit e07d5b2", "a hub skill against a commit",
		),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case update && commit != "":
				return fail(exitUsage, "--update and --commit cannot be given together", "compare with the update or with a commit, one at a time")
			case cmd.Flags().Changed("commit") && strings.TrimSpace(commit) == "":
				return fail(exitUsage, "--commit needs a commit", "name a commit by its id, such as one 'agentx skill history' lists")
			case update:
				return inv.skillDiffUpdate(cmd.Context(), args[0])
			}
			return inv.skillDiff(cmd.Context(), args[0], commit)
		},
	}
	cmd.Flags().BoolVar(&update, "update", false, "show what an update would apply")
	cmd.Flags().StringVar(&commit, "commit", "", "compare a hub skill with this commit instead of its last published version")
	return cmd
}

// diffEvent is one file of a skill that differs between two versions, with
// the unified diff git wrote for it. Its path is relative to the skill's
// directory, whatever the upstream calls that directory.
type diffEvent struct {
	event
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"` // added, modified or deleted
	Patch  string `json:"patch"`
	Step   string `json:"step,omitempty"` // with --update, the step of a fork's update: hub or upstream; absent otherwise
}

// The steps of a fork's update a diff of skill diff --update can be, see
// forkDiffUpdate.
const (
	diffStepHub      = "hub"      // the hub step: what another machine published
	diffStepUpstream = "upstream" // the merge of the update from upstream
)

// The statuses of a file in a diff: what the second version did to it.
const (
	diffAdded    = "added"
	diffModified = "modified"
	diffDeleted  = "deleted"
)

// fileDiff is one file of a diff before it becomes an event.
type fileDiff struct {
	path, status, patch string
}

// skillDiff compares a managed skill's library directory with its base
// version. The directory's tree id is computed in process first, and a
// directory that holds the base by it costs no git at all. Otherwise git
// compares the two with the directory as a work tree, over an index loaded
// from the base: git produces every diff and applies the skill's ignore
// and attribute rules, so a file git ignores is never in it, and the
// library is only read.
//
// Whether the directory matches its base is decided as its state is, so
// that the diff never says a skill matches while the listing calls it
// modified: a directory holding something git cannot record is not the
// base version, even when every path git can record is.
//
// A hub skill is compared with its last published version, or
// with the commit given, see forkDiff.
func (inv *invocation) skillDiff(ctx context.Context, name, commit string) error {
	gitDir, rec, held, err := inv.hubRecord(ctx, name)
	if err != nil {
		return err
	}
	if held && rec.Kind == lineage.KindFork {
		return inv.forkDiff(ctx, gitDir, rec, commit)
	}
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return inv.noLibrarySkill(name)
	}
	if commit != "" {
		return notAForkRefusal(name, held)
	}
	if err := managedRefusal(name, rec, held); err != nil {
		return err
	}
	tree, err := inv.readLibraryTree(lib.Path)
	if err != nil {
		return err
	}
	against := "its base version at " + short(rec.Import.Commit)
	subject := diffSubject{plain: name, painted: inv.out.paint(heading, sanitised(name))}
	base := baseVersion(rec)
	if fastHolds(tree, inv.systemFilesIgnored(), base) {
		inv.reportDiff(subject, name, "", against, nil, 0)
		return nil
	}
	for _, p := range tree.Unrecordable {
		inv.out.warn(quotedPath(filepath.Join(lib.Path, filepath.FromSlash(p))) + " cannot be recorded by git and is left out of the diff")
	}
	wt, err := inv.openWorkTree(ctx, gitDir, lib.ResolvedPath)
	if err != nil {
		return storeFailure(err)
	}
	defer wt.Close()
	written, err := writeWorkTree(ctx, wt, base)
	if err != nil {
		return storeFailure(err)
	}
	var files []fileDiff
	if !base.holds(written) {
		status, patch, err := wt.DiffCached(ctx, base.load)
		if err == nil {
			files, err = parseDiff(status, patch)
		}
		if err != nil {
			return storeFailure(err)
		}
	}
	inv.reportDiff(subject, name, "", against, files, len(tree.Unrecordable))
	return nil
}

// skillDiffUpdate shows what 'agentx skill update <name>' would apply, as
// the store holds it now: it reads nothing else, writes nothing and never
// reaches the network. A managed skill's update is the one the last check
// found, see upstreamChanges. A fork's is its hub step first, then that
// update, see forkDiffUpdate. A skill with neither has nothing to show,
// and the refusal says how to look for an update.
func (inv *invocation) skillDiffUpdate(ctx context.Context, name string) error {
	if _, ok := librarySkill(inv.dirs.Library, name); !ok {
		return inv.noLibrarySkill(name)
	}
	gitDir, rec, held, err := inv.hubRecord(ctx, name)
	if err != nil {
		return err
	}
	if held && rec.Kind == lineage.KindFork {
		return inv.forkDiffUpdate(ctx, gitDir, rec)
	}
	if err := managedRefusal(name, rec, held); err != nil {
		return err
	}
	c, ok := rec.AtCandidate()
	if !ok {
		return noUpdateRefusal(name)
	}
	files, err := diffTrees(ctx, inv.git, gitDir, rec.Commit+":"+rec.Import.Dir(), c.Commit+":"+c.Import.Dir())
	if err != nil {
		return storeFailure(err)
	}
	inv.reportUpdateDiff(name, "", rec, c, files)
	return nil
}

// noUpdateRefusal is the refusal of skill diff --update for a skill that
// has no update to show.
func noUpdateRefusal(name string) error {
	return fail(exitRefused, "no update of "+name+" is known",
		"run '"+checkUpdatesCommand+"' to look for one; it pins what it finds for this command to show")
}

// forkDiffUpdate is skill diff --update of the fork rec, in the order its
// update applies it, each part only when there is one. First its hub
// step, see judgePull: what the hub's branch of it, as last fetched,
// changed since the commit the two share, which is what another machine
// published. Then the update the last check found from its upstream,
// compared with the fork's base version, the import commit its history
// names, exactly as a managed skill's: what the update changes upstream,
// which is what merging it brings in, and not how it differs from the
// fork's own commits. After a hub step, that base is the one the hub
// step leaves, see pulledFork, and an update it took in, or left stale,
// is none. Each diff event names its step. A fork
// the hub holds nothing new for is refused as a managed skill with no
// update is, or, with no upstream, as one that has none to update from.
func (inv *invocation) forkDiffUpdate(ctx context.Context, gitDir string, rec lineage.Record) error {
	name := rec.Name
	tip, base, err := inv.hubAheadOf(ctx, gitDir, rec)
	if err != nil {
		return err
	}
	var hubFiles []fileDiff
	if tip != "" {
		dir, err := inv.forkDir(ctx, gitDir, rec)
		if err != nil {
			return err
		}
		if hubFiles, err = diffTrees(ctx, inv.git, gitDir, base+":"+dir, tip+":"+dir); err != nil {
			return storeFailure(err)
		}
	}
	if rec.Fork == nil {
		recs := map[string]lineage.Record{name: rec}
		if err := lineage.ReadForks(ctx, inv.git, gitDir, recs, inv.forkWalks); err != nil {
			return storeFailure(err)
		}
		rec = recs[name]
	}
	after := rec
	if tip != "" {
		if after, err = inv.pulledFork(ctx, gitDir, rec, tip, base); err != nil {
			return err
		}
	}
	var baseErr error
	forkBase, f := forkBaseRecord(after)
	c, upstream := lineage.Record{}, false
	if f != nil {
		baseErr = f
	} else {
		c, upstream = forkBase.AtCandidate()
	}
	switch {
	case tip == "" && baseErr != nil:
		return baseErr
	case tip == "" && !upstream:
		return noUpdateRefusal(name)
	}
	var summaries []string
	if tip != "" {
		at := " at " + short(tip)
		subject := diffSubject{plain: "what the hub holds of " + name + at, painted: "what the hub holds of " + inv.out.paint(heading, sanitised(name)) + at}
		inv.reportDiff(subject, name, diffStepHub, "the version both share at "+short(base), hubFiles, 0)
		summaries = append(summaries, inv.summary)
	}
	if upstream {
		files, err := diffTrees(ctx, inv.git, gitDir, forkBase.Commit+":"+forkBase.Import.Dir(), c.Commit+":"+c.Import.Dir())
		if err != nil {
			return storeFailure(err)
		}
		inv.reportUpdateDiff(name, diffStepUpstream, forkBase, c, files)
		summaries = append(summaries, inv.summary)
	}
	inv.summary = strings.Join(summaries, "; ")
	return nil
}

// pulledFork is the fork rec as the hub step of its update leaves its
// lineage, tip being the hub's branch of it and shared the commit the two
// share, see judgePull: the remote's lineage after a fast-forward, and
// after a merge the fork's own with the base lineage.PickBase chooses. Its
// candidate is gone when the new base leaves it stale, see staleCandidate,
// as the update deletes it then. One git log reads the remote's lineage.
func (inv *invocation) pulledFork(ctx context.Context, gitDir string, rec lineage.Record, tip, shared string) (lineage.Record, error) {
	walked, err := lineage.Walk(ctx, inv.git, gitDir, []string{tip})
	if err != nil {
		return rec, storeFailure(err)
	}
	next := walked[tip]
	if shared != rec.Commit && rec.Fork != nil {
		there := next
		next = *rec.Fork
		if lineage.PickBase(ctx, inv.git, gitDir, *rec.Fork, there) != rec.Fork.Base {
			next.Base, next.BaseTree, next.BaseTime, next.Import = there.Base, there.BaseTree, there.BaseTime, there.Import
		}
	}
	pulled := rec
	pulled.Fork = &next
	if inv.staleCandidate(ctx, gitDir, rec, next.Base, next.Import) != "" {
		pulled.Candidate = nil
	}
	return pulled, nil
}

// hubAheadOf is the hub's branch of the fork rec, as the last fetch
// of the hub left its remote-tracking branch, and the commit the two share,
// when that branch holds commits the fork's lacks, see hubAhead: what
// the hub step of the fork's update takes in. Both are "" when it holds
// none, the hub holds no branch of the fork, or no hub is set.
func (inv *invocation) hubAheadOf(ctx context.Context, gitDir string, rec lineage.Record) (tip, base string, err error) {
	_, remote, set, err := inv.hubSource()
	if err != nil || !set {
		return "", "", err
	}
	out, err := inv.git.Isolated(ctx, gitDir, "for-each-ref", "--format=%(objectname)", lineage.RemoteForkRef(remote, rec.Name))
	if err != nil {
		return "", "", storeFailure(err)
	}
	tip = strings.TrimSpace(out)
	if tip == "" || tip == rec.Commit {
		return "", "", nil
	}
	out, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", rec.Commit, tip)
	if err != nil {
		return "", "", storeFailure(err)
	}
	base = strings.TrimSpace(out)
	if status != 0 || base == tip { // no commit in common, which an update refuses, or nothing the fork lacks
		return "", "", nil
	}
	return tip, base, nil
}

// reportUpdateDiff reports files, what the update c, the candidate of
// the base version rec, changes in it, as the diff of step.
func (inv *invocation) reportUpdateDiff(name, step string, rec, c lineage.Record, files []fileDiff) {
	at := " at " + short(c.Import.Commit)
	subject := diffSubject{plain: "the update of " + name + at, painted: "the update of " + inv.out.paint(heading, sanitised(name)) + at}
	inv.reportDiff(subject, name, step, "its base version at "+short(rec.Import.Commit), files, 0)
}

// diffSubject is what a diff's line says was compared with a base version:
// the skill's library directory, named by the skill, or its update. plain
// is the words the result carries, painted the line stdout prints.
type diffSubject struct {
	plain, painted string
}

// reportDiff emits one diff event per file of the skill called name and
// prints the diffs under one line that says what was compared with what.
// step names the step of an update the diff is, for a fork's, see
// forkDiffUpdate, and is "" for every other diff.
// subject names what was compared, against the version it was compared
// with, and unrecordable is how many paths of the library directory git
// cannot record, which no diff shows and each of which makes the directory
// another version all the same: the line counts them, and it says the two
// match only when there are neither diffs nor such paths.
func (inv *invocation) reportDiff(subject diffSubject, name, step, against string, files []fileDiff, unrecordable int) {
	out := inv.out
	for _, f := range files {
		out.emit(diffEvent{event: newEvent("diff"), Name: name, Path: f.path, Status: f.status, Patch: f.patch, Step: step})
	}
	var in, painted string // what the line says it differs in, bare and painted
	switch {
	case len(files) == 0 && unrecordable == 0:
		inv.summary = subject.plain + " matches " + against
		out.print(subject.painted, " matches ", against)
		return
	case len(files) == 0:
		in = "only in " + plural(unrecordable, "path") + " git cannot record"
		painted = "only in " + out.paint(noteStyle, plural(unrecordable, "path")) + " git cannot record"
	default:
		in = "in " + plural(len(files), "file")
		painted = "in " + out.paint(noteStyle, plural(len(files), "file"))
		if unrecordable > 0 {
			in += ", and " + plural(unrecordable, "path") + " git cannot record"
			painted += ", and " + out.paint(noteStyle, plural(unrecordable, "path")) + " git cannot record"
		}
	}
	inv.summary = subject.plain + " differs from " + against + " " + in
	out.print(subject.painted, " differs from ", against, " ", painted)
	for _, f := range files {
		for _, line := range strings.SplitAfter(f.patch, "\n") {
			if line != "" {
				out.print(patchLine(strings.TrimSuffix(line, "\n")))
			}
		}
	}
}

// patchLine is one line of a diff as the text output prints it. The lines
// hold a file's own bytes, which whoever edited the file chose, so no
// control character may reach the terminal; but a diff is read for its
// layout, so where sanitised would fold every run of spaces into one, this
// keeps every space and every tab and turns each other control character
// into a space of its own. It reads bytes rather than runes, as quotedPath
// does, so a file that is not UTF-8 text prints as the bytes it holds
// rather than with a replacement character for each byte UTF-8 does not
// take.
//
// One such byte is not printed as it is: a byte from 0x80 to 0x9F that is
// no part of a UTF-8 character, which a terminal set to 8-bit controls
// reads as a C1 control, 0x9B being the one-byte form of the escape that
// starts a sequence. It becomes a space too. A UTF-8 character is copied
// whole, so a byte in that range that continues one, as the second byte
// of "é" does, is never taken for a control.
func patchLine(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	for i := 0; i < len(line); {
		if n := controlAt(line, i); n > 0 && line[i] != '\t' {
			b.WriteByte(' ')
			i += n
			continue
		}
		if c := line[i]; c >= utf8.RuneSelf {
			if r, size := utf8.DecodeRuneInString(line[i:]); r != utf8.RuneError || size > 1 {
				b.WriteString(line[i : i+size])
				i += size
				continue
			}
			if c <= 0x9f {
				c = ' '
			}
			b.WriteByte(c)
			i++
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

// diffTrees is the diff between two trees of the store, read as
// parseDiff reads it. Two reads that do not depend on each other, run at
// once: the status of every file and the diff itself.
func diffTrees(ctx context.Context, r *gitx.Runner, gitDir, from, to string) ([]fileDiff, error) {
	outs, err := r.IsolatedAll(ctx, gitDir, [][]string{
		{"diff-tree", "-r", "-z", "--no-renames", "--name-status", from, to},
		{"diff-tree", "-r", "-p", "--no-renames", "--no-color", from, to},
	})
	if err != nil {
		return nil, err
	}
	return parseDiff(outs[0], outs[1])
}

// parseDiff reads a diff between two versions of a skill as git writes it,
// one entry per file that differs, sorted by path, with the unified diff
// git wrote for it. status is the status of every file, NUL-terminated so
// that a name git would quote comes back as its own bytes, and patch the
// diff itself. Renames are not looked for: a file moved is one deleted and
// one added, which is what happened to the paths an agent reads.
//
// git writes a file whose type changed, a file turned into a symlink or
// back, as two diffs of the one path, the old content deleted and the new
// one added. It is one file here, modified, with both.
func parseDiff(status, patch string) ([]fileDiff, error) {
	fields := strings.Split(strings.TrimSuffix(status, "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		fields = nil
	}
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("git diff: cannot read the status of %q", status)
	}
	var chunks []string
	for _, line := range strings.SplitAfter(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			chunks = append(chunks, line)
		case len(chunks) > 0:
			chunks[len(chunks)-1] += line
		}
	}
	var files []fileDiff
	next := 0
	for i := 0; i < len(fields); i += 2 {
		status, path, n := fields[i], fields[i+1], 1
		f := fileDiff{path: path, status: diffModified}
		switch status {
		case "A":
			f.status = diffAdded
		case "D":
			f.status = diffDeleted
		case "T":
			n = 2
		}
		if next+n > len(chunks) {
			return nil, fmt.Errorf("git diff wrote %d diffs for %d files", len(chunks), len(fields)/2)
		}
		f.patch = strings.Join(chunks[next:next+n], "")
		if !strings.HasSuffix(f.patch, "\n") {
			f.patch += "\n"
		}
		next += n
		files = append(files, f)
	}
	if next != len(chunks) {
		return nil, fmt.Errorf("git diff wrote %d diffs for %d files", len(chunks), len(fields)/2)
	}
	return files, nil
}
