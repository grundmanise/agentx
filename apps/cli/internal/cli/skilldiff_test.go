package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// libraryEntries are the names the library directory holds, hidden ones
// included: what a command that promises to write nothing there must leave.
func libraryEntries(t *testing.T, h *harness) []string {
	t.Helper()
	entries, err := os.ReadDir(h.library)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestSkillDiffShowsEachFileAgainstTheBase edits a managed skill every way
// a tool can: a file changed, one added, one deleted, one made a link and a
// script that lost its exec bit. The diff has one event per file, sorted by
// path, each path relative to the skill's own directory although the base
// holds it under the upstream's name, each with the unified diff git wrote,
// and the library is only read.
func TestSkillDiffShowsEachFileAgainstTheBase(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	short := h.accountGit("log", "-1", "--format=%(trailers:key=Agentx-Upstream-Commit,valueonly)", "refs/heads/managed/pdf")[:7]

	out := h.mustRun("--json", "skill", "diff", "pdf")
	if diffs := h.eventsOfType(out.stdout, "diff"); len(diffs) != 0 {
		t.Fatalf("a skill that holds its base version has diffs: %v", diffs)
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "pdf matches its base version at "+short)

	writeFile(t, filepath.Join(lib, "a.md"), "the same bytes\nand a line of mine\n")
	swapForLink(t, filepath.Join(lib, "b.md"), "a.md")
	remove(t, filepath.Join(lib, "c.md"))
	writeFile(t, filepath.Join(lib, "new.md"), "a file of my own\n")
	chmod(t, filepath.Join(lib, "bin", "run.sh"), 0o644)
	before := libraryEntries(t, h)
	edited := libraryTree(t, lib)

	out = h.mustRun("--json", "skill", "diff", "pdf")
	diffs := h.eventsOfType(out.stdout, "diff")
	var got []string
	for _, d := range diffs {
		equal(t, "name", d["name"], "pdf")
		got = append(got, d["path"].(string)+" "+d["status"].(string))
	}
	want := []string{"a.md modified", "b.md modified", "bin/run.sh modified", "c.md deleted", "new.md added"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diffs = %v, want %v", got, want)
	}
	patch := func(i int) string { return diffs[i]["patch"].(string) }
	contains(t, "a.md's diff", patch(0), "diff --git a/a.md b/a.md\n")
	contains(t, "a.md's diff", patch(0), "\n+and a line of mine\n")
	contains(t, "b.md's diff", patch(1), "deleted file mode 100644")
	contains(t, "b.md's diff", patch(1), "new file mode 120000")
	contains(t, "run.sh's diff", patch(2), "old mode 100755\nnew mode 100644\n")
	contains(t, "c.md's diff", patch(3), "\n-a third file\n")
	contains(t, "new.md's diff", patch(4), "\n+a file of my own\n")
	for i := range diffs {
		if strings.Contains(patch(i), "pdf-tools") {
			t.Errorf("a diff names the upstream's directory:\n%s", patch(i))
		}
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "pdf differs from its base version at "+short+" in 5 files")

	// Nothing was laid out anywhere the library is read from, and nothing
	// was journaled: the diff is a read.
	equal(t, "the library's entries", strings.Join(libraryEntries(t, h), " "), strings.Join(before, " "))
	sameTree(t, "the library directory", libraryTree(t, lib), edited)
	equal(t, "journals", journalCount(t, h), 0)

	text := h.mustRun("skill", "diff", "pdf").stdout
	contains(t, "the text", text, "pdf differs from its base version at "+short+" in 5 files\ndiff --git a/a.md b/a.md\n")
	contains(t, "the text", text, "\n+and a line of mine\n")
}

// TestSkillDiffPrintsBytesThatAreNotUTF8 edits a file into Latin-1, which
// git diffs as text, beside an escape and a C1 control: the text prints the
// Latin-1 byte as it is and each control as one space. The event is JSON,
// whose strings are UTF-8, so there the byte arrives as U+FFFD, and so does
// the byte of a file name that is not UTF-8, which is tried where the file
// system takes one: macOS refuses it.
func TestSkillDiffPrintsBytesThatAreNotUTF8(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	writeFile(t, filepath.Join(lib, "a.md"), "caf\xe9 \x1b[31m \xc2\x9b end\n")
	text := h.mustRun("skill", "diff", "pdf").stdout
	contains(t, "the text", text, "\n+caf\xe9  [31m   end\n")
	patch := h.one(h.mustRun("--json", "skill", "diff", "pdf").stdout, "diff")["patch"].(string)
	contains(t, "the event", patch, "\n+caf\ufffd \x1b[31m \u009b end\n")

	if runtime.GOOS == "darwin" {
		return
	}
	restore(t, filepath.Join(lib, "a.md"), "the same bytes\n")
	writeFile(t, filepath.Join(lib, "caf\xe9.md"), "a file of my own\n")
	diff := h.one(h.mustRun("--json", "skill", "diff", "pdf").stdout, "diff")
	equal(t, "the path", diff["path"], "caf\ufffd.md")
	equal(t, "the status", diff["status"], "added")
}

// TestPatchLineLetsNoControlThrough: a byte from 0x80 to 0x9F on its own,
// which a terminal set to 8-bit controls obeys as a C1 control, 0x9B as
// the escape that starts a sequence, is a space like every other control;
// the same byte continuing a UTF-8 character is part of that character,
// and a byte past 0x9F that UTF-8 does not take is printed as it is.
func TestPatchLineLetsNoControlThrough(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ line, want string }{
		{"A \x9b2J line", "A  2J line"},
		{"\x85next \x80\x9f", " next   "},
		{"\t\x1b[31m red\x7f", "\t [31m red "},
		{"a C1 \xc2\x9b in UTF-8", "a C1   in UTF-8"},
		{"caf\xe9 in Latin-1", "caf\xe9 in Latin-1"},
		{"\u00d0 \u20ac \u4e00 \u00e9", "\u00d0 \u20ac \u4e00 \u00e9"},
		{"cut \xe9\x9b short", "cut \xe9  short"},
		{"cut \xe4\xb8", "cut \xe4\xb8"},
	} {
		equal(t, fmt.Sprintf("patchLine(%q)", tc.line), patchLine(tc.line), tc.want)
	}
}

// TestSkillDiffNamesWhatGitCannotRecord: a repository nested in the skill
// is left out of the diff with a warning naming it, and the skill does not
// match its base for it: the listing calls it modified, and the diff says
// it differs only in what git cannot record, never that it matches. An
// edit beside it is one file more.
func TestSkillDiffNamesWhatGitCannotRecord(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	short := h.accountGit("log", "-1", "--format=%(trailers:key=Agentx-Upstream-Commit,valueonly)", "refs/heads/managed/pdf")[:7]
	nested := filepath.Join(lib, "vendored", ".git")
	writeFile(t, mkdirs(t, nested, "HEAD"), "ref: refs/heads/main\n")
	out := h.mustRun("--json", "skill", "diff", "pdf")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), nested+" cannot be recorded by git and is left out of the diff")
	if diffs := h.eventsOfType(out.stdout, "diff"); len(diffs) != 0 {
		t.Errorf("diff events = %v, want none: nothing else changed", diffs)
	}
	only := "pdf differs from its base version at " + short + " only in 1 path git cannot record"
	equal(t, "summary", h.one(out.stdout, "result")["summary"], only)
	equal(t, "the text", h.mustRun("skill", "diff", "pdf").stdout, only+"\n")
	equal(t, "the listing's state", h.listed("pdf")["state"], stateModified)

	// A .git at the skill's root is one more such path.
	writeFile(t, mkdirs(t, filepath.Join(lib, ".git"), "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(lib, "a.md"), "an edit\n")
	out = h.mustRun("--json", "skill", "diff", "pdf")
	equal(t, "warnings with an edit", len(warnings(h, out.stderr)), 2)
	equal(t, "diff events with an edit", len(h.eventsOfType(out.stdout, "diff")), 1)
	equal(t, "summary with an edit", h.one(out.stdout, "result")["summary"],
		"pdf differs from its base version at "+short+" in 1 file, and 2 paths git cannot record")
	equal(t, "the listing's state with an edit", h.listed("pdf")["state"], stateModified)
}

// TestSkillDiffRefusesWhatHasNoBase: a name the library does not hold
// and an unmanaged skill each have no base version this command can read,
// and a managed skill no commit of its own to name with --commit. A fork
// is not refused for having no base: its versions are its own commits,
// and one whose worktree is gone, as a branch made with git over a
// library directory has none, is refused for that.
func TestSkillDiffRefusesWhatHasNoBase(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	writeFile(t, mkdirs(t, filepath.Join(h.library, "mine"), "SKILL.md"), skill("mine", "My own"))
	writeFile(t, mkdirs(t, filepath.Join(h.library, "forked"), "SKILL.md"), skill("forked", "A fork"))
	h.accountGit("update-ref", "refs/heads/skills/forked", h.accountGit("rev-parse", "refs/heads/managed/pdf"))
	for _, c := range []struct {
		args    []string
		message string
		exit    int
	}{
		{[]string{"nowhere"}, `the library holds no skill called "nowhere"`, 5},
		{[]string{"mine"}, "mine is not managed by agentx, so it has no base version to", 6},
		{[]string{"forked"}, "forked's worktree " + quotedPath(filepath.Join(h.agentx, "worktrees", "forked")) + " is missing", 6},
		{[]string{"pdf", "--commit", "HEAD"}, "pdf is managed, not a fork, so it has no commit of its own to", 6},
		{[]string{"mine", "--commit", "HEAD"}, "mine is not a fork, so it has no commit to", 6},
	} {
		what := "diff " + strings.Join(c.args, " ")
		out := h.run(append([]string{"--json", "skill", "diff"}, c.args...)...)
		equal(t, what+": exit", out.exit, c.exit)
		contains(t, what+": message", h.one(out.stdout, "error")["message"].(string), c.message)
	}
	sameTree(t, "the unmanaged skill", libraryTree(t, filepath.Join(h.library, "mine")), map[string]string{"SKILL.md": skill("mine", "My own")})
}

// TestAdoptionOffersNoDiff: choosing a base when adopting records it and
// shows no diff; comparing is skill diff's, after the adoption.
func TestAdoptionOffersNoDiff(t *testing.T) {
	t.Parallel()
	h, s, v1, _ := adoptHarness(t)
	editLibrary(t, h, "alpha", "notes.md", "alpha notes, and a line of mine\n")
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha",
	}})
	out := h.mustRun("--json", "adopt", "--skill", "alpha", "--base", v1)
	if diffs := h.eventsOfType(out.stdout, "diff"); len(diffs) != 0 {
		t.Errorf("adopt emitted diff events: %v", diffs)
	}
	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "alpha").stdout, "diff")
	if len(diffs) != 1 || diffs[0]["path"] != "notes.md" {
		t.Errorf("skill diff after the adoption = %v, want notes.md", diffs)
	}
}

// TestIgnoreRulesAndAttributesApplyAsGitAppliesThem: a skill holding a
// .gitignore that ignores everything and a .gitattributes that asks for
// CRLF line endings is compared as git compares a work tree. A file the
// base holds counts whatever the .gitignore says, so the edit to a.md is
// the one difference, and its diff carries no carriage return, since git
// normalises what it adds. With a.md put back by hand the skill is current
// again, both files being ones git ignores.
func TestIgnoreRulesAndAttributesApplyAsGitAppliesThem(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	want := libraryTree(t, lib)
	writeFile(t, filepath.Join(lib, ".gitignore"), "*\n")
	writeFile(t, filepath.Join(lib, ".gitattributes"), "* text eol=crlf\n")
	writeFile(t, filepath.Join(lib, "a.md"), "the same bytes\nand a line of mine\n")
	equal(t, "state", h.listed("pdf")["state"], stateModified)

	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "pdf").stdout, "diff")
	if len(diffs) != 1 || diffs[0]["path"] != "a.md" || diffs[0]["status"] != diffModified {
		t.Fatalf("diffs = %v, want a.md modified alone", diffs)
	}
	patch := diffs[0]["patch"].(string)
	contains(t, "a.md's diff", patch, "\n+and a line of mine\n")
	if strings.Contains(patch, "\r") {
		t.Errorf("a.md's diff carries a carriage return:\n%q", patch)
	}

	writeFile(t, filepath.Join(lib, "a.md"), want["a.md"])
	equal(t, "state with a.md put back", h.listed("pdf")["state"], stateCurrent)
}

// TestSkillDiffTakesARelativeHome names agentx home and the library
// relative to the working directory. git runs in the skill directory, so
// the paths agentx gives it are made absolute first: the diff shows the
// edit.
func TestSkillDiffTakesARelativeHome(t *testing.T) {
	// Not parallel: it changes the process's working directory.
	h, _ := driftHarness(t)
	root := filepath.Dir(h.agentx)
	t.Chdir(root)
	for key, path := range map[string]string{"AGENTX_HOME": h.agentx, "AGENTX_LIBRARY": h.library} {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		h.env[key] = rel
	}
	writeFile(t, filepath.Join(h.library, "pdf", "a.md"), "the same bytes\nand a line of mine\n")

	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "pdf").stdout, "diff")
	if len(diffs) != 1 || diffs[0]["path"] != "a.md" || diffs[0]["status"] != diffModified {
		t.Fatalf("diffs = %v, want a.md modified alone", diffs)
	}
}

// TestSkillDiffOfAFork compares a greenfield skill never published, with
// a .gitignore at its worktree's root that a commit made with git put
// there, with its creation commit and with a commit --commit names. An
// edit, a new file, a file the root .gitignore names and a file the
// system-file list names make two diffs, with every path relative to the
// skill's directory and never the root's own entries; once the edits are
// recorded on its branch, unpublished, the diff still shows them, and the
// fork matches the commit they were recorded as. A commit the account
// repo does not hold, one that holds no skill directory of that name, and
// --update with --commit are refused.
func TestSkillDiffOfAFork(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	h.mustRun("skill", "new", "notes")
	first := h.ref(lineage.ForkRef("notes"))
	root := filepath.Join(h.agentx, "worktrees", "notes")
	writeFile(t, filepath.Join(root, ".gitignore"), "*.log\n")
	gitIn(t, h, root, "add", "-A")
	gitIn(t, h, root, "commit", "-q", "-m", "Ignore logs")
	writeFile(t, filepath.Join(h.library, "notes", "SKILL.md"), skill("notes", "Edited"))
	writeFile(t, filepath.Join(h.library, "notes", "extra.md"), "extra\n")
	writeFile(t, filepath.Join(h.library, "notes", "debug.log"), "ignored by the root .gitignore\n")
	writeFile(t, filepath.Join(h.library, "notes", ".DS_Store"), "finder\n")

	files := func(out outcome) string {
		t.Helper()
		var got []string
		for _, d := range h.eventsOfType(out.stdout, "diff") {
			got = append(got, d["path"].(string)+" "+d["status"].(string))
		}
		return strings.Join(got, ",")
	}
	out := h.mustRun("--json", "skill", "diff", "notes")
	equal(t, "the diffs against the creation commit", files(out), "SKILL.md modified,extra.md added")
	diffs := h.eventsOfType(out.stdout, "diff")
	contains(t, "SKILL.md's patch", diffs[0]["patch"].(string), "diff --git a/SKILL.md b/SKILL.md\n")
	equal(t, "the result against the creation commit", h.one(out.stdout, "result")["summary"], "notes differs from its creation commit "+short(first)+" in 2 files")

	committed := h.record("notes")
	out = h.mustRun("--json", "skill", "diff", "notes")
	equal(t, "the diffs once recorded", files(out), "SKILL.md modified,extra.md added")
	equal(t, "the result once recorded", h.one(out.stdout, "result")["summary"], "notes differs from its creation commit "+short(first)+" in 2 files")
	equal(t, "the text against the recorded commit", h.mustRun("skill", "diff", "notes", "--commit", short(committed)).stdout, "notes matches commit "+short(committed)+"\n")

	for _, c := range []struct {
		args    []string
		message string
		exit    int
	}{
		{[]string{"--commit", "deadbeef"}, "the account repo holds no commit deadbeef", 6},
		{[]string{"--commit", h.ref(lineage.ManagedRef("alpha"))}, "holds no skill directory notes to compare notes with", 6},
		{[]string{"--commit", first, "--update"}, "--update and --commit cannot be given together", 1},
	} {
		out := h.run(append([]string{"--json", "skill", "diff", "notes"}, c.args...)...)
		equal(t, strings.Join(c.args, " ")+": exit", out.exit, c.exit)
		contains(t, strings.Join(c.args, " ")+": message", h.one(out.stdout, "error")["message"].(string), c.message)
	}
}
