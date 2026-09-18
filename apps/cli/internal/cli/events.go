package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
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
type writer struct {
	stdout  io.Writer
	stderr  io.Writer
	json    bool
	verbose bool
}

// emit writes one event line in JSON mode.
func (w *writer) emit(ev any) {
	if w.json {
		w.line(w.stdout, ev)
	}
}

// table returns an aligned-text writer for human mode. Flush it when done.
func (w *writer) table() *tabwriter.Writer {
	out := w.stdout
	if w.json {
		out = io.Discard
	}
	return tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
}

// printf writes a human line; it does nothing in JSON mode.
func (w *writer) printf(format string, args ...any) {
	if !w.json {
		fmt.Fprintf(w.stdout, format, args...)
	}
}

// warnf logs at warn level.
func (w *writer) warnf(msg string) {
	if w.json {
		w.line(w.stderr, logEvent{event: newEvent("log"), Level: "warn", Message: msg})
		return
	}
	fmt.Fprintf(w.stderr, "warning: %s\n", msg)
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
	fmt.Fprintf(w.stderr, "debug: %s\n", msg)
}

func (w *writer) fail(f *failure) {
	if w.json {
		w.emit(errorEvent{event: newEvent("error"), Code: f.status.code, Message: f.message, Hint: f.hint})
		return
	}
	fmt.Fprintf(w.stderr, "error: %s\n", f.message)
	if f.hint != "" {
		fmt.Fprintf(w.stderr, "hint: %s\n", f.hint)
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
	out.Write(append(b, '\n'))
}
