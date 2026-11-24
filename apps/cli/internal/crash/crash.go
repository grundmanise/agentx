// Package crash turns a panic into an error a command can end with, and
// into the exception PostHog's error tracking receives.
package crash

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/posthog/posthog-go/v2"
)

// module is the prefix of every function of agentx's own code but main's.
const module = "github.com/grundmanise/agentx/"

// Panic is a recovered panic, as an error: what was passed to panic and
// the stack it was raised on, as the SDK reads it.
type Panic struct {
	Value  any
	Stack  *posthog.ExceptionStacktrace
	Images []posthog.DebugImage
}

// Error is the panic as Go prints it: it is meant for the user's own
// terminal.
func (p *Panic) Error() string { return fmt.Sprintf("panic: %v", p.Value) }

// Type is the Go type of the panic's value, such as "runtime.boundsError"
// or "*errors.errorString".
func (p *Panic) Type() string { return fmt.Sprintf("%T", p.Value) }

// Recovered turns what recover returned into a Panic, or nil when nothing
// panicked. It is called from the deferred function that recovered, while
// the stack that panicked is still below it; the frames of the recovering
// code and of runtime.gopanic are dropped.
func Recovered(v any) *Panic {
	if v == nil {
		return nil
	}
	x := posthog.DefaultStackTraceExtractor{InAppDecider: inApp}
	stack := x.GetStackTrace(2) // from Recovered's caller
	for i := len(stack.Frames) - 1; i >= 0; i-- {
		if stack.Frames[i].Function == "runtime.gopanic" {
			stack.Frames = stack.Frames[:i]
			break
		}
	}
	return &Panic{Value: v, Stack: stack, Images: x.GetDebugImages()}
}

// Recover is deferred at the entry of a goroutine: a panic there is handed
// to report, and the goroutine ends, instead of the process.
func Recover(report func(*Panic)) {
	if p := Recovered(recover()); p != nil {
		report(p)
	}
}

// inApp says whether a frame is agentx's own code. The SDK's default
// decides by file path, which a -trimpath build makes module-relative for
// the standard library and dependencies too.
func inApp(f runtime.Frame) bool {
	return strings.HasPrefix(f.Function, module) || strings.HasPrefix(f.Function, "main.")
}

// Exception is p as an unhandled exception of distinctID, at at.
func (p *Panic) Exception(distinctID string, at time.Time) posthog.Exception {
	return posthog.Exception{
		DistinctId: distinctID,
		Timestamp:  at,
		ExceptionList: []posthog.ExceptionItem{{
			Type:       p.Type(),
			Value:      fmt.Sprint(p.Value),
			Mechanism:  &posthog.ExceptionMechanism{Handled: posthog.Ptr(false)},
			Stacktrace: p.Stack,
		}},
		DebugImages: p.Images,
	}
}
