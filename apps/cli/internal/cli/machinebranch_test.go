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
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
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

// machineOperation is the operation the commit tip on the bare repository
// remote records in its trailers, read back with the reader agentx writes
// them against. Its id, a ULID, and its machine, h's, are checked and left
// out, so that a test compares the rest.
func machineOperation(t *testing.T, h *harness, remote, tip string) lineage.OperationTrailers {
	t.Helper()
	op, err := lineage.ParseOperation(remoteGit(t, h, remote, "log", "-1", "--format=%B", tip))
	if err != nil {
		t.Fatalf("the trailers of %s: %v", short(tip), err)
	}
	if !lineage.IsOperationID(op.ID) {
		t.Errorf("the operation id of %s is %q, not a ULID", short(tip), op.ID)
	}
	if machine, _, err := home.MachineID(h.agentx, h.env, nil); err != nil || op.Machine != machine {
		t.Errorf("the machine of %s is %q, want %q (%v)", short(tip), op.Machine, machine, err)
	}
	op.ID, op.Machine = "", ""
	return op
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
//
// The first home reaches the account remote by an SSH URL and the second,
// as the cloud machine does, by an HTTPS URL of the same repository, and
// can reach no other: alpha, one of your own skills on the first, is one
// of your own on the second too, installed from the account remote, and
// the second keeps its own URL of the account remote.
func TestExportToAMachineAndImportFromIt(t *testing.T) {
	t.Parallel()
	const overSSH, overHTTPS = "git@example.com:me/agentx-account.git", "https://example.com/me/agentx-account"
	a, s := installHarness(t)
	a.mustRun("config", "set", "label", "first-laptop")
	a.mustRun("skill", "add", s.url, "--all", "--to", "claude-code")
	a.mustRun("skill", "fork", "alpha")
	remote := newAccountRemote(t, a)
	a.reachAs(remote, "ssh://git@example.com/me/agentx-account")
	a.setAccount(overSSH)
	a.mustRun("skill", "publish", "alpha")

	file := a.exportSnapshot("export")
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
	equal(t, "the commit's trailers", fmt.Sprintf("%+v", machineOperation(t, a, remote, tip)), fmt.Sprintf("%+v", lineage.OperationTrailers{Operation: "export"}))
	equal(t, "alpha's placements", fmt.Sprint(doc["skills"].([]any)[0].(map[string]any)["placements"]), "[claude-code codex cursor gemini-cli]")

	again := a.run("export", "--machine", "cloud")
	equal(t, "export --machine again: exit", again.exit, 0)
	contains(t, "export --machine again", again.stdout, "machines/cloud on the account remote holds this snapshot already, at "+short(tip)+", so nothing was pushed")
	equal(t, "the tip after the second export", remoteGit(t, a, remote, "rev-parse", "refs/heads/machines/cloud"), tip)

	b := newHarness(t)
	b.build(t, fixture{dirs: []string{".claude"}})
	b.rewrite(s)
	b.reachAs(remote, overHTTPS)
	b.setAccount(overHTTPS)
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
	settings, err := home.LoadSettings(b.agentx)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := accountEntry(settings)
	equal(t, "the second home's account remote", account.URL, overHTTPS)
	if _, err := b.accountGitErr("rev-parse", "--verify", "--quiet", lineage.ForkRef("alpha")); err != nil {
		t.Errorf("alpha is not one of your own skills on the second home: %v", err)
	}
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

// reachAs makes h's git reach the bare repository remote at url, as
// url.<base>.insteadOf does, beside every mapping h's git configuration
// holds already: a machine that reaches the account remote by that URL and
// by no other.
func (h *harness) reachAs(remote, url string) {
	h.t.Helper()
	f, err := os.OpenFile(filepath.Join(h.home, ".gitconfig"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("[url \"file://" + remote + "\"]\n\tinsteadOf = " + url + "\n"); err != nil {
		h.t.Fatal(err)
	}
}

// TestABranchWithNoSettingsIsRefused: a machine's branch whose snapshot
// holds no settings is refused, exit code 6, by import and by place, with
// a hint that names writing the branch again rather than a file's path.
func TestABranchWithNoSettingsIsRefused(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha", "--to", "claude-code")
	remote := newAccountRemote(t, h)
	h.setAccount(remote)
	h.mustRun("skill", "place", "alpha", "--machine", "cloud")
	tip, doc, _ := machineSnapshot(t, h, remote, "cloud")
	delete(doc, "settings")
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), gitx.SnapshotFile)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	blob := remoteGit(t, h, remote, "hash-object", "-w", file)
	mktree := exec.Command("git", "-C", remote, "mktree")
	mktree.Stdin = strings.NewReader("100644 blob " + blob + "\t" + gitx.SnapshotFile + "\n")
	tree, err := mktree.Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := remoteGit(t, h, remote, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"commit-tree", strings.TrimSpace(string(tree)), "-p", tip, "-m", "drop the settings")
	remoteGit(t, h, remote, "update-ref", "refs/heads/"+gitx.MachineBranch("cloud"), commit)

	for _, args := range [][]string{
		{"import", "--machine", "cloud", "--yes"},
		{"skill", "place", "alpha", "--machine", "cloud", "--to", "cursor"},
	} {
		out := h.run(args...)
		equal(t, strings.Join(args, " ")+": exit", out.exit, 6)
		contains(t, strings.Join(args, " ")+": its error", out.stderr, "carries no settings")
		contains(t, strings.Join(args, " ")+": its hint", out.stderr, "agentx export --machine cloud")
	}
}

// TestSkillPlaceAndRemoveOnTheCloudMachine turns a skill on and off on the
// cloud machine: place without --to commits its entry, which follows, in
// every configuration this machine enables, to machines/cloud, which it
// creates with the settings of a fresh home and this machine's account
// remote entry, without its access; remove
// --from takes one out, and remove without --from deletes the entry; each
// event names the commit pushed, and nothing on this machine's disk
// changes. --from naming a universal client is refused, and so are a
// computer's id, --force, a --to no client of the registry has, --remote
// beside --machine, a file or --force beside export's, and a push over a branch
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
	equal(t, "the commit's trailers", fmt.Sprintf("%+v", machineOperation(t, h, remote, tip)),
		fmt.Sprintf("%+v", lineage.OperationTrailers{Operation: "place", Skill: "alpha", Configurations: []string{"claude-code", "codex", "cursor", "gemini-cli"}}))
	here, err := home.LoadSettings(h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := accountEntry(here)
	born := doc["settings"].(map[string]any)
	equal(t, "the settings the branch is born with", fmt.Sprint(born["schema_version"], born["ignore_system_files"], born["copy_mode"]), "1 true map[]")
	sources := born["sources"].([]any)
	equal(t, "the sources it is born with", len(sources), 1)
	equal(t, "the account entry's fields, without the access", keysOf(sources[0].(map[string]any)), "account,last_fetched,url")
	equal(t, "the account entry's URL", sources[0].(map[string]any)["url"], account.URL)

	text := h.run("skill", "remove", "alpha", "--machine", "cloud", "--from", "cursor")
	equal(t, "remove --from: exit", text.exit, 0)
	tip, doc, _ = machineSnapshot(t, h, remote, "cloud")
	contains(t, "remove --from", text.stdout, "removed alpha from cursor on the cloud machine: pushed "+short(tip)+" to machines/cloud")
	equal(t, "the placements left", fmt.Sprint(doc["skills"].([]any)[0].(map[string]any)["placements"]), "[claude-code codex gemini-cli]")
	equal(t, "the remove's trailers", fmt.Sprintf("%+v", machineOperation(t, h, remote, tip)),
		fmt.Sprintf("%+v", lineage.OperationTrailers{Operation: "remove", Skill: "alpha", Configurations: []string{"cursor"}}))

	refused := h.run("skill", "remove", "alpha", "--machine", "cloud", "--from", "codex")
	equal(t, "remove --from a universal client: exit", refused.exit, 6)
	contains(t, "its error", refused.stderr, "codex reads the library directly, so alpha cannot be removed from it alone")
	contains(t, "its hint", refused.stderr, "agentx skill remove alpha --machine cloud")

	for _, tc := range []struct {
		args []string
		exit int
	}{
		{[]string{"skill", "place", "alpha", "--machine", strings.Repeat("a", 32)}, 1},
		{[]string{"skill", "place", "alpha", "--machine", "cloud", "--force"}, 1},
		{[]string{"skill", "place", "alpha", "--machine", "cloud", "--to", "universal"}, 5},
		{[]string{"skill", "remove", "alpha", "--machine", "cloud", "--remote"}, 1},
		{[]string{"export", "cloud", "--machine", "cloud"}, 1},
		{[]string{"export", "--machine", "cloud", "--force"}, 1},
	} {
		equal(t, strings.Join(tc.args, " ")+": exit", h.run(tc.args...).exit, tc.exit)
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

// TestPlaceACopyOnTheCloudMachineAndImportIt: skill place --machine cloud
// --copy records the configurations it places the skill in in the
// snapshot's copy_mode, as skill place --copy records them on a computer,
// and skill remove --from takes them out again. A second home whose account
// remote is the same repository by another URL imports the branch: it
// keeps its own URL of the account remote, and the skill is placed in
// cursor as a copy, a real directory.
func TestPlaceACopyOnTheCloudMachineAndImportIt(t *testing.T) {
	t.Parallel()
	const overSSH, overHTTPS = "git@example.com:me/agentx-account.git", "https://example.com/me/agentx-account"
	a, s := installHarness(t)
	a.mustRun("skill", "add", s.url, "--name", "alpha", "--to", "claude-code")
	remote := newAccountRemote(t, a)
	a.reachAs(remote, "ssh://git@example.com/me/agentx-account")
	a.setAccount(overSSH)
	a.mustRun("skill", "place", "alpha", "--machine", "cloud", "--to", "cursor", "--copy")
	_, doc, _ := machineSnapshot(t, a, remote, "cloud")
	equal(t, "copy_mode after place --copy", fmt.Sprint(doc["settings"].(map[string]any)["copy_mode"]), "map[alpha:[cursor]]")

	b := newHarness(t)
	b.build(t, fixture{dirs: []string{".cursor"}})
	b.rewrite(s)
	b.reachAs(remote, overHTTPS)
	b.setAccount(overHTTPS)
	out := b.run("import", "--machine", "cloud", "--yes")
	equal(t, "import --machine: exit", out.exit, 0)
	settings, err := home.LoadSettings(b.agentx)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := accountEntry(settings)
	equal(t, "the second home's account remote", account.URL, overHTTPS)
	info, err := os.Lstat(filepath.Join(b.home, ".cursor", "skills", "alpha"))
	if err != nil || !info.IsDir() {
		t.Errorf("alpha is not placed in cursor as a copy: %v %v", info, err)
	}

	a.mustRun("skill", "remove", "alpha", "--machine", "cloud", "--from", "cursor")
	_, doc, _ = machineSnapshot(t, a, remote, "cloud")
	equal(t, "copy_mode after remove --from", fmt.Sprint(doc["settings"].(map[string]any)["copy_mode"]), "map[]")
}

// TestCloudUniversal: a client is universal on the cloud machine when it
// reads the library on a fresh home, as codex does and claude-code does
// not, whatever links this machine's disk holds: a skills directory linked
// to the library here makes no client universal there.
func TestCloudUniversal(t *testing.T) {
	t.Parallel()
	user := t.TempDir()
	fresh := home.Dirs{User: user, Library: filepath.Join(user, ".agents", "skills")}
	for _, id := range scan.Slugs() {
		c, _ := scan.Lookup(id)
		equal(t, id, cloudUniversal(id), scan.ReadsLibrary(c, fresh))
	}
	equal(t, "codex", cloudUniversal("codex"), true)
	equal(t, "claude-code", cloudUniversal("claude-code"), false)
	equal(t, "an id the registry does not know", cloudUniversal("nope"), false)

	linked := t.TempDir()
	here := home.Dirs{User: linked, Library: filepath.Join(linked, ".agents", "skills")}
	if err := os.MkdirAll(here.Library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(linked, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(here.Library, filepath.Join(linked, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}
	c, _ := scan.Lookup("claude-code")
	equal(t, "claude-code linked to the library here", scan.ReadsLibrary(c, here), true)
	equal(t, "claude-code on the cloud machine", cloudUniversal("claude-code"), false)
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
	snap := snapshot{Settings: home.DefaultSettings(), Skills: []snapshotSkill{exact}}

	placeInSnapshot(&snap, snapshotSkill{Name: "alpha", Kind: "managed", Source: exact.Source, Subpath: ptr("skills/alpha")}, []string{"cursor"})
	placeInSnapshot(&snap, exact, []string{"cursor", "codex"})
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the snapshot", string(b), `{"schema_version":2,`+
		`"settings":{"schema_version":1,"accept_operations":false,"ignore_system_files":true,"disabled_configurations":[],"sources":[],"copy_mode":{}},`+
		`"configurations":[],"skills":[`+
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
