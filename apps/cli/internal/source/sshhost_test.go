package source

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
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
	writeShim(t, filepath.Join(bin, "ssh"), "#!/bin/sh\n"+
		"[ \"$1\" = --version ] && exit 0\n"+
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

// writeShim writes an executable script and waits until it can run: a
// parallel test that forks while the file is still open for writing makes
// exec fail with ETXTBSY.
func writeShim(t *testing.T, path, script string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for {
		if err := exec.Command(path, "--version").Run(); !errors.Is(err, syscall.ETXTBSY) {
			return
		}
		runtime.Gosched()
	}
}
