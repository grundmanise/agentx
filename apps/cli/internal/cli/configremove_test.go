package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// deep is equal for values == cannot compare: lists and maps.
func deep(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

// removedIDs are the ids of the config_removal events, in their order.
func removedIDs(events []jsonEvent) []string {
	var ids []string
	for _, ev := range events {
		if ev["type"] == "config_removal" {
			ids = append(ids, ev["id"].(string))
		}
	}
	return ids
}

// TestConfigRemoveDeletesTheNamedDirectories removes configurations
// whatever their directories hold: the directories are gone and the
// settings no longer name them. Two configurations kept in one directory
// are one removal, and a configuration whose directory lies inside one
// that goes is removed and reported with it.
func TestConfigRemoveDeletesTheNamedDirectories(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{
		dirs: []string{".zencoder/skills", ".gemini/antigravity/skills"},
		files: map[string]string{
			".agents/skills/pdf/SKILL.md":  skill("pdf", "Reads PDFs"),
			".augment/skills/pdf/SKILL.md": skill("pdf", "Reads other PDFs"),
			".augment/settings.json":       "{}\n",
			".claude/settings.json":        "{}\n",
		},
		links:    map[string]string{".adal/skills/pdf": ".agents/skills/pdf"},
		settings: `{"schema_version": 1, "disabled_configurations": ["adal", "cursor"], "copy_mode": {"pdf": ["augment", "claude-code"]}}`,
	})
	before := mutationVersion(t, h)

	out := h.run("--json", "config", "remove", "augment", "adal", "--yes")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"config_removal", "config_removal", "settings", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	deep(t, "the event of adal", events[0], jsonEvent{
		"type": "config_removal", "schema_version": float64(schemaVersion),
		"id": "adal", "name": "AdaL", "path": filepath.Join(h.home, ".adal"),
	})
	equal(t, "the second event", events[1]["id"], "augment")
	settings := events[2]["settings"].(map[string]any)
	deep(t, "disabled_configurations", settings["disabled_configurations"], any([]any{"cursor"}))
	deep(t, "copy_mode", settings["copy_mode"], any(map[string]any{"pdf": []any{"claude-code"}}))
	equal(t, "summary", events[3]["summary"], "removed adal and augment")
	equal(t, "mutations", mutationVersion(t, h)-before, 1)
	for _, dir := range []string{".adal", ".augment"} {
		if _, err := os.Lstat(filepath.Join(h.home, dir)); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", dir, err)
		}
	}
	if left := hiddenEntries(t, h.home); len(left) > 0 {
		t.Errorf("retained content left behind: %v", left)
	}

	// Zencoder and Zenflow keep one directory, and Antigravity's lies
	// inside Gemini CLI's.
	out = h.run("--json", "config", "remove", "zencoder", "gemini-cli", "zenflow", "--yes")
	equal(t, "exit", out.exit, 0)
	events = h.events(out.stdout)
	deep(t, "removed", removedIDs(events), []string{"antigravity", "gemini-cli", "zencoder", "zenflow"})
	equal(t, "the path of antigravity", events[0]["path"], filepath.Join(h.home, ".gemini", "antigravity"))
	for _, dir := range []string{".zencoder", ".gemini"} {
		if _, err := os.Lstat(filepath.Join(h.home, dir)); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", dir, err)
		}
	}

	claude := filepath.Join(h.home, ".claude")
	out = h.run("config", "remove", "claude-code", "--yes")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "✓ removed 1 configuration\n  claude-code  "+claude+"\n")
	equal(t, "stderr", out.stderr, "")
}

// TestConfigRemoveRefusals are the removals that change nothing: one that
// cannot ask and was not told --yes, one that names no configuration or
// one that is not detected, and one whose directory holds the library.
func TestConfigRemoveRefusals(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{
		dirs:  []string{".adal/skills", ".bob/skills"},
		links: map[string]string{".agents/skills": ".bob/skills"},
	})
	adal, bob := filepath.Join(h.home, ".adal"), filepath.Join(h.home, ".bob")
	before := mutationVersion(t, h)

	// Neither stream is a terminal here, which is every script and the
	// desktop app.
	out := h.run("config", "remove", "adal")
	equal(t, "without --yes: exit", out.exit, 6)
	equal(t, "without --yes: stderr", out.stderr, "error: removing deletes "+adal+", and there is no terminal to confirm at\n"+
		"hint: run it again with --yes to remove without asking\n")

	out = h.run("--json", "config", "remove", "adal")
	equal(t, "JSON without --yes: exit", out.exit, 6)
	equal(t, "JSON without --yes: code", lastError(t, h.events(out.stdout))["code"], "refused")

	out = h.run("config", "remove", "--yes")
	equal(t, "no id: exit", out.exit, 1)

	out = h.run("config", "remove", "adal", "zed", "--yes")
	equal(t, "not detected: exit", out.exit, 5)
	contains(t, "not detected: stderr", out.stderr, `configuration "zed" is not detected`)

	// The library is a symlink into IBM Bob's directory.
	out = h.run("config", "remove", "adal", "bob", "--yes")
	equal(t, "the library: exit", out.exit, 6)
	contains(t, "the library: stderr", out.stderr, "error: "+bob+" is or holds the library, so nothing was removed")

	for _, dir := range []string{adal, bob} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("a refused run removed a directory: %v", err)
		}
	}
	equal(t, "mutations", mutationVersion(t, h)-before, 0)
}

// TestConfigRemoveKeepsWhatAgentxWorksFrom is the one rule on what a
// removal may delete: a directory that is, or holds, the library, agentx
// home or the user's home directory stays. A configuration directory that
// is a symlink is only unlinked, so where it leads does not count.
func TestConfigRemoveKeepsWhatAgentxWorksFrom(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(root, "home")
	for _, dir := range []string{".adal", "elsewhere/skills", "elsewhere/agentx"} {
		if err := os.MkdirAll(filepath.Join(user, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{"library": "elsewhere/skills", ".bob": "elsewhere"} {
		if err := os.Symlink(filepath.Join(user, target), filepath.Join(user, link)); err != nil {
			t.Fatal(err)
		}
	}
	at := func(rel string) string { return filepath.Join(user, rel) }
	for _, c := range []struct {
		name                string
		library, agentx, of string // "" for where it is by default
		dir                 string
		want                string
	}{
		{name: "nothing agentx works from", dir: ".adal"},
		{name: "the library, as it is spelled", library: ".adal/skills", dir: ".adal", want: "the library"},
		{name: "the library, a symlink into it", library: "library", dir: "elsewhere", want: "the library"},
		{name: "agentx home", agentx: "elsewhere/agentx", dir: "elsewhere", want: "agentx home"},
		{name: "the home directory", of: "elsewhere/skills", dir: "elsewhere", want: "your home directory"},
		{name: "a symlink to where the library is", library: "library", dir: ".bob"},
	} {
		d := home.Dirs{User: user, Home: at(".agentx"), Library: at(".agents/skills")}
		if c.library != "" {
			d.Library = at(c.library)
		}
		if c.agentx != "" {
			d.Home = at(c.agentx)
		}
		if c.of != "" {
			d.User = at(c.of)
		}
		inv := &invocation{dirs: d}
		equal(t, c.name, inv.neededIn(at(c.dir)), c.want)
	}
}

// TestConfigRemoveReadsTheAnswer is the question itself, which names every
// directory that is deleted: yes in the forms a person types it, and
// everything else left as a refusal.
func TestConfigRemoveReadsTheAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		answer string
		yes    bool
	}{
		{"y\n", true}, {"  YES  \n", true}, {"\n", false}, {"", false}, {"no\n", false},
	} {
		var stdout, stderr bytes.Buffer
		inv := &invocation{out: &writer{stdout: &stdout, stderr: &stderr, env: map[string]string{}}}
		err := inv.askToRemove([]string{"/u/.adal", "/u/.augment"}, strings.NewReader(tc.answer), "")
		if (err == nil) != tc.yes {
			t.Errorf("the answer %q gave %v, want yes = %v", tc.answer, err, tc.yes)
		}
		equal(t, "the question", stdout.String(), "Delete /u/.adal and /u/.augment with everything in them? [y/N] ")
		if err != nil {
			var f *failure
			if !errors.As(err, &f) || f.status != exitRefused {
				t.Errorf("a declined removal gave %v, want a refusal", err)
			}
		}
	}
}
