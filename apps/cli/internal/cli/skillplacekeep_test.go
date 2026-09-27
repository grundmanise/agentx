package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// everywhereHarness is installHarness with alpha installed everywhere: a
// symlink from Claude Code's skills directory and from Cursor's, and the
// library entry itself for Codex and Gemini CLI.
func everywhereHarness(t *testing.T) (h *harness, lib, claude, cursor string) {
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

// everyRow is the text skill place prints for alpha in everywhereHarness
// once every placement is the library's symlink, below its first line:
// one row per configuration and the universal clients.
func everyRow(lib, claude, cursor string) string {
	return "  claude-code  symlink  " + claude + " -> " + lib + "\n" +
		"  codex        library  " + lib + "\n" +
		"  cursor       symlink  " + cursor + " -> " + lib + "\n" +
		"  gemini-cli   library  " + lib + "\n"
}

// universalLine is the line that ends the text of skill place in
// everywhereHarness and placementHarness, naming the universal clients.
const universalLine = "  always available to universal clients: codex, gemini-cli\n"

// universalClauseOf is how the summary of skill place ends in
// everywhereHarness and placementHarness.
const universalClauseOf = "; always available to universal clients: codex, gemini-cli"

// cleanAfterPlace holds a run of skill place to what it leaves behind: no
// journal and nothing staged or retained beside any of dirs.
func cleanAfterPlace(t *testing.T, h *harness, dirs ...string) {
	t.Helper()
	equal(t, "journals", journalCount(t, h), 0)
	for _, dir := range dirs {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
}

// placesNothing runs skill place with args and holds it to changing no
// file: the home directory, the library and the settings hold what they
// held, byte for byte and link for link, and no journal is left. It
// returns the text.
func placesNothing(t *testing.T, h *harness, args ...string) string {
	t.Helper()
	home, library := onDisk(t, h.home), onDisk(t, h.library)
	settings := fileBody(t, filepath.Join(h.agentx, "settings.json"))
	out := h.mustRun(append([]string{"skill", "place"}, args...)...)
	run := "skill place " + strings.Join(args, " ")
	equal(t, "the home directory after "+run, onDisk(t, h.home), home)
	equal(t, "the library after "+run, onDisk(t, h.library), library)
	equal(t, "the settings after "+run, fileBody(t, filepath.Join(h.agentx, "settings.json")), settings)
	equal(t, "journals after "+run, journalCount(t, h), 0)
	return out.stdout
}

// TestSkillPlacePutsBackMissingPlacements deletes two placements by hand:
// Claude Code's symlink and the copy copy_mode records for Copilot. skill
// place without --to makes both again as they were, a symlink and a copy of
// the library, leaves the settings as they are and every other placement
// alone, and reports on every enabled configuration. Placing again then
// changes nothing.
func TestSkillPlacePutsBackMissingPlacements(t *testing.T) {
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
	equal(t, "drift before skill place", drift(h.listed("alpha")), "missing")
	version := mutationVersion(t, h)

	out := h.mustRun("--json", "skill", "place", "alpha")
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
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 6 configurations, 1 placement as copy"+universalClauseOf)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	equal(t, "one mutation", mutationVersion(t, h), version+1)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(copilot))

	equal(t, "drift after skill place", drift(h.listed("alpha")), "")
	contains(t, "placing again", placesNothing(t, h, "alpha"), "✓ placed alpha in 6 configurations\n")
}

// TestSkillPlaceAdoptsADirectoryHoldingTheLibrarysContent: a directory
// that replaced a symlink and holds exactly what the library holds loses
// nothing to the symlink, so no flag is needed: it is adopted, as an
// install adopts it, and Cursor's missing symlink is placed beside it.
func TestSkillPlaceAdoptsADirectoryHoldingTheLibrarysContent(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := everywhereHarness(t)
	displace(t, lib, claude, false)
	remove(t, cursor)
	equal(t, "drift before skill place", drift(h.listed("alpha")), "displaced,missing")

	out := h.mustRun("skill", "place", "alpha")
	equal(t, "the text", out.stdout, "✓ placed alpha in 4 configurations\n"+everyRow(lib, claude, cursor)+
		"  adopted "+claude+"\n"+universalLine)
	equal(t, "stderr", out.stderr, "")
	linksToLibrary(t, "claude's placement", claude, lib)
	linksToLibrary(t, "cursor's placement", cursor, lib)
	ev := h.listed("alpha")
	equal(t, "drift after skill place", drift(ev), "")
	equal(t, "state after skill place", ev["state"], stateCurrent)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
	equal(t, "placing again", placesNothing(t, h, "alpha"), "✓ placed alpha in 4 configurations\n"+everyRow(lib, claude, cursor)+universalLine)
}

// TestSkillPlaceRefusesADirectoryThatDiffers: a displaced directory whose
// content is not the library's is one of two versions of the skill, and
// replacing it without a flag would discard one of them. skill place
// refuses, names both flags and changes nothing at all, the missing
// placement it could have made included. A file made executable is a
// difference too, as it is to the state of the skill.
func TestSkillPlaceRefusesADirectoryThatDiffers(t *testing.T) {
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
			h, lib, claude, cursor := everywhereHarness(t)
			displace(t, lib, claude, false)
			c.edit(t, claude)
			remove(t, cursor)
			held := libraryTree(t, claude)

			out := h.run("--json", "skill", "place", "alpha")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], claude+" is a directory whose content differs from the library's alpha, so nothing was placed")
			equal(t, "hint", e["hint"], "to keep the library's content and discard it, run 'agentx skill place alpha --keep-library';"+
				" to make its content the library's, run 'agentx skill place alpha --keep-placement'")
			if _, ok := isSymlink(t, claude); ok {
				t.Fatal("claude's directory was replaced")
			}
			sameTree(t, "claude's directory", libraryTree(t, claude), held)
			nothingAt(t, "cursor's placement", cursor)
			equal(t, "drift", drift(h.listed("alpha")), "displaced,missing")
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
		})
	}

	t.Run("two of them", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, cursor := everywhereHarness(t)
		displace(t, lib, claude, true)
		displace(t, lib, cursor, true)
		out := h.run("--json", "skill", "place", "alpha")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], claude+", "+cursor+" are directories whose content differs from the library's alpha, so nothing was placed")
		equal(t, "hint", e["hint"], "to keep the library's content and discard them, run 'agentx skill place alpha --keep-library';"+
			" to make their content the library's, run 'agentx skill place alpha --keep-placement'")
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
	})
}

// TestSkillPlaceComparesWhatGitCannotRecordByteForByte: a library skill
// holding a repository of its own and a displaced directory copied from it
// hold the same content, though no tree records the repository. The two
// are compared byte for byte instead, so the directory is adopted without
// a flag, as any directory holding the library's content is. A directory
// whose repository differs is a directory that differs, and skill place
// refuses.
func TestSkillPlaceComparesWhatGitCannotRecordByteForByte(t *testing.T) {
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
			h, lib, claude, _ := everywhereHarness(t)
			writeFile(t, mkdirs(t, filepath.Join(lib, "sub", ".git"), "HEAD"), "ref: refs/heads/main\n")
			displace(t, lib, claude, false)
			if !c.same {
				writeFile(t, filepath.Join(claude, "sub", ".git", "HEAD"), "ref: refs/heads/other\n")
			}
			held := libraryTree(t, claude)

			out := h.run("--json", "skill", "place", "alpha")
			if c.same {
				equal(t, "exit", out.exit, 0)
				linksToLibrary(t, "claude's placement", claude, lib)
				equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 4 configurations, 1 placement adopted"+universalClauseOf)
				equal(t, "drift after skill place", drift(h.listed("alpha")), "")
			} else {
				equal(t, "exit", out.exit, 6)
				equal(t, "message", h.one(out.stdout, "error")["message"],
					claude+" is a directory whose content differs from the library's alpha, so nothing was placed")
				if _, ok := isSymlink(t, claude); ok {
					t.Fatal("claude's directory was replaced")
				}
				sameTree(t, "claude's directory", libraryTree(t, claude), held)
			}
			equal(t, "the library's repository", fileBody(t, filepath.Join(lib, "sub", ".git", "HEAD")), "ref: refs/heads/main\n")
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// TestSkillPlaceKeepLibraryDiscardsTheDirectory: --keep-library is the
// explicit choice to discard a displaced directory that differs. The
// symlink replaces it, the library is untouched, the skill stays current,
// and the run names what it discarded.
func TestSkillPlaceKeepLibraryDiscardsTheDirectory(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := everywhereHarness(t)
	base := libraryTree(t, lib)
	displace(t, lib, claude, true)

	out := h.mustRun("--json", "skill", "place", "alpha", "--keep-library")
	linksToLibrary(t, "claude's placement", claude, lib)
	sameTree(t, "the library directory", libraryTree(t, lib), base)
	linksToLibrary(t, "cursor's placement", cursor, lib)
	ev := h.one(out.stdout, "library_skill")
	equal(t, "drift", drift(ev), "")
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 4 configurations; discarded what "+claude+" held"+universalClauseOf)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude))

	displace(t, lib, claude, true)
	text := h.mustRun("skill", "place", "alpha", "--keep-library").stdout
	equal(t, "the text", text, "✓ placed alpha in 4 configurations\n"+everyRow(lib, claude, cursor)+
		"  discarded what "+claude+" held\n"+universalLine)
	equal(t, "placing again", placesNothing(t, h, "alpha", "--keep-library"), "✓ placed alpha in 4 configurations\n"+everyRow(lib, claude, cursor)+universalLine)
	equal(t, "drift after placing again", drift(h.listed("alpha")), "")
}

// TestSkillPlaceKeepPlacementMakesItTheLibrary: --keep-placement is the
// explicit choice to keep a displaced directory that differs. Its content
// becomes the library's, so the skill is modified against its base
// version, and the symlink replaces it. Every other placement follows the
// library as a revert makes it follow: a copy that held the library's
// content is refreshed, a copy edited where it is is kept, named and
// counted as skipped, and a missing copy is made from the content kept.
func TestSkillPlaceKeepPlacementMakesItTheLibrary(t *testing.T) {
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
	equal(t, "drift before skill place", drift(h.listed("alpha")), "displaced,missing")

	out := h.mustRun("--json", "skill", "place", "alpha", "--keep-placement")
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
		"placed alpha in 5 configurations, 1 placement as copy, 1 copy placement refreshed, 1 placement skipped; the library now holds what "+claude+" held"+universalClauseOf)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor,github-copilot,windsurf")
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(windsurf), filepath.Dir(copilot))

	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "alpha").stdout, "diff")
	var changed []string
	for _, d := range diffs {
		changed = append(changed, d["path"].(string)+" "+d["status"].(string))
	}
	equal(t, "the diff against the base", strings.Join(changed, ", "), "mine.md added, notes.md modified")
	contains(t, "placing again", placesNothing(t, h, "alpha", "--keep-placement"), "✓ placed alpha in 5 configurations, 1 placement skipped\n")
	equal(t, "drift after placing again", drift(h.listed("alpha")), "")
}

// TestSkillPlaceKeepPlacementJudgesWhatGitCannotRecordByteForByte: a
// library holding a repository of its own leaves one in every copy placed
// from it, which no tree records. --keep-placement still refreshes such a
// copy when it holds exactly what the library directory held, judged byte
// for byte, and keeps the copy whose repository was edited where it is.
func TestSkillPlaceKeepPlacementJudgesWhatGitCannotRecordByteForByte(t *testing.T) {
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

	out := h.mustRun("--json", "skill", "place", "alpha", "--keep-placement")
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
		"placed alpha in 5 configurations, 1 copy placement refreshed, 1 placement skipped; the library now holds what "+claude+" held"+universalClauseOf)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(windsurf))
}

// TestSkillPlaceKeepPlacementRefreshesACopyOfTheBase: copies placed
// before the library was edited still hold the base version, which is what
// agentx placed there, so --keep-placement refreshes them with the content
// kept, as it refreshes a copy of what the library held. A copy edited
// where it is beside them is still kept byte for byte, and named.
func TestSkillPlaceKeepPlacementRefreshesACopyOfTheBase(t *testing.T) {
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
	equal(t, "state before skill place", h.listed("alpha")["state"], stateModified)
	displace(t, lib, claude, true)
	kept := libraryTree(t, claude)

	out := h.mustRun("--json", "skill", "place", "alpha", "--keep-placement")
	sameTree(t, "the library directory", libraryTree(t, lib), kept)
	linksToLibrary(t, "claude's placement", claude, lib)
	sameTree(t, "windsurf's copy of the base", libraryTree(t, windsurf), kept)
	sameTree(t, "copilot's copy of the base", libraryTree(t, copilot), kept)
	sameTree(t, "cursor's edited copy", libraryTree(t, cursor), edited)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		keptCopyWarning(cursor, "alpha", "agentx skill remove alpha --from cursor", "agentx skill place alpha --to cursor --copy"))
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"placed alpha in 5 configurations, 2 copy placements refreshed, 1 placement skipped; the library now holds what "+claude+" held"+universalClauseOf)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(windsurf), filepath.Dir(copilot))
}

// TestSkillPlaceKeepPlacementOfTheBaseLeavesItCurrent: a displaced
// directory holding the skill's base version, kept with --keep-placement
// while the library is edited, makes the library hold the base again, so
// the skill is current afterwards, not modified. A copy that held what
// the library held is refreshed with it, and a copy that already holds the
// base is left alone and counted as placed, not as refreshed.
func TestSkillPlaceKeepPlacementOfTheBaseLeavesItCurrent(t *testing.T) {
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
	equal(t, "state before skill place", ev["state"], stateModified)
	equal(t, "drift before skill place", drift(ev), "displaced")
	cursorBefore, err := os.Lstat(cursor)
	if err != nil {
		t.Fatal(err)
	}

	out := h.mustRun("--json", "skill", "place", "alpha", "--keep-placement")
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
		"placed alpha in 6 configurations, 1 copy placement refreshed; the library now holds what "+claude+" held"+universalClauseOf)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor), filepath.Dir(windsurf), filepath.Dir(copilot))
}

// TestSkillPlaceKeepPlacementText: the text is the one skill place always
// prints, a row per configuration and the directories it adopted, and it
// names the directory the library now holds the content of below them,
// above the universal clients.
func TestSkillPlaceKeepPlacementText(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := everywhereHarness(t)
	displace(t, lib, cursor, false)
	displace(t, lib, claude, true)
	equal(t, "the text", h.mustRun("skill", "place", "alpha", "--keep-placement").stdout, "✓ placed alpha in 4 configurations\n"+
		everyRow(lib, claude, cursor)+
		"  adopted "+cursor+"\n"+
		"  the library now holds what "+claude+" held\n"+
		universalLine)
}

// TestSkillPlaceKeepPlacementRefusals: --keep-placement makes one
// directory's content the library's, so it refuses what would leave the
// library holding something else. Nothing is changed by any of them.
func TestSkillPlaceKeepPlacementRefusals(t *testing.T) {
	t.Parallel()
	t.Run("two directories that differ from each other", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, cursor := everywhereHarness(t)
		displace(t, lib, claude, true)
		displace(t, lib, cursor, false)
		writeFile(t, filepath.Join(cursor, "notes.md"), "another edit\n")
		out := h.run("--json", "skill", "place", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "--keep-placement keeps the content of one directory, and "+claude+", "+cursor+" hold different content")
		contains(t, "hint", e["hint"].(string), "move aside every directory but the one to keep")
		equal(t, "cursor's edit", fileBody(t, filepath.Join(cursor, "notes.md")), "another edit\n")
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))

		// Two directories holding the same content are one content to keep.
		displace(t, lib, cursor, true)
		h.mustRun("skill", "place", "alpha", "--keep-placement")
		linksToLibrary(t, "claude's placement", claude, lib)
		linksToLibrary(t, "cursor's placement", cursor, lib)
		equal(t, "the library's notes.md", fileBody(t, filepath.Join(lib, "notes.md")), "alpha notes, edited in the displaced directory\n")
	})

	t.Run("a directory with no SKILL.md", func(t *testing.T) {
		t.Parallel()
		h, _, claude, _ := everywhereHarness(t)
		remove(t, claude)
		writeFile(t, mkdirs(t, claude, "notes.md"), "not a skill\n")
		out := h.run("--json", "skill", "place", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], claude+" holds no SKILL.md, so its content cannot become the library's alpha")
		equal(t, "the directory", fileBody(t, filepath.Join(claude, "notes.md")), "not a skill\n")
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
	})

	t.Run("a directory holding a repository", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, _ := everywhereHarness(t)
		displace(t, lib, claude, true)
		writeFile(t, mkdirs(t, filepath.Join(claude, "vendored", ".git"), "HEAD"), "ref: refs/heads/main\n")
		out := h.run("--json", "skill", "place", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		equal(t, "message", h.one(out.stdout, "error")["message"], claude+" holds what git cannot record, so its content cannot become the library's")
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
	})
}

// keptLinkRefusal is the message and hint of skill place refusing to make
// the content of dir the library's because it holds the symlink link.
func keptLinkRefusal(dir, link string) (string, string) {
	return dir + " holds the symlink " + link + ", so its content cannot become the library's",
		"replace the link with the files it leads to, then run 'agentx skill place alpha --keep-placement' again, " +
			"or keep the library's content with 'agentx skill place alpha --keep-library'"
}

// TestSkillPlaceKeepPlacementRefusesADirectoryHoldingASymlink: the kept
// directory is copied into the library link for link, so a symlink anywhere
// in it would lead from the library into what the run replaces, the
// library directory, another displaced directory or a copy, or through one
// of them and out by "..", or, spelled relative, elsewhere than it did.
// Rather than follow each link to find out, --keep-placement refuses a
// directory holding any symlink at any depth, one that leads somewhere
// harmless included, names it, and changes nothing. --keep-library, which
// copies nothing, places the skill and leaves the library as it was.
func TestSkillPlaceKeepPlacementRefusesADirectoryHoldingASymlink(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, link string // the link, relative to Claude Code's displaced directory
		// arrange sets the machine up and returns what the link names.
		arrange func(t *testing.T, h *harness, lib, claude, cursor string) string
	}{
		{"a link into the library directory", "scripts", func(t *testing.T, _ *harness, lib, _, _ string) string {
			return filepath.Join(lib, "scripts")
		}},
		{"a link to the library's SKILL.md", "SKILL.md", func(t *testing.T, _ *harness, lib, _, _ string) string {
			return filepath.Join(lib, "SKILL.md")
		}},
		{"a relative link into the library", "scripts", func(t *testing.T, _ *harness, lib, claude, _ string) string {
			rel, err := filepath.Rel(claude, filepath.Join(lib, "scripts"))
			if err != nil {
				t.Fatal(err)
			}
			return rel
		}},
		{"a link into another displaced directory", "scripts", func(t *testing.T, _ *harness, lib, _, cursor string) string {
			displace(t, lib, cursor, false)
			return filepath.Join(cursor, "scripts")
		}},
		{"a link into a copy placement", "scripts", func(t *testing.T, h *harness, _, _, cursor string) string {
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			return filepath.Join(cursor, "scripts")
		}},
		{"a link to a directory outside holding a link into the library", "ext", func(t *testing.T, h *harness, lib, _, _ string) string {
			outside := filepath.Join(h.home, "ext")
			link(t, filepath.Join(lib, "scripts"), mkdirs(t, outside, "y"))
			return outside
		}},
		{"a link through another displaced directory and out by '..'", "ref", func(t *testing.T, h *harness, lib, _, cursor string) string {
			displace(t, lib, cursor, false)
			writeFile(t, mkdirs(t, filepath.Join(h.home, ".cursor", "vendor"), "x.md"), "notes kept outside the skill\n")
			return cursor + "/../../vendor"
		}},
		{"a link through the directory itself and out by '..'", "ref", func(t *testing.T, h *harness, _, claude, _ string) string {
			writeFile(t, mkdirs(t, filepath.Join(h.home, ".claude", "vendor"), "x.md"), "notes kept outside the skill\n")
			return claude + "/../../vendor"
		}},
		{"an absolute link to a file outside", "shared.md", func(t *testing.T, h *harness, _, _, _ string) string {
			outside := filepath.Join(h.home, "shared-notes.md")
			writeFile(t, outside, "notes kept outside the skill\n")
			return outside
		}},
		{"a link to a sibling in the client's skills directory", "sibling", func(t *testing.T, _ *harness, _, claude, _ string) string {
			sibling := filepath.Join(filepath.Dir(claude), "shared-notes")
			writeFile(t, mkdirs(t, sibling, "x.md"), "notes beside the skill\n")
			return sibling
		}},
		{"a relative link that stays inside it", "run", func(t *testing.T, _ *harness, _, _, _ string) string {
			return filepath.Join("scripts", "run.sh")
		}},
		{"a relative link out of it and in again by its name", "again.md", func(t *testing.T, _ *harness, _, _, _ string) string {
			return filepath.Join("..", "alpha", "mine.md")
		}},
		{"a relative link out of it", "notes", func(t *testing.T, _ *harness, _, claude, _ string) string {
			writeFile(t, mkdirs(t, filepath.Join(filepath.Dir(claude), "notes-of-mine"), "n.md"), "my notes\n")
			return filepath.Join("..", "notes-of-mine")
		}},
		{"a relative link out of it, deep inside it", filepath.Join("scripts", "notes"), func(t *testing.T, _ *harness, _, claude, _ string) string {
			writeFile(t, mkdirs(t, filepath.Join(filepath.Dir(claude), "notes-of-mine"), "n.md"), "my notes\n")
			return filepath.Join("..", "..", "notes-of-mine")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			displace(t, lib, claude, true)
			target := c.arrange(t, h, lib, claude, cursor)
			at := filepath.Join(claude, c.link)
			remove(t, at)
			link(t, target, at)
			library := onDisk(t, lib)

			message, hint := keptLinkRefusal(claude, at)
			refusesUntouched(t, h, []string{"--keep-placement"}, message, hint)
			if got, ok := isSymlink(t, at); !ok || got != target {
				t.Errorf("the link in claude's directory: link %v to %q, want a link to %q", ok, got, target)
			}
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))

			// Told to keep the library's content, skill place discards the
			// directory, link and all, and the library keeps its files.
			h.mustRun("skill", "place", "alpha", "--keep-library")
			linksToLibrary(t, "claude's placement", claude, lib)
			equal(t, "the library directory after --keep-library", onDisk(t, lib), library)
			equal(t, "the library's run.sh", fileBody(t, filepath.Join(lib, "scripts", "run.sh")), "#!/bin/sh\n")
		})
	}
}

// TestSkillPlaceKeepPlacementIgnoresALibraryLinkOutOfTheLibraryDirectory:
// a library link that enters alpha's library directory by name and leaves
// it by ".." does not pass through it once --keep-placement replaced it:
// the library directory is a real directory again, whose ".." is where it
// was, and the link still leads where it did.
func TestSkillPlaceKeepPlacementIgnoresALibraryLinkOutOfTheLibraryDirectory(t *testing.T) {
	t.Parallel()
	h, lib, claude, _ := everywhereHarness(t)
	displace(t, lib, claude, true)
	writeFile(t, mkdirs(t, filepath.Join(h.library, "gamma"), "SKILL.md"), skill("gamma", "A skill of my own"))
	delta := filepath.Join(h.library, "delta")
	link(t, lib+"/../gamma", delta)

	h.mustRun("skill", "place", "alpha", "--keep-placement")
	linksToLibrary(t, "claude's placement", claude, lib)
	equal(t, "what delta leads to", fileBody(t, filepath.Join(delta, "SKILL.md")), skill("gamma", "A skill of my own"))
	equal(t, "alpha's state", h.listed("alpha")["state"], stateModified)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
}

// TestSkillPlaceKeepPlacementCopiesTheExecBitGitRecords: git records a
// file as executable by its owner's bit alone, and so does every copy
// skill place makes: a file only others may run is laid out in the library
// and in a refreshed copy as a plain file, so each holds the tree it was
// judged to hold, and the run lands.
func TestSkillPlaceKeepPlacementCopiesTheExecBitGitRecords(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	h.mustRun("skill", "place", "alpha", "--to", "windsurf", "--copy")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
	displace(t, lib, claude, true)
	chmod(t, filepath.Join(claude, "mine.md"), 0o645)

	out := h.mustRun("--json", "skill", "place", "alpha", "--keep-placement")
	for _, dir := range []string{lib, windsurf} {
		if executable(t, filepath.Join(dir, "mine.md")) {
			t.Errorf("mine.md in %s is executable, want the plain file git records", dir)
		}
		if !executable(t, filepath.Join(dir, "scripts", "run.sh")) {
			t.Errorf("scripts/run.sh in %s is not executable", dir)
		}
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"placed alpha in 6 configurations, 1 copy placement refreshed; the library now holds what "+claude+" held"+universalClauseOf)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(windsurf))
}

// TestSkillPlaceRefreshesALinkedCopyOnce: Cursor's skills directory made a
// symlink to Claude Code's makes the copies copy_mode records for the two
// one directory spelled two ways, and on a disk that ignores case so does
// a link that spells Claude Code's directory in another case.
// --keep-placement refreshes it once and counts it for both
// configurations: refreshed twice, the second removal would find the
// first one's copy there and stop the mutation part way.
func TestSkillPlaceRefreshesALinkedCopyOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		otherCase bool
	}{{"spelled the same", false}, {"spelled in another case", true}} {
		otherCase := c.otherCase
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if otherCase {
				ignoresCase(t)
			}
			h, s := placementHarness(t)
			h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--to", "cursor", "--copy")
			h.mustRun("skill", "place", "alpha", "--to", "windsurf")
			lib := filepath.Join(h.library, "alpha")
			claude := filepath.Join(h.home, ".claude", "skills", "alpha")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			windsurf := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
			skills := filepath.Dir(claude)
			if otherCase {
				skills = filepath.Join(inAnotherCase(filepath.Join(h.home, ".claude")), "skills")
			}
			remove(t, filepath.Dir(cursor))
			link(t, skills, filepath.Dir(cursor))
			displace(t, lib, windsurf, true)
			kept := libraryTree(t, windsurf)

			out := h.run("--json", "skill", "place", "alpha", "--keep-placement")
			if out.exit != 0 {
				t.Fatalf("skill place: exit %d\n%s", out.exit, out.stderr)
			}
			sameTree(t, "the library directory", libraryTree(t, lib), kept)
			sameTree(t, "the copy both read", libraryTree(t, claude), kept)
			linksToLibrary(t, "windsurf's placement", windsurf, lib)
			equal(t, "summary", h.one(out.stdout, "result")["summary"],
				"placed alpha in 6 configurations, 2 copy placements refreshed; the library now holds what "+windsurf+" held"+universalClauseOf)
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
			ev := h.one(out.stdout, "library_skill")
			equal(t, "state", ev["state"], stateModified)
			equal(t, "drift", drift(ev), "")
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(windsurf))
		})
	}
}

// TestSkillPlaceKeepFlagsAreExclusive: each flag keeps one of the two
// contents, so giving both is a usage error, and nothing is changed.
func TestSkillPlaceKeepFlagsAreExclusive(t *testing.T) {
	t.Parallel()
	h, lib, claude, _ := everywhereHarness(t)
	displace(t, lib, claude, true)
	out := h.run("--json", "skill", "place", "alpha", "--keep-library", "--keep-placement")
	equal(t, "exit", out.exit, 1)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "usage")
	equal(t, "message", e["message"], "--keep-library and --keep-placement cannot both be given")
	if _, ok := isSymlink(t, claude); ok {
		t.Fatal("claude's directory was replaced")
	}
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
}

// TestSkillPlaceWithoutToCoversEveryEnabledConfiguration: with no --to,
// skill place covers every enabled configuration and no other. Claude
// Code's missing placement is made, and Cursor, disabled, is left as it
// is, a directory there that differs from the library included, which
// stops nothing.
func TestSkillPlaceWithoutToCoversEveryEnabledConfiguration(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := everywhereHarness(t)
	equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)
	remove(t, claude)
	displace(t, lib, cursor, true)
	held := libraryTree(t, cursor)

	out := h.mustRun("--json", "skill", "place", "alpha")
	linksToLibrary(t, "claude's placement", claude, lib)
	sameTree(t, "cursor's directory", libraryTree(t, cursor), held)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 3 configurations"+universalClauseOf)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
}

// TestSkillPlaceKeepFlagsCombineWithTo: --to narrows skill place to the
// configurations it names, the flags included. A directory that differs
// in a configuration not named is not judged, the refusal and its hint
// name the --to given, and --keep-library then discards only the named
// configuration's directory.
func TestSkillPlaceKeepFlagsCombineWithTo(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := everywhereHarness(t)
	displace(t, lib, claude, true)
	displace(t, lib, cursor, true)
	held := libraryTree(t, claude)

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], cursor+" is a directory whose content differs from the library's alpha, so nothing was placed")
	equal(t, "hint", e["hint"], "to keep the library's content and discard it, run 'agentx skill place alpha --to cursor --keep-library';"+
		" to make its content the library's, run 'agentx skill place alpha --to cursor --keep-placement'")

	out = h.mustRun("--json", "skill", "place", "alpha", "--to", "cursor", "--keep-library")
	linksToLibrary(t, "cursor's placement", cursor, lib)
	sameTree(t, "claude's directory", libraryTree(t, claude), held)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 1 configuration; discarded what "+cursor+" held"+universalClauseOf)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
}

// TestSkillPlaceKeepFlagsWithCopyWriteACopy: with --copy, a displaced
// directory that differs is replaced by a copy, which copy_mode records:
// of the library's content with --keep-library, and of the directory's own
// with --keep-placement, which first makes it the library's.
func TestSkillPlaceKeepFlagsWithCopyWriteACopy(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--keep-library", "--keep-placement"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, _ := everywhereHarness(t)
			want := libraryTree(t, lib)
			displace(t, lib, claude, true)
			clause := "; discarded what " + claude + " held"
			if flag == "--keep-placement" {
				want = libraryTree(t, claude)
				clause = "; the library now holds what " + claude + " held"
			}

			out := h.mustRun("--json", "skill", "place", "alpha", "--to", "claude-code", "--copy", flag)
			if _, ok := isSymlink(t, claude); ok {
				t.Fatal("claude's placement is a symlink, want a copy")
			}
			sameTree(t, "the library directory", libraryTree(t, lib), want)
			sameTree(t, "claude's copy", libraryTree(t, claude), want)
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code")
			equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 1 configuration, 1 placement as copy"+clause+universalClauseOf)
			equal(t, "drift", drift(h.listed("alpha")), "")
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// TestSkillPlaceKeepPlacementWithCopyReplacesADirectoryOfTheOldLibrary: a
// displaced directory holding what the library held before --keep-placement
// is adopted, and with --copy it too is replaced by a copy of the content
// kept, never left as a directory that is not this skill.
func TestSkillPlaceKeepPlacementWithCopyReplacesADirectoryOfTheOldLibrary(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := everywhereHarness(t)
	displace(t, lib, claude, true)
	displace(t, lib, cursor, false)
	kept := libraryTree(t, claude)

	out := h.mustRun("--json", "skill", "place", "alpha", "--keep-placement", "--copy")
	sameTree(t, "the library directory", libraryTree(t, lib), kept)
	for _, place := range []string{claude, cursor} {
		if _, ok := isSymlink(t, place); ok {
			t.Fatalf("%s is a symlink, want a copy", place)
		}
		sameTree(t, "the copy at "+place, libraryTree(t, place), kept)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code,cursor")
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"placed alpha in 4 configurations, 2 placements as copy, 1 placement adopted; the library now holds what "+claude+" held"+universalClauseOf)
	equal(t, "drift", drift(h.listed("alpha")), "")
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
}

// TestSkillRepairIsNoLongerACommand: skill place puts placements back, and
// skill repair is gone.
func TestSkillRepairIsNoLongerACommand(t *testing.T) {
	t.Parallel()
	h, _, _, _ := everywhereHarness(t)
	out := h.run("skill", "repair", "alpha")
	equal(t, "exit", out.exit, 1)
	contains(t, "stderr", out.stderr, `unknown command "repair" for "agentx skill"`)
}

// TestSkillPlaceReplacesTheLibrarysLinkWithACopy: the library's own
// symlink where copy_mode records a copy is displaced. The link holds
// nothing, so skill place replaces it with a copy of the library without a
// flag, and copy_mode is left as it is.
func TestSkillPlaceReplacesTheLibrarysLinkWithACopy(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--copy")
	lib := filepath.Join(h.library, "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	remove(t, cursor)
	link(t, lib, cursor)
	equal(t, "drift before skill place", drift(h.listed("alpha")), "displaced")

	out := h.mustRun("skill", "place", "alpha")
	equal(t, "the text", out.stdout, "✓ placed alpha in 4 configurations\n"+
		"  claude-code  copy     "+filepath.Join(h.home, ".claude", "skills", "alpha")+"\n"+
		"  codex        library  "+lib+"\n"+
		"  cursor       copy     "+cursor+"\n"+
		"  gemini-cli   library  "+lib+"\n"+
		universalLine)
	if _, ok := isSymlink(t, cursor); ok {
		t.Fatal("cursor's placement is still a symlink")
	}
	sameTree(t, "cursor's copy", libraryTree(t, cursor), libraryTree(t, lib))
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code,cursor")
	equal(t, "drift after skill place", drift(h.listed("alpha")), "")
	cleanAfterPlace(t, h, h.library, filepath.Dir(cursor))
}

// TestSkillPlaceLeavesWhatIsNotDrift: a symlink of the user's to a
// directory of their own, a copy edited where it is and a disabled
// configuration's empty place are no drift, and skill place never touches
// them, whatever flag it is given: the link and the copy are named in a
// warning and counted as skipped, as an install skips them, and a disabled
// configuration is not placed into without --to. Where a missing placement
// is put back beside them, they are still left alone.
func TestSkillPlaceLeavesWhatIsNotDrift(t *testing.T) {
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
		args := []string{"alpha"}
		if flag != "" {
			args = append(args, flag)
		}
		contains(t, "skill place "+flag, placesNothing(t, h, args...), "✓ placed alpha in 3 configurations, 2 placements skipped\n")
	}
	linksToLibrary(t, "claude's own link", claude, mine)
	sameTree(t, "cursor's edited copy", libraryTree(t, cursor), edited)
	nothingAt(t, "copilot's place", copilot)

	remove(t, windsurf)
	out := h.mustRun("--json", "skill", "place", "alpha")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 3 configurations, 2 placements skipped"+universalClauseOf)
	linksToLibrary(t, "windsurf's placement", windsurf, filepath.Join(h.library, "alpha"))
	linksToLibrary(t, "claude's own link", claude, mine)
	sameTree(t, "cursor's edited copy", libraryTree(t, cursor), edited)
	nothingAt(t, "copilot's place", copilot)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		claude+" is a link of your own and was left as it is; no placement was made for claude-code\n"+
			keptCopyWarning(cursor, "alpha", "agentx skill remove alpha --from cursor", "agentx skill place alpha --to cursor --copy"))
}

// TestSkillPlaceLeavesTheLibraryAClientReadsThroughALink: a client whose
// skills directory is a symlink to the library, or the library a symlink
// to it, reads the library, and the skill's directory there is the library
// directory itself, not a directory displacing a placement. So does
// Cursor, which reads Claude Code's skills directory too. Drift names
// nothing there, the listing and a placement in Claude Code report the
// skill there as the library entry, and skill place, whatever flag it is
// given, leaves the library directory as it is, an edit of it included:
// replacing that directory with the symlink would replace the library
// with a link to itself. Removing the skill from Claude Code alone is
// refused, as it is for any client that reads the library. A placement
// that went missing elsewhere is still put back.
func TestSkillPlaceLeavesTheLibraryAClientReadsThroughALink(t *testing.T) {
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

			for _, flag := range []string{"", "--keep-library", "--keep-placement"} {
				args := []string{"alpha"}
				if flag != "" {
					args = append(args, flag)
				}
				contains(t, "skill place "+flag, placesNothing(t, h, args...), "✓ placed alpha in 6 configurations\n")
			}
			info, err := os.Lstat(lib)
			if err != nil || !info.IsDir() {
				t.Fatalf("the library directory is no longer a directory: %v", err)
			}
			sameTree(t, "the library directory", libraryTree(t, lib), held)
			linksToLibrary(t, "cursor's link", cursor, lib)
			ev = h.listed("alpha")
			equal(t, "state after placing", ev["state"], state)
			equal(t, "drift after placing", drift(ev), "")
			cleanAfterPlace(t, h, h.library, skills)

			// Placing the skill in Claude Code names the library entry too.
			equal(t, "the text of a placement", h.mustRun("skill", "place", "alpha", "--to", "claude-code").stdout,
				"✓ placed alpha in 1 configuration\n"+
					"  claude-code  library  "+filepath.Join(skills, "alpha")+"\n"+
					"  always available to universal clients: claude-code, codex, cursor, gemini-cli\n")
			sameTree(t, "the library directory after a placement", libraryTree(t, lib), held)
			cleanAfterPlace(t, h, h.library, skills)

			removal := h.run("--json", "skill", "remove", "alpha", "--from", "claude-code")
			equal(t, "exit of a removal from claude-code", removal.exit, 6)
			equal(t, "the refusal", h.one(removal.stdout, "error")["message"], "claude-code reads the library directly, so alpha cannot be removed from it alone")
			sameTree(t, "the library directory after a refused removal", libraryTree(t, lib), held)

			remove(t, windsurf)
			equal(t, "drift with a placement missing", drift(h.listed("alpha")), "missing")
			text := h.mustRun("skill", "place", "alpha").stdout
			contains(t, "the text", text, "✓ placed alpha in 6 configurations\n")
			contains(t, "the text", text, "\n  windsurf        symlink  "+windsurf+" -> "+lib+"\n")
			linksToLibrary(t, "windsurf's placement", windsurf, lib)
			sameTree(t, "the library directory after putting it back", libraryTree(t, lib), held)
			cleanAfterPlace(t, h, h.library, skills, filepath.Dir(windsurf))
		})
	}
}

// TestSkillPlaceLeavesALibraryEntryThatLinksIntoAClient: a library entry
// made a symlink to a client's skill directory, here Claude Code's, makes
// that directory the library's own. It is no directory displacing a
// placement: drift names nothing there, the listing reports it as the
// library entry, and skill place, whatever flag it is given, leaves it and
// the link as they are, and so does placing the skill in Claude Code alone,
// as a symlink or as a copy. Replacing the directory with the library's symlink
// would leave a link to a link to itself and the skill's content gone.
func TestSkillPlaceLeavesALibraryEntryThatLinksIntoAClient(t *testing.T) {
	t.Parallel()
	h, lib, claude, cursor := everywhereHarness(t)
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
	untouched := func(what string) {
		t.Helper()
		linksToLibrary(t, what+": the library entry", lib, claude)
		if _, ok := isSymlink(t, claude); ok {
			t.Fatalf("%s: claude's directory, the library's own, was replaced", what)
		}
		sameTree(t, what+": claude's directory", libraryTree(t, claude), held)
		linksToLibrary(t, what+": cursor's link", cursor, lib)
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
	}

	for _, flag := range []string{"", "--keep-library", "--keep-placement"} {
		args := []string{"alpha"}
		if flag != "" {
			args = append(args, flag)
		}
		contains(t, "skill place "+flag, placesNothing(t, h, args...), "✓ placed alpha in 4 configurations\n")
	}
	untouched("after placing everywhere")

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
// that has to prove a refused run changed nothing, links included, or that
// a run kept a link as the link it was, compares two of them.
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

// refusesUntouched runs skill place of alpha once with each of flags, ""
// for none, and holds every run to exit code 6 with message and hint and
// to changing nothing at all: the home directory and the library hold what
// they held, byte for byte and link for link, no mutation was made and no
// journal is left.
func refusesUntouched(t *testing.T, h *harness, flags []string, message, hint string) {
	t.Helper()
	home, library := onDisk(t, h.home), onDisk(t, h.library)
	version := mutationVersion(t, h)
	for _, flag := range flags {
		args := []string{"--json", "skill", "place", "alpha"}
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
	}
	equal(t, "no mutation", mutationVersion(t, h), version)
	equal(t, "journals", journalCount(t, h), 0)
}

// everyFlag is every way skill place can be asked to put a placement back.
var everyFlag = []string{"", "--keep-library", "--keep-placement"}

// overlapHint is the hint of skill place refusing because two paths
// overlap.
const overlapHint = "replace the symlink that joins them with the files it leads to, or move one of them elsewhere, then run 'agentx skill place alpha' again"

// libraryOverlap is the message of skill place refusing because the path
// at place stands to the library as rel says, "lies inside", "holds" or
// "is".
func libraryOverlap(place, rel, library string) string {
	return place + " " + rel + " the library " + library + ", and placing it would change what the library holds, so nothing was placed"
}

// entryOverlap is the message of skill place refusing because the path at
// place stands to real, where the library entry leads, as rel says.
func entryOverlap(place, rel, real, entry string) string {
	if rel == "is" {
		return "the library entry " + entry + " leads to " + place + ", and placing it would change what the library holds, so nothing was placed"
	}
	return place + " " + rel + " " + real + ", where the library entry " + entry + " leads, and placing it would change what the library holds, so nothing was placed"
}

// nestedOverlap is the message of skill place refusing because the path
// at inner lies inside the one at outer, one of the two being a path it
// changes.
func nestedOverlap(inner, outer string) string {
	return inner + " lies inside " + outer + ", and placing one would change the other, so nothing was placed"
}

// TestSkillPlaceRefusesALibraryEntryThatIsASymlink: agentx installs a
// managed skill as a real directory, so a library entry that is a symlink
// is a hand edit, and replacing anything beside it could drop the link or
// reach through it. skill place refuses wherever a displaced directory is
// to be replaced, whatever the flags, names the link and changes nothing.
// A missing placement alone removes nothing, and is put back as a symlink
// to the entry.
func TestSkillPlaceRefusesALibraryEntryThatIsASymlink(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// arrange sets the machine up and returns where the library entry
		// leads.
		arrange func(t *testing.T, h *harness, lib, claude, cursor string) string
	}{
		{"an entry linked to a directory of the user's, beside a directory that differs", func(t *testing.T, h *harness, lib, claude, _ string) string {
			dev := filepath.Join(h.home, "dev-alpha")
			if err := os.Rename(lib, dev); err != nil {
				t.Fatal(err)
			}
			link(t, dev, lib)
			displace(t, dev, claude, true)
			return dev
		}},
		{"an entry linked beneath the displaced directory", func(t *testing.T, _ *harness, lib, claude, _ string) string {
			remove(t, claude)
			inner := mkdirs(t, claude, "inner")
			if err := os.Rename(lib, inner); err != nil {
				t.Fatal(err)
			}
			link(t, inner, lib)
			writeFile(t, filepath.Join(claude, "README.md"), "a file beside the library's content\n")
			return inner
		}},
		{"an entry linked to a directory holding a link into the displaced directory", func(t *testing.T, h *harness, lib, claude, _ string) string {
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
			return dev
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			target := c.arrange(t, h, lib, claude, cursor)
			if drift(h.listed("alpha")) == "" {
				t.Fatal("alpha has no drift to put back")
			}
			refusesUntouched(t, h, everyFlag, lib+" is a symlink to "+target+", not the directory agentx installed, so nothing was placed",
				"replace the link with the directory it leads to, then run 'agentx skill place alpha' again")
			linksToLibrary(t, "the library entry", lib, target)
			cleanAfterPlace(t, h, filepath.Dir(claude), filepath.Dir(cursor))
		})
	}

	t.Run("an entry linked to a directory of the user's, with a placement missing", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, cursor := everywhereHarness(t)
		dev := filepath.Join(h.home, "dev-alpha")
		if err := os.Rename(lib, dev); err != nil {
			t.Fatal(err)
		}
		link(t, dev, lib)
		remove(t, cursor)
		held := onDisk(t, dev)
		equal(t, "drift", drift(h.listed("alpha")), "missing")

		for _, flag := range everyFlag {
			remove(t, cursor)
			args := []string{"skill", "place", "alpha"}
			if flag != "" {
				args = append(args, flag)
			}
			contains(t, "skill place "+flag, h.mustRun(args...).stdout, "✓ placed alpha in 4 configurations\n")
			linksToLibrary(t, "cursor's placement", cursor, lib)
			linksToLibrary(t, "claude's placement", claude, lib)
			linksToLibrary(t, "the library entry", lib, dev)
			equal(t, "what the entry leads to", onDisk(t, dev), held)
			cleanAfterPlace(t, h, h.library, filepath.Dir(cursor))
		}
		equal(t, "drift after skill place", drift(h.listed("alpha")), "")
	})
}

// TestSkillPlaceRefusesAPathThatOverlapsTheLibrary: skill place removes and
// writes whole directories, so a path it changes that is, lies inside or
// holds the library, or the directory a library entry of another skill
// leads to, would change what the library holds, whatever the path holds
// and even where a displaced directory holds exactly the library's content.
// Every such run refuses, whatever the flags, names the two paths and
// changes nothing: a displaced directory or a missing place a client's
// skills directory linked into the library puts there, one a library
// entry's link leads into, through or above, the library's symlink where a
// copy belongs there, and a displaced directory the library was moved
// into. --keep-placement, which also replaces the library directory and
// refreshes every copy copy_mode records, refuses as well where another
// skill's entry leads into that directory, or where a copy lies in the
// library or in what an entry leads to; --keep-library goes ahead there.
func TestSkillPlaceRefusesAPathThatOverlapsTheLibrary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// arrange sets the machine up and returns the message of the
		// refusal and a file of the library it must keep.
		arrange func(t *testing.T, h *harness, lib, claude, cursor string) (message, kept string)
	}{
		{"another skill's entry linked into a directory that differs", func(t *testing.T, h *harness, lib, claude, _ string) (string, string) {
			displace(t, lib, claude, true)
			kept := mkdirs(t, filepath.Join(claude, "beta"), "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, filepath.Join(claude, "beta"), beta)
			return entryOverlap(claude, "holds", filepath.Join(claude, "beta"), beta), kept
		}},
		{"another skill's entry linked into a directory holding the library's content", func(t *testing.T, h *harness, lib, claude, _ string) (string, string) {
			writeFile(t, mkdirs(t, filepath.Join(lib, "beta"), "SKILL.md"), skill("beta", "A skill of my own"))
			displace(t, lib, claude, false)
			beta := filepath.Join(h.library, "beta")
			link(t, filepath.Join(claude, "beta"), beta)
			return entryOverlap(claude, "holds", filepath.Join(claude, "beta"), beta), filepath.Join(claude, "beta", "SKILL.md")
		}},
		{"another skill's entry linked to the directory itself", func(t *testing.T, h *harness, lib, claude, _ string) (string, string) {
			displace(t, lib, claude, true)
			beta := filepath.Join(h.library, "beta")
			link(t, claude, beta)
			return entryOverlap(claude, "is", claude, beta), filepath.Join(claude, "mine.md")
		}},
		{"another skill's entry linked to the skills directory above the directory", func(t *testing.T, h *harness, lib, claude, _ string) (string, string) {
			displace(t, lib, claude, true)
			beta := filepath.Join(h.library, "beta")
			link(t, filepath.Dir(claude), beta)
			return entryOverlap(claude, "lies inside", filepath.Dir(claude), beta), filepath.Join(claude, "mine.md")
		}},
		{"a displaced directory inside the library", func(t *testing.T, h *harness, lib, _, cursor string) (string, string) {
			beta := filepath.Join(h.library, "beta")
			kept := mkdirs(t, beta, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			copyTree(t, lib, filepath.Join(beta, "alpha"))
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, beta, skills)
			return libraryOverlap(cursor, "lies inside", h.library), kept
		}},
		{"a displaced directory the library was moved into", func(t *testing.T, h *harness, lib, claude, _ string) (string, string) {
			displace(t, lib, claude, true)
			moved := filepath.Join(claude, "library")
			if err := os.Rename(h.library, moved); err != nil {
				t.Fatal(err)
			}
			link(t, moved, h.library)
			return libraryOverlap(claude, "holds", h.library), filepath.Join(moved, "alpha", "SKILL.md")
		}},
		{"a missing place inside the library", func(t *testing.T, h *harness, _, _, cursor string) (string, string) {
			beta := filepath.Join(h.library, "beta")
			kept := mkdirs(t, beta, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, beta, skills)
			return libraryOverlap(cursor, "lies inside", h.library), kept
		}},
		{"a missing place inside what a library entry leads to", func(t *testing.T, h *harness, _, _, cursor string) (string, string) {
			dev := filepath.Join(h.home, "dev", "beta")
			kept := mkdirs(t, dev, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, dev, skills)
			return entryOverlap(cursor, "lies inside", dev, beta), kept
		}},
		{"a missing place inside what a library entry leads to, reached through that entry", func(t *testing.T, h *harness, _, _, cursor string) (string, string) {
			dev := filepath.Join(h.home, "dev", "beta")
			kept := mkdirs(t, dev, "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, beta, skills)
			return entryOverlap(cursor, "lies inside", dev, beta), kept
		}},
		{"a missing copy inside what a library entry leads to", func(t *testing.T, h *harness, _, _, cursor string) (string, string) {
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
			return entryOverlap(cursor, "lies inside", dev, beta), kept
		}},
		{"the library's symlink where a copy belongs, inside what a library entry leads to", func(t *testing.T, h *harness, lib, _, cursor string) (string, string) {
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
			return entryOverlap(cursor, "lies inside", dev, beta), kept
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			message, kept := c.arrange(t, h, lib, claude, cursor)
			if drift(h.listed("alpha")) == "" {
				t.Fatal("alpha has no drift to put back")
			}
			content := fileBody(t, kept)
			refusesUntouched(t, h, everyFlag, message, overlapHint)
			equal(t, "what the library keeps", fileBody(t, kept), content)
			h.mustRun("skill", "list")
		})
	}

	// --keep-placement replaces the library directory itself, so it refuses
	// where another skill's entry leads into it; --keep-library does not
	// replace it and places the skill, the other skill kept as it was.
	t.Run("another skill's entry linked into the library directory", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, _ := everywhereHarness(t)
		displace(t, lib, claude, true)
		vendored := filepath.Join(lib, "vendored", "beta")
		writeFile(t, mkdirs(t, vendored, "SKILL.md"), skill("beta", "A skill of my own"))
		beta := filepath.Join(h.library, "beta")
		link(t, vendored, beta)

		refusesUntouched(t, h, []string{"--keep-placement"}, entryOverlap(lib, "holds", vendored, beta), overlapHint)
		h.mustRun("skill", "place", "alpha", "--keep-library")
		linksToLibrary(t, "claude's placement", claude, lib)
		equal(t, "what beta leads to", fileBody(t, filepath.Join(beta, "SKILL.md")), skill("beta", "A skill of my own"))
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
	})

	// --keep-placement also refreshes every copy copy_mode records with the
	// content it keeps, so it refuses where one of them lies inside the
	// library or inside what a library entry leads to, even a copy nothing
	// is wrong with; --keep-library refreshes no copy and places the skill,
	// the copy kept as it was.
	for _, c := range []struct {
		name string
		// arrange returns the directory to hold beta and Cursor's skills,
		// and the message of the refusal.
		arrange func(t *testing.T, h *harness, cursor string) (dir, message string)
	}{
		{"a copy inside the library", func(t *testing.T, h *harness, cursor string) (string, string) {
			return filepath.Join(h.library, "beta"), libraryOverlap(cursor, "lies inside", h.library)
		}},
		{"a copy inside what a library entry leads to", func(t *testing.T, h *harness, cursor string) (string, string) {
			dev := filepath.Join(h.home, "dev", "beta")
			beta := filepath.Join(h.library, "beta")
			link(t, dev, beta)
			return dev, entryOverlap(cursor, "lies inside", dev, beta)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			dir, message := c.arrange(t, h, cursor)
			writeFile(t, mkdirs(t, dir, "SKILL.md"), skill("beta", "A skill of my own"))
			copyTree(t, lib, filepath.Join(dir, "alpha"))
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, dir, skills)
			equal(t, "drift of Cursor's copy", drift(h.listed("alpha")), "")
			displace(t, lib, claude, true)
			equal(t, "drift", drift(h.listed("alpha")), "displaced")
			beta := onDisk(t, dir)

			refusesUntouched(t, h, []string{"--keep-placement"}, message, overlapHint)
			if _, err := os.Lstat(filepath.Join(dir, "alpha", "mine.md")); !os.IsNotExist(err) {
				t.Errorf("Cursor's copy was refreshed with the displaced directory's content: %v", err)
			}
			h.mustRun("skill", "place", "alpha", "--keep-library")
			linksToLibrary(t, "claude's placement", claude, lib)
			equal(t, "what beta holds", onDisk(t, dir), beta)
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude), dir)
		})
	}
}

// TestSkillPlaceRefusesAPlaceInsideAnother: a client's skills directory
// made a symlink into another client's skill directory puts its place
// inside that one. The journal applies each step to a path as it resolves
// then, so once the outer directory is replaced by the library's symlink
// the inner place resolves into the library, and placing it would change
// the library; placed first, it would change what the outer directory was
// judged to hold. A place the run does not write goes with the outer
// directory all the same, whatever it holds: a copy of this skill, edited
// or not, and the skills directory holding another skill's copy, a
// disabled client's included, which skill place without --to never places
// into but still must not delete. skill place refuses each whatever it is
// told to keep, names both paths and changes nothing.
func TestSkillPlaceRefusesAPlaceInsideAnother(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// arrange sets the machine up and returns a file skill place must
		// keep.
		arrange func(t *testing.T, h *harness, lib, claude, cursor string) string
	}{
		{"a client's place inside another client's displaced directory", func(t *testing.T, _ *harness, lib, claude, cursor string) string {
			// Cursor's place is the library's sub/alpha, as Claude Code's
			// displaced copy of it holds it.
			writeFile(t, mkdirs(t, filepath.Join(lib, "sub", "alpha"), "x.md"), "a file of the library's own\n")
			displace(t, lib, claude, false)
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, filepath.Join(claude, "sub"), skills)
			return filepath.Join(lib, "sub", "alpha", "x.md")
		}},
		{"a copy inside the directory", func(t *testing.T, h *harness, lib, claude, cursor string) string {
			remove(t, cursor)
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
			displace(t, lib, claude, true)
			if err := os.Rename(cursor, filepath.Join(claude, "scripts", "alpha")); err != nil {
				t.Fatal(err)
			}
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, filepath.Join(claude, "scripts"), skills)
			return filepath.Join(claude, "scripts", "alpha", "SKILL.md")
		}},
		{"an edited copy inside the directory", func(t *testing.T, h *harness, lib, claude, cursor string) string {
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
			return filepath.Join(moved, "mine.md")
		}},
		{"another skill's edited copy in a skills directory inside the directory", func(t *testing.T, h *harness, lib, claude, cursor string) string {
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
			return filepath.Join(vendor, "beta", "mine.md")
		}},
		{"a disabled client's skills directory inside the directory", func(t *testing.T, h *harness, lib, claude, cursor string) string {
			// Cursor is disabled, so skill place never places into it, but its
			// skills directory, beta's edited copy in it, is still inside
			// Claude Code's displaced directory.
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
			h.mustRun("config", "disable", "cursor")
			return filepath.Join(vendor, "beta", "mine.md")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			kept := c.arrange(t, h, lib, claude, cursor)
			if drift(h.listed("alpha")) == "" {
				t.Fatal("alpha has no drift to put back")
			}
			content := fileBody(t, kept)
			refusesUntouched(t, h, everyFlag, nestedOverlap(cursor, claude), overlapHint)
			equal(t, "what skill place keeps", fileBody(t, kept), content)
			h.mustRun("skill", "list")
		})
	}
}

// TestSkillPlaceRefusesALibraryDirectoryHoldingASymlink: an import never
// holds a symlink, so one in the skill's library directory is a hand edit,
// and it may lead into, through or above what skill place removes. Rather
// than follow it, every run that removes a displaced directory or replaces
// the library directory refuses while the library directory holds a
// symlink at any depth, whatever the flags, --keep-placement included,
// which would replace the library directory link and all, names the link,
// points at diff and revert, and changes nothing. A run that only writes
// placements, a missing one or a copy where the library's symlink stands,
// removes no directory and goes ahead.
func TestSkillPlaceRefusesALibraryDirectoryHoldingASymlink(t *testing.T) {
	t.Parallel()
	refusal := func(lib, at string) (message, hint string) {
		return "the library directory " + lib + " holds the symlink " + at + ", which no version agentx installs holds, so nothing was placed",
			"replace the link with the files it leads to, or see what changed with 'agentx skill diff alpha' and go back to the installed version with 'agentx skill revert alpha', then run 'agentx skill place alpha' again"
	}
	for _, c := range []struct {
		name string
		// arrange sets the machine up, Claude Code's place holding a
		// displaced directory that differs, and returns the link.
		arrange func(t *testing.T, h *harness, lib, claude string) string
	}{
		{"a directory of the library linked into the displaced directory", func(t *testing.T, _ *harness, lib, claude string) string {
			scripts := filepath.Join(lib, "scripts")
			swapForLink(t, scripts, filepath.Join(claude, "scripts"))
			return scripts
		}},
		{"the library's SKILL.md linked into the displaced directory", func(t *testing.T, _ *harness, lib, claude string) string {
			file := filepath.Join(lib, "SKILL.md")
			swapForLink(t, file, filepath.Join(claude, "SKILL.md"))
			return file
		}},
		{"a link to the skills directory above the displaced directory", func(t *testing.T, _ *harness, lib, claude string) string {
			up := filepath.Join(lib, "up")
			link(t, filepath.Dir(claude), up)
			return up
		}},
		{"a link to a directory outside holding a link into the displaced directory", func(t *testing.T, h *harness, lib, claude string) string {
			writeFile(t, mkdirs(t, filepath.Join(claude, "shared"), "data.md"), "only in the displaced directory\n")
			vendor := filepath.Join(h.home, "vendor")
			link(t, filepath.Join(claude, "shared"), mkdirs(t, vendor, "shared"))
			at := filepath.Join(lib, "vendor")
			link(t, vendor, at)
			return at
		}},
		{"a link the displaced directory holds too, which another skill's entry leads through", func(t *testing.T, h *harness, lib, claude string) string {
			real := filepath.Join(h.home, "real-beta")
			writeFile(t, mkdirs(t, real, "SKILL.md"), skill("beta", "A skill of my own"))
			at := filepath.Join(lib, "beta-link")
			link(t, real, at)
			link(t, real, filepath.Join(claude, "beta-link"))
			link(t, filepath.Join(claude, "beta-link"), filepath.Join(h.library, "beta"))
			return at
		}},
		{"a link back into the displaced directory", func(t *testing.T, _ *harness, lib, claude string) string {
			at := filepath.Join(lib, "back")
			link(t, filepath.Join(claude, "SKILL.md"), at)
			return at
		}},
		{"a link to a file of the user's elsewhere", func(t *testing.T, h *harness, lib, _ string) string {
			outside := filepath.Join(h.home, "shared-notes.md")
			writeFile(t, outside, "notes kept outside the skill\n")
			at := filepath.Join(lib, "shared.md")
			link(t, outside, at)
			return at
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, _ := everywhereHarness(t)
			displace(t, lib, claude, true)
			at := c.arrange(t, h, lib, claude)
			equal(t, "drift", drift(h.listed("alpha")), "displaced")
			message, hint := refusal(lib, at)
			refusesUntouched(t, h, everyFlag, message, hint)
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
		})
	}

	t.Run("the library's symlink where a copy belongs, alone", func(t *testing.T) {
		t.Parallel()
		h, lib, _, cursor := everywhereHarness(t)
		remove(t, cursor)
		h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")
		remove(t, cursor)
		link(t, lib, cursor)
		outside := filepath.Join(h.home, "shared-notes.md")
		writeFile(t, outside, "notes kept outside the skill\n")
		at := filepath.Join(lib, "shared.md")
		link(t, outside, at)
		equal(t, "drift", drift(h.listed("alpha")), "displaced")
		h.mustRun("skill", "place", "alpha")
		if _, ok := isSymlink(t, cursor); ok {
			t.Fatal("cursor's placement is still a symlink, want the copy copy_mode records")
		}
		sameTree(t, "cursor's copy", libraryTree(t, cursor), libraryTree(t, lib))
		linksToLibrary(t, "the library's own link", at, outside)
		cleanAfterPlace(t, h, h.library, filepath.Dir(cursor))
	})

	t.Run("a missing placement alone", func(t *testing.T) {
		t.Parallel()
		h, lib, _, cursor := everywhereHarness(t)
		outside := filepath.Join(h.home, "shared-notes.md")
		writeFile(t, outside, "notes kept outside the skill\n")
		link(t, outside, filepath.Join(lib, "shared.md"))
		remove(t, cursor)
		h.mustRun("skill", "place", "alpha")
		linksToLibrary(t, "cursor's placement", cursor, lib)
		linksToLibrary(t, "the library's own link", filepath.Join(lib, "shared.md"), outside)
		cleanAfterPlace(t, h, h.library, filepath.Dir(cursor))
	})
}

// TestSkillPlaceFollowsASkillsDirectoryLinkedElsewhere: a client's skills
// directory that is a symlink to a directory of the user's outside the
// library is simply followed there: nothing overlaps, and every run lands,
// the displaced directory there replaced and a missing placement made.
func TestSkillPlaceFollowsASkillsDirectoryLinkedElsewhere(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--keep-library", "--keep-placement"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			displace(t, lib, claude, true)
			kept := libraryTree(t, claude)
			want := libraryTree(t, lib)
			if flag == "--keep-placement" {
				want = kept
			}
			elsewhere := filepath.Join(h.home, "dotfiles", "claude-skills")
			if err := os.MkdirAll(filepath.Dir(elsewhere), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Dir(claude), elsewhere); err != nil {
				t.Fatal(err)
			}
			link(t, elsewhere, filepath.Dir(claude))
			remove(t, cursor)
			equal(t, "drift before skill place", drift(h.listed("alpha")), "displaced,missing")

			h.mustRun("skill", "place", "alpha", flag)
			linksToLibrary(t, "claude's placement", filepath.Join(elsewhere, "alpha"), lib)
			linksToLibrary(t, "cursor's placement", cursor, lib)
			sameTree(t, "the library directory", libraryTree(t, lib), want)
			equal(t, "drift after skill place", drift(h.listed("alpha")), "")
			cleanAfterPlace(t, h, h.library, elsewhere, filepath.Dir(cursor))
		})
	}
}

// TestSkillPlaceLooksNoFurtherThanTheLibrarysEntries: another skill's
// library entry is followed to where it leads and no further, so an entry
// linked to a directory of the user's that overlaps nothing skill place
// changes never stops it, whatever that directory holds: links round
// in circles, a link back to the library, or a directory this machine
// cannot read. Nor does a link deep inside another skill's directory.
func TestSkillPlaceLooksNoFurtherThanTheLibrarysEntries(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--keep-library", "--keep-placement"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, _ := everywhereHarness(t)
			displace(t, lib, claude, true)
			want := libraryTree(t, lib)
			if flag == "--keep-placement" {
				want = libraryTree(t, claude)
			}
			beta := filepath.Join(h.library, "beta")
			writeFile(t, mkdirs(t, beta, "SKILL.md"), skill("beta", "A skill of my own"))
			link(t, beta, filepath.Join(beta, "loop"))
			link(t, h.home, filepath.Join(beta, "home"))
			dev := filepath.Join(h.home, "dev", "gamma")
			writeFile(t, mkdirs(t, dev, "SKILL.md"), skill("gamma", "Another skill of my own"))
			link(t, dev, filepath.Join(dev, "self"))
			link(t, h.library, filepath.Join(dev, "library"))
			link(t, filepath.Dir(dev), filepath.Join(dev, "up"))
			link(t, dev, filepath.Join(h.library, "gamma"))
			if os.Geteuid() != 0 {
				private := filepath.Join(h.home, "dev", "private")
				writeFile(t, mkdirs(t, private, "SKILL.md"), skill("delta", "A skill no one can read"))
				link(t, private, filepath.Join(h.library, "delta"))
				chmod(t, private, 0)
				t.Cleanup(func() { _ = os.Chmod(private, 0o755) }) // so the temporary home can be removed
			}

			out := h.run("skill", "place", "alpha", flag)
			if out.exit != 0 {
				t.Fatalf("skill place: exit %d\n%s", out.exit, out.stderr)
			}
			linksToLibrary(t, "claude's placement", claude, lib)
			sameTree(t, "the library directory", libraryTree(t, lib), want)
			equal(t, "beta's SKILL.md", fileBody(t, filepath.Join(beta, "SKILL.md")), skill("beta", "A skill of my own"))
			equal(t, "gamma's SKILL.md", fileBody(t, filepath.Join(h.library, "gamma", "SKILL.md")), skill("gamma", "Another skill of my own"))
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// TestSkillPlaceKeepsLinksIntoADirectoryResolving: skill place does not
// search the disk for links into what it replaces, and does not have to
// for what it keeps: a displaced directory holding the library's content is
// replaced by a symlink to that same content, and one whose content
// --keep-placement keeps becomes the library's, so a link elsewhere that
// leads into either, a link deep in another skill's directory or in a
// directory of the user's, still resolves to what it did.
func TestSkillPlaceKeepsLinksIntoADirectoryResolving(t *testing.T) {
	t.Parallel()
	t.Run("a directory holding the library's content", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, _ := everywhereHarness(t)
		writeFile(t, mkdirs(t, filepath.Join(lib, "shared"), "data.md"), "data alpha and beta share\n")
		displace(t, lib, claude, false)
		beta := filepath.Join(h.library, "beta")
		writeFile(t, mkdirs(t, beta, "SKILL.md"), skill("beta", "A skill of my own"))
		shared := filepath.Join(beta, "shared")
		link(t, filepath.Join(claude, "shared"), shared)

		h.mustRun("skill", "place", "alpha")
		linksToLibrary(t, "claude's placement", claude, lib)
		equal(t, "what beta's link leads to", fileBody(t, filepath.Join(shared, "data.md")), "data alpha and beta share\n")
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
	})

	t.Run("a directory whose content --keep-placement keeps", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, _ := everywhereHarness(t)
		displace(t, lib, claude, true)
		writeFile(t, mkdirs(t, filepath.Join(claude, "shared"), "data.md"), "only in the displaced directory\n")
		dev := filepath.Join(h.home, "dev", "beta")
		writeFile(t, mkdirs(t, dev, "SKILL.md"), skill("beta", "A skill of my own"))
		shared := filepath.Join(dev, "shared")
		link(t, filepath.Join(claude, "shared"), shared)
		link(t, dev, filepath.Join(h.library, "beta"))

		h.mustRun("skill", "place", "alpha", "--keep-placement")
		linksToLibrary(t, "claude's placement", claude, lib)
		equal(t, "the library's copy", fileBody(t, filepath.Join(lib, "shared", "data.md")), "only in the displaced directory\n")
		equal(t, "what the link leads to", fileBody(t, filepath.Join(shared, "data.md")), "only in the displaced directory\n")
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
	})
}

// TestSkillPlacePlacesBesideWhatALibraryLinkLeadsTo: a link of the
// library that leads to a directory no place lies in is no reason to
// refuse a missing placement elsewhere, and skill place makes it.
func TestSkillPlacePlacesBesideWhatALibraryLinkLeadsTo(t *testing.T) {
	t.Parallel()
	h, lib, _, cursor := everywhereHarness(t)
	dev := filepath.Join(h.home, "dev", "beta")
	writeFile(t, mkdirs(t, dev, "SKILL.md"), skill("beta", "A skill of my own"))
	link(t, dev, filepath.Join(h.library, "beta"))
	remove(t, cursor)
	equal(t, "drift before skill place", drift(h.listed("alpha")), "missing")

	h.mustRun("skill", "place", "alpha")
	linksToLibrary(t, "cursor's placement", cursor, lib)
	equal(t, "drift after skill place", drift(h.listed("alpha")), "")
	cleanAfterPlace(t, h, h.library, filepath.Dir(cursor))
}

// TestSkillPlaceJudgesALinkedSkillsDirectoryOnce: Cursor's skills
// directory made a symlink to Claude Code's makes their places one
// directory spelled two ways. A displaced directory there is judged and
// replaced once, whatever is kept of it, and reported for both
// configurations, each at the path it names the place by: replaced twice,
// the second removal would find the first one's link there and stop the
// mutation part way.
func TestSkillPlaceJudgesALinkedSkillsDirectoryOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, flag string
		edited     bool
		state      string
		last       string // the line under the rows, naming what was adopted, kept or discarded
	}{
		{"the library's content", "", false, stateCurrent, "adopted %s"},
		{"an edit, with --keep-library", "--keep-library", true, stateCurrent, "discarded what %s held"},
		{"an edit, with --keep-placement", "--keep-placement", true, stateModified, "the library now holds what %s held"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			remove(t, filepath.Dir(cursor))
			link(t, filepath.Dir(claude), filepath.Dir(cursor))
			displace(t, lib, claude, c.edited)
			want := libraryTree(t, lib)
			if c.flag == "--keep-placement" {
				want = libraryTree(t, claude)
			}
			equal(t, "drift before skill place", drift(h.listed("alpha")), "displaced")

			args := []string{"skill", "place", "alpha"}
			if c.flag != "" {
				args = append(args, c.flag)
			}
			equal(t, "the text", h.mustRun(args...).stdout, "✓ placed alpha in 4 configurations\n"+
				everyRow(lib, claude, cursor)+
				"  "+fmt.Sprintf(c.last, claude)+"\n"+
				universalLine)
			linksToLibrary(t, "the placement both read", claude, lib)
			sameTree(t, "the library directory", libraryTree(t, lib), want)
			ev := h.listed("alpha")
			equal(t, "state", ev["state"], c.state)
			equal(t, "drift after skill place", drift(ev), "")
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// ignoresCase skips the test unless the disk its temporary directories are
// on ignores case, as a Mac's does by default: a file made as a is found as
// A.
func ignoresCase(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), "")
	if _, err := os.Lstat(filepath.Join(dir, "A")); err != nil {
		t.Skip("the disk tells case apart")
	}
}

// inAnotherCase is path with its last element in upper case, which a disk
// that ignores case finds as path itself.
func inAnotherCase(path string) string {
	return filepath.Join(filepath.Dir(path), strings.ToUpper(filepath.Base(path)))
}

// TestSkillPlaceJudgesAPlaceSpelledInAnotherCaseOnce: on a disk that
// ignores case, Cursor's skills directory made a symlink to
// ~/.CLAUDE/skills is Claude Code's ~/.claude/skills, though the link
// spells it in another case and the two resolve to two spellings. The two
// places are one: drift judges it once, and skill place puts one symlink
// there and reports it for both configurations, whether Claude Code's
// directory displaced the placement with the library's content, which it
// adopts, or the placement is missing.
func TestSkillPlaceJudgesAPlaceSpelledInAnotherCaseOnce(t *testing.T) {
	t.Parallel()
	ignoresCase(t)
	for _, c := range []struct {
		drift   string
		adopted bool
		arrange func(t *testing.T, lib, claude string)
	}{
		{"displaced", true, func(t *testing.T, lib, claude string) { displace(t, lib, claude, false) }},
		{"missing", false, func(t *testing.T, _, claude string) { remove(t, claude) }},
	} {
		t.Run(c.drift, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			remove(t, filepath.Dir(cursor))
			link(t, filepath.Join(inAnotherCase(filepath.Join(h.home, ".claude")), "skills"), filepath.Dir(cursor))
			c.arrange(t, lib, claude)
			targets := []placeTarget{{id: "claude-code", dir: filepath.Dir(claude)}, {id: "cursor", dir: filepath.Dir(cursor)}}
			equal(t, "places", len(ownPlaces(targets, h.library, "alpha", nil, nil)), 1)
			equal(t, "drift before skill place", drift(h.listed("alpha")), c.drift)

			adopted := ""
			if c.adopted {
				adopted = "  adopted " + claude + "\n"
			}
			equal(t, "the text", h.mustRun("skill", "place", "alpha").stdout, "✓ placed alpha in 4 configurations\n"+
				everyRow(lib, claude, cursor)+adopted+universalLine)
			linksToLibrary(t, "the placement both read", claude, lib)
			equal(t, "drift after skill place", drift(h.listed("alpha")), "")
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// TestSkillPlaceRefusesAnOverlapSpelledInAnotherCase: on a disk that
// ignores case, a link can spell the library, or a client's directory, in
// another case than skill place names it, and the spellings alone would
// hide that the two overlap. The paths are compared as files as well, so
// every run refuses as it does where the spellings agree, names the paths
// as skill place names them and changes nothing.
func TestSkillPlaceRefusesAnOverlapSpelledInAnotherCase(t *testing.T) {
	t.Parallel()
	ignoresCase(t)
	for _, c := range []struct {
		name string
		// arrange sets the machine up and returns the message of the
		// refusal and a file skill place must keep.
		arrange func(t *testing.T, h *harness, lib, claude, cursor string) (message, kept string)
	}{
		{"a missing place inside the library", func(t *testing.T, h *harness, _, _, cursor string) (string, string) {
			kept := mkdirs(t, filepath.Join(h.library, "beta"), "SKILL.md")
			writeFile(t, kept, skill("beta", "A skill of my own"))
			skills := filepath.Dir(cursor)
			remove(t, skills)
			link(t, filepath.Join(inAnotherCase(h.library), "beta"), skills)
			return libraryOverlap(cursor, "lies inside", h.library), kept
		}},
		{"another skill's entry linked to the displaced directory", func(t *testing.T, h *harness, lib, claude, _ string) (string, string) {
			displace(t, lib, claude, true)
			beta := filepath.Join(h.library, "beta")
			link(t, filepath.Join(inAnotherCase(filepath.Join(h.home, ".claude")), "skills", "alpha"), beta)
			return entryOverlap(claude, "is", claude, beta), filepath.Join(claude, "mine.md")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, lib, claude, cursor := everywhereHarness(t)
			message, kept := c.arrange(t, h, lib, claude, cursor)
			if drift(h.listed("alpha")) == "" {
				t.Fatal("alpha has no drift to put back")
			}
			content := fileBody(t, kept)
			refusesUntouched(t, h, everyFlag, message, overlapHint)
			equal(t, "what skill place keeps", fileBody(t, kept), content)
		})
	}
}

// TestSkillPlaceJudgesASharedPlaceOnce: Zencoder and Zenflow read one
// skills directory, and copy_mode records a copy for Zenflow alone. The
// place both read is put back once, as a copy, since a copy placed for
// Zenflow is the one Zencoder reads, and counted for both configurations,
// whether skill place covers every enabled configuration or names the two;
// copy_mode is left as it is.
func TestSkillPlaceJudgesASharedPlaceOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		to    []string
		text  string // the first line of the text
		drift string // afterwards: a skill placed with --to is missing from the rest
	}{
		{"every enabled configuration", nil, "✓ placed alpha in 3 configurations\n", ""},
		{"the two named", []string{"--to", "zencoder", "--to", "zenflow"}, "✓ placed alpha in 2 configurations\n", "missing"},
	} {
		t.Run(c.name, func(t *testing.T) {
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
			equal(t, "drift before skill place", drift(h.listed("alpha")), "missing")
			dirs := []string{h.library, filepath.Dir(shared)}
			if c.to == nil {
				dirs = append(dirs, filepath.Dir(claude))
			}

			out := h.mustRun(append([]string{"--json", "skill", "place", "alpha"}, c.to...)...)
			contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), ", 1 placement as copy")
			if c.to == nil {
				linksToLibrary(t, "claude's placement", claude, lib)
			} else {
				nothingAt(t, "claude's place", claude)
			}
			sameTree(t, "the shared copy", libraryTree(t, shared), libraryTree(t, lib))
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "zenflow")
			equal(t, "drift after skill place", drift(h.listed("alpha")), c.drift)
			cleanAfterPlace(t, h, dirs...)

			remove(t, shared)
			contains(t, "the text", h.mustRun(append([]string{"skill", "place", "alpha"}, c.to...)...).stdout, c.text)
			sameTree(t, "the shared copy placed again", libraryTree(t, shared), libraryTree(t, lib))
			cleanAfterPlace(t, h, dirs...)
		})
	}
}

// TestSkillPlaceSkipsAPlaceItCannotRead: a displaced directory this machine
// cannot read whole can be judged neither the library's content nor
// anything else, so it is left as it is, counted as skipped and named with
// the cause, and the rest of the run still lands.
func TestSkillPlaceSkipsAPlaceItCannotRead(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	h, lib, claude, cursor := everywhereHarness(t)
	displace(t, lib, claude, false)
	remove(t, cursor)
	scripts := filepath.Join(claude, "scripts")
	chmod(t, scripts, 0)
	t.Cleanup(func() { _ = os.Chmod(scripts, 0o755) }) // so the temporary home can be removed

	out := h.run("--json", "skill", "place", "alpha", "--keep-library")
	chmod(t, scripts, 0o755)
	if out.exit != 0 {
		t.Fatalf("skill place: exit %d\n%s", out.exit, out.stderr)
	}
	warned := warnings(h, out.stderr)
	if len(warned) != 1 || !strings.HasPrefix(warned[0], "cannot place "+claude+": ") || !strings.HasSuffix(warned[0], "; no placement was made for claude-code") {
		t.Errorf("the warnings = %q, want one naming %s", warned, claude)
	}
	if _, ok := isSymlink(t, claude); ok {
		t.Fatal("the directory that could not be read was replaced")
	}
	sameTree(t, "claude's directory", libraryTree(t, claude), libraryTree(t, lib))
	linksToLibrary(t, "cursor's placement", cursor, lib)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 3 configurations, 1 placement skipped"+universalClauseOf)
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
}

// TestSkillPlaceSkipsAPlaceItCannotWrite: a displaced directory in a
// skills directory this machine cannot write cannot be replaced by the
// symlink. That is found out before a step of it is recorded, so it is
// left as it is, counted as skipped and named with the cause, as a
// placement that cannot be made is, and the rest of the run still lands
// with no journal left behind.
func TestSkillPlaceSkipsAPlaceItCannotWrite(t *testing.T) {
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
			h, lib, claude, cursor := everywhereHarness(t)
			displace(t, lib, claude, false)
			remove(t, cursor)
			skills := filepath.Dir(claude)
			chmod(t, skills, 0o555)
			t.Cleanup(func() { _ = os.Chmod(skills, 0o755) }) // so the temporary home can be removed

			args := []string{"--json", "skill", "place", "alpha"}
			if flag != "" {
				args = append(args, flag)
			}
			out := h.run(args...)
			chmod(t, skills, 0o755)
			if out.exit != 0 {
				t.Fatalf("skill place: exit %d\n%s", out.exit, out.stderr)
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
			equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 3 configurations, 1 placement skipped"+universalClauseOf)
			cleanAfterPlace(t, h, h.library, skills, filepath.Dir(cursor))
		})
	}
}

// TestSkillPlaceWhenEveryPlaceIsSkipped: skill place that can put back
// none of the places it covers still succeeds, since each place it could
// not reach is named in a warning. It records no step, so it leaves no
// journal and nothing beside the place, and says it placed the skill in no
// configuration.
func TestSkillPlaceWhenEveryPlaceIsSkipped(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	h, lib, claude, _ := everywhereHarness(t)
	displace(t, lib, claude, false)
	scripts := filepath.Join(claude, "scripts")
	chmod(t, scripts, 0)
	t.Cleanup(func() { _ = os.Chmod(scripts, 0o755) }) // so the temporary home can be removed

	out := h.run("--json", "skill", "place", "alpha", "--to", "claude-code")
	text := h.run("skill", "place", "alpha", "--to", "claude-code")
	chmod(t, scripts, 0o755)
	for _, o := range []outcome{out, text} {
		if o.exit != 0 {
			t.Fatalf("skill place: exit %d\n%s", o.exit, o.stderr)
		}
	}
	warned := warnings(h, out.stderr)
	if len(warned) != 1 || !strings.HasPrefix(warned[0], "cannot place "+claude+": ") || !strings.HasSuffix(warned[0], "; no placement was made for claude-code") {
		t.Errorf("the warnings = %q, want one naming %s", warned, claude)
	}
	if _, ok := isSymlink(t, claude); ok {
		t.Fatal("the directory that could not be read was replaced")
	}
	sameTree(t, "claude's directory", libraryTree(t, claude), libraryTree(t, lib))
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 0 configurations, 1 placement skipped"+universalClauseOf)
	if !strings.HasPrefix(text.stdout, "✓ placed alpha in 0 configurations, 1 placement skipped\n") {
		t.Errorf("the text = %q, want it to say it placed alpha in no configuration", text.stdout)
	}
	cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
}

// TestSkillPlaceKeepFlagsOnWhatTheyCannotJudge: a fork is placed as it
// always was, a directory that differs left in place and counted as
// skipped, and --keep-library or --keep-placement on it stops with exit 6
// and changes nothing. A skill agentx does not manage is judged like a
// managed one: a directory that differs stops the run until a flag decides.
// A managed skill whose library directory is gone has nothing to place,
// one whose import branch records no version agentx can read has no base
// for --keep-placement to refresh copies against, and a name the library
// does not hold is not found.
func TestSkillPlaceKeepFlagsOnWhatTheyCannotJudge(t *testing.T) {
	t.Parallel()
	t.Run("a fork", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, cursor := everywhereHarness(t)
		commit := h.accountGit("rev-parse", "refs/heads/managed/alpha")
		h.accountGit("update-ref", "refs/heads/skills/alpha", commit)
		h.accountGit("update-ref", "-d", "refs/heads/managed/alpha")
		remove(t, cursor)
		displace(t, lib, claude, true)
		kept := libraryTree(t, claude)
		version := mutationVersion(t, h)
		for _, flag := range []string{"--keep-library", "--keep-placement"} {
			out := h.run("--json", "skill", "place", "alpha", flag)
			equal(t, "exit "+flag, out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message "+flag, e["message"], "alpha is a fork on this machine, which --keep-library and --keep-placement do not apply to")
			equal(t, "hint "+flag, e["hint"], "place it without them with 'agentx skill place alpha'")
		}
		nothingAt(t, "cursor's placement", cursor)
		equal(t, "no mutation", mutationVersion(t, h), version)

		out := h.mustRun("--json", "skill", "place", "alpha")
		sameTree(t, "claude's directory", libraryTree(t, claude), kept)
		linksToLibrary(t, "cursor's placement", cursor, lib)
		contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), ", 1 placement skipped")
		equal(t, "the fork", h.accountGit("rev-parse", "refs/heads/skills/alpha"), commit)
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
	})

	t.Run("an unmanaged skill", func(t *testing.T) {
		t.Parallel()
		h, _ := installHarness(t)
		lib := filepath.Join(h.library, "notes")
		claude := filepath.Join(h.home, ".claude", "skills", "notes")
		writeFile(t, mkdirs(t, lib, "SKILL.md"), skill("notes", "My own notes"))
		h.mustRun("skill", "place", "notes")
		linksToLibrary(t, "claude's placement", claude, lib)
		remove(t, claude)
		copyTree(t, lib, claude)
		writeFile(t, filepath.Join(claude, "mine.md"), "a file of my own\n")
		kept := libraryTree(t, claude)

		out := h.run("--json", "skill", "place", "notes")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], claude+" is a directory whose content differs from the library's notes, so nothing was placed")
		equal(t, "hint", e["hint"], "to keep the library's content and discard it, run 'agentx skill place notes --keep-library';"+
			" to make its content the library's, run 'agentx skill place notes --keep-placement'")

		out = h.mustRun("--json", "skill", "place", "notes", "--keep-placement")
		sameTree(t, "the library directory", libraryTree(t, lib), kept)
		linksToLibrary(t, "claude's placement", claude, lib)
		equal(t, "kind", h.one(out.stdout, "library_skill")["kind"], "unmanaged")
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
	})

	t.Run("a managed skill whose library directory is gone", func(t *testing.T) {
		t.Parallel()
		h, s := installHarness(t)
		h.mustRun("skill", "add", s.url, "--skill", "alpha")
		remove(t, filepath.Join(h.library, "alpha"))
		out := h.run("--json", "skill", "place", "alpha")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "alpha is managed in the account repo but the library holds no skill directory for it, so there is no skill to place")
		equal(t, "hint", e["hint"], "run 'agentx skill add "+shellWord(s.url)+" --skill alpha' to install it again, or 'agentx skill remove alpha' to stop managing it")
		equal(t, "journals", journalCount(t, h), 0)
	})

	t.Run("a managed skill whose import branch carries no lineage", func(t *testing.T) {
		t.Parallel()
		h, lib, claude, cursor := everywhereHarness(t)
		tree := h.accountGit("rev-parse", "refs/heads/managed/alpha^{tree}")
		plain := h.accountGit("commit-tree", tree, "-m", "plain")
		h.accountGit("update-ref", "refs/heads/managed/alpha", plain)
		displace(t, lib, claude, true)
		version := mutationVersion(t, h)
		out := h.run("--json", "skill", "place", "alpha", "--keep-placement")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], "the import branch refs/heads/managed/alpha records no version agentx can read")
		equal(t, "hint", e["hint"], "run 'agentx doctor' and check the account repo it names")
		equal(t, "no mutation", mutationVersion(t, h), version)

		h.mustRun("skill", "place", "alpha", "--keep-library")
		linksToLibrary(t, "claude's placement", claude, lib)
		cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
	})

	t.Run("a name the library does not hold", func(t *testing.T) {
		t.Parallel()
		h, _ := installHarness(t)
		version := mutationVersion(t, h)
		out := h.run("--json", "skill", "place", "nothing", "--keep-library")
		equal(t, "exit", out.exit, 5)
		contains(t, "message", h.one(out.stdout, "error")["message"].(string), `the library holds no skill called "nothing"`)
		equal(t, "no mutation", mutationVersion(t, h), version)
		equal(t, "journals", journalCount(t, h), 0)
		nothingAt(t, "the library", h.library)
	})
}

// TestSkillPlaceRefusesWhenItsInputsChange: what skill place discards is
// what the paths held when it judged them, against the version the import
// branch named and the settings as they were. A git wrapper changes one of
// them once the run holds the lock, at its read of the import branch: the
// branch moves, a fork of the name appears, the library directory or the
// displaced directory is edited, the library entry becomes a symlink to
// the same content, which every run that replaces a directory refuses,
// another skill's library entry comes to lead into the displaced
// directory, which the overlap read again under the lock refuses by name,
// or the settings disable the configuration. The run then refuses before
// it writes a journal and changes nothing, and the change is there
// afterwards.
func TestSkillPlaceRefusesWhenItsInputsChange(t *testing.T) {
	t.Parallel()
	moved := "the import branch refs/heads/managed/alpha moved while alpha was being placed, so nothing was changed"
	changed := "alpha or its placements changed while it was being placed, so nothing was changed"
	for _, c := range []struct {
		name, flag string
		message    string // "" for the refusal of beta's entry leading into the displaced directory
		hint       string // "" for the hint to run skill place again
		edits      string // the directory the change edits, whose tree is not held to what it was
		// change is what the wrapper runs, git being the real one, and a
		// check of what it left once the run refused.
		change func(t *testing.T, h *harness, git, lib, claude string) (script string, check func(t *testing.T))
	}{
		{"the import branch moves", "--keep-library", moved, "", "", func(t *testing.T, h *harness, git, _, _ string) (string, func(*testing.T)) {
			before := h.accountGit("rev-parse", "refs/heads/managed/alpha")
			written := h.accountGit("commit-tree", before+"^{tree}", "-p", before, "-m", "moved")
			return accountRepoCommand(h, git, "update-ref", "refs/heads/managed/alpha", written), func(t *testing.T) {
				equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), written)
			}
		}},
		{"a fork appears", "--keep-library", moved, "", "", func(t *testing.T, h *harness, git, _, _ string) (string, func(*testing.T)) {
			before := h.accountGit("rev-parse", "refs/heads/managed/alpha")
			return accountRepoCommand(h, git, "update-ref", "refs/heads/skills/alpha", before), func(t *testing.T) {
				equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), before)
				equal(t, "the fork", h.accountGit("rev-parse", "refs/heads/skills/alpha"), before)
			}
		}},
		{"the library is edited", "--keep-placement", changed, "", "library", func(t *testing.T, _ *harness, _, lib, _ string) (string, func(*testing.T)) {
			file := filepath.Join(lib, "notes.md")
			return "printf 'an edit made meanwhile\\n' > " + shellWord(file), func(t *testing.T) {
				equal(t, "the library's notes.md", fileBody(t, file), "an edit made meanwhile\n")
			}
		}},
		{"the library entry becomes a symlink", "--keep-placement", changed, "", "library", func(t *testing.T, _ *harness, _, lib, _ string) (string, func(*testing.T)) {
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
		{"the displaced directory is edited", "--keep-library", changed, "", "place", func(t *testing.T, _ *harness, _, _, claude string) (string, func(*testing.T)) {
			file := filepath.Join(claude, "notes.md")
			return "printf 'an edit made meanwhile\\n' > " + shellWord(file), func(t *testing.T) {
				equal(t, "the directory's notes.md", fileBody(t, file), "an edit made meanwhile\n")
			}
		}},
		{"a library entry comes to lead into the displaced directory", "--keep-library", "", overlapHint, "place", func(t *testing.T, h *harness, _, _, claude string) (string, func(*testing.T)) {
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
		{"the settings change", "--keep-library", changed, "", "", func(t *testing.T, h *harness, _, _, _ string) (string, func(*testing.T)) {
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
			h, lib, claude, _ := everywhereHarness(t)
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
			message, hint := c.message, c.hint
			if message == "" {
				message = entryOverlap(claude, "holds", filepath.Join(claude, "beta"), filepath.Join(h.library, "beta"))
			}
			if hint == "" {
				hint = "run 'agentx skill place alpha' again"
			}
			out := h.run("--json", "skill", "place", "alpha", c.flag)
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], message)
			equal(t, "hint", e["hint"], hint)
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
			cleanAfterPlace(t, h, h.library, filepath.Dir(claude))
		})
	}
}

// accountRepoCommand is the shell command that runs the real git, at git,
// with args against the account repo of h, as a wrapper runs it.
func accountRepoCommand(h *harness, git string, args ...string) string {
	return git + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx)) + " " + strings.Join(args, " ")
}

// placeChildEnv marks the process TestSkillPlaceRecoversAtEveryBoundary
// starts, which runs the skill place its value holds, the name and then
// the flags, against the parent's temporary home and is killed in the
// middle of it.
const placeChildEnv = "AGENTX_TEST_PLACE_CHILD"

// TestPlaceChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestPlaceChildProcess(t *testing.T) {
	args := strings.Fields(os.Getenv(placeChildEnv))
	if len(args) == 0 {
		t.Skip("not the skill place child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), append([]string{"skill", "place"}, args...), env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// killedPlace runs one skill place in a child process that a git wrapper
// kills the moment its journal is on disk: the first git the run starts
// after writing it is the read of the import branch its ref step holds it
// to, before any path changed.
func killedPlace(t *testing.T, h *harness, args ...string) string {
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
	child := exec.Command(os.Args[0], "-test.run=^TestPlaceChildProcess$", "-test.v")
	child.Env = append(os.Environ(), placeChildEnv+"="+strings.Join(args, " "))
	for k, v := range h.env {
		child.Env = append(child.Env, k+"="+v)
	}
	out, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("skill place was not killed:\n%s", out)
	}
	return string(out)
}

// TestSkillPlaceRecoversAtEveryBoundary kills skill place with SIGKILL
// once its journal is on disk, then leaves the machine as a process killed
// after each later step would, for each way a displaced directory is put
// back beside a missing placement: adopted without a flag when it holds
// the library's content, and with each flag when it differs. The next
// command recovers each one, and the run is then whole: the library holds
// the content chosen, both placements are the library's symlink, the
// import branch is where it was, and nothing staged or retained is left
// behind.
func TestSkillPlaceRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		flag  string // "" for a directory holding the library's content
		kinds string // the journal's steps, as recorded
		state string // the state of the skill afterwards
	}{
		{"", "remove, link, link, ref", stateCurrent},
		{"--keep-library", "remove, link, link, ref", stateCurrent},
		{"--keep-placement", "remove, publish, remove, link, link, ref", stateModified},
	} {
		pathSteps := strings.Count(c.kinds, ",")
		for stop := 0; stop <= pathSteps; stop++ {
			t.Run(fmt.Sprintf("%s after %d steps", cmp.Or(c.flag, "adopted"), stop), func(t *testing.T) {
				t.Parallel()
				h, lib, claude, cursor := everywhereHarness(t)
				base := libraryTree(t, lib)
				branch := h.accountGit("rev-parse", "refs/heads/managed/alpha")
				displace(t, lib, claude, c.flag != "")
				kept := libraryTree(t, claude)
				remove(t, cursor)

				out := killedPlace(t, h, "alpha", c.flag)
				steps := readJournal(t, h)
				var kinds []string
				for _, s := range steps {
					kinds = append(kinds, s.Kind)
				}
				equal(t, "the journal's steps", strings.Join(kinds, ", "), c.kinds)
				sameTree(t, "claude's directory when skill place was killed", libraryTree(t, claude), kept)
				applySteps(t, steps, stop)

				if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
					t.Fatalf("the command after the killed run: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
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
				cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(cursor))
				ev := h.listed("alpha")
				equal(t, "state", ev["state"], c.state)
				equal(t, "drift", drift(ev), "")
			})
		}
	}
}

// TestSkillPlaceRecoversCopiesAtEveryBoundary kills skill place as
// TestSkillPlaceRecoversAtEveryBoundary does, for the steps it takes at
// copy placements: with --keep-placement a copy holding what the library
// held is refreshed and a missing copy is made from the content kept, and
// with --keep-library the library's symlink where copy_mode records a copy
// is replaced by a copy. The next command recovers each one, and every copy
// then holds what the library holds.
func TestSkillPlaceRecoversCopiesAtEveryBoundary(t *testing.T) {
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

				out := killedPlace(t, h, "alpha", c.flag)
				steps := readJournal(t, h)
				var kinds []string
				for _, s := range steps {
					kinds = append(kinds, s.Kind)
				}
				equal(t, "the journal's steps", strings.Join(kinds, ", "), c.kinds)
				applySteps(t, steps, stop)

				if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
					t.Fatalf("the command after the killed run: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
				}
				sameTree(t, "the library directory", libraryTree(t, lib), want)
				linksToLibrary(t, "claude's placement", claude, lib)
				for _, place := range []string{windsurf, copilot} {
					if _, ok := isSymlink(t, place); ok {
						t.Errorf("%s is a symlink, want the copy copy_mode records", place)
					}
					sameTree(t, "the copy at "+place, libraryTree(t, place), want)
				}
				cleanAfterPlace(t, h, h.library, filepath.Dir(claude), filepath.Dir(windsurf), filepath.Dir(copilot))
				equal(t, "drift", drift(h.listed("alpha")), "")
			})
		}
	}
}

// TestSkillPlaceSweepsStagingAKilledPlaceLeft stands in for a skill place
// killed after it staged and before its journal was written: what it
// staged, beside the library directory, beside a copy placement or beside
// a place it was putting back, is in directories nothing names. The next
// run sweeps them before it stages anything and leaves nothing behind.
// A link in what was staged in the library, as a copy of a displaced
// directory holds, is no link of the library's and refuses nothing.
func TestSkillPlaceSweepsStagingAKilledPlaceLeft(t *testing.T) {
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

			out := h.run(append([]string{"skill", "place", "alpha"}, c.args...)...)
			if out.exit != 0 {
				t.Fatalf("skill place: exit %d\n%s", out.exit, out.stderr)
			}
			linksToLibrary(t, "claude's placement", claude, lib)
			cleanAfterPlace(t, h, dirs...)
		})
	}
}
