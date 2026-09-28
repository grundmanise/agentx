package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

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
