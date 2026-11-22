package rpc

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// pipes is a stdio door with the client's ends of its streams.
type pipes struct {
	t     *testing.T
	s     *Stdio
	in    *io.PipeWriter
	lines *bufio.Scanner
}

func openStdio(t *testing.T, ctx context.Context, methods map[string]Method) *pipes {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	t.Cleanup(func() {
		_ = inW.Close()
		_ = outR.Close()
	})
	return &pipes{t: t, s: ServeStdio(ctx, inR, outW, methods), in: inW, lines: bufio.NewScanner(outR)}
}

func (p *pipes) send(text string) {
	p.t.Helper()
	if _, err := io.WriteString(p.in, text+"\n"); err != nil {
		p.t.Fatal(err)
	}
}

func (p *pipes) want(text string) {
	p.t.Helper()
	if !p.lines.Scan() {
		p.t.Fatalf("no line, want %s", text)
	}
	if got := p.lines.Text(); got != text {
		p.t.Fatalf("got  %s\nwant %s", got, text)
	}
}

// The standard streams carry what the WebSocket carries, one message per
// line: a request's notifications and then its answer, the error of a line
// that is no JSON, and a cancel. A blank line is no message. A client that
// is behind on reading makes the writes wait rather than lose what they
// carry, and the next request is taken meanwhile: nothing is read from
// stdout until the second request has started.
func TestStdioAnswersLineByLine(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 1)
	p := openStdio(t, context.Background(), map[string]Method{
		"count": func(_ context.Context, c Call) (any, *Error) {
			c.Notify("event", map[string]any{"id": c.ID, "n": 1})
			return 2, nil
		},
		"wait": waiting(started),
	})
	p.send(`{"jsonrpc":"2.0","id":1,"method":"count"}`)
	p.send("")
	p.send(`{"jsonrpc":"2.0","id":"w","method":"wait"}`)
	<-started
	p.want(`{"jsonrpc":"2.0","method":"event","params":{"id":1,"n":1}}`)
	p.want(`{"jsonrpc":"2.0","id":1,"result":2}`)
	p.send(`not json`)
	if !p.lines.Scan() || !strings.HasPrefix(p.lines.Text(), `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,`) {
		t.Fatalf("a line that is no JSON was answered %s", p.lines.Text())
	}
	p.send(`{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":"w"}}`)
	p.want(`{"jsonrpc":"2.0","id":"w","error":{"code":-32800,"message":"cancelled"}}`)
}

// The end of stdin cancels the requests in progress, and the door is done
// once their answers are written; so is a context that ends.
func TestStdioEndsWithItsInputAndWithItsContext(t *testing.T) {
	t.Parallel()
	started := make(chan struct{}, 1)
	p := openStdio(t, context.Background(), map[string]Method{"wait": waiting(started)})
	p.send(`{"jsonrpc":"2.0","id":1,"method":"wait"}`)
	<-started
	_ = p.in.Close()
	p.want(`{"jsonrpc":"2.0","id":1,"error":{"code":-32800,"message":"cancelled"}}`)
	select {
	case <-p.s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the door went on after its input ended")
	}

	ctx, cancel := context.WithCancel(context.Background())
	p = openStdio(t, ctx, map[string]Method{"wait": waiting(started)})
	p.send(`{"jsonrpc":"2.0","id":1,"method":"wait"}`)
	<-started
	cancel()
	p.want(`{"jsonrpc":"2.0","id":1,"error":{"code":-32800,"message":"cancelled"}}`)
	select {
	case <-p.s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the door went on after its context ended")
	}
}
