package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillListReportsManagedAndUnmanaged lists every directory of the
// library: the one an install manages, with its upstream and version, and
// one that was put there by hand, which agentx knows nothing about.
func TestSkillListReportsManagedAndUnmanaged(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	byHand := filepath.Join(h.library, "mine")
	if err := os.MkdirAll(byHand, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(byHand, "SKILL.md"), "---\nname: mine\ndescription: made here\n---\n\nmine\n")

	out := h.run("--json", "skill", "list")
	equal(t, "exit", out.exit, 0)
	events := h.eventsOfType(out.stdout, "library_skill")
	if len(events) != 2 {
		t.Fatalf("%d skill events, want 2:\n%s", len(events), out.stdout)
	}
	managed, unmanaged := events[0], events[1]
	equal(t, "the first name", managed["name"], "alpha")
	equal(t, "the first kind", managed["kind"], "managed")
	equal(t, "the first base hash", managed["base_hash"], managed["content_hash"])
	equal(t, "the second name", unmanaged["name"], "mine")
	equal(t, "the second kind", unmanaged["kind"], "unmanaged")
	for _, field := range []string{"source", "subpath", "upstream_commit", "base_hash", "state"} {
		if _, ok := unmanaged[field]; ok {
			t.Errorf("an unmanaged skill carries %s: %v", field, unmanaged[field])
		}
	}
	if _, ok := unmanaged["content_hash"]; !ok {
		t.Error("an unmanaged skill carries no content hash")
	}

	// An edit to the library directory is what tells a managed skill from
	// its base version.
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "edited\n")
	after := h.run("--json", "skill", "list")
	equal(t, "state", h.eventsOfType(after.stdout, "library_skill")[0]["state"], "modified")
}

// TestSkillListReadsLineageFromTheBranchesAlone is the criterion that keeps
// the listing independent of the sources: every source ref goes, and the
// listing does not change by one byte.
func TestSkillListReadsLineageFromTheBranchesAlone(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	before := h.run("--json", "skill", "list")
	equal(t, "exit", before.exit, 0)

	// Every source ref and the source itself go.
	equal(t, "exit", h.run("source", "remove", s.url).exit, 0)
	for _, ref := range strings.Split(h.accountGit("for-each-ref", "--format=%(refname)", "refs/agentx/sources/"), "\n") {
		if ref != "" {
			t.Fatalf("the source ref %s is still there", ref)
		}
	}
	after := h.run("--json", "skill", "list")
	equal(t, "exit", after.exit, 0)
	if after.stdout != before.stdout {
		t.Errorf("the listing changed when the sources went:\nbefore:\n%safter:\n%s", before.stdout, after.stdout)
	}
}

// TestSkillListReportsAFork reads the fork namespace too, so that a branch
// written there is listed as a fork rather than as an unmanaged directory.
// Nothing creates forks yet; the branch is made with plain git.
func TestSkillListReportsAFork(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	h.accountGit("update-ref", "refs/heads/skills/beta", h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	copyTree(t, filepath.Join(h.library, "alpha"), filepath.Join(h.library, "beta"))

	out := h.run("--json", "skill", "list")
	equal(t, "exit", out.exit, 0)
	events := h.eventsOfType(out.stdout, "library_skill")
	if len(events) != 2 {
		t.Fatalf("%d skill events, want 2:\n%s", len(events), out.stdout)
	}
	equal(t, "kind", events[1]["kind"], "fork")
	if _, ok := events[1]["state"]; ok {
		t.Error("a fork carries a state, which its own history decides and this command does not read")
	}
}

// TestSkillListWithoutALibrary says so and exits 0: a machine that has
// installed nothing is not a failure.
func TestSkillListWithoutALibrary(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	out := h.run("skill", "list")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "No skills in the library.")
	json := h.run("--json", "skill", "list")
	equal(t, "exit", json.exit, 0)
	equal(t, "events", strings.Join(h.types(h.events(json.stdout)), ","), "result")
}

// TestSkillListSpawnsOneGitProcess counts the git processes of a listing:
// the startup version check, and one for-each-ref over both namespaces.
// The scan the listing runs reads the filesystem and spawns nothing.
func TestSkillListSpawnsOneGitProcess(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	calls := countingGit(t, h)
	equal(t, "exit", h.run("skill", "list").exit, 0)
	refs := 0
	for _, call := range calls() {
		switch {
		case strings.Contains(call, "for-each-ref"):
			refs++
		case strings.Contains(call, "--version"), strings.Contains(call, "rev-parse --is-bare-repository"):
		default:
			t.Errorf("skill list ran git %s", call)
		}
	}
	equal(t, "for-each-ref calls", refs, 1)
}
