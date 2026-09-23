package cli

import (
	"strings"
	"testing"
)

// TestAdoptEventsAreInNameOrder: the contract has one adoption event per
// entry the run covered, in name order, whichever order the flags were
// given in, so a consumer reading a run over several skills reads them in
// one order and not in the user's.
func TestAdoptEventsAreInNameOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	for _, name := range []string{"alpha", "middle", "zeta"} {
		s.skill("skills/"+name, name, "A skill", nil)
	}
	s.commit("three skills")
	for _, name := range []string{"alpha", "middle", "zeta"} {
		vercelInstall(t, h, s, "skills/"+name, name)
	}
	entries := map[string]lockEntry{}
	for _, name := range []string{"alpha", "middle", "zeta"} {
		entries[name] = lockEntry{Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/" + name}
	}
	h.writeLock(h.lockPath(), entries)

	out := h.run("--json", "adopt", "--skill", "zeta", "--skill", "alpha", "--skill", "middle")
	equal(t, "exit", out.exit, 0)
	var names []string
	for _, ev := range h.eventsOfType(out.stdout, "adoption") {
		names = append(names, ev["name"].(string))
	}
	equal(t, "the adoption events", strings.Join(names, ","), "alpha,middle,zeta")
	var subjects []string
	for _, ev := range h.eventsOfType(out.stdout, "progress") {
		if ev["phase"] == phaseAdopt {
			subjects = append(subjects, ev["subject"].(string))
		}
	}
	equal(t, "the progress subjects", strings.Join(subjects, ","), "alpha,middle,zeta")
}
