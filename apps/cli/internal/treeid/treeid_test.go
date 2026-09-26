package treeid

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// gitTree is the tree id git itself records for dir: every entry added to
// a temporary index of a throwaway repository, then written as a tree. The
// test machine's git configuration is kept out, so nothing but git's
// defaults decides how the files are read.
func gitTree(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	repo := filepath.Join(t.TempDir(), "repo.git")
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	run := func(env []string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(env, "init", "--quiet", "--bare", repo)
	env = append(env, "GIT_DIR="+repo, "GIT_WORK_TREE="+dir, "GIT_INDEX_FILE="+filepath.Join(t.TempDir(), "index"))
	run(env, "add", "--all", ".")
	return run(env, "write-tree")
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // past the umask
		t.Fatal(err)
	}
}

// TestReadGivesTheIdGitGives holds the walker to git on a directory with
// everything a skill can hold: an executable, a nested directory, an empty
// directory, which git does not record, symlinks inside and outside the
// directory and a broken one, which git records as links, and names whose
// order git decides differently from a plain sort because one of them is a
// directory.
func TestReadGivesTheIdGitGives(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "SKILL.md"), "---\nname: alpha\n---\n", 0o644)
	write(t, filepath.Join(dir, "scripts", "run.sh"), "#!/bin/sh\necho run\n", 0o755)
	write(t, filepath.Join(dir, "scripts", "lib", "deep.txt"), "deep\n", 0o644)
	write(t, filepath.Join(dir, "a.b"), "sorted before a/ by git, after it by name\n", 0o644)
	write(t, filepath.Join(dir, "a", "inner.md"), "inner\n", 0o644)
	write(t, filepath.Join(dir, "a-b"), "a dash\n", 0o644)
	write(t, filepath.Join(dir, "group-only-x"), "only the group may run this\n", 0o654)
	if err := os.MkdirAll(filepath.Join(dir, "empty", "emptier"), 0o755); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"inside": "SKILL.md", "outside": "/etc/hostname", "broken": "nowhere/at/all"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := gitTree(t, dir); got.ID != want {
		t.Errorf("tree id = %s, git writes %s", got.ID, want)
	}
	if len(got.Unrecordable) != 0 {
		t.Errorf("unrecordable = %v, want none", got.Unrecordable)
	}
	// The root comes last, after every directory below it, which is the
	// order a writer that needs each subtree before its parent takes.
	if last := got.Dirs[len(got.Dirs)-1]; last.Path != "" || last.ID != got.ID {
		t.Errorf("the last directory is %q at %s, want the root at %s", last.Path, last.ID, got.ID)
	}
	for _, d := range got.Dirs {
		if strings.HasPrefix(d.Path, "empty") {
			t.Errorf("the empty directory %s is recorded", d.Path)
		}
	}

	// A mode is content: making a file executable is a new tree.
	if err := os.Chmod(filepath.Join(dir, "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	moded, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if moded.ID == got.ID {
		t.Error("an exec bit did not change the tree id")
	}
	if want := gitTree(t, dir); moded.ID != want {
		t.Errorf("tree id after chmod = %s, git writes %s", moded.ID, want)
	}
}

// TestReadNamesWhatGitCannotRecord: a repository nested in the directory
// and a named pipe are named, and the id is that of what is left.
func TestReadNamesWhatGitCannotRecord(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "SKILL.md"), "skill\n", 0o644)
	clean, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "vendored", ".git", "HEAD"), "ref: refs/heads/main\n", 0o644)
	write(t, filepath.Join(dir, ".GIT"), "a file git refuses by name in any case\n", 0o644)
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".GIT", "pipe", "vendored/.git"}; !reflect.DeepEqual(got.Unrecordable, want) {
		t.Errorf("unrecordable = %v, want %v", got.Unrecordable, want)
	}
	if got.ID != clean.ID {
		t.Errorf("tree id = %s, want %s, the id of what git can record", got.ID, clean.ID)
	}
}

// TestWrapIsTheImportTreeShape: the one-entry tree an import commit holds.
func TestWrapIsTheImportTreeShape(t *testing.T) {
	t.Parallel()
	outer := t.TempDir()
	write(t, filepath.Join(outer, "pdf-tools", "SKILL.md"), "skill\n", 0o644)
	inner, err := Read(filepath.Join(outer, "pdf-tools"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Wrap("pdf-tools", inner.ID), gitTree(t, outer); got != want {
		t.Errorf("wrapped id = %s, git writes %s", got, want)
	}
}

func TestAnEmptyDirectoryIsTheEmptyTree(t *testing.T) {
	t.Parallel()
	got, err := Read(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	equalID := TreeID(nil)
	if got.ID != EmptyTree || equalID != EmptyTree {
		t.Errorf("empty directory = %s, TreeID(nil) = %s, want %s", got.ID, equalID, EmptyTree)
	}
}
