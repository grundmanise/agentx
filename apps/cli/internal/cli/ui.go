package cli

import (
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// style is one ANSI SGR parameter list, applied only when a stream is coloured.
type style string

const (
	plain   style = ""
	bold    style = "1"
	dim     style = "2"
	red     style = "31"
	green   style = "32"
	yellow  style = "33"
	blue    style = "34"
	magenta style = "35"
	cyan    style = "36"
	boldRed style = "1;31"
)

// The one meaning each colour has across every command, so that a reader
// learns the palette once.
const (
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

// ink is the colour decision for one stream: true paints, false passes text through.
type ink bool

// paint wraps s in st when the ink is on.
func (k ink) paint(st style, s string) string {
	if !k || st == plain || s == "" {
		return s
	}
	return "\x1b[" + string(st) + "m" + s + "\x1b[0m"
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
		return true
	case colorOff:
		return false
	}
	if _, set := env["NO_COLOR"]; set || env["TERM"] == "dumb" {
		return false
	}
	return ink(isTerminal(out))
}

func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// cell is one table cell: the text and the style it is painted in. Widths
// are measured on the text without its escape sequences, so painting never
// disturbs alignment, and a cell may carry text painted in several styles.
type cell struct {
	text  string
	style style
}

// width is the number of columns text takes on a terminal: its runes,
// escape sequences excluded.
func width(text string) int {
	n := 0
	for i := 0; i < len(text); {
		if text[i] == '\x1b' && i+1 < len(text) && text[i+1] == '[' {
			j := i + 2
			for j < len(text) && text[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
		n++
	}
	return n
}

func c(text string, st style) cell { return cell{text: text, style: st} }

// table is aligned text: columns padded to their widest cell, two spaces
// apart, with nothing after the last non-empty cell of a row. It replaces
// text/tabwriter, which cannot measure painted text.
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
		for i, c := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], width(c.text))
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
				b.WriteString(strings.Repeat(" ", widths[i]-width(row[i].text)+2))
			}
		}
		b.WriteByte('\n')
	}
	_, _ = io.WriteString(out, b.String())
}
