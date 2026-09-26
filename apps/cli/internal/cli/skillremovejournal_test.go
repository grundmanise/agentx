package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// TestARefusedRemovalLeavesTheSkillManaged is what the ordering of a
// removal's steps is for. A removal that cannot finish stops with exit code
// 6 and a hint offering two ways out, one of which is to move the journal
// aside and keep what is on disk. That offer has to be safe to take.
//
// It is not safe if the ref deletions have already run: the library still
// holds the directory, the import branch is gone, and the journal the user
// was invited to move aside was the last record of the commit it pointed
// at. Taking the offer would turn a managed skill into an unmanaged one for
// good, the one thing mutation safety says not to make of half-applied
// state. So the deletions run last, after every step that could still
// refuse, and a refused removal leaves the skill exactly as managed as it
// was.
func TestARefusedRemovalLeavesTheSkillManaged(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := refValue(t, h, "refs/heads/managed/alpha")
	if head == "" {
		t.Fatal("the install wrote no import branch")
	}

	// The journal a removal writes: its ref deletions, then the placement
	// it takes away. This one cannot be finished, because the placement now
	// sits under a path that is a regular file.
	blocked := filepath.Join(h.home, ".cursor", "blocked")
	writeFile(t, blocked, "not a directory\n")
	gitDir := filepath.Join(h.agentx, "account.git")
	writeJournalFile(t, h, `{"progress":"staged","replace":[],"steps":[`+
		`{"kind":"ref","git_dir":`+quote(gitDir)+`,"ref":"refs/heads/managed/alpha","old":"`+head+`","new":""},`+
		`{"kind":"remove","path":`+quote(filepath.Join(blocked, "alpha"))+`,"old":"link:`+filepath.Join(h.library, "alpha")+`","new":"absent"}]}`)

	out := h.run("skill", "list")
	equal(t, "exit", out.exit, 6)
	contains(t, "the refusal", out.stderr, "recovery required")
	equal(t, "the import branch", refValue(t, h, "refs/heads/managed/alpha"), head)

	// The hint's second way out: keep what is on disk. The skill is still
	// the managed skill it was, and its lineage did not go with the journal.
	moveJournalAside(t, h)
	listed := h.run("skill", "list")
	equal(t, "exit", listed.exit, 0)
	contains(t, "the listing", listed.stdout, "alpha")
	if strings.Contains(listed.stdout, "unmanaged") {
		t.Errorf("abandoning the journal made a managed skill unmanaged:\n%s", listed.stdout)
	}
}

// refValue is what a ref of the account repo holds, empty when it holds
// nothing: the removal under test may have deleted it, which is the thing
// being measured rather than a reason to fail on the spot.
func refValue(t *testing.T, h *harness, ref string) string {
	t.Helper()
	out, err := h.accountGitErr("for-each-ref", "--format=%(objectname)", ref)
	if err != nil {
		t.Fatalf("for-each-ref %s: %v", ref, err)
	}
	return strings.TrimSpace(out)
}

// moveJournalAside does what the refusal's hint offers: takes the journal
// out of agentx home and leaves the machine as the interrupted run left it.
func moveJournalAside(t *testing.T, h *harness) {
	t.Helper()
	dir := home.MutationsDir(h.agentx)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	moved := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if err := os.Rename(filepath.Join(dir, e.Name()), filepath.Join(t.TempDir(), e.Name())); err != nil {
			t.Fatal(err)
		}
		moved++
	}
	if moved == 0 {
		t.Fatal("no journal was left for the user to move aside")
	}
}
