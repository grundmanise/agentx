package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
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

// A pipe gets bare text, so every human line a test asserts on is
// unpainted, and painting changes nothing but the escape sequences: columns
// stay aligned.
func TestColorKeepsAlignment(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, args := range [][]string{{"version"}, {"config", "list"}, {"doctor"}, {"help"}, {"machine"}, {"bogus"}} {
		plain := h.run(args...)
		if escapes.MatchString(plain.stdout) || escapes.MatchString(plain.stderr) {
			t.Errorf("%v: output is painted on a pipe:\n%s%s", args, plain.stdout, plain.stderr)
		}
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

// TestHelpSections pins the shape of a help page: the sectioned command
// list of a group and of the root, the two example columns, the one-line
// global flags of a command page and the full ones of the root, and the
// banner. The rows themselves come from the command data, so only the
// structure around them is pinned here.
func TestHelpSections(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("config", "--help")
	equal(t, "exit", out.exit, 0)
	for _, s := range []string{
		"Read and change the settings\n\nUsage:\n  agentx config <command> [arguments]\n\nSETTINGS\n  list ",
		"  get <key>  ",
		"  disable <configuration>  Leave a client out of placements by default\n",
		"\nExamples:\n  agentx config list  ",
		"\nGlobal flags: --json, --verbose, --color (see 'agentx --help')\n\nRun 'agentx config <command> --help' for the details, flags and examples of a command.\n",
	} {
		contains(t, "stdout", out.stdout, s)
	}
	for _, s := range []string{"Commands:", "Flags:\n", "--help  help for"} {
		if strings.Contains(out.stdout, s) {
			t.Errorf("config --help still prints %q:\n%s", s, out.stdout)
		}
	}

	out = h.run("--help")
	equal(t, "exit", out.exit, 0)
	for _, s := range []string{
		strings.Join(banner, "\n") + "\n\nInventory agent clients, skills, MCP servers and plugins\nhttps://docs.agentx.wtf\n\nUsage:\n  agentx [--json] [--verbose] [--color on|off] <command> [arguments]\n\nSKILLS\n  skill add <source>  ",
		"\nUPDATES AND PUBLISHING\n  skill check-updates  ",
		"\nSOURCES\n  source add <url>  ",
		"\nMACHINES\n  machine  ",
		"\nSETTINGS\n  config list  ",
		"\nINVENTORY AND DIAGNOSTICS\n  scan  ",
		"\nExamples:\n" +
			"  agentx skill add anthropics/skills --name pdf  install one skill into every enabled client\n" +
			"  agentx skill new release-notes                 create a skill of your own\n" +
			"  agentx skill check-updates                     see what changed upstream before you update\n" +
			"  agentx export --machine cloud                  seed the cloud machine with this one's skills\n" +
			"\nGlobal flags:\n" +
			"      --color string  colour the text output: on or off; left out, a terminal gets colour and a pipe does not\n" +
			"      --json          write newline-delimited JSON events to stdout\n" +
			"      --verbose       log at debug level on stderr\n" +
			"\nRun 'agentx <command> --help' for the details, flags and examples of a command.\n",
	} {
		contains(t, "stdout", out.stdout, s)
	}
	if strings.Contains(out.stdout, "\n  help ") {
		t.Errorf("the root lists cobra's help command:\n%s", out.stdout)
	}
	equal(t, "the dim line is the root's only on other pages", strings.Contains(out.stdout, "Global flags: --json"), false)

	// A runnable command with children is a row of its own page's section
	// and keeps its own usage line.
	out = h.run("machine", "--help")
	contains(t, "stdout", out.stdout, "Usage:\n  agentx machine [flags]\n  agentx machine <command> [arguments]\n\nMACHINES\n  rename <label>  ")
	contains(t, "stdout", out.stdout, "Run 'agentx machine <command> --help' for")

	// An unknown help topic gets the usage block: the list without the
	// banner or the examples.
	out = h.run("help", "bogus")
	contains(t, "stdout", out.stdout, "Usage:\n  agentx [--json] [--verbose] [--color on|off] <command> [arguments]\n\nSKILLS\n")
	for _, s := range []string{banner[1], "Examples:"} {
		if strings.Contains(out.stdout, s) {
			t.Errorf("the usage block prints %q:\n%s", s, out.stdout)
		}
	}

	out = h.run("--color=on", "scan", "--help")
	contains(t, "stdout", out.stdout, "\x1b[1mUsage:\x1b[0m\n  agentx scan [flags]\n")
	contains(t, "stdout", out.stdout, "\x1b[1mFlags:\x1b[0m\n")
	contains(t, "stdout", out.stdout, "\x1b[36m    --handshake\x1b[0m")
	contains(t, "stdout", out.stdout, "\x1b[36m    --project string\x1b[0m")
	contains(t, "stdout", out.stdout, "\n\x1b[2mGlobal flags: --json, --verbose, --color (see 'agentx --help')\x1b[0m\n")

	out = h.run("--color=on", "config", "--help")
	contains(t, "stdout", out.stdout, "\x1b[1mSETTINGS\x1b[0m\n  \x1b[36mlist\x1b[0m")
	contains(t, "stdout", out.stdout, "  \x1b[36mget <key>\x1b[0m")
	contains(t, "stdout", out.stdout, "\x1b[1mExamples:\x1b[0m\n  \x1b[36magentx config list\x1b[0m")
	contains(t, "stdout", out.stdout, "Run \x1b[36m'agentx config <command> --help'\x1b[0m for")

	out = h.run("--color=on", "--help")
	contains(t, "stdout", out.stdout, "\x1b[36m"+banner[0]+"\x1b[0m\n\x1b[36m"+banner[1]+"\x1b[0m\n")
	contains(t, "stdout", out.stdout, "\x1b[1mInventory agent clients, skills, MCP servers and plugins\x1b[0m\n\x1b[2mhttps://docs.agentx.wtf\x1b[0m\n")
	contains(t, "stdout", out.stdout, "\x1b[1mGlobal flags:\x1b[0m\n  \x1b[36m    --color string\x1b[0m")
}

// TestHelpSynopsis pins the first column of a command's row: its path below
// the page's command, then the arguments of its Use line or the shorter
// ones annotationSynopsis gives.
func TestHelpSynopsis(t *testing.T) {
	t.Parallel()
	root := &cobra.Command{Use: "agentx"}
	skill := &cobra.Command{Use: "skill"}
	add := &cobra.Command{Use: "add [<source>[/<subpath>]] [--name <name>]...", Annotations: map[string]string{annotationSynopsis: "<source>"}}
	list := &cobra.Command{Use: "list"}
	rename := &cobra.Command{Use: "rename <old> <new>"}
	bare := &cobra.Command{Use: "publish [<name>]", Annotations: map[string]string{annotationSynopsis: ""}}
	skill.AddCommand(add, list, rename, bare)
	root.AddCommand(skill)
	tests := []struct {
		name string
		page *cobra.Command
		cmd  *cobra.Command
		want string
	}{
		{"an override replaces the arguments of Use", root, add, "skill add <source>"},
		{"a group page drops the group's name", skill, add, "add <source>"},
		{"a name alone", root, list, "skill list"},
		{"the arguments of Use", skill, rename, "rename <old> <new>"},
		{"an empty override hides the arguments", root, bare, "skill publish"},
		{"a group is its path", root, skill, "skill"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := synopsis(tt.page, tt.cmd); got != tt.want {
				t.Errorf("synopsis = %q, want %q", got, tt.want)
			}
		})
	}

	args := []struct{ use, want string }{
		{"list", ""},
		{"get <key>", "<key>"},
		{"export [<name> | --machine <id>]", "[<name> | --machine <id>]"},
		{"  set   <key> <value>  ", "<key> <value>"},
		{"", ""},
	}
	for _, tt := range args {
		if got := useArgs(tt.use); got != tt.want {
			t.Errorf("useArgs(%q) = %q, want %q", tt.use, got, tt.want)
		}
	}
}

// TestHelpExampleSplit pins how an Example line becomes two columns: split
// at its first tab, and left whole without one.
func TestHelpExampleSplit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, line, command, description string
		ok                               bool
	}{
		{"a command and its description", "agentx scan\tinventory every client", "agentx scan", "inventory every client", true},
		{"the first tab splits", "agentx scan\ta\tb", "agentx scan", "a\tb", true},
		{"a line without a tab is kept whole", "agentx scan   inventory", "agentx scan   inventory", "", false},
		{"an empty description", "agentx scan\t", "agentx scan", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, description, ok := splitExample(tt.line)
			equal(t, "command", command, tt.command)
			equal(t, "description", description, tt.description)
			equal(t, "ok", ok, tt.ok)
		})
	}
	equal(t, "examples joins pairs with a tab", examples("a", "b", "c", "d"), "a\tb\nc\td")

	var out bytes.Buffer
	writeExamples(&out, ink{}, "agentx scan\tinventory\nplain line\nagentx scan --json\tas events", 0)
	equal(t, "columns", out.String(), "  agentx scan         inventory\n  plain line\n  agentx scan --json  as events\n")
}

// TestHelpTreeIsComplete walks the command tree and holds every command to
// the help contract: a runnable command falls in a section one of its
// ancestors or itself names, a Short fits a row and ends without a full
// stop, and every Example line is a command line starting with agentx, a
// tab and a description. A command that breaks the contract would still
// render, in the last section or as one column, which is why a test guards
// it rather than the renderer.
func TestHelpTreeIsComplete(t *testing.T) {
	t.Parallel()
	known := map[string]bool{}
	for _, s := range sections {
		known[s.id] = true
	}
	root := newRoot(&invocation{env: map[string]string{}, out: &writer{}})
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		path := cmd.CommandPath()
		if id, set := cmd.Annotations[annotationSection]; set && !known[id] {
			t.Errorf("%s: section %q is not one of sections", path, id)
		}
		if cmd.Runnable() && cmd.Annotations[annotationGroup] == "" && cmd.HasParent() {
			if _, set := cmd.Annotations[annotationSection]; !set && !inherits(cmd) {
				t.Errorf("%s: no section of its own or of an ancestor", path)
			}
		}
		if cmd.HasParent() && cmd.IsAvailableCommand() {
			if len(cmd.Short) > 60 || strings.HasSuffix(cmd.Short, ".") || cmd.Short == "" {
				t.Errorf("%s: Short %q is empty, over 60 characters or ends with a full stop", path, cmd.Short)
			}
		}
		for _, line := range strings.Split(cmd.Example, "\n") {
			if cmd.Example == "" {
				break
			}
			command, description, ok := splitExample(line)
			if !ok || description == "" || !strings.HasPrefix(command, "agentx") {
				t.Errorf("%s: Example line %q is not a command line, a tab and a description", path, line)
			}
		}
		for _, sub := range cmd.Commands() {
			if sub.IsAvailableCommand() {
				walk(sub)
			}
		}
	}
	walk(root)
}

// inherits reports whether an ancestor of cmd names a section.
func inherits(cmd *cobra.Command) bool {
	for c := cmd.Parent(); c != nil; c = c.Parent() {
		if c.Annotations[annotationSection] != "" {
			return true
		}
	}
	return false
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

// TestWrapWords pins how the last column of a help row is wrapped: at
// spaces, to the width, with a word wider than the width on a line of its
// own, and left whole when the width is narrow or the text is painted.
func TestWrapWords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"text that fits is one line", "place a copy", 40, []string{"place a copy"}},
		{"a break falls at a space", "replace a displaced directory whose content differs", 30, []string{"replace a displaced directory", "whose content differs"}},
		{"a word wider than the width gets its own line", "see https://docs.agentx.wtf/cli/skill#place-a-skill now", 30, []string{"see", "https://docs.agentx.wtf/cli/skill#place-a-skill", "now"}},
		{"a width under minWrap leaves the text whole", "one two three four five six seven", 20, []string{"one two three four five six seven"}},
		{"painted text is never split", "\x1b[1mone\x1b[0m two three four five six seven eight", 24, []string{"\x1b[1mone\x1b[0m two three four five six seven eight"}},
		{"wide characters count by their cells", "日本語 日本語 日本語 日本語 日本語", 24, []string{"日本語 日本語 日本語", "日本語 日本語"}},
		{"empty stays empty", "", 40, []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wrapWords(tt.text, tt.width); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("wrapWords(%q, %d) = %q, want %q", tt.text, tt.width, got, tt.want)
			}
		})
	}

	// A table wraps only its last column, and continues it under itself.
	tb := &table{wrap: 40}
	tb.add(c("--force", label), c("replace a displaced directory whose content differs from the library", plain))
	tb.add(c("--copy", label), c("place a copy", plain))
	var out bytes.Buffer
	tb.render(&out, ink{}, "  ")
	equal(t, "wrapped", out.String(), "  --force  replace a displaced directory\n           whose content differs from\n           the library\n  --copy   place a copy\n")
}

// helpGoldenFile is the whole of agentx --help, pinned once. Write it again
// with go test ./internal/cli -run TestHelpGolden -update.
const helpGoldenFile = "testdata/golden/help.txt"

// TestHelpGolden pins the root page exactly, and the wrapping a terminal of
// 80 columns gets, which the environment's COLUMNS stands in for.
func TestHelpGolden(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("--help")
	equal(t, "exit", out.exit, 0)
	if *update {
		if err := os.WriteFile(helpGoldenFile, []byte(out.stdout), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		want, err := os.ReadFile(helpGoldenFile)
		if err != nil {
			t.Fatalf("%v; write it with go test ./internal/cli -run TestHelpGolden -update", err)
		}
		equal(t, "agentx --help", out.stdout, string(want))
	}

	h.env["COLUMNS"] = "80"
	out = h.run("skill", "place", "--help")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "      --machine string  place the skill on this machine instead, by its branch\n                        on the account remote; only cloud is accepted\n")
	for _, line := range strings.Split(out.stdout, "\n") {
		if strings.HasPrefix(line, "      --") && len(line) > 80 {
			t.Errorf("a flag row runs past 80 columns: %q", line)
		}
	}
	out = h.run("--json", "skill", "place", "--help")
	contains(t, "stderr", out.stderr, "      --machine string  place the skill on this machine instead, by its branch\n")
	delete(h.env, "COLUMNS")
	out = h.run("skill", "place", "--help")
	contains(t, "stdout", out.stdout, "      --machine string  place the skill on this machine instead, by its branch on the account remote; only cloud is accepted\n")
}
