package home

import (
	"os"
	"path/filepath"
	"strings"
)

// A fork's worktree is told apart from files alone, with no git: the
// journal decides from what is on disk whether a worktree step ran, a
// recovery may have no git to ask, and a command checks a worktree's
// pointers before it journals anything that runs git inside it. A linked
// worktree is two files that point at each other: the .git file at the
// worktree's root, "gitdir: <admin directory>", and the gitdir file in that
// admin directory, the path of the .git file. Either may be absolute or,
// once the repository writes relative worktree paths, relative to the
// directory it sits in. The admin directory's HEAD names the branch.

// WorktreeAt reports whether path is a linked worktree whose registration
// names it back and whose HEAD is the branch, a short branch name such as
// skills/pdf.
func WorktreeAt(path, branch string) bool {
	admin, ok := AdminDirOf(path)
	if !ok || !namesBack(admin, path) {
		return false
	}
	return headIs(admin, branch)
}

// HalfMade reports whether path holds what a worktree add stopped half way
// leaves: nothing but a .git file, whose registration is gone or does not
// yet have HEAD on the branch. git writes the admin directory and the .git
// file before HEAD names the branch, and cleans them up after a signal it
// can catch, never after SIGKILL or a power loss.
func HalfMade(path, branch string) bool {
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".git" || !entries[0].Type().IsRegular() {
		return false
	}
	admin, ok := AdminDirOf(path)
	if !ok {
		return true
	}
	if _, err := os.Stat(admin); err != nil {
		return true
	}
	return !headIs(admin, branch)
}

// PointersMoved reports whether the worktree at path has a .git file whose
// pointers no longer meet: the admin directory it names is not there, or
// that directory's gitdir names another path. That is what moving agentx
// home as a whole, or the worktree on its own, leaves; git cannot run in
// such a worktree until its pointers are repaired.
func PointersMoved(path string) bool {
	admin, ok := AdminDirOf(path)
	if !ok {
		return false
	}
	if _, err := os.Stat(admin); err != nil {
		return true
	}
	return !namesBack(admin, path)
}

// AdminDirOf is the admin directory the .git file at path's root names,
// made absolute, and false when path has no such file.
func AdminDirOf(path string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil {
		return "", false
	}
	target, ok := strings.CutPrefix(strings.TrimRight(string(b), "\r\n"), "gitdir: ")
	if !ok || target == "" {
		return "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(path, target)
	}
	return filepath.Clean(target), true
}

// namesBack reports whether the admin directory's gitdir file names the
// .git file at path's root.
func namesBack(admin, path string) bool {
	b, err := os.ReadFile(filepath.Join(admin, "gitdir"))
	if err != nil {
		return false
	}
	named := strings.TrimRight(string(b), "\r\n")
	if !filepath.IsAbs(named) {
		named = filepath.Join(admin, named)
	}
	return samePath(filepath.Clean(named), filepath.Join(path, ".git"))
}

// samePath compares two spellings of one path, resolving symlinks in their
// directories when they differ as written, as /var and /private/var do.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(filepath.Dir(a))
	rb, errB := filepath.EvalSymlinks(filepath.Dir(b))
	return errA == nil && errB == nil && filepath.Join(ra, filepath.Base(a)) == filepath.Join(rb, filepath.Base(b))
}

// headIs reports whether the admin directory's HEAD is on the branch.
func headIs(admin, branch string) bool {
	b, err := os.ReadFile(filepath.Join(admin, "HEAD"))
	return err == nil && strings.TrimRight(string(b), "\r\n") == "ref: refs/heads/"+branch
}
