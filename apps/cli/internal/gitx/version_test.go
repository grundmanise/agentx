package gitx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestVersionIsAskedOncePerGit: every runner over the same git takes the
// version the first one asked for; a git rewritten in place, to another
// size or at another time, is asked again, and so is one that failed to
// answer. It is not parallel: it writes the stub git and runs it at once,
// which fails with ETXTBSY while another test forks.
func TestVersionIsAskedOncePerGit(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	git := filepath.Join(dir, "git")
	stub := func(out string, at time.Time) {
		t.Helper()
		if err := os.WriteFile(git, []byte("#!/bin/sh\necho \"$@\" >> "+calls+"\necho '"+out+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(git, at, at); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	version := func() (string, int) {
		t.Helper()
		v, err := New(map[string]string{"PATH": dir}, false, func(string, ...any) {}).Version(ctx)
		b, _ := os.ReadFile(calls)
		if err != nil {
			return "error", strings.Count(string(b), "\n")
		}
		return v.String(), strings.Count(string(b), "\n")
	}
	then := time.Now().Add(-time.Hour).Truncate(time.Second)
	stub("git version 2.41.0", then)
	for i := range 3 {
		if got, asked := version(); got != "2.41.0" || asked != 1 {
			t.Fatalf("runner %d: version %s after %d calls, want 2.41.0 after 1", i, got, asked)
		}
	}
	for _, c := range []struct {
		out, want string
		at        time.Time
	}{
		{"git version 2.141.0", "2.141.0", then},                  // another size, the same time
		{"git version 2.142.0", "2.142.0", then.Add(time.Second)}, // the same size, another time
		{"git versio_ 2.143.0", "error", then.Add(2 * time.Second)},
		{"git versio_ 2.143.0", "error", then.Add(2 * time.Second)}, // a failure is never kept
	} {
		before := 0
		if b, err := os.ReadFile(calls); err == nil {
			before = strings.Count(string(b), "\n")
		}
		stub(c.out, c.at)
		if got, asked := version(); got != c.want || asked != before+1 {
			t.Errorf("%q: version %s after %d calls, want %s after %d", c.out, got, asked, c.want, before+1)
		}
	}
}
