package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"

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

// TestSanitisedText pins the rule Streams states for text agentx did not
// write: every control character becomes a space, runs of spaces become
// one, and the leading and trailing ones are dropped.
func TestSanitisedText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want string
	}{
		{"plain text is untouched", "A plain skill", "A plain skill"},
		{"a newline joins the lines", "first line\nsecond line", "first line second line"},
		{"a tab cannot disturb a column", "before\tafter", "before after"},
		{"a lone carriage return cannot overwrite the line", "shown\rhidden", "shown hidden"},
		{"a CRLF is one space", "first\r\nsecond", "first second"},
		{"an SGR sequence loses its escape", "\x1b[31mRED\x1b[0m", "[31mRED [0m"},
		{"an OSC hyperlink loses its escapes", "\x1b]8;;http://evil\x07text", "]8;;http://evil text"},
		{"a NUL and a DEL are spaces", "a\x00b\x7fc", "a b c"},
		{"a C1 control is a space", "a\u009bb", "a b"},
		{"spaces are collapsed and trimmed", "  wide   gap  ", "wide gap"},
		{"nothing but control characters is nothing", "\n\t\r", ""},
		{"empty stays empty", "", ""},
		{"text beyond ASCII is kept", "café — naïve 日本語", "café — naïve 日本語"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitised(tt.text); got != tt.want {
				t.Errorf("sanitised(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
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

// TestQuotedPath pins the rule for a path, which is the rule sanitised
// applies to prose made lossless: a path that carries a control character
// is quoted whole, the way git quotes one, and anything else is printed as
// it is.
func TestQuotedPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want string
	}{
		{"an ordinary path is untouched", "/home/u/.agents/skills/commit", "/home/u/.agents/skills/commit"},
		{"a path beyond ASCII is untouched", "/home/u/skills/日本語", "/home/u/skills/日本語"},
		{"a double quote alone does not quote a path", `/home/u/a"b`, `/home/u/a"b`},
		{"an escape quotes the path, in octal", "/s/na\x1b[31msty", `"/s/na\033[31msty"`},
		{"a newline has a letter of its own", "/s/two\nrows", `"/s/two\nrows"`},
		{"so have a bell, a tab and a carriage return", "/s/a\ab\tc\rd", `"/s/a\ab\tc\rd"`},
		{"a quote and a backslash are escaped in a quoted path", "/s/a\"b\\c\x1b", `"/s/a\"b\\c\033"`},
		{"a C1 control is quoted byte by byte", "/s/a\u009bb", `"/s/a\302\233b"`},
		{"a path that is not UTF-8 is kept whole", "/s/a\xffb\nc", "\"/s/a\xffb\\nc\""},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quotedPath(tt.path); got != tt.want {
				t.Errorf("quotedPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestUnderShown pins how a line on stdout names the directory of a source
// a skill was read from: sanitised, as the source chose it, and quoted the
// way a path is when sanitising would leave nothing, so that the line never
// reads as if the skill sat at the root of the source.
func TestUnderShown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		subpath string
		want    string
	}{
		{"the root names no directory", "", ""},
		{"an ordinary subpath is untouched", "skills/alpha", " under skills/alpha"},
		{"a control character becomes a space", "skills/al\u009bpha", " under skills/al pha"},
		{"nothing but a control character is quoted", "\u009b", ` under "\302\233"`},
		{"nothing but spaces is quoted too", "  ", ` under "  "`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := underShown(tt.subpath); got != tt.want {
				t.Errorf("underShown(%q) = %q, want %q", tt.subpath, got, tt.want)
			}
		})
	}
}

// TestStderrIsSanitisedByTheWriter pins where the rule is applied for the
// lines agentx puts on stderr: at the writer, once, rather than at each of
// the callers that relay what a git, a source or a configuration file said.
// A message is plain text by then (agentx paints the level prefix and
// nothing else), so there is no escape sequence of its own to lose, and the
// callers not yet written are covered too. JSON mode carries the message as
// it was read.
func TestStderrIsSanitisedByTheWriter(t *testing.T) {
	t.Parallel()
	const raw = "remote: \x1b[2K\x1b]0;pwned\ahello\nfrom the server"
	var text bytes.Buffer
	w := &writer{stderr: &text, verbose: true}
	w.warn(raw)
	w.hint(raw)
	w.debugf("git stderr: %s", raw)
	w.fail(&failure{status: exitSource, message: raw, hint: raw})
	w.warnWith(raw, raw)
	for _, r := range text.String() {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("a control character reached the terminal: %q in\n%q", r, text.String())
		}
	}
	equal(t, "lines", strings.Count(text.String(), "\n"), 7) // warn, hint, debug, error, its hint, a warning and the line under it
	contains(t, "stderr", text.String(), "warning: remote: [2K ]0;pwned hello from the server\n")
	contains(t, "stderr", text.String(), "warning: remote: [2K ]0;pwned hello from the server\n  remote: [2K ]0;pwned hello from the server\n")

	var events bytes.Buffer
	j := &writer{stderr: &events, json: true, verbose: true}
	j.warn(raw)
	j.warnWith(raw, raw)
	var messages []string
	for _, line := range strings.Split(strings.TrimSuffix(events.String(), "\n"), "\n") {
		var ev logEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		equal(t, "log.level", ev.Level, "warn")
		messages = append(messages, ev.Message)
	}
	if len(messages) != 2 {
		t.Fatalf("%d log events, want 2:\n%s", len(messages), events.String())
	}
	equal(t, "log.message", messages[0], raw)
	equal(t, "the message of a warning with a line under it", messages[1], raw+"; "+raw)
}
