package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestServeAutoPushesAfterTheQuietPeriod runs one serve over a published
// fork with a commit the account remote lacks and an edit nobody
// committed, and a fork never published, with a quiet period of a few
// milliseconds. With auto_push off, as it is by default, nothing is pushed
// however many quiet periods pass. Turned on while serve runs, the commit
// is pushed once the branch stood still for a quiet period, reported as a
// publish of this serve process that names the uncommitted edit, which
// stays uncommitted: auto-push never commits, and the branch holds the
// commit it held. The fork never published is not pushed: a branch the
// account remote does not hold is for agentx publish to make. A fork whose
// branch there holds its tip and more has nothing to push, and is not
// warned about. A fork the account remote holds commits of that it lacks,
// and that holds commits of its own, is not pushed, and is warned about
// once, and nor is one whose branch there is another fork, by its fork id,
// although the push would fast-forward it. Last, with a longer quiet
// period, a commit serve finds at start and one made right after are
// pushed as one, the second, once the branch stood still.
func TestServeAutoPushesAfterTheQuietPeriod(t *testing.T) {
	t.Parallel()
	h, _, skillDir, _ := forkHarness(t)
	h.mustRun("skill", "new", "drafts")
	remote := newAccountRemote(t, h)
	h.mustRun("remote", "set", remote)
	h.mustRun("publish", "notes")
	published := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(skillDir, "notes.md"), "committed, not published\n")
	h.mustRun("skill", "commit", "notes")
	tip := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(skillDir, "draft.md"), "never committed\n")
	h.env["AGENTX_CHECK_INTERVAL"] = "1h"
	h.env["AGENTX_PUSH_QUIET"] = "20ms"
	remoteTip := func() string { return remoteGit(t, h, remote, "rev-parse", "refs/heads/skills/notes") }

	p := h.serve(t, "--json")
	p.next("snapshot")
	time.Sleep(300 * time.Millisecond) // fifteen quiet periods with auto_push off
	equal(t, "the remote's notes, auto_push off", remoteTip(), published)

	h.mustRunBeside("config", "set", "auto_push", "true")
	ev := p.nextOf("publish")
	equal(t, "name", ev["name"], "notes")
	equal(t, "outcome", ev["outcome"], publishPushed)
	equal(t, "commit", ev["commit"], tip)
	equal(t, "uncommitted", ev["uncommitted"], true)
	if id, _ := ev["instance_id"].(string); id == "" {
		t.Error("the publish names no serve process")
	}
	equal(t, "the remote's notes", remoteTip(), tip)
	equal(t, "the branch", h.ref(lineage.ForkRef("notes")), tip)
	equal(t, "git status", strings.TrimSpace(gitIn(t, h, filepath.Join(h.agentx, "worktrees", "notes"), "status", "--porcelain")), "?? notes/draft.md")

	// Another machine publishes a commit this one lacks, which a fetch
	// brings here: notes has nothing to push, and nothing is said of it.
	// Then this one commits one of its own: the push would not be a
	// fast-forward, so auto-push leaves it to a publish, once, with a
	// warning that names it.
	theirs := h.accountGit("commit-tree", tip+"^{tree}", "-p", tip, "-m", "made on another machine")
	h.accountGit("push", "--quiet", remote, theirs+":refs/heads/skills/notes")
	h.mustRunBeside("skill", "list", "--remote")
	time.Sleep(100 * time.Millisecond) // five quiet periods behind the remote
	equal(t, "warnings, behind", len(p.logged("warn", "auto-push: ")), 0)
	h.mustRunBeside("skill", "commit", "notes")
	warned := p.awaitLogged("warn", "auto-push: ", 1)
	equal(t, "the warning", warned[0], "auto-push: notes was not pushed, since the account remote holds commits it lacks; run 'agentx publish notes' to take them in and publish it")
	equal(t, "the remote's notes, diverged", remoteTip(), theirs)

	// This machine's notes becomes another fork of the name, as renaming
	// it away and back makes it, on top of the remote's: the push would be
	// a fast-forward, and is not made.
	other := h.accountGit("commit-tree", h.ref(lineage.ForkRef("notes"))+"^{tree}", "-p", theirs, "-m", "Fork notes\n\nAgentx-Fork-ID: fedcba98-7654-4321-8fed-cba987654321")
	h.accountGit("update-ref", lineage.ForkRef("notes"), other)
	warned = p.awaitLogged("warn", "auto-push: ", 2)
	equal(t, "the second warning", warned[1], "auto-push: the account remote's skills/notes is a different fork than notes on this machine, so notes cannot be published; rename yours with 'agentx skill rename notes <new>', then publish that one")
	equal(t, "the remote's notes, another fork", remoteTip(), theirs)
	equal(t, "exit", p.close(), 0)
	equal(t, "warnings", len(p.logged("warn", "auto-push: ")), 2)
	equal(t, "the remote's drafts", remoteGit(t, h, remote, "for-each-ref", "refs/heads/skills/drafts"), "")

	// A commit serve finds at start, and another right after, go out as
	// one push of the second, once the branch stood still for a quiet
	// period: the first is never pushed, and nothing is pushed sooner.
	h.mustRun("publish", "drafts")
	drafts := h.forkDir("drafts", "drafts")
	draftsTip := func() string { return remoteGit(t, h, remote, "rev-parse", "refs/heads/skills/drafts") }
	before := draftsTip()
	writeFile(t, filepath.Join(drafts, "one.md"), "first\n")
	h.mustRun("skill", "commit", "drafts")
	h.env["AGENTX_PUSH_QUIET"] = "2s"
	started := time.Now()
	p = h.serve(t, "--json")
	p.next("snapshot")
	writeFile(t, filepath.Join(drafts, "two.md"), "second\n")
	h.mustRunBeside("skill", "commit", "drafts")
	second := h.ref(lineage.ForkRef("drafts"))
	equal(t, "the remote's drafts before the quiet period", draftsTip(), before)
	ev = p.nextOf("publish")
	// A lower bound, which a loaded machine cannot break: no tick before
	// serve started can have seen either commit.
	if waited := time.Since(started); waited < 2*time.Second {
		t.Errorf("the push came %s after serve started, within the quiet period", waited)
	}
	equal(t, "the pushed fork", ev["name"], "drafts")
	equal(t, "the commit pushed, the second", ev["commit"], second)
	equal(t, "the remote's drafts", draftsTip(), second)
	equal(t, "exit of the second serve", p.close(), 0)
}

// mustRunBeside is mustRun for a command run while serve runs: it runs it
// again for as long as it finds the lock held, exit code 7, which it may
// while the update check serve starts with holds the lock through its
// fetch, as a user would retry it. Any other failure fails the test.
func (h *harness) mustRunBeside(args ...string) outcome {
	h.t.Helper()
	deadline := time.Now().Add(serveDeadline)
	for {
		out := h.run(args...)
		if out.exit == exitLocked.exit && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if out.exit != 0 {
			h.t.Fatalf("agentx %s: exit %d\n%s%s", strings.Join(args, " "), out.exit, out.stdout, out.stderr)
		}
		return out
	}
}
