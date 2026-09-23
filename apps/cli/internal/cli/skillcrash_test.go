package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// installChildEnv names the variables the child process of the crash test
// below reads: it runs one install against the parent's temporary home.
const (
	installChildEnv = "AGENTX_TEST_INSTALL_CHILD"
	installChildURL = "AGENTX_TEST_SOURCE_URL"
)

// TestInstallChildProcess is not a test: it is the body of the process
// TestSkillAddRecoversFromAKilledRun starts, which installs one skill and
// is killed in the middle of it. It does nothing when the variable that
// marks that process is not set.
func TestInstallChildProcess(t *testing.T) {
	if os.Getenv(installChildEnv) == "" {
		t.Skip("not the install child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	code := Run(context.Background(), []string{"skill", "add", os.Getenv(installChildURL), "--skill", "alpha"},
		env, strings.NewReader(""), os.Stdout, os.Stderr)
	os.Exit(code)
}

// TestSkillAddRecoversFromAKilledRun kills an install with SIGKILL at a
// durable boundary: a git wrapper on the PATH hands update-ref to the real
// git and then kills the process that ran it, so the import branch is
// written and the machine is left with the journal that describes the rest.
// The next command that mutates agentx home recovers it, and the install is
// then whole: library directory, branch and placements.
func TestSkillAddRecoversFromAKilledRun(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// The wrapper kills its own parent, which is the agentx process, right
	// after the ref is created: a process stopped after a live write and
	// before it could record that it made it.
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
	child := exec.Command(os.Args[0], "-test.run=^TestInstallChildProcess$", "-test.v")
	child.Env = append(os.Environ(), installChildEnv+"=1", installChildURL+"="+s.url)
	for k, v := range h.env {
		child.Env = append(child.Env, k+"="+v)
	}
	out, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("the install was not killed:\n%s", out)
	}

	// The branch is there, the library is not, and the journal says what is
	// left to do.
	if head := h.accountGit("for-each-ref", "--format=%(objectname)", "refs/heads/managed/alpha"); head == "" {
		t.Fatalf("the killed run created no branch, so it was not killed where the test expects:\n%s", out)
	}
	if n := journalCount(t, h); n != 1 {
		t.Fatalf("%d journals after the killed run, want 1:\n%s", n, out)
	}
	// The ref is written before the library directory, which is the order
	// the contract prescribes. Were the library published first, a process
	// stopped here would leave a real skill directory that no lineage
	// branch names, and the next scan would read it as a new unmanaged
	// skill — which is what the mutation safety spec forbids.
	if _, err := os.Stat(filepath.Join(h.library, "alpha")); err == nil {
		t.Errorf("the library was published before the ref was written")
	}
	equal(t, "the journal's steps", strings.Join(journalSteps(t, h), ", "), "ref, publish, link, link")

	// The next mutation recovers it, under the lock it takes anyway.
	if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
		t.Fatalf("the command after the killed run: exit %d\n%s", got.exit, got.stderr)
	}
	equal(t, "journals after recovery", journalCount(t, h), 0)
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("the library directory was not recovered: %v", err)
	}
	for _, rel := range []string{".claude/skills/alpha", ".cursor/skills/alpha"} {
		if _, err := os.Readlink(filepath.Join(h.home, rel)); err != nil {
			t.Errorf("the placement %s was not recovered: %v", rel, err)
		}
	}
	// And the machine reports the skill as installed, at the version the
	// branch names.
	list := h.run("--json", "skill", "list")
	equal(t, "exit", list.exit, 0)
	ev := h.one(list.stdout, "library_skill")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "state", ev["state"], "current")
}

// journalSteps is the kinds of the steps of the one journal waiting in
// agentx home, in the order the command recorded them, which is the order
// the contract prescribes and the order recovery applies them in.
func journalSteps(t *testing.T, h *harness) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(h.agentx, "mutations", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("%d journals, want 1: %v", len(paths), err)
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var j struct {
		Steps []struct {
			Kind string `json:"kind"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		t.Fatalf("the journal is not JSON: %v", err)
	}
	kinds := make([]string, len(j.Steps))
	for i, s := range j.Steps {
		kinds[i] = s.Kind
	}
	return kinds
}
