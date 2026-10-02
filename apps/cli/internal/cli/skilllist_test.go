package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
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
// The branch is made with plain git and the library holds a directory of
// its own under its name, not the symlink into the fork's worktree, so the
// directory is not the fork's checkout and carries no state.
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
		t.Error("a fork whose library entry is no checkout of it carries a state")
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

// TestSkillListSpawnsOneGitProcessWhateverTheDrift counts the git processes
// of a listing and of a snapshot over skills that differ every way drift
// reads: alpha edited, a file of it made executable, with a candidate and a
// pending merge; beta displaced in one configuration, missing from another,
// removed upstream, and holding a file the system-file list names; a skill
// of the user's own; and a managed branch with no library directory and no
// lineage, which a warning names; and two greenfield skills, whose lineage
// their own history holds, one as its tip records it and one edited. All
// of it comes from one for-each-ref, one walk of the forks' histories and
// the filesystem: only the two edited skills ask git, each with a
// read-tree, an add and a write-tree, the fork whose directory holds its
// tip none, and the user's global ignore file is read once.
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
	writeFile(t, filepath.Join(h.library, "beta", ".DS_Store"), "finder\n")
	h.accountGit("update-ref", "refs/heads/managed/ghost", h.accountGit("commit-tree", "refs/heads/managed/beta^{tree}", "-m", "no lineage"))
	// A candidate is a commit of its own, as a check pins it: here alpha's
	// version again, with its lineage, on top of the branch. One the branch
	// already holds is no update, and lists as none.
	message := h.accountGit("log", "-1", "--format=%B", "refs/heads/managed/alpha")
	candidate := strings.TrimSpace(h.accountGit("commit-tree", "refs/heads/managed/alpha^{tree}", "-p", "refs/heads/managed/alpha", "-m", message))
	h.accountGit("update-ref", "refs/agentx/candidate/alpha", candidate)
	h.accountGit("update-ref", "refs/agentx/upstream-removed/beta", "refs/agentx/sources/"+source.ID(s.url))
	// A pending merge is a checkout of the account repo, as an update adds
	// one.
	h.accountGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason,
		pendingCheckout(h, "alpha"), h.ref(lineage.ManagedRef("alpha")))
	h.mustRun("skill", "new", "fresh")
	h.mustRun("skill", "new", "edited")
	writeFile(t, filepath.Join(h.library, "edited", "notes.md"), "notes\n")
	equal(t, "the edited fork's state", h.listed("edited")["state"], stateModified)
	equal(t, "the fresh fork's state", h.listed("fresh")["state"], stateCurrent)
	equal(t, "alpha's state", h.listed("alpha")["state"], stateModified)
	if h.listed("alpha")["candidate"] == nil {
		t.Error("alpha carries no candidate")
	}
	equal(t, "alpha's pending merge", h.listed("alpha")["pending_merge"], true)
	equal(t, "beta's drift", drift(h.listed("beta")), "displaced,missing,upstream removed")

	calls := countingGit(t, h)
	alpha := "--work-tree=" + filepath.Join(h.library, "alpha") + " "
	edited, err := filepath.EvalSymlinks(filepath.Join(h.library, "edited"))
	if err != nil {
		t.Fatal(err)
	}
	fork := "--work-tree=" + filepath.Dir(edited) + " "
	count := func(what string, calls []string) {
		t.Helper()
		var ran []string
		for _, call := range calls {
			_, inAlpha, _ := strings.Cut(call, alpha)
			if _, inFork, _ := strings.Cut(call, fork); inFork != "" {
				inAlpha = inFork
			}
			switch {
			case strings.Contains(call, "--version"), strings.Contains(call, "rev-parse --is-bare-repository"):
			case strings.Contains(call, "for-each-ref"):
				ran = append(ran, "for-each-ref")
			case strings.Contains(call, " log --stdin "):
				ran = append(ran, "log")
			case strings.HasSuffix(call, "config --path --get core.excludesFile"):
				ran = append(ran, "config")
			case inAlpha != "":
				ran = append(ran, strings.Fields(inAlpha)[0])
			default:
				t.Errorf("%s ran git %s", what, call)
			}
		}
		sort.Strings(ran)
		equal(t, what+": git runs", strings.Join(ran, " "), "add add config for-each-ref log read-tree read-tree write-tree write-tree")
	}
	ghost := "ghost is managed in the account repo but the library holds no skill directory for it;" +
		" run 'agentx skill add <source> --skill ghost' to install it again, or 'agentx skill remove ghost' to stop managing it"
	list := h.mustRun("skill", "list")
	equal(t, "skill list's warning", list.stderr, "warning: "+ghost+"\n")
	contains(t, "skill list", list.stdout, "modified, update available, merge pending  ")
	count("skill list", calls())
	before := len(calls())
	snap := h.snapshot(t)
	count("the snapshot", calls()[before:])
	states := map[string]string{}
	for _, e := range snap["library"].([]any) {
		entry := e.(map[string]any)
		states[entry["name"].(string)] = fmt.Sprint(entry["state"]) + " " + drift(entry) + " " + fmt.Sprint(entry["pending_merge"])
	}
	equal(t, "alpha in the snapshot", states["alpha"], stateModified+"  true")
	equal(t, "beta in the snapshot", states["beta"], stateCurrent+" displaced,missing,upstream removed <nil>")
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
