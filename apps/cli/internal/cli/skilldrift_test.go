package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// driftHarness is a machine with four configurations and one managed skill
// placed everywhere: pdf, whose frontmatter name is not the name of its
// directory in the source, tools/pdf-tools, so every comparison with its
// base has to find the base under the upstream's name. It holds two files
// of the same bytes, one more file and an executable script.
func driftHarness(t *testing.T) (*harness, *sourceRepo) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex", ".gemini"}})
	s := h.newSourceRepo("tools", true)
	s.skill("tools/pdf-tools", "pdf", "Named by its frontmatter", map[string]string{
		"a.md": "the same bytes\n", "b.md": "the same bytes\n", "c.md": "a third file\n", "bin/run.sh": "#!/bin/sh\necho run\n",
	})
	s.executable("tools/pdf-tools/bin/run.sh")
	s.commit("pdf")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--skill", "pdf")
	return h, s
}

// listed is the library_skill event skill list emits for name.
func (h *harness) listed(name string) jsonEvent {
	h.t.Helper()
	return h.librarySkill(h.mustRun("--json", "skill", "list").stdout, name)
}

// TestModifiedIsDecidedByTreeID holds state to the one definition of an
// edit: the library directory's tree, as git would record it, against the
// tree of the import commit. A mode, a link and a nested repository are
// all content, where the content hash, which names a version and reads
// neither modes nor links, would call the skill current; and a directory
// git does not record at all, an empty one, is no edit.
func TestModifiedIsDecidedByTreeID(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	expect := func(what, state string, sameHash bool) {
		t.Helper()
		ev := h.listed("pdf")
		equal(t, what+": state", ev["state"], state)
		if sameHash {
			equal(t, what+": content_hash", ev["content_hash"], ev["base_hash"])
		}
	}
	expect("after the install", stateCurrent, true)

	chmod(t, filepath.Join(lib, "a.md"), 0o755)
	expect("a file made executable", stateModified, true)
	chmod(t, filepath.Join(lib, "a.md"), 0o644)
	expect("the mode put back", stateCurrent, true)

	// A file swapped for a link to a file of the same bytes reads the same
	// to the content hash, which follows a link inside the skill.
	swapForLink(t, filepath.Join(lib, "b.md"), "a.md")
	expect("a file swapped for a link to the same bytes", stateModified, true)
	restore(t, filepath.Join(lib, "b.md"), "the same bytes\n")
	expect("the file put back", stateCurrent, true)

	// A link that leads outside the skill, or nowhere, is skipped by the
	// content hash and recorded by git.
	if err := os.Symlink(filepath.Join(h.home, "nowhere"), filepath.Join(lib, "outside")); err != nil {
		t.Fatal(err)
	}
	expect("a link out of the skill", stateModified, true)
	remove(t, filepath.Join(lib, "outside"))
	expect("the link taken away", stateCurrent, true)

	// git records no empty directory, so neither does the comparison.
	if err := os.MkdirAll(filepath.Join(lib, "empty", "emptier"), 0o755); err != nil {
		t.Fatal(err)
	}
	expect("an empty directory", stateCurrent, true)

	// A repository nested in the skill is what git cannot record at all.
	writeFile(t, mkdirs(t, filepath.Join(lib, "vendored", ".git"), "HEAD"), "ref: refs/heads/main\n")
	expect("a nested repository", stateModified, false)
	remove(t, filepath.Join(lib, "vendored"))
	expect("the repository taken away", stateCurrent, true)

	writeFile(t, filepath.Join(lib, "a.md"), "an edit\n")
	expect("an edit", stateModified, false)
	// The snapshot the desktop app applies says the same.
	equal(t, "the snapshot's state", h.snapshotLibrary("pdf")["state"], stateModified)
}

// TestARootSkillIsComparedUnderTheRepositoryName installs a skill that is a
// whole repository: its import tree holds it under the repository's name,
// and the comparison, the diff and the revert all find it there.
func TestARootSkillIsComparedUnderTheRepositoryName(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("solo", true)
	s.skill("", "solo", "A skill at the root of its repository", map[string]string{"notes.md": "notes\n"})
	s.commit("a root skill")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url)
	equal(t, "state after the install", h.listed("solo")["state"], stateCurrent)
	contains(t, "the import commit", h.accountGit("cat-file", "commit", "refs/heads/managed/solo"), "Agentx-Path: .")

	writeFile(t, filepath.Join(h.library, "solo", "notes.md"), "notes, edited\n")
	equal(t, "state after an edit", h.listed("solo")["state"], stateModified)
	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "solo").stdout, "diff")
	if len(diffs) != 1 || diffs[0]["path"] != "notes.md" || diffs[0]["status"] != "modified" {
		t.Fatalf("diff events = %v, want notes.md modified", diffs)
	}
	h.mustRun("skill", "revert", "solo")
	equal(t, "state after the revert", h.listed("solo")["state"], stateCurrent)
}

// TestPlacementDriftIsReadAtEachConfigurationsOwnPlace: displaced and
// missing are read at the path each enabled configuration's placement of
// the skill is made at, and nowhere else.
func TestPlacementDriftIsReadAtEachConfigurationsOwnPlace(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	expect := func(what, want string) {
		t.Helper()
		equal(t, what, drift(h.listed("alpha")), want)
	}
	// Codex and Gemini CLI read the library itself: they are never missing
	// the skill, and have no placement of their own to be displaced.
	expect("after the install", "")

	// A link that became a real directory is displaced, whatever it holds.
	remove(t, claude)
	copyTree(t, lib, claude)
	expect("a directory where a link was", "displaced")
	equal(t, "the snapshot's drift", drift(h.snapshotLibrary("alpha")), "displaced")
	equal(t, "the state beside it", h.listed("alpha")["state"], stateCurrent)
	remove(t, claude)
	link(t, lib, claude)
	expect("the link put back", "")

	// Nothing at the place is missing, but only for an enabled
	// configuration, and a link of the user's there takes the place.
	remove(t, cursor)
	expect("the placement gone", "missing")
	h.mustRun("config", "disable", "cursor")
	expect("the configuration disabled", "")
	h.mustRun("config", "enable", "cursor")
	expect("the configuration enabled again", "missing")
	link(t, filepath.Join(h.home, "mine"), cursor)
	expect("a link of the user's", "")
	remove(t, cursor)

	// Every word at once, sorted, and in the text listing after the state.
	remove(t, claude)
	copyTree(t, lib, claude)
	h.mustRun("source", "remove", s.url)
	expect("all of them", "displaced,missing,source removed")
	contains(t, "skill list", h.mustRun("skill", "list").stdout, "current, displaced, missing, source removed")
}

// TestACopyIsDisplacedByALinkAndNeverMissing: copy_mode records a copy, so
// the library's own link there is displaced, while a copy that no longer
// holds the library's version, edited in place or left behind by an edit
// of the library, is still the copy and takes the place.
func TestACopyIsDisplacedByALinkAndNeverMissing(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--copy")
	lib := filepath.Join(h.library, "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	equal(t, "after a copy install", drift(h.listed("alpha")), "")

	editCopy(t, cursor)
	equal(t, "an edited copy", drift(h.listed("alpha")), "")
	editLibrary(t, h, "alpha", "notes.md", "edited in the library\n")
	ev := h.listed("alpha")
	equal(t, "an edited library", drift(ev), "")
	equal(t, "an edited library's state", ev["state"], stateModified)
	// Placing again keeps the edited copy, and it still takes the place.
	h.mustRun("skill", "place", "alpha", "--to", "cursor")
	equal(t, "a kept copy", drift(h.listed("alpha")), "")

	remove(t, cursor)
	link(t, lib, cursor)
	equal(t, "the library's link where a copy is recorded", drift(h.listed("alpha")), "displaced")
}

// TestAnUnmanagedSkillHasNoStateAndNoDrift: a library directory with no
// branch is inventoried as it is, placed nowhere or not.
func TestAnUnmanagedSkillHasNoStateAndNoDrift(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	writeFile(t, mkdirs(t, filepath.Join(h.library, "notes"), "SKILL.md"), skill("notes", "My own notes"))
	ev := h.listed("notes")
	equal(t, "kind", ev["kind"], "unmanaged")
	if _, ok := ev["state"]; ok {
		t.Errorf("an unmanaged skill carries a state: %v", ev["state"])
	}
	if _, ok := ev["drift"]; ok {
		t.Errorf("an unmanaged skill carries drift: %v", ev["drift"])
	}
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func remove(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// swapForLink replaces the file at path with a link to target.
func swapForLink(t *testing.T, path, target string) {
	t.Helper()
	remove(t, path)
	link(t, target, path)
}

// restore puts a regular file back at path, whatever is there now.
func restore(t *testing.T, path, content string) {
	t.Helper()
	remove(t, path)
	writeFile(t, path, content)
}

// mkdirs creates dir and returns the path of name inside it.
func mkdirs(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name)
}

// until reads the child's events up to and including the refresh_complete
// of the request id, and returns them in order. A change on disk can reach
// serve as one rescan or as two, the second finding nothing new, so a test
// that asks what a change emitted reads everything up to the refresh it
// sent after the change rather than a fixed number of events.
func (p *serveProc) until(id string) []jsonEvent {
	p.t.Helper()
	var events []jsonEvent
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				p.t.Fatalf("serve ended before the refresh_complete of %s", id)
			}
			var e jsonEvent
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				p.t.Fatalf("not a JSON event: %q: %v", line, err)
			}
			events = append(events, e)
			if e["type"] == "refresh_complete" && e["request_id"] == id {
				return events
			}
		case <-time.After(serveDeadline):
			p.t.Fatalf("no refresh_complete of %s within %s", id, serveDeadline)
		}
	}
}

// replaceFile puts content at path in one rename, the way an editor saves
// a file, so a watcher sees the file whole.
func replaceFile(t *testing.T, path, content string) {
	t.Helper()
	stage := filepath.Join(t.TempDir(), "saved")
	writeFile(t, stage, content)
	if err := os.Rename(stage, path); err != nil {
		t.Fatal(err)
	}
}

// words is a JSON array of strings as a comma-separated list.
func words(v any) string {
	list, ok := v.([]any)
	if !ok {
		return "<not a list>"
	}
	var out []string
	for _, w := range list {
		out = append(out, w.(string))
	}
	return strings.Join(out, ",")
}

// TestServeEmitsADriftEventOnEachTransition runs serve while a managed skill
// goes through every state: edited in an editor, missing a placement,
// displaced from another, reverted, and left without its branch. Each
// change is followed by one drift event, after the snapshot that shows it
// and naming that snapshot, with the state and drift before and after; the
// first snapshot is where every state starts and is followed by none.
func TestServeEmitsADriftEventOnEachTransition(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	p := h.serve(t, "--json")
	first := p.next("snapshot")
	p.send(`{"type":"refresh","request_id":"start"}`)
	equal(t, "the event after the first snapshot", p.next("refresh_complete")["request_id"], "start")

	change := func(id string, do func()) jsonEvent {
		t.Helper()
		do()
		p.send(`{"type":"refresh","request_id":"` + id + `"}`)
		var snap, found jsonEvent
		for _, e := range p.until(id) {
			switch e["type"] {
			case "snapshot":
				snap = e
			case "drift":
				if found != nil {
					t.Fatalf("%s: a second drift event %v after %v", id, e, found)
				}
				if snap == nil {
					t.Fatalf("%s: a drift event before any snapshot: %v", id, e)
				}
				found = e
				equal(t, id+": scan_counter", e["scan_counter"], snap["scan_counter"])
				equal(t, id+": instance_id", e["instance_id"], first["instance_id"])
			}
		}
		if found == nil {
			t.Fatalf("%s: no drift event", id)
		}
		equal(t, id+": name", found["name"], "alpha")
		return found
	}
	expect := func(e jsonEvent, kind, state, drift, previousState, previousDrift string) {
		t.Helper()
		got := []any{e["kind"], e["state"], words(e["drift"]), e["previous_state"], words(e["previous_drift"])}
		want := []any{kind, state, drift, previousState, previousDrift}
		for i, field := range []string{"kind", "state", "drift", "previous_state", "previous_drift"} {
			if want[i] == "" && field != "drift" && field != "previous_drift" {
				want[i] = nil
			}
			equal(t, e["name"].(string)+" "+field, got[i], want[i])
		}
	}

	lib := filepath.Join(h.library, "alpha")
	// An edit saved in an editor reaches serve through the watcher alone:
	// no refresh is asked for, and the drift follows the snapshot it made.
	replaceFile(t, filepath.Join(lib, "notes.md"), "edited in an editor\n")
	snap := p.next("snapshot")
	edited := p.next("drift")
	equal(t, "edited: scan_counter", edited["scan_counter"], snap["scan_counter"])
	expect(edited, "managed", stateModified, "", stateCurrent, "")

	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	expect(change("unplaced", func() { remove(t, filepath.Join(h.home, ".cursor", "skills", "alpha")) }),
		"managed", stateModified, "missing", stateModified, "")
	expect(change("displaced", func() { remove(t, claude); copyTree(t, lib, claude) }),
		"managed", stateModified, "displaced,missing", stateModified, "missing")
	expect(change("reverted", func() { h.runBesideServe("skill", "revert", "alpha") }),
		"managed", stateCurrent, "displaced,missing", stateModified, "displaced,missing")
	expect(change("unmanaged", func() { h.accountGit("update-ref", "-d", "refs/heads/managed/alpha") }),
		"unmanaged", "", "", stateCurrent, "displaced,missing")

	equal(t, "exit", p.close(), 0)
	p.next("result")
	// A single pass has nothing to compare with and emits no drift; its
	// snapshot shows the skill as it now is.
	out := h.serveOnce("--json")
	if got := strings.Join(h.types(h.events(out.stdout)), ","); got != "snapshot,result" {
		t.Errorf("serve --once emitted %s, want snapshot,result", got)
	}
	library := h.one(out.stdout, "snapshot")["library"].([]any)
	if len(library) != 1 {
		t.Fatalf("the snapshot's library = %v, want alpha alone", library)
	}
	entry := library[0].(map[string]any)
	equal(t, "the snapshot's kind", entry["kind"], "unmanaged")
	if _, ok := entry["state"]; ok {
		t.Errorf("an unmanaged skill carries a state in the snapshot: %v", entry["state"])
	}
}

// TestAdoptionAndListingAgreeOnModified: the other tool's copy of the
// version differs from it by one exec bit alone. Adopting records the
// version the lock file names, and both the adoption and every listing
// after it call the directory modified, by the one comparison there is.
func TestAdoptionAndListingAgreeOnModified(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	chmod(t, filepath.Join(h.library, "alpha", "notes.md"), 0o755)
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})
	out := h.mustRun("--json", "adopt", "--skill", "alpha")
	ev := h.one(out.stdout, "adoption")
	equal(t, "the adoption's modified", ev["modified"], true)
	equal(t, "the adoption's content_hash", ev["content_hash"], ev["base_hash"])
	equal(t, "the listing's state", h.listed("alpha")["state"], stateModified)
	chmod(t, filepath.Join(h.library, "alpha", "notes.md"), 0o644)
	equal(t, "the listing's state with the bit put back", h.listed("alpha")["state"], stateCurrent)
}

// TestAManagedSkillWithoutItsDirectoryIsNamedInAWarning: a managed skill
// whose library directory was deleted outside agentx has no library entry,
// so no state and no drift, and is named in one warning instead, by skill
// list and in the snapshot's warnings alike, with the install that lays
// the directory out again. Leaving the library is no drift transition:
// serve says it through the snapshot and its warning.
func TestAManagedSkillWithoutItsDirectoryIsNamedInAWarning(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	p := h.serve(t, "--json")
	p.next("snapshot")
	p.send(`{"type":"refresh","request_id":"start"}`)
	equal(t, "the event after the first snapshot", p.next("refresh_complete")["request_id"], "start")

	remove(t, filepath.Join(h.library, "alpha"))
	want := "alpha is managed in the account repo but the library holds no skill directory for it; run 'agentx skill add " +
		shellWord(s.url) + " --skill alpha' to install it again"
	p.send(`{"type":"refresh","request_id":"gone"}`)
	var snap jsonEvent
	for _, e := range p.until("gone") {
		switch e["type"] {
		case "snapshot":
			snap = e
		case "drift":
			t.Errorf("a drift event for a skill that left the library: %v", e)
		}
	}
	if snap == nil {
		t.Fatal("no snapshot after the library directory went")
	}
	equal(t, "the snapshot's library", len(snap["library"].([]any)), 0)
	var named []string
	for _, w := range snap["warnings"].([]any) {
		if strings.Contains(w.(string), "alpha is managed") {
			named = append(named, w.(string))
		}
	}
	equal(t, "the snapshot's warnings about alpha", strings.Join(named, "\n"), want)
	equal(t, "exit", p.close(), 0)
	p.next("result")

	out := h.mustRun("--json", "skill", "list")
	if got := h.eventsOfType(out.stdout, "library_skill"); len(got) != 0 {
		t.Errorf("skill list emitted %v for a skill the library does not hold", got)
	}
	equal(t, "skill list's warnings", strings.Join(warnings(h, out.stderr), "\n"), want)
	text := h.mustRun("skill", "list")
	equal(t, "the text", text.stdout, "No skills in the library. Install one with agentx skill add <source>.\n")
	equal(t, "the text warning", text.stderr, "warning: "+want+"\n")

	// The warning is the whole of it: a skill the library holds again is
	// listed again, and warned about no more.
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	out = h.mustRun("--json", "skill", "list")
	equal(t, "state after installing it again", h.librarySkill(out.stdout, "alpha")["state"], stateCurrent)
	equal(t, "warnings after installing it again", strings.Join(warnings(h, out.stderr), "\n"), "")
}
