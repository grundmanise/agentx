package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// TestSkillAddSkipsAPlacementItCannotMake installs into a machine where one
// client's skills directory cannot hold a placement. The contract skips a
// placement path something else holds, with a warning and a count in the
// result; a directory agentx cannot write is the same answer. An install
// that aborted instead would leave the user with an unfinished journal and
// a machine every later command fails on.
func TestSkillAddSkipsAPlacementItCannotMake(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		build func(t *testing.T, h *harness, dir string)
	}{{
		// A regular file where the client's skills directory belongs: the
		// placement path cannot even be read, let alone written.
		name: "a file where the skills directory belongs",
		build: func(t *testing.T, h *harness, dir string) {
			writeFile(t, dir, "not a directory\n")
		},
	}, {
		// A skills directory of another owner, a read-only mount or, on
		// macOS, one the user has not granted access to.
		name: "a skills directory that cannot be written",
		build: func(t *testing.T, h *harness, dir string) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			if os.Geteuid() == 0 {
				t.Skip("root writes a read-only directory anyway")
			}
		},
	}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s := installHarness(t)
			c.build(t, h, filepath.Join(h.home, ".cursor", "skills"))

			out := h.run("--json", "skill", "add", s.url, "--skill", "alpha")
			if out.exit != 0 {
				t.Fatalf("skill add: exit %d, want the placement skipped and the install done\n%s", out.exit, out.stderr)
			}
			contains(t, "stderr", out.stderr, filepath.Join(h.home, ".cursor", "skills", "alpha"))
			contains(t, "stderr", out.stderr, "no placement was made for cursor")
			result := h.one(out.stdout, "result")
			contains(t, "the result", result["summary"].(string), "1 placement skipped")

			// The rest of the install is whole, and nothing is left for a
			// later command to trip over.
			if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
				t.Errorf("the library directory was not written: %v", err)
			}
			if _, err := os.Readlink(filepath.Join(h.home, ".claude", "skills", "alpha")); err != nil {
				t.Errorf("the placement of a configuration that works was not made: %v", err)
			}
			equal(t, "journals", journalCount(t, h), 0)
			// The machine still answers every command afterwards.
			equal(t, "scan", h.run("scan").exit, 0)
			equal(t, "skill list", h.run("skill", "list").exit, 0)
			equal(t, "config set", h.run("config", "set", "label", "after").exit, 0)
		})
	}
}

// TestRecoveryRefusesAStepItCannotApplyCleanly drives a command over a
// journal whose placement step cannot be applied at all. A step that cannot
// be finished is a refused recovery — exit code 6 naming the path and the
// journal, with the hint that says how to get out of it — and never an
// internal error carrying a raw operating system message, which tells the
// user nothing and wedges every later command the same way.
func TestRecoveryRefusesAStepItCannotApplyCleanly(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)

	// A journal left by an interrupted install whose placement lands under
	// a path that is a regular file: the step can never be applied.
	blocked := filepath.Join(h.home, ".cursor", "blocked")
	writeFile(t, blocked, "not a directory\n")
	writeJournalFile(t, h, `{"progress":"staged","replace":[],"steps":[`+
		`{"kind":"link","path":`+quote(filepath.Join(blocked, "alpha"))+`,"old":"absent","new":"link:`+filepath.Join(h.library, "alpha")+`"}]}`)

	out := h.run("--json", "config", "set", "label", "wedged")
	equal(t, "exit", out.exit, 6)
	ev := lastError(t, h.events(out.stdout))
	equal(t, "code", ev["code"], "refused")
	contains(t, "the message", ev["message"].(string), "recovery required")
	contains(t, "the message", ev["message"].(string), filepath.Join(blocked, "alpha"))
	if ev["hint"] == nil || !strings.Contains(ev["hint"].(string), "move the journal aside") {
		t.Errorf("hint = %v, want it to say how to get out of the journal", ev["hint"])
	}
	// doctor still names the journal, and every other command says the same
	// thing rather than a different one.
	equal(t, "doctor", h.run("doctor").exit, 0)
	equal(t, "scan", h.run("scan").exit, 6)
	equal(t, "skill list", h.run("skill", "list").exit, 6)
}

// TestCommandsRefuseAJournalWhoseAccountRepoIsGone leaves a journal with a
// ref step and takes the account repo away. The contract calls an unusable
// account repo exit code 8 wherever it surfaces, a journal's ref step
// included.
func TestCommandsRefuseAJournalWhoseAccountRepoIsGone(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	gitDir := filepath.Join(h.agentx, "account.git")
	writeJournalFile(t, h, `{"progress":"staged","replace":[],"steps":[`+
		`{"kind":"ref","git_dir":`+quote(gitDir)+`,"ref":"refs/heads/managed/beta","old":"","new":"0123456789abcdef0123456789abcdef01234567"}]}`)
	if err := os.RemoveAll(gitDir); err != nil {
		t.Fatal(err)
	}

	out := h.run("--json", "config", "set", "label", "gone")
	equal(t, "exit", out.exit, 8)
	equal(t, "code", lastError(t, h.events(out.stdout))["code"], "account_repo")
}

// writeJournalFile puts one journal into agentx home, the way a process
// stopped between its journal and its first live write leaves one.
func writeJournalFile(t *testing.T, h *harness, body string) {
	t.Helper()
	var check map[string]any
	if err := json.Unmarshal([]byte(body), &check); err != nil {
		t.Fatalf("the test wrote a journal that is not JSON: %v", err)
	}
	dir := home.MutationsDir(h.agentx)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "1700000000000000000-00000000.json"), body+"\n")
}

// quote renders a path as a JSON string.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
