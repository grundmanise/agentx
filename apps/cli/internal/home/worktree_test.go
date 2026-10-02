package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The test ref updater adds worktrees the way git lays them out, with files
// alone, so that the worktree step is exercised without git: an admin
// directory in the repository's worktrees directory holding gitdir, HEAD
// and the lock, and the .git file at the worktree's root naming it.

func (r refs) AddWorktree(gitDir, path, branch string) error {
	if !emptyOrAbsent(path) {
		// What an add stopped half way left is cleared, as the contract
		// says; anything else is refused, as git refuses it.
		if entries, _ := os.ReadDir(path); len(entries) != 1 || entries[0].Name() != ".git" {
			return errors.New(path + " already exists")
		}
		if err := os.Remove(filepath.Join(path, ".git")); err != nil {
			return err
		}
	}
	admin := filepath.Join(gitDir, "worktrees", filepath.Base(path))
	if err := halfAdd(gitDir, path); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(admin, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o644)
}

func (r refs) ResetIndex(path string) error {
	admin, ok := AdminDirOf(path)
	if !ok {
		return errors.New("not a worktree: " + path)
	}
	return os.WriteFile(filepath.Join(admin, "index"), []byte("index\n"), 0o644)
}

// halfAdd is what a worktree add killed part way leaves: the admin
// directory, locked, and the .git file, with no HEAD on the branch yet.
func halfAdd(gitDir, path string) error {
	admin := filepath.Join(gitDir, "worktrees", filepath.Base(path))
	if err := os.MkdirAll(admin, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	for name, content := range map[string]string{"gitdir": filepath.Join(path, ".git"), "locked": "agentx fork"} {
		if err := os.WriteFile(filepath.Join(admin, name), []byte(content+"\n"), 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(path, ".git"), []byte("gitdir: "+admin+"\n"), 0o644)
}

// creation is the shape of a greenfield skill's creation: the fork's branch,
// its worktree, the skill directory published into the worktree from
// content staged in the worktrees directory, and the library symlink to it.
type creation struct {
	install
	worktrees string
}

func newCreation(t *testing.T) (creation, refs) {
	t.Helper()
	in, u := newInstall(t)
	c := creation{install: in, worktrees: filepath.Join(in.dir, "worktrees")}
	if err := os.MkdirAll(c.worktrees, 0o755); err != nil {
		t.Fatal(err)
	}
	return c, u
}

func (c creation) root(name string) string { return filepath.Join(c.worktrees, name) }

func (c creation) creationOf(t *testing.T, name string) *Mutation {
	t.Helper()
	m := NewMutation(c.dir)
	staged := m.Sibling(c.root(name), "staged")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "SKILL.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := Fingerprint(staged)
	if err != nil {
		t.Fatal(err)
	}
	m.Ref(c.gitDir, "refs/heads/skills/"+name, "", "c0ffee-"+name)
	m.Worktree(c.gitDir, c.root(name), "skills/"+name)
	m.Publish(filepath.Join(c.root(name), name), staged, fingerprint)
	m.Link(filepath.Join(c.library, name), filepath.Join(c.root(name), name))
	return m
}

// created reports what of a creation is not in place.
func (c creation) created(u refs, name string) []string {
	var missing []string
	if u[c.gitDir+" refs/heads/skills/"+name] != "c0ffee-"+name {
		missing = append(missing, "the branch")
	}
	if !WorktreeAt(c.root(name), "skills/"+name) {
		missing = append(missing, "the worktree")
	} else if admin, _ := AdminDirOf(c.root(name)); !exists(filepath.Join(admin, "index")) {
		missing = append(missing, "the worktree's index")
	}
	if b, err := os.ReadFile(filepath.Join(c.root(name), name, "SKILL.md")); err != nil || string(b) != "new\n" {
		missing = append(missing, "the skill directory")
	}
	if target, err := os.Readlink(filepath.Join(c.library, name)); err != nil || target != filepath.Join(c.root(name), name) {
		missing = append(missing, "the library symlink")
	}
	if left, _ := Journals(c.dir); len(left) > 0 {
		missing = append(missing, "the journal is still there")
	}
	if hidden, _ := filepath.Glob(filepath.Join(c.worktrees, ".agentx-*")); len(hidden) > 0 {
		missing = append(missing, "the staged content is still there")
	}
	return missing
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// TestCreationRecoversFromEveryBoundary stops the creation of a fork after
// each step, which is where a process killed at a durable boundary leaves
// it, and checks that recovery finishes it, twice over without repeating
// anything.
func TestCreationRecoversFromEveryBoundary(t *testing.T) {
	t.Parallel()
	total := 0
	{
		c, _ := newCreation(t)
		total = c.creationOf(t, "alpha").steps()
	}
	for stop := 0; stop <= total; stop++ {
		t.Run(fmt.Sprintf("after %d steps", stop), func(t *testing.T) {
			t.Parallel()
			c, u := newCreation(t)
			m := c.creationOf(t, "alpha")
			if err := m.stopAfter(stop, u); err != nil {
				t.Fatalf("stopping after %d steps: %v", stop, err)
			}
			for _, pass := range []string{"recovery", "recovery run again"} {
				if err := recoverJournals(c.dir, u); err != nil {
					t.Fatalf("%s after %d steps: %v", pass, stop, err)
				}
				if missing := c.created(u, "alpha"); len(missing) > 0 {
					t.Errorf("after %s from step %d: %s", pass, stop, strings.Join(missing, ", "))
				}
			}
		})
	}
}

// forkRevertOf is the shape of a revert of a fork, created first: its
// branch moved to the new commit, its skill directory, edited since,
// retained in the worktrees directory and filled again with content staged
// there, never inside the worktree, and the worktree's index reset to the
// branch. A revert of the edits alone has the same steps, its ref step
// holding the branch where it is.
func (c creation) forkRevertOf(t *testing.T, u refs, name string) *Mutation {
	t.Helper()
	if err := c.creationOf(t, name).Apply(u); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(c.root(name), name)
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	admin, _ := AdminDirOf(c.root(name))
	if err := os.Remove(filepath.Join(admin, "index")); err != nil {
		t.Fatal(err)
	}
	old, err := State(skill)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMutation(c.dir)
	staged := m.Sibling(c.root(name), "staged")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "SKILL.md"), []byte("reverted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := Fingerprint(staged)
	if err != nil {
		t.Fatal(err)
	}
	m.Ref(c.gitDir, "refs/heads/skills/"+name, "c0ffee-"+name, "revert-"+name)
	if err := m.RemoveInto(skill, old, c.worktrees); err != nil {
		t.Fatal(err)
	}
	m.Publish(skill, staged, fingerprint)
	m.Worktree(c.gitDir, c.root(name), "skills/"+name)
	return m
}

// reverted reports what of a fork's revert is not in place.
func (c creation) reverted(u refs, name string) []string {
	var missing []string
	if u[c.gitDir+" refs/heads/skills/"+name] != "revert-"+name {
		missing = append(missing, "the branch")
	}
	if admin, _ := AdminDirOf(c.root(name)); !WorktreeAt(c.root(name), "skills/"+name) || !exists(filepath.Join(admin, "index")) {
		missing = append(missing, "the worktree's index")
	}
	if b, err := os.ReadFile(filepath.Join(c.root(name), name, "SKILL.md")); err != nil || string(b) != "reverted\n" {
		missing = append(missing, "the skill directory")
	}
	if entries, _ := os.ReadDir(c.root(name)); len(entries) != 2 {
		missing = append(missing, "a worktree holding its .git and the skill directory alone")
	}
	if hidden, _ := filepath.Glob(filepath.Join(c.worktrees, ".agentx-*")); len(hidden) > 0 {
		missing = append(missing, "the staged or retained content is still there")
	}
	if left, _ := Journals(c.dir); len(left) > 0 {
		missing = append(missing, "the journal is still there")
	}
	return missing
}

// TestForkRevertRecoversFromEveryBoundary stops the revert of a fork after
// each step and checks that recovery finishes it, twice over without
// repeating anything, the edited directory it retained dropped at the end.
func TestForkRevertRecoversFromEveryBoundary(t *testing.T) {
	t.Parallel()
	for stop := 0; stop <= 4; stop++ {
		t.Run(fmt.Sprintf("after %d steps", stop), func(t *testing.T) {
			t.Parallel()
			c, u := newCreation(t)
			m := c.forkRevertOf(t, u, "alpha")
			if stop == 0 && m.steps() != 4 {
				t.Fatalf("%d steps, want 4", m.steps())
			}
			if err := m.stopAfter(stop, u); err != nil {
				t.Fatalf("stopping after %d steps: %v", stop, err)
			}
			for _, pass := range []string{"recovery", "recovery run again"} {
				if err := recoverJournals(c.dir, u); err != nil {
					t.Fatalf("%s after %d steps: %v", pass, stop, err)
				}
				if missing := c.reverted(u, "alpha"); len(missing) > 0 {
					t.Errorf("after %s from step %d: %s", pass, stop, strings.Join(missing, ", "))
				}
			}
		})
	}
}

// TestWorktreeStepClearsAHalfMadeWorktree is a creation killed inside git's
// worktree add, which leaves the admin directory and the .git file and no
// HEAD on the branch. Nothing of the user's is there, so recovery adds the
// worktree over it rather than refusing the path for holding something.
func TestWorktreeStepClearsAHalfMadeWorktree(t *testing.T) {
	t.Parallel()
	c, u := newCreation(t)
	m := c.creationOf(t, "alpha")
	if err := m.stopAfter(1, u); err != nil { // the branch is written
		t.Fatal(err)
	}
	if err := halfAdd(c.gitDir, c.root("alpha")); err != nil {
		t.Fatal(err)
	}
	if !HalfMade(c.root("alpha"), "skills/alpha") {
		t.Fatal("the worktree a stopped add leaves does not read as half made")
	}
	if err := recoverJournals(c.dir, u); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if missing := c.created(u, "alpha"); len(missing) > 0 {
		t.Errorf("after recovery: %s", strings.Join(missing, ", "))
	}
}

// TestWorktreeStepRefusesAPathThatHoldsSomethingElse is the worktree step's
// guard: a path that is neither the worktree nor empty holds something the
// mutation did not put there, and recovery keeps both it and the journal.
func TestWorktreeStepRefusesAPathThatHoldsSomethingElse(t *testing.T) {
	t.Parallel()
	c, u := newCreation(t)
	m := c.creationOf(t, "alpha")
	if err := m.stopAfter(1, u); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(c.root("alpha"), "notes.md")
	if err := os.MkdirAll(c.root("alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recoverJournals(c.dir, u); !errors.Is(err, ErrRecovery) {
		t.Fatalf("recovery = %v, want it refused", err)
	}
	if b, err := os.ReadFile(mine); err != nil || string(b) != "mine\n" {
		t.Errorf("the path's content is %q, %v, want it kept", b, err)
	}
	if left, _ := Journals(c.dir); len(left) != 1 {
		t.Errorf("%d journals left, want the refused one kept", len(left))
	}
}

// TestWorktreeStepNeedsGit is a recovery that is handed no git: it refuses
// the step and says why, as it does for a ref step.
func TestWorktreeStepNeedsGit(t *testing.T) {
	t.Parallel()
	c, u := newCreation(t)
	m := c.creationOf(t, "alpha")
	if err := m.stopAfter(1, u); err != nil {
		t.Fatal(err)
	}
	type refsOnly struct{ RefUpdater }
	err := recoverJournals(c.dir, refsOnly{u})
	if !errors.Is(err, ErrRecovery) || !strings.Contains(err.Error(), "needs git to finish") {
		t.Fatalf("recovery = %v, want it refused for want of git", err)
	}
}

// TestApplyAbandonsAJournalWhoseRefMovedFirst is a fork's branch moved by
// git in its worktree after the command read it under the lock: the first
// ref transaction refuses before any path changed, so the journal and what
// it staged are removed and nothing is left for recovery to refuse.
func TestApplyAbandonsAJournalWhoseRefMovedFirst(t *testing.T) {
	t.Parallel()
	c, u := newCreation(t)
	m := c.creationOf(t, "alpha")
	u[c.gitDir+" refs/heads/skills/alpha"] = "committed meanwhile"
	err := m.Apply(u)
	if !errors.Is(err, ErrMovedBeforeApply) {
		t.Fatalf("apply = %v, want it abandoned", err)
	}
	if m.Journaled() {
		t.Error("an abandoned journal reads as journaled")
	}
	if left, _ := Journals(c.dir); len(left) != 0 {
		t.Errorf("%d journals left, want none", len(left))
	}
	if hidden, _ := filepath.Glob(filepath.Join(c.worktrees, ".agentx-*")); len(hidden) != 0 {
		t.Errorf("the staged content is still there: %v", hidden)
	}
	if exists(c.root("alpha")) {
		t.Error("the worktree was added by a journal that was abandoned")
	}
}

// TestRemoveIntoRetainsInTheGivenDirectory keeps a fork's displaced skill
// directory in the worktrees directory, never beside it in the worktree,
// where git would see it as a file of the branch.
func TestRemoveIntoRetainsInTheGivenDirectory(t *testing.T) {
	t.Parallel()
	c, u := newCreation(t)
	skill := filepath.Join(c.root("alpha"), "alpha")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := State(skill)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMutation(c.dir)
	if err := m.RemoveInto(skill, state, c.worktrees); err != nil {
		t.Fatal(err)
	}
	retained := m.j.Steps[0].Retained
	if filepath.Dir(retained) != c.worktrees {
		t.Errorf("retained at %s, want it in %s", retained, c.worktrees)
	}
	if err := m.stopAfter(1, u); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(retained, "SKILL.md")); err != nil || string(b) != "old\n" {
		t.Errorf("the retained directory holds %q, %v", b, err)
	}
	if entries, _ := os.ReadDir(c.root("alpha")); len(entries) != 0 {
		t.Errorf("the worktree root holds %d entries, want none", len(entries))
	}
	if err := recoverJournals(c.dir, u); err != nil {
		t.Fatal(err)
	}
	if exists(retained) {
		t.Error("the retained directory outlived the mutation")
	}
}

// TestWorktreeFilesTellAWorktreeApart is the reading of a worktree's two
// pointer files, absolute and relative, on and off the branch, copied and
// moved; and of what its admin directory says git is in the middle of.
func TestWorktreeFilesTellAWorktreeApart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitDir := filepath.Join(root, "account.git")
	path := filepath.Join(root, "worktrees", "alpha")
	var u refs
	if err := u.AddWorktree(gitDir, path, "skills/alpha"); err != nil {
		t.Fatal(err)
	}
	admin := filepath.Join(gitDir, "worktrees", "alpha")
	check := func(what string, at, half, moved bool) {
		t.Helper()
		if got := WorktreeAt(path, "skills/alpha"); got != at {
			t.Errorf("%s: WorktreeAt = %v, want %v", what, got, at)
		}
		if got := HalfMade(path, "skills/alpha"); got != half {
			t.Errorf("%s: HalfMade = %v, want %v", what, got, half)
		}
		if got := PointersMoved(path); got != moved {
			t.Errorf("%s: PointersMoved = %v, want %v", what, got, moved)
		}
	}
	check("absolute", true, false, false)
	if WorktreeAt(path, "skills/beta") {
		t.Error("a worktree reads as on a branch it is not on")
	}
	if got := Unfinished(path) + IndexLock(path); got != "" {
		t.Errorf("a worktree git is idle in reads as busy: %q", got)
	}
	for _, tc := range []struct{ file, want string }{
		{"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"},
		{"rebase-merge/", "rebase"}, {"rebase-apply/", "rebase"}, {"rebase-apply/applying", "am"},
	} {
		dir, file := filepath.Split(admin + "/" + tc.file)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if file != "" {
			if err := os.WriteFile(filepath.Join(dir, file), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got := Unfinished(path); got != tc.want {
			t.Errorf("with %s: Unfinished = %q, want %q", tc.file, got, tc.want)
		}
		if err := os.RemoveAll(filepath.Join(admin, strings.Split(tc.file, "/")[0])); err != nil {
			t.Fatal(err)
		}
	}
	lock := filepath.Join(admin, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := IndexLock(path); got != lock {
		t.Errorf("IndexLock = %q, want %q", got, lock)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(root, "worktrees", "alpha-copy")
	if err := os.MkdirAll(copied, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copied, ".git"), []byte("gitdir: "+admin+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := HeldBy(copied); !ok || !SamePath(got, path) {
		t.Errorf("HeldBy(a copy) = %q, %v, want %q", got, ok, path)
	}
	if got, ok := HeldBy(path); ok {
		t.Errorf("HeldBy(the worktree itself) = %q, want none", got)
	}
	rel, _ := filepath.Rel(path, admin)
	back, _ := filepath.Rel(admin, filepath.Join(path, ".git"))
	if err := os.WriteFile(filepath.Join(path, ".git"), []byte("gitdir: "+rel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(admin, "gitdir"), []byte(back+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("relative", true, false, false)
	if err := os.WriteFile(filepath.Join(admin, "gitdir"), []byte("/elsewhere/.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("named elsewhere", false, false, true)
	if err := os.RemoveAll(admin); err != nil {
		t.Fatal(err)
	}
	check("admin directory gone", false, true, true)
}
