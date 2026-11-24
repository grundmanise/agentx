package crash

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	t.Parallel()
	paths := Paths{
		Home:    "/home/jo/.agentx",
		Library: "/home/jo/.agents/skills",
		Config:  "/home/jo/.config",
		Temp:    "/tmp/run1/",
		User:    "/home/jo",
	}
	tests := []struct{ name, in, want string }{
		{"agentx home inside HOME", "open /home/jo/.agentx/settings.json: permission denied",
			"open <agentx-home>/settings.json: permission denied"},
		{"HOME", "read /home/jo/project/AGENTS.md", "read ~/project/AGENTS.md"},
		{"library and config", "/home/jo/.agents/skills/pdf and /home/jo/.config/x", "<library>/pdf and <xdg-config>/x"},
		{"temporary directory", "/tmp/run1/a.lock", "<tmp>/a.lock"},
		{"name that only starts like HOME", "/home/joe/x", "<path>"},
		{"module cache of another user", "/home/ci/go/pkg/mod/github.com/spf13/cobra@v1.9.1/command.go",
			"<path>"},
		{"macOS temporary directory", `stat "/private/var/folders/x1/T/agentx-3/f": no such file`,
			`stat "<path>": no such file`},
		{"other users' directories", "/Users/jane/a /root/b /var/folders/c", "<path> <path> <path>"},
		{"Windows path", `open C:\Users\jane\.agentx\settings.json`, `open C:\Users\jane\.agentx\settings.json`},
		{"module path", "github.com/grundmanise/agentx/apps/cli/internal/home/dirs.go",
			"github.com/grundmanise/agentx/apps/cli/internal/home/dirs.go"},
		{"no path", "runtime error: index out of range [3] with length 2",
			"runtime error: index out of range [3] with length 2"},
	}
	for _, tt := range tests {
		if got := Redact(tt.in, paths); got != tt.want {
			t.Errorf("%s: Redact(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestReport(t *testing.T) {
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
	typ, _, frames := p.Report(Paths{})
	if typ != "runtime.boundsError" {
		t.Errorf("type = %q", typ)
	}
	var inApp, std bool
	for _, f := range frames {
		if f.Platform != "go" {
			t.Errorf("frame %+v has no platform go", f)
		}
		if strings.HasPrefix(f.Function, "github.com/grundmanise/agentx/apps/cli/internal/crash.TestReport") {
			inApp = f.InApp
		}
		if f.Function == "testing.tRunner" {
			std = !f.InApp
		}
	}
	if !inApp || !std {
		t.Errorf("in_app wrong: test frame in app %v, testing.tRunner out of app %v; frames %+v", inApp, std, frames)
	}
	if frames[0].Function == "runtime.gopanic" || strings.Contains(frames[0].Function, "Recovered") {
		t.Errorf("innermost frame is the recovering code: %+v", frames[0])
	}

	long := &Panic{Value: strings.Repeat("é", MaxValue), Frames: make([]Frame, MaxFrames)}
	if _, v, _ := long.Report(Paths{}); len(v) > MaxValue {
		t.Errorf("value is %d bytes", len(v))
	}
	deep := func() (p *Panic) {
		defer func() { p = Recovered(recover()) }()
		var recurse func(int)
		recurse = func(n int) {
			if n == 0 {
				panic("deep")
			}
			recurse(n - 1)
		}
		recurse(2 * MaxFrames)
		return nil
	}()
	if len(deep.Frames) != MaxFrames {
		t.Errorf("%d frames, want %d", len(deep.Frames), MaxFrames)
	}
}
