package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestAPanicEndsTheRunAsAnInternalError makes scan panic: the run exits
// 10, with the panic's own message on stderr, or in an error event with
// code internal under --json. Without the user's yes, from an unset or off
// setting, DO_NOT_TRACK or a build with no host, nothing is recorded.
func TestAPanicEndsTheRunAsAnInternalError(t *testing.T) {
	t.Parallel()
	h, c := telemetryHarness(t)
	h.env["AGENTX_TEST_PANIC"] = "1"

	out := h.run("scan")
	equal(t, "exit", out.exit, 10)
	contains(t, "stderr", out.stderr, "error: panic: test panic: open "+h.agentx+"/settings.json and "+h.home+"/project")

	out = h.run("scan", "--json")
	equal(t, "exit", out.exit, 10)
	events := h.events(out.stdout)
	if len(events) != 2 {
		t.Fatalf("events = %v, want an error and the result", events)
	}
	equal(t, "code", events[0]["code"], "internal")
	contains(t, "message", events[0]["message"].(string), "panic: test panic: open "+h.agentx)
	equal(t, "result ok", events[1]["ok"], false)

	h.mustRun("config", "set", "telemetry", "off")
	equal(t, "exit", h.run("scan").exit, 10)
	h.mustRun("config", "set", "telemetry", "on")
	for k, v := range map[string]string{"DO_NOT_TRACK": "1", "AGENTX_TELEMETRY_ENDPOINT": ""} {
		saved := h.env[k]
		h.env[k] = v
		equal(t, "exit with "+k+"="+v, h.run("scan").exit, 10)
		h.env[k] = saved
	}
	// config set telemetry on is the one run recorded, and sent.
	if n := len(spoolFiles(t, h)); n != 0 || strings.Contains(c.raw(), "exception") {
		t.Errorf("a crash was recorded without consent: %d spooled, sent %q", n, c.raw())
	}
}

// TestACrashIsReported makes scan panic with telemetry on: one exception
// record is spooled beside the command's, and sent as an $exception event
// that names no path of the machine.
func TestACrashIsReported(t *testing.T) {
	t.Parallel()
	h, c := telemetryHarness(t)
	root := filepath.Dir(h.home)
	h.env["TMPDIR"] = root
	h.mustRun("config", "set", "telemetry", "on")
	c.answer(http.StatusServiceUnavailable) // keeps the spool to read
	h.env["AGENTX_TEST_PANIC"] = "1"
	equal(t, "exit", h.run("scan").exit, 10)
	delete(h.env, "AGENTX_TEST_PANIC")

	var exceptions []map[string]any
	for _, name := range spoolFiles(t, h) {
		b, err := os.ReadFile(filepath.Join(spoolDir(h.agentx), name))
		if err != nil {
			t.Fatal(err)
		}
		var rec map[string]any
		if err := json.Unmarshal(b, &rec); err != nil {
			t.Fatal(err)
		}
		if rec["type"] == recordException {
			exceptions = append(exceptions, rec)
		}
	}
	if len(exceptions) != 1 {
		t.Fatalf("%d exception records spooled, want 1", len(exceptions))
	}
	var keys []string
	for k := range exceptions[0] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{"arch", "at", "cli_version", "command", "exception_type", "exception_value", "frames", "os", "schema_version", "telemetry_id", "type"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("record fields = %v, want %v", keys, want)
	}
	equal(t, "command", exceptions[0]["command"], "scan")

	c.answer(http.StatusOK)
	h.mustRun("config", "list")
	if n := len(spoolFiles(t, h)); n != 0 {
		t.Errorf("%d records left after a 200", n)
	}
	var crash map[string]any
	for _, e := range c.entries(t) {
		if e["event"] == "$exception" {
			crash = e
		}
	}
	if crash == nil {
		t.Fatalf("no $exception event sent:\n%s", c.raw())
	}
	machine := h.events(h.run("--json", "machine").stdout)[0]
	equal(t, "distinct_id", crash["distinct_id"], telemetryID(machine["id"].(string)))
	props := crash["properties"].(map[string]any)
	equal(t, "$process_person_profile", props["$process_person_profile"], false)
	list := props["$exception_list"].([]any)
	exception := list[0].(map[string]any)
	equal(t, "type", exception["type"], "*errors.errorString")
	equal(t, "handled", exception["mechanism"].(map[string]any)["handled"], false)
	value := exception["value"].(string)
	contains(t, "value", value, "<agentx-home>/settings.json")
	contains(t, "value", value, "~/project")
	stack := exception["stacktrace"].(map[string]any)
	equal(t, "stacktrace type", stack["type"], "raw")
	var inApp bool
	for _, f := range stack["frames"].([]any) {
		frame := f.(map[string]any)
		equal(t, "platform", frame["platform"], "go")
		if strings.HasSuffix(frame["function"].(string), "testPanic") {
			inApp = frame["in_app"].(bool)
		}
	}
	if !inApp {
		t.Error("the frame that panicked is not in_app")
	}
	for _, private := range []string{root, h.home, h.agentx, h.library} {
		if strings.Contains(c.raw(), private) {
			t.Errorf("a body carries %q:\n%s", private, c.raw())
		}
	}
}
