package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillFilesReadsEveryFileOfALibrarySkill reads a managed skill that
// holds, beside the upstream's files, a nested script, a binary file, a
// file over the limit, a file git ignores, a file with control characters
// and a byte that is not UTF-8, and a symlink to a file: one event per
// file sorted by path, each as the golden fixture pins it, the binary and
// the oversized file described and never dumped, the ignored file marked,
// the control characters passed through JSON escaping alone. A symlink to
// a directory is left out with a warning. The library is only read.
func TestSkillFilesReadsEveryFileOfALibrarySkill(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	writeFile(t, filepath.Join(lib, "logo.png"), "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	writeFile(t, filepath.Join(lib, "big.md"), strings.Repeat("a line of a large file\n", skillFileLimit/23+1))
	writeFile(t, filepath.Join(lib, ".gitignore"), "node_modules/\n")
	writeFile(t, mkdirs(t, filepath.Join(lib, "node_modules", "dep"), "index.js"), "module.exports = 1\n")
	writeFile(t, filepath.Join(lib, "notes.md"), "tab\there, an escape \x1b[31m, caf\xe9 and no newline at the end")
	writeFile(t, filepath.Join(lib, "empty.md"), "")
	link(t, "a.md", filepath.Join(lib, "alias.md"))
	before := libraryEntries(t, h)
	edited := libraryTree(t, lib)

	out := h.mustRun("--json", "skill", "files", "pdf")
	equal(t, "stderr", out.stderr, "")
	events := h.eventsOfType(out.stdout, "skill_file")
	var paths []string
	for _, e := range events {
		equal(t, "name", e["name"], "pdf")
		paths = append(paths, e["path"].(string))
	}
	equal(t, "the paths", strings.Join(paths, " "), ".gitignore SKILL.md a.md alias.md b.md big.md bin/run.sh c.md empty.md logo.png node_modules/dep/index.js notes.md")
	byPath := map[string]jsonEvent{}
	for _, e := range events {
		byPath[e["path"].(string)] = e
	}
	// Control characters and the Latin-1 byte reach the event through JSON
	// escaping alone: the tab and the escape as they were, the byte as
	// U+FFFD, since a JSON string is UTF-8.
	equal(t, "notes.md", byPath["notes.md"]["content"], "tab\there, an escape \x1b[31m, caf� and no newline at the end")
	equal(t, "notes.md's lines", byPath["notes.md"]["lines"], float64(1))
	equal(t, "empty.md", byPath["empty.md"]["content"], "")
	equal(t, "empty.md's lines", byPath["empty.md"]["lines"], float64(0))
	equal(t, "alias.md", byPath["alias.md"]["content"], "the same bytes\n")
	for _, tc := range []struct{ path, flag string }{{"logo.png", "binary"}, {"big.md", "truncated"}} {
		ev := byPath[tc.path]
		equal(t, tc.path+" "+tc.flag, ev[tc.flag], true)
		for _, absent := range []string{"content", "lines"} {
			if _, ok := ev[absent]; ok {
				t.Errorf("%s carries %s: %v", tc.path, absent, ev)
			}
		}
	}
	equal(t, "big.md's size", byPath["big.md"]["size"], float64(23*(skillFileLimit/23+1)))
	equal(t, "the ignored file", byPath["node_modules/dep/index.js"]["ignored"], true)
	if _, ok := byPath["a.md"]["ignored"]; ok {
		t.Errorf("a.md is marked: %v", byPath["a.md"])
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "pdf has 12 files")

	got := h.portable(out.stdout)
	path := filepath.Join("testdata", "golden", "skill-files.ndjson")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if want, err := os.ReadFile(path); err != nil {
		t.Fatalf("%v; run go test ./internal/cli -run TestSkillFilesReadsEveryFileOfALibrarySkill -update to create it", err)
	} else if got != string(want) {
		t.Errorf("the events differ from %s; run go test ./internal/cli -run TestSkillFilesReadsEveryFileOfALibrarySkill -update after checking the diff\ngot:\n%s\nwant:\n%s", path, got, want)
	}

	// Nothing was written and nothing journaled: the read is a read.
	equal(t, "the library's entries", strings.Join(libraryEntries(t, h), " "), strings.Join(before, " "))
	sameTree(t, "the library directory", libraryTree(t, lib), edited)
	equal(t, "journals", journalCount(t, h), 0)

	text := h.mustRun("skill", "files", "pdf").stdout
	contains(t, "the text", text, "pdf has 12 files in "+lib+"\n")
	contains(t, "the text", text, "  262154 bytes  over 256 KiB\n")
	contains(t, "the text", text, "  16 bytes      binary\n")
	contains(t, "the text", text, "  1 line        ignored\n")
	contains(t, "the text", text, "  0 bytes       0 lines\n")
	excludes(t, "the text", text, "the same bytes")
}

// TestSkillFilesReadsAPluginSkillAndAConfigurationSkill reads a skill a
// plugin provides, named as skill fork names it, and a skill directory of
// a configuration's own, which the library does not hold and no plugin
// provides, whose symlink to a directory is left out with a warning, and
// refuses what none of them holds: a name no plugin
// provides, a configuration that is not detected, a skill the configuration
// does not hold and an empty --configuration.
func TestSkillFilesReadsAPluginSkillAndAConfigurationSkill(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	pluginHome(t, h)
	commit := filepath.Join(h.home, ".claude", "skills", "commit")
	writeFile(t, mkdirs(t, commit, "SKILL.md"), skill("commit", "Write a commit message"))
	writeFile(t, mkdirs(t, filepath.Join(commit, "ref"), "a.md"), "a reference\n")
	link(t, "ref", filepath.Join(commit, "docs"))

	out := h.mustRun("--json", "skill", "files", "formatter:format")
	ev := h.one(out.stdout, "skill_file")
	equal(t, "name", ev["name"], "format")
	equal(t, "path", ev["path"], "SKILL.md")
	equal(t, "content", ev["content"], skill("format", "Format the code with formatter"))
	if _, ok := ev["ignored"]; ok {
		t.Errorf("a plugin's file is marked: %v", ev)
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "format of plugin formatter has 1 file")

	out = h.mustRun("--json", "skill", "files", "commit", "--configuration", "claude-code")
	contains(t, "stderr", out.stderr, `"level":"warn","message":"`+filepath.Join(commit, "docs")+` is a symlink to a directory and is left out"`)
	files := h.eventsOfType(out.stdout, "skill_file")
	equal(t, "files", len(files), 2)
	equal(t, "name", files[0]["name"], "commit")
	equal(t, "path", files[0]["path"], "SKILL.md")
	equal(t, "content", files[0]["content"], skill("commit", "Write a commit message"))
	equal(t, "the nested path", files[1]["path"], "ref/a.md")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "commit in claude-code has 2 files")

	for _, tc := range []struct {
		args []string
		exit status
		says string
	}{
		{[]string{"commit"}, exitNotFound, `neither the library nor any plugin holds a skill called "commit"`},
		{[]string{"commit", "--configuration", "windsurf"}, exitNotFound, `configuration "windsurf" is not detected`},
		{[]string{"format", "--configuration", "claude-code"}, exitNotFound, `claude-code holds no skill called "format"`},
		{[]string{"commit", "--configuration", ""}, exitUsage, "--configuration needs a configuration"},
	} {
		out := h.run(append([]string{"--json", "skill", "files"}, tc.args...)...)
		equal(t, strings.Join(tc.args, " ")+": exit", out.exit, tc.exit.exit)
		contains(t, strings.Join(tc.args, " ")+": error", h.one(out.stdout, "error")["message"].(string), tc.says)
		if n := len(h.eventsOfType(out.stdout, "skill_file")); n != 0 {
			t.Errorf("%s: %d files read", strings.Join(tc.args, " "), n)
		}
	}
}

// TestFileFacts: git's rule for a binary file, a NUL byte within the first
// 8000 bytes and nowhere else; the limit, past which the content is left
// out and nothing is counted; and the line count, newlines plus a last line
// without one.
func TestFileFacts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		data              string
		size              int64
		binary, truncated bool
		lines             int
	}{
		{"empty", "", 0, false, false, 0},
		{"one line", "a\n", 2, false, false, 1},
		{"one line without a newline", "a", 1, false, false, 1},
		{"two lines", "a\nb", 3, false, false, 2},
		{"a NUL at the start", "\x00abc", 4, true, false, 0},
		{"a NUL within the probe", strings.Repeat("a", binaryProbe-1) + "\x00", binaryProbe, true, false, 0},
		{"a NUL past the probe", strings.Repeat("a", binaryProbe) + "\x00\n", binaryProbe + 2, false, false, 1},
		{"at the limit", strings.Repeat("a", skillFileLimit), skillFileLimit, false, false, 1},
		{"over the limit, probed", "a line\n", skillFileLimit + 1, false, true, 0},
		{"over the limit, read whole", strings.Repeat("a", skillFileLimit+1), skillFileLimit + 1, false, true, 0},
		{"over the limit and binary", "\x00", skillFileLimit + 1, true, true, 0},
	} {
		binary, truncated, lines := fileFacts([]byte(tc.data), tc.size)
		got := fmt.Sprintf("%t %t %d", binary, truncated, lines)
		equal(t, tc.name, got, fmt.Sprintf("%t %t %d", tc.binary, tc.truncated, tc.lines))
	}
}
