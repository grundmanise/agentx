package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The tests in this file are the acceptance suite of forks synced between
// two machines of one user through the account remote. Each one is a
// scenario on two homes, a and b, that share the remote and the upstream
// source, and reads the outcome back with plain git: the branches, their
// merge commits and trailers, the worktrees and the checkouts of pending
// merges. What each sync gives is what git merge gives; the tests pin that
// nothing either machine committed is ever lost on the way, either merged
// or in a conflict the user resolves.

// scenarioMachines is accountHomes with a second machine that differs from
// the first in what most often changes a commit: another time zone, and a
// git configuration with line ending conversion, signing and hooks of its
// own, which agentx keeps out of every commit it writes. b has installed
// nothing and added no source.
func scenarioMachines(t *testing.T) (a, b *harness, s *sourceRepo, remote string) {
	t.Helper()
	a, b, s, remote = accountHomes(t)
	b.env["TZ"] = "Pacific/Chatham"
	spoilTheGitConfig(t, b)
	b.withIdentity("Machine B", "b@example.com")
	return a, b, s, remote
}

// scenarioHomes is scenarioMachines with the forks called names installed
// on b from the account remote alone, which adds their source to b, so
// every import commit b writes of a version it takes is its own, and has to
// come out as the one a writes of that version for the two to merge as one
// history.
func scenarioHomes(t *testing.T, names ...string) (a, b *harness, s *sourceRepo, remote string) {
	t.Helper()
	a, b, s, remote = scenarioMachines(t)
	for _, name := range names {
		b.mustRun("skill", "add", "--from-account", name)
	}
	return a, b, s, remote
}

// takeVersion has h check for updates and take the version its source
// holds now into every fork of it.
func takeVersion(h *harness) {
	h.t.Helper()
	h.mustRun("skill", "check-updates")
	h.mustRun("skill", "update", "--all")
}

// mergedImport is the import commit the tip of the fork called name on h
// merged, its second parent: the version the fork's last update took.
func mergedImport(h *harness, name string) string {
	h.t.Helper()
	return h.accountGit("rev-parse", lineage.ForkRef(name)+"^2")
}

// forkIDOf is the fork id the listing of h gives the fork called name.
func forkIDOf(h *harness, name string) string {
	h.t.Helper()
	id, _ := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, name)["fork_id"].(string)
	return id
}

// worktreeStatus is git status of the worktree of the fork called name on h.
func worktreeStatus(h *harness, name string) string {
	h.t.Helper()
	return gitIn(h.t, h, filepath.Join(h.agentx, "worktrees", name), "status", "--porcelain")
}

// keptInConflict fails the test unless the checkout of the merge pending
// for the fork called name on h holds, for path in the fork's directory
// dir, the blob mine holds as its own stage and the blob theirs holds as
// the other's: what each side committed is in the conflict, whole.
func keptInConflict(t *testing.T, h *harness, name, dir, path, mine, theirs string) {
	t.Helper()
	stages := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(checkoutGit(t, h, name, "ls-files", "-u", "--format=%(stage) %(objectname)", "--", dir+"/"+path)), "\n") {
		if stage, blob, ok := strings.Cut(line, " "); ok {
			stages[stage] = blob
		}
	}
	equal(t, path+": mine", stages["2"], h.accountGit("rev-parse", mine+":"+dir+"/"+path))
	equal(t, path+": theirs", stages["3"], h.accountGit("rev-parse", theirs+":"+dir+"/"+path))
}

// noMarkers fails the test when the file at path holds a conflict marker.
func noMarkers(t *testing.T, what, path string) {
	t.Helper()
	if body := fileBody(t, path); strings.Contains(body, "<<<<<<<") || strings.Contains(body, ">>>>>>>") {
		t.Errorf("%s holds conflict markers:\n%s", what, body)
	}
}

// besideConflicts reads a file git merged with conflicts: what is outside
// every conflict, with one empty line where each conflict is, and each
// conflict's sides in the order git writes them, mine, the base and
// theirs, joined by " / ", one conflict per line.
func besideConflicts(body string) (merged, conflicts string) {
	var out, blocks, block []string
	in := false
	for _, line := range strings.SplitAfter(body, "\n") {
		text := strings.TrimSuffix(line, "\n")
		switch {
		case !in && strings.HasPrefix(text, "<<<<<<<"):
			in, block = true, nil
			out = append(out, "\n")
		case in && strings.HasPrefix(text, ">>>>>>>"):
			in = false
			blocks = append(blocks, strings.Join(block, " / "))
		case in && (strings.HasPrefix(text, "|||||||") || strings.HasPrefix(text, "=======") && strings.Trim(text, "=") == ""):
		case in:
			block = append(block, text)
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, ""), strings.Join(blocks, "\n")
}

// TestScenarioDisjointEditsMergeClean syncs one fork a published and b
// installed from the account remote, under the fork id a gave it. Each
// machine edits other lines and commits. An uncommitted edit on b refuses
// the update, exit 6, naming the fork, and changes nothing on either
// machine or the remote; once b commits it, the same update merges clean, b
// publishes the merge and a's update fast-forwards to it, so both machines
// hold every line either edited. The publish that an uncommitted edit
// refuses when the remote is ahead is
// TestPublishMergesFirstAndNamesUncommittedForks.
func TestScenarioDisjointEditsMergeClean(t *testing.T) {
	t.Parallel()
	a, b, _, remote := scenarioHomes(t, "alpha")
	equal(t, "b's fork id", forkIDOf(b, "alpha"), forkIDOf(a, "alpha"))
	alphaB := b.forkDir("alpha", "alpha")

	theirs := a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("skill", "publish", "alpha")
	mine := b.commitFork("alpha", forkNotes("eight", "eight, b"))
	writeFile(t, filepath.Join(alphaB, "draft.md"), "a draft\n")
	out := b.run("--json", "skill", "update", "alpha")
	equal(t, "the update with an uncommitted edit: exit", out.exit, 6)
	contains(t, "its message", b.one(out.stdout, "error")["message"].(string), "alpha has uncommitted edits, so it cannot be updated")
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), mine)
	equal(t, "a's alpha", a.ref(lineage.ForkRef("alpha")), theirs)
	equal(t, "the remote's alpha", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/alpha"), theirs)
	equal(t, "b's notes", fileBody(t, filepath.Join(alphaB, "notes.md")), forkNotes("eight", "eight, b"))
	equal(t, "b's draft", fileBody(t, filepath.Join(alphaB, "draft.md")), "a draft\n")
	noCheckout(t, b, "alpha")

	b.mustRun("skill", "commit", "alpha")
	committed := b.ref(lineage.ForkRef("alpha"))
	out = b.mustRun("--json", "skill", "update", "alpha")
	equal(t, "the pull", b.one(out.stdout, "pull")["outcome"], pullMerged)
	merged := b.ref(lineage.ForkRef("alpha"))
	equal(t, "the merge's parents", b.parents(merged), committed+"\n"+theirs)
	b.mustRun("skill", "publish", "alpha")
	out = a.mustRun("--json", "skill", "update", "alpha")
	equal(t, "a's pull", a.one(out.stdout, "pull")["outcome"], pullFastForward)
	equal(t, "a's alpha", a.ref(lineage.ForkRef("alpha")), merged)
	for _, h := range []*harness{a, b} {
		dir := h.forkDir("alpha", "alpha")
		equal(t, "the notes", fileBody(t, filepath.Join(dir, "notes.md")), forkNotes("one", "one, a", "eight", "eight, b"))
		equal(t, "the draft", fileBody(t, filepath.Join(dir, "draft.md")), "a draft\n")
		equal(t, "git status", worktreeStatus(h, "alpha"), "")
	}
}

// TestScenarioSameVersionMergesClean has both machines take the source's
// second version into both forks. The import commit each writes of it is
// the same commit, though the machines differ in time zone and git
// configuration. alpha, which neither machine edited, merges clean: the
// merge holds exactly the version and names its import as the base. beta,
// which each machine edited on one line of its own and on one line both
// edited, conflicts in that one line alone, the version's change and each
// machine's own line merged around it, each side's blob whole in the
// conflict, and no marker in either machine's worktree. Giving the merge
// up leaves both machines' branches and worktrees as they were.
func TestScenarioSameVersionMergesClean(t *testing.T) {
	t.Parallel()
	a, b, s, remote := scenarioHomes(t, "alpha", "beta")
	a.commitFork("beta", forkNotes("one", "one, a", "six", "six, a"))
	b.commitFork("beta", forkNotes("one", "one, b", "eight", "eight, b"))
	s.write("skills/alpha/notes.md", forkNotes("four", "four, v2"))
	s.write("skills/beta/notes.md", forkNotes("four", "four, v2"))
	s.commit("second version")
	takeVersion(a)
	takeVersion(b)
	for _, name := range []string{"alpha", "beta"} {
		equal(t, name+": b's import of the version", mergedImport(b, name), mergedImport(a, name))
	}
	importTwo := mergedImport(a, "alpha")
	a.mustRun("skill", "publish")

	out := b.mustRun("--json", "skill", "update", "alpha")
	equal(t, "alpha's pull", b.one(out.stdout, "pull")["outcome"], pullMerged)
	merged := b.ref(lineage.ForkRef("alpha"))
	equal(t, "alpha's base", b.trailer(merged, lineage.TrailerBase), importTwo)
	equal(t, "the base event", b.one(out.stdout, "pull")["base"], importTwo)
	sameTree(t, "alpha", libraryTree(t, b.forkDir("alpha", "alpha")), libraryTree(t, filepath.Join(s.work, "skills", "alpha")))
	equal(t, "alpha's status", worktreeStatus(b, "alpha"), "")

	mineB, theirsA := b.ref(lineage.ForkRef("beta")), a.ref(lineage.ForkRef("beta"))
	out = b.run("--json", "skill", "publish", "beta")
	equal(t, "beta's publish: exit", out.exit, 4)
	conflict := b.one(out.stdout, "conflict")
	equal(t, "the conflicted file", conflictPaths(conflict), "notes.md")
	keptInConflict(t, b, "beta", "beta", "notes.md", mineB, theirsA)
	around, conflicts := besideConflicts(fileBody(t, filepath.Join(pendingCheckout(b, "beta"), "beta", "notes.md")))
	equal(t, "the merge around the conflict", around, forkNotes("one", "", "four", "four, v2", "six", "six, a", "eight", "eight, b"))
	equal(t, "the conflict", conflicts, "one, b / one / one, a")
	equal(t, "the remote's beta", remoteGit(t, b, remote, "rev-parse", "refs/heads/skills/beta"), theirsA)
	for _, h := range []*harness{a, b} {
		noMarkers(t, "beta's worktree", filepath.Join(h.forkDir("beta", "beta"), "notes.md"))
	}

	b.mustRun("skill", "update", "beta", "--abort")
	noCheckout(t, b, "beta")
	equal(t, "b's beta", b.ref(lineage.ForkRef("beta")), mineB)
	equal(t, "a's beta", a.ref(lineage.ForkRef("beta")), theirsA)
	equal(t, "b's notes", fileBody(t, filepath.Join(b.forkDir("beta", "beta"), "notes.md")), forkNotes("one", "one, b", "four", "four, v2", "eight", "eight, b"))
	equal(t, "a's notes", fileBody(t, filepath.Join(a.forkDir("beta", "beta"), "notes.md")), forkNotes("one", "one, a", "four", "four, v2", "six", "six, a"))
	equal(t, "b's status", worktreeStatus(b, "beta"), "")
	equal(t, "a's status", worktreeStatus(a, "beta"), "")
}

// TestScenarioARevertIsNeverUndone has both machines take the source's
// second version into alpha; then a puts back one line the version changed
// and b edits another, away from the version's changes. The two histories
// share the version's import commit, which has no ancestor in common with
// the commit they last shared, so git merges them over both, and b's update
// does not take a's line back to the version's text: it conflicts, a's
// put-back line in the conflict and a's side whole in the checkout, and b's
// worktree and branch stay as they were. Resolved with a's line kept, the
// next update completes the merge.
//
// Over the same two merge bases, an edit is enough, and not only to the
// version's lines: in beta, a edits again a line of the fork's own that
// both machines held before they took the version, b edits nothing, and
// b's update of beta conflicts all the same, a's side whole in the checkout.
func TestScenarioARevertIsNeverUndone(t *testing.T) {
	t.Parallel()
	a, b, s, _ := scenarioHomes(t, "alpha", "beta")
	a.commitFork("beta", forkNotes("one", "one, x"))
	a.mustRun("skill", "publish", "beta")
	b.mustRun("skill", "update", "beta")
	s.write("skills/alpha/notes.md", forkNotes("two", "two, v2", "seven", "seven, v2"))
	s.write("skills/beta/notes.md", forkNotes("seven", "seven, v2"))
	s.commit("second version")
	takeVersion(a)
	takeVersion(b)
	a.commitFork("alpha", forkNotes("seven", "seven, v2"))
	a.commitFork("beta", forkNotes("one", "one, x and more", "seven", "seven, v2"))
	a.mustRun("skill", "publish")
	b.commitFork("alpha", forkNotes("two", "two, v2", "four", "four, b", "seven", "seven, v2"))

	mine, theirs := b.ref(lineage.ForkRef("alpha")), a.ref(lineage.ForkRef("alpha"))
	out := b.run("--json", "skill", "update", "alpha")
	equal(t, "the update: exit", out.exit, 4)
	equal(t, "the conflicted file", conflictPaths(b.one(out.stdout, "conflict")), "notes.md")
	keptInConflict(t, b, "alpha", "alpha", "notes.md", mine, theirs)
	equal(t, "a's side", b.accountGit("show", theirs+":alpha/notes.md"), strings.TrimSuffix(forkNotes("seven", "seven, v2"), "\n"))
	// a's side closes the conflict, its put-back line first; the merge
	// base git writes between the sides holds those lines too, but never
	// right before a closing marker.
	contains(t, "the conflict", fileBody(t, filepath.Join(pendingCheckout(b, "alpha"), "alpha", "notes.md")), "\ntwo\nthree\nfour\n>>>>>>>")
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), mine)
	noMarkers(t, "b's worktree", filepath.Join(b.forkDir("alpha", "alpha"), "notes.md"))

	writeFile(t, filepath.Join(pendingCheckout(b, "alpha"), "alpha", "notes.md"), forkNotes("four", "four, b", "seven", "seven, v2"))
	checkoutGit(t, b, "alpha", "add", "alpha/notes.md")
	out = b.mustRun("--json", "skill", "update", "alpha")
	contains(t, "the completion", b.one(out.stdout, "result")["summary"].(string), "updated alpha with the merge you resolved")
	equal(t, "its pull events", len(b.eventsOfType(out.stdout, "pull")), 0)
	equal(t, "the merge's parents", b.parents(b.ref(lineage.ForkRef("alpha"))), mine+"\n"+theirs)
	equal(t, "b's notes", fileBody(t, filepath.Join(b.forkDir("alpha", "alpha"), "notes.md")), forkNotes("four", "four, b", "seven", "seven, v2"))

	mine, theirs = b.ref(lineage.ForkRef("beta")), a.ref(lineage.ForkRef("beta"))
	out = b.run("--json", "skill", "update", "beta")
	equal(t, "beta's update: exit", out.exit, 4)
	equal(t, "beta's conflicted file", conflictPaths(b.one(out.stdout, "conflict")), "notes.md")
	keptInConflict(t, b, "beta", "beta", "notes.md", mine, theirs)
	equal(t, "b's beta", b.ref(lineage.ForkRef("beta")), mine)
}

// TestScenarioDifferentVersionsOtherLines has a take the source's second
// version into both forks and b its third, which keeps the second's change
// and changes other lines too. b publishes first and a updates: each merge
// is clean and holds the third version, and records as its base the
// version the source history proves the newer one. For alpha, a's account
// repo has never fetched the third version, so nothing proves its order
// and a's own base, the second version, stays; for beta, a fetched the
// source first, and the merge records the third. a's next check then
// finds the third version as alpha's update, whose merge changes no line:
// nothing was lost over the older base.
//
// Once both machines are in step again, a plain git clone of the remote
// reads the same upstream version and base off the trailers as agentx
// lists, and deleting every source ref leaves b's listing, drift
// included, as it was.
func TestScenarioDifferentVersionsOtherLines(t *testing.T) {
	t.Parallel()
	a, b, s, remote := scenarioHomes(t, "alpha", "beta")
	s.write("skills/alpha/notes.md", forkNotes("two", "two, v2"))
	s.write("skills/beta/notes.md", forkNotes("two", "two, v2"))
	s.commit("second version")
	takeVersion(a)
	alphaTwo, betaTwo := mergedImport(a, "alpha"), mergedImport(a, "beta")
	s.write("skills/alpha/notes.md", forkNotes("two", "two, v2", "seven", "seven, v3"))
	s.write("skills/beta/notes.md", forkNotes("two", "two, v2", "seven", "seven, v3"))
	s.commit("third version")
	takeVersion(b)
	alphaThree, betaThree := mergedImport(b, "alpha"), mergedImport(b, "beta")
	b.mustRun("skill", "publish")

	out := a.mustRun("--json", "skill", "update", "alpha")
	ev := a.one(out.stdout, "pull")
	equal(t, "alpha's pull", ev["outcome"], pullMerged)
	equal(t, "alpha's base, unproved", a.trailer(a.ref(lineage.ForkRef("alpha")), lineage.TrailerBase), alphaTwo)
	equal(t, "the base event", ev["base"], alphaTwo)
	a.mustRun("source", "fetch", s.url)
	out = a.mustRun("--json", "skill", "update", "beta")
	equal(t, "beta's pull", a.one(out.stdout, "pull")["outcome"], pullMerged)
	equal(t, "beta's base, proved newer", a.trailer(a.ref(lineage.ForkRef("beta")), lineage.TrailerBase), betaThree)
	if betaTwo == betaThree {
		t.Fatal("the two versions of beta have one import")
	}
	for _, name := range []string{"alpha", "beta"} {
		sameTree(t, name, libraryTree(t, a.forkDir(name, name)), libraryTree(t, filepath.Join(s.work, "skills", name)))
		equal(t, name+": git status", worktreeStatus(a, name), "")
	}

	out = a.mustRun("--json", "skill", "check-updates")
	equal(t, "the update found", a.one(out.stdout, "update_available")["name"], "alpha")
	equal(t, "its candidate", a.ref(lineage.CandidateRef("alpha")), alphaThree)
	a.mustRun("skill", "update", "alpha")
	tip := a.ref(lineage.ForkRef("alpha"))
	equal(t, "alpha's base after its update", a.trailer(tip, lineage.TrailerBase), alphaThree)
	equal(t, "the update changed no line", a.accountGit("diff", tip+"^1", tip), "")
	a.mustRun("skill", "publish")
	b.mustRun("skill", "update", "--all")
	writeFile(t, filepath.Join(b.forkDir("beta", "beta"), "notes.md"), "edited\n")

	listed := map[string]jsonEvent{}
	before := b.mustRun("--json", "skill", "list").stdout
	for _, e := range b.eventsOfType(before, "library_skill") {
		listed[e["name"].(string)] = e
	}
	clone := filepath.Join(t.TempDir(), "clone.git")
	remoteGit(t, b, remote, "clone", "--bare", "--quiet", remote, clone)
	tips := map[string]string{}
	for _, name := range []string{"alpha", "beta"} {
		tips[name] = remoteGit(t, b, clone, "rev-parse", "refs/heads/skills/"+name)
	}
	walked, err := lineage.Walk(context.Background(), gitx.New(b.env, false, func(string, ...any) {}), clone, []string{tips["alpha"], tips["beta"]})
	if err != nil {
		t.Fatal(err)
	}
	for name, tip := range tips {
		l := walked[tip]
		equal(t, name+": the fork id", l.ID, listed[name]["fork_id"])
		equal(t, name+": the upstream commit", l.Import.Commit, listed[name]["upstream_commit"])
		equal(t, name+": the base hash", l.Import.Hash, listed[name]["base_hash"])
		base := remoteGit(t, b, clone, "log", "-1", "--format=%(trailers:key="+lineage.TrailerBase+",valueonly)", tip)
		equal(t, name+": the base's upstream commit, read by git", remoteGit(t, b, clone, "log", "-1", "--format=%(trailers:key="+lineage.TrailerCommit+",valueonly)", base), listed[name]["upstream_commit"])
		equal(t, name+": the base's hash, read by git", remoteGit(t, b, clone, "log", "-1", "--format=%(trailers:key="+lineage.TrailerHash+",valueonly)", base), listed[name]["base_hash"])
	}
	equal(t, "beta's state", listed["beta"]["state"], stateModified)

	for _, ref := range strings.Split(b.accountGit("for-each-ref", "--format=%(refname)", source.RefPrefix), "\n") {
		if ref != "" {
			b.accountGit("update-ref", "-d", ref)
		}
	}
	if after := b.mustRun("--json", "skill", "list").stdout; after != before {
		t.Errorf("the listing changed when the source refs went:\nbefore:\n%safter:\n%s", before, after)
	}
}

// pendingBase is the base the merge pending for the fork called name on h
// is to record, the Agentx-Base of its MERGE_MSG.
func pendingBase(t *testing.T, h *harness, name string) string {
	t.Helper()
	_, _, msg := mergeState(t, h, name)
	trailers, err := lineage.ParseFork(msg)
	if err != nil {
		t.Fatalf("the MERGE_MSG of %s: %v\n%s", name, err, msg)
	}
	return trailers.Base
}

// TestScenarioDifferentVersionsSameLines has a take the source's second
// version into both forks and b its third, which changes again the line
// the second changed. Each sync of the two conflicts: the merge is left
// pending in a checkout under the merges directory, each side's blob whole
// in it, and neither machine's worktree holds a conflict marker. On a, the
// machine with the second version, the update of alpha first records a's
// own base, since a has not fetched the third version and nothing proves
// it the newer; giving the merge up leaves both machines' branches and
// worktrees as they were. Once a fetched the source, the same update records
// the third version, and resolved in the checkout, the next update completes
// the merge with that base. On b, the machine with the third version, the
// update of a's beta records b's own base, the third version, which is also
// the newer.
func TestScenarioDifferentVersionsSameLines(t *testing.T) {
	t.Parallel()
	a, b, s, _ := scenarioHomes(t, "alpha", "beta")
	s.write("skills/alpha/notes.md", forkNotes("two", "two, v2"))
	s.write("skills/beta/notes.md", forkNotes("two", "two, v2"))
	s.commit("second version")
	takeVersion(a)
	alphaTwo := mergedImport(a, "alpha")
	s.write("skills/alpha/notes.md", forkNotes("two", "two, v3"))
	s.write("skills/beta/notes.md", forkNotes("two", "two, v3"))
	s.commit("third version")
	takeVersion(b)
	alphaThree, betaThree := mergedImport(b, "alpha"), mergedImport(b, "beta")
	b.mustRun("skill", "publish", "alpha")

	mine, theirs := a.ref(lineage.ForkRef("alpha")), b.ref(lineage.ForkRef("alpha"))
	notesA, notesB := filepath.Join(a.forkDir("alpha", "alpha"), "notes.md"), filepath.Join(b.forkDir("alpha", "alpha"), "notes.md")
	out := a.run("--json", "skill", "update", "alpha")
	equal(t, "alpha's update: exit", out.exit, 4)
	equal(t, "the conflicted file", conflictPaths(a.one(out.stdout, "conflict")), "notes.md")
	keptInConflict(t, a, "alpha", "alpha", "notes.md", mine, theirs)
	equal(t, "the base it is to record, unproved", pendingBase(t, a, "alpha"), alphaTwo)
	noMarkers(t, "a's worktree", notesA)
	noMarkers(t, "b's worktree", notesB)
	a.mustRun("skill", "update", "alpha", "--abort")
	noCheckout(t, a, "alpha")
	equal(t, "a's alpha", a.ref(lineage.ForkRef("alpha")), mine)
	equal(t, "b's alpha", b.ref(lineage.ForkRef("alpha")), theirs)
	equal(t, "a's notes", fileBody(t, notesA), forkNotes("two", "two, v2"))
	equal(t, "b's notes", fileBody(t, notesB), forkNotes("two", "two, v3"))
	equal(t, "a's status", worktreeStatus(a, "alpha"), "")

	a.mustRun("source", "fetch", s.url)
	equal(t, "the update again: exit", a.run("skill", "update", "alpha").exit, 4)
	equal(t, "the base it is to record, proved newer", pendingBase(t, a, "alpha"), alphaThree)
	writeFile(t, filepath.Join(pendingCheckout(a, "alpha"), "alpha", "notes.md"), forkNotes("two", "two, v2 and v3"))
	checkoutGit(t, a, "alpha", "add", "alpha/notes.md")
	checkoutGit(t, a, "alpha", "commit", "--quiet", "-m", "resolve")
	out = a.mustRun("--json", "skill", "update", "alpha")
	contains(t, "the completion", a.one(out.stdout, "result")["summary"].(string), "with the merge you resolved")
	done := a.ref(lineage.ForkRef("alpha"))
	equal(t, "the merge's parents", a.parents(done), mine+"\n"+theirs)
	equal(t, "the merge's base", a.trailer(done, lineage.TrailerBase), alphaThree)
	equal(t, "a's notes, resolved", fileBody(t, notesA), forkNotes("two", "two, v2 and v3"))
	noCheckout(t, a, "alpha")

	a.mustRun("skill", "publish", "beta")
	mine, theirs = b.ref(lineage.ForkRef("beta")), a.ref(lineage.ForkRef("beta"))
	out = b.run("--json", "skill", "update", "beta")
	equal(t, "beta's update: exit", out.exit, 4)
	keptInConflict(t, b, "beta", "beta", "notes.md", mine, theirs)
	equal(t, "the base it is to record, b's own", pendingBase(t, b, "beta"), betaThree)
	for _, h := range []*harness{a, b} {
		noMarkers(t, "beta's worktree", filepath.Join(h.forkDir("beta", "beta"), "notes.md"))
	}
	writeFile(t, filepath.Join(pendingCheckout(b, "beta"), "beta", "notes.md"), forkNotes("two", "two, v3"))
	checkoutGit(t, b, "beta", "add", "beta/notes.md")
	b.mustRun("skill", "update", "beta")
	done = b.ref(lineage.ForkRef("beta"))
	equal(t, "beta's merge's parents", b.parents(done), mine+"\n"+theirs)
	equal(t, "beta's merge's base", b.trailer(done, lineage.TrailerBase), betaThree)
}

// TestScenarioUnprovableOrderKeepsTheLocalBase has a take the source's
// second version into alpha. Then the source is force-pushed: its history
// is rewritten from the first version, with the second's change made again
// and another beside it, in a commit whose time is later than any other.
// b takes that rewritten version and publishes, and a, which has fetched
// the rewritten history too, updates: the merge is clean, but no history
// proves the rewritten version the newer, so a's base stays, though the
// rewritten version is the later one by its time. a's next check finds the
// rewritten version as alpha's update, which merges over a's base without
// changing a line: nothing was lost over it.
func TestScenarioUnprovableOrderKeepsTheLocalBase(t *testing.T) {
	t.Parallel()
	a, b, s, _ := scenarioHomes(t, "alpha")
	first := s.run("rev-parse", "HEAD")
	s.write("skills/alpha/notes.md", forkNotes("two", "two, v2"))
	s.commit("second version")
	takeVersion(a)
	alphaTwo := mergedImport(a, "alpha")
	s.run("reset", "--quiet", "--soft", first)
	s.write("skills/alpha/notes.md", forkNotes("two", "two, v2", "seven", "seven, rewritten"))
	s.commitAt("the second version, rewritten", "1900000000 +0000")
	takeVersion(b)
	rewritten := mergedImport(b, "alpha")
	b.mustRun("skill", "publish", "alpha")

	a.mustRun("source", "fetch", s.url)
	out := a.mustRun("--json", "skill", "update", "alpha")
	equal(t, "the pull", a.one(out.stdout, "pull")["outcome"], pullMerged)
	merged := a.ref(lineage.ForkRef("alpha"))
	equal(t, "the base, a's own", a.trailer(merged, lineage.TrailerBase), alphaTwo)
	sameTree(t, "alpha", libraryTree(t, a.forkDir("alpha", "alpha")), libraryTree(t, filepath.Join(s.work, "skills", "alpha")))

	a.mustRun("skill", "check-updates")
	equal(t, "the update found", a.ref(lineage.CandidateRef("alpha")), rewritten)
	a.mustRun("skill", "update", "alpha")
	tip := a.ref(lineage.ForkRef("alpha"))
	equal(t, "the base after the update", a.trailer(tip, lineage.TrailerBase), rewritten)
	equal(t, "the update changed no line", a.accountGit("diff", merged, tip), "")
}

// TestScenarioIgnoredFilesStayLocal has a commit and publish alpha with
// files beside it that a's ignore rules leave out: one the skill's own
// .gitignore names, one a's global ignore file names, and a .DS_Store,
// which ignore_system_files leaves out. None of them is in what a
// published, and b's update brings none of them. b, whose rules do not
// ignore the file a's global ignore file names, commits one of its own at
// that path and publishes it; a's update then replaces a's ignored file with
// the one b committed, as git checkout replaces it, while the journal holds
// a's own until the update is done, and the files a's rules still ignore stay
// as they were.
func TestScenarioIgnoredFilesStayLocal(t *testing.T) {
	t.Parallel()
	a, b, _, remote := scenarioHomes(t, "alpha")
	alphaA, alphaB := a.forkDir("alpha", "alpha"), b.forkDir("alpha", "alpha")
	writeFile(t, mkdirs(t, filepath.Join(a.config, "git"), "ignore"), "*.local\n")
	writeFile(t, filepath.Join(alphaA, ".gitignore"), "*.log\n")
	writeFile(t, filepath.Join(alphaA, "build.log"), "a's build\n")
	writeFile(t, filepath.Join(alphaA, "notes.local"), "a's own notes\n")
	writeFile(t, filepath.Join(alphaA, ".DS_Store"), "finder\n")
	a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("skill", "publish", "alpha")
	equal(t, "what a published", remoteGit(t, b, remote, "ls-tree", "--name-only", "refs/heads/skills/alpha:alpha"), ".gitignore\nSKILL.md\nnotes.md")
	b.mustRun("skill", "update", "alpha")
	sameTree(t, "b's alpha", libraryTree(t, alphaB), map[string]string{
		".gitignore": "*.log\n", "SKILL.md": fileBody(t, filepath.Join(alphaA, "SKILL.md")), "notes.md": forkNotes("one", "one, a"),
	})

	writeFile(t, filepath.Join(alphaB, "notes.local"), "b's notes, committed\n")
	b.mustRun("skill", "commit", "alpha")
	b.mustRun("skill", "publish", "alpha")
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// The journal's last step resets the worktree's index; by then the new
	// content is in place and a's own file is still held beside it. The
	// wrapper copies it with shell builtins alone: the harness PATH holds
	// nothing but the wrapper.
	held := filepath.Join(t.TempDir(), "held")
	path := a.env["PATH"]
	stubGit(t, a, `#!/bin/sh
case " $* " in
*" reset "*)
	for f in `+shellWord(filepath.Join(a.agentx, "worktrees"))+`/.agentx-retained-*/notes.local; do
		while IFS= read -r line; do printf '%s\n' "$line"; done < "$f"
	done > `+shellWord(held)+`
	;;
esac
exec `+shellWord(real)+` "$@"
`)
	out := a.mustRun("--json", "skill", "update", "alpha")
	a.env["PATH"] = path
	equal(t, "the pull", a.one(out.stdout, "pull")["outcome"], pullFastForward)
	equal(t, "what the journal held", fileBody(t, held), "a's own notes\n")
	equal(t, "a's notes.local", fileBody(t, filepath.Join(alphaA, "notes.local")), "b's notes, committed\n")
	equal(t, "a's build.log", fileBody(t, filepath.Join(alphaA, "build.log")), "a's build\n")
	equal(t, "a's .DS_Store", fileBody(t, filepath.Join(alphaA, ".DS_Store")), "finder\n")
	equal(t, "a's status", worktreeStatus(a, "alpha"), "")
	equal(t, "what is left beside the worktrees", strings.Join(hiddenEntries(t, filepath.Join(a.agentx, "worktrees")), " "), "")
}

// TestScenarioEdgeCases syncs what is not plain text lines. b added the
// source by another spelling of its URL before it installed the forks,
// and the install adds no second source: both machines take the source's
// second version as one import commit, and b's update of a's merge records
// that import as its base. In alpha, a makes notes.md executable, adds a
// binary file and a symlink, while b edits notes.md: b's update merges all
// of it as git does, the mode with b's edit, the binary file and the
// symlink as they are. In beta, both machines change one binary file and
// one symlink each their own way: b's update conflicts in the two, each
// shown whole, as each side's blob, with b's own in the checkout, never
// with markers; taking a's side of both in the checkout completes the
// merge with a's file and a's symlink.
func TestScenarioEdgeCases(t *testing.T) {
	t.Parallel()
	a, b, s, _ := scenarioMachines(t)
	b.mustRun("source", "add", "https://GitHub.com/fixtures/forked.git/")
	for _, name := range []string{"alpha", "beta"} {
		out := b.mustRun("--json", "skill", "add", "--from-account", name)
		if sources := b.eventsOfType(out.stdout, "source"); len(sources) != 0 {
			t.Errorf("installing %s added a source: %v", name, sources)
		}
	}
	shared := 0
	for _, ev := range b.eventsOfType(b.mustRun("--json", "source", "list").stdout, "source") {
		if ev["account"] != true {
			shared++
		}
	}
	equal(t, "b's shared sources", shared, 1)
	s.write("skills/alpha/notes.md", forkNotes("four", "four, v2"))
	s.commit("second version")
	takeVersion(a)
	takeVersion(b)
	imp := mergedImport(a, "alpha")
	equal(t, "b's import of the version", mergedImport(b, "alpha"), imp)
	alphaA, alphaB := a.forkDir("alpha", "alpha"), b.forkDir("alpha", "alpha")
	if err := os.Chmod(filepath.Join(alphaA, "notes.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(alphaA, "logo.png"), "\x89PNG\r\n\x1a\n\x00a's logo")
	link(t, "notes.md", filepath.Join(alphaA, "link.md"))
	a.mustRun("skill", "commit", "alpha")
	a.mustRun("skill", "publish")
	b.commitFork("alpha", forkNotes("four", "four, v2", "eight", "eight, b"))
	out := b.mustRun("--json", "skill", "update", "alpha")
	equal(t, "alpha's pull", b.one(out.stdout, "pull")["outcome"], pullMerged)
	merged := b.ref(lineage.ForkRef("alpha"))
	equal(t, "alpha's base", b.trailer(merged, lineage.TrailerBase), imp)
	equal(t, "the merged tree", b.accountGit("ls-tree", merged+":alpha", "--format=%(objectmode) %(path)"),
		"100644 SKILL.md\n120000 link.md\n100644 logo.png\n100755 notes.md")
	equal(t, "b's notes", fileBody(t, filepath.Join(alphaB, "notes.md")), forkNotes("four", "four, v2", "eight", "eight, b"))
	if info, err := os.Stat(filepath.Join(alphaB, "notes.md")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("b's notes.md is not executable: %v %v", info, err)
	}
	equal(t, "b's logo", fileBody(t, filepath.Join(alphaB, "logo.png")), "\x89PNG\r\n\x1a\n\x00a's logo")
	equal(t, "b's link", readLink(t, filepath.Join(alphaB, "link.md")), "notes.md")
	equal(t, "b's status", worktreeStatus(b, "alpha"), "")

	betaA, betaB := a.forkDir("beta", "beta"), b.forkDir("beta", "beta")
	writeFile(t, filepath.Join(betaA, "logo.png"), "\x89PNG\r\n\x1a\n\x00the logo")
	link(t, "SKILL.md", filepath.Join(betaA, "link.md"))
	a.mustRun("skill", "commit", "beta")
	a.mustRun("skill", "publish", "beta")
	b.mustRun("skill", "update", "beta")
	writeFile(t, filepath.Join(betaA, "logo.png"), "\x89PNG\r\n\x1a\n\x00a's logo")
	swapForLink(t, filepath.Join(betaA, "link.md"), "notes.md")
	a.mustRun("skill", "commit", "beta")
	a.mustRun("skill", "publish", "beta")
	writeFile(t, filepath.Join(betaB, "logo.png"), "\x89PNG\r\n\x1a\n\x00b's logo")
	swapForLink(t, filepath.Join(betaB, "link.md"), "SKILL.md.orig")
	b.mustRun("skill", "commit", "beta")
	mine, theirs := b.ref(lineage.ForkRef("beta")), a.ref(lineage.ForkRef("beta"))
	out = b.run("--json", "skill", "update", "beta")
	equal(t, "beta's update: exit", out.exit, 4)
	equal(t, "the conflicted files", conflictPaths(b.one(out.stdout, "conflict")), "link.md,logo.png")
	checkout := filepath.Join(pendingCheckout(b, "beta"), "beta")
	for _, path := range []string{"link.md", "logo.png"} {
		keptInConflict(t, b, "beta", "beta", path, mine, theirs)
	}
	equal(t, "the logo in the checkout", fileBody(t, filepath.Join(checkout, "logo.png")), "\x89PNG\r\n\x1a\n\x00b's logo")
	equal(t, "the link in the checkout", readLink(t, filepath.Join(checkout, "link.md")), "SKILL.md.orig")
	equal(t, "b's logo", fileBody(t, filepath.Join(betaB, "logo.png")), "\x89PNG\r\n\x1a\n\x00b's logo")

	checkoutGit(t, b, "beta", "checkout", "--theirs", "--", "beta/logo.png", "beta/link.md")
	checkoutGit(t, b, "beta", "add", "beta/logo.png", "beta/link.md")
	b.mustRun("skill", "update", "beta")
	equal(t, "beta's merge's parents", b.parents(b.ref(lineage.ForkRef("beta"))), mine+"\n"+theirs)
	equal(t, "b's logo, resolved", fileBody(t, filepath.Join(betaB, "logo.png")), "\x89PNG\r\n\x1a\n\x00a's logo")
	equal(t, "b's link, resolved", readLink(t, filepath.Join(betaB, "link.md")), "notes.md")
	equal(t, "b's beta status", worktreeStatus(b, "beta"), "")
}

// readLink is the target of the symlink at path.
func readLink(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
