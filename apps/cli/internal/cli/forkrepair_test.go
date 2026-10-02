package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// forkHarness is a home with a greenfield skill called notes placed in
// Claude Code, and where its worktree, its skill directory and its library
// entry are.
func forkHarness(t *testing.T) (h *harness, root, skillDir, lib string) {
	t.Helper()
	h = newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	h.mustRun("skill", "new", "notes")
	root = filepath.Join(h.agentx, "worktrees", "notes")
	return h, root, filepath.Join(root, "notes"), filepath.Join(h.library, "notes")
}

// forkLinked fails unless the library entry lib leads to the fork's skill
// directory skillDir.
func forkLinked(t *testing.T, lib, skillDir string) {
	t.Helper()
	if real, err := filepath.EvalSymlinks(lib); err != nil || real != skillDir {
		t.Errorf("the library entry leads to %q, %v, want %s", real, err, skillDir)
	}
}

// TestSkillPlaceRepairsAFork puts one fork back each way skill place does,
// in order. A worktree deleted by hand, its library symlink left leading
// nowhere, is named in skill list's warnings, and once in those of any
// other command that reads the lineage, and checked out again from the
// branch, symlink and all. A worktree whose pointers no longer meet, as
// moving agentx home leaves them, is repaired with git. A skill directory
// deleted while a git merge waits in the worktree is named, and skill
// place refuses to lay it out until the merge is over. A worktree whose
// registration was removed by hand, holding an edit, is in the way: skill
// place refuses it, naming --force, which adopts it in place with the edit
// left uncommitted, and skill revert then discards the edit. A library
// directory is not adopted over a skill directory holding a file git
// ignores, which would go with it.
func TestSkillPlaceRepairsAFork(t *testing.T) {
	t.Parallel()
	h, root, skillDir, lib := forkHarness(t)
	claude := filepath.Join(h.home, ".claude", "skills", "notes")
	// The branch holds a .gitignore beside the skill directory, committed
	// with git, which comes back with the worktree and goes on ignoring.
	writeFile(t, filepath.Join(root, ".gitignore"), ".env\n")
	gitIn(t, h, root, "add", ".gitignore")
	gitIn(t, h, root, "commit", "-q", "-m", "Ignore secrets")

	remove(t, root)
	list := h.mustRun("--json", "skill", "list")
	contains(t, "skill list's warnings", strings.Join(warnings(h, list.stderr), "\n"),
		"notes's worktree "+root+" is missing; run 'agentx skill place notes' to check it out again from its branch")
	history := h.mustRun("skill", "history", "notes")
	equal(t, "skill history's warnings", strings.Count(history.stderr, "notes's worktree "+root+" is missing"), 1)
	out := h.mustRun("--json", "skill", "place", "notes")
	if !home.WorktreeAt(root, "skills/notes") {
		t.Fatal("the worktree is not checked out on its branch")
	}
	equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
	writeFile(t, filepath.Join(skillDir, ".env"), "TOKEN=1\n")
	equal(t, "git status with an ignored file", gitIn(t, h, root, "status", "--porcelain"), "")
	remove(t, filepath.Join(skillDir, ".env"))
	forkLinked(t, lib, skillDir)
	linksToLibrary(t, "claude's placement", claude, lib)
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "; checked notes's worktree out again from its branch")

	admin, _ := home.AdminDirOf(root)
	writeFile(t, filepath.Join(admin, "gitdir"), filepath.Join(h.agentx, "elsewhere", ".git")+"\n")
	out = h.mustRun("--json", "skill", "place", "notes")
	if home.PointersMoved(root) {
		t.Error("the worktree's pointers were not repaired")
	}
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "; repaired the pointers of notes's worktree")

	mergeHead := filepath.Join(admin, "MERGE_HEAD")
	writeFile(t, mergeHead, h.ref("refs/heads/skills/notes")+"\n")
	remove(t, skillDir)
	contains(t, "skill list's warnings", strings.Join(warnings(h, h.mustRun("--json", "skill", "list").stderr), "\n"),
		"notes's skill directory "+skillDir+" is missing from its worktree, where a git merge stopped part way")
	out = h.run("--json", "skill", "place", "notes")
	equal(t, "exit", out.exit, 6)
	equal(t, "message", h.one(out.stdout, "error")["message"], "notes's worktree "+root+" is in the middle of a git merge")
	if !lexists(mergeHead) {
		t.Fatal("the merge waiting in the worktree was lost")
	}
	remove(t, mergeHead)
	out = h.mustRun("--json", "skill", "place", "notes")
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "; checked notes's worktree out again from its branch")

	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "my own instructions\n")
	remove(t, admin)
	out = h.run("--json", "skill", "place", "notes")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], root+" is in the way of notes's worktree, so nothing was placed")
	equal(t, "hint", e["hint"], "run 'agentx skill place notes --force' to adopt it: its content becomes uncommitted edits of the fork, which 'agentx skill revert notes' discards")

	ignore := filepath.Join(root, ".gitignore")
	out = h.run("--json", "skill", "place", "notes", "--force")
	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), root+" holds "+ignore+" beside notes's skill directory notes")
	remove(t, ignore)
	out = h.mustRun("--json", "skill", "place", "notes", "--force")
	if !home.WorktreeAt(root, "skills/notes") {
		t.Fatal("the adopted worktree is not checked out on its branch")
	}
	equal(t, "git status in the adopted worktree", gitIn(t, h, root, "status", "--porcelain"), " M notes/SKILL.md\n")
	equal(t, "state", h.one(out.stdout, "library_skill")["state"], stateModified)
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "; adopted "+root+" as notes's worktree; its content is uncommitted edits of the fork")
	cleanAfterPlace(t, h, filepath.Dir(root), h.library)

	h.mustRun("skill", "revert", "notes")
	equal(t, "git status once reverted", gitIn(t, h, root, "status", "--porcelain"), "")
	equal(t, "state once reverted", h.listed("notes")["state"], stateCurrent)

	// A file git ignores is the user's, but a .DS_Store Finder left is not.
	writeFile(t, filepath.Join(skillDir, ".DS_Store"), "finder\n")
	local := filepath.Join(skillDir, ".env")
	writeFile(t, local, "TOKEN=1\n")
	remove(t, lib)
	writeFile(t, mkdirs(t, lib, "SKILL.md"), "instructions edited in the library\n")
	out = h.run("--json", "skill", "place", "notes", "--force")
	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), " holds what git does not record, "+local+", and adopting "+lib+" in its place would delete it")
	if !lexists(local) || !isDir(lib) {
		t.Error("the refused adoption changed the skill directory or the library entry")
	}

	// A fork whose content is no skill, with no SKILL.md, is not placed.
	remove(t, local)
	remove(t, filepath.Join(lib, "SKILL.md"))
	writeFile(t, filepath.Join(lib, "notes.md"), "no instructions\n")
	out = h.run("--json", "skill", "place", "notes", "--force")
	equal(t, "exit", out.exit, 5)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), lib+" holds no SKILL.md, so notes is no skill to place")
	if !isDir(lib) || !lexists(filepath.Join(skillDir, ".DS_Store")) {
		t.Error("the refused place changed the skill directory or the library entry")
	}
}

// TestForkRepairRecoversWhereItWasKilled kills skill place with SIGKILL at
// one boundary of each journal it writes for a fork: the repair of a
// deleted worktree right after git added the worktree again, and the
// adoption of an orphaned worktree once its journal is on disk, which is
// after its first move, since the worktree add that follows is the first
// git it runs. Every other boundary is the journal's, which the home tests
// replay without git. The next command finishes each: the worktree is on
// its branch, the library symlink leads into it, an adopted edit is still
// there, uncommitted, and nothing hidden is left in the worktrees
// directory.
func TestForkRepairRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, args, kinds, status, script string
		orphan                            bool
	}{
		{"a repair, after the worktree was added", "notes", "ref, worktree, publish, remove, link", "", `
case " $* " in
*" worktree add "*)
	%GIT% "$@"
	kill -9 $PPID
	exit 1 ;;
esac
exec %GIT% "$@"
`, false},
		{"an adoption, once the journal is on disk", "notes --force", "move, worktree, move", " M notes/SKILL.md\n", `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec %GIT% "$@"
`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, root, skillDir, lib := forkHarness(t)
			if tc.orphan {
				writeFile(t, filepath.Join(skillDir, "SKILL.md"), "my own instructions\n")
				admin, _ := home.AdminDirOf(root)
				remove(t, admin)
			} else {
				remove(t, root)
			}
			out := killedChild(t, h, "TestPlaceChildProcess", placeChildEnv, tc.args, tc.script)
			_, kinds := journalKinds(t, h)
			equal(t, "the journal's steps", kinds, tc.kinds)
			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed skill place: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			if !home.WorktreeAt(root, "skills/notes") {
				t.Fatal("the worktree is not on its branch after recovery")
			}
			equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), tc.status)
			forkLinked(t, lib, skillDir)
			equal(t, "what is left in the worktrees directory", strings.Join(hiddenEntries(t, filepath.Dir(root)), " "), "")
		})
	}
}
