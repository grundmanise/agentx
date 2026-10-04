package cli

import (
	"bytes"
	"encoding/json"
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
// document turns on: an export names where a skill came from and carries
// nothing it could be reconstructed from. The skill's description, the
// bytes of its files and a secret-looking value inside one of them appear
// nowhere in the export, and the document holds no snapshot.
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
	for _, leaked := range []string{description, body, secret, "SKILL.md", "notes.md", "snapshot"} {
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
	equal(t, "the document's fields", keysOf(doc), "machine,schema_version,settings,skills")
	equal(t, "schema_version", doc["schema_version"], float64(1))
	equal(t, "machine.label", doc["machine"].(map[string]any)["label"], "test-host")
	skills := doc["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("%d records, want 1:\n%s", len(skills), raw)
	}
	rec := skills[0].(map[string]any)
	equal(t, "the record's fields", keysOf(rec), "base_hash,commit,kind,name,placed,source,subpath,upstream_commit")
	equal(t, "name", rec["name"], "keeper")
	equal(t, "kind", rec["kind"], "managed")
	equal(t, "source", rec["source"], s.url)
	equal(t, "subpath", rec["subpath"], "skills/keeper")
	equal(t, "upstream_commit", rec["upstream_commit"], head)
	equal(t, "placed", rec["placed"], true)
	equal(t, "commit", rec["commit"], h.accountGit("rev-parse", "refs/heads/managed/keeper"))

	// A record carries the base version the branch records and no hash of
	// any directory, so its base_hash is the base_hash the listing reports
	// for the same skill: one datum under one name in both.
	list := h.run("--json", "skill", "list")
	equal(t, "base_hash", rec["base_hash"], h.one(list.stdout, "library_skill")["base_hash"])
}

// TestExportListsTheBranchesAndNotTheLibrary is the shape of the listing:
// one record per branch of the account repo, sorted by name, whatever this
// machine's library happens to hold. A directory nothing manages is no
// lineage record and is left out. A record tells a skill the machine placed
// somewhere from one it only holds in the library, which is what tells a
// reader what that machine was actually using. And the document is a pure
// function of the machine's state: nothing about the run, not a date and
// not an order, reaches it, so two exports of an unchanged machine are one
// file.
func TestExportListsTheBranchesAndNotTheLibrary(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	// Claude Code and Cursor keep their own skills directories, so a skill
	// with no placement is seen by no client at all.
	h.build(t, fixture{dirs: []string{".claude", ".cursor"}})
	s, _, _ := h.standardSource(true)
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	h.mustRun("skill", "remove", "beta", "--from", "claude-code", "--from", "cursor")
	// A fork branch, which nothing creates yet, and a directory put in the
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
	records := map[string]map[string]any{}
	for _, entry := range h.readExportFile(first)["skills"].([]any) {
		rec := entry.(map[string]any)
		names = append(names, rec["name"].(string))
		records[rec["name"].(string)] = rec
	}
	equal(t, "the records", strings.Join(names, ","), "alpha,beta,zeta")
	equal(t, "alpha", records["alpha"]["kind"], "managed")
	// The fork branch is one of your own skills: managed too, with no
	// source while no account remote is set, and the upstream its base
	// came from.
	equal(t, "zeta's fields", keysOf(records["zeta"]), "base_hash,commit,kind,name,placed,upstream,upstream_commit,upstream_subpath")
	equal(t, "zeta", records["zeta"]["kind"], "managed")
	equal(t, "zeta's upstream", records["zeta"]["upstream"], s.url)
	equal(t, "alpha placed", records["alpha"]["placed"], true)
	equal(t, "beta placed", records["beta"]["placed"], false)
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
	contains(t, "stdout", out.stdout, "exported the settings and 0 lineage records")
	doc := h.readExportFile(file)
	equal(t, "records", len(doc["skills"].([]any)), 0)
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

// TestExportLeavesOutABranchTheLibraryCannotHold: agentx makes one branch
// per skill, whose name is one library directory, but the two lineage
// namespaces are shared with the account remote, so a fetch or a stray
// push can put refs/heads/skills/nested/deeper there. Exporting it wrote a
// record this very CLI's reader refuses, and the reader refuses the whole
// document, so one stray branch cost the settings restore too. The branch
// is left out and said out loud instead.
func TestExportLeavesOutABranchTheLibraryCannotHold(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--all").exit, 0)
	alpha := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	h.accountGit("update-ref", "refs/heads/skills/nested/deeper", alpha)
	// An import branch with no lineage on it names no source, which the
	// document would read as one of your own skills.
	bare := h.accountGit("commit-tree", "-m", "no lineage", alpha+"^{tree}")
	h.accountGit("update-ref", "refs/heads/managed/bare", bare)

	file := h.exportPath("export.json")
	out := h.run("export", file)
	equal(t, "exit", out.exit, 0)
	contains(t, "stderr", out.stderr, "left refs/heads/skills/nested/deeper out of the export")
	contains(t, "stderr", out.stderr, "not a name the library and a branch can both hold")
	contains(t, "stderr", out.stderr, "left refs/heads/managed/bare out of the export: its commit names no source")

	doc := h.readExportFile(file)
	var names []string
	for _, rec := range doc["skills"].([]any) {
		names = append(names, rec.(map[string]any)["name"].(string))
	}
	equal(t, "the records", strings.Join(names, ","), "alpha,beta")

	// The point of leaving it out: the document agentx writes is one agentx
	// reads, so the settings still come back on the other machine.
	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	in := to.run("import", file, "--yes")
	equal(t, "import", in.exit, 0)
	contains(t, "stdout", in.stdout, "2 skills in the export")
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
	equal(t, "schema_version", h.readExportFile(file)["schema_version"], float64(1))
}
