package source

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/testshim"
)

func TestSSHHostname(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ out, want string }{
		{"user git\nhostname github.com\nport 22\n", "github.com"},
		{"hostname ssh.github.com\r\n", "ssh.github.com"},
		{"hostnamefoo bar\nport 22\n", ""},
		{"", ""},
	} {
		if got := sshHostname(tc.out); got != tc.want {
			t.Errorf("sshHostname(%q) = %q, want %q", tc.out, got, tc.want)
		}
	}
}

// TestSSHHosts runs the resolver against an ssh that answers for one alias
// and logs every call, so that it shows what is asked of ssh and how often:
// once per host, never for a host that would read as an option, and not at
// all where the environment has no ssh.
func TestSSHHosts(t *testing.T) {
	t.Parallel()
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	testshim.Write(t, filepath.Join(bin, "ssh"), "#!/bin/sh\n"+
		"echo \"$*\" >> "+log+"\n"+
		"[ \"$3\" = github-work ] && { echo 'user git'; echo 'hostname github.com'; exit 0; }\n"+
		"[ \"$3\" = broken ] && exit 255\n"+
		"echo \"hostname $3\"\n")
	resolve := SSHHosts(context.Background(), map[string]string{"PATH": bin})
	for _, tc := range []struct{ host, want string }{
		{"github-work", "github.com"},
		{"github-work", "github.com"},
		{"example.com", "example.com"},
		{"broken", "broken"},
		{"-oProxyCommand=x", "-oProxyCommand=x"},
	} {
		if got := resolve(tc.host); got != tc.want {
			t.Errorf("resolve(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(calls), "-G -- github-work\n-G -- example.com\n-G -- broken\n"; got != want {
		t.Errorf("ssh was run as\n%s, want\n%s", got, want)
	}
	if got := SSHHosts(context.Background(), map[string]string{"PATH": t.TempDir()})("github-work"); got != "github-work" {
		t.Errorf("without ssh, resolve(github-work) = %q", got)
	}
}

// TestSSHHostsReadsAnSSHThatLeftAProcessRunning: an ssh that exits while a
// process it started still holds its output, as a Match exec command can,
// resolves to the host name it printed.
func TestSSHHostsReadsAnSSHThatLeftAProcessRunning(t *testing.T) {
	t.Parallel()
	bin := t.TempDir()
	left := filepath.Join(t.TempDir(), "left")
	t.Cleanup(func() {
		if b, err := os.ReadFile(left); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	sleeper, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	testshim.Write(t, filepath.Join(bin, "ssh"), "#!/bin/sh\necho 'hostname github.com'\n"+
		sleeper+" 60 &\necho $! > "+left+"\n")
	if got := SSHHosts(context.Background(), map[string]string{"PATH": bin})("github-work"); got != "github.com" {
		t.Errorf("resolve(github-work) = %q, want %q", got, "github.com")
	}
}
