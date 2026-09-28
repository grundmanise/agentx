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
// version the first one asked for, and a git rewritten in place, in the
// same tick of the clock and at the same size, is asked again, as is one
// that failed to answer.
func TestVersionIsAskedOncePerGit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	git := filepath.Join(dir, "git")
	var stamp time.Time
	stub := func(out string) {
		t.Helper()
		if err := os.WriteFile(git, []byte("#!/bin/sh\necho \"$@\" >> "+calls+"\necho '"+out+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if stamp.IsZero() {
			info, err := os.Stat(git)
			if err != nil {
				t.Fatal(err)
			}
			stamp = info.ModTime()
		}
		if err := os.Chtimes(git, stamp, stamp); err != nil {
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
	stub("git version 2.41.0")
	for i, want := range []string{"2.41.0", "2.41.0", "2.41.0"} {
		if got, asked := version(); got != want || asked != 1 {
			t.Fatalf("runner %d: version %s after %d calls, want %s after 1", i, got, asked, want)
		}
	}
	stub("git version 2.42.0")
	if got, asked := version(); got != "2.42.0" || asked != 2 {
		t.Errorf("rewritten in place: version %s after %d calls, want 2.42.0 after 2", got, asked)
	}
	stub("git versio_ 2.43.0")
	if got, asked := version(); got != "error" || asked != 3 {
		t.Errorf("unreadable answer: version %s after %d calls, want an error after 3", got, asked)
	}
	if got, asked := version(); got != "error" || asked != 4 {
		t.Errorf("unreadable answer again: version %s after %d calls, want an error after 4", got, asked)
	}
}
