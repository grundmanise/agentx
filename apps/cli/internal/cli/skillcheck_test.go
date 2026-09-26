package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// checkHarness is a harness with one configuration and a source of two
// skills, alpha and beta, both installed from its first commit. The source
// has moved on from nothing yet, so a check finds no update until a test
// commits one.
func checkHarness(t *testing.T) (*harness, *sourceRepo, string) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	s.skill("skills/beta", "beta", "The second skill", nil)
	first := s.commit("first version")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	return h, s, first
}

// updateOf is the one update_available event of the named skill in a run,
// failing the test when there is not exactly one.
func (h *harness) updateOf(text, name string) jsonEvent {
	h.t.Helper()
	var found []jsonEvent
	for _, e := range h.eventsOfType(text, "update_available") {
		if e["name"] == name {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		h.t.Fatalf("%d update_available events for %s, want 1:\n%s", len(found), name, text)
	}
	return found[0]
}

// files is the files of an update_available event as "<status> <path>".
func files(e jsonEvent) string {
	var out []string
	for _, f := range e["files"].([]any) {
		m := f.(map[string]any)
		out = append(out, m["status"].(string)+" "+m["path"].(string))
	}
	return strings.Join(out, ", ")
}

// ref is what a ref of the account repo holds, "" when it does not exist,
// read with plain git.
func (h *harness) ref(name string) string {
	h.t.Helper()
	return h.accountGit("for-each-ref", "--format=%(objectname)", name)
}

// TestSkillCheckReportsAnUpdateAndPinsIt is the check on a machine with two
// sources, one of which cannot be reached. The reachable one is fetched,
// compared and recorded: its changed skill is reported with the files the
// newer version changes, relative to the skill's directory, and pinned
// under its candidate ref, which a second invocation reads back with plain
// git; the skill beside it is current and gets no candidate. The other
// source is one warning naming it and the skill it left unchecked, and the
// run exits with its code. The reachable source's last_fetched is
// rewritten and the other's is left as it was. The library is not touched.
func TestSkillCheckReportsAnUpdateAndPinsIt(t *testing.T) {
	t.Parallel()
	h, s, first := checkHarness(t)
	other := h.newSourceRepo("other", true)
	other.skill("gamma", "gamma", "A skill of the other source", nil)
	other.commit("gamma")
	h.mustRun("skill", "add", other.url)
	base := h.listed("alpha")

	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes, revised\n", "extra.md": "new\n"})
	second := s.commit("second version")
	if err := os.Rename(other.gitDir, other.gitDir+".gone"); err != nil {
		t.Fatal(err)
	}

	backdate(t, h)
	before := mutationVersion(t, h)
	out := h.run("--json", "skill", "check")
	equal(t, "exit", out.exit, 3)
	// One write for the candidate and last_fetched, which bumps the version
	// file once: that bump is what makes a serve rescan.
	equal(t, "mutations", mutationVersion(t, h), before+1)
	refetched(t, h, s.url)
	equal(t, "last_fetched of the unreachable source", lastFetched(t, h, other.url), backdated)
	progress := h.eventsOfType(out.stdout, "progress")
	equal(t, "progress events", len(progress), 2)
	for _, p := range progress {
		equal(t, "phase", p["phase"], "fetch")
		equal(t, "total", p["total"], float64(2))
	}
	up := h.updateOf(out.stdout, "alpha")
	equal(t, "kind", up["kind"], "managed")
	equal(t, "source", up["source"], s.url)
	equal(t, "subpath", up["subpath"], "skills/alpha")
	equal(t, "base_hash", up["base_hash"], base["base_hash"])
	equal(t, "upstream_commit", up["upstream_commit"], first)
	equal(t, "candidate_upstream_commit", up["candidate_upstream_commit"], second)
	equal(t, "files", files(up), "added extra.md, modified notes.md")
	if _, ok := up["upstream_name"]; ok {
		t.Errorf("an update that keeps the name carries upstream_name %v", up["upstream_name"])
	}
	if got := h.eventsOfType(out.stdout, "update_available"); len(got) != 1 {
		t.Errorf("%d update_available events, want alpha's alone:\n%s", len(got), out.stdout)
	}
	errEvent := h.one(out.stdout, "error")
	equal(t, "error code", errEvent["code"], "source")
	contains(t, "error", errEvent["message"].(string), other.url)
	contains(t, "error", errEvent["message"].(string), "not checked: gamma")
	result := h.one(out.stdout, "result")
	equal(t, "ok", result["ok"], false)
	warned := strings.Join(warnings(h, out.stderr), "\n")
	contains(t, "the warning", warned, other.url)
	contains(t, "the warning", warned, "not checked: gamma")

	// Read back by a second invocation, with plain git: the candidate is the
	// import commit of the newer version, parentless and with its four
	// trailers, and beta has none.
	candidate := h.ref(lineage.CandidateRef("alpha"))
	equal(t, "the candidate ref", candidate, up["candidate"])
	body := h.accountGit("cat-file", "commit", candidate)
	if strings.Contains(body, "\nparent ") {
		t.Errorf("the candidate has a parent:\n%s", body)
	}
	for _, trailer := range []string{"Agentx-Source: " + s.url, "Agentx-Path: skills/alpha", "Agentx-Upstream-Commit: " + second, "Agentx-Content-Hash: " + up["candidate_hash"].(string)} {
		contains(t, "the candidate", body, trailer)
	}
	equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), "")
	equal(t, "the source ref", h.accountGit("rev-parse", "refs/agentx/sources/"+source.ID(s.url)), second)

	// The check applied nothing: the library holds the version it did, and
	// skill list shows the update beside it.
	listed := h.listed("alpha")
	equal(t, "state", listed["state"], stateCurrent)
	equal(t, "notes", string(readFile(t, filepath.Join(h.library, "alpha", "notes.md"))), "alpha notes\n")
	cand, ok := listed["candidate"].(map[string]any)
	if !ok {
		t.Fatalf("skill list carries no candidate: %v", listed)
	}
	equal(t, "candidate.upstream_commit", cand["upstream_commit"], second)
	equal(t, "candidate.content_hash", cand["content_hash"], up["candidate_hash"])
	if _, ok := h.listed("beta")["candidate"]; ok {
		t.Error("beta, which has no update, carries a candidate")
	}
	contains(t, "skill list", h.mustRun("skill", "list").stdout, "  alpha  managed  current, update available  ")
	if entry := h.snapshotLibrary("alpha"); entry["candidate"] == nil {
		t.Errorf("the snapshot's entry carries no candidate: %v", entry)
	}
}

// TestSkillCheckLeavesAnUnreachableSourcesSkillsAsTheyAre: a source that
// cannot be fetched says nothing about what it holds now, so the candidate
// and the upstream-removed marker its skills got from the last check stay
// as they are, and so do the settings: a check whose every fetch failed
// writes nothing at all, not even last_fetched, and bumps no version.
func TestSkillCheckLeavesAnUnreachableSourcesSkillsAsTheyAre(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	s.run("rm", "-r", "--quiet", "skills/beta")
	second := s.commit("alpha revised, beta removed")
	h.mustRun("skill", "check")
	candidate, marker := h.ref(lineage.CandidateRef("alpha")), h.ref(lineage.UpstreamRemovedRef("beta"))
	if candidate == "" || marker == "" {
		t.Fatalf("candidate %q and marker %q before the source went", candidate, marker)
	}
	settings := string(readFile(t, filepath.Join(h.agentx, "settings.json")))

	s.skill("skills/alpha", "alpha", "The first skill, revised again", nil)
	s.skill("skills/beta", "beta", "The second skill, back", nil)
	s.commit("alpha revised again, beta back")
	if err := os.Rename(s.gitDir, s.gitDir+".gone"); err != nil {
		t.Fatal(err)
	}
	before := mutationVersion(t, h)
	out := h.run("--json", "skill", "check")
	equal(t, "exit", out.exit, 3)
	contains(t, "error", h.one(out.stdout, "error")["message"].(string), "not checked: alpha, beta")
	if got := h.eventsOfType(out.stdout, "update_available"); len(got) != 0 {
		t.Errorf("update_available events from a source that could not be fetched: %v", got)
	}
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "beta's marker", h.ref(lineage.UpstreamRemovedRef("beta")), marker)
	equal(t, "the settings", string(readFile(t, filepath.Join(h.agentx, "settings.json"))), settings)
	equal(t, "mutations", mutationVersion(t, h), before)
	equal(t, "beta's drift", drift(h.listed("beta")), "upstream removed")
	cand, ok := h.listed("alpha")["candidate"].(map[string]any)
	if !ok {
		t.Fatal("alpha lost its candidate")
	}
	equal(t, "alpha's candidate upstream commit", cand["upstream_commit"], second)
}

// TestSkillCheckWithSeveralFailures ends a check that could not check
// several things the way a fetch of several sources ends: every failure is
// a warning, the refusal names each one and says the warnings do, and the
// exit code is the one they all agree on, or the source-level one when
// they do not. What could be checked is pinned all the same.
func TestSkillCheckWithSeveralFailures(t *testing.T) {
	t.Parallel()
	// withOther is checkHarness plus a second source, other, holding gamma,
	// installed from it.
	withOther := func(t *testing.T) (*harness, *sourceRepo, *sourceRepo) {
		h, s, _ := checkHarness(t)
		other := h.newSourceRepo("other", true)
		other.skill("gamma", "gamma", "A skill of the other source", nil)
		other.commit("gamma")
		h.mustRun("skill", "add", other.url)
		return h, s, other
	}
	unreachable := func(t *testing.T, s *sourceRepo) {
		if err := os.Rename(s.gitDir, s.gitDir+".gone"); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a source and a skill", func(t *testing.T) {
		t.Parallel()
		h, s, other := withOther(t)
		s.write("skills/alpha/we\\ird.md", "a backslash in a name\n")
		s.skill("skills/beta", "beta", "The second skill, revised", nil)
		s.commit("alpha breaks, beta moves on")
		unreachable(t, other)
		out := h.run("--json", "skill", "check")
		equal(t, "exit", out.exit, 3)
		e := h.one(out.stdout, "error")
		equal(t, "code", e["code"], "source")
		message := e["message"].(string)
		contains(t, "message", message, other.url+" (gamma)")
		contains(t, "message", message, "alpha")
		equal(t, "hint", e["hint"], "the warnings name each failure; "+refusalHint(exitSource))
		warned := warnings(h, out.stderr)
		equal(t, "warnings", len(warned), 2)
		var source, skill bool
		for _, w := range warned {
			source = source || strings.Contains(w, other.url) && strings.Contains(w, "not checked: gamma")
			skill = skill || strings.HasPrefix(w, "alpha: ")
		}
		if !source || !skill {
			t.Errorf("the warnings do not name the source and the skill: %q", warned)
		}
		equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), h.updateOf(out.stdout, "beta")["candidate"])
	})

	t.Run("two skills", func(t *testing.T) {
		t.Parallel()
		h, s, _ := checkHarness(t)
		for _, name := range []string{"alpha", "beta"} {
			s.write("skills/"+name+"/we\\ird.md", "a backslash in a name\n")
		}
		s.commit("both break")
		out := h.run("--json", "skill", "check")
		equal(t, "exit", out.exit, 6)
		e := h.one(out.stdout, "error")
		equal(t, "code", e["code"], "refused")
		equal(t, "message", e["message"], "could not check alpha, beta")
		equal(t, "hint", e["hint"], "the warnings name each failure")
		equal(t, "warnings", len(warnings(h, out.stderr)), 2)
	})

	t.Run("two sources", func(t *testing.T) {
		t.Parallel()
		h, s, other := withOther(t)
		unreachable(t, s)
		unreachable(t, other)
		out := h.run("--json", "skill", "check")
		equal(t, "exit", out.exit, 3)
		e := h.one(out.stdout, "error")
		equal(t, "code", e["code"], "source")
		message := e["message"].(string)
		if !strings.HasPrefix(message, "could not check ") {
			t.Errorf("message = %q", message)
		}
		contains(t, "message", message, s.url+" (alpha, beta)")
		contains(t, "message", message, other.url+" (gamma)")
		equal(t, "hint", e["hint"], "the warnings name each failure; "+refusalHint(exitSource))
		equal(t, "warnings", len(warnings(h, out.stderr)), 2)
	})
}

// TestSkillCheckReadsTheUpstreamDirectory covers the two skills whose
// upstream directory is not their library name: one at the root of its
// repository, held under the repository's name, and one whose frontmatter
// names it otherwise than its directory. Each is current until its source
// changes, and its update names every file relative to the skill's own
// directory, in the event and in the diff.
func TestSkillCheckReadsTheUpstreamDirectory(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	solo := h.newSourceRepo("solo", true)
	solo.skill("", "solo", "A skill at the root", map[string]string{"notes.md": "solo notes\n"})
	solo.commit("solo")
	tools := h.newSourceRepo("tools", true)
	tools.skill("tools/pdf-tools", "pdf", "Named by its frontmatter", map[string]string{"notes.md": "pdf notes\n"})
	tools.commit("pdf")
	for _, s := range []*sourceRepo{solo, tools} {
		h.mustRun("source", "add", s.url)
		h.mustRun("skill", "add", s.url)
	}
	out := h.mustRun("--json", "skill", "check")
	if got := h.eventsOfType(out.stdout, "update_available"); len(got) != 0 {
		t.Fatalf("updates before any source changed: %v", got)
	}

	solo.write("notes.md", "solo notes, revised\n")
	solo.commit("solo revised")
	tools.write("tools/pdf-tools/guide.md", "a guide\n")
	tools.commit("pdf guide")
	out = h.mustRun("--json", "skill", "check")
	equal(t, "solo's files", files(h.updateOf(out.stdout, "solo")), "modified notes.md")
	equal(t, "pdf's files", files(h.updateOf(out.stdout, "pdf")), "added guide.md")
	for _, name := range []string{"solo", "pdf"} {
		if _, ok := h.updateOf(out.stdout, name)["upstream_name"]; ok {
			t.Errorf("%s carries an upstream_name", name)
		}
	}
	var paths []string
	for _, name := range []string{"solo", "pdf"} {
		for _, e := range h.eventsOfType(h.mustRun("--json", "skill", "diff", name, "--upstream").stdout, "diff") {
			paths = append(paths, e["path"].(string))
		}
	}
	equal(t, "the diffs' paths", strings.Join(paths, ", "), "notes.md, guide.md")
}

// TestSkillCheckReportsAnUpdateItCannotTake: a newer version holding an
// entry agentx will not lay out is refused for its skill alone, as an
// install refuses it. The check names the skill and the reason, pins
// nothing for it and exits with the install's code, and the update of the
// skill beside it is pinned all the same.
func TestSkillCheckReportsAnUpdateItCannotTake(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.write("skills/alpha/we\\ird.md", "a backslash in a name\n")
	s.skill("skills/beta", "beta", "The second skill, revised", nil)
	s.commit("alpha breaks, beta moves on")
	out := h.run("--json", "skill", "check")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "refused")
	contains(t, "message", e["message"].(string), "could not check alpha: ")
	contains(t, "message", e["message"].(string), `we\\ird.md`)
	contains(t, "the warning", strings.Join(warnings(h, out.stderr), "\n"), "alpha: ")
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), h.updateOf(out.stdout, "beta")["candidate"])
	if got := h.eventsOfType(out.stdout, "update_available"); len(got) != 1 {
		t.Errorf("update_available events = %v, want beta's alone", got)
	}
}

// TestSkillCheckTextOutput is what a person reads: the summary, one line per
// update with the files it changes under it, and how to read one.
func TestSkillCheckTextOutput(t *testing.T) {
	t.Parallel()
	h, s, first := checkHarness(t)
	out := h.mustRun("skill", "check")
	equal(t, "nothing new", out.stdout, "✓ checked 2 skills from 1 source: no update available\n")

	s.skill("skills/beta", "beta", "The second skill, revised", map[string]string{"docs/usage.md": "use it\n"})
	second := s.commit("second version")
	out = h.mustRun("skill", "check")
	equal(t, "stdout", out.stdout, "✓ checked 2 skills from 1 source: 1 update available\n"+
		"  beta  update available  "+short(first)+" -> "+short(second)+"  2 files\n"+
		"    modified  SKILL.md\n"+
		"    added     docs/usage.md\n"+
		"Read an update with agentx skill diff <name> --upstream.\n")
	equal(t, "stderr", out.stderr, "")
}

// TestSkillCheckCandidateIsTheCommitAnInstallWrites: the candidate is
// written through the import an install runs, so installing the version it
// pins, after taking the skill away, writes the very same commit.
func TestSkillCheckCandidateIsTheCommitAnInstallWrites(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"scripts/run.sh": "#!/bin/sh\n"})
	s.executable("skills/alpha/scripts/run.sh")
	s.commit("second version")
	h.mustRun("skill", "check")
	candidate := h.ref(lineage.CandidateRef("alpha"))
	if candidate == "" {
		t.Fatal("the check pinned no candidate")
	}
	h.mustRun("skill", "remove", "alpha")
	equal(t, "the candidate after the removal", h.ref(lineage.CandidateRef("alpha")), "")
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
}

// TestSkillCheckMovesTheCandidate: a check that finds a newer version still
// moves the candidate to it, and one that finds nothing new announces the
// update it pinned before, as it is.
func TestSkillCheckMovesTheCandidate(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	s.commit("second version")
	out := h.mustRun("--json", "skill", "check")
	c1 := h.updateOf(out.stdout, "alpha")["candidate"]
	equal(t, "the first candidate", h.ref(lineage.CandidateRef("alpha")), c1)

	again := h.mustRun("--json", "skill", "check")
	equal(t, "the candidate announced again", h.updateOf(again.stdout, "alpha")["candidate"], c1)
	equal(t, "the warnings of a check that moved nothing", again.stderr, "")

	s.skill("skills/alpha", "alpha", "The first skill, revised again", nil)
	third := s.commit("third version")
	out = h.mustRun("--json", "skill", "check")
	up := h.updateOf(out.stdout, "alpha")
	if up["candidate"] == c1 {
		t.Fatalf("the candidate stayed at %v after a newer version", c1)
	}
	equal(t, "the candidate's upstream commit", up["candidate_upstream_commit"], third)
	equal(t, "the moved candidate", h.ref(lineage.CandidateRef("alpha")), up["candidate"])
}

// TestSkillCheckMarksAndClearsUpstreamRemoved: a directory deleted
// upstream, and one left without its SKILL.md, mark their skills upstream
// removed with a marker ref naming the commit the check fetched, and drop
// the candidate the first had; the skills stay as they are. Once the
// source holds them again the next check clears both markers. A removal
// takes a marker with the skill.
func TestSkillCheckMarksAndClearsUpstreamRemoved(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/gamma", "gamma", "A third skill", map[string]string{"notes.md": "gamma notes\n"})
	s.commit("gamma")
	h.mustRun("skill", "add", s.url, "--skill", "gamma", "--fetch")
	s.skill("skills/beta", "beta", "The second skill, revised", nil)
	s.commit("beta revised")
	h.mustRun("skill", "check")
	if h.ref(lineage.CandidateRef("beta")) == "" {
		t.Fatal("no candidate for beta before it was removed upstream")
	}

	s.run("rm", "-r", "--quiet", "skills/beta", "skills/gamma/SKILL.md")
	gone := s.commit("beta and gamma's SKILL.md removed")
	out := h.mustRun("skill", "check")
	contains(t, "stdout", out.stdout, "✓ checked 3 skills from 1 source: no update available, 2 upstream removed\n")
	contains(t, "stdout", out.stdout, "  beta  upstream removed  "+s.url+"/skills/beta at "+short(gone)+"\n")
	for _, name := range []string{"beta", "gamma"} {
		equal(t, name+"'s marker", h.ref(lineage.UpstreamRemovedRef(name)), gone)
		listed := h.listed(name)
		equal(t, name+"'s drift", drift(listed), "upstream removed")
		equal(t, name+"'s state", listed["state"], stateCurrent)
		if _, err := os.Stat(filepath.Join(h.library, name, "SKILL.md")); err != nil {
			t.Errorf("%s left the library: %v", name, err)
		}
	}
	equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), "")
	contains(t, "skill list", h.mustRun("skill", "list").stdout, "  beta   managed  current, upstream removed  ")

	s.skill("skills/beta", "beta", "The second skill", nil)
	s.skill("skills/gamma", "gamma", "A third skill", nil)
	s.commit("both back")
	h.mustRun("skill", "check")
	for _, name := range []string{"beta", "gamma"} {
		equal(t, name+"'s marker once it is back", h.ref(lineage.UpstreamRemovedRef(name)), "")
		equal(t, name+"'s drift once it is back", drift(h.listed(name)), "")
	}

	s.run("rm", "-r", "--quiet", "skills/beta")
	s.commit("beta removed again")
	h.mustRun("skill", "check")
	if h.ref(lineage.UpstreamRemovedRef("beta")) == "" {
		t.Fatal("no marker for beta")
	}
	h.mustRun("skill", "remove", "beta")
	equal(t, "the marker after the removal", h.ref(lineage.UpstreamRemovedRef("beta")), "")
}

// TestSkillCheckClearsUpstreamRemovedForAVersionItCannotTake: a skill that
// comes back upstream in a version the check refuses, for an entry agentx
// will not lay out, is held by its source again all the same. The check
// names the skill as one it could not check and clears its marker, and the
// candidate an earlier check pinned for the skill beside it, whose newer
// version it refuses too, stays where it is.
func TestSkillCheckClearsUpstreamRemovedForAVersionItCannotTake(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	s.run("rm", "-r", "--quiet", "skills/beta")
	s.commit("alpha revised, beta removed")
	h.mustRun("skill", "check")
	candidate, marker := h.ref(lineage.CandidateRef("alpha")), h.ref(lineage.UpstreamRemovedRef("beta"))
	if candidate == "" || marker == "" {
		t.Fatalf("candidate %q and marker %q before beta came back", candidate, marker)
	}

	s.skill("skills/beta", "beta", "The second skill", nil)
	for _, name := range []string{"alpha", "beta"} {
		s.write("skills/"+name+"/we\\ird.md", "a backslash in a name\n")
	}
	s.commit("beta back, both with an entry agentx will not lay out")
	out := h.run("--json", "skill", "check")
	equal(t, "exit", out.exit, 6)
	equal(t, "message", h.one(out.stdout, "error")["message"], "could not check alpha, beta")
	warned := warnings(h, out.stderr)
	sort.Strings(warned)
	equal(t, "warnings", len(warned), 2)
	for i, name := range []string{"alpha", "beta"} {
		if !strings.HasPrefix(warned[i], name+": ") || !strings.Contains(warned[i], `we\\ird.md`) {
			t.Errorf("warning %d = %q, want %s's refusal", i, warned[i], name)
		}
	}
	equal(t, "beta's marker", h.ref(lineage.UpstreamRemovedRef("beta")), "")
	equal(t, "beta's drift", drift(h.listed("beta")), "")
	equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), "")
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
	if got := h.eventsOfType(out.stdout, "update_available"); len(got) != 0 {
		t.Errorf("update_available events from a check that checked nothing: %v", got)
	}
	if listed := h.mustRun("skill", "list").stdout; strings.Contains(listed, "upstream removed") {
		t.Errorf("skill list still shows upstream removed:\n%s", listed)
	}
}

// TestSkillCheckSkipsARemovedSource: a source removed with source remove is
// not fetched, and the candidate and the marker its skills have stay as
// they are, whatever the source holds by now.
func TestSkillCheckSkipsARemovedSource(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	s.run("rm", "-r", "--quiet", "skills/beta")
	s.commit("alpha revised, beta removed")
	h.mustRun("skill", "check")
	candidate, marker := h.ref(lineage.CandidateRef("alpha")), h.ref(lineage.UpstreamRemovedRef("beta"))
	if candidate == "" || marker == "" {
		t.Fatalf("candidate %q and marker %q before the source was removed", candidate, marker)
	}

	h.mustRun("source", "remove", s.url)
	s.skill("skills/alpha", "alpha", "The first skill, revised again", nil)
	s.skill("skills/beta", "beta", "The second skill", nil)
	s.commit("both changed")
	out := h.mustRun("--verbose", "skill", "check")
	equal(t, "stdout", out.stdout, "Nothing to check: no managed skill comes from a source added on this machine.\n")
	equal(t, "fetches", fetches(out.stderr), 0)
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "the marker", h.ref(lineage.UpstreamRemovedRef("beta")), marker)
	equal(t, "alpha's drift", drift(h.listed("alpha")), "source removed")
	equal(t, "beta's drift", drift(h.listed("beta")), "source removed,upstream removed")
}

// gateImport holds the fast-import that writes a run's import commits. In
// an update check that is the point between the comparison and the locked
// write that records it: the check has decided everything from the lineage
// it read before the network, and has told nobody yet.
func gateImport(t *testing.T, h *harness) (arm func() (reached, release func())) {
	t.Helper()
	return gateGit(t, h, `for arg in "$@"; do
	case "$arg" in
	fast-import) gate=1 ;;
	esac
done`)
}

// gateSourcePublish is gatePublish for one source alone: it holds the
// update-ref that puts a fetch of that source on its ref, and lets the
// fetches of every other source of the run by.
func gateSourcePublish(t *testing.T, h *harness, url string) (arm func() (reached, release func())) {
	t.Helper()
	return gateGit(t, h, `case " $* " in
*" update-ref `+source.Ref(source.ID(url))+` "*) gate=1 ;;
esac`)
}

// TestSkillCheckBesideAFetchOfTheSameSource runs a source fetch and an
// update check of one source at once, as the serve child's check and a
// command the user runs do. The fetch is held after its network and before
// it reads what it staged, while the check fetches the same source, records
// what it found and ends. Each fetch stages on a ref of its own, so the
// check's neither overwrites nor deletes the held one's: both succeed, the
// source ref ends on the commit both fetched and no staging ref is left.
func TestSkillCheckBesideAFetchOfTheSameSource(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	second := s.commit("second version")
	// The first read of a staging ref is held, and that one alone: the
	// check's own read of its staging ref goes by while the fetch waits.
	once := filepath.Join(t.TempDir(), "once")
	arm := gateGit(t, h, `sub=
for arg in "$@"; do
	case "$arg" in
	for-each-ref) sub=for-each-ref ;;
	`+source.StagingRefPrefix+`*) [ "$sub" = for-each-ref ] && (set -C; : > `+once+`) 2>/dev/null && gate=1 ;;
	esac
done`)

	reached, release := arm()
	done := make(chan outcome, 1)
	go func() { done <- h.run("--json", "source", "fetch", s.url) }()
	reached()
	check := h.run("--json", "skill", "check")
	release()
	fetch := <-done

	equal(t, "the check's exit", check.exit, 0)
	equal(t, "the fetch's exit", fetch.exit, 0)
	equal(t, "the fetch's warnings", strings.Join(warnings(h, fetch.stderr), "\n"), "")
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), h.updateOf(check.stdout, "alpha")["candidate"])
	equal(t, "the source ref", h.ref(source.Ref(source.ID(s.url))), second)
	equal(t, "staging refs", h.accountGit("for-each-ref", "--format=%(refname)", source.StagingRefPrefix), "")
}

// TestSkillCheckRecordsNothingForWhatChangedMidRun holds a check that found
// updates for alpha and beta after it read the lineage and before it
// writes, changes what it read, and lets it go on. The write reads the
// lineage and the settings again under the lock, and records nothing for a
// skill that is gone, whose branch moved or whose source was removed
// meanwhile: no candidate, no marker, no update_available and no count in
// the summary. The skill nothing happened to is recorded all the same, so
// that a check that recorded nothing at all could not pass.
func TestSkillCheckRecordsNothingForWhatChangedMidRun(t *testing.T) {
	t.Parallel()
	// revised commits a newer version of every skill named.
	revised := func(s *sourceRepo, names ...string) {
		for _, name := range names {
			s.skill("skills/"+name, name, "Revised mid-run", nil)
		}
		s.commit("revised: " + strings.Join(names, ", "))
	}
	// held runs a check parked at the gate arm opens, runs meanwhile while
	// it is, and returns the check's outcome once it is let go.
	held := func(h *harness, arm func() (reached, release func()), meanwhile func()) outcome {
		reached, release := arm()
		done := make(chan outcome, 1)
		go func() { done <- h.run("--json", "skill", "check") }()
		reached()
		meanwhile()
		release()
		return <-done
	}
	// nothingFor fails the test when the check recorded anything for name.
	nothingFor := func(t *testing.T, h *harness, out outcome, name string) {
		t.Helper()
		for _, e := range h.eventsOfType(out.stdout, "update_available") {
			if e["name"] == name {
				t.Errorf("an update_available for %s: %v", name, e)
			}
		}
		equal(t, name+"'s candidate", h.ref(lineage.CandidateRef(name)), "")
		equal(t, name+"'s marker", h.ref(lineage.UpstreamRemovedRef(name)), "")
	}

	t.Run("the skill is removed", func(t *testing.T) {
		t.Parallel()
		h, s, _ := checkHarness(t)
		revised(s, "alpha", "beta")
		arm := gateImport(t, h)
		out := held(h, arm, func() { equal(t, "remove", h.run("skill", "remove", "alpha").exit, 0) })
		equal(t, "exit", out.exit, 0)
		nothingFor(t, h, out, "alpha")
		equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), h.updateOf(out.stdout, "beta")["candidate"])
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "checked 1 skill from 1 source: 1 update available")
	})

	t.Run("the branch moves", func(t *testing.T) {
		t.Parallel()
		h, s, _ := checkHarness(t)
		revised(s, "alpha")
		h.mustRun("skill", "check")
		tip, pinned := h.ref(lineage.ManagedRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
		if pinned == "" {
			t.Fatal("no candidate for alpha before the run")
		}
		revised(s, "alpha", "beta")
		arm := gateImport(t, h)
		before := mutationVersion(t, h)
		// An update of alpha lands while the check is held: the branch moves
		// to the version the last check pinned, and the candidate goes.
		out := held(h, arm, func() {
			h.accountGit("update-ref", lineage.ManagedRef("alpha"), pinned, tip)
			h.accountGit("update-ref", "-d", lineage.CandidateRef("alpha"), pinned)
		})
		equal(t, "exit", out.exit, 0)
		nothingFor(t, h, out, "alpha")
		equal(t, "alpha's branch", h.ref(lineage.ManagedRef("alpha")), pinned)
		equal(t, "beta's candidate", h.ref(lineage.CandidateRef("beta")), h.updateOf(out.stdout, "beta")["candidate"])
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "checked 1 skill from 1 source: 1 update available")
		// The write still carries last_fetched, in one mutation.
		equal(t, "mutations", mutationVersion(t, h), before+1)
	})

	t.Run("the source is removed", func(t *testing.T) {
		t.Parallel()
		h, s, _ := checkHarness(t)
		other := h.newSourceRepo("other", true)
		other.skill("skills/gamma", "gamma", "A skill of the other source", nil)
		other.commit("gamma")
		h.mustRun("skill", "add", other.url)
		revised(s, "alpha", "beta")
		revised(other, "gamma")
		// The fetch of s is held after its network and before it publishes
		// its ref, so the ref is published after the removal took it away.
		arm := gateSourcePublish(t, h, s.url)
		out := held(h, arm, func() { equal(t, "source remove", h.run("source", "remove", s.url).exit, 0) })
		equal(t, "exit", out.exit, 0)
		nothingFor(t, h, out, "alpha")
		nothingFor(t, h, out, "beta")
		gamma := h.updateOf(out.stdout, "gamma")["candidate"]
		equal(t, "gamma's candidate", h.ref(lineage.CandidateRef("gamma")), gamma)
		equal(t, "summary", h.one(out.stdout, "result")["summary"], "checked 1 skill from 1 source: 1 update available")
		// Nothing of the removed source is left: the write took its ref away
		// again, and last_fetched did not bring its entry back.
		equal(t, "refs under refs/agentx", h.agentxRefs(), lineage.CandidateRef("gamma")+"\n"+source.Ref(source.ID(other.url)))
		sources := readSettingsFile(t, h)["sources"].([]any)
		equal(t, "sources", len(sources), 1)
		equal(t, "the source left", sources[0].(map[string]any)["url"], other.url)
	})
}

// TestSkillCheckIgnoresWhatAnImportLeavesOut: an import leaves a symlink
// out, so the directory the source holds never has the tree the base
// version has. A check compares what an import would take instead, and
// finds no update for a skill whose symlink is all that differs, on the
// first check and after the symlink itself changes upstream, and drops a
// candidate it finds that no update backs. A real change is an update, and
// the check that pins it says what the update leaves out, once: a check
// that moves nothing says nothing.
func TestSkillCheckIgnoresWhatAnImportLeavesOut(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("linked", true)
	s.skill("linked", "linked", "A skill with a symlink", map[string]string{"notes.md": "notes\n"})
	link(t, "notes.md", filepath.Join(s.work, "linked", "link.md"))
	s.commit("with a link")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url)

	for _, step := range []string{"first check", "after the link moved"} {
		if step == "after the link moved" {
			remove(t, filepath.Join(s.work, "linked", "link.md"))
			link(t, "SKILL.md", filepath.Join(s.work, "linked", "link.md"))
			s.commit("the link moved")
			// A candidate no update backs, which the check drops.
			h.accountGit("update-ref", lineage.CandidateRef("linked"), h.ref(lineage.ManagedRef("linked")))
		}
		out := h.mustRun("--json", "skill", "check")
		if got := h.eventsOfType(out.stdout, "update_available"); len(got) != 0 {
			t.Errorf("%s: an update for a change to a symlink alone: %v", step, got)
		}
		equal(t, step+": the candidate", h.ref(lineage.CandidateRef("linked")), "")
		equal(t, step+": the warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
	}

	writeFile(t, filepath.Join(s.work, "linked", "notes.md"), "notes, revised\n")
	s.commit("revised")
	out := h.mustRun("--json", "skill", "check")
	equal(t, "the files of the update", files(h.updateOf(out.stdout, "linked")), "modified notes.md")
	if h.ref(lineage.CandidateRef("linked")) == "" {
		t.Fatal("no candidate for a real update")
	}
	equal(t, "the warning of the check that pinned it", strings.Join(warnings(h, out.stderr), "\n"),
		"linked/link.md is not a regular file and is left out of the update of linked")
	again := h.mustRun("--json", "skill", "check")
	h.updateOf(again.stdout, "linked")
	equal(t, "the warnings of a check that moved nothing", again.stderr, "")
}

// TestSkillCheckNamesAnUpstreamRename: an upstream that renames a skill in
// its frontmatter has an update like any other; the skill keeps its name,
// which the event says beside the upstream's, and the check warns about it
// when it pins that version.
func TestSkillCheckNamesAnUpstreamRename(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha-renamed", "The first skill, renamed", nil)
	s.commit("renamed")
	out := h.mustRun("--json", "skill", "check")
	equal(t, "upstream_name", h.updateOf(out.stdout, "alpha")["upstream_name"], "alpha-renamed")
	equal(t, "the warning", strings.Join(warnings(h, out.stderr), "\n"),
		`alpha: the update names the skill "alpha-renamed"; updating it keeps the name alpha`)
	again := h.mustRun("--json", "skill", "check")
	equal(t, "upstream_name again", h.updateOf(again.stdout, "alpha")["upstream_name"], "alpha-renamed")
	equal(t, "the warnings of a check that moved nothing", again.stderr, "")
}

// TestSkillDiffUpstream shows the update a check pinned against the base
// version, every path relative to the skill's directory, and refuses a
// skill with no update known, saying how to look for one.
func TestSkillDiffUpstream(t *testing.T) {
	t.Parallel()
	h, s, first := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes, revised\n", "extra.md": "new\n"})
	second := s.commit("second version")
	h.mustRun("skill", "check")

	out := h.mustRun("--json", "skill", "diff", "alpha", "--upstream")
	var got []string
	for _, e := range h.eventsOfType(out.stdout, "diff") {
		equal(t, "name", e["name"], "alpha")
		got = append(got, e["status"].(string)+" "+e["path"].(string))
	}
	equal(t, "diffs", strings.Join(got, ", "), "added extra.md, modified notes.md")
	summary := "the update of alpha at " + short(second) + " differs from its base version at " + short(first) + " in 2 files"
	equal(t, "summary", h.one(out.stdout, "result")["summary"], summary)
	text := h.mustRun("skill", "diff", "alpha", "--upstream")
	if !strings.HasPrefix(text.stdout, summary+"\ndiff --git a/extra.md b/extra.md\n") {
		t.Errorf("stdout = %q", text.stdout)
	}
	contains(t, "stdout", text.stdout, "+alpha notes, revised\n")

	// The library is not what is compared: an edit shows in the plain diff
	// and not in this one.
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "edited here\n")
	equal(t, "the upstream diff after an edit", h.mustRun("skill", "diff", "alpha", "--upstream").stdout, text.stdout)

	refused := h.run("--json", "skill", "diff", "beta", "--upstream")
	equal(t, "exit", refused.exit, 6)
	e := h.one(refused.stdout, "error")
	equal(t, "message", e["message"], "no update of beta is known")
	contains(t, "hint", e["hint"].(string), "agentx skill check")
}

// TestSkillCheckWithNothingToCheck spawns no git beyond the startup check
// on a machine with no source, and fetches nothing on one whose sources no
// managed skill came from.
func TestSkillCheckWithNothingToCheck(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	calls := countingGit(t, h)
	out := h.mustRun("--json", "skill", "check")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "nothing to check: no managed skill comes from a source added on this machine")
	equal(t, "git calls", strings.Join(calls(), "|"), "--version")

	s := h.newSourceRepo("skills", true)
	s.skill("alpha", "alpha", "Never installed", nil)
	s.commit("alpha")
	h.mustRun("source", "add", s.url)
	out = h.mustRun("--verbose", "skill", "check")
	equal(t, "stdout", out.stdout, "Nothing to check: no managed skill comes from a source added on this machine.\n")
	equal(t, "fetches", fetches(out.stderr), 0)
}

// TestSkillCheckSpawnsBoundedGit counts the git processes of a check over a
// hundred managed skills: with no upstream change, and with a few and with
// many changed skills, whose count does not depend on how many changed.
// Nothing is read per skill: the fetch lists every skill directory's tree,
// the lineage every branch's, the changed ones are read and written
// together, and what each update changes is read in one diff-tree.
func TestSkillCheckSpawnsBoundedGit(t *testing.T) {
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("many", true)
	const skills = 100
	for i := range skills {
		s.skill(fmt.Sprintf("skills/s%03d", i), fmt.Sprintf("s%03d", i), "Skill number "+fmt.Sprint(i), map[string]string{"notes.md": "notes\n"})
	}
	s.commit("a hundred skills")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")

	count := func(what string) int {
		t.Helper()
		calls := countingGit(t, h)
		out := h.run("--json", "skill", "check")
		if out.exit != 0 {
			t.Fatalf("%s: exit %d\n%s", what, out.exit, out.stderr)
		}
		return len(calls())
	}
	change := func(from, n int) int {
		t.Helper()
		for i := from; i < from+n; i++ {
			s.write(fmt.Sprintf("skills/s%03d/notes.md", i), fmt.Sprintf("notes, changed at %d\n", from))
		}
		s.commit(fmt.Sprintf("%d changed", n))
		return n
	}

	unchanged := count("no change")
	change(0, 3)
	few := count("3 changed")
	change(10, 60)
	many := count("60 changed")
	t.Logf("git processes: %d with no change, %d with 3 changed, %d with 60 changed", unchanged, few, many)
	// The bounds leave room for a git that fetches in another way; a count
	// per skill would be a hundred at least.
	if unchanged > 30 {
		t.Errorf("a check with no change spawned %d git processes", unchanged)
	}
	if many != few {
		t.Errorf("a check of 60 changed skills spawned %d git processes and one of 3 spawned %d: the count grows with the skills", many, few)
	}
	if few > 45 {
		t.Errorf("a check of changed skills spawned %d git processes", few)
	}
	listed := h.mustRun("--json", "skill", "list").stdout
	if n := len(h.eventsOfType(listed, "library_skill")); n != skills {
		t.Fatalf("%d skills listed, want %d", n, skills)
	}
	updates := 0
	for _, e := range h.eventsOfType(listed, "library_skill") {
		if e["candidate"] != nil {
			updates++
		}
	}
	equal(t, "skills with an update", updates, 63)
}

// checkChildEnv marks the child process of TestSkillCheckRecoversFromAKilledRun.
const checkChildEnv = "AGENTX_TEST_CHECK_CHILD"

// TestCheckChildProcess is not a test: it is the body of the process
// TestSkillCheckRecoversFromAKilledRun starts, which runs one check and is
// killed in the middle of its write. It does nothing when the variable
// that marks that process is not set.
func TestCheckChildProcess(t *testing.T) {
	if os.Getenv(checkChildEnv) == "" {
		t.Skip("not the check child process")
	}
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	os.Exit(Run(context.Background(), []string{"skill", "check"}, env, strings.NewReader(""), os.Stdout, os.Stderr))
}

// TestSkillCheckRecoversFromAKilledRun kills a check at the durable
// boundary of its one write: a git wrapper hands the transaction that moves
// the candidate ref to the real git and then kills the process that ran it,
// so the ref is live and the settings write that goes with it is not. The
// journal describes the rest, and the next command that takes the lock
// finishes it.
func TestSkillCheckRecoversFromAKilledRun(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	s.commit("second version")
	// A last_fetched the check is bound to rewrite, whatever second it runs in.
	settings := readSettingsFile(t, h)
	settings["sources"].([]any)[0].(map[string]any)["last_fetched"] = "2000-01-01T00:00:00Z"
	b, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.agentx, "settings.json"), string(b))
	before := readSettingsFile(t, h)["sources"]

	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	// The transaction is told from the other update-ref --stdin of a check,
	// the deletions that end each fetch and the run, by the candidate ref it
	// names. PATH holds git alone, so the input is read with builtins.
	stubGit(t, h, `#!/bin/sh
case " $* " in
*" update-ref --stdin "*)
	input=
	while IFS= read -r line; do input="$input$line
"; done
	printf '%s' "$input" | `+real+` "$@"
	status=$?
	case "$input" in
	*" `+lineage.CandidatePrefix+`"*) kill -9 $PPID ;;
	esac
	exit $status
	;;
esac
exec `+real+` "$@"
`)
	child := exec.Command(os.Args[0], "-test.run=^TestCheckChildProcess$", "-test.v")
	child.Env = append(os.Environ(), checkChildEnv+"=1")
	for k, v := range h.env {
		child.Env = append(child.Env, k+"="+v)
	}
	if out, err := child.CombinedOutput(); err == nil {
		t.Fatalf("the check was not killed:\n%s", out)
	}
	candidate := h.ref(lineage.CandidateRef("alpha"))
	if candidate == "" {
		t.Fatal("the killed check wrote no candidate, so it was not killed where the test expects")
	}
	equal(t, "journals after the killed run", journalCount(t, h), 1)
	equal(t, "the settings before recovery", fmt.Sprint(readSettingsFile(t, h)["sources"]), fmt.Sprint(before))

	h.env["PATH"] = os.Getenv("PATH")
	h.mustRun("config", "set", "label", "recovered")
	equal(t, "journals after recovery", journalCount(t, h), 0)
	if fmt.Sprint(readSettingsFile(t, h)["sources"]) == fmt.Sprint(before) {
		t.Error("recovery did not write the last_fetched the check recorded")
	}
	equal(t, "the candidate after recovery", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "skill list", h.listed("alpha")["candidate"].(map[string]any)["upstream_commit"], h.accountGit("rev-parse", "refs/agentx/sources/"+source.ID(s.url)))
}

// TestServeChecksForUpdates runs serve with the check interval shortened: the
// first check starts once the initial snapshot is out and announces the
// update the source holds, with the instance id of this serve, and the
// snapshot its write sets off carries the candidate; a later check on the
// timer announces a newer version. A single pass checks nothing.
func TestServeChecksForUpdates(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	second := s.commit("second version")

	once := h.serveOnce("--json", "--verbose")
	equal(t, "serve --once", once.exit, 0)
	equal(t, "fetches of serve --once", fetches(once.stderr), 0)
	equal(t, "the candidate after serve --once", h.ref(lineage.CandidateRef("alpha")), "")

	h.env["AGENTX_CHECK_INTERVAL"] = "300ms"
	p := h.serve(t, "--json")
	first := p.next("snapshot")
	update, events := p.nextUpdate(func(e jsonEvent) bool { return e["candidate_upstream_commit"] == second })
	equal(t, "name", update["name"], "alpha")
	equal(t, "instance_id", update["instance_id"], first["instance_id"])
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), update["candidate"])

	s.skill("skills/alpha", "alpha", "The first skill, revised again", nil)
	third := s.commit("third version")
	later, passed := p.nextUpdate(func(e jsonEvent) bool { return e["candidate_upstream_commit"] == third })
	equal(t, "the later candidate", h.ref(lineage.CandidateRef("alpha")), later["candidate"])
	// The write that pinned it bumped the version file, and the rescan that
	// sets off, or the one this refresh asks for, carries the candidate in
	// the snapshot the desktop app applies.
	p.send(`{"type":"refresh","request_id":"after"}`)
	events = append(append(events, passed...), p.until("after")...)
	var last jsonEvent
	for _, e := range events {
		if e["type"] == "snapshot" {
			last = e
		}
	}
	if last == nil {
		t.Fatal("no snapshot after the checks wrote their candidates")
	}
	var entry map[string]any
	for _, e := range last["library"].([]any) {
		if m := e.(map[string]any); m["name"] == "alpha" {
			entry = m
		}
	}
	cand, ok := entry["candidate"].(map[string]any)
	if !ok {
		t.Fatalf("the last snapshot's alpha carries no candidate: %v", entry)
	}
	equal(t, "the snapshot's candidate", cand["upstream_commit"], third)
	equal(t, "exit", p.close(), 0)
}

// TestServeChecksAtLaunch runs serve on an interval no test waits out: the
// update it announces comes from the check serve runs at launch, not from
// one on the timer.
func TestServeChecksAtLaunch(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	second := s.commit("second version")
	h.env["AGENTX_CHECK_INTERVAL"] = "1h"
	p := h.serve(t, "--json")
	p.next("snapshot")
	update, _ := p.nextUpdate(func(e jsonEvent) bool { return e["candidate_upstream_commit"] == second })
	equal(t, "name", update["name"], "alpha")
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), update["candidate"])
	equal(t, "exit", p.close(), 0)
}

// TestServeCheckWaitsForTheLock holds the lock over several ticks of the
// check once the first check is done: a check that meets it waits for it
// rather than failing, so no tick warns, and the check that gets the lock
// once it is free pins the version the source moved to meanwhile.
func TestServeCheckWaitsForTheLock(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	second := s.commit("second version")
	h.env["AGENTX_CHECK_INTERVAL"] = "200ms"
	p := h.serve(t, "--json")
	p.next("snapshot")
	p.nextUpdate(func(e jsonEvent) bool { return e["candidate_upstream_commit"] == second })

	release := holdLock(t, h)
	s.skill("skills/alpha", "alpha", "The first skill, revised again", nil)
	third := s.commit("third version")
	// Only widens the window in which ticks meet the lock: what is asserted
	// does not depend on how many of them do.
	time.Sleep(time.Second)
	release()
	update, _ := p.nextUpdate(func(e jsonEvent) bool { return e["candidate_upstream_commit"] == third })
	equal(t, "the candidate", h.ref(lineage.CandidateRef("alpha")), update["candidate"])
	equal(t, "exit", p.close(), 0)
	for _, w := range warnings(h, p.stderr.String()) {
		if strings.HasPrefix(w, "update check:") {
			t.Errorf("a check warned: %s", w)
		}
	}
}

// TestServeWarnsAboutASourceItCannotCheck: a source serve's check cannot
// fetch is a warning on every check that fails, naming the source, why it
// failed and the skills it left unchecked, and it ends nothing: the source
// beside it is still checked and pinned, later checks still run and serve
// still answers requests.
func TestServeWarnsAboutASourceItCannotCheck(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	other := h.newSourceRepo("other", true)
	other.skill("gamma", "gamma", "A skill of the other source", nil)
	other.commit("gamma")
	h.mustRun("skill", "add", other.url)
	s.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	second := s.commit("second version")
	if err := os.Rename(other.gitDir, other.gitDir+".gone"); err != nil {
		t.Fatal(err)
	}

	h.env["AGENTX_CHECK_INTERVAL"] = "200ms"
	p := h.serve(t, "--json")
	p.next("snapshot")
	pinned := func(e jsonEvent) bool { return e["name"] == "alpha" && e["candidate_upstream_commit"] == second }
	update, _ := p.nextUpdate(pinned)
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), update["candidate"])
	// Every check announces the update it finds pinned, so a second one
	// proves a later check ran after the first one failed for other.
	p.nextUpdate(pinned)
	p.send(`{"type":"refresh","request_id":"alive"}`)
	p.until("alive")
	equal(t, "exit", p.close(), 0)

	var warned []string
	for _, w := range warnings(h, p.stderr.String()) {
		if strings.HasPrefix(w, "update check: ") {
			warned = append(warned, w)
		}
	}
	if len(warned) < 2 {
		t.Fatalf("%d update check warnings, want one per check, at least two:\n%s", len(warned), p.stderr.String())
	}
	for _, w := range warned {
		if !strings.HasPrefix(w, "update check: "+other.url+": ") || !strings.HasSuffix(w, "; not checked: gamma") {
			t.Errorf("the warning = %q, want the failure of %s and the skill it left unchecked", w, other.url)
		}
		if strings.Contains(w, "alpha") {
			t.Errorf("a warning names alpha, which was checked: %q", w)
		}
	}
	equal(t, "gamma's candidate", h.ref(lineage.CandidateRef("gamma")), "")
}

// TestServeChecksNothingWithoutAManagedSkill: on a machine with a source
// and no managed skill from it, or whose managed skills came from a source
// removed since, the last snapshot says there is nothing to check, and the
// ticks of the check spawn no git at all.
func TestServeChecksNothingWithoutAManagedSkill(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(h *harness, s *sourceRepo)
	}{
		{"no skill installed", func(h *harness, s *sourceRepo) {}},
		{"the source of the skill removed", func(h *harness, s *sourceRepo) {
			// Another source stays, so that the settings still name one and
			// only the snapshot can tell there is nothing to check.
			other := h.newSourceRepo("other", true)
			other.skill("gamma", "gamma", "Never installed", nil)
			other.commit("gamma")
			h.mustRun("source", "add", other.url)
			h.mustRun("skill", "add", s.url)
			h.mustRun("source", "remove", s.url)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixture{dirs: []string{".claude"}})
			s := h.newSourceRepo("skills", true)
			s.skill("alpha", "alpha", "A skill", nil)
			s.commit("alpha")
			h.mustRun("source", "add", s.url)
			tc.setup(h, s)
			calls := countingGit(t, h)
			h.env["AGENTX_CHECK_INTERVAL"] = "50ms"
			p := h.serve(t, "--json")
			p.next("snapshot")
			// The scan and the source index it rebuilds have run their git by
			// the time a refresh is acknowledged.
			p.send(`{"type":"refresh","request_id":"settled"}`)
			p.until("settled")
			n := len(calls())
			time.Sleep(500 * time.Millisecond) // ten ticks
			if later := calls()[n:]; len(later) > 0 {
				t.Errorf("the ticks of a check with nothing to check spawned git:\n%s", strings.Join(later, "\n"))
			}
			equal(t, "exit", p.close(), 0)
		})
	}
}

// TestServeRefusesABadCheckInterval: the interval is a positive duration
// or nothing.
func TestServeRefusesABadCheckInterval(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	for _, v := range []string{"soon", "0s", "-5m"} {
		h.env["AGENTX_CHECK_INTERVAL"] = v
		out := h.run("--json", "serve")
		equal(t, v+": exit", out.exit, 1)
		e := h.one(out.stdout, "error")
		equal(t, v+": code", e["code"], "usage")
		equal(t, v+": message", e["message"], "AGENTX_CHECK_INTERVAL "+v+" is not a positive duration")
	}
	equal(t, "serve --once ignores it", h.serveOnce("--json").exit, 0)
}

// nextUpdate reads the child's events up to the first update_available
// that match accepts, and returns it with every event it passed over on the
// way: a check and the rescans its write sets off interleave as they
// happen to, and a check on a short timer announces the same update again.
func (p *serveProc) nextUpdate(match func(jsonEvent) bool) (jsonEvent, []jsonEvent) {
	p.t.Helper()
	var passed []jsonEvent
	deadline := time.After(serveDeadline)
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				p.t.Fatal("serve ended before the update")
			}
			var e jsonEvent
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				p.t.Fatalf("not a JSON event: %q: %v", line, err)
			}
			if e["type"] == "update_available" && match(e) {
				return e, passed
			}
			passed = append(passed, e)
		case <-deadline:
			p.t.Fatalf("no matching update_available within %s", serveDeadline)
		}
	}
}
