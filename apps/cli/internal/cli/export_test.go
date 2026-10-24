package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// exportPath is a path for an export file beside the harness directories,
// outside agentx home and outside the library.
func (h *harness) exportPath(name string) string {
	return filepath.Join(filepath.Dir(h.home), name)
}

// readExportFile reads an export document as a map, which is what the
// assertions about field names read; the struct would hide a field the
// document should not carry.
func (h *harness) readExportFile(path string) map[string]any {
	h.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		h.t.Fatalf("the export is not JSON: %v\n%s", err, b)
	}
	return doc
}

func keysOf(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// TestExportCarriesCoordinatesAndNoContent is the constraint the whole
// snapshot turns on: it names where a skill came from, its version and its
// placements, and carries nothing it could be reconstructed from and
// nothing that only describes this machine. The skill's description, the
// bytes of its files, a secret-looking value inside one of them, the paths
// of this machine and the inventory's ids, state and drift appear nowhere
// in it.
func TestExportCarriesCoordinatesAndNoContent(t *testing.T) {
	t.Parallel()
	const (
		description = "peculiar-description-b7f3"
		body        = "peculiar-body-c9a1"
		secret      = "AKIAIOSFODNN7EXAMPLE"
	)
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".codex"}})
	s := h.newSourceRepo("private", true)
	s.skill("skills/keeper", "keeper", description, map[string]string{
		"notes.md":       body + "\n",
		"scripts/env.sh": "export TOKEN=" + secret + "\n",
	})
	s.commit("one skill")
	head := s.run("rev-parse", "HEAD")
	equal(t, "source add", h.run("source", "add", s.url).exit, 0)
	equal(t, "skill add", h.run("skill", "add", s.url, "--name", "keeper").exit, 0)

	file := h.exportPath("export.json")
	out := h.run("--json", "export", file)
	equal(t, "exit", out.exit, 0)
	ev := h.one(out.stdout, "export")
	equal(t, "export.path", ev["path"], file)
	equal(t, "export.skills", ev["skills"], float64(1))

	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{
		description, body, secret, "SKILL.md", "notes.md", h.library, filepath.Join(h.home, ".claude"),
		"physical_id", "logical_id", "content_hash", "edges", "warnings", "drift", "instance_id", "scan_counter",
	} {
		if bytes.Contains(raw, []byte(leaked)) {
			t.Errorf("the export carries %q:\n%s", leaked, raw)
		}
	}
	// The file holds the machine settings, so it is the owner's alone.
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "mode", info.Mode().Perm().String(), "-rw-------")

	doc := h.readExportFile(file)
	equal(t, "the snapshot's fields", keysOf(doc), "configurations,machine,mcp_servers,plugins,schema_version,settings,skills")
	equal(t, "schema_version", doc["schema_version"], float64(2))
	equal(t, "machine.label", doc["machine"].(map[string]any)["label"], "test-host")
	confs := doc["configurations"].([]any)
	equal(t, "configurations", len(confs), 2)
	equal(t, "a configuration's fields", keysOf(confs[0].(map[string]any)), "client,enabled,id,name,reads_library")
	skills := doc["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("%d records, want 1:\n%s", len(skills), raw)
	}
	rec := skills[0].(map[string]any)
	equal(t, "the entry's fields", keysOf(rec), "base_hash,commit,kind,name,placements,source,subpath,upstream_commit")
	equal(t, "name", rec["name"], "keeper")
	equal(t, "kind", rec["kind"], "managed")
	equal(t, "source", rec["source"], s.url)
	equal(t, "subpath", rec["subpath"], "skills/keeper")
	equal(t, "upstream_commit", rec["upstream_commit"], head)
	// Codex reads the library, so the library entry is its placement.
	equal(t, "placements", fmt.Sprint(rec["placements"]), "[claude-code codex]")
	equal(t, "commit", rec["commit"], h.accountGit("rev-parse", "refs/heads/managed/keeper"))

	// An entry carries the base version the branch records and no hash of
	// any directory, so its base_hash is the base_hash the listing reports
	// for the same skill: one datum under one name in both.
	list := h.run("--json", "skill", "list")
	equal(t, "base_hash", rec["base_hash"], h.one(list.stdout, "library_skill")["base_hash"])
}

// TestExportListsTheLibraryWithItsPlacements is the shape of the listing
// and the round trip: one entry per skill of the library, sorted by name,
// a directory nothing manages included, as unmanaged, and a branch the
// library holds no directory of left out, as the inventory leaves it out.
// Each entry names the configurations the skill is placed in, which an
// import reads back as they were: a skill placed in two of three
// configurations comes back placed in those two. And the snapshot is a pure
// function of the machine's state: nothing about the run, not a date and
// not an order, reaches it, so two exports of an unchanged machine are one
// file.
func TestExportListsTheLibraryWithItsPlacements(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Three clients that each keep their own skills directory and read no
	// other's, so a placement is in one configuration alone.
	h.build(t, fixture{dirs: []string{".claude", ".copilot", ".codeium/windsurf"}})
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	h.mustRun("skill", "remove", "alpha", "--from", "windsurf")
	h.mustRun("skill", "remove", "beta", "--from", "claude-code", "--from", "github-copilot", "--from", "windsurf")
	// A fork branch with no library directory, and a directory put in the
	// library by hand, which no branch knows about.
	h.accountGit("update-ref", "refs/heads/skills/zeta", h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	byHand := filepath.Join(h.library, "mine")
	if err := os.MkdirAll(byHand, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(byHand, "SKILL.md"), "---\nname: mine\ndescription: made here\n---\n\nmine\n")

	first, second := h.exportPath("first.json"), h.exportPath("second.json")
	equal(t, "first", h.run("export", first).exit, 0)
	equal(t, "second", h.run("export", second).exit, 0)
	if a, b := readText(t, first), readText(t, second); a != b {
		t.Errorf("two exports of one machine differ:\n%s\n%s", a, b)
	}

	var names []string
	entries := map[string]map[string]any{}
	for _, entry := range h.readExportFile(first)["skills"].([]any) {
		e := entry.(map[string]any)
		names = append(names, e["name"].(string))
		entries[e["name"].(string)] = e
	}
	equal(t, "the entries", strings.Join(names, ","), "alpha,beta,mine")
	equal(t, "alpha placements", fmt.Sprint(entries["alpha"]["placements"]), "[claude-code github-copilot]")
	equal(t, "beta placements", fmt.Sprint(entries["beta"]["placements"]), "[]")
	equal(t, "mine's fields", keysOf(entries["mine"]), "kind,name,placements")
	equal(t, "mine", entries["mine"]["kind"], "unmanaged")

	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	out := to.run("--json", "import", first, "--yes")
	equal(t, "import", out.exit, 0)
	imported := map[string]jsonEvent{}
	for _, e := range to.eventsOfType(out.stdout, "import_skill") {
		imported[e["name"].(string)] = e
	}
	// Placed where it was in the configurations detected here.
	equal(t, "alpha imported", fmt.Sprint(imported["alpha"]["placements"]), "[claude-code]")
	equal(t, "beta imported", fmt.Sprint(imported["beta"]["placements"]), "[]")
	equal(t, "mine imported", imported["mine"]["kind"], "unmanaged")
}

// TestExportWithoutAnAccountRepo is a machine that has installed nothing:
// the settings are exported, the listing is empty, and no account repo is
// created to answer the question.
func TestExportWithoutAnAccountRepo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "label", h.run("config", "set", "label", "fresh").exit, 0)

	file := h.exportPath("export.json")
	out := h.run("export", file)
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "exported the settings and 0 skills")
	doc := h.readExportFile(file)
	equal(t, "entries", len(doc["skills"].([]any)), 0)
	equal(t, "label", doc["settings"].(map[string]any)["label"], "fresh")
	if _, err := os.Stat(filepath.Join(h.agentx, "account.git")); err == nil {
		t.Error("the export created an account repo")
	}
}

// TestExportRefusesAPathItCannotWrite names the directory rather than
// leaving the user with git's or the operating system's words.
func TestExportRefusesAPathItCannotWrite(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	out := h.run("export", h.exportPath(filepath.Join("nowhere", "export.json")))
	equal(t, "exit", out.exit, 5)
	contains(t, "stderr", out.stderr, "does not exist")
}

// TestExportLeavesOutWhatItsReaderRefuses: a library directory whose name
// no branch can carry, and one whose import branch carries no lineage,
// which would read as one of your own skills, are no entries this very
// CLI's reader accepts, and the reader refuses the whole snapshot, so one
// stray directory cost the settings restore too. Each is left out and said
// out loud instead, and so is what export cannot read of the library, as
// skill list says it.
func TestExportLeavesOutWhatItsReaderRefuses(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--all").exit, 0)
	alpha := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	for _, name := range []string{"has space", "bare"} {
		dir := filepath.Join(h.library, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: odd\ndescription: by hand\n---\n\nodd\n")
	}
	if err := os.Symlink(filepath.Join(h.library, "nowhere"), filepath.Join(h.library, "dangling")); err != nil {
		t.Fatal(err)
	}
	bare := h.accountGit("commit-tree", "-m", "no lineage", alpha+"^{tree}")
	h.accountGit("update-ref", "refs/heads/managed/bare", bare)

	file := h.exportPath("export.json")
	out := h.run("export", file)
	equal(t, "exit", out.exit, 0)
	contains(t, "stderr", out.stderr, "left has space out of the snapshot: it is not a name the library and a branch can both hold")
	contains(t, "stderr", out.stderr, "left refs/heads/managed/bare out of the snapshot: its commit names no source")
	contains(t, "stderr", out.stderr, "dangling: broken symlink, skipped")

	doc := h.readExportFile(file)
	var names []string
	for _, e := range doc["skills"].([]any) {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	equal(t, "the entries", strings.Join(names, ","), "alpha,beta")

	// The point of leaving them out: the snapshot agentx writes is one
	// agentx reads, so the settings still come back on the other machine.
	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	// The importing machine reaches the source the way the exporting one does.
	writeFile(t, filepath.Join(to.home, ".gitconfig"), readText(t, filepath.Join(h.home, ".gitconfig")))
	in := to.run("import", file, "--yes")
	equal(t, "import", in.exit, 0)
	contains(t, "stdout", in.stdout, "2 skills in the snapshot: 2 installed")
}

// TestExportRefusesADirectoryWithAHintThatHelps: --force overwrites a file
// that is already there, and cannot overwrite a directory, so offering it
// for one sent the reader after a fix that does not exist and then failed
// as an internal error.
func TestExportRefusesADirectoryWithAHintThatHelps(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	dir := h.exportPath("a-directory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"export", dir}, {"export", dir, "--force"}} {
		out := h.run(args...)
		equal(t, strings.Join(args, " ")+" exit", out.exit, 6)
		contains(t, "stderr", out.stderr, dir+" is a directory")
		contains(t, "stderr", out.stderr, "give the path of the file to write")
		if strings.Contains(out.stderr, "--force") {
			t.Errorf("the hint offers a flag that cannot help:\n%s", out.stderr)
		}
	}
	// A file that is already there still names --force, which does help,
	// and what the file holds is not agentx's to lose until it is given.
	file := h.exportPath("export.json")
	writeFile(t, file, "not an export\n")
	again := h.run("export", file)
	equal(t, "exit", again.exit, 6)
	contains(t, "stderr", again.stderr, "pass --force to overwrite it")
	equal(t, "the file", readText(t, file), "not an export\n")
	equal(t, "with --force", h.run("export", file, "--force").exit, 0)
	equal(t, "schema_version", h.readExportFile(file)["schema_version"], float64(2))
}
