package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
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

// TestSkillDiffKeepsEveryByteButControls prints a diff whose file holds a
// tab, runs of spaces and an escape sequence: the layout survives and the
// sequence does not reach the terminal. The event carries the bytes.
func TestSkillDiffKeepsEveryByteButControls(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	writeFile(t, filepath.Join(h.library, "pdf", "a.md"), "\tindented  twice\x1b[31m red\n")
	text := h.mustRun("skill", "diff", "pdf").stdout
	contains(t, "the text", text, "\n+\tindented  twice [31m red\n")
	patch := h.one(h.mustRun("--json", "skill", "diff", "pdf").stdout, "diff")["patch"].(string)
	contains(t, "the event", patch, "\n+\tindented  twice\x1b[31m red\n")
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

// TestSkillDiffSaysADeletedFileChanged: a file deleted between the read of
// the directory and git writing it is the directory changing, exit code 6
// with the hint to run the command again, and not an account repo git
// cannot use. A git wrapper deletes it as the blobs are written.
func TestSkillDiffSaysADeletedFileChanged(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	file := filepath.Join(h.library, "pdf", "new.md")
	writeFile(t, file, "a file of my own\n")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	rm, err := exec.LookPath("rm") // the wrapper's PATH holds nothing but itself
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" hash-object "*) `+rm+` -f `+shellWord(file)+` ;;
esac
exec `+real+` "$@"
`)
	out := h.run("--json", "skill", "diff", "pdf")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	contains(t, "message", e["message"].(string), "pdf changed while agentx read it")
	equal(t, "hint", e["hint"], "run the command again")
}

// TestSkillDiffAndRevertRefuseWhatHasNoBase: a name the library does not
// hold, an unmanaged skill and a fork each have no base version this
// command can read.
func TestSkillDiffAndRevertRefuseWhatHasNoBase(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	writeFile(t, mkdirs(t, filepath.Join(h.library, "mine"), "SKILL.md"), skill("mine", "My own"))
	writeFile(t, mkdirs(t, filepath.Join(h.library, "forked"), "SKILL.md"), skill("forked", "A fork"))
	h.accountGit("update-ref", "refs/heads/skills/forked", h.accountGit("rev-parse", "refs/heads/managed/pdf"))
	for _, verb := range []string{"diff", "revert"} {
		for _, c := range []struct {
			name, message string
			exit          int
		}{
			{"nowhere", `the library holds no skill called "nowhere"`, 5},
			{"mine", "mine is not managed by agentx, so it has no base version to", 6},
			{"forked", "forked is a fork on this machine", 6},
		} {
			out := h.run("--json", "skill", verb, c.name)
			equal(t, verb+" "+c.name+": exit", out.exit, c.exit)
			contains(t, verb+" "+c.name+": message", h.one(out.stdout, "error")["message"].(string), c.message)
		}
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

// TestIgnoreRulesAndAttributesChangeNothing: a skill holding a .gitignore
// that ignores everything and a .gitattributes that asks for CRLF line
// endings is compared and reverted as the bytes it holds. Neither file
// hides a file from the diff or changes a byte of one, and the revert
// takes both away with the rest of the edit.
func TestIgnoreRulesAndAttributesChangeNothing(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	base := libraryTree(t, lib)
	writeFile(t, filepath.Join(lib, ".gitignore"), "*\n")
	writeFile(t, filepath.Join(lib, ".gitattributes"), "* text eol=crlf\n")
	writeFile(t, filepath.Join(lib, "a.md"), "the same bytes\nand a line of mine\n")

	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "pdf").stdout, "diff")
	var got []string
	for _, d := range diffs {
		got = append(got, d["path"].(string)+" "+d["status"].(string))
	}
	if want := []string{".gitattributes added", ".gitignore added", "a.md modified"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("diffs = %v, want %v", got, want)
	}
	patch := diffs[2]["patch"].(string)
	contains(t, "a.md's diff", patch, "\n+and a line of mine\n")
	if strings.Contains(patch, "\r") {
		t.Errorf("a.md's diff carries a carriage return the file does not hold:\n%q", patch)
	}

	h.mustRun("skill", "revert", "pdf")
	sameTree(t, "the library directory", libraryTree(t, lib), base)
	for _, name := range []string{".gitignore", ".gitattributes"} {
		if _, err := os.Lstat(filepath.Join(lib, name)); err == nil {
			t.Errorf("%s is still there after the revert", name)
		}
	}
	equal(t, "state", h.listed("pdf")["state"], stateCurrent)
}
