package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
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

// accountRemoteName is the name of the account remote's git remote in h's
// account repo: the remote of its source, named after its id.
func accountRemoteName(t *testing.T, h *harness) string {
	t.Helper()
	settings, err := home.LoadSettings(h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := accountEntry(settings)
	if !ok {
		t.Fatal("no account remote is set")
	}
	return source.RemoteName(source.ID(entry.URL))
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

// accountHomes is two machines of one user with one account remote: a,
// with alpha and beta installed from a source at its first commit and
// forked, both published, and b, a machine with the remote set that has
// installed nothing and added no source. Each has a git identity of its
// own. It returns the source and the remote.
func accountHomes(t *testing.T) (a, b *harness, s *sourceRepo, remote string) {
	t.Helper()
	a, s, _ = forkUpdateHarness(t)
	a.withIdentity("Machine A", "a@example.com")
	a.mustRun("skill", "fork", "alpha")
	a.mustRun("skill", "fork", "beta")
	remote = newAccountRemote(t, a)
	a.setAccount(remote)
	pushOwn(a, remote, "alpha", "beta")

	b = newHarness(t)
	b.build(t, fixture{dirs: []string{".claude"}})
	b.rewrite(s)
	b.withIdentity("Machine B", "b@example.com")
	b.setAccount(remote)
	return a, b, s, remote
}

// pushOwn pushes the branches of h's own skills called names, which hold
// no edits, to the account remote at remote in one git push, as a publish
// of each by name would: a bare publish creates no branch, and one publish
// per skill would cost a fixture a run each.
func pushOwn(h *harness, remote string, names ...string) {
	h.t.Helper()
	args := []string{"push", "-q", source.RemoteName(source.ID("file://" + remote))}
	for _, n := range names {
		args = append(args, strings.TrimPrefix(lineage.ForkRef(n), "refs/heads/"))
	}
	h.accountGit(args...)
}

// twoHomes is accountHomes with both forks installed on b from the account
// remote, which adds their source to b too.
func twoHomes(t *testing.T) (a, b *harness, s *sourceRepo, remote string) {
	t.Helper()
	a, b, s, remote = accountHomes(t)
	b.mustRun("skill", "add", "--name", "alpha")
	b.mustRun("skill", "add", "--name", "beta")
	return a, b, s, remote
}

// commitFork writes notes.md of the fork called name on h and records it
// on the fork's branch, unpublished, see record.
func (h *harness) commitFork(name, notes string) string {
	h.t.Helper()
	writeFile(h.t, filepath.Join(h.forkDir(name, name), "notes.md"), notes)
	return h.record(name)
}

// TestPublishNeverMerges publishes from machine b while machine a
// published alpha first. b holds a commit of its own and an edit, so the
// two diverged: b's publish of alpha is refused, exit 6, outcome moved,
// its hint naming the update, with nothing recorded or written here or on
// the remote and no pull event, since a publish never merges. Once b's
// update has taken a's commit in, the publish with -m pushes the commit,
// the merge and a new edit as one commit on a's, under its message; beta,
// recorded on b and not named, stays unpushed until a bare publish, in
// which alpha is up to date. The push carries the fork branches alone: the
// remote holds no import branch, update candidate or other ref. a, which
// holds both skills unchanged since, is behind: its publish of every skill
// pushes nothing, exit 0, each skill's outcome behind and its warning
// naming the update, and so is its publish of alpha alone, which says so
// in its result; with an edit, a bare publish refuses a's alpha as moved,
// exit 6, naming it, while beta stays behind, and nothing is recorded. A
// push a hook of the remote's declines is reported, exit 6, with the
// hook's message and no update hint, and never forced.
func TestPublishNeverMerges(t *testing.T) {
	t.Parallel()
	a, b, _, remote := twoHomes(t)
	alphaB := b.forkDir("alpha", "alpha")
	theirs := a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("skill", "publish", "alpha")
	mine := b.commitFork("alpha", forkNotes("eight", "eight, b"))
	betaBefore := remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/beta")
	b.commitFork("beta", forkNotes("two", "two, b"))
	draft := filepath.Join(alphaB, "draft.md")
	writeFile(t, draft, "an edit\n")

	out := b.run("--json", "skill", "publish", "alpha")
	equal(t, "a publish over another machine's commit: exit", out.exit, 6)
	equal(t, "its outcome", b.one(out.stdout, "publish")["outcome"], publishMoved)
	e := b.one(out.stdout, "error")
	contains(t, "its message", e["message"].(string), "the account remote holds changes to alpha that this machine lacks, published from another machine")
	contains(t, "its hint", e["hint"].(string), "run 'agentx skill update alpha' to get them, then publish again")
	equal(t, "its pull events", len(b.eventsOfType(out.stdout, "pull")), 0)
	equal(t, "b's alpha, kept", b.ref(lineage.ForkRef("alpha")), mine)
	equal(t, "b's edit, kept", fileBody(t, draft), "an edit\n")
	noCheckout(t, b, "alpha")
	remove(t, draft)
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), theirs)

	b.mustRun("skill", "update", "alpha")
	merged := b.ref(lineage.ForkRef("alpha"))
	equal(t, "the merge's parents", b.parents(merged), mine+"\n"+theirs)
	// With -m, what b holds that the remote lacks, its commit, the merge
	// the update recorded and an edit, is published as one commit on the
	// remote's, under the message.
	writeFile(t, draft, "an edit\n")
	out = b.mustRun("--json", "skill", "publish", "alpha", "-m", "Eight, merged")
	ev := b.one(out.stdout, "publish")
	equal(t, "outcome", ev["outcome"], publishPushed)
	folded := b.ref(lineage.ForkRef("alpha"))
	equal(t, "commit", ev["commit"], folded)
	equal(t, "the fold's one parent", b.parents(folded), theirs)
	equal(t, "its message", b.accountGit("log", "-1", "--format=%s", folded), "Eight, merged")
	equal(t, "what it adds to the merge", b.accountGit("diff-tree", "-r", "--name-status", merged, folded), "A\talpha/draft.md")
	merged = folded
	var phases []string
	for _, e := range b.eventsOfType(out.stdout, "progress") {
		phases = append(phases, e["phase"].(string)+" "+e["subject"].(string))
	}
	equal(t, "progress", strings.Join(phases, ", "), "fetch file://"+remote+", publish alpha")
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), merged)
	equal(t, "the remote's beta", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/beta"), betaBefore)

	tip := b.commitFork("alpha", forkNotes("one", "one, a", "eight", "eight, b", "two", "two, b"))
	out = b.run("skill", "publish", "alpha")
	equal(t, "exit", out.exit, 0)
	contains(t, "the line", out.stdout, "published alpha as "+short(tip))
	out = b.mustRun("--json", "skill", "publish")
	equal(t, "summary", b.one(out.stdout, "result")["summary"], "published 1 of 2 skills")
	for _, e := range b.eventsOfType(out.stdout, "publish") {
		switch e["name"] {
		case "alpha":
			equal(t, "alpha", e["outcome"], publishUpToDate)
		case "beta":
			equal(t, "beta", e["outcome"], publishPushed)
		}
	}
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), tip)
	equal(t, "fork progress", len(b.eventsOfType(out.stdout, "progress")), 3)
	equal(t, "the remote's refs", remoteGit(t, b, remote, "for-each-ref", "--format=%(refname)"), "refs/heads/skills/alpha\nrefs/heads/skills/beta")

	refs := remoteGit(t, b, remote, "for-each-ref", "--format=%(refname) %(objectname)")
	out = a.run("--json", "skill", "publish")
	equal(t, "a publish of forks the remote is ahead of: exit", out.exit, 0)
	behind := a.eventsOfType(out.stdout, "publish")
	equal(t, "its publish events", len(behind), 2)
	for _, e := range behind {
		equal(t, e["name"].(string)+"'s outcome", e["outcome"], publishBehind)
	}
	contains(t, "alpha's warning", out.stderr, "alpha has nothing to publish; the account remote holds changes published from another machine; run 'agentx skill update alpha' to get them")
	equal(t, "the remote's refs, kept", remoteGit(t, b, remote, "for-each-ref", "--format=%(refname) %(objectname)"), refs)
	out = a.mustRun("--json", "skill", "publish", "alpha")
	contains(t, "the summary of one fork behind", a.one(out.stdout, "result")["summary"].(string), "alpha has nothing to publish")
	behindTip := a.ref(lineage.ForkRef("alpha"))
	writeFile(t, filepath.Join(a.forkDir("alpha", "alpha"), "draft.md"), "a's edit\n")
	out = a.run("--json", "skill", "publish")
	equal(t, "a bare publish with an edit behind the remote: exit", out.exit, 6)
	for _, e := range a.eventsOfType(out.stdout, "publish") {
		switch e["name"] {
		case "alpha":
			equal(t, "alpha, edited, outcome", e["outcome"], publishMoved)
		case "beta":
			equal(t, "beta's outcome", e["outcome"], publishBehind)
		}
	}
	contains(t, "alpha's refusal", out.stderr, "the account remote holds changes to alpha that this machine lacks")
	equal(t, "a's alpha, kept", a.ref(lineage.ForkRef("alpha")), behindTip)

	writeShim(t, filepath.Join(remote, "hooks", "pre-receive"), "#!/bin/sh\necho 'no pushes on Fridays' >&2\nexit 1\n")
	b.commitFork("alpha", forkNotes("nine", "nine, b"))
	out = b.run("--json", "skill", "publish", "alpha")
	equal(t, "a declined push: exit", out.exit, 6)
	equal(t, "its outcome", b.one(out.stdout, "publish")["outcome"], publishDeclined)
	e = b.one(out.stdout, "error")
	equal(t, "its message", e["message"], "the account remote declined skills/alpha: pre-receive hook declined: no pushes on Fridays")
	contains(t, "its hint", e["hint"].(string), "agentx never forces a push")
	if strings.Contains(e["hint"].(string), "skill update") {
		t.Errorf("a declined push's hint names the update: %q", e["hint"])
	}
	equal(t, "the remote's alpha, kept", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), tip)
}

// TestPublishRefusesADifferentFork creates a skill of the same name on two
// machines, which are two forks with two fork ids. The first publishes it;
// the second's publish of it is refused, exit 6, and the hint names the
// way out, a rename, and the remote keeps the first machine's branch; so
// is the second's removal of it with --remote, which would delete the
// first's. The refused publish records nothing of the edit the second
// machine's skill holds. Renamed and published, the second's skill never
// deletes the first's branch. Before the second machine exists, the first's
// check-updates, with no shared source, checks its own skill against the
// account remote and counts it as one source.
func TestPublishRefusesADifferentFork(t *testing.T) {
	t.Parallel()
	a, _, _, _ := forkHarness(t)
	remote := newAccountRemote(t, a)
	a.setAccount(remote)
	out := a.mustRun("--json", "skill", "publish", "notes")
	equal(t, "outcome", a.one(out.stdout, "publish")["outcome"], publishPushed)
	equal(t, "summary", a.one(out.stdout, "result")["summary"], "published notes")
	out = a.mustRun("--json", "skill", "check-updates")
	equal(t, "the check of a machine with only its own skills", a.one(out.stdout, "result")["summary"], "checked 1 skill from 1 source: no update available")

	b, _, _, _ := forkHarness(t)
	b.setAccount(remote)
	bTip := b.ref(lineage.ForkRef("notes"))
	writeFile(t, filepath.Join(b.forkDir("notes", "notes"), "draft.md"), "an edit\n")
	out = b.run("--json", "skill", "publish", "notes")
	equal(t, "publish: exit", out.exit, 6)
	e := b.one(out.stdout, "error")
	contains(t, "publish: message", e["message"].(string), "the account remote's skills/notes is a different skill than notes on this machine")
	contains(t, "publish: hint", e["hint"].(string), "rename yours with 'agentx skill rename notes <new>'")
	equal(t, "b's notes, nothing recorded", b.ref(lineage.ForkRef("notes")), bTip)
	// Nor does a removal of b's notes with --remote delete a's from the
	// account remote, or anything of b's; it names what does its work on b
	// alone.
	remove(t, filepath.Join(b.forkDir("notes", "notes"), "draft.md"))
	out = b.run("--json", "skill", "remove", "notes", "--remote")
	equal(t, "remove --remote: exit", out.exit, 6)
	e = b.one(out.stdout, "error")
	contains(t, "remove --remote: message", e["message"].(string), "the account remote's skills/notes is a different skill than notes on this machine, so notes cannot be removed from the account remote")
	equal(t, "remove --remote: hint", e["hint"], "remove it from this machine alone with 'agentx skill remove notes'")
	if b.ref(lineage.ForkRef("notes")) == "" || !lexists(b.forkDir("notes", "notes")) {
		t.Error("a refused removal changed b's notes")
	}
	// An export of a lists notes as a managed skill whose source is the
	// account remote, and a fresh machine that set the account remote holds
	// a's under skills/, present. Which state each mix of local and remote
	// branches gives is TestRestoreStates'.
	file := a.exportPath("export.json")
	a.mustRun("export", file)
	fresh := newHarness(t)
	fresh.setAccount(remote)
	ev := fresh.one(fresh.mustRun("--json", "import", file, "--yes").stdout, "import_skill")
	equal(t, "the fresh machine's notes", ev["state"], restorePresent)
	equal(t, "its kind", ev["kind"], lineage.KindManaged)
	equal(t, "its source", ev["source"], "file://"+remote)

	// The way out the hint names: b renames its notes and publishes that
	// one, which never deletes a's notes, a different skill by its fork id.
	b.mustRun("skill", "rename", "notes", "jottings")
	b.mustRun("skill", "publish", "jottings")
	equal(t, "the remote's jottings", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/jottings"), b.ref(lineage.ForkRef("jottings")))
	equal(t, "the remote's notes", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/notes"), a.ref(lineage.ForkRef("notes")))
}

// sourceEntryOf is the settings entry of the source at url, as the file
// holds it.
func sourceEntryOf(t *testing.T, h *harness, url string) map[string]any {
	t.Helper()
	sources, _ := readSettingsFile(t, h)["sources"].([]any)
	for _, s := range sources {
		if entry := s.(map[string]any); entry["url"] == url {
			return entry
		}
	}
	t.Fatalf("no settings entry for %s in %v", url, sources)
	return nil
}

// TestSkillUpdateOfAForkPullsThenMerges updates on machine b a fork that
// machine a published a commit of and whose upstream has a new version: the
// update takes in a's commit first, as a merge of its own with b's commit,
// then merges the upstream version on top of it, two commits on b's branch.
//
// Then b's check pins a third version for both forks, and a takes a fourth
// into alpha and publishes it. b's update of every fork fast-forwards alpha
// to a's commit, whose base is newer than the candidate b pinned: the
// candidate goes with the account step, so the update never merges the older
// version back over the newer one, and alpha is left as a published it
// while beta, which the remote has nothing new for, takes its own update.
//
// A skill a made with skill new, which has no upstream, is what a publishes
// next. b's check names it published from another machine, with the file a
// changed; b's update of every skill alone then takes it in, its account
// step with a pull event and the skill's library_skill event. The update
// of it alone brings it to the next version a publishes, saying so in its
// summary; updated again, it is up to date with the account remote, exit 0. Once both
// machines changed its line, and b added a file of its own, b's check lists
// only what a changed since the commit both share, and b's update leaves
// the merge pending, exit 4, reported again as the account remote's while
// unresolved; resolved in its checkout, with an edit made meanwhile in its
// worktree, the next update records the edit and completes the merge over
// it, keeping both. With that merge and beta's not yet published, the
// update of every skill has nothing to do and says nothing else. Last, with
// the account remote out of reach, the update of beta only warns, while
// that of the skill with no upstream fails, exit 3, alone or with every
// skill, and so does the check, as for a source it cannot reach, naming the
// account remote.
func TestSkillUpdateOfAForkPullsThenMerges(t *testing.T) {
	t.Parallel()
	a, b, s, remote := twoHomes(t)
	theirs := a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("skill", "publish", "alpha")
	mine := b.commitFork("alpha", forkNotes("three", "three, b"))
	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, upstream"))
	second := s.commit("second version")
	out := b.mustRun("skill", "check-updates")
	contains(t, "the check's line for what a published", out.stdout, "alpha  published from another machine")
	contains(t, "its hint, with an upstream update listed too", out.stdout, "agentx skill diff <name> --update")
	candidate := b.ref(lineage.CandidateRef("alpha"))

	out = b.mustRun("--json", "skill", "update", "alpha")
	equal(t, "pull outcome", b.one(out.stdout, "pull")["outcome"], pullMerged)
	tip := b.ref(lineage.ForkRef("alpha"))
	account := b.accountGit("rev-parse", tip+"^1")
	equal(t, "the update's parents", b.parents(tip), account+"\n"+candidate)
	equal(t, "the account merge's parents", b.parents(account), mine+"\n"+theirs)
	equal(t, "its subject", b.accountGit("log", "-1", "--format=%s", account), "alpha: merge the account remote (test-host)")
	equal(t, "its author", b.accountGit("log", "-1", "--format=%an <%ae>", account), "Machine B <b@example.com>")
	equal(t, "the base", b.trailer(tip, lineage.TrailerBase), candidate)
	equal(t, "b's notes", fileBody(t, filepath.Join(b.forkDir("alpha", "alpha"), "notes.md")), forkNotes("one", "one, a", "three", "three, b", "seven", "seven, upstream"))
	equal(t, "the candidate", b.ref(lineage.CandidateRef("alpha")), "")

	// An export names the version the fork is based on now, not its tip's.
	file := b.exportPath("export.json")
	b.mustRun("export", file)
	for _, rec := range b.readExportFile(file)["skills"].([]any) {
		if r := rec.(map[string]any); r["name"] == "alpha" {
			equal(t, "the exported upstream commit", r["upstream_commit"], second)
			equal(t, "the exported upstream subpath", r["upstream_subpath"], "skills/alpha")
		}
	}

	b.mustRun("skill", "publish", "alpha")
	a.mustRun("skill", "update", "alpha")
	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, third"))
	s.write("skills/beta/notes.md", forkNotes("six", "six, third"))
	s.commit("third version")
	b.mustRun("skill", "check-updates")
	betaCandidate := b.ref(lineage.CandidateRef("beta"))
	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, fourth", "eight", "eight, fourth"))
	s.commit("fourth version")
	a.mustRun("skill", "check-updates")
	a.mustRun("skill", "update", "alpha")
	a.mustRun("skill", "publish", "alpha")
	published := a.ref(lineage.ForkRef("alpha"))

	out = b.mustRun("--json", "skill", "update", "--all")
	equal(t, "pull outcome", b.one(out.stdout, "pull")["outcome"], pullFastForward)
	equal(t, "alpha's tip", b.ref(lineage.ForkRef("alpha")), published)
	equal(t, "alpha's candidate", b.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "alpha's notes", fileBody(t, filepath.Join(b.forkDir("alpha", "alpha"), "notes.md")),
		forkNotes("one", "one, a", "three", "three, b", "seven", "seven, fourth", "eight", "eight, fourth"))
	equal(t, "beta's base", b.trailer(b.ref(lineage.ForkRef("beta")), lineage.TrailerBase), betaCandidate)
	equal(t, "beta's notes", fileBody(t, filepath.Join(b.forkDir("beta", "beta"), "notes.md")), forkNotes("six", "six, third"))

	a.mustRun("skill", "new", "notes")
	out = a.mustRun("skill", "update", "notes")
	contains(t, "the update of a skill never published", out.stdout, "notes has no branch on the account remote")
	a.mustRun("skill", "publish", "notes")
	b.mustRun("skill", "add", "--name", "notes")
	local := b.ref(lineage.ForkRef("notes"))
	published = a.commitFork("notes", forkNotes("one", "one, a"))
	a.mustRun("skill", "publish", "notes")
	backdate(t, b)
	out = b.mustRun("--json", "skill", "check-updates")
	equal(t, "the check's summary, alpha and beta counted once", b.one(out.stdout, "result")["summary"], "checked 3 skills from 2 sources: 1 update available")
	progress := b.eventsOfType(out.stdout, "progress")
	equal(t, "its last progress event", fmt.Sprint(progress[len(progress)-1]["current"], "/", progress[len(progress)-1]["total"]), "2/2")
	update := b.one(out.stdout, "update_available")
	equal(t, "the check's update", update["name"], "notes")
	equal(t, "its source", update["source"], "file://"+remote)
	equal(t, "its candidate", update["candidate"], published)
	equal(t, "its upstream commit", update["upstream_commit"], "")
	equal(t, "its kind", update["kind"], lineage.KindManaged)
	if _, ok := update["upstream"]; ok {
		t.Errorf("the update of what another machine published names an upstream: %v", update["upstream"])
	}
	equal(t, "its files", fmt.Sprint(update["files"]), "[map[path:notes.md status:added]]")
	if lastFetched(t, b, "file://"+remote) == backdated {
		t.Error("the check left the account remote's last_fetched as it was")
	}
	out = b.mustRun("skill", "check-updates")
	contains(t, "the check's line", out.stdout, "notes  published from another machine  "+short(local)+" -> "+short(published)+"  1 file")
	if strings.Contains(out.stdout, "--update") {
		t.Errorf("the check of what another machine published sends to skill diff --update:\n%s", out.stdout)
	}
	out = b.mustRun("--json", "skill", "update", "--all")
	pulled := b.one(out.stdout, "pull")
	equal(t, "the account step of notes", pulled["name"], "notes")
	equal(t, "its outcome", pulled["outcome"], pullFastForward)
	equal(t, "its commit", pulled["commit"], published)
	b.librarySkill(out.stdout, "notes")
	equal(t, "notes' tip", b.ref(lineage.ForkRef("notes")), published)
	writeFile(t, filepath.Join(a.forkDir("notes", "notes"), "steps.md"), "the steps\n")
	a.mustRun("skill", "publish", "notes", "-m", "Add the steps")
	out = b.mustRun("--json", "skill", "update", "notes")
	contains(t, "the update of notes alone", b.one(out.stdout, "result")["summary"].(string), "updated notes to the latest published version; it has no upstream to update from")
	out = b.mustRun("skill", "update", "notes")
	contains(t, "the update with nothing new", out.stdout, "notes is up to date with the account remote")

	theirs = a.commitFork("notes", forkNotes("one", "one, a", "two", "two, a"))
	a.mustRun("skill", "publish", "notes")
	writeFile(t, filepath.Join(b.forkDir("notes", "notes"), "mine.md"), "b's own\n")
	mine = b.commitFork("notes", forkNotes("one", "one, a", "two", "two, b"))
	out = b.mustRun("--json", "skill", "check-updates")
	equal(t, "the files a changed since the commit both share", fmt.Sprint(b.updateOf(out.stdout, "notes")["files"]), "[map[path:notes.md status:modified]]")
	out = b.run("--json", "skill", "update", "--all")
	equal(t, "the update of both machines' line: exit", out.exit, 4)
	equal(t, "its pull outcome", b.one(out.stdout, "pull")["outcome"], pullConflict)
	equal(t, "its warnings, with beta ahead of the remote and left out", out.stderr, "")
	out = b.run("--json", "skill", "update", "notes")
	equal(t, "the merge reported again: exit", out.exit, 4)
	contains(t, "its message, still the account remote's", b.one(out.stdout, "error")["message"].(string), "notes conflicts with the account remote in 1 file")
	writeFile(t, filepath.Join(pendingCheckout(b, "notes"), "notes", "notes.md"), forkNotes("one", "one, a", "two", "two, a and b"))
	checkoutGit(t, b, "notes", "add", "notes/notes.md")
	writeFile(t, filepath.Join(b.forkDir("notes", "notes"), "later.md"), "edited while the merge was pending\n")
	out = b.mustRun("--json", "skill", "update", "notes")
	contains(t, "the completion", b.one(out.stdout, "result")["summary"].(string), "updated notes with the merge you resolved, committed as ")
	equal(t, "its pull events", len(b.eventsOfType(out.stdout, "pull")), 0)
	tip = b.ref(lineage.ForkRef("notes"))
	recorded, completed := b.accountGit("rev-parse", tip+"^1"), b.accountGit("rev-parse", tip+"^2")
	equal(t, "the edit made while the merge was pending, recorded on the tip", b.parents(recorded), mine)
	equal(t, "the completed merge's parents", b.parents(completed), mine+"\n"+theirs)
	equal(t, "b's notes", fileBody(t, filepath.Join(b.forkDir("notes", "notes"), "notes.md")), forkNotes("one", "one, a", "two", "two, a and b"))
	equal(t, "the edit, kept", b.accountGit("show", tip+":notes/later.md"), "edited while the merge was pending")
	noCheckout(t, b, "notes")
	// An update with nothing to take in records no edit.
	idle := filepath.Join(b.forkDir("notes", "notes"), "idle.md")
	writeFile(t, idle, "an edit no update takes in\n")
	out = b.mustRun("skill", "update", "--all")
	contains(t, "the update of every skill with notes and beta ahead of the remote", out.stdout, "Nothing to update")
	equal(t, "its warnings", out.stderr, "")
	equal(t, "notes' tip, with an edit and nothing to take in", b.ref(lineage.ForkRef("notes")), tip)
	if err := os.Remove(idle); err != nil {
		t.Fatal(err)
	}

	// An account remote git cannot reach leaves the upstream's version to
	// take in, with a warning.
	s.write("skills/beta/notes.md", forkNotes("six", "six, fifth"))
	s.commit("fifth version")
	b.mustRun("skill", "check-updates")
	before := b.ref(lineage.ForkRef("beta"))
	if err := os.Rename(remote, remote+".gone"); err != nil {
		t.Fatal(err)
	}
	out = b.mustRun("skill", "update", "beta")
	contains(t, "the warning", out.stderr, "so beta is updated from upstream only")
	equal(t, "beta's first parent", b.accountGit("rev-parse", b.ref(lineage.ForkRef("beta"))+"^1"), before)
	equal(t, "beta's notes after the upstream-only update", fileBody(t, filepath.Join(b.forkDir("beta", "beta"), "notes.md")), forkNotes("six", "six, fifth"))
	out = b.mustRun("skill", "update", "beta")
	contains(t, "the warning with nothing from upstream", out.stderr, "so nothing was updated to the latest published version")
	out = b.run("--json", "skill", "update", "notes")
	equal(t, "the update of a skill with no upstream: exit", out.exit, 3)
	contains(t, "its error", b.one(out.stdout, "error")["message"].(string), "cannot reach the account remote")
	out = b.run("skill", "update", "--all")
	equal(t, "the update of every skill with no remote: exit", out.exit, 3)
	out = b.run("--json", "skill", "check-updates")
	equal(t, "the check with no remote: exit", out.exit, 3)
	e := b.one(out.stdout, "error")
	equal(t, "its code", e["code"], "source")
	contains(t, "its message", e["message"].(string), "could not check the account remote file://"+remote+": cannot reach it: ")
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
		{"two ids", rec(two), lineage.ForkLineage{ID: one}, "is a different skill than notes"},
		{"none here", rec(""), lineage.ForkLineage{ID: one}, "records no fork id"},
		{"none there", rec(one), lineage.ForkLineage{}, "records no fork id"},
		{"history not read", lineage.Record{Name: "notes", Ref: lineage.ForkRef("notes")}, lineage.ForkLineage{ID: one}, "records no fork id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := sameForkRefusal(tc.rec, tc.there, "updated")
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

// TestReportSync is the line the account step of an update prints for a
// fork it moved: the subject of the remote tip alone, sanitised, for a
// fast-forward, and no commit for a merge, which is not published yet.
func TestReportSync(t *testing.T) {
	t.Parallel()
	const commit = "4c2d1e9a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e"
	moved := &updating{fork: &forkUpdate{commit: commit}}
	for _, tc := range []struct {
		name string
		s    forkSync
		want string
	}{
		{"fast-forward", forkSync{name: "notes", outcome: pullFastForward, u: moved, subject: "Tighten\tthe summary steps"},
			"✓ updated notes to the latest published version: \"Tighten the summary steps\"\n"},
		{"merged", forkSync{name: "notes", outcome: pullMerged, u: moved, subject: "Tighten the summary steps"},
			"✓ merged the latest published version of notes with your local edits\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			inv := &invocation{out: &writer{stdout: &stdout, stderr: &stderr, env: map[string]string{}}}
			inv.reportSync(tc.s)
			equal(t, "the line", stdout.String(), tc.want)
		})
	}
}
