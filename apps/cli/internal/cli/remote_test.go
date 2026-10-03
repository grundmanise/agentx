package cli

import (
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
	a.mustRun("skill", "publish")

	b = newHarness(t)
	b.build(t, fixture{dirs: []string{".claude"}})
	b.rewrite(s)
	b.withIdentity("Machine B", "b@example.com")
	b.setAccount(remote)
	return a, b, s, remote
}

// twoHomes is accountHomes with both forks installed on b from the account
// remote, which adds their source to b too.
func twoHomes(t *testing.T) (a, b *harness, s *sourceRepo, remote string) {
	t.Helper()
	a, b, s, remote = accountHomes(t)
	b.mustRun("skill", "add", "--from-account", "alpha")
	b.mustRun("skill", "add", "--from-account", "beta")
	return a, b, s, remote
}

// commitFork writes notes.md of the fork called name on h and commits it.
func (h *harness) commitFork(name, notes string) string {
	h.t.Helper()
	writeFile(h.t, filepath.Join(h.forkDir(name, name), "notes.md"), notes)
	h.mustRun("skill", "commit", name)
	return h.ref(lineage.ForkRef(name))
}

// TestPublishMergesFirstAndNamesUncommittedForks publishes from machine b
// while machine a published alpha first. With an uncommitted edit, b's
// publish of alpha would have to merge a's commit, and is refused, exit 6,
// nothing pushed, the refusal saying so once. Once the edit is undone,
// the publish merges a's commit first, as an update does, and pushes the
// merge; beta, committed on b and not named, stays unpushed. A publish
// with nothing to take in pushes alpha's commits even though alpha holds
// an uncommitted edit, and names it: publishing never commits. The push
// carries the fork branches alone: the remote holds no import branch,
// update candidate or other ref. A publish on a whose merge of b's beta
// conflicts leaves it pending, exit 4, and its hint names the update that
// completes it, since a publish never does; reported again by skill
// update, the merge is still the account remote's. A push the remote
// rejects is reported, exit 6, and never forced.
func TestPublishMergesFirstAndNamesUncommittedForks(t *testing.T) {
	t.Parallel()
	a, b, _, remote := twoHomes(t)
	alphaB := b.forkDir("alpha", "alpha")
	theirs := a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("skill", "publish", "alpha")
	mine := b.commitFork("alpha", forkNotes("eight", "eight, b"))
	betaBefore := remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/beta")
	b.commitFork("beta", forkNotes("two", "two, b"))

	committed := readText(t, filepath.Join(alphaB, "SKILL.md"))
	writeFile(t, filepath.Join(alphaB, "SKILL.md"), "uncommitted\n")
	out := b.run("--json", "skill", "publish", "alpha")
	equal(t, "exit", out.exit, 6)
	contains(t, "the error", b.one(out.stdout, "error")["message"].(string), "alpha has uncommitted edits, so it cannot be published")
	if strings.Contains(out.stderr, "which were not published") {
		t.Errorf("the refusal is warned about again: %s", out.stderr)
	}
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), theirs)

	writeFile(t, filepath.Join(alphaB, "SKILL.md"), committed)
	out = b.mustRun("--json", "skill", "publish", "alpha")
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
	equal(t, "progress", strings.Join(phases, ", "), "fetch file://"+remote+", publish alpha")
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), merged)
	equal(t, "the remote's beta", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/beta"), betaBefore)

	tip := b.commitFork("alpha", forkNotes("one", "one, a", "eight", "eight, b", "two", "two, b"))
	writeFile(t, filepath.Join(alphaB, "notes.md"), "uncommitted\n")
	out = b.run("skill", "publish", "alpha")
	equal(t, "exit", out.exit, 0)
	contains(t, "the line", out.stdout, "published alpha as "+short(tip))
	contains(t, "the warning", out.stderr, "alpha has uncommitted edits, which were not published")
	out = b.mustRun("--json", "skill", "publish")
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

	a.commitFork("beta", forkNotes("two", "two, a"))
	out = a.run("--json", "skill", "publish", "beta")
	equal(t, "a conflicting publish: exit", out.exit, 4)
	equal(t, "its outcome", a.one(out.stdout, "publish")["outcome"], publishConflict)
	contains(t, "its hint", a.one(out.stdout, "error")["hint"].(string), "then run 'agentx skill update beta' to complete it before publishing again, or ")
	out = a.run("--json", "skill", "update", "beta")
	equal(t, "the merge reported again: exit", out.exit, 4)
	contains(t, "its message", a.one(out.stdout, "error")["message"].(string), "beta conflicts with the account remote in 1 file")
	out = a.run("--json", "skill", "publish", "beta")
	equal(t, "a publish over the pending merge: exit", out.exit, 4)
	contains(t, "its hint", a.one(out.stdout, "error")["hint"].(string), "resolve it with git in "+filepath.Join(a.agentx, "merges", "beta", "beta")+" and run")

	writeShim(t, filepath.Join(remote, "hooks", "pre-receive"), "#!/bin/sh\nexit 1\n")
	b.commitFork("alpha", forkNotes("nine", "nine, b"))
	out = b.run("--json", "skill", "publish", "alpha")
	equal(t, "a rejected push: exit", out.exit, 6)
	equal(t, "its outcome", b.one(out.stdout, "publish")["outcome"], publishRejected)
	e := b.one(out.stdout, "error")
	contains(t, "its message", e["message"].(string), "the account remote rejected skills/alpha: ")
	contains(t, "its hint", e["hint"].(string), "run 'agentx skill update alpha' to take in what it holds, then publish again; agentx never forces a push")
	equal(t, "the remote's alpha, kept", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), tip)
}

// TestPublishRefusesADifferentFork creates a skill of the same name on two
// machines, which are two forks with two fork ids. The first publishes it;
// the second's publish of it is refused, exit 6, and the hint
// names the way out, a rename, and the remote keeps the first machine's
// branch; so is the second's removal of it with --remote, which would
// delete the first's. The publish still names the uncommitted edits the
// second machine's fork holds, which its refusal does not. Before the
// second machine exists, the first's check-updates, with no shared source,
// checks its own skill against the account remote and counts it as one
// source.
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
	writeFile(t, filepath.Join(b.forkDir("notes", "notes"), "draft.md"), "uncommitted\n")
	out = b.run("--json", "skill", "publish", "notes")
	equal(t, "publish: exit", out.exit, 6)
	e := b.one(out.stdout, "error")
	contains(t, "publish: message", e["message"].(string), "the account remote's skills/notes is a different fork than notes on this machine")
	contains(t, "publish: hint", e["hint"].(string), "rename yours with 'agentx skill rename notes <new>'")
	contains(t, "the uncommitted edits", out.stderr, "notes has uncommitted edits, which were not published")
	// Nor does a removal of b's notes, or the rename the hint above names,
	// delete a's from the account remote, or anything of b's; each names
	// what does its work on b alone, which for the rename is not a removal.
	remove(t, filepath.Join(b.forkDir("notes", "notes"), "draft.md"))
	for _, tc := range []struct{ cmd, hint string }{
		{"remove", "remove it from this machine alone with 'agentx skill remove notes'"},
		{"rename", "rename it on this machine alone with 'agentx skill rename notes jottings'"},
	} {
		args := []string{"--json", "skill", tc.cmd, "notes", "--remote"}
		if tc.cmd == "rename" {
			args = []string{"--json", "skill", "rename", "notes", "jottings", "--remote"}
		}
		out := b.run(args...)
		equal(t, tc.cmd+" --remote: exit", out.exit, 6)
		e := b.one(out.stdout, "error")
		contains(t, tc.cmd+" --remote: message", e["message"].(string), "the account remote's skills/notes is a different fork than notes on this machine, so notes cannot be removed from the account remote")
		equal(t, tc.cmd+" --remote: hint", e["hint"], tc.hint)
		if b.ref(lineage.ForkRef("notes")) == "" || !lexists(b.forkDir("notes", "notes")) || b.ref(lineage.ForkRef("jottings")) != "" {
			t.Errorf("a refused %s changed b's forks", tc.cmd)
		}
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
	fresh.setAccount(remote)
	ev = state(fresh)
	equal(t, "the fresh machine's notes", ev["state"], restorePresent)
	equal(t, "its kind", ev["local_kind"], lineage.KindFork)
	writeFile(t, filepath.Join(a.forkDir("notes", "notes"), "more.md"), "more\n")
	a.mustRun("skill", "commit", "notes")
	a.mustRun("skill", "publish", "notes")
	fresh.setAccount(remote)
	ev = state(fresh)
	equal(t, "the fresh machine's notes, published again", ev["state"], restoreDifferent)
	equal(t, "its commit", ev["local_commit"], a.ref(lineage.ForkRef("notes")))
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
// step with a pull event and the skill's library_skill event; updated
// again, it is up to date with the account remote, exit 0. Once both
// machines changed its line, and b added a file of its own, b's check lists
// only what a changed since the commit both share, and b's update leaves
// the merge pending, exit 4; resolved in its checkout, the merge is not
// completed by a publish, exit 4, whose hint names the update, and the next
// update completes it. With that merge and beta's not yet published, the
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
			equal(t, "the exported subpath", r["subpath"], "skills/alpha")
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
	b.mustRun("skill", "add", "--from-account", "notes")
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
	writeFile(t, filepath.Join(pendingCheckout(b, "notes"), "notes", "notes.md"), forkNotes("one", "one, a", "two", "two, a and b"))
	checkoutGit(t, b, "notes", "add", "notes/notes.md")
	out = b.run("--json", "skill", "publish", "notes")
	equal(t, "a publish of the resolved merge: exit", out.exit, 4)
	contains(t, "its hint", b.one(out.stdout, "error")["hint"].(string), "run 'agentx skill update notes' to complete it, then publish again")
	out = b.mustRun("--json", "skill", "update", "notes")
	contains(t, "the completion", b.one(out.stdout, "result")["summary"].(string), "updated notes with the merge you resolved, committed as ")
	equal(t, "its pull events", len(b.eventsOfType(out.stdout, "pull")), 0)
	equal(t, "the completed merge's parents", b.parents(b.ref(lineage.ForkRef("notes"))), mine+"\n"+theirs)
	equal(t, "b's notes", fileBody(t, filepath.Join(b.forkDir("notes", "notes"), "notes.md")), forkNotes("one", "one, a", "two", "two, a and b"))
	noCheckout(t, b, "notes")
	out = b.mustRun("skill", "update", "--all")
	contains(t, "the update of every skill with notes and beta ahead of the remote", out.stdout, "Nothing to update")
	equal(t, "its warnings", out.stderr, "")

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
	contains(t, "the warning with nothing from upstream", out.stderr, "so what your other machines published is not taken in")
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
		{"two ids", rec(two), lineage.ForkLineage{ID: one}, "is a different fork than notes"},
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
