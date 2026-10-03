package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// stagingIn names the .agentx-staged-* entries left in dir, which is what a
// mutation stages beside the live path before its journal exists.
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

// TestACopyPlacementLeavesNoStagedDirectory covers two configurations that
// share one skills directory: Zencoder and Zenflow both use
// ~/.zencoder/skills. A --copy run plans a publish for each of them, so it
// plans two publishes of the same path: the first lands and the second
// finds the path already holding what it was to write, and does nothing.
//
// The directory that second step staged is then beside the live path with
// no step left to publish it and no journal left to name it. Nothing sweeps
// a client's skills directory except a later install into it, so a full
// copy of the skill's content would stay there indefinitely, surviving even
// the removal whose job was to take the skill away.
func TestACopyPlacementLeavesNoStagedDirectory(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".zencoder"}})
	s, _, _ := h.standardSource(true)
	equal(t, "source add", h.run("source", "add", s.url).exit, 0)
	equal(t, "add", h.run("skill", "add", s.url, "--name", "alpha", "--to", "claude-code").exit, 0)

	shared := filepath.Join(h.home, ".zencoder", "skills")
	out := h.run("skill", "place", "alpha", "--to", "zencoder", "--to", "zenflow", "--copy")
	equal(t, "exit", out.exit, 0)
	equal(t, "staged directories after the placement", strings.Join(stagingIn(t, shared), ", "), "")

	// And the placement itself is whole in both configurations.
	contains(t, "the copy", fileBody(t, filepath.Join(shared, "alpha", "notes.md")), "alpha notes")
	contains(t, "the result", out.stdout, "placed alpha in 2 configurations")

	equal(t, "remove", h.run("skill", "remove", "alpha", "--from", "zencoder", "--from", "zenflow").exit, 0)
	equal(t, "staged directories after the removal", strings.Join(stagingIn(t, shared), ", "), "")
}
