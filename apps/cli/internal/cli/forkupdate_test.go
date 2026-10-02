package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// forkNotes is notes.md of the skills of forkUpdateHome, with every line
// of lines replaced as given, "three" by "three, upstream" say. Its fifth
// line is a literal conflict marker, as a Markdown rule writes one.
func forkNotes(lines ...string) string {
	notes := []string{"one", "two", "three", "four", "=======", "six", "seven", "eight"}
	for i := 0; i+1 < len(lines); i += 2 {
		for j, l := range notes {
			if l == lines[i] {
				notes[j] = lines[i+1]
			}
		}
	}
	return strings.Join(notes, "\n") + "\n"
}

// forkUpdateHarness is a home with alpha and beta installed from a source
// at its first commit, each with forkNotes as its notes.md, ready to be
// forked. It returns that commit.
func forkUpdateHarness(t *testing.T) (*harness, *sourceRepo, string) {
	t.Helper()
	h, s, ids := forkUpdateHome.copy(t)
	return h, s, ids[0]
}

// forkUpdateHome is the home forkUpdateHarness hands out.
var forkUpdateHome = &fixtureHome{
	source: "forked",
	dirs:   []string{".claude"},
	build: func(h *harness, s *sourceRepo) []string {
		s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": forkNotes()})
		s.skill("skills/beta", "beta", "The second skill", map[string]string{"notes.md": forkNotes()})
		first := s.commit("first version")
		h.mustRun("source", "add", s.url)
		h.mustRun("skill", "add", s.url, "--all")
		return []string{first}
	},
}

// forkDir is the skill directory of the fork called name, whose branch
// holds it under dir, in its worktree.
func (h *harness) forkDir(name, dir string) string {
	return filepath.Join(h.agentx, "worktrees", name, dir)
}

// parents is the parents of commit, as rev-parse lists them.
func (h *harness) parents(commit string) string {
	return h.accountGit("rev-parse", commit+"^@")
}

// TestSkillUpdateMergesUpstreamIntoAFork updates two forks in one run of
// --all. alpha has no commits of its own: its update merges clean and its
// skill directory holds exactly the new version, besides the .DS_Store git
// ignores in it. beta has a commit of its own, an edit away from the
// lines the update changes and a .gitignore naming build/: the merge
// keeps the edit and takes the update's, the ignored build/keep stays,
// and the update's own build/x replaces the local file of that path, as
// git checkout replaces it. Each fork's branch moves to a merge commit
// with the tip and the candidate as parents and the candidate as its
// base, written with the user's identity; the candidate goes, git status
// in each worktree is clean, nothing is pending and nothing is left
// beside the worktrees.
func TestSkillUpdateMergesUpstreamIntoAFork(t *testing.T) {
	t.Parallel()
	h, s, _ := forkUpdateHarness(t)
	h.withIdentity("Fork Writer", "writer@example.com")
	h.mustRun("skill", "fork", "alpha")
	h.mustRun("skill", "fork", "beta")
	alpha, beta := h.forkDir("alpha", "alpha"), h.forkDir("beta", "beta")
	writeFile(t, filepath.Join(beta, "notes.md"), forkNotes("two", "two, mine"))
	writeFile(t, filepath.Join(beta, ".gitignore"), "build/\n")
	h.mustRun("skill", "commit", "beta")
	writeFile(t, filepath.Join(alpha, ".DS_Store"), "finder\n")
	writeFile(t, mkdirs(t, filepath.Join(beta, "build"), "x"), "local build\n")
	writeFile(t, filepath.Join(beta, "build", "keep"), "kept\n")

	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, upstream"))
	s.write("skills/beta/notes.md", forkNotes("seven", "seven, upstream"))
	s.write("skills/beta/build/x", "upstream build\n")
	second := s.commit("second version")
	h.mustRun("skill", "check")
	tips := map[string]string{"alpha": h.ref(lineage.ForkRef("alpha")), "beta": h.ref(lineage.ForkRef("beta"))}
	candidates := map[string]string{"alpha": h.ref(lineage.CandidateRef("alpha")), "beta": h.ref(lineage.CandidateRef("beta"))}

	out := h.mustRun("--json", "skill", "update", "--all")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "updated 2 skills")
	for _, name := range []string{"alpha", "beta"} {
		ev := h.librarySkill(out.stdout, name)
		equal(t, name+": upstream_commit", ev["upstream_commit"], second)
		equal(t, name+": state", ev["state"], stateCurrent)
		if ev["candidate"] != nil {
			t.Errorf("%s still lists an update: %v", name, ev["candidate"])
		}
		tip := h.ref(lineage.ForkRef(name))
		equal(t, name+": the merge's parents", h.parents(tip), tips[name]+"\n"+candidates[name])
		equal(t, name+": its base", h.trailer(tip, lineage.TrailerBase), candidates[name])
		equal(t, name+": its subject", h.accountGit("log", "-1", "--format=%s", tip), name+": merge upstream "+short(second)+" (test-host)")
		equal(t, name+": its author", h.accountGit("log", "-1", "--format=%an <%ae>", tip), "Fork Writer <writer@example.com>")
		equal(t, name+": the candidate", h.ref(lineage.CandidateRef(name)), "")
		equal(t, name+": git status", gitIn(t, h, filepath.Join(h.agentx, "worktrees", name), "status", "--porcelain"), "")
		noCheckout(t, h, name)
	}
	sameTree(t, "alpha's skill directory", libraryTree(t, alpha), withFile(libraryTree(t, filepath.Join(s.work, "skills", "alpha")), ".DS_Store", "finder\n"))
	equal(t, "beta's notes", fileBody(t, filepath.Join(beta, "notes.md")), forkNotes("two", "two, mine", "seven", "seven, upstream"))
	equal(t, "beta's build/x", fileBody(t, filepath.Join(beta, "build", "x")), "upstream build\n")
	equal(t, "beta's build/keep", fileBody(t, filepath.Join(beta, "build", "keep")), "kept\n")
	equal(t, "what is left beside the worktrees", strings.Join(hiddenEntries(t, filepath.Join(h.agentx, "worktrees")), " "), "")
}

// TestSkillUpdateOfAForkConflicts updates two forks whose own changes
// overlap their update's. alpha committed an edit to the line the update
// changes; other, a fork of alpha under a new name, has its name written
// into SKILL.md, the line next to the description the update changes,
// which conflicts as any adjacent edit does in git. Uncommitted edits
// refuse the update first, exit 6, and nothing changes. The update then
// leaves alpha's merge pending, exit 4, with a conflict of kind fork: the
// fork's worktree and branch are untouched, and its checkout holds the
// merge in progress, with conflict markers one longer than the literal
// marker line of notes.md, which stays as it is, and the merge's base,
// mine and theirs as ORIG_HEAD, MERGE_HEAD and the Agentx-Base of
// MERGE_MSG. Giving it up leaves the fork, its branch and the candidate
// as they were. Once the merge is set up again and resolved with git, an
// uncommitted edit refuses its completion, exit 6; without it, the update
// completes the merge: the branch moves to a merge commit of the tip and
// the candidate, which names the candidate as its base, the worktree holds
// the resolution and keeps the .DS_Store git ignores, and the checkout and
// the candidate are gone. other's merge, resolved and committed with git
// in its checkout, is applied as committed.
func TestSkillUpdateOfAForkConflicts(t *testing.T) {
	t.Parallel()
	h, s, first := forkUpdateHarness(t)
	h.mustRun("skill", "fork", "alpha")
	h.mustRun("skill", "fork", "alpha", "--name", "other")
	alpha := h.forkDir("alpha", "alpha")
	writeFile(t, filepath.Join(alpha, "notes.md"), forkNotes("seven", "seven, mine"))
	h.mustRun("skill", "commit", "alpha")
	s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": forkNotes("seven", "seven, upstream")})
	second := s.commit("second version")
	h.mustRun("skill", "check")
	tip, candidate := h.ref(lineage.ForkRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	base := h.accountGit("rev-parse", tip+"~2") // the commit, then the fork's creation, then the import

	writeFile(t, filepath.Join(alpha, "notes.md"), forkNotes("seven", "seven, uncommitted"))
	was := h.unchangedHome()
	refused := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit over uncommitted edits", refused.exit, 6)
	equal(t, "message", h.one(refused.stdout, "error")["message"], "alpha has uncommitted edits, so it cannot be updated until they are committed or reverted")
	was.check(t, h, "the refused update", 0)
	writeFile(t, filepath.Join(alpha, "notes.md"), forkNotes("seven", "seven, mine"))
	worktree := onDisk(t, filepath.Join(h.agentx, "worktrees", "alpha"))

	out := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit", out.exit, 4)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "alpha conflicts with its update in 1 file, so the merge is pending and the fork's worktree and branch were left as they are")
	equal(t, "hint", e["hint"], conflictHint(h, "alpha", "alpha"))
	ev := h.one(out.stdout, "conflict")
	equal(t, "kind", ev["kind"], lineage.KindFork)
	equal(t, "base", ev["base"], base)
	equal(t, "mine", ev["mine"], tip)
	equal(t, "theirs", ev["theirs"], candidate)
	equal(t, "files", conflictPaths(ev), "notes.md")
	equal(t, "the branch", h.ref(lineage.ForkRef("alpha")), tip)
	equal(t, "the worktree", onDisk(t, filepath.Join(h.agentx, "worktrees", "alpha")), worktree)
	head, mergeHead, msg := mergeState(t, h, "alpha")
	equal(t, "the checkout's HEAD", head, tip)
	equal(t, "the checkout's MERGE_HEAD", mergeHead, candidate)
	equal(t, "the checkout's ORIG_HEAD", strings.TrimSpace(checkoutGit(t, h, "alpha", "rev-parse", "ORIG_HEAD")), tip)
	contains(t, "the checkout's MERGE_MSG", msg, "alpha: merge upstream "+short(second)+" (test-host)\n\nAgentx-Base: "+candidate+"\nAgentx-Machine: ")
	merged := fileBody(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha", "notes.md"))
	contains(t, "the sized markers", merged, "four\n=======\nsix\n<<<<<<<< "+tip+"\nseven, mine\n|||||||| "+base+"\nseven\n========\nseven, upstream\n>>>>>>>> "+candidate+"\n")
	equal(t, "the clean file", fileBody(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha", "SKILL.md")), skill("alpha", "The first skill, revised"))
	equal(t, "pending_merge", h.listed("alpha")["pending_merge"], true)
	text := h.run("skill", "update", "alpha")
	equal(t, "stdout of the update run again", text.stdout, "alpha conflicts with its update from "+short(first)+" to "+short(second)+" in 1 file\nnotes.md: both modified\n")

	abort := h.mustRun("skill", "update", "alpha", "--abort")
	equal(t, "stdout of the abort", abort.stdout, "✓ gave up the merge of alpha; the fork's worktree and branch are as they were\n")
	noCheckout(t, h, "alpha")
	equal(t, "the branch once given up", h.ref(lineage.ForkRef("alpha")), tip)
	equal(t, "the candidate once given up", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the worktree once given up", onDisk(t, filepath.Join(h.agentx, "worktrees", "alpha")), worktree)

	if out := h.run("skill", "update", "alpha"); out.exit != 4 {
		t.Fatalf("the update set up again: exit %d\n%s", out.exit, out.stderr)
	}
	resolved := forkNotes("seven", "seven, resolved")
	writeFile(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha", "notes.md"), resolved)
	checkoutGit(t, h, "alpha", "add", "alpha/notes.md")
	writeFile(t, filepath.Join(alpha, "SKILL.md"), skill("alpha", "Uncommitted"))
	refused = h.run("--json", "skill", "update", "alpha")
	equal(t, "exit of the completion over uncommitted edits", refused.exit, 6)
	equal(t, "message", h.one(refused.stdout, "error")["message"], "alpha has uncommitted edits, so it cannot be updated until they are committed or reverted")
	// A plain revert is refused while the merge is pending, so the edit is
	// undone by hand.
	writeFile(t, filepath.Join(alpha, "SKILL.md"), skill("alpha", "The first skill"))
	writeFile(t, filepath.Join(alpha, ".DS_Store"), "finder\n")

	done := h.mustRun("skill", "update", "alpha")
	merge := h.ref(lineage.ForkRef("alpha"))
	equal(t, "stdout of the completion", done.stdout, "✓ updated alpha from "+short(first)+" to "+short(second)+" with the merge you resolved, committed as "+short(merge)+"\n")
	equal(t, "the merge's parents", h.parents(merge), tip+"\n"+candidate)
	equal(t, "the merge's message", h.accountGit("log", "-1", "--format=%B", merge), strings.TrimSuffix(msg, "\n"))
	equal(t, "the notes", fileBody(t, filepath.Join(alpha, "notes.md")), resolved)
	equal(t, "the ignored file", fileBody(t, filepath.Join(alpha, ".DS_Store")), "finder\n")
	equal(t, "git status", gitIn(t, h, filepath.Join(h.agentx, "worktrees", "alpha"), "status", "--porcelain"), "")
	equal(t, "the candidate once applied", h.ref(lineage.CandidateRef("alpha")), "")
	noCheckout(t, h, "alpha")

	otherCandidate := h.ref(lineage.CandidateRef("other"))
	out = h.run("--json", "skill", "update", "other")
	equal(t, "exit of the renamed fork's update", out.exit, 4)
	equal(t, "the renamed fork's conflict", conflictPaths(h.one(out.stdout, "conflict")), "SKILL.md")
	renamed := "---\nname: other\ndescription: The first skill, revised\n---\n\n# alpha\n"
	writeFile(t, filepath.Join(pendingCheckout(h, "other"), "alpha", "SKILL.md"), renamed)
	checkoutGit(t, h, "other", "add", "alpha/SKILL.md")
	checkoutGit(t, h, "other", "commit", "--quiet", "--no-edit")
	h.mustRun("skill", "update", "other")
	equal(t, "the renamed fork's SKILL.md", fileBody(t, filepath.Join(h.forkDir("other", "alpha"), "SKILL.md")), renamed)
	equal(t, "the renamed fork's base", h.trailer(h.ref(lineage.ForkRef("other")), lineage.TrailerBase), otherCandidate)
}

// TestForkCompletionAfterTheTipMoved: a commit made in the fork while its
// merge was pending, on a line the resolution changed too, makes the
// completion merge the finished merge with the new tip, the tip the merge
// started from as the base, so that only that line conflicts, exit 4, in
// the same checkout, now at the new tip. Once that is resolved, the
// completion applies it: a merge of the new tip and the finished merge,
// itself a merge of the old tip and the candidate, which names the
// candidate as the fork's base. The candidate goes, and a check right
// after finds nothing to merge again.
func TestForkCompletionAfterTheTipMoved(t *testing.T) {
	t.Parallel()
	h, s, _ := forkUpdateHarness(t)
	h.mustRun("skill", "fork", "alpha")
	alpha := h.forkDir("alpha", "alpha")
	writeFile(t, filepath.Join(alpha, "notes.md"), forkNotes("seven", "seven, mine"))
	h.mustRun("skill", "commit", "alpha")
	s.write("skills/alpha/notes.md", forkNotes("one", "one, upstream", "seven", "seven, upstream"))
	s.commit("second version")
	h.mustRun("skill", "check")
	old, candidate := h.ref(lineage.ForkRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	if out := h.run("skill", "update", "alpha"); out.exit != 4 {
		t.Fatalf("the update that should conflict: exit %d\n%s", out.exit, out.stderr)
	}
	writeFile(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha", "notes.md"), forkNotes("one", "one, upstream", "seven", "seven, resolved"))
	checkoutGit(t, h, "alpha", "add", "alpha/notes.md")
	writeFile(t, filepath.Join(alpha, "notes.md"), forkNotes("one", "one, meanwhile", "seven", "seven, mine"))
	h.mustRun("skill", "commit", "alpha")
	tip := h.ref(lineage.ForkRef("alpha"))

	out := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit", out.exit, 4)
	equal(t, "message", h.one(out.stdout, "error")["message"],
		"alpha conflicts with the commits made while its merge was pending in 1 file, so the merge is pending and the fork's worktree and branch were left as they are")
	ev := h.one(out.stdout, "conflict")
	equal(t, "base", ev["base"], old)
	equal(t, "mine", ev["mine"], tip)
	finished := ev["theirs"].(string)
	equal(t, "the finished merge's parents", h.parents(finished), old+"\n"+candidate)
	head, mergeHead, _ := mergeState(t, h, "alpha")
	equal(t, "the checkout's HEAD", head, tip)
	equal(t, "the checkout's MERGE_HEAD", mergeHead, finished)
	merged := fileBody(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha", "notes.md"))
	if !strings.HasPrefix(merged, "<<<<<<<< "+tip+"\none, meanwhile\n|||||||| "+old+"\none\n========\none, upstream\n>>>>>>>> "+finished+"\n") ||
		!strings.HasSuffix(merged, "\nseven, resolved\neight\n") {
		t.Errorf("the checkout's notes.md shows more than the overlap:\n%s", merged)
	}

	checkoutGit(t, h, "alpha", "checkout", "--ours", "alpha/notes.md")
	checkoutGit(t, h, "alpha", "add", "alpha/notes.md")
	h.mustRun("skill", "update", "alpha")
	final := h.ref(lineage.ForkRef("alpha"))
	equal(t, "the final merge's parents", h.parents(final), tip+"\n"+finished)
	equal(t, "its base", h.trailer(final, lineage.TrailerBase), candidate)
	equal(t, "the notes", fileBody(t, filepath.Join(alpha, "notes.md")), forkNotes("one", "one, meanwhile", "seven", "seven, mine"))
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), "")
	noCheckout(t, h, "alpha")
	check := h.mustRun("--json", "skill", "check")
	equal(t, "a check right after", h.one(check.stdout, "result")["summary"], "checked 2 skills from 1 source: no update available")
}

// TestARevertSurvivesTheNextUpdate: a fork that took an update and then
// put one of its lines back as it was before keeps that line through the
// next update, which merges with the import it took as the base, where a
// merge from the import it was forked from would bring the line back.
func TestARevertSurvivesTheNextUpdate(t *testing.T) {
	t.Parallel()
	h, s, _ := forkUpdateHarness(t)
	h.mustRun("skill", "fork", "alpha")
	alpha := h.forkDir("alpha", "alpha")
	s.write("skills/alpha/notes.md", forkNotes("three", "three, upstream"))
	s.commit("second version")
	h.mustRun("skill", "check")
	h.mustRun("skill", "update", "alpha")
	writeFile(t, filepath.Join(alpha, "notes.md"), forkNotes())
	h.mustRun("skill", "commit", "alpha")
	s.write("skills/alpha/notes.md", forkNotes("three", "three, upstream", "seven", "seven, upstream"))
	s.commit("third version")
	h.mustRun("skill", "check")
	h.mustRun("skill", "update", "alpha")
	equal(t, "the notes", fileBody(t, filepath.Join(alpha, "notes.md")), forkNotes("seven", "seven, upstream"))
}

// TestForkUpdateRecoversWhereItWasKilled kills a fork's clean update with
// SIGKILL at two boundaries: once its journal is on disk, before anything
// was applied, and right after its last live write, the deletion of the
// candidate. Every boundary between them is the journal's, which
// home.TestForkRevertRecoversFromEveryBoundary replays without git for the
// same steps. The next command finishes the update: the branch at the
// merge, the skill directory holding it with the ignored file kept, git
// status in the worktree clean, the candidate gone and nothing staged or
// retained left.
func TestForkUpdateRecoversWhereItWasKilled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, script string }{
		{"once the journal is on disk", killedUpdateScript},
		{"after its last live write", lastWriteScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := forkUpdateHarness(t)
			h.mustRun("skill", "fork", "alpha")
			alpha := h.forkDir("alpha", "alpha")
			writeFile(t, filepath.Join(alpha, ".DS_Store"), "finder\n")
			s.write("skills/alpha/notes.md", forkNotes("seven", "seven, upstream"))
			s.commit("second version")
			h.mustRun("skill", "check")
			tip, candidate := h.ref(lineage.ForkRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
			out := killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", tc.script)
			_, kinds := journalKinds(t, h)
			equal(t, "the journal's steps", kinds, "ref, remove, publish, worktree, ref")
			if got := h.run("skill", "list"); got.exit != 0 {
				t.Fatalf("the command after the killed update: exit %d\n%s\nthe killed run:\n%s", got.exit, got.stderr, out)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			equal(t, "the merge's parents", h.parents(h.ref(lineage.ForkRef("alpha"))), tip+"\n"+candidate)
			equal(t, "the notes", fileBody(t, filepath.Join(alpha, "notes.md")), forkNotes("seven", "seven, upstream"))
			equal(t, "the ignored file", fileBody(t, filepath.Join(alpha, ".DS_Store")), "finder\n")
			equal(t, "git status in the worktree", gitIn(t, h, filepath.Join(h.agentx, "worktrees", "alpha"), "status", "--porcelain"), "")
			equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), "")
			equal(t, "what is left in the worktrees directory", strings.Join(hiddenEntries(t, filepath.Join(h.agentx, "worktrees")), " "), "")
		})
	}
}

// TestMarkerSize is how long the conflict markers of a merge are, from the
// lines of the files that conflict: one longer than the longest run of
// one marker character starting a line, and never shorter than git's.
func TestMarkerSize(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		contents []string
		want     int
	}{
		{"no file", nil, 7},
		{"plain text", []string{"one\ntwo\n"}, 7},
		{"a short rule", []string{"Title\n=====\n"}, 7},
		{"a run of seven", []string{"a\n=======\nb\n"}, 8},
		{"a quoted conflict", []string{"<<<<<<<<<< ours\n"}, 11},
		{"a run that is not at the start of a line", []string{"a ========== b\n"}, 7},
		{"a run of mixed characters", []string{"<<<<>>>>\n"}, 7},
		{"the base marker", []string{"|||||||| base\n"}, 9},
		{"the longest of several files", []string{">>>>>>>>\n", "========\n=========\n"}, 10},
		{"a line ending in CRLF", []string{"=======\r\n"}, 8},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			equal(t, "markerSize", markerSize(c.contents), c.want)
		})
	}
}

// TestForkUpdateRefusals is what refuses a fork's update before anything
// is written besides uncommitted edits: a repository nested in its skill
// directory that no ignore rule covers, exit 6, named, and a candidate
// that holds the skill under another upstream directory than the fork's
// base, exit 8. Neither changes a ref, the library, a placement or the
// version file.
func TestForkUpdateRefusals(t *testing.T) {
	t.Parallel()
	h, s, _ := forkUpdateHarness(t)
	h.mustRun("skill", "fork", "alpha")
	s.write("skills/alpha/notes.md", forkNotes("seven", "seven, upstream"))
	s.commit("second version")
	h.mustRun("skill", "check")
	vendored := filepath.Join(h.forkDir("alpha", "alpha"), "vendored")
	writeFile(t, mkdirs(t, filepath.Join(vendored, ".git"), "HEAD"), "ref: refs/heads/main\n")
	was := h.unchangedHome()
	out := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit over a nested repository", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "alpha holds a Git repository at vendored/.git")
	was.check(t, h, "the update refused over a nested repository", 0)

	remove(t, vendored)
	h.accountGit("update-ref", lineage.CandidateRef("alpha"), h.ref(lineage.ManagedRef("beta")))
	was = h.unchangedHome()
	out = h.run("--json", "skill", "update", "alpha")
	equal(t, "exit over a candidate of another directory", out.exit, 8)
	equal(t, "message", h.one(out.stdout, "error")["message"],
		"the update candidate refs/agentx/candidate/alpha holds alpha under another source or directory than its base version")
	was.check(t, h, "the update refused over a candidate of another directory", 0)
}
