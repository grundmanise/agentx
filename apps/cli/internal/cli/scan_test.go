package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden snapshot files from the current output")

// fixture builds one machine under the harness's HOME. Paths are relative to
// HOME, link targets too. The library is moved under HOME so that every path
// in the snapshot starts with it.
type fixture struct {
	files    map[string]string
	links    map[string]string
	dirs     []string
	settings string // settings.json content, "" for none
	project  string // passed to --project, relative to HOME
}

const skillMD = "---\nname: %s\ndescription: %s\n---\n\n# %s\n"

func skill(name, description string) string {
	return fmt.Sprintf(skillMD, name, description, name)
}

func (h *harness) build(t *testing.T, f fixture) {
	t.Helper()
	h.library = filepath.Join(h.home, ".agents", "skills")
	h.env["AGENTX_LIBRARY"] = h.library
	for _, dir := range f.dirs {
		if err := os.MkdirAll(filepath.Join(h.home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range f.files {
		full := filepath.Join(h.home, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for path, target := range f.links {
		full := filepath.Join(h.home, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(h.home, target), full); err != nil {
			t.Fatal(err)
		}
	}
	if f.settings != "" {
		if err := os.WriteFile(filepath.Join(h.agentx, "settings.json"), []byte(f.settings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A fixed machine id: the platform-derived id depends on the user id
	// running the tests, which a golden file cannot know.
	if err := os.WriteFile(filepath.Join(h.agentx, "machine.json"), []byte(`{"id": "0123456789abcdef0123456789abcdef"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.env["AGENTX_INSTANCE_ID"] = "instance-test"
}

// portable replaces HOME with ~ so output compares across temporary directories.
func (h *harness) portable(text string) string {
	real, err := filepath.EvalSymlinks(h.home)
	if err == nil && real != h.home {
		text = strings.ReplaceAll(text, real, "~")
	}
	return strings.ReplaceAll(text, h.home, "~")
}

var fixtures = map[string]fixture{
	"cursor-sees-claude": {
		dirs: []string{".cursor"},
		files: map[string]string{
			".claude/skills/commit/SKILL.md": skill("commit", "Write a commit message"),
		},
	},
	"all-clients": {
		dirs: []string{".codex", ".gemini", ".codeium/windsurf", ".copilot", ".cursor/skills/.hidden", ".cursor/skills/node_modules/pkg", ".cursor/skills/no-skill-md"},
		files: map[string]string{
			".claude/skills/commit/SKILL.md":           skill("commit", "Write a commit message"),
			".cursor/skills/review/SKILL.md":           "---\nname: \"review\"\ndescription: >-\n  Review a change\n  before merging\n---\n",
			".cursor/skills/node_modules/pkg/SKILL.md": skill("pkg", "Not a skill"),
			".cursor/skills/.hidden/SKILL.md":          skill("hidden", "Not a skill"),
			".cursor/skills/no-skill-md/README.md":     "no skill here",
			".codex/skills/plan/SKILL.md":              skill("plan", "Plan a task"),
			".gemini/skills/plan/SKILL.md":             skill("plan", "Plan a task"),
			".codeium/windsurf/skills/deploy/SKILL.md": skill("deploy", "Deploy the service"),
			".copilot/skills/test/SKILL.md":            skill("test", "Write tests"),
		},
		settings: `{"schema_version":1,"disabled_configurations":["windsurf"],"copy_mode":{"review":["cursor"]}}`,
	},
	"symlink-chain": {
		dirs: []string{".codex"},
		files: map[string]string{
			".agents/skills/commit/SKILL.md": skill("commit", "Write a commit message"),
		},
		links: map[string]string{
			".claude/skills/commit": ".agents/skills/commit",
			".cursor/skills/commit": ".claude/skills/commit",
			".codex/skills":         ".claude/skills",
		},
	},
	"broken-symlink": {
		files: map[string]string{
			".claude/skills/commit/SKILL.md": skill("commit", "Write a commit message"),
		},
		links: map[string]string{
			".claude/skills/gone": ".agents/skills/missing",
		},
	},
	"library-read-by-codex": {
		dirs: []string{".codex", ".gemini", ".cursor"},
		files: map[string]string{
			".agents/skills/commit/SKILL.md": skill("commit", "Write a commit message"),
		},
		settings: `{"schema_version":1,"disabled_configurations":["codex"]}`,
	},
	"unparsable-frontmatter": {
		files: map[string]string{
			".claude/skills/no-frontmatter/SKILL.md": "# Just a heading\n",
			".claude/skills/broken/SKILL.md":         "---\nname: broken\nthis line is not a key\n---\n",
			".claude/skills/unclosed/SKILL.md":       "---\nname: unclosed\n",
			".claude/skills/nameless/SKILL.md":       "---\ndescription: No name here\n---\n",
		},
	},
	"nested-symlink": {
		files: map[string]string{
			".claude/skills/docs/SKILL.md":         skill("docs", "Read the docs"),
			".claude/skills/docs/guide/GUIDE.md":   "the guide\n",
			".claude/skills/docs/guide/.hidden.md": "hidden but hashed\n",
			"elsewhere.txt":                        "outside the skill\n",
		},
		links: map[string]string{
			".claude/skills/docs/README.md":   ".claude/skills/docs/guide/GUIDE.md",
			".claude/skills/docs/outside.md":  "elsewhere.txt",
			".claude/skills/docs/guide-again": ".claude/skills/docs/guide",
			".claude/skills/docs/loop":        ".claude/skills/docs",
		},
	},
	"project-scope": {
		dirs: []string{".cursor", ".copilot"},
		files: map[string]string{
			".claude/skills/commit/SKILL.md":                  skill("commit", "Write a commit message"),
			"work/app/.claude/skills/migrate/SKILL.md":        skill("migrate", "Run a migration"),
			"work/app/.agents/skills/lint/SKILL.md":           skill("lint", "Lint the code"),
			"work/app/.github/skills/release/SKILL.md":        skill("release", "Cut a release"),
			"work/app/.cursor/skills/node_modules/x/SKILL.md": skill("x", "Not a skill"),
		},
		project: "work/app",
	},
}

func scanArgs(h *harness, f fixture) []string {
	args := []string{"--json", "scan"}
	if f.project != "" {
		args = append(args, "--project", filepath.Join(h.home, f.project))
	}
	return args
}

func TestScanGolden(t *testing.T) {
	t.Parallel()
	for name, f := range fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, f)
			args := scanArgs(h, f)

			first := h.run(args...)
			equal(t, "exit", first.exit, 0)
			equal(t, "stderr", first.stderr, "")
			second := h.run(args...)
			equal(t, "second exit", second.exit, 0)
			if first.stdout != second.stdout {
				t.Errorf("two scans differ:\n%s\n%s", first.stdout, second.stdout)
			}

			events := h.events(first.stdout)
			if got, want := h.types(events), []string{"snapshot", "result"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event types = %v, want %v", got, want)
			}
			got := h.portable(strings.SplitN(first.stdout, "\n", 2)[0] + "\n")
			path := filepath.Join("testdata", "golden", name+".snapshot.json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v; run go test ./internal/cli -run TestScanGolden -update to create it", err)
			}
			if got != string(want) {
				t.Errorf("snapshot differs from %s; run go test ./internal/cli -run TestScanGolden -update after checking the diff\ngot:\n%s\nwant:\n%s", path, got, want)
			}
		})
	}
}

// snapshot runs a JSON scan and returns the snapshot event.
func (h *harness) snapshot(t *testing.T, args ...string) jsonEvent {
	t.Helper()
	out := h.run(append([]string{"--json", "scan"}, args...)...)
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"snapshot", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	return events[0]
}

// occurrences lists "configuration kind scope path" for every occurrence of the named skill.
func occurrences(t *testing.T, h *harness, snap jsonEvent, name string) []string {
	t.Helper()
	var rows []string
	for _, s := range snap["skills"].([]any) {
		skill := s.(map[string]any)
		if skill["name"] != name {
			continue
		}
		for _, o := range skill["occurrences"].([]any) {
			occ := o.(map[string]any)
			rows = append(rows, fmt.Sprintf("%s %s %s %s", occ["configuration"], occ["kind"], occ["scope"], h.portable(occ["path"].(string))))
		}
	}
	sort.Strings(rows)
	return rows
}

func TestScanCursorSeesClaudeSkill(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixtures["cursor-sees-claude"])
	snap := h.snapshot(t)

	equal(t, "machine.id", snap["machine"].(map[string]any)["id"], "0123456789abcdef0123456789abcdef")
	equal(t, "machine.label", snap["machine"].(map[string]any)["label"], "test-host")
	equal(t, "instance_id", snap["instance_id"], "instance-test")
	equal(t, "scan_counter", snap["scan_counter"], float64(1))

	var slugs []string
	for _, c := range snap["configurations"].([]any) {
		conf := c.(map[string]any)
		slugs = append(slugs, conf["id"].(string))
		equal(t, "enabled", conf["enabled"], true)
	}
	if want := []string{"claude-code", "cursor"}; !reflect.DeepEqual(slugs, want) {
		t.Errorf("configurations = %v, want %v", slugs, want)
	}
	equal(t, "skill count", len(snap["skills"].([]any)), 1)
	want := []string{
		"claude-code directory user ~/.claude/skills/commit",
		"cursor directory user ~/.claude/skills/commit",
	}
	if got := occurrences(t, h, snap, "commit"); !reflect.DeepEqual(got, want) {
		t.Errorf("occurrences = %v, want %v", got, want)
	}
	equal(t, "edges", len(snap["edges"].([]any)), 4) // machine to two configurations, two configurations to one skill

	out := h.run("scan")
	equal(t, "exit", out.exit, 0)
	equal(t, "stderr", out.stderr, "")
	contains(t, "stdout", out.stdout, "Claude Code (claude-code)")
	contains(t, "stdout", out.stdout, "Cursor (cursor)")
	contains(t, "stdout", out.stdout, "Claude Code (claude-code)  "+filepath.Join(h.home, ".claude")+"  enabled")
	contains(t, "stdout", out.stdout, "commit  directory  user  "+filepath.Join(h.home, ".claude/skills/commit"))
	equal(t, "commit rows", strings.Count(out.stdout, "commit  directory  user"), 2)
}

func TestScanSymlinkChainAndLibrary(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixtures["symlink-chain"])
	snap := h.snapshot(t)

	equal(t, "skill count", len(snap["skills"].([]any)), 1)
	want := []string{
		"claude-code symlink user ~/.claude/skills/commit",
		"codex directory user ~/.agents/skills/commit",
		"codex symlink user ~/.codex/skills/commit",
		"cursor symlink user ~/.claude/skills/commit",
		"cursor symlink user ~/.codex/skills/commit",
		"cursor symlink user ~/.cursor/skills/commit",
	}
	if got := occurrences(t, h, snap, "commit"); !reflect.DeepEqual(got, want) {
		t.Errorf("occurrences = %v, want %v", got, want)
	}
	for _, o := range snap["skills"].([]any)[0].(map[string]any)["occurrences"].([]any) {
		equal(t, "resolved_path", h.portable(o.(map[string]any)["resolved_path"].(string)), "~/.agents/skills/commit")
	}

	out := h.run("scan")
	contains(t, "stdout", out.stdout, "commit  symlink  user  "+filepath.Join(h.home, ".cursor/skills/commit")+" -> "+filepath.Join(h.home, ".agents/skills/commit"))
}

func TestScanWarnings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fixture string
		want    []string
	}{
		{"broken-symlink", []string{"~/.claude/skills/gone: broken symlink, skipped"}},
		{"unparsable-frontmatter", []string{
			"~/.claude/skills/broken/SKILL.md: unparsable frontmatter at line 3, using the directory name",
			"~/.claude/skills/nameless/SKILL.md: frontmatter has no name, using the directory name",
			"~/.claude/skills/no-frontmatter/SKILL.md: no frontmatter, using the directory name",
			"~/.claude/skills/unclosed/SKILL.md: unparsable frontmatter, the --- block is not closed, using the directory name",
		}},
		{"nested-symlink", []string{
			"~/.claude/skills/docs/loop: symlink loops inside the skill, skipped",
			"~/.claude/skills/docs/outside.md: symlink resolves outside the skill, skipped",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixtures[tt.fixture])
			snap := h.snapshot(t)
			var got []string
			for _, w := range snap["warnings"].([]any) {
				got = append(got, h.portable(w.(string)))
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("warnings = %q, want %q", got, tt.want)
			}

			out := h.run("scan")
			equal(t, "exit", out.exit, 0)
			for _, w := range tt.want {
				contains(t, "stderr", h.portable(out.stderr), "warning: "+w)
			}
		})
	}
}

func TestScanProjectScope(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixtures["project-scope"])
	project := filepath.Join(h.home, "work", "app")

	snap := h.snapshot(t, "--project", project)
	got := map[string][]string{}
	for _, name := range []string{"commit", "migrate", "lint", "release", "x"} {
		got[name] = occurrences(t, h, snap, name)
	}
	want := map[string][]string{
		"commit":  {"claude-code directory user ~/.claude/skills/commit", "cursor directory user ~/.claude/skills/commit"},
		"migrate": {"claude-code directory project ~/work/app/.claude/skills/migrate", "cursor directory project ~/work/app/.claude/skills/migrate"},
		"lint":    {"cursor directory project ~/work/app/.agents/skills/lint", "github-copilot directory project ~/work/app/.agents/skills/lint"},
		"release": {"github-copilot directory project ~/work/app/.github/skills/release"},
		"x":       nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("occurrences = %v, want %v", got, want)
	}

	// Without the flag only user scope is scanned; nothing was written to the project.
	equal(t, "user-scope skills", len(h.snapshot(t)["skills"].([]any)), 1)
	equal(t, "project entries", listDir(t, project), ".agents .claude .cursor .github")

	out := h.run("--json", "scan", "--project", filepath.Join(h.home, "nowhere"))
	equal(t, "exit", out.exit, 5)
	events := h.events(out.stdout)
	equal(t, "error.code", events[0]["code"], "not_found")
	contains(t, "error.message", events[0]["message"].(string), filepath.Join(h.home, "nowhere"))

	out = h.run("scan", "--project", project)
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "migrate  directory  project  "+filepath.Join(project, ".claude/skills/migrate"))
}

func TestScanConfigurationFlag(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixtures["cursor-sees-claude"])

	whole := h.snapshot(t)
	targeted := h.snapshot(t, "--configuration", "cursor")
	if !reflect.DeepEqual(whole, targeted) {
		t.Errorf("a targeted scan differs from the whole scan:\n%v\n%v", whole, targeted)
	}

	out := h.run("--json", "scan", "--configuration", "codex")
	equal(t, "exit", out.exit, 5)
	events := h.events(out.stdout)
	equal(t, "error.code", events[0]["code"], "not_found")
	equal(t, "error.message", events[0]["message"], `configuration "codex" is not detected on this machine`)
	equal(t, "error.hint", events[0]["hint"], "detected configurations: claude-code, cursor")
}

func TestScanHoldsSharedLock(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixtures["cursor-sees-claude"])

	out := h.run("--json", "scan")
	equal(t, "exit", out.exit, 0)
	if _, err := os.Stat(filepath.Join(h.agentx, "version")); !os.IsNotExist(err) {
		t.Errorf("scan wrote the version file: %v", err)
	}
	equal(t, "home entries", listDir(t, h.agentx), "lock machine.json ops")

	lock, err := os.OpenFile(filepath.Join(h.agentx, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	out = h.run("--json", "scan")
	equal(t, "exit", out.exit, 7)
	events := h.events(out.stdout)
	equal(t, "error.code", events[0]["code"], "locked")
}

func TestConfigEnableDisable(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixtures["cursor-sees-claude"])

	enabled := func(snap jsonEvent) map[string]bool {
		m := map[string]bool{}
		for _, c := range snap["configurations"].([]any) {
			conf := c.(map[string]any)
			m[conf["id"].(string)] = conf["enabled"].(bool)
		}
		return m
	}
	if got, want := enabled(h.snapshot(t)), (map[string]bool{"claude-code": true, "cursor": true}); !reflect.DeepEqual(got, want) {
		t.Errorf("enabled = %v, want %v", got, want)
	}

	out := h.run("--json", "config", "disable", "cursor")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"settings", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if got, want := events[0]["settings"].(map[string]any)["disabled_configurations"], []any{"cursor"}; !reflect.DeepEqual(got, want) {
		t.Errorf("disabled_configurations = %v, want %v", got, want)
	}
	out = h.run("config", "disable", "cursor") // twice is a no-op
	equal(t, "exit", out.exit, 0)
	if got, want := readSettingsFile(t, h)["disabled_configurations"], []any{"cursor"}; !reflect.DeepEqual(got, want) {
		t.Errorf("disabled_configurations = %v, want %v", got, want)
	}
	if got, want := enabled(h.snapshot(t)), (map[string]bool{"claude-code": true, "cursor": false}); !reflect.DeepEqual(got, want) {
		t.Errorf("enabled = %v, want %v", got, want)
	}
	out = h.run("config", "list")
	contains(t, "stdout", out.stdout, "disabled_configurations  cursor")

	out = h.run("config", "enable", "cursor")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "")
	if got, want := readSettingsFile(t, h)["disabled_configurations"], []any{}; !reflect.DeepEqual(got, want) {
		t.Errorf("disabled_configurations = %v, want %v", got, want)
	}
	equal(t, "version", readVersion(t, h), 3)

	for _, args := range [][]string{{"config", "enable", "codex"}, {"config", "disable", "codex"}} {
		out = h.run(append([]string{"--json"}, args...)...)
		equal(t, "exit", out.exit, 5)
		events = h.events(out.stdout)
		equal(t, "error.code", events[0]["code"], "not_found")
		equal(t, "error.hint", events[0]["hint"], "detected configurations: claude-code, cursor")
	}
	for _, args := range [][]string{{"config", "enable"}, {"config", "disable", "a", "b"}} {
		equal(t, "exit", h.run(args...).exit, 1)
	}
}

// TestScanSpawnBudget puts a counting git on PATH and scans a 100-skill library.
func TestScanSpawnBudget(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	f := fixture{dirs: []string{".codex", ".gemini", ".cursor"}, files: map[string]string{}, links: map[string]string{}}
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("skill-%03d", i)
		f.files[".agents/skills/"+name+"/SKILL.md"] = skill(name, "Skill number "+name)
		f.files[".agents/skills/"+name+"/notes.md"] = strings.Repeat("notes\n", i)
		f.links[".claude/skills/"+name] = ".agents/skills/" + name
	}
	h.build(t, f)

	bin := filepath.Join(h.t.TempDir(), "bin")
	counter := filepath.Join(bin, "count")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho \"$@\" >> "+counter+"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.env["PATH"] = bin + string(os.PathListSeparator) + os.Getenv("PATH")

	start := time.Now()
	snap := h.snapshot(t)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("scan took %s, want a fraction of a second", elapsed)
	}
	equal(t, "skills", len(snap["skills"].([]any)), 100)
	for _, s := range snap["skills"].([]any) {
		equal(t, "occurrences per skill", len(s.(map[string]any)["occurrences"].([]any)), 4) // claude-code, codex, cursor, gemini-cli
	}
	if b, err := os.ReadFile(counter); err == nil {
		t.Errorf("scan spawned git %d times: %s", strings.Count(string(b), "\n"), b)
	}
}
