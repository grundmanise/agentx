package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// placementHarness is a machine with six configurations, one source added
// and nothing installed yet. Claude Code, Cursor, Windsurf and GitHub
// Copilot keep their own skills directories, which gives a command four
// placement paths to put four different things at; Codex and Gemini CLI
// read the library itself, so their occurrence is the library entry.
func placementHarness(t *testing.T) (*harness, *sourceRepo) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".cursor", ".codeium/windsurf", ".copilot", ".codex", ".gemini"}})
	s, _, _ := h.standardSource(true)
	s.executable("skills/alpha/scripts/run.sh") // a source holds modes too
	s.commit("an executable script")
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	return h, s
}

// mutationVersion is the change counter of agentx home, so a test can say
// how many mutations a command made: one command is one journal and so one
// bump, however many paths it touched.
func mutationVersion(t *testing.T, h *harness) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.agentx, "version"))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("version file is not a number: %q", b)
	}
	return n
}

// copyModeOf is the configurations the settings record as holding a copy of
// the skill, joined with commas.
func copyModeOf(t *testing.T, h *harness, name string) string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(h.agentx, "settings.json")); err != nil {
		return "" // no settings file at all records no copy
	}
	modes, ok := readSettingsFile(t, h)["copy_mode"].(map[string]any)
	if !ok {
		return ""
	}
	ids, ok := modes[name].([]any)
	if !ok {
		return ""
	}
	var got []string
	for _, id := range ids {
		got = append(got, id.(string))
	}
	return strings.Join(got, ",")
}

// isSymlink reports whether path is a symlink, and what it points at.
func isSymlink(t *testing.T, path string) (string, bool) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	return target, true
}

// fileBody is the content of a regular file, failing the test when there is
// none: it is how these tests say the user's bytes are still on disk.
func fileBody(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is gone: %v", path, err)
	}
	return string(b)
}

// nothingAt fails when anything is at path, a dangling symlink included:
// it is Lstat where the journal tests' gone is Stat, since a removal that
// left a broken link behind would still have left something.
func nothingAt(t *testing.T, what, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("%s is still there at %s", what, path)
	}
}

// TestSkillRemoveFromOneConfiguration takes one placement away and leaves
// every other trace of the skill: the other placements, the library
// directory and the import branch.
func TestSkillRemoveFromOneConfiguration(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	before := mutationVersion(t, h)

	out := h.run("--json", "skill", "remove", "alpha", "--from", "cursor")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "the cursor placement", filepath.Join(h.home, ".cursor", "skills", "alpha"))
	if _, ok := isSymlink(t, filepath.Join(h.home, ".claude", "skills", "alpha")); !ok {
		t.Error("a placement the command did not name was removed")
	}
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("the library directory was removed by a --from removal: %v", err)
	}
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals left behind", journalCount(t, h), 0)

	// The targeted rescan reports on the configuration the command covered,
	// as it found it: Cursor's own placement is gone, and what it still sees
	// is Claude Code's directory, which Cursor reads too. The event says what
	// is there rather than what the command meant to do.
	ev := h.one(out.stdout, "library_skill")
	equal(t, "kind", ev["kind"], "managed")
	places := placementsOf(t, ev)
	equal(t, "placements", strings.Join(places, ";"), "cursor symlink symlink")
	for _, p := range ev["placements"].([]any) {
		equal(t, "the placement Cursor still sees", p.(map[string]any)["path"], filepath.Join(h.home, ".claude", "skills", "alpha"))
	}
	contains(t, "the result", out.stdout, "removed alpha from cursor: 1 placement")
}

// TestSkillRemoveTakesTheSkillOffTheMachine removes every placement, the
// library directory, the import branch, the candidate ref and the copy-mode
// entries, in one mutation.
func TestSkillRemoveTakesTheSkillOffTheMachine(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--copy").exit, 0)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code,cursor,github-copilot,windsurf")
	// A candidate ref, which a later spec writes and this command deletes.
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	h.accountGit("update-ref", "refs/agentx/candidate/alpha", strings.TrimSpace(head))
	before := mutationVersion(t, h)

	out := h.run("--json", "skill", "remove", "alpha")
	equal(t, "exit", out.exit, 0)
	for _, rel := range []string{".claude/skills/alpha", ".cursor/skills/alpha", ".codeium/windsurf/skills/alpha", ".copilot/skills/alpha"} {
		nothingAt(t, "the placement", filepath.Join(h.home, filepath.FromSlash(rel)))
	}
	nothingAt(t, "the library directory", filepath.Join(h.library, "alpha"))
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("the import branch is still there")
	}
	equal(t, "refs under refs/agentx", h.agentxRefs(), source.Ref(source.ID(s.url)))
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals left behind", journalCount(t, h), 0)
	equal(t, "library_skill events", len(h.eventsOfType(out.stdout, "library_skill")), 0)
	contains(t, "the result", out.stdout, "removed alpha from the library, 6 placements and its import branch")
}

// TestSkillRemoveDeletesOnlyWhatAgentxMade is the rule of the whole
// command, put under pressure: four placement paths holding four different
// things, only two of which agentx made. The two it made go; the two it did
// not stay, with their bytes where the user left them.
func TestSkillRemoveDeletesOnlyWhatAgentxMade(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	// Claude Code gets the symlink an install makes; Cursor gets a copy
	// agentx records; Windsurf and GitHub Copilot get placements of the
	// user's own that agentx must not touch.
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)
	equal(t, "place", h.run("skill", "place", "alpha", "--to", "cursor", "--copy").exit, 0)

	// A real directory holding exactly the library's version, which nothing
	// recorded as a copy: it looks like agentx's work and is not.
	unrecorded := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	copyTree(t, filepath.Join(h.library, "alpha"), unrecorded)
	writeFile(t, filepath.Join(unrecorded, "mine.md"), "the user's own file\n")
	// A symlink of the user's own, to a directory of theirs.
	mine := filepath.Join(h.home, "my-skills", "alpha")
	copyTree(t, filepath.Join(h.library, "alpha"), mine)
	foreign := filepath.Join(h.home, ".copilot", "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mine, foreign); err != nil {
		t.Fatal(err)
	}

	out := h.run("--json", "skill", "remove", "alpha")
	equal(t, "exit", out.exit, 0)

	// What agentx made is gone.
	nothingAt(t, "the symlink placement", filepath.Join(h.home, ".claude", "skills", "alpha"))
	nothingAt(t, "the recorded copy", filepath.Join(h.home, ".cursor", "skills", "alpha"))
	nothingAt(t, "the library directory", filepath.Join(h.library, "alpha"))

	// What the user made is where they left it, bytes and all.
	equal(t, "the unrecorded directory", fileBody(t, filepath.Join(unrecorded, "mine.md")), "the user's own file\n")
	contains(t, "the unrecorded directory", fileBody(t, filepath.Join(unrecorded, "SKILL.md")), "name: alpha")
	target, ok := isSymlink(t, foreign)
	if !ok || target != mine {
		t.Errorf("the user's symlink is %q (symlink %v), want %q", target, ok, mine)
	}
	contains(t, "what the user's symlink points at", fileBody(t, filepath.Join(mine, "SKILL.md")), "name: alpha")

	// And both were named, on stderr and in the summary.
	contains(t, "stderr", out.stderr, unrecorded)
	contains(t, "stderr", out.stderr, foreign)
	contains(t, "stderr", out.stderr, "left as it is")
	contains(t, "the result", out.stdout, "2 placements left in place")
}

// TestSkillRemoveLeavesAFileAndAHandMadeDirectory covers the two kinds of
// thing an install would also refuse: a regular file at a placement path
// and a directory of the user's own content.
func TestSkillRemoveLeavesAFileAndAHandMadeDirectory(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	// Install into a client that reads the library, so no placement of
	// agentx's own is made in the four directories that have one.
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "codex").exit, 0)

	aFile := filepath.Join(h.home, ".claude", "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(aFile), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, aFile, "not a skill at all\n")
	handMade := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(handMade, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(handMade, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")

	out := h.run("--json", "skill", "remove", "alpha")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "the library directory", filepath.Join(h.library, "alpha"))
	equal(t, "the file", fileBody(t, aFile), "not a skill at all\n")
	contains(t, "the hand-made directory", fileBody(t, filepath.Join(handMade, "SKILL.md")), "description: mine")
	contains(t, "stderr", out.stderr, aFile+" is not a placement agentx made")
	contains(t, "stderr", out.stderr, handMade+" is a directory agentx did not place there")
	contains(t, "the result", out.stdout, "2 placements left in place")
}

// TestSkillRemoveRefusesAUniversalLibraryOccurrence: for a client that
// reads the library there is no placement to delete, the library entry is
// the occurrence, and taking it away would take the skill from every other
// client that reads it. The request is refused and nothing is touched.
func TestSkillRemoveRefusesAUniversalLibraryOccurrence(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	before := mutationVersion(t, h)

	out := h.run("skill", "remove", "alpha", "--from", "codex")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "codex reads the library directly")
	contains(t, "the hint", out.stderr, "agentx skill remove alpha")
	contains(t, "the hint", out.stderr, filepath.Join(h.library, "alpha"))

	// Nothing moved: not the library, not one placement, not the counter.
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("a refused removal touched the library: %v", err)
	}
	if _, ok := isSymlink(t, filepath.Join(h.home, ".claude", "skills", "alpha")); !ok {
		t.Error("a refused removal took a placement away")
	}
	equal(t, "mutations", mutationVersion(t, h), before)

	// Naming it among several configurations refuses the whole request, so
	// that a run cannot half-do what it cannot do at all.
	both := h.run("skill", "remove", "alpha", "--from", "cursor", "--from", "gemini-cli")
	equal(t, "exit", both.exit, 6)
	if _, ok := isSymlink(t, filepath.Join(h.home, ".cursor", "skills", "alpha")); !ok {
		t.Error("a refused removal took the other configuration's placement away")
	}
}

// TestSkillRemoveRefusesAFork leaves a fork's branch and library entry
// alone: removing a fork is its own command. Its placements can still be
// taken away one configuration at a time.
func TestSkillRemoveRefusesAFork(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	h.accountGit("update-ref", "refs/heads/skills/alpha", head)

	out := h.run("skill", "remove", "alpha")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "alpha is a fork on this machine")
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("a refused removal took the library directory: %v", err)
	}
	equal(t, "the fork branch", strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/skills/alpha")), head)

	one := h.run("skill", "remove", "alpha", "--from", "cursor")
	equal(t, "exit", one.exit, 0)
	nothingAt(t, "the placement", filepath.Join(h.home, ".cursor", "skills", "alpha"))
	equal(t, "the fork branch", strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/skills/alpha")), head)
}

// TestSkillRemoveDropsTheCopyModeOfWhatItRemoved leaves the copy modes of
// the configurations it did not touch where they are.
func TestSkillRemoveDropsTheCopyModeOfWhatItRemoved(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--copy").exit, 0)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code,cursor,github-copilot,windsurf")

	out := h.run("skill", "remove", "alpha", "--from", "cursor")
	equal(t, "exit", out.exit, 0)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code,github-copilot,windsurf")
	nothingAt(t, "the copy", filepath.Join(h.home, ".cursor", "skills", "alpha"))
	if _, err := os.Stat(filepath.Join(h.home, ".claude", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("another configuration's copy was removed: %v", err)
	}
}

// TestSkillRemoveSaysACopyWasNotTheLibraryVersion: a copy agentx recorded
// is removed as asked whatever it holds, and the run says what went rather
// than letting it go quietly.
//
// What it says is only what agentx can see. Nothing records how the
// directory came to differ from the library's version — the user may have
// edited the copy, replaced it with something of another project, or had a
// directory of their own adopted there that was never a copy agentx wrote —
// so the warning claims no history, and the second case proves it: a
// directory holding somebody else's README is not "a copy you edited".
func TestSkillRemoveSaysACopyWasNotTheLibraryVersion(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what  string
		build func(t *testing.T, place string)
	}{{
		what: "a copy the user edited",
		build: func(t *testing.T, place string) {
			writeFile(t, filepath.Join(place, "notes.md"), "edited by hand\n")
		},
	}, {
		what: "a copy the user replaced with something else",
		build: func(t *testing.T, place string) {
			if err := os.RemoveAll(place); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(place, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(place, "README.md"), "# another project\n")
		},
	}} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy").exit, 0)
			place := filepath.Join(h.home, ".cursor", "skills", "alpha")
			c.build(t, place)

			out := h.run("skill", "remove", "alpha", "--from", "cursor")
			equal(t, "exit", out.exit, 0)
			nothingAt(t, "the copy", place)
			contains(t, "stderr", out.stderr, place+" did not hold the library's version of alpha")
			contains(t, "stderr", out.stderr, "removing the copy took what was there with it")
			if strings.Contains(out.stderr, "edited") {
				t.Errorf("the warning claims a history agentx has no record of:\n%s", out.stderr)
			}
		})
	}
}

// TestSkillRemoveRefusesWhatItCannotFind answers for a skill the library
// does not hold and for a configuration that is not detected.
func TestSkillRemoveRefusesWhatItCannotFind(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)

	none := h.run("skill", "remove", "gamma")
	equal(t, "exit", none.exit, 5)
	contains(t, "stderr", none.stderr, "the library holds no skill called \"gamma\"")

	nowhere := h.run("skill", "remove", "alpha", "--from", "nowhere")
	equal(t, "exit", nowhere.exit, 5)
	contains(t, "stderr", nowhere.stderr, "detected configurations:")

	// A directory in the library that is not a skill is not one of its
	// skills either, and is left where it is.
	notASkill := filepath.Join(h.library, "notes")
	if err := os.MkdirAll(notASkill, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(notASkill, "README.md"), "no skill here\n")
	out := h.run("skill", "remove", "notes")
	equal(t, "exit", out.exit, 5)
	equal(t, "the directory", fileBody(t, filepath.Join(notASkill, "README.md")), "no skill here\n")
}

// TestSkillRemoveHandlesAPlacementThatIsNotThere: a configuration with
// nothing at the placement path is not an error, and the run says it
// removed nothing there.
func TestSkillRemoveHandlesAPlacementThatIsNotThere(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)

	out := h.run("--json", "skill", "remove", "alpha", "--from", "cursor")
	equal(t, "exit", out.exit, 0)
	contains(t, "the result", out.stdout, "removed alpha from cursor: 0 placements")
	if _, ok := isSymlink(t, filepath.Join(h.home, ".claude", "skills", "alpha")); !ok {
		t.Error("a removal from another configuration took this one's placement")
	}
	equal(t, "journals left behind", journalCount(t, h), 0)
}
