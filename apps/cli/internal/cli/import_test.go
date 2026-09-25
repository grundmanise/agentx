package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// readText reads a file the test compares byte for byte.
func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// entriesOf names what a directory holds, sorted.
func entriesOf(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// exportedFrom builds a machine with settings worth restoring and exports
// it: a label, a disabled configuration, two sources one of which is
// pinned, two skills and a copy mode.
func exportedFrom(t *testing.T) (*harness, string) {
	t.Helper()
	h, s := installHarness(t)
	equal(t, "label", h.run("config", "set", "label", "first-laptop").exit, 0)
	equal(t, "disable", h.run("config", "disable", "gemini-cli").exit, 0)
	equal(t, "add", h.run("skill", "add", s.url, "--all", "--to", "cursor", "--copy").exit, 0)
	other := h.newSourceRepo("other", true)
	other.skill("tools/gamma", "gamma", "The third skill", nil)
	other.commit("one skill")
	equal(t, "source add", h.run("source", "add", other.url+"#main").exit, 0)

	file := h.exportPath("export.json")
	equal(t, "export", h.run("export", file).exit, 0)
	return h, file
}

// plainExport is an export a test can read without installing anything: a
// machine with settings of its own and no account repo, exported by the
// command itself, with one sound lineage record put into the document
// afterwards. Writing that record rather than installing a skill for it
// keeps a test about reading a document from paying for a fetch, and every
// test that an export of real branches is what this reads is elsewhere.
func plainExport(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "label", h.run("config", "set", "label", "first-laptop").exit, 0)
	file := h.exportPath("export.json")
	equal(t, "export", h.run("export", file).exit, 0)

	doc := h.readExportFile(file)
	doc["skills"] = []any{map[string]any{
		"name":            "alpha",
		"kind":            "managed",
		"commit":          strings.Repeat("a", 40),
		"source":          "https://github.com/example/skills",
		"subpath":         "skills/alpha",
		"upstream_commit": strings.Repeat("b", 40),
		"base_hash":       strings.Repeat("c", 64),
		"placed":          true,
	}}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, string(b))
	return h, file
}

// TestImportRestoresTheSettingsByteForByte is the round trip: the settings
// file of the machine that exported and the settings file the import wrote
// into a fresh home are the same bytes.
func TestImportRestoresTheSettingsByteForByte(t *testing.T) {
	t.Parallel()
	from, file := exportedFrom(t)
	before := readText(t, home.SettingsPath(from.agentx))

	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	out := to.run("import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	if after := readText(t, home.SettingsPath(to.agentx)); after != before {
		t.Errorf("the settings were not restored byte for byte:\nexported:\n%s\nimported:\n%s", before, after)
	}
	contains(t, "stdout", out.stdout, "imported the settings of first-laptop")
	contains(t, "stdout", out.stdout, "2 skills in the export: 0 in the account repo, 2 missing, 0 at a different version")
	contains(t, "stdout", out.stdout, "  alpha  managed  missing  file://")
	contains(t, "stdout", out.stdout, "  beta   managed  missing  file://")
	contains(t, "stdout", out.stdout, "only the settings were written")

	// The settings are now this machine's own, read by every command.
	equal(t, "label", strings.TrimSpace(to.run("config", "get", "label").stdout), "first-laptop")
	contains(t, "sources", to.run("source", "list").stdout, "2 sources")
}

// TestImportWritesNothingButTheSettings is the whole of what an import
// does: no account repo, no library, no placement, no fetch. A fresh home
// holds the settings file and the files any lock leaves behind, and
// nothing else.
func TestImportWritesNothingButTheSettings(t *testing.T) {
	t.Parallel()
	_, file := plainExport(t)
	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude", ".cursor"}})
	before := entriesOf(t, to.agentx)
	equal(t, "exit", to.run("import", file, "--yes").exit, 0)

	var added []string
	for _, name := range entriesOf(t, to.agentx) {
		if !slices.Contains(before, name) {
			added = append(added, name)
		}
	}
	// The settings file, and what taking the lock leaves behind.
	equal(t, "what the import added to agentx home", strings.Join(added, ","), "lock,mutations,ops,settings.json,version")
	if journals, err := home.Journals(to.agentx); err != nil || len(journals) != 0 {
		t.Errorf("journals = %v, %v, want none", journals, err)
	}
	if entries, err := os.ReadDir(to.library); err == nil && len(entries) > 0 {
		t.Errorf("the import filled the library: %v", entries)
	}
	if entries, err := os.ReadDir(filepath.Join(to.home, ".claude", "skills")); err == nil && len(entries) > 0 {
		t.Errorf("the import made a placement: %v", entries)
	}
}

// TestImportAsksBeforeItWrites: the settings are what only this machine
// decides, so replacing them is never the default answer. Where the
// command cannot ask it refuses, and --yes is how the desktop app and a
// script say yes.
func TestImportAsksBeforeItWrites(t *testing.T) {
	t.Parallel()
	_, file := plainExport(t)
	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "label", to.run("config", "set", "label", "keep-me").exit, 0)
	before := readText(t, home.SettingsPath(to.agentx))

	// Neither stream is a terminal here, which is every script and the
	// desktop app; the answer names the flag that says yes.
	out := to.run("import", file)
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "no terminal to confirm at")
	contains(t, "stderr", out.stderr, "--yes")
	equal(t, "the settings", readText(t, home.SettingsPath(to.agentx)), before)

	// In JSON mode the question could not be asked either way: stdout
	// carries events alone.
	js := to.run("--json", "import", file)
	equal(t, "exit", js.exit, 6)
	equal(t, "code", to.events(js.stdout)[0]["code"], "refused")
	equal(t, "the settings", readText(t, home.SettingsPath(to.agentx)), before)

	equal(t, "with --yes", to.run("import", file, "--yes").exit, 0)
	if readText(t, home.SettingsPath(to.agentx)) == before {
		t.Error("--yes changed nothing")
	}
}

// TestImportRefusesADocumentItCannotRead: an export is a file from
// somewhere else, so every shape of broken one is refused, and the
// settings on disk are untouched whichever it was.
func TestImportRefusesADocumentItCannotRead(t *testing.T) {
	t.Parallel()
	_, good := plainExport(t)
	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "label", to.run("config", "set", "label", "keep-me").exit, 0)
	before := readText(t, home.SettingsPath(to.agentx))
	sound := to.readExportFile(good)

	// edited copies the good document with one thing changed.
	edited := func(change func(map[string]any)) string {
		doc := to.readExportFile(good)
		change(doc)
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	settingsWith := func(key string, value any) string {
		return edited(func(doc map[string]any) { doc["settings"].(map[string]any)[key] = value })
	}
	recordWith := func(key string, value any) string {
		return edited(func(doc map[string]any) {
			rec := doc["skills"].([]any)[0].(map[string]any)
			rec[key] = value
		})
	}

	whole := readText(t, good)
	// A credential that would be unmistakable in a settings file, an event
	// or an export if any of them ever carried one.
	const token = "ghp_NotARealTokenJustForThisTest"
	for _, tc := range []struct {
		name, content string
		exit          int
		says          string
	}{
		{"empty", "", 6, "not an agentx export"},
		{"not json", "{ not json", 6, "not an agentx export"},
		{"truncated", whole[:len(whole)/2], 6, "not an agentx export"},
		{"trailing rubbish", whole + "}", 6, "not an agentx export"},
		{"no version", `{"settings":{},"skills":[]}`, 6, "carries no schema_version"},
		{"a later version", edited(func(doc map[string]any) { doc["schema_version"] = 2 }), 6, "schema version 2"},
		{"a field agentx does not know", edited(func(doc map[string]any) { doc["snapshot"] = map[string]any{} }), 6, "not an agentx export"},
		{"later settings", settingsWith("schema_version", 2), 6, "schema_version is 2"},
		{"a settings field agentx does not know", settingsWith("secrets", "x"), 6, "not an agentx export"},
		{"a label of two lines", settingsWith("label", "one\ntwo"), 6, "one non-empty line"},
		{"a source carrying a token", settingsWith("sources", []any{map[string]any{"url": "https://user:token@github.com/example/skills"}}), 6, "canonical URL"},
		{"a source with a subpath", settingsWith("sources", []any{map[string]any{"url": "https://github.com/example/skills/tools"}}), 6, "canonical URL"},
		{"a record of another kind", recordWith("kind", "greenfield"), 6, "neither managed nor fork"},
		{"a record with no commit", recordWith("commit", "HEAD"), 6, "does not name the commit"},
		{"a record with a bad hash", recordWith("base_hash", "nonsense"), 6, "one upstream version"},
		{"a record that walks out", recordWith("name", "../evil"), 6, "not a name the library and a branch can both hold"},
		{"a record named for a nested branch", recordWith("name", "nested/deeper"), 6, "not a name the library and a branch can both hold"},
		{"a record given twice", edited(func(doc map[string]any) {
			skills := doc["skills"].([]any)
			doc["skills"] = []any{skills[0], skills[0]}
		}), 6, "is listed twice"},

		// Every other field of a source entry. url is the one a reader
		// would think of; alias is a second URL of the same source that
		// nothing else in the CLI writes, pin is a ref this machine hands
		// to git, and last_fetched is a date agentx wrote.
		{"an alias carrying a token", settingsWith("sources", []any{map[string]any{
			"url":   "https://github.com/example/skills",
			"alias": "https://user:" + token + "@github.com/example/skills",
		}}), 6, "canonical URL"},
		{"an alias with a subpath", settingsWith("sources", []any{map[string]any{
			"url":   "https://github.com/example/skills",
			"alias": "https://github.com/example/skills-old/tools",
		}}), 6, "canonical URL"},
		{"a source with no url", settingsWith("sources", []any{map[string]any{
			"alias": "https://github.com/example/skills",
		}}), 6, "not a source URL"},
		{"an alias that is not a URL", settingsWith("sources", []any{map[string]any{
			"url":   "https://github.com/example/skills",
			"alias": "not a url at all",
		}}), 6, "not a source URL"},
		{"a pin git refuses", settingsWith("sources", []any{map[string]any{
			"url": "https://github.com/example/skills", "pin": "main:refs/heads/skills/EVIL",
		}}), 6, "not a ref git accepts"},
		{"a pin with a newline", settingsWith("sources", []any{map[string]any{
			"url": "https://github.com/example/skills", "pin": "main\nINJECTED",
		}}), 6, "not a ref git accepts"},
		{"a pin that reads as an option", settingsWith("sources", []any{map[string]any{
			"url": "https://github.com/example/skills", "pin": "--upload-pack=touch",
		}}), 6, "not a ref git accepts"},
		{"a last_fetched that is not a date", settingsWith("sources", []any{map[string]any{
			"url": "https://github.com/example/skills", "last_fetched": "whenever",
		}}), 6, "RFC 3339 in UTC"},
		{"a last_fetched that is not in UTC", settingsWith("sources", []any{map[string]any{
			"url": "https://github.com/example/skills", "last_fetched": "2026-09-18T12:00:00+02:00",
		}}), 6, "RFC 3339 in UTC"},
		{"one source twice", settingsWith("sources", []any{
			map[string]any{"url": "https://github.com/example/skills"},
			map[string]any{"url": "https://github.com/example/skills", "pin": "v1"},
		}), 6, "is listed twice"},
		{"sources out of order", settingsWith("sources", []any{
			map[string]any{"url": "https://github.com/example/skills"},
			map[string]any{"url": "https://github.com/example/alpha"},
		}), 6, "sorted by url"},

		// disabled_configurations holds ids config disable wrote, which
		// are slugs of the client registry and nothing else.
		{"a disabled configuration carrying an escape", settingsWith("disabled_configurations",
			[]any{"\u001b[31mcursor"}), 6, "not a configuration id"},
		{"a disabled configuration of ten thousand characters", settingsWith("disabled_configurations",
			[]any{strings.Repeat("a", 10000)}), 6, "not a configuration id"},
		{"a configuration disabled twice", settingsWith("disabled_configurations",
			[]any{"cursor", "cursor"}), 6, "disabled twice"},
		{"disabled configurations out of order", settingsWith("disabled_configurations",
			[]any{"cursor", "codex"}), 6, "must be sorted"},

		// copy_mode is kept as raw JSON so that a write never loses it,
		// which is also why a spelling agentx would not write is kept for
		// good once it is imported.
		{"a copy mode that is not one", settingsWith("copy_mode", []any{"alpha"}), 6, "copy_mode"},
		{"a copy mode of JSON null", settingsWith("copy_mode", nil), 6, "the way agentx writes it"},
		{"a copy mode with a key twice", edited(func(doc map[string]any) {
			doc["settings"].(map[string]any)["copy_mode"] = json.RawMessage(`{"alpha":["cursor"],"alpha":["codex"]}`)
		}), 6, "the way agentx writes it"},
		{"a copy mode out of order", edited(func(doc map[string]any) {
			doc["settings"].(map[string]any)["copy_mode"] = json.RawMessage(`{"beta":["cursor"],"alpha":["codex"]}`)
		}), 6, "the way agentx writes it"},
		{"a copy mode with no configuration", settingsWith("copy_mode",
			map[string]any{"alpha": []any{}}), 6, "which agentx removes rather than writes"},
		{"a copy mode of a name the library cannot hold", settingsWith("copy_mode",
			map[string]any{"../evil": []any{"cursor"}}), 6, "no name a skill of the library has"},
		{"a copy mode to something that is no configuration", settingsWith("copy_mode",
			map[string]any{"alpha": []any{"\u001b[31mcursor"}}), 6, "not a configuration id"},

		// The label, which config set label cannot make this long because
		// its argument is bounded by ARG_MAX, and an import can.
		{"a label carrying an escape", settingsWith("label", "\u001b[31mred"), 6, "no control character"},
		{"a label of a thousand characters", settingsWith("label", strings.Repeat("l", 1000)), 6, "at most 256 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := to.exportPath(strings.ReplaceAll(tc.name, " ", "-") + ".json")
			writeFile(t, file, tc.content)
			out := to.run("import", file, "--yes")
			equal(t, "exit", out.exit, tc.exit)
			contains(t, "stderr", out.stderr, tc.says)
			equal(t, "the settings", readText(t, home.SettingsPath(to.agentx)), before)
		})
	}

	t.Run("a file that is not there", func(t *testing.T) {
		out := to.run("import", to.exportPath("nothing.json"), "--yes")
		equal(t, "exit", out.exit, 5)
		contains(t, "stderr", out.stderr, "does not exist")
		equal(t, "the settings", readText(t, home.SettingsPath(to.agentx)), before)
	})

	// The good document still imports after all of that, so the refusals
	// were about the documents and not about the machine.
	equal(t, "the sound document", to.run("import", good, "--yes").exit, 0)
	equal(t, "records", len(sound["skills"].([]any)), 1)
}

// TestImportListsWhatTheAccountRepoHas is the point of the listing: one
// line per record of the export saying whether this machine's account repo
// holds that skill, is missing it, or holds it at another version.
func TestImportListsWhatTheAccountRepoHas(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	other := h.newSourceRepo("third", true)
	other.skill("tools/gamma", "gamma", "The third skill", nil)
	other.commit("one skill")
	equal(t, "source add", h.run("source", "add", other.url).exit, 0)
	equal(t, "add", h.run("skill", "add", s.url, "--all").exit, 0)
	equal(t, "add gamma", h.run("skill", "add", other.url).exit, 0)

	file := h.exportPath("export.json")
	equal(t, "export", h.run("export", file).exit, 0)

	// beta's branch moves to another commit, and gamma leaves the machine.
	alpha := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	h.accountGit("update-ref", "refs/heads/managed/beta", alpha)
	equal(t, "remove", h.run("skill", "remove", "gamma").exit, 0)

	out := h.run("--json", "import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	types := h.types(h.events(out.stdout))
	want := "settings,import_skill,import_skill,import_skill,result"
	if strings.Join(types, ",") != want {
		t.Errorf("events = %v, want %v\n%s", types, want, out.stdout)
	}
	states := map[string]jsonEvent{}
	for _, e := range h.eventsOfType(out.stdout, "import_skill") {
		states[e["name"].(string)] = e
	}
	equal(t, "alpha", states["alpha"]["state"], "present")
	equal(t, "beta", states["beta"]["state"], "different")
	equal(t, "gamma", states["gamma"]["state"], "missing")
	equal(t, "beta's local kind", states["beta"]["local_kind"], "managed")
	equal(t, "beta's local commit", states["beta"]["local_commit"], alpha)
	if _, ok := states["gamma"]["local_commit"]; ok {
		t.Error("a missing skill carries a local commit")
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"imported the settings from "+file+": 1 of 3 skills in the account repo, 1 missing, 1 at a different version")

	text := h.run("import", file, "--yes")
	contains(t, "stdout", text.stdout, "3 skills in the export: 1 in the account repo, 1 missing, 1 at a different version")
	contains(t, "stdout", text.stdout, "  alpha  managed  present")
	contains(t, "stdout", text.stdout, "  gamma  managed  missing")
}

// TestImportOntoAMachineWithNoAccountRepo: everything the export lists is
// missing, and the repo is not created to find that out.
func TestImportOntoAMachineWithNoAccountRepo(t *testing.T) {
	t.Parallel()
	_, file := plainExport(t)
	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	out := to.run("--json", "import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	for _, e := range to.eventsOfType(out.stdout, "import_skill") {
		equal(t, e["name"].(string)+" state", e["state"], "missing")
	}
	if _, err := os.Stat(filepath.Join(to.agentx, "account.git")); err == nil {
		t.Error("the import created an account repo")
	}
}

// TestNothingReadsAnExportAutomatically: an export is read when a person
// asks for it and never otherwise. One left in agentx home, broken, is read
// by no command.
func TestNothingReadsAnExportAutomatically(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--all").exit, 0)
	inHome := filepath.Join(h.agentx, "export.json")
	equal(t, "export", h.run("export", inHome).exit, 0)
	settings := readText(t, home.SettingsPath(h.agentx))
	list := h.run("--json", "skill", "list").stdout

	// Whatever it holds, nothing looks at it.
	writeFile(t, inHome, "{ not an export at all")
	for _, args := range [][]string{
		{"scan"}, {"config", "list"}, {"skill", "list"}, {"source", "list"}, {"machine"}, {"doctor"},
	} {
		out := h.run(args...)
		equal(t, strings.Join(args, " "), out.exit, 0)
	}
	equal(t, "the file", readText(t, inHome), "{ not an export at all")
	equal(t, "the settings", readText(t, home.SettingsPath(h.agentx)), settings)
	equal(t, "the listing", h.run("--json", "skill", "list").stdout, list)
}

// TestImportReadsTheAnswer is the question itself: yes in the forms a
// person types it, and everything else, an empty line and a stream that
// ends included, left as a refusal that changes nothing.
func TestImportReadsTheAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		answer string
		yes    bool
	}{
		{"y\n", true}, {"Y\n", true}, {"yes\n", true}, {"  YES  \n", true}, {"y", true},
		{"\n", false}, {"", false}, {"n\n", false}, {"no\n", false}, {"why not\n", false},
	} {
		var stdout, stderr bytes.Buffer
		inv := &invocation{
			env:  map[string]string{},
			out:  &writer{stdout: &stdout, stderr: &stderr, env: map[string]string{}},
			dirs: home.Dirs{Home: filepath.Join("tmp", "agentx")},
		}
		doc := exportDocument{Machine: exportMachine{Label: "first-laptop"}}
		err := inv.askToImport("export.json", doc, strings.NewReader(tc.answer))
		if (err == nil) != tc.yes {
			t.Errorf("the answer %q gave %v, want yes = %v", tc.answer, err, tc.yes)
		}
		contains(t, "the question", stdout.String(),
			"Replace the settings in "+filepath.Join("tmp", "agentx", "settings.json")+
				" with those of first-laptop from export.json? [y/N] ")
		if err != nil {
			var f *failure
			if !errors.As(err, &f) || f.status != exitRefused {
				t.Errorf("a declined import gave %v, want a refusal", err)
			}
		}
	}
}

// TestImportFillsInWhatADocumentLeavesOut: a document is not a settings
// file, and one that leaves out what agentx always writes is written as
// agentx writes settings rather than through, so the file on disk is one
// every command reads the same way.
func TestImportFillsInWhatADocumentLeavesOut(t *testing.T) {
	t.Parallel()
	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	file := to.exportPath("minimal.json")
	writeFile(t, file, `{"schema_version":1,"machine":{"id":"0123456789abcdef0123456789abcdef","label":"other"},`+
		`"settings":{"schema_version":1},"skills":[]}`)

	out := to.run("import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "No skills in the export.")
	equal(t, "the settings", readText(t, home.SettingsPath(to.agentx)), `{
  "schema_version": 1,
  "auto_push": false,
  "accept_operations": false,
  "disabled_configurations": [],
  "sources": [],
  "copy_mode": {}
}
`)
	equal(t, "config list", to.run("config", "list").exit, 0)
	js := to.run("--json", "import", file, "--yes")
	equal(t, "summary", to.one(js.stdout, "result")["summary"],
		"imported the settings from "+file+": the export lists no skills")
}

// TestImportSaysWhyWithoutEchoingTheDocument: the reason a document was
// refused quotes it – a field name, a character – and a document comes from
// another machine, so what it holds is sanitised before it is printed and
// no escape sequence of its reaches the terminal.
func TestImportSaysWhyWithoutEchoingTheDocument(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	file := h.exportPath("evil.json")
	writeFile(t, file, "{\"schema_version\": 1, \"\\u001b[31mred\\r\": 1}")

	out := h.run("import", file, "--yes")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "not an agentx export")
	if strings.ContainsAny(out.stderr, "\x1b\r") {
		t.Errorf("the refusal carried a control character from the document: %q", out.stderr)
	}
}

// TestImportPutsNoCredentialInTheSettingsByAnyField is the gate the
// contract makes load-bearing: a URL carrying a user or a token "reaches
// the settings by no route at all and may not reach them by this one". An
// import is the only route by which any string reaches a source entry's
// alias – nothing else in the CLI writes that field – so the same
// credential is put into every field of the entry in turn and each one is
// refused, with the settings, the source event and the next export left
// carrying no part of it.
func TestImportPutsNoCredentialInTheSettingsByAnyField(t *testing.T) {
	t.Parallel()
	const token = "ghp_NotARealTokenJustForThisTest"
	const credentialed = "https://user:" + token + "@github.com/example/skills"

	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "label", h.run("config", "set", "label", "keep-me").exit, 0)
	before := readText(t, home.SettingsPath(h.agentx))

	for _, field := range []string{"url", "alias"} {
		t.Run(field, func(t *testing.T) {
			entry := map[string]any{"url": "https://github.com/example/skills"}
			entry[field] = credentialed
			file := h.exportPath("credential-in-" + field + ".json")
			writeFile(t, file, `{"schema_version":1,"machine":{"id":"0123456789abcdef0123456789abcdef","label":"attacker"},`+
				`"settings":{"schema_version":1,"sources":[`+mustJSON(t, entry)+`]},"skills":[]}`)

			out := h.run("import", file, "--yes")
			equal(t, "exit", out.exit, 6)
			contains(t, "stderr", out.stderr, "must be stored as the canonical URL of a source alone")
			// The refusal names the URL it parsed to, never what it was given.
			if strings.Contains(out.stdout+out.stderr, token) {
				t.Errorf("the refusal repeated the credential:\n%s%s", out.stdout, out.stderr)
			}
			equal(t, "the settings", readText(t, home.SettingsPath(h.agentx)), before)
		})
	}

	// And nothing of it is anywhere afterwards: not at rest in the
	// settings, not in the source event the desktop app reads, and not in
	// an export that would carry it on to the next machine.
	equal(t, "source list", h.run("--json", "source", "list").exit, 0)
	onward := h.exportPath("onward.json")
	equal(t, "export", h.run("export", onward).exit, 0)
	for what, text := range map[string]string{
		"the settings":      readText(t, home.SettingsPath(h.agentx)),
		"the source events": h.run("--json", "source", "list").stdout,
		"the next export":   readText(t, onward),
	} {
		if strings.Contains(text, token) || strings.Contains(text, "user:") {
			t.Errorf("%s carries the credential:\n%s", what, text)
		}
	}
}

// mustJSON marshals a value a test builds a document out of.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestValidSettingsCoversEveryFieldOfTheSettings is the guard on the audit
// rather than on one bug: an import is the only route by which a string
// somebody else chose reaches the settings file, so a field added to the
// settings later must either be checked by validSettings or be one whose
// JSON type is the whole of its domain. The field names are read by
// reflection, so adding one and leaving it unchecked fails here rather
// than shipping a second alias.
func TestValidSettingsCoversEveryFieldOfTheSettings(t *testing.T) {
	t.Parallel()
	// Every field of the settings and of one source entry, with what holds
	// it to what agentx would write. A bool is its own whole domain: the
	// decoder refuses anything else, so there is nothing left to check.
	covered := map[string]string{
		"Settings.SchemaVersion":          "badSchemaVersion",
		"Settings.Label":                  "badLabel (validLabel, as config set label is)",
		"Settings.AutoPush":               "a bool: the JSON type is the whole domain",
		"Settings.AcceptOperations":       "a bool: the JSON type is the whole domain",
		"Settings.DisabledConfigurations": "badDisabledConfigurations",
		"Settings.Sources":                "badSources",
		"Settings.CopyMode":               "badCopyMode",
		"Source.URL":                      "badSources, through badSourceURL",
		"Source.Alias":                    "badSources, through badSourceURL",
		"Source.Pin":                      "badSources, through source.ValidRef",
		"Source.LastFetched":              "badSources, through fetchTime",
	}
	for _, spec := range []struct {
		kind string
		of   reflect.Type
	}{
		{"Settings", reflect.TypeOf(home.Settings{})},
		{"Source", reflect.TypeOf(home.Source{})},
	} {
		for i := range spec.of.NumField() {
			name := spec.kind + "." + spec.of.Field(i).Name
			if _, ok := covered[name]; !ok {
				t.Errorf("%s is written by an import and nothing in validSettings checks it; "+
					"check it there and name the check here", name)
			}
			delete(covered, name)
		}
	}
	for name := range covered {
		t.Errorf("%s is named as covered but is no longer a field of the settings", name)
	}
}

// TestImportRefusesADocumentNoMachineWrote bounds the file rather than
// guessing a bound for each field in it. config set label cannot write a
// hundred-megabyte label because its argument is bounded by ARG_MAX; a
// document's label is not, and the settings file it would write is read
// and written whole by every command after it, and exported again at twice
// the size.
func TestImportRefusesADocumentNoMachineWrote(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "label", h.run("config", "set", "label", "keep-me").exit, 0)
	before := readText(t, home.SettingsPath(h.agentx))

	file := h.exportPath("enormous.json")
	head := `{"schema_version":1,"machine":{"id":"0123456789abcdef0123456789abcdef","label":"a"},` +
		`"settings":{"schema_version":1,"label":"`
	writeFile(t, file, head+strings.Repeat("L", documentLimit)+`"},"skills":[]}`)

	out := h.run("import", file, "--yes")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "is larger than")
	equal(t, "the settings", readText(t, home.SettingsPath(h.agentx)), before)

	// Under the bound the label rule is what answers, so nothing of this
	// size reaches the settings either way.
	small := h.exportPath("large-label.json")
	writeFile(t, small, head+strings.Repeat("L", 100_000)+`"},"skills":[]}`)
	out = h.run("import", small, "--yes")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "at most 256 bytes")
	equal(t, "the settings", readText(t, home.SettingsPath(h.agentx)), before)
}

// TestImportSaysWhatToRunNext: an import restores the settings entry of a
// source and nothing of the source itself – no account repo, no remote –
// so on the machine an import just made, skill add answers that the source
// was never added. source add is the step that has to come first, and the
// closing line said to run skill add.
func TestImportSaysWhatToRunNext(t *testing.T) {
	t.Parallel()
	from, file := exportedFrom(t)
	url := from.run("--json", "source", "list")
	sources := from.eventsOfType(url.stdout, "source")
	if len(sources) == 0 {
		t.Fatal("the exporting machine has no source")
	}

	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude", ".cursor"}})
	out := to.run("import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "only the settings were written: add each source again, then install a missing skill with 'agentx skill add <source>'")

	// What the closing line now names is what works; what it named before
	// is exit 5 until that has been run.
	first := sources[0]["url"].(string)
	early := to.run("skill", "add", first, "--all")
	equal(t, "skill add before source add", early.exit, 5)
	contains(t, "stderr", early.stderr, "source not fetched")
	equal(t, "source add", to.run("source", "add", first).exit, 0)
	equal(t, "skill add after source add", to.run("skill", "add", first, "--all").exit, 0)
}

// TestImportPrintsTheSourceAddThatKeepsThePin: source add writes the pin its
// argument names, so an import that pointed at the bare URL had the user
// unpin the very source it had just restored. It prints one source add per
// source instead, with the pin the settings hold, and running those lines
// leaves every source pinned where the export had it.
func TestImportPrintsTheSourceAddThatKeepsThePin(t *testing.T) {
	t.Parallel()
	from, file := exportedFrom(t)
	exported, err := home.LoadSettings(from.agentx)
	if err != nil {
		t.Fatal(err)
	}
	pins := func(s home.Settings) map[string]string {
		m := map[string]string{}
		for _, src := range s.Sources {
			m[src.URL] = src.Pin
		}
		return m
	}
	if !slices.ContainsFunc(exported.Sources, func(src home.Source) bool { return src.Pin != "" }) {
		t.Fatal("the exporting machine pins no source")
	}

	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	out := to.run("import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	printed := printedSourceAdds(t, out.stdout, t.TempDir())
	equal(t, "source add lines", len(printed), len(exported.Sources))
	for _, arg := range printed {
		equal(t, "source add "+arg, to.run("source", "add", arg).exit, 0)
	}

	restored, err := home.LoadSettings(to.agentx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pins(restored), pins(exported); !reflect.DeepEqual(got, want) {
		t.Errorf("pins after the printed source add = %v, want %v\n%s", got, want, out.stdout)
	}
}

// TestTheNotFetchedHintKeepsAnImportedPin: on the machine an import made, a
// source is in the settings with its pin and nothing of it is fetched, and
// skill add, source fetch and source skills answer that with a source add
// to run. That hint named the bare URL, and source add writes the pin its
// argument names, so following it unpinned the source the import had just
// restored. It names the pin the settings hold, both before this machine
// has an account repo and once another source has made one.
func TestTheNotFetchedHintKeepsAnImportedPin(t *testing.T) {
	t.Parallel()
	from, file := exportedFrom(t)
	exported, err := home.LoadSettings(from.agentx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(exported.Sources, func(src home.Source) bool { return src.Pin != "" })
	j := slices.IndexFunc(exported.Sources, func(src home.Source) bool { return src.Pin == "" })
	if i < 0 || j < 0 {
		t.Fatalf("the exporting machine should have a pinned and an unpinned source: %+v", exported.Sources)
	}
	pinned, unpinned := exported.Sources[i], exported.Sources[j]

	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "import", to.run("import", file, "--yes").exit, 0)

	// hinted checks the argument of the source add each command's refusal
	// names, as a POSIX shell reads it back, and returns the last one.
	hinted := func(when string) (arg string) {
		t.Helper()
		for _, args := range [][]string{
			{"skill", "add", pinned.URL, "--all"},
			{"source", "fetch", pinned.URL},
			{"source", "skills", pinned.URL},
		} {
			out := to.run(append([]string{"--json"}, args...)...)
			equal(t, when+": "+args[0]+" "+args[1]+" exit", out.exit, 5)
			hint, _ := lastError(t, to.events(out.stdout))["hint"].(string)
			word, prefixed := strings.CutPrefix(hint, "run 'agentx source add ")
			word, suffixed := strings.CutSuffix(word, "' to fetch it")
			if !prefixed || !suffixed {
				t.Errorf("%s: %s %s hint = %q, want a source add to fetch it", when, args[0], args[1], hint)
				continue
			}
			got := shellRead(t, word, t.TempDir())
			if want := []string{pinned.URL + "#" + pinned.Pin}; !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %s %s hint names %q, want %q", when, args[0], args[1], got, want)
			}
			if len(got) == 1 {
				arg = got[0]
			}
		}
		return arg
	}
	hinted("no account repo")
	equal(t, "source add of the unpinned source", to.run("source", "add", unpinned.URL).exit, 0)
	arg := hinted("an account repo without the source")
	if arg == "" {
		t.FailNow()
	}

	// Following the hint keeps the pin, and skill add then installs.
	equal(t, "source add as hinted", to.run("source", "add", arg).exit, 0)
	restored, err := home.LoadSettings(to.agentx)
	if err != nil {
		t.Fatal(err)
	}
	if k := restored.FindSource(pinned.URL); k < 0 || restored.Sources[k].Pin != pinned.Pin {
		t.Errorf("after the hinted source add the settings hold %+v, want %s pinned to %q", restored.Sources, pinned.URL, pinned.Pin)
	}
	equal(t, "skill add after the hinted source add", to.run("skill", "add", pinned.URL, "--all").exit, 0)
}

// TestImportWithNoSourceNamesNoSourceAdd: settings with no source have none
// to add again, so the closing line goes straight to skill add, which adds
// a source it is given by URL itself.
func TestImportWithNoSourceNamesNoSourceAdd(t *testing.T) {
	t.Parallel()
	_, file := plainExport(t)
	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	out := to.run("import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "only the settings were written: install a missing skill with 'agentx skill add <source>'")
	if strings.Contains(out.stdout, "source add") {
		t.Errorf("an import that restored no source names source add:\n%s", out.stdout)
	}
}

// printedSourceAdds is the argument of every source add line an import
// printed, as a POSIX shell reads it back: each line's argument is run
// through sh in dir, so the test finds there whatever a line did besides
// handing its argument over.
func printedSourceAdds(t *testing.T, stdout, dir string) []string {
	t.Helper()
	var args []string
	for _, line := range strings.Split(stdout, "\n") {
		if word, ok := strings.CutPrefix(line, "    agentx source add "); ok {
			args = append(args, shellRead(t, word, dir)...)
		}
	}
	return args
}

// shellRead is what a POSIX shell reads text back as, one argument per
// element, run in dir.
func shellRead(t *testing.T, text, dir string) []string {
	t.Helper()
	cmd := exec.Command("sh", "-c", `printf '%s\n' `+text)
	cmd.Dir = dir
	b, err := cmd.Output()
	if err != nil {
		t.Errorf("sh on %q: %v", text, err)
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// editedExport writes a copy of the export at file with change made to it,
// next to it under name, and returns the copy's path.
func editedExport(t *testing.T, h *harness, file, name string, change func(doc map[string]any)) string {
	t.Helper()
	doc := h.readExportFile(file)
	change(doc)
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	edited := h.exportPath(name)
	writeFile(t, edited, string(b))
	return edited
}

// TestImportQuotesTheSourceAddItPrints: the URL and the pin on a source add
// line come from the export, and a URL and a pin agentx accepts may still
// hold what a shell acts on: & ends a command, $( ) runs one and a quote
// opens a string. Pasted as printed, such a line ran a command the export
// chose and handed source add another pin. The argument is quoted for a
// POSIX shell, which reads back exactly the URL and the pin.
func TestImportQuotesTheSourceAddItPrints(t *testing.T) {
	t.Parallel()
	h, good := plainExport(t)
	file := editedExport(t, h, good, "hostile.json", func(doc map[string]any) {
		doc["settings"].(map[string]any)["sources"] = []any{
			map[string]any{"url": "https://example.com/o/a&id"},
			map[string]any{"url": "https://example.com/o/q", "pin": "it's"},
			map[string]any{"url": "https://example.com/o/r", "pin": "x$(touch${IFS}pwned)"},
		}
	})

	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	out := to.run("import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "    agentx source add 'https://example.com/o/r#x$(touch${IFS}pwned)'\n")

	dir := t.TempDir()
	want := []string{
		"https://example.com/o/a&id",
		"https://example.com/o/q#it's",
		"https://example.com/o/r#x$(touch${IFS}pwned)",
	}
	if got := printedSourceAdds(t, out.stdout, dir); !reflect.DeepEqual(got, want) {
		t.Errorf("the printed arguments read back as %q, want %q\n%s", got, want, out.stdout)
	}
	if left := entriesOf(t, dir); len(left) != 0 {
		t.Errorf("the printed lines ran a command, which left %v behind", left)
	}
}

// TestImportWithNoSkillsStillNamesTheSourcesToAdd: a machine that added
// sources and installed nothing exports no lineage record, and its sources
// still have to be added again on the machine an import makes. The source
// add lines follow the line saying the export lists no skills.
func TestImportWithNoSkillsStillNamesTheSourcesToAdd(t *testing.T) {
	t.Parallel()
	h, good := plainExport(t)
	file := editedExport(t, h, good, "sources-only.json", func(doc map[string]any) {
		doc["skills"] = []any{}
		doc["settings"].(map[string]any)["sources"] = []any{
			map[string]any{"url": "https://github.com/example/skills", "pin": "release"},
		}
	})

	to := newHarness(t)
	to.build(t, fixture{dirs: []string{".claude"}})
	out := to.run("import", file, "--yes")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "No skills in the export.\n"+
		"  only the settings were written: add each source again, then install a missing skill with 'agentx skill add <source>'\n"+
		"    agentx source add https://github.com/example/skills#release\n")
}
