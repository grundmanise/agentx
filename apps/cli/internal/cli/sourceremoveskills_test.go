package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSourceRemoveWithSkillsTakesItsSkillsInOneChange removes a source with
// --skills: the skills installed from it leave the machine with it, behind
// one change signal, and a hub skill and a skill created by hand stay.
func TestSourceRemoveWithSkillsTakesItsSkillsInOneChange(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	h.mustRun("skill", "add", s.url, "--name", "beta")
	h.mustRun("skill", "new", "mine")
	h.addLibrarySkill(t, "hand")
	version := readVersion(t, h)

	out := h.mustRun("--json", "source", "remove", s.url, "--skills")

	equal(t, "version", readVersion(t, h), version+1)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "removed "+s.url+" and 2 skills")
	equal(t, "source events", len(h.eventsOfType(out.stdout, "source")), 1)
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 0)
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Lstat(filepath.Join(h.library, name)); !os.IsNotExist(err) {
			t.Errorf("the library still holds %s: %v", name, err)
		}
	}
	equal(t, "import branches", h.storeGit("for-each-ref", "refs/heads/managed/"), "")
	for _, name := range []string{"mine", "hand"} {
		if _, err := os.Stat(filepath.Join(h.library, name)); err != nil {
			t.Errorf("%s left the library: %v", name, err)
		}
	}
}

// TestSourceRemoveWithSkillsStopsAtASkillItCannotRemove gives beta a
// pending merge, which refuses its removal: alpha is gone and reported,
// that change is signalled, and beta and the source stay.
func TestSourceRemoveWithSkillsStopsAtASkillItCannotRemove(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	h.mustRun("skill", "add", s.url, "--name", "beta")
	h.storeGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason,
		pendingCheckout(h, "beta"), h.ref(lineage.ManagedRef("beta")))
	version := readVersion(t, h)

	out := h.run("source", "remove", s.url, "--skills")

	equal(t, "exit", out.exit, 4)
	contains(t, "stdout", out.stdout, "removed alpha from the library")
	equal(t, "version", readVersion(t, h), version+1)
	if _, err := os.Lstat(filepath.Join(h.library, "alpha")); !os.IsNotExist(err) {
		t.Errorf("the library still holds alpha: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.library, "beta")); err != nil {
		t.Errorf("beta left the library: %v", err)
	}
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 1)
}

// TestSourceRemoveWithSkillsRefusesTheHub: the hub's skills are not
// installed from it, so the flag is refused rather than ignored.
func TestSourceRemoveWithSkillsRefusesTheHub(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	remote := newHubRemote(t, h)
	h.setHub(remote)

	out := h.run("--json", "source", "remove", remote, "--skills")

	equal(t, "exit", out.exit, 1)
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 1)
}
