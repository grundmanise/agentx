package cli

import (
	"context"
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
	for _, dir := range []string{h.library, filepath.Dir(claude), filepath.Dir(cursor)} {
		equal(t, "what is left beside "+dir, strings.Join(hiddenEntries(t, dir), " "), "")
	}
	equal(t, "journals", journalCount(t, h), 0)

	listed := h.listed("alpha")
	equal(t, "state after the update", listed["state"], stateCurrent)
	equal(t, "upstream_commit after the update", listed["upstream_commit"], second)
	if strings.Contains(h.mustRun("skill", "list").stdout, updateAvailable) {
		t.Error("skill list still shows an update after it was applied")
	}
	check := h.mustRun("--json", "skill", "check")
	if got := h.eventsOfType(check.stdout, "update_available"); len(got) != 0 {
		t.Errorf("a check right after the update found %d updates: %v", len(got), got)
	}
	equal(t, "the candidate ref after a check", h.ref(lineage.CandidateRef("alpha")), "")
}

// mixedHarness is a source of four skills, all installed from its first
// commit and symlinked into Claude Code, and a second commit after which a
// check finds: an update for alpha, which nobody edited; an update for
// beta, which the test then edits in the library; gamma gone from the
// source; and delta as it was.
func mixedHarness(t *testing.T) (h *harness, s *sourceRepo, first, second string) {
	t.Helper()
	h = newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s = h.newSourceRepo("skills", true)
	for _, name := range []string{"alpha", "beta", "gamma", "delta"} {
		s.skill("skills/"+name, name, "The skill "+name, map[string]string{"notes.md": name + " notes\n"})
	}
	first = s.commit("first version")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	s.write("skills/alpha/notes.md", "alpha notes, revised\n")
	s.write("skills/beta/notes.md", "beta notes, revised\n")
	s.run("rm", "-r", "--quiet", "skills/gamma")
	second = s.commit("second version")
	h.mustRun("skill", "check")
	editLibrary(t, h, "beta", "notes.md", "beta notes, edited here\n")
	return h, s, first, second
}

// TestSkillUpdateAllUpdatesEveryUnmodifiedSkill runs update --all over a
// mix: the unmodified skill with an update is updated; the modified one is
// skipped with a warning that says why and what to do, and keeps its
// candidate and its edit; the upstream-removed skill and the one with no
// update are not touched. Skipping a modified skill is no failure, so the
// run exits 0, and the text says what it did in one line and a row.
func TestSkillUpdateAllUpdatesEveryUnmodifiedSkill(t *testing.T) {
	t.Parallel()
	h, _, first, second := mixedHarness(t)
	betaCandidate := h.ref(lineage.CandidateRef("beta"))
	gammaMarker := h.ref(lineage.UpstreamRemovedRef("gamma"))
	deltaTip := h.ref(lineage.ManagedRef("delta"))
	betaTip := h.ref(lineage.ManagedRef("beta"))
	gammaTree := libraryTree(t, filepath.Join(h.library, "gamma"))
	if betaCandidate == "" || gammaMarker == "" {
		t.Fatalf("the check left beta's candidate %q and gamma's marker %q", betaCandidate, gammaMarker)
	}

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 0)
	events := h.eventsOfType(out.stdout, "library_skill")
	if len(events) != 1 || events[0]["name"] != "alpha" {
		t.Fatalf("library_skill events %v, want alpha's alone", events)
	}
	equal(t, "alpha's state", events[0]["state"], stateCurrent)
	equal(t, "alpha's upstream_commit", events[0]["upstream_commit"], second)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "updated 1 skill, 1 modified skill skipped")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		"beta was edited since it was installed, so it was not updated: merging a modified skill with its update is not supported yet;"+
			" run 'agentx skill diff beta' to see the edits, or 'agentx skill revert beta' to discard them and update it")
	equal(t, "alpha's notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, revised\n")
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "beta's notes", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes, edited here\n")
	equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), betaCandidate)
	equal(t, "beta's branch", h.ref(lineage.ManagedRef("beta")), betaTip)
	equal(t, "gamma's marker", h.ref(lineage.UpstreamRemovedRef("gamma")), gammaMarker)
	sameTree(t, "gamma's library directory", libraryTree(t, filepath.Join(h.library, "gamma")), gammaTree)
	equal(t, "delta's branch", h.ref(lineage.ManagedRef("delta")), deltaTip)

	// Once more, in text: nothing is left to update but beta, which is
	// skipped again.
	h.mustRun("skill", "revert", "beta")
	text := h.mustRun("skill", "update", "--all")
	equal(t, "the text", text.stdout, "✓ updated 1 skill\n  beta  "+first[:7]+" -> "+second[:7]+"\n")
	equal(t, "beta's notes after the revert and update", fileBody(t, filepath.Join(h.library, "beta", "notes.md")), "beta notes, revised\n")
	nothing := h.mustRun("skill", "update", "--all")
	equal(t, "the text of a run with nothing to update", nothing.stdout,
		"Nothing to update: no managed skill has an update as of the last update check. Run agentx skill check to look again.\n")
}

// TestSkillUpdateAllSkipsOnlyModifiedSkills: a run whose every skill with
// an update is modified updates nothing and says so, and still exits 0.
func TestSkillUpdateAllSkipsOnlyModifiedSkills(t *testing.T) {
	t.Parallel()
	h, _, _, _ := mixedHarness(t)
	h.mustRun("skill", "update", "alpha")
	out := h.run("skill", "update", "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "the text", out.stdout, "no skill was updated, 1 modified skill skipped\n")
	contains(t, "the warning", out.stderr, "warning: beta was edited since it was installed, so it was not updated")
}

// TestSkillUpdateAllReportsEachRefusalAndGoesOn: a run over every skill
// with an update gives up on each skill that refuses, names it in a
// warning, and updates the rest. Its exit code is the one the refusals
// agree on, here the removed source's 5 alone, and 6 once a second
// refusal disagrees; the error names every skill it gave up on.
func TestSkillUpdateAllReportsEachRefusalAndGoesOn(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	other := h.newSourceRepo("other", true)
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
	h.mustRun("source", "remove", other.url)

	out := h.run("--json", "skill", "update", "--all")
	equal(t, "exit", out.exit, 5)
	gammaRefusal := "gamma was installed from " + other.url + ", which was removed from this machine, so it is not updated"
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "gamma: "+gammaRefusal)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "not_found")
	equal(t, "message", e["message"], "1 of 3 skills could not be updated: gamma: "+gammaRefusal)
	equal(t, "hint", e["hint"], "run 'agentx source add "+other.url+"' to add it again")
	var updated []string
	for _, ev := range h.eventsOfType(out.stdout, "library_skill") {
		updated = append(updated, ev["name"].(string))
	}
	equal(t, "updated", strings.Join(updated, ", "), "alpha, beta")
	if h.ref(lineage.CandidateRef("gamma")) == "" {
		t.Error("gamma's candidate went with a refused update")
	}

	// A second refusal of another kind: delta's library directory is gone.
	s.skill("skills/delta", "delta", "The fourth skill", nil)
	s.commit("delta")
	h.mustRun("skill", "add", s.url, "--skill", "delta", "--fetch")
	s.skill("skills/delta", "delta", "The fourth skill, revised", nil)
	s.commit("delta revised")
	h.mustRun("skill", "check")
	remove(t, filepath.Join(h.library, "delta"))
	mixed := h.run("--json", "skill", "update", "--all")
	equal(t, "exit of a run whose refusals disagree", mixed.exit, 6)
	e = h.one(mixed.stdout, "error")
	equal(t, "code", e["code"], "refused")
	contains(t, "message", e["message"].(string), "2 of 2 skills could not be updated: delta: delta is managed in the account repo but the library holds no skill directory for it, so there is nothing to update; gamma: ")
	equal(t, "hint", e["hint"], "run 'agentx skill list' to see which skills have an update, then update the rest one at a time")
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

// TestSkillUpdateRefusesInOrder is every skill update refuses, one name at
// a time, each with its code, its message and its hint, and each where the
// order of the checks puts it: a removed source before an upstream that no
// longer holds the skill, and both before whether there is an update at
// all, which comes before whether the skill was edited. A skill with no
// update is nothing to do and exits 0. None of them changes a ref, the
// library or a placement, or leaves a journal.
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
			name: "an edited skill with an update", skill: "alpha", exit: 6,
			setup: func(t *testing.T, h *harness, s *sourceRepo) {
				checked(t, h, s)
				editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited\n")
			},
			message: "alpha was edited since it was installed, and merging a modified skill with its update is not supported yet",
			hint:    "run 'agentx skill diff alpha' to see the edits, or 'agentx skill revert alpha' to discard them and then 'agentx skill update alpha'",
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
// of the name made meanwhile, a check that finds the source no longer holds
// the skill, and the source removed from this machine. The update then
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
			hint:    "run 'agentx skill diff alpha' to see the change; a skill edited since it was installed is not updated",
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
				settings, err := home.LoadSettings(h.agentx)
				if err != nil {
					t.Fatal(err)
				}
				settings.RemoveSource(s.url)
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
			},
			exit:   5,
			branch: "tip", candidate: "candidate", notes: "alpha notes\n",
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

// TestSkillUpdateKeepsItsCandidateWhenTheLibraryChangesMidway: the library
// directory is edited after the journal is on disk, as the import branch
// moves. The step that would take the directory out of the way finds
// something it did not capture and refuses, so the directory keeps the
// edit, the candidate ref still names the version being applied, since the
// journal deletes it last, and the journal waits for the user. Restoring
// the directory lets the next command finish the update.
func TestSkillUpdateKeepsItsCandidateWhenTheLibraryChangesMidway(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	want := secondTree(t, s)
	candidate := h.ref(lineage.CandidateRef("alpha"))
	notes := filepath.Join(h.library, "alpha", "notes.md")
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

	equal(t, "exit", out.exit, 6)
	contains(t, "message", h.one(out.stdout, "error")["message"].(string), "recovery required")
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the import branch, moved before the paths", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "alpha's notes", fileBody(t, notes), "an edit made midway\n")
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
