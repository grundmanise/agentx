package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// protocolVersion is the last MCP revision that opens with an initialize
// request; the server may answer with an earlier one, which is accepted as
// is. Revisions from 2026-07-28 on carry the version on every request
// instead; a server implementing only those rejects initialize.
const protocolVersion = "2025-11-25"

// Item is one tool, prompt, resource or resource template a server exposes.
type Item struct {
	Kind        string // tool, prompt, resource or resource_template
	Name        string
	Description string
	Hash        string // hex SHA-256 over the item's serialisation, see signature

	serialised string
}

// Result is what a handshake found: the items sorted by kind then name, and
// the signature over all of them.
type Result struct {
	Signature string
	Items     []Item
}

// The reasons a handshake fails, for the hint that goes with the warning.
const (
	ReasonUnauthorized = "unauthorized" // the server refused the request for want of credentials
	ReasonUnreachable  = "unreachable"  // nothing answers at the URL
	ReasonNotFound     = "not_found"    // the command does not exist
	ReasonTimeout      = "timeout"      // the budget ran out
	ReasonFailed       = "failed"       // the server started or answered, but not as MCP asks
)

// reasoned is a failure whose reason is known where it happens.
type reasoned struct {
	reason string
	error
}

func (e *reasoned) Unwrap() error { return e.error }

// httpError is an HTTP response outside 2xx, with the JSON-RPC error its
// body carries, if any.
type httpError struct {
	code   int
	status string
	rpc    *rpcError
}

func (e *httpError) Error() string {
	if e.rpc != nil {
		return "HTTP " + e.status + ": " + e.rpc.Error()
	}
	return "HTTP " + e.status
}

// refused reads the error of a response outside 2xx and closes its body.
func refused(resp *http.Response) *httpError {
	defer resp.Body.Close()
	e := &httpError{code: resp.StatusCode, status: resp.Status}
	var m message
	if b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10)); err == nil && json.Unmarshal(b, &m) == nil {
		e.rpc = m.Error
	}
	return e
}

// Reason classifies the error of a handshake as one of the reasons above.
func Reason(err error) string {
	var r *reasoned
	var h *httpError
	switch {
	case errors.As(err, &r):
		return r.reason
	case errors.As(err, &h):
		switch h.code {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ReasonUnauthorized
		case http.StatusNotFound, http.StatusGone:
			if h.rpc == nil { // with one, an MCP server answered
				return ReasonUnreachable
			}
		}
		return ReasonFailed
	case errors.Is(err, context.DeadlineExceeded):
		return ReasonTimeout
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return ReasonNotFound
	}
	return ReasonFailed
}

// Hint is what the user can do about a handshake of s that failed for
// reason. It names no value from the declaration.
func Hint(reason string, s Server) string {
	switch reason {
	case ReasonUnauthorized:
		if s.bearerVar != "" {
			return "the server refused the bearer token; set " + s.bearerVar + " in the environment agentx runs in, or renew the token it holds"
		}
		if len(s.HeaderKeys) > 0 {
			return "the server refused the declared headers; renew the key or token they carry"
		}
		return "the server wants a sign-in; agentx sends only the headers a declaration sets and cannot use the sign-in an agent client holds, so declare a header with an API key to have the tools listed"
	case ReasonUnreachable:
		return "start the server or check the URL in the declaration, and the proxy settings if the server is remote"
	case ReasonNotFound:
		return "check the command in the declaration; a bare name is looked up on the PATH of agentx, and a relative path resolves against the declared cwd, else the directory agentx runs in"
	case ReasonTimeout:
		return "the server may be slow to start; raise the budget with AGENTX_HANDSHAKE_TIMEOUT, such as 30s"
	}
	return "run the server's command or request its URL by hand to see what it answers"
}

// Handshake starts or connects to s, lists what it exposes and returns the
// items with their signature. The declared values are expanded from env as
// the client that declares them does. A stdio server runs with env plus its
// declared environment; a remote server receives the headers of
// requestHeaders. A
// streamable-http server that refuses the opening POST with 400, 404 or 405
// is tried once more on the legacy HTTP+SSE transport at the same URL, as
// the protocol's backwards compatibility asks. ctx bounds the whole
// exchange; a deadline that passes is ctx.Err(). An error never carries a
// value from the declaration.
func Handshake(ctx context.Context, s Server, env map[string]string, clientVersion string) (Result, error) {
	s = s.expanded(env)
	var c conn
	var err error
	switch s.Transport {
	case "stdio":
		c, err = startStdio(ctx, s, env)
	case "sse":
		c, err = openSSE(ctx, s.URL, requestHeaders(s, env))
	default:
		c = newHTTP(s.URL, requestHeaders(s, env))
	}
	if err != nil {
		return Result{}, err
	}
	defer func() { c.close() }()

	init, err := initialize(ctx, c, clientVersion)
	if h, ok := c.(*httpConn); ok && legacy(err) {
		// The original error stands when the URL opens no event stream.
		if sse, serr := openSSE(ctx, h.url, h.headers); serr == nil {
			c.close()
			c = sse
			init, err = initialize(ctx, c, clientVersion)
		}
	}
	if err != nil {
		return Result{}, err
	}
	c.initialized(init.ProtocolVersion)
	if err := c.notify(ctx, "notifications/initialized"); err != nil {
		return Result{}, err
	}
	var items []Item
	for _, l := range lists {
		if _, ok := init.Capabilities[l.capability]; !ok {
			continue
		}
		found, err := list(ctx, c, l)
		if err != nil {
			return Result{}, err
		}
		items = append(items, found...)
	}
	return signature(items), nil
}

// initializeResult is the part of the initialize result a handshake reads.
type initializeResult struct {
	ProtocolVersion string                     `json:"protocolVersion"`
	Capabilities    map[string]json.RawMessage `json:"capabilities"`
}

// initialize sends the initialize request.
func initialize(ctx context.Context, c conn, clientVersion string) (initializeResult, error) {
	var init initializeResult
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agentx", "version": clientVersion},
	}
	err := c.call(ctx, "initialize", params, &init)
	return init, err
}

// legacy is whether err is how a server on the legacy HTTP+SSE transport
// refuses the POST that opens a streamable HTTP session: 400, 404 or 405
// without a JSON-RPC error, which is how a current server refuses a method.
func legacy(err error) bool {
	var h *httpError
	return errors.As(err, &h) && h.rpc == nil &&
		(h.code == http.StatusBadRequest || h.code == http.StatusNotFound || h.code == http.StatusMethodNotAllowed)
}

// signature sorts items by kind, name and serialisation and computes the
// signature: SHA-256 over the items' serialisations joined by NUL. An item's
// serialisation is its kind, name, description, input schema and output
// schema joined by NUL, the schemas as canonical JSON, empty where absent;
// its hash is SHA-256 over that serialisation.
func signature(items []Item) Result {
	slices.SortFunc(items, func(a, b Item) int {
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.serialised, b.serialised)
	})
	h := sha256.New()
	for i, it := range items {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(it.serialised))
	}
	return Result{Signature: hex.EncodeToString(h.Sum(nil)), Items: items}
}

// lists are the listing methods, each behind a capability of the initialize
// result; a method the server does not implement lists nothing.
var lists = []listing{
	{"tool", "tools/list", "tools", "tools"},
	{"prompt", "prompts/list", "prompts", "prompts"},
	{"resource", "resources/list", "resources", "resources"},
	{"resource_template", "resources/templates/list", "resources", "resourceTemplates"},
}

type listing struct {
	kind, method, capability, field string
}

type entry struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

// item serialises e as signature documents and hashes it.
func (l listing) item(e entry) (Item, error) {
	input, err := canonical(e.InputSchema)
	if err != nil {
		return Item{}, fmt.Errorf("%s: %s %q has a malformed input schema", l.method, l.kind, e.Name)
	}
	output, err := canonical(e.OutputSchema)
	if err != nil {
		return Item{}, fmt.Errorf("%s: %s %q has a malformed output schema", l.method, l.kind, e.Name)
	}
	serialised := strings.Join([]string{l.kind, e.Name, e.Description, input, output}, "\x00")
	sum := sha256.Sum256([]byte(serialised))
	return Item{Kind: l.kind, Name: e.Name, Description: e.Description, Hash: hex.EncodeToString(sum[:]), serialised: serialised}, nil
}

// canonical renders raw JSON with object keys sorted, no whitespace, numbers
// as written and only the escapes JSON requires; empty for absent input.
func canonical(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// conn is one JSON-RPC connection to a server: a process on stdio or an
// HTTP endpoint.
type conn interface {
	// call sends a request and decodes the matching response's result into
	// out; a JSON-RPC error response is an *rpcError.
	call(ctx context.Context, method string, params any, out any) error
	notify(ctx context.Context, method string) error
	// initialized records the negotiated protocol version.
	initialized(version string)
	close()
}

// list pages through one listing method until the server sends no cursor.
func list(ctx context.Context, c conn, l listing) ([]Item, error) {
	var items []Item
	params := map[string]any{}
	for {
		var page map[string]json.RawMessage
		err := c.call(ctx, l.method, params, &page)
		var rpc *rpcError
		if errors.As(err, &rpc) && rpc.Code == methodNotFound {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var entries []entry
		if raw, ok := page[l.field]; ok {
			if err := json.Unmarshal(raw, &entries); err != nil {
				return nil, fmt.Errorf("%s: %s is not a list", l.method, l.field)
			}
		}
		for _, e := range entries {
			it, err := l.item(e)
			if err != nil {
				return nil, err
			}
			items = append(items, it)
		}
		var cursor string
		if raw, ok := page["nextCursor"]; !ok || json.Unmarshal(raw, &cursor) != nil || cursor == "" {
			return items, nil
		}
		params = map[string]any{"cursor": cursor}
	}
}

const methodNotFound = -32601

// message is a JSON-RPC 2.0 request, notification or response.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	msg := strings.Join(strings.Fields(e.Message), " ")
	if len(msg) > 120 {
		msg = msg[:120] + "..."
	}
	return fmt.Sprintf("server error %d: %s", e.Code, msg)
}

func request(id int64, method string, params any) message {
	return message{JSONRPC: "2.0", ID: json.RawMessage(strconv.FormatInt(id, 10)), Method: method, Params: params}
}

// decode reads the result of the response to id from m; false when m is
// another message, such as a notification or a request from the server.
func (m message) decode(id int64, out any) (bool, error) {
	if string(m.ID) != strconv.FormatInt(id, 10) || m.Method != "" {
		return false, nil
	}
	if m.Error != nil {
		return true, m.Error
	}
	if len(m.Result) == 0 {
		return true, nil
	}
	if err := json.Unmarshal(m.Result, out); err != nil {
		return true, errors.New("result is not an object")
	}
	return true, nil
}

// maxMessage bounds one message from a server; a listing larger than this is
// refused rather than buffered.
const maxMessage = 16 << 20

// stdioConn is a server process speaking newline-delimited JSON-RPC.
type stdioConn struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	in     io.WriteCloser
	out    *os.File // the read end of the server's stdout
	lines  *bufio.Scanner
	next   int64
}

// startStdio starts the server in its declared working directory, else in
// the one agentx runs in. A bare command is looked up on the PATH of the
// agentx process, as any subprocess is.
func startStdio(ctx context.Context, s Server, env map[string]string) (*stdioConn, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	cmd.Dir = s.cwd // a relative command resolves against it
	merged := maps.Clone(env)
	maps.Copy(merged, s.env)
	cmd.Env = []string{} // nil would inherit the process environment
	for _, k := range slices.Sorted(maps.Keys(merged)) {
		cmd.Env = append(cmd.Env, k+"="+merged[k])
	}
	// Stderr stays on the null device: a server may log what it was given.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = time.Second
	out, w, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, err
	}
	cmd.Stdout = w
	in, err := cmd.StdinPipe()
	if err == nil {
		err = cmd.Start()
	}
	w.Close() // the server holds the write end now
	if err != nil {
		cancel()
		out.Close()
		return nil, err
	}
	// A server, or a child it leaves behind, that keeps stdout open past
	// the deadline must not block the scan: the pending read fails instead.
	context.AfterFunc(ctx, func() { _ = out.SetReadDeadline(time.Now()) })
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 64<<10), maxMessage)
	return &stdioConn{cmd: cmd, cancel: cancel, in: in, out: out, lines: lines}, nil
}

func (c *stdioConn) send(m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := c.in.Write(append(b, '\n')); err != nil {
		return errors.New("the server exited")
	}
	return nil
}

func (c *stdioConn) call(ctx context.Context, method string, params any, out any) error {
	c.next++
	if err := c.send(request(c.next, method, params)); err != nil {
		return c.fail(ctx, method, err)
	}
	for c.lines.Scan() {
		line := bytes.TrimSpace(c.lines.Bytes())
		if len(line) == 0 {
			continue
		}
		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			return fmt.Errorf("%s: not a JSON-RPC message", method)
		}
		if done, err := m.decode(c.next, out); done {
			if err != nil {
				return fmt.Errorf("%s: %w", method, err)
			}
			return nil
		}
	}
	err := c.lines.Err()
	if err == nil {
		err = errors.New("the server exited before answering")
	}
	return c.fail(ctx, method, err)
}

// fail wraps a transport error with the method; once ctx is done that is
// why the server is gone, so ctx's error replaces it.
func (c *stdioConn) fail(ctx context.Context, method string, err error) error {
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return fmt.Errorf("%s: %w", method, err)
}

func (c *stdioConn) notify(ctx context.Context, method string) error {
	if err := c.send(message{JSONRPC: "2.0", Method: method}); err != nil {
		return c.fail(ctx, method, err)
	}
	return nil
}

func (c *stdioConn) initialized(string) {}

// close ends the session as the protocol asks: stdin is closed, a server
// still running a second later receives SIGTERM, and one that ignores that
// for another second is killed.
func (c *stdioConn) close() {
	c.in.Close()
	term := time.AfterFunc(time.Second, c.cancel)
	_ = c.cmd.Wait() // the exit status is irrelevant once the handshake is done
	term.Stop()
	c.cancel()
	c.out.Close()
}

// httpConn is a streamable HTTP endpoint.
type httpConn struct {
	client  *http.Client
	url     string
	headers map[string]string
	session string // Mcp-Session-Id the server assigned, if any
	version string // the negotiated protocol version, after initialize
	next    int64
}

// requestHeaders are the headers a remote server receives: the declared
// ones, a Codex env_http_headers value from env, and, for a Codex
// bearer_token_env_var that env sets, Authorization with that bearer token.
func requestHeaders(s Server, env map[string]string) map[string]string {
	headers := map[string]string{}
	maps.Copy(headers, s.headers)
	for name, variable := range s.envHeaders {
		if v, ok := env[variable]; ok {
			headers[name] = v
		}
	}
	if v, ok := env[s.bearerVar]; ok && s.bearerVar != "" {
		headers["Authorization"] = "Bearer " + v
	}
	return headers
}

// newHTTP is a streamable HTTP endpoint. The default transport takes its
// proxy from the environment of the agentx process (HTTP_PROXY,
// HTTPS_PROXY, NO_PROXY), as any HTTP client does.
func newHTTP(url string, headers map[string]string) *httpConn {
	return &httpConn{client: &http.Client{}, url: url, headers: headers}
}

func (c *httpConn) post(ctx context.Context, m message) (*http.Response, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	c.prepare(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &reasoned{ReasonUnreachable, errors.New(errText(err))}
	}
	if resp.StatusCode/100 != 2 {
		return nil, refused(resp)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.session = sid
	}
	return resp, nil
}

// prepare adds the declared headers and the session and version headers.
func (c *httpConn) prepare(req *http.Request) {
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
}

// errText is the innermost cause of a transport error, without the address.
func errText(err error) string {
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return err.Error()
		}
		err = inner
	}
}

func (c *httpConn) call(ctx context.Context, method string, params any, out any) error {
	c.next++
	resp, err := c.post(ctx, request(c.next, method, params))
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, maxMessage)
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType == "text/event-stream" {
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 64<<10), maxMessage)
		for {
			_, data, ok := event(sc)
			if !ok {
				break
			}
			var m message
			if err := json.Unmarshal(data, &m); err != nil {
				return fmt.Errorf("%s: not a JSON-RPC message", method)
			}
			if done, err := m.decode(c.next, out); done {
				if err != nil {
					return fmt.Errorf("%s: %w", method, err)
				}
				return nil
			}
		}
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", method, ctx.Err())
		}
		return fmt.Errorf("%s: the event stream ended before the response", method)
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var m message
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("%s: not a JSON-RPC message", method)
	}
	done, err := m.decode(c.next, out)
	if !done {
		return fmt.Errorf("%s: the response carries another id", method)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

// event reads server-sent events up to the next one with data and returns
// its name, "message" when it has none, and its data; false at the end of
// the stream or past maxMessage. An event without data, such as the one a
// server sends first to hand out an event id, is skipped.
func event(sc *bufio.Scanner) (string, []byte, bool) {
	var name string
	var data []byte
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			if len(data) > 0 {
				break
			}
			name = ""
			continue
		}
		if rest, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			if len(data) > 0 {
				data = append(data, '\n')
			}
			if data = append(data, bytes.TrimPrefix(rest, []byte(" "))...); len(data) > maxMessage {
				return "", nil, false
			}
		} else if rest, ok := bytes.CutPrefix(line, []byte("event:")); ok {
			name = string(bytes.TrimPrefix(rest, []byte(" ")))
		}
	}
	if name == "" {
		name = "message"
	}
	return name, data, len(data) > 0
}

func (c *httpConn) notify(ctx context.Context, method string) error {
	resp, err := c.post(ctx, message{JSONRPC: "2.0", Method: method})
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	resp.Body.Close()
	return nil
}

func (c *httpConn) initialized(version string) { c.version = version }

// close ends the session the server assigned, if any; the server may refuse.
func (c *httpConn) close() {
	if c.session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.url, nil)
	if err != nil {
		return
	}
	c.prepare(req)
	if resp, err := c.client.Do(req); err == nil {
		resp.Body.Close()
	}
}
