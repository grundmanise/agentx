package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// repairHarness is installHarness with alpha installed everywhere: a
// symlink from Claude Code's skills directory and from Cursor's, and the
// library entry itself for Codex and Gemini CLI.
func repairHarness(t *testing.T) (h *harness, lib, claude, cursor string) {
	t.Helper()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	return h, filepath.Join(h.library, "alpha"), filepath.Join(h.home, ".claude", "skills", "alpha"), filepath.Join(h.home, ".cursor", "skills", "alpha")
}

// displace replaces the placement at place with a real directory holding a
// copy of the library directory lib, which is what an installer that
// copies over links leaves, and with edited it edits that copy too.
func displace(t *testing.T, lib, place string, edited bool) {
	t.Helper()
	remove(t, place)
	copyTree(t, lib, place)
	if edited {
		writeFile(t, filepath.Join(place, "notes.md"), "alpha notes, edited in the displaced directory\n")
		writeFile(t, filepath.Join(place, "mine.md"), "a file of my own\n")
	}
}

// linksToLibrary fails unless the placement at place is a symlink naming the
// library directory lib.
func linksToLibrary(t *testing.T, what, place, lib string) {
	t.Helper()
	target, ok := isSymlink(t, place)
	if !ok {
		t.Fatalf("%s: %s is not a symlink", what, place)
	}
	equal(t, what, target, lib)
}

// nothingWasRepaired is the answer of a repair of a skill whose placements
// drift finds nothing wrong with.
func nothingWasRepaired(name string) string {
	return name + " has no missing or displaced placement; nothing was repaired\n"
}

// cleanAfterRepair holds a repair to what it leaves behind: no journal and
// nothing staged or retained beside any of dirs.
func cleanAfterRepair(t *testing.T, h *harness, dirs ...string) {
	t.Helper()
	equal(t, "journals", journalCount(t, h), 0)
	for _, dir := range dirs {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
}

// TestSkillRepairPutsBackMissingPlacements deletes two placements by hand:
// Claude Code's symlink and the copy copy_mode records for Copilot. The
// repair makes both again as they were, a symlink and a copy of the
// library, leaves the settings as they are and every other placement
// alone, and reports on every placement of the skill. A second repair finds
// nothing to do.
func TestSkillRepairPutsBackMissingPlacements(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--to", "cursor", "--to", "windsurf")
	h.mustRun("skill", "place", "alpha", "--to", "github-copilot", "--copy")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	copilot := filepath.Join(h.home, ".copilot", "skills", "alpha")
	settings := fileBody(t, filepath.Join(h.agentx, "settings.json"))
	before, err := os.Lstat(cursor)
	if err != nil {
		t.Fatal(err)
	}
	remove(t, claude)
	remove(t, copilot)
	equal(t, "drift before the repair", drift(h.listed("alpha")), "missing")
	version := mutationVersion(t, h)

	out := h.mustRun("--json", "skill", "repair", "alpha")
	linksToLibrary(t, "claude's placement", claude, lib)
	if _, ok := isSymlink(t, copilot); ok {
		t.Fatalf("copilot's placement is a symlink, want the copy copy_mode records")
	}
	sameTree(t, "copilot's copy", libraryTree(t, copilot), libraryTree(t, lib))
	if !executable(t, filepath.Join(copilot, "scripts", "run.sh")) {
		t.Error("scripts/run.sh of the copy is not executable")
	}
	after, err := os.Lstat(cursor)
	if err != nil || !os.SameFile(before, after) {
		t.Errorf("cursor's placement was touched although nothing was wrong with it: %v", err)
	}
	equal(t, "the settings", fileBody(t, filepath.Join(h.agentx, "settings.json")), settings)
	ev := h.one(out.stdout, "library_skill")
	equal(t, "drift", drift(ev), "")
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "placements", strings.Join(placementsOf(t, ev), ";"),
		"claude-code symlink symlink;codex library library;cursor symlink symlink;cursor symlink symlink;gemini-cli library library;github-copilot copy copy;windsurf symlink symlink")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "repaired alpha in 2 configurations")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	equal(t, "one mutation", mutationVersion(t, h), version+1)
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(copilot))

	equal(t, "drift after the repair", drift(h.listed("alpha")), "")
	equal(t, "a second repair", h.mustRun("skill", "repair", "alpha").stdout, nothingWasRepaired("alpha"))
	equal(t, "no second mutation", mutationVersion(t, h), version+1)
}

// TestSkillRepairRelinksADirectoryHoldingTheLibrarysContent: a directory
// that replaced a symlink and holds exactly what the library holds loses
// nothing to the symlink, so no flag is needed. The text names what was
// done in each configuration: Claude Code's directory relinked, Cursor's
// missing symlink placed.
func TestSkillRepairRelinksADirectoryHoldingTheLibrarysContent(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := repairHarness(t)
	displace(t, lib, claude, false)
	remove(t, cursor)
	equal(t, "drift before the repair", drift(h.listed("alpha")), "displaced,missing")

	out := h.mustRun("skill", "repair", "alpha")
	equal(t, "the text", out.stdout, "✓ repaired alpha in 2 configurations\n"+
		"  claude-code  relinked  symlink  "+claude+" -> "+lib+"\n"+
		"  cursor       placed    symlink  "+cursor+" -> "+lib+"\n")
	equal(t, "stderr", out.stderr, "")
	linksToLibrary(t, "claude's placement", claude, lib)
	linksToLibrary(t, "cursor's placement", cursor, lib)
	ev := h.listed("alpha")
	equal(t, "drift after the repair", drift(ev), "")
	equal(t, "state after the repair", ev["state"], stateCurrent)
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
	equal(t, "a second repair", h.mustRun("skill", "repair", "alpha").stdout, nothingWasRepaired("alpha"))
}

// TestSkillRepairRefusesADirectoryThatDiffers: a displaced directory whose
// content is not the library's is one of two versions of the skill, and a
// repair without a flag would discard one of them. It refuses, names both
// flags and changes nothing at all, the missing placement it could have
// made included. A file made executable is a difference too, as it is to
// the state of the skill.
func TestSkillRepairRefusesADirectoryThatDiffers(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		edit func(t *testing.T, place string)
	}{
		{"an edited file", func(t *testing.T, place string) {
			writeFile(t, filepath.Join(place, "notes.md"), "alpha notes, edited in the displaced directory\n")
		}},
		{"a file made executable", func(t *testing.T, place string) { chmod(t, filepath.Join(place, "notes.md"), 0o755) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := repairHarness(t)
			displace(t, lib, claude, false)
			c.edit(t, claude)
			remove(t, cursor)
			held := libraryTree(t, claude)

			out := h.run("--json", "skill", "repair", "alpha")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], claude+" is a directory whose content differs from the library's alpha, so nothing was repaired")
			equal(t, "hint", e["hint"], "to keep the library's content and discard it, run 'agentx skill repair alpha --keep-library';"+
				" to make its content the library's, run 'agentx skill repair alpha --keep-placement'")
			if _, ok := isSymlink(t, claude); ok {
				t.Fatal("claude's directory was replaced")
			}
			sameTree(t, "claude's directory", libraryTree(t, claude), held)
			nothingAt(t, "cursor's placement", cursor)
			equal(t, "drift", drift(h.listed("alpha")), "displaced,missing")
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
		})
	}

	t.Run("two of them", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, cursor := repairHarness(t)
		displace(t, lib, claude, true)
		displace(t, lib, cursor, true)
		out := h.run("--json", "skill", "repair", "alpha")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], claude+", "+cursor+" are directories whose content differs from the library's alpha, so nothing was repaired")
		equal(t, "hint", e["hint"], "to keep the library's content and discard them, run 'agentx skill repair alpha --keep-library';"+
			" to make their content the library's, run 'agentx skill repair alpha --keep-placement'")
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
	})
}

// TestSkillRepairComparesWhatGitCannotRecordByteForByte: a library skill
// holding a repository of its own and a displaced directory copied from it
// hold the same content, though no tree records the repository. The two
// are compared byte for byte instead, so the directory is relinked without
// a flag, as any directory holding the library's content is. A directory
// whose repository differs is a directory that differs, and the repair
// refuses.
func TestSkillRepairComparesWhatGitCannotRecordByteForByte(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		same bool
	}{
		{"the same repository", true},
		{"a repository that differs", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, _ := repairHarness(t)
			writeFile(t, mkdirs(t, filepath.Join(lib, "sub", ".git"), "HEAD"), "ref: refs/heads/main\n")
			displace(t, lib, claude, false)
			if !c.same {
				writeFile(t, filepath.Join(claude, "sub", ".git", "HEAD"), "ref: refs/heads/other\n")
			}
			held := libraryTree(t, claude)

			out := h.run("--json", "skill", "repair", "alpha")
			if c.same {
				equal(t, "exit", out.exit, 0)
				linksToLibrary(t, "claude's placement", claude, lib)
				equal(t, "summary", h.one(out.stdout, "result")["summary"], "repaired alpha in 1 configuration")
				equal(t, "drift after the repair", drift(h.listed("alpha")), "")
			} else {
				equal(t, "exit", out.exit, 6)
				equal(t, "message", h.one(out.stdout, "error")["message"],
					claude+" is a directory whose content differs from the library's alpha, so nothing was repaired")
				if _, ok := isSymlink(t, claude); ok {
					t.Fatal("claude's directory was replaced")
				}
				sameTree(t, "claude's directory", libraryTree(t, claude), held)
			}
			equal(t, "the library's repository", fileBody(t, filepath.Join(lib, "sub", ".git", "HEAD")), "ref: refs/heads/main\n")
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// TestSkillRepairKeepLibraryDiscardsTheDirectory: --keep-library is the
// explicit choice to discard a displaced directory that differs. The
// symlink replaces it, the library is untouched, the skill stays current,
// and the run names what it discarded.
func TestSkillRepairKeepLibraryDiscardsTheDirectory(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := repairHarness(t)
	base := libraryTree(t, lib)
	displace(t, lib, claude, true)

	out := h.mustRun("--json", "skill", "repair", "alpha", "--keep-library")
	linksToLibrary(t, "claude's placement", claude, lib)
	sameTree(t, "the library directory", libraryTree(t, lib), base)
	linksToLibrary(t, "cursor's placement", cursor, lib)
	ev := h.one(out.stdout, "library_skill")
	equal(t, "drift", drift(ev), "")
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "repaired alpha in 1 configuration; discarded what "+claude+" held")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude))

	displace(t, lib, claude, true)
	text := h.mustRun("skill", "repair", "alpha", "--keep-library").stdout
	equal(t, "the text", text, "✓ repaired alpha in 1 configuration\n"+
		"  claude-code  relinked  symlink  "+claude+" -> "+lib+"\n"+
		"  discarded what "+claude+" held\n")
	equal(t, "a second repair", h.mustRun("skill", "repair", "alpha", "--keep-library").stdout, nothingWasRepaired("alpha"))
	equal(t, "drift after a second repair", drift(h.listed("alpha")), "")
}

// TestSkillRepairKeepPlacementMakesItTheLibrary: --keep-placement is the
// explicit choice to keep a displaced directory that differs. Its content
// becomes the library's, so the skill is modified against its base
// version, and the symlink replaces it. Every other placement follows the
// library as a revert makes it follow: a copy that held the library's
// content is refreshed, a copy edited where it is is kept and named, and a
// missing copy is made from the content kept.
func TestSkillRepairKeepPlacementMakesItTheLibrary(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	h.mustRun("skill", "place", "alpha", "--to", "cursor", "--to", "windsurf", "--to", "github-copilot", "--copy")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	copilot := filepath.Join(h.home, ".copilot", "skills", "alpha")
	displace(t, lib, claude, true)
	kept := libraryTree(t, claude)
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	remove(t, copilot)
	equal(t, "drift before the repair", drift(h.listed("alpha")), "displaced,missing")

	out := h.mustRun("--json", "skill", "repair", "alpha", "--keep-placement")
	sameTree(t, "the library directory", libraryTree(t, lib), kept)
	linksToLibrary(t, "claude's placement", claude, lib)
	sameTree(t, "cursor's edited copy", libraryTree(t, cursor), edited)
	sameTree(t, "windsurf's refreshed copy", libraryTree(t, windsurf), kept)
	sameTree(t, "copilot's copy", libraryTree(t, copilot), kept)
	if !executable(t, filepath.Join(lib, "scripts", "run.sh")) {
		t.Error("scripts/run.sh of the library is not executable")
	}
	ev := h.one(out.stdout, "library_skill")
	equal(t, "drift", drift(ev), "")
	equal(t, "state", ev["state"], stateModified)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		keptCopyWarning(cursor, "alpha", "agentx skill remove alpha --from cursor", "agentx skill place alpha --to cursor --copy"))
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"repaired alpha in 2 configurations, 1 copy placement refreshed, 1 placement skipped; the library now holds what "+claude+" held")
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor,github-copilot,windsurf")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(windsurf), filepath.Dir(copilot))

	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "alpha").stdout, "diff")
	var changed []string
	for _, d := range diffs {
		changed = append(changed, d["path"].(string)+" "+d["status"].(string))
	}
	equal(t, "the diff against the base", strings.Join(changed, ", "), "mine.md added, notes.md modified")
	equal(t, "a second repair", h.mustRun("skill", "repair", "alpha", "--keep-placement").stdout, nothingWasRepaired("alpha"))
	equal(t, "drift after a second repair", drift(h.listed("alpha")), "")
}

// TestSkillRepairKeepPlacementRefreshesACopyOfTheBase: copies placed
// before the library was edited still hold the base version, which is what
// agentx placed there, so --keep-placement refreshes them with the content
// kept, as it refreshes a copy of what the library held. A copy edited
// where it is beside them is still kept byte for byte, and named.
func TestSkillRepairKeepPlacementRefreshesACopyOfTheBase(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	h.mustRun("skill", "place", "alpha", "--to", "cursor", "--to", "windsurf", "--to", "github-copilot", "--copy")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	copilot := filepath.Join(h.home, ".copilot", "skills", "alpha")
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	writeFile(t, filepath.Join(lib, "lib-edit.md"), "an edit made in the library\n")
	equal(t, "state before the repair", h.listed("alpha")["state"], stateModified)
	displace(t, lib, claude, true)
	kept := libraryTree(t, claude)

	out := h.mustRun("--json", "skill", "repair", "alpha", "--keep-placement")
	sameTree(t, "the library directory", libraryTree(t, lib), kept)
	linksToLibrary(t, "claude's placement", claude, lib)
	sameTree(t, "windsurf's copy of the base", libraryTree(t, windsurf), kept)
	sameTree(t, "copilot's copy of the base", libraryTree(t, copilot), kept)
	sameTree(t, "cursor's edited copy", libraryTree(t, cursor), edited)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		keptCopyWarning(cursor, "alpha", "agentx skill remove alpha --from cursor", "agentx skill place alpha --to cursor --copy"))
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"repaired alpha in 1 configuration, 2 copy placements refreshed, 1 placement skipped; the library now holds what "+claude+" held")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(windsurf), filepath.Dir(copilot))
}

// TestSkillRepairKeepPlacementOfTheBaseLeavesItCurrent: a displaced
// directory holding the skill's base version, kept with --keep-placement
// while the library is edited, makes the library hold the base again, so
// the skill is current afterwards, not modified. A copy that held what
// the library held is refreshed with it, and a copy that already holds the
// base is left alone and not counted.
func TestSkillRepairKeepPlacementOfTheBaseLeavesItCurrent(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	h.mustRun("skill", "place", "alpha", "--to", "cursor", "--to", "windsurf", "--to", "github-copilot", "--copy")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	copilot := filepath.Join(h.home, ".copilot", "skills", "alpha")
	base := libraryTree(t, lib)
	writeFile(t, filepath.Join(lib, "lib-edit.md"), "an edit made in the library\n")
	remove(t, windsurf)
	copyTree(t, lib, windsurf)
	remove(t, claude)
	copyTree(t, cursor, claude)
	ev := h.listed("alpha")
	equal(t, "state before the repair", ev["state"], stateModified)
	equal(t, "drift before the repair", drift(ev), "displaced")
	cursorBefore, err := os.Lstat(cursor)
	if err != nil {
		t.Fatal(err)
	}

	out := h.mustRun("--json", "skill", "repair", "alpha", "--keep-placement")
	sameTree(t, "the library directory", libraryTree(t, lib), base)
	linksToLibrary(t, "claude's placement", claude, lib)
	for _, place := range []string{cursor, windsurf, copilot} {
		sameTree(t, "the copy at "+place, libraryTree(t, place), base)
	}
	if cursorAfter, err := os.Lstat(cursor); err != nil || !os.SameFile(cursorBefore, cursorAfter) {
		t.Errorf("cursor's copy of the base was replaced although it held what the library now holds: %v", err)
	}
	ev = h.one(out.stdout, "library_skill")
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "drift", drift(ev), "")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"repaired alpha in 1 configuration, 1 copy placement refreshed; the library now holds what "+claude+" held")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(windsurf), filepath.Dir(copilot))
}

// TestSkillRepairKeepPlacementText: the text names the directory the
// library now holds the content of, below the rows.
func TestSkillRepairKeepPlacementText(t *testing.T) {
	t.Parallel()
	h, lib, claude, _ := repairHarness(t)
	displace(t, lib, claude, true)
	equal(t, "the text", h.mustRun("skill", "repair", "alpha", "--keep-placement").stdout, "✓ repaired alpha in 1 configuration\n"+
		"  claude-code  relinked  symlink  "+claude+" -> "+lib+"\n"+
		"  the library now holds what "+claude+" held\n")
}

// TestSkillRepairKeepPlacementRefusals: --keep-placement makes one
// directory's content the library's, so it refuses what would leave the
// library holding something else. Nothing is changed by any of them.
func TestSkillRepairKeepPlacementRefusals(t *testing.T) {
	t.Parallel()
	t.Run("two directories that differ from each other", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, cursor := repairHarness(t)
		displace(t, lib, claude, true)
		displace(t, lib, cursor, false)
		writeFile(t, filepath.Join(cursor, "notes.md"), "another edit\n")
		out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "--keep-placement keeps the content of one directory, and "+claude+", "+cursor+" hold different content")
		contains(t, "hint", e["hint"].(string), "move aside every directory but the one to keep")
		equal(t, "cursor's edit", fileBody(t, filepath.Join(cursor, "notes.md")), "another edit\n")
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))

		// Two directories holding the same content are one content to keep.
		displace(t, lib, cursor, true)
		h.mustRun("skill", "repair", "alpha", "--keep-placement")
		linksToLibrary(t, "claude's placement", claude, lib)
		linksToLibrary(t, "cursor's placement", cursor, lib)
		equal(t, "the library's notes.md", fileBody(t, filepath.Join(lib, "notes.md")), "alpha notes, edited in the displaced directory\n")
	})

	t.Run("a library entry that is a symlink", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, _ := repairHarness(t)
		dev := filepath.Join(h.home, "dev-alpha")
		if err := os.Rename(lib, dev); err != nil {
			t.Fatal(err)
		}
		link(t, dev, lib)
		displace(t, dev, claude, true)
		out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], lib+" is a symlink to "+dev+
			"; keeping the placement replaces the library directory and would drop the link without touching the files it leads to")
		linksToLibrary(t, "the library entry", lib, dev)
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
	})

	t.Run("a directory with no SKILL.md", func(t *testing.T) {
		t.Parallel()
		h, _, claude, _ := repairHarness(t)
		remove(t, claude)
		writeFile(t, mkdirs(t, claude, "notes.md"), "not a skill\n")
		out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], claude+" holds no SKILL.md, so its content cannot become the library's alpha")
		equal(t, "the directory", fileBody(t, filepath.Join(claude, "notes.md")), "not a skill\n")
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
	})

	t.Run("a directory holding a repository", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, _ := repairHarness(t)
		displace(t, lib, claude, true)
		writeFile(t, mkdirs(t, filepath.Join(claude, "vendored", ".git"), "HEAD"), "ref: refs/heads/main\n")
		out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], claude+" holds what git cannot record, so its content cannot become the library's")
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
	})
}

// TestSkillRepairKeepFlagsAreExclusive: each flag keeps one of the two
// contents, so giving both is a usage error, and nothing is changed.
func TestSkillRepairKeepFlagsAreExclusive(t *testing.T) {
	t.Parallel()
	h, lib, claude, _ := repairHarness(t)
	displace(t, lib, claude, true)
	out := h.run("--json", "skill", "repair", "alpha", "--keep-library", "--keep-placement")
	equal(t, "exit", out.exit, 1)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "usage")
	equal(t, "message", e["message"], "--keep-library and --keep-placement cannot both be given")
	if _, ok := isSymlink(t, claude); ok {
		t.Fatal("claude's directory was replaced")
	}
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
}

// TestSkillRepairReplacesTheLibrarysLinkWithACopy: the library's own
// symlink where copy_mode records a copy is displaced. The link holds
// nothing, so the repair replaces it with a copy of the library without a
// flag, as skill place would, and copy_mode is left as it is.
func TestSkillRepairReplacesTheLibrarysLinkWithACopy(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--copy")
	lib := filepath.Join(h.library, "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	remove(t, cursor)
	link(t, lib, cursor)
	equal(t, "drift before the repair", drift(h.listed("alpha")), "displaced")

	out := h.mustRun("skill", "repair", "alpha")
	equal(t, "the text", out.stdout, "✓ repaired alpha in 1 configuration\n  cursor  copied  copy  "+cursor+"\n")
	if _, ok := isSymlink(t, cursor); ok {
		t.Fatal("cursor's placement is still a symlink")
	}
	sameTree(t, "cursor's copy", libraryTree(t, cursor), libraryTree(t, lib))
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code,cursor")
	equal(t, "drift after the repair", drift(h.listed("alpha")), "")
	cleanAfterRepair(t, h, h.library, filepath.Dir(cursor))
}

// TestSkillRepairLeavesWhatIsNotDrift: a symlink of the user's to a
// directory of their own, a copy edited where it is and a disabled
// configuration's empty place are no drift, and a repair never touches
// them, whatever flag it is given. Where a missing placement is repaired
// beside them, they are still left alone.
func TestSkillRepairLeavesWhatIsNotDrift(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--to", "windsurf", "--to", "github-copilot")
	h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	copilot := filepath.Join(h.home, ".copilot", "skills", "alpha")
	mine := filepath.Join(h.home, "my-alpha")
	copyTree(t, filepath.Join(h.library, "alpha"), mine)
	remove(t, claude)
	link(t, mine, claude)
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	remove(t, copilot)
	h.mustRun("config", "disable", "github-copilot")
	equal(t, "drift", drift(h.listed("alpha")), "")

	for _, flag := range []string{"", "--keep-library", "--keep-placement"} {
		args := []string{"skill", "repair", "alpha"}
		if flag != "" {
			args = append(args, flag)
		}
		equal(t, "a repair "+flag, h.mustRun(args...).stdout, nothingWasRepaired("alpha"))
	}
	linksToLibrary(t, "claude's own link", claude, mine)
	sameTree(t, "cursor's edited copy", libraryTree(t, cursor), edited)
	nothingAt(t, "copilot's place", copilot)

	remove(t, windsurf)
	out := h.mustRun("--json", "skill", "repair", "alpha")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "repaired alpha in 1 configuration")
	linksToLibrary(t, "windsurf's placement", windsurf, filepath.Join(h.library, "alpha"))
	linksToLibrary(t, "claude's own link", claude, mine)
	sameTree(t, "cursor's edited copy", libraryTree(t, cursor), edited)
	nothingAt(t, "copilot's place", copilot)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
}

// TestSkillRepairLeavesTheLibraryAClientReadsThroughALink: a client whose
// skills directory is a symlink to the library, or the library a symlink
// to it, reads the library, and the skill's directory there is the library
// directory itself, not a directory displacing a placement. So does
// Cursor, which reads Claude Code's skills directory too. Drift names
// nothing there, the listing and a placement in Claude Code report the
// skill there as the library entry, and a repair, whatever flag it is
// given, leaves the library directory as it is, an edit of it included:
// replacing that directory with the symlink would replace the library
// with a link to itself. Removing the skill from Claude Code alone is
// refused, as it is for any client that reads the library. A placement
// that went missing elsewhere is still put back.
func TestSkillRepairLeavesTheLibraryAClientReadsThroughALink(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name    string
		reverse bool // the library is the link, to Claude Code's skills directory
		edited  bool // the library directory was edited
	}{
		{"a skills directory linked to the library", false, false},
		{"a skills directory linked to an edited library", false, true},
		{"a library linked to a skills directory", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			h.mustRun("skill", "add", s.url, "--skill", "alpha")
			lib := filepath.Join(h.library, "alpha")
			skills := filepath.Join(h.home, ".claude", "skills")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
			remove(t, skills)
			if c.reverse {
				if err := os.Rename(h.library, skills); err != nil {
					t.Fatal(err)
				}
				link(t, skills, h.library)
			} else {
				link(t, h.library, skills)
			}
			state := stateCurrent
			if c.edited {
				writeFile(t, filepath.Join(lib, "notes.md"), "alpha notes, edited in the library\n")
				state = stateModified
			}
			held := libraryTree(t, lib)
			ev := h.listed("alpha")
			equal(t, "drift", drift(ev), "")
			equal(t, "universal clients", fmt.Sprint(ev["universal"]), "[claude-code codex cursor gemini-cli]")
			equal(t, "placements", strings.Join(placementsOf(t, ev), ";"),
				"claude-code library library;codex library library;cursor library library;cursor symlink symlink;"+
					"gemini-cli library library;github-copilot symlink symlink;windsurf symlink symlink")
			version := mutationVersion(t, h)

			for _, flag := range []string{"", "--keep-library", "--keep-placement"} {
				args := []string{"skill", "repair", "alpha"}
				if flag != "" {
					args = append(args, flag)
				}
				equal(t, "a repair "+flag, h.mustRun(args...).stdout, nothingWasRepaired("alpha"))
			}
			info, err := os.Lstat(lib)
			if err != nil || !info.IsDir() {
				t.Fatalf("the library directory is no longer a directory: %v", err)
			}
			sameTree(t, "the library directory", libraryTree(t, lib), held)
			linksToLibrary(t, "cursor's link", cursor, lib)
			equal(t, "no mutation", mutationVersion(t, h), version)
			ev = h.listed("alpha")
			equal(t, "state after the repairs", ev["state"], state)
			equal(t, "drift after the repairs", drift(ev), "")
			cleanAfterRepair(t, h, h.library, skills)

			// Placing the skill in Claude Code names the library entry too.
			equal(t, "the text of a placement", h.mustRun("skill", "place", "alpha", "--to", "claude-code").stdout,
				"✓ placed alpha in 1 configuration\n"+
					"  claude-code  library  "+filepath.Join(skills, "alpha")+"\n"+
					"  always available to universal clients: claude-code, codex, cursor, gemini-cli\n")
			sameTree(t, "the library directory after a placement", libraryTree(t, lib), held)
			cleanAfterRepair(t, h, h.library, skills)

			removal := h.run("--json", "skill", "remove", "alpha", "--from", "claude-code")
			equal(t, "exit of a removal from claude-code", removal.exit, 6)
			equal(t, "the refusal", h.one(removal.stdout, "error")["message"], "claude-code reads the library directly, so alpha cannot be removed from it alone")
			sameTree(t, "the library directory after a refused removal", libraryTree(t, lib), held)

			remove(t, windsurf)
			equal(t, "drift with a placement missing", drift(h.listed("alpha")), "missing")
			equal(t, "the text", h.mustRun("skill", "repair", "alpha").stdout, "✓ repaired alpha in 1 configuration\n"+
				"  windsurf  placed  symlink  "+windsurf+" -> "+lib+"\n")
			linksToLibrary(t, "windsurf's placement", windsurf, lib)
			sameTree(t, "the library directory after a repair", libraryTree(t, lib), held)
			cleanAfterRepair(t, h, h.library, skills, filepath.Dir(windsurf))
		})
	}
}

// TestSkillRepairLeavesALibraryEntryThatLinksIntoAClient: a library entry
// made a symlink to a client's skill directory, here Claude Code's, makes
// that directory the library's own. It is no directory displacing a
// placement: drift names nothing there, the listing reports it as the
// library entry, and a repair, whatever flag it is given, leaves it and the
// link as they are, and so does placing the skill in Claude Code, as a
// symlink or as a copy. Replacing the directory with the library's symlink
// would leave a link to a link to itself and the skill's content gone.
func TestSkillRepairLeavesALibraryEntryThatLinksIntoAClient(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := repairHarness(t)
	remove(t, claude)
	if err := os.Rename(lib, claude); err != nil {
		t.Fatal(err)
	}
	link(t, claude, lib)
	held := libraryTree(t, claude)
	ev := h.listed("alpha")
	equal(t, "drift", drift(ev), "")
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "placements", strings.Join(placementsOf(t, ev), ";"),
		"claude-code library library;codex library library;cursor library library;cursor symlink symlink;gemini-cli library library")
	version := mutationVersion(t, h)
	untouched := func(what string) {
		t.Helper()
		linksToLibrary(t, what+": the library entry", lib, claude)
		if _, ok := isSymlink(t, claude); ok {
			t.Fatalf("%s: claude's directory, the library's own, was replaced", what)
		}
		sameTree(t, what+": claude's directory", libraryTree(t, claude), held)
		linksToLibrary(t, what+": cursor's link", cursor, lib)
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
	}

	for _, flag := range []string{"", "--keep-library", "--keep-placement"} {
		args := []string{"skill", "repair", "alpha"}
		if flag != "" {
			args = append(args, flag)
		}
		equal(t, "a repair "+flag, h.mustRun(args...).stdout, nothingWasRepaired("alpha"))
	}
	equal(t, "no mutation", mutationVersion(t, h), version)
	untouched("after the repairs")

	equal(t, "the text of a placement", h.mustRun("skill", "place", "alpha", "--to", "claude-code").stdout,
		"✓ placed alpha in 1 configuration\n"+
			"  claude-code  library  "+claude+"\n"+
			"  always available to universal clients: codex, gemini-cli\n")
	h.mustRun("skill", "place", "alpha", "--to", "claude-code", "--copy")
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")
	untouched("after the placements")
	equal(t, "drift after the placements", drift(h.listed("alpha")), "")
}

// TestSkillRepairJudgesALinkedSkillsDirectoryOnce: Cursor's skills
// directory made a symlink to Claude Code's makes their places one
// directory spelled two ways. A displaced directory there is judged and
// repaired once, whatever is kept of it, and reported for both
// configurations, each at the path it names the place by: repaired twice,
// the second removal would find the first one's link there and stop the
// mutation part way.
func TestSkillRepairJudgesALinkedSkillsDirectoryOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, flag string
		edited     bool
		state      string
		last       string // the line under the rows, naming what was kept or discarded
	}{
		{"the library's content", "", false, stateCurrent, ""},
		{"an edit, with --keep-library", "--keep-library", true, stateCurrent, "discarded what %s held"},
		{"an edit, with --keep-placement", "--keep-placement", true, stateModified, "the library now holds what %s held"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := repairHarness(t)
			remove(t, filepath.Dir(cursor))
			link(t, filepath.Dir(claude), filepath.Dir(cursor))
			displace(t, lib, claude, c.edited)
			want := libraryTree(t, lib)
			if c.flag == "--keep-placement" {
				want = libraryTree(t, claude)
			}
			equal(t, "drift before the repair", drift(h.listed("alpha")), "displaced")

			args := []string{"skill", "repair", "alpha"}
			if c.flag != "" {
				args = append(args, c.flag)
			}
			text := "✓ repaired alpha in 2 configurations\n" +
				"  claude-code  relinked  symlink  " + claude + " -> " + lib + "\n" +
				"  cursor       relinked  symlink  " + cursor + " -> " + lib + "\n"
			if c.last != "" {
				text += "  " + fmt.Sprintf(c.last, claude) + "\n"
			}
			equal(t, "the text", h.mustRun(args...).stdout, text)
			linksToLibrary(t, "the placement both read", claude, lib)
			sameTree(t, "the library directory", libraryTree(t, lib), want)
			ev := h.listed("alpha")
			equal(t, "state", ev["state"], c.state)
			equal(t, "drift after the repair", drift(ev), "")
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// TestSkillRepairJudgesASharedPlaceOnce: Zencoder and Zenflow read one
// skills directory, and copy_mode records a copy for Zenflow alone. The
// place both read is put back once, as a copy, since a copy placed for
// Zenflow is the one Zencoder reads, and counted for both configurations;
// copy_mode is left as it is.
func TestSkillRepairJudgesASharedPlaceOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".zencoder"}})
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "zenflow", "--copy")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	shared := filepath.Join(h.home, ".zencoder", "skills", "alpha")
	remove(t, shared)
	equal(t, "drift before the repair", drift(h.listed("alpha")), "missing")

	out := h.mustRun("skill", "repair", "alpha")
	equal(t, "the text", out.stdout, "✓ repaired alpha in 3 configurations\n"+
		"  claude-code  placed  symlink  "+claude+" -> "+lib+"\n"+
		"  zencoder     placed  copy     "+shared+"\n"+
		"  zenflow      placed  copy     "+shared+"\n")
	sameTree(t, "the shared copy", libraryTree(t, shared), libraryTree(t, lib))
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "zenflow")
	equal(t, "drift after the repair", drift(h.listed("alpha")), "")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(shared))
}

// TestSkillRepairSkipsAPlaceItCannotRead: a displaced directory this machine
// cannot read whole can be judged neither the library's content nor
// anything else, so it is left as it is, counted as skipped and named with
// the cause, and the rest of the repair still lands.
func TestSkillRepairSkipsAPlaceItCannotRead(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	h, lib, claude, cursor := repairHarness(t)
	displace(t, lib, claude, false)
	remove(t, cursor)
	scripts := filepath.Join(claude, "scripts")
	chmod(t, scripts, 0)
	t.Cleanup(func() { _ = os.Chmod(scripts, 0o755) }) // so the temporary home can be removed

	out := h.run("--json", "skill", "repair", "alpha", "--keep-library")
	chmod(t, scripts, 0o755)
	if out.exit != 0 {
		t.Fatalf("repair: exit %d\n%s", out.exit, out.stderr)
	}
	warned := warnings(h, out.stderr)
	if len(warned) != 1 || !strings.HasPrefix(warned[0], "cannot repair "+claude+": ") || !strings.HasSuffix(warned[0], "; it was left as it is") {
		t.Errorf("the warnings = %q, want one naming %s", warned, claude)
	}
	if _, ok := isSymlink(t, claude); ok {
		t.Fatal("the directory that could not be read was replaced")
	}
	sameTree(t, "claude's directory", libraryTree(t, claude), libraryTree(t, lib))
	linksToLibrary(t, "cursor's placement", cursor, lib)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "repaired alpha in 1 configuration, 1 placement skipped")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
}

// TestSkillRepairSkipsAPlaceItCannotWrite: a displaced directory in a
// skills directory this machine cannot write cannot be replaced by the
// symlink. That is found out before a step of it is recorded, so it is
// left as it is, counted as skipped and named with the cause, as a
// placement that cannot be made is, and the rest of the repair still lands
// with no journal left behind.
func TestSkillRepairSkipsAPlaceItCannotWrite(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only directory anyway")
	}
	for _, flag := range []string{"", "--keep-library"} {
		name := flag
		if name == "" {
			name = "no flag"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := repairHarness(t)
			displace(t, lib, claude, false)
			remove(t, cursor)
			skills := filepath.Dir(claude)
			chmod(t, skills, 0o555)
			t.Cleanup(func() { _ = os.Chmod(skills, 0o755) }) // so the temporary home can be removed

			args := []string{"--json", "skill", "repair", "alpha"}
			if flag != "" {
				args = append(args, flag)
			}
			out := h.run(args...)
			chmod(t, skills, 0o755)
			if out.exit != 0 {
				t.Fatalf("repair: exit %d\n%s", out.exit, out.stderr)
			}
			warned := warnings(h, out.stderr)
			if len(warned) != 1 || !strings.HasPrefix(warned[0], "cannot place "+claude+": ") || !strings.HasSuffix(warned[0], "; no placement was made for claude-code") {
				t.Errorf("the warnings = %q, want one naming %s", warned, claude)
			}
			if _, ok := isSymlink(t, claude); ok {
				t.Fatal("the directory that could not be replaced was replaced")
			}
			sameTree(t, "claude's directory", libraryTree(t, claude), libraryTree(t, lib))
			linksToLibrary(t, "cursor's placement", cursor, lib)
			equal(t, "summary", h.one(out.stdout, "result")["summary"], "repaired alpha in 1 configuration, 1 placement skipped")
			cleanAfterRepair(t, h, h.library, skills, filepath.Dir(cursor))
		})
	}
}

// TestSkillRepairWhenEveryPlaceIsSkipped: a repair that can put back none
// of the places drift names still succeeds, since each place it could not
// reach is named in a warning. It records no step, so it leaves no journal
// and nothing beside the place, and says it repaired the skill in no
// configuration.
func TestSkillRepairWhenEveryPlaceIsSkipped(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	h, lib, claude, _ := repairHarness(t)
	displace(t, lib, claude, false)
	scripts := filepath.Join(claude, "scripts")
	chmod(t, scripts, 0)
	t.Cleanup(func() { _ = os.Chmod(scripts, 0o755) }) // so the temporary home can be removed

	out := h.run("--json", "skill", "repair", "alpha")
	text := h.run("skill", "repair", "alpha")
	chmod(t, scripts, 0o755)
	for _, o := range []outcome{out, text} {
		if o.exit != 0 {
			t.Fatalf("repair: exit %d\n%s", o.exit, o.stderr)
		}
	}
	warned := warnings(h, out.stderr)
	if len(warned) != 1 || !strings.HasPrefix(warned[0], "cannot repair "+claude+": ") || !strings.HasSuffix(warned[0], "; it was left as it is") {
		t.Errorf("the warnings = %q, want one naming %s", warned, claude)
	}
	if _, ok := isSymlink(t, claude); ok {
		t.Fatal("the directory that could not be read was replaced")
	}
	sameTree(t, "claude's directory", libraryTree(t, claude), libraryTree(t, lib))
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "repaired alpha in 0 configurations, 1 placement skipped")
	equal(t, "the text", text.stdout, "✓ repaired alpha in 0 configurations, 1 placement skipped\n")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
}

// TestSkillRepairRefusesWhatIsNotAManagedSkill: drift is judged for a
// managed skill alone, so a repair refuses every other name, each with its
// own way on: an unmanaged skill, a fork, a managed skill whose library
// directory is gone, a managed skill whose import branch records no version
// agentx can read, and a name the library does not hold at all.
func TestSkillRepairRefusesWhatIsNotAManagedSkill(t *testing.T) {
	t.Parallel()
	t.Run("an unmanaged skill", func(t *testing.T) {
		t.Parallel()
		h, _ := installHarness(t)
		writeFile(t, mkdirs(t, filepath.Join(h.library, "notes"), "SKILL.md"), skill("notes", "My own notes"))
		out := h.run("--json", "skill", "repair", "notes")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "notes is not managed by agentx, so its placements are not judged for drift")
		equal(t, "hint", e["hint"], "place it in a configuration with 'agentx skill place notes --to <configuration>'")
	})

	t.Run("a fork", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, cursor := repairHarness(t)
		commit := h.accountGit("rev-parse", "refs/heads/managed/alpha")
		h.accountGit("update-ref", "refs/heads/skills/alpha", commit)
		h.accountGit("update-ref", "-d", "refs/heads/managed/alpha")
		remove(t, cursor)
		version := mutationVersion(t, h)
		out := h.run("--json", "skill", "repair", "alpha")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], "alpha is a fork on this machine")
		linksToLibrary(t, "claude's placement", claude, lib)
		nothingAt(t, "cursor's placement", cursor)
		equal(t, "the fork", h.accountGit("rev-parse", "refs/heads/skills/alpha"), commit)
		equal(t, "no mutation", mutationVersion(t, h), version)
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
	})

	t.Run("a managed skill whose library directory is gone", func(t *testing.T) {
		t.Parallel()
		h, s := installHarness(t)
		h.mustRun("skill", "add", s.url, "--skill", "alpha")
		remove(t, filepath.Join(h.library, "alpha"))
		out := h.run("--json", "skill", "repair", "alpha")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "alpha is managed in the account repo but the library holds no skill directory for it, so there is no skill to place")
		equal(t, "hint", e["hint"], "run 'agentx skill add "+shellWord(s.url)+" --skill alpha' to install it again, or 'agentx skill remove alpha' to stop managing it")
		equal(t, "journals", journalCount(t, h), 0)
	})

	t.Run("a managed skill whose import branch carries no lineage", func(t *testing.T) {
		t.Parallel()
		h, _, claude, cursor := repairHarness(t)
		remove(t, cursor)
		tree := h.accountGit("rev-parse", "refs/heads/managed/alpha^{tree}")
		plain := h.accountGit("commit-tree", tree, "-m", "plain")
		h.accountGit("update-ref", "refs/heads/managed/alpha", plain)
		version := mutationVersion(t, h)
		for _, flag := range []string{"", "--keep-placement"} {
			args := []string{"--json", "skill", "repair", "alpha"}
			if flag != "" {
				args = append(args, flag)
			}
			out := h.run(args...)
			equal(t, "exit "+flag, out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message "+flag, e["message"], "the import branch refs/heads/managed/alpha records no version agentx can read")
			equal(t, "hint "+flag, e["hint"], "run 'agentx doctor' and check the account repo it names")
		}
		nothingAt(t, "cursor's placement", cursor)
		equal(t, "no mutation", mutationVersion(t, h), version)
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
	})

	t.Run("a name the library does not hold", func(t *testing.T) {
		t.Parallel()
		h, _ := installHarness(t)
		version := mutationVersion(t, h)
		out := h.run("--json", "skill", "repair", "nothing")
		equal(t, "exit", out.exit, 5)
		contains(t, "message", h.one(out.stdout, "error")["message"].(string), `the library holds no skill called "nothing"`)
		equal(t, "no mutation", mutationVersion(t, h), version)
		equal(t, "journals", journalCount(t, h), 0)
		nothingAt(t, "the library", h.library)
	})
}

// TestSkillRepairRefusesWhenItsInputsChange: what a repair discards is
// what the paths held when it judged them, against the version the import
// branch named and the settings as they were. A git wrapper changes one of
// them once the repair holds the lock, at its read of the import branch:
// the branch moves, a fork of the name appears, the library directory or
// the displaced directory is edited, or the settings disable the
// configuration. The repair then refuses before it writes a journal and
// changes nothing, and the change is there afterwards.
func TestSkillRepairRefusesWhenItsInputsChange(t *testing.T) {
	t.Parallel()
	moved := "the import branch refs/heads/managed/alpha moved while alpha was being repaired, so nothing was changed"
	changed := "alpha or its placements changed while it was being repaired, so nothing was changed"
	for _, c := range []struct {
		name, flag, message string
		edits               string // the directory the change edits, whose tree is not held to what it was
		// change is what the wrapper runs, git being the real one, and a
		// check of what it left once the repair refused.
		change func(t *testing.T, h *harness, git, lib, claude string) (script string, check func(t *testing.T))
	}{
		{"the import branch moves", "--keep-library", moved, "", func(t *testing.T, h *harness, git, _, _ string) (string, func(*testing.T)) {
			before := h.accountGit("rev-parse", "refs/heads/managed/alpha")
			written := h.accountGit("commit-tree", before+"^{tree}", "-p", before, "-m", "moved")
			return accountRepoCommand(h, git, "update-ref", "refs/heads/managed/alpha", written), func(t *testing.T) {
				equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), written)
			}
		}},
		{"a fork appears", "--keep-library", moved, "", func(t *testing.T, h *harness, git, _, _ string) (string, func(*testing.T)) {
			before := h.accountGit("rev-parse", "refs/heads/managed/alpha")
			return accountRepoCommand(h, git, "update-ref", "refs/heads/skills/alpha", before), func(t *testing.T) {
				equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), before)
				equal(t, "the fork", h.accountGit("rev-parse", "refs/heads/skills/alpha"), before)
			}
		}},
		{"the library is edited", "--keep-placement", changed, "library", func(t *testing.T, _ *harness, _, lib, _ string) (string, func(*testing.T)) {
			file := filepath.Join(lib, "notes.md")
			return "printf 'an edit made meanwhile\\n' > " + shellWord(file), func(t *testing.T) {
				equal(t, "the library's notes.md", fileBody(t, file), "an edit made meanwhile\n")
			}
		}},
		{"the displaced directory is edited", "--keep-library", changed, "place", func(t *testing.T, _ *harness, _, _, claude string) (string, func(*testing.T)) {
			file := filepath.Join(claude, "notes.md")
			return "printf 'an edit made meanwhile\\n' > " + shellWord(file), func(t *testing.T) {
				equal(t, "the directory's notes.md", fileBody(t, file), "an edit made meanwhile\n")
			}
		}},
		{"the settings change", "--keep-library", changed, "", func(t *testing.T, h *harness, _, _, _ string) (string, func(*testing.T)) {
			settings := filepath.Join(h.agentx, "settings.json")
			edited := readSettingsFile(t, h)
			edited["disabled_configurations"] = []any{"claude-code"}
			b, err := json.Marshal(edited)
			if err != nil {
				t.Fatal(err)
			}
			next := filepath.Join(t.TempDir(), "settings.json")
			writeFile(t, next, string(b))
			cat, err := exec.LookPath("cat")
			if err != nil {
				t.Fatal(err)
			}
			return cat + " " + shellWord(next) + " > " + shellWord(settings), func(t *testing.T) {
				equal(t, "the settings", fileBody(t, settings), string(b))
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, _ := repairHarness(t)
			displace(t, lib, claude, true)
			library, place := libraryTree(t, lib), libraryTree(t, claude)
			version := mutationVersion(t, h)
			real, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			script, check := c.change(t, h, real, lib, claude)
			// The ref read under the lock asks for the refs by name, with a
			// format that ends in the object name; the listing of every branch
			// asks for trees and trailers too.
			stubGit(t, h, `#!/bin/sh
case " $* " in
*"%(objectname) refs/heads/managed/alpha "*) `+script+` || exit 1 ;;
esac
exec `+real+` "$@"
`)
			out := h.run("--json", "skill", "repair", "alpha", c.flag)
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], c.message)
			equal(t, "hint", e["hint"], "run 'agentx skill repair alpha' again")
			check(t)
			if _, ok := isSymlink(t, claude); ok {
				t.Fatal("the displaced directory was replaced")
			}
			if c.edits != "place" {
				sameTree(t, "the displaced directory", libraryTree(t, claude), place)
			}
			if c.edits != "library" {
				sameTree(t, "the library directory", libraryTree(t, lib), library)
			}
			equal(t, "no mutation", mutationVersion(t, h), version)
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// accountRepoCommand is the shell command that runs the real git, at git,
// with args against the account repo of h, as a wrapper runs it.
func accountRepoCommand(h *harness, git string, args ...string) string {
	return git + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx)) + " " + strings.Join(args, " ")
}

// repairChildEnv marks the process TestSkillRepairRecoversAtEveryBoundary
// starts, which runs the repair its value holds, the name and then the
// flags, against the parent's temporary home and is killed in the middle
// of it.
const repairChildEnv = "AGENTX_TEST_REPAIR_CHILD"

// TestRepairChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestRepairChildProcess(t *testing.T) {
	args := strings.Fields(os.Getenv(repairChildEnv))
	if len(args) == 0 {
		t.Skip("not the repair child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), append([]string{"skill", "repair"}, args...), env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// killedRepair runs one repair in a child process that a git wrapper kills
// the moment its journal is on disk: the first git the repair runs after
// writing it is the read of the import branch its ref step holds it to,
// before any path changed.
func killedRepair(t *testing.T, h *harness, args ...string) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	path := h.env["PATH"]
	defer func() { h.env["PATH"] = path }()
	stubGit(t, h, `#!/bin/sh
for f in `+shellWord(filepath.Join(h.agentx, "mutations"))+`/*.json; do
	if [ -e "$f" ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec `+real+` "$@"
`)
	child := exec.Command(os.Args[0], "-test.run=^TestRepairChildProcess$", "-test.v")
	child.Env = append(os.Environ(), repairChildEnv+"="+strings.Join(args, " "))
	for k, v := range h.env {
		child.Env = append(child.Env, k+"="+v)
	}
	out, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("the repair was not killed:\n%s", out)
	}
	return string(out)
}

// TestSkillRepairRecoversAtEveryBoundary kills a repair with SIGKILL once
// its journal is on disk, then leaves the machine as a process killed
// after each later step would, for each way a displaced directory that
// differs is repaired, beside a missing placement. The next command
// recovers each one, and the repair is then whole: the library holds the
// content chosen, both placements are the library's symlink, the import
// branch is where it was, and nothing staged or retained is left behind.
func TestSkillRepairRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		flag  string
		kinds string // the journal's steps, as recorded
		state string // the state of the skill afterwards
	}{
		{"--keep-library", "remove, link, link, ref", stateCurrent},
		{"--keep-placement", "remove, publish, remove, link, link, ref", stateModified},
	} {
		pathSteps := strings.Count(c.kinds, ",")
		for stop := 0; stop <= pathSteps; stop++ {
			t.Run(fmt.Sprintf("%s after %d steps", c.flag, stop), func(t *testing.T) {
				t.Parallel()
				h, lib, claude, cursor := repairHarness(t)
				base := libraryTree(t, lib)
				branch := h.accountGit("rev-parse", "refs/heads/managed/alpha")
				displace(t, lib, claude, true)
				kept := libraryTree(t, claude)
				remove(t, cursor)

				out := killedRepair(t, h, "alpha", c.flag)
				steps := readJournal(t, h)
				var kinds []string
				for _, s := range steps {
					kinds = append(kinds, s.Kind)
				}
				equal(t, "the journal's steps", strings.Join(kinds, ", "), c.kinds)
				sameTree(t, "claude's directory when the repair was killed", libraryTree(t, claude), kept)
				applySteps(t, steps, stop)

				if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
					t.Fatalf("the command after the killed repair: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
				}
				want := base
				if c.flag == "--keep-placement" {
					want = kept
				}
				sameTree(t, "the library directory", libraryTree(t, lib), want)
				linksToLibrary(t, "claude's placement", claude, lib)
				linksToLibrary(t, "cursor's placement", cursor, lib)
				if !executable(t, filepath.Join(lib, "scripts", "run.sh")) {
					t.Error("scripts/run.sh is not executable after recovery")
				}
				equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), branch)
				cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
				ev := h.listed("alpha")
				equal(t, "state", ev["state"], c.state)
				equal(t, "drift", drift(ev), "")
			})
		}
	}
}

// TestSkillRepairRecoversCopiesAtEveryBoundary kills a repair as
// TestSkillRepairRecoversAtEveryBoundary does, for the steps it takes at
// copy placements: with --keep-placement a copy holding what the library
// held is refreshed and a missing copy is made from the content kept, and
// with --keep-library the library's symlink where copy_mode records a copy
// is replaced by a copy. The next command recovers each one, and every copy
// then holds what the library holds.
func TestSkillRepairRecoversCopiesAtEveryBoundary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		flag  string
		kinds string // the journal's steps, as recorded
	}{
		{"--keep-placement", "remove, publish, remove, publish, remove, link, publish, ref"},
		{"--keep-library", "remove, link, remove, publish, ref"},
	} {
		pathSteps := strings.Count(c.kinds, ",")
		for stop := 0; stop <= pathSteps; stop++ {
			t.Run(fmt.Sprintf("%s after %d steps", c.flag, stop), func(t *testing.T) {
				t.Parallel()
				h, s := placementHarness(t)
				h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--to", "cursor")
				h.mustRun("skill", "place", "alpha", "--to", "windsurf", "--to", "github-copilot", "--copy")
				lib := filepath.Join(h.library, "alpha")
				claude := filepath.Join(h.home, ".claude", "skills", "alpha")
				windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
				copilot := filepath.Join(h.home, ".copilot", "skills", "alpha")
				want := libraryTree(t, lib)
				displace(t, lib, claude, true)
				if c.flag == "--keep-placement" {
					want = libraryTree(t, claude)
					remove(t, copilot)
				} else {
					remove(t, windsurf)
					link(t, lib, windsurf)
				}

				out := killedRepair(t, h, "alpha", c.flag)
				steps := readJournal(t, h)
				var kinds []string
				for _, s := range steps {
					kinds = append(kinds, s.Kind)
				}
				equal(t, "the journal's steps", strings.Join(kinds, ", "), c.kinds)
				applySteps(t, steps, stop)

				if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
					t.Fatalf("the command after the killed repair: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
				}
				sameTree(t, "the library directory", libraryTree(t, lib), want)
				linksToLibrary(t, "claude's placement", claude, lib)
				for _, place := range []string{windsurf, copilot} {
					if _, ok := isSymlink(t, place); ok {
						t.Errorf("%s is a symlink, want the copy copy_mode records", place)
					}
					sameTree(t, "the copy at "+place, libraryTree(t, place), want)
				}
				cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(windsurf), filepath.Dir(copilot))
				equal(t, "drift", drift(h.listed("alpha")), "")
			})
		}
	}
}

// TestSkillRepairSweepsStagingAKilledRepairLeft stands in for a repair
// killed after it staged and before its journal was written: what it
// staged, beside the library directory, beside a copy placement or beside
// a place it was putting back, is in directories nothing names. The next
// repair sweeps them before it stages anything and leaves nothing behind.
func TestSkillRepairSweepsStagingAKilledRepairLeft(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		args []string
	}{
		{"a displaced directory kept", []string{"--keep-placement"}},
		{"a missing placement", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s := installHarness(t)
			h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			lib := filepath.Join(h.library, "alpha")
			claude := filepath.Join(h.home, ".claude", "skills", "alpha")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			stage := func(dir string, n int) {
				writeFile(t, mkdirs(t, filepath.Join(dir, fmt.Sprintf(".agentx-staged-deadbeef-%d", n)), "SKILL.md"), skill("alpha", "staged content nothing names"))
			}
			dirs := []string{filepath.Dir(claude)}
			if c.args == nil {
				remove(t, claude)
			} else {
				displace(t, lib, claude, true)
				stage(h.library, 1)
				stage(filepath.Dir(cursor), 3)
				dirs = append(dirs, h.library, filepath.Dir(cursor))
			}
			stage(filepath.Dir(claude), 2)

			out := h.run(append([]string{"skill", "repair", "alpha"}, c.args...)...)
			if out.exit != 0 {
				t.Fatalf("repair: exit %d\n%s", out.exit, out.stderr)
			}
			linksToLibrary(t, "claude's placement", claude, lib)
			cleanAfterRepair(t, h, dirs...)
		})
	}
}
