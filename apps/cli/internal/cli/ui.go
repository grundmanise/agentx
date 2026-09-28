package cli

import (
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// style is a lipgloss style; the palette below is every one in use.
type style = lipgloss.Style

// base is the style every other derives from. It leaves tabs alone, so a
// painted fragment carries the exact text a pipe receives.
var base = lipgloss.NewStyle().TabWidth(lipgloss.NoTabConversion)

// fg is base in one of the basic ANSI colours, 0 to 7, so that --color on
// gives the same 16-colour sequences on every terminal.
func fg(colour string) style { return base.Foreground(lipgloss.Color(colour)) }

var (
	plain   = base
	bold    = base.Bold(true)
	dim     = base.Faint(true)
	red     = fg("1")
	green   = fg("2")
	yellow  = fg("3")
	blue    = fg("4")
	magenta = fg("5")
	cyan    = fg("6")
	boldRed = red.Bold(true)
)

// The one meaning each colour has across every command, so that a reader
// learns the palette once.
var (
	heading   = bold    // a section title or a row's leading name
	label     = cyan    // the key of a key-value row
	muted     = dim     // secondary detail: kinds, scopes, resolved paths
	okStyle   = green   // ok, enabled, success
	warnStyle = yellow  // warn, disabled, hints
	failStyle = boldRed // fail, error
	infoStyle = blue    // info
	tagStyle  = magenta // a marker such as (plugin name)
	noteStyle = cyan    // a count such as 12 tools
)

// The status glyphs a row starts with. Their meaning matches the colour.
const (
	glyphOK   = "✓"
	glyphWarn = "!"
	glyphFail = "✗"
	glyphInfo = "•"
)

// ink is the colour decision for one stream: the renderer that paints it,
// or nil when text passes through bare.
type ink struct{ r *lipgloss.Renderer }

// on reports whether the ink paints.
func (k ink) on() bool { return k.r != nil }

// paint wraps s in st when the ink is on.
func (k ink) paint(st style, s string) string {
	if k.r == nil || s == "" {
		return s
	}
	return st.Renderer(k.r).Render(s)
}

// colorMode is the value of --color: on, off, or unset when the flag is
// left out.
type colorMode string

const (
	colorUnset colorMode = ""
	colorOn    colorMode = "on"
	colorOff   colorMode = "off"
)

// resolve decides the ink for out. With the flag unset, colour is on when
// out is a terminal, NO_COLOR is unset and TERM is not dumb, following the
// conventions at no-color.org.
func (m colorMode) resolve(out io.Writer, env map[string]string) ink {
	switch m {
	case colorOn:
		return ink{r: painter(out)}
	case colorOff:
		return ink{}
	}
	if _, set := env["NO_COLOR"]; set || env["TERM"] == "dumb" {
		return ink{}
	}
	if isTerminal(out) {
		return ink{r: painter(out)}
	}
	return ink{}
}

// painter is the renderer for a stream that resolve decided to paint, fixed
// at the basic 16 colours. The profile is set twice on purpose: without the
// option termenv detects one from the process environment and the stream
// when the output is created, and without the call lipgloss detects one
// again when the first style is rendered. Neither may happen here: resolve
// is the only decision, and it reads the environment it was given.
func painter(out io.Writer) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(out, termenv.WithProfile(termenv.ANSI))
	r.SetColorProfile(termenv.ANSI)
	return r
}

// isTerminal reports whether a stream is a terminal. It takes any stream,
// since a prompt has to know it of standard input as colour does of an
// output stream.
func isTerminal(stream any) bool {
	f, ok := stream.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// sanitised is text agentx did not write made fit to print: a skill's name
// and description, which the repository a source was added from supplies;
// the name of a directory in the library or under a client's configuration,
// which whoever made the directory chose; a command line or URL a
// configuration file declares; and whatever agentx relays of what something
// else said, such as the stderr of a git it ran, which carries the messages
// a server sent it. Every control character becomes a space, runs of spaces
// become one and the leading and trailing ones are dropped. A newline
// therefore cannot break the one row per item a listing prints, a tab
// cannot disturb a column, a carriage return cannot overwrite the line
// already printed, and no escape sequence reaches the terminal, whatever
// --color says. A control character is what unicode.IsControl calls one:
// the C0 controls, DEL and the C1 controls, 65 characters and nothing above
// U+00FF, so a format character such as a zero-width joiner passes through,
// being what several writing systems are spelled with. A path takes
// quotedPath instead, being text a reader copies.
//
// Nothing is parsed. agentx does not recognise an escape sequence and strip
// it whole: a parser that misjudged one sequence's end would let the rest
// of it through, while a rule that admits no control character at all
// cannot. What a sequence leaves behind once its ESC is a space, the "[31m"
// of a red, prints as the ordinary text it is, which also shows the reader
// that the source tried.
//
// Where it is applied is the writer's decision for a line on stderr and the
// caller's for a cell on stdout, for the reason the writer gives. Either
// way it happens before anything is painted, so that stripping the SGR
// sequences agentx adds still gives the exact text a pipe receives. JSON
// output is not sanitised: its values are escaped already, so a consumer
// reads what the source wrote.
func sanitised(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	space := true // as if a space had just been written, so a leading one is dropped
	for _, r := range text {
		if unicode.IsControl(r) {
			r = ' '
		}
		if r == ' ' {
			space = true
			continue // written only if a character follows, which drops the trailing ones
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// quotedPath is sanitised for a path: text agentx did not write that the
// reader may want to copy. Turning a control character into a space, which
// is right for a name or a description, would hand back a path that does
// not exist and say nothing about why, so a path that carries one is quoted
// instead, the way git quotes a path under core.quotePath: the whole of it
// in double quotes, a C escape for the bytes that have one, an octal escape
// for the rest, and a backslash before a double quote or a backslash of its
// own. What is printed is then one field, unambiguous and reversible, in a
// convention a reader already knows from git status. A path that carries no
// control character is printed as it is, non-ASCII included, which is git
// with core.quotePath off: the rule here is about the characters a terminal
// obeys, and escaping a CJK path helps nobody reading one.
func quotedPath(path string) string {
	for i := 0; i < len(path); i++ {
		if controlAt(path, i) > 0 {
			return gitQuoted(path)
		}
	}
	return path
}

// gitQuoted is the quoting quotedPath applies, applied whatever the path
// holds.
func gitQuoted(path string) string {
	var b strings.Builder
	b.Grow(len(path) + 2)
	b.WriteByte('"')
	for i := 0; i < len(path); i++ {
		esc, n := cEscapes[path[i]], controlAt(path, i)
		switch {
		case n == 1 && esc != "":
			b.WriteString(esc)
		case n > 0:
			for _, o := range []byte(path[i : i+n]) {
				b.WriteByte('\\')
				b.WriteByte('0' + o>>6)
				b.WriteByte('0' + o>>3&7)
				b.WriteByte('0' + o&7)
			}
			i += n - 1
		case path[i] == '"', path[i] == '\\':
			b.WriteByte('\\')
			b.WriteByte(path[i])
		default:
			b.WriteByte(path[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

// cEscapes are the control characters git's quoting gives a letter of its
// own; every other one is written in octal.
var cEscapes = map[byte]string{'\a': `\a`, '\b': `\b`, '\t': `\t`, '\n': `\n`, '\v': `\v`, '\f': `\f`, '\r': `\r`}

// controlAt is how many bytes of the control character path[i:] starts
// with, and 0 when it starts with none. It is the rule sanitised applies
// read byte by byte, so that the two cover the same characters: a C0
// control and DEL are one byte, and a C1 control, which a terminal obeys as
// readily, is the two bytes of its UTF-8. Reading bytes rather than runes
// keeps a path that is not valid UTF-8, which a file name may be, whole.
func controlAt(path string, i int) int {
	switch b := path[i]; {
	case b < 0x20, b == 0x7f:
		return 1
	case b == 0xc2 && i+1 < len(path) && path[i+1] >= 0x80 && path[i+1] <= 0x9f:
		return 2
	}
	return 0
}

// clipped bounds a value an export or a source supplies before it is put
// into a message. agentx writes a word where a document may hold a
// megabyte, and a refusal that quoted the whole of one would be no easier
// to read than the file. The cut is on a rune boundary, so what is left is
// still text.
func clipped(value string) string {
	const limit = 60
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "…"
}

// cell is one table cell: the text and the style it is painted in. Widths
// are measured on the text without its escape sequences, so painting never
// disturbs alignment, and a cell may carry text painted in several styles.
type cell struct {
	text  string
	style style
}

func c(text string, st style) cell { return cell{text: text, style: st} }

// table is aligned text: columns padded to their widest cell, two spaces
// apart, with nothing after the last non-empty cell of a row. A row of one
// cell is a line of its own, such as a title or a blank line, and sets no
// column width. It replaces text/tabwriter, which cannot measure painted
// text, and lipgloss/table, which pads the last cell of a row to its column.
type table struct {
	rows [][]cell
}

func (t *table) add(cells ...cell) { t.rows = append(t.rows, cells) }

// sortRows orders rows by their first cell and then by the whole row, so
// two runs print the same order and equal names stay together.
func (t *table) sortRows() {
	key := func(cells []cell) string {
		parts := make([]string, len(cells))
		for i, c := range cells {
			parts[i] = c.text
		}
		return strings.Join(parts, "\t")
	}
	sort.SliceStable(t.rows, func(i, j int) bool {
		a, b := t.rows[i][0].text, t.rows[j][0].text
		if a != b {
			return a < b
		}
		return key(t.rows[i]) < key(t.rows[j])
	})
}

// render writes the rows to out, each prefixed with indent.
func (t *table) render(out io.Writer, k ink, indent string) {
	var widths []int
	for _, row := range t.rows {
		if len(row) == 1 {
			continue
		}
		for i, c := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], lipgloss.Width(c.text))
		}
	}
	var b strings.Builder
	for _, row := range t.rows {
		last := len(row) - 1
		for last >= 0 && row[last].text == "" {
			last--
		}
		b.WriteString(indent)
		for i := 0; i <= last; i++ {
			b.WriteString(k.paint(row[i].style, row[i].text))
			if i < last {
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(row[i].text)+2))
			}
		}
		b.WriteByte('\n')
	}
	_, _ = io.WriteString(out, b.String())
}
