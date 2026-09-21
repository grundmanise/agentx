package scan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/mcp"
)

// HandshakeOptions bound the handshakes of one scan.
type HandshakeOptions struct {
	Env     map[string]string // the environment a stdio server starts with, plus its declared variables
	Timeout time.Duration     // per server
	Version string            // the CLI version, told to servers
	Debug   func(format string, args ...any)
}

// parallel is how many servers are handshaken at once.
const parallel = 4

// Handshake connects to every declared server, outside any lock, at most
// parallel at a time, and records what each exposed. A server the client
// has turned off is never started or connected to. A server that cannot
// be reached, answers badly or runs out of time is one warning naming its
// file and name, never its environment or headers, keeps its stored
// signature, if any, and carries why it failed with a hint on its occurrence.
func (s *Scan) Handshake(ctx context.Context, o HandshakeOptions) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, parallel)
	for _, d := range s.declared {
		if d.disabled {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			hctx, cancel := context.WithTimeout(ctx, o.Timeout)
			defer cancel()
			r, err := mcp.Handshake(hctx, d.server, o.Env, o.Version)
			mu.Lock()
			defer mu.Unlock()
			if names := mcp.Withheld(d.server, o.Env); len(names) > 0 && o.Debug != nil {
				o.Debug("%s: server %s: %s left empty in the URL and headers, as Claude Code withholds credentials from a remote server", d.file, d.server.Name, strings.Join(names, ", "))
			}
			reason := mcp.Reason(err)
			if errors.Is(err, context.DeadlineExceeded) {
				err = fmt.Errorf("timed out after %s", o.Timeout)
			}
			if err != nil {
				s.warn(fmt.Sprintf("%s: server %s: %v", d.file, d.server.Name, err))
				d.failed = &HandshakeError{Reason: reason, Message: err.Error(), Hint: mcp.Hint(reason, d.server)}
				return
			}
			d.fresh = &r
			d.at = time.Now().UTC().Format(time.RFC3339)
		}()
	}
	wg.Wait()
}
