package cli

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sync"
	"testing"
)

var hexID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func showMachine(t *testing.T, h *harness) jsonEvent {
	t.Helper()
	out := h.run("--json", "machine")
	equal(t, "exit", out.exit, 0)
	equal(t, "stderr", out.stderr, "")
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"machine", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	id := events[0]["id"].(string)
	if !hexID.MatchString(id) {
		t.Errorf("id = %q, want 32 lowercase hex characters", id)
	}
	return events[0]
}

func TestMachineFromPlatformID(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	mac := hmac.New(sha256.New, []byte("agentx-machine-id/v1"))
	fmt.Fprintf(mac, "platform-test\n%d", os.Getuid())
	want := hex.EncodeToString(mac.Sum(nil))[:32]

	first := showMachine(t, h)
	equal(t, "id", first["id"], want)
	equal(t, "label", first["label"], "test-host")
	equal(t, "derivation", first["derivation"], "platform")

	second := showMachine(t, h)
	equal(t, "id on the second run", second["id"], want)
	if _, err := os.Stat(filepath.Join(h.agentx, "machine.json")); !os.IsNotExist(err) {
		t.Errorf("machine.json written although a platform id exists: %v", err)
	}

	h.env["AGENTX_PLATFORM_ID"] = "another-computer"
	other := showMachine(t, h)
	if other["id"] == want {
		t.Errorf("id did not change with the platform id")
	}

	out := h.run("machine")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "Machine id    "+other["id"].(string))
	contains(t, "stdout", out.stdout, "Label         test-host")
	contains(t, "stdout", out.stdout, "Derivation    platform")
}

// The machine event says where agentx keeps its files, as AGENTX_HOME and
// AGENTX_LIBRARY resolve them, and names the account repo with its size once
// it exists.
func TestMachinePaths(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	before := showMachine(t, h)
	equal(t, "home", before["home"], h.agentx)
	equal(t, "settings_file", before["settings_file"], filepath.Join(h.agentx, "settings.json"))
	equal(t, "library", before["library"], h.library)
	for _, key := range []string{"account_repo", "account_repo_bytes"} {
		if v, ok := before[key]; ok {
			t.Errorf("%s = %v before the account repo exists", key, v)
		}
	}
	out := h.run("machine")
	contains(t, "stdout", out.stdout, "Home          "+h.agentx+"\n")
	contains(t, "stdout", out.stdout, "Account repo  not created yet\n")

	h.mustRun("skill", "new", "notes") // the first command that needs the account repo creates it
	repo := filepath.Join(h.agentx, "account.git")
	size, err := dirSize(repo)
	if err != nil {
		t.Fatal(err)
	}
	if size == 0 {
		t.Fatal("the account repo is empty")
	}
	after := showMachine(t, h)
	equal(t, "account_repo", after["account_repo"], repo)
	equal(t, "account_repo_bytes", after["account_repo_bytes"], float64(size))

	out = h.run("machine")
	contains(t, "stdout", out.stdout, "Account repo  "+repo+" ("+formatBytes(size)+")\n")
}

func TestDirSize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(name string, n int) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("outside/big", 4096)
	write("repo/HEAD", 23)
	write("repo/objects/ab/cdef", 100)
	write("repo/worktrees/notes/index", 7)
	// A symlink is never followed: what lies outside the tree is not its size.
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "repo", "link")); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		dir  string
		want int64
	}{
		{"empty", 0},
		{"repo", 130},
		{"outside", 4096},
	} {
		got, err := dirSize(filepath.Join(root, tc.dir))
		if err != nil {
			t.Errorf("dirSize(%s): %v", tc.dir, err)
		}
		equal(t, "dirSize("+tc.dir+")", got, tc.want)
	}
	if _, err := dirSize(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Errorf("dirSize(missing) = %v, want a not-exist error", err)
	}
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{10 * 1024 * 1024, "10.0 MiB"},
		{3 * 1024 * 1024 * 1024, "3.0 GiB"},
		{2 * 1024 * 1024 * 1024 * 1024, "2.0 TiB"},
	} {
		equal(t, fmt.Sprintf("formatBytes(%d)", tc.n), formatBytes(tc.n), tc.want)
	}
}

func TestMachineRandomFallback(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.env["AGENTX_PLATFORM_ID"] = ""
	if err := os.Remove(h.agentx); err != nil {
		t.Fatal(err)
	}

	first := showMachine(t, h)
	equal(t, "derivation", first["derivation"], "random")

	equal(t, "machine.json id", readMachineFile(t, h), first["id"])

	second := showMachine(t, h)
	equal(t, "id on the second run", second["id"], first["id"])
	if _, err := os.Stat(filepath.Join(h.agentx, "version")); !os.IsNotExist(err) {
		t.Errorf("agentx machine bumped the version file: %v", err)
	}

	if err := os.WriteFile(filepath.Join(h.agentx, "machine.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := h.run("--json", "machine")
	equal(t, "exit", out.exit, 10)
	contains(t, "error.message", h.events(out.stdout)[0]["message"].(string), filepath.Join(h.agentx, "machine.json"))
}

// Concurrent first runs without a platform id agree on one stored id.
func TestMachineRandomFallbackConcurrent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.env["AGENTX_PLATFORM_ID"] = ""
	outs := make([]outcome, 4)
	var wg sync.WaitGroup
	for i := range outs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i] = h.run("--json", "machine")
		}()
	}
	wg.Wait()

	stored := readMachineFile(t, h)
	succeeded := 0
	for _, out := range outs {
		switch out.exit {
		case 0:
			equal(t, "id", h.events(out.stdout)[0]["id"], stored)
			succeeded++
		case 7:
		default:
			t.Errorf("agentx machine: exit %d, stdout %q", out.exit, out.stdout)
		}
	}
	if succeeded == 0 {
		t.Fatal("no run succeeded")
	}
}

func readMachineFile(t *testing.T, h *harness) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.agentx, "machine.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatalf("machine.json: %v\n%s", err, b)
	}
	return file.ID
}

func TestMachineResetID(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	before := showMachine(t, h)

	out := h.run("--json", "machine", "reset-id")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"machine", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	equal(t, "derivation after reset", events[0]["derivation"], "random")
	if events[0]["id"] == before["id"] {
		t.Errorf("id did not change after reset-id")
	}

	after := showMachine(t, h)
	equal(t, "id after reset", after["id"], events[0]["id"])
	equal(t, "derivation with a platform id present", after["derivation"], "random")
	equal(t, "version", readVersion(t, h), 1)

	out = h.run("machine", "reset-id")
	equal(t, "exit", out.exit, 0)
	again := showMachine(t, h)
	equal(t, "stdout", out.stdout, "✓ machine id is now "+again["id"].(string)+"\n")
	if again["id"] == after["id"] {
		t.Errorf("id did not change after the second reset-id")
	}
	equal(t, "version", readVersion(t, h), 2)
}

func TestMachineRename(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	out := h.run("--json", "machine", "rename", "desk")
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	equal(t, "event type", events[0]["type"], "machine")
	equal(t, "label", events[0]["label"], "desk")

	equal(t, "machine label", showMachine(t, h)["label"], "desk")
	out = h.run("config", "get", "label")
	equal(t, "stdout", out.stdout, "desk\n")
	equal(t, "file label", readSettingsFile(t, h)["label"], "desk")

	out = h.run("machine", "rename", "")
	equal(t, "exit", out.exit, 1)
	out = h.run("machine", "rename")
	equal(t, "exit", out.exit, 1)
	out = h.run("machine", "bogus")
	equal(t, "exit", out.exit, 1)
	contains(t, "stderr", out.stderr, `unknown command "bogus" for "agentx machine"`)
}
