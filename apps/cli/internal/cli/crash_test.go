package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAPanicEndsTheRunAsAnInternalError makes scan panic: the run exits
// 10, with the panic's own message on stderr, or in an error event with
// code internal under --json. Without the user's yes, from an unset or off
// setting, DO_NOT_TRACK or a source build, no crash is sent.
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
	if strings.Contains(c.raw(), "$exception") {
		t.Errorf("a crash was sent without consent: %s", c.raw())
	}
}

// TestACrashIsReported makes scan panic with telemetry on: PostHog
// receives one $exception event under the anonymous id, with the panic's
// type, message and stack, and no path of the machine.
func TestACrashIsReported(t *testing.T) {
	t.Parallel()
	h, c := telemetryHarness(t)
	h.mustRun("config", "set", "telemetry", "on")
	h.env["AGENTX_TEST_PANIC"] = "1"
	equal(t, "exit", h.run("scan").exit, 10)
	delete(h.env, "AGENTX_TEST_PANIC")

	var crashes []map[string]any
	for _, e := range c.entries(t) {
		if e["event"] == "$exception" {
			crashes = append(crashes, e)
		}
	}
	if len(crashes) != 1 {
		t.Fatalf("%d $exception events sent, want 1:\n%s", len(crashes), c.raw())
	}
	machine := h.events(h.run("--json", "machine").stdout)[0]
	equal(t, "distinct_id", crashes[0]["distinct_id"], anonymousID(machine["id"].(string)))
	props := crashes[0]["properties"].(map[string]any)
	equal(t, "release_channel", props["release_channel"], "stable")
	exception := props["$exception_list"].([]any)[0].(map[string]any)
	equal(t, "type", exception["type"], "*errors.errorString")
	equal(t, "handled", exception["mechanism"].(map[string]any)["handled"], false)
	contains(t, "value", exception["value"].(string), "test panic: open <path>")
	var site string
	for _, f := range exception["stacktrace"].(map[string]any)["frames"].([]any) {
		frame := f.(map[string]any)
		if frame["in_app"] == true {
			site, _ = frame["function"].(string)
		}
	}
	if !strings.HasSuffix(site, "testPanic") {
		t.Errorf("the last frame in app is %q, want testPanic", site)
	}
	for _, private := range []string{filepath.Dir(h.home), h.home, h.agentx, h.library} {
		if strings.Contains(c.raw(), private) {
			t.Errorf("a body carries %q:\n%s", private, c.raw())
		}
	}
}
