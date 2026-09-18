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
	"maps"
	"mime"
	"net/http"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// protocolVersion is the latest MCP revision that opens with an initialize
// request; the server may answer with an earlier one, which is accepted as is.
const protocolVersion = "2025-11-25"

// Item is one tool, prompt, resource or resource template a server exposes.
type Item struct {
	Kind        string // tool, prompt, resource or resource_template
	Name        string
	Description string
	Hash        string // hex SHA-256 over the item's serialisation, see Signature

	serialised string
}

// Result is what a handshake found: the items sorted by kind then name, and
// the signature over all of them.
type Result struct {
	Signature string
	Items     []Item
}

// Handshake starts or connects to s, lists what it exposes and returns the
// items with their signature. A stdio server runs with env plus its declared
// environment; a streamable-http server receives its declared headers, with
// a Codex env_http_headers value taken from env. ctx bounds the whole
// exchange; a deadline that passes is ctx.Err(). An error never carries a
// value from the declaration.
func Handshake(ctx context.Context, s Server, env map[string]string, clientVersion string) (Result, error) {
	var c conn
	switch s.Transport {
	case "stdio":
		var err error
		if c, err = startStdio(ctx, s, env); err != nil {
			return Result{}, err
		}
	case "streamable-http":
		c = newHTTP(s, env)
	default:
		return Result{}, errors.New("legacy sse transport is not handshaken")
	}
	defer c.close()

	var init struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
	}
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "agentx", "version": clientVersion},
	}
	if err := c.call(ctx, "initialize", params, &init); err != nil {
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
	return Signature(items), nil
}

// Signature sorts items by kind, name and serialisation and computes the
// signature: SHA-256 over the items' serialisations joined by NUL. An item's
// serialisation is its kind, name, description, input schema and output
// schema joined by NUL, the schemas as canonical JSON, empty where absent;
// its hash is SHA-256 over that serialisation.
func Signature(items []Item) Result {
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

// item serialises e as Signature documents and hashes it.
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
	out    *bufio.Scanner
	next   int64
}

func startStdio(ctx context.Context, s Server, env map[string]string) (*stdioConn, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, s.Command, s.Args...)
	merged := maps.Clone(env)
	maps.Copy(merged, s.env)
	cmd.Env = []string{} // nil would inherit the process environment
	for _, k := range slices.Sorted(maps.Keys(merged)) {
		cmd.Env = append(cmd.Env, k+"="+merged[k])
	}
	cmd.Stderr = io.Discard // never recorded: a server may log what it was given
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), maxMessage)
	return &stdioConn{cmd: cmd, cancel: cancel, in: in, out: sc}, nil
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
		return fmt.Errorf("%s: %w", method, c.exited(ctx, err))
	}
	for c.out.Scan() {
		line := bytes.TrimSpace(c.out.Bytes())
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
	err := c.out.Err()
	if err == nil {
		err = errors.New("the server exited before answering")
	}
	return fmt.Errorf("%s: %w", method, c.exited(ctx, err))
}

// exited is ctx's error when the deadline passed, which is why the server
// is gone, else err.
func (c *stdioConn) exited(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (c *stdioConn) notify(ctx context.Context, method string) error {
	if err := c.send(message{JSONRPC: "2.0", Method: method}); err != nil {
		return fmt.Errorf("%s: %w", method, c.exited(ctx, err))
	}
	return nil
}

func (c *stdioConn) initialized(string) {}

// close ends the session as the protocol asks: stdin is closed, a server
// still running receives SIGTERM, and one that ignores it is killed.
func (c *stdioConn) close() {
	c.in.Close()
	c.cancel()
	c.cmd.Wait()
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

func newHTTP(s Server, env map[string]string) *httpConn {
	headers := maps.Clone(s.headers)
	for name, variable := range s.envHeaders {
		if v, ok := env[variable]; ok {
			headers[name] = v
		}
	}
	return &httpConn{client: &http.Client{}, url: s.URL, headers: headers}
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
		return nil, errors.New(errText(err))
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, errors.New("HTTP " + resp.Status)
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
			data, ok := event(sc)
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

// event reads one server-sent event and returns its data; false at the end
// of the stream.
func event(sc *bufio.Scanner) ([]byte, bool) {
	var data [][]byte
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			if len(data) > 0 {
				return bytes.Join(data, []byte("\n")), true
			}
			continue
		}
		if rest, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			data = append(data, bytes.Clone(bytes.TrimPrefix(rest, []byte(" "))))
		}
	}
	if len(data) > 0 {
		return bytes.Join(data, []byte("\n")), true
	}
	return nil, false
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
