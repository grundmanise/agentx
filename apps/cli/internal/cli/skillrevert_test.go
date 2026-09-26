package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// hiddenEntries are the entries of dir whose names start with .agentx-,
// which is what a mutation stages and retains beside a live path: after a
// mutation that finished, whether at once or by recovery, there are none.
func hiddenEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentx-") {
			names = append(names, e.Name())
		}
	}
	return names
}

func executable(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()&0o100 != 0
}

// TestSkillRevertRestoresTheBase edits a managed skill every way a tool can
// and reverts it: every file is back as the base holds it, the exec bit
// included, and a file or a directory the base does not hold is gone. The
// branch does not move, nothing is left beside the library, and a second
// revert has nothing to do.
func TestSkillRevertRestoresTheBase(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	base := libraryTree(t, lib)
	branch := h.accountGit("rev-parse", "refs/heads/managed/pdf")
	short := h.accountGit("log", "-1", "--format=%(trailers:key=Agentx-Upstream-Commit,valueonly)", branch)[:7]

	writeFile(t, filepath.Join(lib, "a.md"), "an edit\n")
	swapForLink(t, filepath.Join(lib, "b.md"), "a.md")
	remove(t, filepath.Join(lib, "c.md"))
	writeFile(t, filepath.Join(lib, "new.md"), "a file of my own\n")
	writeFile(t, mkdirs(t, filepath.Join(lib, "mine", "deeper"), "notes.md"), "a directory of my own\n")
	chmod(t, filepath.Join(lib, "bin", "run.sh"), 0o644)

	out := h.mustRun("--json", "skill", "revert", "pdf")
	sameTree(t, "the library directory", libraryTree(t, lib), base)
	if !executable(t, filepath.Join(lib, "bin", "run.sh")) {
		t.Error("bin/run.sh is not executable after the revert")
	}
	if info, err := os.Lstat(filepath.Join(lib, "b.md")); err != nil || !info.Mode().IsRegular() {
		t.Errorf("b.md is not a regular file after the revert: %v", err)
	}
	ev := h.one(out.stdout, "library_skill")
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "drift", drift(ev), "")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "reverted pdf to its base version at "+short)
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/pdf"), branch)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
	equal(t, "journals", journalCount(t, h), 0)

	again := h.mustRun("skill", "revert", "pdf")
	equal(t, "a second revert", again.stdout, "pdf already matches its base version at "+short+"; nothing was reverted\n")
	writeFile(t, filepath.Join(lib, "a.md"), "another edit\n")
	contains(t, "the text", h.mustRun("skill", "revert", "pdf").stdout, "✓ reverted pdf to its base version at "+short+"\n")
}

// TestSkillRevertRefreshesOnlyUnchangedCopies reverts a skill with four
// copy placements: one placed after the library was edited, which holds
// what agentx put there and is refreshed; one edited where it is, which is
// kept byte for byte, skipped and named; and two that already hold the
// base, which are left alone without a word.
func TestSkillRevertRefreshesOnlyUnchangedCopies(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--copy")
	lib := filepath.Join(h.library, "alpha")
	base := libraryTree(t, lib)
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	untouched := []string{
		filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha"),
		filepath.Join(h.home, ".copilot", "skills", "alpha"),
	}
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited in the library\n")
	h.mustRun("skill", "remove", "alpha", "--from", "claude-code")
	h.mustRun("skill", "place", "alpha", "--to", "claude-code", "--copy")
	sameTree(t, "claude's copy before the revert", libraryTree(t, claude), libraryTree(t, lib))
	before := make([]os.FileInfo, len(untouched))
	for i, p := range untouched {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		before[i] = info
	}

	out := h.mustRun("--json", "skill", "revert", "alpha")
	sameTree(t, "the library directory", libraryTree(t, lib), base)
	sameTree(t, "claude's copy", libraryTree(t, claude), base)
	sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
	for i, p := range untouched {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(info, before[i]) {
			t.Errorf("%s was replaced although it already held the base", p)
		}
		sameTree(t, p, libraryTree(t, p), base)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), editedCopyWarning(cursor))
	contains(t, "the result", h.one(out.stdout, "result")["summary"].(string), ", 1 copy placement refreshed, 1 placement skipped")
	for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
}

// TestSkillRevertSkipsACopyItCannotRead: a copy placement this machine
// cannot read whole can be judged neither unchanged nor edited, so it is
// left as it is, counted as skipped and named with the cause, and the
// revert of the library still lands.
func TestSkillRevertSkipsACopyItCannotRead(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--copy")
	lib := filepath.Join(h.library, "alpha")
	base := libraryTree(t, lib)
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited in the library\n")
	scripts := filepath.Join(cursor, "scripts")
	chmod(t, scripts, 0)
	t.Cleanup(func() { _ = os.Chmod(scripts, 0o755) }) // so the temporary home can be removed

	out := h.run("--json", "skill", "revert", "alpha")
	chmod(t, scripts, 0o755)
	if out.exit != 0 {
		t.Fatalf("revert: exit %d\n%s", out.exit, out.stderr)
	}
	sameTree(t, "the library directory", libraryTree(t, lib), base)
	sameTree(t, "cursor's copy", libraryTree(t, cursor), base)
	var skipped []string
	for _, w := range warnings(h, out.stderr) {
		if strings.HasPrefix(w, "cannot refresh ") {
			skipped = append(skipped, w)
		}
	}
	if len(skipped) != 1 || !strings.HasPrefix(skipped[0], "cannot refresh "+cursor+": ") || !strings.HasSuffix(skipped[0], "; the copy was left as it is") {
		t.Errorf("the warnings for a copy that cannot be refreshed = %q, want one naming %s", skipped, cursor)
	}
	contains(t, "the result", h.one(out.stdout, "result")["summary"].(string), ", 1 placement skipped")
	equal(t, "journals", journalCount(t, h), 0)
	for _, dir := range []string{h.library, filepath.Dir(cursor)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
}

// TestSkillRevertJudgesASharedCopyOnce reverts a skill whose copy two
// configurations share: Zencoder and Zenflow both read ~/.zencoder/skills,
// and copy_mode records a copy for each of them. The one copy is judged
// and planned once. Holding what the library held, it is refreshed once
// and counted for both configurations, as the placement counted it; edited
// where it is, it is kept byte for byte with one warning and one skip.
func TestSkillRevertJudgesASharedCopyOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		edited bool
	}{{"a copy of the edited library", false}, {"a copy edited where it is", true}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixture{dirs: []string{".claude", ".zencoder"}})
			s, _, _ := h.standardSource(true)
			h.mustRun("source", "add", s.url)
			h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
			lib := filepath.Join(h.library, "alpha")
			base := libraryTree(t, lib)
			editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited in the library\n")
			shared := filepath.Join(h.home, ".zencoder", "skills")
			place := filepath.Join(shared, "alpha")
			h.mustRun("skill", "place", "alpha", "--to", "zencoder", "--to", "zenflow", "--copy")
			if c.edited {
				editCopy(t, place)
			}
			before := libraryTree(t, place)

			out := h.run("--json", "skill", "revert", "alpha")
			if out.exit != 0 {
				t.Fatalf("revert: exit %d\n%s", out.exit, out.stderr)
			}
			sameTree(t, "the library directory", libraryTree(t, lib), base)
			summary := h.one(out.stdout, "result")["summary"].(string)
			warned := warnings(h, out.stderr)
			if c.edited {
				sameTree(t, "the shared copy", libraryTree(t, place), before)
				equal(t, "warnings", len(warned), 1)
				contains(t, "the warning", strings.Join(warned, "\n"), "zencoder's copy of alpha is different from the library")
				contains(t, "the result", summary, ", 1 placement skipped")
				if strings.Contains(summary, "refreshed") {
					t.Errorf("the result says a kept copy was refreshed: %s", summary)
				}
			} else {
				sameTree(t, "the shared copy", libraryTree(t, place), base)
				equal(t, "warnings", strings.Join(warned, "\n"), "")
				contains(t, "the result", summary, ", 2 copy placements refreshed")
				if strings.Contains(summary, "skipped") {
					t.Errorf("the result says a refreshed copy was skipped: %s", summary)
				}
			}
			for _, dir := range []string{h.library, shared} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "journals", journalCount(t, h), 0)
		})
	}
}

// TestSkillRevertSweepsStagingAKilledRevertLeft stands in for a revert
// killed after it staged the base beside the library directory and a
// refreshed copy beside a copy placement, and before its journal was
// written: nothing names either directory, so the next revert sweeps both
// before it stages anything, and leaves nothing beside the library or a
// copy.
func TestSkillRevertSweepsStagingAKilledRevertLeft(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--copy")
	editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited in the library\n")
	claude := filepath.Join(h.home, ".claude", "skills")
	cursor := filepath.Join(h.home, ".cursor", "skills")
	writeFile(t, mkdirs(t, filepath.Join(cursor, ".agentx-staged-deadbeef-2"), "SKILL.md"), skill("alpha", "a refreshed copy nothing names"))
	writeFile(t, mkdirs(t, filepath.Join(h.library, ".agentx-staged-deadbeef-1"), "SKILL.md"), skill("alpha", "a base nothing names"))

	out := h.run("skill", "revert", "alpha")
	if out.exit != 0 {
		t.Fatalf("revert: exit %d\n%s", out.exit, out.stderr)
	}
	for _, dir := range []string{h.library, claude, cursor} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillRevertRefusesWhatGitCannotRecord: a repository nested in the
// skill would be discarded with no record of it anywhere, so the revert
// refuses and changes nothing.
func TestSkillRevertRefusesWhatGitCannotRecord(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	nested := filepath.Join(lib, "vendored", ".git")
	writeFile(t, mkdirs(t, nested, "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(lib, "a.md"), "an edit\n")
	before := libraryTree(t, lib)
	out := h.run("--json", "skill", "revert", "pdf")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "pdf holds "+nested+", which git cannot record")
	sameTree(t, "the library directory", libraryTree(t, lib), before)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillRevertRefusesALibraryEntryThatIsASymlink: a library entry the
// user made a symlink to a directory of their own leads to the files they
// edit. A revert replaces the entry, so it would drop the link and leave
// every edit where the link led; it refuses instead and discards nothing,
// and the link still leads where it did.
func TestSkillRevertRefusesALibraryEntryThatIsASymlink(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	lib := filepath.Join(h.library, "pdf")
	dev := filepath.Join(h.home, "dev-pdf")
	if err := os.Rename(lib, dev); err != nil {
		t.Fatal(err)
	}
	link(t, dev, lib)
	writeFile(t, filepath.Join(dev, "a.md"), "edited\n")
	equal(t, "state", h.listed("pdf")["state"], stateModified)

	out := h.run("--json", "skill", "revert", "pdf")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], lib+" is a symlink to "+dev+
		"; a revert replaces the library directory and would drop the link without touching the files it leads to")
	equal(t, "hint", e["hint"], "replace the link with the directory it points to, then run 'agentx skill revert pdf' again,"+
		" or put the files back by hand: 'agentx skill diff pdf' shows what differs")
	target, err := os.Readlink(lib)
	if err != nil {
		t.Fatalf("the library entry is no longer a link: %v", err)
	}
	equal(t, "where the link leads", target, dev)
	equal(t, "a.md", fileBody(t, filepath.Join(dev, "a.md")), "edited\n")
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")

	// A link whose directory holds the base has nothing to revert.
	writeFile(t, filepath.Join(dev, "a.md"), "the same bytes\n")
	contains(t, "a revert of a current skill", h.mustRun("skill", "revert", "pdf").stdout, "nothing was reverted")
	if _, err := os.Readlink(lib); err != nil {
		t.Errorf("the library entry is no longer a link: %v", err)
	}
}

// TestSkillRevertGuardsAnEditMadeWhileItRuns: what a revert discards is
// what the directory held when the command began. A git wrapper makes an
// edit of the library while the revert reads the base version, after the
// content was captured and before the lock is taken; the revert then
// refuses, and the edit is there afterwards, not discarded with the rest.
func TestSkillRevertGuardsAnEditMadeWhileItRuns(t *testing.T) {
	t.Parallel()
	h, _ := driftHarness(t)
	file := filepath.Join(h.library, "pdf", "a.md")
	writeFile(t, file, "an edit\n")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) printf 'an edit made meanwhile\n' > `+shellWord(file)+` ;;
esac
exec `+real+` "$@"
`)
	out := h.run("--json", "skill", "revert", "pdf")
	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "pdf changed while it was being reverted, so nothing was discarded")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "a.md", string(b), "an edit made meanwhile\n")
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
}

// TestSkillRevertRefusesWhenTheImportBranchMoves: the base a revert lays
// out is the version the import branch named when the command began. A git
// wrapper moves the branch, or makes a fork of the name, while the revert
// reads the base version, after the lineage was read and before the lock
// is taken. The revert then refuses under the lock, before it writes a
// journal: the edit is still there, nothing is left beside the library and
// the ref the other writer wrote stays as it was written.
func TestSkillRevertRefusesWhenTheImportBranchMoves(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, ref string
		moved     bool // the managed branch moves; otherwise a fork of the name appears
	}{
		{"the managed branch moves", "refs/heads/managed/pdf", true},
		{"a fork appears", "refs/heads/skills/pdf", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _ := driftHarness(t)
			file := filepath.Join(h.library, "pdf", "a.md")
			writeFile(t, file, "an edit\n")
			before := h.accountGit("rev-parse", "refs/heads/managed/pdf")
			written := before
			if c.moved {
				written = h.accountGit("commit-tree", before+"^{tree}", "-p", before, "-m", "moved")
			}
			real, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			repo := gitx.AccountRepoPath(h.agentx)
			stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) `+real+` --git-dir=`+shellWord(repo)+` update-ref `+c.ref+` `+written+` || exit 1 ;;
esac
exec `+real+` "$@"
`)
			out := h.run("--json", "skill", "revert", "pdf")
			equal(t, "exit", out.exit, 6)
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], "the import branch refs/heads/managed/pdf moved while pdf was being reverted, so nothing was discarded")
			contains(t, "hint", e["hint"].(string), "agentx skill diff pdf")
			equal(t, "a.md", fileBody(t, file), "an edit\n")
			equal(t, "the ref written meanwhile", h.accountGit("rev-parse", c.ref), written)
			if !c.moved {
				equal(t, "the managed branch", h.accountGit("rev-parse", "refs/heads/managed/pdf"), before)
			}
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
		})
	}
}

// revertChildEnv marks the process TestSkillRevertRecoversAtEveryBoundary
// starts, which reverts the skill it names against the parent's temporary
// home and is killed in the middle of it.
const revertChildEnv = "AGENTX_TEST_REVERT_CHILD"

// TestRevertChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestRevertChildProcess(t *testing.T) {
	name := os.Getenv(revertChildEnv)
	if name == "" {
		t.Skip("not the revert child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"skill", "revert", name}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// killedRevert runs one revert in a child process that a git wrapper kills
// the moment its journal is on disk: the first git the revert runs after
// writing it is the read of the import branch its ref step holds it to,
// before any path changed. The wrapper leaves the harness PATH as it was
// when the child is gone, so that nothing else the test runs meets it.
func killedRevert(t *testing.T, h *harness, name string) string {
	t.Helper()
	return killedRevertBy(t, h, name, `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec %GIT% "$@"
`)
}

// killedRevertBy runs one revert in a child process under a git wrapper
// whose body is script, in which %GIT% is the real git and %MUTATIONS%
// the journal directory, and which kills the revert where it chooses.
func killedRevertBy(t *testing.T, h *harness, name, script string) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	path := h.env["PATH"]
	defer func() { h.env["PATH"] = path }()
	stubGit(t, h, "#!/bin/sh"+strings.NewReplacer("%MUTATIONS%", shellWord(filepath.Join(h.agentx, "mutations")), "%GIT%", real).Replace(script))
	child := exec.Command(os.Args[0], "-test.run=^TestRevertChildProcess$", "-test.v")
	child.Env = append(os.Environ(), revertChildEnv+"="+name)
	for k, v := range h.env {
		child.Env = append(child.Env, k+"="+v)
	}
	out, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("the revert was not killed:\n%s", out)
	}
	return string(out)
}

// journalStep is one step of a journal as the file records it.
type journalStep struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	New      string `json:"new"`
	Staged   string `json:"staged"`
	Retained string `json:"retained"`
}

// readJournal is the steps of the one journal waiting in agentx home.
func readJournal(t *testing.T, h *harness) []journalStep {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(h.agentx, "mutations", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("%d journals, want 1: %v", len(paths), err)
	}
	b, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var j struct {
		Steps []journalStep `json:"steps"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		t.Fatalf("the journal is not JSON: %v", err)
	}
	return j.Steps
}

// applySteps does what a process that went on would have done with the
// first n path steps of the journal, which is what one killed after the nth
// of them leaves: the live paths part way through, the journal as it was
// written. Its ref step moves nothing, so the path steps are all there is.
func applySteps(t *testing.T, steps []journalStep, n int) {
	t.Helper()
	for _, s := range steps {
		if s.Kind == "ref" {
			continue
		}
		if n == 0 {
			return
		}
		n--
		var err error
		switch {
		case s.Kind == "remove" && s.Retained != "":
			err = os.Rename(s.Path, s.Retained)
		case s.Kind == "remove":
			err = os.Remove(s.Path)
		case s.Kind == "publish":
			err = os.Rename(s.Staged, s.Path)
		case s.Kind == "link":
			err = os.Symlink(strings.TrimPrefix(s.New, "link:"), s.Path)
		}
		if err != nil {
			t.Fatalf("applying the %s step at %s: %v", s.Kind, s.Path, err)
		}
	}
}

// TestSkillRevertRecoversAtEveryBoundary kills a revert with SIGKILL once its
// journal is on disk, then leaves the machine as a process killed after
// each later step would: nothing applied, the library directory retained,
// the base published, the copy retained, the copy refreshed, and every live
// path done with the journal not yet told. The next command recovers each
// one, and the revert is then whole: the library and the copy hold the
// base, the import branch is where it was, and nothing staged or retained
// is left behind.
func TestSkillRevertRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	const pathSteps = 4 // the library's remove and publish, then the copy's
	for stop := 0; stop <= pathSteps; stop++ {
		t.Run(fmt.Sprintf("after %d steps", stop), func(t *testing.T) {
			t.Parallel()
			h, s := installHarness(t)
			h.mustRun("skill", "add", s.url, "--skill", "alpha")
			lib := filepath.Join(h.library, "alpha")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			base := libraryTree(t, lib)
			branch := h.accountGit("rev-parse", "refs/heads/managed/alpha")
			editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited\n")
			h.mustRun("skill", "remove", "alpha", "--from", "cursor")
			h.mustRun("skill", "place", "alpha", "--to", "cursor", "--copy")

			out := killedRevert(t, h, "alpha")
			steps := readJournal(t, h)
			var kinds []string
			for _, s := range steps {
				kinds = append(kinds, s.Kind)
			}
			equal(t, "the journal's steps", strings.Join(kinds, ", "), "ref, remove, publish, remove, publish")
			sameTree(t, "the library when the revert was killed", libraryTree(t, lib), map[string]string{
				"SKILL.md": base["SKILL.md"], "notes.md": "alpha notes, edited\n", "scripts/run.sh": base["scripts/run.sh"],
			})
			applySteps(t, steps, stop)

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed revert: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			sameTree(t, "the library directory", libraryTree(t, lib), base)
			sameTree(t, "cursor's copy", libraryTree(t, cursor), base)
			if !executable(t, filepath.Join(lib, "scripts", "run.sh")) {
				t.Error("scripts/run.sh is not executable after recovery")
			}
			equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), branch)
			for _, dir := range []string{h.library, filepath.Dir(cursor)} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "state", h.listed("alpha")["state"], stateCurrent)
		})
	}
}
