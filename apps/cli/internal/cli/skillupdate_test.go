package cli

import (
	"context"
	"encoding/json"
	"errors"
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
	h, s, ids := updateHome.copy(t)
	return h, s, ids[0]
}

// updateHome is the home updateHarness hands out; its id is the first commit.
var updateHome = &fixtureHome{
	source: "skills",
	dirs:   []string{".claude", ".cursor"},
	build: func(h *harness, s *sourceRepo) []string {
		s.skill("skills/alpha-dir", "alpha", "The first skill", map[string]string{
			"notes.md": "alpha notes\n", "old.md": "a file the update deletes\n", "scripts/run.sh": "#!/bin/sh\necho run\n",
		})
		s.skill("skills/beta", "beta", "The second skill", map[string]string{"notes.md": "beta notes\n"})
		first := s.commit("first version")
		h.mustRun("source", "add", s.url)
		h.mustRun("skill", "add", s.url, "--all", "--copy")
		return []string{first}
	},
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

// upToDate is how skill update answers for a skill it has no update to
// apply to, as its summary.
func upToDate(name string) string {
	return name + " is up to date as of the last update check; run 'agentx skill check-updates' to look again"
}

// unchangedHome is what a command that changes nothing leaves as it found
// it: every ref of the account repo, the library and the clients'
// directories byte for byte, and the version file.
type unchangedHome struct {
	refs, library, clients string
	version                int
}

// unchangedHome reads h as the command about to run finds it.
func (h *harness) unchangedHome() unchangedHome {
	h.t.Helper()
	return unchangedHome{refs: h.refLines(), library: onDisk(h.t, h.library), clients: onDisk(h.t, h.home), version: mutationVersion(h.t, h)}
}

// check fails the test unless h is as u found it, but for bumps writes of
// the version file, with no journal and nothing staged or retained beside
// the library.
func (u unchangedHome) check(t *testing.T, h *harness, what string, bumps int) {
	t.Helper()
	equal(t, what+": the refs", h.refLines(), u.refs)
	equal(t, what+": the library", onDisk(t, h.library), u.library)
	equal(t, what+": the clients' directories", onDisk(t, h.home), u.clients)
	equal(t, what+": mutations", mutationVersion(t, h), u.version+bumps)
	equal(t, what+": journals", journalCount(t, h), 0)
	equal(t, what+": what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
}

// TestSkillUpdateReplacesAnUnmodifiedSkill applies the update a check found
// to a skill nobody edited. The import branch moves to the candidate, which
// a second invocation reads back with plain git, and the candidate ref is
// gone; the library directory holds the new version, the changed, added
// and deleted files and the exec bit included, and keeps the .DS_Store git
// ignores in it. Of the two copies, the one that held the version replaced
// is refreshed and keeps a .DS_Store of its own, and the one edited where
// it is kept byte for byte, with the warning and the skipped count. The
// skill reads current at the new base afterwards with nothing to diff,
// skill list shows no update, a check right after finds none, and the old
// import stays reachable through the branch's reflog.
func TestSkillUpdateReplacesAnUnmodifiedSkill(t *testing.T) {
	t.Parallel()
	h, s, first := updateHarness(t)
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	editCopy(t, cursor)
	edited := libraryTree(t, cursor)
	// The two hold the same bytes, since a copy whose files differ from
	// the library's, ignored or not, is listed as no placement of it.
	writeFile(t, filepath.Join(lib, ".DS_Store"), "finder data\n")
	writeFile(t, filepath.Join(claude, ".DS_Store"), "finder data\n")
	second := newVersion(t, s)
	want := secondTree(t, s)
	h.mustRun("skill", "check-updates")
	refs := h.refMap()
	tip, candidate := refs[lineage.ManagedRef("alpha")], refs[lineage.CandidateRef("alpha")]
	if candidate == "" || candidate == tip {
		t.Fatalf("the check pinned candidate %q against tip %q", candidate, tip)
	}
	before := mutationVersion(t, h)

	out := h.mustRun("--json", "skill", "update", "alpha")
	equal(t, "mutations", mutationVersion(t, h), before+1)
	refs = h.refMap()
	equal(t, "the import branch", refs[lineage.ManagedRef("alpha")], candidate)
	equal(t, "the candidate ref", refs[lineage.CandidateRef("alpha")], "")
	contains(t, "the branch's reflog", h.accountGit("reflog", "show", "--format=%H", lineage.ManagedRef("alpha")), tip)
	sameTree(t, "the library directory", libraryTree(t, lib), withFile(want, ".DS_Store", "finder data\n"))
	if !executable(t, filepath.Join(lib, "scripts", "run.sh")) {
		t.Error("scripts/run.sh is not executable after the update")
	}
	nothingAt(t, "old.md", filepath.Join(lib, "old.md"))
	sameTree(t, "claude's copy", libraryTree(t, claude), withFile(want, ".DS_Store", "finder data\n"))
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
	equal(t, "the diff after the update", diffPaths(h, "alpha"), "")
	if strings.Contains(h.mustRun("skill", "list").stdout, updateAvailable) {
		t.Error("skill list still shows an update after it was applied")
	}
	check := h.mustRun("--json", "skill", "check-updates")
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

// TestSkillUpdateCountsASystemFileWhenTheSettingIsOff: with
// ignore_system_files off, a .DS_Store in the library directory is a file
// of the skill, so the skill is edited: its update merges the .DS_Store in
// as an edit, and the skill stays modified. Once the setting is on again,
// git ignores the .DS_Store and the skill is current.
func TestSkillUpdateCountsASystemFileWhenTheSettingIsOff(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	lib := filepath.Join(h.library, "alpha")
	writeFile(t, filepath.Join(lib, ".DS_Store"), "finder data\n")
	h.mustRun("config", "set", "ignore_system_files", "false")

	out := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit", out.exit, 0)
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "merged its edits cleanly")
	sameTree(t, "the library directory", libraryTree(t, lib), withFile(secondTree(t, s), ".DS_Store", "finder data\n"))
	equal(t, "state", h.listed("alpha")["state"], stateModified)

	h.mustRun("config", "set", "ignore_system_files", "true")
	equal(t, "state with the setting on", h.listed("alpha")["state"], stateCurrent)
}

// mixedHarness is a source of five skills, all installed from its first
// commit and symlinked into Claude Code, and a second commit after which a
// check finds: an update for alpha, which nobody edited; an update for
// beta, which the test then edits in the library where the update changes
// it too; an update for epsilon, which the test edits in a file the update
// leaves alone; gamma gone from the source; and delta as it was.
func mixedHarness(t *testing.T) (h *harness, s *sourceRepo, first, second string) {
	t.Helper()
	h, s, ids := mixedHome.copy(t)
	return h, s, ids[0], ids[1]
}

// mixedHome is the home mixedHarness hands out; its ids are the two commits.
var mixedHome = &fixtureHome{
	source: "skills",
	dirs:   []string{".claude"},
	build: func(h *harness, s *sourceRepo) []string {
		for _, name := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
			s.skill("skills/"+name, name, "The skill "+name, map[string]string{"notes.md": name + " notes\n", "usage.md": name + " usage\n"})
		}
		first := s.commit("first version")
		h.mustRun("source", "add", s.url)
		h.mustRun("skill", "add", s.url, "--all")
		s.write("skills/alpha/notes.md", "alpha notes, revised\n")
		s.write("skills/beta/notes.md", "beta notes, revised\n")
		s.write("skills/epsilon/notes.md", "epsilon notes, revised\n")
		s.run("rm", "-r", "--quiet", "skills/gamma")
		second := s.commit("second version")
		h.mustRun("skill", "check-updates")
		editLibrary(h.t, h, "beta", "notes.md", "beta notes, edited here\n")
		editLibrary(h.t, h, "epsilon", "usage.md", "epsilon usage, edited here\n")
		return []string{first, second}
	},
}

// TestSkillUpdateAllMergesEditsAndLeavesConflictsPending runs update --all
// over a mix: the unmodified skill with an update is updated; the modified
// one whose edit the update leaves alone is merged, keeping the edit on the
// new version and staying modified; the modified one whose edit conflicts
// is left as it was, with a merge pending and a conflict event, and costs
// the run exit code 4 as a refusal of its own would; the upstream-removed
// skill and the one with no update are not touched. A second run, with
// only the conflicting skill left to update, is answered as that skill on
// its own: its merge is still pending and is reported again, with no
// warning before it, no library_skill event and no journal. Once it is
// resolved with git, a third run applies it.
func TestSkillUpdateAllMergesEditsAndLeavesConflictsPending(t *testing.T) {
	t.Parallel()
	h, _, _, second := mixedHarness(t)
	refs := h.refMap()
	betaCandidate, betaTip := refs[lineage.CandidateRef("beta")], refs[lineage.ManagedRef("beta")]
	epsilonCandidate := refs[lineage.CandidateRef("epsilon")]
	gammaMarker := refs[lineage.UpstreamRemovedRef("gamma")]
	deltaTip := refs[lineage.ManagedRef("delta")]
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
	equal(t, "the conflict's files", conflictPaths(conflict), "notes.md")
	refusal := "beta conflicts with its update in 1 file, so the merge is pending and the library directory was left as it is"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), refusal)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "pending_merge")
	equal(t, "message", e["message"], "1 of 3 skills could not be updated: "+refusal)
	equal(t, "hint", e["hint"], conflictHint(h, "beta", "beta"))
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, revised\n")
	equal(t, "epsilon's notes", fileBody(t, filepath.Join(h.library, "epsilon", "notes.md")), "epsilon notes, revised\n")
	equal(t, "epsilon's usage", fileBody(t, filepath.Join(h.library, "epsilon", "usage.md")), "epsilon usage, edited here\n")
	equal(t, "beta's notes", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes, edited here\n")
	refs = h.refMap()
	equal(t, "alpha's candidate", refs[lineage.CandidateRef("alpha")], "")
	equal(t, "epsilon's branch", refs[lineage.ManagedRef("epsilon")], epsilonCandidate)
	equal(t, "epsilon's candidate", refs[lineage.CandidateRef("epsilon")], "")
	equal(t, "beta's candidate", refs[lineage.CandidateRef("beta")], betaCandidate)
	equal(t, "beta's branch", refs[lineage.ManagedRef("beta")], betaTip)
	equal(t, "gamma's marker", refs[lineage.UpstreamRemovedRef("gamma")], gammaMarker)
	equal(t, "delta's branch", refs[lineage.ManagedRef("delta")], deltaTip)
	betaHead, betaMergeHead, _ := mergeState(t, h, "beta")
	equal(t, "beta's pending merge's HEAD", betaHead, conflict["mine"].(string))
	equal(t, "beta's pending merge's MERGE_HEAD", betaMergeHead, betaCandidate)
	sameTree(t, "gamma's library directory", libraryTree(t, filepath.Join(h.library, "gamma")), gammaTree)
	equal(t, "journals", journalCount(t, h), 0)

	again := h.run("--json", "skill", "update", "--all")
	equal(t, "exit of the second run", again.exit, 4)
	equal(t, "message of the second run", h.one(again.stdout, "error")["message"], refusal)
	equal(t, "warnings of the second run", strings.Join(warnings(h, again.stderr), "\n"), "")
	if got := h.eventsOfType(again.stdout, "library_skill"); len(got) != 0 {
		t.Errorf("a run that updated nothing reported %v", got)
	}
	sameEvent(t, "the conflict of the second run", h.one(again.stdout, "conflict"), conflict)
	equal(t, "beta's candidate after the second run", h.ref(lineage.CandidateRef("beta")), betaCandidate)
	equal(t, "journals after the second run", journalCount(t, h), 0)

	checkoutGit(t, h, "beta", "checkout", "--theirs", "--", "beta/notes.md")
	checkoutGit(t, h, "beta", "add", "--", "beta/notes.md")
	third := h.mustRun("--json", "skill", "update", "--all")
	equal(t, "summary of the third run", h.one(third.stdout, "result")["summary"], "updated 1 skill")
	equal(t, "beta's notes once applied", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes, revised\n")
	refs = h.refMap()
	equal(t, "beta's branch once applied", refs[lineage.ManagedRef("beta")], betaCandidate)
	equal(t, "beta's candidate once applied", refs[lineage.CandidateRef("beta")], "")
	noCheckout(t, h, "beta")
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
		"notes.md: both modified\n")
	contains(t, "stderr", out.stderr, "error: 1 of 3 skills could not be updated: beta conflicts with its update in 1 file")
}

// TestSkillUpdateAllReportsConflictsInNameOrder: a merge still pending
// from an earlier run and one the run leaves pending for the first time
// are reported together, in name order, their warnings too.
func TestSkillUpdateAllReportsConflictsInNameOrder(t *testing.T) {
	t.Parallel()
	h, _, _, _ := mixedHarness(t)
	equal(t, "exit of beta's update", h.run("skill", "update", "beta").exit, 4)
	editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited here\n")
	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 4)
	var names []string
	for _, e := range h.eventsOfType(out.stdout, "conflict") {
		names = append(names, e["name"].(string))
	}
	equal(t, "the conflicts' skills", strings.Join(names, " "), "alpha beta")
	refusal := " conflicts with its update in 1 file, so the merge is pending and the library directory was left as it is"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "alpha"+refusal+"\nbeta"+refusal)
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
	h.mustRun("skill", "check-updates")
	refs := h.refMap()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if refs[lineage.CandidateRef(name)] == "" {
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
	refs := h.refMap()
	gammaTip, gammaCandidate := refs[lineage.ManagedRef("gamma")], refs[lineage.CandidateRef("gamma")]
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
	refs = h.refMap()
	equal(t, "gamma's branch", refs[lineage.ManagedRef("gamma")], gammaTip)
	equal(t, "gamma's candidate", refs[lineage.CandidateRef("gamma")], gammaCandidate)
	sameTree(t, "gamma's library directory", libraryTree(t, filepath.Join(h.library, "gamma")), gammaTree)

	one := h.run("--json", "skill", "update", "gamma")
	equal(t, "exit of an update of gamma by name", one.exit, 5)
	e := h.one(one.stdout, "error")
	equal(t, "code", e["code"], "not_found")
	equal(t, "message", e["message"], refusal)
	equal(t, "hint", e["hint"], hint)

	h.mustRun("skill", "check-updates")
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
// updates the rest. Here delta's library directory is gone and epsilon
// holds a repository git cannot record, which are exit code 6 each, the
// second counted rather than skipped, since what an update would discard
// there has no record anywhere; and beta's candidate holds no directory of
// beta's, which is the account repo's exit code 8. Refusals that disagree
// end the run with 6, the hint of a mixed run and an error naming every
// skill it gave up on. A skill of a removed source is skipped all the same
// and is not among them.
func TestSkillUpdateAllReportsEachRefusalAndGoesOn(t *testing.T) {
	t.Parallel()
	h, s, other := otherSourceHarness(t)
	h.mustRun("skill", "update", "alpha")
	s.skill("skills/delta", "delta", "The fourth skill", nil)
	s.skill("skills/epsilon", "epsilon", "The fifth skill", nil)
	s.commit("delta and epsilon")
	h.mustRun("skill", "add", s.url, "--name", "delta", "--name", "epsilon", "--fetch")
	s.write("skills/alpha-dir/notes.md", "alpha notes, revised again\n")
	s.skill("skills/delta", "delta", "The fourth skill, revised", nil)
	s.skill("skills/epsilon", "epsilon", "The fifth skill, revised", nil)
	s.commit("alpha, delta and epsilon revised")
	h.mustRun("skill", "check-updates")
	h.mustRun("source", "remove", other.url)
	refs := h.refMap()
	for _, name := range []string{"alpha", "beta", "delta", "epsilon", "gamma"} {
		if refs[lineage.CandidateRef(name)] == "" {
			t.Fatalf("the check pinned no candidate for %s", name)
		}
	}
	remove(t, filepath.Join(h.library, "delta"))
	nested := filepath.Join(h.library, "epsilon", "vendored", ".git")
	writeFile(t, mkdirs(t, nested, "HEAD"), "ref: refs/heads/main\n")
	// beta's candidate carries its lineage over a tree that holds delta's
	// directory and not beta's.
	message := h.accountGit("log", "-1", "--format=%B", lineage.CandidateRef("beta"))
	elsewhere := h.accountGit("commit-tree", h.accountGit("rev-parse", refs[lineage.ManagedRef("delta")]+"^{tree}"), "-m", message)
	h.accountGit("update-ref", lineage.CandidateRef("beta"), elsewhere)

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 6)
	delta := "delta is managed in the account repo but the library holds no skill directory for it, so there is nothing to update"
	epsilon := "epsilon holds " + nested + ", which git cannot record"
	beta := `not an import commit: refs/heads/managed/beta holds "delta" beside beta`
	gamma := "gamma was installed from " + other.url + ", which was removed from this machine, so it is not updated; run 'agentx source add " + other.url + "' to add it again"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), strings.Join([]string{delta, epsilon, gamma, "beta: " + beta}, "\n"))
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "refused")
	equal(t, "message", e["message"], "3 of 5 skills could not be updated: "+delta+"; "+epsilon+"; beta: "+beta)
	equal(t, "hint", e["hint"], "run 'agentx skill list' to see which skills have an update, then update the rest one at a time")
	// The result of a run that refused a skill is its error, with no copy
	// counts: what it did for the skills it updated is in their
	// library_skill events and its warnings.
	r := h.one(out.stdout, "result")
	equal(t, "ok", r["ok"], false)
	equal(t, "summary", r["summary"], e["message"])
	equal(t, "updated", updatedNames(h, out.stdout), "alpha")
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, revised again\n")
	after := h.refMap()
	equal(t, "beta's branch", after[lineage.ManagedRef("beta")], refs[lineage.ManagedRef("beta")])
	equal(t, "beta's candidate", after[lineage.CandidateRef("beta")], elsewhere)
	equal(t, "epsilon's candidate", after[lineage.CandidateRef("epsilon")], refs[lineage.CandidateRef("epsilon")])
	equal(t, "beta's notes", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes\n")
	equal(t, "epsilon's nested repository", fileBody(t, filepath.Join(nested, "HEAD")), "ref: refs/heads/main\n")
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillUpdateAllRefreshesEachSkillsCopies runs update --all over two
// skills with an update, each with a copy in Claude Code and in Cursor, and
// beta's Cursor copy edited in place. Every copy that held the version
// replaced is refreshed, beta's own as much as alpha's, and the edited one
// is kept byte for byte with the warning for a kept copy. The first line
// adds up what the run did to copies over every skill, as the result
// does, and each row says what the update of its skill did.
func TestSkillUpdateAllRefreshesEachSkillsCopies(t *testing.T) {
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
	h.mustRun("skill", "check-updates")
	refs := h.refMap()
	for _, name := range []string{"alpha", "beta"} {
		if refs[lineage.CandidateRef(name)] == "" {
			t.Fatalf("the check pinned no candidate for %s", name)
		}
	}

	out := h.mustRun("skill", "update", "--all")
	after := h.refMap()
	for _, name := range []string{"alpha", "beta"} {
		equal(t, name+"'s import branch", after[lineage.ManagedRef(name)], refs[lineage.CandidateRef(name)])
		equal(t, name+"'s candidate", after[lineage.CandidateRef(name)], "")
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

	warning, wayOn := keptCopyLines(betaCursor, "beta", "agentx skill remove beta --from cursor", "agentx skill place beta --to cursor --copy")
	equal(t, "stderr", out.stderr, "warning: "+warning+"\n  "+wayOn+"\n")
	moved := first[:7] + " -> " + second[:7]
	equal(t, "the text", out.stdout, "✓ updated 2 skills, 3 copy placements refreshed, 1 placement skipped\n"+
		"  alpha  "+moved+"  2 copy placements refreshed\n"+
		"  beta   "+moved+"  1 copy placement refreshed, 1 placement skipped\n")
}

// TestSkillUpdateSkipsACopyItCannotRead: a copy placement this machine
// cannot read whole can be judged neither unchanged nor edited, so the
// update leaves it as it is, counts it as skipped and names it with the
// cause, and the update of the library and of the other copy still lands.
func TestSkillUpdateSkipsACopyItCannotRead(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root reads a directory whatever its mode")
	}
	h, s, _ := updateHarness(t)
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
	newVersion(t, s)
	want := secondTree(t, s)
	h.mustRun("skill", "check-updates")
	scripts := filepath.Join(cursor, "scripts")
	chmod(t, scripts, 0)
	t.Cleanup(func() { _ = os.Chmod(scripts, 0o755) }) // so the temporary home can be removed

	out := h.run("--json", "skill", "update", "alpha")
	chmod(t, scripts, 0o755)
	if out.exit != 0 {
		t.Fatalf("update: exit %d\n%s", out.exit, out.stderr)
	}
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
	warned := warnings(h, out.stderr)
	if len(warned) != 1 || !strings.HasPrefix(warned[0], "cannot refresh "+cursor+": ") || !strings.HasSuffix(warned[0], "; the copy was left as it is") {
		t.Errorf("the warnings = %q, want one that cannot refresh %s", warned, cursor)
	}
	contains(t, "the result", h.one(out.stdout, "result")["summary"].(string), ", 1 copy placement refreshed, 1 placement skipped")
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "what is left beside the copy", strings.Join(hiddenEntries(t, filepath.Dir(cursor)), " "), "")
}

// TestSkillUpdateRefreshesACopyTwoConfigurationsShareOnce updates a skill
// one of whose copies two configurations reach. Zencoder and Zenflow both
// read ~/.zencoder/skills, and copy_mode records a copy for each; Cursor's
// skills directory made a symlink to Claude Code's makes their two copies
// one directory spelled two ways. The one copy is judged and planned once:
// planned twice, the second removal would find the first one's copy there
// and stop the update part way. Holding the version replaced, it is
// refreshed once and counted for both configurations; edited where it is,
// it is kept byte for byte with one warning and one skip.
func TestSkillUpdateRefreshesACopyTwoConfigurationsShareOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, refreshed string
		linked, edited  bool
	}{
		{"shared by zencoder and zenflow", ", 4 copy placements refreshed", false, false},
		{"shared and edited where it is", ", 2 copy placements refreshed, 1 placement skipped", false, true},
		{"reached through a linked skills directory", ", 2 copy placements refreshed", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills")
			shared := filepath.Join(h.home, ".zencoder", "skills")
			if c.linked {
				shared = claude
				cursor := filepath.Join(h.home, ".cursor", "skills")
				remove(t, cursor)
				link(t, claude, cursor)
			} else {
				if err := os.MkdirAll(filepath.Join(h.home, ".zencoder"), 0o755); err != nil {
					t.Fatal(err)
				}
				h.mustRun("skill", "place", "alpha", "--to", "zencoder", "--to", "zenflow", "--copy")
			}
			place := filepath.Join(shared, "alpha")
			if c.edited {
				editCopy(t, place)
			}
			before := libraryTree(t, place)
			newVersion(t, s)
			want := secondTree(t, s)
			h.mustRun("skill", "check-updates")

			out := h.run("--json", "skill", "update", "alpha")
			if out.exit != 0 {
				t.Fatalf("update: exit %d\n%s", out.exit, out.stderr)
			}
			summary := h.one(out.stdout, "result")["summary"].(string)
			if !strings.HasSuffix(summary, c.refreshed) {
				t.Errorf("the result = %q, want it to end %q", summary, c.refreshed)
			}
			warned := warnings(h, out.stderr)
			if c.edited {
				sameTree(t, "the shared copy", libraryTree(t, place), before)
				equal(t, "warnings", len(warned), 1)
				contains(t, "the warning", strings.Join(warned, "\n"), "zencoder's copy of alpha is different from the library")
			} else {
				sameTree(t, "the shared copy", libraryTree(t, place), want)
				equal(t, "warnings", strings.Join(warned, "\n"), "")
			}
			for _, dir := range []string{h.library, shared} {
				equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
			}
			equal(t, "journals", journalCount(t, h), 0)
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
	h.mustRun("skill", "check-updates")
}

// upstreamRemoved deletes beta from the source and runs a check, which
// marks it upstream removed.
func upstreamRemoved(t *testing.T, h *harness, s *sourceRepo) {
	t.Helper()
	s.run("rm", "-r", "--quiet", "skills/beta")
	s.commit("beta removed")
	h.mustRun("skill", "check-updates")
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
// order of the checks puts it: an import branch agentx cannot read, which
// names no source, before a removed source, a removed source before an
// upstream that no longer holds the skill, and both before whether there
// is an update at all, which comes before whether the library entry is a
// symlink, and that before whether the skill holds something git cannot
// record. A skill with no update, or with a candidate whose lineage agentx
// cannot read, is nothing to do and exits 0, edited or not. None of them
// changes a ref, the library, a placement or the version file, or leaves a
// journal, so each group of cases runs one after another on one machine,
// every case held to leaving it as it found it. A removed source with an
// update to apply is TestSkillUpdateAllSkipsASkillWhoseSourceWasRemoved's,
// and a merge pending TestSkillUpdateLeavesAConflictPending's.
func TestSkillUpdateRefusesInOrder(t *testing.T) {
	t.Parallel()
	type refusal struct {
		name  string
		skill string
		setup func(t *testing.T, h *harness, s *sourceRepo)
		exit  int
		// message and hint, in which %LIB% is the library, %URL% the
		// source's URL and %HOME% the user's home; for exit code 0 the
		// message is the summary
		message, hint string
	}
	for _, group := range []struct {
		name   string
		source bool // the machine of updateHarness, rather than an empty one
		setup  func(t *testing.T, h *harness, s *sourceRepo)
		cases  []refusal
	}{
		{
			name: "what is no managed skill",
			cases: []refusal{
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
			},
		},
		{
			name: "what has no update to apply", source: true,
			cases: []refusal{
				{
					name: "a fork with no upstream", skill: "forky", exit: 6,
					setup: func(t *testing.T, h *harness, _ *sourceRepo) {
						h.mustRun("skill", "new", "forky")
					},
					message: "forky has no upstream to update from",
					hint:    "a skill created with 'agentx skill new', or forked from an unmanaged or a plugin's skill, has no upstream version; its versions are its own commits",
				},
				{
					name: "an edited skill with no update", skill: "alpha", exit: 0,
					setup: func(t *testing.T, h *harness, _ *sourceRepo) {
						editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited\n")
					},
					message: upToDate("alpha"),
				},
				{
					name: "an edited skill whose candidate carries no lineage", skill: "alpha", exit: 0,
					setup:   lineagelessCandidate,
					message: upToDate("alpha"),
				},
				{
					// A branch with no lineage names no source, which no
					// settings entry holds, so a removed source would answer
					// first were it asked about first.
					name: "a managed skill whose import branch records no version agentx can read", skill: "beta", exit: 6,
					setup:   withoutLineage,
					message: "the import branch refs/heads/managed/beta records no version agentx can read",
					hint:    "run 'agentx doctor' and check the account repo it names",
				},
				{
					name: "a managed skill whose library directory is gone", skill: "alpha", exit: 6,
					setup:   func(t *testing.T, h *harness, _ *sourceRepo) { remove(t, filepath.Join(h.library, "alpha")) },
					message: "alpha is managed in the account repo but the library holds no skill directory for it, so there is nothing to update",
					hint:    "run 'agentx skill add %URL% --name alpha' to install it again, or 'agentx skill remove alpha' to stop managing it",
				},
			},
		},
		{
			// alpha has an update and beta is upstream removed.
			name: "what an update would lose", source: true,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				newVersion(t, s)
				upstreamRemoved(t, h, s)
			},
			cases: []refusal{
				{
					name: "a skill with an update that holds what git cannot record", skill: "alpha", exit: 6,
					setup: func(t *testing.T, h *harness, _ *sourceRepo) {
						writeFile(t, mkdirs(t, filepath.Join(h.library, "alpha", "vendored", ".git"), "HEAD"), "ref: refs/heads/main\n")
						editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited\n")
					},
					message: "alpha holds %LIB%/alpha/vendored/.git, which git cannot record",
					hint:    "an update would discard it with no record of it anywhere; move it out of the skill, then run 'agentx skill update alpha' again",
				},
				{
					// What the link leads to is not judged: an edit there is
					// no reason to refuse, nor is what git cannot record.
					name: "a skill whose library entry is a symlink to an edited directory", skill: "alpha", exit: 6,
					setup: func(t *testing.T, h *harness, _ *sourceRepo) {
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
					name: "an upstream-removed skill that was edited", skill: "beta", exit: 6,
					setup: func(t *testing.T, h *harness, _ *sourceRepo) {
						editLibrary(t, h, "beta", "notes.md", "beta notes, edited\n")
					},
					message: "the last update check found that the source of beta no longer holds it, so it is kept as it is and never updated",
					hint:    "run 'agentx skill check-updates' once the source holds it again, or 'agentx skill remove beta' to remove it",
				},
				{
					name: "an upstream-removed skill whose source was removed", skill: "beta", exit: 5,
					setup:   func(t *testing.T, h *harness, s *sourceRepo) { h.mustRun("source", "remove", s.url) },
					message: "beta was installed from %URL%, which was removed from this machine, so it is not updated",
					hint:    "run 'agentx source add %URL%' to add it again",
				},
			},
		},
	} {
		t.Run(group.name, func(t *testing.T) {
			t.Parallel()
			h, s := newHarness(t), (*sourceRepo)(nil)
			url := ""
			if group.source {
				h, s, _ = updateHarness(t)
				url = s.url
			}
			if group.setup != nil {
				group.setup(t, h, s)
			}
			expand := strings.NewReplacer("%LIB%", h.library, "%URL%", url, "%HOME%", h.home).Replace
			for _, c := range group.cases {
				if c.setup != nil {
					c.setup(t, h, s)
				}
				was := h.unchangedHome()
				out := h.run("--json", "skill", "update", c.skill)
				equal(t, c.name+": exit", out.exit, c.exit)
				if c.exit == 0 {
					equal(t, c.name+": summary", h.one(out.stdout, "result")["summary"], expand(c.message))
					if got := h.eventsOfType(out.stdout, "library_skill"); len(got) != 0 {
						t.Errorf("%s: a run that updated nothing reported %v", c.name, got)
					}
				} else {
					e := h.one(out.stdout, "error")
					equal(t, c.name+": message", e["message"], expand(c.message))
					equal(t, c.name+": hint", e["hint"], expand(c.hint))
				}
				was.check(t, h, c.name, 0)
			}
		})
	}
}

// TestRecheckUpdateRefusesWhatChangedSinceTheSkillWasJudged is every input
// an update reads again under the lock, as recheckUpdate is handed them:
// the refs git read, the sources the settings hold, the merges directory
// and the library directory, whose state it reads itself. Each change
// refuses the skill in its own words, and changes made together are
// answered for in the order the update reads them: the import branch,
// which a fork of the name supersedes, a merge pending since, the source,
// the upstream-removed marker, the candidate and last the library
// directory. The refusal of a removed source is the one a run over every
// skill skips rather than counts.
func TestRecheckUpdateRefusesWhatChangedSinceTheSkillWasJudged(t *testing.T) {
	t.Parallel()
	const url = "https://example.com/skills.git"
	var (
		moved = refuse(exitRefused, "the import branch refs/heads/managed/alpha moved while alpha was being updated, so nothing was changed",
			"run 'agentx skill update alpha' again")
		pending = refuse(exitPendingMerge, "alpha has a merge with its update pending, so it cannot be updated until the merge is resolved or given up",
			"run 'agentx skill update alpha --abort' to give the merge up; the library directory stays as it is")
		sourceGone = refuse(exitNotFound, "alpha was installed from "+url+", which was removed from this machine, so it is not updated",
			"run 'agentx source add "+url+"' to add it again")
		upstreamGone = refuse(exitRefused, "the last update check found that the source of alpha no longer holds it, so it is kept as it is and never updated",
			"run 'agentx skill check-updates' once the source holds it again, or 'agentx skill remove alpha' to remove it")
		candidate = refuse(exitRefused, "the update candidate refs/agentx/candidate/alpha moved while alpha was being updated, so nothing was changed",
			"run 'agentx skill update alpha' again to apply the update the last check found")
		edited = refuse(exitRefused, "alpha changed while it was being updated, so nothing was changed",
			"run 'agentx skill update alpha' again to update it as it is now")
	)
	// A change is one of these, applied to what recheckUpdate is handed.
	type state struct {
		t       *testing.T
		inv     *invocation
		u       *updating
		values  map[string]string
		sources map[string]bool
	}
	branch := func(s state) { s.values[lineage.ManagedRef("alpha")] = "another" }
	fork := func(s state) { s.values[lineage.ForkRef("alpha")] = "another" }
	merging := func(s state) { mkdirs(s.t, s.inv.checkoutPath("alpha"), "") }
	removed := func(s state) { delete(s.sources, url) }
	marked := func(s state) {
		s.values[lineage.UpstreamRemovedRef("alpha")] = "gone"
		delete(s.values, lineage.CandidateRef("alpha")) // a check that marks a skill drops its candidate
	}
	moveCandidate := func(s state) { s.values[lineage.CandidateRef("alpha")] = "another" }
	dropCandidate := func(s state) { delete(s.values, lineage.CandidateRef("alpha")) }
	edit := func(s state) { writeFile(s.t, filepath.Join(s.u.libPath, "notes.md"), "an edit made meanwhile\n") }
	gone := func(s state) { remove(s.t, s.u.libPath) }
	applying := func(s state) { s.u.checkout = s.inv.checkoutPath("alpha") } // the update applies the merge pending
	for _, c := range []struct {
		name    string
		changes []func(state)
		want    *failure
	}{
		{"nothing changed", nil, nil},
		{"the import branch moved", []func(state){branch}, moved},
		{"a fork of the name made", []func(state){fork}, moved},
		{"a merge left pending", []func(state){merging}, pending},
		{"the merge pending the update applies", []func(state){applying, merging}, nil},
		{"the source removed", []func(state){removed}, sourceGone},
		{"the skill marked upstream removed", []func(state){marked}, upstreamGone},
		{"the candidate moved", []func(state){moveCandidate}, candidate},
		{"the candidate dropped", []func(state){dropCandidate}, candidate},
		{"the library directory edited", []func(state){edit}, edited},
		{"the library directory gone", []func(state){gone}, edited},
		{"the import branch moved and a merge left pending", []func(state){branch, merging}, moved},
		{"a merge left pending and the source removed", []func(state){merging, removed}, pending},
		{"the source removed and the skill marked upstream removed", []func(state){removed, marked}, sourceGone},
		{"the skill marked upstream removed and its library directory edited", []func(state){marked, edit}, upstreamGone},
		{"the candidate moved and the library directory edited", []func(state){moveCandidate, edit}, candidate},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			inv := &invocation{dirs: home.Dirs{Home: filepath.Join(root, "agentx"), Library: filepath.Join(root, "library")}}
			lib := filepath.Join(inv.dirs.Library, "alpha")
			writeFile(t, mkdirs(t, lib, "SKILL.md"), skill("alpha", "The first skill"))
			writeFile(t, filepath.Join(lib, "notes.md"), "alpha notes\n")
			captured, err := home.State(lib)
			if err != nil {
				t.Fatal(err)
			}
			s := state{
				t:   t,
				inv: inv,
				u: &updating{name: "alpha", libPath: lib, captured: captured, rec: lineage.Record{
					Commit: "tip", Import: lineage.Import{Source: url}, Candidate: &lineage.Candidate{Commit: "pinned"},
				}},
				values:  map[string]string{lineage.ManagedRef("alpha"): "tip", lineage.CandidateRef("alpha"): "pinned"},
				sources: map[string]bool{url: true},
			}
			for _, change := range c.changes {
				change(s)
			}
			got := inv.recheckUpdate(s.u, s.values, s.sources)
			if c.want == nil {
				if got != nil {
					t.Fatalf("refused: %s", got.message)
				}
				return
			}
			if got == nil {
				t.Fatalf("not refused, want %q", c.want.message)
			}
			equal(t, "code", got.status, c.want.status)
			equal(t, "message", got.message, c.want.message)
			equal(t, "hint", got.hint, c.want.hint)
			equal(t, "skipped by a run over every skill", errors.Is(got, errSourceRemoved), c.want == sourceGone)
		})
	}
}

// TestSkillUpdateRefusesWhatChangedBeforeTheLock: what an update replaces
// is what it read when it began. A git wrapper changes one input while the
// update reads the candidate's version, after the skill was judged and
// before the lock is taken: an edit of the library directory, then a check
// that moves the candidate. The update refuses under the lock each time,
// before it writes a journal, and loses nothing: the edit is there, every
// ref holds what the other writer wrote, nothing is left beside the library
// or a copy, and the version file is not bumped for a run that changed
// nothing. Every other input read again under the lock is
// TestRecheckUpdateRefusesWhatChangedSinceTheSkillWasJudged's, a source
// removed meanwhile
// TestSkillUpdateAllSkipsASkillWhoseSourceIsRemovedBeforeTheLock's.
func TestSkillUpdateRefusesWhatChangedBeforeTheLock(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	refs := h.refMap()
	tip, pinned, other := refs[lineage.ManagedRef("alpha")], refs[lineage.CandidateRef("alpha")], refs[lineage.ManagedRef("beta")]
	real := realGit(t)
	path := h.env["PATH"]
	git := real + " --git-dir=" + shellWord(gitx.AccountRepoPath(h.agentx))
	notes := filepath.Join(h.library, "alpha", "notes.md")
	for _, c := range []struct {
		name, change     string // the shell line the wrapper runs
		candidate, notes string // what alpha's candidate and notes.md hold afterwards
		message, hint    string
		restore          func() // puts back what the change changed, for the next case
	}{
		{
			name: "the library directory is edited", change: `printf 'an edit made meanwhile\n' > ` + shellWord(notes),
			candidate: pinned, notes: "an edit made meanwhile\n",
			message: "alpha changed while it was being updated, so nothing was changed",
			hint:    "run 'agentx skill update alpha' again to update it as it is now",
			restore: func() { writeFile(t, notes, "alpha notes\n") },
		},
		{
			name: "a check moves the candidate", change: git + ` update-ref ` + lineage.CandidateRef("alpha") + ` ` + other,
			candidate: other, notes: "alpha notes\n",
			message: "the update candidate refs/agentx/candidate/alpha moved while alpha was being updated, so nothing was changed",
			hint:    "run 'agentx skill update alpha' again to apply the update the last check found",
		},
	} {
		before := mutationVersion(t, h)
		stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) `+c.change+` || exit 1 ;;
esac
exec `+real+` "$@"
`)
		out := h.run("--json", "skill", "update", "alpha")
		h.env["PATH"] = path
		equal(t, c.name+": exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, c.name+": message", e["message"], c.message)
		equal(t, c.name+": hint", e["hint"], c.hint)
		after := h.refMap()
		equal(t, c.name+": the import branch", after[lineage.ManagedRef("alpha")], tip)
		equal(t, c.name+": the candidate", after[lineage.CandidateRef("alpha")], c.candidate)
		equal(t, c.name+": alpha's notes", fileBody(t, notes), c.notes)
		nothingAt(t, c.name+": new.md", filepath.Join(h.library, "alpha", "new.md"))
		equal(t, c.name+": claude's notes", fileBody(t, filepath.Join(h.home, ".claude", "skills", "alpha", "notes.md")), "alpha notes\n")
		equal(t, c.name+": journals", journalCount(t, h), 0)
		equal(t, c.name+": mutations", mutationVersion(t, h), before)
		for _, dir := range []string{h.library, filepath.Join(h.home, ".claude", "skills"), filepath.Join(h.home, ".cursor", "skills")} {
			equal(t, c.name+": what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
		}
		if c.restore != nil {
			c.restore()
		}
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
	refs := h.refMap()
	gammaTip, gammaCandidate := refs[lineage.ManagedRef("gamma")], refs[lineage.CandidateRef("gamma")]
	gammaTree := libraryTree(t, filepath.Join(h.library, "gamma"))
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) `+removingSource(t, h, other.url)+` || exit 1 ;;
esac
exec `+realGit(t)+` "$@"
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
	refs = h.refMap()
	equal(t, "gamma's branch", refs[lineage.ManagedRef("gamma")], gammaTip)
	equal(t, "gamma's candidate", refs[lineage.CandidateRef("gamma")], gammaCandidate)
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
	h.mustRun("skill", "check-updates")
	refs := h.refMap()
	alphaTip, alphaCandidate := refs[lineage.ManagedRef("alpha")], refs[lineage.CandidateRef("alpha")]
	betaCandidate := refs[lineage.CandidateRef("beta")]
	if alphaCandidate == "" || betaCandidate == "" {
		t.Fatalf("the check pinned candidates %q and %q", alphaCandidate, betaCandidate)
	}
	before := mutationVersion(t, h)
	notes := filepath.Join(h.library, "alpha", "notes.md")
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" ls-tree "*) printf 'an edit made meanwhile\n' > `+shellWord(notes)+` || exit 1 ;;
esac
exec `+realGit(t)+` "$@"
`)

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 6)
	refusal := "alpha changed while it was being updated, so nothing was changed"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), refusal)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "1 of 2 skills could not be updated: "+refusal)
	equal(t, "hint", e["hint"], "run 'agentx skill update alpha' again to update it as it is now")
	equal(t, "updated", updatedNames(h, out.stdout), "beta")

	claude := filepath.Join(h.home, ".claude", "skills")
	cursor := filepath.Join(h.home, ".cursor", "skills")
	refs = h.refMap()
	equal(t, "beta's import branch", refs[lineage.ManagedRef("beta")], betaCandidate)
	equal(t, "beta's candidate", refs[lineage.CandidateRef("beta")], "")
	for _, dir := range []string{h.library, claude, cursor} {
		equal(t, "beta's notes in "+dir, fileBody(t, filepath.Join(dir, "beta", "notes.md")), "beta notes, revised\n")
	}
	equal(t, "alpha's import branch", refs[lineage.ManagedRef("alpha")], alphaTip)
	equal(t, "alpha's candidate", refs[lineage.CandidateRef("alpha")], alphaCandidate)
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
// the warning the check gave. The name is read from the candidate's
// SKILL.md whether the update replaces the library directory, as here, or
// merges edits into it.
func TestSkillUpdateKeepsTheLocalNameThroughAnUpstreamRename(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	s.skill("skills/alpha-dir", "alpha-renamed", "The first skill, renamed upstream", nil)
	s.commit("alpha renamed")
	h.mustRun("skill", "check-updates")
	candidate := h.ref(lineage.CandidateRef("alpha"))

	out := h.mustRun("--json", "skill", "update", "alpha")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		`alpha: the update names the skill "alpha-renamed"; updating it keeps the name alpha`)
	refs := h.refMap()
	equal(t, "the import branch", refs[lineage.ManagedRef("alpha")], candidate)
	equal(t, "a branch of the new name", refs[lineage.ManagedRef("alpha-renamed")], "")
	nothingAt(t, "a library directory of the new name", filepath.Join(h.library, "alpha-renamed"))
	contains(t, "the SKILL.md", fileBody(t, filepath.Join(h.library, "alpha", "SKILL.md")), "name: alpha-renamed\n")
	sameTree(t, "claude's copy", libraryTree(t, filepath.Join(h.home, ".claude", "skills", "alpha")), libraryTree(t, filepath.Join(h.library, "alpha")))
	ev := h.one(out.stdout, "library_skill")
	equal(t, "name", ev["name"], "alpha")
	equal(t, "state", ev["state"], stateCurrent)
}

// TestSkillUpdateOfASkillAtTheRootOfItsSource: a skill that is its whole
// repository has the repository's name as its upstream directory, and its
// import tree holds it under that name, so it updates and merges like any
// other: replaced when unedited, an edit the update leaves alone kept, and
// one it overlaps conflicting at a path relative to the skill's directory.
// The text says a skill with no update is up to date, and an update on
// one line.
func TestSkillUpdateOfASkillAtTheRootOfItsSource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("rooted", true)
	s.skill("", "rooted", "A skill at the root", map[string]string{"notes.md": "root notes\n", "usage.md": "root usage\n"})
	first := s.commit("first version")
	h.mustRun("skill", "add", s.url)
	equal(t, "the text with no update", h.mustRun("skill", "update", "rooted").stdout,
		"rooted is up to date as of the last update check; run agentx skill check-updates to look again\n")
	s.write("notes.md", "root notes, revised\n")
	second := s.commit("second version")
	h.mustRun("skill", "check-updates")
	candidate := h.ref(lineage.CandidateRef("rooted"))
	if candidate == "" {
		t.Fatal("the check pinned no candidate")
	}

	out := h.mustRun("skill", "update", "rooted")
	equal(t, "the text", out.stdout, "✓ updated rooted from "+first[:7]+" to "+second[:7]+"\n")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("rooted")), candidate)
	skillMD := fileBody(t, filepath.Join(s.work, "SKILL.md"))
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "rooted")), map[string]string{
		"SKILL.md": skillMD, "notes.md": "root notes, revised\n", "usage.md": "root usage\n",
	})
	equal(t, "state", h.listed("rooted")["state"], stateCurrent)

	editLibrary(t, h, "rooted", "usage.md", "root usage, edited here\n")
	s.write("notes.md", "root notes, revised again\n")
	s.commit("third version")
	h.mustRun("skill", "check-updates")
	contains(t, "the text of a merge", h.mustRun("skill", "update", "rooted").stdout, " and merged its edits cleanly\n")
	sameTree(t, "the merged library directory", libraryTree(t, filepath.Join(h.library, "rooted")), map[string]string{
		"SKILL.md": skillMD, "notes.md": "root notes, revised again\n", "usage.md": "root usage, edited here\n",
	})
	equal(t, "state once merged", h.listed("rooted")["state"], stateModified)

	s.write("usage.md", "root usage, revised\n")
	s.commit("fourth version")
	h.mustRun("skill", "check-updates")
	conflicted := h.run("--json", "skill", "update", "rooted")
	equal(t, "exit of the update that conflicts", conflicted.exit, 4)
	equal(t, "files", conflictPaths(h.one(conflicted.stdout, "conflict")), "usage.md")
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

// TestSkillUpdateRecoversAtEveryBoundary kills an update with SIGKILL at
// the two boundaries of its own: once its journal is on disk, before any
// ref or path changed, and for real right after its last live write, the
// deletion of the candidate, with the journal not yet told. Every boundary
// in between is one of the journal's, which
// home.TestUpdateRecoversFromEveryBoundary replays without git. The
// journal holds the update's steps in the order it applies them, and the
// next command recovers either one, and the update is then whole: the
// branch at the candidate, the candidate gone, the library and the
// unedited copy holding the new version, the library with the .DS_Store
// git ignores in it, the edited copy kept, and nothing staged or retained
// left behind.
func TestSkillUpdateRecoversAtEveryBoundary(t *testing.T) {
	t.Parallel()
	for _, last := range []bool{false, true} {
		name := "killed once its journal is on disk"
		if last {
			name = "killed after its last live write"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			claude := filepath.Join(h.home, ".claude", "skills", "alpha")
			cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")
			editCopy(t, cursor)
			edited := libraryTree(t, cursor)
			writeFile(t, filepath.Join(h.library, "alpha", ".DS_Store"), "finder data\n")
			checked(t, h, s)
			want := secondTree(t, s)
			refs := h.refMap()
			tip, candidate := refs[lineage.ManagedRef("alpha")], refs[lineage.CandidateRef("alpha")]

			if last {
				out := killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", lastWriteScript)
				if h.ref(lineage.CandidateRef("alpha")) != "" {
					t.Fatalf("the update was not killed after it deleted the candidate:\n%s", out)
				}
				equal(t, "journals the killed update left", journalCount(t, h), 1)
			} else {
				killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", killedUpdateScript)
				_, kinds := journalKinds(t, h)
				equal(t, "the journal's steps", kinds, "ref, remove, publish, remove, publish, ref")
				equal(t, "the import branch when the update was killed", h.ref(lineage.ManagedRef("alpha")), tip)
				equal(t, "alpha's notes when the update was killed", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes\n")
			}

			if got := h.run("config", "set", "label", "recovered"); got.exit != 0 {
				t.Fatalf("the command after the killed update: exit %d\n%s", got.exit, got.stderr)
			}
			equal(t, "journals after recovery", journalCount(t, h), 0)
			refs = h.refMap()
			equal(t, "the import branch", refs[lineage.ManagedRef("alpha")], candidate)
			equal(t, "the candidate ref", refs[lineage.CandidateRef("alpha")], "")
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), withFile(want, ".DS_Store", "finder data\n"))
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
// its journal is on disk and leaves the machine as a process killed later
// would: after the import branch moved, and after the library directory
// was retained too. Running the update again, by name or with --all,
// finishes that journal before it judges anything: the import branch the
// journal moves first would otherwise read as the update applied while
// the library still held the version replaced, or as a managed skill whose
// library directory is gone. The run then answers for the machine as the
// recovery left it, with the update applied and nothing left to update.
func TestSkillUpdateRunAgainFinishesTheUpdateThatStopped(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args    []string
		stop    int
		summary string
	}{
		{[]string{"skill", "update", "alpha"}, 2, upToDate("alpha")},
		{[]string{"skill", "update", "--all"}, 1, "nothing to update: no managed skill or fork has an update as of the last update check; run 'agentx skill check-updates' to look again"},
	} {
		t.Run(fmt.Sprintf("%s after %d steps", strings.Join(c.args, " "), c.stop), func(t *testing.T) {
			t.Parallel()
			h, s, _ := updateHarness(t)
			checked(t, h, s)
			want := secondTree(t, s)
			candidate := h.ref(lineage.CandidateRef("alpha"))
			killedChild(t, h, "TestUpdateChildProcess", updateChildEnv, "alpha", killedUpdateScript)
			applyUpdateSteps(t, h, readJournal(t, h), c.stop)

			out := h.run(append([]string{"--json"}, c.args...)...)
			equal(t, "exit", out.exit, 0)
			equal(t, "journals", journalCount(t, h), 0)
			refs := h.refMap()
			equal(t, "the import branch", refs[lineage.ManagedRef("alpha")], candidate)
			equal(t, "the candidate ref", refs[lineage.CandidateRef("alpha")], "")
			sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), want)
			sameTree(t, "claude's copy", libraryTree(t, filepath.Join(h.home, ".claude", "skills", "alpha")), want)
			sameTree(t, "cursor's copy", libraryTree(t, filepath.Join(h.home, ".cursor", "skills", "alpha")), want)
			equal(t, "what is left beside the library", strings.Join(hiddenEntries(t, h.library), " "), "")
			if strings.Contains(out.stdout+out.stderr, "no skill directory") {
				t.Errorf("the run answered for the library directory the stopped update retained:\n%s%s", out.stdout, out.stderr)
			}
			equal(t, "summary", h.one(out.stdout, "result")["summary"], c.summary)
			equal(t, "state", h.listed("alpha")["state"], stateCurrent)
		})
	}
}

// TestSkillUpdateStoppedBeforeItsJournalChangesNothing stops an update with
// SIGTERM while it reads the version it is about to lay out, before the
// lock and before any journal: the run answers with exit code 9 and leaves
// the machine as it was, with nothing staged, and the next update applies.
// A stop once the journal is on disk is the crash boundary
// TestATerminalCtrlCDuringTheJournalIsTheCrashBoundary stops an install
// at, which the recovery TestSkillUpdateRecoversAtEveryBoundary kills an
// update at finishes.
func TestSkillUpdateStoppedBeforeItsJournalChangesNothing(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	candidate := h.ref(lineage.CandidateRef("alpha"))
	was := h.unchangedHome()
	ready := filepath.Join(t.TempDir(), "reading")
	path := h.env["PATH"]
	hangingGit(t, h, "cat-file", ready)

	code, stderr := signalled(t, h, stopRun{ready: ready, sigs: []syscall.Signal{syscall.SIGTERM},
		args: []string{"skill", "update", "alpha", "--color", "off"}})
	killLeftover(t, ready)
	h.env["PATH"] = path

	equal(t, "the exit code of a stopped update", code, exitInterrupted.exit)
	contains(t, "stderr", stderr, "error: interrupted")
	was.check(t, h, "the stopped update", 0)

	h.mustRun("skill", "update", "alpha")
	equal(t, "the import branch after the next update", h.ref(lineage.ManagedRef("alpha")), candidate)
}

// TestSkillUpdateSweepsStagingAKilledUpdateLeft stands in for an update
// killed after it staged the new version beside the library directory and
// a refreshed copy beside each copy placement, and before its journal was
// written: nothing names any of them, so the next update sweeps them all
// before it stages anything, and leaves nothing beside the library or a
// copy. A run over every skill, which sweeps as a run of one name does,
// sweeps the client directories two skills share once, before either
// stages its copy there, so that no sweep takes a copy the run itself
// staged a moment earlier.
func TestSkillUpdateSweepsStagingAKilledUpdateLeft(t *testing.T) {
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

	h.mustRun("skill", "update", "--all")
	for _, dir := range []string{h.library, claude, cursor} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "journals", journalCount(t, h), 0)
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	want := secondTree(t, s)
	for _, dir := range []string{h.library, claude, cursor} {
		sameTree(t, "alpha in "+dir, libraryTree(t, filepath.Join(dir, "alpha")), want)
		equal(t, "beta's notes in "+dir, fileBody(t, filepath.Join(dir, "beta", "notes.md")), "beta notes, revised\n")
	}
}

// editedMidway runs an update of alpha whose library directory is edited
// after the journal is on disk, as the import branch moves. The step that
// would take the directory out of the way finds something it did not
// capture and refuses, and the journal waits for the user. It returns the
// candidate the update was applying and the path of alpha's notes.md. A
// copy edited midway meets the same refusal, a remove step finding what it
// did not capture, which home.TestUpdateKeepsItsCandidateWhenTheRemoveRefuses
// holds a journal to, and a directory edited before a recovery is
// home.TestUpdateRecoveryKeepsItsCandidateWhenTheRemoveRefuses's.
func editedMidway(t *testing.T) (h *harness, s *sourceRepo, candidate, notes string) {
	t.Helper()
	h, s, _ = updateHarness(t)
	checked(t, h, s)
	candidate = h.ref(lineage.CandidateRef("alpha"))
	notes = filepath.Join(h.library, "alpha", "notes.md")
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
exec `+realGit(t)+` "$@"
`)
	out := h.run("--json", "skill", "update", "alpha")
	h.env["PATH"] = path

	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "recovery required")
	refs := h.refMap()
	equal(t, "the candidate ref", refs[lineage.CandidateRef("alpha")], candidate)
	equal(t, "the import branch, moved before the paths", refs[lineage.ManagedRef("alpha")], candidate)
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
	equal(t, "summary", h.one(out.stdout, "result")["summary"], upToDate("alpha"))
}

// TestSkillUpdateWhoseJournalWasMovedAsideOffersNoUpdate: the user keeps
// the edit made midway by moving the waiting journal aside. The import
// branch already holds the new version and the candidate ref still names
// it, which is no update: skill list shows the skill modified against the
// new version with no update available, skill diff --update knows of no
// update, skill update says the skill is up to date rather than
// discarding the edit, and update --all has nothing to do. The next check
// deletes the leftover ref and announces nothing.
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
	diff := h.run("--json", "skill", "diff", "alpha", "--update")
	equal(t, "exit of skill diff --update", diff.exit, 6)
	equal(t, "skill diff --update", h.one(diff.stdout, "error")["message"], "no update of alpha is known")
	update := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit of skill update", update.exit, 0)
	equal(t, "skill update", h.one(update.stdout, "result")["summary"], upToDate("alpha"))
	all := h.mustRun("skill", "update", "--all")
	contains(t, "skill update --all", all.stdout, "Nothing to update")
	equal(t, "alpha's notes", fileBody(t, notes), "an edit made midway\n")
	equal(t, "the candidate ref before a check", h.ref(lineage.CandidateRef("alpha")), candidate)

	check := h.mustRun("--json", "skill", "check-updates")
	if got := h.eventsOfType(check.stdout, "update_available"); len(got) != 0 {
		t.Errorf("the check announced %v", got)
	}
	refs := h.refMap()
	equal(t, "the candidate ref after a check", refs[lineage.CandidateRef("alpha")], "")
	equal(t, "the import branch after a check", refs[lineage.ManagedRef("alpha")], candidate)
	equal(t, "state after a check", h.listed("alpha")["state"], stateModified)
}

// TestSkillUpdateOffersNoUpdateWhoseLineageItCannotRead: a candidate ref
// that holds a commit whose trailers agentx cannot read names no version
// agentx could lay out or record, so it is no update to any command that
// shows or applies one. skill list shows no candidate and no update
// available, skill diff --update knows of no update, skill update says
// the skill is up to date, and update --all has nothing to do; none of
// them touches the ref, and the skill stays current at its base.
func TestSkillUpdateOffersNoUpdateWhoseLineageItCannotRead(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	lineagelessCandidate(t, h, s)
	refs := h.refMap()
	tip, candidate := refs[lineage.ManagedRef("alpha")], refs[lineage.CandidateRef("alpha")]

	listed := h.listed("alpha")
	equal(t, "state", listed["state"], stateCurrent)
	if _, ok := listed["candidate"]; ok {
		t.Errorf("the skill lists a candidate whose lineage agentx cannot read: %v", listed["candidate"])
	}
	if list := h.mustRun("skill", "list").stdout; strings.Contains(list, updateAvailable) {
		t.Errorf("skill list offers an update whose lineage agentx cannot read:\n%s", list)
	}
	diff := h.run("--json", "skill", "diff", "alpha", "--update")
	equal(t, "exit of skill diff --update", diff.exit, 6)
	equal(t, "skill diff --update", h.one(diff.stdout, "error")["message"], "no update of alpha is known")
	update := h.run("--json", "skill", "update", "alpha")
	equal(t, "exit of skill update", update.exit, 0)
	equal(t, "skill update", h.one(update.stdout, "result")["summary"], upToDate("alpha"))
	all := h.mustRun("skill", "update", "--all")
	contains(t, "skill update --all", all.stdout, "Nothing to update")

	refs = h.refMap()
	equal(t, "the candidate ref", refs[lineage.CandidateRef("alpha")], candidate)
	equal(t, "the import branch", refs[lineage.ManagedRef("alpha")], tip)
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes\n")
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillUpdateRefusesACandidateStoredInAnOlderForm: a candidate whose
// import commit stores its version over a source's own tree, legacy modes
// and all, is a version no library directory is ever current against, so
// applying it would leave the skill modified at once. The update refuses it
// for what the account repo holds and changes nothing; a check, which
// finds the source holding the version the branch names, drops it. A run
// over every skill refuses it while it reads the versions it lays out, as
// it refuses beta's candidate in
// TestSkillUpdateAllReportsEachRefusalAndGoesOn, and goes on.
func TestSkillUpdateRefusesACandidateStoredInAnOlderForm(t *testing.T) {
	t.Parallel()
	h, s, _ := legacyHarness(t)
	legacy, canonical := h.storeInOlderForm(t, s)
	h.accountGit("update-ref", lineage.ManagedRef("nc"), canonical, legacy)
	h.accountGit("update-ref", lineage.CandidateRef("nc"), legacy)
	was := h.unchangedHome()

	out := h.run("--json", "skill", "update", "nc")
	equal(t, "exit", out.exit, 8)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "the update candidate refs/agentx/candidate/nc stores its version in a form agentx does not write")
	equal(t, "hint", e["hint"], "run 'agentx skill check-updates' to pin the update again")
	was.check(t, h, "the refused update", 0)

	h.mustRun("skill", "check-updates")
	equal(t, "the candidate after a check", h.ref(lineage.CandidateRef("nc")), "")
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
	h.mustRun("skill", "check-updates")
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
