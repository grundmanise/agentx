package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillDiffOfAFork compares a greenfield skill, with a .gitignore at
// its worktree's root that a commit made with git put there, with its last
// commit and with an earlier one. An edit, a new file, a file the root
// .gitignore names and a file the system-file list names make two diffs,
// with every path relative to the skill's directory and never the root's
// own entries; once the edits are committed the fork matches its last
// commit and still differs from the first. A commit the account repo does
// not hold, one that holds no skill directory of that name, and --update
// with --commit are refused.
func TestSkillDiffOfAFork(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.mustRun("skill", "new", "notes")
	first := h.ref(lineage.ForkRef("notes"))
	root := filepath.Join(h.agentx, "worktrees", "notes")
	writeFile(t, filepath.Join(root, ".gitignore"), "*.log\n")
	gitIn(t, h, root, "add", "-A")
	gitIn(t, h, root, "commit", "-q", "-m", "Ignore logs")
	tip := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(h.library, "notes", "SKILL.md"), skill("notes", "Edited"))
	writeFile(t, filepath.Join(h.library, "notes", "extra.md"), "extra\n")
	writeFile(t, filepath.Join(h.library, "notes", "debug.log"), "ignored by the root .gitignore\n")
	writeFile(t, filepath.Join(h.library, "notes", ".DS_Store"), "finder\n")

	files := func(out outcome) string {
		t.Helper()
		var got []string
		for _, d := range h.eventsOfType(out.stdout, "diff") {
			got = append(got, d["path"].(string)+" "+d["status"].(string))
		}
		return strings.Join(got, ",")
	}
	out := h.mustRun("--json", "skill", "diff", "notes")
	equal(t, "the diffs against the tip", files(out), "SKILL.md modified,extra.md added")
	diffs := h.eventsOfType(out.stdout, "diff")
	contains(t, "SKILL.md's patch", diffs[0]["patch"].(string), "diff --git a/SKILL.md b/SKILL.md\n")
	equal(t, "the result against the tip", h.one(out.stdout, "result")["summary"], "notes differs from its last commit "+short(tip)+" in 2 files")

	h.mustRun("skill", "commit", "notes")
	committed := h.ref(lineage.ForkRef("notes"))
	equal(t, "the text once committed", h.mustRun("skill", "diff", "notes").stdout, "notes matches its last commit "+short(committed)+"\n")
	out = h.mustRun("--json", "skill", "diff", "notes", "--commit", short(first))
	equal(t, "the diffs against the first commit", files(out), "SKILL.md modified,extra.md added")
	equal(t, "the result against the first commit", h.one(out.stdout, "result")["summary"], "notes differs from commit "+short(first)+" in 2 files")

	for _, c := range []struct {
		args    []string
		message string
		exit    int
	}{
		{[]string{"--commit", "deadbeef"}, "the account repo holds no commit deadbeef", 6},
		{[]string{"--commit", h.ref(lineage.ManagedRef("alpha"))}, "holds no skill directory notes to compare notes with", 6},
		{[]string{"--commit", first, "--update"}, "--update and --commit cannot be given together", 1},
	} {
		out := h.run(append([]string{"--json", "skill", "diff", "notes"}, c.args...)...)
		equal(t, strings.Join(c.args, " ")+": exit", out.exit, c.exit)
		contains(t, strings.Join(c.args, " ")+": message", h.one(out.stdout, "error")["message"].(string), c.message)
	}
}

// TestSkillRevertOfAFork discards a greenfield skill's edits, one of them
// staged with git in its worktree, beside a file the system-file list
// names and a copy placement holding the edits: the skill directory is the
// tip's again and the copy with it, the branch stays where it was, git
// status in the worktree is clean, the ignored file is kept and nothing
// staged or retained is left. A second revert has nothing to do. A fork
// holding a nested repository no ignore rule covers is refused, since the
// revert would discard it.
func TestSkillRevertOfAFork(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	h.mustRun("skill", "new", "notes")
	tip := h.ref(lineage.ForkRef("notes"))
	lib := filepath.Join(h.library, "notes")
	root := filepath.Join(h.agentx, "worktrees", "notes")
	cursor := filepath.Join(h.home, ".cursor", "skills", "notes")
	skillDir := filepath.Join(root, "notes")
	want := libraryTree(t, skillDir)
	writeFile(t, filepath.Join(lib, "SKILL.md"), skill("notes", "Edited"))
	writeFile(t, filepath.Join(lib, "extra.md"), "extra\n")
	gitIn(t, h, root, "add", "notes/extra.md")
	h.mustRun("skill", "remove", "notes", "--from", "cursor")
	h.mustRun("skill", "place", "notes", "--to", "cursor", "--copy")
	writeFile(t, filepath.Join(lib, ".DS_Store"), "finder\n")

	out := h.mustRun("--json", "skill", "revert", "notes")
	equal(t, "the result", h.one(out.stdout, "result")["summary"], "reverted notes to its last commit "+short(tip)+", 1 copy placement refreshed")
	equal(t, "the state", h.librarySkill(out.stdout, "notes")["state"], stateCurrent)
	sameTree(t, "the skill directory", libraryTree(t, skillDir), withFile(want, ".DS_Store", "finder\n"))
	sameTree(t, "cursor's copy", libraryTree(t, cursor), want)
	equal(t, "the branch", h.ref(lineage.ForkRef("notes")), tip)
	equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
	for _, dir := range []string{filepath.Join(h.agentx, "worktrees"), root, filepath.Dir(cursor)} {
		equal(t, "what is left in "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "a second revert", h.mustRun("skill", "revert", "notes").stdout, "notes has no uncommitted edits; nothing was reverted\n")

	writeFile(t, mkdirs(t, filepath.Join(lib, "vendor", ".git"), "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(lib, "vendor", "lib.js"), "x\n")
	refused := h.run("--json", "skill", "revert", "notes")
	equal(t, "a nested repository: exit", refused.exit, exitRefused.exit)
	contains(t, "a nested repository: message", h.one(refused.stdout, "error")["message"].(string), "which git cannot record")
	if _, err := os.Stat(filepath.Join(lib, "vendor", "lib.js")); err != nil {
		t.Errorf("the refused revert discarded the nested repository: %v", err)
	}
	// Once an ignore rule covers it, the nested repository is a file git
	// ignores like any other, and stays where it is, whole.
	writeFile(t, filepath.Join(root, ".gitignore"), "vendor/\n")
	gitIn(t, h, root, "add", ".gitignore")
	gitIn(t, h, root, "commit", "-q", "-m", "Keep vendor local")
	writeFile(t, filepath.Join(lib, "SKILL.md"), skill("notes", "Edited again"))
	h.mustRun("skill", "revert", "notes")
	sameTree(t, "the skill directory beside an ignored repository", libraryTree(t, skillDir), withFile(withFile(withFile(want,
		".DS_Store", "finder\n"), "vendor/lib.js", "x\n"), "vendor/.git/HEAD", "ref: refs/heads/main\n"))
}

// TestSkillRevertToACommit restores a fork from an earlier commit of its
// history. In one home, refused first and changing nothing: uncommitted
// edits, exit code 6; a commit outside the fork's history; and a merge
// pending, exit code 4, for a revert to a commit and for one of the edits,
// which is refused with no edits to discard too.
// Then the revert writes a new commit on top of the tip, with the earlier
// commit's skill directory and the machine, and keeps the history it was
// written on. That commit tracked a file at a path the system-file list
// ignores, which the tip no longer did: the restored file replaces the
// local one, as git checkout replaces it. A second revert to the same
// commit has nothing to restore.
func TestSkillRevertToACommit(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	h.mustRun("skill", "new", "notes")
	lib := filepath.Join(h.library, "notes")
	root := filepath.Join(h.agentx, "worktrees", "notes")
	writeFile(t, filepath.Join(lib, "SKILL.md"), skill("notes", "As it was"))
	writeFile(t, filepath.Join(lib, ".DS_Store"), "tracked\n")
	gitIn(t, h, root, "add", "-A")
	gitIn(t, h, root, "add", "-f", "notes/.DS_Store")
	gitIn(t, h, root, "commit", "-q", "-m", "As it was")
	earlier := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(lib, "SKILL.md"), skill("notes", "As it is"))
	gitIn(t, h, root, "rm", "-q", "--cached", "notes/.DS_Store")
	gitIn(t, h, root, "commit", "-q", "-a", "-m", "As it is")
	tip := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(lib, ".DS_Store"), "local\n")
	equal(t, "the state before", h.listed("notes")["state"], stateCurrent)

	stranger := h.accountGit("commit-tree", earlier+"^{tree}", "-m", "Not in the history")
	writeFile(t, filepath.Join(lib, "extra.md"), "an edit\n")
	pending := filepath.Join(h.agentx, "merges", "notes")
	for _, c := range []struct {
		name, message string
		args          []string
		exit          int
	}{
		{"uncommitted edits", "notes has uncommitted edits, so it cannot be reverted to an earlier commit until they are committed or reverted", []string{"--to", earlier}, 6},
		{"not in the history", short(stranger) + " is not in the history of notes", []string{"--to", stranger}, 6},
		{"a merge pending", "notes has a merge pending, so it cannot be reverted to an earlier commit until", []string{"--to", earlier}, 4},
		{"a merge pending, the edits", "notes has a merge pending, so it cannot be reverted until", nil, 4},
	} {
		if c.exit == 4 && !lexists(pending) {
			h.accountGit("worktree", "add", "--quiet", "--detach", "--lock", "--reason", pendingReason, pending, tip)
			remove(t, filepath.Join(lib, "extra.md"))
		}
		out := h.run(append([]string{"--json", "skill", "revert", "notes"}, c.args...)...)
		equal(t, c.name+": exit", out.exit, c.exit)
		contains(t, c.name+": message", h.one(out.stdout, "error")["message"].(string), c.message)
		equal(t, c.name+": the branch", h.ref(lineage.ForkRef("notes")), tip)
	}
	h.accountGit("worktree", "remove", "-f", "-f", pending)

	machine, _, err := home.MachineID(h.agentx, h.env, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := h.mustRun("skill", "revert", "notes", "--to", short(earlier))
	reverted := h.ref(lineage.ForkRef("notes"))
	subject := "notes: revert to " + short(earlier) + " (test-host)"
	equal(t, "the text output", out.stdout, "✓ reverted notes to "+short(earlier)+" as "+short(reverted)+": "+subject+"\n")
	equal(t, "the new commit and its parent", h.accountGit("rev-list", "--parents", "-n", "1", reverted), reverted+" "+tip)
	equal(t, "the new commit's message", h.accountGit("log", "-1", "--format=%B", reverted), subject+"\n\nAgentx-Machine: "+machine)
	equal(t, "the restored tree", h.accountGit("rev-parse", reverted+":notes"), h.accountGit("rev-parse", earlier+":notes"))
	equal(t, "the history it kept", h.accountGit("rev-list", "--count", reverted), "4")
	if b, err := os.ReadFile(filepath.Join(lib, ".DS_Store")); err != nil || string(b) != "tracked\n" {
		t.Errorf("the restored file at an ignored path holds %q, %v; want the commit's", b, err)
	}
	equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
	equal(t, "the state after", h.listed("notes")["state"], stateCurrent)
	for _, dir := range []string{filepath.Join(h.agentx, "worktrees"), root} {
		equal(t, "what is left in "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "a second revert to it", h.mustRun("skill", "revert", "notes", "--to", earlier).stdout,
		"notes already holds what "+short(earlier)+" holds; nothing was reverted\n")
	equal(t, "the branch after it", h.ref(lineage.ForkRef("notes")), reverted)
}

// TestSkillRevertOfAForkRunInItsFolder: a revert run in the fork's skill
// directory, reached through its library entry or in the worktree, exits 0
// and leaves the process in the directory that now holds the reverted
// content. The revert replaces the directory, and a git started in the one
// it displaced, which is removed once the revert is complete, would fail.
func TestSkillRevertOfAForkRunInItsFolder(t *testing.T) {
	// Not parallel: it changes the process's working directory.
	h, _ := installHarness(t)
	h.mustRun("skill", "new", "notes")
	first := h.ref(lineage.ForkRef("notes"))
	lib := filepath.Join(h.library, "notes")
	root := filepath.Join(h.agentx, "worktrees", "notes")
	written := readText(t, filepath.Join(lib, "SKILL.md"))
	writeFile(t, filepath.Join(lib, "SKILL.md"), skill("notes", "Committed"))
	gitIn(t, h, root, "commit", "-q", "-a", "-m", "Committed")
	writeFile(t, filepath.Join(lib, "SKILL.md"), skill("notes", "An edit"))
	for _, c := range []struct {
		name, wd, want string
		args           []string
	}{
		{"the edits, in the library entry", lib, skill("notes", "Committed"), nil},
		{"an earlier commit, in the worktree", filepath.Join(root, "notes"), written, []string{"--to", first}},
	} {
		t.Chdir(c.wd)
		out := h.run(append([]string{"skill", "revert", "notes"}, c.args...)...)
		equal(t, c.name+": exit", out.exit, 0)
		equal(t, c.name+": SKILL.md", readText(t, filepath.Join(lib, "SKILL.md")), c.want)
		if wd, err := os.Getwd(); err != nil || wd != c.wd {
			t.Errorf("%s: the working directory is %q, %v; want %s", c.name, wd, err, c.wd)
		}
	}
}

// forkRevertChildEnv marks the process TestSkillRevertOfAForkRecoversWhereItWasKilled
// starts, which runs the skill revert whose arguments it holds, separated
// by spaces, and is killed part way.
const forkRevertChildEnv = "AGENTX_TEST_FORK_REVERT_CHILD"

// TestForkRevertChildProcess is not a test: it is the body of that
// process. It does nothing when the variable that marks it is not set.
func TestForkRevertChildProcess(t *testing.T) {
	args := os.Getenv(forkRevertChildEnv)
	if args == "" {
		t.Skip("not the fork revert child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), append([]string{"skill", "revert"}, strings.Fields(args)...), env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestSkillRevertOfAForkRecoversWhereItWasKilled kills each kind of fork
// revert with SIGKILL at one boundary: a revert of the edits once its
// journal is on disk, before anything was applied, and a revert to an
// earlier commit right after its branch moved. Both journal the same
// steps, and every boundary between them is the journal's, which
// home.TestForkRevertRecoversFromEveryBoundary replays without git. The
// next command finishes the revert: the skill directory as the branch
// holds it, the ignored file kept, git status in the worktree clean, and
// nothing staged or retained left.
func TestSkillRevertOfAForkRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script string
		to           bool
	}{
		{"a revert of the edits once the journal is on disk", `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec %GIT% "$@"
`, false},
		{"a revert to a commit after the branch moved", `
case " $* " in
*" update-ref --stdin "*)
	%GIT% "$@"
	kill -9 $PPID
	exit 1 ;;
esac
exec %GIT% "$@"
`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _ := installHarness(t)
			h.mustRun("skill", "new", "notes")
			lib := filepath.Join(h.library, "notes")
			root := filepath.Join(h.agentx, "worktrees", "notes")
			first := h.ref(lineage.ForkRef("notes"))
			want := libraryTree(t, filepath.Join(root, "notes"))
			args := "notes"
			if tc.to {
				writeFile(t, filepath.Join(lib, "SKILL.md"), skill("notes", "Committed"))
				h.mustRun("skill", "commit", "notes")
				args = "notes --to " + first
			} else {
				writeFile(t, filepath.Join(lib, "SKILL.md"), skill("notes", "Edited"))
			}
			writeFile(t, filepath.Join(lib, ".DS_Store"), "finder\n")
			before := h.ref(lineage.ForkRef("notes"))
			out := killedChild(t, h, "TestForkRevertChildProcess", forkRevertChildEnv, args, tc.script)
			_, kinds := journalKinds(t, h)
			equal(t, "the journal's steps", kinds, "ref, remove, publish, worktree")
			if got := h.run("skill", "list"); got.exit != 0 {
				t.Fatalf("the command after the killed revert: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			sameTree(t, "the skill directory", libraryTree(t, filepath.Join(root, "notes")), withFile(want, ".DS_Store", "finder\n"))
			if tc.to {
				equal(t, "the new commit's parent", h.accountGit("rev-parse", lineage.ForkRef("notes")+"^"), before)
			} else {
				equal(t, "the branch", h.ref(lineage.ForkRef("notes")), before)
			}
			equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
			equal(t, "what is left in the worktrees directory", strings.Join(hiddenEntries(t, filepath.Join(h.agentx, "worktrees")), " "), "")
		})
	}
}

// TestSplitUnrecordable is what a revert of a fork carries over of what
// git cannot record in its skill directory, and what it refuses to lose.
func TestSplitUnrecordable(t *testing.T) {
	t.Parallel()
	j := forkJudged{
		unrecordable: []string{"cache/.git", "pipe", "vendor/.git", "vendor/sub/.GIT"},
		exposed:      []string{"vendor/.git"},
	}
	kept, lost := splitUnrecordable(j)
	equal(t, "kept", strings.Join(kept, " "), "cache/.git vendor/sub/.GIT")
	equal(t, "lost", strings.Join(lost, " "), "pipe vendor/.git")
}
