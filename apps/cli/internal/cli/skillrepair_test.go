package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
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

// TestSkillRepairKeepPlacementJudgesWhatGitCannotRecordByteForByte: a
// library holding a repository of its own leaves one in every copy placed
// from it, which no tree records. --keep-placement still refreshes such a
// copy when it holds exactly what the library directory held, judged byte
// for byte, and keeps the copy whose repository was edited where it is.
func TestSkillRepairKeepPlacementJudgesWhatGitCannotRecordByteForByte(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--to", "github-copilot")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	writeFile(t, mkdirs(t, filepath.Join(lib, "sub", ".git"), "HEAD"), "ref: refs/heads/main\n")
	h.mustRun("skill", "place", "alpha", "--to", "windsurf", "--to", "cursor", "--copy")
	sameTree(t, "windsurf's copy", libraryTree(t, windsurf), libraryTree(t, lib))
	writeFile(t, filepath.Join(cursor, "sub", ".git", "HEAD"), "ref: refs/heads/other\n")
	edited := libraryTree(t, cursor)
	displace(t, lib, claude, true)
	remove(t, filepath.Join(claude, "sub"))
	kept := libraryTree(t, claude)

	out := h.mustRun("--json", "skill", "repair", "alpha", "--keep-placement")
	sameTree(t, "the library directory", libraryTree(t, lib), kept)
	linksToLibrary(t, "claude's placement", claude, lib)
	sameTree(t, "windsurf's refreshed copy", libraryTree(t, windsurf), kept)
	if _, err := os.Lstat(filepath.Join(windsurf, "sub")); err == nil {
		t.Error("windsurf's refreshed copy still holds the repository the library held")
	}
	sameTree(t, "cursor's edited copy", libraryTree(t, cursor), edited)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		keptCopyWarning(cursor, "alpha", "agentx skill remove alpha --from cursor", "agentx skill place alpha --to cursor --copy"))
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"repaired alpha in 1 configuration, 1 copy placement refreshed, 1 placement skipped; the library now holds what "+claude+" held")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(windsurf))
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

// TestSkillRepairKeepPlacementRefusesALinkIntoWhatItReplaces: the kept
// directory is copied into the library link for link, so a link in it that
// leads into the library directory, which --keep-placement replaces, would
// lead to itself once copied, and the files it led to would be discarded
// with the old library. So would one that leads into another displaced
// directory or a copy placement, both replaced as well, one whose route
// only passes through the library on its way elsewhere, even back into the
// directory: copied, it would pass through the library's new content,
// where what it passed through is gone, one inside a directory outside
// that a link of the directory leads to, which the copied link still
// leads to, and one that enters a displaced directory, another or the
// kept one, by name and leaves it by "..": once that directory is the
// library's symlink, its ".." is the library's parent. The repair refuses
// each, however the link is spelled, and changes nothing.
func TestSkillRepairKeepPlacementRefusesALinkIntoWhatItReplaces(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, link string
		// arrange sets the machine up and returns what the link in Claude
		// Code's displaced directory names and the path it leads into.
		arrange func(t *testing.T, h *harness, lib, claude, cursor string) (target, into string)
		// back is the link of the library, relative to it, that leads back
		// into the directory, so that --keep-library refuses as well.
		back string
		// named is the link the refusal names, relative to the directory,
		// when it is not link itself but one in the directory link leads to.
		named string
	}{
		{"a directory of the library", "scripts", func(t *testing.T, _ *harness, lib, _, _ string) (string, string) {
			return filepath.Join(lib, "scripts"), lib
		}, "", ""},
		{"the library's SKILL.md", "SKILL.md", func(t *testing.T, _ *harness, lib, _, _ string) (string, string) {
			return filepath.Join(lib, "SKILL.md"), lib
		}, "", ""},
		{"a relative link into the library", "scripts", func(t *testing.T, _ *harness, lib, claude, _ string) (string, string) {
			rel, err := filepath.Rel(claude, filepath.Join(lib, "scripts"))
			if err != nil {
				t.Fatal(err)
			}
			return rel, lib
		}, "", ""},
		{"another displaced directory", "scripts", func(t *testing.T, _ *harness, lib, _, cursor string) (string, string) {
			displace(t, lib, cursor, false)
			return filepath.Join(cursor, "scripts"), cursor
		}, "", ""},
		{"a copy placement", "scripts", func(t *testing.T, h *harness, _, _, cursor string) (string, string) {
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			return filepath.Join(cursor, "scripts"), cursor
		}, "", ""},
		{"a link through a library link that leads outside", "ref", func(t *testing.T, h *harness, lib, _, _ string) (string, string) {
			outside := filepath.Join(h.home, "outside")
			writeFile(t, mkdirs(t, outside, "x.md"), "notes kept outside the skill\n")
			link(t, outside, filepath.Join(lib, "out"))
			return filepath.Join(lib, "out"), lib
		}, "", ""},
		{"a link through a library link that leads back into the directory", "ref", func(t *testing.T, _ *harness, lib, claude, _ string) (string, string) {
			link(t, filepath.Join(claude, "SKILL.md"), filepath.Join(lib, "back"))
			return filepath.Join(lib, "back"), lib
		}, "back", ""},
		{"a link in a directory outside that a link of the directory leads to", "ext", func(t *testing.T, h *harness, lib, _, _ string) (string, string) {
			outside := filepath.Join(h.home, "ext")
			link(t, filepath.Join(lib, "scripts"), mkdirs(t, outside, "y"))
			return outside, lib
		}, "", filepath.Join("ext", "y")},
		{"a link through another displaced directory and out by '..'", "ref", func(t *testing.T, h *harness, lib, _, cursor string) (string, string) {
			// Once Cursor's directory is the library's symlink, its ".."
			// is the library's parent, and the link leads nowhere.
			displace(t, lib, cursor, false)
			writeFile(t, mkdirs(t, filepath.Join(h.home, ".cursor", "vendor"), "x.md"), "notes kept outside the skill\n")
			return cursor + "/../../vendor", cursor
		}, "", ""},
		{"an absolute link through the directory itself and out by '..'", "ref", func(t *testing.T, h *harness, _, claude, _ string) (string, string) {
			writeFile(t, mkdirs(t, filepath.Join(h.home, ".claude", "vendor"), "x.md"), "notes kept outside the skill\n")
			return claude + "/../../vendor", claude
		}, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := repairHarness(t)
			displace(t, lib, claude, true)
			target, into := c.arrange(t, h, lib, claude, cursor)
			swapForLink(t, filepath.Join(claude, c.link), target)
			library := onDisk(t, lib)
			version := mutationVersion(t, h)
			named := c.link
			if c.named != "" {
				named = c.named
			}

			out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], named+" in "+claude+" is a symlink into "+into+
				", which keeping the placement replaces, so its content cannot become the library's")
			equal(t, "hint", e["hint"], "replace the link "+named+" with the files it leads to, then run "+
				"'agentx skill repair alpha --keep-placement' again, or keep the library's content with 'agentx skill repair alpha --keep-library'")
			equal(t, "the library directory", onDisk(t, lib), library)
			equal(t, "the library's run.sh", fileBody(t, filepath.Join(lib, "scripts", "run.sh")), "#!/bin/sh\n")
			if info, err := os.Lstat(claude); err != nil || !info.IsDir() {
				t.Fatalf("claude's directory was replaced: %v", err)
			}
			if got, ok := isSymlink(t, filepath.Join(claude, c.link)); !ok || got != target {
				t.Errorf("the link in claude's directory: link %v to %q, want a link to %q", ok, got, target)
			}
			equal(t, "no mutation", mutationVersion(t, h), version)
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))

			if c.back != "" {
				// The library's own link leads into the directory, so
				// discarding it would lose what that link leads to.
				out := h.run("--json", "skill", "repair", "alpha", "--keep-library")
				equal(t, "exit of --keep-library", out.exit, 6)
				message, _ := heldRefusal(claude, filepath.Join(lib, c.back))
				equal(t, "message of --keep-library", h.one(out.stdout, "error")["message"], message)
				equal(t, "the library directory after --keep-library", onDisk(t, lib), library)
				return
			}
			// Told to keep the library's content, the repair discards the
			// directory, link and all, and the library keeps its files.
			h.mustRun("skill", "repair", "alpha", "--keep-library")
			linksToLibrary(t, "claude's placement", claude, lib)
			equal(t, "the library directory after --keep-library", onDisk(t, lib), library)
		})
	}
}

// TestSkillRepairKeepPlacementKeepsALinkThatLeadsElsewhere: an absolute
// link in the kept directory that leads outside everything the repair
// replaces, a sibling in the client's skills directory among them, and a
// relative one to a file of the directory itself, even spelled out of it
// and in again by its name, lose nothing once copied into the library, so
// the repair keeps each as the link it is.
func TestSkillRepairKeepPlacementKeepsALinkThatLeadsElsewhere(t *testing.T) {
	t.Parallel()
	h, lib, claude, _ := repairHarness(t)
	displace(t, lib, claude, true)
	outside := filepath.Join(h.home, "shared-notes.md")
	writeFile(t, outside, "notes kept outside the skill\n")
	link(t, outside, filepath.Join(claude, "shared.md"))
	link(t, filepath.Join("scripts", "run.sh"), filepath.Join(claude, "run"))

	sibling := filepath.Join(filepath.Dir(claude), "shared-notes")
	writeFile(t, mkdirs(t, sibling, "x.md"), "notes beside the skill\n")
	link(t, sibling, filepath.Join(claude, "sibling"))
	again := filepath.Join("..", "alpha", "mine.md")
	link(t, again, filepath.Join(claude, "again.md"))

	out := h.mustRun("--json", "skill", "repair", "alpha", "--keep-placement")
	linksToLibrary(t, "claude's placement", claude, lib)
	linksToLibrary(t, "the library's shared.md", filepath.Join(lib, "shared.md"), outside)
	linksToLibrary(t, "the library's run", filepath.Join(lib, "run"), filepath.Join("scripts", "run.sh"))
	linksToLibrary(t, "the library's sibling", filepath.Join(lib, "sibling"), sibling)
	linksToLibrary(t, "the library's again.md", filepath.Join(lib, "again.md"), again)
	equal(t, "what shared.md leads to", fileBody(t, filepath.Join(lib, "shared.md")), "notes kept outside the skill\n")
	equal(t, "what run leads to", fileBody(t, filepath.Join(lib, "run")), "#!/bin/sh\n")
	equal(t, "what sibling leads to", fileBody(t, filepath.Join(claude, "sibling", "x.md")), "notes beside the skill\n")
	equal(t, "what again.md leads to", fileBody(t, filepath.Join(claude, "again.md")), "a file of my own\n")
	equal(t, "state", h.one(out.stdout, "library_skill")["state"], stateModified)
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
}

// TestSkillRepairKeepPlacementRefusesARelativeLinkOutOfIt: the kept
// directory is copied into the library link for link, each link's target
// as it is spelled, so a relative link that leads out of the directory
// would lead from the library instead, to nothing or to another skill of
// the same name, and the client reading the library's symlink would lose
// what it reached through it. --keep-placement refuses it and changes
// nothing, the directory, the library and what the link leads to kept as
// they were; --keep-library, which copies nothing, repairs the skill.
func TestSkillRepairKeepPlacementRefusesARelativeLinkOutOfIt(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, link, target string
	}{
		{"a sibling in the client's skills directory", "notes", filepath.Join("..", "notes-of-mine")},
		{"from a directory inside it", filepath.Join("scripts", "notes"), filepath.Join("..", "..", "notes-of-mine")},
		{"out and in again under another name", "notes", filepath.Join("..", "notes-of-mine", "..", "notes-of-mine")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, _ := repairHarness(t)
			displace(t, lib, claude, true)
			notes := filepath.Join(filepath.Dir(claude), "notes-of-mine", "n.md")
			writeFile(t, mkdirs(t, filepath.Dir(notes), "n.md"), "my notes\n")
			link(t, c.target, filepath.Join(claude, c.link))
			equal(t, "what the link leads to", fileBody(t, filepath.Join(claude, c.link, "n.md")), "my notes\n")
			home, library, kept := onDisk(t, h.home), onDisk(t, h.library), onDisk(t, claude)
			version := mutationVersion(t, h)

			out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], c.link+" in "+claude+" is a relative symlink that leads out of it, "+
				"and copied into the library it would lead elsewhere, so its content cannot become the library's")
			equal(t, "hint", e["hint"], "make the link "+c.link+" absolute or replace it with the files it leads to, then run "+
				"'agentx skill repair alpha --keep-placement' again, or keep the library's content with 'agentx skill repair alpha --keep-library'")
			equal(t, "the home directory", onDisk(t, h.home), home)
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "the displaced directory", onDisk(t, claude), kept)
			equal(t, "no mutation", mutationVersion(t, h), version)
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude))

			h.mustRun("skill", "repair", "alpha", "--keep-library")
			linksToLibrary(t, "claude's placement", claude, lib)
			equal(t, "the library after --keep-library", onDisk(t, h.library), library)
			equal(t, "what the link led to after --keep-library", fileBody(t, notes), "my notes\n")
		})
	}
}

// TestSkillRepairKeepPlacementIgnoresALibraryLinkOutOfTheLibraryDirectory:
// a library link that enters alpha's library directory by name and leaves
// it by ".." does not pass through it once --keep-placement replaced it:
// the library directory is a real directory again, whose ".." is where it
// was, and the link still leads where it did.
func TestSkillRepairKeepPlacementIgnoresALibraryLinkOutOfTheLibraryDirectory(t *testing.T) {
	t.Parallel()
	h, lib, claude, _ := repairHarness(t)
	displace(t, lib, claude, true)
	writeFile(t, mkdirs(t, filepath.Join(h.library, "gamma"), "SKILL.md"), skill("gamma", "A skill of my own"))
	delta := filepath.Join(h.library, "delta")
	link(t, lib+"/../gamma", delta)

	h.mustRun("skill", "repair", "alpha", "--keep-placement")
	linksToLibrary(t, "claude's placement", claude, lib)
	equal(t, "what delta leads to", fileBody(t, filepath.Join(delta, "SKILL.md")), skill("gamma", "A skill of my own"))
	equal(t, "alpha's state", h.listed("alpha")["state"], stateModified)
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
}

// TestSkillRepairKeepPlacementCopiesTheExecBitGitRecords: git records a
// file as executable by its owner's bit alone, and so does every copy the
// repair makes: a file only others may run is laid out in the library and
// in a refreshed copy as a plain file, so each holds the tree it was judged
// to hold, and the repair lands.
func TestSkillRepairKeepPlacementCopiesTheExecBitGitRecords(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	h.mustRun("skill", "place", "alpha", "--to", "windsurf", "--copy")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	displace(t, lib, claude, true)
	chmod(t, filepath.Join(claude, "mine.md"), 0o645)

	out := h.mustRun("--json", "skill", "repair", "alpha", "--keep-placement")
	for _, dir := range []string{lib, windsurf} {
		if executable(t, filepath.Join(dir, "mine.md")) {
			t.Errorf("mine.md in %s is executable, want the plain file git records", dir)
		}
		if !executable(t, filepath.Join(dir, "scripts", "run.sh")) {
			t.Errorf("scripts/run.sh in %s is not executable", dir)
		}
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"repaired alpha in 3 configurations, 1 copy placement refreshed; the library now holds what "+claude+" held")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(windsurf))
}

// TestSkillRepairRefreshesALinkedCopyOnce: Cursor's skills directory made a
// symlink to Claude Code's makes the copies copy_mode records for the two
// one directory spelled two ways. --keep-placement refreshes it once and
// counts it for both configurations: refreshed twice, the second removal
// would find the first one's copy there and stop the mutation part way.
func TestSkillRepairRefreshesALinkedCopyOnce(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--to", "cursor", "--copy")
	h.mustRun("skill", "place", "alpha", "--to", "windsurf")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	remove(t, filepath.Dir(cursor))
	link(t, filepath.Dir(claude), filepath.Dir(cursor))
	displace(t, lib, windsurf, true)
	kept := libraryTree(t, windsurf)

	out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
	if out.exit != 0 {
		t.Fatalf("repair: exit %d\n%s", out.exit, out.stderr)
	}
	sameTree(t, "the library directory", libraryTree(t, lib), kept)
	sameTree(t, "the copy both read", libraryTree(t, claude), kept)
	linksToLibrary(t, "windsurf's placement", windsurf, lib)
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"repaired alpha in 2 configurations, 2 copy placements refreshed; the library now holds what "+windsurf+" held")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	ev := h.one(out.stdout, "library_skill")
	equal(t, "state", ev["state"], stateModified)
	equal(t, "drift", drift(ev), "")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(windsurf))
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

// onDisk is what the directory at root holds, entry by entry, without
// following a symlink: a link is its target, not what it leads to. A test
// that has to prove a refused repair changed nothing, links included, or
// that a repair kept a link as the link it was, compares two of them.
func onDisk(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "%s -> %s\n", rel, target)
		case d.IsDir():
			fmt.Fprintf(&b, "%s/\n", rel)
		default:
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "%s: %q\n", rel, content)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// heldRefusal is the message and hint of a repair that refuses to replace
// the directory at place because the link of the library leads into or
// through it.
func heldRefusal(place, link string) (string, string) {
	return place + " holds what the link " + link + " in the library leads to, and replacing it would delete that content, so nothing was repaired",
		"replace the link " + link + " with the files it leads to, then run 'agentx skill repair alpha' again"
}

// TestSkillRepairRefusesADirectoryALibraryLinkLeadsInto: every repair of a
// displaced directory removes it and puts the symlink in its place, so a
// symlink the library reaches that leads into the directory, or through it
// on its way elsewhere, entering it by name and leaving it by "..", or to
// a directory above it, would lose what it leads to: a library entry, a link deep inside another skill's directory,
// one inside alpha's own, or one inside a directory outside the library
// that such a link leads to. So would a directory that lies inside the
// library, as a client's skills directory made a symlink into a skill's
// directory leaves it. The repair refuses each, whatever it is told to
// keep, even where the directory holds exactly what the library holds, and
// changes nothing. A link inside alpha's own library directory is the one
// exception, with --keep-placement: that replaces the library directory,
// link and all, with the directory's content, so nothing the link leads to
// is lost.
func TestSkillRepairRefusesADirectoryALibraryLinkLeadsInto(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// arrange sets the machine up and returns the displaced directory
		// the repair refuses and the message and hint it refuses with.
		arrange func(t *testing.T, h *harness, lib, claude string) (place, message, hint string)
		// kept is whether --keep-placement repairs the skill all the same,
		// the link being inside alpha's own library directory.
		kept bool
		// entry is whether alpha's library entry is a symlink, which
		// --keep-placement refuses for that reason before any other.
		entry bool
	}{
		{"alpha's own entry, beneath the directory", func(t *testing.T, _ *harness, lib, claude string) (string, string, string) {
			remove(t, claude)
			inner := mkdirs(t, claude, "inner")
			if err := os.Rename(lib, inner); err != nil {
				t.Fatal(err)
			}
			link(t, inner, lib)
			writeFile(t, filepath.Join(claude, "README.md"), "a file beside the library's content\n")
			message, hint := heldRefusal(claude, lib)
			return claude, message, hint
		}, false, false},
		{"another skill's entry, in a directory that differs", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			displace(t, lib, claude, true)
			writeFile(t, mkdirs(t, filepath.Join(claude, "beta"), "SKILL.md"), skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, filepath.Join(claude, "beta"), beta)
			message, hint := heldRefusal(claude, beta)
			return claude, message, hint
		}, false, false},
		{"another skill's entry, in a directory holding the library's content", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			writeFile(t, mkdirs(t, filepath.Join(lib, "beta"), "SKILL.md"), skill("beta", "A skill of my own"))
			displace(t, lib, claude, false)
			beta := filepath.Join(h.library, "beta")
			link(t, filepath.Join(claude, "beta"), beta)
			message, hint := heldRefusal(claude, beta)
			return claude, message, hint
		}, false, false},
		{"a link deep in another skill's directory", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			writeFile(t, mkdirs(t, filepath.Join(lib, "shared"), "data.md"), "data alpha and beta share\n")
			displace(t, lib, claude, false)
			beta := filepath.Join(h.library, "beta")
			writeFile(t, mkdirs(t, beta, "SKILL.md"), skill("beta", "A skill of my own"))
			shared := filepath.Join(beta, "shared")
			link(t, filepath.Join(claude, "shared"), shared)
			message, hint := heldRefusal(claude, shared)
			return claude, message, hint
		}, false, false},
		{"an entry whose route passes through the directory", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			displace(t, lib, claude, false)
			real := filepath.Join(h.home, "real-beta")
			writeFile(t, mkdirs(t, real, "SKILL.md"), skill("beta", "A skill of my own"))
			// The same link in both, so the directory still holds exactly
			// what the library holds; only the library's beta leads
			// through the directory's.
			link(t, real, filepath.Join(lib, "beta-link"))
			link(t, real, filepath.Join(claude, "beta-link"))
			beta := filepath.Join(h.library, "beta")
			link(t, filepath.Join(claude, "beta-link"), beta)
			message, hint := heldRefusal(claude, beta)
			return claude, message, hint
		}, false, false},
		{"an entry whose route leaves a directory holding the library's content by '..'", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			// Once the directory is the library's symlink, its ".." is the
			// library's parent, and beta leads nowhere.
			displace(t, lib, claude, false)
			writeFile(t, mkdirs(t, filepath.Join(h.home, ".claude", "vendor-beta"), "SKILL.md"), skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, claude+"/../../vendor-beta", beta)
			message, hint := heldRefusal(claude, beta)
			return claude, message, hint
		}, false, false},
		{"an entry whose route leaves a directory that differs by '..'", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			displace(t, lib, claude, true)
			writeFile(t, mkdirs(t, filepath.Join(h.home, ".claude", "vendor-beta"), "SKILL.md"), skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, claude+"/scripts/../../../vendor-beta", beta)
			message, hint := heldRefusal(claude, beta)
			return claude, message, hint
		}, false, false},
		{"a directory inside the library", func(t *testing.T, h *harness, lib, _ string) (string, string, string) {
			beta := filepath.Join(h.library, "beta")
			writeFile(t, mkdirs(t, beta, "SKILL.md"), skill("beta", "A skill of my own"))
			copyTree(t, lib, filepath.Join(beta, "alpha"))
			skills := filepath.Join(h.home, ".cursor", "skills")
			remove(t, skills)
			link(t, beta, skills)
			place := filepath.Join(skills, "alpha")
			return place, place + " lies inside the library " + h.library + ", and replacing it would delete what the library holds there, so nothing was repaired",
				"make " + skills + " a directory outside the library, then run 'agentx skill repair alpha' again"
		}, false, false},
		{"a link in alpha's library directory", func(t *testing.T, _ *harness, lib, claude string) (string, string, string) {
			displace(t, lib, claude, true)
			scripts := filepath.Join(lib, "scripts")
			swapForLink(t, scripts, filepath.Join(claude, "scripts"))
			message, hint := heldRefusal(claude, scripts)
			return claude, message, hint + ", or make the content of " + claude + " the library's with 'agentx skill repair alpha --keep-placement'"
		}, true, false},
		{"alpha's SKILL.md", func(t *testing.T, _ *harness, lib, claude string) (string, string, string) {
			displace(t, lib, claude, true)
			file := filepath.Join(lib, "SKILL.md")
			swapForLink(t, file, filepath.Join(claude, "SKILL.md"))
			message, hint := heldRefusal(claude, file)
			return claude, message, hint + ", or make the content of " + claude + " the library's with 'agentx skill repair alpha --keep-placement'"
		}, true, false},
		{"a link in a directory outside alpha's linked entry", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			// Alpha is developed in a directory of the user's that the
			// library entry links to, and a link there leads into the
			// displaced directory: removing it would empty the library's
			// scripts.
			displace(t, lib, claude, true)
			dev := filepath.Join(h.home, "dev", "alpha")
			if err := os.MkdirAll(filepath.Dir(dev), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(lib, dev); err != nil {
				t.Fatal(err)
			}
			link(t, dev, lib)
			swapForLink(t, filepath.Join(dev, "scripts"), filepath.Join(claude, "scripts"))
			message, hint := heldRefusal(claude, filepath.Join(lib, "scripts"))
			return claude, message, hint
		}, false, true},
		{"a link in a directory outside another skill's linked entry", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			displace(t, lib, claude, true)
			writeFile(t, mkdirs(t, filepath.Join(claude, "shared"), "data.md"), "only in the displaced directory\n")
			dev := filepath.Join(h.home, "dev", "beta")
			writeFile(t, mkdirs(t, dev, "SKILL.md"), skill("beta", "A skill of my own"))
			link(t, filepath.Join(claude, "shared"), filepath.Join(dev, "shared"))
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			message, hint := heldRefusal(claude, filepath.Join(beta, "shared"))
			return claude, message, hint
		}, false, false},
		{"a link in a directory outside that another skill's link leads to", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			displace(t, lib, claude, true)
			beta := filepath.Join(h.library, "beta")
			writeFile(t, mkdirs(t, beta, "SKILL.md"), skill("beta", "A skill of my own"))
			outside := filepath.Join(h.home, "shared")
			link(t, filepath.Join(claude, "mine.md"), mkdirs(t, outside, "mine.md"))
			link(t, outside, filepath.Join(beta, "shared"))
			message, hint := heldRefusal(claude, filepath.Join(beta, "shared", "mine.md"))
			return claude, message, hint
		}, false, false},
		{"a link in a directory outside that alpha's own link leads to", func(t *testing.T, h *harness, lib, claude string) (string, string, string) {
			// The link that leads into the directory is not inside alpha's
			// library directory, so --keep-placement, which replaces only
			// that, would not take it away either.
			displace(t, lib, claude, true)
			writeFile(t, mkdirs(t, filepath.Join(claude, "shared"), "data.md"), "only in the displaced directory\n")
			vendor := filepath.Join(h.home, "vendor")
			link(t, filepath.Join(claude, "shared"), mkdirs(t, vendor, "shared"))
			link(t, vendor, filepath.Join(lib, "vendor"))
			message, hint := heldRefusal(claude, filepath.Join(lib, "vendor", "shared"))
			return claude, message, hint
		}, false, false},
		{"a link to a directory above the directory", func(t *testing.T, _ *harness, lib, claude string) (string, string, string) {
			// Through the link the library reaches every client's skill
			// in that skills directory, the displaced one among them.
			displace(t, lib, claude, true)
			up := filepath.Join(lib, "up")
			link(t, filepath.Dir(claude), up)
			message, hint := heldRefusal(claude, up)
			return claude, message, hint + ", or make the content of " + claude + " the library's with 'agentx skill repair alpha --keep-placement'"
		}, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, _ := repairHarness(t)
			place, message, hint := c.arrange(t, h, lib, claude)
			equal(t, "drift", drift(h.listed("alpha")), "displaced")
			home, library, held := onDisk(t, h.home), onDisk(t, h.library), onDisk(t, place)
			version := mutationVersion(t, h)

			flags := []string{"", "--keep-library", "--keep-placement"}
			if c.kept {
				flags = flags[:2]
			}
			for _, flag := range flags {
				args := []string{"--json", "skill", "repair", "alpha"}
				if flag != "" {
					args = append(args, flag)
				}
				out := h.run(args...)
				equal(t, "exit "+flag, out.exit, 6)
				e := h.one(out.stdout, "error")
				if c.entry && flag == "--keep-placement" {
					contains(t, "message "+flag, e["message"].(string), lib+" is a symlink to ")
				} else {
					equal(t, "message "+flag, e["message"], message)
					equal(t, "hint "+flag, e["hint"], hint)
				}
				equal(t, "the home directory "+flag, onDisk(t, h.home), home)
				equal(t, "the library "+flag, onDisk(t, h.library), library)
			}
			equal(t, "no mutation", mutationVersion(t, h), version)
			cleanAfterRepair(t, h, h.library, filepath.Dir(place))
			if !c.kept {
				return
			}

			// --keep-placement replaces the library directory, the link
			// included, with what the directory holds, which is then the
			// library's own content.
			h.mustRun("skill", "repair", "alpha", "--keep-placement")
			linksToLibrary(t, "claude's placement", claude, lib)
			equal(t, "the library directory", onDisk(t, lib), held)
			equal(t, "the library's run.sh", fileBody(t, filepath.Join(lib, "scripts", "run.sh")), "#!/bin/sh\n")
			ev := h.listed("alpha")
			equal(t, "state", ev["state"], stateModified)
			equal(t, "drift after the repair", drift(ev), "")
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// TestSkillRepairKeepPlacementRefusesToReplaceWhatALibraryLinkLeadsInto:
// --keep-placement replaces the library directory with the displaced
// directory's content and refreshes the copies that hold what the library
// held. A symlink of the library outside alpha's own directory that leads
// into either, as a skill vendored inside alpha's directory is linked from
// the library, would lose what it leads to, so the repair refuses and
// changes nothing. --keep-library replaces neither and repairs the skill,
// the other skill kept as it was.
func TestSkillRepairKeepPlacementRefusesToReplaceWhatALibraryLinkLeadsInto(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// arrange sets the machine up and returns the link of the library,
		// the path it leads into, the library directory or a copy, and a
		// file the other skill reaches through it.
		arrange func(t *testing.T, h *harness, lib, cursor string) (link, into, reached string)
	}{
		{"another skill vendored in alpha's directory", func(t *testing.T, h *harness, lib, _ string) (string, string, string) {
			vendored := filepath.Join(lib, "vendored", "beta")
			writeFile(t, mkdirs(t, vendored, "SKILL.md"), skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, vendored, beta)
			return beta, lib, filepath.Join(beta, "SKILL.md")
		}},
		{"another skill's link into a copy", func(t *testing.T, h *harness, _, cursor string) (string, string, string) {
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			beta := filepath.Join(h.library, "beta")
			writeFile(t, mkdirs(t, beta, "SKILL.md"), skill("beta", "A skill of my own"))
			scripts := filepath.Join(beta, "scripts")
			link(t, filepath.Join(cursor, "scripts"), scripts)
			return scripts, cursor, filepath.Join(scripts, "run.sh")
		}},
		{"a link in a directory outside another skill's linked entry", func(t *testing.T, h *harness, lib, _ string) (string, string, string) {
			// Beta is developed in a directory of the user's that the
			// library entry links to, and borrows alpha's scripts from there.
			dev := filepath.Join(h.home, "dev", "beta")
			writeFile(t, mkdirs(t, dev, "SKILL.md"), skill("beta", "A skill of my own"))
			link(t, filepath.Join(lib, "scripts"), filepath.Join(dev, "scripts"))
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			scripts := filepath.Join(beta, "scripts")
			return scripts, lib, filepath.Join(scripts, "run.sh")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := repairHarness(t)
			displace(t, lib, claude, true)
			held, into, reached := c.arrange(t, h, lib, cursor)
			home, library, content := onDisk(t, h.home), onDisk(t, h.library), fileBody(t, reached)
			version := mutationVersion(t, h)

			out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], into+" holds what the link "+held+" in the library leads to, and keeping the placement replaces it, which would delete that content, so nothing was repaired")
			equal(t, "hint", e["hint"], "replace the link "+held+" with the files it leads to, then run 'agentx skill repair alpha --keep-placement' again, "+
				"or keep the library's content with 'agentx skill repair alpha --keep-library'")
			equal(t, "the home directory", onDisk(t, h.home), home)
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "no mutation", mutationVersion(t, h), version)
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))

			h.mustRun("skill", "repair", "alpha", "--keep-library")
			linksToLibrary(t, "claude's placement", claude, lib)
			equal(t, "what beta reaches after --keep-library", fileBody(t, reached), content)
			equal(t, "drift after --keep-library", drift(h.listed("alpha")), "")
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
		})
	}
}

// TestSkillRepairRefusesAPlaceInsideWhatItReplaces: a client's skills
// directory made a symlink into another client's skill directory puts its
// place inside that one. The journal applies each step to a path as it
// resolves then, so once the outer directory is replaced by the library's
// symlink the inner place resolves into the library, and repairing it
// would delete or overwrite the library's own content; repaired first, it
// would change what the outer directory was judged to hold, and the
// outer one's removal would stop the mutation part way. A place the repair
// does not write goes with the outer directory all the same, whatever it
// holds: a copy of this skill, edited or not, and another skill's copy in
// that client's skills directory. A copy --keep-placement refreshes is
// written the same way, and so is a missing place inside the library, or
// inside what a link of the library leads to, and the library's own
// symlink where a copy belongs there. The repair refuses each whatever it
// is told to keep, changes nothing and leaves no journal behind.
func TestSkillRepairRefusesAPlaceInsideWhatItReplaces(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// arrange sets the machine up and returns the message and hint the
		// repair refuses with and a file of the library it must keep.
		arrange func(t *testing.T, h *harness, lib, claude, cursor string) (message, hint, kept string)
		flags   []string
	}{
		{"a client's place inside another client's displaced directory", func(t *testing.T, _ *harness, lib, claude, cursor string) (string, string, string) {
			// Cursor's place is the library's sub/alpha, as Claude Code's
			// displaced copy of it holds it.
			kept := mkdirs(t, filepath.Join(lib, "sub", "alpha"), "x.md")
			writeFile(t, kept, "a file of the library's own\n")
			displace(t, lib, claude, false)
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, filepath.Join(claude, "sub"), skills)
			return cursor + " lies inside " + claude + ", which the repair replaces, so nothing was repaired",
				"make " + skills + " a directory outside " + claude + ", then run 'agentx skill repair alpha' again", kept
		}, []string{"", "--keep-library", "--keep-placement"}},
		{"a copy inside the directory --keep-placement keeps", func(t *testing.T, h *harness, lib, claude, cursor string) (string, string, string) {
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			displace(t, lib, claude, true)
			if err := os.Rename(cursor, filepath.Join(claude, "scripts", "alpha")); err != nil {
				t.Fatal(err)
			}
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, filepath.Join(claude, "scripts"), skills)
			return cursor + " lies inside " + claude + ", which the repair replaces, so nothing was repaired",
				"make " + skills + " a directory outside " + claude + ", then run 'agentx skill repair alpha' again", filepath.Join(lib, "scripts", "run.sh")
		}, []string{"", "--keep-library", "--keep-placement"}},
		{"an edited copy inside the directory", func(t *testing.T, h *harness, lib, claude, cursor string) (string, string, string) {
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			writeFile(t, filepath.Join(cursor, "mine.md"), "a file of my own, in Cursor's copy\n")
			displace(t, lib, claude, true)
			moved := filepath.Join(claude, "scripts", "alpha")
			if err := os.Rename(cursor, moved); err != nil {
				t.Fatal(err)
			}
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, filepath.Join(claude, "scripts"), skills)
			return cursor + " lies inside " + claude + ", which the repair replaces, so nothing was repaired",
				"make " + skills + " a directory outside " + claude + ", then run 'agentx skill repair alpha' again", filepath.Join(moved, "mine.md")
		}, []string{"", "--keep-library", "--keep-placement"}},
		{"another skill's edited copy in a skills directory inside the directory", func(t *testing.T, h *harness, lib, claude, cursor string) (string, string, string) {
			// Nothing of alpha's is wrong in Cursor, but Cursor's whole
			// skills directory, beta's copy in it, is inside Claude Code's
			// displaced directory.
			writeFile(t, mkdirs(t, filepath.Join(h.library, "beta"), "SKILL.md"), skill("beta", "A skill of my own"))
			h.mustRun("skill", "place", "beta", "--to", "cursor", "--copy")
			skills := filepath.Dir(cursor)
			writeFile(t, filepath.Join(skills, "beta", "mine.md"), "a file of my own, in Cursor's copy of beta\n")
			displace(t, lib, claude, true)
			vendor := filepath.Join(claude, "vendor")
			if err := os.Rename(skills, vendor); err != nil {
				t.Fatal(err)
			}
			link(t, vendor, skills)
			return cursor + " lies inside " + claude + ", which the repair replaces, so nothing was repaired",
				"make " + skills + " a directory outside " + claude + ", then run 'agentx skill repair alpha' again", filepath.Join(vendor, "beta", "mine.md")
		}, []string{"", "--keep-library", "--keep-placement"}},
		{"a missing place inside what a link of the library leads to", func(t *testing.T, h *harness, _, _, cursor string) (string, string, string) {
			dev := filepath.Join(h.home, "dev", "beta")
			kept := mkdirs(t, dev, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, dev, skills)
			message, hint := writtenRefusal(cursor, beta)
			return message, hint, kept
		}, []string{"", "--keep-library", "--keep-placement"}},
		{"a missing place inside what a link of the library leads to, reached through that link", func(t *testing.T, h *harness, _, _, cursor string) (string, string, string) {
			dev := filepath.Join(h.home, "dev", "beta")
			kept := mkdirs(t, dev, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, beta, skills)
			message, hint := writtenRefusal(cursor, beta)
			return message, hint, kept
		}, []string{"", "--keep-library", "--keep-placement"}},
		{"a missing copy inside what a link of the library leads to", func(t *testing.T, h *harness, _, _, cursor string) (string, string, string) {
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			dev := filepath.Join(h.home, "dev", "beta")
			kept := mkdirs(t, dev, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, dev, skills)
			message, hint := writtenRefusal(cursor, beta)
			return message, hint, kept
		}, []string{"", "--keep-library", "--keep-placement"}},
		{"the library's symlink where a copy belongs, inside what a link of the library leads to", func(t *testing.T, h *harness, lib, _, cursor string) (string, string, string) {
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			dev := filepath.Join(h.home, "dev", "beta")
			kept := mkdirs(t, dev, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			link(t, lib, filepath.Join(dev, "alpha"))
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, dev, skills)
			message, hint := writtenRefusal(cursor, beta)
			return message, hint, kept
		}, []string{"", "--keep-library", "--keep-placement"}},
		{"a missing place inside the library", func(t *testing.T, h *harness, _, _, cursor string) (string, string, string) {
			beta := filepath.Join(h.library, "beta")
			kept := mkdirs(t, beta, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, beta, skills)
			return cursor + " lies inside the library " + h.library + ", and placing the skill there would change what the library holds, so nothing was repaired",
				"make " + skills + " a directory outside the library, then run 'agentx skill repair alpha' again", kept
		}, []string{"", "--keep-library", "--keep-placement"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := repairHarness(t)
			message, hint, kept := c.arrange(t, h, lib, claude, cursor)
			if drift(h.listed("alpha")) == "" {
				t.Fatal("alpha has no drift to repair")
			}
			home, library, content := onDisk(t, h.home), onDisk(t, h.library), fileBody(t, kept)
			version := mutationVersion(t, h)

			for _, flag := range c.flags {
				args := []string{"--json", "skill", "repair", "alpha"}
				if flag != "" {
					args = append(args, flag)
				}
				out := h.run(args...)
				equal(t, "exit "+flag, out.exit, 6)
				e := h.one(out.stdout, "error")
				equal(t, "message "+flag, e["message"], message)
				equal(t, "hint "+flag, e["hint"], hint)
				equal(t, "the home directory "+flag, onDisk(t, h.home), home)
				equal(t, "the library "+flag, onDisk(t, h.library), library)
				equal(t, "what the library keeps "+flag, fileBody(t, kept), content)
			}
			equal(t, "no mutation", mutationVersion(t, h), version)
			cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
			h.mustRun("skill", "list")
		})
	}
}

// writtenRefusal is the message and hint of a repair that refuses to write
// the place at place because it lies inside what the link of the library
// leads to.
func writtenRefusal(place, link string) (string, string) {
	return place + " lies inside what the link " + link + " in the library leads to, and placing the skill there would change what the library holds, so nothing was repaired",
		"replace the link " + link + " with the files it leads to, or make " + filepath.Dir(place) + " a directory outside it, then run 'agentx skill repair alpha' again"
}

// TestSkillRepairPlacesBesideWhatALibraryLinkLeadsTo: a link of the
// library that leads to a directory no place lies in is no reason to
// refuse a missing placement elsewhere, and the repair makes it.
func TestSkillRepairPlacesBesideWhatALibraryLinkLeadsTo(t *testing.T) {
	t.Parallel()
	h, lib, _, cursor := repairHarness(t)
	dev := filepath.Join(h.home, "dev", "beta")
	writeFile(t, mkdirs(t, dev, "SKILL.md"), skill("beta", "A skill of my own"))
	link(t, dev, filepath.Join(h.library, "beta"))
	remove(t, cursor)
	equal(t, "drift before the repair", drift(h.listed("alpha")), "missing")

	h.mustRun("skill", "repair", "alpha")
	linksToLibrary(t, "cursor's placement", cursor, lib)
	equal(t, "drift after the repair", drift(h.listed("alpha")), "")
	cleanAfterRepair(t, h, h.library, filepath.Dir(cursor))
}

// TestSkillRepairFollowsEveryLinkOnce: the links the library reaches are
// followed wherever they lead, and a directory is walked once however many
// links lead to it: a link back to its own skill, a directory outside the
// library linking to itself and back to the library. The walk ends, finds
// nothing leading into the displaced directory, and the repair lands.
func TestSkillRepairFollowsEveryLinkOnce(t *testing.T) {
	t.Parallel()
	h, lib, claude, _ := repairHarness(t)
	displace(t, lib, claude, false)
	beta := filepath.Join(h.library, "beta")
	writeFile(t, mkdirs(t, beta, "SKILL.md"), skill("beta", "A skill of my own"))
	link(t, beta, filepath.Join(beta, "loop"))
	dev := filepath.Join(h.home, "dev", "gamma")
	writeFile(t, mkdirs(t, dev, "SKILL.md"), skill("gamma", "Another skill of my own"))
	link(t, dev, filepath.Join(dev, "self"))
	link(t, h.library, filepath.Join(dev, "library"))
	link(t, filepath.Dir(dev), filepath.Join(dev, "up"))
	link(t, dev, filepath.Join(h.library, "gamma"))

	h.mustRun("skill", "repair", "alpha")
	linksToLibrary(t, "claude's placement", claude, lib)
	equal(t, "drift after the repair", drift(h.listed("alpha")), "")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
}

// TestSkillRepairRefusesALibraryThatReachesTooFar: a link of the library
// to a directory holding more files and directories than the walk visits,
// as a link to a home directory would, cannot be shown not to lead into
// the directory a repair removes, so the repair refuses and names the link.
// The same library under a higher limit is walked to the end.
func TestSkillRepairRefusesALibraryThatReachesTooFar(t *testing.T) {
	t.Parallel()
	library, outside := t.TempDir(), t.TempDir()
	for i := range 5 {
		writeFile(t, filepath.Join(outside, fmt.Sprint(i)), "a file outside the library\n")
	}
	beta := filepath.Join(library, "beta")
	link(t, outside, beta)
	place := filepath.Join(t.TempDir(), "alpha")
	writeFile(t, mkdirs(t, place, "SKILL.md"), skill("alpha", "A displaced copy"))
	state, err := home.State(place)
	if err != nil {
		t.Fatal(err)
	}
	plan := repairPlan{
		lib:     scan.LibrarySkill{Name: "alpha", Path: filepath.Join(library, "alpha")},
		library: library,
		places:  []repairPlace{{placeSite: placeSite{path: place}, word: driftDisplaced, state: state}},
	}

	err = guardRepair(&plan, 3)
	var f *failure
	if !errors.As(err, &f) {
		t.Fatalf("a walk past its limit: %v, want a refusal", err)
	}
	equal(t, "exit", f.status.exit, 6)
	equal(t, "message", f.message, "the links in the library "+library+" lead to more than 3 files and directories, "+
		"too many to show that none of them leads into what the repair replaces, so nothing was repaired")
	equal(t, "hint", f.hint, "replace the link "+beta+" with the files it leads to, or make it lead to a smaller directory, "+
		"then run 'agentx skill repair alpha' again")
	if err := guardRepair(&plan, 100); err != nil {
		t.Fatalf("a walk within its limit: %v", err)
	}
}

// TestSkillRepairRefusesWhatItCannotFollow: a directory this machine cannot
// read, reached through a link of the library or of the directory
// --keep-placement keeps, or inside the library itself, cannot be shown
// not to lead into what the repair replaces. The library's refuses every
// repair that writes a place, naming the directory and, where a link led
// there, the link, and the kept directory's refuses --keep-placement,
// whose content it would become; --keep-library does not copy it and
// still lands.
func TestSkillRepairRefusesWhatItCannotFollow(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	cannotFollow := func(library, dir string) string {
		return "the links in the library " + library + " cannot all be followed, so it cannot be shown that none of them leads into what the repair replaces, " +
			"and nothing was repaired: open " + dir + ": permission denied"
	}
	t.Run("through a link of the library", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, _ := repairHarness(t)
		displace(t, lib, claude, true)
		dev := filepath.Join(h.home, "dev", "beta")
		writeFile(t, mkdirs(t, dev, "SKILL.md"), skill("beta", "A skill of my own"))
		beta := filepath.Join(h.library, "beta")
		link(t, dev, beta)
		chmod(t, dev, 0)
		t.Cleanup(func() { _ = os.Chmod(dev, 0o755) }) // so the temporary home can be removed
		version := mutationVersion(t, h)
		for _, flag := range []string{"--keep-library", "--keep-placement"} {
			out := h.run("--json", "skill", "repair", "alpha", flag)
			equal(t, "exit "+flag, out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message "+flag, e["message"], cannotFollow(h.library, dev))
			equal(t, "hint "+flag, e["hint"], "make the directory it names readable, or replace the link "+beta+
				" with the files it leads to, then run 'agentx skill repair alpha' again")
		}
		chmod(t, dev, 0o755)
		if _, ok := isSymlink(t, claude); ok {
			t.Fatal("the displaced directory was replaced")
		}
		equal(t, "no mutation", mutationVersion(t, h), version)
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
	})
	t.Run("inside another skill's library directory", func(t *testing.T) {
		t.Parallel()
		h, lib, _, cursor := repairHarness(t)
		remove(t, cursor)
		private := filepath.Join(h.library, "beta", "private")
		writeFile(t, mkdirs(t, filepath.Join(h.library, "beta"), "SKILL.md"), skill("beta", "A skill of my own"))
		writeFile(t, mkdirs(t, private, "x.md"), "a file of beta's own\n")
		chmod(t, private, 0)
		t.Cleanup(func() { _ = os.Chmod(private, 0o755) })
		version := mutationVersion(t, h)
		out := h.run("--json", "skill", "repair", "alpha")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], cannotFollow(h.library, private))
		equal(t, "hint", e["hint"], "make the directory it names readable, then run 'agentx skill repair alpha' again")
		if _, err := os.Lstat(cursor); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("cursor's missing placement was made: %v", err)
		}
		equal(t, "no mutation", mutationVersion(t, h), version)

		chmod(t, private, 0o755)
		h.mustRun("skill", "repair", "alpha")
		linksToLibrary(t, "cursor's placement", cursor, lib)
		cleanAfterRepair(t, h, h.library, filepath.Dir(cursor))
	})
	t.Run("through a link of the kept directory", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, _ := repairHarness(t)
		displace(t, lib, claude, true)
		ext := filepath.Join(h.home, "ext")
		writeFile(t, mkdirs(t, ext, "notes.md"), "notes kept outside the skill\n")
		link(t, ext, filepath.Join(claude, "ext"))
		chmod(t, ext, 0)
		t.Cleanup(func() { _ = os.Chmod(ext, 0o755) })
		library := onDisk(t, lib)
		out := h.run("--json", "skill", "repair", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		message := e["message"].(string)
		if !strings.HasPrefix(message, "the links in "+claude+" cannot all be followed, so its content cannot become the library's: ") || !strings.Contains(message, ext) {
			t.Errorf("message = %q, want one naming %s and %s", message, claude, ext)
		}
		equal(t, "hint", e["hint"], "replace the links in "+claude+" that lead outside it with the files they lead to, then run "+
			"'agentx skill repair alpha --keep-placement' again, or keep the library's content with 'agentx skill repair alpha --keep-library'")
		equal(t, "the library directory", onDisk(t, lib), library)
		cleanAfterRepair(t, h, h.library, filepath.Dir(claude))

		h.mustRun("skill", "repair", "alpha", "--keep-library")
		linksToLibrary(t, "claude's placement", claude, lib)
		equal(t, "the library directory after --keep-library", onDisk(t, lib), library)
	})
}

// TestReachFollowsLinksAndStops holds the walk behind the repair's guards
// to its three promises: a link inside a directory another link leads to
// is found and named through that link, a directory is walked once however
// many links lead to it, round in circles or back to where the walk
// started, and past its limit the walk gives up rather than read on. The
// library's walk skips only what a mutation staged beside its entries.
func TestReachFollowsLinksAndStops(t *testing.T) {
	t.Parallel()
	real := func(dir string) string {
		t.Helper()
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}
	root, outside := real(t.TempDir()), real(t.TempDir())
	writeFile(t, mkdirs(t, filepath.Join(outside, "deep"), "x.md"), "x\n")
	link(t, outside, filepath.Join(root, "out"))
	link(t, root, filepath.Join(outside, "back"))
	link(t, outside, filepath.Join(outside, "deep", "self"))
	link(t, filepath.Join(outside, "deep"), filepath.Join(root, "deep"))
	link(t, filepath.Join(root, "nowhere"), filepath.Join(root, "dangling"))

	links, err := reach(root, "lib", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, l := range links {
		names = append(names, l.path)
	}
	// outside/deep is reached through lib/deep first, so the walk of
	// outside, reached through lib/out, does not go into it again.
	equal(t, "the links reached", strings.Join(names, " "), strings.Join([]string{
		filepath.Join("lib", "deep"), filepath.Join("lib", "out"),
		filepath.Join("lib", "deep", "self"), filepath.Join("lib", "out", "back"),
	}, " "))

	_, err = reach(root, "lib", 2, nil)
	var far *reachLimitError
	if !errors.As(err, &far) {
		t.Fatalf("a walk past its limit: %v, want a reachLimitError", err)
	}
	equal(t, "the link past the limit", far.link, filepath.Join("lib", "deep"))

	staged := mkdirs(t, filepath.Join(root, ".agentx-staged-1-0"), "in-staging")
	link(t, outside, staged)
	nested := mkdirs(t, filepath.Join(root, "beta", ".agentx-staged-mine"), "kept")
	link(t, outside, nested)
	links, err = libraryLinks(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, l := range links {
		found[l.path] = true
	}
	equal(t, "a link in a skill's directory named like staging", found[nested], true)
	equal(t, "a link in what a mutation staged", found[staged], false)
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

// TestSkillRepairListsRowsInDetectionOrder: Windsurf's skills directory
// made a symlink to Claude Code's makes their places one, repaired once,
// and the rows of the text still follow the order the configurations are
// detected in, not the order the places are.
func TestSkillRepairListsRowsInDetectionOrder(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--to", "windsurf")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	copilot := filepath.Join(h.home, ".copilot", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	remove(t, filepath.Dir(windsurf))
	link(t, filepath.Dir(claude), filepath.Dir(windsurf))
	displace(t, lib, claude, false)

	row := func(id, action, path string) string {
		return fmt.Sprintf("  %-14s  %-8s  symlink  %s -> %s\n", id, action, path, lib)
	}
	equal(t, "the text", h.mustRun("skill", "repair", "alpha").stdout, "✓ repaired alpha in 4 configurations\n"+
		row("claude-code", "relinked", claude)+
		row("cursor", "placed", cursor)+
		row("github-copilot", "placed", copilot)+
		row("windsurf", "relinked", windsurf))
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(copilot))
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
// the displaced directory is edited, the library entry becomes a symlink
// to the same content, which --keep-placement refuses to replace, another
// skill's library entry comes to lead into the displaced directory, which
// every repair refuses to replace, a link in the displaced directory comes
// to lead into the library, which --keep-placement refuses to copy, or the
// settings disable the configuration. The repair then refuses before it
// writes a journal and changes nothing, and the change is there
// afterwards.
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
		{"the library entry becomes a symlink", "--keep-placement", changed, "library", func(t *testing.T, _ *harness, _, lib, _ string) (string, func(*testing.T)) {
			// The same content, moved behind a link: no tree changes, and
			// only the library entry, read again, tells the two apart.
			dev := filepath.Join(t.TempDir(), "dev-alpha")
			mv, err := exec.LookPath("mv")
			if err != nil {
				t.Fatal(err)
			}
			ln, err := exec.LookPath("ln")
			if err != nil {
				t.Fatal(err)
			}
			return mv + " " + shellWord(lib) + " " + shellWord(dev) + " && " + ln + " -s " + shellWord(dev) + " " + shellWord(lib), func(t *testing.T) {
				target, ok := isSymlink(t, lib)
				if !ok || target != dev {
					t.Fatalf("library entry: link %v to %q, want a link to %q", ok, target, dev)
				}
			}
		}},
		{"the displaced directory is edited", "--keep-library", changed, "place", func(t *testing.T, _ *harness, _, _, claude string) (string, func(*testing.T)) {
			file := filepath.Join(claude, "notes.md")
			return "printf 'an edit made meanwhile\\n' > " + shellWord(file), func(t *testing.T) {
				equal(t, "the directory's notes.md", fileBody(t, file), "an edit made meanwhile\n")
			}
		}},
		{"a library entry comes to lead into the displaced directory", "--keep-library", changed, "place", func(t *testing.T, h *harness, _, _, claude string) (string, func(*testing.T)) {
			// The directory is there from the first read; only the link
			// that makes it another skill's content comes later.
			inside := filepath.Join(claude, "beta")
			writeFile(t, mkdirs(t, inside, "SKILL.md"), skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			ln, err := exec.LookPath("ln")
			if err != nil {
				t.Fatal(err)
			}
			return ln + " -s " + shellWord(inside) + " " + shellWord(beta), func(t *testing.T) {
				linksToLibrary(t, "beta's library entry", beta, inside)
				equal(t, "beta's SKILL.md", fileBody(t, filepath.Join(inside, "SKILL.md")), skill("beta", "A skill of my own"))
			}
		}},
		{"a link in the displaced directory comes to lead into the library", "--keep-placement", changed, "place", func(t *testing.T, h *harness, _, lib, claude string) (string, func(*testing.T)) {
			// The link is there from the first read and leads outside the
			// library; only what it leads through changes, so no tree does.
			through := filepath.Join(h.home, "shared-notes.md")
			writeFile(t, through, "notes kept outside the skill\n")
			shared := filepath.Join(claude, "shared.md")
			link(t, through, shared)
			ln, err := exec.LookPath("ln")
			if err != nil {
				t.Fatal(err)
			}
			notes := filepath.Join(lib, "notes.md")
			return ln + " -sf " + shellWord(notes) + " " + shellWord(through), func(t *testing.T) {
				linksToLibrary(t, "what the link leads through", through, notes)
				linksToLibrary(t, "the link in the displaced directory", shared, through)
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
// A link in what was staged in the library, as a copy of a displaced
// directory holds, is no link of the library's and refuses nothing.
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
				link(t, filepath.Join(claude, "notes.md"), filepath.Join(h.library, ".agentx-staged-deadbeef-1", "notes.md"))
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
