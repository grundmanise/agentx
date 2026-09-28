package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// TestSplitMergedReadsTheMarkers reads the hunks of a file a merge wrote
// in zdiff3 style out of its marker blocks: several blocks numbered in
// order with the text around them, a line of the file's own that is git's
// default marker, which the markers sized past it never take for one, a
// file with CRLF line endings and none at its end, sides of which only one
// ends the file without a newline, which the hunk gives back as each
// version holds it, and a file with no marker block agentx can tell, which
// has no hunk. Every hunk gives each version's lines back as that version
// holds them.
func TestSplitMergedReadsTheMarkers(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name               string
		base, mine, theirs string
		file               string   // what the merge wrote
		want               []string // mine|base|theirs of each hunk
		around             []string // the text around the blocks
	}{
		{
			name: "two blocks", base: "one\ntwo\nthree\n", mine: "ONE\ntwo\nTHREE\n", theirs: "uno\ntwo\ntres\n",
			file: "<<<<<<< a\nONE\n||||||| b\none\n=======\nuno\n>>>>>>> c\ntwo\n<<<<<<< a\nTHREE\n||||||| b\nthree\n=======\ntres\n>>>>>>> c\n",
			want: []string{"ONE\n|one\n|uno\n", "THREE\n|three\n|tres\n"}, around: []string{"", "two\n", ""},
		},
		{
			name: "a line that is git's own marker", base: "<<<<<<<\nx\n", mine: "<<<<<<<\nmine\n", theirs: "<<<<<<<\ntheirs\n",
			file: "<<<<<<<\n<<<<<<<< a\nmine\n|||||||| b\nx\n========\ntheirs\n>>>>>>>> c\n",
			want: []string{"mine\n|x\n|theirs\n"}, around: []string{"<<<<<<<\n", ""},
		},
		{
			name: "CRLF with no line ending at the end", base: "a\r\nb", mine: "a\r\nb, mine", theirs: "a\r\nb, theirs",
			file: "a\r\n<<<<<<< a\r\nb, mine\r\n||||||| b\r\nb\r\n=======\r\nb, theirs\r\n>>>>>>> c\r\n",
			want: []string{"b, mine|b|b, theirs"}, around: []string{"a\r\n", ""},
		},
		{
			name: "one side with no newline at the end", base: "a\nb\n", mine: "a\nb, mine", theirs: "a\nb, theirs\n",
			file: "a\n<<<<<<< a\nb, mine\n||||||| b\nb\n=======\nb, theirs\n>>>>>>> c\n",
			want: []string{"b, mine|b\n|b, theirs\n"}, around: []string{"a\n", ""},
		},
		{
			name: "no marker block", base: "<<<<<<<\n", mine: "<<<<<<< a\n", theirs: "x\n",
			file: "<<<<<<< a\nx\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			versions := map[string]staged{
				gitStageBase: {mode: "100644", oid: "base"}, gitStageMine: {mode: "100644", oid: "mine"}, gitStageTheirs: {mode: "100644", oid: "theirs"},
			}
			bodies := map[string]string{"base": c.base, "mine": c.mine, "theirs": c.theirs}
			path := filepath.Join(t.TempDir(), "file")
			writeFile(t, path, c.file)
			hunks, why, err := hunksIn(path, versions, bodies)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for i, h := range hunks {
				if h.Index != i+1 {
					t.Errorf("hunk %d is numbered %d", i+1, h.Index)
				}
				got = append(got, fmt.Sprintf("%s|%s|%s", h.Mine, h.Base, h.Theirs))
			}
			equal(t, "hunks", strings.Join(got, " / "), strings.Join(c.want, " / "))
			if len(c.want) == 0 {
				equal(t, "why", why, "changed here and by the update, with no conflict markers agentx can tell from its lines")
				return
			}
			around, _, err := splitMerged(c.file, markerSizeIn(c.file, c.mine, c.base, c.theirs))
			if err != nil {
				t.Fatal(err)
			}
			equal(t, "around", strings.Join(around, "/"), strings.Join(c.around, "/"))
		})
	}
	if _, _, err := splitMerged("<<<<<<< a\nx\n=======\n", 7); err == nil {
		t.Error("a block with no end reads")
	}
}

// TestMarkerSizeInIgnoresContentRuns: the markers of a merged file are the
// first run of < longer than any run of a marker character at the start of
// a line of its versions and followed by a space or the end of the line,
// so a line of the versions' own is never one, however long, whatever
// size the markers were written at.
func TestMarkerSizeInIgnoresContentRuns(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		file  string
		blobs []string
		want  int
	}{
		{name: "git's own size", file: "a\n<<<<<<< mine\n", blobs: []string{"a\n"}, want: 7},
		{name: "past a run of the versions", file: "<<<<<<<<< x\n<<<<<<<<<< mine\n", blobs: []string{"<<<<<<<<< x\n"}, want: 10},
		{name: "past a run of another marker character", file: "<<<<<<<< mine\n", blobs: []string{"=======\n"}, want: 8},
		{name: "a marker with no label", file: "<<<<<<<<\r\n", blobs: []string{"<<<<<<<\r\n"}, want: 8},
		{name: "a run with text after it", file: "<<<<<<<<x\n", blobs: []string{"a\n"}},
		{name: "no longer than the versions'", file: "<<<<<<< mine\n", blobs: []string{"||||||| x\n"}},
		{name: "none", file: "a\nb\n", blobs: []string{"a\n"}},
	} {
		if got := markerSizeIn(c.file, c.blobs...); got != c.want {
			t.Errorf("%s: markerSizeIn = %d, want %d", c.name, got, c.want)
		}
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
