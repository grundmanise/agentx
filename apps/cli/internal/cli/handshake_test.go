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
	"strings"
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
		if r.Header.Get("X-Key") != "secret-hand-header-value" {
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

// handshakeFixture declares the fixture server twice with the same command
// line and different environments, a server for each failure, a legacy sse
// server and the web endpoint. Two servers take the whole budget: hang never
// answers, and linger leaves a child holding stdout open past the deadline.
func handshakeFixture(web string) fixture {
	return fixture{
		dirs: []string{".claude", ".cursor"},
		files: map[string]string{
			".claude.json": fmt.Sprintf(`{"mcpServers": {
  "fix": {"command": %[1]q, "args": [], "env": {"SECRET_TOKEN": "secret-hand-env-value"}},
  "hang": {"command": %[1]q, "args": ["--hang"]},
  "exit": {"command": %[1]q, "args": ["--exit"]},
  "linger": {"command": %[1]q, "args": ["--linger"]},
  "missing": {"command": "$HOME/nowhere/mcpserver"},
  "legacy": {"type": "sse", "url": "https://legacy.example.com/sse"}}}`, mcpServer),
			".cursor/mcp.json": fmt.Sprintf(`{"mcpServers": {
  "fix": {"command": %q, "args": [], "env": {"MCPSERVER_VARIANT": "b", "SECRET_TOKEN": "secret-hand-env-value"}},
  "web": {"url": %q, "headers": {"X-Key": "secret-hand-header-value"}}}}`, mcpServer, web),
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
	f := handshakeFixture(webServer(t).URL)
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

	for _, name := range []string{"hang", "linger", "exit", "missing", "legacy"} {
		node := nodes[name][0]
		equal(t, name+" signature", node["signature"], "none")
		equal(t, name+" handshake", node["occurrences"].([]any)[0].(map[string]any)["handshake"], false)
		for _, field := range []string{"tools", "signature_at"} {
			if _, ok := node[field]; ok {
				t.Errorf("%s carries %s: %v", name, field, node[field])
			}
		}
	}
	warnings := fmt.Sprint(snap["warnings"])
	for _, want := range []string{
		"~/.claude.json: server hang: timed out after 1s",
		"~/.claude.json: server linger: timed out after 1s",
		"~/.claude.json: server exit: ",
		"the server exited",
		"~/.claude.json: server missing: fork/exec ~/nowhere/mcpserver: no such file or directory",
		"~/.claude.json: server legacy: legacy sse transport is not handshaken",
		"handshakes.json: not a handshakes file, ignored",
	} {
		contains(t, "warnings", h.portable(warnings), want)
	}
	equal(t, "warnings", len(snap["warnings"].([]any)), 6)

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
	equal(t, "stored servers", len(stored), 2)
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
	equal(t, "secrets in fixture", len(secrets(f)), 3)
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
		}
	}

	h.env["AGENTX_HANDSHAKE_TIMEOUT"] = "soon"
	out := h.run("--json", "scan", "--handshake")
	equal(t, "exit", out.exit, 1)
	equal(t, "error.code", h.events(out.stdout)[0]["code"], "usage")
}
