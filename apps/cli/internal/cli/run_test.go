package cli

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestUsageErrors(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{"no command", nil, "no command given"},
		{"unknown command", []string{"bogus"}, `unknown command "bogus" for "agentx"`},
		{"unknown global flag", []string{"--bogus", "version"}, "unknown flag: --bogus"},
		{"unknown command flag", []string{"version", "--bogus"}, "unknown flag: --bogus"},
		{"unexpected argument", []string{"version", "extra"}, `unknown command "extra" for "agentx version"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := h.run(tt.args...)
			equal(t, "exit", out.exit, 1)
			equal(t, "stdout", out.stdout, "")
			contains(t, "stderr", out.stderr, tt.message)
			contains(t, "stderr", out.stderr, "agentx help")

			out = h.run(append([]string{"--json"}, tt.args...)...)
			equal(t, "exit", out.exit, 1)
			equal(t, "stderr", out.stderr, "")
			events := h.events(out.stdout)
			if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event types = %v, want %v", got, want)
			}
			equal(t, "error.code", events[0]["code"], "usage")
			equal(t, "error.message", events[0]["message"], tt.message)
			contains(t, "error.hint", events[0]["hint"].(string), "agentx help")
			equal(t, "result.ok", events[1]["ok"], false)
		})
	}
}

func TestHelp(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}, {"version", "--help"}} {
		out := h.run(args...)
		equal(t, "exit", out.exit, 0)
		contains(t, "stdout", out.stdout, "Usage:")
		contains(t, "stdout", out.stdout, "agentx")
		contains(t, "stdout", out.stdout, "version")
		contains(t, "stdout", out.stdout, "--json")
		equal(t, "stderr", out.stderr, "")
	}

	// In JSON mode help text goes to stderr so stdout stays events only.
	out := h.run("--json", "help")
	equal(t, "exit", out.exit, 0)
	contains(t, "stderr", out.stderr, "Usage:")
	if got, want := h.types(h.events(out.stdout)), []string{"result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
}

func TestVerboseLogsDirectories(t *testing.T) {
	h := newHarness(t)

	quiet := h.run("version")
	equal(t, "quiet stderr", quiet.stderr, "")

	out := h.run("--verbose", "version")
	equal(t, "exit", out.exit, 0)
	contains(t, "stderr", out.stderr, "debug: ")
	contains(t, "stderr", out.stderr, "agentx home "+h.agentx)
	contains(t, "stderr", out.stderr, "library "+h.library)
	contains(t, "stderr", out.stderr, "config home "+h.config)

	out = h.run("--json", "--verbose", "version")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout events", len(h.events(out.stdout)), 2)
	logs := h.events(out.stderr)
	if got, want := h.types(logs), []string{"log"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stderr event types = %v, want %v", got, want)
	}
	equal(t, "log.level", logs[0]["level"], "debug")
	contains(t, "log.message", logs[0]["message"].(string), h.agentx)
}

func TestDirectoriesDefaultFromHome(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "AGENTX_HOME")
	delete(h.env, "AGENTX_LIBRARY")
	delete(h.env, "XDG_CONFIG_HOME")

	out := h.run("--verbose", "version")
	equal(t, "exit", out.exit, 0)
	contains(t, "stderr", out.stderr, "agentx home "+filepath.Join(h.home, ".agentx"))
	contains(t, "stderr", out.stderr, "library "+filepath.Join(h.home, ".agents", "skills"))
	contains(t, "stderr", out.stderr, "config home "+filepath.Join(h.home, ".config"))
}

func TestMissingHomeIsUsageError(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "HOME")

	out := h.run("--json", "version")
	equal(t, "exit", out.exit, 1)
	events := h.events(out.stdout)
	equal(t, "error.code", events[0]["code"], "usage")
	equal(t, "error.message", events[0]["message"], "HOME is not set")
}
