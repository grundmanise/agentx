package cli

import (
	"bytes"
	"reflect"
	"regexp"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

var escapes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestColorResolve(t *testing.T) {
	t.Parallel()
	var pipe bytes.Buffer
	tests := []struct {
		name string
		mode colorMode
		env  map[string]string
		want bool
	}{
		{"unset on a pipe", colorUnset, map[string]string{}, false},
		{"on, on a pipe", colorOn, map[string]string{}, true},
		{"on despite NO_COLOR", colorOn, map[string]string{"NO_COLOR": "1"}, true},
		{"off", colorOff, map[string]string{}, false},
		{"NO_COLOR set empty", colorUnset, map[string]string{"NO_COLOR": ""}, false},
		{"TERM dumb", colorUnset, map[string]string{"TERM": "dumb"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			equal(t, "ink", tt.mode.resolve(&pipe, tt.env).on(), tt.want)
		})
	}
}

// A pipe gets bare text: every human line a test asserts on is unpainted.
func TestColorOffOnPipe(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, args := range [][]string{{"version"}, {"doctor"}, {"config", "list"}, {"help"}, {"bogus"}} {
		out := h.run(args...)
		if escapes.MatchString(out.stdout) || escapes.MatchString(out.stderr) {
			t.Errorf("%v: output is painted on a pipe:\n%s%s", args, out.stdout, out.stderr)
		}
	}
}

func TestColorAlwaysPaintsEveryStream(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	out := h.run("--color", "on", "version")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "\x1b[36mCLI version\x1b[0m")
	contains(t, "stdout", out.stdout, "\x1b[1m"+cliVersion+"\x1b[0m")

	out = h.run("--color=on", "doctor")
	contains(t, "stdout", out.stdout, "\x1b[1mSystem\x1b[0m\n  \x1b[32m✓\x1b[0m git")
	contains(t, "stdout", out.stdout, "\x1b[33m!\x1b[0m \x1b[1mclients\x1b[0m")
	contains(t, "stdout", out.stdout, "\x1b[33mhint:\x1b[0m install an agent client")

	out = h.run("--color=on", "bogus")
	equal(t, "exit", out.exit, 1)
	contains(t, "stderr", out.stderr, "\x1b[1;31merror:\x1b[0m unknown command")
	contains(t, "stderr", out.stderr, "\x1b[33mhint:\x1b[0m run 'agentx help'")

	out = h.run("--color=on", "--verbose", "version")
	contains(t, "stderr", out.stderr, "\x1b[2mdebug:\x1b[0m agentx home")

	// JSON stays JSON: colour never reaches an event stream.
	out = h.run("--color=on", "--json", "version")
	equal(t, "exit", out.exit, 0)
	if escapes.MatchString(out.stdout) {
		t.Errorf("JSON stdout is painted:\n%s", out.stdout)
	}
	h.events(out.stdout)
}

// Painting changes nothing but the escape sequences: columns stay aligned.
func TestColorKeepsAlignment(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, args := range [][]string{{"config", "list"}, {"doctor"}, {"help"}, {"machine"}} {
		plain := h.run(args...)
		painted := h.run(append([]string{"--color=on"}, args...)...)
		if got := escapes.ReplaceAllString(painted.stdout, ""); got != plain.stdout {
			t.Errorf("%v: painted output, escapes stripped, differs from plain:\n%s\n%s", args, got, plain.stdout)
		}
	}
}

func TestColorRejectsUnknownMode(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("--color", "always", "version")
	equal(t, "exit", out.exit, 1)
	equal(t, "stdout", out.stdout, "")
	contains(t, "stderr", out.stderr, `error: invalid value "always" for --color`)
	contains(t, "stderr", out.stderr, "hint: use on or off")

	out = h.run("--color", "off", "version")
	equal(t, "exit", out.exit, 0)
	if escapes.MatchString(out.stdout) {
		t.Errorf("--color off is painted:\n%s", out.stdout)
	}

	out = h.run("--json", "--color", "always", "version")
	equal(t, "exit", out.exit, 1)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "error.code", events[0]["code"], "usage")
}

func TestTableAlignsPaintedCells(t *testing.T) {
	t.Parallel()
	tb := &table{}
	tb.add(c("\x1b[1mab\x1b[0m", plain), c("x", plain), c("end", plain))
	tb.add(c("abcd", plain), c("", plain), c("", plain))
	tb.add(c("", plain), c("", plain), c("note", plain))
	tb.add(c("a title wider than any column", plain))
	var plainOut, painted bytes.Buffer
	tb.render(&plainOut, ink{}, "> ")
	tb.render(&painted, colorOn.resolve(&painted, nil), "> ")
	equal(t, "plain", plainOut.String(), "> \x1b[1mab\x1b[0m    x  end\n> abcd\n>          note\n> a title wider than any column\n")
	equal(t, "painted equals plain: cells carry their own escapes", painted.String(), plainOut.String())

	styled := &table{}
	styled.add(c("k", label), c("v", okStyle))
	painted.Reset()
	styled.render(&painted, colorOn.resolve(&painted, nil), "")
	equal(t, "styled", painted.String(), "\x1b[36mk\x1b[0m  \x1b[32mv\x1b[0m\n")
	equal(t, "width", lipgloss.Width("\x1b[1;31m✓\x1b[0m ab"), 4)
}

func TestHelpSections(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("config", "--help")
	equal(t, "exit", out.exit, 0)
	for _, s := range []string{
		"Read and change the settings\n\nUsage:\n  agentx config [command]\n",
		"Commands:\n",
		"  disable  Leave a configuration out of placements by default\n",
		"  list     Print every setting\n",
		"Global flags:\n",
		"      --color string  colour the text output: on or off; left out, a terminal gets colour and a pipe does not\n",
		"      --json          write newline-delimited JSON events to stdout\n",
		`Use "agentx config [command] --help" for more information about a command.`,
	} {
		contains(t, "stdout", out.stdout, s)
	}

	out = h.run("--color=on", "scan", "--help")
	contains(t, "stdout", out.stdout, "\x1b[1mUsage:\x1b[0m\n  agentx scan [flags]\n")
	contains(t, "stdout", out.stdout, "\x1b[1mFlags:\x1b[0m\n")
	contains(t, "stdout", out.stdout, "\x1b[36m    --handshake\x1b[0m")
	contains(t, "stdout", out.stdout, "\x1b[36m    --project string\x1b[0m")
}
