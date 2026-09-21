package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// mcpServer is the fixture MCP server built by TestMain; mcpServerErr says
// why it is missing.
var (
	mcpServer    string
	mcpServerErr error
)

// TestMain builds the fixture server once per test run. Tests that need it
// skip when go is not on PATH.
func TestMain(m *testing.M) {
	os.Exit(func() int {
		if _, err := exec.LookPath("go"); err != nil {
			mcpServerErr = err
			return m.Run()
		}
		dir, err := os.MkdirTemp("", "agentx-mcpserver")
		if err != nil {
			mcpServerErr = err
			return m.Run()
		}
		defer os.RemoveAll(dir)
		mcpServer = filepath.Join(dir, "mcpserver")
		if out, err := exec.Command("go", "build", "-o", mcpServer, "./testdata/mcpserver").CombinedOutput(); err != nil {
			mcpServerErr = fmt.Errorf("build the fixture server: %v\n%s", err, out)
		}
		return m.Run()
	}())
}

// webServer is a streamable-http MCP endpoint: it refuses a request without
// the declared header value, assigns a session id that must be echoed, and
// answers tools/list as an event stream that opens with a comment, an event
// without data and a notification before the response.
func webServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "secret-hand-header-value" && r.Header.Get("Authorization") != "Bearer secret-hand-bearer-value" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodDelete {
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if m.Method != "initialize" && (r.Header.Get("Mcp-Session-Id") != "session-1" || r.Header.Get("MCP-Protocol-Version") != "2025-11-25") {
			http.Error(w, "no session", http.StatusBadRequest)
			return
		}
		switch m.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session-1")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"web","version":"0"}}}`, m.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, ": keep-alive\n\nid: 1\ndata:\n\nid: 2\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"level\":\"info\",\"data\":\"listing\"}}\n\nevent: message\r\nid: 3\r\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\r\ndata: \"result\":{\"tools\":[{\"name\":\"search\",\"description\":\"Search the web\",\"inputSchema\":{\"type\":\"object\"}}]}}\r\n\r\n", m.ID)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}`, m.ID)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

// sseServer is an MCP endpoint on the legacy HTTP+SSE transport that refuses
// a request without the declared header value. A GET on a path under /sse
// opens a stream whose first event names the endpoint, and each answer comes
// on that stream; a POST there, how a streamable HTTP session opens, is
// refused with 405, except under /sse-modern, which answers 404 with a
// JSON-RPC error as a current server does. /offsite names an endpoint on
// another origin.
func sseServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	sessions := map[string]chan string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "secret-hand-header-value" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && (strings.HasPrefix(r.URL.Path, "/sse") || r.URL.Path == "/offsite"):
			mu.Lock()
			id := strconv.Itoa(len(sessions) + 1)
			answers := make(chan string, 8)
			sessions[id] = answers
			mu.Unlock()
			endpoint := "/messages?session=" + id
			if r.URL.Path == "/offsite" {
				endpoint = "http://elsewhere.example" + endpoint
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, ": open\n\nevent: endpoint\ndata: %s\n\n", endpoint)
			w.(http.Flusher).Flush()
			for {
				select {
				case a := <-answers:
					fmt.Fprintf(w, "event: message\ndata: %s\n\n", a)
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
		case r.Method == http.MethodPost && r.URL.Path == "/sse-modern":
			// How a current server refuses a method: no reason to try SSE.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/messages":
			mu.Lock()
			answers, ok := sessions[r.URL.Query().Get("session")]
			mu.Unlock()
			var m struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if !ok || json.NewDecoder(r.Body).Decode(&m) != nil {
				http.Error(w, "no session", http.StatusNotFound)
				return
			}
			switch m.Method {
			case "initialize":
				answers <- fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"legacy","version":"0"}}}`, m.ID)
			case "tools/list":
				answers <- fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"recall","description":"Recall a note","inputSchema":{"type":"object"}}]}}`, m.ID)
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

// handshakeFixture declares the fixture server twice with the same command
// line and different environments, a server for each failure, a legacy sse
// server, a streamable HTTP declaration of a legacy server, one whose stream
// names an endpoint elsewhere and the web endpoint. Two servers take the whole budget: hang never
// answers, and linger leaves a child holding stdout open past the deadline.
func handshakeFixture(web, sse string) fixture {
	return fixture{
		dirs: []string{".claude", ".cursor"},
		files: map[string]string{
			".claude.json": fmt.Sprintf(`{"mcpServers": {
  "fix": {"command": %[1]q, "args": [], "env": {"SECRET_TOKEN": "secret-hand-env-value"}},
  "hang": {"command": %[1]q, "args": ["--hang"]},
  "exit": {"command": %[1]q, "args": ["--exit"]},
  "linger": {"command": %[1]q, "args": ["--linger"]},
  "missing": {"command": "$HOME/nowhere/mcpserver"},
  "legacy": {"type": "sse", "url": %[2]q, "headers": {"X-Key": "secret-hand-header-value"}},
  "fallback": {"type": "http", "url": %[3]q, "headers": {"X-Key": "secret-hand-header-value"}},
  "offsite": {"type": "sse", "url": %[4]q, "headers": {"X-Key": "secret-hand-header-value"}}}}`, mcpServer, sse+"/sse", sse+"/sse-fallback", sse+"/offsite"),
			".cursor/mcp.json": fmt.Sprintf(`{"mcpServers": {
  "fix": {"command": %q, "args": [], "env": {"MCPSERVER_VARIANT": "b", "SECRET_TOKEN": "secret-hand-env-value"}},
  "web": {"url": %q, "headers": {"X-Key": "secret-hand-header-value"}},
  "locked": {"url": %q}}}`, mcpServer, web, web+"/locked"),
		},
	}
}

// serverNodes groups the server nodes of a snapshot by name.
func serverNodes(snap jsonEvent) map[string][]map[string]any {
	nodes := map[string][]map[string]any{}
	for _, s := range snap["mcp_servers"].([]any) {
		node := s.(map[string]any)
		nodes[node["name"].(string)] = append(nodes[node["name"].(string)], node)
	}
	return nodes
}

// tools lists "kind name" for a node's tools, and their ids by name.
func tools(node map[string]any) (rows []string, ids map[string]string) {
	ids = map[string]string{}
	list, _ := node["tools"].([]any)
	for _, item := range list {
		tool := item.(map[string]any)
		rows = append(rows, tool["kind"].(string)+" "+tool["name"].(string))
		ids[tool["name"].(string)] = tool["id"].(string)
	}
	return rows, ids
}

func TestScanHandshake(t *testing.T) {
	t.Parallel()
	if mcpServerErr != nil {
		t.Skip(mcpServerErr)
	}
	h := newHarness(t)
	f := handshakeFixture(webServer(t).URL, sseServer(t).URL)
	h.build(t, f)
	h.env["AGENTX_HANDSHAKE_TIMEOUT"] = "1s"
	if err := os.WriteFile(filepath.Join(h.agentx, "handshakes.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	snap := h.snapshot(t, "--handshake")
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Errorf("scan took %s, want the hanging server cut off after 1s", elapsed)
	}
	nodes := serverNodes(snap)
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)

	// The same command line with different environments: one logical server,
	// two physical ones, telling the tool descriptions apart.
	fix := nodes["fix"]
	equal(t, "fix nodes", len(fix), 2)
	equal(t, "fix logical ids", fix[0]["logical_id"], fix[1]["logical_id"])
	if fix[0]["physical_id"] == fix[1]["physical_id"] || fix[0]["signature"] == fix[1]["signature"] {
		t.Errorf("the two variants share a physical identity: %v", fix)
	}
	var variantB map[string]any
	for _, node := range fix {
		if !hex64.MatchString(node["signature"].(string)) {
			t.Errorf("fix signature = %v, want 64 hex characters", node["signature"])
		}
		if _, err := time.Parse(time.RFC3339, node["signature_at"].(string)); err != nil {
			t.Errorf("fix signature_at = %v: %v", node["signature_at"], err)
		}
		rows, _ := tools(node)
		if want := []string{"prompt summarize", "resource notes", "tool add", "tool echo"}; !reflect.DeepEqual(rows, want) {
			t.Errorf("fix tools = %v, want %v", rows, want)
		}
		occ := node["occurrences"].([]any)
		equal(t, "fix occurrences", len(occ), 1)
		equal(t, "fix handshake", occ[0].(map[string]any)["handshake"], true)
		if occ[0].(map[string]any)["configuration"] == "cursor" {
			variantB = node
		}
	}
	_, idsA := tools(fix[0])
	_, idsB := tools(fix[1])
	equal(t, "add tool id", idsA["add"], idsB["add"])
	if idsA["echo"] == idsB["echo"] {
		t.Errorf("the echo tool has one id in both variants: %s", idsA["echo"])
	}

	web := nodes["web"][0]
	if rows, _ := tools(web); !reflect.DeepEqual(rows, []string{"tool search"}) {
		t.Errorf("web tools = %v", rows)
	}
	equal(t, "web handshake", web["occurrences"].([]any)[0].(map[string]any)["handshake"], true)

	// The legacy transport, declared as such or found behind a 405.
	for _, name := range []string{"legacy", "fallback"} {
		node := nodes[name][0]
		if rows, _ := tools(node); !reflect.DeepEqual(rows, []string{"tool recall"}) {
			t.Errorf("%s tools = %v", name, rows)
		}
		equal(t, name+" handshake", node["occurrences"].([]any)[0].(map[string]any)["handshake"], true)
	}

	for _, name := range []string{"hang", "linger", "exit", "missing", "offsite"} {
		node := nodes[name][0]
		equal(t, name+" signature", node["signature"], "none")
		equal(t, name+" handshake", node["occurrences"].([]any)[0].(map[string]any)["handshake"], false)
		for _, field := range []string{"tools", "signature_at"} {
			if _, ok := node[field]; ok {
				t.Errorf("%s carries %s: %v", name, field, node[field])
			}
		}
	}
	// Why each one failed, with a hint, for a consumer of the snapshot.
	for name, reason := range map[string]string{"hang": "timeout", "linger": "timeout", "exit": "failed", "missing": "not_found", "offsite": "failed", "locked": "unauthorized"} {
		node := nodes[name][0]
		equal(t, name+" signature", node["signature"], "none")
		failure, _ := node["occurrences"].([]any)[0].(map[string]any)["handshake_error"].(map[string]any)
		equal(t, name+" reason", failure["reason"], reason)
		for _, field := range []string{"message", "hint"} {
			if text, _ := failure[field].(string); text == "" {
				t.Errorf("%s handshake_error has no %s: %v", name, field, failure)
			}
		}
	}
	equal(t, "locked message", nodes["locked"][0]["occurrences"].([]any)[0].(map[string]any)["handshake_error"].(map[string]any)["message"], "initialize: HTTP 401 Unauthorized")
	if _, ok := web["occurrences"].([]any)[0].(map[string]any)["handshake_error"]; ok {
		t.Errorf("web carries a handshake_error: %v", web)
	}
	warnings := fmt.Sprint(snap["warnings"])
	for _, want := range []string{
		"~/.claude.json: server hang: timed out after 1s",
		"~/.claude.json: server linger: timed out after 1s",
		"~/.claude.json: server exit: ",
		"the server exited",
		"~/.claude.json: server missing: fork/exec ~/nowhere/mcpserver: no such file or directory",
		"~/.claude.json: server offsite: the endpoint event names another origin",
		"~/.cursor/mcp.json: server locked: initialize: HTTP 401 Unauthorized",
		"handshakes.json: not a handshakes file, ignored",
	} {
		contains(t, "warnings", h.portable(warnings), want)
	}
	equal(t, "warnings", len(snap["warnings"].([]any)), 7)

	// The handshakes file, corrupt before, now keeps the last signature per
	// logical server and nothing for a server that failed; it is derived
	// state, so no version bump.
	stored := map[string]map[string]any{}
	b, err := os.ReadFile(filepath.Join(h.agentx, "handshakes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &stored); err != nil {
		t.Fatal(err)
	}
	equal(t, "stored servers", len(stored), 4)
	entry := stored[variantB["logical_id"].(string)]
	equal(t, "stored signature", entry["signature"], variantB["signature"])
	equal(t, "stored tools", len(entry["tools"].([]any)), 4)
	if _, err := time.Parse(time.RFC3339, entry["at"].(string)); err != nil {
		t.Errorf("stored at = %v: %v", entry["at"], err)
	}
	equal(t, "web stored", stored[web["logical_id"].(string)]["signature"], web["signature"])
	if _, err := os.Stat(filepath.Join(h.agentx, "version")); !os.IsNotExist(err) {
		t.Errorf("the handshake bumped the version file: %v", err)
	}

	// A plain scan reuses the stored signature: the two declarations now
	// merge into one node, and no occurrence claims a handshake.
	after := h.snapshot(t)
	equal(t, "warnings after", len(after["warnings"].([]any)), 0)
	plain := serverNodes(after)
	equal(t, "fix nodes after", len(plain["fix"]), 1)
	equal(t, "fix signature after", plain["fix"][0]["signature"], variantB["signature"])
	equal(t, "fix signature_at after", plain["fix"][0]["signature_at"], variantB["signature_at"])
	equal(t, "fix occurrences after", len(plain["fix"][0]["occurrences"].([]any)), 2)
	for _, o := range plain["fix"][0]["occurrences"].([]any) {
		equal(t, "fix handshake after", o.(map[string]any)["handshake"], false)
	}
	if rows, _ := tools(plain["fix"][0]); len(rows) != 4 {
		t.Errorf("fix tools after = %v", rows)
	}
	equal(t, "web signature after", plain["web"][0]["signature"], web["signature"])
	equal(t, "hang signature after", plain["hang"][0]["signature"], "none")

	// Neither output mode prints a value from the declarations, and the
	// fixture server echoes its whole environment to stderr.
	equal(t, "secrets in fixture", len(secrets(f)), 6)
	for _, args := range [][]string{{"scan", "--handshake"}, {"--json", "scan", "--handshake"}} {
		out := h.run(args...)
		equal(t, "exit", out.exit, 0)
		for _, secret := range secrets(f) {
			if strings.Contains(out.stdout, secret) || strings.Contains(out.stderr, secret) {
				t.Errorf("%v printed %q", args, secret)
			}
		}
		if args[0] == "scan" {
			contains(t, "stdout", out.stdout, "4 tools")
			contains(t, "stdout", out.stdout, "1 tool\n")
			contains(t, "stderr", h.portable(out.stderr), "warning: ~/.claude.json: server hang: timed out after 1s")
			contains(t, "stdout", out.stdout, "needs sign-in\n")
			contains(t, "stdout", out.stdout, "timed out\n")
			contains(t, "stderr", out.stderr, "hint: needs sign-in: the server wants a sign-in")
			equal(t, "timeout hints", strings.Count(out.stderr, "hint: timed out: "), 1)
		}
	}

	h.env["AGENTX_HANDSHAKE_TIMEOUT"] = "soon"
	out := h.run("--json", "scan", "--handshake")
	equal(t, "exit", out.exit, 1)
	equal(t, "error.code", h.events(out.stdout)[0]["code"], "usage")
}

// A declaration is handshaken as its client would run it: in its working
// directory, with the bearer token Codex takes from the environment, with
// its placeholders expanded in the client's syntax, and not at all when the
// client has it turned off.
func TestScanHandshakeDeclarations(t *testing.T) {
	t.Parallel()
	if mcpServerErr != nil {
		t.Skip(mcpServerErr)
	}
	h := newHarness(t)
	web := webServer(t).URL
	sse := sseServer(t).URL
	h.build(t, fixture{
		dirs: []string{".claude", ".codex", ".cursor", ".gemini", ".codeium/windsurf", ".copilot"},
		files: map[string]string{
			// Claude Code: a variable, a default, a credential variable a
			// remote header never receives, and a local command.
			".claude.json": fmt.Sprintf(`{"mcpServers": {
  "claude-key": {"type": "http", "url": "${AGENTX_TEST_WEB}", "headers": {"X-Key": "${AGENTX_TEST_KEY}"}},
  "claude-default": {"type": "http", "url": %[1]q, "headers": {"X-Key": "${AGENTX_TEST_NONE:-secret-hand-header-value}"}},
  "claude-credential": {"type": "http", "url": %[2]q, "headers": {"X-Key": "${ANTHROPIC_API_KEY}"}},
  "claude-local": {"command": "${AGENTX_TEST_BIN}", "args": ["--variant", "${AGENTX_TEST_NONE:-b}"]},
  "modern": {"type": "http", "url": %[3]q, "headers": {"X-Key": "secret-hand-header-value"}}}}`, web+"/default", web+"/credential", sse+"/sse-modern"),
			".cursor/mcp.json": fmt.Sprintf(`{"mcpServers": {
  "cursor-key": {"url": %q, "headers": {"X-Key": "${env:AGENTX_TEST_KEY}"}},
  "cursor-local": {"command": "${userHome}${/}bin${/}mcpserver"}}}`, web+"/cursor"),
			// Gemini CLI: $VAR, and servers turned off by settings or by
			// `gemini mcp disable`, which records the lowercased name.
			".gemini/settings.json": fmt.Sprintf(`{"mcp": {"excluded": ["gemini-excluded"]}, "mcpServers": {
  "gemini-key": {"httpUrl": %q, "headers": {"X-Key": "$AGENTX_TEST_KEY"}},
  "gemini-excluded": {"command": %[2]q, "args": ["--exit"]},
  "Gemini-Toggled": {"command": %[2]q, "args": ["--linger"]}}}`, web+"/gemini", mcpServer),
			".gemini/mcp-server-enablement.json": `{"gemini-toggled": {"enabled": false}}`,
			// Windsurf: ${env:NAME} and ${file:/path}.
			".codeium/windsurf/mcp_config.json": fmt.Sprintf(`{"mcpServers": {
  "windsurf-env": {"serverUrl": %q, "headers": {"X-Key": "${env:AGENTX_TEST_KEY}"}},
  "windsurf-file": {"serverUrl": %q, "headers": {"X-Key": "${file:$HOME/key.txt}"}}}}`, web+"/windsurf-env", web+"/windsurf-file"),
			"key.txt": "secret-hand-header-value\n",
			// Copilot CLI: ${VAR}, ~ in cwd, and /mcp disable.
			".copilot/mcp-config.json": fmt.Sprintf(`{"mcpServers": {
  "copilot-key": {"type": "http", "url": %q, "headers": {"X-Key": "${AGENTX_TEST_KEY}"}},
  "copilot-home": {"command": "./mcpserver", "args": ["--variant", "b"], "cwd": "~/bin"},
  "copilot-off": {"command": %q, "args": ["--hang", "--exit"]}}}`, web+"/copilot", mcpServer),
			".copilot/settings.json": `{"disabledMcpServers": ["copilot-off"]}`,
			".codex/config.toml": fmt.Sprintf(`[mcp_servers.bearer]
url = %[1]q
bearer_token_env_var = "AGENTX_TEST_BEARER"

[mcp_servers.unset]
url = %[1]q
bearer_token_env_var = "AGENTX_TEST_UNSET"

[mcp_servers.there]
command = "./mcpserver"
cwd = "$HOME/bin"

[mcp_servers.off]
command = %[2]q
args = ["--variant", "b"]
enabled = false

[plugins."quiet@team"]
enabled = false
`, web, mcpServer),
			".codex/plugins/cache/team/quiet/1.0.0/.mcp.json": fmt.Sprintf(`{"mcpServers": {"quiet": {"command": %q, "args": ["--hang"]}}}`, mcpServer),
			// A relative cwd resolves against the plugin; an Agent Plugins
			// server without one runs in the plugin.
			".codex/plugins/cache/team/tools/1.0.0/.mcp.json":   `{"mcpServers": {"here": {"command": "./mcpserver", "args": ["--variant", "a"], "cwd": "bin"}}}`,
			".codex/plugins/cache/team/agent/1.0.0/plugin.json": `{"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", "name": "agent", "version": "1.0.0"}`,
			".codex/plugins/cache/team/agent/1.0.0/mcp.json":    `{"mcpServers": {"inside": {"command": "./bin/mcpserver"}}}`,
		},
	})
	for _, dir := range []string{"bin", ".codex/plugins/cache/team/tools/1.0.0/bin", ".codex/plugins/cache/team/agent/1.0.0/bin"} {
		if err := os.MkdirAll(filepath.Join(h.home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(mcpServer, filepath.Join(h.home, dir, "mcpserver")); err != nil {
			t.Fatal(err)
		}
	}
	h.env["AGENTX_TEST_BEARER"] = "secret-hand-bearer-value"
	h.env["AGENTX_TEST_WEB"] = web
	h.env["AGENTX_TEST_KEY"] = "secret-hand-header-value"
	h.env["ANTHROPIC_API_KEY"] = "secret-hand-header-value"
	h.env["AGENTX_TEST_BIN"] = mcpServer

	snap := h.snapshot(t, "--handshake")
	nodes := serverNodes(snap)
	occurrence := func(name string) map[string]any {
		t.Helper()
		equal(t, name+" nodes", len(nodes[name]), 1)
		return nodes[name][0]["occurrences"].([]any)[0].(map[string]any)
	}
	for name, want := range map[string]string{
		"bearer": "tool search", "there": "tool add", "here": "tool add", "inside": "tool add",
		"claude-key": "tool search", "claude-default": "tool search", "claude-local": "tool add",
		"cursor-key": "tool search", "cursor-local": "tool add",
		"gemini-key": "tool search", "windsurf-env": "tool search", "windsurf-file": "tool search",
		"copilot-key": "tool search", "copilot-home": "tool add",
	} {
		equal(t, name+" handshake", occurrence(name)["handshake"], true)
		if rows, _ := tools(nodes[name][0]); len(rows) == 0 || !slices.Contains(rows, want) {
			t.Errorf("%s tools = %v, want %s among them", name, rows, want)
		}
	}
	equal(t, "bearer header keys", fmt.Sprint(occurrence("bearer")["header_keys"]), "[Authorization]")
	// The snapshot records the declaration as written, never an expansion.
	equal(t, "claude-key url", occurrence("claude-key")["url"], "${AGENTX_TEST_WEB}")
	equal(t, "claude-local command", occurrence("claude-local")["command"], "${AGENTX_TEST_BIN}")
	credential, _ := occurrence("claude-credential")["handshake_error"].(map[string]any)
	equal(t, "claude-credential reason", credential["reason"], "unauthorized")
	modern, _ := occurrence("modern")["handshake_error"].(map[string]any)
	equal(t, "modern reason", modern["reason"], "failed")
	equal(t, "modern message", modern["message"], "initialize: HTTP 404 Not Found: server error -32601: Method not found")

	failure, _ := occurrence("unset")["handshake_error"].(map[string]any)
	equal(t, "unset reason", failure["reason"], "unauthorized")
	contains(t, "unset hint", fmt.Sprint(failure["hint"]), "set AGENTX_TEST_UNSET in the environment agentx runs in")

	// Turned off, by its own declaration or with its plugin: never started,
	// no warning, no failure.
	for _, name := range []string{"off", "quiet", "gemini-excluded", "Gemini-Toggled", "copilot-off"} {
		occ := occurrence(name)
		equal(t, name+" enabled", occ["enabled"], false)
		equal(t, name+" handshake", occ["handshake"], false)
		if _, ok := occ["handshake_error"]; ok {
			t.Errorf("%s carries a handshake_error: %v", name, occ)
		}
	}
	warnings := fmt.Sprint(snap["warnings"])
	equal(t, "warnings", len(snap["warnings"].([]any)), 3)
	contains(t, "warnings", warnings, "server unset: initialize: HTTP 401 Unauthorized")
	contains(t, "warnings", warnings, "server claude-credential: initialize: HTTP 401 Unauthorized")

	// --verbose names what was withheld, never its value.
	out := h.run("--verbose", "scan", "--handshake")
	contains(t, "stderr", h.portable(out.stderr), "debug: ~/.claude.json: server claude-credential: ANTHROPIC_API_KEY left empty in the URL and headers")
	for _, secret := range []string{"secret-hand-header-value", "secret-hand-bearer-value"} {
		if strings.Contains(out.stdout+out.stderr, secret) {
			t.Errorf("--verbose printed %q", secret)
		}
	}
}
