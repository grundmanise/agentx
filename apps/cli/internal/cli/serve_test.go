package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
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

// addSkill builds a skill outside dir and moves it in whole, so the watcher
// sees one complete skill rather than a directory being filled.
func (h *harness) addSkill(t *testing.T, dir, name string) {
	t.Helper()
	stage := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "SKILL.md"), []byte(skill(name, "A "+name+" skill")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

// editSkill replaces the SKILL.md of a skill in dir in one rename, so the
// watcher sees one complete file rather than a truncation and a write.
func (h *harness) editSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(stage, []byte(skill(name, description)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, filepath.Join(dir, name, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) addLibrarySkill(t *testing.T, name string) {
	t.Helper()
	h.addSkill(t, h.library, name)
}

func (h *harness) editLibrarySkill(t *testing.T, name, description string) {
	t.Helper()
	h.editSkill(t, h.library, name, description)
}

func skillDescriptions(e jsonEvent) []string {
	var descriptions []string
	for _, s := range e["skills"].([]any) {
		descriptions = append(descriptions, s.(map[string]any)["description"].(string))
	}
	return descriptions
}

// serveOnce runs `serve --once` after an earlier serve of the same home
// ended. Every test here is one process: a git process another parallel test
// forks at that moment holds a copy of the earlier serve's lock descriptor
// until it execs, so a refusal within that window is retried.
func (h *harness) serveOnce(args ...string) outcome {
	args = append([]string{"serve", "--once"}, args...)
	out := h.run(args...)
	for start := time.Now(); out.exit == 6 && time.Since(start) < serveDeadline; out = h.run(args...) {
		time.Sleep(10 * time.Millisecond)
	}
	return out
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
		out := h.serveOnce("--json")
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

	out := h.serveOnce()
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
	out = h.serveOnce("--json")
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
	// Wait until serve is idle: its first scan created the lock file, a
	// change in agentx home that schedules one more scan, and a mutation
	// that meets a scan's read section exits 7 instead of waiting.
	p.send(`{"type":"refresh","request_id":"idle"}`)
	p.next("refresh_complete")

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

func TestServeWatchesInsideSkills(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.addLibrarySkill(t, "commit")
	p := h.serve(t, "--json")
	snap := p.next("snapshot")
	equal(t, "skills", strings.Join(skillDescriptions(snap), " "), "A commit skill")
	// Wait until serve is idle: its first scan created the lock file, a
	// change in agentx home that schedules one more scan.
	p.send(`{"type":"refresh","request_id":"idle"}`)
	p.next("refresh_complete")

	// An edit inside the skill directory is a change signal: no refresh is requested.
	h.editLibrarySkill(t, "commit", "Write a commit message")
	snap = p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillDescriptions(snap), " "), "Write a commit message")
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

func TestServeWatchesInsideSkillsAddedLater(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("snapshot")

	h.addLibrarySkill(t, "commit")
	snap := p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillNames(snap), " "), "commit")

	h.editLibrarySkill(t, "commit", "Write a commit message")
	snap = p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(3))
	equal(t, "skills", strings.Join(skillDescriptions(snap), " "), "Write a commit message")
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

// TestWatchedDirsCoverEverySkillsDirectoryOnce pins what serve asks the
// watcher for: agentx home and the account repo flat, then every tree, each
// real path once however many clients read it.
func TestWatchedDirsCoverEverySkillsDirectoryOnce(t *testing.T) {
	t.Parallel()
	inv := &invocation{dirs: home.Dirs{
		User:    "/u",
		Home:    "/u/.agentx",
		Library: "/u/.agents/skills",
		Config:  "/u/.config",
	}}
	dirs, trees := inv.watchedDirs()
	if got, want := dirs[:2], []string{"/u/.agentx", gitx.AccountRepoPath("/u/.agentx")}; !reflect.DeepEqual(got, want) {
		t.Errorf("flat directories = %v, want %v", got, want)
	}
	if got, want := dirs[2:], trees; !reflect.DeepEqual(got, want) {
		t.Errorf("dirs after the flat ones = %v, want the trees %v", got, want)
	}
	if got, want := trees[:3], []string{"/u/.agentx/worktrees", "/u/.agents/skills", "/u/.claude/skills"}; !reflect.DeepEqual(got, want) {
		t.Errorf("first trees = %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, dir := range trees {
		if seen[dir] {
			t.Errorf("tree %s is watched twice", dir)
		}
		seen[dir] = true
	}
	// Cursor reads the Claude Code and Codex directories, and several clients
	// read the library: each is one tree, not one per client.
	for _, dir := range []string{"/u/.codex/skills", "/u/.config/opencode/skills"} {
		if !seen[dir] {
			t.Errorf("%s is not watched", dir)
		}
	}
}

// TestServeWatchesAClientSkillsDirectory covers a skill an agent client
// reads from its own directory rather than from the library: the scan reads
// it, so an edit to it is a change signal like a library edit is.
func TestServeWatchesAClientSkillsDirectory(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	skills := filepath.Join(h.home, ".claude", "skills")
	h.addSkill(t, skills, "commit")
	p := h.serve(t, "--json")
	snap := p.next("snapshot")
	equal(t, "skills", strings.Join(skillDescriptions(snap), " "), "A commit skill")
	// Wait until serve is idle: its first scan created the lock file, a
	// change in agentx home that schedules one more scan.
	p.send(`{"type":"refresh","request_id":"idle"}`)
	p.next("refresh_complete")

	// An edit inside the skill directory is a change signal: no refresh is requested.
	h.editSkill(t, skills, "commit", "Write a commit message")
	snap = p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillDescriptions(snap), " "), "Write a commit message")

	// So is a skill added to that directory. Skill nodes are ordered by
	// identity, which is content, so the names are compared sorted.
	h.addSkill(t, skills, "review")
	snap = p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(3))
	names := skillNames(snap)
	sort.Strings(names)
	equal(t, "skills", strings.Join(names, " "), "commit review")
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

// TestServeWatchesASkillsDirectoryAClientSharesOnce covers the directories
// two clients read: Cursor reads the Claude Code directory, and watching it
// twice would be watching one real path twice.
func TestServeWatchesASkillsDirectoryAClientSharesOnce(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	if err := os.MkdirAll(filepath.Join(h.home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(h.home, ".claude", "skills")
	h.addSkill(t, skills, "commit")
	p := h.serve(t, "--json")
	snap := p.next("snapshot")
	equal(t, "configurations", len(snap["configurations"].([]any)), 3)
	p.send(`{"type":"refresh","request_id":"idle"}`)
	p.next("refresh_complete")

	h.editSkill(t, skills, "commit", "Write a commit message")
	snap = p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillDescriptions(snap), " "), "Write a commit message")
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

func TestServeSurvivesARemovedSkillDirectory(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.addLibrarySkill(t, "commit")
	p := h.serve(t, "--json")
	p.next("snapshot")

	if err := os.RemoveAll(filepath.Join(h.library, "commit")); err != nil {
		t.Fatal(err)
	}
	snap := p.next("snapshot")
	equal(t, "scan_counter", snap["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillNames(snap), " "), "")

	// The loop still scans and acknowledges.
	p.send(`{"type":"refresh","request_id":"r1"}`)
	e := p.next("refresh_complete")
	equal(t, "request_id", e["request_id"], "r1")
	equal(t, "ok", e["ok"], true)
	equal(t, "scan_counter", e["scan_counter"], float64(2))
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

func TestServeEndsOnStdinEOF(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("snapshot")
	equal(t, "exit", p.close(), 0)
	p.next("result")
	equal(t, "stderr", p.stderr.String(), "")
}

func TestServeEndsOnCancel(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("snapshot")
	equal(t, "exit", p.cancelRun(), 0)
	p.next("result")
	equal(t, "stderr", p.stderr.String(), "")
}

func TestServeRefusesWhenWatchingFails(t *testing.T) {
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
	e := p.next("error")
	equal(t, "error.code", e["code"], "refused")
	contains(t, "error.message", e["message"].(string), "cannot watch for changes")
	contains(t, "error.message", e["message"].(string), h.library)
	contains(t, "error.hint", e["hint"].(string), "watch limit")
	equal(t, "result.ok", p.next("result")["ok"], false)
	equal(t, "exit", p.close(), 6)
}
