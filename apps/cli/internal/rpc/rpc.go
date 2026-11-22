// Package rpc is the JSON-RPC 2.0 server of the serve child, with its two
// doors: the WebSocket, which takes one message per frame from the clients
// that may connect, and the standard streams, which take one message per
// line from the process that started serve. Both answer each request from
// the method it names, in the same way.
package rpc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
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
	c.conn.put(Notification(method, params))
}

// Notification is a notification as it is sent: one JSON object, with no
// newline after it.
func Notification(method string, params any) []byte {
	return encode(message{JSONRPC: version, Method: method, Params: raw(params)})
}

// Method answers one request. ctx is done when the client cancels the
// request or disconnects, and when the server closes. A nil result is sent
// as null.
type Method func(ctx context.Context, c Call) (result any, err *Error)

// Options says who may connect and what they may ask.
type Options struct {
	Token   string            // what a client presents, as ?token=
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
	conns    map[*socket]struct{}
	retained []byte // the notification every new client is sent first
	closed   bool
	serving  sync.WaitGroup // one per connection
	queue    int            // messages that may wait for one client
}

// New makes a server; nothing runs until a client connects.
func New(o Options) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{o: o, ctx: ctx, cancel: cancel, conns: map[*socket]struct{}{}, queue: queueLength}
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
	c := &socket{ws: ws, out: make(chan []byte, s.queue)}
	c.conn = newConn(s.ctx, s.o.Methods, c.send)
	if !s.add(c) { // closed since it was asked
		c.cancel()
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

// authorised reports whether r carries the token, in the address: a
// browser cannot set a header on a WebSocket handshake.
func (s *Server) authorised(r *http.Request) bool {
	return subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(s.o.Token)) == 1
}

// add takes a new connection in and queues the retained notification for
// it, both under the lock a broadcast holds, so that the client misses no
// broadcast after the retained one and gets none before it.
func (s *Server) add(c *socket) bool {
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
	b := Notification(method, params)
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

// conn is one client, whichever door it came through: the requests it
// made that are in progress, and how a message reaches it.
type conn struct {
	ctx     context.Context // done when the connection ends, from either side
	cancel  context.CancelFunc
	methods map[string]Method
	put     func(b []byte) // sends one message to the client

	mu      sync.Mutex
	calls   map[string]context.CancelFunc // the requests in progress, by id
	calling sync.WaitGroup
}

func newConn(ctx context.Context, methods map[string]Method, put func([]byte)) *conn {
	ctx, cancel := context.WithCancel(ctx)
	return &conn{ctx: ctx, cancel: cancel, methods: methods, put: put, calls: map[string]context.CancelFunc{}}
}

// end stops what the client asked for and was not answered yet, as a
// cancel stops it, and waits for the methods to return. It cancels under
// mu, under which handle checks the context, so that no request is taken
// once the wait has begun.
func (c *conn) end() {
	c.mu.Lock()
	c.cancel()
	c.mu.Unlock()
	c.calling.Wait()
}

// socket is one client of the WebSocket door.
type socket struct {
	*conn
	ws  *websocket.Conn
	out chan []byte // what the writer sends, in order
}

// serve reads messages until the connection ends, then stops what the
// client asked for and was not answered yet.
func (c *socket) serve() {
	written := make(chan struct{})
	go func() {
		defer close(written)
		c.write()
	}()
	for {
		_, data, err := c.ws.Read(c.ctx)
		if err != nil {
			break
		}
		c.handle(data)
	}
	c.end()
	<-written
	_ = c.ws.CloseNow()
}

// write sends the queue, one message per frame.
func (c *socket) write() {
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
func (c *socket) send(b []byte) {
	select {
	case c.out <- b:
	case <-c.ctx.Done():
	}
}

// offer queues b when there is room and ends the connection when there is
// none.
func (c *socket) offer(b []byte) {
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
	c.put(encode(message{JSONRPC: version, ID: id, Error: &Error{Code: code, Message: text}}))
}

// handle takes one message: a request is answered on a goroutine of its
// own, so that a cancel for it can be read meanwhile; a cancel stops the
// request it names; any other notification, and a response, is ignored.
func (c *conn) handle(data []byte) {
	var m message
	if err := json.Unmarshal(data, &m); err != nil {
		code := CodeInvalidRequest // JSON that is no object, a batch among them
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			code = CodeParse
		}
		c.fail(null, code, "the message is not one JSON-RPC object: "+err.Error())
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
	ended := c.ctx.Err() != nil
	_, busy := c.calls[key]
	if !busy && !ended {
		c.calls[key] = cancel
		c.calling.Add(1)
	}
	c.mu.Unlock()
	if ended { // the connection ended while the message was read
		cancel()
		return
	}
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
			c.put(encode(message{JSONRPC: version, ID: m.ID, Error: rpcErr}))
			return
		}
		c.put(encode(message{JSONRPC: version, ID: m.ID, Result: raw(result)}))
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

// idKey is the id of a request as the key it is kept under, which is the
// id as the client wrote it. It reports false for anything but a string
// or a number.
func idKey(id json.RawMessage) (string, bool) {
	// A JSON string starts with a quote, a number with a digit or a minus.
	return string(id), len(id) > 0 && strings.IndexByte(`"-0123456789`, id[0]) >= 0
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
