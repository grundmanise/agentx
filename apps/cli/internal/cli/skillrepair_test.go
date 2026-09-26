package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

// TestSkillRepairJudgesASharedPlaceOnce: Zencoder and Zenflow read one
// skills directory, and copy_mode records a copy for Zenflow alone. The
// copy both read is repaired once, as the copy the settings name, and
// counted for both configurations; copy_mode is left as it is.
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

// TestSkillRepairRefusesWhatIsNotAManagedSkill: drift is judged for a
// managed skill alone, so a repair refuses every other name, each with its
// own way on: an unmanaged skill, a fork, a managed skill whose library
// directory is gone, and a name the library does not hold at all.
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
		h, _, _, _ := repairHarness(t)
		commit := h.accountGit("rev-parse", "refs/heads/managed/alpha")
		h.accountGit("update-ref", "refs/heads/skills/alpha", commit)
		h.accountGit("update-ref", "-d", "refs/heads/managed/alpha")
		out := h.run("--json", "skill", "repair", "alpha")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], "alpha is a fork on this machine")
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

	t.Run("a name the library does not hold", func(t *testing.T) {
		t.Parallel()
		h, _ := installHarness(t)
		out := h.run("--json", "skill", "repair", "nothing")
		equal(t, "exit", out.exit, 5)
		contains(t, "message", h.one(out.stdout, "error")["message"].(string), `the library holds no skill called "nothing"`)
	})
}

// TestSkillRepairRefusesAChangeMadeWhileItRuns: what a repair discards is
// what the paths held when it judged them. A git wrapper edits the
// displaced directory once the repair holds the lock, at the read of the
// import branch; the repair then finds the plan changed, refuses and
// changes nothing, and the edit is there afterwards.
func TestSkillRepairRefusesAChangeMadeWhileItRuns(t *testing.T) {
	t.Parallel()
	h, lib, claude, _ := repairHarness(t)
	displace(t, lib, claude, true)
	file := filepath.Join(claude, "notes.md")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// The ref read under the lock asks for the refs by name, with a format
	// that ends in the object name; the listing of every branch asks for
	// trees and trailers too.
	stubGit(t, h, `#!/bin/sh
case " $* " in
*"%(objectname) refs/heads/managed/alpha "*) printf 'an edit made meanwhile\n' > `+shellWord(file)+` ;;
esac
exec `+real+` "$@"
`)
	out := h.run("--json", "skill", "repair", "alpha", "--keep-library")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "alpha or its placements changed while it was being repaired, so nothing was changed")
	equal(t, "hint", e["hint"], "run 'agentx skill repair alpha' again")
	equal(t, "notes.md", fileBody(t, file), "an edit made meanwhile\n")
	cleanAfterRepair(t, h, h.library, filepath.Dir(claude))
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
