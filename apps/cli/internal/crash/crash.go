// Package crash turns a panic into an error a command can end with, and
// into the record a crash report carries: the panic's type, its message and
// its stack, with every user path taken out.
package crash

import (
	"fmt"
	"runtime"
	"strings"
)

// MaxFrames is how many frames a Panic keeps, innermost first.
const MaxFrames = 50

// module is the prefix of every function of agentx's own code but main's;
// a frame of any other function is the standard library's or a
// dependency's.
const module = "github.com/grundmanise/agentx/"

// Frame is one call of a panic's stack, as a crash report carries it.
type Frame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	Lineno   int    `json:"lineno"`
	InApp    bool   `json:"in_app"`
	Platform string `json:"platform"`
}

// Panic is a recovered panic, as an error: what was passed to panic and
// the stack it was raised on, innermost frame first.
type Panic struct {
	Value  any
	Frames []Frame
}

// Error is the panic as Go prints it, unredacted: it is meant for the
// user's own terminal.
func (p *Panic) Error() string { return fmt.Sprintf("panic: %v", p.Value) }

// Type is the Go type of the panic's value, such as "runtime.boundsError"
// or "*errors.errorString".
func (p *Panic) Type() string { return fmt.Sprintf("%T", p.Value) }

// Recovered turns what recover returned into a Panic, or nil when nothing
// panicked. It is called from the deferred function that recovered, while
// the stack that panicked is still below it.
func Recovered(v any) *Panic {
	if v == nil {
		return nil
	}
	pcs := make([]uintptr, 256)
	pcs = pcs[:runtime.Callers(1, pcs)]
	return &Panic{Value: v, Frames: frames(pcs)}
}

// Recover is deferred at the entry of a goroutine: a panic there is handed
// to report, and the goroutine ends, instead of the process.
func Recover(report func(*Panic)) {
	if p := Recovered(recover()); p != nil {
		report(p)
	}
}

// frames reads pcs, innermost first, and keeps what is below the panic:
// the frames of the recovering code and of runtime.gopanic itself are
// dropped.
func frames(pcs []uintptr) []Frame {
	var all []Frame
	cf := runtime.CallersFrames(pcs)
	for {
		f, more := cf.Next()
		all = append(all, Frame{
			Filename: f.File,
			Function: f.Function,
			Lineno:   f.Line,
			InApp:    strings.HasPrefix(f.Function, module) || strings.HasPrefix(f.Function, "main."),
			Platform: "go",
		})
		if !more {
			break
		}
	}
	for i, f := range all {
		if f.Function == "runtime.gopanic" {
			all = all[i+1:]
			break
		}
	}
	if len(all) > MaxFrames {
		all = all[:MaxFrames]
	}
	return all
}
