package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
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
	const other = "refs/heads/managed/beta"
	value := func(name string) string {
		t.Helper()
		values, err := refs.RefValues(gitDir, []string{name})
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		return values[name]
	}
	update := func(name, newValue, oldValue string) error {
		return refs.UpdateRefs(gitDir, []home.RefUpdate{{Ref: name, New: newValue, Old: oldValue}})
	}
	if got := value(ref); got != "" {
		t.Fatalf("a ref that does not exist reads as %q", got)
	}
	if err := update(ref, first, ""); err != nil {
		t.Fatalf("creating the ref: %v", err)
	}
	if got := value(ref); got != first {
		t.Fatalf("the ref reads as %q, want %s", got, first)
	}
	// Creating it again, which is what a second command claiming the name
	// would do, is refused rather than silently overwriting.
	if err := update(ref, second, ""); err == nil {
		t.Error("a ref that already exists was created again")
	}
	if got := value(ref); got != first {
		t.Errorf("the refused update moved the ref to %s", got)
	}
	// Moving it from the wrong value is refused too; from the right one it
	// goes through.
	if err := update(ref, second, second); err == nil {
		t.Error("the ref moved from a value it did not hold")
	}
	if err := update(ref, second, first); err != nil {
		t.Errorf("moving the ref from the value it holds: %v", err)
	}
	if got := value(ref); got != second {
		t.Errorf("the ref is at %s, want %s", got, second)
	}

	// A batch is one transaction: a claim that cannot be met leaves every
	// other ref of the batch where it was, which is what lets a journal of
	// thirty skills be recovered rather than unpicked.
	batch := []home.RefUpdate{{Ref: other, New: first, Old: ""}, {Ref: ref, New: first, Old: ""}}
	if err := refs.UpdateRefs(gitDir, batch); err == nil {
		t.Error("a batch claiming a name that is taken went through")
	}
	if got := value(other); got != "" {
		t.Errorf("%s was created at %s although the batch was refused", other, got)
	}
	// And the reads of a whole batch come back in one call, a ref that holds
	// nothing being absent rather than empty.
	values, err := refs.RefValues(gitDir, []string{ref, other})
	if err != nil {
		t.Fatalf("reading a batch of refs: %v", err)
	}
	if values[ref] != second {
		t.Errorf("%s reads as %q, want %s", ref, values[ref], second)
	}
	if _, ok := values[other]; ok {
		t.Errorf("%s, which does not exist, came back as %q", other, values[other])
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

// TestRefValuesAnswersOnlyForTheNamesAsked pins what a name given to
// for-each-ref means. git reads it as a pattern, and a pattern matches the
// refs below it as well as the ref itself, so a repository holding
// refs/heads/managed/a/b would answer a read of refs/heads/managed/a with
// a ref nobody asked about. Nothing agentx writes creates such a name (a
// library name cannot hold a separator), but the account repo is a git
// repository the user can write to, and a value read for the wrong ref is
// the value a journal would move a branch from.
func TestRefValuesAnswersOnlyForTheNamesAsked(t *testing.T) {
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
	commit, err := r.IsolatedAt(ctx, gitDir, "1700000000 +0000", "commit-tree", empty, "-m", "one")
	if err != nil {
		t.Fatal(err)
	}
	// Only the ref below the name exists; the name itself does not.
	if _, err := r.Isolated(ctx, gitDir, "update-ref", "refs/heads/managed/a/b", commit); err != nil {
		t.Fatal(err)
	}
	values, err := r.Refs(ctx).RefValues(gitDir, []string{"refs/heads/managed/a"})
	if err != nil {
		t.Fatalf("reading a name no ref holds: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("reading refs/heads/managed/a answered with %v, want nothing: the ref does not exist", values)
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
