package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// serveHarness is a harness with a Codex configuration, which reads the
// library directly, so that a library change shows up in the snapshot.
func serveHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	if err := os.MkdirAll(filepath.Join(h.home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

// addLibrarySkill builds a skill outside the library and moves it in whole,
// so the watcher sees one complete skill rather than a directory being filled.
func (h *harness) addLibrarySkill(t *testing.T, name string) {
	t.Helper()
	stage := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "SKILL.md"), []byte(skill(name, "A "+name+" skill")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, filepath.Join(h.library, name)); err != nil {
		t.Fatal(err)
	}
}

func skillNames(e jsonEvent) []string {
	var names []string
	for _, s := range e["skills"].([]any) {
		names = append(names, s.(map[string]any)["name"].(string))
	}
	return names
}

func TestServeOnceEmitsInitialSnapshot(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.addLibrarySkill(t, "commit")
	var instances []string
	for range 2 {
		out := h.run("serve", "--once", "--json")
		equal(t, "exit", out.exit, 0)
		events := h.events(out.stdout)
		if got, want := h.types(events), []string{"snapshot", "result"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("event types = %v, want %v", got, want)
		}
		equal(t, "scan_counter", events[0]["scan_counter"], float64(1))
		equal(t, "skills", strings.Join(skillNames(events[0]), " "), "commit")
		equal(t, "result.ok", events[1]["ok"], true)
		instances = append(instances, events[0]["instance_id"].(string))
	}
	if instances[0] == instances[1] || len(instances[0]) != 32 {
		t.Errorf("instance ids %v: want two distinct 32-character ids", instances)
	}

	out := h.run("serve", "--once")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "snapshot 1: 1 configuration, 1 skill")
}

func TestServeRefusesASecondChild(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("snapshot")

	out := h.run("serve", "--json")
	equal(t, "exit", out.exit, 6)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "error.code", events[0]["code"], "refused")
	contains(t, "error.hint", events[0]["hint"].(string), filepath.Join(h.agentx, "serve.lock"))
	out = h.run("serve", "--once")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "serve.lock")

	equal(t, "exit", p.close(), 0)
	p.next("result")

	// The lock is released with the child.
	out = h.run("serve", "--once", "--json")
	equal(t, "exit", out.exit, 0)
}

func TestServeAnswersRequests(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	snap := p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(1))

	p.send(`{"type":"bogus"}`)
	e := p.next("error")
	equal(t, "error.code", e["code"], "usage")
	contains(t, "error.message", e["message"].(string), `"bogus"`)
	p.send(`not json`)
	e = p.next("error")
	equal(t, "error.code", e["code"], "usage")
	contains(t, "error.hint", e["hint"].(string), `"refresh"`)
	p.send(`{"type":"refresh"}`)
	e = p.next("error")
	contains(t, "error.message", e["message"].(string), "request_id")

	p.send(`{"type":"refresh","request_id":"r1"}`)
	e = p.next("refresh_complete")
	equal(t, "request_id", e["request_id"], "r1")
	equal(t, "instance_id", e["instance_id"], snap["instance_id"])
	equal(t, "ok", e["ok"], true)
	equal(t, "scan_counter", e["scan_counter"], float64(1))

	equal(t, "exit", p.close(), 0)
	r := p.next("result")
	equal(t, "result.ok", r["ok"], true)
}

func TestServeWaitsForTheMutationLockAndCoalescesRefreshes(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("snapshot")

	// A mutation in progress: the rescan the requests ask for cannot begin
	// until the exclusive lock is released, so both requests share it.
	f, err := os.OpenFile(filepath.Join(h.agentx, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	p.send(`{"type":"refresh","request_id":"r1"}`)
	p.send(`{"type":"refresh","request_id":"r2"}`)
	h.addLibrarySkill(t, "review")
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}

	snap := p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillNames(snap), " "), "review")
	for _, id := range []string{"r1", "r2"} {
		e := p.next("refresh_complete")
		equal(t, "request_id", e["request_id"], id)
		equal(t, "ok", e["ok"], true)
		equal(t, "scan_counter", e["scan_counter"], float64(2))
	}
	equal(t, "exit", p.close(), 0)
}

func TestServeRescansOnVersionBumpWithoutSnapshot(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("snapshot")

	// A one-shot mutation that changes nothing the snapshot shows.
	out := h.run("config", "set", "auto_push", "true")
	equal(t, "exit", out.exit, 0)
	equal(t, "version", readVersion(t, h), 1)
	p.send(`{"type":"refresh","request_id":"after-bump"}`)
	e := p.next("refresh_complete")
	equal(t, "request_id", e["request_id"], "after-bump")
	equal(t, "scan_counter", e["scan_counter"], float64(1))

	// A change the snapshot shows: the counter moves and the snapshot precedes the acknowledgement.
	h.addLibrarySkill(t, "commit")
	p.send(`{"type":"refresh","request_id":"after-skill"}`)
	snap := p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	e = p.next("refresh_complete")
	equal(t, "request_id", e["request_id"], "after-skill")
	equal(t, "scan_counter", e["scan_counter"], float64(2))
	equal(t, "exit", p.close(), 0)
}

func TestServeWatchesTheLibrary(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("snapshot")

	h.addLibrarySkill(t, "commit")
	snap := p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillNames(snap), " "), "commit")

	if err := os.RemoveAll(filepath.Join(h.library, "commit")); err != nil {
		t.Fatal(err)
	}
	snap = p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(3))
	equal(t, "skills", strings.Join(skillNames(snap), " "), "")
	equal(t, "exit", p.cancelRun(), 0)
}

func TestServeEndsOnStdinEOFAndOnCancel(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("snapshot")
	equal(t, "exit", p.close(), 0)
	p.next("result")

	p = h.serve(t, "--json")
	p.next("snapshot")
	equal(t, "exit", p.cancelRun(), 0)
	p.next("result")
	equal(t, "stderr", p.stderr.String(), "")
}

func TestServeFallsBackToPeriodicRescan(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	// A library that resolves to a symlink loop cannot be watched.
	if err := os.Remove(h.library); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(h.library, h.library); err != nil {
		t.Fatal(err)
	}
	p := h.serve(t, "--json")
	p.next("snapshot")

	// Nothing watches the Codex skills directory; the periodic rescan finds the skill.
	dir := filepath.Join(h.home, ".codex", "skills", "commit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill("commit", "Write a commit message")), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillNames(snap), " "), "commit")
	equal(t, "exit", p.close(), 0)

	logs := h.events(p.stderr.String())
	if len(logs) == 0 {
		t.Fatal("no warning on stderr")
	}
	equal(t, "log.level", logs[0]["level"], "warn")
	contains(t, "log.message", logs[0]["message"].(string), "rescanning every 2s")
	contains(t, "log.message", logs[0]["message"].(string), h.library)
}
