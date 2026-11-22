package rpc

import (
	"bufio"
	"bytes"
	"context"
	"io"
)

// Stdio is the door of the process that started serve: one client, which
// writes its messages to serve's stdin and reads the answers from its
// stdout, one message per line, as the stdio transport of MCP does.
type Stdio struct {
	c     *conn
	ended chan struct{}
}

// ServeStdio answers the messages read from in until in ends, at EOF or on
// a read error, or ctx is done. Every message it sends is one Write of the
// message and a newline to out, made from any goroutine: out must take the
// lock every other writer of that stream takes, so that no line of serve's
// own lands inside a message. A write waits for as long as out does, as a
// pipe makes it wait for a reader that is behind; the client is never
// dropped for it.
//
// When in ends, or ctx is done, every request in progress is cancelled as
// $/cancelRequest cancels it, and Done is closed once their methods have
// returned and their responses are written. A read blocked on in cannot be
// interrupted: when ctx ends the door first, that goroutine lives on until
// in ends, and what it reads meanwhile is not answered.
func ServeStdio(ctx context.Context, in io.Reader, out io.Writer, methods map[string]Method) *Stdio {
	s := &Stdio{ended: make(chan struct{})}
	s.c = newConn(ctx, methods, func(b []byte) {
		_, _ = out.Write(append(b, '\n')) // a reader that is gone makes serve end on its stdin's EOF
	})
	go func() {
		<-s.c.ctx.Done()
		s.c.end()
		close(s.ended)
	}()
	go func() {
		defer s.c.cancel()
		r := bufio.NewReader(in)
		for {
			line, err := r.ReadBytes('\n')
			if line = bytes.TrimSpace(line); len(line) > 0 {
				if s.c.ctx.Err() != nil {
					return
				}
				s.c.handle(line)
			}
			if err != nil {
				return
			}
		}
	}()
	return s
}

// Done is closed once the door has ended, see ServeStdio.
func (s *Stdio) Done() <-chan struct{} { return s.ended }
