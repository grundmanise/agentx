package cli

import (
	"path/filepath"
	"testing"
)

// TestPlaceAdoptsADirectoryOfThisVersionAndSaysSo covers the one placement
// that changes whose a directory is without writing a byte: the path
// already holds a real directory of exactly this version, so agentx keeps
// it and from then on treats it as a placement of its own — which means a
// later removal deletes it, with whatever the user has since put inside.
//
// That is an adoption whichever way the placement was asked for. With
// --copy nothing is written and the configuration only gains a copy_mode
// entry, which made it easy to report as an ordinary placement; the
// consequence for the user is the same, so the report has to be the same.
func TestPlaceAdoptsADirectoryOfThisVersionAndSaysSo(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what     string
		args     []string
		copyMode string
	}{
		{"a symlink placement", nil, ""},
		{"a copy placement", []string{"--copy"}, "cursor"},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			t.Run("names the path it adopted", func(t *testing.T) {
				t.Parallel()
				h, place := adoptable(t)
				out := h.run(append([]string{"skill", "place", "alpha", "--to", "cursor"}, c.args...)...)
				equal(t, "exit", out.exit, 0)
				contains(t, "the output", out.stdout, "adopted "+place)
				equal(t, "copy_mode", copyModeOf(t, h, "alpha"), c.copyMode)
			})
			t.Run("counts it in the result", func(t *testing.T) {
				t.Parallel()
				h, _ := adoptable(t)
				out := h.run(append([]string{"--json", "skill", "place", "alpha", "--to", "cursor"}, c.args...)...)
				equal(t, "exit", out.exit, 0)
				contains(t, "the result", out.stdout, "1 placement adopted")
			})
		})
	}
}

// adoptable is a machine whose Cursor placement path already holds a
// directory of the user's that is byte for byte the library's version of
// alpha, as a cp -a of the library directory leaves it. It returns the
// harness and that path.
func adoptable(t *testing.T) (*harness, string) {
	t.Helper()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "codex").exit, 0)
	place := filepath.Join(h.home, ".cursor", "skills", "alpha")
	copyTree(t, filepath.Join(h.library, "alpha"), place)
	return h, place
}
