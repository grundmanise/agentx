package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigEnablePlaceAllPlacesEveryLibrarySkill puts the whole library
// into a configuration that was disabled when the skills were installed,
// as one mutation: the settings change and the placements it implies are
// one thing the user asked for.
func TestConfigEnablePlaceAllPlacesEveryLibrarySkill(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)
	equal(t, "add", h.run("skill", "add", s.url, "--all").exit, 0)
	for _, name := range []string{"alpha", "beta"} {
		nothingAt(t, "a placement in the disabled configuration", filepath.Join(h.home, ".cursor", "skills", name))
	}
	before := mutationVersion(t, h)

	out := h.run("--json", "config", "enable", "cursor", "--place-all")
	equal(t, "exit", out.exit, 0)
	for _, name := range []string{"alpha", "beta"} {
		target, ok := isSymlink(t, filepath.Join(h.home, ".cursor", "skills", name))
		if !ok || target != filepath.Join(h.library, name) {
			t.Errorf("%s is %q (symlink %v), want a link to the library", name, target, ok)
		}
	}
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals left behind", journalCount(t, h), 0)
	equal(t, "disabled_configurations", len(readSettingsFile(t, h)["disabled_configurations"].([]any)), 0)

	types := h.types(h.events(out.stdout))
	want := []string{"settings", "progress", "progress", "progress", "library_skill", "library_skill", "result"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("events = %v, want %v\n%s", types, want, out.stdout)
	}
	contains(t, "the result", out.stdout, "cursor is now enabled, 2 skills placed")
}

// TestConfigEnableWithoutPlaceAllChangesOnlyTheDefault: enabling on its own
// decides where future installs go and places nothing, which is what the
// flag exists to change.
func TestConfigEnableWithoutPlaceAllChangesOnlyTheDefault(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)

	out := h.run("--json", "config", "enable", "cursor")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "a placement", filepath.Join(h.home, ".cursor", "skills", "alpha"))
	equal(t, "events", strings.Join(h.types(h.events(out.stdout)), ","), "settings,result")

	// A skill installed after it, though, goes there like anywhere else.
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "beta").exit, 0)
	if _, ok := isSymlink(t, filepath.Join(h.home, ".cursor", "skills", "beta")); !ok {
		t.Error("an install after the configuration was enabled did not place into it")
	}
}

// TestConfigEnablePlaceAllSkipsWhatIsInTheWay follows the rule every
// placement follows: a path holding something agentx did not make is left
// as it is and named, the other skills still land, and the run succeeds.
func TestConfigEnablePlaceAllSkipsWhatIsInTheWay(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)
	equal(t, "add", h.run("skill", "add", s.url, "--all").exit, 0)
	handMade := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(handMade, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(handMade, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")

	out := h.run("--json", "config", "enable", "cursor", "--place-all")
	equal(t, "exit", out.exit, 0)
	contains(t, "the hand-made directory", fileBody(t, filepath.Join(handMade, "SKILL.md")), "description: mine")
	if _, ok := isSymlink(t, filepath.Join(h.home, ".cursor", "skills", "beta")); !ok {
		t.Error("a skill that had nothing in its way was not placed")
	}
	contains(t, "stderr", out.stderr, handMade)
	contains(t, "the result", out.stdout, "1 placement skipped")
	equal(t, "disabled_configurations", len(readSettingsFile(t, h)["disabled_configurations"].([]any)), 0)
}

// TestConfigEnablePlaceAllIntoAClientThatReadsTheLibrary makes no entry of
// its own: that client already sees every library skill. So the run says
// that rather than reporting placements it did not make — the contract asks
// it to say so rather than making a second entry, and claiming to have
// placed what was already there is the same untruth by other means.
func TestConfigEnablePlaceAllIntoAClientThatReadsTheLibrary(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "disable", h.run("config", "disable", "codex").exit, 0)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)

	out := h.run("--json", "config", "enable", "codex", "--place-all")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "an entry of the client's own", filepath.Join(h.home, ".codex", "skills", "alpha"))
	equal(t, "placements", strings.Join(placementsOf(t, h.one(out.stdout, "library_skill")), ";"), "codex library library")
	contains(t, "the result", out.stdout, "codex is now enabled and already sees 1 skill through the library")
	if strings.Contains(out.stdout, "1 skill placed") {
		t.Errorf("the result claims a placement into a client that reads the library:\n%s", out.stdout)
	}

	text := h.run("config", "disable", "codex")
	equal(t, "disable again", text.exit, 0)
	again := h.run("config", "enable", "codex", "--place-all")
	equal(t, "exit", again.exit, 0)
	contains(t, "the output", again.stdout, "codex reads the library and already sees 1 skill; nothing was placed")
	contains(t, "the row", again.stdout, "alpha")
}

// TestConfigDisableTakesNoPlaceAll: the flag belongs to enable alone.
func TestConfigDisableTakesNoPlaceAll(t *testing.T) {
	t.Parallel()
	h, _ := placementHarness(t)
	out := h.run("config", "disable", "cursor", "--place-all")
	equal(t, "exit", out.exit, 1)
	contains(t, "stderr", out.stderr, "place-all")
}

// TestConfigEnablePlaceAllOnAnEmptyLibrary places nothing and still enables
// the configuration.
func TestConfigEnablePlaceAllOnAnEmptyLibrary(t *testing.T) {
	t.Parallel()
	h, _ := placementHarness(t)
	equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)

	out := h.run("--json", "config", "enable", "cursor", "--place-all")
	equal(t, "exit", out.exit, 0)
	equal(t, "disabled_configurations", len(readSettingsFile(t, h)["disabled_configurations"].([]any)), 0)
	contains(t, "the result", out.stdout, "cursor is now enabled, 0 skills placed")
}
