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
	if !ok || !NamesBack(admin, path) {
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
	return !NamesBack(admin, path)
}

// HeldBy is the worktree that holds the registration the .git file at
// path's root names, when that is another worktree that is still there:
// the admin directory names another path, whose own .git file names the
// same admin directory back. That is what copying a worktree leaves, not
// moving it, and repairing the copy's pointers would take the registration
// from the worktree it belongs to. It is false for a worktree whose
// pointers meet, and for one whose registration is gone or names a path
// that is not a worktree of it, as moving agentx home leaves it.
func HeldBy(path string) (string, bool) {
	admin, ok := AdminDirOf(path)
	if !ok {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(admin, "gitdir"))
	if err != nil {
		return "", false
	}
	named := strings.TrimRight(string(b), "\r\n")
	if !filepath.IsAbs(named) {
		named = filepath.Join(admin, named)
	}
	if named == "" || SamePath(named, filepath.Join(path, ".git")) {
		return "", false
	}
	other := filepath.Dir(filepath.Clean(named))
	if back, ok := AdminDirOf(other); !ok || !SamePath(back, admin) {
		return "", false
	}
	return other, true
}

// gitStops are the files git keeps in a worktree's admin directory while a
// command it stopped part way waits for the user, each with the command
// that finishes or aborts it, in the order they are asked about.
var gitStops = []struct{ file, command string }{
	{"MERGE_HEAD", "merge"},
	{"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"},
	{"rebase-merge", "rebase"},
	{"rebase-apply/applying", "am"},
	{"rebase-apply", "rebase"},
}

// Unfinished is the git command that stopped part way in the worktree at
// path and waits for the user to finish or abort it: merge, cherry-pick,
// revert, rebase or am, as the files in the worktree's admin directory
// say, and "" when none did. Its index may hold conflicts, and resetting
// it would lose the command's state.
func Unfinished(path string) string {
	admin, ok := AdminDirOf(path)
	if !ok {
		return ""
	}
	for _, s := range gitStops {
		if _, err := os.Lstat(filepath.Join(admin, filepath.FromSlash(s.file))); err == nil {
			return s.command
		}
	}
	return ""
}

// IndexLock is the lock file of the index of the worktree at path while a
// git holds it, such as a git commit waiting for its message in an
// editor, and "" when none does.
func IndexLock(path string) string {
	admin, ok := AdminDirOf(path)
	if !ok {
		return ""
	}
	lock := filepath.Join(admin, "index.lock")
	if _, err := os.Lstat(lock); err != nil {
		return ""
	}
	return lock
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

// NamesBack reports whether the admin directory's gitdir file names the
// .git file at path's root, which is how a registration names the worktree
// it is for.
func NamesBack(admin, path string) bool {
	b, err := os.ReadFile(filepath.Join(admin, "gitdir"))
	if err != nil {
		return false
	}
	named := strings.TrimRight(string(b), "\r\n")
	if !filepath.IsAbs(named) {
		named = filepath.Join(admin, named)
	}
	return SamePath(named, filepath.Join(path, ".git"))
}

// SamePath compares two spellings of one path, resolving symlinks in their
// directories when they differ as written, as /var and /private/var do.
// The path need not exist, nor need its directory: a worktree removed by
// hand is still named by its registration, under a directory that is gone
// too, so each is resolved as far as it exists.
func SamePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || resolved(a) == resolved(b)
}

// resolved is path with the symlinks of its longest existing ancestor
// resolved and the rest joined on as written.
func resolved(path string) string {
	rest := ""
	for dir := path; ; dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(r, rest)
		}
		if dir == filepath.Dir(dir) {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// headIs reports whether the admin directory's HEAD is on the branch.
func headIs(admin, branch string) bool {
	b, err := os.ReadFile(filepath.Join(admin, "HEAD"))
	return err == nil && strings.TrimRight(string(b), "\r\n") == "ref: refs/heads/"+branch
}

// Registered reports whether the repository at gitDir holds an admin
// directory whose gitdir names the worktree at path, read from the files
// of its admin directories. A registration outlives the worktree's
// directory: a locked worktree deleted by hand keeps it, which is how a
// worktree that was there is told from one never made.
func Registered(gitDir, path string) bool {
	admins := filepath.Join(gitDir, "worktrees")
	entries, err := os.ReadDir(admins)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() && NamesBack(filepath.Join(admins, e.Name()), path) {
			return true
		}
	}
	return false
}

// RegisteredIn reports whether the worktree at path is a worktree the
// repository at gitDir registers: its .git file names an admin directory
// of that repository, and the admin directory names it back.
func RegisteredIn(gitDir, path string) bool {
	admin, ok := AdminDirOf(path)
	return ok && NamesBack(admin, path) && SamePath(filepath.Dir(admin), filepath.Join(gitDir, "worktrees"))
}

// StaleForkRegistrations are the admin directories of the repository at
// gitDir that a removed fork left: each is locked with reason, its
// worktree's directory is gone, and the branch its HEAD names no longer
// exists, as a loose ref or in packed-refs. A removal drops the
// registration with git once its journal is applied; one killed before
// that leaves it, and this is how what is left is found again, from files
// alone. A registration whose branch exists is a fork whose worktree went
// missing, which skill place puts back, and is never one of these; nor is
// one locked for any other reason, a pending merge's say.
func StaleForkRegistrations(gitDir, reason string) []string {
	admins := filepath.Join(gitDir, "worktrees")
	entries, err := os.ReadDir(admins)
	if err != nil {
		return nil
	}
	var packed []byte // read once, and only when a candidate needs it
	var stale []string
	for _, e := range entries {
		admin := filepath.Join(admins, e.Name())
		locked, err := os.ReadFile(filepath.Join(admin, "locked"))
		if !e.IsDir() || err != nil || strings.TrimRight(string(locked), "\r\n") != reason {
			continue
		}
		b, err := os.ReadFile(filepath.Join(admin, "gitdir"))
		if err != nil {
			continue
		}
		named := strings.TrimRight(string(b), "\r\n")
		if !filepath.IsAbs(named) {
			named = filepath.Join(admin, named)
		}
		if _, err := os.Lstat(filepath.Dir(named)); !os.IsNotExist(err) {
			continue // the worktree's directory is there, or cannot be told
		}
		head, err := os.ReadFile(filepath.Join(admin, "HEAD"))
		ref, ok := strings.CutPrefix(strings.TrimRight(string(head), "\r\n"), "ref: ")
		if err != nil || !ok || !strings.HasPrefix(ref, "refs/heads/") {
			continue
		}
		if _, err := os.Lstat(filepath.Join(gitDir, filepath.FromSlash(ref))); err == nil {
			continue
		}
		if packed == nil {
			if packed, err = os.ReadFile(filepath.Join(gitDir, "packed-refs")); err != nil && !os.IsNotExist(err) {
				continue // cannot tell whether the branch is there
			}
			if packed == nil {
				packed = []byte{}
			}
		}
		if packedHolds(packed, ref) {
			continue
		}
		stale = append(stale, admin)
	}
	return stale
}

// packedHolds reports whether the packed-refs file holding packed has a
// line for ref: "<object id> <ref>".
func packedHolds(packed []byte, ref string) bool {
	for _, line := range strings.Split(string(packed), "\n") {
		if _, name, ok := strings.Cut(strings.TrimRight(line, "\r"), " "); ok && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "^") && name == ref {
			return true
		}
	}
	return false
}
