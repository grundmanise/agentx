package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillPlaceAddsAPlacementLater puts a library skill into a
// configuration the install left out, and changes nothing else.
func TestSkillPlaceAddsAPlacementLater(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "codex").exit, 0)
	nothingAt(t, "a placement the install did not make", filepath.Join(h.home, ".cursor", "skills", "alpha"))
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	before := mutationVersion(t, h)

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	place := filepath.Join(h.home, ".cursor", "skills", "alpha")
	target, ok := isSymlink(t, place)
	if !ok || target != filepath.Join(h.library, "alpha") {
		t.Errorf("the placement is %q (symlink %v), want a link to the library", target, ok)
	}
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals left behind", journalCount(t, h), 0)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")

	ev := h.one(out.stdout, "library_skill")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "placements", strings.Join(placementsOf(t, ev), ";"), "cursor symlink symlink")
	contains(t, "the result", out.stdout, "placed alpha in 1 configuration")
}

// TestSkillPlaceCopyRecordsTheMode places a copy of the library directory
// and records the configuration in copy_mode, as an install with --copy
// does.
func TestSkillPlaceCopyRecordsTheMode(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "codex").exit, 0)

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor", "--copy")
	equal(t, "exit", out.exit, 0)
	place := filepath.Join(h.home, ".cursor", "skills", "alpha")
	info, err := os.Lstat(place)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the placement is not a copy: %v", err)
	}
	contains(t, "the copy", fileBody(t, filepath.Join(place, "notes.md")), "alpha notes")
	// The executable bit of the source survives the copy, so the copy hashes
	// to the version it was made from.
	script, err := os.Stat(filepath.Join(place, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("the copy is missing the script: %v", err)
	}
	if script.Mode()&0o111 == 0 {
		t.Errorf("the copied script is not executable: %v", script.Mode())
	}
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor")
	equal(t, "placements", strings.Join(placementsOf(t, h.one(out.stdout, "library_skill")), ";"), "cursor copy copy")
	contains(t, "the result", out.stdout, "1 placement as copy")
}

// TestPlacingAgainKeepsARecordedCopy: a configuration copy_mode records for
// the skill keeps its copy when the skill is placed there again without
// --copy, since its client may not follow a symlink, and agentx's own copy
// is not reported as adopted. Otherwise copy_mode would name a copy where a
// symlink stands, and a directory the user later put at that path would be
// one a removal deletes.
func TestPlacingAgainKeepsARecordedCopy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what  string
		again func(t *testing.T, h *harness, s *sourceRepo) outcome
	}{
		{"skill place", func(_ *testing.T, h *harness, _ *sourceRepo) outcome {
			return h.run("--json", "skill", "place", "alpha", "--to", "cursor")
		}},
		{"skill add", func(_ *testing.T, h *harness, s *sourceRepo) outcome {
			return h.run("--json", "skill", "add", s.url, "--skill", "alpha", "--to", "cursor")
		}},
		{"config enable --place-all", func(t *testing.T, h *harness, _ *sourceRepo) outcome {
			equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)
			return h.run("--json", "config", "enable", "cursor", "--place-all")
		}},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy").exit, 0)

			out := c.again(t, h, s)
			equal(t, "exit", out.exit, 0)
			place := filepath.Join(h.home, ".cursor", "skills", "alpha")
			info, err := os.Lstat(place)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("the placement is no longer a copy: %v", err)
			}
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor")
			equal(t, "placements", strings.Join(placementsOf(t, h.one(out.stdout, "library_skill")), ";"), "cursor copy copy")
			if strings.Contains(out.stdout, "placement adopted") {
				t.Errorf("agentx's own copy was reported as adopted:\n%s", out.stdout)
			}
		})
	}
}

// TestSkillPlaceLeavesWhatItDidNotMake refuses the placement path and keeps
// the user's bytes, exactly as an install does: a directory of their own,
// and a symlink of their own that happens to hold the same version.
func TestSkillPlaceLeavesWhatItDidNotMake(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)

	handMade := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(handMade, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(handMade, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")
	mine := filepath.Join(h.home, "my-skills", "alpha")
	copyTree(t, filepath.Join(h.library, "alpha"), mine)
	foreign := filepath.Join(h.home, ".copilot", "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mine, foreign); err != nil {
		t.Fatal(err)
	}

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor", "--to", "github-copilot")
	equal(t, "exit", out.exit, 0)
	contains(t, "the hand-made directory", fileBody(t, filepath.Join(handMade, "SKILL.md")), "description: mine")
	target, ok := isSymlink(t, foreign)
	if !ok || target != mine {
		t.Errorf("the user's symlink is %q (symlink %v), want %q", target, ok, mine)
	}
	contains(t, "stderr", out.stderr, handMade)
	contains(t, "stderr", out.stderr, foreign)
	contains(t, "the result", out.stdout, "2 placements skipped")
}

// TestSkillPlaceIsThePlacementAnInstallMakes proves the reuse: installing
// straight into a configuration and placing into it later leave the same
// thing on disk and the same entry in the settings.
func TestSkillPlaceIsThePlacementAnInstallMakes(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy").exit, 0)

	later := newHarness(t)
	later.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex"}})
	equal(t, "source add", later.run("source", "add", s.url).exit, 0)
	equal(t, "add", later.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)
	equal(t, "place", later.run("skill", "place", "alpha", "--to", "cursor", "--copy").exit, 0)

	installed := filepath.Join(h.home, ".cursor", "skills", "alpha")
	placed := filepath.Join(later.home, ".cursor", "skills", "alpha")
	for _, rel := range []string{"SKILL.md", "notes.md", filepath.Join("scripts", "run.sh")} {
		equal(t, "the file "+rel+" of the placement", fileBody(t, filepath.Join(placed, rel)), fileBody(t, filepath.Join(installed, rel)))
	}
	equal(t, "copy_mode", copyModeOf(t, later, "alpha"), copyModeOf(t, h, "alpha"))
}

// TestSkillPlaceKeepsAPlacementThatIsAlreadyRight leaves a symlink already
// naming the library where it is and reports it.
func TestSkillPlaceKeepsAPlacementThatIsAlreadyRight(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor").exit, 0)

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	target, ok := isSymlink(t, filepath.Join(h.home, ".cursor", "skills", "alpha"))
	if !ok || target != filepath.Join(h.library, "alpha") {
		t.Errorf("the placement is %q (symlink %v)", target, ok)
	}
	if strings.Contains(out.stderr, "left as it is") {
		t.Errorf("a placement that was already right was reported as skipped:\n%s", out.stderr)
	}
	contains(t, "the result", out.stdout, "placed alpha in 1 configuration")
}

// TestSkillPlaceIntoAClientThatReadsTheLibrary makes no second entry: the
// library entry is the placement, and a symlink beside it would make that
// client list the skill twice.
func TestSkillPlaceIntoAClientThatReadsTheLibrary(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)

	out := h.run("--json", "skill", "place", "alpha", "--to", "codex")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "a placement in the client's own directory", filepath.Join(h.home, ".codex", "skills", "alpha"))
	equal(t, "placements", strings.Join(placementsOf(t, h.one(out.stdout, "library_skill")), ";"), "codex library library")
}

// TestSkillPlaceRefusesWhatItCannotFind answers for a missing --to, a skill
// the library does not hold and a configuration that is not detected.
func TestSkillPlaceRefusesWhatItCannotFind(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)

	noTo := h.run("skill", "place", "alpha")
	equal(t, "exit", noTo.exit, 1)
	contains(t, "stderr", noTo.stderr, "skill place needs --to")

	none := h.run("skill", "place", "gamma", "--to", "cursor")
	equal(t, "exit", none.exit, 5)
	contains(t, "stderr", none.stderr, "the library holds no skill called \"gamma\"")

	nowhere := h.run("skill", "place", "alpha", "--to", "nowhere")
	equal(t, "exit", nowhere.exit, 5)
	contains(t, "stderr", nowhere.stderr, "detected configurations:")
}

// TestSkillPlaceQuotesThePathsOfItsRows places a library directory whoever
// made it named across two lines and with an escape sequence in it, where
// Cursor already holds a copy of it, so the placement adopts that copy.
// The name on the line that confirms the placement is sanitised, and the
// paths of its row and of the line naming what it adopted carry the name,
// so they are quoted: every line stays one line, and each path still names
// a directory on disk.
func TestSkillPlaceQuotesThePathsOfItsRows(t *testing.T) {
	t.Parallel()
	h, _ := placementHarness(t)
	const raw = "two\nrows \x1b[31mRED\x1b[0m"
	dir := filepath.Join(h.library, raw)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: mine\ndescription: made here\n---\n\nmine\n")
	copyTree(t, dir, filepath.Join(h.home, ".cursor", "skills", raw))

	out := h.run("skill", "place", raw, "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	const quoted = `two\nrows \033[31mRED\033[0m"`
	placement := `"` + filepath.Join(h.home, ".cursor", "skills") + string(filepath.Separator) + quoted
	library := `"` + h.library + string(filepath.Separator) + quoted
	equal(t, "stdout", out.stdout, "✓ placed two rows [31mRED [0m in 1 configuration\n"+
		"  cursor  symlink  "+placement+" -> "+library+"\n"+
		"  adopted "+placement+"\n"+
		"  always available to universal clients: codex, gemini-cli\n")
}
