package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRefsUpdateWithAnExpectedOldValue is the mechanism an install creates
// its import branch with: a ref is created only when it does not exist yet,
// and moved only from the value the caller expects, so that two commands
// cannot both claim one name.
func TestRefsUpdateWithAnExpectedOldValue(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}
	r := New(env, false, func(string, ...any) {})
	if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	empty, err := r.Isolated(ctx, gitDir, "hash-object", "-t", "tree", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.IsolatedAt(ctx, gitDir, "1700000000 +0000", "commit-tree", empty, "-m", "one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.IsolatedAt(ctx, gitDir, "1700000001 +0000", "commit-tree", empty, "-m", "two")
	if err != nil {
		t.Fatal(err)
	}

	refs := r.Refs(ctx)
	const ref = "refs/heads/managed/alpha"
	if value, err := refs.RefValue(gitDir, ref); err != nil || value != "" {
		t.Fatalf("a ref that does not exist reads as %q: %v", value, err)
	}
	if err := refs.UpdateRef(gitDir, ref, first, ""); err != nil {
		t.Fatalf("creating the ref: %v", err)
	}
	if value, err := refs.RefValue(gitDir, ref); err != nil || value != first {
		t.Fatalf("the ref reads as %q, want %s: %v", value, first, err)
	}
	// Creating it again, which is what a second command claiming the name
	// would do, is refused rather than silently overwriting.
	if err := refs.UpdateRef(gitDir, ref, second, ""); err == nil {
		t.Error("a ref that already exists was created again")
	}
	if value, _ := refs.RefValue(gitDir, ref); value != first {
		t.Errorf("the refused update moved the ref to %s", value)
	}
	// Moving it from the wrong value is refused too; from the right one it
	// goes through.
	if err := refs.UpdateRef(gitDir, ref, second, second); err == nil {
		t.Error("the ref moved from a value it did not hold")
	}
	if err := refs.UpdateRef(gitDir, ref, second, first); err != nil {
		t.Errorf("moving the ref from the value it holds: %v", err)
	}
	if value, _ := refs.RefValue(gitDir, ref); value != second {
		t.Errorf("the ref is at %s, want %s", value, second)
	}
}

// TestIsolatedAllRunsReadsAtOnce checks the bounded way a command runs the
// reads that do not depend on each other: every answer comes back, in the
// order the calls were given, and a failure is reported without losing the
// others.
func TestIsolatedAllRunsReadsAtOnce(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}
	r := New(env, false, func(string, ...any) {})
	if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	calls := make([][]string, 0, Workers*3)
	for range Workers * 3 {
		calls = append(calls, []string{"hash-object", "-t", "blob", "-w", "--stdin", "--path", "x"})
	}
	calls[2] = []string{"cat-file", "-t", strings.Repeat("0", 40)}
	out, err := r.IsolatedAll(ctx, gitDir, calls)
	if err == nil {
		t.Fatal("a failing call was not reported")
	}
	if len(out) != len(calls) {
		t.Fatalf("%d answers for %d calls", len(out), len(calls))
	}
	for i, answer := range out {
		if i != 2 && answer == "" {
			t.Errorf("call %d answered nothing although another call failed", i)
		}
	}
}

// TestIsolatedAllReportsTheFirstFailureByCallOrder pins the half of the
// contract one failing call cannot show: the calls come back in the order
// they were given and the first failure by that order is the error. The
// work is parallel, so which call fails first in time is not decided by
// anything a caller can see; which one is reported has to be.
func TestIsolatedAllReportsTheFirstFailureByCallOrder(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	ctx := context.Background()
	gitDir := filepath.Join(t.TempDir(), "account.git")
	env := map[string]string{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}
	r := New(env, false, func(string, ...any) {})
	if _, err := r.Isolated(ctx, gitDir, "init", "--bare", "--quiet", gitDir); err != nil {
		t.Fatal(err)
	}
	// Two calls that both fail, run by different subcommands so the error
	// says which one was reported.
	calls := [][]string{
		{"cat-file", "-t", strings.Repeat("a", 40)},
		{"rev-parse", "--verify", "refs/heads/nope"},
	}
	_, err := r.IsolatedAll(ctx, gitDir, calls)
	if err == nil {
		t.Fatal("neither failing call was reported")
	}
	if !strings.HasPrefix(err.Error(), "git cat-file") {
		t.Errorf("IsolatedAll reported %q, want the first failing call by call order", err)
	}
}
