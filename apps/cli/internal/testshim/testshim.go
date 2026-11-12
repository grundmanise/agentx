// Package testshim writes the executables that tests run in place of git,
// ssh and the hooks of a repository.
package testshim

import (
	"os"
	"syscall"
	"testing"
)

// Write writes an executable script at path that the test can run at once.
//
// The tests of a package share one process and start processes in parallel.
// A fork duplicates every descriptor open in the process, so a child forked
// while the script is open for writing keeps it open until the child execs,
// and running the script in that time fails with ETXTBSY. Write holds
// syscall.ForkLock for reading from the open to the close, which keeps
// every fork of the process out of that window: no child inherits the
// descriptor, and nothing is left to wait for or to retry.
func Write(t testing.TB, path, script string) {
	t.Helper()
	syscall.ForkLock.RLock()
	err := os.WriteFile(path, []byte(script), 0o755)
	syscall.ForkLock.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
}
