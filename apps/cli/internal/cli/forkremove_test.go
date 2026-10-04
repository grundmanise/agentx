package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillRemoveOfAFork takes a fork off machine a, which published it,
// while machine b, which installed it, keeps it. A merge pending refuses
// the removal, exit 4, and --remote beside --from is a usage error, both
// changing nothing, as does a git running in the worktree, exit 6, which
// could commit to the branch at any moment. Then the removal takes the
// placements, the library symlink, the worktree with its uncommitted edit
// and ignored file, the branch and the copy mode, and drops the worktree's
// registration, leaving nothing hidden; the account remote keeps its
// branch. --remote then on beta removes it here, but for a directory of
// the user's at its library path, which stays, and a push git cannot make
// leaves its branch there, exit 3, and run again it deletes that branch
// alone, as it does alpha's; asked again, neither holds it, exit 5. b
// still has both forks, worktrees and all, after an update of every skill,
// which reads them not published, and a bare publish leaves them out,
// naming each; its removal of one drops the tracking configuration its
// install from the account wrote, and a publish of the other by name puts
// it back. --remote of a skill of a shared source, or of one with no
// source, is refused, exit 6, before the account remote is fetched.
func TestSkillRemoveOfAFork(t *testing.T) {
	t.Parallel()
	a, b, s, remote := twoHomes(t)
	root := filepath.Join(a.agentx, "worktrees", "alpha")
	lib := filepath.Join(a.library, "alpha")
	claude := filepath.Join(a.home, ".claude", "skills", "alpha")
	a.mustRun("skill", "place", "alpha", "--copy")
	equal(t, "copy_mode", copyModeOf(t, a, "alpha"), "claude-code")
	tip := a.ref(lineage.ForkRef("alpha"))

	pending := filepath.Join(a.agentx, "merges", "alpha")
	a.accountGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason, pending, tip)
	out := a.run("skill", "remove", "alpha")
	equal(t, "a merge pending: exit", out.exit, 4)
	contains(t, "a merge pending: stderr", out.stderr, "alpha has a merge pending, so it cannot be removed until the merge is resolved or given up")
	a.accountGit("worktree", "remove", "-f", "-f", pending)
	out = a.run("skill", "remove", "alpha", "--remote", "--from", "claude-code")
	equal(t, "--remote with --from: exit", out.exit, 1)
	contains(t, "--remote with --from: stderr", out.stderr, "--remote and --from claude-code cannot both be given")
	admin, _ := home.AdminDirOf(root)
	lock := filepath.Join(admin, "index.lock")
	writeFile(t, lock, "")
	out = a.run("skill", "remove", "alpha")
	equal(t, "git running: exit", out.exit, 6)
	contains(t, "git running: stderr", out.stderr, "git is running in alpha's worktree")
	remove(t, lock)
	equal(t, "the branch, refused", a.ref(lineage.ForkRef("alpha")), tip)
	if _, ok := isSymlink(t, lib); !ok {
		t.Fatal("a refused removal took the library symlink")
	}

	writeFile(t, filepath.Join(root, "alpha", "notes.md"), "edited, never committed\n")
	writeFile(t, filepath.Join(root, "alpha", ".DS_Store"), "ignored\n")
	// An update candidate a check pinned goes with its fork, and no other.
	a.accountGit("update-ref", lineage.CandidateRef("alpha"), tip)
	a.accountGit("update-ref", lineage.CandidateRef("beta"), tip)
	out = a.run("skill", "remove", "alpha")
	equal(t, "exit", out.exit, 0)
	equal(t, "alpha's candidate", a.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "beta's candidate", a.ref(lineage.CandidateRef("beta")), tip)
	a.accountGit("update-ref", "-d", lineage.CandidateRef("beta"))
	equal(t, "the text", out.stdout, "✓ removed alpha from the library: 1 placement\n"+
		"  claude-code  copy  "+claude+"\n"+
		"  deleted "+lib+", "+root+" and refs/heads/skills/alpha\n")
	for what, path := range map[string]string{"the library symlink": lib, "the worktree": root, "the placement": claude} {
		nothingAt(t, what, path)
	}
	equal(t, "the branch", a.ref(lineage.ForkRef("alpha")), "")
	equal(t, "copy_mode", copyModeOf(t, a, "alpha"), "")
	if list := a.accountGit("worktree", "list", "--porcelain"); strings.Contains(list, root) {
		t.Errorf("the worktree is still registered:\n%s", list)
	}
	equal(t, "what is left in the worktrees directory", strings.Join(hiddenEntries(t, filepath.Dir(root)), " "), "")
	equal(t, "the remote's alpha", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/alpha"), tip)

	// A push git cannot make leaves the account remote's branch, and the
	// same command, run again, deletes it alone.
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// A directory the user put where beta's library symlink was is theirs,
	// as skill place says of it, and the removal leaves it.
	betaLib := filepath.Join(a.library, "beta")
	if err := os.Remove(betaLib); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(betaLib, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(betaLib, "SKILL.md"), skill("beta", "Mine"))
	writeFile(t, filepath.Join(betaLib, "precious.md"), "mine\n")
	path := a.env["PATH"]
	stubGit(t, a, "#!/bin/sh\ncase \" $* \" in *\" push \"*) echo 'fatal: unable to access the remote' >&2; exit 128 ;; esac\nexec "+real+" \"$@\"\n")
	out = a.run("--json", "skill", "remove", "beta", "--remote")
	a.env["PATH"] = path
	equal(t, "an unreachable remote: exit", out.exit, 3)
	e := a.one(out.stdout, "error")
	equal(t, "its message", e["message"], "beta was removed from this machine, but the account remote still holds skills/beta: git push: fatal: unable to access the remote")
	contains(t, "its hint", e["hint"].(string), "then run 'agentx skill remove beta --remote' again")
	contains(t, "the directory left", out.stderr, betaLib+" is a directory agentx did not place there and was left as it is")
	if _, err := os.Stat(filepath.Join(betaLib, "precious.md")); err != nil {
		t.Errorf("the user's directory at beta's library path: %v", err)
	}
	if err := os.RemoveAll(betaLib); err != nil {
		t.Fatal(err)
	}
	equal(t, "beta's branch", a.ref(lineage.ForkRef("beta")), "")
	for _, name := range []string{"alpha", "beta"} {
		out = a.run("--json", "skill", "remove", name, "--remote")
		equal(t, name+" --remote with no fork here: exit", out.exit, 0)
		equal(t, "its summary", a.one(out.stdout, "result")["summary"], "removed skills/"+name+" from the account remote; this machine has no skill called "+name)
	}
	equal(t, "the remote's branches", remoteGit(t, a, remote, "for-each-ref", "--format=%(refname)"), "")
	equal(t, "a's remote-tracking branches", a.accountGit("for-each-ref", "--format=%(refname)", "refs/remotes/"), "")
	out = a.run("skill", "remove", "beta", "--remote")
	equal(t, "neither holds beta: exit", out.exit, 5)
	contains(t, "neither holds beta: stderr", out.stderr, "neither this machine nor the account remote holds a skill called beta")

	bTips := map[string]string{"alpha": b.ref(lineage.ForkRef("alpha")), "beta": b.ref(lineage.ForkRef("beta"))}
	b.mustRun("skill", "update", "--all")
	for name, want := range bTips {
		equal(t, "b's "+name, b.ref(lineage.ForkRef(name)), want)
		if _, err := os.Stat(filepath.Join(b.forkDir(name, name), "SKILL.md")); err != nil {
			t.Errorf("b's %s: %v", name, err)
		}
	}
	// That update's fetch found both branches gone from the account remote,
	// and nothing records it: b's skills read not published, and a publish
	// of every skill names them rather than put them back.
	for _, ev := range b.eventsOfType(b.mustRun("--json", "skill", "list").stdout, "library_skill") {
		equal(t, "b's "+ev["name"].(string)+": drift", fmt.Sprint(ev["drift"]), "["+driftNotPublished+"]")
	}
	out = b.mustRun("--json", "skill", "publish")
	equal(t, "b's publish: summary", b.one(out.stdout, "result")["summary"], "published no skill; 2 skills are not published")
	var warned []string
	for _, ev := range b.eventsOfType(out.stderr, "log") {
		if ev["level"] == "warn" {
			warned = append(warned, ev["message"].(string))
		}
	}
	equal(t, "b's publish: warnings", strings.Join(warned, "\n"),
		"alpha is not published: run 'agentx skill publish alpha' to publish it\n"+
			"beta is not published: run 'agentx skill publish beta' to publish it")
	equal(t, "the remote's branches after b's publish", remoteGit(t, a, remote, "for-each-ref", "--format=%(refname)"), "")
	// b installed both from the account, which made their branches track
	// the account remote's: a removal takes that tracking with the branch,
	// so that a later fork of the name tracks nothing.
	contains(t, "b's tracking", b.accountGit("config", "--get-regexp", `^branch\.`), "branch.skills/alpha.remote")
	b.mustRun("skill", "remove", "alpha")
	if tracking := b.accountGit("config", "--get-regexp", `^branch\.`); strings.Contains(tracking, "skills/alpha") || !strings.Contains(tracking, "skills/beta") {
		t.Errorf("b's tracking after alpha's removal:\n%s", tracking)
	}
	// Published by name, beta is back on the account remote.
	b.mustRun("skill", "publish", "beta")
	equal(t, "the remote's beta, published again", remoteGit(t, a, remote, "rev-parse", "refs/heads/skills/beta"), bTips["beta"])

	// --remote deletes only from the account remote: a skill of a shared
	// source and one with no source are refused before it is fetched.
	b.mustRun("skill", "add", s.url, "--name", "alpha")
	writeFile(t, mkdirs(t, filepath.Join(b.library, "mine"), "SKILL.md"), skill("mine", "My own"))
	refused := func(name string) string {
		out := b.run("--json", "skill", "remove", name, "--remote")
		equal(t, name+" --remote: exit", out.exit, 6)
		equal(t, name+" --remote: progress", len(b.eventsOfType(out.stdout, "progress")), 0)
		message, _ := b.one(out.stdout, "error")["message"].(string)
		return message
	}
	equal(t, "alpha --remote: message", refused("alpha"), "alpha comes from "+s.url+", and agentx deletes skills only from the account remote")
	contains(t, "mine --remote: message", refused("mine"), "mine has no source")
	equal(t, "b's alpha, kept", b.ref(lineage.ManagedRef("alpha")) != "", true)
}

// TestSkillRemoveOfAForkRecoversWhereItWasKilled kills a fork's removal
// with SIGKILL at two of its durable boundaries: once its journal is on
// disk and its paths are gone, at the read of the branch its deletion goes
// last with, so the placement, the library symlink and the worktree are
// gone and the branch is not; and right after that deletion, before the
// journal was told and before the worktree's registration was dropped. The
// next command finishes either one, and drops the registration, which no
// fork's branch holds any more.
func TestSkillRemoveOfAForkRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script string
		branch       bool // the branch is still there when the run is killed
	}{
		{"once the journal is on disk", `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ] && [ ! -L %LIB% ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec %GIT% "$@"
`, true},
		{"after the branch is deleted", `
case " $* " in
*" update-ref "*)
	%GIT% "$@"
	status=$?
	kill -9 $PPID
	exit $status
	;;
esac
exec %GIT% "$@"
`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, root, _, lib := forkHarness(t)
			tip := h.ref(lineage.ForkRef("notes"))
			out := killedChild(t, h, "TestRemoveChildProcess", removeChildEnv, "notes", strings.ReplaceAll(tc.script, "%LIB%", shellWord(lib)))
			_, kinds := journalKinds(t, h)
			equal(t, "the journal's steps", kinds, "ref, remove, remove, remove, ref")
			nothingAt(t, "the library symlink", lib)
			nothingAt(t, "the worktree", root)
			want := ""
			if tc.branch {
				want = tip
			}
			equal(t, "the branch when the removal was killed", h.ref(lineage.ForkRef("notes")), want)

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed removal: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			equal(t, "the branch", h.ref(lineage.ForkRef("notes")), "")
			if list := h.accountGit("worktree", "list", "--porcelain"); strings.Contains(list, root) {
				t.Errorf("the worktree is still registered:\n%s", list)
			}
			equal(t, "what is left in the worktrees directory", strings.Join(hiddenEntries(t, filepath.Dir(root)), " "), "")
		})
	}
}

// TestRemoteDeleteRefusal is what a deletion the account remote rejected
// says, and what it says to do next: a default branch, which running again
// would only be refused again, needs another default first; a branch moved
// since the fetch is fetched again by running again; anything else is run
// again.
func TestRemoteDeleteRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, done string
		status     gitx.PushStatus
		message    string
		hint       string
	}{
		{"default branch", "pdf was removed from this machine",
			gitx.PushStatus{Flag: '!', Summary: "[remote rejected]", Reason: "refusing to delete the current branch: refs/heads/skills/pdf"},
			"pdf was removed from this machine, but the account remote still holds skills/pdf: the account remote rejected its deletion: refusing to delete the current branch: refs/heads/skills/pdf",
			"make another branch the account remote's default branch on its hosting service, then run 'agentx skill remove pdf --remote' again"},
		{"moved since the fetch", "",
			gitx.PushStatus{Flag: '!', Summary: "[rejected]", Reason: "stale info"},
			"the account remote still holds skills/pdf: the account remote rejected its deletion: stale info",
			"another machine moved it since the fetch; run 'agentx skill remove pdf --remote' again to delete what it holds now"},
		{"a hook", "",
			gitx.PushStatus{Flag: '!', Summary: "[remote rejected]", Reason: "pre-receive hook declined"},
			"the account remote still holds skills/pdf: the account remote rejected its deletion: pre-receive hook declined",
			"run 'agentx skill remove pdf --remote' again"},
		{"no reason", "",
			gitx.PushStatus{Flag: '!', Summary: "[rejected]"},
			"the account remote still holds skills/pdf: the account remote rejected its deletion: [rejected]",
			"run 'agentx skill remove pdf --remote' again"},
	} {
		f := remoteDeleteRefusal("pdf", tc.done, tc.status)
		equal(t, tc.name+": status", f.status, exitSource)
		equal(t, tc.name+": message", f.message, tc.message)
		equal(t, tc.name+": hint", f.hint, tc.hint)
	}
}

// TestUniversalLibraryNamesWhatAForkLoses: the removal the refusal of a
// universal client offers takes a fork's worktree and branch too, which
// its hint says when the fork has a worktree here.
func TestUniversalLibraryNamesWhatAForkLoses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		worktree bool
		hint     string
	}{
		{"not a fork", false, "take pdf off the machine with 'agentx skill remove pdf --from universal', which removes it from claude-code, codex"},
		{"a fork", true, "take pdf off the machine with 'agentx skill remove pdf --from universal', which removes it from claude-code, codex and deletes its worktree, with any unpublished edits, and its branch"},
	} {
		var f *failure
		if !errors.As(universalLibrary([]string{"codex"}, "pdf", []string{"claude-code", "codex"}, tc.worktree), &f) {
			t.Fatalf("%s: not a failure", tc.name)
		}
		equal(t, tc.name+": hint", f.hint, tc.hint)
	}
}
