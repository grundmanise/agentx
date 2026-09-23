package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillAddSpawnsABoundedNumberOfGitProcesses counts the git processes
// of one install over a source with many skills and a skill with many
// files: the count follows neither. The reads that do not depend on each
// other run at once, so the gate that holds two of them open at the same
// time is passed, and the count is the same whether they do or not.
func TestSkillAddSpawnsABoundedNumberOfGitProcesses(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex", ".gemini"}})
	s := h.newSourceRepo("many", true)
	for i := range 20 {
		s.skill(fmt.Sprintf("skills/skill%d", i), fmt.Sprintf("skill%d", i), "One of many", nil)
	}
	files := map[string]string{}
	for i := range 40 {
		files[fmt.Sprintf("notes/%d.md", i)] = fmt.Sprintf("note %d\n", i)
	}
	s.skill("skills/big", "big", "A skill of many files", files)
	s.commit("a source of many skills")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)

	// The wrapper goes on the PATH only now: the add above must not open the
	// gate the install has to open for itself. Two reads of the account repo
	// that do not depend on each other pass through it together.
	counts := gatedGitOn(t, h, 2, `*" rev-list "*|*" show "*`)
	out := h.run("skill", "add", s.url, "--skill", "big")
	equal(t, "exit", out.exit, 0)
	peak, total := counts()
	if peak < 2 {
		t.Errorf("at most %d git processes ran at once: the independent reads were run one after another", peak)
	}
	if bound := 18; total > bound {
		t.Errorf("%d git processes for one install, want at most %d", total, bound)
	}
	// The same install from a source with one small skill costs the same:
	// nothing here is per skill of the source or per file of the skill.
	small := newHarness(t)
	small.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex", ".gemini"}})
	tiny := small.newSourceRepo("one", true)
	tiny.skill("only", "only", "The one skill", map[string]string{"notes.md": "one note\n"})
	tiny.commit("one skill")
	equal(t, "exit", small.run("source", "add", tiny.url).exit, 0)
	tinyCounts := countingGit(t, small)
	equal(t, "exit", small.run("skill", "add", tiny.url).exit, 0)
	if got := len(tinyCounts()); got != total {
		t.Errorf("a small install ran %d git processes and a large one %d", got, total)
	}
}

// TestSkillAddFetchesTheBlobsInOneBatch checks that the files of the skill
// come down in one fetch, and that installing the same version again needs
// no network at all.
func TestSkillAddFetchesTheBlobsInOneBatch(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	out := h.run("--verbose", "skill", "add", s.url, "--skill", "alpha")
	equal(t, "exit", out.exit, 0)
	equal(t, "fetches", fetches(out.stderr), 1)

	// The same version again, in another home that fetched the source but
	// holds no blobs yet, still costs one fetch; in this home, where the
	// blobs are already in the account repo, it costs none.
	if err := os.RemoveAll(filepath.Join(h.library, "alpha")); err != nil {
		t.Fatal(err)
	}
	again := h.run("--verbose", "skill", "add", s.url, "--skill", "alpha")
	equal(t, "exit", again.exit, 0)
	equal(t, "fetches of a version already held", fetches(again.stderr), 0)
}

// TestSkillAddCreatesTheBranchWithAnExpectedOldValue checks the update-ref
// the install runs: the branch is created with an empty expected old value,
// so that two commands cannot both claim the name.
func TestSkillAddCreatesTheBranchWithAnExpectedOldValue(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	calls := countingGit(t, h)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	var updates []string
	for _, call := range calls() {
		if strings.Contains(call, "update-ref") {
			updates = append(updates, call)
		}
	}
	if len(updates) != 1 {
		t.Fatalf("%d update-ref calls, want 1: %v", len(updates), updates)
	}
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	// The wrapper joins the arguments with spaces, so an expected old value
	// of empty is the trailing space after the commit: the branch is created
	// only when it does not exist yet. What that argument does to git is
	// TestRefsUpdateWithAnExpectedOldValue in the gitx package.
	want := "update-ref refs/heads/managed/alpha " + head + " "
	if !strings.HasSuffix(strings.TrimRight(updates[0], "\n"), want) {
		t.Errorf("the update-ref call was %q, want it to end with %q", updates[0], want)
	}
}
