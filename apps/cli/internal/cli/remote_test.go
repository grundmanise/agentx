package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// newAccountRemote is an empty bare repository under the test's own
// directory, for the homes of one test to share as their account remote.
func newAccountRemote(t *testing.T, h *harness) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(root, "remote.git")
	if _, err := gitx.New(h.env, false, func(string, ...any) {}).Isolated(context.Background(), remote, "init", "--bare", "--quiet"); err != nil {
		t.Fatal(err)
	}
	return remote
}

// remoteGit runs git against the bare repository at remote, as a plain git
// reads it, and returns stdout without its trailing newline.
func remoteGit(t *testing.T, h *harness, remote string, args ...string) string {
	t.Helper()
	out, err := gitx.New(h.env, false, func(string, ...any) {}).Isolated(context.Background(), remote, args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), remote, err)
	}
	return out
}

// placeRemoteFork gives h the fork called name the account remote holds,
// as an install of it from there does: its branch at the remote-tracking
// branch, checked out and placed by skill place.
func (h *harness) placeRemoteFork(name string) {
	h.t.Helper()
	h.accountGit("update-ref", lineage.ForkRef(name), lineage.RemoteForkRef(name))
	h.mustRun("skill", "place", name)
}

// twoHomes is two machines of one user with one account remote: a, with
// alpha and beta installed from a source at its first commit and forked,
// both published, and b, a machine with the same source added that took
// both forks from the account remote. Each has a git identity of its own.
// It returns the source and the remote.
func twoHomes(t *testing.T) (a, b *harness, s *sourceRepo, remote string) {
	t.Helper()
	a, s, _ = forkUpdateHarness(t)
	a.withIdentity("Machine A", "a@example.com")
	a.mustRun("skill", "fork", "alpha")
	a.mustRun("skill", "fork", "beta")
	remote = newAccountRemote(t, a)
	a.mustRun("remote", "set", remote)
	a.mustRun("publish", "--all")

	b = newHarness(t)
	b.build(t, fixture{dirs: []string{".claude"}})
	b.rewrite(s)
	b.withIdentity("Machine B", "b@example.com")
	b.mustRun("source", "add", s.url)
	b.mustRun("remote", "set", remote)
	b.placeRemoteFork("alpha")
	b.placeRemoteFork("beta")
	return a, b, s, remote
}

// commitFork writes notes.md of the fork called name on h and commits it.
func (h *harness) commitFork(name, notes string) string {
	h.t.Helper()
	writeFile(h.t, filepath.Join(h.forkDir(name, name), "notes.md"), notes)
	h.mustRun("skill", "commit", name)
	return h.ref(lineage.ForkRef(name))
}

// TestPullFastForwardsMergesAndConflicts pulls on machine b what machine a
// published, each way a pull goes. alpha, which b did not change, is
// fast-forwarded to a's commit, its worktree following. Then a takes the
// source's second version into alpha while b commits an edit of its own:
// b's pull merges the two cleanly, a merge commit of b's tip and a's on
// b's branch, written with b's identity, and since b's account repo has
// not fetched the source's second version, nothing proves it newer and
// the merge keeps b's base. Once b fetched the source, the next merge of
// the two records the newer version as the base. Last, a pull of every
// fork in one run: beta, whose line both machines changed, conflicts and
// is left pending in its checkout under the merges directory, exit 4,
// while alpha, which holds an uncommitted edit, is refused, exit 6, and
// the run says both; the fork's worktree and branch stay as they were,
// with no conflict marker in the worktree. Resolved with git in the
// checkout, the next pull completes beta's merge.
func TestPullFastForwardsMergesAndConflicts(t *testing.T) {
	t.Parallel()
	a, b, s, _ := twoHomes(t)
	alphaB := b.forkDir("alpha", "alpha")

	one := a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("publish", "alpha")
	out := b.mustRun("--json", "pull", "alpha")
	ev := b.one(out.stdout, "pull")
	equal(t, "outcome", ev["outcome"], pullFastForward)
	equal(t, "commit", ev["commit"], one)
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), one)
	equal(t, "b's notes", fileBody(t, filepath.Join(alphaB, "notes.md")), forkNotes("one", "one, a"))
	equal(t, "git status", gitIn(t, b, filepath.Join(b.agentx, "worktrees", "alpha"), "status", "--porcelain"), "")
	b.librarySkill(out.stdout, "alpha")

	importOne := a.accountGit("rev-parse", one+"~2") // the import, then the fork's creation commit, then a's commit
	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, upstream"))
	s.commit("second version")
	a.mustRun("skill", "check")
	a.mustRun("skill", "update", "alpha")
	importTwo := a.accountGit("rev-parse", lineage.ForkRef("alpha")+"^2")
	a.mustRun("publish", "alpha")
	mine := b.commitFork("alpha", forkNotes("one", "one, a", "two", "two, b"))
	out = b.mustRun("--json", "pull", "alpha")
	ev = b.one(out.stdout, "pull")
	equal(t, "outcome", ev["outcome"], pullMerged)
	merged := b.ref(lineage.ForkRef("alpha"))
	equal(t, "commit", ev["commit"], merged)
	equal(t, "the merge's parents", b.parents(merged), mine+"\n"+a.ref(lineage.ForkRef("alpha")))
	equal(t, "the base, unproved", b.trailer(merged, lineage.TrailerBase), importOne)
	equal(t, "base", ev["base"], importOne)
	equal(t, "the subject", b.accountGit("log", "-1", "--format=%s", merged), "alpha: merge the account remote (test-host)")
	equal(t, "the author", b.accountGit("log", "-1", "--format=%an <%ae>", merged), "Machine B <b@example.com>")
	equal(t, "b's notes", fileBody(t, filepath.Join(alphaB, "notes.md")), forkNotes("one", "one, a", "two", "two, b", "seven", "seven, upstream"))

	a.commitFork("alpha", forkNotes("one", "one, a", "seven", "seven, upstream", "four", "four, a"))
	a.mustRun("publish", "alpha")
	b.commitFork("alpha", forkNotes("one", "one, a", "two", "two, b", "seven", "seven, upstream", "eight", "eight, b"))
	b.mustRun("source", "fetch", s.url)
	b.mustRun("pull", "alpha")
	equal(t, "the base, proved newer", b.trailer(b.ref(lineage.ForkRef("alpha")), lineage.TrailerBase), importTwo)

	a.commitFork("beta", forkNotes("six", "six, a"))
	a.mustRun("publish", "beta")
	betaTip := b.commitFork("beta", forkNotes("six", "six, b"))
	a.commitFork("alpha", forkNotes("one", "one, a", "seven", "seven, upstream", "four", "four, a", "three", "three, a"))
	a.mustRun("publish", "alpha")
	alphaTip := b.ref(lineage.ForkRef("alpha"))
	writeFile(t, filepath.Join(alphaB, "notes.md"), "uncommitted\n")
	out = b.run("--json", "pull")
	equal(t, "exit", out.exit, 6)
	outcomes := map[string]any{}
	for _, e := range b.eventsOfType(out.stdout, "pull") {
		outcomes[e["name"].(string)] = e["outcome"]
	}
	equal(t, "alpha", outcomes["alpha"], pullRefused)
	equal(t, "beta", outcomes["beta"], pullConflict)
	contains(t, "the error", b.one(out.stdout, "error")["message"].(string), "alpha: alpha has uncommitted edits, so it cannot be pulled")
	contains(t, "the error", b.one(out.stdout, "error")["message"].(string), "beta: beta conflicts with the account remote in 1 file")
	conflict := b.one(out.stdout, "conflict")
	equal(t, "kind", conflict["kind"], lineage.KindFork)
	equal(t, "mine", conflict["mine"], betaTip)
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), alphaTip)
	equal(t, "b's beta", b.ref(lineage.ForkRef("beta")), betaTip)
	betaB := b.forkDir("beta", "beta")
	equal(t, "b's beta notes", fileBody(t, filepath.Join(betaB, "notes.md")), forkNotes("six", "six, b"))
	head, mergeHead, msg := mergeState(t, b, "beta")
	equal(t, "HEAD", head, betaTip)
	equal(t, "MERGE_HEAD", mergeHead, a.ref(lineage.ForkRef("beta")))
	contains(t, "MERGE_MSG", msg, "beta: merge the account remote (test-host)")
	contains(t, "the checkout's notes", fileBody(t, filepath.Join(pendingCheckout(b, "beta"), "beta", "notes.md")), "<<<<<<<< ")

	writeFile(t, filepath.Join(pendingCheckout(b, "beta"), "beta", "notes.md"), forkNotes("six", "six, a and b"))
	checkoutGit(t, b, "beta", "add", "beta/notes.md")
	out = b.mustRun("--json", "pull", "beta")
	equal(t, "outcome", b.one(out.stdout, "pull")["outcome"], pullMerged)
	done := b.ref(lineage.ForkRef("beta"))
	equal(t, "the completed merge's parents", b.parents(done), betaTip+"\n"+a.ref(lineage.ForkRef("beta")))
	equal(t, "b's beta notes", fileBody(t, filepath.Join(betaB, "notes.md")), forkNotes("six", "six, a and b"))
	noCheckout(t, b, "beta")
	if _, err := os.Stat(filepath.Join(b.agentx, "merges", "beta")); err == nil {
		t.Error("the checkout is still there")
	}
}

// TestPublishMergesFirstAndNamesUncommittedForks publishes from machine b
// while machine a published alpha first. With an uncommitted edit, b's
// publish of alpha would have to merge a's commit, and is refused, exit 6,
// nothing pushed. Once the edit is reverted, the publish merges a's commit
// first, as a pull does, and pushes the merge; beta, committed on b and
// not named, stays unpushed. A publish with nothing to take in pushes
// alpha's commits even though alpha holds an uncommitted edit, and names
// it: publishing never commits. The push carries the fork branches alone:
// the remote holds no import branch, update candidate or other ref.
func TestPublishMergesFirstAndNamesUncommittedForks(t *testing.T) {
	t.Parallel()
	a, b, _, remote := twoHomes(t)
	alphaB := b.forkDir("alpha", "alpha")
	theirs := a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("publish", "alpha")
	mine := b.commitFork("alpha", forkNotes("eight", "eight, b"))
	betaBefore := remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/beta")
	b.commitFork("beta", forkNotes("two", "two, b"))

	writeFile(t, filepath.Join(alphaB, "SKILL.md"), "uncommitted\n")
	out := b.run("--json", "publish", "alpha")
	equal(t, "exit", out.exit, 6)
	contains(t, "the error", b.one(out.stdout, "error")["message"].(string), "alpha has uncommitted edits, so it cannot be published")
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), theirs)

	b.mustRun("skill", "revert", "alpha")
	out = b.mustRun("--json", "publish", "alpha")
	merged := b.ref(lineage.ForkRef("alpha"))
	equal(t, "the merge's parents", b.parents(merged), mine+"\n"+theirs)
	equal(t, "pull outcome", b.one(out.stdout, "pull")["outcome"], pullMerged)
	ev := b.one(out.stdout, "publish")
	equal(t, "outcome", ev["outcome"], publishPushed)
	equal(t, "commit", ev["commit"], merged)
	equal(t, "uncommitted", ev["uncommitted"], false)
	var phases []string
	for _, e := range b.eventsOfType(out.stdout, "progress") {
		phases = append(phases, e["phase"].(string)+" "+e["subject"].(string))
	}
	equal(t, "progress", strings.Join(phases, ", "), "fetch "+remote+", publish alpha")
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), merged)
	equal(t, "the remote's beta", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/beta"), betaBefore)

	tip := b.commitFork("alpha", forkNotes("one", "one, a", "eight", "eight, b", "two", "two, b"))
	writeFile(t, filepath.Join(alphaB, "notes.md"), "uncommitted\n")
	out = b.run("publish", "alpha")
	equal(t, "exit", out.exit, 0)
	contains(t, "the line", out.stdout, "published alpha as "+short(tip))
	contains(t, "the warning", out.stderr, "alpha has uncommitted edits, which were not published")
	out = b.mustRun("--json", "publish", "--all")
	equal(t, "summary", b.one(out.stdout, "result")["summary"], "published 1 of 2 forks; uncommitted edits were not published: alpha")
	for _, e := range b.eventsOfType(out.stdout, "publish") {
		switch e["name"] {
		case "alpha":
			equal(t, "alpha", e["outcome"], publishUpToDate)
			equal(t, "alpha uncommitted", e["uncommitted"], true)
		case "beta":
			equal(t, "beta", e["outcome"], publishPushed)
		}
	}
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), tip)
	equal(t, "fork progress", len(b.eventsOfType(out.stdout, "progress")), 3)
	equal(t, "the remote's refs", remoteGit(t, b, remote, "for-each-ref", "--format=%(refname)"), "refs/heads/skills/alpha\nrefs/heads/skills/beta")
}

// TestPublishRefusesADifferentFork creates a skill of the same name on two
// machines, which are two forks with two fork ids. The first publishes it;
// the second's publish and pull of it are refused, exit 6, and the hint
// names the way out, a fork under another name, and the remote keeps the
// first machine's branch.
func TestPublishRefusesADifferentFork(t *testing.T) {
	t.Parallel()
	a, _, _, _ := forkHarness(t)
	remote := newAccountRemote(t, a)
	a.mustRun("remote", "set", remote)
	out := a.mustRun("--json", "publish", "notes")
	equal(t, "outcome", a.one(out.stdout, "publish")["outcome"], publishPushed)
	equal(t, "summary", a.one(out.stdout, "result")["summary"], "published notes")

	b, _, _, _ := forkHarness(t)
	b.mustRun("remote", "set", remote)
	for _, cmd := range []string{"publish", "pull"} {
		out := b.run("--json", cmd, "notes")
		equal(t, cmd+": exit", out.exit, 6)
		e := b.one(out.stdout, "error")
		contains(t, cmd+": message", e["message"].(string), "the account remote's skills/notes is a different fork than notes on this machine")
		contains(t, cmd+": hint", e["hint"].(string), "agentx skill fork notes --name <new>")
	}
	equal(t, "the remote's notes", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/notes"), a.ref(lineage.ForkRef("notes")))

	// An export of a lists notes as a fork; b holds another notes, which is
	// different, and a fresh machine that set the account remote holds a's,
	// present, until a publishes another commit of it.
	file := a.exportPath("export.json")
	a.mustRun("export", file)
	state := func(h *harness) jsonEvent {
		t.Helper()
		return h.one(h.mustRun("--json", "import", file, "--yes").stdout, "import_skill")
	}
	ev := state(b)
	equal(t, "b's notes", ev["state"], restoreDifferent)
	equal(t, "b's commit", ev["local_commit"], b.ref(lineage.ForkRef("notes")))
	fresh := newHarness(t)
	fresh.mustRun("remote", "set", remote)
	ev = state(fresh)
	equal(t, "the fresh machine's notes", ev["state"], restorePresent)
	equal(t, "its kind", ev["local_kind"], lineage.KindFork)
	writeFile(t, filepath.Join(a.forkDir("notes", "notes"), "more.md"), "more\n")
	a.mustRun("skill", "commit", "notes")
	a.mustRun("publish", "notes")
	fresh.mustRun("remote", "set", remote)
	ev = state(fresh)
	equal(t, "the fresh machine's notes, published again", ev["state"], restoreDifferent)
	equal(t, "its commit", ev["local_commit"], a.ref(lineage.ForkRef("notes")))
}

// TestRemoteSetShowUnset attaches, shows and detaches the account remote.
// With none set, show says so and pull and publish are refused, exit 6. A
// URL with a password is refused, exit 1, and one git cannot reach, exit
// 3, before anything is written, an account repo included. A remote that
// is set has one fetch refspec, the fork branches, and no tags, and what
// it holds is fetched. Setting another URL forgets what the first one held;
// unsetting it removes its configuration and its remote-tracking branches,
// and the local fork stays.
func TestRemoteSetShowUnset(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.mustRun("--json", "remote", "show")
	if _, ok := h.one(out.stdout, "remote")["url"]; ok {
		t.Errorf("a remote is shown on a machine with none:\n%s", out.stdout)
	}
	for _, args := range [][]string{{"pull"}, {"publish", "--all"}} {
		out := h.run(append([]string{"--json"}, args...)...)
		equal(t, args[0]+": exit", out.exit, 6)
		equal(t, args[0]+": message", h.one(out.stdout, "error")["message"], "no account remote is set")
	}
	equal(t, "a password", h.run("remote", "set", "https://me:secret@example.com/skills.git").exit, 1)
	remote := newAccountRemote(t, h)
	equal(t, "an unreachable remote", h.run("remote", "set", remote+"-missing").exit, 3)
	if _, err := os.Stat(gitx.AccountRepoPath(h.agentx)); err == nil {
		t.Error("a remote that could not be set created the account repo")
	}

	h.mustRun("skill", "new", "notes")
	h.mustRun("remote", "set", remote)
	h.mustRun("publish", "notes")
	other := newAccountRemote(t, h)
	h.mustRun("remote", "set", remote) // setting it again is no change
	equal(t, "the remote-tracking branch", h.ref(lineage.RemoteForkRef("notes")), h.ref(lineage.ForkRef("notes")))
	out = h.mustRun("--json", "remote", "set", other)
	ev := h.one(out.stdout, "remote")
	equal(t, "url", ev["url"], other)
	equal(t, "fetch", ev["fetch"], gitx.ForkRefspec)
	equal(t, "fetch refspecs", h.accountGit("config", "--get-all", "remote.origin.fetch"), gitx.ForkRefspec)
	equal(t, "tags", h.accountGit("config", "--get", "remote.origin.tagOpt"), "--no-tags")
	equal(t, "what the first remote held", h.ref(lineage.RemoteForkRef("notes")), "")
	out = h.mustRun("remote", "show")
	equal(t, "show", strings.TrimSpace(out.stdout), "origin  "+other)

	h.mustRun("remote", "set", remote)
	h.mustRun("remote", "unset")
	if out, err := h.accountGitErr("config", "--get-regexp", "^remote\\."); err == nil {
		t.Errorf("the remote's configuration is still there: %s", out)
	}
	equal(t, "remote-tracking branches", h.accountGit("for-each-ref", "refs/remotes/"), "")
	if h.ref(lineage.ForkRef("notes")) == "" {
		t.Error("unsetting the remote took the local fork")
	}
	contains(t, "unset again", h.mustRun("remote", "unset").stdout, "No account remote is set")
}

// TestRemoteURLRefusal is which URLs the account remote can be set to.
func TestRemoteURLRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		url    string
		reason string // a word of the refusal, "" for a URL that is accepted
	}{
		{"https://github.com/me/skills.git", ""},
		{"ssh://git@github.com/me/skills.git", ""},
		{"git@github.com:me/skills.git", ""},
		{"github.com:me/skills.git", ""},
		{"file:///srv/skills.git", ""},
		{"/srv/skills.git", ""},
		{"", "not a URL"},
		{" https://github.com/me/skills.git", "not a URL"},
		{"--upload-pack=x", "not a URL"},
		{"me/skills", "not a URL"},
		{"skills.git", "not a URL"},
		{"https://github.com/me/skills.git\n", "not a URL"},
		{"https://me:secret@github.com/me/skills.git", "password or a token"},
		{"https://token@github.com/me/skills.git", "password or a token"},
		{"ssh://git:secret@github.com/me/skills.git", "password or a token"},
		{"git:secret@github.com:me/skills.git", "password or a token"},
		{"https://github.com/me/skills.git#main", "names a ref"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			f := remoteURLRefusal(tc.url)
			switch {
			case tc.reason == "" && f != nil:
				t.Errorf("refused: %s", f.message)
			case tc.reason != "" && f == nil:
				t.Errorf("accepted, want a refusal saying %q", tc.reason)
			case f != nil:
				contains(t, "the refusal", f.message, tc.reason)
				equal(t, "exit", f.status.exit, 1)
			}
		})
	}
}

// TestSkillUpdateOfAForkPullsThenMerges updates on machine b a fork that
// machine a published a commit of and whose upstream has a new version: the
// update takes in a's commit first, as a merge of its own with b's commit,
// then merges the upstream version on top of it, two commits on b's branch.
func TestSkillUpdateOfAForkPullsThenMerges(t *testing.T) {
	t.Parallel()
	a, b, s, _ := twoHomes(t)
	theirs := a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("publish", "alpha")
	mine := b.commitFork("alpha", forkNotes("three", "three, b"))
	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, upstream"))
	second := s.commit("second version")
	b.mustRun("skill", "check")
	candidate := b.ref(lineage.CandidateRef("alpha"))

	out := b.mustRun("--json", "skill", "update", "alpha")
	equal(t, "pull outcome", b.one(out.stdout, "pull")["outcome"], pullMerged)
	tip := b.ref(lineage.ForkRef("alpha"))
	account := b.accountGit("rev-parse", tip+"^1")
	equal(t, "the update's parents", b.parents(tip), account+"\n"+candidate)
	equal(t, "the account merge's parents", b.parents(account), mine+"\n"+theirs)
	equal(t, "the base", b.trailer(tip, lineage.TrailerBase), candidate)
	equal(t, "b's notes", fileBody(t, filepath.Join(b.forkDir("alpha", "alpha"), "notes.md")), forkNotes("one", "one, a", "three", "three, b", "seven", "seven, upstream"))
	equal(t, "the candidate", b.ref(lineage.CandidateRef("alpha")), "")

	// An export names the version the fork is based on now, not its tip's.
	file := b.exportPath("export.json")
	b.mustRun("export", file)
	for _, rec := range b.readExportFile(file)["skills"].([]any) {
		if r := rec.(map[string]any); r["name"] == "alpha" {
			equal(t, "the exported upstream commit", r["upstream_commit"], second)
			equal(t, "the exported subpath", r["subpath"], "skills/alpha")
		}
	}
}

// pullChildEnv names the fork TestPullChildProcess pulls.
const pullChildEnv = "AGENTX_TEST_PULL_CHILD"

// TestPullChildProcess is the pull a crash test kills: it runs agentx pull
// of the fork pullChildEnv names, with the environment it was given.
func TestPullChildProcess(t *testing.T) {
	name := os.Getenv(pullChildEnv)
	if name == "" {
		t.Skip("not the pull child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"pull", name}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// resetKilledScript lets the pull run to its last live write, the reset of
// the worktree's index that its journal's worktree step makes, and kills
// it the moment that is done.
const resetKilledScript = `
case " $* " in
*" reset "*)
	%GIT% "$@"
	status=$?
	kill -9 $PPID
	exit $status
	;;
esac
exec %GIT% "$@"
`

// TestPullRecoversWhereItWasKilled kills a pull that merges on machine b
// with SIGKILL at two boundaries of its own: once its journal is on disk,
// before anything changed, and right after its last live write. Every
// boundary in between is one of the journal's, which the home package
// replays without git. The journal moves the branch, replaces the skill
// directory and resets the worktree's index, and the next command
// recovers it: the branch at the merge of both machines' commits, the
// worktree holding it and clean, nothing left behind.
func TestPullRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, script string }{
		{"once the journal is on disk", killedUpdateScript},
		{"after its last live write", resetKilledScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, b, _, _ := twoHomes(t)
			theirs := a.commitFork("alpha", forkNotes("one", "one, a"))
			a.mustRun("publish", "alpha")
			mine := b.commitFork("alpha", forkNotes("eight", "eight, b"))
			out := killedChild(t, b, "TestPullChildProcess", pullChildEnv, "alpha", tc.script)
			_, kinds := journalKinds(t, b)
			equal(t, "the journal's steps", kinds, "ref, remove, publish, worktree")
			if got := b.run("skill", "list"); got.exit != 0 {
				t.Fatalf("the command after the killed pull: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, b), 0)
			equal(t, "the merge's parents", b.parents(b.ref(lineage.ForkRef("alpha"))), mine+"\n"+theirs)
			equal(t, "the notes", fileBody(t, filepath.Join(b.forkDir("alpha", "alpha"), "notes.md")), forkNotes("one", "one, a", "eight", "eight, b"))
			equal(t, "git status in the worktree", gitIn(t, b, filepath.Join(b.agentx, "worktrees", "alpha"), "status", "--porcelain"), "")
			equal(t, "what is left in the worktrees directory", strings.Join(hiddenEntries(t, filepath.Join(b.agentx, "worktrees")), " "), "")
		})
	}
}

// TestSameForkRefusal is when a remote branch is the same fork as the local
// branch of its name: by their fork ids alone.
func TestSameForkRefusal(t *testing.T) {
	t.Parallel()
	const one, two = "01234567-89ab-4def-8123-456789abcdef", "11234567-89ab-4def-8123-456789abcdef"
	rec := func(id string) lineage.Record {
		return lineage.Record{Name: "notes", Ref: lineage.ForkRef("notes"), Fork: &lineage.ForkLineage{ID: id}}
	}
	for _, tc := range []struct {
		name  string
		rec   lineage.Record
		there lineage.ForkLineage
		want  string // a word of the refusal, "" for none
	}{
		{"one id", rec(one), lineage.ForkLineage{ID: one}, ""},
		{"two ids", rec(two), lineage.ForkLineage{ID: one}, "is a different fork than notes"},
		{"none here", rec(""), lineage.ForkLineage{ID: one}, "records no fork id"},
		{"none there", rec(one), lineage.ForkLineage{}, "records no fork id"},
		{"history not read", lineage.Record{Name: "notes", Ref: lineage.ForkRef("notes")}, lineage.ForkLineage{ID: one}, "records no fork id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := sameForkRefusal(tc.rec, tc.there, "pulled")
			switch {
			case tc.want == "" && f != nil:
				t.Errorf("refused: %s", f.message)
			case tc.want != "" && f == nil:
				t.Errorf("not refused, want %q", tc.want)
			case f != nil:
				contains(t, "the refusal", f.message, tc.want)
				equal(t, "exit", f.status.exit, 6)
			}
		})
	}
}

// TestPullSummary is the result of a pull, by outcome.
func TestPullSummary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		names  []string
		counts map[string]int
		want   string
	}{
		{[]string{"notes"}, map[string]int{pullFastForward: 1}, "pulled notes: 1 fast-forward"},
		{[]string{"a", "b", "c"}, map[string]int{pullUpToDate: 1, pullMerged: 1, pullConflict: 1}, "pulled 3 forks: 1 merged, 1 up to date, 1 conflict"},
		{[]string{"a", "b"}, map[string]int{pullNoBranch: 2}, "pulled 2 forks: 2 no remote branch"},
	} {
		equal(t, "pullSummary", pullSummary(tc.names, tc.counts), tc.want)
	}
}
