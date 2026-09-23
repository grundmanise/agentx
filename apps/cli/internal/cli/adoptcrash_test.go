package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// adoptChildEnv marks the process TestAdoptRecoversFromAKilledRun starts,
// which adopts against the parent's temporary home and is killed in the
// middle of it.
const adoptChildEnv = "AGENTX_TEST_ADOPT_CHILD"

// TestAdoptChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestAdoptChildProcess(t *testing.T) {
	if os.Getenv(adoptChildEnv) == "" {
		t.Skip("not the adopt child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"adopt", "--all"}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestAdoptRecoversFromAKilledRun kills an adoption with SIGKILL at its one
// durable boundary: a git wrapper hands the update-ref that writes the
// import branch to the real git and then kills the process that ran it, so
// the branch is live and nothing has recorded that it went in. The next
// command recovers the journal, and the adoption is whole afterwards.
//
// The library directory is the test's other half. An adoption writes no
// path at all, so it must be byte for byte what it was before the run, both
// while the journal is unfinished and after it is recovered: a recovery
// that touched the user's directory would be the very thing the command
// exists not to do.
func TestAdoptRecoversFromAKilledRun(t *testing.T) {
	t.Parallel()
	h, s, v1, installed := adoptHarness(t)
	editLibrary(t, h, "alpha", "notes.md", "an edit of mine\n")
	before := libraryTree(t, filepath.Join(h.library, "alpha"))
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})
	// The source is added first, so that the only update-ref the killed run
	// makes is the one that writes the import branch.
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	out := killedAdopt(t, h)

	if head := h.accountGit("for-each-ref", "--format=%(objectname)", "refs/heads/managed/alpha"); head == "" {
		t.Fatalf("the killed run created no branch, so it was not killed where the test expects:\n%s", out)
	}
	if n := journalCount(t, h); n != 1 {
		t.Fatalf("%d journals after the killed run, want 1:\n%s", n, out)
	}
	h.lockUnchanged(h.lockPath(), lock)
	sameTree(t, "the library directory after the killed run", libraryTree(t, filepath.Join(h.library, "alpha")), before)

	// The next mutation recovers the journal under the lock it takes anyway.
	if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
		t.Fatalf("the command after the killed run: exit %d\n%s", got.exit, got.stderr)
	}
	equal(t, "journals after recovery", journalCount(t, h), 0)
	h.lockUnchanged(h.lockPath(), lock)
	sameTree(t, "the library directory after recovery", libraryTree(t, filepath.Join(h.library, "alpha")), before)

	list := h.run("--json", "skill", "list")
	equal(t, "exit", list.exit, 0)
	ev := h.one(list.stdout, "library_skill")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "state", ev["state"], stateModified)
	equal(t, "upstream_commit", ev["upstream_commit"], v1)
}

// killedAdopt runs one adoption in a child process that a git wrapper kills
// the moment a ref is written. It returns what the child printed, for a
// test that has to say where it was killed.
func killedAdopt(t *testing.T, h *harness) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" update-ref "*)
	`+real+` "$@"
	status=$?
	kill -9 $PPID
	exit $status
	;;
esac
exec `+real+` "$@"
`)
	child := exec.Command(os.Args[0], "-test.run=^TestAdoptChildProcess$", "-test.v")
	child.Env = append(os.Environ(), adoptChildEnv+"=1")
	for k, v := range h.env {
		child.Env = append(child.Env, k+"="+v)
	}
	out, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("the adoption was not killed:\n%s", out)
	}
	return string(out)
}
