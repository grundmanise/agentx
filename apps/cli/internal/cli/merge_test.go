package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// TestHunksOfReadsWhatMergeFileWrites holds the hunks of one file to the
// three versions they come from: several hunks numbered in order, lines of
// the file that are longer runs of marker characters than git's own
// markers, lines that are git's own markers, in the hunk and around it, a
// file with CRLF line endings and no line ending at its end, sides of which
// only one ends the file without a newline, and a file merge-file merges
// whole although merge-tree found it conflicting, which is one hunk of the
// whole of each version. Every hunk gives each version's lines back as that
// version holds them.
func TestHunksOfReadsWhatMergeFileWrites(t *testing.T) {
	t.Parallel()
	requireGit(t)
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	r := gitx.New(map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, false, func(string, ...any) {})
	if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name               string
		base, mine, theirs string
		want               []string // mine|base|theirs of each hunk
	}{
		{
			name: "two hunks", base: "one\ntwo\nthree\nfour\nfive\n", mine: "ONE\ntwo\nthree\nfour\nFIVE\n", theirs: "uno\ntwo\nthree\nfour\ncinco\n",
			want: []string{"ONE\n|one\n|uno\n", "FIVE\n|five\n|cinco\n"},
		},
		{
			name: "lines longer than git's markers", base: "<<<<<<<<<<<<\nx\n============\n", mine: "<<<<<<<<<<<<\nmine\n============\n", theirs: "<<<<<<<<<<<<\ntheirs\n============\n",
			want: []string{"mine\n|x\n|theirs\n"},
		},
		{
			name: "lines that are git's own markers", base: "<<<<<<< x\na\n=======\n>>>>>>> y\n",
			mine: "<<<<<<< x\n=======\n|||||||\n=======\n>>>>>>> y\n", theirs: "<<<<<<< x\n>>>>>>> y\n=======\n>>>>>>> y\n",
			want: []string{"=======\n|||||||\n|a\n|>>>>>>> y\n"},
		},
		{
			name: "a file merge-file merges whole", base: "if (y) {\nreturn x;\n}\nc\n\nc\n", mine: "if (y) {\nreturn x;\nc\n\nc\n", theirs: "if (y) {\nreturn x;\nc\nc\n\n",
			want: []string{"if (y) {\nreturn x;\nc\n\nc\n|if (y) {\nreturn x;\n}\nc\n\nc\n|if (y) {\nreturn x;\nc\nc\n\n"},
		},
		{
			name: "CRLF with no line ending at the end", base: "a\r\nb", mine: "a\r\nb, mine", theirs: "a\r\nb, theirs",
			want: []string{"b, mine|b|b, theirs"},
		},
		{
			name: "one side with no newline at the end", base: "a\nb\n", mine: "a\nb, mine", theirs: "a\nb, theirs\n",
			want: []string{"b, mine|b\n|b, theirs\n"},
		},
		{
			name: "a hunk followed by lines both keep", base: "a\nb", mine: "A\nb", theirs: "α\nb",
			want: []string{"A\n|a\n|α\n"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			versions := map[string]staged{
				gitStageBase: {mode: "100644", oid: "base"}, gitStageMine: {mode: "100644", oid: "mine"}, gitStageTheirs: {mode: "100644", oid: "theirs"},
			}
			bodies := map[string]string{"base": c.base, "mine": c.mine, "theirs": c.theirs}
			merged, err := mergeText(ctx, r, gitDir, t.TempDir(), versions, bodies)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for i, h := range merged.hunks() {
				if h.Index != i+1 {
					t.Errorf("hunk %d is numbered %d", i+1, h.Index)
				}
				got = append(got, fmt.Sprintf("%s|%s|%s", h.Mine, h.Base, h.Theirs))
			}
			equal(t, "hunks", strings.Join(got, " / "), strings.Join(c.want, " / "))
		})
	}
}

// TestMovedAsideReadsThePathAVersionWasMovedFrom: a path merge-tree moved
// one side's version to, to make room at its own path, is read back to
// that path whichever side it was and however git numbered it, and a path
// of the skill that only looks like one is left as it is.
func TestMovedAsideReadsThePathAVersionWasMovedFrom(t *testing.T) {
	t.Parallel()
	mine, theirs := strings.Repeat("a", 40), strings.Repeat("b", 40)
	m := lineage.Merge{Base: strings.Repeat("c", 40), Mine: mine, Theirs: theirs}
	for _, c := range []struct {
		path, real string
		moved      bool
	}{
		{path: "extra~" + mine, real: "extra", moved: true},
		{path: "docs/notes.md~" + theirs, real: "docs/notes.md", moved: true},
		{path: "docs~" + theirs + "_2", real: "docs", moved: true},
		{path: "notes.md"},
		{path: "notes.md~"},
		{path: "notes.md~" + m.Base},
		{path: "notes.md~" + mine + "_"},
		{path: "notes.md~" + mine + "_x"},
		{path: "notes.md~" + mine + ".bak"},
		{path: "~" + mine},
	} {
		real, moved := movedAside(c.path, m)
		if real != c.real || moved != c.moved {
			t.Errorf("movedAside(%q) = %q, %v; want %q, %v", c.path, real, moved, c.real, c.moved)
		}
	}
}

// TestSkillUpdateOffersAFileMergeFileMergesAsOneHunk: git merge-file and
// the merge merge-tree runs do not always agree, and a file merge-tree
// finds conflicting may be one merge-file merges with no conflict left. It
// is still a conflict of its content, and has a hunk to choose from: one,
// the whole of each version, never a file that conflicts whole with
// nothing to choose.
func TestSkillUpdateOffersAFileMergeFileMergesAsOneHunk(t *testing.T) {
	t.Parallel()
	const (
		base   = "if (y) {\nreturn x;\n}\nc\n\nc\n"
		mine   = "if (y) {\nreturn x;\nc\n\nc\n"
		theirs = "if (y) {\nreturn x;\nc\nc\n\n"
	)
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("code", true)
	s.skill("skills/code", "code", "Code", map[string]string{"code.txt": base})
	s.commit("first version")
	h.mustRun("skill", "add", s.url)
	s.write("skills/code/code.txt", theirs)
	s.commit("second version")
	h.mustRun("skill", "check")
	editLibrary(t, h, "code", "code.txt", mine)
	library := onDisk(t, h.library)

	out := h.run("--json", "skill", "update", "code")
	equal(t, "exit", out.exit, 4)
	ev := h.one(out.stdout, "conflict")
	equal(t, "files", conflictFiles(ev), "code.txt:1")
	equal(t, "the hunk", hunkOf(t, ev, "code.txt", 1), mine+"|"+base+"|"+theirs)
	equal(t, "binary", fileOf(t, ev, "code.txt")["binary"], false)
	equal(t, "the library", onDisk(t, h.library), library)

	h.accountGit("update-ref", "-d", lineage.MergeRef("code"))
	text := h.run("skill", "update", "code")
	equal(t, "exit in text", text.exit, 4)
	contains(t, "the text", text.stdout, "code.txt:1\n<<<<<<< mine\n"+mine+"||||||| base\n"+base+"=======\n"+theirs+">>>>>>> theirs\n")
	if strings.Contains(text.stdout, "changed here and by the update") {
		t.Errorf("the text says the file conflicts whole:\n%s", text.stdout)
	}
}

// TestCaseClashFindsPathsThatDifferInCaseAlone: two files, a directory and
// a file, or two directories whose names differ in case alone clash, the
// directories as themselves before anything below them, whatever letters
// Unicode folds together; paths that differ in more than case do not.
func TestCaseClashFindsPathsThatDifferInCaseAlone(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		paths []string // in git's order, a directory followed by what it holds
		want  string   // the pair found, as a|b, or "" for none
	}{
		{name: "no clash", paths: []string{"SKILL.md", "notes.md", "scripts", "scripts/run.sh"}},
		{name: "two files", paths: []string{"README.md", "SKILL.md", "readme.md"}, want: "README.md|readme.md"},
		{name: "a directory and a file", paths: []string{"Docs", "Docs/a.md", "SKILL.md", "docs"}, want: "Docs|docs"},
		{name: "two directories", paths: []string{"Docs", "Docs/a.md", "SKILL.md", "docs", "docs/b.md"}, want: "Docs|docs"},
		{name: "files below one directory", paths: []string{"docs", "docs/A.md", "docs/a.md"}, want: "docs/A.md|docs/a.md"},
		{name: "letters beyond ASCII", paths: []string{"SKILL.md", "Ärger.md", "ärger.md"}, want: "Ärger.md|ärger.md"},
		{name: "a letter folded with a symbol", paths: []string{"SKILL.md", "key.md", "Key.md"}, want: "key.md|Key.md"},
		{name: "more than case", paths: []string{"a.md", "b.md", "ab.md", "AB", "AB/c.md"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			entries := make([]source.TreeEntry, len(c.paths))
			for i, p := range c.paths {
				entries[i] = source.TreeEntry{Path: p, Mode: source.FileMode}
			}
			got := ""
			if a, b, ok := caseClash(entries); ok {
				got = a + "|" + b
			}
			equal(t, "the clash", got, c.want)
		})
	}
}

// TestFoldsCaseProbesTheFileSystem: whether a library directory's file
// system folds case is read from a file it holds, spelled in the other
// case, and agrees with what the file system does; a directory with no
// letter to swap is judged by its operating system.
func TestFoldsCaseProbesTheFileSystem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: probe\n---\n")
	tree, err := treeid.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "a directory with a letter", foldsCase(dir, tree), foldsCaseAt(t, filepath.Join(dir, "SKILL.md")))

	digits := t.TempDir()
	writeFile(t, filepath.Join(digits, "1.2"), "no letter\n")
	if tree, err = treeid.Read(digits); err != nil {
		t.Fatal(err)
	}
	equal(t, "a directory with no letter", foldsCase(digits, tree), runtime.GOOS == "darwin" || runtime.GOOS == "windows")
}

// foldsCaseAt reports whether the file system finds the file at path with
// its name spelled in lower case, as the same file.
func foldsCaseAt(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.Lstat(filepath.Join(filepath.Dir(path), strings.ToLower(filepath.Base(path))))
	return err == nil && os.SameFile(info, other)
}
