package cli

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillRenameIsAForkAndARemoval renames a fork on machine a, which
// published it, while machine b installed it. Every refusal of either step
// comes before anything changes: a name outside the grammar, a name
// another skill has, a skill that is no fork, uncommitted edits and a merge
// pending. The rename is then a fork of the fork under the new name, with
// a fork id of its own and the old branch's commits as its history, placed
// where the old one was with its copy mode, and the old fork's removal;
// the account remote keeps the old branch. Published, the renamed fork is
// a new fork b can install, and b keeps the old one. A rename with
// --remote deletes the old branch from the account remote too.
func TestSkillRenameIsAForkAndARemoval(t *testing.T) {
	t.Parallel()
	a, b, _, remote := twoHomes(t)
	a.mustRun("skill", "place", "alpha", "--copy")
	writeFile(t, mkdirs(t, filepath.Join(a.library, "mine"), "SKILL.md"), skill("mine", "My own"))
	old := a.ref(lineage.ForkRef("alpha"))
	before := a.refLines()

	pending := filepath.Join(a.agentx, "merges", "alpha")
	for _, tc := range []struct {
		name, from, to string
		exit           int
		says           string
	}{
		{"a name outside the grammar", "alpha", "Renamed", 6, "Renamed"},
		{"a name taken", "alpha", "beta", 6, "beta"},
		{"no fork", "mine", "yours", 6, "mine is not a fork, so it cannot be renamed"},
		{"uncommitted edits", "alpha", "renamed", 6, "alpha has uncommitted edits"},
		{"a merge pending", "alpha", "renamed", 4, "alpha has a merge pending"},
	} {
		switch tc.name {
		case "uncommitted edits":
			writeFile(t, filepath.Join(a.forkDir("alpha", "alpha"), "draft.md"), "uncommitted\n")
		case "a merge pending":
			remove(t, filepath.Join(a.forkDir("alpha", "alpha"), "draft.md"))
			a.accountGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason, pending, old)
		}
		out := a.run("skill", "rename", tc.from, tc.to)
		equal(t, tc.name+": exit", out.exit, tc.exit)
		contains(t, tc.name+": stderr", out.stderr, tc.says)
		equal(t, tc.name+": refs", a.refLines(), before)
	}
	a.accountGit("worktree", "remove", "-f", "-f", pending)

	out := a.run("--json", "skill", "rename", "alpha", "renamed")
	equal(t, "exit", out.exit, 0)
	equal(t, "summary", a.one(out.stdout, "result")["summary"],
		"renamed alpha to renamed; removed alpha from the library, 1 placement, its worktree and its branch")
	renamed := a.ref(lineage.ForkRef("renamed"))
	equal(t, "the renamed fork's parent", a.parents(renamed), old)
	if id := a.trailer(renamed, lineage.TrailerForkID); id == "" || id == a.trailer(old, lineage.TrailerForkID) {
		t.Errorf("the renamed fork's id is %q, want a new one", id)
	}
	contains(t, "its SKILL.md", fileBody(t, filepath.Join(a.forkDir("renamed", "alpha"), "SKILL.md")), "name: renamed")
	equal(t, "copy_mode of the renamed fork", copyModeOf(t, a, "renamed"), "claude-code")
	equal(t, "copy_mode of the old one", copyModeOf(t, a, "alpha"), "")
	if _, ok := isSymlink(t, filepath.Join(a.home, ".claude", "skills", "renamed")); ok || !lexists(filepath.Join(a.home, ".claude", "skills", "renamed", "SKILL.md")) {
		t.Error("the renamed fork is not copied where the old one was")
	}
	equal(t, "the old branch", a.ref(lineage.ForkRef("alpha")), "")
	nothingAt(t, "the old library entry", filepath.Join(a.library, "alpha"))
	equal(t, "the remote's old branch", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/alpha"), old)

	a.mustRun("publish", "renamed")
	listed := b.mustRun("--json", "skill", "list", "--remote")
	installable := b.eventsOfType(listed.stdout, "installable_fork")
	if len(installable) != 1 || installable[0]["name"] != "renamed" || installable[0]["fork_id"] != a.trailer(renamed, lineage.TrailerForkID) {
		t.Errorf("b's installable forks = %v, want renamed with its own fork id", installable)
	}
	if b.ref(lineage.ForkRef("alpha")) == "" || !lexists(filepath.Join(b.library, "alpha", "SKILL.md")) {
		t.Error("b lost its alpha")
	}

	a.mustRun("skill", "rename", "beta", "gamma", "--remote")
	equal(t, "the remote's beta", remoteGit(t, a, remote, "for-each-ref", "--format=%(refname)", "refs/heads/skills/beta"), "")
}

// TestSkillRenameNamesTheCommandThatFinishesIt: a removal that fails once
// the fork is made says so and names the command that finishes the rename.
// A git wrapper moves the old fork's branch at the last moment it can, as
// a commit made with git in its worktree would: after the removal read it
// under its lock, at the journal's first transaction, which holds the
// branch to the commit the fork was made from. The removal refuses there,
// before anything of the old fork goes, rather than delete a commit the
// new fork lacks, and leaves no journal for every later command to refuse
// at.
func TestSkillRenameNamesTheCommandThatFinishesIt(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	h, root, skillDir, lib := forkHarness(t)
	tip := h.ref(lineage.ForkRef("notes"))
	moved := h.accountGit("commit-tree", tip+"^{tree}", "-p", tip, "-m", "made with git")
	marker := filepath.Join(t.TempDir(), "moved")
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" for-each-ref --format=%(refname)%00%(objectname) refs/heads/skills/notes ")
	if [ ! -e `+shellWord(marker)+` ]; then
		: > `+shellWord(marker)+`
		`+real+` --git-dir=`+shellWord(gitx.AccountRepoPath(h.agentx))+` update-ref refs/heads/skills/notes `+moved+` || exit 1
	fi ;;
esac
exec `+real+` "$@"
`)
	out := h.run("--json", "skill", "rename", "notes", "jottings")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "jottings was created, but notes could not be removed: notes changed while it was being removed, so nothing was removed")
	equal(t, "hint", e["hint"], "run 'agentx skill remove notes' to finish the rename")
	if made := h.ref(lineage.ForkRef("jottings")); made == "" || h.parents(made) != tip {
		t.Error("the new fork is not there, made from the old one's tip")
	}
	equal(t, "the old branch", h.ref(lineage.ForkRef("notes")), moved)
	equal(t, "journals", journalCount(t, h), 0)
	if _, ok := isSymlink(t, lib); !ok || !lexists(filepath.Join(skillDir, "SKILL.md")) || len(hiddenEntries(t, filepath.Dir(root))) > 0 {
		t.Error("the refused removal took something of the old fork")
	}
}
