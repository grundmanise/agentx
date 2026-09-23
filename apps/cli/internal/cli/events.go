package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// schemaVersion is the output schema every event carries. Bump it when an
// event changes incompatibly.
const schemaVersion = 1

// event is the envelope every JSON line starts with. Event structs embed it.
type event struct {
	Type          string `json:"type"`
	SchemaVersion int    `json:"schema_version"`
}

func newEvent(typ string) event { return event{Type: typ, SchemaVersion: schemaVersion} }

type logEvent struct {
	event
	Level   string `json:"level"`
	Message string `json:"message"`
}

type errorEvent struct {
	event
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

type resultEvent struct {
	event
	OK      bool   `json:"ok"`
	Summary string `json:"summary,omitempty"`
}

// writer is the one place output is written. In JSON mode stdout carries
// NDJSON events and stderr carries log events; otherwise stdout carries text
// and tables and stderr carries plain log lines. Methods that do not apply
// to the current mode do nothing, so a command calls all of them.
//
// Human text is painted per stream: a stream that is a terminal gets colour
// unless NO_COLOR or --color says otherwise, a pipe gets the same text bare.
//
// Every human line this writer puts on stderr (a warning, a hint, an error
// and its hint, a debug line) is sanitised here rather than by whoever
// built it. A message is plain text by the time it arrives: agentx paints
// the level prefix and nothing else, so there is no escape sequence of its
// own to lose, and one rule at the one exit covers the callers that relay
// what a source, a git or a configuration file said, including the ones not
// written yet. Stdout is the other way round: a table cell may already
// carry the SGR it was painted with, which this writer could not tell from
// a source's, so a cell is sanitised where it is built, before it is
// painted. Neither applies in JSON mode, where an event carries what was
// read and the encoder escapes it.
//
// Serve answers searches from the goroutine that reads its stdin while the
// loop reports scans from its own, so every write of one event or one line
// is made under mu and lands whole.
type writer struct {
	stdout  io.Writer
	stderr  io.Writer
	env     map[string]string
	json    bool
	verbose bool
	color   string // the --color flag: on, off, or empty when left out
	mu      sync.Mutex
	inkMu   sync.Mutex
	outInk  *ink // decided on first use, once the flags are parsed
	errInk  *ink
}

// out is the ink for stdout; err the ink for stderr.
func (w *writer) out() ink { return w.inkFor(&w.outInk, w.stdout) }
func (w *writer) err() ink { return w.inkFor(&w.errInk, w.stderr) }

func (w *writer) inkFor(cached **ink, stream io.Writer) ink {
	w.inkMu.Lock()
	defer w.inkMu.Unlock()
	if *cached == nil {
		k := colorMode(w.color).resolve(stream, w.env)
		*cached = &k
	}
	return **cached
}

// emit writes one event line in JSON mode.
func (w *writer) emit(ev any) {
	if w.json {
		w.line(w.stdout, ev)
	}
}

// print writes one human line to stdout from parts that are joined as they
// are; it does nothing in JSON mode.
func (w *writer) print(parts ...string) {
	if w.json {
		return
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p)
	}
	b.WriteByte('\n')
	w.write(w.stdout, b.String())
}

// prompt writes a question to stdout without a newline, so the answer is
// typed after it; it does nothing in JSON mode, where nothing but events
// may reach stdout. Only a command that has decided a person is there to
// answer calls it.
func (w *writer) prompt(text string) {
	if !w.json {
		w.write(w.stdout, text)
	}
}

// paint styles s for stdout.
func (w *writer) paint(st style, s string) string { return w.out().paint(st, s) }

// render writes t to stdout with indent before every row; nothing in JSON mode.
func (w *writer) render(t *table, indent string) {
	if !w.json {
		t.render(w.stdout, w.out(), indent)
	}
}

// done confirms a change on stdout: a green check and the message.
func (w *writer) done(msg string) {
	w.print(w.paint(okStyle, glyphOK), " ", msg)
}

// hint says on stderr what to do about the warnings above it; in JSON mode
// the snapshot carries the hints.
func (w *writer) hint(msg string) {
	if !w.json {
		w.write(w.stderr, fmt.Sprintf("%s %s\n", w.err().paint(warnStyle, "hint:"), sanitised(msg)))
	}
}

// warn logs at warn level.
func (w *writer) warn(msg string) {
	if w.json {
		w.line(w.stderr, logEvent{event: newEvent("log"), Level: "warn", Message: msg})
		return
	}
	w.write(w.stderr, fmt.Sprintf("%s %s\n", w.err().paint(warnStyle, "warning:"), sanitised(msg)))
}

// debugf logs at debug level, shown only with --verbose.
func (w *writer) debugf(format string, args ...any) {
	if !w.verbose {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if w.json {
		w.line(w.stderr, logEvent{event: newEvent("log"), Level: "debug", Message: msg})
		return
	}
	w.write(w.stderr, fmt.Sprintf("%s %s\n", w.err().paint(muted, "debug:"), sanitised(msg)))
}

func (w *writer) fail(f *failure) {
	if w.json {
		w.emit(errorEvent{event: newEvent("error"), Code: f.status.code, Message: f.message, Hint: f.hint})
		return
	}
	k := w.err()
	text := fmt.Sprintf("%s %s\n", k.paint(failStyle, "error:"), sanitised(f.message))
	if f.hint != "" {
		text += fmt.Sprintf("%s %s\n", k.paint(warnStyle, "hint:"), sanitised(f.hint))
	}
	w.write(w.stderr, text)
}

// result terminates the event stream; it is emitted exactly once per run.
func (w *writer) result(ok bool, summary string) {
	w.emit(resultEvent{event: newEvent("result"), OK: ok, Summary: summary})
}

func (w *writer) line(out io.Writer, ev any) {
	b, err := json.Marshal(ev)
	if err != nil {
		panic(err) // events are plain structs; marshalling cannot fail
	}
	w.write(out, string(b)+"\n")
}

// write puts text on out in one write under the lock; a failed write
// surfaces through the exit code, not per line.
func (w *writer) write(out io.Writer, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = io.WriteString(out, text)
}
