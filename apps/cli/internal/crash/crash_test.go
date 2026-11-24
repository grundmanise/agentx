package crash

import (
	"strings"
	"testing"
)

func TestRecovered(t *testing.T) {
	t.Parallel()
	var p *Panic
	func() {
		defer func() { p = Recovered(recover()) }()
		var s []int
		_ = s[len(strings.Repeat("x", 3))]
	}()
	if p == nil {
		t.Fatal("nothing recovered")
	}
	if p.Type() != "runtime.boundsError" {
		t.Errorf("type = %q", p.Type())
	}
	frames := p.Stack.Frames
	var site string // wire order: the panic site is the last frame in app
	for _, f := range frames {
		if f.InApp {
			site = f.Function
		}
	}
	if !strings.HasPrefix(site, "github.com/grundmanise/agentx/apps/cli/internal/crash.TestRecovered") {
		t.Errorf("the last frame in app is %q, want the test's own; frames %+v", site, frames)
	}
	for _, f := range frames {
		if f.Function == "testing.tRunner" && f.InApp {
			t.Errorf("testing.tRunner is in app")
		}
		if f.Function == "runtime.gopanic" || strings.Contains(f.Function, "crash.Recovered") {
			t.Errorf("the stack holds the recovering code: %+v", f)
		}
	}
}
