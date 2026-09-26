package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
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
// the listing independent of what was fetched: every source ref goes, and
// the listing does not change by one byte, in text or in JSON. The sources
// stay in the settings, which is what an import leaves too: a source this
// machine has and holds no fetch of is not a source removed.
func TestSkillListReadsLineageFromTheBranchesAlone(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	beforeJSON := h.mustRun("--json", "skill", "list")
	beforeText := h.mustRun("skill", "list")

	// Every source ref goes, with plain git, behind agentx's back.
	for _, ref := range strings.Split(h.accountGit("for-each-ref", "--format=%(refname)", "refs/agentx/sources/"), "\n") {
		if ref != "" {
			h.accountGit("update-ref", "-d", ref)
		}
	}
	if refs := h.accountGit("for-each-ref", "refs/agentx/sources/"); refs != "" {
		t.Fatalf("source refs are still there:\n%s", refs)
	}
	for _, c := range []struct{ what, before, after string }{
		{"JSON", beforeJSON.stdout, h.mustRun("--json", "skill", "list").stdout},
		{"text", beforeText.stdout, h.mustRun("skill", "list").stdout},
	} {
		if c.after != c.before {
			t.Errorf("the %s listing changed when the source refs went:\nbefore:\n%safter:\n%s", c.what, c.before, c.after)
		}
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

// TestSkillListSpawnsOneGitProcessWhateverTheDrift holds the budget above
// for skills that differ every way drift reads: one edited, a file of it
// made executable too, one whose link became a real directory in one
// configuration and whose placement is gone from another, and a skill of
// the user's own beside them, and a managed branch whose library directory
// is gone and whose commit carries no lineage, which a warning names with
// <source> for the source it cannot name. Whether a skill is modified,
// displaced, missing or gone is read in process, so the listing still runs
// one for-each-ref, and so does the snapshot.
func TestSkillListSpawnsOneGitProcessWhateverTheDrift(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--skill", "beta")
	writeFile(t, filepath.Join(h.library, "alpha", "SKILL.md"), skill("alpha", "Edited in the library"))
	chmod(t, filepath.Join(h.library, "alpha", "notes.md"), 0o755)
	claude := filepath.Join(h.home, ".claude", "skills", "beta")
	remove(t, claude)
	copyTree(t, filepath.Join(h.library, "beta"), claude)
	remove(t, filepath.Join(h.home, ".cursor", "skills", "beta"))
	writeFile(t, mkdirs(t, filepath.Join(h.library, "mine"), "SKILL.md"), skill("mine", "A skill of my own"))
	h.accountGit("update-ref", "refs/heads/managed/ghost", h.accountGit("commit-tree", "refs/heads/managed/beta^{tree}", "-m", "no lineage"))
	equal(t, "alpha's state", h.listed("alpha")["state"], stateModified)
	equal(t, "beta's drift", drift(h.listed("beta")), "displaced,missing")

	calls := countingGit(t, h)
	count := func(what string, calls []string) {
		t.Helper()
		refs := 0
		for _, call := range calls {
			switch {
			case strings.Contains(call, "for-each-ref"):
				refs++
			case strings.Contains(call, "--version"), strings.Contains(call, "rev-parse --is-bare-repository"):
			default:
				t.Errorf("%s ran git %s", what, call)
			}
		}
		equal(t, what+": for-each-ref calls", refs, 1)
	}
	ghost := "ghost is managed in the account repo but the library holds no skill directory for it;" +
		" run 'agentx skill add <source> --skill ghost' to install it again, or 'agentx skill remove ghost' to stop managing it"
	equal(t, "skill list's warning", h.mustRun("skill", "list").stderr, "warning: "+ghost+"\n")
	count("skill list", calls())
	before := len(calls())
	snap := h.snapshot(t)
	count("the snapshot", calls()[before:])
	states := map[string]string{}
	for _, e := range snap["library"].([]any) {
		entry := e.(map[string]any)
		states[entry["name"].(string)] = fmt.Sprint(entry["state"]) + " " + drift(entry)
	}
	equal(t, "alpha in the snapshot", states["alpha"], stateModified+" ")
	equal(t, "beta in the snapshot", states["beta"], stateCurrent+" displaced,missing")
	contains(t, "the snapshot's warnings", fmt.Sprint(snap["warnings"]), ghost)
}

// TestSkillListSanitisesTheNameAndTheUpstream covers a library directory
// whose name carries what a terminal obeys. The library is a plain
// directory of this machine that anything may write into, and the name of
// an unmanaged skill is that directory's own, so it is text agentx did not
// write. Left raw, a directory named across two lines prints one skill as
// two rows: the one row per item the contract promises, broken by whoever
// made the directory.
func TestSkillListSanitisesTheNameAndTheUpstream(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const raw = "two\nrows \x1b[31mRED\x1b[0m"
	dir := filepath.Join(h.library, raw)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: mine\ndescription: made here\n---\n\nmine\n")

	out := h.run("skill", "list")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "1 skill\n  two rows [31mRED [0m  unmanaged  -  (none)  0 placements\n")
	if lines := strings.Count(out.stdout, "\n"); lines != 2 {
		t.Errorf("one skill printed on %d rows:\n%q", lines-1, out.stdout)
	}
	for _, r := range out.stdout {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("a control character reached the listing: %q in\n%q", r, out.stdout)
		}
	}

	// The event carries the directory name as it is, the way JSON carries
	// every value agentx did not write.
	events := h.run("--json", "skill", "list")
	equal(t, "exit", events.exit, 0)
	equal(t, "name", h.eventsOfType(events.stdout, "library_skill")[0]["name"], raw)

	// The upstream is the row's other value a source has a hand in: the URL
	// a source was added from and one of its directories. Neither can carry
	// a control character today (a URL that does is not a source URL, and
	// lineage.ValidPath refuses a subpath that does, so such an import
	// commit is not read back at all), so the row is held to the rule here,
	// where the cell is built, rather than through a source that cannot
	// reach it.
	subpath := "na\x1b[31msty"
	cells := row(&writer{}, librarySkillEvent{Name: "alpha", Source: "file:///s", Subpath: &subpath})
	equal(t, "upstream", cells[3].text, "file:///s/na [31msty")
}
