package cli

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/coder/websocket"
)

// doorClient is one client of the door of a serve started with --listen.
type doorClient struct {
	t  *testing.T
	ws *websocket.Conn
}

// dialDoor connects to the door a listening event names, as a browser at
// origin does.
func dialDoor(t *testing.T, listening jsonEvent, origin string) *doorClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), serveDeadline)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, listening["url"].(string)+"?token="+listening["token"].(string),
		&websocket.DialOptions{HTTPHeader: map[string][]string{"Origin": {origin}}})
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(-1)
	t.Cleanup(func() { _ = ws.CloseNow() })
	return &doorClient{t: t, ws: ws}
}

// send writes one message: a request with id, a notification with a nil
// one.
func (c *doorClient) send(id any, method string, params any) {
	c.t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		m["id"] = id
	}
	if params != nil {
		m["params"] = params
	}
	b, err := json.Marshal(m)
	if err != nil {
		c.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), serveDeadline)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

func (c *doorClient) next() jsonEvent {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runDeadline)
	defer cancel()
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		c.t.Fatal(err)
	}
	var m jsonEvent
	if err := json.Unmarshal(data, &m); err != nil {
		c.t.Fatalf("not a JSON message: %q: %v", data, err)
	}
	equal(c.t, "jsonrpc", m["jsonrpc"], "2.0")
	return m
}

// answer reads up to the response to the request with id. It returns the
// response's result, or its error, the events of the request's run and the
// events serve sent on its own meanwhile, each in the order it came.
func (c *doorClient) answer(id float64) (result, rpcError jsonEvent, run, own []jsonEvent) {
	c.t.Helper()
	for {
		m := c.next()
		if m["id"] == id {
			result, _ = m["result"].(map[string]any)
			rpcError, _ = m["error"].(map[string]any)
			return result, rpcError, run, own
		}
		equal(c.t, "method", m["method"], "event")
		params := m["params"].(map[string]any)
		ev, ok := params["event"].(map[string]any)
		if !ok {
			c.t.Fatalf("a line that is no event: %v", params)
		}
		checkEvent(c.t, ev)
		if params["id"] == id {
			run = append(run, ev)
		} else {
			equal(c.t, "id of an event of serve's own", params["id"], nil)
			own = append(own, ev)
		}
	}
}

// alike is equal for values that hold slices or maps.
func alike(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

// A browser on this machine does through the door what the desktop app
// does through its shell: it gets the inventory, runs commands, whose
// events and exit code come back, and asks for a refresh and a search.
// After a command that changed something, the result names the inventory
// that holds the change, which was sent before it. What serve answers
// stdin with stays on stdout.
func TestServeListenAnswersABrowser(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.env[cliChildEnv] = "1"
	p := h.serve(t, "--json", "--listen", "localhost", "--origin", "http://localhost:1420")
	listening := p.next("listening")
	inventory := p.next("inventory")
	c := dialDoor(t, listening, "http://localhost:1420")
	p.send(`{"type":"refresh","request_id":"r1"}`)
	p.next("refresh_complete")

	// The inventory serve last emitted is what a new client is sent first,
	// and the answer to stdin's refresh is not sent at all.
	c.send(1, "run", map[string]any{"args": []string{"version"}})
	result, _, run, own := c.answer(1)
	alike(t, "run events", h.types(run), []string{"version", "result"})
	alike(t, "own events", h.types(own), []string{"inventory"})
	equal(t, "inventory.scan_counter", own[0]["scan_counter"], float64(1))
	alike(t, "result", result, jsonEvent{"exit_code": float64(0), "cancelled": false, "timed_out": false, "instance_id": inventory["instance_id"]})

	c.send("new", "run", map[string]any{"args": []string{"skill", "new", "notes"}, "deadline_ms": 60000})
	for {
		m := c.next()
		if m["id"] == "new" {
			result = m["result"].(map[string]any)
			break
		}
		if ev := m["params"].(map[string]any)["event"].(map[string]any); ev["type"] == "inventory" {
			inventory = ev
		}
	}
	equal(t, "exit_code", result["exit_code"], float64(0))
	equal(t, "scan_counter", result["scan_counter"], float64(2))
	equal(t, "the inventory sent before the result", inventory["scan_counter"], float64(2))
	alike(t, "skills in it", libraryNames(inventory), []string{"notes"})

	c.send(3, "refresh", nil)
	result, _, _, _ = c.answer(3)
	alike(t, "refresh", result, jsonEvent{"instance_id": inventory["instance_id"], "scan_counter": float64(2)})

	c.send(4, "search", map[string]any{"query": "notes"})
	result, _, _, _ = c.answer(4)
	alike(t, "search", result, jsonEvent{"instance_id": inventory["instance_id"], "query": "notes", "results": []any{}})

	// A command that fails ran: its exit code is a result, and serve scans
	// after it, since it may have changed something before it failed.
	c.send(5, "run", map[string]any{"args": []string{"skill", "remove", "missing"}})
	result, _, run, _ = c.answer(5)
	alike(t, "run events", h.types(run), []string{"error", "result"})
	equal(t, "exit_code", result["exit_code"], float64(exitNotFound.exit))
	equal(t, "scan_counter", result["scan_counter"], float64(2))

	c.send(6, "run", map[string]any{"args": []string{}})
	_, rpcError, _, _ := c.answer(6)
	equal(t, "error.code", rpcError["code"], float64(-32602))
	c.send(7, "search", map[string]any{})
	_, rpcError, _, _ = c.answer(7)
	equal(t, "error.code", rpcError["code"], float64(-32602))

	// Nothing of the door reaches stdout but where it is.
	equal(t, "exit", p.close(), 0)
	p.next("inventory")
	p.next("result")
}

func libraryNames(inventory jsonEvent) []string {
	names := []string{}
	for _, e := range inventory["library"].([]any) {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	return names
}

// A cancel stops the command and the git it waits for, and the command
// says so itself: it ends with the exit code of a run that was stopped.
// So does serve ending, which leaves no command behind.
func TestServeListenStopsARunOnCancel(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.env[cliChildEnv] = "1"
	// The git that never answers leaves its process id behind, which is
	// how the test knows the command waits for it, and which process that is.
	waiting := filepath.Join(t.TempDir(), "waiting")
	stubGit(t, h, "#!/bin/sh\n"+hostPATH()+`case "$*" in *never.invalid*) echo $$ > `+shellWord(waiting)+`; exec sleep 60 ;; esac
exec git "$@"
`)
	waitingGit := func() (pid int) {
		awaitTrue(t, "the command to wait for its git", func() bool {
			b, _ := os.ReadFile(waiting)
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			return pid != 0
		})
		return pid
	}
	p := h.serve(t, "--json", "--listen", "127.0.0.1")
	c := dialDoor(t, p.next("listening"), "")
	c.send(1, "run", map[string]any{"args": []string{"source", "add", "https://never.invalid/skills.git"}})
	waitingGit()
	c.send(nil, "$/cancelRequest", map[string]any{"id": 1})
	result, _, run, _ := c.answer(1)
	equal(t, "cancelled", result["cancelled"], true)
	equal(t, "exit_code", result["exit_code"], float64(exitInterrupted.exit))
	equal(t, "scan_counter", result["scan_counter"], float64(1))
	equal(t, "the command's last event", run[len(run)-1]["ok"], false)

	// Serve ending stops a command the same way, and waits for it: by
	// the time serve has ended, the git the command waited for is gone.
	if err := os.Remove(waiting); err != nil {
		t.Fatal(err)
	}
	c.send(2, "run", map[string]any{"args": []string{"source", "add", "https://never.invalid/skills.git"}})
	git := waitingGit()
	equal(t, "exit", p.close(), 0)
	equal(t, "the git the command waited for", syscall.Kill(git, 0), error(syscall.ESRCH))
}

// What --listen and --origin refuse, each before serve takes its lock, and
// a port that is taken.
func TestServeListenRefusals(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	for _, tc := range []struct {
		args    []string
		exit    int
		message string
	}{
		{[]string{"--listen", "0.0.0.0"}, exitUsage.exit, "not a loopback address"},
		{[]string{"--listen", "localhost", "--once"}, exitUsage.exit, "--listen and --once cannot both be given"},
		{[]string{"--origin", "http://localhost:1420"}, exitUsage.exit, "--origin needs --listen"},
		{[]string{"--listen", "localhost", "--origin", "localhost:1420"}, exitUsage.exit, "not a web origin"},
		{[]string{"--listen", taken.Addr().String()}, exitRefused.exit, "cannot listen on " + taken.Addr().String()},
	} {
		out := h.run(append([]string{"serve", "--json"}, tc.args...)...)
		equal(t, "exit", out.exit, tc.exit)
		contains(t, "error", out.stdout, tc.message)
	}
}

func TestListenAddress(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]string{
		"localhost":      "127.0.0.1:0",
		"localhost:4319": "127.0.0.1:4319",
		"127.0.0.1":      "127.0.0.1:0",
		"127.0.0.1:0":    "127.0.0.1:0",
		"127.8.9.10:80":  "127.8.9.10:80",
		"::1":            "[::1]:0",
		"[::1]":          "[::1]:0",
		"[::1]:4319":     "[::1]:4319",
		"":               "",
		"4319":           "",
		":4319":          "",
		"0.0.0.0":        "",
		"192.168.1.5:80": "",
		"example.com":    "",
		"[::]:4319":      "",
		"localhost:http": "",
		"localhost:-1":   "",
		"localhost:":     "",
	} {
		got, err := listenAddress(value)
		if got != want || (err == nil) != (want != "") {
			t.Errorf("listenAddress(%q) = %q, %v, want %q", value, got, err, want)
		}
	}
}

func TestWebOrigin(t *testing.T) {
	t.Parallel()
	for value, ok := range map[string]bool{
		"http://localhost:1420":    true,
		"https://app.example.com":  true,
		"http://127.0.0.1:5173":    true,
		"http://[::1]:5173":        true,
		"http://localhost:1420/":   false,
		"http://localhost:1420/x":  false,
		"http://localhost:1420?x":  false,
		"http://user@localhost":    false,
		"HTTP://localhost:1420":    false,
		"http://LocalHost:1420":    false,
		"localhost:1420":           false,
		"ws://localhost:1420":      false,
		"file:///tmp/x":            false,
		"http://":                  false,
		"*":                        false,
		"null":                     false,
		"":                         false,
		"http://localhost:1420\n":  false,
		"http://localhost:1420 ":   false,
		"http://localhost:1420#":   false,
		"http://localhost:1420/#x": false,
	} {
		if err := webOrigin(value); (err == nil) != ok {
			t.Errorf("webOrigin(%q) = %v, want ok %t", value, err, ok)
		}
	}
}

// A line of a run is sent as the event it is, or as text when it is no
// JSON object; a blank one is not sent.
func TestRunLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want string // the params as JSON; empty for a line that is not sent
	}{
		{`{"type":"result","ok":true}`, `{"id":7,"stream":"stdout","event":{"type":"result","ok":true}}`},
		{"panic: runtime error", `{"id":7,"stream":"stdout","line":"panic: runtime error"}`},
		{`{"cut short`, `{"id":7,"stream":"stdout","line":"{\"cut short"}`},
		{"  ", ""},
	} {
		params, ok := runLine(json.RawMessage("7"), "stdout", []byte(tc.text))
		got, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		if ok != (tc.want != "") || ok && string(got) != tc.want {
			t.Errorf("runLine(%q) = %s, %t, want %s", tc.text, got, ok, tc.want)
		}
	}
}

// Every event the writer is given reaches tee, in text mode too: what
// stdout carries with --json, and the log events of stderr.
func TestWriterTeesEveryEvent(t *testing.T) {
	t.Parallel()
	var got []any
	w := &writer{stdout: io.Discard, stderr: io.Discard, verbose: true, tee: func(ev any) { got = append(got, ev) }}
	w.emit(newEvent("inventory"))
	w.warn("careful")
	w.warnWith("careful", "do this")
	w.debugf("seen %d", 1)
	alike(t, "teed", got, []any{
		newEvent("inventory"),
		logEvent{event: newEvent("log"), Level: levelWarn, Message: "careful"},
		logEvent{event: newEvent("log"), Level: levelWarn, Message: "careful; do this"},
		logEvent{event: newEvent("log"), Level: levelDebug, Message: "seen 1"},
	})
}

// --json is added where the command line takes it.
func TestJSONArgs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ args, want []string }{
		{[]string{"version"}, []string{"version", "--json"}},
		{[]string{"skill", "add", "--", "-x"}, []string{"skill", "add", "--json", "--", "-x"}},
		{[]string{"--json", "doctor"}, []string{"--json", "doctor"}},
	} {
		alike(t, "jsonArgs", jsonArgs(tc.args), tc.want)
	}
}
