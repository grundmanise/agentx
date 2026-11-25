package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// serveHarness is a harness with a Codex configuration, which reads the
// library directly, so that a library change shows up in the inventory.
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

// serveOnce runs `serve --once`. A serve of the same home that ended has
// released its lock by then: home.Unlock lets go of the lock itself rather
// than closing a descriptor that a git another test forks meanwhile may
// hold a copy of, so a refusal here is a lock serve leaked.
func (h *harness) serveOnce(args ...string) outcome {
	return h.run(append([]string{"serve", "--once"}, args...)...)
}

func skillNames(e jsonEvent) []string {
	var names []string
	for _, s := range e["skills"].([]any) {
		names = append(names, s.(map[string]any)["name"].(string))
	}
	return names
}

func TestServeOnceEmitsInitialInventory(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.addLibrarySkill(t, "commit")
	var instances []string
	for range 2 {
		out := h.serveOnce("--json")
		equal(t, "exit", out.exit, 0)
		events := h.events(out.stdout)
		if got, want := h.types(events), []string{"inventory", "result"}; !reflect.DeepEqual(got, want) {
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
	contains(t, "stdout", out.stdout, "inventory 1: 1 configuration, 1 skill")
}

// A second serve of one home cannot serve, says so in an error event of
// its own and stays up to run commands until its stdin ends: it refuses a
// refresh and finds nothing on a search. A one-shot serve is refused at
// once.
func TestServeStaysUpBesideASecondChild(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.env[cliChildEnv] = "1"
	p := h.serve(t, "--json")
	p.next("inventory")

	second := h.serve(t, "--json", "--listen", "127.0.0.1")
	c := dialDoor(t, second.next("listening"), "")
	e := second.next("error")
	equal(t, "error.code", e["code"], "refused")
	contains(t, "error.hint", e["hint"].(string), filepath.Join(h.agentx, "serve.lock"))
	second.request(1, "run", map[string]any{"args": []string{"version"}})
	result, _, run, _ := second.answer(float64(1))
	alike(t, "run events", h.types(run), []string{"version", "result"})
	equal(t, "exit_code", result["exit_code"], float64(0))
	// The door is open too, and answers as stdin does.
	c.send(1, "refresh", nil)
	_, rpcError, _, _ := c.answer(1)
	equal(t, "refresh error.code", rpcError["code"], float64(-32000))
	contains(t, "refresh error.message", rpcError["message"].(string), "another agentx serve is running")
	second.request("s1", "search", map[string]any{"query": "commit"})
	alike(t, "search results", second.answered("s1")["results"], []any{})
	equal(t, "exit", second.close(), 6)
	equal(t, "result.ok", second.next("result")["ok"], false)

	out := h.run("serve", "--once")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "serve.lock")

	equal(t, "exit", p.close(), 0)
	p.next("result")

	// The lock is released with the child.
	out = h.serveOnce("--json")
	equal(t, "exit", out.exit, 0)
}

// Stdin carries JSON-RPC: a refresh is answered with the counter of the
// inventory after its scan, a search from the index, and a line that is no
// JSON with the error JSON-RPC gives it, after which serve goes on.
func TestServeAnswersRequests(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	inventory := p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(1))

	p.send(`not json`)
	_, rpcError := p.response(nil)
	equal(t, "error.code", rpcError["code"], float64(-32700))
	p.request("q", "search", nil)
	_, rpcError = p.response("q")
	equal(t, "error.code", rpcError["code"], float64(-32602))

	e := p.refreshed("r1")
	alike(t, "refresh", e, jsonEvent{"instance_id": inventory["instance_id"], "scan_counter": float64(1)})

	equal(t, "exit", p.close(), 0)
	r := p.next("result")
	equal(t, "result.ok", r["ok"], true)
}

// A command runs through stdin as through the door: its events come as
// they are written and its exit code after them. A cancel stops it, and so
// does the end of stdin, which ends serve once the command has ended.
func TestServeRunsCommandsFromStdin(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.env[cliChildEnv] = "1"
	waiting := filepath.Join(t.TempDir(), "waiting")
	stubGit(t, h, "#!/bin/sh\n"+hostPATH()+`case "$*" in *never.invalid*) echo $$ > `+shellWord(waiting)+`; exec sleep 60 ;; esac
exec git "$@"
`)
	waitingGit := func() (pid int) {
		awaitTrue(t, "the command to wait for its git", func() bool {
			b, _ := os.ReadFile(waiting)
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			return pid != 0
		})
		return pid
	}
	p := h.serve(t, "--json")
	// Serve answers a run while it reconciles, which holds the lock, and
	// source add gives up on a held lock before it runs any git. The first
	// inventory comes once serve has reconciled.
	p.next("inventory")
	p.request("v", "run", map[string]any{"args": []string{"version"}})
	result, _, run, _ := p.answer("v")
	alike(t, "run events", h.types(run), []string{"version", "result"})
	alike(t, "result", result, jsonEvent{"exit_code": float64(0), "cancelled": false, "timed_out": false})

	p.request(1, "run", map[string]any{"args": []string{"source", "add", "https://never.invalid/skills.git"}})
	waitingGit()
	p.request(nil, "$/cancelRequest", map[string]any{"id": 1})
	result, _, _, _ = p.answer(float64(1))
	equal(t, "cancelled", result["cancelled"], true)
	equal(t, "exit_code", result["exit_code"], float64(exitInterrupted.exit))

	if err := os.Remove(waiting); err != nil {
		t.Fatal(err)
	}
	p.request(2, "run", map[string]any{"args": []string{"source", "add", "https://never.invalid/skills.git"}})
	git := waitingGit()
	equal(t, "exit", p.close(), 0)
	equal(t, "the git the command waited for", syscall.Kill(git, 0), error(syscall.ESRCH))
	result, _, _, _ = p.answer(float64(2))
	equal(t, "cancelled", result["cancelled"], true)
}

// Under --json every event of serve's own goes out as an event
// notification with no id, one line each.
func TestEventNotificationsEncloseEachEvent(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	w := &writer{stdout: eventNotifications{out: &out}, stderr: io.Discard, json: true}
	w.result(true, "")
	equal(t, "stdout", out.String(), `{"jsonrpc":"2.0","method":"event","params":{"stream":"stdout","event":{"type":"result","schema_version":`+strconv.Itoa(schemaVersion)+`,"ok":true}}}`+"\n")
}

func TestServeWaitsForTheMutationLockAndCoalescesRefreshes(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("inventory")

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
	p.refresh("r1")
	p.refresh("r2")
	h.addLibrarySkill(t, "review")
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}

	inventory := p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillNames(inventory), " "), "review")
	// Each request is answered on its own, so the two answers come in
	// either order.
	answered := map[any]any{}
	for range 2 {
		var m jsonEvent
		if err := json.Unmarshal([]byte(<-p.lines), &m); err != nil {
			t.Fatal(err)
		}
		answered[m["id"]] = m["result"].(map[string]any)["scan_counter"]
	}
	alike(t, "the counters the refreshes were answered with", answered, map[any]any{"r1": float64(2), "r2": float64(2)})
	equal(t, "exit", p.close(), 0)
}

func TestServeRescansOnVersionBumpWithoutInventory(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("inventory")
	// Wait until serve is idle: its first scan created the lock file, a
	// change in agentx home that schedules one more scan, and a mutation
	// that meets a scan's read section exits 7 instead of waiting.
	p.refreshed("idle")

	// A one-shot mutation that changes nothing the inventory shows.
	out := h.run("config", "set", "accept_operations", "true")
	equal(t, "exit", out.exit, 0)
	equal(t, "version", readVersion(t, h), 1)
	e := p.refreshed("after-bump")
	equal(t, "scan_counter", e["scan_counter"], float64(1))

	// A change the inventory shows: the counter moves and the inventory precedes the acknowledgement.
	h.addLibrarySkill(t, "commit")
	p.refresh("after-skill")
	inventory := p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(2))
	e = p.answered("after-skill")
	equal(t, "scan_counter", e["scan_counter"], float64(2))
	equal(t, "exit", p.close(), 0)
}

// TestServeWatchesTheLibrary takes one skill through the library while
// serve runs, each step a change signal with no refresh asked for: the
// skill arrives, an edit inside its directory, watched from the rescan that
// found it, shows, and the directory goes, its watch with it. The loop
// still scans and acknowledges after that.
func TestServeWatchesTheLibrary(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("inventory")

	h.addLibrarySkill(t, "commit")
	inventory := p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillNames(inventory), " "), "commit")

	h.editLibrarySkill(t, "commit", "Write a commit message")
	inventory = p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(3))
	equal(t, "skills", strings.Join(skillDescriptions(inventory), " "), "Write a commit message")

	if err := os.RemoveAll(filepath.Join(h.library, "commit")); err != nil {
		t.Fatal(err)
	}
	inventory = p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(4))
	equal(t, "skills", strings.Join(skillNames(inventory), " "), "")

	e := p.refreshed("r1")
	equal(t, "scan_counter", e["scan_counter"], float64(4))
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

func TestServeWatchesInsideSkills(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.addLibrarySkill(t, "commit")
	p := h.serve(t, "--json")
	inventory := p.next("inventory")
	equal(t, "skills", strings.Join(skillDescriptions(inventory), " "), "A commit skill")
	// Wait until serve is idle: its first scan created the lock file, a
	// change in agentx home that schedules one more scan.
	p.refreshed("idle")

	// An edit inside the skill directory is a change signal: no refresh is requested.
	h.editLibrarySkill(t, "commit", "Write a commit message")
	inventory = p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillDescriptions(inventory), " "), "Write a commit message")
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

// TestWatchedDirsCoverEverySkillsDirectoryOnce pins what serve asks the
// watcher for: agentx home, the store and its logs and logs/refs flat,
// then every tree, each real path once however many clients read it. The
// flat logs directories are where a skill's first publish shows: it creates
// logs/refs/remotes and everything below it at once.
func TestWatchedDirsCoverEverySkillsDirectoryOnce(t *testing.T) {
	t.Parallel()
	inv := &invocation{dirs: home.Dirs{
		User:    "/u",
		Home:    "/u/.agentx",
		Library: "/u/.agents/skills",
		Config:  "/u/.config",
	}}
	dirs, trees := inv.watchedDirs()
	repo := gitx.StorePath("/u/.agentx")
	if got, want := dirs[:4], []string{"/u/.agentx", repo, repo + "/logs", repo + "/logs/refs"}; !reflect.DeepEqual(got, want) {
		t.Errorf("flat directories = %v, want %v", got, want)
	}
	if got, want := dirs[4:], trees; !reflect.DeepEqual(got, want) {
		t.Errorf("dirs after the flat ones = %v, want the trees %v", got, want)
	}
	// The reflogs of the fork branches are where a commit made with git in
	// a fork's worktree shows, and those of the remote-tracking branches
	// where a publish that only pushes does.
	logs := gitx.StorePath("/u/.agentx") + "/logs/refs"
	if got, want := trees[:5], []string{"/u/.agentx/worktrees", "/u/.agents/skills", logs + "/heads/skills", logs + "/remotes", "/u/.claude/skills"}; !reflect.DeepEqual(got, want) {
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
	inventory := p.next("inventory")
	equal(t, "skills", strings.Join(skillDescriptions(inventory), " "), "A commit skill")
	// Wait until serve is idle: its first scan created the lock file, a
	// change in agentx home that schedules one more scan.
	p.refreshed("idle")

	// An edit inside the skill directory is a change signal: no refresh is requested.
	h.editSkill(t, skills, "commit", "Write a commit message")
	inventory = p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(2))
	equal(t, "skills", strings.Join(skillDescriptions(inventory), " "), "Write a commit message")

	// So is a skill added to that directory. Skill nodes are ordered by
	// identity, which is content, so the names are compared sorted.
	h.addSkill(t, skills, "review")
	inventory = p.next("inventory")
	equal(t, "scan_counter", inventory["scan_counter"], float64(3))
	names := skillNames(inventory)
	sort.Strings(names)
	equal(t, "skills", strings.Join(names, " "), "commit review")
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

// TestServeKeepsServingWhenTheStoreCannotBeRead breaks the home
// repo under a running serve and repairs it. The scan in between still
// succeeds, with the library listed empty and a warning saying why, so the
// desktop app keeps the rest of its inventory; the first scan after the
// repair gives the library back, with a skill edited meanwhile modified.
func TestServeKeepsServingWhenTheStoreCannotBeRead(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	store := gitx.StorePath(h.agentx)
	head := filepath.Join(store, "HEAD")
	healthy, err := os.ReadFile(head)
	if err != nil {
		t.Fatal(err)
	}
	// Each HEAD is renamed into place, so no scan reads one half written.
	setHead := func(content string) {
		t.Helper()
		writeFile(t, head+".new", content)
		if err := os.Rename(head+".new", head); err != nil {
			t.Fatal(err)
		}
	}
	p := h.serve(t, "--json")
	first := p.next("inventory")
	equal(t, "library at start", len(first["library"].([]any)), 1)
	p.refreshed("idle")

	setHead("garbage\n")
	p.refresh("broken")
	broken := p.next("inventory")
	equal(t, "library while broken", len(broken["library"].([]any)), 0)
	equal(t, "skills while broken", strings.Join(skillNames(broken), " "), "alpha")
	warnings := broken["warnings"].([]any)
	if len(warnings) != 1 || !strings.Contains(warnings[0].(string), store) {
		t.Errorf("warnings = %q, want one naming %s", warnings, store)
	}
	p.answered("broken")

	replaceFile(t, filepath.Join(h.library, "alpha", "notes.md"), "edited while the store was broken\n")
	setHead(string(healthy))
	events := p.until("repaired")
	var repaired jsonEvent
	for _, e := range events {
		if e["type"] == "inventory" {
			repaired = e
		}
	}
	if repaired == nil {
		t.Fatal("no inventory after the repair")
	}
	library := repaired["library"].([]any)
	if len(library) != 1 {
		t.Fatalf("library after the repair = %v, want alpha alone", library)
	}
	entry := library[0].(map[string]any)
	equal(t, "the skill after the repair", entry["name"], "alpha")
	equal(t, "its state after the repair", entry["state"], stateModified)
	equal(t, "warnings after the repair", len(repaired["warnings"].([]any)), 0)
	equal(t, "exit", p.close(), 0)
}

func TestServeEndsOnCancel(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	p := h.serve(t, "--json")
	p.next("inventory")
	equal(t, "exit", p.cancelRun(), 0)
	p.next("result")
	equal(t, "stderr", p.stderr.String(), "")
}

// A directory serve cannot watch is reported, and serve goes on: it scans
// when it is asked to.
func TestServeGoesOnWhenWatchingFails(t *testing.T) {
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
	p.next("inventory")
	equal(t, "refresh", p.refreshed("r1")["scan_counter"], float64(1))
	equal(t, "exit", p.close(), 0)
	equal(t, "result.ok", p.next("result")["ok"], true)
}

// A scan that fails at start, as one does while git is broken, is
// reported with the code of its error, and serve goes on: the first scan
// that succeeds, here the one a refresh asks for, emits inventory 1.
func TestServeGoesOnFromAFailedInitialScan(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	broken := filepath.Join(t.TempDir(), "broken")
	writeFile(t, broken, "")
	stubGit(t, h, "#!/bin/sh\n"+hostPATH()+"if [ -e "+shellWord(broken)+" ]; then echo 'git is broken' >&2; exit 1; fi\nexec git \"$@\"\n")
	p := h.serve(t, "--json")
	e := p.next("error")
	equal(t, "error.code", e["code"], "git")
	contains(t, "error.message", e["message"].(string), "git is broken")

	if err := os.Remove(broken); err != nil {
		t.Fatal(err)
	}
	p.refresh("r1")
	equal(t, "the first inventory", p.next("inventory")["scan_counter"], float64(1))
	equal(t, "refresh", p.answered("r1")["scan_counter"], float64(1))
	equal(t, "exit", p.close(), 0)
}
