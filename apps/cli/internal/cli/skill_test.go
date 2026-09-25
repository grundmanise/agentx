package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// installHarness is a machine with four configurations, one source added
// and nothing installed yet: the starting point of every install test.
// Claude Code and Cursor keep their own skills directories; Codex and
// Gemini CLI read the library itself.
func installHarness(t *testing.T) (*harness, *sourceRepo) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex", ".gemini"}})
	s, _, _ := h.standardSource(true)
	s.executable("skills/alpha/scripts/run.sh") // a source holds modes too
	s.commit("an executable script")
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	return h, s
}

// eventsOfType returns the events of one type from a JSON run.
func (h *harness) eventsOfType(text, typ string) []jsonEvent {
	h.t.Helper()
	var found []jsonEvent
	for _, e := range h.events(text) {
		if e["type"] == typ {
			found = append(found, e)
		}
	}
	return found
}

// one returns the single event of a type, failing the test otherwise.
func (h *harness) one(text, typ string) jsonEvent {
	h.t.Helper()
	found := h.eventsOfType(text, typ)
	if len(found) != 1 {
		h.t.Fatalf("%d %s events, want 1:\n%s", len(found), typ, text)
	}
	return found[0]
}

func TestSkillAddPlacesTheSkillEverywhere(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha")
	equal(t, "exit", out.exit, 0)
	types := h.types(h.events(out.stdout))
	want := []string{"source", "progress", "progress", "progress", "progress", "library_skill", "result"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("events = %v, want %v\n%s", types, want, out.stdout)
	}

	// The library holds the skill under its frontmatter name, as a real
	// directory with the upstream's files.
	lib := filepath.Join(h.library, "alpha")
	for _, rel := range []string{"SKILL.md", "notes.md", "scripts/run.sh"} {
		if _, err := os.Stat(filepath.Join(lib, rel)); err != nil {
			t.Errorf("library: %v", err)
		}
	}
	if info, err := os.Stat(filepath.Join(lib, "scripts", "run.sh")); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("scripts/run.sh is not executable: %v", err)
	}

	// Claude Code and Cursor get a symlink; Codex and Gemini CLI, which read
	// the library, get none and are placed at the library itself.
	for _, rel := range []string{".claude/skills/alpha", ".cursor/skills/alpha"} {
		link := filepath.Join(h.home, rel)
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		equal(t, rel+" target", target, lib)
	}
	for _, rel := range []string{".codex/skills/alpha", ".gemini/skills/alpha"} {
		if _, err := os.Lstat(filepath.Join(h.home, rel)); err == nil {
			t.Errorf("%s exists: a client that reads the library needs no placement", rel)
		}
	}

	// The result says what happened, for a script that reads nothing else.
	result := h.one(out.stdout, "result")
	equal(t, "ok", result["ok"], true)
	// The source event and the summary both say when the source the version
	// was read from was fetched, so that an old fetch shows.
	fetched, _ := h.one(out.stdout, "source")["last_fetched"].(string)
	if fetched == "" {
		t.Error("the source event does not say when the source was fetched")
	}
	equal(t, "summary", result["summary"], "installed alpha from "+s.url+" under skills/alpha in 4 configurations, source fetched "+fetched+
		"; always available to universal clients: codex, gemini-cli")

	ev := h.one(out.stdout, "library_skill")
	equal(t, "name", ev["name"], "alpha")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "source", ev["source"], s.url)
	equal(t, "subpath", ev["subpath"], "skills/alpha")
	equal(t, "state", ev["state"], "current")
	equal(t, "drift", drift(ev), "")
	equal(t, "universal", universalOf(t, ev), "codex,gemini-cli")
	places := ev["placements"].([]any)
	var got []string
	for _, p := range places {
		p := p.(map[string]any)
		got = append(got, p["configuration"].(string)+" "+p["mode"].(string)+" "+p["kind"].(string))
	}
	// Cursor reads the Claude Code directory as well as its own, so it sees
	// the skill through both placements; the inventory reports what is
	// there rather than what the install meant.
	want = []string{
		"claude-code symlink symlink",
		"codex library library",
		"cursor symlink symlink",
		"cursor symlink symlink",
		"gemini-cli library library",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("placements = %v, want %v", got, want)
	}
}

func TestSkillAddWritesTheImportCommit(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)

	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	if parents := h.accountGit("rev-list", "--count", head); parents != "1" {
		t.Errorf("the import commit has %s commits behind it, want it parentless", parents)
	}
	body := h.accountGit("cat-file", "commit", head)
	for _, want := range []string{
		"author agentx <agentx@localhost> ",
		"committer agentx <agentx@localhost> ",
		"Agentx-Source: " + s.url,
		"Agentx-Path: skills/alpha",
	} {
		contains(t, "the import commit", body, want)
	}
	if strings.Contains(body, "Agentx-Machine") {
		t.Errorf("the import commit carries a machine trailer:\n%s", body)
	}
	// The dates are the upstream committer's time, written as epoch seconds
	// with a zero offset.
	when := s.bare("show", "-s", "--format=%ct", s.bare("rev-parse", "HEAD"))
	for _, who := range []string{"author", "committer"} {
		contains(t, "the import commit", body, who+" agentx <agentx@localhost> "+when+" +0000")
	}
	// The tree holds the upstream directory alone, with the modes git keeps.
	tree := h.accountGit("ls-tree", head)
	equal(t, "the import tree", tree, "040000 tree "+s.tree("skills/alpha")+"\talpha")
	files := h.accountGit("ls-tree", "-r", head)
	for _, line := range strings.Split(files, "\n") {
		if !strings.HasPrefix(line, "100644 ") && !strings.HasPrefix(line, "100755 ") {
			t.Errorf("the import tree holds %q, want only regular files", line)
		}
	}
}

// TestSkillAddSanitisesTheDirectoryItNames installs a skill from a
// directory the source named with a control sequence in it, under a name
// its frontmatter spells with a C1 control, which a library directory and
// an import branch can both hold. The line that confirms the install names
// both, and a source chose both, so they are sanitised there as source
// skills and skill list sanitise them. The directory's sequence opens with
// the C1 CSI rather than ESC: a directory holding a C0 control is refused
// at install, since the import commit cannot record it, while a C1 control
// is recorded and read back as it is, and a terminal obeys it all the same.
func TestSkillAddSanitisesTheDirectoryItNames(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("evil", true)
	const name = "pl\u009bain"
	s.write(filepath.Join("na\u009b31msty", "SKILL.md"), "---\nname: "+yamlQuoted(name)+"\ndescription: A plain skill\n---\n\n# plain\n")
	s.commit("skills")
	equal(t, "source add", h.run("source", "add", s.url).exit, 0)

	out := h.run("skill", "add", s.url, "--skill", name)
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "✓ installed pl ain from "+s.url+" under na 31msty at ")
	for _, r := range out.stdout {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("a control character reached the output: %q in\n%q", r, out.stdout)
		}
	}
}
