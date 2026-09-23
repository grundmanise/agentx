package cli

import (
	"fmt"
	"os"
	"os/exec"
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
	// Not parallel, for the reason harness_test.go gives above
	// suiteParallel: it counts processes over three homes, one of thirty
	// skills, and must not share a machine with other tests' forks.
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
	counts := gatedGitOn(t, h, 2, `*" rev-list "*|*" log "*`)
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

	// And the bound is not per skill: a batch of thirty skills costs exactly
	// what one skill costs, because every read, every tree write, every
	// commit and every branch of a run is one git process for the whole run.
	bulk := newHarness(t)
	bulk.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex", ".gemini"}})
	many := bulk.newSourceRepo("thirty", true)
	for i := range 30 {
		many.skill(fmt.Sprintf("skills/skill%02d", i), fmt.Sprintf("skill%02d", i), "One of thirty",
			map[string]string{"notes.md": fmt.Sprintf("note %d\n", i)})
	}
	many.commit("thirty skills")
	equal(t, "exit", bulk.run("source", "add", many.url).exit, 0)
	bulkCounts := countingGit(t, bulk)
	bulkOut := bulk.run("skill", "add", many.url, "--all")
	equal(t, "exit", bulkOut.exit, 0)
	calls := bulkCounts()
	if len(calls) > total {
		t.Errorf("%d git processes for thirty skills, want at most the %d one skill costs:\n%s", len(calls), total, strings.Join(calls, "\n"))
	}
	imports := 0
	for _, call := range calls {
		if strings.Contains(call, "fast-import") {
			imports++
		}
	}
	if imports != 1 {
		t.Errorf("%d fast-import processes for thirty skills, want 1", imports)
	}
	// The upstream commit of every skill comes out of one walk of the
	// source's history, which is one of the processes counted above. The
	// log is read whole: two processes that start together can write their
	// lines into each other.
	all := strings.Join(calls, "\n")
	if walks := strings.Count(all, " log --full-history "); walks != 1 {
		t.Errorf("%d history walks for thirty skills of one source, want 1:\n%s", walks, all)
	}
	for i := range 30 {
		contains(t, "the history walk", all, fmt.Sprintf(" :(literal)skills/skill%02d", i))
	}
	for i := range 30 {
		name := fmt.Sprintf("skill%02d", i)
		if _, err := os.Stat(filepath.Join(bulk.library, name, "SKILL.md")); err != nil {
			t.Errorf("the library holds no %s: %v", name, err)
		}
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
// the install runs: every branch of a run is created in one transaction
// with an empty expected old value, so that two commands cannot both claim
// a name and no run leaves half its branches behind. The refs go on git's
// standard input, so that is where the test reads them.
func TestSkillAddCreatesTheBranchWithAnExpectedOldValue(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	fed := stdinGit(t, h, "update-ref")
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha", "--skill", "beta").exit, 0)

	var created, deleted []string
	for _, in := range fed() {
		if strings.Contains(in, "update ") {
			created = append(created, in)
		}
		if strings.Contains(in, "delete ") {
			deleted = append(deleted, in)
		}
	}
	if len(created) != 1 {
		t.Fatalf("%d update-ref transactions created branches, want 1: %v", len(created), created)
	}
	// An expected old value of empty is git's way of requiring the ref not
	// to exist. What that argument does to git is
	// TestRefsUpdateWithAnExpectedOldValue in the gitx package.
	//
	// start and commit are what make the batch all or nothing even when the
	// stream is cut short: without them git commits the prefix it read when
	// the input ended, so a writer that died mid-batch would leave some
	// branches written and the rest not.
	want := "start\n"
	for _, name := range []string{"alpha", "beta"} {
		want += "update refs/heads/managed/" + name + " " + h.accountGit("rev-parse", "refs/heads/managed/"+name) + " \"\"\n"
	}
	want += "commit\n"
	equal(t, "the update-ref transaction", created[0], want)

	// The staging refs the import held its commits on are taken away again
	// once the branches point at them, in one transaction of their own.
	if len(deleted) != 1 {
		t.Errorf("%d update-ref transactions deleted refs, want 1: %v", len(deleted), deleted)
	}
	if left := h.agentxRefs(); strings.Contains(left, "importing") {
		t.Errorf("the import left staging refs behind:\n%s", left)
	}
}

// stdinGit puts a git wrapper alone on the harness PATH that keeps what
// each invocation of one subcommand was fed on standard input and hands
// every invocation to the real git. A command whose work is batched carries
// it on stdin rather than in its arguments, which is what a test has to
// read to see one transaction rather than one call per skill.
func stdinGit(t *testing.T, h *harness, sub string) func() []string {
	t.Helper()
	requireGit(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// PATH is set inside the script because the harness PATH holds this
	// wrapper alone, so the shell would not find tee; the pipeline's status
	// is the real git's, which is the one the caller must see.
	stubGit(t, h, `#!/bin/sh
PATH=`+os.Getenv("PATH")+`
for arg in "$@"; do
	case "$arg" in
	`+sub+`)
		tee `+dir+`/$$.in | `+real+` "$@"
		exit $?
		;;
	esac
done
exec `+real+` "$@"
`)
	return func() []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		var fed []string
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			fed = append(fed, string(b))
		}
		return fed
	}
}
