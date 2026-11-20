package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/proc"
	"github.com/grundmanise/agentx/apps/cli/internal/rpc"
	"github.com/grundmanise/agentx/apps/cli/internal/serve"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// listeningEvent says where the WebSocket door of a serve started with
// --listen is, and the token a client presents to it. It is written to
// stdout alone: the token is never sent through the door and never logged.
type listeningEvent struct {
	event
	URL   string `json:"url"`   // what a client connects to: ws://<host>:<port>/, the port being the one that was free when none was asked for
	Token string `json:"token"` // random for each serve process
}

const (
	listenHint = "pass a loopback address, such as 127.0.0.1, localhost:4319 or [::1]"
	originHint = "pass a web origin as a browser sends it, such as http://localhost:1420, with nothing after the host and the port"
)

// listenAddress is the address --listen names, as net.Listen takes it: a
// loopback address, 127.0.0.1 for localhost, with the port after it, 0
// when none is given, which is any free port. Any other address is
// refused: the door is for this machine alone.
func listenAddress(value string) (string, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		host, port = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"), "0"
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("--listen %s is not a loopback address", value)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", fmt.Errorf("--listen %s has no valid port", value)
	}
	return net.JoinHostPort(ip.String(), port), nil
}

// webOrigin is nil for an origin as a browser sends it in the Origin
// header: scheme://host, with the port when it is not the scheme's own,
// in lower case and with nothing after it. Origins are compared whole, so
// one written any other way would match no browser.
func webOrigin(value string) error {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Scheme+"://"+u.Host != value || strings.ToLower(value) != value {
		return fmt.Errorf("--origin %s is not a web origin", value)
	}
	return nil
}

// serveDoor is the WebSocket door of one serve process: the JSON-RPC
// server, the listener it answers on, and what its methods need.
type serveDoor struct {
	inv      *invocation
	rpc      *rpc.Server
	http     *http.Server
	requests *serve.Requests // what the methods ask of the serve loop
	self     string          // the agentx executable a run starts
	env      []string        // the environment a run gets: serve's own
}

// openDoor listens on address, an address listenAddress returned, reports
// where with a listening event, and only then starts to send every event
// serve emits on its own through the door as well, so the token goes to
// stdout alone. The inventory is retained: it is the first thing a client
// that connects later is sent.
func (inv *invocation) openDoor(address string, origins []string) (*serveDoor, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fail(exitRefused, "cannot listen on "+address+": "+err.Error(), "pass another port, or none for one that is free")
	}
	token := rand.Text()
	at := l.Addr().(*net.TCPAddr)
	bound := at.String()
	d := &serveDoor{inv: inv, requests: serve.NewRequests(), self: self}
	for k, v := range inv.env {
		d.env = append(d.env, k+"="+v)
	}
	d.rpc = rpc.New(rpc.Options{
		Token:   token,
		Hosts:   []string{bound, net.JoinHostPort("localhost", strconv.Itoa(at.Port))},
		Origins: origins,
		Methods: map[string]rpc.Method{"run": d.run, "refresh": d.refresh, "search": d.search},
	})
	d.http = &http.Server{Handler: d.rpc, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = d.http.Serve(l) }() // ends when close closes the listener

	ev := listeningEvent{event: newEvent("listening"), URL: "ws://" + bound + "/", Token: token}
	inv.out.emit(ev)
	inv.out.print(inv.out.paint(heading, "listening"), ": ", ev.URL)
	inv.out.print(inv.out.paint(heading, "token"), ": ", ev.Token)
	inv.out.tee = d.own
	return d, nil
}

// close stops the door: no client connects any more, every run in
// progress is stopped as a cancel stops it and waited for, so that no
// command serve started outlives it, and every connection is closed.
func (d *serveDoor) close() {
	_ = d.http.Close()
	d.rpc.Close()
}

// eventParams are the params of an event notification: one event, or one
// line that is no JSON object, of the run whose request has the id, or
// one event of serve's own, which has none.
type eventParams struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Stream string          `json:"stream"`
	Event  any             `json:"event,omitempty"`
	Line   *string         `json:"line,omitempty"`
}

// own sends one event serve emitted on its own to every client, a log
// event as one of stderr. The answers to stdin's requests are for whoever
// wrote to stdin, so those are not sent.
func (d *serveDoor) own(ev any) {
	stream := proc.Stdout
	switch ev.(type) {
	case refreshCompleteEvent, searchEvent, errorEvent:
		return
	case logEvent:
		stream = proc.Stderr
	}
	_, inventory := ev.(inventoryEvent)
	d.rpc.Notify("event", eventParams{Stream: stream, Event: ev}, inventory)
}

// runLine is the event notification for one line a run wrote on stream:
// the line as the event it is when it is a JSON object, and as text
// otherwise. A blank line is not sent, which ok reports.
func runLine(id json.RawMessage, stream string, text []byte) (params eventParams, ok bool) {
	params = eventParams{ID: id, Stream: stream}
	switch line := bytes.TrimSpace(text); {
	case len(line) == 0:
		return params, false
	case line[0] == '{' && json.Valid(line):
		params.Event = json.RawMessage(line)
	default:
		params.Line = ptr(string(text))
	}
	return params, true
}

// runResult is the result of run. The command ran, whatever its exit
// code.
type runResult struct {
	ExitCode  *int `json:"exit_code"` // null when a signal ended the command
	Cancelled bool `json:"cancelled"`
	TimedOut  bool `json:"timed_out"`
}

// run is the method run: one agentx command line as a child process, the
// same executable with serve's environment and working directory, each
// line it writes sent on as an event notification with the request's id.
// A cancel of the request or a passed deadline stops the command and
// everything it started. The request names no environment and no
// directory: whoever could set those could have git run any program.
//
// Serve does not scan for the command: a client that needs the inventory
// to hold what the command changed asks for a refresh, as the desktop app
// does after a command of its shell.
func (d *serveDoor) run(ctx context.Context, c rpc.Call) (any, *rpc.Error) {
	var p struct {
		Args       []string `json:"args"`
		DeadlineMS int64    `json:"deadline_ms"`
	}
	if err := json.Unmarshal(c.Params, &p); err != nil || len(p.Args) == 0 || p.DeadlineMS < 0 {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: `run takes {"args": ["<word>", ...]}, with "deadline_ms" for a deadline`}
	}
	res, err := proc.Run(ctx, proc.Command{
		Path: d.self, Args: jsonArgs(p.Args), Env: d.env,
		Deadline: time.Duration(p.DeadlineMS) * time.Millisecond,
	}, func(stream string, text []byte) {
		if params, ok := runLine(c.ID, stream, text); ok {
			c.Notify("event", params)
		}
	})
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeFailed, Message: "the command could not start: " + err.Error()}
	}
	return runResult{ExitCode: res.ExitCode, Cancelled: res.Cancelled, TimedOut: res.TimedOut}, nil
}

// jsonArgs is args with --json added, before a -- when there is one,
// unless it is there already.
func jsonArgs(args []string) []string {
	if slices.Contains(args, "--json") {
		return args
	}
	at := slices.Index(args, "--")
	if at < 0 {
		at = len(args)
	}
	return slices.Insert(slices.Clone(args), at, "--json")
}

// refreshResult is the result of refresh: what a refresh_complete event
// carries for a scan that succeeded.
type refreshResult struct {
	InstanceID  string `json:"instance_id"`
	ScanCounter int    `json:"scan_counter"`
}

// refresh is the method refresh: stdin's refresh request, answered with
// its result once a scan that began after the request has ended.
func (d *serveDoor) refresh(ctx context.Context, _ rpc.Call) (any, *rpc.Error) {
	counter, err := d.requests.Refresh(ctx)
	switch {
	case ctx.Err() != nil:
		return nil, &rpc.Error{Code: rpc.CodeCancelled, Message: "the request was cancelled"}
	case err != nil:
		return nil, &rpc.Error{Code: rpc.CodeFailed, Message: "the scan failed: " + err.Error()}
	}
	return refreshResult{InstanceID: d.inv.instanceID(), ScanCounter: counter}, nil
}

// searchResult is the result of search: what a search event carries.
type searchResult struct {
	InstanceID string         `json:"instance_id"`
	Query      string         `json:"query"`
	Results    []source.Match `json:"results"` // empty, never absent, when nothing matched
}

// search is the method search: stdin's search request, answered from the
// index in memory.
func (d *serveDoor) search(_ context.Context, c rpc.Call) (any, *rpc.Error) {
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(c.Params, &p); err != nil || p.Query == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: `search takes {"query": "<text>"}`}
	}
	return searchResult{InstanceID: d.inv.instanceID(), Query: p.Query, Results: d.requests.Search(p.Query)}, nil
}
