package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// updateHarness is a machine with Claude Code and Cursor and a source of
// two skills, both installed from its first commit with a copy in each
// client. alpha lives at skills/alpha-dir, so its upstream directory is not
// its library name. The source has not moved on yet; newVersion commits the
// update a test applies.
func updateHarness(t *testing.T) (h *harness, s *sourceRepo, first string) {
	t.Helper()
	h = newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".cursor"}})
	s = h.newSourceRepo("skills", true)
	s.skill("skills/alpha-dir", "alpha", "The first skill", map[string]string{
		"notes.md": "alpha notes\n", "old.md": "a file the update deletes\n", "scripts/run.sh": "#!/bin/sh\necho run\n",
	})
	s.skill("skills/beta", "beta", "The second skill", map[string]string{"notes.md": "beta notes\n"})
	first = s.commit("first version")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all", "--copy")
	return h, s, first
}

// newVersion commits the second version of alpha: a changed file, an added
// file, a deleted file and a script made executable, and an ignore file and
// an attributes file that name files of the version, which an update lays
// out byte for byte like any other and never applies.
func newVersion(t *testing.T, s *sourceRepo) string {
	t.Helper()
	s.write("skills/alpha-dir/notes.md", "alpha notes, revised upstream\n")
	s.write("skills/alpha-dir/new.md", "a file the update adds\n")
	s.write("skills/alpha-dir/.gitignore", "notes.md\n")
	s.write("skills/alpha-dir/.gitattributes", "*.md text eol=crlf\n")
	s.run("rm", "--quiet", "skills/alpha-dir/old.md")
	s.executable("skills/alpha-dir/scripts/run.sh")
	return s.commit("second version")
}

// secondTree is what alpha's directory holds at the second version.
func secondTree(t *testing.T, s *sourceRepo) map[string]string {
	t.Helper()
	return libraryTree(t, filepath.Join(s.work, "skills", "alpha-dir"))
}

// TestSkillUpdateReplacesAnUnmodifiedSkill applies the update a check found
// to a skill nobody edited. The import branch moves to the candidate, which
// a second invocation reads back with plain git, and the candidate ref is
// gone; the library directory holds the new version, the changed, added
// and deleted files and the exec bit included. Of the two copies, the one
// that held the version replaced is refreshed and the one edited where it
// is kept byte for byte, with the warning and the skipped count. The skill
// reads current at the new base afterwards, skill list shows no update, a
// check right after finds none, and the old import stays reachable through
// the branch's reflog.
func TestSkillUpdateReplacesAnUnmodifiedSkill(t *testing.T) {
	t.Parallel()
	h, s, first := updateHarness(t)
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	second := newVersion(t, s)
	want := secondTree(t, s)
	h.mustRun("skill", "check")
	tip := h.ref(lineage.ManagedRef("alpha"))
	candidate := h.ref(lineage.CandidateRef("alpha"))
	if candidate == "" || candidate == tip {
		t.Fatalf("the check pinned candidate %q against tip %q", candidate, tip)
	}
	before := mutationVersion(t, h)

	out := h.mustRun("--json", "skill", "update", "alpha")
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
	contains(t, "the branch's reflog", h.accountGit("reflog", "show", "--format=%H", lineage.ManagedRef("alpha")), tip)
	lib := filepath.Join(h.library, "alpha")
	sameTree(t, "the library directory", libraryTree(t, lib), want)
	if !executable(t, filepath.Join(lib, "scripts", "run.sh")) {
		t.Error("scripts/run.sh is not executable after the update")
	}
	nothingAt(t, "old.md", filepath.Join(lib, "old.md"))
	sameTree(t, "claude's copy", libraryTree(t, claude), want)
	sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), editedCopyWarning(cursor))
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"updated alpha from "+first[:7]+" to "+second[:7]+", 1 copy placement refreshed, 1 placement skipped")
	ev := h.one(out.stdout, "library_skill")
	equal(t, "name", ev["name"], "alpha")
	equal(t, "state", ev["state"], stateCurrent)
	equal(t, "upstream_commit", ev["upstream_commit"], second)
	if _, ok := ev["candidate"]; ok {
		t.Errorf("the updated skill still carries a candidate: %v", ev["candidate"])
	}
	// The event is the rescan after the update: the refreshed copy, which
	// Cursor sees through Claude Code's directory too, and not Cursor's own
	// edited copy, which no longer holds what the library does.
	equal(t, "placements", strings.Join(placementsOf(t, ev), "|"), "claude-code copy copy|cursor copy copy")
	equal(t, "placement paths", strings.Join(pathsOf(t, ev), "|"), "claude-code "+claude+"|cursor "+claude)
	equal(t, "drift", drift(ev), "")
	for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "journals", journalCount(t, h), 0)

	listed := h.listed("alpha")
	equal(t, "state after the update", listed["state"], stateCurrent)
	equal(t, "upstream_commit after the update", listed["upstream_commit"], second)
	sameEvent(t, "the update's library_skill and skill list's", ev, listed)
	if strings.Contains(h.mustRun("skill", "list").stdout, updateAvailable) {
		t.Error("skill list still shows an update after it was applied")
	}
	check := h.mustRun("--json", "skill", "check")
	if got := h.eventsOfType(check.stdout, "update_available"); len(got) != 0 {
		t.Errorf("a check right after the update found %d updates: %v", len(got), got)
	}
	equal(t, "the candidate ref after a check", h.ref(lineage.CandidateRef("alpha")), "")
}

// sameEvent fails the test unless two events say the same, whatever
// their type: an update's library_skill and skill list's, say, which both
// come from a scan of the machine as it is.
func sameEvent(t *testing.T, what string, got, want jsonEvent) {
	t.Helper()
	strip := func(ev jsonEvent) string {
		c := jsonEvent{}
		for k, v := range ev {
			if k != "type" && k != "schema_version" {
				c[k] = v
			}
		}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	equal(t, what, strip(got), strip(want))
}

// mixedHarness is a source of five skills, all installed from its first
// commit and symlinked into Claude Code, and a second commit after which a
// check finds: an update for alpha, which nobody edited; an update for
// beta, which the test then edits in the library where the update changes
// it too; an update for epsilon, which the test edits in a file the update
// leaves alone; gamma gone from the source; and delta as it was.
func mixedHarness(t *testing.T) (h *harness, s *sourceRepo, first, second string) {
	t.Helper()
	h = newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s = h.newSourceRepo("skills", true)
	for _, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
		s.skill("skills/"+name, name, "The skill "+name, map[string]string{"notes.md": name + " notes\n", "usage.md": name + " usage\n"})
	}
	first = s.commit("first version")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	s.write("skills/alpha/notes.md", "alpha notes, revised\n")
	s.write("skills/beta/notes.md", "beta notes, revised\n")
	s.write("skills/epsilon/notes.md", "epsilon notes, revised\n")
	s.run("rm", "-r", "--quiet", "skills/gamma")
	second = s.commit("second version")
	h.mustRun("skill", "check")
	editLibrary(t, h, "beta", "notes.md", "beta notes, edited here\n")
	editLibrary(t, h, "epsilon", "usage.md", "epsilon usage, edited here\n")
	return h, s, first, second
}

// TestSkillUpdateAllMergesEditsAndLeavesConflictsPending runs update --all
// over a mix: the unmodified skill with an update is updated; the modified
// one whose edit the update leaves alone is merged, keeping the edit on the
// new version and staying modified; the modified one whose edit conflicts
// is left as it was, with a merge pending and a conflict event, and costs
// the run exit code 4 as a refusal of its own would; the upstream-removed
// skill and the one with no update are not touched. A second run, with
// only the conflicting skill left to update, is answered as that skill on
// its own: the merge pending blocks it.
func TestSkillUpdateAllMergesEditsAndLeavesConflictsPending(t *testing.T) {
	t.Parallel()
	h, _, _, second := mixedHarness(t)
	betaCandidate, betaTip := h.ref(lineage.CandidateRef("beta")), h.ref(lineage.ManagedRef("beta"))
	epsilonCandidate := h.ref(lineage.CandidateRef("epsilon"))
	gammaMarker := h.ref(lineage.UpstreamRemovedRef("gamma"))
	deltaTip := h.ref(lineage.ManagedRef("delta"))
	gammaTree := libraryTree(t, filepath.Join(h.library, "gamma"))
	if betaCandidate == "" || epsilonCandidate == "" || gammaMarker == "" {
		t.Fatalf("the check left beta's candidate %q, epsilon's %q and gamma's marker %q", betaCandidate, epsilonCandidate, gammaMarker)
	}

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 4)
	events := h.eventsOfType(out.stdout, "library_skill")
	if len(events) != 2 || events[0]["name"] != "alpha" || events[1]["name"] != "epsilon" {
		t.Fatalf("library_skill events %v, want alpha's and epsilon's", events)
	}
	equal(t, "alpha's state", events[0]["state"], stateCurrent)
	equal(t, "alpha's upstream_commit", events[0]["upstream_commit"], second)
	equal(t, "epsilon's state", events[1]["state"], stateModified)
	equal(t, "epsilon's upstream_commit", events[1]["upstream_commit"], second)
	equal(t, "alpha's placements", strings.Join(placementsOf(t, events[0]), "|"), "claude-code symlink symlink")
	sameEvent(t, "alpha's library_skill and skill list's", events[0], h.listed("alpha"))
	sameEvent(t, "epsilon's library_skill and skill list's", events[1], h.listed("epsilon"))
	conflict := h.one(out.stdout, "conflict")
	equal(t, "the conflict's skill", conflict["name"], "beta")
	equal(t, "the conflict's files", conflictFiles(conflict), "notes.md:1")
	refusal := "beta conflicts with its update in 1 file, so the merge is pending and the library directory was left as it is"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "beta: "+refusal)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "pending_merge")
	equal(t, "message", e["message"], "1 of 3 skills could not be updated: beta: "+refusal)
	equal(t, "hint", e["hint"], "run 'agentx skill resolve beta' to resolve the conflicts, or 'agentx skill resolve beta --abort' to give the merge up")
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, revised\n")
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "epsilon's notes", fileBody(t, filepath.Join(h.library, "epsilon", "notes.md")), "epsilon notes, revised\n")
	equal(t, "epsilon's usage", fileBody(t, filepath.Join(h.library, "epsilon", "usage.md")), "epsilon usage, edited here\n")
	equal(t, "epsilon's branch", h.ref(lineage.ManagedRef("epsilon")), epsilonCandidate)
	equal(t, "epsilon's candidate", h.ref(lineage.CandidateRef("epsilon")), "")
	equal(t, "beta's notes", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes, edited here\n")
	equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), betaCandidate)
	equal(t, "beta's branch", h.ref(lineage.ManagedRef("beta")), betaTip)
	equal(t, "beta's pending merge", h.accountGit("rev-parse", lineage.MergeRef("beta")+"^@"), conflict["mine"].(string)+"\n"+betaCandidate)
	equal(t, "gamma's marker", h.ref(lineage.UpstreamRemovedRef("gamma")), gammaMarker)
	sameTree(t, "gamma's library directory", libraryTree(t, filepath.Join(h.library, "gamma")), gammaTree)
	equal(t, "delta's branch", h.ref(lineage.ManagedRef("delta")), deltaTip)
	equal(t, "journals", journalCount(t, h), 0)

	again := h.run("--json", "skill", "update", "--all")
	equal(t, "exit of the second run", again.exit, 4)
	e = h.one(again.stdout, "error")
	equal(t, "message of the second run", e["message"], "beta has a merge with its update pending, so it cannot be updated until the merge is resolved or given up")
	equal(t, "hint of the second run", e["hint"], "run 'agentx skill resolve beta --abort' to give the merge up; the library directory stays as it is")
	if got := h.eventsOfType(again.stdout, "conflict"); len(got) != 0 {
		t.Errorf("the second run reported conflicts again: %v", got)
	}
	equal(t, "beta's candidate after the second run", h.ref(lineage.CandidateRef("beta")), betaCandidate)
}

// TestSkillUpdateAllTextOfAMixedRun is the text of the run above: one line
// for what was updated, a row for each skill, the merged one saying so,
// then the conflicts of the skill left pending, and the error that names
// it.
func TestSkillUpdateAllTextOfAMixedRun(t *testing.T) {
	t.Parallel()
	h, _, first, second := mixedHarness(t)
	out := h.run("skill", "update", "--all")
	equal(t, "exit", out.exit, 4)
	moved := first[:7] + " -> " + second[:7]
	equal(t, "stdout", out.stdout, "✓ updated 2 skills, 1 edited skill merged cleanly\n"+
		"  alpha    "+moved+"\n"+
		"  epsilon  "+moved+"  edits merged cleanly\n"+
		"beta conflicts with its update from "+first[:7]+" to "+second[:7]+" in 1 file\n"+
		"notes.md:1\n<<<<<<< mine\nbeta notes, edited here\n||||||| base\nbeta notes\n=======\nbeta notes, revised\n>>>>>>> theirs\n")
	contains(t, "stderr", out.stderr, "error: 1 of 3 skills could not be updated: beta: beta conflicts with its update in 1 file")
}

// TestSkillUpdateAllWithOnlyAConflict: a run whose one skill with an
// update conflicts updates nothing, and is answered as that skill on its
// own would be: its conflicts, and exit code 4 with no warning before it.
func TestSkillUpdateAllWithOnlyAConflict(t *testing.T) {
	t.Parallel()
	h, _, _, _ := mixedHarness(t)
	h.mustRun("skill", "update", "alpha")
	h.mustRun("skill", "update", "epsilon")
	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 4)
	equal(t, "the conflict's skill", h.one(out.stdout, "conflict")["name"], "beta")
	equal(t, "message", h.one(out.stdout, "error")["message"],
		"beta conflicts with its update in 1 file, so the merge is pending and the library directory was left as it is")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	if got := h.eventsOfType(out.stdout, "library_skill"); len(got) != 0 {
		t.Errorf("a run that updated nothing reported %v", got)
	}
}

// otherSourceHarness is updateHarness with a second source holding gamma,
// installed too, and a check that found an update for all three skills:
// alpha's second version, beta revised and gamma revised.
func otherSourceHarness(t *testing.T) (h *harness, s, other *sourceRepo) {
	t.Helper()
	h, s, _ = updateHarness(t)
	other = h.newSourceRepo("other", true)
	other.skill("gamma", "gamma", "A skill of the other source", nil)
	other.commit("gamma")
	h.mustRun("skill", "add", other.url)
	newVersion(t, s)
	s.write("skills/beta/notes.md", "beta notes, revised\n")
	s.commit("beta revised")
	other.skill("gamma", "gamma", "A skill of the other source, revised", nil)
	other.commit("gamma revised")
	h.mustRun("skill", "check")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if h.ref(lineage.CandidateRef(name)) == "" {
			t.Fatalf("the check pinned no candidate for %s", name)
		}
	}
	return h, s, other
}

// updatedNames is the skills a run's library_skill events name, in order.
func updatedNames(h *harness, stdout string) string {
	var updated []string
	for _, ev := range h.eventsOfType(stdout, "library_skill") {
		updated = append(updated, ev["name"].(string))
	}
	return strings.Join(updated, ", ")
}

// TestSkillUpdateAllSkipsASkillWhoseSourceWasRemoved: a run over every
// skill skips a skill whose source was removed from this machine, as it
// skips a modified one. A warning says why and names the command that adds
// the source again, the other skills are updated, and the run exits 0 with
// no error: removing a source is the user's own choice, and a check keeps
// the update it found for the skill for as long as the source is gone, so
// a failure here would fail every later run too. The skill keeps its
// version and that update. Updating it by name still refuses with exit
// code 5, and a later run with nothing else to update skips it again and
// still exits 0.
func TestSkillUpdateAllSkipsASkillWhoseSourceWasRemoved(t *testing.T) {
	t.Parallel()
	h, _, other := otherSourceHarness(t)
	gammaTip, gammaCandidate := h.ref(lineage.ManagedRef("gamma")), h.ref(lineage.CandidateRef("gamma"))
	gammaTree := libraryTree(t, filepath.Join(h.library, "gamma"))
	h.mustRun("source", "remove", other.url)
	refusal := "gamma was installed from " + other.url + ", which was removed from this machine, so it is not updated"
	hint := "run 'agentx source add " + other.url + "' to add it again"

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), refusal+"; "+hint)
	if got := h.eventsOfType(out.stdout, "error"); len(got) != 0 {
		t.Errorf("a run that skipped a skill of a removed source failed: %v", got)
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"updated 2 skills, 4 copy placements refreshed, 1 skill from a removed source skipped")
	equal(t, "updated", updatedNames(h, out.stdout), "alpha, beta")
	equal(t, "beta's notes", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes, revised\n")
	equal(t, "gamma's branch", h.ref(lineage.ManagedRef("gamma")), gammaTip)
	equal(t, "gamma's candidate", h.ref(lineage.CandidateRef("gamma")), gammaCandidate)
	sameTree(t, "gamma's library directory", libraryTree(t, filepath.Join(h.library, "gamma")), gammaTree)

	one := h.run("--json", "skill", "update", "gamma")
	equal(t, "exit of an update of gamma by name", one.exit, 5)
	e := h.one(one.stdout, "error")
	equal(t, "code", e["code"], "not_found")
	equal(t, "message", e["message"], refusal)
	equal(t, "hint", e["hint"], hint)

	h.mustRun("skill", "check")
	equal(t, "gamma's candidate after another check", h.ref(lineage.CandidateRef("gamma")), gammaCandidate)
	again := h.run("skill", "update", "--all")
	equal(t, "exit of the next run", again.exit, 0)
	equal(t, "the text of the next run", again.stdout, "no skill was updated, 1 skill from a removed source skipped\n")
	equal(t, "the warning of the next run", again.stderr, "warning: "+refusal+"\n  "+hint+"\n")
	equal(t, "gamma's candidate after the next run", h.ref(lineage.CandidateRef("gamma")), gammaCandidate)
}

// TestSkillUpdateAllReportsEachRefusalAndGoesOn: a run over every skill
// with an update gives up on each skill that refuses, before the lock or
// while it reads the version it lays out, names it in a warning, and
// updates the rest. Here delta's library directory is gone, which is exit
// code 6, and beta's candidate holds no directory of beta's, which is the
// account repo's exit code 8; refusals that disagree end the run with 6,
// the hint of a mixed run and an error naming every skill it gave up on.
// A skill of a removed source is skipped all the same and is not among
// them.
func TestSkillUpdateAllReportsEachRefusalAndGoesOn(t *testing.T) {
	t.Parallel()
	h, s, other := otherSourceHarness(t)
	h.mustRun("skill", "update", "alpha")
	s.skill("skills/delta", "delta", "The fourth skill", nil)
	s.commit("delta")
	h.mustRun("skill", "add", s.url, "--skill", "delta", "--fetch")
	s.write("skills/alpha-dir/notes.md", "alpha notes, revised again\n")
	s.skill("skills/delta", "delta", "The fourth skill, revised", nil)
	s.commit("alpha and delta revised")
	h.mustRun("skill", "check")
	h.mustRun("source", "remove", other.url)
	for _, name := range []string{"alpha", "beta", "delta", "gamma"} {
		if h.ref(lineage.CandidateRef(name)) == "" {
			t.Fatalf("the check pinned no candidate for %s", name)
		}
	}
	remove(t, filepath.Join(h.library, "delta"))
	// beta's candidate carries its lineage over a tree that holds delta's
	// directory and not beta's.
	message := h.accountGit("log", "-1", "--format=%B", lineage.CandidateRef("beta"))
	elsewhere := h.accountGit("commit-tree", h.accountGit("rev-parse", lineage.ManagedRef("delta")+"^{tree}"), "-m", message)
	h.accountGit("update-ref", lineage.CandidateRef("beta"), elsewhere)
	betaTip := h.ref(lineage.ManagedRef("beta"))

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 6)
	delta := "delta is managed in the account repo but the library holds no skill directory for it, so there is nothing to update"
	beta := `not an import commit: refs/heads/managed/beta holds "delta" beside beta`
	gamma := "gamma was installed from " + other.url + ", which was removed from this machine, so it is not updated; run 'agentx source add " + other.url + "' to add it again"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), strings.Join([]string{"delta: " + delta, gamma, "beta: " + beta}, "\n"))
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "refused")
	equal(t, "message", e["message"], "2 of 4 skills could not be updated: delta: "+delta+"; beta: "+beta)
	equal(t, "hint", e["hint"], "run 'agentx skill list' to see which skills have an update, then update the rest one at a time")
	// The result of a run that refused a skill is its error, with no copy
	// counts: what it did for the skills it updated is in their
	// library_skill events and its warnings.
	r := h.one(out.stdout, "result")
	equal(t, "ok", r["ok"], false)
	equal(t, "summary", r["summary"], e["message"])
	equal(t, "updated", updatedNames(h, out.stdout), "alpha")
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, revised again\n")
	equal(t, "beta's branch", h.ref(lineage.ManagedRef("beta")), betaTip)
	equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), elsewhere)
	equal(t, "beta's notes", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes\n")
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillUpdateAllRefusesASkillGitCannotRecord: in a run over every
// skill, a skill that holds something git cannot record is not skipped as
// a modified skill is but refused, since what an update would discard
// there has no record anywhere: the warning and the error name the path,
// and the other skill is updated.
func TestSkillUpdateAllRefusesASkillGitCannotRecord(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	newVersion(t, s)
	s.write("skills/beta/notes.md", "beta notes, revised\n")
	s.commit("beta revised")
	h.mustRun("skill", "check")
	nested := filepath.Join(h.library, "alpha", "vendored", ".git")
	writeFile(t, mkdirs(t, nested, "HEAD"), "ref: refs/heads/main\n")
	alphaCandidate := h.ref(lineage.CandidateRef("alpha"))

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 6)
	refusal := "alpha holds " + nested + ", which git cannot record"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "alpha: "+refusal)
	equal(t, "message", h.one(out.stdout, "error")["message"], "1 of 2 skills could not be updated: alpha: "+refusal)
	var updated []string
	for _, ev := range h.eventsOfType(out.stdout, "library_skill") {
		updated = append(updated, ev["name"].(string))
	}
	equal(t, "updated", strings.Join(updated, ", "), "beta")
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), alphaCandidate)
	equal(t, "alpha's nested repository", fileBody(t, filepath.Join(nested, "HEAD")), "ref: refs/heads/main\n")
	equal(t, "beta's notes", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes, revised\n")
}

// TestSkillUpdateAllRefreshesEachSkillsCopies runs update --all over two
// skills with an update, each with a copy in Claude Code and in Cursor, and
// beta's Cursor copy edited in place. Every copy that held the version
// replaced is refreshed, beta's own as much as alpha's, and the edited one
// is kept byte for byte with the warning for a kept copy. The result adds
// up what the run did to copies over every skill, and in text each row says
// what the update of its skill did.
func TestSkillUpdateAllRefreshesEachSkillsCopies(t *testing.T) {
	t.Parallel()
	for _, json := range []bool{false, true} {
		name := "text"
		if json {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, s, first := updateHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills")
			cursor := filepath.Join(h.home, ".cursor", "skills")
			betaCursor := filepath.Join(cursor, "beta")
			editCopy(t, betaCursor)
			edited := libraryTree(t, betaCursor)
			s.write("skills/beta/notes.md", "beta notes, revised\n")
			second := newVersion(t, s)
			alphaWant := secondTree(t, s)
			betaWant := libraryTree(t, filepath.Join(s.work, "skills", "beta"))
			h.mustRun("skill", "check")
			candidates := map[string]string{}
			for _, name := range []string{"alpha", "beta"} {
				if candidates[name] = h.ref(lineage.CandidateRef(name)); candidates[name] == "" {
					t.Fatalf("the check pinned no candidate for %s", name)
				}
			}

			args := []string{"skill", "update", "--all"}
			if json {
				args = append([]string{"--json"}, args...)
			}
			out := h.mustRun(args...)
			for name, candidate := range candidates {
				equal(t, name+"'s import branch", h.ref(lineage.ManagedRef(name)), candidate)
				equal(t, name+"'s candidate", h.ref(lineage.CandidateRef(name)), "")
			}
			sameTree(t, "alpha's library directory", libraryTree(t, filepath.Join(h.library, "alpha")), alphaWant)
			sameTree(t, "alpha's claude copy", libraryTree(t, filepath.Join(claude, "alpha")), alphaWant)
			sameTree(t, "alpha's cursor copy", libraryTree(t, filepath.Join(cursor, "alpha")), alphaWant)
			sameTree(t, "beta's library directory", libraryTree(t, filepath.Join(h.library, "beta")), betaWant)
			sameTree(t, "beta's claude copy", libraryTree(t, filepath.Join(claude, "beta")), betaWant)
			sameTree(t, "beta's cursor copy", libraryTree(t, betaCursor), edited)
			for _, dir := range []string{h.library, claude, cursor} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "journals", journalCount(t, h), 0)

			kept := keptCopyWarning(betaCursor, "beta", "agentx skill remove beta --from cursor", "agentx skill place beta --to cursor --copy")
			if json {
				equal(t, "summary", h.one(out.stdout, "result")["summary"], "updated 2 skills, 3 copy placements refreshed, 1 placement skipped")
				equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), kept)
				var updated []string
				for _, ev := range h.eventsOfType(out.stdout, "library_skill") {
					updated = append(updated, ev["name"].(string))
				}
				equal(t, "updated", strings.Join(updated, ", "), "alpha, beta")
				return
			}
			warning, wayOn := keptCopyLines(betaCursor, "beta", "agentx skill remove beta --from cursor", "agentx skill place beta --to cursor --copy")
			equal(t, "stderr", out.stderr, "warning: "+warning+"\n  "+wayOn+"\n")
			moved := first[:7] + " -> " + second[:7]
			equal(t, "the text", out.stdout, "✓ updated 2 skills, 3 copy placements refreshed, 1 placement skipped\n"+
				"  alpha  "+moved+"  2 copy placements refreshed\n"+
				"  beta   "+moved+"  1 copy placement refreshed, 1 placement skipped\n")
		})
	}
}

// TestSkillUpdateUsage: a name and --all are the two forms, and one of them
// is needed.
func TestSkillUpdateUsage(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, c := range []struct {
		args    []string
		message string
	}{
		{[]string{"skill", "update"}, "no skill to update"},
		{[]string{"skill", "update", "alpha", "--all"}, "skill update takes a skill name or --all, not both"},
	} {
		out := h.run(append([]string{"--json"}, c.args...)...)
		equal(t, strings.Join(c.args, " ")+": exit", out.exit, 1)
		e := h.one(out.stdout, "error")
		equal(t, "message", e["message"], c.message)
		equal(t, "hint", e["hint"], "name the skill to update, or run 'agentx skill update --all' to update every skill the last check found an update for")
	}
	out := h.run("skill", "update", "--all")
	equal(t, "exit of --all on an empty machine", out.exit, 0)
	contains(t, "stdout", out.stdout, "Nothing to update")
}

// checked commits alpha's second version and runs a check, which pins it
// as alpha's candidate.
func checked(t *testing.T, h *harness, s *sourceRepo) {
	t.Helper()
	newVersion(t, s)
	h.mustRun("skill", "check")
}

// upstreamRemoved deletes beta from the source and runs a check, which
// marks it upstream removed.
func upstreamRemoved(t *testing.T, h *harness, s *sourceRepo) {
	t.Helper()
	s.run("rm", "-r", "--quiet", "skills/beta")
	s.commit("beta removed")
	h.mustRun("skill", "check")
}

// withoutLineage points beta's import branch at a commit of the version it
// holds whose message carries no lineage.
func withoutLineage(t *testing.T, h *harness, _ *sourceRepo) {
	t.Helper()
	tree := h.accountGit("rev-parse", lineage.ManagedRef("beta")+"^{tree}")
	h.accountGit("update-ref", lineage.ManagedRef("beta"), h.accountGit("commit-tree", tree, "-m", "no lineage"))
}

// lineagelessCandidate runs a check that pins alpha's second version as its
// candidate, then points the candidate ref at a commit of the same tree
// whose message carries no lineage, which agentx cannot read as an update.
func lineagelessCandidate(t *testing.T, h *harness, s *sourceRepo) {
	t.Helper()
	checked(t, h, s)
	tree := h.accountGit("rev-parse", lineage.CandidateRef("alpha")+"^{tree}")
	h.accountGit("update-ref", lineage.CandidateRef("alpha"), h.accountGit("commit-tree", tree, "-p", lineage.ManagedRef("alpha"), "-m", "no lineage"))
}

// TestSkillUpdateRefusesInOrder is every skill update refuses, one name at
// a time, each with its code, its message and its hint, and each where the
// order of the checks puts it: an import branch agentx cannot read before
// a merge pending, a merge pending before a removed source, whether or not
// there is an update, a removed source before an upstream that no longer
// holds the skill, and both before whether there is an update at all,
// which comes before whether the library entry is a symlink, and that
// before whether the skill holds something git cannot record. A skill with
// no update, or with a candidate whose lineage agentx cannot read, is
// nothing to do and exits 0. None of them changes a ref, the library or a
// placement, or leaves a journal.
func TestSkillUpdateRefusesInOrder(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		skill string
		setup func(t *testing.T, h *harness, s *sourceRepo)
		exit  int
		// message and hint, in which %LIB% is the library and %URL% the
		// source's URL
		message, hint string
	}{
		{
			name: "a name the library does not hold", skill: "nosuch", exit: 5,
			message: `the library holds no skill called "nosuch" at %LIB%`, hint: "run 'agentx skill list' to see what the library holds",
		},
		{
			name: "an unmanaged skill", skill: "mine", exit: 6,
			setup: func(t *testing.T, h *harness, _ *sourceRepo) {
				writeFile(t, mkdirs(t, filepath.Join(h.library, "mine"), "SKILL.md"), skill("mine", "A skill of my own"))
			},
			message: "mine is not managed by agentx, so it has no upstream to update from", hint: "run 'agentx skill list' to see which skills are managed",
		},
		{
			name: "a fork", skill: "forky", exit: 6,
			setup: func(t *testing.T, h *harness, _ *sourceRepo) {
				h.accountGit("update-ref", lineage.ForkRef("forky"), h.ref(lineage.ManagedRef("alpha")))
				writeFile(t, mkdirs(t, filepath.Join(h.library, "forky"), "SKILL.md"), skill("forky", "A fork"))
			},
			message: "forky is a fork on this machine", hint: "a fork's versions are its own history; this command works on a managed skill",
		},
		{
			name: "a managed skill whose library directory is gone", skill: "beta", exit: 6,
			setup:   func(t *testing.T, h *harness, _ *sourceRepo) { remove(t, filepath.Join(h.library, "beta")) },
			message: "beta is managed in the account repo but the library holds no skill directory for it, so there is nothing to update",
			hint:    "run 'agentx skill add %URL% --skill beta' to install it again, or 'agentx skill remove beta' to stop managing it",
		},
		{
			name: "a managed skill whose import branch records no version agentx can read", skill: "beta", exit: 6,
			setup:   withoutLineage,
			message: "the import branch refs/heads/managed/beta records no version agentx can read",
			hint:    "run 'agentx doctor' and check the account repo it names",
		},
		{
			// A branch with no lineage names no source, which no settings
			// entry holds either.
			name: "a managed skill whose import branch records no version agentx can read, from a removed source", skill: "beta", exit: 6,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				withoutLineage(t, h, s)
				h.mustRun("source", "remove", s.url)
			},
			message: "the import branch refs/heads/managed/beta records no version agentx can read",
			hint:    "run 'agentx doctor' and check the account repo it names",
		},
		{
			name: "a skill with an update whose source was removed", skill: "alpha", exit: 5,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				checked(t, h, s)
				h.mustRun("source", "remove", s.url)
			},
			message: "alpha was installed from %URL%, which was removed from this machine, so it is not updated",
			hint:    "run 'agentx source add %URL%' to add it again",
		},
		{
			name: "an upstream-removed skill whose source was removed", skill: "beta", exit: 5,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				upstreamRemoved(t, h, s)
				h.mustRun("source", "remove", s.url)
			},
			message: "beta was installed from %URL%, which was removed from this machine, so it is not updated",
			hint:    "run 'agentx source add %URL%' to add it again",
		},
		{
			name: "an upstream-removed skill", skill: "beta", exit: 6,
			setup:   upstreamRemoved,
			message: "the last update check found that the source of beta no longer holds it, so it is kept as it is and never updated",
			hint:    "run 'agentx skill check' once the source holds it again, or 'agentx skill remove beta' to remove it",
		},
		{
			name: "an upstream-removed skill that was edited", skill: "beta", exit: 6,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				upstreamRemoved(t, h, s)
				editLibrary(t, h, "beta", "notes.md", "beta notes, edited\n")
			},
			message: "the last update check found that the source of beta no longer holds it, so it is kept as it is and never updated",
			hint:    "run 'agentx skill check' once the source holds it again, or 'agentx skill remove beta' to remove it",
		},
		{
			name: "a skill with no update", skill: "alpha", exit: 0,
			message: "alpha is up to date as of the last update check; run 'agentx skill check' to look again",
		},
		{
			name: "an edited skill with no update", skill: "alpha", exit: 0,
			setup: func(t *testing.T, h *harness, _ *sourceRepo) {
				editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited\n")
			},
			message: "alpha is up to date as of the last update check; run 'agentx skill check' to look again",
		},
		{
			name: "a skill whose candidate carries no lineage", skill: "alpha", exit: 0,
			setup:   lineagelessCandidate,
			message: "alpha is up to date as of the last update check; run 'agentx skill check' to look again",
		},
		{
			name: "an edited skill whose candidate carries no lineage", skill: "alpha", exit: 0,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				lineagelessCandidate(t, h, s)
				editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited\n")
			},
			message: "alpha is up to date as of the last update check; run 'agentx skill check' to look again",
		},
		{
			name: "a skill with an update that holds what git cannot record", skill: "alpha", exit: 6,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				checked(t, h, s)
				writeFile(t, mkdirs(t, filepath.Join(h.library, "alpha", "vendored", ".git"), "HEAD"), "ref: refs/heads/main\n")
				editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited\n")
			},
			message: "alpha holds %LIB%/alpha/vendored/.git, which git cannot record",
			hint:    "an update would discard it with no record of it anywhere; move it out of the skill, then run 'agentx skill update alpha' again",
		},
		{
			name: "a skill with a merge pending", skill: "alpha", exit: 4,
			setup:   pendingMerge,
			message: "alpha has a merge with its update pending, so it cannot be updated until the merge is resolved or given up",
			hint:    "run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is",
		},
		{
			name: "a skill with a merge pending whose source was removed", skill: "alpha", exit: 4,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				pendingMerge(t, h, s)
				h.mustRun("source", "remove", s.url)
			},
			message: "alpha has a merge with its update pending, so it cannot be updated until the merge is resolved or given up",
			hint:    "run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is",
		},
		{
			name: "a skill with a merge pending and no update left", skill: "alpha", exit: 4,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				pendingMerge(t, h, s)
				h.accountGit("update-ref", "-d", lineage.CandidateRef("alpha"))
			},
			message: "alpha has a merge with its update pending, so it cannot be updated until the merge is resolved or given up",
			hint:    "run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is",
		},
		{
			name: "a skill whose library entry is a symlink", skill: "alpha", exit: 6,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				checked(t, h, s)
				lib := filepath.Join(h.library, "alpha")
				if err := os.Rename(lib, filepath.Join(h.home, "dev-alpha")); err != nil {
					t.Fatal(err)
				}
				link(t, filepath.Join(h.home, "dev-alpha"), lib)
			},
			message: "%LIB%/alpha is a symlink to %HOME%/dev-alpha; an update replaces the library directory and would drop the link without touching the files it leads to",
			hint:    "replace the link with the directory it points to, then run 'agentx skill update alpha' again",
		},
		{
			// What the link leads to is not judged: an edit there is no
			// reason to send the user to a revert, which refuses the link
			// too, nor is what git cannot record.
			name: "a skill whose library entry is a symlink to an edited directory", skill: "alpha", exit: 6,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				checked(t, h, s)
				lib := filepath.Join(h.library, "alpha")
				writeFile(t, filepath.Join(lib, "notes.md"), "alpha notes, edited\n")
				writeFile(t, mkdirs(t, filepath.Join(lib, "vendored", ".git"), "HEAD"), "ref: refs/heads/main\n")
				if err := os.Rename(lib, filepath.Join(h.home, "dev-alpha")); err != nil {
					t.Fatal(err)
				}
				link(t, filepath.Join(h.home, "dev-alpha"), lib)
			},
			message: "%LIB%/alpha is a symlink to %HOME%/dev-alpha; an update replaces the library directory and would drop the link without touching the files it leads to",
			hint:    "replace the link with the directory it points to, then run 'agentx skill update alpha' again",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			if c.setup != nil {
				c.setup(t, h, s)
			}
			refs := h.accountGit("for-each-ref", "--format=%(refname) %(objectname)")
			library := onDisk(t, h.library)
			expand := strings.NewReplacer("%LIB%", h.library, "%URL%", s.url, "%HOME%", h.home).Replace

			out := h.run("--json", "skill", "update", c.skill)
			equal(t, "exit", out.exit, c.exit)
			if c.exit == 0 {
				equal(t, "summary", h.one(out.stdout, "result")["summary"], expand(c.message))
				if got := h.eventsOfType(out.stdout, "library_skill"); len(got) != 0 {
					t.Errorf("a run that updated nothing reported %v", got)
				}
			} else {
				e := h.one(out.stdout, "error")
				equal(t, "message", e["message"], expand(c.message))
				equal(t, "hint", e["hint"], expand(c.hint))
			}
			equal(t, "the refs", h.accountGit("for-each-ref", "--format=%(refname) %(objectname)"), refs)
			equal(t, "the library", onDisk(t, h.library), library)
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
		})
	}
}

// TestSkillUpdateRefusesWhatChangedBeforeTheLock: what an update replaces
// is what it read when it began. A git wrapper changes one input while the
// update reads the candidate's version, after the skill was judged and
// before the lock is taken: an edit of the library directory, a check that
// moves or drops the candidate, a branch moved by another command, a fork
// of the name made meanwhile, a merge another update of the skill left
// pending, a check that finds the source no longer holds the skill, and the
// source removed from this machine. The update then
// refuses under the lock, before it writes a journal, and loses nothing:
// the edit is there, every ref holds what the other writer wrote, nothing
// is left beside the library or a copy, and the version file is not bumped
// for a run that changed nothing.
func TestSkillUpdateRefusesWhatChangedBeforeTheLock(t *testing.T) {
	t.Parallel()
	edited := "an edit made meanwhile\n"
	for _, c := range []struct {
		name string
		// change is the shell line the wrapper runs, given the real git
		// with the account repo, and the commit beta's branch holds, which
		// stands for a commit another writer wrote
		change func(t *testing.T, h *harness, s *sourceRepo, git, other string) string
		exit   int // 6 when left out
		// branch, candidate, fork and marker are what alpha's import
		// branch, candidate, fork branch and upstream-removed marker hold
		// afterwards: "tip", "candidate", "other" or ""
		branch, candidate, fork, marker string
		notes                           string // what alpha's notes.md holds afterwards
		// message and hint, in which %URL% is the source's URL
		message, hint string
	}{
		{
			name: "the library directory is edited",
			change: func(_ *testing.T, h *harness, _ *sourceRepo, _, _ string) string {
				return `printf 'an edit made meanwhile\n' > ` + shellWord(filepath.Join(h.library, "alpha", "notes.md"))
			},
			branch: "tip", candidate: "candidate", notes: edited,
			message: "alpha changed while it was being updated, so nothing was changed",
			hint:    "run 'agentx skill update alpha' again to update it as it is now",
		},
		{
			name: "another update of the skill leaves a merge pending",
			change: func(_ *testing.T, _ *harness, _ *sourceRepo, git, other string) string {
				return git + ` update-ref ` + lineage.MergeRef("alpha") + ` ` + other
			},
			exit:   4,
			branch: "tip", candidate: "candidate", notes: "alpha notes\n",
			message: "alpha has a merge with its update pending, so it cannot be updated until the merge is resolved or given up",
			hint:    "run 'agentx skill resolve alpha --abort' to give the merge up; the library directory stays as it is",
		},
		{
			name: "a check moves the candidate",
			change: func(_ *testing.T, _ *harness, _ *sourceRepo, git, other string) string {
				return git + ` update-ref ` + lineage.CandidateRef("alpha") + ` ` + other
			},
			branch: "tip", candidate: "other", notes: "alpha notes\n",
			message: "the update candidate refs/agentx/candidate/alpha moved while alpha was being updated, so nothing was changed",
			hint:    "run 'agentx skill update alpha' again to apply the update the last check found",
		},
		{
			name: "a check drops the candidate",
			change: func(_ *testing.T, _ *harness, _ *sourceRepo, git, _ string) string {
				return git + ` update-ref -d ` + lineage.CandidateRef("alpha")
			},
			branch: "tip", candidate: "", notes: "alpha notes\n",
			message: "the update candidate refs/agentx/candidate/alpha moved while alpha was being updated, so nothing was changed",
			hint:    "run 'agentx skill update alpha' again to apply the update the last check found",
		},
		{
			name: "the import branch moves",
			change: func(_ *testing.T, _ *harness, _ *sourceRepo, git, other string) string {
				return git + ` update-ref ` + lineage.ManagedRef("alpha") + ` ` + other
			},
			branch: "other", candidate: "candidate", notes: "alpha notes\n",
			message: "the import branch refs/heads/managed/alpha moved while alpha was being updated, so nothing was changed",
			hint:    "run 'agentx skill update alpha' again",
		},
		{
			name: "a fork of the name appears",
			change: func(_ *testing.T, _ *harness, _ *sourceRepo, git, other string) string {
				return git + ` update-ref ` + lineage.ForkRef("alpha") + ` ` + other
			},
			branch: "tip", candidate: "candidate", fork: "other", notes: "alpha notes\n",
			message: "the import branch refs/heads/managed/alpha moved while alpha was being updated, so nothing was changed",
			hint:    "run 'agentx skill update alpha' again",
		},
		{
			// A check that marks a skill upstream removed also deletes its
			// candidate; the marker is what the update answers for.
			name: "a check finds the source no longer holds the skill",
			change: func(_ *testing.T, _ *harness, _ *sourceRepo, git, other string) string {
				return git + ` update-ref ` + lineage.UpstreamRemovedRef("alpha") + ` ` + other + ` && ` +
					git + ` update-ref -d ` + lineage.CandidateRef("alpha")
			},
			branch: "tip", candidate: "", marker: "other", notes: "alpha notes\n",
			message: "the last update check found that the source of alpha no longer holds it, so it is kept as it is and never updated",
			hint:    "run 'agentx skill check' once the source holds it again, or 'agentx skill remove alpha' to remove it",
		},
		{
			name: "the source is removed",
			change: func(t *testing.T, h *harness, s *sourceRepo, _, _ string) string {
				return removingSource(t, h, s.url)
			},
			exit:   5,
			branch: "tip", candidate: "candidate", notes: "alpha notes\n",
			message: "alpha was installed from %URL%, which was removed from this machine, so it is not updated",
			hint:    "run 'agentx source add %URL%' to add it again",
		},
		{
			// Under the lock as before it, a removed source is answered
			// for ahead of the marker.
			name: "a check finds the source no longer holds the skill, and the source is removed",
			change: func(t *testing.T, h *harness, s *sourceRepo, git, other string) string {
				return git + ` update-ref ` + lineage.UpstreamRemovedRef("alpha") + ` ` + other + ` && ` +
					git + ` update-ref -d ` + lineage.CandidateRef("alpha") + ` && ` + removingSource(t, h, s.url)
			},
			exit:   5,
			branch: "tip", candidate: "", marker: "other", notes: "alpha notes\n",
			message: "alpha was installed from %URL%, which was removed from this machine, so it is not updated",
			hint:    "run 'agentx source add %URL%' to add it again",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			checked(t, h, s)
			held := map[string]string{
				"tip": h.ref(lineage.ManagedRef("alpha")), "candidate": h.ref(lineage.CandidateRef("alpha")),
				"other": h.ref(lineage.ManagedRef("beta")), "": "",
			}
			real, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			git := real + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx))
			change := c.change(t, h, s, git, held["other"])
			before := mutationVersion(t, h)
			stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) `+change+` || exit 1 ;;
esac
exec `+real+` "$@"
`)
			out := h.run("--json", "skill", "update", "alpha")
			exit := c.exit
			if exit == 0 {
				exit = 6
			}
			equal(t, "exit", out.exit, exit)
			expand := strings.NewReplacer("%URL%", s.url).Replace
			e := h.one(out.stdout, "error")
			equal(t, "message", e["message"], expand(c.message))
			equal(t, "hint", e["hint"], expand(c.hint))
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), held[c.branch])
			equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), held[c.candidate])
			equal(t, "the fork branch", h.ref(lineage.ForkRef("alpha")), held[c.fork])
			equal(t, "the upstream-removed marker", h.ref(lineage.UpstreamRemovedRef("alpha")), held[c.marker])
			equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), c.notes)
			nothingAt(t, "new.md", filepath.Join(h.library, "alpha", "new.md"))
			equal(t, "claude's notes", fileBody(t, filepath.Join(h.home, ".claude", "skills", "alpha", "notes.md")), "alpha notes\n")
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "mutations", mutationVersion(t, h), before)
			for _, dir := range []string{h.library, filepath.Join(h.home, ".claude", "skills"), filepath.Join(h.home, ".cursor", "skills")} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
		})
	}
}

// removingSource is a shell line that leaves the settings of h's agentx
// home without the source at url, as removing it does, for a git wrapper to
// run while a command is under way.
func removingSource(t *testing.T, h *harness, url string) string {
	t.Helper()
	settings, err := home.LoadSettings(h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	settings.RemoveSource(url)
	b, err := home.MarshalSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	without := filepath.Join(t.TempDir(), "settings.json")
	writeFile(t, without, string(b))
	cp, err := exec.LookPath("cp")
	if err != nil {
		t.Fatal(err)
	}
	return cp + " " + shellWord(without) + " " + shellWord(home.SettingsPath(h.agentx))
}

// TestSkillUpdateAllSkipsASkillWhoseSourceIsRemovedBeforeTheLock: a source
// removed while a run over every skill reads the versions it lays out,
// after its skills were judged and before the lock, is found under the
// lock, and its skill is skipped there as it would have been before: the
// same warning, the other skills updated, no error and exit code 0. The
// skill keeps its version and its candidate, and nothing of its update is
// staged or left behind.
func TestSkillUpdateAllSkipsASkillWhoseSourceIsRemovedBeforeTheLock(t *testing.T) {
	t.Parallel()
	h, _, other := otherSourceHarness(t)
	gammaTip, gammaCandidate := h.ref(lineage.ManagedRef("gamma")), h.ref(lineage.CandidateRef("gamma"))
	gammaTree := libraryTree(t, filepath.Join(h.library, "gamma"))
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) `+removingSource(t, h, other.url)+` || exit 1 ;;
esac
exec `+real+` "$@"
`)

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		"gamma was installed from "+other.url+", which was removed from this machine, so it is not updated; run 'agentx source add "+other.url+"' to add it again")
	if got := h.eventsOfType(out.stdout, "error"); len(got) != 0 {
		t.Errorf("a run that skipped a skill of a removed source failed: %v", got)
	}
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"updated 2 skills, 4 copy placements refreshed, 1 skill from a removed source skipped")
	equal(t, "updated", updatedNames(h, out.stdout), "alpha, beta")
	equal(t, "gamma's branch", h.ref(lineage.ManagedRef("gamma")), gammaTip)
	equal(t, "gamma's candidate", h.ref(lineage.CandidateRef("gamma")), gammaCandidate)
	sameTree(t, "gamma's library directory", libraryTree(t, filepath.Join(h.library, "gamma")), gammaTree)
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
}

// TestSkillUpdateAllGoesOnPastASkillThatChangedBeforeTheLock: in a run
// over every skill, one skill whose library directory is edited while the
// run reads the versions it lays out is refused under the lock on its own.
// Its warning names it, the other skill is updated all the same, copies
// and all, and the run ends with the refusal's exit code and an error
// naming it. The refused skill keeps its edit, its branch and its
// candidate, and nothing of its update is staged or left behind.
func TestSkillUpdateAllGoesOnPastASkillThatChangedBeforeTheLock(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	newVersion(t, s)
	s.write("skills/beta/notes.md", "beta notes, revised\n")
	s.commit("beta revised")
	h.mustRun("skill", "check")
	alphaTip, alphaCandidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	betaCandidate := h.ref(lineage.CandidateRef("beta"))
	if alphaCandidate == "" || betaCandidate == "" {
		t.Fatalf("the check pinned candidates %q and %q", alphaCandidate, betaCandidate)
	}
	before := mutationVersion(t, h)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(h.library, "alpha", "notes.md")
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) printf 'an edit made meanwhile\n' > `+shellWord(notes)+` || exit 1 ;;
esac
exec `+real+` "$@"
`)

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 6)
	refusal := "alpha changed while it was being updated, so nothing was changed"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "alpha: "+refusal)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "1 of 2 skills could not be updated: alpha: "+refusal)
	equal(t, "hint", e["hint"], "run 'agentx skill update alpha' again to update it as it is now")
	var updated []string
	for _, ev := range h.eventsOfType(out.stdout, "library_skill") {
		updated = append(updated, ev["name"].(string))
	}
	equal(t, "updated", strings.Join(updated, ", "), "beta")

	claude := filepath.Join(h.home, ".claude", "skills")
	cursor := filepath.Join(h.home, ".cursor", "skills")
	equal(t, "beta's import branch", h.ref(lineage.ManagedRef("beta")), betaCandidate)
	equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), "")
	for _, dir := range []string{h.library, claude, cursor} {
		equal(t, "beta's notes in "+dir, fileBody(t, filepath.Join(dir, "beta", "notes.md")), "beta notes, revised\n")
	}
	equal(t, "alpha's import branch", h.ref(lineage.ManagedRef("alpha")), alphaTip)
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), alphaCandidate)
	equal(t, "alpha's notes", fileBody(t, notes), "an edit made meanwhile\n")
	nothingAt(t, "alpha's new.md", filepath.Join(h.library, "alpha", "new.md"))
	for _, dir := range []string{claude, cursor} {
		equal(t, "alpha's notes in "+dir, fileBody(t, filepath.Join(dir, "alpha", "notes.md")), "alpha notes\n")
	}
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	for _, dir := range []string{h.library, claude, cursor} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
}

// TestSkillUpdateKeepsTheLocalNameThroughAnUpstreamRename: a newer version
// whose SKILL.md names the skill otherwise is applied all the same. The
// skill keeps its library name, its import branch and its placements, its
// SKILL.md is laid out as the upstream wrote it, and the update says so in
// the warning the check gave.
func TestSkillUpdateKeepsTheLocalNameThroughAnUpstreamRename(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	s.skill("skills/alpha-dir", "alpha-renamed", "The first skill, renamed upstream", nil)
	s.commit("alpha renamed")
	h.mustRun("skill", "check")
	candidate := h.ref(lineage.CandidateRef("alpha"))

	out := h.mustRun("--json", "skill", "update", "alpha")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		`alpha: the update names the skill "alpha-renamed"; updating it keeps the name alpha`)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "a branch of the new name", h.ref(lineage.ManagedRef("alpha-renamed")), "")
	nothingAt(t, "a library directory of the new name", filepath.Join(h.library, "alpha-renamed"))
	contains(t, "the SKILL.md", fileBody(t, filepath.Join(h.library, "alpha", "SKILL.md")), "name: alpha-renamed\n")
	sameTree(t, "claude's copy", libraryTree(t, filepath.Join(h.home, ".claude", "skills", "alpha")), libraryTree(t, filepath.Join(h.library, "alpha")))
	ev := h.one(out.stdout, "library_skill")
	equal(t, "name", ev["name"], "alpha")
	equal(t, "state", ev["state"], stateCurrent)
}

// TestSkillUpdateOfASkillAtTheRootOfItsSource: a skill that is its whole
// repository has the repository's name as its upstream directory, and
// updates like any other. The text says so on one line, and says a skill
// with no update is up to date.
func TestSkillUpdateOfASkillAtTheRootOfItsSource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("rooted", true)
	s.skill("", "rooted", "A skill at the root", map[string]string{"notes.md": "root notes\n"})
	first := s.commit("first version")
	h.mustRun("skill", "add", s.url)
	equal(t, "the text with no update", h.mustRun("skill", "update", "rooted").stdout,
		"rooted is up to date as of the last update check; run agentx skill check to look again\n")
	s.write("notes.md", "root notes, revised\n")
	second := s.commit("second version")
	h.mustRun("skill", "check")
	candidate := h.ref(lineage.CandidateRef("rooted"))
	if candidate == "" {
		t.Fatal("the check pinned no candidate")
	}

	out := h.mustRun("skill", "update", "rooted")
	equal(t, "the text", out.stdout, "✓ updated rooted from "+first[:7]+" to "+second[:7]+"\n")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("rooted")), candidate)
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "rooted")), map[string]string{
		"SKILL.md": fileBody(t, filepath.Join(s.work, "SKILL.md")), "notes.md": "root notes, revised\n",
	})
	equal(t, "state", h.listed("rooted")["state"], stateCurrent)
}

// updateChildEnv marks the process the crash tests of skill update start,
// which updates the skill it names against the parent's temporary home and
// is killed in the middle of it.
const updateChildEnv = "AGENTX_TEST_UPDATE_CHILD"

// TestUpdateChildProcess is not a test: it is the body of that process. It
// does nothing when the variable that marks it is not set.
func TestUpdateChildProcess(t *testing.T) {
	name := os.Getenv(updateChildEnv)
	if name == "" {
		t.Skip("not the update child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"skill", "update", name}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// killedUpdateScript kills the update the moment its journal is on disk:
// the first git it runs after writing the journal reads the refs of the
// journal's first transaction, before any ref or path changed.
const killedUpdateScript = `
for f in %MUTATIONS%/*.json; do
	if [ -e "$f" ]; then
		kill -9 $PPID
		exit 1
	fi
done
exec %GIT% "$@"
`

// lastWriteScript lets the update run to its last live write, the deletion
// of the candidate ref, which is the journal's second update-ref, and
// kills it the moment that write is done: a process stopped after its last
// live write and before it could record that the journal is applied.
const lastWriteScript = `
case " $* " in
*" update-ref "*)
	%GIT% "$@"
	status=$?
	if [ -e %MUTATIONS%/.moved ]; then
		kill -9 $PPID
	fi
	: > %MUTATIONS%/.moved
	exit $status
	;;
esac
exec %GIT% "$@"
`

// applyUpdateSteps does what a process that went on would have done with
// the first n steps of an update's journal, in the order the journal
// applies them, which is what one killed after the nth leaves: the refs it
// moves, then its path steps, then the refs it deletes, each ref written
// with plain git.
func applyUpdateSteps(t *testing.T, h *harness, steps []journalStep, n int) {
	t.Helper()
	var early, paths, late []journalStep
	for _, s := range steps {
		switch {
		case s.Kind != "ref":
			paths = append(paths, s)
		case s.New == "":
			late = append(late, s)
		default:
			early = append(early, s)
		}
	}
	for _, s := range append(append(early, paths...), late...) {
		if n == 0 {
			return
		}
		n--
		switch {
		case s.Kind == "ref" && s.New == "":
			h.accountGit("update-ref", "-d", s.Ref, s.Old)
		case s.Kind == "ref":
			h.accountGit("update-ref", s.Ref, s.New, s.Old)
		default:
			applySteps(t, []journalStep{s}, 1)
		}
	}
}

// TestSkillUpdateRecoversAtEveryBoundary kills an update with SIGKILL once
// its journal is on disk, then leaves the machine as a process killed
// after each later step would: the import branch moved, the library
// directory retained, the new version published, the copy retained, the
// copy refreshed, and the candidate deleted with the journal not yet told.
// A last case is killed for real right after that deletion. The next
// command recovers each one, and the update is then whole: the branch at
// the candidate, the candidate gone, the library and the unedited copy
// holding the new version, the edited copy kept, and nothing staged or
// retained left behind.
func TestSkillUpdateRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	const steps = 6 // the branch, the library's remove and publish, the copy's, then the candidate
	for stop := 0; stop <= steps+1; stop++ {
		name := fmt.Sprintf("after %d steps", stop)
		if stop > steps {
			name = "killed after its last live write"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills", "alpha")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			editCopy(t, cursor)
			edited := libraryTree(t, cursor)
			checked(t, h, s)
			want := secondTree(t, s)
			tip := h.ref(lineage.ManagedRef("alpha"))
			candidate := h.ref(lineage.CandidateRef("alpha"))

			if stop > steps {
				out := killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", lastWriteScript)
				if h.ref(lineage.CandidateRef("alpha")) != "" {
					t.Fatalf("the update was not killed after it deleted the candidate:\n%s", out)
				}
				equal(t, "journals the killed update left", journalCount(t, h), 1)
			} else {
				killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", killedUpdateScript)
				journal := readJournal(t, h)
				var kinds []string
				for _, s := range journal {
					kinds = append(kinds, s.Kind)
				}
				equal(t, "the journal's steps", strings.Join(kinds, ", "), "ref, remove, publish, remove, publish, ref")
				equal(t, "the import branch when the update was killed", h.ref(lineage.ManagedRef("alpha")), tip)
				equal(t, "alpha's notes when the update was killed", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes\n")
				applyUpdateSteps(t, h, journal, stop)
			}

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed update: exit %d\n%s", got.exit, got.stderr)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
			equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
			if !executable(t, filepath.Join(h.library, "alpha", "scripts", "run.sh")) {
				t.Error("scripts/run.sh is not executable after recovery")
			}
			sameTree(t, "claude's copy", libraryTree(t, claude), want)
			sameTree(t, "cursor's copy", libraryTree(t, cursor), edited)
			for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "state", h.listed("alpha")["state"], stateCurrent)
		})
	}
}

// TestSkillUpdateRunAgainFinishesTheUpdateThatStopped kills an update once
// its journal is on disk and leaves the machine as a process killed before
// its first step, after the import branch moved, and after the library
// directory was retained. Running the update again, by name or with --all,
// finishes that journal before it judges anything: the import branch the
// journal moves first would otherwise read as the update applied while
// the library still held the version replaced, or as a managed skill whose
// library directory is gone. The run then answers for the machine as the
// recovery left it, with the update applied and nothing left to update.
func TestSkillUpdateRunAgainFinishesTheUpdateThatStopped(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"skill", "update", "alpha"}, {"skill", "update", "--all"}} {
		for stop := 0; stop <= 2; stop++ {
			t.Run(fmt.Sprintf("%s after %d steps", strings.Join(args, " "), stop), func(t *testing.T) {
				t.Parallel()
				h, s, _ := updateHarness(t)
				checked(t, h, s)
				want := secondTree(t, s)
				candidate := h.ref(lineage.CandidateRef("alpha"))
				killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", killedUpdateScript)
				applyUpdateSteps(t, h, readJournal(t, h), stop)

				out := h.run(append([]string{"--json"}, args...)...)
				equal(t, "exit", out.exit, 0)
				equal(t, "journals", journalCount(t, h), 0)
				equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
				equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
				sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
				sameTree(t, "claude's copy", libraryTree(t, filepath.Join(h.home, ".claude", "skills", "alpha")), want)
				sameTree(t, "cursor's copy", libraryTree(t, filepath.Join(h.home, ".cursor", "skills", "alpha")), want)
				equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
				if strings.Contains(out.stdout+out.stderr, "no skill directory") {
					t.Errorf("the run answered for the library directory the stopped update retained:\n%s%s", out.stdout, out.stderr)
				}
				summary := "alpha is up to date as of the last update check; run 'agentx skill check' to look again"
				if args[2] == "--all" {
					summary = "nothing to update: no managed skill has an update as of the last update check; run 'agentx skill check' to look again"
				}
				equal(t, "summary", h.one(out.stdout, "result")["summary"], summary)
				equal(t, "state", h.listed("alpha")["state"], stateCurrent)
			})
		}
	}
}

// TestSkillUpdateStoppedBeforeItsJournalChangesNothing stops an update with
// SIGTERM while it reads the version it is about to lay out, before the
// lock and before any journal: the run answers with exit code 9 and leaves
// the machine as it was, with nothing staged, and the next update applies.
func TestSkillUpdateStoppedBeforeItsJournalChangesNothing(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	library := onDisk(t, h.library)
	ready := filepath.Join(t.TempDir(), "reading")
	path := h.env["PATH"]
	hangingGit(t, h, "cat-file", ready)

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM},
		args: []string{"skill", "update", "alpha", "--color", "off"}})
	killLeftover(t, ready)
	h.env["PATH"] = path

	equal(t, "the exit code of a stopped update", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")

	h.mustRun("skill", "update", "alpha")
	equal(t, "the import branch after the next update", h.ref(lineage.ManagedRef("alpha")), candidate)
}

// TestSkillUpdateSweepsStagingAKilledUpdateLeft stands in for an update
// killed after it staged the new version beside the library directory and
// a refreshed copy beside each copy placement, and before its journal was
// written: nothing names any of them, so the next update sweeps them all
// before it stages anything, and leaves nothing beside the library or a
// copy. A run over every skill sweeps the client directories two skills
// share once, before either stages its copy there, so that no sweep takes
// a copy the run itself staged a moment earlier.
func TestSkillUpdateSweepsStagingAKilledUpdateLeft(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"skill", "update", "alpha"}, {"skill", "update", "--all"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			s.write("skills/beta/notes.md", "beta notes, revised\n")
			checked(t, h, s)
			candidate := h.ref(lineage.CandidateRef("alpha"))
			claude := filepath.Join(h.home, ".claude", "skills")
			cursor := filepath.Join(h.home, ".cursor", "skills")
			for i, dir := range []string{h.library, claude, cursor} {
				staged := filepath.Join(dir, fmt.Sprintf(".agentx-staged-deadbeef-%d", i+1))
				writeFile(t, mkdirs(t, staged, "SKILL.md"), skill("alpha", "a version nothing names"))
			}

			out := h.run(args...)
			if out.exit != 0 {
				t.Fatalf("update: exit %d\n%s", out.exit, out.stderr)
			}
			for _, dir := range []string{h.library, claude, cursor} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "journals", journalCount(t, h), 0)
			equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
			want := secondTree(t, s)
			for _, dir := range []string{h.library, claude, cursor} {
				sameTree(t, "alpha in "+dir, libraryTree(t, filepath.Join(dir, "alpha")), want)
			}
			if args[2] == "--all" {
				for _, dir := range []string{h.library, claude, cursor} {
					equal(t, "beta's notes in "+dir, fileBody(t, filepath.Join(dir, "beta", "notes.md")), "beta notes, revised\n")
				}
			}
		})
	}
}

// TestSkillUpdateStoppedInItsJournalIsRecovered stops an update the way a
// terminal's Ctrl-C does, signalling the whole process group while the
// journal's first ref transaction is in git: that git dies with the run,
// which answers with exit code 9 and leaves its journal, and the next
// command that takes the lock finishes the update.
func TestSkillUpdateStoppedInItsJournalIsRecovered(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	want := secondTree(t, s)
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	ready := filepath.Join(t.TempDir(), "updating")
	path := h.env["PATH"]
	hangingGit(t, h, "update-ref", ready)

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGINT}, group: true,
		args: []string{"skill", "update", "alpha", "--color", "off"}})
	killLeftover(t, ready)
	h.env["PATH"] = path

	equal(t, "the exit code of a stopped update", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	equal(t, "the import branch when the update was stopped", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "journals the stopped update left", journalCount(t, h), 1)

	h.mustRun("config", "set", "label", "recovered")
	equal(t, "journals after recovery", journalCount(t, h), 0)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
	sameTree(t, "claude's copy", libraryTree(t, filepath.Join(h.home, ".claude", "skills", "alpha")), want)
	equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
}

// updateEditingMidway runs an update of alpha that writes an edit into the
// file at notes after its journal is on disk, as the import branch moves,
// and returns what the run printed.
func updateEditingMidway(t *testing.T, h *harness, notes string) outcome {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "edited")
	path := h.env["PATH"]
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" update-ref "*)
	if [ ! -e `+shellWord(marker)+` ]; then
		: > `+shellWord(marker)+`
		printf 'an edit made midway\n' > `+shellWord(notes)+`
	fi
	;;
esac
exec `+real+` "$@"
`)
	out := h.run("--json", "skill", "update", "alpha")
	h.env["PATH"] = path
	return out
}

// editedMidway runs an update of alpha whose library directory is edited
// after the journal is on disk, as the import branch moves. The step that
// would take the directory out of the way finds something it did not
// capture and refuses, and the journal waits for the user. It returns the
// candidate the update was applying and the path of alpha's notes.md.
func editedMidway(t *testing.T) (h *harness, s *sourceRepo, candidate, notes string) {
	t.Helper()
	h, s, _ = updateHarness(t)
	checked(t, h, s)
	candidate = h.ref(lineage.CandidateRef("alpha"))
	notes = filepath.Join(h.library, "alpha", "notes.md")
	out := updateEditingMidway(t, h, notes)

	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "recovery required")
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the import branch, moved before the paths", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "alpha's notes", fileBody(t, notes), "an edit made midway\n")
	nothingAt(t, "new.md", filepath.Join(h.library, "alpha", "new.md"))
	equal(t, "journals", journalCount(t, h), 1)
	return h, s, candidate, notes
}

// TestSkillUpdateKeepsItsCandidateWhenTheLibraryChangesMidway: an update
// whose library directory changes while it is written keeps the edit, and
// the candidate ref still names the version being applied, since the
// journal deletes it last. Running the update again, the natural retry,
// stops with the refusal a recovery gives while the edit is there, rather
// than answering from the moved branch that the skill is up to date.
// Restoring the directory lets it finish the update.
func TestSkillUpdateKeepsItsCandidateWhenTheLibraryChangesMidway(t *testing.T) {
	t.Parallel()
	h, s, candidate, notes := editedMidway(t)
	want := secondTree(t, s)

	retry := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit of the retry", retry.exit, 6)
	contains(t, "the retry's message", h.one(retry.stdout, "error")["message"].(string), "recovery required")
	equal(t, "journals after the retry", journalCount(t, h), 1)
	equal(t, "the candidate ref after the retry", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "alpha's notes after the retry", fileBody(t, notes), "an edit made midway\n")

	writeFile(t, notes, "alpha notes\n")
	out := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit", out.exit, 0)
	equal(t, "journals after recovery", journalCount(t, h), 0)
	equal(t, "the candidate ref after recovery", h.ref(lineage.CandidateRef("alpha")), "")
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
	equal(t, "summary", h.one(out.stdout, "result")["summary"],
		"alpha is up to date as of the last update check; run 'agentx skill check' to look again")
}

// TestSkillUpdateKeepsACopyEditedMidway: an update whose unedited copy is
// edited after its journal is on disk, as the import branch moves, has
// already replaced the library directory when the step that would take the
// copy out of the way finds something it did not capture and refuses. The
// copy keeps the edit byte for byte, the candidate ref still names the
// version being applied, since the journal deletes it last, and the journal
// waits for the user. Restoring the copy lets the next command finish the
// update, the copy refreshed with the rest.
func TestSkillUpdateKeepsACopyEditedMidway(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	want := secondTree(t, s)
	candidate := h.ref(lineage.CandidateRef("alpha"))
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	notes := filepath.Join(claude, "notes.md")
	out := updateEditingMidway(t, h, notes)

	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "recovery required")
	equal(t, "claude's notes", fileBody(t, notes), "an edit made midway\n")
	nothingAt(t, "new.md in claude's copy", filepath.Join(claude, "new.md"))
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the import branch, moved before the paths", h.ref(lineage.ManagedRef("alpha")), candidate)
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
	equal(t, "journals", journalCount(t, h), 1)

	writeFile(t, notes, "alpha notes\n")
	retry := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit after the copy is restored", retry.exit, 0)
	equal(t, "summary", h.one(retry.stdout, "result")["summary"],
		"alpha is up to date as of the last update check; run 'agentx skill check' to look again")
	equal(t, "journals after recovery", journalCount(t, h), 0)
	equal(t, "the candidate ref after recovery", h.ref(lineage.CandidateRef("alpha")), "")
	sameTree(t, "claude's copy after recovery", libraryTree(t, claude), want)
	sameTree(t, "the library directory after recovery", libraryTree(t, filepath.Join(h.library, "alpha")), want)
	for _, dir := range []string{h.library, filepath.Dir(claude)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
}

// TestSkillUpdateWhoseJournalWasMovedAsideOffersNoUpdate: the user keeps
// the edit made midway by moving the waiting journal aside. The import
// branch already holds the new version and the candidate ref still names
// it, which is no update: skill list shows the skill modified against the
// new version with no update available, skill diff --upstream knows of no
// update, skill update says the skill is up to date rather than sending
// the user to a revert that would discard the edit, and update --all has
// nothing to do. The next check deletes the leftover ref and announces
// nothing.
func TestSkillUpdateWhoseJournalWasMovedAsideOffersNoUpdate(t *testing.T) {
	t.Parallel()
	h, _, candidate, notes := editedMidway(t)
	journals, err := filepath.Glob(filepath.Join(h.agentx, "mutations", "*.json"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("journals %v, %v", journals, err)
	}
	if err := os.Rename(journals[0], filepath.Join(t.TempDir(), "kept.json")); err != nil {
		t.Fatal(err)
	}

	listed := h.listed("alpha")
	equal(t, "state", listed["state"], stateModified)
	if _, ok := listed["candidate"]; ok {
		t.Errorf("the skill lists a candidate its branch already holds: %v", listed["candidate"])
	}
	if list := h.mustRun("skill", "list").stdout; strings.Contains(list, updateAvailable) {
		t.Errorf("skill list offers an update the branch already holds:\n%s", list)
	}
	diff := h.run("--json", "skill", "diff", "alpha", "--upstream")
	equal(t, "exit of skill diff --upstream", diff.exit, 6)
	equal(t, "skill diff --upstream", h.one(diff.stdout, "error")["message"], "no update of alpha is known")
	update := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit of skill update", update.exit, 0)
	equal(t, "skill update", h.one(update.stdout, "result")["summary"],
		"alpha is up to date as of the last update check; run 'agentx skill check' to look again")
	all := h.mustRun("skill", "update", "--all")
	contains(t, "skill update --all", all.stdout, "Nothing to update")
	equal(t, "alpha's notes", fileBody(t, notes), "an edit made midway\n")
	equal(t, "the candidate ref before a check", h.ref(lineage.CandidateRef("alpha")), candidate)

	check := h.mustRun("--json", "skill", "check")
	if got := h.eventsOfType(check.stdout, "update_available"); len(got) != 0 {
		t.Errorf("the check announced %v", got)
	}
	equal(t, "the candidate ref after a check", h.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "the import branch after a check", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "state after a check", h.listed("alpha")["state"], stateModified)
}

// TestSkillUpdateOffersNoUpdateWhoseLineageItCannotRead: a candidate ref
// that holds a commit whose trailers agentx cannot read names no version
// agentx could lay out or record, so it is no update to any command that
// shows or applies one. skill list shows no candidate and no update
// available, skill diff --upstream knows of no update, skill update says
// the skill is up to date, and update --all has nothing to do; none of
// them touches the ref, and the skill stays current at its base.
func TestSkillUpdateOffersNoUpdateWhoseLineageItCannotRead(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	lineagelessCandidate(t, h, s)
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))

	listed := h.listed("alpha")
	equal(t, "state", listed["state"], stateCurrent)
	if _, ok := listed["candidate"]; ok {
		t.Errorf("the skill lists a candidate whose lineage agentx cannot read: %v", listed["candidate"])
	}
	if list := h.mustRun("skill", "list").stdout; strings.Contains(list, updateAvailable) {
		t.Errorf("skill list offers an update whose lineage agentx cannot read:\n%s", list)
	}
	diff := h.run("--json", "skill", "diff", "alpha", "--upstream")
	equal(t, "exit of skill diff --upstream", diff.exit, 6)
	equal(t, "skill diff --upstream", h.one(diff.stdout, "error")["message"], "no update of alpha is known")
	update := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit of skill update", update.exit, 0)
	equal(t, "skill update", h.one(update.stdout, "result")["summary"],
		"alpha is up to date as of the last update check; run 'agentx skill check' to look again")
	all := h.mustRun("skill", "update", "--all")
	contains(t, "skill update --all", all.stdout, "Nothing to update")

	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes\n")
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillUpdateRecoveryKeepsItsCandidateWhenTheLibraryChanged kills an
// update once its journal is on disk, and the library directory is edited
// before the next command. That command's recovery moves the branch, finds
// the directory holding something the update did not capture, and refuses:
// the command stops with the refusal a recovery gives, the directory keeps
// the edit, and the candidate ref, read back with plain git, still names
// the version being applied, since recovery deletes it after the paths as
// the update does. Restoring the directory lets the next command finish.
func TestSkillUpdateRecoveryKeepsItsCandidateWhenTheLibraryChanged(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	want := secondTree(t, s)
	tip, candidate := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", killedUpdateScript)
	equal(t, "the import branch when the update was killed", h.ref(lineage.ManagedRef("alpha")), tip)
	equal(t, "journals the killed update left", journalCount(t, h), 1)
	notes := filepath.Join(h.library, "alpha", "notes.md")
	writeFile(t, notes, "an edit made after the update stopped\n")

	out := h.run("--json", "config", "set", "label", "recovered")
	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "recovery required")
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the import branch, moved before the paths", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "alpha's notes", fileBody(t, notes), "an edit made after the update stopped\n")
	nothingAt(t, "new.md", filepath.Join(h.library, "alpha", "new.md"))
	equal(t, "journals", journalCount(t, h), 1)

	writeFile(t, notes, "alpha notes\n")
	h.mustRun("config", "set", "label", "recovered")
	equal(t, "journals after recovery", journalCount(t, h), 0)
	equal(t, "the candidate ref after recovery", h.ref(lineage.CandidateRef("alpha")), "")
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
}

// TestSkillUpdateRefusesACandidateStoredInAnOlderForm: a candidate whose
// import commit stores its version over a source's own tree, legacy modes
// and all, is a version no library directory is ever current against, so
// applying it would leave the skill modified at once. The update refuses it
// for what the account repo holds and changes nothing; a check, which
// finds the source holding the version the branch names, drops it.
func TestSkillUpdateRefusesACandidateStoredInAnOlderForm(t *testing.T) {
	t.Parallel()
	h, s, _ := legacyHarness(t)
	legacy, canonical := h.storeInOlderForm(t, s)
	h.accountGit("update-ref", lineage.ManagedRef("nc"), canonical, legacy)
	h.accountGit("update-ref", lineage.CandidateRef("nc"), legacy)
	library := onDisk(t, h.library)

	out := h.run("--json", "skill", "update", "nc")
	equal(t, "exit", out.exit, 8)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "the update candidate refs/agentx/candidate/nc stores its version in a form agentx does not write")
	equal(t, "hint", e["hint"], "run 'agentx skill check' to pin the update again")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("nc")), canonical)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("nc")), legacy)
	equal(t, "the library", onDisk(t, h.library), library)
	equal(t, "journals", journalCount(t, h), 0)

	h.mustRun("skill", "check")
	equal(t, "the candidate after a check", h.ref(lineage.CandidateRef("nc")), "")
}

// TestSkillUpdateAllGoesOnPastACandidateStoredInAnOlderForm: in a run over
// every skill, a candidate stored in a form agentx does not write is found
// while the run reads the versions it lays out, before the lock, and costs
// only its own skill. Its warning names it, the other skill is updated, and
// the run ends with the account repo's exit code 8 and an error naming it.
// The refused skill keeps its branch, its candidate and its library
// directory, and no journal is left.
func TestSkillUpdateAllGoesOnPastACandidateStoredInAnOlderForm(t *testing.T) {
	t.Parallel()
	h, s, _ := legacyHarness(t)
	// alpha comes from a source of its own, so that nothing is committed
	// over the legacy source's tree.
	other := h.newSourceRepo("other", true)
	other.skill("skills/alpha", "alpha", "A skill with an update", map[string]string{"notes.md": "alpha notes\n"})
	other.commit("alpha")
	h.mustRun("skill", "add", other.url, "--skill", "alpha")
	other.write("skills/alpha/notes.md", "alpha notes, revised\n")
	other.commit("alpha revised")
	h.mustRun("skill", "check")
	alphaCandidate := h.ref(lineage.CandidateRef("alpha"))
	if alphaCandidate == "" {
		t.Fatal("the check pinned no candidate for alpha")
	}
	legacy, canonical := h.storeInOlderForm(t, s)
	h.accountGit("update-ref", lineage.ManagedRef("nc"), canonical, legacy)
	h.accountGit("update-ref", lineage.CandidateRef("nc"), legacy)
	nc := onDisk(t, filepath.Join(h.library, "nc"))

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 8)
	refusal := "the update candidate refs/agentx/candidate/nc stores its version in a form agentx does not write"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "nc: "+refusal)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "1 of 2 skills could not be updated: nc: "+refusal)
	equal(t, "hint", e["hint"], "run 'agentx skill check' to pin the update again")
	equal(t, "updated", updatedNames(h, out.stdout), "alpha")
	equal(t, "alpha's branch", h.ref(lineage.ManagedRef("alpha")), alphaCandidate)
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, revised\n")
	equal(t, "nc's branch", h.ref(lineage.ManagedRef("nc")), canonical)
	equal(t, "nc's candidate", h.ref(lineage.CandidateRef("nc")), legacy)
	equal(t, "nc's library directory", onDisk(t, filepath.Join(h.library, "nc")), nc)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillUpdateOfAnAdoptedSkill: a skill another tool installed and
// agentx adopted, with nothing edited, is a managed skill like any other.
// The check finds the version its source moved on to, and the update lays
// it out and moves the branch there, while the other tool's lock file is
// never written.
func TestSkillUpdateOfAnAdoptedSkill(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha/SKILL.md", SkillFolderHash: installed,
	}})
	h.mustRun("adopt", "--all")
	h.mustRun("skill", "check")
	candidate := h.ref(lineage.CandidateRef("alpha"))
	if candidate == "" {
		t.Fatal("the check pinned no candidate for the adopted skill")
	}

	h.mustRun("skill", "update", "alpha")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, revised\n")
	equal(t, "state", h.listed("alpha")["state"], stateCurrent)
	h.lockUnchanged(h.lockPath(), lock)
}
