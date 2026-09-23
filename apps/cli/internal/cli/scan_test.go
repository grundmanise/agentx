package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode"
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
		content = strings.ReplaceAll(content, "$HOME", h.home) // a config file that names an absolute path
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
	"broken-symlinks": {
		files: map[string]string{
			".claude/skills/commit/SKILL.md": skill("commit", "Write a commit message"),
		},
		links: map[string]string{
			".claude/skills/gone": ".agents/skills/missing",
			".claude/skills/lost": ".agents/skills/missing",
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
	// The frontmatter block is YAML: every scalar form it allows reaches the
	// description, and a nested block contributes nothing.
	"frontmatter-scalars": {
		files: map[string]string{
			".claude/skills/continued/SKILL.md": "---\nname: continued\ndescription:\n  A description continued on\n  the lines below its key\n---\n",
			".claude/skills/folded/SKILL.md":    "---\nname: folded\ndescription: >-\n  A folded description\n  joined with spaces\n---\n",
			".claude/skills/literal/SKILL.md":   "---\nname: literal\ndescription: |\n  first line\n  second line\n---\n",
			".claude/skills/nested/SKILL.md":    "---\nname: nested\ndescription: A skill with a nested block\nmetadata:\n  author: someone\n  version: \"3.0.0\"\n---\n",
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
	"mcp-all-clients": {
		dirs: []string{".claude", ".codex", ".cursor", ".gemini", ".codeium/windsurf", ".copilot"},
		files: map[string]string{
			".claude.json": `{"numStartups": 3, "mcpServers": {
  "context7": {"type": "stdio", "command": "npx", "args": ["-y", "@upstash/context7-mcp@1.0.0"], "env": {"CONTEXT7_TOKEN": "secret-claude-env-value"}},
  "stripe": {"type": "http", "url": "https://MCP.Stripe.com:443/", "headers": {"Authorization": "Bearer secret-claude-header-value"}},
  "legacy": {"type": "sse", "url": "https://legacy.example.com/sse"}}}`,
			".cursor/mcp.json": `{"mcpServers": {
  "context7": {"command": "npx", "args": ["-y", "@upstash/context7-mcp"]},
  "stripe": {"url": "https://mcp.stripe.com", "headers": {"X-Key": "secret-cursor-header-value"}},
  "pg": {"command": "docker", "args": ["run", "-i", "--rm", "-e", "PGURL", "ghcr.io/example/pg-mcp:1.4"], "env": {"PGURL": "secret-cursor-env-value"}}}}`,
			".codex/config.toml": "model = \"o3\"\n\n[mcp_servers.fetch]\ncommand = \"uvx\"\nargs = [\"mcp-server-fetch==0.6\"]\nenv = { FETCH_KEY = \"secret-codex-env-value\" }\n\n[mcp_servers.figma]\nurl = \"https://mcp.figma.com/mcp\"\nbearer_token_env_var = \"FIGMA_TOKEN\"\nhttp_headers = { \"X-Figma-Region\" = \"secret-codex-header-value\" }\nenv_http_headers = { \"X-Env\" = \"FIGMA_ENV\" }\n",
			".gemini/settings.json": `{"theme": "dark", "mcpServers": {
  "events": {"url": "https://events.example.com/sse", "headers": {"Authorization": "secret-gemini-header-value"}},
  "stream": {"httpUrl": "https://stream.example.com/mcp"},
  "git": {"command": "pipx", "args": ["run", "mcp-server-git"]},
  "local": {"command": "python", "args": ["-m", "my_server"], "env": {"DB": "secret-gemini-env-value"}}}}`,
			".codeium/windsurf/mcp_config.json": `{"mcpServers": {
  "remote": {"serverUrl": "https://remote.example.com/mcp", "headers": {"API_KEY": "secret-windsurf-header-value"}},
  "pg": {"command": "docker", "args": ["run", "--rm", "-i", "ghcr.io/example/pg-mcp:2.0"], "env": {"PGURL": "secret-windsurf-env-value"}}}}`,
			".copilot/mcp-config.json": `{"mcpServers": {
  "sentry": {"type": "local", "command": "npx", "args": ["@sentry/mcp-server@latest"], "env": {"SENTRY_TOKEN": "secret-copilot-env-value"}, "tools": ["*"]},
  "cloudflare": {"type": "sse", "url": "https://docs.mcp.cloudflare.com/sse", "tools": ["*"]},
  "gh": {"type": "http", "url": "https://api.githubcopilot.com/mcp/readonly", "headers": {"X-MCP-Toolsets": "secret-copilot-header-value"}, "tools": ["*"]}}}`,
		},
	},
	"mcp-malformed": {
		dirs: []string{".claude", ".codex", ".cursor"},
		files: map[string]string{
			".claude.json":                           `{"mcpServers": {"empty": {"env": {"X": "secret-empty-env-value"}}, "ok": {"command": "echo"}, "text": "not an object"}}`,
			".claude/plugins/installed_plugins.json": `{"version": 2, "plugins": ["formatter@acme-tools"]}`,
			".cursor/mcp.json":                       `{"mcpServers": {"broken": {"env": {"K": "secret-cursor-broken-value"}}, "bare": {"env": {"K": secret-cursor-bare-value}}}}`,
			".codex/config.toml":                     "[mcp_servers.fetch]\ncommand = \"uvx\"\nenv = { K = secretcodexbarevalue }\n",
		},
	},
	"plugins": {
		dirs: []string{".codex", ".cursor"},
		files: map[string]string{
			".claude/plugins/installed_plugins.json":                                      `{"version": 2, "plugins": {"formatter@acme-tools": [{"scope": "user", "installPath": "$HOME/.claude/plugins/cache/acme-tools/formatter/1.2.0", "version": "1.2.0", "installedAt": "2026-09-01T00:00:00Z"}], "legacy@acme-tools": {"version": "0.1.0"}, "gone@acme-tools": [{"installPath": "$HOME/.claude/plugins/cache/acme-tools/gone/2.0.0", "version": "2.0.0"}]}}`,
			".claude/plugins/cache/acme-tools/formatter/1.2.0/.claude-plugin/plugin.json": `{"name": "formatter", "version": "1.2.0", "description": "Format code"}`,
			".claude/plugins/cache/acme-tools/formatter/1.2.0/skills/format/SKILL.md":     skill("format", "Format the code"),
			".claude/plugins/cache/acme-tools/formatter/1.2.0/.mcp.json":                  `{"mcpServers": {"formatter-db": {"command": "npx", "args": ["-y", "@acme/formatter-mcp"], "env": {"DB_TOKEN": "secret-plugin-env-value"}}}}`,
			".claude/skills/format/SKILL.md":                                              skill("format", "Format the code"),
			".gemini/extensions/security/gemini-extension.json":                           `{"name": "security", "version": "0.3.0", "mcpServers": {"scanner": {"command": "node", "args": ["${extensionPath}/server.js"]}}}`,
			".gemini/extensions/security/.gemini-extension-install.json":                  `{"source": "https://github.com/example/security-ext", "type": "git"}`,
			".gemini/extensions/security/skills/security-audit/SKILL.md":                  skill("security-audit", "Audit the code"),
			".gemini/extensions/notes/gemini-extension.json":                              `{"name": "notes", "version": "1.0.0"}`,
		},
	},
	"codex-plugins": {
		files: map[string]string{
			".codex/config.toml": "model = \"o3\"\n\n[marketplaces.team]\nsource_type = \"local\"\nsource = \"$HOME/marketplaces/team\"\n\n" +
				"[plugins.\"alpha@personal\"]\nenabled = true\n\n[plugins.\"alpha@personal\".mcp_servers.alpha-web]\nenabled = false\nstartup_timeout_sec = 30\n\n[plugins.\"beta@team\"]\nenabled = false\n\n[plugins.\"beta@team\".mcp_servers.agent-srv]\nenabled = true\n\n" +
				"[plugins.\"broken@personal\"]\n\n[plugins.\"delta@team\"]\n\n[plugins.\"ghost@team\"]\nenabled = true\n\n[plugins.\"no-marketplace\"]\nenabled = true\n",
			// alpha: two versions, local wins; the manifest names the server file;
			// the table disables one of its two servers.
			".codex/plugins/cache/personal/alpha/1.2.3/.codex-plugin/plugin.json": `{"name": "alpha", "version": "1.2.3"}`,
			".codex/plugins/cache/personal/alpha/1.2.3/skills/old/SKILL.md":       skill("old", "Must not appear"),
			".codex/plugins/cache/personal/alpha/local/.codex-plugin/plugin.json": `{"name": "alpha", "description": "Alpha tools", "mcpServers": "./conf/mcp.json"}`,
			".codex/plugins/cache/personal/alpha/local/skills/one/SKILL.md":       skill("one", "The first skill"),
			".codex/plugins/cache/personal/alpha/local/conf/mcp.json":             `{"mcpServers": {"alpha-db": {"command": "npx", "args": ["-y", "@acme/alpha-mcp"], "env": {"DB_TOKEN": "secret-codex-plugin-env-value"}}, "alpha-web": {"url": "https://mcp.example.com/alpha"}}}`,
			// beta: disabled, agent-plugins manifest at the root, servers in mcp.json.
			".codex/plugins/cache/team/beta/0.4.0/plugin.json":         `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "beta", "version": "0.4.0"}`,
			".codex/plugins/cache/team/beta/0.4.0/mcp.json":            `{"$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json", "mcpServers": {"agent-srv": {"type": "http", "url": "https://mcp.example.com/beta", "http_headers": {"X-Key": "secret-codex-plugin-header-value"}, "env_http_headers": {"X-Env": "BETA_ENV"}}}}`,
			".codex/plugins/cache/team/beta/0.4.0/skills/two/SKILL.md": skill("two", "The second skill"),
			// broken: a manifest that is not JSON.
			".codex/plugins/cache/personal/broken/1.0.0/.codex-plugin/plugin.json": `{not json`,
			// delta: semver decides between 1.9.0 and 1.10.0; a root plugin.json
			// without the agent-plugins schema is not the manifest; the manifest
			// names the skills root and declares its server inline.
			".codex/plugins/cache/team/delta/1.9.0/.codex-plugin/plugin.json":  `{"name": "delta", "version": "1.9.0"}`,
			".codex/plugins/cache/team/delta/1.10.0/plugin.json":               `{"name": "decoy", "version": "9.9.9"}`,
			".codex/plugins/cache/team/delta/1.10.0/.codex-plugin/plugin.json": `{"name": "delta", "version": "1.10.0", "skills": ["./extra"], "mcpServers": {"delta-srv": {"command": "node", "args": ["srv.js"], "env": {"TOKEN": "secret-codex-inline-env-value"}}}}`,
			".codex/plugins/cache/team/delta/1.10.0/extra/four/SKILL.md":       skill("four", "The fourth skill"),
			".codex/plugins/cache/team/delta/1.10.0/skills/ignored/SKILL.md":   skill("ignored", "Must not appear"),
			// gamma: a cache entry without a config key, a legacy .claude-plugin
			// manifest, a bare server map in .mcp.json.
			".codex/plugins/cache/team/gamma/2.0.0/.claude-plugin/plugin.json":  `{"name": "gamma", "version": "2.0.0"}`,
			".codex/plugins/cache/team/gamma/2.0.0/skills/three/SKILL.md":       skill("three", "The third skill"),
			".codex/plugins/cache/team/gamma/2.0.0/.mcp.json":                   `{"gamma-srv": {"command": "python", "args": ["-m", "gamma"]}}`,
			".codex/plugins/cache/team/gamma/.codex-remote-plugin-install.json": `{"schema_version": 1, "remote_plugin_id": "gamma-remote"}`,
		},
	},
	"cursor-plugins": {
		files: map[string]string{
			// local: both manifest formats, a plugin without a manifest, a
			// manifest that is not JSON, a file, a hidden directory.
			".cursor/plugins/local/agent-std/plugin.json":                    `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "agent-std", "version": "0.1.0"}`,
			".cursor/plugins/local/agent-std/skills/lint/SKILL.md":           skill("lint", "Lint the code"),
			".cursor/plugins/local/agent-std/mcp.json":                       `{"mcpServers": {"std-srv": {"command": "${CURSOR_PLUGIN_ROOT}/bin/srv", "args": ["--root", "${CURSOR_PLUGIN_ROOT}"], "env": {"TOKEN": "secret-cursor-plugin-env-value"}}}}`,
			".cursor/plugins/local/cursor-fmt/.cursor-plugin/plugin.json":    `{"name": "cursor-fmt", "version": "1.0.0", "description": "Format"}`,
			".cursor/plugins/local/cursor-fmt/rules/style.mdc":               "# style\n",
			".cursor/plugins/local/cursor-fmt/skills/format/SKILL.md":        skill("format", "Format the code"),
			".cursor/plugins/local/cursor-fmt/.mcp.json":                     `{"mcpServers": {"fmt-srv": {"command": "node", "args": ["${CLAUDE_PLUGIN_ROOT}/server.js"]}}}`,
			".cursor/plugins/local/bare/skills/notes/SKILL.md":               skill("notes", "Take notes"),
			".cursor/plugins/local/bad/.cursor-plugin/plugin.json":           `{not json`,
			".cursor/plugins/local/.store/stored/.cursor-plugin/plugin.json": `{"name": "stored", "version": "2.0.0"}`,
			".cursor/plugins/local/README.md":                                "not a plugin\n",
			// mani: the manifest names the skills root without the ./, one skill
			// by its own directory, one path that leaves the plugin, and
			// declares its server inline.
			".cursor/plugins/local/mani/.cursor-plugin/plugin.json": `{"name": "mani", "version": "0.2.0", "skills": ["tools", "solo/draft", "../outside"], "mcpServers": {"mani-srv": {"command": "${CURSOR_PLUGIN_ROOT}/bin/mani", "env": {"KEY": "secret-cursor-inline-env-value"}}}}`,
			".cursor/plugins/local/mani/tools/plan/SKILL.md":        skill("plan", "Plan the work"),
			".cursor/plugins/local/mani/solo/draft/SKILL.md":        skill("draft", "Draft the text"),
			".cursor/plugins/local/mani/skills/ignored/SKILL.md":    skill("ignored", "Must not appear"),
			// named: the manifest names the server file; mcp.json is not read.
			".cursor/plugins/local/named/.cursor-plugin/plugin.json": `{"name": "named", "version": "0.3.0", "mcpServers": "./conf/servers.json"}`,
			".cursor/plugins/local/named/conf/servers.json":          `{"mcpServers": {"named-srv": {"url": "https://named.example.com/mcp"}}}`,
			".cursor/plugins/local/named/mcp.json":                   `{"mcpServers": {"decoy-srv": {"command": "decoy"}}}`,
			"elsewhere/escaped/.cursor-plugin/plugin.json":           `{"name": "escaped", "version": "1.0.0"}`,
			// cache: a commit SHA and a release tag as version directories, one
			// entry without the completion marker.
			".cursor/plugins/cache/cursor-public/thermos/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b/.cache-complete":            "",
			".cursor/plugins/cache/cursor-public/thermos/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b/.cursor-plugin/plugin.json": `{"name": "thermos", "description": "Keep it warm"}`,
			".cursor/plugins/cache/cursor-public/thermos/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b/skills/brew/SKILL.md":       skill("brew", "Brew tea"),
			".cursor/plugins/cache/cursor-public/thermos/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b/.mcp.json":                  `{"mcpServers": {"thermos-api": {"url": "https://api.thermos.example.com/mcp", "headers": {"Authorization": "secret-cursor-plugin-header-value"}}}}`,
			".cursor/plugins/cache/acme-team/tools/release_v1.2.0/.cache-complete":                                            "",
			".cursor/plugins/cache/acme-team/tools/release_v1.2.0/.claude-plugin/plugin.json":                                 `{"name": "tools", "version": "1.2.0"}`,
			".cursor/plugins/cache/cursor-public/half/0000000000000000000000000000000000000000/.cursor-plugin/plugin.json":    `{"name": "half"}`,
		},
		links: map[string]string{
			".cursor/plugins/local/escape": "elsewhere/escaped",
			".cursor/plugins/local/linked": ".cursor/plugins/local/.store/stored",
		},
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
			row := fmt.Sprintf("%s %s %s %s", occ["configuration"], occ["kind"], occ["scope"], h.portable(occ["path"].(string)))
			if plugin, ok := occ["plugin"]; ok {
				row += " plugin=" + plugin.(string)
			}
			rows = append(rows, row)
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

// TestScanSanitisesUntrustedNames covers a skill whose frontmatter names it
// with the characters a terminal obeys. A SKILL.md comes from wherever the
// skill did, so the text inventory prints the name as Streams prescribes,
// while the snapshot event carries it as the file wrote it.
func TestScanSanitisesUntrustedNames(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const raw = "na\tsty \x1b[31mRED\x1b[0m"
	h.build(t, fixture{files: map[string]string{
		".claude/skills/nasty/SKILL.md": "---\n" + `name: "na\tsty \x1b[31mRED\x1b[0m"` + "\n" + `description: "two\nlines"` + "\n---\n",
	}})

	skills := h.snapshot(t)["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("skills = %v, want one", skills)
	}
	equal(t, "raw name", skills[0].(map[string]any)["name"], raw)

	out := h.run("scan")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "na sty [31mRED [0m  directory  user  ")
	for _, r := range out.stdout {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("a control character reached the inventory: %q in\n%q", r, out.stdout)
		}
	}
}

func TestScanWarnings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fixture string
		want    []string
	}{
		{"broken-symlink", []string{"~/.claude/skills/gone: broken symlink, skipped"}},
		{"broken-symlinks", []string{"~/.claude/skills: 2 broken symlinks (gone, lost), skipped"}},
		{"unparsable-frontmatter", []string{
			"~/.claude/skills/broken/SKILL.md: unparsable frontmatter at line 3: non-map value is specified, using the directory name",
			"~/.claude/skills/nameless/SKILL.md: frontmatter has no name, using the directory name",
			"~/.claude/skills/no-frontmatter/SKILL.md: no frontmatter, using the directory name",
			"~/.claude/skills/unclosed/SKILL.md: unparsable frontmatter, the --- block is not closed, using the directory name",
		}},
		{"nested-symlink", []string{
			"~/.claude/skills/docs/loop: symlink loops inside the skill, skipped",
			"~/.claude/skills/docs/outside.md: symlink resolves outside the skill, skipped",
		}},
		{"mcp-malformed", []string{
			"~/.claude/plugins/installed_plugins.json: invalid JSON, skipped",
			"~/.codex/config.toml: invalid TOML, skipped",
			"~/.cursor/mcp.json: invalid JSON, skipped",
		}},
		{"plugins", []string{
			"~/.claude/plugins/cache/acme-tools/gone/2.0.0: plugin gone@acme-tools is not installed there, skipped",
			"~/.claude/plugins/installed_plugins.json: plugin legacy@acme-tools has no installPath, skipped",
		}},
		{"codex-plugins", []string{
			"~/.codex/config.toml: plugin key \"no-marketplace\" is not <name>@<marketplace>, skipped",
			"~/.codex/plugins/cache/personal/broken/1.0.0/.codex-plugin/plugin.json: invalid JSON, skipped",
			"~/.codex/plugins/cache/team/ghost: plugin ghost@team is not installed there, skipped",
		}},
		{"cursor-plugins", []string{
			"~/.cursor/plugins/cache/cursor-public/half/0000000000000000000000000000000000000000: incomplete plugin cache, skipped",
			"~/.cursor/plugins/local/bad/.cursor-plugin/plugin.json: invalid JSON, skipped",
			"~/.cursor/plugins/local/escape: symlink resolves outside ~/.cursor/plugins/local, skipped",
			"~/.cursor/plugins/local/mani/.cursor-plugin/plugin.json: skills path \"../outside\" must be relative and stay inside the plugin, skipped",
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
			noSecrets(t, h, fixtures[tt.fixture])
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
	equal(t, "error.message", events[0]["message"], `configuration "codex" is not detected`)
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
	equal(t, "home entries", listDir(t, h.agentx), "lock machine.json mutations ops")

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
	equal(t, "stdout", out.stdout, "✓ cursor is now enabled\n")
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
	// Not parallel, for the reason harness_test.go gives above
	// suiteParallel: it counts the processes a scan spawns and times it,
	// and it writes a shim it then execs.
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
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}
	writeShim(t, filepath.Join(bin, "git"), "#!/bin/sh\necho \"$@\" >> "+counter+"\nexec "+real+" \"$@\"\n")
	_ = os.Remove(counter) // the --version that cleared ETXTBSY is not one of the scan's
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
	// The startup gate runs `git --version` once; the scan itself spawns nothing.
	b, _ := os.ReadFile(counter)
	if got := strings.TrimSpace(string(b)); got != "--version" {
		t.Errorf("git spawned with:\n%s\nwant exactly one --version", b)
	}
}

// secrets lists every distinctive secret value a fixture's config files hold,
// including the bare tokens a decoder quotes in its error message.
func secrets(f fixture) []string {
	var found []string
	for _, content := range f.files {
		found = append(found, regexp.MustCompile(`secret[a-z-]*`).FindAllString(content, -1)...)
	}
	sort.Strings(found)
	return found
}

// noSecrets scans in both output modes and fails when any secret value of
// f reaches stdout or stderr.
func noSecrets(t *testing.T, h *harness, f fixture) {
	t.Helper()
	for _, args := range [][]string{{"scan"}, {"--json", "scan"}} {
		out := h.run(args...)
		equal(t, "exit", out.exit, 0)
		for _, secret := range secrets(f) {
			if strings.Contains(out.stdout, secret) || strings.Contains(out.stderr, secret) {
				t.Errorf("%v printed %q", args, secret)
			}
		}
	}
}

// servers lists "name transport configuration command args|url" for every server occurrence.
func servers(t *testing.T, snap jsonEvent) (rows []string, nodes int) {
	t.Helper()
	for _, s := range snap["mcp_servers"].([]any) {
		node := s.(map[string]any)
		nodes++
		for _, o := range node["occurrences"].([]any) {
			occ := o.(map[string]any)
			what := occ["url"].(string)
			if cmd := occ["command"].(string); cmd != "" {
				what = cmd
				for _, a := range occ["args"].([]any) {
					what += " " + a.(string)
				}
			}
			rows = append(rows, fmt.Sprintf("%s %s %s %s", node["name"], occ["transport"], occ["configuration"], what))
		}
	}
	sort.Strings(rows)
	return rows, nodes
}

func TestScanMCPServers(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	f := fixtures["mcp-all-clients"]
	h.build(t, f)
	snap := h.snapshot(t)

	rows, nodes := servers(t, snap)
	want := []string{
		"cloudflare sse github-copilot https://docs.mcp.cloudflare.com/sse",
		"context7 stdio claude-code npx -y @upstash/context7-mcp@1.0.0",
		"context7 stdio cursor npx -y @upstash/context7-mcp",
		"events sse gemini-cli https://events.example.com/sse",
		"fetch stdio codex uvx mcp-server-fetch==0.6",
		"figma streamable-http codex https://mcp.figma.com/mcp",
		"gh streamable-http github-copilot https://api.githubcopilot.com/mcp/readonly",
		"git stdio gemini-cli pipx run mcp-server-git",
		"legacy sse claude-code https://legacy.example.com/sse",
		"local stdio gemini-cli python -m my_server",
		"pg stdio cursor docker run -i --rm -e PGURL ghcr.io/example/pg-mcp:1.4",
		"pg stdio windsurf docker run --rm -i ghcr.io/example/pg-mcp:2.0",
		"remote streamable-http windsurf https://remote.example.com/mcp",
		"sentry stdio github-copilot npx @sentry/mcp-server@latest",
		"stream streamable-http gemini-cli https://stream.example.com/mcp",
		"stripe streamable-http claude-code https://MCP.Stripe.com:443/",
		"stripe streamable-http cursor https://mcp.stripe.com",
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("server occurrences = %q, want %q", rows, want)
	}
	// The same package, the same normalised URL and the same image with another
	// tag each merge into one node with two occurrences.
	equal(t, "server nodes", nodes, 14)

	logical := map[string]string{}
	for _, s := range snap["mcp_servers"].([]any) {
		node := s.(map[string]any)
		logical[node["name"].(string)] = node["logical_id"].(string)
		equal(t, node["name"].(string)+" signature", node["signature"], "none")
		for _, o := range node["occurrences"].([]any) {
			occ := o.(map[string]any)
			equal(t, node["name"].(string)+" handshake", occ["handshake"], false)
			if _, ok := occ["plugin"]; ok {
				t.Errorf("%s: a configuration's own server carries a plugin field", node["name"])
			}
			switch node["name"] {
			case "figma":
				// bearer_token_env_var sets Authorization.
				if got := occ["header_keys"]; !reflect.DeepEqual(got, []any{"Authorization", "X-Env", "X-Figma-Region"}) {
					t.Errorf("figma header_keys = %v", got)
				}
			case "sentry":
				if got := occ["env_keys"]; !reflect.DeepEqual(got, []any{"SENTRY_TOKEN"}) {
					t.Errorf("sentry env_keys = %v", got)
				}
				equal(t, "sentry config_file", h.portable(occ["config_file"].(string)), "~/.copilot/mcp-config.json")
			case "stream":
				if got := occ["header_keys"]; !reflect.DeepEqual(got, []any{}) {
					t.Errorf("stream header_keys = %v, want empty", got)
				}
			}
		}
	}
	if logical["local"] == logical["git"] || logical["context7"] == logical["sentry"] {
		t.Errorf("distinct servers share a logical id: %v", logical)
	}
	equal(t, "edges", len(snap["edges"].([]any)), 6+17) // machine to six configurations, one per occurrence

	equal(t, "secrets in fixture", len(secrets(f)), 12)
	noSecrets(t, h, f)

	out := h.run("scan")
	contains(t, "stdout", out.stdout, "  servers:\n")
	contains(t, "stdout", out.stdout, "npx -y @upstash/context7-mcp@1.0.0")
	contains(t, "stdout", out.stdout, "streamable-http")
	contains(t, "stdout", out.stdout, "https://stream.example.com/mcp")
	if strings.Contains(out.stdout, "plugins:") {
		t.Errorf("a machine without plugins prints a plugins block:\n%s", out.stdout)
	}
}

// plugins lists "name|marketplace|version|configuration|path" for every
// plugin node, with "|enabled=<bool>" when the node carries the field, and
// the physical ids by name.
func plugins(t *testing.T, h *harness, snap jsonEvent) (rows []string, ids map[string]string) {
	t.Helper()
	ids = map[string]string{}
	for _, p := range snap["plugins"].([]any) {
		plugin := p.(map[string]any)
		ids[plugin["name"].(string)] = plugin["physical_id"].(string)
		row := fmt.Sprintf("%s|%s|%s|%s|%s", plugin["name"], plugin["marketplace"], plugin["version"], plugin["configuration"], h.portable(plugin["path"].(string)))
		if enabled, ok := plugin["enabled"]; ok {
			row += fmt.Sprintf("|enabled=%v", enabled)
		}
		rows = append(rows, row)
	}
	sort.Strings(rows)
	return rows, ids
}

func TestScanPlugins(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixtures["plugins"])
	snap := h.snapshot(t)

	rows, ids := plugins(t, h, snap)
	want := []string{
		"formatter|acme-tools|1.2.0|claude-code|~/.claude/plugins/cache/acme-tools/formatter/1.2.0",
		"notes||1.0.0|gemini-cli|~/.gemini/extensions/notes",
		"security|https://github.com/example/security-ext|0.3.0|gemini-cli|~/.gemini/extensions/security",
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("plugins = %q, want %q", rows, want)
	}

	equal(t, "skill nodes", len(snap["skills"].([]any)), 2)
	wantOcc := []string{
		"claude-code directory user ~/.claude/plugins/cache/acme-tools/formatter/1.2.0/skills/format plugin=formatter",
		"claude-code directory user ~/.claude/skills/format",
		"cursor directory user ~/.claude/skills/format",
	}
	if got := occurrences(t, h, snap, "format"); !reflect.DeepEqual(got, wantOcc) {
		t.Errorf("format occurrences = %q, want %q", got, wantOcc)
	}
	wantOcc = []string{"gemini-cli directory user ~/.gemini/extensions/security/skills/security-audit plugin=security"}
	if got := occurrences(t, h, snap, "security-audit"); !reflect.DeepEqual(got, wantOcc) {
		t.Errorf("security-audit occurrences = %q, want %q", got, wantOcc)
	}

	serverRows, nodes := servers(t, snap)
	// ${extensionPath} stands for the extension's directory.
	wantServers := []string{
		"formatter-db stdio claude-code npx -y @acme/formatter-mcp",
		"scanner stdio gemini-cli node " + filepath.Join(h.home, ".gemini/extensions/security/server.js"),
	}
	if !reflect.DeepEqual(serverRows, wantServers) {
		t.Errorf("server occurrences = %q, want %q", serverRows, wantServers)
	}
	equal(t, "server nodes", nodes, 2)
	for _, s := range snap["mcp_servers"].([]any) {
		node := s.(map[string]any)
		occ := node["occurrences"].([]any)[0].(map[string]any)
		equal(t, node["name"].(string)+" plugin", occ["plugin"], map[string]string{"formatter-db": "formatter", "scanner": "security"}[node["name"].(string)])
	}

	// Provides edges: plugin to skill and plugin to server, next to the
	// configuration's own edges.
	from := map[string]int{}
	for _, e := range snap["edges"].([]any) {
		from[e.(map[string]any)["from"].(string)]++
	}
	equal(t, "edges from formatter", from[ids["formatter"]], 2)
	equal(t, "edges from security", from[ids["security"]], 2)
	equal(t, "edges from notes", from[ids["notes"]], 0)
	equal(t, "edges", len(snap["edges"].([]any)), 4+3+3+2+4) // machine to four configurations, three plugins, three skill placements, two servers, four provides

	out := h.run("scan")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "  plugins:\n")
	contains(t, "stdout", out.stdout, "formatter  1.2.0  1 skill, 1 server\n")
	contains(t, "stdout", out.stdout, "(plugin formatter)")
	contains(t, "stdout", out.stdout, "notes     1.0.0\n")
	contains(t, "stdout", out.stdout, "security  0.3.0  1 skill, 1 server\n")
	noSecrets(t, h, fixtures["plugins"])
}

func TestScanCodexPlugins(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	f := fixtures["codex-plugins"]
	h.build(t, f)
	snap := h.snapshot(t)

	rows, ids := plugins(t, h, snap)
	want := []string{
		"alpha|personal|local|codex|~/.codex/plugins/cache/personal/alpha/local|enabled=true",
		"beta|team|0.4.0|codex|~/.codex/plugins/cache/team/beta/0.4.0|enabled=false",
		"broken|personal|1.0.0|codex|~/.codex/plugins/cache/personal/broken/1.0.0|enabled=true",
		"delta|team|1.10.0|codex|~/.codex/plugins/cache/team/delta/1.10.0|enabled=true",
		"gamma|team|2.0.0|codex|~/.codex/plugins/cache/team/gamma/2.0.0",
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("plugins = %q, want %q", rows, want)
	}

	equal(t, "skill nodes", len(snap["skills"].([]any)), 4)
	for skill, wantOcc := range map[string][]string{
		"one":     {"codex directory user ~/.codex/plugins/cache/personal/alpha/local/skills/one plugin=alpha"},
		"two":     {"codex directory user ~/.codex/plugins/cache/team/beta/0.4.0/skills/two plugin=beta"},
		"three":   {"codex directory user ~/.codex/plugins/cache/team/gamma/2.0.0/skills/three plugin=gamma"},
		"four":    {"codex directory user ~/.codex/plugins/cache/team/delta/1.10.0/extra/four plugin=delta"},
		"old":     nil,
		"ignored": nil,
	} {
		if got := occurrences(t, h, snap, skill); !reflect.DeepEqual(got, wantOcc) {
			t.Errorf("%s occurrences = %q, want %q", skill, got, wantOcc)
		}
	}

	serverRows, nodes := servers(t, snap)
	wantServers := []string{
		"agent-srv streamable-http codex https://mcp.example.com/beta",
		"alpha-db stdio codex npx -y @acme/alpha-mcp",
		"alpha-web streamable-http codex https://mcp.example.com/alpha",
		"delta-srv stdio codex node srv.js",
		"gamma-srv stdio codex python -m gamma",
	}
	if !reflect.DeepEqual(serverRows, wantServers) {
		t.Errorf("server occurrences = %q, want %q", serverRows, wantServers)
	}
	equal(t, "server nodes", nodes, 5)
	for _, s := range snap["mcp_servers"].([]any) {
		node := s.(map[string]any)
		occ := node["occurrences"].([]any)[0].(map[string]any)
		// alpha-web is turned off by its overlay, agent-srv with its plugin.
		if _, ok := occ["enabled"]; ok != (node["name"] == "alpha-web" || node["name"] == "agent-srv") {
			t.Errorf("%s occurrence enabled = %v, want it only on alpha-web and agent-srv", node["name"], occ["enabled"])
		}
		switch node["name"] {
		case "agent-srv":
			equal(t, "agent-srv plugin", occ["plugin"], "beta")
			equal(t, "agent-srv config_file", h.portable(occ["config_file"].(string)), "~/.codex/plugins/cache/team/beta/0.4.0/mcp.json")
			if got := occ["header_keys"]; !reflect.DeepEqual(got, []any{"X-Env", "X-Key"}) {
				t.Errorf("agent-srv header_keys = %v", got)
			}
		case "alpha-web":
			equal(t, "alpha-web plugin", occ["plugin"], "alpha")
			equal(t, "alpha-web enabled", occ["enabled"], false)
		case "alpha-db":
			equal(t, "alpha-db config_file", h.portable(occ["config_file"].(string)), "~/.codex/plugins/cache/personal/alpha/local/conf/mcp.json")
			if got := occ["env_keys"]; !reflect.DeepEqual(got, []any{"DB_TOKEN"}) {
				t.Errorf("alpha-db env_keys = %v", got)
			}
		case "delta-srv":
			equal(t, "delta-srv config_file", h.portable(occ["config_file"].(string)), "~/.codex/plugins/cache/team/delta/1.10.0/.codex-plugin/plugin.json")
			if got := occ["env_keys"]; !reflect.DeepEqual(got, []any{"TOKEN"}) {
				t.Errorf("delta-srv env_keys = %v", got)
			}
		case "gamma-srv":
			equal(t, "gamma-srv config_file", h.portable(occ["config_file"].(string)), "~/.codex/plugins/cache/team/gamma/2.0.0/.mcp.json")
		}
	}

	from := map[string]int{}
	for _, e := range snap["edges"].([]any) {
		from[e.(map[string]any)["from"].(string)]++
	}
	for name, n := range map[string]int{"alpha": 3, "beta": 2, "broken": 0, "delta": 2, "gamma": 2} {
		equal(t, "edges from "+name, from[ids[name]], n)
	}
	equal(t, "edges", len(snap["edges"].([]any)), 1+5+4+5+9) // machine to codex, five plugins, four skills, five servers, nine provides

	out := h.run("scan")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "  plugins:\n")
	var beta, alphaWeb string
	for _, line := range strings.Split(out.stdout, "\n") {
		switch row := strings.TrimSpace(line); {
		case strings.HasPrefix(row, "beta "):
			beta = line
		case strings.HasPrefix(row, "alpha-web "):
			alphaWeb = line
		}
	}
	contains(t, "beta line", beta, "0.4.0")
	contains(t, "beta line", beta, "(disabled)")
	contains(t, "alpha-web line", alphaWeb, "(plugin alpha)")
	contains(t, "alpha-web line", alphaWeb, "(disabled)")
	equal(t, "disabled markers", strings.Count(out.stdout, "(disabled)"), 3)
	equal(t, "secrets in fixture", len(secrets(f)), 3)
	noSecrets(t, h, f)
}

func TestScanCursorPlugins(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	f := fixtures["cursor-plugins"]
	h.build(t, f)
	snap := h.snapshot(t)

	rows, ids := plugins(t, h, snap)
	want := []string{
		"agent-std||0.1.0|cursor|~/.cursor/plugins/local/agent-std",
		"bad|||cursor|~/.cursor/plugins/local/bad",
		"bare|||cursor|~/.cursor/plugins/local/bare",
		"cursor-fmt||1.0.0|cursor|~/.cursor/plugins/local/cursor-fmt",
		"mani||0.2.0|cursor|~/.cursor/plugins/local/mani",
		"named||0.3.0|cursor|~/.cursor/plugins/local/named",
		"stored||2.0.0|cursor|~/.cursor/plugins/local/linked",
		"thermos|cursor-public|9f86d081884c7d659a2feaa0c55ad015a3bf4f1b|cursor|~/.cursor/plugins/cache/cursor-public/thermos/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b",
		"tools|acme-team|1.2.0|cursor|~/.cursor/plugins/cache/acme-team/tools/release_v1.2.0",
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("plugins = %q, want %q", rows, want)
	}

	equal(t, "skill nodes", len(snap["skills"].([]any)), 6)
	for skill, wantOcc := range map[string][]string{
		"lint":    {"cursor directory user ~/.cursor/plugins/local/agent-std/skills/lint plugin=agent-std"},
		"format":  {"cursor directory user ~/.cursor/plugins/local/cursor-fmt/skills/format plugin=cursor-fmt"},
		"notes":   {"cursor directory user ~/.cursor/plugins/local/bare/skills/notes plugin=bare"},
		"brew":    {"cursor directory user ~/.cursor/plugins/cache/cursor-public/thermos/9f86d081884c7d659a2feaa0c55ad015a3bf4f1b/skills/brew plugin=thermos"},
		"plan":    {"cursor directory user ~/.cursor/plugins/local/mani/tools/plan plugin=mani"},
		"draft":   {"cursor directory user ~/.cursor/plugins/local/mani/solo/draft plugin=mani"},
		"ignored": nil,
	} {
		if got := occurrences(t, h, snap, skill); !reflect.DeepEqual(got, wantOcc) {
			t.Errorf("%s occurrences = %q, want %q", skill, got, wantOcc)
		}
	}

	serverRows, nodes := servers(t, snap)
	for i := range serverRows {
		serverRows[i] = h.portable(serverRows[i])
	}
	wantServers := []string{
		"fmt-srv stdio cursor node ~/.cursor/plugins/local/cursor-fmt/server.js",
		"mani-srv stdio cursor ~/.cursor/plugins/local/mani/bin/mani",
		"named-srv streamable-http cursor https://named.example.com/mcp",
		"std-srv stdio cursor ~/.cursor/plugins/local/agent-std/bin/srv --root ~/.cursor/plugins/local/agent-std",
		"thermos-api streamable-http cursor https://api.thermos.example.com/mcp",
	}
	if !reflect.DeepEqual(serverRows, wantServers) {
		t.Errorf("server occurrences = %q, want %q", serverRows, wantServers)
	}
	equal(t, "server nodes", nodes, 5)
	for _, s := range snap["mcp_servers"].([]any) {
		node := s.(map[string]any)
		occ := node["occurrences"].([]any)[0].(map[string]any)
		switch node["name"] {
		case "std-srv":
			equal(t, "std-srv plugin", occ["plugin"], "agent-std")
			equal(t, "std-srv config_file", h.portable(occ["config_file"].(string)), "~/.cursor/plugins/local/agent-std/mcp.json")
		case "fmt-srv":
			equal(t, "fmt-srv config_file", h.portable(occ["config_file"].(string)), "~/.cursor/plugins/local/cursor-fmt/.mcp.json")
		case "mani-srv":
			equal(t, "mani-srv config_file", h.portable(occ["config_file"].(string)), "~/.cursor/plugins/local/mani/.cursor-plugin/plugin.json")
			if got := occ["env_keys"]; !reflect.DeepEqual(got, []any{"KEY"}) {
				t.Errorf("mani-srv env_keys = %v", got)
			}
		case "named-srv":
			equal(t, "named-srv config_file", h.portable(occ["config_file"].(string)), "~/.cursor/plugins/local/named/conf/servers.json")
		case "thermos-api":
			if got := occ["header_keys"]; !reflect.DeepEqual(got, []any{"Authorization"}) {
				t.Errorf("thermos-api header_keys = %v", got)
			}
		}
	}

	from := map[string]int{}
	for _, e := range snap["edges"].([]any) {
		from[e.(map[string]any)["from"].(string)]++
	}
	for name, n := range map[string]int{"agent-std": 2, "bad": 0, "bare": 1, "cursor-fmt": 2, "mani": 3, "named": 1, "stored": 0, "thermos": 2, "tools": 0} {
		equal(t, "edges from "+name, from[ids[name]], n)
	}
	equal(t, "edges", len(snap["edges"].([]any)), 1+9+6+5+11) // machine to cursor, nine plugins, six skills, five servers, eleven provides

	out := h.run("scan")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "  plugins:\n")
	contains(t, "stdout", h.portable(out.stdout), "~/.cursor/plugins/local/agent-std/bin/srv --root ~/.cursor/plugins/local/agent-std  (plugin agent-std)")
	if !regexp.MustCompile(`(?m)^ +thermos +9f86d081884c7d659a2feaa0c55ad015a3bf4f1b  1 skill, 1 server$`).MatchString(out.stdout) {
		t.Errorf("stdout has no thermos plugin line with its commit as the version:\n%s", out.stdout)
	}
	if strings.Contains(out.stdout, "(disabled)") {
		t.Errorf("Cursor records no enabled state, yet the output marks a plugin disabled:\n%s", out.stdout)
	}
	equal(t, "secrets in fixture", len(secrets(f)), 3)
	noSecrets(t, h, f)
}

// TestScanQuotesPathsAndSanitisesDeclarations covers the values of the
// inventory that a skill, a plugin or a configuration file supplies rather
// than agentx: the path a placement is at, the path a symlink resolves to,
// the command line or URL of a server declaration, and the warnings a scan
// ends with. A directory name is chosen by whoever made the directory and a
// declaration by whoever wrote the file, so none of them is agentx's own
// text. The two are printed by different rules: a path is quoted, being
// something to copy, and a declaration is sanitised, being something to
// read.
func TestScanQuotesPathsAndSanitisesDeclarations(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{
		files: map[string]string{
			".claude/skills/na\x1b[31msty/SKILL.md":  skill("nasty", "A skill in a directory named to be obeyed"),
			".agents/skills/li\x1b[2Kbrary/SKILL.md": skill("linked", "A library skill a placement points at"),
			".claude.json":                           `{"mcpServers": {"evil": {"command": "\u001b[2Kecho", "args": ["\u001b]0;pwned\u0007"]}}}`,
		},
		links: map[string]string{
			".claude/skills/linked":      ".agents/skills/li\x1b[2Kbrary",
			".claude/skills/go\x1b[2Kne": ".agents/skills/missing",
		},
	})

	out := h.run("scan")
	equal(t, "exit", out.exit, 0)
	stdout := h.portable(out.stdout)
	// The path of the placement and the path the symlink resolves to are
	// quoted whole, so what is printed still names the directory on disk.
	contains(t, "stdout", stdout, `"~/.claude/skills/na\033[31msty"`)
	// Only what needs quoting is quoted: the placement's own path carries
	// nothing a terminal obeys and is printed as it is.
	contains(t, "stdout", stdout, `~/.claude/skills/linked -> "~/.agents/skills/li\033[2Kbrary"`)
	// The command line is sanitised: joining the arguments with spaces has
	// already made it something to read rather than something to run.
	contains(t, "stdout", stdout, "evil  stdio  [2Kecho ]0;pwned")
	// A warning names a path it read, so it carries the same text.
	contains(t, "stderr", h.portable(out.stderr), "broken symlink, skipped")
	for _, stream := range []struct{ name, text string }{{"stdout", out.stdout}, {"stderr", out.stderr}} {
		for _, r := range stream.text {
			if unicode.IsControl(r) && r != '\n' {
				t.Fatalf("a control character reached %s: %q in\n%q", stream.name, r, stream.text)
			}
		}
	}

	// The snapshot carries every one of them as it was read.
	skills := h.snapshot(t)["skills"].([]any)
	var paths []string
	for _, s := range skills {
		for _, o := range s.(map[string]any)["occurrences"].([]any) {
			paths = append(paths, h.portable(o.(map[string]any)["path"].(string)))
		}
	}
	if !slices.Contains(paths, "~/.claude/skills/na\x1b[31msty") {
		t.Errorf("the snapshot does not carry the path as it is: %q", paths)
	}
}
