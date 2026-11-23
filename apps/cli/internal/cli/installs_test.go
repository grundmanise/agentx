package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// installsOf is the installs the inventory of a scan lists, by their name.
func installsOf(t *testing.T, h *harness) map[string]map[string]any {
	t.Helper()
	inventory := h.one(h.mustRun("--json", "scan").stdout, "inventory")
	byName := map[string]map[string]any{}
	for _, e := range inventory["installs"].([]any) {
		entry := e.(map[string]any)
		byName[entry["name"].(string)] = entry
	}
	return byName
}

// storedInstall is the entry installs.json holds of the logical asset
// that source and subpath, or forkID, name, read without a scan.
func storedInstall(t *testing.T, h *harness, source, subpath, forkID string) home.Install {
	t.Helper()
	list, err := home.LoadInstalls(h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.ForkID == forkID && e.Source == source && (e.Subpath == nil || *e.Subpath == subpath) {
			return e
		}
	}
	t.Fatalf("installs.json holds no entry of %q %q %q: %+v", source, subpath, forkID, list)
	return home.Install{}
}

// TestInstallsRecordWhenSkillsEnterAndLeaveTheLibrary: skill add records
// when a skill of a source entered the library, by its source and subpath,
// and skill new when a hub skill did, by its fork id; installing
// a skill the library already holds again adds nothing. A removal from
// some configurations leaves the dates alone, and a removal of the whole
// skill records when it left, keeping when it was added, which the next
// add overwrites while the removal stays.
func TestInstallsRecordWhenSkillsEnterAndLeaveTheLibrary(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	h.mustRun("skill", "new", "notes")
	installs := installsOf(t, h)
	alpha, notes := installs["alpha"], installs["notes"]
	equal(t, "the source", alpha["source"], s.url)
	equal(t, "the subpath", alpha["subpath"], "skills/alpha")
	equal(t, "a fork id of a skill of a source", alpha["fork_id"], nil)
	if notes["fork_id"] == nil || notes["source"] != nil {
		t.Errorf("notes = %v, want it by its fork id alone", notes)
	}
	for _, e := range []map[string]any{alpha, notes} {
		if _, ok := e["added"].(string); !ok || e["removed"] != nil {
			t.Errorf("%v: want an added date and no removed one", e)
		}
	}

	// An earlier date written over alpha's tells an add that records one
	// from an add that does not.
	const earlier = "2026-01-02T03:04:05Z"
	path := filepath.Join(h.agentx, "installs.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored []map[string]any
	if err := json.Unmarshal(b, &stored); err != nil {
		t.Fatal(err)
	}
	for _, e := range stored {
		if e["name"] == "alpha" {
			e["added"] = earlier
		}
	}
	if b, err = json.Marshal(stored); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	h.mustRun("skill", "remove", "alpha", "--from", "claude-code")
	alpha = installsOf(t, h)["alpha"]
	equal(t, "added after installing it again and removing it from one client", alpha["added"], earlier)
	equal(t, "removed after removing it from one client", alpha["removed"], nil)

	h.mustRun("skill", "remove", "alpha")
	h.mustRun("skill", "remove", "notes")
	installs = installsOf(t, h)
	alpha, notes = installs["alpha"], installs["notes"]
	equal(t, "added after the removal", alpha["added"], earlier)
	removed, ok := alpha["removed"].(string)
	if !ok {
		t.Fatalf("alpha = %v, want a removed date", alpha)
	}
	if _, ok := notes["removed"].(string); !ok {
		t.Errorf("notes = %v, want a removed date", notes)
	}

	h.mustRun("skill", "add", s.url, "--name", "alpha")
	alpha = installsOf(t, h)["alpha"]
	if alpha["added"] == earlier {
		t.Error("installing alpha again after its removal kept the earlier added date")
	}
	equal(t, "removed after installing it again", alpha["removed"], removed)
}

// TestInstallsThatCannotBeWrittenAreAWarning: the skill is created all the
// same, and the scan says the file could not be read.
func TestInstallsThatCannotBeWrittenAreAWarning(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	if err := os.MkdirAll(filepath.Join(h.agentx, "installs.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := h.run("skill", "new", "notes")
	equal(t, "exit", out.exit, 0)
	contains(t, "the warning", out.stderr, "could not record when skills were added or removed")
	equal(t, "installs", len(installsOf(t, h)), 0)
}
