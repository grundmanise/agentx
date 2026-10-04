package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// sharedPublishHarness is a home that installed every skill of a shared
// source, alpha and beta, from a blobless fetch, with no account remote
// set. The source holds a README beside the skills and a symlink in
// alpha's folder, which the install leaves out. It returns the source's
// one commit.
func sharedPublishHarness(t *testing.T) (*harness, *sourceRepo, string) {
	t.Helper()
	h, s, ids := sharedPublishHome.copy(t)
	return h, s, ids[0]
}

var sharedPublishHome = &fixtureHome{
	source: "shared",
	dirs:   []string{".claude"},
	build: func(h *harness, s *sourceRepo) []string {
		s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
		s.skill("skills/beta", "beta", "The second skill", map[string]string{"notes.md": "beta notes\n"})
		s.write("README.md", "# shared\n")
		if err := os.Symlink("notes.md", filepath.Join(s.work, "skills", "alpha", "link")); err != nil {
			s.t.Fatal(err)
		}
		first := s.commit("first version")
		h.mustRun("source", "add", s.url)
		h.mustRun("skill", "add", s.url, "--all")
		return []string{first}
	},
}

// TestPublishToASharedSource publishes an edited alpha to the shared
// source it was installed from, on a home with no account remote: one
// commit on the source's main, whose one parent is the commit it was
// installed at, which changes alpha's folder alone, keeps the symlink the
// source holds there, is written by the user's identity with the message
// -m gives and no trailer of agentx's, and no other ref reaches the
// source; alpha stays managed and reads current. Then another clone
// pushes to beta's folder, and a second publish of alpha fast-forwards
// onto it, fetching the source alone although an account remote git cannot
// reach is set. A branch another clone moves between the fetch and the push
// is moved, exit 6, by the lease. Then the source changes alpha: a publish of a new edit is
// moved, exit 6, with the update hint, the edit stays and the update the
// check found is pinned. Once the update took the change in, a hook of the
// source declines the push, exit 6, with the hook's message, and nothing
// moves. Last, alpha's import branch moves while a publish runs: at the
// fetch, nothing is published, exit 6; after the push, the publish is
// reported with a warning and leaves the import branch as it was moved.
func TestPublishToASharedSource(t *testing.T) {
	t.Parallel()
	h, s, first := sharedPublishHarness(t)
	h.withIdentity("Ada Lovelace", "ada@example.com")
	installed := h.ref(lineage.ManagedRef("alpha"))
	lib := filepath.Join(h.library, "alpha")
	writeFile(t, filepath.Join(lib, "notes.md"), "alpha notes, mine\n")
	writeFile(t, filepath.Join(lib, "new.md"), "a file of mine\n")

	out := h.mustRun("--json", "skill", "publish", "alpha", "-m", "Tighten the notes")
	tip := s.query("rev-parse", "main")
	ev := h.one(out.stdout, "publish")
	equal(t, "outcome", ev["outcome"], publishPushed)
	equal(t, "source", ev["source"], s.url)
	equal(t, "branch", ev["branch"], "main")
	equal(t, "commit", ev["commit"], tip)
	equal(t, "the commit and its one parent", s.query("rev-list", "--parents", "-n", "1", tip), tip+" "+first)
	equal(t, "what it changes", s.query("diff-tree", "-r", "--name-only", "--no-commit-id", first, tip), "skills/alpha/new.md\nskills/alpha/notes.md")
	equal(t, "its author, committer and message", s.query("log", "-1", "--format=%an <%ae>|%cn <%ce>|%B", tip),
		"Ada Lovelace <ada@example.com>|Ada Lovelace <ada@example.com>|Tighten the notes")
	equal(t, "the symlink the source holds", strings.Fields(s.query("ls-tree", tip, "skills/alpha/link"))[0], "120000")
	equal(t, "the source's refs", s.query("for-each-ref", "--format=%(refname)"), "refs/heads/main")
	equal(t, "the account repo's fetch of the source, blobless", h.accountGit("config", "remote."+source.RemoteName(source.ID(s.url))+".partialclonefilter"), "blob:none")
	lib0 := h.librarySkill(out.stdout, "alpha")
	equal(t, "alpha's kind", lib0["kind"], "managed")
	equal(t, "alpha's state", lib0["state"], stateCurrent)
	equal(t, "the result", h.one(out.stdout, "result")["summary"], "published alpha")

	// Another clone of the source changes beta, and an account remote is
	// set, holding a skill of your own, that git cannot reach, which the
	// publish neither fetches nor fails on.
	s.run("reset", "--hard", "--quiet")
	s.write("skills/beta/notes.md", "beta notes, theirs\n")
	other := s.commit("Edit beta")
	writeFile(t, filepath.Join(lib, "notes.md"), "alpha notes, mine again\n")
	remote := newAccountRemote(t, h)
	h.setAccount(remote)
	h.mustRun("skill", "new", "jot") // a skill of your own, which a check fetches the account remote for
	if err := os.Rename(remote, remote+".away"); err != nil {
		t.Fatal(err)
	}
	out = h.mustRun("--verbose", "skill", "publish", "alpha")
	if err := os.Rename(remote+".away", remote); err != nil {
		t.Fatal(err)
	}
	if fetches(out.stderr) == 0 {
		t.Error("the publish fetched nothing")
	}
	for _, line := range strings.Split(out.stderr, "\n") {
		if strings.Contains(line, " fetch --") && strings.Contains(line, accountRemoteName(t, h)) {
			t.Errorf("the publish fetched the account remote: %s", line)
		}
	}
	second := s.query("rev-parse", "main")
	contains(t, "the line", out.stdout, "published alpha to main of "+s.url+" as "+short(second))
	equal(t, "a fast-forward onto the other clone's commit", s.query("rev-parse", second+"^"), other)
	equal(t, "its message", s.query("log", "-1", "--format=%B", second), "Update skills/alpha")

	// Another clone moves main between the publish's fetch and its push:
	// the lease refuses it.
	writeFile(t, filepath.Join(lib, "draft.md"), "a draft\n")
	managed := h.ref(lineage.ManagedRef("alpha"))
	s.write("skills/beta/notes.md", "beta notes, theirs again\n")
	next := s.commit("Edit beta again")
	s.run("update-ref", "refs/heads/main", second)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	path := h.env["PATH"]
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" push "*)
	`+real+` --git-dir=`+shellWord(s.gitDir)+` update-ref refs/heads/main `+next+` || exit 1 ;;
esac
exec `+real+` "$@"
`)
	out = h.run("--json", "skill", "publish", "alpha")
	h.env["PATH"] = path
	equal(t, "moved at the push: exit", out.exit, exitRefused.exit)
	equal(t, "moved at the push: outcome", h.one(out.stdout, "publish")["outcome"], publishMoved)
	e := h.one(out.stdout, "error")
	contains(t, "moved at the push: message", e["message"].(string), "moved main since it was fetched")
	contains(t, "moved at the push: hint", e["hint"].(string), "agentx skill update alpha")
	equal(t, "moved at the push: the import branch", h.ref(lineage.ManagedRef("alpha")), managed)
	equal(t, "moved at the push: the source", s.query("rev-parse", "main"), next)

	// The source changes alpha: a publish is moved.
	s.run("reset", "--hard", "--quiet")
	s.write("skills/alpha/notes.md", "alpha notes, theirs\n")
	s.commit("Edit alpha")
	upstream := s.query("rev-parse", "main")
	out = h.run("--json", "skill", "publish", "alpha")
	equal(t, "moved: exit", out.exit, exitRefused.exit)
	equal(t, "moved: outcome", h.one(out.stdout, "publish")["outcome"], publishMoved)
	e = h.one(out.stdout, "error")
	equal(t, "moved: message", e["message"], s.url+" changed alpha since it was installed, so it was not published")
	contains(t, "moved: hint", e["hint"].(string), "agentx skill update alpha")
	equal(t, "moved: the edit, kept", fileBody(t, filepath.Join(lib, "draft.md")), "a draft\n")
	equal(t, "moved: the import branch", h.ref(lineage.ManagedRef("alpha")), managed)
	if h.ref(lineage.CandidateRef("alpha")) == "" {
		t.Error("moved: the check pinned no update")
	}
	equal(t, "moved: the source", s.query("rev-parse", "main"), upstream)

	h.mustRun("skill", "update", "alpha")
	writeShim(t, mkdirs(t, filepath.Join(s.gitDir, "hooks"), "pre-receive"), "#!/bin/sh\necho 'no pushes on Fridays' >&2\nexit 1\n")
	managed = h.ref(lineage.ManagedRef("alpha"))
	out = h.run("--json", "skill", "publish", "alpha")
	equal(t, "declined: exit", out.exit, exitRefused.exit)
	equal(t, "declined: outcome", h.one(out.stdout, "publish")["outcome"], publishDeclined)
	e = h.one(out.stdout, "error")
	equal(t, "declined: message", e["message"], s.url+" declined main: pre-receive hook declined: no pushes on Fridays")
	excludes(t, "declined: hint", e["hint"].(string), "skill update")
	equal(t, "declined: the import branch", h.ref(lineage.ManagedRef("alpha")), managed)
	equal(t, "declined: the source", s.query("rev-parse", "main"), upstream)

	// Without the hook, alpha's import branch moves while the publish
	// runs: before the push, nothing is published; after it, the publish
	// is reported, with a warning, and the import branch is left as the
	// other command moved it.
	if err := os.Remove(filepath.Join(s.gitDir, "hooks", "pre-receive")); err != nil {
		t.Fatal(err)
	}
	move := real + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx)) + " update-ref " + lineage.ManagedRef("alpha") + " " + installed
	for _, at := range []string{"fetch", "push"} {
		stubGit(t, h, `#!/bin/sh
case " $* " in
*" `+at+` "*)
	`+real+` "$@"
	status=$?
	`+move+` || exit 1
	exit $status ;;
esac
exec `+real+` "$@"
`)
		out = h.run("--json", "skill", "publish", "alpha")
		h.env["PATH"] = path
		if at == "fetch" {
			equal(t, "moved before the push: exit", out.exit, exitRefused.exit)
			contains(t, "moved before the push: message", h.one(out.stdout, "error")["message"].(string), "alpha changed while it was being published, so nothing was published")
			equal(t, "moved before the push: the source", s.query("rev-parse", "main"), upstream)
			h.accountGit("update-ref", lineage.ManagedRef("alpha"), managed)
			continue
		}
		equal(t, "moved after the push: exit", out.exit, 0)
		equal(t, "moved after the push: outcome", h.one(out.stdout, "publish")["outcome"], publishPushed)
		contains(t, "moved after the push: warning", out.stderr, "alpha changed while it was being published, so its import branch was left as it is")
		equal(t, "moved after the push: the import branch", h.ref(lineage.ManagedRef("alpha")), installed)
	}
}

// TestSharedPublishRefusesWithoutChangingAnything refuses, in one home and
// changing nothing a skill reads, a publish to a shared source by a user
// with no git identity, before anything else; then, with one, publishes an
// unmodified skill as up to date with nothing fetched; refuses an edit
// that adds a symlink, which a publish never pushes; refuses a push the
// host denies, exit 3, and one whose answer names nothing of the branch,
// exit 3; and refuses an edit of a skill installed from a tag, which has
// no branch to publish to.
func TestSharedPublishRefusesWithoutChangingAnything(t *testing.T) {
	t.Parallel()
	h, s, first := sharedPublishHarness(t)
	managed := h.ref(lineage.ManagedRef("alpha"))
	out := h.run("--json", "skill", "publish", "alpha")
	equal(t, "no identity: exit", out.exit, exitRefused.exit)
	contains(t, "no identity: message", h.one(out.stdout, "error")["message"].(string), "git has no user.name and user.email")

	h.withIdentity("Ada Lovelace", "ada@example.com")
	out = h.mustRun("--json", "--verbose", "skill", "publish", "alpha")
	equal(t, "unmodified: outcome", h.one(out.stdout, "publish")["outcome"], publishUpToDate)
	equal(t, "unmodified: fetches", fetches(out.stderr), 0)

	lib := filepath.Join(h.library, "alpha")
	if err := os.Symlink("notes.md", filepath.Join(lib, "shortcut")); err != nil {
		t.Fatal(err)
	}
	out = h.run("--json", "skill", "publish", "alpha")
	equal(t, "an added symlink: exit", out.exit, exitRefused.exit)
	contains(t, "an added symlink: message", h.one(out.stdout, "error")["message"].(string), "skills/alpha/shortcut is a symlink")
	if err := os.Remove(filepath.Join(lib, "shortcut")); err != nil {
		t.Fatal(err)
	}

	// A push the host denies, and one that ends saying nothing of the
	// branch, change nothing either.
	writeFile(t, filepath.Join(lib, "notes.md"), "alpha notes, mine\n")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	path := h.env["PATH"]
	for _, tc := range []struct {
		what, push, outcome, says, hint string
	}{
		{"denied", `echo "remote: Permission to me/shared.git denied to someone." >&2
	echo "fatal: unable to access 'https://example.com/me/shared.git/': The requested URL returned error: 403" >&2
	exit 128`, publishDenied, "did not let this machine push main", "credential helper"},
		{"no answer for the branch", "exit 0", publishRefused, "whether " + s.url + " took main is not known", "that finishes the publish"},
	} {
		stubGit(t, h, `#!/bin/sh
case " $* " in
*" push "*)
	`+tc.push+` ;;
esac
exec `+real+` "$@"
`)
		out = h.run("--json", "skill", "publish", "alpha")
		h.env["PATH"] = path
		equal(t, tc.what+": exit", out.exit, exitSource.exit)
		equal(t, tc.what+": outcome", h.one(out.stdout, "publish")["outcome"], tc.outcome)
		e := h.one(out.stdout, "error")
		contains(t, tc.what+": message", e["message"].(string), tc.says)
		contains(t, tc.what+": hint", e["hint"].(string), tc.hint)
	}

	s.tag("v1")
	editSettings(t, h, func(st *home.Settings) {
		for i := range st.Sources {
			st.Sources[i].Pin = "v1"
		}
	})
	out = h.run("--json", "skill", "publish", "alpha")
	equal(t, "a tag pin: exit", out.exit, exitRefused.exit)
	e := h.one(out.stdout, "error")
	equal(t, "a tag pin: message", e["message"], "alpha has no branch of "+s.url+" to publish to: the source is not on a branch: v1 is refs/tags/v1")
	contains(t, "a tag pin: hint", e["hint"].(string), "agentx source add")

	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), managed)
	equal(t, "the source", s.query("for-each-ref", "--format=%(refname) %(objectname)", "refs/heads"), "refs/heads/main "+first)
}

// TestSharedPublishRefusal holds the order of the refusals a publish to a
// shared source makes before it judges the skill's edits.
func TestSharedPublishRefusal(t *testing.T) {
	t.Parallel()
	ok := sharedPublishState{name: "alpha", library: "/lib", held: true, readable: true, url: "https://example.com/s", added: true,
		libPath: "/lib/alpha", ident: gitx.Ident{Name: "Ada", Email: "ada@example.com"}}
	for _, tc := range []struct {
		name string
		edit func(*sharedPublishState)
		exit status
		says string
	}{
		{"nothing wrong", func(*sharedPublishState) {}, exitOK, ""},
		{"not in the library, before all else", func(st *sharedPublishState) {
			st.held, st.readable, st.added, st.pending, st.removed, st.ident = false, false, false, true, true, gitx.Ident{}
		}, exitNotFound, `the library holds no skill called "alpha" at /lib`},
		{"an unreadable import branch", func(st *sharedPublishState) { st.readable, st.added = false, false }, exitRefused, "records no version agentx can read"},
		{"the source removed, before a merge pending", func(st *sharedPublishState) { st.added, st.pending = false, true }, exitNotFound, "which was removed from this machine"},
		{"a merge pending, before the marker", func(st *sharedPublishState) { st.pending, st.removed = true, true }, exitPendingMerge, "has a merge with its update pending"},
		{"upstream removed, before what git cannot record", func(st *sharedPublishState) {
			st.removed, st.unrecordable = true, []string{"vendor"}
		}, exitRefused, "no longer holds alpha"},
		{"what git cannot record, before the identity", func(st *sharedPublishState) {
			st.unrecordable, st.ident = []string{"vendor"}, gitx.Ident{}
		}, exitRefused, "/lib/alpha/vendor"},
		{"no name", func(st *sharedPublishState) { st.ident.Name = "" }, exitRefused, "git has no user.name and user.email"},
		{"no email", func(st *sharedPublishState) { st.ident.Email = "" }, exitRefused, "git has no user.name and user.email"},
	} {
		st := ok
		tc.edit(&st)
		f := sharedPublishRefusal(st)
		if tc.exit == exitOK {
			if f != nil {
				t.Errorf("%s: refused: %s", tc.name, f.message)
			}
			continue
		}
		if f == nil {
			t.Errorf("%s: not refused", tc.name)
			continue
		}
		equal(t, tc.name+": exit", f.status, tc.exit)
		contains(t, tc.name, f.message, tc.says)
	}
}

// TestSharedFailure is the exit code a publish to a shared source reports
// for an error after its publish event, the one the run would report.
func TestSharedFailure(t *testing.T) {
	t.Parallel()
	inv := &invocation{dirs: home.Dirs{Home: "/home/agentx"}}
	for _, tc := range []struct {
		name string
		err  error
		exit status
		says string
	}{
		{"a lock another command holds", home.ErrLocked, exitLocked, "wait for the command holding"},
		{"a lock, through the account repo's failure", accountRepoFailure(fmt.Errorf("fetch: %w", home.ErrLocked)), exitLocked, "wait for the command holding"},
		{"a ref that moved", home.ErrMovedBeforeApply, exitRefused, "run the command again"},
		{"a journal that could not be finished", home.ErrRecovery, exitRefused, "move the journal aside"},
		{"a refusal", refuse(exitSource, "cannot reach it", "check it"), exitSource, "check it"},
		{"an account repo git cannot read", accountRepoFailure(errors.New("bad object")), exitAccountRepo, "agentx doctor"},
		{"anything else", errors.New("boom"), exitInternal, ""},
	} {
		f := inv.sharedFailure(tc.err)
		equal(t, tc.name+": exit", f.status, tc.exit)
		contains(t, tc.name+": hint", f.hint, tc.says)
	}
}

// TestOutside is the first path a publish's commit changes outside the
// skill's folder.
func TestOutside(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ out, subpath, want string }{
		{"skills/alpha/a\x00skills/alpha/b/c\x00", "skills/alpha", ""},
		{"skills/alpha/a\x00skills/alphabet/b\x00", "skills/alpha", "skills/alphabet/b"},
		{"skills/alpha\x00", "skills/alpha", "skills/alpha"},
		{"README.md\x00", "", ""},
	} {
		equal(t, "outside "+tc.subpath, outside(tc.out, tc.subpath), tc.want)
	}
}

// TestSharedPublishRecoversWhereItWasKilled kills a publish to a shared
// source with SIGKILL at the two boundaries of its finish: once the source
// took the commit, before the lock, and once the journal that moves the
// import branch is on disk. The next publish finishes it as up to date,
// pushing nothing again: alpha reads current, the source's main holds the
// one commit pushed, and no candidate or journal is left.
func TestSharedPublishRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, script string
		journal      bool
	}{
		{"after the push", `
case " $* " in
*" push "*)
	%GIT% "$@"
	status=$?
	: > %MUTATIONS%.pushed
	exit $status ;;
esac
if [ -e %MUTATIONS%.pushed ]; then
	kill -9 $PPID
	exit 1
fi
exec %GIT% "$@"
`, false},
		{"once the journal is on disk", killedUpdateScript, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, s, first := sharedPublishHarness(t)
			h.withIdentity("Ada Lovelace", "ada@example.com")
			writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "alpha notes, mine\n")
			out := killedChild(t, h, "TestPublishChildProcess", publishChildEnv, "alpha", tc.script)
			pushed := s.query("rev-parse", "main")
			if pushed == first {
				t.Fatalf("the killed publish pushed nothing:\n%s", out)
			}
			if tc.journal {
				_, kinds := journalKinds(t, h)
				equal(t, "the journal's steps", kinds, "ref")
			} else {
				equal(t, "journals before the next publish", journalCount(t, h), 0)
			}
			got := h.mustRun("--json", "skill", "publish", "alpha")
			equal(t, "the next publish: outcome", h.one(got.stdout, "publish")["outcome"], publishUpToDate)
			equal(t, "the source's main", s.query("rev-list", "--parents", "-n", "1", "main"), pushed+" "+first)
			equal(t, "alpha's state", h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")["state"], stateCurrent)
			equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), "")
			equal(t, "journals", journalCount(t, h), 0)
		})
	}
}
