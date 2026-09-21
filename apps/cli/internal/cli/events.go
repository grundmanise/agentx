package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
type writer struct {
	stdout  io.Writer
	stderr  io.Writer
	env     map[string]string
	json    bool
	verbose bool
	color   string // the --color flag: on, off, or empty when left out
	outInk  *ink   // decided on first use, once the flags are parsed
	errInk  *ink
}

// out is the ink for stdout; err the ink for stderr.
func (w *writer) out() ink { return w.inkFor(&w.outInk, w.stdout) }
func (w *writer) err() ink { return w.inkFor(&w.errInk, w.stderr) }

func (w *writer) inkFor(cached **ink, stream io.Writer) ink {
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
	_, _ = io.WriteString(w.stdout, b.String())
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
		fmt.Fprintf(w.stderr, "%s %s\n", w.err().paint(warnStyle, "hint:"), msg)
	}
}

// warn logs at warn level.
func (w *writer) warn(msg string) {
	if w.json {
		w.line(w.stderr, logEvent{event: newEvent("log"), Level: "warn", Message: msg})
		return
	}
	fmt.Fprintf(w.stderr, "%s %s\n", w.err().paint(warnStyle, "warning:"), msg)
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
	fmt.Fprintf(w.stderr, "%s %s\n", w.err().paint(muted, "debug:"), msg)
}

func (w *writer) fail(f *failure) {
	if w.json {
		w.emit(errorEvent{event: newEvent("error"), Code: f.status.code, Message: f.message, Hint: f.hint})
		return
	}
	k := w.err()
	fmt.Fprintf(w.stderr, "%s %s\n", k.paint(failStyle, "error:"), f.message)
	if f.hint != "" {
		fmt.Fprintf(w.stderr, "%s %s\n", k.paint(warnStyle, "hint:"), f.hint)
	}
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
	_, _ = out.Write(append(b, '\n')) // stdout errors surface through the exit code, not per event
}
