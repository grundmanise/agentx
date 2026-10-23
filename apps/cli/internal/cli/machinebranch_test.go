package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// machineSnapshot is what the branch of the machine id on the bare
// repository remote holds: its tip and its one file, read with plain git,
// failing the test when its tree holds anything else.
func machineSnapshot(t *testing.T, h *harness, remote, id string) (tip string, doc map[string]any, raw string) {
	t.Helper()
	ref := "refs/heads/" + gitx.MachineBranch(id)
	tip = remoteGit(t, h, remote, "rev-parse", ref)
	equal(t, "the files of "+ref, remoteGit(t, h, remote, "ls-tree", "--name-only", tip), gitx.SnapshotFile)
	raw = remoteGit(t, h, remote, "cat-file", "blob", tip+":"+gitx.SnapshotFile) + "\n"
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("%s holds no JSON: %v\n%s", ref, err, raw)
	}
	return tip, doc, raw
}

// TestExportToAMachineAndImportFromIt is a snapshot's round trip through
// the account remote: an export to a file pushes nothing; an export naming
// the cloud machine writes the same bytes as the one file of a commit on
// machines/cloud, its event naming the machine and the commit and no path;
// a second one pushes nothing. A fresh second home whose account remote
// takes no push, as a read-only credential, imports it with --yes: the
// import names the branch, restores the settings and installs and places
// every skill the branch lists, in the configurations detected there. A
// machine whose branch the remote does not hold is exit code 5.
func TestExportToAMachineAndImportFromIt(t *testing.T) {
	t.Parallel()
	a, s := installHarness(t)
	a.mustRun("config", "set", "label", "first-laptop")
	a.mustRun("skill", "add", s.url, "--all", "--to", "claude-code")
	remote := newAccountRemote(t, a)
	a.setAccount(remote)

	file := a.exportPath("export.json")
	a.mustRun("export", file)
	equal(t, "the remote's machine branches after an export to a file", remoteGit(t, a, remote, "for-each-ref", "refs/heads/machines/"), "")

	out := a.run("--json", "export", "--machine", "cloud")
	equal(t, "export --machine: exit", out.exit, 0)
	ev := a.one(out.stdout, "export")
	tip, doc, raw := machineSnapshot(t, a, remote, "cloud")
	equal(t, "export.machine", ev["machine"], "cloud")
	equal(t, "export.commit", ev["commit"], tip)
	equal(t, "export.skills", ev["skills"], float64(2))
	if _, ok := ev["path"]; ok {
		t.Errorf("an export to a machine names a path: %v", ev)
	}
	equal(t, "the branch's snapshot is the file's", raw, readText(t, file))
	equal(t, "the commit's subject", remoteGit(t, a, remote, "log", "-1", "--format=%s", tip), "Export first-laptop")
	equal(t, "alpha's placements", fmt.Sprint(doc["skills"].([]any)[0].(map[string]any)["placements"]), "[claude-code codex cursor gemini-cli]")

	again := a.run("export", "--machine", "cloud")
	equal(t, "export --machine again: exit", again.exit, 0)
	contains(t, "export --machine again", again.stdout, "machines/cloud on the account remote holds this snapshot already, at "+short(tip)+", so nothing was pushed")
	equal(t, "the tip after the second export", remoteGit(t, a, remote, "rev-parse", "refs/heads/machines/cloud"), tip)

	b := newHarness(t)
	b.build(t, fixture{dirs: []string{".claude"}})
	b.rewrite(s)
	b.setAccount(remote)
	writeShim(t, filepath.Join(remote, "hooks", "pre-receive"), "#!/bin/sh\necho 'read only' >&2\nexit 1\n")
	out = b.run("--json", "import", "--machine", "cloud", "--yes")
	equal(t, "import --machine: exit", out.exit, 0)
	equal(t, "the label imported", b.one(out.stdout, "settings")["settings"].(map[string]any)["label"], "first-laptop")
	var applied []string
	for _, e := range b.eventsOfType(out.stdout, "import_skill") {
		applied = append(applied, fmt.Sprint(e["name"], " ", e["action"], " ", e["placements"]))
	}
	equal(t, "the skills imported", strings.Join(applied, ", "), "alpha installed [claude-code], beta installed [claude-code]")
	equal(t, "import's summary", b.one(out.stdout, "result")["summary"], "imported the settings from machines/cloud: 2 installed")
	for _, name := range []string{"alpha", "beta"} {
		if !lexists(filepath.Join(b.home, ".claude", "skills", name)) {
			t.Errorf("%s is not placed in claude-code on the second home", name)
		}
	}
	equal(t, "the tip after the import", remoteGit(t, a, remote, "rev-parse", "refs/heads/machines/cloud"), tip)

	out = b.run("import", "--machine", strings.Repeat("0", 32), "--yes")
	equal(t, "import of a machine with no branch: exit", out.exit, 5)
	contains(t, "its error", out.stderr, "the account remote holds no machines/"+strings.Repeat("0", 32))
}

// TestSkillPlaceAndRemoveOnTheCloudMachine turns a skill on and off on the
// cloud machine: place without --to commits its entry, which follows, in
// every configuration this machine enables, to machines/cloud; remove
// --from takes one out, and remove without --from deletes the entry; each
// event names the commit pushed, and nothing on this machine's disk
// changes. --from naming a universal client is refused, and so are a
// computer's id, --remote beside --machine, and a push over a branch
// another writer moved since it was read, with a hint to run it again.
func TestSkillPlaceAndRemoveOnTheCloudMachine(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha", "--to", "claude-code")
	remote := newAccountRemote(t, h)
	h.setAccount(remote)
	disk := func() []string {
		return slices.DeleteFunc(diskState(t, h), func(e string) bool {
			return strings.HasPrefix(e, gitx.AccountRepoPath(h.agentx))
		})
	}
	before := disk()

	out := h.run("--json", "skill", "place", "alpha", "--machine", "cloud")
	equal(t, "place --machine: exit", out.exit, 0)
	ev := h.one(out.stdout, "machine_skill")
	tip, doc, _ := machineSnapshot(t, h, remote, "cloud")
	equal(t, "machine_skill.commit", ev["commit"], tip)
	equal(t, "machine_skill.placements", fmt.Sprint(ev["placements"]), "[claude-code codex cursor gemini-cli]")
	skills := doc["skills"].([]any)
	equal(t, "the snapshot's skills", len(skills), 1)
	entry := skills[0].(map[string]any)
	equal(t, "the entry's fields", keysOf(entry), "kind,name,placements,source,subpath")
	equal(t, "the entry's source", entry["source"], s.url)
	equal(t, "the commit's subject", remoteGit(t, h, remote, "log", "-1", "--format=%s", tip), "Place alpha in claude-code, codex, cursor, gemini-cli")

	text := h.run("skill", "remove", "alpha", "--machine", "cloud", "--from", "cursor")
	equal(t, "remove --from: exit", text.exit, 0)
	tip, doc, _ = machineSnapshot(t, h, remote, "cloud")
	contains(t, "remove --from", text.stdout, "removed alpha from cursor on the cloud machine: pushed "+short(tip)+" to machines/cloud")
	equal(t, "the placements left", fmt.Sprint(doc["skills"].([]any)[0].(map[string]any)["placements"]), "[claude-code codex gemini-cli]")

	refused := h.run("skill", "remove", "alpha", "--machine", "cloud", "--from", "codex")
	equal(t, "remove --from a universal client: exit", refused.exit, 6)
	contains(t, "its error", refused.stderr, "codex reads the library directly, so alpha cannot be removed from it alone")
	contains(t, "its hint", refused.stderr, "agentx skill remove alpha --machine cloud")

	for _, args := range [][]string{
		{"skill", "place", "alpha", "--machine", strings.Repeat("a", 32)},
		{"skill", "remove", "alpha", "--machine", "cloud", "--remote"},
	} {
		equal(t, strings.Join(args, " ")+": exit", h.run(args...).exit, 1)
	}

	out = h.run("--json", "skill", "remove", "alpha", "--machine", "cloud")
	equal(t, "remove --machine: exit", out.exit, 0)
	ev = h.one(out.stdout, "machine_skill")
	tip, doc, _ = machineSnapshot(t, h, remote, "cloud")
	equal(t, "machine_skill.removed", ev["removed"], true)
	equal(t, "machine_skill.commit", ev["commit"], tip)
	equal(t, "the snapshot's skills after the removal", len(doc["skills"].([]any)), 0)
	equal(t, "this machine's disk", strings.Join(disk(), "\n"), strings.Join(before, "\n"))

	// Another writer moves the branch between the read and the push.
	other := remoteGit(t, h, remote, "commit-tree", tip+"^{tree}", "-p", tip, "-m", "another writer")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	bin := t.TempDir()
	writeShim(t, filepath.Join(bin, "git"), "#!/bin/sh\ncase \" $* \" in *\" push \"*machines/cloud*) "+
		real+" --git-dir="+remote+" update-ref refs/heads/machines/cloud "+other+" ;; esac\nexec "+real+" \"$@\"\n")
	h.env["PATH"] = bin + string(os.PathListSeparator) + os.Getenv("PATH")
	moved := h.run("skill", "place", "alpha", "--machine", "cloud", "--to", "claude-code")
	equal(t, "a place over a moved branch: exit", moved.exit, 6)
	contains(t, "its error", moved.stderr, "machines/cloud on the account remote changed since it was read, so nothing was pushed")
	contains(t, "its hint", moved.stderr, "run 'agentx skill place alpha --to claude-code --machine cloud' again")
	equal(t, "the other writer's tip", remoteGit(t, h, remote, "rev-parse", "refs/heads/machines/cloud"), other)
}

// TestCloudRemoval: --from naming a universal client is refused whatever
// else it names, and --from universal beside a configuration is a usage
// error; anything else is a removal.
func TestCloudRemoval(t *testing.T) {
	t.Parallel()
	universal := func(id string) bool { return id == "codex" || id == "gemini-cli" }
	for _, tc := range []struct {
		from []string
		exit int // 0 for a removal
	}{
		{nil, 0},
		{[]string{"universal"}, 0},
		{[]string{"claude-code", "cursor"}, 0},
		{[]string{"claude-code", "codex"}, 6},
		{[]string{"universal", "gemini-cli"}, 6},
		{[]string{"universal", "claude-code"}, 1},
	} {
		err := cloudRemoval("pdf", tc.from, universal)
		got := 0
		var f *failure
		if errors.As(err, &f) {
			got = f.status.exit
		}
		equal(t, fmt.Sprint(tc.from), got, tc.exit)
	}
}

// TestPlaceAndRemoveInASnapshot: a place adds its configurations to those
// the entry holds, each once, sorted, writes the entry as one that follows
// and keeps the skills sorted; a removal takes configurations out or
// deletes the entry, and names a skill the snapshot does not list.
func TestPlaceAndRemoveInASnapshot(t *testing.T) {
	t.Parallel()
	exact := snapshotSkill{Name: "pdf", Kind: "managed", Commit: strings.Repeat("a", 40), Source: "https://github.com/example/skills",
		Subpath: ptr("skills/pdf"), UpstreamCommit: strings.Repeat("b", 40), BaseHash: strings.Repeat("c", 64), Placements: []string{"codex"}}
	snap := snapshot{Skills: []snapshotSkill{exact}}

	placeInSnapshot(&snap, snapshotSkill{Name: "alpha", Kind: "managed", Source: exact.Source, Subpath: ptr("skills/alpha")}, []string{"cursor"})
	placeInSnapshot(&snap, exact, []string{"cursor", "codex"})
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the snapshot", string(b), `{"schema_version":2,"configurations":[],"skills":[`+
		`{"name":"alpha","kind":"managed","source":"https://github.com/example/skills","subpath":"skills/alpha","placements":["cursor"]},`+
		`{"name":"pdf","kind":"managed","source":"https://github.com/example/skills","subpath":"skills/pdf","placements":["codex","cursor"]}],`+
		`"mcp_servers":[],"plugins":[]}`)

	whole, ok := removeFromSnapshot(&snap, "pdf", []string{"codex", "claude-code"})
	equal(t, "a removal from configurations", fmt.Sprint(whole, ok, snapshotEntry(snap, "pdf").Placements), "false true [cursor]")
	whole, ok = removeFromSnapshot(&snap, "alpha", nil)
	equal(t, "a removal of the entry", fmt.Sprint(whole, ok, len(snap.Skills)), "true true 1")
	_, ok = removeFromSnapshot(&snap, "alpha", nil)
	equal(t, "a removal of a skill not listed", ok, false)
}

// TestMachineArg: export and import take the cloud machine or a computer's
// id, and nothing else.
func TestMachineArg(t *testing.T) {
	t.Parallel()
	for id, ok := range map[string]bool{
		"cloud": true, strings.Repeat("0123456789abcdef", 2): true,
		"Cloud": false, strings.Repeat("A", 32): false, "../cloud": false, strings.Repeat("a", 31): false,
	} {
		equal(t, id, machineArg(id) == nil, ok)
	}
}
