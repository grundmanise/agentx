package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

func newSkillDiffCommand(inv *invocation) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <name>",
		Short: "Show how a managed skill differs from the version it was installed at",
		Long: "Show how the library directory of a managed skill differs from its base version,\n" +
			"the version it was installed at, as one unified diff per file. Every edit counts,\n" +
			"whatever tool made it, a file made executable and a file turned into a symlink\n" +
			"included. Nothing is written to the library.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillDiff(cmd.Context(), args[0])
		},
	}
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
// version. The directory's tree is computed in process first, and a
// directory that holds exactly the base costs no write at all. Otherwise
// the directory is written into the account repo as the tree it is,
// through the one writer every tree of the directory takes, and git
// compares the two trees: git produces every diff, and the library is only
// read.
//
// Whether the directory matches its base is decided as its state is, by
// Record.Current and by nothing else, so that the diff never says a skill
// matches while the listing calls it modified: a directory holding
// something git cannot record is not the base version, even when every
// path git can record is. Nor is one holding every file of a base that an
// earlier agentx stored over a source's own tree, in a form no directory
// is current against: git finds no file that differs, and the command
// says the difference is where the version is stored, and that a revert
// stores it again without touching a file.
//
// The versions compared are chosen here and diffed by diffTrees, so a
// comparison of other versions of the same skill is another choice of the
// two trees and nothing more.
func (inv *invocation) skillDiff(ctx context.Context, name string) error {
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return inv.noLibrarySkill(name)
	}
	gitDir, rec, err := inv.managedRecord(ctx, name, "compare with")
	if err != nil {
		return err
	}
	tree, err := inv.readLibraryTree(lib.Path)
	if err != nil {
		return err
	}
	against := "its base version at " + short(rec.Import.Commit)
	if rec.Current(tree) {
		inv.reportDiff(name, against, nil, 0)
		return nil
	}
	for _, p := range tree.Unrecordable {
		inv.out.warn(quotedPath(filepath.Join(lib.Path, filepath.FromSlash(p))) + " cannot be recorded by git and is left out of the diff")
	}
	base, err := lineage.ReadBase(ctx, inv.git, gitDir, rec)
	if err != nil {
		return accountRepoFailure(err)
	}
	if base.HeldBy(tree) {
		inv.reportStoredDiff(name, against)
		return nil
	}
	var files []fileDiff
	if tree.ID != base.ID() {
		written, err := lineage.WriteDir(ctx, inv.git, gitDir, lib.ResolvedPath, tree)
		if errors.Is(err, lineage.ErrChanged) {
			return fail(exitRefused, fmt.Sprintf("%s changed while agentx read it: %v", name, err), "run the command again")
		}
		if err != nil {
			return accountRepoFailure(err)
		}
		if files, err = diffTrees(ctx, inv.git, gitDir, base.Tree, written); err != nil {
			return accountRepoFailure(err)
		}
	}
	inv.reportDiff(name, against, files, len(tree.Unrecordable))
	return nil
}

// reportDiff emits one diff event per file and prints the diffs under one
// line that says what was compared with what. against names the version
// the library was compared with, and unrecordable is how many paths of the
// library directory git cannot record, which no diff shows and each of
// which makes the directory another version all the same: the line counts
// them, and it says the two match only when there are neither diffs nor
// such paths.
func (inv *invocation) reportDiff(name, against string, files []fileDiff, unrecordable int) {
	out := inv.out
	for _, f := range files {
		out.emit(diffEvent{event: newEvent("diff"), Name: name, Path: f.path, Status: f.status, Patch: f.patch})
	}
	var in, painted string // what the line says it differs in, bare and painted
	switch {
	case len(files) == 0 && unrecordable == 0:
		inv.summary = name + " matches " + against
		out.print(out.paint(heading, sanitised(name)), " matches ", against)
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
	inv.summary = name + " differs from " + against + " " + in
	out.print(out.paint(heading, sanitised(name)), " differs from ", against, " ", painted)
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
// to put that right.
func (inv *invocation) reportStoredDiff(name, against string) {
	const stored = " only in how the account repo stores it; run '"
	fix := skillCommand("revert", name) + "' to store it as git writes it today, which changes no file"
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

// diffTrees is the diff between two trees of the account repo, one entry
// per file that differs, sorted by path, with the unified diff git writes
// for it. Two reads that do not depend on each other, run at once: the
// status of every file, NUL-terminated so that a name git would quote comes
// back as its own bytes, and the diff itself. Renames are not looked for:
// a file moved is one deleted and one added, which is what happened to the
// paths an agent reads.
//
// git writes a file whose type changed, a file turned into a symlink or
// back, as two diffs of the one path, the old content deleted and the new
// one added. It is one file here, modified, with both.
func diffTrees(ctx context.Context, r *gitx.Runner, gitDir, from, to string) ([]fileDiff, error) {
	outs, err := r.IsolatedAll(ctx, gitDir, [][]string{
		{"diff-tree", "-r", "-z", "--no-renames", "--name-status", from, to},
		{"diff-tree", "-r", "-p", "--no-renames", "--no-color", from, to},
	})
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(outs[0], "\x00"), "\x00")
	if len(fields) == 1 && fields[0] == "" {
		fields = nil
	}
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("git diff-tree: cannot read the status of %q", outs[0])
	}
	var chunks []string
	for _, line := range strings.SplitAfter(outs[1], "\n") {
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
			return nil, fmt.Errorf("git diff-tree wrote %d diffs for %d files", len(chunks), len(fields)/2)
		}
		f.patch = strings.Join(chunks[next:next+n], "")
		if !strings.HasSuffix(f.patch, "\n") {
			f.patch += "\n"
		}
		next += n
		files = append(files, f)
	}
	if next != len(chunks) {
		return nil, fmt.Errorf("git diff-tree wrote %d diffs for %d files", len(chunks), len(fields)/2)
	}
	return files, nil
}
