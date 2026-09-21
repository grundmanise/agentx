package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// sseConn is a server on the legacy HTTP+SSE transport of protocol revision
// 2024-11-05: a GET opens an event stream whose first event, endpoint, names
// where requests are POSTed, and every response arrives on that stream as a
// message event. The stream is read by its own goroutine, so a server that
// writes a response before it answers the POST cannot stall it.
type sseConn struct {
	client   *http.Client
	endpoint string
	headers  map[string]string
	cancel   context.CancelFunc // ends the stream
	messages chan []byte        // the data of message events; closed when the stream ends
	next     int64
}

// openSSE opens the event stream at rawURL with headers and waits for the
// endpoint event. The endpoint must share the stream's origin, since the
// declared headers are sent to it.
func openSSE(ctx context.Context, rawURL string, headers map[string]string) (*sseConn, error) {
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("the URL does not parse")
	}
	stream, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(stream, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel()
		return nil, errors.New("the URL does not parse")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &reasoned{ReasonUnreachable, errors.New(errText(err))}
	}
	if resp.StatusCode/100 != 2 {
		cancel()
		return nil, refused(resp)
	}
	if mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mediaType != "text/event-stream" {
		resp.Body.Close()
		cancel()
		return nil, errors.New("the server did not open an event stream")
	}
	c := &sseConn{client: client, headers: headers, cancel: cancel, messages: make(chan []byte, 16)}
	endpoint := make(chan string, 1)
	go c.read(stream, resp.Body, endpoint)
	select {
	case e, ok := <-endpoint:
		if !ok {
			c.close()
			return nil, errors.New("the event stream ended before the endpoint event")
		}
		ref, err := base.Parse(strings.TrimSpace(e))
		if err != nil || ref.Scheme != base.Scheme || ref.Host != base.Host {
			c.close()
			return nil, errors.New("the endpoint event names another origin")
		}
		c.endpoint = ref.String()
		return c, nil
	case <-ctx.Done():
		c.close()
		return nil, ctx.Err()
	}
}

// read delivers the stream's first endpoint event and every message event
// until the stream or ctx ends.
func (c *sseConn) read(ctx context.Context, body io.ReadCloser, endpoint chan<- string) {
	defer close(c.messages)
	defer close(endpoint)
	defer body.Close()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), maxMessage)
	sent := false
	for {
		name, data, ok := event(sc)
		if !ok {
			return
		}
		switch {
		case name == "endpoint" && !sent:
			endpoint <- string(data)
			sent = true
		case name == "message":
			select {
			case c.messages <- data:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (c *sseConn) post(ctx context.Context, m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &reasoned{ReasonUnreachable, errors.New(errText(err))}
	}
	if resp.StatusCode/100 != 2 {
		return refused(resp)
	}
	// The answer to a POST is an acknowledgement; the response comes on the stream.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return nil
}

func (c *sseConn) call(ctx context.Context, method string, params any, out any) error {
	c.next++
	if err := c.post(ctx, request(c.next, method, params)); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	for {
		select {
		case data, ok := <-c.messages:
			if !ok {
				if ctx.Err() != nil {
					return fmt.Errorf("%s: %w", method, ctx.Err())
				}
				return fmt.Errorf("%s: the event stream ended before the response", method)
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
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", method, ctx.Err())
		}
	}
}

func (c *sseConn) notify(ctx context.Context, method string) error {
	if err := c.post(ctx, message{JSONRPC: "2.0", Method: method}); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

func (c *sseConn) initialized(string) {}

// close ends the stream, which ends the server's session.
func (c *sseConn) close() { c.cancel() }
