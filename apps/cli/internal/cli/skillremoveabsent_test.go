package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// absentHarness installs alpha with a link in Claude Code and a copy in
// Cursor, which copy_mode records, then deletes its library directory the
// way something outside agentx would: the import branch and the two
// placements are what is left of the skill.
func absentHarness(t *testing.T) (h *harness, claude, cursor string) {
	t.Helper()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.mustRun("skill", "remove", "alpha", "--from", "cursor")
	h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
	remove(t, filepath.Join(h.library, "alpha"))
	return h, filepath.Join(h.home, ".claude", "skills", "alpha"), filepath.Join(h.home, ".cursor", "skills", "alpha")
}

// TestSkillRemoveTakesAwayWhatIsLeftOfASkillTheLibraryNoLongerHolds: the
// listing warns about a managed skill whose library directory is gone until
// it is installed again or removed, and the removal always works, whatever
// the source holds by now. It deletes the import branch, the link into the
// library, dangling now, and the copy this machine recorded, with its copy
// mode. --from is refused: with no library entry there is no skill for one
// configuration to keep while another loses it.
func TestSkillRemoveTakesAwayWhatIsLeftOfASkillTheLibraryNoLongerHolds(t *testing.T) {
	t.Parallel()
	h, claude, cursor := absentHarness(t)

	one := h.run("--json", "skill", "remove", "alpha", "--from", "cursor")
	equal(t, "exit of --from", one.exit, 6)
	e := h.one(one.stdout, "error")
	equal(t, "message", e["message"], "the library holds no skill directory for alpha, so it cannot be removed from cursor alone")
	equal(t, "hint", e["hint"], "take what is left of it off the machine with 'agentx skill remove alpha'")
	if _, err := os.Lstat(cursor); err != nil {
		t.Errorf("the refused removal took cursor's copy: %v", err)
	}
	equal(t, "copy_mode after the refusal", copyModeOf(t, h, "alpha"), "cursor")

	out := h.run("--json", "skill", "remove", "alpha", "--from", "universal")
	if out.exit != 0 {
		t.Fatalf("remove: exit %d\n%s", out.exit, out.stderr)
	}
	nothingAt(t, "claude-code's link", claude)
	nothingAt(t, "cursor's copy", cursor)
	equal(t, "the import branch", refValue(t, h, "refs/heads/managed/alpha"), "")
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "removed alpha, which the library no longer held: its import branch and 2 placements")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	equal(t, "journals", journalCount(t, h), 0)
	for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "the listing's warnings", strings.Join(warnings(h, h.mustRun("--json", "skill", "list").stderr), "\n"), "")

	// With the branch gone the name is in neither the library nor the
	// account repo.
	equal(t, "a second removal", h.run("skill", "remove", "alpha").exit, 5)
}

// TestSkillRemoveOfAnAbsentSkillSaysAnEditedCopyWent: with the library
// directory gone, a recorded copy may be the last of the skill on the
// machine and hold the user's changes. It is compared with the base
// version the import branch names, and a copy that differs from it goes
// with the warning a removal gives a copy that differs, naming the base
// version, since there is no library to differ from.
func TestSkillRemoveOfAnAbsentSkillSaysAnEditedCopyWent(t *testing.T) {
	t.Parallel()
	h, claude, cursor := absentHarness(t)
	writeFile(t, filepath.Join(cursor, "SKILL.md"), skill("alpha", "Edited in the copy"))

	out := h.run("--json", "skill", "remove", "alpha")
	if out.exit != 0 {
		t.Fatalf("remove: exit %d\n%s", out.exit, out.stderr)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		"cursor's copy of alpha was different from its base version; removing it deleted those changes ("+cursor+")")
	nothingAt(t, "claude-code's link", claude)
	nothingAt(t, "cursor's copy", cursor)
	equal(t, "the import branch", refValue(t, h, "refs/heads/managed/alpha"), "")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "removed alpha, which the library no longer held: its import branch and 2 placements")
}

// TestSkillRemoveOfAnAbsentSkillWithNoLineageSaysEveryCopyWent: an import
// branch whose commit carries no lineage agentx can read names no base
// version to compare a recorded copy with, so the removal cannot tell a
// copy holding the user's changes from one that does not: a copy it
// deletes, even one holding the files it was placed with, goes with the
// warning, never silently.
func TestSkillRemoveOfAnAbsentSkillWithNoLineageSaysEveryCopyWent(t *testing.T) {
	t.Parallel()
	h, claude, cursor := absentHarness(t)
	h.accountGit("update-ref", "refs/heads/managed/alpha", h.accountGit("commit-tree", "refs/heads/managed/alpha^{tree}", "-m", "no lineage"))

	out := h.run("--json", "skill", "remove", "alpha")
	if out.exit != 0 {
		t.Fatalf("remove: exit %d\n%s", out.exit, out.stderr)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		"cursor's copy of alpha was different from its base version; removing it deleted those changes ("+cursor+")")
	nothingAt(t, "claude-code's link", claude)
	nothingAt(t, "cursor's copy", cursor)
	equal(t, "the import branch", refValue(t, h, "refs/heads/managed/alpha"), "")
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillRemoveOfAnAbsentSkillRefusesWhatChangedUnderTheLock: what a
// removal of a skill the library no longer holds takes away is decided by
// what it read before the lock, and it reads both inputs again under it. A
// git wrapper changes one of them right after the first read of the refs:
// the library comes to hold the skill again, the import branch moves, or a
// fork of the name appears. The removal then refuses before it writes a
// journal, and every placement, the copy mode and whatever the other
// writer wrote stay as they were.
func TestSkillRemoveOfAnAbsentSkillRefusesWhatChangedUnderTheLock(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	moved := "the import branch refs/heads/managed/alpha moved while alpha was being removed, so nothing was removed"
	for _, c := range []struct {
		name, message string
		ref           string // the ref the wrapper writes; empty when it writes the library
		fork          bool   // the ref is a fork's, and the managed branch stays where it was
	}{
		{"the library comes to hold the skill", "the library came to hold alpha while it was being removed, so nothing was removed", "", false},
		{"the managed branch moves", moved, "refs/heads/managed/alpha", false},
		{"a fork appears", moved, "refs/heads/skills/alpha", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, claude, cursor := absentHarness(t)
			tip := refValue(t, h, "refs/heads/managed/alpha")
			lib := filepath.Join(h.library, "alpha")
			var change, written string
			if c.ref == "" {
				// A directory with no SKILL.md is not the skill, so the
				// removal still takes the path of one the library does
				// not hold, until the wrapper writes the SKILL.md.
				if err := os.MkdirAll(lib, 0o755); err != nil {
					t.Fatal(err)
				}
				change = "printf '%s' " + shellWord(skill("alpha", "Back again")) + " > " + shellWord(filepath.Join(lib, "SKILL.md"))
			} else {
				written = tip
				if !c.fork {
					written = h.accountGit("commit-tree", tip+"^{tree}", "-p", tip, "-m", "moved")
				}
				change = real + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx)) + " update-ref " + c.ref + " " + written + " || exit 1"
			}
			marker := filepath.Join(t.TempDir(), "changed")
			stubGit(t, h, `#!/bin/sh
case " $* " in
*" for-each-ref "*)
	`+real+` "$@"
	status=$?
	if [ ! -e `+shellWord(marker)+` ]; then
		: > `+shellWord(marker)+`
		`+change+`
	fi
	exit $status
	;;
esac
exec `+real+` "$@"
`)
			out := h.run("--json", "skill", "remove", "alpha")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], c.message)
			contains(t, "hint", e["hint"].(string), "agentx skill remove alpha")
			equal(t, "journals", journalCount(t, h), 0)
			if _, ok := isSymlink(t, claude); !ok {
				t.Errorf("claude-code's link is gone")
			}
			if info, err := os.Lstat(cursor); err != nil || !info.IsDir() {
				t.Errorf("cursor's copy is gone: %v", err)
			}
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor")
			if c.ref == "" {
				equal(t, "the SKILL.md written meanwhile", fileBody(t, filepath.Join(lib, "SKILL.md")), skill("alpha", "Back again"))
				equal(t, "the import branch", refValue(t, h, "refs/heads/managed/alpha"), tip)
			} else {
				equal(t, "the ref written meanwhile", refValue(t, h, c.ref), written)
			}
			if c.fork {
				equal(t, "the import branch", refValue(t, h, "refs/heads/managed/alpha"), tip)
			}
			for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
		})
	}
}

// TestSkillRemoveLeavesADirectoryThatLostItsSKILLmd: a library directory
// without its SKILL.md is not the skill, and may hold what the user moved
// there, so the removal deletes the branch and the links and leaves the
// directory byte for byte, saying so. The listing then warns no more.
func TestSkillRemoveLeavesADirectoryThatLostItsSKILLmd(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	lib := filepath.Join(h.library, "alpha")
	remove(t, filepath.Join(lib, "SKILL.md"))
	left := libraryTree(t, lib)

	out := h.run("skill", "remove", "alpha")
	if out.exit != 0 {
		t.Fatalf("remove: exit %d\n%s", out.exit, out.stderr)
	}
	contains(t, "the text", out.stdout, "✓ removed alpha, which the library no longer held: 2 placements\n")
	contains(t, "the text", out.stdout, "\n  deleted refs/heads/managed/alpha\n")
	equal(t, "the warning", out.stderr, "warning: "+lib+" holds no skill and was left as it is; move it aside before installing alpha again\n")
	sameTree(t, "the directory left", libraryTree(t, lib), left)
	nothingAt(t, "claude-code's link", filepath.Join(h.home, ".claude", "skills", "alpha"))
	nothingAt(t, "cursor's link", filepath.Join(h.home, ".cursor", "skills", "alpha"))
	equal(t, "the import branch", refValue(t, h, "refs/heads/managed/alpha"), "")
	equal(t, "the listing's warnings", strings.Join(warnings(h, h.mustRun("--json", "skill", "list").stderr), "\n"), "")
}

// removeChildEnv marks the process TestSkillRemoveOfAnAbsentSkillRecovers
// starts, which removes the skill it names against the parent's temporary
// home and is killed in the middle of it.
const removeChildEnv = "AGENTX_TEST_REMOVE_CHILD"

// TestRemoveChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestRemoveChildProcess(t *testing.T) {
	name := os.Getenv(removeChildEnv)
	if name == "" {
		t.Skip("not the remove child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"skill", "remove", name}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestSkillRemoveOfAnAbsentSkillRecovers kills the removal with SIGKILL at
// its two durable boundaries: once its journal is on disk, at the first git
// it runs after writing it, which is the read of the import branch its
// deletion goes last with, so every placement is gone and the branch is
// not; and right after that deletion, before the journal was told. The
// next command recovers either one, and the removal is then whole.
func TestSkillRemoveOfAnAbsentSkillRecovers(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, script string
		branch       bool // the branch is still there when the run is killed
	}{
		{"before the branch is deleted", `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ]; then
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
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, claude, cursor := absentHarness(t)
			branch := refValue(t, h, "refs/heads/managed/alpha")

			path := h.env["PATH"]
			script := strings.NewReplacer("%MUTATIONS%", shellWord(filepath.Join(h.agentx, "mutations")), "%GIT%", real).Replace(c.script)
			stubGit(t, h, "#!/bin/sh"+script)
			child := exec.Command(os.Args[0], "-test.run=^TestRemoveChildProcess$", "-test.v")
			child.Env = append(os.Environ(), removeChildEnv+"=alpha")
			for k, v := range h.env {
				child.Env = append(child.Env, k+"="+v)
			}
			out, err := child.CombinedOutput()
			h.env["PATH"] = path
			if err == nil {
				t.Fatalf("the removal was not killed:\n%s", out)
			}
			equal(t, "journals when the removal was killed", journalCount(t, h), 1)
			nothingAt(t, "claude-code's link", claude)
			nothingAt(t, "cursor's copy", cursor)
			if c.branch {
				equal(t, "the import branch when the removal was killed", refValue(t, h, "refs/heads/managed/alpha"), branch)
			} else {
				equal(t, "the import branch when the removal was killed", refValue(t, h, "refs/heads/managed/alpha"), "")
			}

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed removal: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			equal(t, "the import branch", refValue(t, h, "refs/heads/managed/alpha"), "")
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")
			for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "the listing's warnings", strings.Join(warnings(h, h.mustRun("--json", "skill", "list").stderr), "\n"), "")
		})
	}
}
