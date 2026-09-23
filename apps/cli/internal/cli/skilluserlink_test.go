package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// A link of the user's that resolves into the library is the one placement
// no command may touch. It reads as the library's content and hashes as the
// library's version, so every cheap test for "is this ours" says yes; only
// what the link names says no. The contract says it twice — a removal
// leaves "a symlink of theirs pointing somewhere else however it resolves"
// alone, and a placement skips "a symlink somewhere other than the library
// included however its target reads" — and the help center sells it as the
// central safety promise of the command.
//
// These tests are the guard on that. Each one puts the user's own link at
// the placement path, pointing at a directory of theirs which in turn
// points into the library, and asserts the link is still theirs afterwards.

// userLink puts a link of the user's at <dir>/skills/<name>, where a
// client's placement goes: that path holds a symlink to ~/mylinks/<name>,
// which is itself a symlink to the library directory. Following it twice reaches the library; reading it
// once does not. It returns the placement path and what the link must still
// read afterwards.
func userLink(t *testing.T, h *harness, dir, name string) (place, target string) {
	t.Helper()
	mine := filepath.Join(h.home, "mylinks")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	target = filepath.Join(mine, name)
	if err := os.Symlink(filepath.Join(h.library, name), target); err != nil {
		t.Fatal(err)
	}
	place = filepath.Join(h.home, dir, "skills", name)
	if err := os.MkdirAll(filepath.Dir(place), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(place); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, place); err != nil {
		t.Fatal(err)
	}
	return place, target
}

// stillTheirs fails unless path is still the same symlink, naming target.
func stillTheirs(t *testing.T, what, path, target string) {
	t.Helper()
	got, ok := isSymlink(t, path)
	if !ok {
		t.Fatalf("%s: %s is no longer a symlink of the user's", what, path)
	}
	if got != target {
		t.Errorf("%s: %s links to %q, want %q", what, path, got, target)
	}
}

// TestSkillRemoveLeavesALinkThatOnlyResolvesIntoTheLibrary is the data-loss
// guard. The placement path holds a link of the user's that reaches the
// library only by way of a link of their own. Deleting it would take a path
// agentx never wrote, and take it silently: the run would report it as a
// placement removed.
func TestSkillRemoveLeavesALinkThatOnlyResolvesIntoTheLibrary(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	place, target := userLink(t, h, ".claude", "alpha")

	out := h.run("--json", "skill", "remove", "alpha", "--from", "claude-code")
	equal(t, "exit", out.exit, 0)
	stillTheirs(t, "after a removal", place, target)
	contains(t, "the warning", out.stderr, place)
	contains(t, "the warning", out.stderr, "points at "+target+", not at ")
	contains(t, "the result", out.stdout, "removed alpha from claude-code: 0 placements, 1 placement left in place")
}

// TestSkillPlaceLeavesALinkThatOnlyResolvesIntoTheLibrary is the other side
// of the same predicate: a placement over that link must not claim to have
// made a link it did not make, and --copy must not delete it to publish a
// copy over it.
func TestSkillPlaceLeavesALinkThatOnlyResolvesIntoTheLibrary(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what string
		args []string
	}{
		{"a symlink placement", nil},
		{"a copy placement", []string{"--copy"}},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "codex").exit, 0)
			place, target := userLink(t, h, ".cursor", "alpha")

			out := h.run(append([]string{"skill", "place", "alpha", "--to", "cursor"}, c.args...)...)
			equal(t, "exit", out.exit, 0)
			stillTheirs(t, "after a placement", place, target)
			contains(t, "the warning", out.stderr, place)
			contains(t, "the warning", out.stderr, "is a link of your own")
			contains(t, "the result", out.stdout, "placed alpha in 0 configurations, 1 placement skipped")
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")
		})
	}
}

// TestSkillAddLeavesALinkThatOnlyResolvesIntoTheLibrary is the install's
// half of it: the same path, reached through skill add rather than through
// skill place, since the two share the code that decides.
func TestSkillAddLeavesALinkThatOnlyResolvesIntoTheLibrary(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "codex").exit, 0)
	place, target := userLink(t, h, ".cursor", "alpha")

	out := h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	stillTheirs(t, "after an install", place, target)
	contains(t, "the warning", out.stderr, "is a link of your own")
	contains(t, "the result", out.stdout, "1 placement skipped")
}
