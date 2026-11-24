package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector is a fake telemetry endpoint: it keeps every body posted to
// /batch/ and answers with status.
type collector struct {
	mu     sync.Mutex
	bodies [][]byte
	status int
}

func newCollector(t *testing.T) (*collector, string) {
	t.Helper()
	c := &collector{status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		defer c.mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/batch/" {
			t.Errorf("request %s %s, want POST /batch/", r.Method, r.URL.Path)
		}
		c.bodies = append(c.bodies, b)
		w.WriteHeader(c.status)
	}))
	t.Cleanup(srv.Close)
	return c, srv.URL
}

func (c *collector) answer(status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = status
}

// entries are the batch entries of every body received, in order.
func (c *collector) entries(t *testing.T) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var all []map[string]any
	for _, b := range c.bodies {
		var body struct {
			APIKey string           `json:"api_key"`
			Batch  []map[string]any `json:"batch"`
		}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatalf("body is not JSON: %v\n%s", err, b)
		}
		equal(t, "api_key", body.APIKey, "none")
		all = append(all, body.Batch...)
	}
	return all
}

func (c *collector) raw() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(bytes.Join(c.bodies, []byte("\n")))
}

// telemetryHarness is a harness whose telemetry goes to a collector and is
// flushed before each command returns.
func telemetryHarness(t *testing.T) (*harness, *collector) {
	h := newHarness(t)
	c, url := newCollector(t)
	h.env["AGENTX_TELEMETRY_ENDPOINT"] = url
	h.env["AGENTX_TELEMETRY_FLUSH"] = "inline"
	return h, c
}

func (h *harness) runWith(stdin string, args ...string) outcome {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), args, h.env, strings.NewReader(stdin), &stdout, &stderr)
	return outcome{exit: exit, stdout: stdout.String(), stderr: stderr.String()}
}

func spoolFiles(t *testing.T, h *harness) []string {
	t.Helper()
	return spooled(spoolDir(h.agentx))
}

// TestTelemetry is the whole flow: no answer without a terminal, the
// consent event with --json, the answer at the terminal, one command event
// per run sent under the telemetry id and nothing of the machine in it.
func TestTelemetry(t *testing.T) {
	t.Parallel()
	h, c := telemetryHarness(t)
	h.env["AGENTX_INTERACTIVE"] = "0"

	out := h.run("scan")
	equal(t, "exit", out.exit, 0)
	if strings.Contains(out.stdout, "Send anonymous usage data") {
		t.Errorf("a run with no terminal asked:\n%s", out.stdout)
	}
	equal(t, "telemetry", h.run("config", "get", "telemetry").stdout, "unset\n")

	out = h.run("--json", "scan")
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"inventory", "consent", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "topic", events[1]["topic"], "telemetry")
	equal(t, "state", events[1]["state"], "unset")
	for _, line := range strings.Split(out.stdout, "\n") {
		if strings.Contains(line, "telemetry") && !strings.Contains(line, `"type":"consent"`) {
			t.Errorf("stdout mentions telemetry outside the consent event: %s", line)
		}
	}
	if n := len(spoolFiles(t, h)); n != 0 || c.raw() != "" {
		t.Fatalf("recorded before consent: %d spooled, sent %q", n, c.raw())
	}

	h.env["AGENTX_INTERACTIVE"] = "1"
	out = h.runWith("y\n", "scan")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "Send anonymous usage data? [y/N] ")
	contains(t, "stdout", out.stdout, "telemetry is now on")
	out = h.runWith("y\n", "scan")
	if strings.Contains(out.stdout, "Send anonymous usage data") {
		t.Errorf("a second scan asked again:\n%s", out.stdout)
	}

	out = h.run("--json", "config", "set", "telemetry", "on")
	equal(t, "exit", out.exit, 0)
	settings := h.events(out.stdout)[0]
	machine := h.events(h.run("--json", "machine").stdout)[0]
	id := telemetryID(machine["id"].(string))
	equal(t, "telemetry_id", settings["telemetry_id"], id)
	if id == machine["id"] || len(id) != 32 {
		t.Errorf("telemetry id %q is the machine id or not 32 characters", id)
	}

	entries := c.entries(t)
	var commands []string
	for _, e := range entries {
		equal(t, "event", e["event"], "command_run")
		equal(t, "distinct_id", e["distinct_id"], id)
		props := e["properties"].(map[string]any)
		commands = append(commands, props["command"].(string))
		var keys []string
		for k := range props {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		want := []string{"$lib", "$lib_version", "$process_person_profile", "arch", "cli_version", "command", "duration_ms", "exit_code", "json", "os", "schema_version"}
		if !reflect.DeepEqual(keys, want) {
			t.Errorf("properties = %v, want %v", keys, want)
		}
		equal(t, "$process_person_profile", props["$process_person_profile"], false)
		equal(t, "$lib", props["$lib"], "agentx-cli")
		if ts, err := time.Parse(time.RFC3339, e["timestamp"].(string)); err != nil || ts.Second() != 0 {
			t.Errorf("timestamp %v is not RFC 3339 to the minute", e["timestamp"])
		}
	}
	if want := []string{"scan", "scan", "config set", "machine"}; !reflect.DeepEqual(commands, want) {
		t.Errorf("commands sent = %v, want %v", commands, want)
	}
	if n := len(spoolFiles(t, h)); n != 0 {
		t.Errorf("%d records left after a 200", n)
	}
	for _, private := range []string{h.home, h.agentx, h.library, "test-host", "platform-test"} {
		if strings.Contains(c.raw(), private) {
			t.Errorf("a body carries %q:\n%s", private, c.raw())
		}
	}

	h.mustRun("machine", "reset-id")
	machine = h.events(h.run("--json", "machine").stdout)[0]
	if next := telemetryID(machine["id"].(string)); next == id {
		t.Error("the telemetry id did not change with the machine id")
	}
}

// TestTelemetryFlushAnswers: a 5xx keeps the records for the next flush, a
// 4xx drops them, and turning telemetry off drops the spool and sends
// nothing more.
func TestTelemetryFlushAnswers(t *testing.T) {
	t.Parallel()
	h, c := telemetryHarness(t)
	h.mustRun("config", "set", "telemetry", "on")
	c.answer(http.StatusServiceUnavailable)
	h.mustRun("config", "list")
	h.mustRun("config", "list")
	if n := len(spoolFiles(t, h)); n != 2 {
		t.Fatalf("%d records spooled after two 503s, want 2", n)
	}
	c.answer(http.StatusBadRequest)
	h.mustRun("config", "list")
	if n := len(spoolFiles(t, h)); n != 0 {
		t.Errorf("%d records left after a 400", n)
	}
	if got := len(c.entries(t)); got != 1+1+2+3 {
		t.Errorf("entries sent = %d, want 7", got)
	}
	c.answer(http.StatusServiceUnavailable)
	h.mustRun("config", "list")
	h.mustRun("config", "set", "telemetry", "off")
	if _, err := os.Stat(spoolDir(h.agentx)); !os.IsNotExist(err) {
		t.Errorf("the spool is still there after telemetry off: %v", err)
	}
	sent := len(c.entries(t))
	h.mustRun("config", "list")
	if got := len(c.entries(t)); got != sent {
		t.Errorf("sent %d entries after telemetry off", got-sent)
	}
}

// TestTelemetryNeverAsked: DO_NOT_TRACK and a build with no host neither
// ask, emit the consent event nor record, even with the setting on.
func TestTelemetryNeverAsked(t *testing.T) {
	t.Parallel()
	for name, env := range map[string]map[string]string{
		"DO_NOT_TRACK": {"DO_NOT_TRACK": "1"},
		"no host":      {"AGENTX_TELEMETRY_ENDPOINT": ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, c := telemetryHarness(t)
			h.env["AGENTX_INTERACTIVE"] = "1"
			for k, v := range env {
				h.env[k] = v
			}
			out := h.runWith("y\n", "scan")
			if strings.Contains(out.stdout, "Send anonymous usage data") {
				t.Errorf("asked:\n%s", out.stdout)
			}
			if got := h.types(h.events(h.run("--json", "scan").stdout)); slices.Contains(got, "consent") {
				t.Errorf("event types = %v, with a consent event", got)
			}
			settings := h.events(h.run("--json", "config", "set", "telemetry", "on").stdout)[0]
			if _, ok := settings["telemetry_id"]; ok && name == "DO_NOT_TRACK" {
				t.Errorf("the settings event carries a telemetry id under DO_NOT_TRACK: %v", settings)
			}
			h.mustRun("config", "list")
			if n := len(spoolFiles(t, h)); n != 0 || c.raw() != "" {
				t.Errorf("recorded: %d spooled, sent %q", n, c.raw())
			}
		})
	}
}

// TestTelemetryServe: serve emits the consent event once after its first
// inventory while the setting is unset, and with the setting on records its
// start and flushes it at once.
func TestTelemetryServe(t *testing.T) {
	t.Parallel()
	h, c := telemetryHarness(t)
	p := h.serve(t, "--json")
	p.next("inventory")
	p.next("consent")
	h.mustRun("config", "set", "telemetry", "on")
	equal(t, "exit", p.close(), 0)

	p = h.serve(t, "--json", "--once")
	p.next("inventory")
	p.next("result")
	p.wait()

	p = h.serve(t, "--json")
	p.next("inventory")
	deadline := time.Now().Add(serveDeadline)
	for !slices.ContainsFunc(c.entries(t), func(e map[string]any) bool { return e["event"] == "serve_start" }) {
		if time.Now().After(deadline) {
			t.Fatalf("no serve_start sent within %s: %s", serveDeadline, c.raw())
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.close()
	for _, e := range c.entries(t) {
		if e["event"] != "serve_start" {
			continue
		}
		if _, ok := e["properties"].(map[string]any)["exit_code"]; ok {
			t.Errorf("serve_start carries an exit code: %v", e)
		}
	}
}

// TestTrimSpool: records past the age bound, or with a name that tells no
// time, go first, then the oldest beyond the cap.
func TestTrimSpool(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now()
	write := func(at time.Time, n int) {
		for i := range n {
			name := fmt.Sprintf("%020d-%04d.json", at.UnixNano()+int64(i), i)
			if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(now.Add(-8*24*time.Hour), 3)
	write(now.Add(-time.Hour), spoolCap+2)
	if err := os.WriteFile(filepath.Join(dir, "stray.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	kept := trimSpool(dir, now)
	equal(t, "kept", len(kept), spoolCap)
	equal(t, "on disk", len(spooled(dir)), spoolCap)
	equal(t, "oldest kept", kept[0], fmt.Sprintf("%020d-%04d.json", now.Add(-time.Hour).UnixNano()+2, 2))
}
