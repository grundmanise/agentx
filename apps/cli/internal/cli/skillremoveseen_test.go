package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWholeRemovalSaysWhoStillSeesTheSkill covers what a removal without
// --from claims. It reports every covered configuration as having lost the
// skill, and for a client that reads the library that claim rests on the
// library entry alone — the removal never looks at a directory of that
// client's own, because it deletes only what agentx placed.
//
// So a leftover directory under the same name leaves the client seeing the
// skill a second after the run said it did not, and a whole removal emits
// no skill event for anything to correct the claim with. The run has to say
// it itself.
func TestWholeRemovalSaysWhoStillSeesTheSkill(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)

	// Codex reads the library, so the removal never examines its own skills
	// directory. Cursor reads that directory too, so both clients keep the
	// skill after the library entry goes.
	leftover := filepath.Join(h.home, ".codex", "skills", "alpha")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "SKILL.md"), []byte(skill("alpha", "A leftover of mine")), 0o644); err != nil {
		t.Fatal(err)
	}

	out := h.run("skill", "remove", "alpha")
	equal(t, "exit", out.exit, 0)
	contains(t, "the warning", out.stderr, "codex still sees alpha at "+leftover)
	contains(t, "the warning", out.stderr, "cursor still sees alpha at "+leftover)

	// What a scan says a moment later is what the run said, not the
	// opposite of it.
	sc := h.run("scan")
	equal(t, "scan", sc.exit, 0)
	contains(t, "the scan", sc.stdout, leftover)
}

// TestWholeRemovalNamesAPlacementItLeftOnlyOnce keeps the new warning from
// repeating one the planning already made: a placement left in place is
// reported when it is left, and the configuration is not told a second time
// that it still sees the skill.
func TestWholeRemovalNamesAPlacementItLeftOnlyOnce(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)
	mine := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte(skill("alpha", "A directory of mine")), 0o644); err != nil {
		t.Fatal(err)
	}

	out := h.run("skill", "remove", "alpha")
	equal(t, "exit", out.exit, 0)
	contains(t, "the warning", out.stderr, "is a directory agentx did not place there")
	if n := strings.Count(out.stderr, mine); n != 1 {
		t.Errorf("%s is named %d times on stderr, want once:\n%s", mine, n, out.stderr)
	}
}
