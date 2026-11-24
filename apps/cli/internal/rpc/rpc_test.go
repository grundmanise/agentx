package rpc

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const token = "the-token"

// door is a server on a loopback port and what a client needs to reach it.
type door struct {
	*Server
	url  string // ws://127.0.0.1:<port>/
	http string // http://127.0.0.1:<port>
}

func open(t *testing.T, methods map[string]Method) *door {
	t.Helper()
	// The server learns its own address only once it listens.
	var s *Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.ServeHTTP(w, r) }))
	host := strings.TrimPrefix(ts.URL, "http://")
	methods = maps.Clone(methods)
	if methods == nil {
		methods = map[string]Method{}
	}
	methods["ping"] = func(context.Context, Call) (any, *Error) { return nil, nil }
	s = New(Options{Token: token, Hosts: []string{host}, Origins: []string{"http://localhost:1420"}, Methods: methods})
	t.Cleanup(func() {
		s.Close()
		ts.Close()
	})
	return &door{Server: s, url: "ws://" + host + "/", http: ts.URL}
}

// client is one connection of a test.
type client struct {
	t    *testing.T
	ws   *websocket.Conn
	kept []string // read by dial, not yet returned by next
}

// dial connects and waits for the answer to one request, so that the
// server has taken the connection in: it does that once the handshake is
// over, and a notification sent before that is not for this client. What
// the server sent before the answer, the retained notification, is kept
// for next.
func (d *door) dial(t *testing.T) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, d.url+"?token="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(-1)
	t.Cleanup(func() { _ = ws.CloseNow() })
	c := &client{t: t, ws: ws}
	c.send(`{"jsonrpc":"2.0","id":"dial","method":"ping"}`)
	for {
		m := c.read()
		if m == `{"jsonrpc":"2.0","id":"dial","result":null}` {
			return c
		}
		c.kept = append(c.kept, m)
	}
}

func (c *client) send(text string) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, []byte(text)); err != nil {
		c.t.Fatal(err)
	}
}

// next is the next message the server sent, as it sent it.
func (c *client) next() string {
	c.t.Helper()
	if len(c.kept) > 0 {
		m := c.kept[0]
		c.kept = c.kept[1:]
		return m
	}
	return c.read()
}

func (c *client) read() string {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		c.t.Fatal(err)
	}
	return string(data)
}

func (c *client) want(text string) {
	c.t.Helper()
	if got := c.next(); got != text {
		c.t.Fatalf("got  %s\nwant %s", got, text)
	}
}

// gone waits for the server to end the connection.
func (c *client) gone() {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, data, err := c.ws.Read(ctx); err == nil {
		c.t.Fatalf("the connection is open and carried %s", data)
	} else if ctx.Err() != nil {
		c.t.Fatal("the connection stayed open")
	}
}

// A request is answered with what its method returns, after the
// notifications the method sent meanwhile, and with the id as the client
// wrote it.
func TestARequestIsAnsweredAfterItsNotifications(t *testing.T) {
	t.Parallel()
	d := open(t, map[string]Method{
		"count": func(_ context.Context, c Call) (any, *Error) {
			c.Notify("event", map[string]any{"id": c.ID, "n": 1})
			return map[string]string{"params": string(c.Params)}, nil
		},
		"nothing": func(context.Context, Call) (any, *Error) { return nil, nil },
		"refuse":  func(context.Context, Call) (any, *Error) { return nil, &Error{Code: CodeFailed, Message: "no"} },
	})
	c := d.dial(t)
	c.send(`{"jsonrpc":"2.0","id":"a","method":"count","params":{"x":1}}`)
	c.want(`{"jsonrpc":"2.0","method":"event","params":{"id":"a","n":1}}`)
	c.want(`{"jsonrpc":"2.0","id":"a","result":{"params":"{\"x\":1}"}}`)
	c.send(`{"jsonrpc":"2.0","id":2,"method":"nothing"}`)
	c.want(`{"jsonrpc":"2.0","id":2,"result":null}`)
	c.send(`{"jsonrpc":"2.0","id":3,"method":"refuse"}`)
	c.want(`{"jsonrpc":"2.0","id":3,"error":{"code":-32000,"message":"no"}}`)
}

// What is no request gets the error the standard names for it, and the
// connection stays open; a notification the server does not know, and a
// response, get no answer at all.
func TestMessagesThatAreNoRequest(t *testing.T) {
	t.Parallel()
	d := open(t, map[string]Method{"ok": func(context.Context, Call) (any, *Error) { return true, nil }})
	c := d.dial(t)
	for _, tc := range []struct {
		send string
		code int
		id   string
	}{
		{`{"jsonrpc":"2.0","id":1,"method":"missing"}`, CodeMethodNotFound, "1"},
		{`not json`, CodeParse, "null"},
		{`5`, CodeInvalidRequest, "null"},
		{`[{"jsonrpc":"2.0","id":1,"method":"ok"}]`, CodeInvalidRequest, "null"},
		{`{"id":4,"method":"ok"}`, CodeInvalidRequest, "4"},
		{`{"jsonrpc":"2.0","id":5}`, CodeInvalidRequest, "5"},
		{`{"jsonrpc":"2.0","id":null,"method":"ok"}`, CodeInvalidRequest, "null"},
		{`{"jsonrpc":"2.0","id":{},"method":"ok"}`, CodeInvalidRequest, "null"},
	} {
		c.send(tc.send)
		var m struct {
			ID    json.RawMessage
			Error *Error
		}
		got := c.next()
		if err := json.Unmarshal([]byte(got), &m); err != nil || m.Error == nil || m.Error.Code != tc.code || string(m.ID) != tc.id {
			t.Errorf("%s was answered %s, want error %d for id %s", tc.send, got, tc.code, tc.id)
		}
	}
	c.send(`{"jsonrpc":"2.0","method":"$/unknown"}`)
	c.send(`{"jsonrpc":"2.0","id":9,"result":null}`)
	c.send(`{"jsonrpc":"2.0","id":10,"method":"ok"}`)
	c.want(`{"jsonrpc":"2.0","id":10,"result":true}`)
}

// waiting is a method that says it started and returns when its context
// is done, with the error of a cancelled request.
func waiting(started chan<- struct{}) Method {
	return func(ctx context.Context, _ Call) (any, *Error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, &Error{Code: CodeCancelled, Message: "cancelled"}
	}
}

// $/cancelRequest stops the request whose id it names and no other; a
// second request may not take an id in progress.
func TestCancelStopsTheRequestItNames(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 2)
	d := open(t, map[string]Method{"wait": waiting(started)})
	c := d.dial(t)
	c.send(`{"jsonrpc":"2.0","id":"a","method":"wait"}`)
	c.send(`{"jsonrpc":"2.0","id":7,"method":"wait"}`)
	<-started
	<-started
	c.send(`{"jsonrpc":"2.0","id":7,"method":"wait"}`)
	if got := c.next(); !strings.Contains(got, `"id":7,"error":{"code":-32600`) {
		t.Fatalf("a second request with an id in progress was answered %s", got)
	}
	c.send(`{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":"missing"}}`)
	c.send(`{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":"a"}}`)
	c.want(`{"jsonrpc":"2.0","id":"a","error":{"code":-32800,"message":"cancelled"}}`)
	c.send(`{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":7}}`)
	c.want(`{"jsonrpc":"2.0","id":7,"error":{"code":-32800,"message":"cancelled"}}`)
}

// A client that goes takes its requests with it, and so does the server
// closing, which waits for them.
func TestARequestEndsWithItsConnectionAndWithTheServer(t *testing.T) {
	t.Parallel()
	started, ended := make(chan struct{}, 2), make(chan struct{}, 2)
	d := open(t, map[string]Method{"wait": func(ctx context.Context, _ Call) (any, *Error) {
		started <- struct{}{}
		<-ctx.Done()
		ended <- struct{}{}
		return nil, nil
	}})
	c := d.dial(t)
	c.send(`{"jsonrpc":"2.0","id":1,"method":"wait"}`)
	<-started
	_ = c.ws.CloseNow()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the request went on after its client had gone")
	}

	c = d.dial(t)
	c.send(`{"jsonrpc":"2.0","id":1,"method":"wait"}`)
	<-started
	d.Close()
	select {
	case <-ended:
	default:
		t.Fatal("Close returned while a request was in progress")
	}
	c.gone()
	if _, _, err := websocket.Dial(context.Background(), d.url+"?token="+token, nil); err == nil {
		t.Fatal("a client connected to a closed server")
	}
}

// A notification of the server reaches every client, and the retained one
// is what a later client gets first.
func TestNotifyReachesEveryClientAndRetainsForLaterOnes(t *testing.T) {
	t.Parallel()
	d := open(t, nil)
	first := d.dial(t)
	d.Notify("event", map[string]int{"n": 1}, Retained)
	d.Notify("event", map[string]int{"n": 2}, Passing)
	first.want(`{"jsonrpc":"2.0","method":"event","params":{"n":1}}`)
	first.want(`{"jsonrpc":"2.0","method":"event","params":{"n":2}}`)
	second := d.dial(t)
	d.Notify("event", map[string]int{"n": 3}, Passing)
	second.want(`{"jsonrpc":"2.0","method":"event","params":{"n":1}}`)
	second.want(`{"jsonrpc":"2.0","method":"event","params":{"n":3}}`)
	first.want(`{"jsonrpc":"2.0","method":"event","params":{"n":3}}`)
}

// A note follows the retained notification for a later client, until the
// next retained one drops it.
func TestANoteFollowsTheRetainedNotificationUntilTheNext(t *testing.T) {
	t.Parallel()
	d := open(t, nil)
	d.Notify("event", map[string]int{"n": 1}, Note)
	first := d.dial(t)
	first.want(`{"jsonrpc":"2.0","method":"event","params":{"n":1}}`)
	d.Notify("event", map[string]int{"n": 2}, Retained)
	d.Notify("event", map[string]int{"n": 3}, Note)
	second := d.dial(t)
	second.want(`{"jsonrpc":"2.0","method":"event","params":{"n":2}}`)
	second.want(`{"jsonrpc":"2.0","method":"event","params":{"n":3}}`)
	d.Notify("event", map[string]int{"n": 4}, Retained)
	third := d.dial(t)
	d.Notify("event", map[string]int{"n": 5}, Passing)
	third.want(`{"jsonrpc":"2.0","method":"event","params":{"n":4}}`)
	third.want(`{"jsonrpc":"2.0","method":"event","params":{"n":5}}`)
}

// A standing notification is sent to a later client after the retained
// one and its note, alone when nothing is retained. The next retained
// notification drops the note and leaves it, and the next standing one
// replaces it.
func TestAStandingNotificationOutlastsTheRetainedOne(t *testing.T) {
	t.Parallel()
	d := open(t, nil)
	d.Notify("event", map[string]int{"n": 1}, Standing)
	first := d.dial(t)
	first.want(`{"jsonrpc":"2.0","method":"event","params":{"n":1}}`)
	d.Notify("event", map[string]int{"n": 2}, Retained)
	d.Notify("event", map[string]int{"n": 3}, Note)
	second := d.dial(t)
	second.want(`{"jsonrpc":"2.0","method":"event","params":{"n":2}}`)
	second.want(`{"jsonrpc":"2.0","method":"event","params":{"n":3}}`)
	second.want(`{"jsonrpc":"2.0","method":"event","params":{"n":1}}`)
	d.Notify("event", map[string]int{"n": 4}, Retained)
	third := d.dial(t)
	third.want(`{"jsonrpc":"2.0","method":"event","params":{"n":4}}`)
	third.want(`{"jsonrpc":"2.0","method":"event","params":{"n":1}}`)
	d.Notify("event", map[string]int{"n": 5}, Standing)
	fourth := d.dial(t)
	d.Notify("event", map[string]int{"n": 6}, Passing)
	fourth.want(`{"jsonrpc":"2.0","method":"event","params":{"n":4}}`)
	fourth.want(`{"jsonrpc":"2.0","method":"event","params":{"n":5}}`)
	fourth.want(`{"jsonrpc":"2.0","method":"event","params":{"n":6}}`)
}

// A client that reads nothing is disconnected once its queue is full, and
// the others are not held up by it.
func TestAClientThatFallsBehindIsDisconnected(t *testing.T) {
	t.Parallel()
	d := open(t, nil)
	d.queue = 16
	slow, quick := d.dial(t), d.dial(t)
	big := strings.Repeat("x", 64<<10)
	for range 400 { // more than the sockets and the queue hold between them
		d.Notify("event", big, Passing)
		quick.next()
	}
	for { // the slow one gets what was sent before it fell behind, then the end
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _, err := slow.ws.Read(ctx)
		late := ctx.Err() != nil
		cancel()
		if late {
			t.Fatal("the slow client stayed connected")
		}
		if err != nil {
			return
		}
	}
}

// Who may connect: a client with the token, on the host the server answers
// to, from an allowed origin or from none.
func TestWhoMayConnect(t *testing.T) {
	t.Parallel()
	d := open(t, nil)
	host := strings.TrimPrefix(d.http, "http://")
	for _, tc := range []struct {
		name   string
		path   string
		host   string
		header http.Header
		status int
	}{
		{"another path", "/rpc?token=" + token, host, nil, http.StatusNotFound},
		{"a name that resolves here", "/?token=" + token, "attacker.example", nil, http.StatusForbidden},
		{"an origin that is not allowed", "/?token=" + token, host, http.Header{"Origin": {"http://localhost:9999"}}, http.StatusForbidden},
		{"the null origin", "/?token=" + token, host, http.Header{"Origin": {"null"}}, http.StatusForbidden},
		{"no token", "/", host, nil, http.StatusUnauthorized},
		{"a wrong token", "/?token=wrong", host, nil, http.StatusUnauthorized},
	} {
		req, err := http.NewRequest(http.MethodGet, d.http+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = tc.host
		for k, v := range tc.header {
			req.Header[k] = v
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, res.StatusCode, tc.status)
		}
	}
	for name, header := range map[string]http.Header{
		"an allowed origin": {"Origin": {"http://localhost:1420"}},
		"no origin":         nil,
	} {
		ws, _, err := websocket.Dial(context.Background(), d.url+"?token="+token, &websocket.DialOptions{HTTPHeader: header})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		_ = ws.CloseNow()
	}
}
