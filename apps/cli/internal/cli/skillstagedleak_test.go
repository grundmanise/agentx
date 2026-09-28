package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

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
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)

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
