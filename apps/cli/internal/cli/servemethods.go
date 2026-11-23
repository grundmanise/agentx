package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"slices"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/proc"
	"github.com/grundmanise/agentx/apps/cli/internal/rpc"
	"github.com/grundmanise/agentx/apps/cli/internal/serve"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// serveMethods are the JSON-RPC methods of one serve process, the same on
// its stdin and through its WebSocket door, and what they need.
type serveMethods struct {
	inv      *invocation
	requests *serve.Requests // what refresh and search ask of the serve loop
	self     string          // the agentx executable a run starts
	env      []string        // the environment a run gets: serve's own
}

func (inv *invocation) newServeMethods() (*serveMethods, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	m := &serveMethods{inv: inv, requests: serve.NewRequests(), self: self}
	for k, v := range inv.env {
		m.env = append(m.env, k+"="+v)
	}
	return m, nil
}

// table is the methods by name.
func (m *serveMethods) table() map[string]rpc.Method {
	return map[string]rpc.Method{"run": m.run, "refresh": m.refresh, "search": m.search}
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

// eventNotifications is serve's stdout under --json: every event line the
// writer writes to it goes out as an event notification with no id, so that
// everything on stdout is a JSON-RPC message, the result serve ends with
// included. The writer writes each line in one Write, under its lock, so
// one Write is one event.
type eventNotifications struct{ out io.Writer }

func (n eventNotifications) Write(p []byte) (int, error) {
	line := rpc.Notification("event", eventParams{Stream: proc.Stdout, Event: json.RawMessage(bytes.TrimSuffix(p, []byte("\n")))})
	if _, err := n.out.Write(append(line, '\n')); err != nil {
		return 0, err
	}
	return len(p), nil
}

// lockedWriter writes to out under the lock of w, as w writes every line,
// so that a message of the stdin door never lands inside a line of serve's
// own, nor one of those inside a message.
type lockedWriter struct {
	w   *writer
	out io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.w.write(l.out, string(p))
	return len(p), nil
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

// maxDeadlineMS is the longest deadline a run takes, in milliseconds: a
// longer one would overflow a time.Duration and run with no deadline at all.
const maxDeadlineMS = int64(math.MaxInt64 / time.Millisecond)

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
func (m *serveMethods) run(ctx context.Context, c rpc.Call) (any, *rpc.Error) {
	var p struct {
		Args       []string `json:"args"`
		DeadlineMS int64    `json:"deadline_ms"`
	}
	if err := json.Unmarshal(c.Params, &p); err != nil || len(p.Args) == 0 || p.DeadlineMS < 0 || p.DeadlineMS > maxDeadlineMS {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: `run takes {"args": ["<word>", ...]}, with "deadline_ms" for a deadline`}
	}
	res, err := proc.Run(ctx, proc.Command{
		Path: m.self, Args: jsonArgs(p.Args), Env: m.env,
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

// refreshResult is the result of refresh, for a scan that succeeded.
type refreshResult struct {
	InstanceID  string `json:"instance_id"`
	ScanCounter int    `json:"scan_counter"` // the counter of the current inventory, changed or not
}

// refresh is the method refresh, answered with its result once a scan that
// began after the request has ended. A serve that found another serve
// holding its lock runs no scan, and answers with what keeps it from it.
func (m *serveMethods) refresh(ctx context.Context, _ rpc.Call) (any, *rpc.Error) {
	counter, err := m.requests.Refresh(ctx)
	switch {
	case ctx.Err() != nil:
		return nil, &rpc.Error{Code: rpc.CodeCancelled, Message: "the request was cancelled"}
	case errors.Is(err, home.ErrServing):
		return nil, &rpc.Error{Code: rpc.CodeFailed, Message: err.Error()}
	case err != nil:
		return nil, &rpc.Error{Code: rpc.CodeFailed, Message: "the scan failed: " + err.Error()}
	}
	return refreshResult{InstanceID: m.inv.instanceID(), ScanCounter: counter}, nil
}

// searchResult is the result of search: every skill of every source whose
// name or description matches, each naming its source by the canonical
// URL, which is what source list prints and what an install takes.
type searchResult struct {
	InstanceID string         `json:"instance_id"`
	Query      string         `json:"query"`
	Results    []source.Match `json:"results"` // empty, never absent, when nothing matched
}

// search is the method search, answered from the index in memory once
// serve's first scan has built it.
func (m *serveMethods) search(ctx context.Context, c rpc.Call) (any, *rpc.Error) {
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(c.Params, &p); err != nil || p.Query == "" {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: `search takes {"query": "<text>"}`}
	}
	results, err := m.requests.Search(ctx, p.Query)
	if err != nil {
		return nil, &rpc.Error{Code: rpc.CodeCancelled, Message: "the request was cancelled"}
	}
	return searchResult{InstanceID: m.inv.instanceID(), Query: p.Query, Results: results}, nil
}
