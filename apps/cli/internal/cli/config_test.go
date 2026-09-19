package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func readSettingsFile(t *testing.T, h *harness) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.agentx, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, b)
	}
	return m
}

func TestConfigListDefaults(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	out := h.run("config", "list")
	equal(t, "exit", out.exit, 0)
	equal(t, "stderr", out.stderr, "")
	for _, row := range []string{
		"schema_version           1",
		"label                    test-host",
		"auto_push                false",
		"accept_operations        false",
		"disabled_configurations  ",
		"sources                  []",
		"copy_mode                {}",
	} {
		contains(t, "stdout", out.stdout, row)
	}
	if _, err := os.Stat(filepath.Join(h.agentx, "settings.json")); !os.IsNotExist(err) {
		t.Errorf("config list wrote settings.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.agentx, "version")); !os.IsNotExist(err) {
		t.Errorf("config list wrote the version file: %v", err)
	}

	out = h.run("--json", "config", "list")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"settings", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	want := map[string]any{
		"schema_version":          float64(1),
		"label":                   "test-host",
		"auto_push":               false,
		"accept_operations":       false,
		"disabled_configurations": []any{},
		"sources":                 []any{},
		"copy_mode":               map[string]any{},
	}
	if got := events[0]["settings"]; !reflect.DeepEqual(got, want) {
		t.Errorf("settings = %#v, want %#v", got, want)
	}
}

func TestConfigSetAndGet(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	out := h.run("config", "set", "label", "work laptop")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "")
	equal(t, "stderr", out.stderr, "")

	out = h.run("config", "get", "label")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "work laptop\n")

	out = h.run("--json", "config", "set", "auto_push", "true")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"settings", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	settings := events[0]["settings"].(map[string]any)
	equal(t, "settings.label", settings["label"], "work laptop")
	equal(t, "settings.auto_push", settings["auto_push"], true)

	out = h.run("config", "set", "accept_operations", "1")
	equal(t, "exit", out.exit, 0)

	file := readSettingsFile(t, h)
	equal(t, "file label", file["label"], "work laptop")
	equal(t, "file auto_push", file["auto_push"], true)
	equal(t, "file accept_operations", file["accept_operations"], true)
	equal(t, "file schema_version", file["schema_version"], float64(1))

	out = h.run("--json", "config", "get", "auto_push")
	equal(t, "exit", out.exit, 0)
	events = h.events(out.stdout)
	equal(t, "get event type", events[0]["type"], "settings")
	equal(t, "get auto_push", events[0]["settings"].(map[string]any)["auto_push"], true)

	out = h.run("config", "get", "auto_push")
	equal(t, "stdout", out.stdout, "true\n")
	out = h.run("config", "list")
	contains(t, "stdout", out.stdout, "label                    work laptop")
	contains(t, "stdout", out.stdout, "auto_push                true")
}

func TestConfigSetKeepsUnknownCollections(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	file := `{"schema_version":1,"disabled_configurations":["cursor"],"sources":[{"url":"https://example.com/skills"}],"copy_mode":{"my-skill":["cursor"]}}`
	if err := os.WriteFile(filepath.Join(h.agentx, "settings.json"), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}

	out := h.run("config", "set", "label", "kept")
	equal(t, "exit", out.exit, 0)

	got := readSettingsFile(t, h)
	equal(t, "label", got["label"], "kept")
	if want := []any{"cursor"}; !reflect.DeepEqual(got["disabled_configurations"], want) {
		t.Errorf("disabled_configurations = %#v, want %#v", got["disabled_configurations"], want)
	}
	if want := []any{map[string]any{"url": "https://example.com/skills"}}; !reflect.DeepEqual(got["sources"], want) {
		t.Errorf("sources = %#v, want %#v", got["sources"], want)
	}
	if want := map[string]any{"my-skill": []any{"cursor"}}; !reflect.DeepEqual(got["copy_mode"], want) {
		t.Errorf("copy_mode = %#v, want %#v", got["copy_mode"], want)
	}

	out = h.run("config", "list")
	contains(t, "stdout", out.stdout, "disabled_configurations  cursor")
	contains(t, "stdout", out.stdout, `sources                  [{"url":"https://example.com/skills"}]`)
	contains(t, "stdout", out.stdout, `copy_mode                {"my-skill":["cursor"]}`)
}

func TestConfigErrors(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	tests := []struct {
		name string
		args []string
		exit int
		code string
		hint string
	}{
		{"get unknown key", []string{"config", "get", "colour"}, 1, "usage", "label"},
		{"set unknown key", []string{"config", "set", "colour", "blue"}, 1, "usage", "auto_push"},
		{"set read-only key", []string{"config", "set", "sources", "[]"}, 1, "usage", "auto_push"},
		{"set bad bool", []string{"config", "set", "auto_push", "yes"}, 1, "usage", "true or false"},
		{"set empty label", []string{"config", "set", "label", " "}, 1, "usage", ""},
		{"set missing value", []string{"config", "set", "label"}, 1, "usage", ""},
		{"get missing key", []string{"config", "get"}, 1, "usage", ""},
		{"bare config in json", []string{"config"}, 1, "usage", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := h.run(append([]string{"--json"}, tt.args...)...)
			equal(t, "exit", out.exit, tt.exit)
			events := h.events(out.stdout)
			if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event types = %v, want %v", got, want)
			}
			equal(t, "error.code", events[0]["code"], tt.code)
			if tt.hint != "" {
				contains(t, "error.hint", events[0]["hint"].(string), tt.hint)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(h.agentx, "version")); !os.IsNotExist(err) {
		t.Errorf("a refused write bumped the version file: %v", err)
	}

	// A bare config prints help for a human.
	out := h.run("config")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "Usage:")
}

func TestUnreadableSettingsExit10(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	path := filepath.Join(h.agentx, "settings.json")
	if err := os.WriteFile(path, []byte("{\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "list"}, {"config", "get", "label"}, {"config", "set", "label", "x"}, {"machine"}} {
		out := h.run(append([]string{"--json"}, args...)...)
		equal(t, "exit", out.exit, 10)
		events := h.events(out.stdout)
		equal(t, "error.code", events[0]["code"], "internal")
		contains(t, "error.message", events[0]["message"].(string), path)
		contains(t, "error.hint", events[0]["hint"].(string), path)
	}
	out := h.run("config", "list")
	equal(t, "exit", out.exit, 10)
	contains(t, "stderr", out.stderr, "error: ")
	contains(t, "stderr", out.stderr, "hint: ")
	contains(t, "stderr", out.stderr, path)
	if _, err := os.Stat(filepath.Join(h.agentx, "version")); !os.IsNotExist(err) {
		t.Errorf("a failed write bumped the version file: %v", err)
	}
}
