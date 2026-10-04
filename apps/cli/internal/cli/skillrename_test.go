package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillRenameIsAForkAndARemoval renames a skill of the account remote
// on machine a, which published it, a version since its creation
// included, while machine b installed it. Every refusal of either step
// comes before anything changes: a name outside the grammar, a name
// another skill has, a skill that is not one of the account remote's and
// a merge pending. The rename is then a fork of the skill under the new name that keeps its
// fork id, with the old branch's commits as its history and the edit a
// held recorded on it, still unpublished, placed where the old one was
// with its copy mode, even once that recorded edit left the copy behind;
// and the old skill's removal. Until it is published, the renamed skill
// compares with the old branch on the account remote, so its diff holds
// the edit and not what was published under the old name. The account
// remote keeps the old branch, which a, holding it under the new name,
// neither lists as installable nor installs with --all. Published, the
// rename deletes the old branch there, once the account remote takes the
// deletion it first refused, which warns and exits 0; b keeps its alpha
// and lists the renamed skill, with the same fork id, as installable, and
// its publish of alpha, which renamed nothing, leaves the renamed skill's
// branch be. An old branch b published to after a renamed it holds
// changes the renamed skill lacks: a's bare publish, which covers the
// renamed skill through that branch, keeps it, warns, and exits 0.
func TestSkillRenameIsAForkAndARemoval(t *testing.T) {
	t.Parallel()
	a, b, _, remote := twoHomes(t)
	id := a.trailer(a.ref(lineage.ForkRef("alpha")), lineage.TrailerForkID)
	writeFile(t, filepath.Join(a.forkDir("alpha", "alpha"), "published.md"), "published since its creation\n")
	a.record("alpha")
	a.mustRun("skill", "publish", "alpha")
	a.mustRun("skill", "place", "alpha", "--copy")
	writeFile(t, mkdirs(t, filepath.Join(a.library, "mine"), "SKILL.md"), skill("mine", "My own"))
	old := a.ref(lineage.ForkRef("alpha"))
	before := a.refLines()

	pending := filepath.Join(a.agentx, "merges", "alpha")
	for _, tc := range []struct {
		name string
		args []string
		exit int
		says string
	}{
		{"a name outside the grammar", []string{"alpha", "Renamed"}, 6, "Renamed"},
		{"a name taken", []string{"alpha", "beta"}, 6, "choose another name with 'agentx skill rename alpha <new>'"},
		{"not the account remote's", []string{"mine", "yours"}, 6, "mine is not a skill of the account remote, so it cannot be renamed"},
		{"a merge pending", []string{"alpha", "renamed"}, 4, "alpha has a merge pending"},
	} {
		if tc.name == "a merge pending" {
			a.accountGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason, pending, old)
		}
		out := a.run(append([]string{"skill", "rename"}, tc.args...)...)
		equal(t, tc.name+": exit", out.exit, tc.exit)
		contains(t, tc.name+": stderr", out.stderr, tc.says)
		equal(t, tc.name+": refs", a.refLines(), before)
	}
	a.accountGit("worktree", "remove", "-f", "-f", pending)
	// An edit the rename records itself, as a commit that leaves the copy
	// behind the library, which a listing no longer counts as a placement;
	// the rename keeps it all the same.
	writeFile(t, filepath.Join(a.forkDir("alpha", "alpha"), "notes.md"), "edited after the copy\n")
	published := old

	out := a.run("--json", "skill", "rename", "alpha", "renamed")
	equal(t, "exit", out.exit, 0)
	// The copy holds alpha's previous commit, which the renamed skill's
	// history keeps, so nothing of the user's is said to be lost.
	excludes(t, "the copy left behind by a commit", out.stdout+out.stderr, "deleted those changes")
	equal(t, "summary", a.one(out.stdout, "result")["summary"],
		"renamed alpha to renamed in 1 configuration")
	renamed := a.ref(lineage.ForkRef("renamed"))
	recorded := a.parents(renamed)
	equal(t, "the recorded commit's parent", a.parents(recorded), published)
	equal(t, "the rename's subject", a.accountGit("log", "-1", "--format=%s", renamed), "Rename alpha to renamed")
	equal(t, "the rename's own fork id", a.trailer(renamed, lineage.TrailerForkID), "")
	equal(t, "the renamed skill's edit", fileBody(t, filepath.Join(a.forkDir("renamed", "alpha"), "notes.md")), "edited after the copy\n")
	listed := a.mustRun("--json", "skill", "list", "--remote")
	shown := 0
	for _, ev := range a.eventsOfType(listed.stdout, "library_skill") {
		if ev["name"] == "renamed" {
			shown++
			equal(t, "the renamed skill's fork id", ev["fork_id"], id)
			equal(t, "the renamed skill's state, its edit and name unpublished", ev["state"], stateModified)
		}
	}
	equal(t, "the renamed skill's rows", shown, 1)
	equal(t, "a's installable skills", len(a.eventsOfType(listed.stdout, "installable_skill")), 0)
	all := a.mustRun("skill", "add", "--all")
	contains(t, "a's add --all", all.stdout, "holds no skill this machine lacks")
	equal(t, "alpha, not installed again", a.ref(lineage.ForkRef("alpha")), "")
	diff := a.mustRun("skill", "diff", "renamed").stdout
	contains(t, "the renamed skill's diff: the edit", diff, "notes.md")
	excludes(t, "the renamed skill's diff: what was published under the old name", diff, "published.md")
	contains(t, "its SKILL.md", fileBody(t, filepath.Join(a.forkDir("renamed", "alpha"), "SKILL.md")), "name: renamed")
	equal(t, "copy_mode of the renamed skill", copyModeOf(t, a, "renamed"), "claude-code")
	equal(t, "copy_mode of the old one", copyModeOf(t, a, "alpha"), "")
	if _, ok := isSymlink(t, filepath.Join(a.home, ".claude", "skills", "renamed")); ok || !lexists(filepath.Join(a.home, ".claude", "skills", "renamed", "SKILL.md")) {
		t.Error("the renamed skill is not copied where the old one was")
	}
	equal(t, "the old branch", a.ref(lineage.ForkRef("alpha")), "")
	nothingAt(t, "the old library entry", filepath.Join(a.library, "alpha"))
	equal(t, "the remote's old branch", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/alpha"), published)

	// The account remote refuses the old branch's deletion: the publish
	// warns and goes on, and the next one deletes it.
	hook := filepath.Join(remote, "hooks", "pre-receive")
	writeShim(t, hook, "#!/bin/sh\nwhile read old new ref; do [ \"$ref\" = refs/heads/skills/alpha ] && exit 1; done\nexit 0\n")
	// -m does not fold a rename, which keeps its commit.
	out = a.run("skill", "publish", "renamed", "-m", "Rename alpha")
	equal(t, "a refused deletion: exit", out.exit, 0)
	contains(t, "-m over a rename", out.stderr, "the edits of renamed were already recorded in commits of their own, which a rename or an update from upstream keeps, so -m was not used")
	contains(t, "its warning", out.stderr, "the account remote still holds skills/alpha: the account remote rejected its deletion")
	contains(t, "its hint", out.stderr, "the next 'agentx skill publish renamed' tries again")
	equal(t, "the remote's alpha, kept", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/alpha"), published)
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	out = a.mustRun("skill", "publish", "renamed")
	contains(t, "the deletion's line", out.stdout, "deleted skills/alpha from the account remote (renamed to renamed)")
	equal(t, "the remote's renamed", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/renamed"), renamed)
	equal(t, "the remote's alpha", remoteGit(t, a, remote, "for-each-ref", "--format=%(refname)", "refs/heads/skills/alpha"), "")
	listed = b.mustRun("--json", "skill", "list", "--remote")
	installable := b.eventsOfType(listed.stdout, "installable_skill")
	if len(installable) != 1 || installable[0]["name"] != "renamed" || installable[0]["fork_id"] != id {
		t.Errorf("b's installable skills = %v, want renamed with alpha's fork id", installable)
	}
	if b.ref(lineage.ForkRef("alpha")) == "" || !lexists(filepath.Join(b.library, "alpha", "SKILL.md")) {
		t.Error("b lost its alpha")
	}
	// b renamed nothing: its publish of alpha leaves renamed's branch be,
	// and puts alpha's back.
	out = b.run("skill", "publish", "alpha")
	equal(t, "b's publish of alpha: exit", out.exit, 0)
	excludes(t, "b's publish of alpha", out.stdout+out.stderr, "renamed")
	equal(t, "the remote's alpha, back", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/alpha"), b.ref(lineage.ForkRef("alpha")))

	// b publishes to beta after a renamed it: the renamed skill lacks that
	// commit, so a's publish keeps beta's branch. A bare publish covers
	// gamma through beta's branch, which holds the same skill.
	renamedOut := a.mustRun("skill", "rename", "beta", "gamma")
	excludes(t, "the fork step's line in a rename", renamedOut.stdout, "stays as it was")
	contains(t, "the rename's line", renamedOut.stdout, "renamed beta to gamma")
	theirs := b.commitFork("beta", "b's commit\n")
	b.mustRun("skill", "publish", "beta")
	out = a.run("skill", "publish")
	equal(t, "a publish over a changed old branch: exit", out.exit, 0)
	excludes(t, "a bare publish of a renamed skill", out.stderr, "gamma is not published")
	contains(t, "its warning", out.stderr, "the account remote's skills/beta holds changes gamma lacks, so it was kept")
	contains(t, "its hint", out.stderr, "agentx skill add --name beta")
	equal(t, "the remote's beta", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/beta"), theirs)
	equal(t, "the remote's gamma", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/gamma"), a.ref(lineage.ForkRef("gamma")))
}

// TestSkillRenameNamesTheCommandThatFinishesIt: a removal that fails once
// the fork is made says so and names the command that finishes the rename,
// and here, since that command would drop a commit the new fork lacks,
// how to keep it first.
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
	contains(t, "hint", e["hint"].(string), "to keep what it holds now, run 'agentx skill remove jottings' and 'agentx skill rename notes jottings' again")
	if made := h.ref(lineage.ForkRef("jottings")); made == "" || h.parents(made) != tip {
		t.Error("the new fork is not there, made from the old one's tip")
	}
	equal(t, "the old branch", h.ref(lineage.ForkRef("notes")), moved)
	equal(t, "journals", journalCount(t, h), 0)
	if _, ok := isSymlink(t, lib); !ok || !lexists(filepath.Join(skillDir, "SKILL.md")) || len(hiddenEntries(t, filepath.Dir(root))) > 0 {
		t.Error("the refused removal took something of the old fork")
	}
}

// TestRenameFinish: the hint of a rename whose removal failed finishes it,
// unless the old skill holds work the new one lacks, which it says how to
// keep before naming the removal that drops it.
func TestRenameFinish(t *testing.T) {
	t.Parallel()
	finish := skillCommand("remove", "notes")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"another failure", refuse(exitAccountRepo, "cannot read", ""),
			"run 'agentx skill remove notes' to finish the rename"},
		{"unpublished edits", uncommittedRefusal("notes", "removed"),
			"to keep the edits, run 'agentx skill remove jottings' and 'agentx skill rename notes jottings' again, which records them; to drop them, run 'agentx skill remove notes'"},
		{"a branch that moved", movedWhileRemoved("notes"),
			"notes's branch moved since jottings was forked from it; to keep what it holds now, run 'agentx skill remove jottings' and 'agentx skill rename notes jottings' again; to drop it, run 'agentx skill remove notes'"},
	} {
		equal(t, tc.name, renameFinish("notes", "jottings", finish, tc.err), tc.want)
	}
}
