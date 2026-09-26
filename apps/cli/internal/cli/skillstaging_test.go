package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestARefusedInstallLeavesNoStagingDirectory refuses an install after it
// has already staged content and checks that nothing of it is left. Only
// the journal ever names a staging directory, so one a refusal leaves
// behind is removed by the sweep of the next install into that same
// directory and by nothing else, and a copy staged in a client directory
// that no later install targets again is never removed at all.
func TestARefusedInstallLeavesNoStagingDirectory(t *testing.T) {
	t.Parallel()
	t.Run("a refusal after the placements were staged", func(t *testing.T) {
		t.Parallel()
		h, s := installHarness(t)
		// copy_mode that is not a map of names to configuration ids: the
		// settings file parses, so the install runs, and the write that
		// records the copies is the last thing to refuse, after every
		// placement has staged its copy.
		spoilCopyMode(t, h)

		out := h.run("skill", "add", s.url, "--skill", "alpha", "--copy")
		if out.exit == 0 {
			t.Fatalf("the install did not refuse:\n%s", out.stderr)
		}
		equal(t, "journals", journalCount(t, h), 0)
		for _, dir := range []string{h.library, filepath.Join(h.home, ".claude", "skills"), filepath.Join(h.home, ".cursor", "skills")} {
			if left := stagingIn(t, dir); len(left) > 0 {
				t.Errorf("the refused install left %v in %s", left, dir)
			}
		}
	})

	t.Run("a refusal on the import branch", func(t *testing.T) {
		t.Parallel()
		h, s := installHarness(t)
		equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
		s.skill("skills/alpha", "alpha", "The first skill, moved on", map[string]string{"notes.md": "newer\n"})
		s.commit("a newer version")
		equal(t, "exit", h.run("source", "fetch", s.url).exit, 0)
		if err := os.RemoveAll(filepath.Join(h.library, "alpha")); err != nil {
			t.Fatal(err)
		}

		out := h.run("skill", "add", s.url, "--skill", "alpha")
		equal(t, "exit", out.exit, 6)
		contains(t, "stderr", out.stderr, "already managed at another version")
		if left := stagingIn(t, h.library); len(left) > 0 {
			t.Errorf("the refused install left %v in the library", left)
		}
	})
}

// spoilCopyMode leaves the settings file parseable but its copy_mode
// unreadable, so that the write recording the copies is the last thing an
// install refuses on: every placement has staged its copy by then.
func spoilCopyMode(t *testing.T, h *harness) {
	t.Helper()
	path := filepath.Join(h.agentx, "settings.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	settings["copy_mode"] = 42
	spoiled, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(spoiled)+"\n")
}

// stagingIn names the staging directories left in dir.
func stagingIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var left []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentx-staged-") {
			left = append(left, e.Name())
		}
	}
	return left
}

// TestStagedContentIsCheckedAgainstTheVersion is the guard that stands
// between the blobs an install read and the directory it renames into
// place: only a directory whose content hash is the version's is published.
// It is the last thing that can tell a version laid out on disk from
// something else, since the content hash is computed from the tree in
// process and never from the files the install wrote.
func TestStagedContentIsCheckedAgainstTheVersion(t *testing.T) {
	t.Parallel()
	body := "---\nname: alpha\ndescription: The first skill\n---\n\n# alpha\n"
	files := []treeFile{
		{path: "SKILL.md", mode: source.FileMode, body: body},
		{path: "scripts/run.sh", mode: source.ExecutableMode, body: "#!/bin/sh\n"},
	}
	version := scan.ContentHash("alpha", "The first skill", []scan.File{
		{Path: "SKILL.md", Content: body},
		{Path: "scripts/run.sh", Content: "#!/bin/sh\n"},
	})
	inv := &invocation{}
	root := t.TempDir()

	// The version's own hash goes through and the files land.
	good := filepath.Join(root, "good")
	if err := inv.stageCopy(good, (&imported{name: "alpha", files: files, hash: version}).placeable()); err != nil {
		t.Fatalf("the version's own content hash was refused: %v", err)
	}
	if _, err := os.Stat(filepath.Join(good, "scripts", "run.sh")); err != nil {
		t.Errorf("the staged directory was not laid out: %v", err)
	}

	// Any other hash does not, and the refusal names both hashes so that a
	// reader can see which version was expected.
	bad := filepath.Join(root, "bad")
	err := inv.stageCopy(bad, (&imported{name: "alpha", files: files, hash: strings.Repeat("0", 64)}).placeable())
	if err == nil {
		t.Fatal("a staged directory whose content hash is not the version's was accepted")
	}
	contains(t, "the refusal", err.Error(), version)
	contains(t, "the refusal", err.Error(), strings.Repeat("0", 64))
}

// TestSkillAddRefusesANameItCannotUse refuses, before anything is written,
// a frontmatter name agentx cannot use. A name is two things at once: a
// directory of the library and one level of refs/heads/managed/<name>. A
// name only one of them accepts would be found out halfway through the
// mutation, with the journal already on disk and the ref step failing, and
// from then on every command would recover that journal and fail the same
// way. The name comes out of a source's SKILL.md, so it is checked and not
// trusted.
func TestSkillAddRefusesANameItCannotUse(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("names", true)
	cases := []struct{ what, name string }{
		{"a separator", "a/b"},
		{"a hidden name", ".hidden"},
		{"a space, which git refuses in a ref", "my skill"},
		{"a colon, which git refuses in a ref", "we:ird"},
		{"a name ending in .lock, which git refuses in a ref", "alpha.lock"},
		{"two dots, which git refuses in a ref", "a..b"},
	}
	for i, c := range cases {
		s.skill(fmt.Sprintf("tools/n%d", i), c.name, "A name agentx cannot use", nil)
	}
	s.commit("skills whose names agentx cannot use")
	equal(t, "source add", h.run("source", "add", s.url).exit, 0)

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			out := h.run("skill", "add", s.url, "--skill", c.name)
			equal(t, "exit", out.exit, 6)
			contains(t, "stderr", out.stderr, c.name)
			// Nothing was written, so the machine still works afterwards.
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "the command after it", h.run("config", "set", "label", "after").exit, 0)
		})
	}
}
