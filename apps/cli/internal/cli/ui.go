package cli

import (
	"io"
	"os"
	"sort"
	"strings"

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

func c(text string, st style) cell { return cell{text: text, style: st} }

// table is aligned text: columns padded to their widest cell, two spaces
// apart, with nothing after the last non-empty cell of a row. It replaces
// text/tabwriter, which cannot measure painted text, and lipgloss/table,
// which pads the last cell of a row to its column.
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
