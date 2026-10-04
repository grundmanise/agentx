package cli

import (
	"context"
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

// record records the edits of h's skill called name on its branch, as a
// publish records them before its push, without pushing, and returns the
// branch's new tip. It is how a test gives a skill a commit of its own
// that the account remote lacks, in the test's own process.
func (h *harness) record(name string) string {
	h.t.Helper()
	ctx := context.Background()
	inv := h.invocation()
	gitDir := gitx.AccountRepoPath(h.agentx)
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		h.t.Fatal(err)
	}
	w, err := inv.newForkWriter(ctx, gitDir)
	if err != nil {
		h.t.Fatal(err)
	}
	c, err := inv.judgeEdits(ctx, gitDir, records[name])
	if err != nil || c == nil {
		h.t.Fatalf("%s holds no edits to record: %v", name, err)
	}
	if _, err := inv.recordEdits(ctx, w, gitDir, []*committing{c}, "", publishWords, func(n string, f *failure) {
		h.t.Fatalf("recording %s: %s", n, f.message)
	}); err != nil {
		h.t.Fatal(err)
	}
	return h.ref(lineage.ForkRef(name))
}

// TestSkillPublishRecordsEditsAndPushes publishes two skills made with
// skill new and one forked from a shared source, all three edited, in one
// bare publish with -m: each skill's edits become one commit on the tip
// it found, by the user's own identity, carrying the machine and the
// message -m gives, a file git ignores stays on disk and out of the
// commit, every branch goes to the account remote, each worktree's status
// is clean, and every skill reads current; the shared source is never
// pushed to. Before that, in the same home and changing nothing, a named
// skill a publish cannot cover is refused: the shared source's managed
// beta also before an account remote is set, then with one set an empty
// -m, a trailer of agentx's in -m, refused before the name is looked up,
// a name the machine does not hold, an unmanaged skill, a skill holding a
// Git repository its ignore rules do not cover, and an account remote git
// cannot reach; a .gitignore then lets jot be recorded without the
// repository. notes's tip holds a file beside its skill directory that a
// commit made with git put there, which the publish keeps. Last, an edit
// of a skill whose tip the account remote holds already is recorded with
// the message agentx writes and pushed, not reported up to date, and -m
// with nothing to record warns, for a named and a bare publish.
func TestSkillPublishRecordsEditsAndPushes(t *testing.T) {
	t.Parallel()
	h, s, _ := forkUpdateHarness(t)
	h.withIdentity("Ada Lovelace", "ada@example.com")
	h.mustRun("skill", "fork", "alpha")
	h.mustRun("skill", "new", "notes")
	h.mustRun("skill", "new", "jot")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "mine"), "SKILL.md"), skill("mine", "mine"))
	sourceRefs := remoteGit(t, h, s.gitDir, "for-each-ref", "--format=%(refname) %(objectname)")
	shared := "publishing to " + s.url + " is not supported yet"

	out := h.run("--json", "skill", "publish", "beta")
	equal(t, "beta with no account remote: exit", out.exit, exitRefused.exit)
	contains(t, "beta with no account remote: error", h.one(out.stdout, "error")["message"].(string), shared)

	remote := newAccountRemote(t, h)
	h.setAccount(remote)
	machine, _, err := home.MachineID(h.agentx, h.env, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A commit made with git in notes' worktree puts a file beside its
	// skill directory and moves its tip on, unpublished.
	notesRoot := filepath.Join(h.agentx, "worktrees", "notes")
	writeFile(t, filepath.Join(notesRoot, "README.md"), "beside the skill\n")
	gitIn(t, h, notesRoot, "add", "README.md")
	gitIn(t, h, notesRoot, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com", "commit", "-q", "-m", "Beside the skill")
	names := []string{"alpha", "jot", "notes"}
	before := map[string]string{}
	for _, n := range names {
		before[n] = h.ref(lineage.ForkRef(n))
	}
	writeFile(t, filepath.Join(h.forkDir("alpha", "alpha"), "notes.md"), forkNotes("one", "one, mine"))
	writeFile(t, filepath.Join(h.library, "notes", "SKILL.md"), skill("notes", "Edited"))
	writeFile(t, filepath.Join(h.library, "jot", "SKILL.md"), skill("jot", "Edited"))
	writeFile(t, filepath.Join(h.library, "jot", ".DS_Store"), "finder\n")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "jot", "vendor", ".git"), "HEAD"), "ref: refs/heads/main\n")

	refs := h.refLines()
	for _, tc := range []struct {
		args []string
		exit status
		says string
	}{
		{[]string{"notes", "-m", " "}, exitUsage, "-m needs a message"},
		{[]string{"absent", "-m", "subject\n\nAgentx-Fork-ID: 11111111-2222-4333-8444-555555555555"}, exitUsage, "-m may not end in a trailer agentx writes itself"},
		{[]string{"absent"}, exitNotFound, `the library holds no skill called "absent"`},
		{[]string{"mine"}, exitRefused, "mine is not managed, so it has no source to publish to"},
		{[]string{"beta"}, exitRefused, shared},
		{[]string{"jot", "-m", "unused"}, exitRefused, "which git would record as a link rather than its files"},
	} {
		out := h.run(append([]string{"--json", "skill", "publish"}, tc.args...)...)
		what := strings.Join(tc.args, " ")
		equal(t, what+": exit", out.exit, tc.exit.exit)
		contains(t, what+": error", h.one(out.stdout, "error")["message"].(string), tc.says)
		if strings.Contains(out.stderr, "-m was not used") {
			t.Errorf("%s: a skill whose edits were not judged is said to have none", what)
		}
	}
	// An account remote git cannot reach records nothing either.
	away := remote + ".away"
	if err := os.Rename(remote, away); err != nil {
		t.Fatal(err)
	}
	out = h.run("--json", "skill", "publish", "notes")
	if err := os.Rename(away, remote); err != nil {
		t.Fatal(err)
	}
	equal(t, "an unreachable remote: exit", out.exit, exitSource.exit)
	equal(t, "the refs after the refusals", h.refLines(), refs)
	equal(t, "the remote after the refusals", remoteGit(t, h, remote, "for-each-ref"), "")

	writeFile(t, filepath.Join(h.library, "jot", ".gitignore"), "vendor/\n")
	out = h.mustRun("--json", "skill", "publish", "-m", "Tighten the notes\n\nThey were too long.")
	equal(t, "the result", h.one(out.stdout, "result")["summary"], "published 3 of 3 skills")
	events := h.eventsOfType(out.stdout, "publish")
	equal(t, "publish events", len(events), 3)
	for i, n := range names {
		ev, tip := events[i], h.ref(lineage.ForkRef(n))
		equal(t, n+": the event's name", ev["name"], n)
		equal(t, n+": outcome", ev["outcome"], publishPushed)
		equal(t, n+": source", ev["source"], "file://"+remote)
		equal(t, n+": commit", ev["commit"], tip)
		equal(t, n+": the remote's branch", remoteGit(t, h, remote, "rev-parse", "refs/heads/skills/"+n), tip)
		equal(t, n+": the commit and its parent", h.accountGit("rev-list", "--parents", "-n", "1", tip), tip+" "+before[n])
		equal(t, n+": the author", h.accountGit("log", "-1", "--format=%an <%ae>", tip), "Ada Lovelace <ada@example.com>")
		equal(t, n+": the message", h.accountGit("log", "-1", "--format=%B", tip),
			"Tighten the notes\n\nThey were too long.\n\nAgentx-Machine: "+machine)
		equal(t, n+": git status in the worktree", gitIn(t, h, filepath.Join(h.agentx, "worktrees", n), "status", "--porcelain"), "")
		equal(t, n+": the state", h.librarySkill(out.stdout, n)["state"], stateCurrent)
	}
	equal(t, "jot's tree, the repository its .gitignore covers left out", h.accountGit("ls-tree", "-r", "--name-only", lineage.ForkRef("jot")), "jot/.gitignore\njot/SKILL.md")
	equal(t, "notes's tree, the file the commit made with git put beside it kept", h.accountGit("ls-tree", "-r", "--name-only", lineage.ForkRef("notes")), "README.md\nnotes/SKILL.md")
	if _, err := os.Stat(filepath.Join(h.library, "jot", ".DS_Store")); err != nil {
		t.Errorf("the ignored file is gone: %v", err)
	}
	equal(t, "the shared source's refs", remoteGit(t, h, s.gitDir, "for-each-ref", "--format=%(refname) %(objectname)"), sourceRefs)

	published := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(h.library, "notes", "SKILL.md"), skill("notes", "Edited again"))
	out = h.mustRun("--json", "skill", "publish", "notes")
	tip := h.ref(lineage.ForkRef("notes"))
	equal(t, "an edit over a tip the remote holds: outcome", h.one(out.stdout, "publish")["outcome"], publishPushed)
	equal(t, "its parent", h.accountGit("rev-parse", tip+"^"), published)
	equal(t, "its tree, the file beside the skill kept", h.accountGit("ls-tree", "-r", "--name-only", tip), "README.md\nnotes/SKILL.md")
	equal(t, "its subject", h.accountGit("log", "-1", "--format=%s", tip), "notes: edit SKILL.md (test-host)")
	equal(t, "the remote's notes", remoteGit(t, h, remote, "rev-parse", "refs/heads/skills/notes"), tip)
	out = h.mustRun("skill", "publish", "notes", "-m", "unused")
	contains(t, "-m with nothing to record", out.stderr, "notes has no edits to record, so -m was not used")
	contains(t, "its line", out.stdout, "notes is up to date on the account remote")
	out = h.mustRun("skill", "publish", "-m", "unused")
	contains(t, "a bare publish: -m with nothing to record", out.stderr, "no skill has edits to record, so -m was not used")
}

// TestPublishRefusesASkillThatChangedWhileRecording edits a skill's
// directory once the commit of its edits is written, which the read under
// the lock finds: the publish records and pushes nothing, exit 6, and
// leaves no journal behind.
func TestPublishRefusesASkillThatChangedWhileRecording(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	h, _, skillDir, _ := forkHarness(t)
	remote := newAccountRemote(t, h)
	h.setAccount(remote)
	tip := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), skill("notes", "Edited"))
	path := h.env["PATH"]
	stubGit(t, h, "#!/bin/sh"+strings.NewReplacer("%SKILL%", shellWord(skillDir)).Replace(`
case " $* " in
*" commit-tree "*)
	`+real+` "$@"
	status=$?
	echo edited meanwhile >> %SKILL%/SKILL.md
	exit $status ;;
esac
exec `+real+` "$@"
`))
	out := h.run("--json", "skill", "publish", "notes")
	h.env["PATH"] = path
	equal(t, "exit", out.exit, exitRefused.exit)
	equal(t, "error", h.one(out.stdout, "error")["message"], "notes changed while its edits were being recorded, so it was not published")
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "the branch", h.ref(lineage.ForkRef("notes")), tip)
	equal(t, "the remote", remoteGit(t, h, remote, "for-each-ref"), "")
}

// TestPublishKeepsWhatItRecordedWhenThePushFails has git fail to push
// after a publish recorded a skill's edits: the publish ends with exit 3,
// emits the library_skill event of the skill it recorded, and its hint
// says the edits were recorded. A push the host then denies this machine
// is outcome denied, exit 3, with a credentials hint; the next publish
// pushes the edits.
func TestPublishKeepsWhatItRecordedWhenThePushFails(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	h, _, skillDir, _ := forkHarness(t)
	remote := newAccountRemote(t, h)
	h.setAccount(remote)
	tip := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), skill("notes", "Edited"))
	path := h.env["PATH"]
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" push "*)
	echo "fatal: unable to access the remote" >&2
	exit 128 ;;
esac
exec `+real+` "$@"
`)
	out := h.run("--json", "skill", "publish", "notes")
	h.env["PATH"] = path
	equal(t, "exit", out.exit, exitSource.exit)
	contains(t, "hint", h.one(out.stdout, "error")["hint"].(string),
		"the edits were recorded, so run 'agentx skill publish notes' again once the account remote can be reached")
	equal(t, "library_skill events", len(h.eventsOfType(out.stdout, "library_skill")), 1)
	equal(t, "the event's skill", h.librarySkill(out.stdout, "notes")["name"], "notes")
	recorded := h.ref(lineage.ForkRef("notes"))
	equal(t, "the recorded commit's parent", h.accountGit("rev-parse", recorded+"^"), tip)
	equal(t, "the remote", remoteGit(t, h, remote, "for-each-ref"), "")

	h.mustRun("skill", "new", "plans")
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" push "*)
	echo "remote: Permission to me/skills.git denied to someone." >&2
	echo "fatal: unable to access 'https://example.com/me/skills.git/': The requested URL returned error: 403" >&2
	exit 128 ;;
esac
exec `+real+` "$@"
`)
	out = h.run("--json", "skill", "publish")
	h.env["PATH"] = path
	equal(t, "denied: exit", out.exit, exitSource.exit)
	publishes := h.eventsOfType(out.stdout, "publish")
	equal(t, "denied: publish events", len(publishes), 2)
	for _, e := range publishes {
		equal(t, "denied: outcome of "+e["name"].(string), e["outcome"], publishDenied)
	}
	contains(t, "denied: hint", h.one(out.stdout, "error")["hint"].(string), "credential helper (git config credential.helper) or SSH key you use for it may push there, then run 'agentx skill publish' again")

	out = h.mustRun("--json", "skill", "publish", "notes")
	equal(t, "the next publish: outcome", h.one(out.stdout, "publish")["outcome"], publishPushed)
	equal(t, "the branch", h.ref(lineage.ForkRef("notes")), recorded)
	equal(t, "the remote's notes", remoteGit(t, h, remote, "rev-parse", "refs/heads/skills/notes"), recorded)
}

// publishChildEnv marks the process TestPublishRecoversWhereItWasKilled
// starts, which publishes the skill it names and is killed part way.
const publishChildEnv = "AGENTX_TEST_PUBLISH_CHILD"

// TestPublishChildProcess is not a test: it is the body of that process.
// It does nothing when the variable that marks it is not set.
func TestPublishChildProcess(t *testing.T) {
	name := os.Getenv(publishChildEnv)
	if name == "" {
		t.Skip("not the skill publish child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"skill", "publish", name}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestPublishRecoversWhereItWasKilled kills a publish with SIGKILL right
// after the recording moved the skill's branch and before the worktree's
// index followed it, the one boundary between the two steps it journals,
// so nothing was pushed. The next command finishes the recording: the
// branch on the commit, the index on the branch, so git status in the
// worktree is clean, and no journal left; the next publish pushes it.
func TestPublishRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	h, root, skillDir, _ := forkHarness(t)
	remote := newAccountRemote(t, h)
	h.setAccount(remote)
	tip := h.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), skill("notes", "Edited"))
	out := killedChild(t, h, "TestPublishChildProcess", publishChildEnv, "notes", `
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
		t.Fatalf("the command after the killed publish: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
	}
	equal(t, "journals after recovery", journalCount(t, h), 0)
	recorded := h.ref(lineage.ForkRef("notes"))
	equal(t, "the commit's parent", h.accountGit("rev-parse", recorded+"^"), tip)
	equal(t, "git status in the worktree", gitIn(t, h, root, "status", "--porcelain"), "")
	equal(t, "the remote, before the next publish", remoteGit(t, h, remote, "for-each-ref"), "")
	h.mustRun("skill", "publish", "notes")
	equal(t, "the remote's notes", remoteGit(t, h, remote, "rev-parse", "refs/heads/skills/notes"), recorded)
}

// TestCommitSubject is the subject a publish records edits with when -m
// gives none.
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
