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
		Use:   "diff <name>",
		Short: "Show how a skill differs from its base version, or a fork from what it published",
		Long: "Show how the library directory of a managed skill differs from its base version,\n" +
			"the version it was installed at, as one unified diff per file. Every edit counts,\n" +
			"whatever tool made it, a file made executable and a file turned into a symlink\n" +
			"included. Files git ignores do not. Nothing is written to the library. With\n" +
			"--update, show instead what the update '" + checkUpdatesCommand + "' found\n" +
			"changes in the base version.\n\n" +
			"For a fork, show its unpublished edits: how its skill directory differs from its\n" +
			"last published version, the newest commit of its branch the account remote holds\n" +
			"as last fetched, or from the commit that created it when the account remote holds\n" +
			"none, so edits an update or a fork recorded show until they are published; or,\n" +
			"with --commit <id>, how it differs from that commit.",
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
	cmd.Flags().BoolVar(&update, "update", false, "compare the base version with the update the last check found")
	cmd.Flags().StringVar(&commit, "commit", "", "compare a fork with this commit instead of its last one")
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
}

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
// base version, even when every path git can record is. Nor is one holding
// every file of a base that an earlier agentx stored over a source's own
// tree, in a form no directory holds: git finds no file that differs, and
// the command says the difference is where the version is stored, and
// that installing that version again stores it anew without touching a
// file.
//
// One of your own skills is compared with its last published version, or
// with the commit given, see forkDiff.
func (inv *invocation) skillDiff(ctx context.Context, name, commit string) error {
	gitDir, rec, held, err := inv.accountRecord(ctx, name)
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
		inv.reportDiff(subject, name, against, nil, 0)
		return nil
	}
	for _, p := range tree.Unrecordable {
		inv.out.warn(quotedPath(filepath.Join(lib.Path, filepath.FromSlash(p))) + " cannot be recorded by git and is left out of the diff")
	}
	wt, err := inv.openWorkTree(ctx, gitDir, lib.ResolvedPath)
	if err != nil {
		return accountRepoFailure(err)
	}
	defer wt.Close()
	written, err := writeWorkTree(ctx, wt, base)
	if err != nil {
		return accountRepoFailure(err)
	}
	var files []fileDiff
	if !base.holds(written) {
		status, patch, err := wt.DiffCached(ctx, base.load)
		if err == nil {
			files, err = parseDiff(status, patch)
		}
		if err != nil {
			return accountRepoFailure(err)
		}
		if len(files) == 0 && len(tree.Unrecordable) == 0 {
			inv.reportStoredDiff(name, rec.Import.Source, lib.Path, against, contentHashAt(lib.Path) == rec.Import.Hash)
			return nil
		}
	}
	inv.reportDiff(subject, name, against, files, len(tree.Unrecordable))
	return nil
}

// skillDiffUpdate shows what the update the last check found for a
// managed skill does to its base version: the base's directory against the
// candidate's, each the upstream directory of its import commit, so that
// every path is relative to the skill's directory. Both trees are in the
// account repo already, so this reads nothing else, writes nothing and
// never reaches the network: the candidate is what the check pinned, not
// what the source holds now. A skill with no candidate, or with one its
// branch already holds, has nothing to show, and the refusal says how to
// look for an update.
//
// A fork's update is compared with the fork's base version, the import
// commit its history names, exactly as a managed skill's: what the update
// changes upstream, which is what merging it brings in, and not how it
// differs from the fork's own commits.
func (inv *invocation) skillDiffUpdate(ctx context.Context, name string) error {
	if _, ok := librarySkill(inv.dirs.Library, name); !ok {
		return inv.noLibrarySkill(name)
	}
	gitDir, rec, held, err := inv.accountRecord(ctx, name)
	if err != nil {
		return err
	}
	if held && rec.Kind == lineage.KindFork {
		if rec, err = inv.forkBaseOf(ctx, gitDir, rec); err != nil {
			return err
		}
	} else if err := managedRefusal(name, rec, held); err != nil {
		return err
	}
	c, ok := rec.AtCandidate()
	if !ok {
		return fail(exitRefused, "no update of "+name+" is known",
			"run '"+checkUpdatesCommand+"' to look for one; it pins what it finds for this command to show")
	}
	files, err := diffTrees(ctx, inv.git, gitDir, rec.Commit+":"+rec.Import.Dir(), c.Commit+":"+c.Import.Dir())
	if err != nil {
		return accountRepoFailure(err)
	}
	at := " at " + short(c.Import.Commit)
	subject := diffSubject{plain: "the update of " + name + at, painted: "the update of " + inv.out.paint(heading, sanitised(name)) + at}
	inv.reportDiff(subject, name, "its base version at "+short(rec.Import.Commit), files, 0)
	return nil
}

// diffSubject is what a diff's line says was compared with a base version:
// the skill's library directory, named by the skill, or its update. plain
// is the words the result carries, painted the line stdout prints.
type diffSubject struct {
	plain, painted string
}

// reportDiff emits one diff event per file of the skill called name and
// prints the diffs under one line that says what was compared with what.
// subject names what was compared, against the version it was compared
// with, and unrecordable is how many paths of the library directory git
// cannot record, which no diff shows and each of which makes the directory
// another version all the same: the line counts them, and it says the two
// match only when there are neither diffs nor such paths.
func (inv *invocation) reportDiff(subject diffSubject, name, against string, files []fileDiff, unrecordable int) {
	out := inv.out
	for _, f := range files {
		out.emit(diffEvent{event: newEvent("diff"), Name: name, Path: f.path, Status: f.status, Patch: f.patch})
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

// reportStoredDiff says that the library directory holds every file of
// its base version while the import commit stores that version in a form
// git no longer writes, which is why the skill lists as modified, and how
// to put that right. Installing the same version again stores it as git
// writes it today, but it adopts only a directory whose content hash is
// the version's, and exact says whether the one at libPath is: a file git
// ignores, which the diff leaves out, counts for the hash, so when it is
// not, the hint says to move such files out first. The install also needs
// the source to hold that version still, which this command cannot see,
// so the hint names the other way out as well: the update to a newer
// version, which stores that one anew.
func (inv *invocation) reportStoredDiff(name, source, libPath, against string, exact bool) {
	const stored = " only in how the account repo stores it; "
	add := "run 'agentx skill add " + shellWord(source) + " --name " + shellWord(name) + "' to install that version again while the source still holds it, which stores it as git writes it today"
	if exact {
		add += " and changes no file"
	} else {
		add = "move what git ignores out of " + quotedPath(libPath) + ", then " + add
	}
	fix := add + ", or, once '" + checkUpdatesCommand + "' finds a newer version, run '" + skillCommand("update", name) + "'"
	inv.summary = name + " differs from " + against + stored + fix
	out := inv.out
	out.print(out.paint(heading, sanitised(name)), " differs from ", against, stored, sanitised(fix))
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

// diffTrees is the diff between two trees of the account repo, read as
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
