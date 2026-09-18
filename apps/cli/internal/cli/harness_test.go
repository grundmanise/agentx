package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// harness drives Run against a temporary home. Every test goes through it;
// no test touches the real home or reads the process environment.
type harness struct {
	t       *testing.T
	env     map[string]string
	home    string // the user's HOME
	agentx  string // AGENTX_HOME
	library string // AGENTX_LIBRARY
	config  string // XDG_CONFIG_HOME
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{
		t:       t,
		home:    filepath.Join(root, "home"),
		agentx:  filepath.Join(root, "agentx"),
		library: filepath.Join(root, "library"),
		config:  filepath.Join(root, "config"),
	}
	for _, dir := range []string{h.home, h.agentx, h.library, h.config} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h.env = map[string]string{
		"HOME":            h.home,
		"AGENTX_HOME":     h.agentx,
		"AGENTX_LIBRARY":  h.library,
		"XDG_CONFIG_HOME": h.config,
	}
	return h
}

type outcome struct {
	exit   int
	stdout string
	stderr string
}

func (h *harness) run(args ...string) outcome {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), args, h.env, strings.NewReader(""), &stdout, &stderr)
	return outcome{exit: exit, stdout: stdout.String(), stderr: stderr.String()}
}

type jsonEvent map[string]any

// events parses NDJSON, failing the test on any line that is not one JSON object.
func (h *harness) events(text string) []jsonEvent {
	h.t.Helper()
	if text == "" {
		return nil
	}
	var events []jsonEvent
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		var e jsonEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			h.t.Fatalf("not a JSON event: %q: %v", line, err)
		}
		events = append(events, e)
	}
	return events
}

// types lists the event types in order; every event must carry schema_version 1.
func (h *harness) types(events []jsonEvent) []string {
	h.t.Helper()
	var types []string
	for _, e := range events {
		if e["schema_version"] != float64(1) {
			h.t.Errorf("event %v: schema_version = %v, want 1", e, e["schema_version"])
		}
		types = append(types, e["type"].(string))
	}
	return types
}

func equal(t *testing.T, what string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

func contains(t *testing.T, what, text, sub string) {
	t.Helper()
	if !strings.Contains(text, sub) {
		t.Errorf("%s does not contain %q:\n%s", what, sub, text)
	}
}
