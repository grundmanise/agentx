package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestSourceAddUnwindsItsRemoteWhenTheSettingsWriteFails covers the one
// window an add cannot journal away. The remote is written under the lock,
// the fetch runs outside it so that the network never blocks a scan, and
// the settings entry is written under the lock again. That second hold can
// fail, and the account repo would then hold a remote for a source the
// machine does not know about. `source fetch` and `source skills` both
// answer from the settings, so nothing would ever name it again and
// nothing would clean it up.
//
// A refusal cleans up after itself, the way a failed fetch already does.
//
// The settings file is taken away while the add is parked in its fetch, so
// the write fails inside the hold the run has already won. That is also
// where the unwind runs: the alternative, taking the lock a second time,
// would have the run competing for it with whatever made it fail.
func TestSourceAddUnwindsItsRemoteWhenTheSettingsWriteFails(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("lost", true)
	s.skill("one", "one", "One", nil)
	s.commit("one")

	// Park the add on the update-ref that publishes its fetch: the remote
	// is written, the fetch is done, the settings entry is not.
	arm := gatePublish(t, h)
	reached, release := arm()
	done := make(chan outcome, 1)
	go func() { done <- h.run("source", "add", s.url) }()
	reached()

	// A settings file that cannot be read or replaced. The lock is free, so
	// only the write fails.
	settings := home.SettingsPath(h.agentx)
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatal(err)
	}
	release()
	out := <-done

	if out.exit == 0 {
		t.Fatalf("the add did not fail although its settings write could not land:\n%s", out.stdout)
	}
	// Nothing of the source is left in the account repo: no remote for a
	// source no command would ever name again, and no ref.
	config := h.accountGit("config", "--list", "--local")
	if strings.Contains(config, "remote.src-"+source.ID(s.url)) {
		t.Errorf("the refused add left its remote behind:\n%s", config)
	}
	if refs := h.agentxRefs(); refs != "" {
		t.Errorf("the refused add left refs behind:\n%s", refs)
	}
	// And the machine works again once the settings file is a file.
	if err := os.RemoveAll(settings); err != nil {
		t.Fatal(err)
	}
	equal(t, "source list", h.run("source", "list").exit, 0)
	equal(t, "source add again", h.run("source", "add", s.url).exit, 0)
}

// TestSourceAddKeepsItsRemoteOnceTheEntryIsWritten is the other side of the
// unwind, and the reason it is not simply "the add failed, take the remote
// back". The settings entry is written before the last thing the mutation
// does, the change signal; a failure after the entry has landed must leave
// the remote alone, or the settings would name a source whose remote is
// gone, the same inconsistency the unwind exists to prevent, inverted.
//
// The change signal is made to fail by leaving a directory where its file
// goes, which is the one step of the mutation that runs after the entry.
func TestSourceAddKeepsItsRemoteOnceTheEntryIsWritten(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("kept", true)
	s.skill("one", "one", "One", nil)
	s.commit("one")

	// One add that works first: it creates the account repo, which is its
	// own mutation, so the run under test fails at the change signal of the
	// add itself and nowhere earlier.
	first := h.newSourceRepo("first", true)
	first.skill("one", "one", "One", nil)
	first.commit("one")
	equal(t, "the first add", h.run("source", "add", first.url).exit, 0)

	// The version file is the change signal every mutation rewrites last.
	// A directory in its place fails the rename that publishes it, after
	// the settings entry is already on disk.
	version := filepath.Join(h.agentx, "version")
	if err := os.Remove(version); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(version, 0o755); err != nil {
		t.Fatal(err)
	}
	out := h.run("source", "add", s.url)
	if out.exit == 0 {
		t.Fatalf("the add did not fail although the change signal could not be written:\n%s", out.stdout)
	}

	// The entry landed, so the remote that serves it has to still be there.
	id := source.ID(s.url)
	entries, _ := readSettingsFile(t, h)["sources"].([]any)
	found := false
	for _, raw := range entries {
		if source.ID(raw.(map[string]any)["url"].(string)) == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("the settings hold no entry for the source, so this is not the case under test:\n%v", entries)
	}
	config := h.accountGit("config", "--list", "--local")
	if !strings.Contains(config, "remote.src-"+id+".url") {
		t.Errorf("the settings name the source but its remote was taken back:\n%s", config)
	}
}
