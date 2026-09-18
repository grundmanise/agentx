package cli

import (
	"reflect"
	"testing"
)

func TestVersionHuman(t *testing.T) {
	h := newHarness(t)
	out := h.run("version")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "CLI version")
	contains(t, "stdout", out.stdout, cliVersion)
	contains(t, "stdout", out.stdout, "Schema version")
	contains(t, "stdout", out.stdout, "1")
	equal(t, "stderr", out.stderr, "")
}

func TestVersionJSON(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"--json", "version"}, {"version", "--json"}} {
		out := h.run(args...)
		equal(t, "exit", out.exit, 0)
		equal(t, "stderr", out.stderr, "")
		contains(t, "stdout", out.stdout, `"schema_version":1,`)
		events := h.events(out.stdout)
		if got, want := h.types(events), []string{"version", "result"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("event types = %v, want %v", got, want)
		}
		equal(t, "version.cli_version", events[0]["cli_version"], cliVersion)
		equal(t, "result.ok", events[1]["ok"], true)
	}
}
