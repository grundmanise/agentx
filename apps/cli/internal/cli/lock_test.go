package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func readVersion(t *testing.T, h *harness) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.agentx, "version"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(string(b[:len(b)-1]))
	if err != nil || b[len(b)-1] != '\n' {
		t.Fatalf("version file %q is not an integer line", b)
	}
	return n
}

func TestMutationBumpsVersionAndCreatesOps(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if err := os.Remove(h.agentx); err != nil {
		t.Fatal(err)
	}

	out := h.run("config", "set", "label", "one")
	equal(t, "exit", out.exit, 0)
	equal(t, "version", readVersion(t, h), 1)

	out = h.run("config", "set", "label", "two")
	equal(t, "exit", out.exit, 0)
	equal(t, "version", readVersion(t, h), 2)

	out = h.run("machine", "rename", "three")
	equal(t, "exit", out.exit, 0)
	equal(t, "version", readVersion(t, h), 3)

	// Agentx home holds exactly the lock, the empty mutations and ops directories, the
	// settings and the version file: no temp file is left behind.
	equal(t, "home entries", listDir(t, h.agentx), "lock mutations ops settings.json version")
	equal(t, "mutations entries", listDir(t, filepath.Join(h.agentx, "mutations")), "")
	equal(t, "ops entries", listDir(t, filepath.Join(h.agentx, "ops")), "")
}

func listDir(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, " ")
}

func TestHeldLockExits7(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	lock := filepath.Join(h.agentx, "lock")
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{{"config", "set", "label", "blocked"}, {"machine", "rename", "blocked"}, {"machine", "reset-id"}} {
		out := h.run(append([]string{"--json"}, args...)...)
		equal(t, "exit", out.exit, 7)
		events := h.events(out.stdout)
		equal(t, "error.code", events[0]["code"], "locked")
		contains(t, "error.hint", events[0]["hint"].(string), lock)
	}
	out := h.run("config", "set", "label", "blocked")
	equal(t, "exit", out.exit, 7)
	contains(t, "stderr", out.stderr, lock)

	// Reads are not blocked by the mutation lock.
	out = h.run("config", "get", "label")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "test-host\n")
	if _, err := os.Stat(filepath.Join(h.agentx, "version")); !os.IsNotExist(err) {
		t.Errorf("a locked-out write bumped the version file: %v", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	out = h.run("config", "set", "label", "free")
	equal(t, "exit", out.exit, 0)
	equal(t, "label", readSettingsFile(t, h)["label"], "free")
	equal(t, "version", readVersion(t, h), 1)
}

func TestConcurrentConfigWrites(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const n = 8
	type write struct {
		key, value string
		out        outcome
	}
	writes := make([]write, n)
	for i := range writes {
		switch i % 3 {
		case 0:
			writes[i] = write{"label", "label-" + strconv.Itoa(i), outcome{}}
		case 1:
			writes[i] = write{"auto_push", "true", outcome{}}
		case 2:
			writes[i] = write{"accept_operations", "true", outcome{}}
		}
	}
	var wg sync.WaitGroup
	for i := range writes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			writes[i].out = h.run("config", "set", writes[i].key, writes[i].value)
		}()
	}
	wg.Wait()

	succeeded := map[string][]string{}
	for _, w := range writes {
		switch w.out.exit {
		case 0:
			succeeded[w.key] = append(succeeded[w.key], w.value)
		case 7:
		default:
			t.Errorf("config set %s %s: exit %d, stderr %q", w.key, w.value, w.out.exit, w.out.stderr)
		}
	}
	if len(succeeded) == 0 {
		t.Fatal("no write succeeded")
	}
	file := readSettingsFile(t, h)
	equal(t, "version", readVersion(t, h), len(succeeded["label"])+len(succeeded["auto_push"])+len(succeeded["accept_operations"]))
	for _, key := range []string{"auto_push", "accept_operations"} {
		equal(t, key, file[key], len(succeeded[key]) > 0)
	}
	if labels := succeeded["label"]; len(labels) > 0 {
		found := false
		for _, l := range labels {
			found = found || file["label"] == l
		}
		if !found {
			t.Errorf("label = %v, want one of %v", file["label"], labels)
		}
	} else if _, ok := file["label"]; ok {
		t.Errorf("label = %v, want none", file["label"])
	}
}
