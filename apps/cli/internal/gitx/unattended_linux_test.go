//go:build linux

package gitx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// unattendedChildEnv marks the process TestAnUnattendedGitDiesWithAgentx
// starts and kills: it runs one unattended git from the directory the
// variable names and waits on it.
const unattendedChildEnv = "AGENTX_TEST_UNATTENDED_CHILD"

// TestUnattendedChildProcess is not a test: it is the body of the process
// TestAnUnattendedGitDiesWithAgentx kills. It does nothing when the
// variable that marks that process is not set.
func TestUnattendedChildProcess(t *testing.T) {
	dir := os.Getenv(unattendedChildEnv)
	if dir == "" {
		t.Skip("not the unattended child process")
	}
	r := New(map[string]string{"PATH": dir, "HOME": dir}, false, func(string, ...any) {})
	_, _ = r.run(context.Background(), call{unattended: true}, "fetch")
	os.Exit(0)
}

// TestAnUnattendedGitDiesWithAgentx: an unattended git runs in a session of
// its own, out of reach of the kill of agentx's process group or tree by
// which the desktop app cancels a run, and its time budget is agentx's to
// enforce. So when agentx is killed outright, git goes too, rather than
// waiting on a host that does not answer with nothing left to end it.
func TestAnUnattendedGitDiesWithAgentx(t *testing.T) {
	t.Parallel()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "ready")
	writeShim(t, filepath.Join(dir, "git"), "#!/bin/sh\n[ \"$1\" = --version ] && exit 0\n"+
		"echo $$ > "+ready+"\nexec "+sleeper+" 300\n")
	child := exec.Command(os.Args[0], "-test.run=^TestUnattendedChildProcess$")
	child.Env = append(os.Environ(), unattendedChildEnv+"="+dir)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	var git int
	for deadline := time.Now().Add(30 * time.Second); git == 0; time.Sleep(10 * time.Millisecond) {
		if b, err := os.ReadFile(ready); err == nil && strings.HasSuffix(string(b), "\n") {
			if git, err = strconv.Atoi(strings.TrimSpace(string(b))); err != nil {
				t.Fatal(err)
			}
		} else if time.Now().After(deadline) {
			_ = child.Process.Kill()
			_ = child.Wait()
			t.Fatal("the unattended git never started")
		}
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	for deadline := time.Now().Add(10 * time.Second); alive(git); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(git, syscall.SIGKILL)
			t.Fatalf("git %d outlived the agentx that started it", git)
		}
	}
}

// alive tells whether the process pid still runs. One that has exited
// and that nobody has reaped yet, as an init that does not reap leaves it,
// is a zombie, which runs nothing.
func alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	// The state follows the command name, which is in parentheses and may
	// hold any character, so it is read after the last closing one.
	i := strings.LastIndexByte(string(b), ')')
	return err != nil || i < 0 || !strings.HasPrefix(strings.TrimLeft(string(b[i+1:]), " "), "Z")
}
