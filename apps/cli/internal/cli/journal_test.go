package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// writeJournal hand-writes what a settings mutation leaves behind when it
// stops after staging: the staged content and the journal expecting the
// live file to hash to old. With progress "applied" the staged file is
// left out, as after the rename. It returns the journal and staged paths.
func writeJournal(t *testing.T, h *harness, id, progress, old string, staged []byte) (journal, stagedPath string) {
	t.Helper()
	dir := filepath.Join(h.agentx, "mutations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	journal = filepath.Join(dir, id+".json")
	stagedPath = filepath.Join(dir, id+".staged")
	if progress == "staged" {
		if err := os.WriteFile(stagedPath, staged, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := map[string]any{
		"progress": progress,
		"replace": []map[string]string{{
			"path":   filepath.Join(h.agentx, "settings.json"),
			"old":    old,
			"new":    sha(staged),
			"staged": stagedPath,
		}},
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return journal, stagedPath
}

// relabel returns the settings file with its label replaced.
func relabel(t *testing.T, h *harness, label string) []byte {
	t.Helper()
	m := readSettingsFile(t, h)
	m["label"] = label
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func gone(t *testing.T, what, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s %s still exists: %v", what, path, err)
	}
}

func TestScanResumesAStagedJournal(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("config", "set", "label", "one")
	equal(t, "exit", out.exit, 0)
	live := filepath.Join(h.agentx, "settings.json")
	journal, staged := writeJournal(t, h, "1000-aaaa", "staged", sha(readFile(t, live)), relabel(t, h, "two"))
	// And one that stopped between staging and writing its journal: an orphan.
	orphan := filepath.Join(h.agentx, "mutations", "0999-ffff.staged")
	if err := os.WriteFile(orphan, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out = h.run("--json", "scan")
	equal(t, "exit", out.exit, 0)
	equal(t, "stderr", out.stderr, "")
	events := h.events(out.stdout)
	equal(t, "snapshot label", events[0]["machine"].(map[string]any)["label"], "two")
	equal(t, "label", readSettingsFile(t, h)["label"], "two")
	gone(t, "journal", journal)
	gone(t, "staged file", staged)
	equal(t, "version", readVersion(t, h), 2)
	equal(t, "mutations entries", listDir(t, filepath.Join(h.agentx, "mutations")), "")
}

func TestMutationFinishesAnAppliedJournal(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	live := filepath.Join(h.agentx, "settings.json")
	out := h.run("config", "set", "label", "one")
	equal(t, "exit", out.exit, 0)
	old := readFile(t, live)
	out = h.run("config", "set", "label", "two")
	equal(t, "exit", out.exit, 0)
	equal(t, "version", readVersion(t, h), 2)
	// The mutation to "two" renamed its staged file over the live one and stopped before removing its journal.
	journal, _ := writeJournal(t, h, "1000-aaaa", "applied", sha(old), readFile(t, live))

	out = h.run("config", "set", "auto_push", "true")
	equal(t, "exit", out.exit, 0)
	gone(t, "journal", journal)
	file := readSettingsFile(t, h)
	equal(t, "label", file["label"], "two")
	equal(t, "auto_push", file["auto_push"], true)
	equal(t, "version", readVersion(t, h), 3) // the finished journal bumps nothing; this command bumps once
}

func TestEditedLiveFileRefusesRecovery(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("config", "set", "label", "one")
	equal(t, "exit", out.exit, 0)
	live := filepath.Join(h.agentx, "settings.json")
	before := readFile(t, live)
	// The journal expects an older live file: someone edited settings.json meanwhile.
	journal, staged := writeJournal(t, h, "1000-aaaa", "staged", sha([]byte("{}\n")), relabel(t, h, "two"))
	stagedBytes := readFile(t, staged)

	out = h.run("--json", "scan")
	equal(t, "exit", out.exit, 6)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "error.code", events[0]["code"], "refused")
	contains(t, "error.message", events[0]["message"].(string), journal)
	contains(t, "error.message", events[0]["message"].(string), live)
	contains(t, "error.hint", events[0]["hint"].(string), "move the journal aside")

	out = h.run("scan")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, journal)

	out = h.run("--json", "config", "set", "label", "three")
	equal(t, "exit", out.exit, 6)
	equal(t, "error.code", h.events(out.stdout)[0]["code"], "refused")
	out = h.run("machine", "reset-id")
	equal(t, "exit", out.exit, 6)

	if got := readFile(t, live); string(got) != string(before) {
		t.Errorf("settings.json was changed:\n%s", got)
	}
	if got := readFile(t, staged); string(got) != string(stagedBytes) {
		t.Errorf("staged file was changed:\n%s", got)
	}
	if _, err := os.Stat(journal); err != nil {
		t.Errorf("journal: %v", err)
	}
	equal(t, "version", readVersion(t, h), 1)

	// Doctor reports without recovering.
	out = h.run("--json", "doctor")
	equal(t, "exit", out.exit, 0)
	rows, _ := doctorRows(t, h.events(out.stdout))
	equal(t, "mutations.status", rows["mutations"]["status"], "warn")
	contains(t, "mutations.detail", rows["mutations"]["detail"].(string), "1 unfinished mutation")
	contains(t, "mutations.detail", rows["mutations"]["detail"].(string), journal)
	if _, err := os.Stat(journal); err != nil {
		t.Errorf("doctor touched the journal: %v", err)
	}
	out = h.run("doctor")
	contains(t, "stdout", out.stdout, "mutations                warn  1 unfinished mutation: "+journal)

	// Restoring the expected live content lets the mutation finish.
	if err := os.WriteFile(live, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = h.run("--json", "scan")
	equal(t, "exit", out.exit, 0)
	equal(t, "label", readSettingsFile(t, h)["label"], "two")
	gone(t, "journal", journal)
	equal(t, "version", readVersion(t, h), 2)
}

func TestServeRecoversAJournalOnRescan(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	out := h.run("config", "set", "label", "one")
	equal(t, "exit", out.exit, 0)
	live := filepath.Join(h.agentx, "settings.json")
	p := h.serve(t, "--json")
	snap := p.next("snapshot")
	equal(t, "label", snap["machine"].(map[string]any)["label"], "one")
	p.send(`{"type":"refresh","request_id":"idle"}`)
	p.next("refresh_complete")

	// A mutation that stopped after staging: the rescan the refresh asks for resumes it.
	journal, _ := writeJournal(t, h, "1000-aaaa", "staged", sha(readFile(t, live)), relabel(t, h, "two"))
	p.send(`{"type":"refresh","request_id":"r1"}`)
	snap = p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "label", snap["machine"].(map[string]any)["label"], "two")
	e := p.next("refresh_complete")
	equal(t, "request_id", e["request_id"], "r1")
	equal(t, "ok", e["ok"], true)
	gone(t, "journal", journal)
	equal(t, "version", readVersion(t, h), 2)

	// One whose live file was edited meanwhile: no fresh inventory, the last snapshot stands.
	journal, _ = writeJournal(t, h, "2000-bbbb", "staged", sha([]byte("{}\n")), relabel(t, h, "three"))
	p.send(`{"type":"refresh","request_id":"r2"}`)
	e = p.next("refresh_complete")
	equal(t, "request_id", e["request_id"], "r2")
	equal(t, "ok", e["ok"], false)
	contains(t, "error", e["error"].(string), journal)
	if _, ok := e["scan_counter"]; ok {
		t.Errorf("refresh_complete claims scan_counter %v after a failed scan", e["scan_counter"])
	}
	equal(t, "label", readSettingsFile(t, h)["label"], "two")

	// Moving the journal aside resolves it; the snapshot is unchanged.
	if err := os.Rename(journal, journal+".aside"); err != nil {
		t.Fatal(err)
	}
	p.send(`{"type":"refresh","request_id":"r3"}`)
	e = p.next("refresh_complete")
	equal(t, "request_id", e["request_id"], "r3")
	equal(t, "ok", e["ok"], true)
	equal(t, "scan_counter", e["scan_counter"], float64(2))
	equal(t, "exit", p.close(), 0)

	logs := h.events(p.stderr.String())
	if len(logs) != 1 {
		t.Fatalf("stderr = %q, want one warning", p.stderr.String())
	}
	equal(t, "log.level", logs[0]["level"], "warn")
	contains(t, "log.message", logs[0]["message"].(string), journal)
	if strings.Contains(p.stderr.String(), "watch") {
		t.Errorf("stderr mentions the watcher: %s", p.stderr.String())
	}
}
