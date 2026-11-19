// Package rpc is the WebSocket door of the serve child: a JSON-RPC 2.0
// server that takes one message per text frame from the clients that may
// connect, and answers each request from the method it names.
package rpc

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// The error codes of JSON-RPC 2.0, the code the Language Server Protocol
// gives a request that was cancelled, and the one code of this server's
// own, for a request it understood and could not carry out.
const (
	CodeParse          = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeFailed         = -32000
	CodeCancelled      = -32800
)

const (
	version      = "2.0"
	cancelMethod = "$/cancelRequest"

	readLimit    = 1 << 20          // bytes of one message a client sends
	queueLength  = 1024             // messages waiting for one client
	writeTimeout = 30 * time.Second // for one message to one client
)

// Error is the error of a response.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Call is one request while its method answers it.
type Call struct {
	ID     json.RawMessage // the id as the client wrote it
	Params json.RawMessage // empty when the request has none
	conn   *conn
}

// Notify sends a notification to the client that made the request, and to
// no other. It waits while that client is behind on reading, as a pipe
// does, and gives up when the client has gone.
func (c Call) Notify(method string, params any) {
	c.conn.send(encode(message{JSONRPC: version, Method: method, Params: raw(params)}))
}

// Method answers one request. ctx is done when the client cancels the
// request or disconnects, and when the server closes. A nil result is sent
// as null.
type Method func(ctx context.Context, c Call) (result any, err *Error)

// Options says who may connect and what they may ask.
type Options struct {
	Token   string            // what a client presents, as ?token= or as a bearer token
	Hosts   []string          // the values of Host the server answers to, in lower case
	Origins []string          // the web origins that may connect; a client that sends no Origin is no browser
	Methods map[string]Method // by name
}

// Server is the door. It is an http.Handler that takes a WebSocket at "/".
type Server struct {
	o      Options
	ctx    context.Context // done once Close is called
	cancel context.CancelFunc

	mu       sync.Mutex
	conns    map[*conn]struct{}
	retained []byte // the notification every new client is sent first
	closed   bool
	serving  sync.WaitGroup // one per connection
	queue    int            // messages that may wait for one client
}

// New makes a server; nothing runs until a client connects.
func New(o Options) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{o: o, ctx: ctx, cancel: cancel, conns: map[*conn]struct{}{}, queue: queueLength}
}

// ServeHTTP refuses a request that is not for this door, in this order: a
// path other than "/", a Host the server does not answer to, an Origin
// that is not allowed, and a missing or wrong token. Anything else must be
// a WebSocket handshake, and the connection is served until either side
// ends it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch origin := r.Header.Get("Origin"); {
	case r.URL.Path != "/":
		http.NotFound(w, r)
		return
	case !slices.Contains(s.o.Hosts, strings.ToLower(r.Host)):
		http.Error(w, "this host name is not served", http.StatusForbidden)
		return
	case origin != "" && !slices.Contains(s.o.Origins, origin):
		http.Error(w, "this origin may not connect", http.StatusForbidden)
		return
	case !s.authorised(r):
		http.Error(w, "the token is missing or wrong", http.StatusUnauthorized)
		return
	case s.stopped():
		http.Error(w, "serve is stopping", http.StatusServiceUnavailable)
		return
	}
	// The origin is checked above, against whole origins rather than the
	// host names the library compares.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return // Accept has answered
	}
	ws.SetReadLimit(readLimit)
	ctx, cancel := context.WithCancel(s.ctx)
	c := &conn{ws: ws, ctx: ctx, cancel: cancel, out: make(chan []byte, s.queue), methods: s.o.Methods, calls: map[string]context.CancelFunc{}}
	if !s.add(c) { // closed since it was asked
		cancel()
		_ = ws.CloseNow()
		return
	}
	defer s.serving.Done()
	c.serve()
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *Server) stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) authorised(r *http.Request) bool {
	token := r.URL.Query().Get("token")
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		token = bearer
	}
	return s.o.Token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.o.Token)) == 1
}

// add takes a new connection in and queues the retained notification for
// it, both under the lock a broadcast holds, so that the client misses no
// broadcast after the retained one and gets none before it.
func (s *Server) add(c *conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	s.serving.Add(1)
	if s.retained != nil {
		c.offer(s.retained)
	}
	return true
}

// Notify sends a notification to every client. It never waits: a client
// with a full queue is behind for good and is disconnected. With retain,
// the notification is also the first thing every later client is sent,
// until another one is retained.
func (s *Server) Notify(method string, params any, retain bool) {
	b := encode(message{JSONRPC: version, Method: method, Params: raw(params)})
	s.mu.Lock()
	defer s.mu.Unlock()
	if retain {
		s.retained = b
	}
	for c := range s.conns {
		c.offer(b)
	}
}

// Close cancels every request in progress, waits for their methods to
// return, and closes every connection. No client connects after it.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.serving.Wait()
}

// conn is one client.
type conn struct {
	ws      *websocket.Conn
	ctx     context.Context // done when the connection ends, from either side
	cancel  context.CancelFunc
	out     chan []byte // what the writer sends, in order
	methods map[string]Method

	mu      sync.Mutex
	calls   map[string]context.CancelFunc // the requests in progress, by id
	calling sync.WaitGroup
}

// serve reads messages until the connection ends, then stops what the
// client asked for and was not answered yet.
func (c *conn) serve() {
	written := make(chan struct{})
	go func() {
		defer close(written)
		c.write()
	}()
	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			break
		}
		if typ != websocket.MessageText {
			_ = c.ws.Close(websocket.StatusUnsupportedData, "send text frames")
			break
		}
		c.handle(data)
	}
	c.cancel()
	c.calling.Wait()
	<-written
	_ = c.ws.CloseNow()
}

// write sends the queue, one message per frame.
func (c *conn) write() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case b := <-c.out:
			ctx, cancel := context.WithTimeout(c.ctx, writeTimeout)
			err := c.ws.Write(ctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

// send queues b and waits for room.
func (c *conn) send(b []byte) {
	select {
	case c.out <- b:
	case <-c.ctx.Done():
	}
}

// offer queues b when there is room and ends the connection when there is
// none.
func (c *conn) offer(b []byte) {
	select {
	case c.out <- b:
	default:
		c.cancel()
	}
}

// message is every message of JSON-RPC 2.0: a request or a notification
// with a method, a response with a result or an error.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

var null = json.RawMessage("null")

func (c *conn) fail(id json.RawMessage, code int, text string) {
	c.send(encode(message{JSONRPC: version, ID: id, Error: &Error{Code: code, Message: text}}))
}

// handle takes one message: a request is answered on a goroutine of its
// own, so that a cancel for it can be read meanwhile; a cancel stops the
// request it names; any other notification, and a response, is ignored.
func (c *conn) handle(data []byte) {
	if b := bytes.TrimSpace(data); len(b) > 0 && b[0] == '[' {
		c.fail(null, CodeInvalidRequest, "batches are not supported: send one message per frame")
		return
	}
	var m message
	if !json.Valid(data) {
		c.fail(null, CodeParse, "the message is not JSON")
		return
	}
	if err := json.Unmarshal(data, &m); err != nil {
		c.fail(null, CodeInvalidRequest, "the message is not a request: "+err.Error())
		return
	}
	key, hasID := idKey(m.ID)
	switch {
	case m.Method == "" && (m.Result != nil || m.Error != nil):
		return // a response: this server asks its clients nothing
	case m.JSONRPC != version || m.Method == "":
		id := null
		if hasID {
			id = m.ID
		}
		c.fail(id, CodeInvalidRequest, `a message carries "jsonrpc": "2.0" and a method`)
		return
	case m.ID == nil:
		if m.Method == cancelMethod {
			var p struct {
				ID json.RawMessage `json:"id"`
			}
			if json.Unmarshal(m.Params, &p) == nil {
				if key, ok := idKey(p.ID); ok {
					c.stop(key)
				}
			}
		}
		return
	case !hasID:
		c.fail(null, CodeInvalidRequest, "the id of a request is a string or a number")
		return
	}
	method, ok := c.methods[m.Method]
	if !ok {
		c.fail(m.ID, CodeMethodNotFound, "unknown method "+m.Method)
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.mu.Lock()
	_, busy := c.calls[key]
	if !busy {
		c.calls[key] = cancel
		c.calling.Add(1)
	}
	c.mu.Unlock()
	if busy {
		cancel()
		c.fail(m.ID, CodeInvalidRequest, "a request with this id is in progress")
		return
	}
	go func() {
		defer c.calling.Done()
		result, rpcErr := method(ctx, Call{ID: m.ID, Params: m.Params, conn: c})
		c.mu.Lock()
		delete(c.calls, key)
		c.mu.Unlock()
		cancel()
		if rpcErr != nil {
			c.send(encode(message{JSONRPC: version, ID: m.ID, Error: rpcErr}))
			return
		}
		c.send(encode(message{JSONRPC: version, ID: m.ID, Result: raw(result)}))
	}()
}

func (c *conn) stop(key string) {
	c.mu.Lock()
	cancel := c.calls[key]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// idKey is what two spellings of one id share, so that a cancel finds its
// request however the client wrote the id. It reports false for anything
// but a string or a number.
func idKey(id json.RawMessage) (string, bool) {
	var v any
	if len(id) == 0 || json.Unmarshal(id, &v) != nil {
		return "", false
	}
	switch v := v.(type) {
	case string:
		return "s" + v, true
	case float64:
		b, _ := json.Marshal(v)
		return "n" + string(b), true
	}
	return "", false
}

// raw is v as JSON, null for nil.
func raw(v any) json.RawMessage {
	if v == nil {
		return null
	}
	return encode(v)
}

func encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // messages are plain structs and events; marshalling cannot fail
	}
	return b
}
