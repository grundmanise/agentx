package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// ignoreHarness installs three skills of one source: pdf; web, whose
// upstream ships a .gitignore naming node_modules/ and a backup file the
// system-file list names; and mac, whose upstream ships a .gitignore that
// takes .DS_Store back from the list.
func ignoreHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("tools", true)
	s.advance("ignoreHarness", func(s *sourceRepo) []string {
		s.skill("tools/pdf", "pdf", "Plain files", map[string]string{"a.md": "the same bytes\n"})
		s.skill("tools/web", "web", "Ships ignore rules of its own", map[string]string{
			".gitignore": "node_modules/\n", "a.md": "the same bytes\n", "SKILL.md~": "a backup the upstream ships\n",
		})
		s.skill("tools/mac", "mac", "Keeps its Finder file", map[string]string{".gitignore": "!.DS_Store\n"})
		s.commit("three skills")
		return nil
	})
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--skill", "pdf", "--skill", "web", "--skill", "mac")
	return h
}

// diffPaths is what skill diff says differs, one "path status" per file.
func diffPaths(h *harness, name string) string {
	h.t.Helper()
	var got []string
	for _, d := range h.eventsOfType(h.mustRun("--json", "skill", "diff", name).stdout, "diff") {
		got = append(got, d["path"].(string)+" "+d["status"].(string))
	}
	return strings.Join(got, ", ")
}

// TestSystemFilesLeaveASkillCurrent: a .DS_Store, an AppleDouble file and
// an editor's backup leave a skill current and out of its diff while
// ignore_system_files is on, and count as files of the skill once it is
// off.
func TestSystemFilesLeaveASkillCurrent(t *testing.T) {
	t.Parallel()
	h := ignoreHarness(t)
	lib := filepath.Join(h.library, "pdf")
	writeFile(t, filepath.Join(lib, ".DS_Store"), "finder\n")
	writeFile(t, filepath.Join(lib, "._a.md"), "apple double\n")
	writeFile(t, filepath.Join(lib, "SKILL.md~"), "a backup\n")
	equal(t, "state", h.listed("pdf")["state"], stateCurrent)
	equal(t, "the diff", diffPaths(h, "pdf"), "")

	writeFile(t, filepath.Join(lib, "a.md"), "an edit\n")
	equal(t, "the diff with an edit", diffPaths(h, "pdf"), "a.md modified")
	restore(t, filepath.Join(lib, "a.md"), "the same bytes\n")

	h.mustRun("config", "set", "ignore_system_files", "false")
	equal(t, "state with the setting off", h.listed("pdf")["state"], stateModified)
	equal(t, "the diff with the setting off", diffPaths(h, "pdf"), ".DS_Store added, ._a.md added, SKILL.md~ added")
	h.mustRun("config", "set", "ignore_system_files", "true")
	equal(t, "state with the setting on again", h.listed("pdf")["state"], stateCurrent)
}

// TestASkillsOwnIgnoreRulesApply: a directory the skill's .gitignore names
// leaves it current, a file its .gitignore takes back from the list counts,
// and so does an edit to a file the upstream ships although the list names
// it.
func TestASkillsOwnIgnoreRulesApply(t *testing.T) {
	t.Parallel()
	h := ignoreHarness(t)
	web := filepath.Join(h.library, "web")
	writeFile(t, mkdirs(t, filepath.Join(web, "node_modules", "pkg"), "index.js"), "module.exports = 1\n")
	writeFile(t, filepath.Join(web, ".DS_Store"), "finder\n")
	equal(t, "web with an ignored directory", h.listed("web")["state"], stateCurrent)
	writeFile(t, filepath.Join(web, "SKILL.md~"), "a backup, edited\n")
	equal(t, "web with the shipped backup edited", h.listed("web")["state"], stateModified)
	equal(t, "web's diff", diffPaths(h, "web"), "SKILL.md~ modified")

	writeFile(t, filepath.Join(h.library, "mac", ".DS_Store"), "finder\n")
	equal(t, "mac with a .DS_Store", h.listed("mac")["state"], stateModified)
	equal(t, "mac's diff", diffPaths(h, "mac"), ".DS_Store added")
}

// TestTheGlobalIgnoreFileApplies: a file only the user's global ignore
// file names leaves a skill current, from git's default place for that
// file and from the place the user's core.excludesFile names.
func TestTheGlobalIgnoreFileApplies(t *testing.T) {
	t.Parallel()
	h := ignoreHarness(t)
	lib := filepath.Join(h.library, "pdf")
	writeFile(t, mkdirs(t, filepath.Join(h.config, "git"), "ignore"), "*.log\n")
	writeFile(t, filepath.Join(lib, "debug.log"), "a log\n")
	equal(t, "state with the default ignore file", h.listed("pdf")["state"], stateCurrent)

	remove(t, filepath.Join(lib, "debug.log"))
	writeFile(t, filepath.Join(h.home, ".gitconfig"), "[core]\n\texcludesFile = ~/my-ignore\n")
	writeFile(t, filepath.Join(h.home, "my-ignore"), "*.tmp\n")
	writeFile(t, filepath.Join(lib, "scratch.tmp"), "scratch\n")
	equal(t, "state with core.excludesFile", h.listed("pdf")["state"], stateCurrent)
	equal(t, "the diff with core.excludesFile", diffPaths(h, "pdf"), "")
}

// TestAGitattributesConversionApplies: a .gitattributes that asks git to
// normalise line endings makes a file that differs only in them the file
// the base holds, as it does in any work tree.
func TestAGitattributesConversionApplies(t *testing.T) {
	t.Parallel()
	h := ignoreHarness(t)
	lib := filepath.Join(h.library, "pdf")
	writeFile(t, filepath.Join(lib, "a.md"), "the same bytes\r\n")
	equal(t, "state with CRLF line endings", h.listed("pdf")["state"], stateModified)
	writeFile(t, filepath.Join(lib, ".gitignore"), ".git*\n")
	writeFile(t, filepath.Join(lib, ".gitattributes"), "*.md text\n")
	equal(t, "state with the conversion", h.listed("pdf")["state"], stateCurrent)
}

// TestRevertKeepsIgnoredFiles: a revert puts back what the skill's files
// were and keeps the files git ignores, a .DS_Store and a directory the
// skill's .gitignore names.
func TestRevertKeepsIgnoredFiles(t *testing.T) {
	t.Parallel()
	h := ignoreHarness(t)
	web := filepath.Join(h.library, "web")
	want := libraryTree(t, web)
	writeFile(t, mkdirs(t, filepath.Join(web, "node_modules", "pkg"), "index.js"), "module.exports = 1\n")
	writeFile(t, filepath.Join(web, ".DS_Store"), "finder\n")
	writeFile(t, filepath.Join(web, "a.md"), "an edit\n")
	writeFile(t, filepath.Join(web, "new.md"), "a file of my own\n")

	h.mustRun("skill", "revert", "web")
	want["node_modules/pkg/index.js"], want[".DS_Store"] = "module.exports = 1\n", "finder\n"
	sameTree(t, "the library directory", libraryTree(t, web), want)
	equal(t, "state", h.listed("web")["state"], stateCurrent)
}

// TestAdoptionIgnoresASystemFile: a directory adopted at the version it
// holds is not modified for a .DS_Store beside it, as skill list says
// afterwards.
func TestAdoptionIgnoresASystemFile(t *testing.T) {
	t.Parallel()
	h, s, v1, _ := adoptHarness(t)
	writeFile(t, filepath.Join(h.library, "alpha", ".DS_Store"), "finder\n")
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha",
	}})
	out := h.mustRun("--json", "adopt", "--skill", "alpha", "--base", v1)
	equal(t, "modified", h.one(out.stdout, "adoption")["modified"], false)
	equal(t, "state", h.listed("alpha")["state"], stateCurrent)
}

// TestConfigSetIgnoreSystemFilesWritesTheList: the setting is on by
// default, and setting it writes the list to the account repo's
// info/exclude, or empties the file, at once.
func TestConfigSetIgnoreSystemFilesWritesTheList(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	exclude := filepath.Join(h.agentx, "account.git", "info", "exclude")
	equal(t, "the default", h.mustRun("config", "get", "ignore_system_files").stdout, "true\n")
	h.mustRun("config", "set", "ignore_system_files", "false")
	equal(t, "info/exclude when off", readText(t, exclude), "")
	h.mustRun("config", "set", "ignore_system_files", "true")
	equal(t, "info/exclude when on", readText(t, exclude), strings.Join(home.SystemFiles, "\n")+"\n")
}

// TestServeKeepsGitsVerdictUntilTheSkillChanges: serve asks git about an
// edited skill once, a refresh with nothing changed runs no git over the
// skill directory, and a refresh after another edit asks git again.
func TestServeKeepsGitsVerdictUntilTheSkillChanges(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	writeFile(t, filepath.Join(h.library, "pdf", "a.md"), "an edit\n")
	calls := countingGit(t, h)
	overSkills := func() int {
		n := 0
		for _, call := range calls() {
			if strings.Contains(call, "--work-tree=") {
				n++
			}
		}
		return n
	}
	p := h.serve(t, "--json")
	p.next("snapshot")
	first := overSkills()
	if first == 0 {
		t.Fatal("the first scan ran no git over the edited skill")
	}
	p.send(`{"type":"refresh","request_id":"r1"}`)
	p.next("refresh_complete")
	equal(t, "git runs over the skill after a refresh", overSkills(), first)
	writeFile(t, filepath.Join(h.library, "pdf", "a.md"), "another edit\n")
	p.send(`{"type":"refresh","request_id":"r2"}`)
	p.until("r2")
	if overSkills() == first {
		t.Error("an edit after the first verdict ran no git over the skill")
	}
	equal(t, "exit", p.close(), 0)
}

// TestCarryIgnoredKeepsWhatTheNewContentHolds: an ignored file is carried
// with its bytes and mode, a symlink as a link, and a path the new content
// holds already keeps the new content's file.
func TestCarryIgnoredKeepsWhatTheNewContentHolds(t *testing.T) {
	t.Parallel()
	from, to := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(from, ".DS_Store"), "finder\n")
	writeFile(t, mkdirs(t, filepath.Join(from, "build"), "run"), "#!/bin/sh\n")
	chmod(t, filepath.Join(from, "build", "run"), 0o755)
	link(t, "a.md", filepath.Join(from, "latest"))
	writeFile(t, filepath.Join(from, "a.md"), "the ignored local file\n")
	writeFile(t, filepath.Join(to, "a.md"), "the new content\n")

	if err := carryIgnored(from, to, []string{".DS_Store", "build/run", "latest", "a.md"}); err != nil {
		t.Fatal(err)
	}
	equal(t, ".DS_Store", readText(t, filepath.Join(to, ".DS_Store")), "finder\n")
	if !executable(t, filepath.Join(to, "build", "run")) {
		t.Error("build/run lost its exec bit")
	}
	if target, err := os.Readlink(filepath.Join(to, "latest")); err != nil || target != "a.md" {
		t.Errorf("latest = %q, %v, want a link to a.md", target, err)
	}
	equal(t, "a.md", readText(t, filepath.Join(to, "a.md")), "the new content\n")
}
