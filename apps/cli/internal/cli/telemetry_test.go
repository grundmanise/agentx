package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/posthog/posthog-go/v2"
)

// collector is a fake PostHog capture endpoint: it keeps every body posted.
type collector struct {
	mu     sync.Mutex
	bodies [][]byte
}

func newCollector(t *testing.T) (*collector, string) {
	t.Helper()
	c := &collector{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/i/v1/analytics/events" {
			t.Errorf("request %s %s, want POST /i/v1/analytics/events", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+posthogToken {
			t.Errorf("Authorization = %q", got)
		}
		c.mu.Lock()
		c.bodies = append(c.bodies, b)
		c.mu.Unlock()
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	return c, srv.URL
}

// entries are the events of every body received, in order.
func (c *collector) entries(t *testing.T) []map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var all []map[string]any
	for _, b := range c.bodies {
		var body struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatalf("body is not JSON: %v\n%s", err, b)
		}
		all = append(all, body.Batch...)
	}
	return all
}

func (c *collector) raw() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(bytes.Join(c.bodies, []byte("\n")))
}

// telemetryHarness is a harness whose telemetry goes to a collector.
func telemetryHarness(t *testing.T) (*harness, *collector) {
	h := newHarness(t)
	c, url := newCollector(t)
	h.env["AGENTX_TELEMETRY_ENDPOINT"] = url
	return h, c
}

func (h *harness) runWith(stdin string, args ...string) outcome {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), args, h.env, strings.NewReader(stdin), &stdout, &stderr)
	return outcome{exit: exit, stdout: stdout.String(), stderr: stderr.String()}
}

// TestTelemetry is the whole flow: no question without a terminal or with
// --json, the answer at the terminal, one command event per run sent under
// the anonymous id and nothing of the machine in it.
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

	h.env["AGENTX_INTERACTIVE"] = "1"
	out = h.runWith("y\n", "--json", "scan")
	if got, want := h.types(h.events(out.stdout)), []string{"inventory", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "telemetry", h.run("config", "get", "telemetry").stdout, "unset\n")
	if c.raw() != "" {
		t.Fatalf("sent before consent: %q", c.raw())
	}

	out = h.runWith("y\n", "scan")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "Send anonymous usage data to PostHog?")
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
	id := anonymousID(machine["id"].(string))
	equal(t, "anonymous_id", settings["anonymous_id"], id)
	if id == machine["id"] || len(id) != 32 {
		t.Errorf("anonymous id %q is the machine id or not 32 characters", id)
	}

	var commands []string
	for _, e := range c.entries(t) {
		equal(t, "event", e["event"], "command_run")
		equal(t, "distinct_id", e["distinct_id"], id)
		equal(t, "process_person_profile", e["options"].(map[string]any)["process_person_profile"], false)
		props := e["properties"].(map[string]any)
		commands = append(commands, props["command"].(string))
		equal(t, "release_channel", props["release_channel"], "stable")
		for _, key := range []string{"exit_code", "duration_ms", "cli_version", "arch", "$os"} {
			if _, ok := props[key]; !ok {
				t.Errorf("properties lack %s: %v", key, props)
			}
		}
		if _, ok := props["json"]; ok {
			t.Errorf("properties carry json: %v", props)
		}
	}
	if want := []string{"scan", "scan", "config set", "machine"}; !reflect.DeepEqual(commands, want) {
		t.Errorf("commands sent = %v, want %v", commands, want)
	}
	for _, private := range []string{h.home, h.agentx, h.library, "test-host", "platform-test"} {
		if strings.Contains(c.raw(), private) {
			t.Errorf("a body carries %q:\n%s", private, c.raw())
		}
	}

	h.mustRun("machine", "reset-id")
	machine = h.events(h.run("--json", "machine").stdout)[0]
	if next := anonymousID(machine["id"].(string)); next == id {
		t.Error("the anonymous id did not change with the machine id")
	}

	sent := len(c.entries(t))
	h.mustRun("config", "set", "telemetry", "off")
	h.mustRun("config", "list")
	if got := len(c.entries(t)); got != sent {
		t.Errorf("sent %d events after telemetry off", got-sent)
	}
}

// TestTelemetryNeverAsked: DO_NOT_TRACK and a source build neither ask nor
// send, even with the setting on.
func TestTelemetryNeverAsked(t *testing.T) {
	t.Parallel()
	for name, env := range map[string]map[string]string{
		"DO_NOT_TRACK": {"DO_NOT_TRACK": "1"},
		"source build": {"AGENTX_TELEMETRY_ENDPOINT": ""},
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
			settings := h.events(h.run("--json", "config", "set", "telemetry", "on").stdout)[0]
			if _, ok := settings["anonymous_id"]; ok && name == "DO_NOT_TRACK" {
				t.Errorf("the settings event carries an anonymous id under DO_NOT_TRACK: %v", settings)
			}
			h.mustRun("config", "list")
			if c.raw() != "" {
				t.Errorf("sent %q", c.raw())
			}
		})
	}
}

// TestTelemetryServe: with the setting on, serve sends its start through
// the client it keeps open, and its command event once it ends.
func TestTelemetryServe(t *testing.T) {
	t.Parallel()
	h, c := telemetryHarness(t)
	h.mustRun("config", "set", "telemetry", "on")
	p := h.serve(t, "--json")
	p.next("inventory")
	p.close()
	var events []string
	for _, e := range c.entries(t) {
		events = append(events, e["event"].(string))
		props := e["properties"].(map[string]any)
		if e["event"] == "serve_start" {
			equal(t, "command", props["command"], "serve")
			if _, ok := props["exit_code"]; ok {
				t.Errorf("serve_start carries an exit code: %v", e)
			}
		}
	}
	if want := []string{"command_run", "serve_start", "command_run"}; !reflect.DeepEqual(events, want) {
		t.Errorf("events = %v, want %v", events, want)
	}
}

// TestRedactPaths: a path under a home or a temporary directory becomes
// <path>; a module path or a relative path stays.
func TestRedactPaths(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"open /Users/me/.agentx/settings.json: denied": "open <path>: denied",
		"/home/me/x and /tmp/y":                        "<path> and <path>",
		`"/root/.agentx"`:                              `"<path>"`,
		"/private/var/folders/x/y":                     "<path>",
		"github.com/grundmanise/agentx/apps/cli/internal/home/dirs.go": "github.com/grundmanise/agentx/apps/cli/internal/home/dirs.go",
		"runtime/panic.go": "runtime/panic.go",
		"~/skills/x":       "~/skills/x",
	} {
		equal(t, in, redactPaths(in), want)
	}
}

// TestRedactMessage: BeforeSend redacts every string an event sends: nested
// properties, an exception's values, frames and binary path.
func TestRedactMessage(t *testing.T) {
	t.Parallel()
	got := redactMessage(posthog.Capture{Properties: posthog.Properties{
		"error": "/home/me/x",
		"list":  []any{"/tmp/y", map[string]any{"z": "/Users/me/z"}},
	}}).(posthog.Capture)
	if b, _ := json.Marshal(got.Properties); strings.Contains(string(b), "me/") || strings.Contains(string(b), "/tmp") {
		t.Errorf("capture properties = %s", b)
	}

	ex := posthog.NewDefaultException(time.Now(), "id", "panic", "open /home/me/.agentx: denied")
	ex.ExceptionList[0].Stacktrace.Frames[0].Filename = "/home/runner/work/agentx/main.go"
	ex.DebugImages = []posthog.DebugImage{{CodeFile: "/Users/me/bin/agentx"}}
	b, _ := json.Marshal(redactMessage(ex))
	for _, private := range []string{"/home/", "/Users/"} {
		if strings.Contains(string(b), private) {
			t.Errorf("exception carries %s: %s", private, b)
		}
	}
	if !slices.ContainsFunc(redactMessage(ex).(posthog.Exception).ExceptionList, func(i posthog.ExceptionItem) bool {
		return i.Value == "open <path>: denied"
	}) {
		t.Errorf("exception value not redacted: %s", b)
	}
}
