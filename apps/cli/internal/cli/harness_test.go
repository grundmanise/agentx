package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// harness drives Run against a temporary home. Every test goes through it;
// no test touches the real home or reads the process environment.
type harness struct {
	t       *testing.T
	env     map[string]string
	home    string // the user's HOME
	agentx  string // AGENTX_HOME
	library string // AGENTX_LIBRARY
	config  string // XDG_CONFIG_HOME
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	// The real path: on macOS the temporary directory is a symlink into
	// /private, and the scan reports symlink targets on their real path.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t:       t,
		home:    filepath.Join(root, "home"),
		agentx:  filepath.Join(root, "agentx"),
		library: filepath.Join(root, "library"),
		config:  filepath.Join(root, "config"),
	}
	for _, dir := range []string{h.home, h.agentx, h.library, h.config} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// PATH is the one value taken from the test process: commands under test
	// still read it from the map, and tests that need another git replace it.
	h.env = map[string]string{
		"PATH":               os.Getenv("PATH"),
		"HOME":               h.home,
		"AGENTX_HOME":        h.agentx,
		"AGENTX_LIBRARY":     h.library,
		"XDG_CONFIG_HOME":    h.config,
		"AGENTX_HOSTNAME":    "test-host",
		"AGENTX_PLATFORM_ID": "platform-test",
	}
	return h
}

type outcome struct {
	exit   int
	stdout string
	stderr string
}

func (h *harness) run(args ...string) outcome {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), args, h.env, strings.NewReader(""), &stdout, &stderr)
	return outcome{exit: exit, stdout: stdout.String(), stderr: stderr.String()}
}

type jsonEvent map[string]any

// events parses NDJSON, failing the test on any line that is not one JSON object.
func (h *harness) events(text string) []jsonEvent {
	h.t.Helper()
	if text == "" {
		return nil
	}
	var events []jsonEvent
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		var e jsonEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			h.t.Fatalf("not a JSON event: %q: %v", line, err)
		}
		events = append(events, e)
	}
	return events
}

// types lists the event types in order; every event must carry schema_version 1.
func (h *harness) types(events []jsonEvent) []string {
	h.t.Helper()
	var types []string
	for _, e := range events {
		if e["schema_version"] != float64(1) {
			h.t.Errorf("event %v: schema_version = %v, want 1", e, e["schema_version"])
		}
		types = append(types, e["type"].(string))
	}
	return types
}

func equal(t *testing.T, what string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

func contains(t *testing.T, what, text, sub string) {
	t.Helper()
	if !strings.Contains(text, sub) {
		t.Errorf("%s does not contain %q:\n%s", what, sub, text)
	}
}

// serveProc is one `agentx serve` run in a goroutine, driven through pipes.
// Every wait is on a channel or a pipe read, never a sleep: send writes a
// request line, next reads the next stdout event, close ends stdin and
// cancelRun ends the context; both return the exit code once Run returned.
type serveProc struct {
	t      *testing.T
	stdin  *os.File
	lines  chan string
	done   chan struct{} // closed once Run returned
	exit   int           // read only after done
	stderr bytes.Buffer  // read only after done
	cancel context.CancelFunc
}

const serveDeadline = 10 * time.Second

func (h *harness) serve(t *testing.T, args ...string) *serveProc {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &serveProc{t: t, stdin: inW, lines: make(chan string, 256), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(p.lines)
		defer outR.Close()
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
	}()
	go func() {
		p.exit = Run(ctx, append([]string{"serve"}, args...), h.env, inR, outW, &p.stderr)
		outW.Close()
		inR.Close()
		close(p.done)
	}()
	t.Cleanup(func() { // no serve outlives its test, whichever way the test ended
		cancel()
		inW.Close()
		p.wait()
	})
	return p
}

// send writes one request line to the child's stdin.
func (p *serveProc) send(line string) {
	p.t.Helper()
	if _, err := p.stdin.Write([]byte(line + "\n")); err != nil {
		p.t.Fatal(err)
	}
}

// next returns the next stdout event, which must be of type typ.
func (p *serveProc) next(typ string) jsonEvent {
	p.t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok {
			p.t.Fatalf("serve ended before a %s event", typ)
		}
		var e jsonEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			p.t.Fatalf("not a JSON event: %q: %v", line, err)
		}
		if e["schema_version"] != float64(1) {
			p.t.Errorf("event %v: schema_version = %v, want 1", e, e["schema_version"])
		}
		if e["type"] != typ {
			p.t.Fatalf("next event = %v, want type %s", e, typ)
		}
		return e
	case <-time.After(serveDeadline):
		p.t.Fatalf("no %s event within %s", typ, serveDeadline)
	}
	return nil
}

// line reads the next stdout line, which must be want: the text output of
// one event, where next reads the JSON one.
func (p *serveProc) line(want string) {
	p.t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok {
			p.t.Fatalf("serve ended before the line %q", want)
		}
		if line != want {
			p.t.Errorf("line = %q, want %q", line, want)
		}
	case <-time.After(serveDeadline):
		p.t.Fatalf("no line %q within %s", want, serveDeadline)
	}
}

// close ends stdin and returns the exit code once Run returned.
func (p *serveProc) close() int {
	p.t.Helper()
	p.stdin.Close()
	return p.wait()
}

// cancelRun cancels the context and returns the exit code once Run returned.
func (p *serveProc) cancelRun() int {
	p.t.Helper()
	p.cancel()
	return p.wait()
}

func (p *serveProc) wait() int {
	p.t.Helper()
	select {
	case <-p.done:
		return p.exit
	case <-time.After(serveDeadline):
		p.t.Fatalf("serve did not end within %s", serveDeadline)
	}
	return -1
}
