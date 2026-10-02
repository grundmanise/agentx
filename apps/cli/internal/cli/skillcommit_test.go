package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// withIdentity gives the user of h a global git identity, as git config
// --global user.name and user.email would.
func (h *harness) withIdentity(name, email string) {
	h.t.Helper()
	f, err := os.OpenFile(filepath.Join(h.home, ".gitconfig"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("[user]\n\tname = " + name + "\n\temail = " + email + "\n"); err != nil {
		h.t.Fatal(err)
	}
}

// invocation is an invocation of agentx for h's user, for a test that calls
// what a command runs directly, with its output discarded.
func (h *harness) invocation() *invocation {
	var sink strings.Builder
	return &invocation{env: h.env, dirs: home.Dirs{Home: h.agentx, Library: h.library, Config: h.config},
		git: gitx.New(h.env, false, func(string, ...any) {}), out: &writer{stdout: &sink, stderr: &sink, env: h.env}}
}

// TestSkillCommitCommitsEachEditedFork edits two of three greenfield skills,
// one of them beside a file the system-file list names, and commits every
// fork with edits: one commit each, on the tip it found, by the user's own
// identity, carrying the machine and a subject naming the skill and the
// files; the ignored file stays on disk and out of the commit, the
// worktree's status is clean afterwards, and the fork with no edit is left
// as it is. Then -m gives the message of a second commit, which is a new
// commit and not an amended one, and a commit made with git directly in
// the worktree is kept as it is, with nothing left to commit after it,
// even once it puts files beside the skill directory.
func TestSkillCommitCommitsEachEditedFork(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	h.withIdentity("Ada Lovelace", "ada@example.com")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		h.mustRun("skill", "new", name)
	}
	machine, _, err := home.MachineID(h.agentx, h.env, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		before[name] = h.ref(lineage.ForkRef(name))
	}
	// Edits made through the library, as an agent client makes them.
	writeFile(t, filepath.Join(h.library, "alpha", "SKILL.md"), skill("alpha", "Edited"))
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "notes\n")
	writeFile(t, filepath.Join(h.library, "beta", "SKILL.md"), skill("beta", "Edited"))
	writeFile(t, filepath.Join(h.library, "beta", ".DS_Store"), "finder\n")
	equal(t, "alpha's state before", h.listed("alpha")["state"], stateModified)

	out := h.mustRun("--json", "skill", "commit")
	commits := h.eventsOfType(out.stdout, "commit")
	if len(commits) != 2 {
		t.Fatalf("%d commit events, want 2:\n%s", len(commits), out.stdout)
	}
	equal(t, "the result", h.one(out.stdout, "result")["summary"], "committed 2 forks")
	for i, want := range []struct{ name, subject, files string }{
		{"alpha", "alpha: edit SKILL.md and add notes.md (test-host)", "SKILL.md modified,notes.md added"},
		{"beta", "beta: edit SKILL.md (test-host)", "SKILL.md modified"},
	} {
		ev, tip := commits[i], h.ref(lineage.ForkRef(want.name))
		equal(t, want.name+": the event's name", ev["name"], want.name)
		equal(t, want.name+": the event's commit", ev["commit"], tip)
		equal(t, want.name+": the event's subject", ev["subject"], want.subject)
		var files []string
		for _, f := range ev["files"].([]any) {
			files = append(files, f.(map[string]any)["path"].(string)+" "+f.(map[string]any)["status"].(string))
		}
		equal(t, want.name+": the event's files", strings.Join(files, ","), want.files)
		equal(t, want.name+": the commit and its parent", h.accountGit("rev-list", "--parents", "-n", "1", tip), tip+" "+before[want.name])
		equal(t, want.name+": the author and committer", h.accountGit("log", "-1", "--format=%an <%ae>|%cn <%ce>", tip),
			"Ada Lovelace <ada@example.com>|Ada Lovelace <ada@example.com>")
		equal(t, want.name+": the message", h.accountGit("log", "-1", "--format=%B", tip), want.subject+"\n\nAgentx-Machine: "+machine)
		equal(t, want.name+": git status in the worktree", gitIn(t, h, filepath.Join(h.agentx, "worktrees", want.name), "status", "--porcelain"), "")
		equal(t, want.name+": the state", h.librarySkill(out.stdout, want.name)["state"], stateCurrent)
	}
	equal(t, "beta's tree", h.accountGit("ls-tree", "-r", "--name-only", lineage.ForkRef("beta")), "beta/SKILL.md")
	if _, err := os.Stat(filepath.Join(h.library, "beta", ".DS_Store")); err != nil {
		t.Errorf("the ignored file is gone: %v", err)
	}
	equal(t, "gamma's branch", h.ref(lineage.ForkRef("gamma")), before["gamma"])

	// -m gives the message: its first line is the subject.
	first := h.ref(lineage.ForkRef("alpha"))
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "more notes\n")
	text := h.mustRun("skill", "commit", "alpha", "-m", "Tighten the notes\n\nThey were too long.")
	second := h.ref(lineage.ForkRef("alpha"))
	equal(t, "the text output", text.stdout, "✓ committed alpha as "+short(second)+": Tighten the notes\n")
	equal(t, "the given message", h.accountGit("log", "-1", "--format=%B", second),
		"Tighten the notes\n\nThey were too long.\n\nAgentx-Machine: "+machine)
	equal(t, "the history", h.accountGit("rev-list", "--parents", lineage.ForkRef("alpha")),
		second+" "+first+"\n"+first+" "+before["alpha"]+"\n"+before["alpha"])

	// A commit made with git in the worktree is the user's, left as it is.
	root := filepath.Join(h.agentx, "worktrees", "alpha")
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "by hand\n")
	gitIn(t, h, root, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit", "-q", "-a", "-m", "By hand")
	mine := h.ref(lineage.ForkRef("alpha"))
	equal(t, "the commit made with git", h.trailer(mine, lineage.TrailerMachine), "")
	equal(t, "the state after it", h.listed("alpha")["state"], stateCurrent)
	again := h.mustRun("--json", "skill", "commit")
	equal(t, "commit events after it", len(h.eventsOfType(again.stdout, "commit")), 0)
	equal(t, "the result after it", h.one(again.stdout, "result")["summary"], "nothing to commit")
	equal(t, "alpha's branch after it", h.ref(lineage.ForkRef("alpha")), mine)
	equal(t, "a named fork with nothing to commit", h.mustRun("skill", "commit", "beta").stdout, "✓ beta has nothing to commit\n")

	// Entries a commit made with git put beside the skill directory are the
	// branch's too: the fork still holds its tip, a .gitignore among them
	// counts as it counts for git status, and a commit keeps them.
	writeFile(t, filepath.Join(root, "README.md"), "readme\n")
	writeFile(t, filepath.Join(root, ".gitignore"), "*.log\n")
	gitIn(t, h, root, "add", "-A")
	gitIn(t, h, root, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit", "-q", "-m", "Beside the skill")
	writeFile(t, filepath.Join(h.library, "alpha", "debug.log"), "ignored by the worktree's .gitignore\n")
	equal(t, "the state beside root entries", h.listed("alpha")["state"], stateCurrent)
	equal(t, "a commit beside root entries", h.mustRun("skill", "commit", "alpha").stdout, "✓ alpha has nothing to commit\n")
	writeFile(t, filepath.Join(h.library, "alpha", "SKILL.md"), skill("alpha", "Edited beside the README"))
	h.mustRun("skill", "commit", "alpha")
	equal(t, "the tree beside root entries", h.accountGit("ls-tree", "-r", "--name-only", lineage.ForkRef("alpha")),
		".gitignore\nREADME.md\nalpha/SKILL.md\nalpha/notes.md")
	equal(t, "the state after that commit", h.listed("alpha")["state"], stateCurrent)
	equal(t, "git status after that commit", gitIn(t, h, root, "status", "--porcelain"), "")
}

// TestSkillCommitRefusals refuses, in one home and committing nothing, a
// managed skill, an unmanaged one, a name the library does not hold, an
// empty -m, a fork holding a nested repository git would record as a link,
// the same fork while a merge the user ran with git waits for them and
// while a git holds its index, and a fork whose worktree is gone, then
// every fork at once, which names both forks. Once the skill's .gitignore
// covers the nested repository, the fork commits without it.
func TestSkillCommitRefusals(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "mine"), "SKILL.md"), skill("mine", "mine"))
	h.mustRun("skill", "new", "notes")
	h.mustRun("skill", "new", "gone")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "notes", "vendor", ".git"), "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(h.library, "notes", "vendor", "lib.js"), "x\n")
	remove(t, filepath.Join(h.agentx, "worktrees", "gone"))
	refs := h.accountGit("for-each-ref")
	gone := filepath.Join(h.agentx, "worktrees", "gone")
	notes := filepath.Join(h.agentx, "worktrees", "notes")
	admin := filepath.Join(gitx.AccountRepoPath(h.agentx), "worktrees", "notes")
	for _, tc := range []struct {
		args []string
		held string // a file of notes' worktree admin directory the case alone holds
		exit status
		says string
	}{
		{[]string{"alpha"}, "", exitRefused, "alpha is managed, not a fork, so it has no history to commit to"},
		{[]string{"mine"}, "", exitRefused, "mine is not a fork, so it has no history to commit to"},
		{[]string{"absent"}, "", exitNotFound, `the library holds no skill called "absent"`},
		{[]string{"notes", "-m", " "}, "", exitUsage, "-m needs a message"},
		{[]string{"notes"}, "", exitRefused, "notes holds a Git repository at vendor/.git, which git would record as a link rather than its files"},
		{[]string{"notes"}, "MERGE_HEAD", exitRefused, "notes's worktree " + notes + " is in the middle of a git merge"},
		{[]string{"notes"}, "index.lock", exitRefused, "git is running in notes's worktree " + notes},
		{[]string{"gone"}, "", exitRefused, "gone's worktree " + gone + " is missing"},
		{nil, "", exitRefused, "2 of 2 forks could not be committed: gone: gone's worktree"},
	} {
		if tc.held != "" {
			writeFile(t, filepath.Join(admin, tc.held), "")
		}
		out := h.run(append([]string{"--json", "skill", "commit"}, tc.args...)...)
		what := strings.Join(tc.args, " ") + " " + tc.held
		equal(t, what+": exit", out.exit, tc.exit.exit)
		contains(t, what+": error", h.one(out.stdout, "error")["message"].(string), tc.says)
		if tc.held != "" {
			remove(t, filepath.Join(admin, tc.held))
		}
	}
	equal(t, "the refs", h.accountGit("for-each-ref"), refs)

	writeFile(t, filepath.Join(h.library, "notes", ".gitignore"), "vendor/\n")
	h.mustRun("skill", "commit", "notes")
	equal(t, "the tree without the ignored repository", h.accountGit("ls-tree", "-r", "--name-only", lineage.ForkRef("notes")),
		"notes/.gitignore\nnotes/SKILL.md")
}

// TestSkillCommitRefusesAForkThatChangedMeanwhile changes a fork while its
// commit is being made, each way the command finds out: its skill
// directory is edited once the commit is written, which the read under the
// lock finds; and its branch is moved by a commit made with git once the
// journal is on disk, which the journal's own move finds. Neither commits
// anything or leaves a journal behind, and a moved branch stays where the
// commit made with git put it.
func TestSkillCommitRefusesAForkThatChangedMeanwhile(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, script, says string
	}{
		{"edited", `
case " $* " in
*" commit-tree "*)
	` + real + ` "$@"
	status=$?
	echo edited meanwhile >> %SKILL%/SKILL.md
	exit $status ;;
esac
exec ` + real + ` "$@"
`, "notes changed while it was being committed, so it was not committed"},
		{"moved", `
case " $* " in
*" for-each-ref --format="*)
	for f in %MUTATIONS%/*.json; do
		if [ -e "$f" ] && [ ! -e %MARK% ]; then
			: > %MARK%
			` + real + ` --git-dir=%ACCOUNT% update-ref refs/heads/skills/notes %MOVED%
		fi
	done ;;
esac
exec ` + real + ` "$@"
`, "notes changed while it was being committed, so nothing was committed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _ := installHarness(t)
			h.mustRun("skill", "new", "notes")
			tip := h.ref(lineage.ForkRef("notes"))
			moved := h.accountGit("commit-tree", tip+"^{tree}", "-p", tip, "-m", "made with git")
			writeFile(t, filepath.Join(h.library, "notes", "SKILL.md"), skill("notes", "Edited"))
			path := h.env["PATH"]
			stubGit(t, h, "#!/bin/sh"+strings.NewReplacer(
				"%SKILL%", shellWord(filepath.Join(h.agentx, "worktrees", "notes", "notes")),
				"%MUTATIONS%", shellWord(filepath.Join(h.agentx, "mutations")),
				"%MARK%", shellWord(filepath.Join(t.TempDir(), "moved")),
				"%ACCOUNT%", shellWord(gitx.AccountRepoPath(h.agentx)),
				"%MOVED%", moved,
			).Replace(tc.script))
			out := h.run("--json", "skill", "commit", "notes")
			h.env["PATH"] = path
			equal(t, "exit", out.exit, exitRefused.exit)
			equal(t, "error", h.one(out.stdout, "error")["message"], tc.says)
			equal(t, "journals", journalCount(t, h), 0)
			want := tip
			if tc.name == "moved" {
				want = moved
			}
			equal(t, "the branch", h.ref(lineage.ForkRef("notes")), want)
		})
	}
}

// commitChildEnv marks the process TestSkillCommitRecoversWhereItWasKilled
// starts, which commits the fork it names and is killed part way.
const commitChildEnv = "AGENTX_TEST_COMMIT_CHILD"

// TestCommitChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestCommitChildProcess(t *testing.T) {
	name := os.Getenv(commitChildEnv)
	if name == "" {
		t.Skip("not the skill commit child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"skill", "commit", name}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestSkillCommitRecoversWhereItWasKilled kills a commit with SIGKILL right
// after its branch moved and before the worktree's index followed it, the
// one boundary between the two steps it journals. The next command
// finishes it: the branch on the commit, the index on the branch, so git
// status in the worktree is clean, and no journal left.
func TestSkillCommitRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	h.mustRun("skill", "new", "notes")
	tip := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(h.library, "notes", "SKILL.md"), skill("notes", "Edited"))
	out := killedChild(t, h, "TestCommitChildProcess", commitChildEnv, "notes", `
case " $* " in
*" update-ref --stdin "*)
	%GIT% "$@"
	kill -9 $PPID
	exit 1 ;;
esac
exec %GIT% "$@"
`)
	_, kinds := journalKinds(t, h)
	equal(t, "the journal's steps", kinds, "ref, worktree")
	if got := h.run("skill", "list"); got.exit != 0 {
		t.Fatalf("the command after the killed commit: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
	}
	equal(t, "journals after recovery", journalCount(t, h), 0)
	committed := h.ref(lineage.ForkRef("notes"))
	equal(t, "the commit's parent", h.accountGit("rev-parse", committed+"^"), tip)
	equal(t, "git status in the worktree", gitIn(t, h, filepath.Join(h.agentx, "worktrees", "notes"), "status", "--porcelain"), "")
	equal(t, "the state", h.listed("notes")["state"], stateCurrent)
}

// TestForkGuardsRefuseUncommittedEdits is the refusal every command that
// moves a fork's branch runs under the lock: none for a fork whose skill
// directory holds its tip, or holds beside it only a file git ignores;
// exit code 6 naming the fork once it holds an edit, whether found before
// the lock or only by the fingerprint read under it; and exit code 4 for a
// merge pending, which is asked about first. A command that does not move
// the branch is not refused for the edits.
func TestForkGuardsRefuseUncommittedEdits(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	h.mustRun("skill", "new", "notes")
	ctx := context.Background()
	inv := h.invocation()
	gitDir := gitx.AccountRepoPath(h.agentx)
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := inv.forkSiteOf(ctx, gitDir, records["notes"])
	if err != nil {
		t.Fatal(err)
	}
	guard := func(needsClean bool) error {
		t.Helper()
		pre, err := inv.judgeSite(ctx, f, false)
		if err != nil {
			t.Fatal(err)
		}
		_, err = inv.forkGuards(ctx, f, pre, "merged", needsClean)
		return err
	}
	refused := func(what string, err error, st status, message string) {
		t.Helper()
		var got *failure
		if !errors.As(err, &got) {
			t.Fatalf("%s: %v, want a refusal", what, err)
		}
		equal(t, what+": exit", got.status, st)
		equal(t, what+": message", got.message, message)
	}
	writeFile(t, filepath.Join(f.skillDir, ".DS_Store"), "finder\n")
	if err := guard(true); err != nil {
		t.Errorf("a fork holding only an ignored file: %v", err)
	}
	writeFile(t, filepath.Join(f.skillDir, "notes.md"), "notes\n")
	refused("an edit", guard(true), exitRefused, "notes has uncommitted edits, so it cannot be merged until they are committed or reverted")
	if err := guard(false); err != nil {
		t.Errorf("a command that does not move the branch: %v", err)
	}
	// An edit made after the judgement, found by the fingerprint alone.
	remove(t, filepath.Join(f.skillDir, "notes.md"))
	pre, err := inv.judgeSite(ctx, f, true)
	if err != nil || !pre.clean {
		t.Fatalf("the clean fork judged %+v, %v", pre, err)
	}
	equal(t, "the ignored files before the lock", strings.Join(pre.ignored, " "), ".DS_Store")
	// An ignored file made after the judgement is one the caller carries
	// over: the directory is judged again, and its list is the one returned.
	writeFile(t, filepath.Join(f.skillDir, "Thumbs.db"), "explorer\n")
	now, err := inv.forkGuards(ctx, f, pre, "merged", true)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the ignored files under the lock", strings.Join(now.ignored, " "), ".DS_Store Thumbs.db")
	writeFile(t, filepath.Join(f.skillDir, "SKILL.md"), skill("notes", "Edited meanwhile"))
	_, err = inv.forkGuards(ctx, f, now, "merged", true)
	refused("an edit made after the judgement", err, exitRefused, "notes has uncommitted edits, so it cannot be merged until they are committed or reverted")
	if err := os.MkdirAll(filepath.Join(h.agentx, "merges", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	refused("a merge pending", guard(true), exitPendingMerge,
		"notes has a merge pending, so it cannot be merged until the merge is resolved or given up")
}

// TestCommitSubject is the subject skill commit writes when -m gives none.
func TestCommitSubject(t *testing.T) {
	t.Parallel()
	f := func(path, status string) changedFile { return changedFile{Path: path, Status: status} }
	for _, tc := range []struct {
		files []changedFile
		want  string
	}{
		{[]changedFile{f("SKILL.md", diffModified)}, "notes: edit SKILL.md (laptop)"},
		{[]changedFile{f("a.md", diffAdded), f("b.md", diffDeleted)}, "notes: add a.md and delete b.md (laptop)"},
		{[]changedFile{f("a.md", diffAdded), f("b.md", diffModified), f("c.md", diffDeleted)}, "notes: add a.md, edit b.md and 1 more file (laptop)"},
		{[]changedFile{f("a.md", diffAdded), f("b.md", diffAdded), f("c", diffAdded), f("d", diffAdded), f("e", diffAdded)}, "notes: add a.md, add b.md and 3 more files (laptop)"},
		{[]changedFile{f("two\nlines.md", diffAdded)}, "notes: add two lines.md (laptop)"},
		{nil, "notes: edit (laptop)"},
	} {
		equal(t, tc.want, commitSubject("notes", "laptop", tc.files), tc.want)
	}
}
