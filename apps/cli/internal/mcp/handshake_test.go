package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// webServer is a streamable-http MCP endpoint: it refuses a request without
// the declared header value, assigns a session id that must be echoed, and
// answers tools/list as an event stream that opens with a comment, an event
// without data and a notification before the response, whose data comes in
// two lines ended by CRLF.
func webServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "secret-hand-header-value" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodDelete {
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if m.Method != "initialize" && (r.Header.Get("Mcp-Session-Id") != "session-1" || r.Header.Get("MCP-Protocol-Version") != "2025-11-25") {
			http.Error(w, "no session", http.StatusBadRequest)
			return
		}
		switch m.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session-1")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"web","version":"0"}}}`, m.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, ": keep-alive\n\nid: 1\ndata:\n\nid: 2\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"level\":\"info\",\"data\":\"listing\"}}\n\nevent: message\r\nid: 3\r\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\r\ndata: \"result\":{\"tools\":[{\"name\":\"search\",\"description\":\"Search the web\",\"inputSchema\":{\"type\":\"object\"}}]}}\r\n\r\n", m.ID)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"method not found"}}`, m.ID)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

// sseServer is an MCP endpoint on the legacy HTTP+SSE transport that refuses
// a request without the declared header value. A GET on a path under /sse
// opens a stream whose first event names the endpoint, and each answer comes
// on that stream; a POST there, how a streamable HTTP session opens, is
// refused with 405, except under /sse-modern, which answers 404 with a
// JSON-RPC error as a current server does. /offsite names an endpoint on
// another origin.
func sseServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	sessions := map[string]chan string{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "secret-hand-header-value" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && (strings.HasPrefix(r.URL.Path, "/sse") || r.URL.Path == "/offsite"):
			mu.Lock()
			id := strconv.Itoa(len(sessions) + 1)
			answers := make(chan string, 8)
			sessions[id] = answers
			mu.Unlock()
			endpoint := "/messages?session=" + id
			if r.URL.Path == "/offsite" {
				endpoint = "http://elsewhere.example" + endpoint
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, ": open\n\nevent: endpoint\ndata: %s\n\n", endpoint)
			w.(http.Flusher).Flush()
			for {
				select {
				case a := <-answers:
					fmt.Fprintf(w, "event: message\ndata: %s\n\n", a)
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
		case r.Method == http.MethodPost && r.URL.Path == "/sse-modern":
			// How a current server refuses a method: no reason to try SSE.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/messages":
			mu.Lock()
			answers, ok := sessions[r.URL.Query().Get("session")]
			mu.Unlock()
			var m struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if !ok || json.NewDecoder(r.Body).Decode(&m) != nil {
				http.Error(w, "no session", http.StatusNotFound)
				return
			}
			switch m.Method {
			case "initialize":
				answers <- fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"legacy","version":"0"}}}`, m.ID)
			case "tools/list":
				answers <- fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"recall","description":"Recall a note","inputSchema":{"type":"object"}}]}}`, m.ID)
			}
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TestHandshakeOverHTTP drives Handshake over the remote transports, each
// server declared as a configuration file declares it: streamable HTTP
// answering in an event stream, the legacy HTTP+SSE transport declared as
// such or found behind the 405 a legacy server gives the POST that opens a
// streamable HTTP session, and the three ways such a handshake fails. A 404
// that carries a JSON-RPC error is a current server refusing the method,
// no reason to try SSE; an endpoint event naming another origin is refused
// before the declared headers go there; and a server that wants other
// credentials is unauthorized, whatever the transport.
func TestHandshakeOverHTTP(t *testing.T) {
	t.Parallel()
	web, sse := webServer(t).URL, sseServer(t).URL
	servers, err := Parse(JSON, fmt.Appendf(nil, `{"mcpServers": {
  "web": {"url": %[1]q, "headers": {"X-Key": "secret-hand-header-value"}},
  "legacy": {"type": "sse", "url": %[2]q, "headers": {"X-Key": "secret-hand-header-value"}},
  "fallback": {"type": "http", "url": %[3]q, "headers": {"X-Key": "secret-hand-header-value"}},
  "modern": {"type": "http", "url": %[4]q, "headers": {"X-Key": "secret-hand-header-value"}},
  "offsite": {"type": "sse", "url": %[5]q, "headers": {"X-Key": "secret-hand-header-value"}},
  "locked": {"url": %[1]q},
  "locked-sse": {"type": "sse", "url": %[2]q}}}`, web, sse+"/sse", sse+"/sse-fallback", sse+"/sse-modern", sse+"/offsite"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		tools  []string // the names listed, for a handshake that works
		reason string   // why it failed, for one that does not
		err    string
	}{
		"web":        {tools: []string{"search"}},
		"legacy":     {tools: []string{"recall"}},
		"fallback":   {tools: []string{"recall"}},
		"modern":     {reason: ReasonFailed, err: "initialize: HTTP 404 Not Found: server error -32601: Method not found"},
		"offsite":    {reason: ReasonFailed, err: "the endpoint event names another origin"},
		"locked":     {reason: ReasonUnauthorized, err: "initialize: HTTP 401 Unauthorized"},
		"locked-sse": {reason: ReasonUnauthorized, err: "HTTP 401 Unauthorized"},
	}
	if len(servers) != len(want) {
		t.Fatalf("parsed %d servers, want %d", len(servers), len(want))
	}
	for _, s := range servers {
		t.Run(s.Name, func(t *testing.T) {
			t.Parallel()
			w := want[s.Name]
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r, err := Handshake(ctx, s, nil, "test")
			if w.reason != "" {
				if err == nil {
					t.Fatalf("handshake worked, want it to fail with %s", w.reason)
				}
				if got := Reason(err); got != w.reason {
					t.Errorf("reason = %s, want %s: %v", got, w.reason, err)
				}
				if err.Error() != w.err {
					t.Errorf("error = %q, want %q", err, w.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, it := range r.Items {
				names = append(names, it.Kind+" "+it.Name)
			}
			var wantNames []string
			for _, n := range w.tools {
				wantNames = append(wantNames, "tool "+n)
			}
			if !slices.Equal(names, wantNames) {
				t.Errorf("items = %v, want %v", names, wantNames)
			}
			if len(r.Signature) != 64 {
				t.Errorf("signature = %q, want 64 hex characters", r.Signature)
			}
		})
	}
}
