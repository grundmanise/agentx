package source

import (
	"context"
	"sync"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// Fetchers is how many sources are fetched at once. A fetch is mostly
// network wait, so a few in flight hide the latency of the others, while
// the bound keeps a machine with many sources from spawning a git process
// per source at the same time. It is the bound every git runner keeps, and
// the count the handshakes of a scan use, for the same reason.
const Fetchers = gitx.Workers

// Result is what fetching one source produced: its listing, or the error
// that stopped it. Source is the source it was asked for, so a caller can
// report a failure without keeping an index.
type Result struct {
	Source  Source
	Listing Listing
	Err     error
}

// FetchAll fetches every source of srcs into the account repo, at most
// Fetchers at a time, and returns one Result per source in the order they
// were given: the work is parallel, what a caller reports from it is not.
// A source that fails carries its error and stops no other.
//
// done, when it is not nil, is called once per source as that source's
// fetch ends, with how many have ended including it, so that a caller can
// report progress while the rest is still running. It is called in
// completion order, which the network decides, under the lock that counts,
// so it need not be safe to call from several goroutines itself.
func FetchAll(ctx context.Context, r *gitx.Runner, gitDir string, srcs []Source, done func(s Source, finished int)) []Result {
	results := make([]Result, len(srcs))
	slots := make(chan struct{}, Fetchers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	finished := 0
	for i, s := range srcs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			listing, err := Fetch(ctx, r, gitDir, s)
			mu.Lock()
			defer mu.Unlock()
			results[i] = Result{Source: s, Listing: listing, Err: err}
			finished++
			if done != nil {
				done(s, finished)
			}
		}()
	}
	wg.Wait()
	return results
}
