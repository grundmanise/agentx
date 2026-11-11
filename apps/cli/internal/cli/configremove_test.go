package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// removalOf is the config_removal event of the configuration id.
func removalOf(t *testing.T, events []jsonEvent, id string) jsonEvent {
	t.Helper()
	for _, ev := range events {
		if ev["type"] == "config_removal" && ev["id"] == id {
			return ev
		}
	}
	t.Fatalf("no config_removal event for %s in %v", id, events)
	return nil
}

// deep is equal for values == cannot compare: lists and maps.
func deep(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

// leftoverHome is a machine as another tool's installs leave it: one
// library skill, placed into AdaL by a symlink, into Augment as a copy
// agentx recorded, an IBM Bob whose skills directory is empty, and a Claude
// Code that is in use.
func leftoverHome(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{
		dirs: []string{".bob/skills"},
		files: map[string]string{
			".agents/skills/pdf/SKILL.md":  skill("pdf", "Reads PDFs"),
			".augment/skills/pdf/SKILL.md": skill("pdf", "Reads PDFs"),
			".augment/skills/.DS_Store":    "finder",
			".claude/settings.json":        "{}\n",
		},
		links:    map[string]string{".adal/skills/pdf": ".agents/skills/pdf"},
		settings: `{"schema_version": 1, "disabled_configurations": ["adal", "cursor"], "copy_mode": {"pdf": ["augment", "claude-code"]}}`,
	})
	return h
}

// TestConfigRemoveDeletesLeftoverConfigurations previews what can be
// removed, which changes nothing, then removes three leftover
// configurations in one run: their directories are gone, the library is
// as it was, and the settings no longer name them.
func TestConfigRemoveDeletesLeftoverConfigurations(t *testing.T) {
	t.Parallel()
	h := leftoverHome(t)

	out := h.run("--json", "config", "remove")
	equal(t, "preview exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"config_removal", "config_removal", "config_removal", "config_removal", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preview event types = %v, want %v", got, want)
	}
	for _, id := range []string{"adal", "augment", "bob"} {
		equal(t, id+" state", removalOf(t, events, id)["state"], removalRemovable)
	}
	claude := removalOf(t, events, "claude-code")
	equal(t, "claude-code state", claude["state"], removalKept)
	deep(t, "claude-code holds", claude["holds"], any([]any{"settings.json"}))
	equal(t, "adal path", removalOf(t, events, "adal")["path"], filepath.Join(h.home, ".adal"))
	if _, err := os.Stat(filepath.Join(h.home, ".adal")); err != nil {
		t.Errorf("the preview removed a directory: %v", err)
	}
	before := mutationVersion(t, h)

	out = h.run("--json", "config", "remove", "adal", "augment")
	equal(t, "exit", out.exit, 0)
	events = h.events(out.stdout)
	if got, want := h.types(events), []string{"config_removal", "config_removal", "settings", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "adal state", removalOf(t, events, "adal")["state"], removalRemoved)
	equal(t, "summary", events[3]["summary"], "removed adal and augment")
	settings := events[2]["settings"].(map[string]any)
	deep(t, "disabled_configurations", settings["disabled_configurations"], any([]any{"cursor"}))
	deep(t, "copy_mode", settings["copy_mode"], any(map[string]any{"pdf": []any{"claude-code"}}))
	equal(t, "mutations", mutationVersion(t, h)-before, 1)
	for _, dir := range []string{".adal", ".augment"} {
		if _, err := os.Lstat(filepath.Join(h.home, dir)); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.library, "pdf", "SKILL.md")); err != nil {
		t.Errorf("the library lost its skill: %v", err)
	}
	if left := hiddenEntries(t, h.home); len(left) > 0 {
		t.Errorf("retained content left behind: %v", left)
	}
	equal(t, "enable a removed configuration", h.run("config", "enable", "adal").exit, 5)

	bob := filepath.Join(h.home, ".bob")
	out = h.run("config", "remove", "bob")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "✓ removed 1 configuration\n  bob  "+bob+"\n")
	equal(t, "stderr", out.stderr, "")

	out = h.run("config", "remove")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "Cannot be removed (1)\n  claude-code  "+filepath.Join(h.home, ".claude")+"  holds settings.json\n")
}

// TestConfigRemoveRefusals are the removals that change nothing: a
// configuration that holds something of its own, which holds back every
// configuration named with it, one that shares its directory with a
// configuration left out, and one that is not detected.
func TestConfigRemoveRefusals(t *testing.T) {
	t.Parallel()
	h := leftoverHome(t)
	before := mutationVersion(t, h)

	out := h.run("--json", "config", "remove", "adal", "claude-code")
	equal(t, "exit", out.exit, 6)
	events := h.events(out.stdout)
	equal(t, "adal state", removalOf(t, events, "adal")["state"], removalRemovable)
	equal(t, "claude-code state", removalOf(t, events, "claude-code")["state"], removalKept)
	ev := lastError(t, events)
	equal(t, "message", ev["message"], "nothing was removed: claude-code holds settings.json")
	equal(t, "hint", ev["hint"], "'agentx config remove adal' removes the others")
	if _, ok := isSymlink(t, filepath.Join(h.home, ".adal", "skills", "pdf")); !ok {
		t.Error("the refused run removed AdaL's placement")
	}
	equal(t, "mutations", mutationVersion(t, h)-before, 0)

	// Two clients keep one directory: naming one of them is held back.
	if err := os.MkdirAll(filepath.Join(h.home, ".zencoder"), 0o755); err != nil {
		t.Fatal(err)
	}
	out = h.run("config", "remove", "zencoder")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "hint: name zenflow as well: the directory is theirs too")

	out = h.run("config", "remove", "adal", "zed")
	equal(t, "exit", out.exit, 5)
	contains(t, "stderr", out.stderr, `configuration "zed" is not detected`)
	if _, err := os.Stat(filepath.Join(h.home, ".adal")); err != nil {
		t.Errorf("a run naming an undetected configuration removed one: %v", err)
	}
}

// TestRemovalScopeDeletesOnlyTheConfigurationDirectory holds every
// registered client to the rule of what a removal may delete: its
// configuration directory and nothing else, so never the library, agentx
// home, a parent directory or a skills directory outside it. The named
// cases are the clients whose paths the rule was written for.
func TestRemovalScopeDeletesOnlyTheConfigurationDirectory(t *testing.T) {
	t.Parallel()
	d := home.Dirs{User: "/u", Home: "/u/.agentx", Library: "/u/.agents/skills", Config: "/u/.config"}
	clients := registeredClients()
	for _, c := range clients {
		s := removalScopeOf(c, d, clients)
		if s.dir != filepath.Clean(c.ConfigDir(d)) || s.protected != "" {
			t.Errorf("%s: dir %q, protected %q", c.Slug(), s.dir, s.protected)
		}
		for _, protected := range []string{d.Library, d.Home, d.Config, d.User} {
			if beneath(protected, s.dir) {
				t.Errorf("%s: removing %s would delete %s", c.Slug(), s.dir, protected)
			}
		}
		for _, skills := range s.skills {
			if !beneath(skills, s.dir) || skills == d.Library {
				t.Errorf("%s: %s is judged as its own skills directory", c.Slug(), skills)
			}
		}
	}

	for _, c := range []struct {
		id      string
		dir     string
		skills  []string
		with    []string
		readers map[string][]string
	}{
		// Its skills directory is inside its configuration directory.
		{id: "adal", dir: "/u/.adal", skills: []string{"/u/.adal/skills"}},
		// Amp reads a skills directory outside its configuration directory.
		{id: "amp", dir: "/u/.config/amp"},
		// A universal client reads the library, which is not its own.
		{id: "cline", dir: "/u/.cline"},
		{id: "codex", dir: "/u/.codex", skills: []string{"/u/.codex/skills"}, readers: map[string][]string{"/u/.codex/skills": {"cursor"}}},
		// Antigravity lives inside Gemini CLI's directory: removing it
		// leaves ~/.gemini, and removing Gemini CLI takes it along.
		{id: "antigravity", dir: "/u/.gemini/antigravity", skills: []string{"/u/.gemini/antigravity/skills"}},
		{id: "gemini-cli", dir: "/u/.gemini", skills: []string{"/u/.gemini/skills"}, with: []string{"antigravity", "antigravity-cli"}},
		// Cursor reads Claude Code's skills directory too.
		{id: "claude-code", dir: "/u/.claude", skills: []string{"/u/.claude/skills"}, readers: map[string][]string{"/u/.claude/skills": {"cursor"}}},
		// Two clients kept in one directory go together.
		{id: "zencoder", dir: "/u/.zencoder", skills: []string{"/u/.zencoder/skills"}, with: []string{"zenflow"}},
		// The parent of a nested configuration directory stays.
		{id: "pi", dir: "/u/.pi/agent", skills: []string{"/u/.pi/agent/skills"}},
	} {
		client, ok := scan.Lookup(c.id)
		if !ok {
			t.Fatalf("%s is not registered", c.id)
		}
		s := removalScopeOf(client, d, clients)
		equal(t, c.id+" dir", s.dir, c.dir)
		deep(t, c.id+" skills", s.skills, c.skills)
		deep(t, c.id+" with", s.with, c.with)
		if c.readers == nil {
			c.readers = map[string][]string{}
		}
		deep(t, c.id+" readers", s.readers, c.readers)
	}

	// A library moved into a configuration directory protects it.
	adal, _ := scan.Lookup("adal")
	moved := d
	moved.Library = "/u/.adal/skills"
	equal(t, "protected", removalScopeOf(adal, moved, clients).protected, "the library")
}

// TestRemovalJudgment is what holds a configuration back, case by case,
// over a home built for each: only what survives elsewhere may go.
func TestRemovalJudgment(t *testing.T) {
	t.Parallel()
	pdf, other := skill("pdf", "Reads PDFs"), skill("pdf", "Reads other PDFs")
	for _, c := range []struct {
		name   string
		files  map[string]string
		links  map[string]string
		dirs   []string
		ids    []string
		system bool
		// unreadable is a file made unreadable; the case is skipped for a
		// user who reads every file, as root does.
		unreadable string
		want       map[string]string // id to "" when removable, else a part of what it holds
	}{
		{name: "a skill that is only there",
			files: map[string]string{".adal/skills/pdf/SKILL.md": pdf},
			ids:   []string{"adal"}, want: map[string]string{"adal": "the skill pdf, which nothing that stays on the machine holds"}},
		{name: "a copy that differs from the library's",
			files: map[string]string{".adal/skills/pdf/SKILL.md": other, ".agents/skills/pdf/SKILL.md": pdf},
			ids:   []string{"adal"}, want: map[string]string{"adal": "the skill pdf, which differs from the library's"}},
		{name: "a copy another configuration that stays holds too",
			files: map[string]string{".adal/skills/pdf/SKILL.md": pdf, ".claude/skills/pdf/SKILL.md": pdf},
			ids:   []string{"adal"}, want: map[string]string{"adal": ""}},
		{name: "the last copy is held back when both holders are named",
			files: map[string]string{".adal/skills/pdf/SKILL.md": pdf, ".augment/skills/pdf/SKILL.md": pdf},
			ids:   []string{"augment", "adal"}, want: map[string]string{"adal": "", "augment": "nothing that stays"}},
		{name: "a link back into the directory is no second copy",
			files: map[string]string{".adal/skills/pdf/SKILL.md": pdf},
			links: map[string]string{".claude/skills/pdf": ".adal/skills/pdf"},
			ids:   []string{"adal"}, want: map[string]string{"adal": "nothing that stays"}},
		{name: "a skills directory that is a symlink",
			dirs:  []string{"shared"},
			links: map[string]string{".adal/skills": "shared"},
			ids:   []string{"adal"}, want: map[string]string{"adal": ""}},
		{name: "a file beside the skills directory",
			files: map[string]string{".adal/history.jsonl": "{}\n"},
			dirs:  []string{".adal/skills", ".adal/cache/empty"},
			ids:   []string{"adal"}, want: map[string]string{"adal": "history.jsonl"}},
		{name: "a file the operating system writes counts while system files are not ignored",
			files: map[string]string{".adal/.DS_Store": "finder"},
			ids:   []string{"adal"}, want: map[string]string{"adal": ".DS_Store"}},
		{name: "a file the operating system writes does not count while they are ignored",
			files: map[string]string{".adal/.DS_Store": "finder", ".adal/sub/Thumbs.db": "explorer"}, system: true,
			ids: []string{"adal"}, want: map[string]string{"adal": ""}},
		{name: "a file that only matches a system file pattern always counts",
			files: map[string]string{".adal/settings.json~": "{}", ".adal/.nfsmount.conf": "x", ".adal/sub/._secrets": "x", ".adal/sub/.config.json.swp": "x"}, system: true,
			ids: []string{"adal"}, want: map[string]string{"adal": ".nfsmount.conf; settings.json~; sub"}},
		{name: "a library that is a symlink into the directory",
			files: map[string]string{".adal/skills/pdf/SKILL.md": pdf, ".bob/skills/pdf/SKILL.md": pdf},
			links: map[string]string{".agents/skills": ".adal/skills"},
			ids:   []string{"adal"}, want: map[string]string{"adal": "the library"}},
		{name: "a library skill that is a link into the directory",
			files: map[string]string{".adal/skills/pdf/SKILL.md": pdf, ".bob/skills/pdf/SKILL.md": pdf},
			links: map[string]string{".agents/skills/pdf": ".adal/skills/pdf"},
			ids:   []string{"adal"}, want: map[string]string{"adal": "the library's skill pdf, which is a link into this directory"}},
		{name: "a skill of a client that stays that is a link into the directory",
			files: map[string]string{".adal/skills/pdf/SKILL.md": pdf, ".agents/skills/pdf/SKILL.md": pdf},
			links: map[string]string{".bob/skills/pdf": ".adal/skills/pdf"},
			ids:   []string{"adal"}, want: map[string]string{"adal": "IBM Bob's skill pdf, which is a link into this directory"}},
		{name: "a file of a copy that cannot be read", unreadable: ".adal/skills/pdf/private.key",
			files: map[string]string{".adal/skills/pdf/SKILL.md": pdf, ".bob/skills/pdf/SKILL.md": pdf, ".bob/skills/pdf/private.key": "k", ".adal/skills/pdf/private.key": "k"},
			ids:   []string{"adal"}, want: map[string]string{"adal": "skills/pdf/private.key, which cannot be read"}},
		{name: "MCP server declarations",
			files: map[string]string{".cursor/mcp.json": "{}\n"},
			ids:   []string{"cursor"}, want: map[string]string{"cursor": "MCP server declarations in mcp.json"}},
		{name: "a skills directory another client reads",
			files: map[string]string{".agents/skills/pdf/SKILL.md": pdf, ".claude/skills/pdf/SKILL.md": pdf},
			dirs:  []string{".cursor"},
			ids:   []string{"claude-code"}, want: map[string]string{"claude-code": "skills, which Cursor reads too"}},
		{name: "a configuration directory that is a symlink",
			dirs:  []string{"dotfiles/adal"},
			links: map[string]string{".adal": "dotfiles/adal"},
			ids:   []string{"adal"}, want: map[string]string{"adal": "the directory is a symlink"}},
		{name: "a universal client with an empty directory",
			files: map[string]string{".agents/skills/pdf/SKILL.md": pdf},
			dirs:  []string{".cline"},
			ids:   []string{"cline"}, want: map[string]string{"cline": ""}},
		{name: "two clients in one directory, one named",
			dirs: []string{".zencoder/skills"},
			ids:  []string{"zencoder"}, want: map[string]string{"zencoder": "the configuration of Zenflow (zenflow), which is not named"}},
		{name: "two clients in one directory, both named",
			dirs: []string{".zencoder/skills"},
			ids:  []string{"zencoder", "zenflow"}, want: map[string]string{"zencoder": "", "zenflow": ""}},
		{name: "a configuration inside another, both named",
			dirs: []string{".gemini/antigravity/skills", ".gemini/skills"},
			ids:  []string{"gemini-cli", "antigravity"}, want: map[string]string{"antigravity": "", "gemini-cli": ""}},
		{name: "a configuration inside another that is kept",
			files: map[string]string{".gemini/antigravity/settings.json": "{}\n"},
			ids:   []string{"gemini-cli", "antigravity"}, want: map[string]string{"antigravity": "settings.json", "gemini-cli": "the configuration of Antigravity in antigravity"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixture{files: c.files, links: c.links, dirs: c.dirs})
			if c.unreadable != "" {
				path := filepath.Join(h.home, c.unreadable)
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				if f, err := os.Open(path); err == nil {
					f.Close()
					t.Skip("this user reads every file")
				}
			}
			d, err := home.Resolve(h.env)
			if err != nil {
				t.Fatal(err)
			}
			inv := &invocation{dirs: d}
			got := map[string]string{}
			for _, ev := range inv.judgeRemovals(c.ids, c.system) {
				holds := strings.Join(ev.Holds, "; ")
				if want := c.want[ev.ID]; (want == "") != (ev.State == removalRemovable) || !strings.Contains(holds, want) {
					t.Errorf("%s is %s and holds %q, want %q", ev.ID, ev.State, holds, want)
				}
				got[ev.ID] = ev.State
			}
			if len(got) != len(c.want) {
				t.Errorf("judged %v, want %d configurations", got, len(c.want))
			}
		})
	}

	// What one configuration is said to hold is bounded.
	equal(t, "clipped", clipHolds(slices.Repeat([]string{"x"}, holdsLimit+3))[holdsLimit], "3 more")
}
