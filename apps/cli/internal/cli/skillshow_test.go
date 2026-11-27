package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestSkillShowReadsWhatTheSeparateReadsRead holds skill show to the reads
// it stands for, event for event: for a managed skill with edits and an
// update, the files of skill files, the edits of skill diff and the update
// of skill diff --update, whose events alone say update, and no history;
// for a fork with a recorded edit, its files, its edits and the history of
// skill history, and no update; for an unmanaged skill, its files alone.
func TestSkillShowReadsWhatTheSeparateReadsRead(t *testing.T) {
	t.Parallel()
	h, s, _ := checkHarness(t)
	h.mustRun("skill", "fork", "beta")
	writeFile(t, filepath.Join(h.library, "beta", "extra.md"), "extra\n")
	h.record("beta")
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "alpha notes, edited here\n")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "mine"), "SKILL.md"), skill("mine", "mine"))
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes, revised\n"})
	s.commit("second version")
	h.mustRun("skill", "check-updates")

	read := func(typ string, args ...string) []jsonEvent {
		t.Helper()
		return h.eventsOfType(h.mustRun(append([]string{"--json", "skill"}, args...)...).stdout, typ)
	}
	for _, tc := range []struct {
		name                   string
		edits, update, history bool
	}{
		{name: "alpha", edits: true, update: true},
		{name: "beta", edits: true, history: true},
		{name: "mine"},
	} {
		out := h.mustRun("--json", "skill", "show", tc.name)
		same := func(what string, got, want []jsonEvent) {
			t.Helper()
			if len(want) == 0 {
				t.Fatalf("%s: the separate read of %s is empty", tc.name, what)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %s = %v, want %v", tc.name, what, got, want)
			}
		}
		same("the files", h.eventsOfType(out.stdout, "skill_file"), read("skill_file", "files", tc.name))
		var edits, update []jsonEvent
		for _, d := range h.eventsOfType(out.stdout, "diff") {
			if d["update"] == true {
				update = append(update, d)
			} else {
				edits = append(edits, d)
			}
		}
		if tc.edits {
			same("the edits", edits, read("diff", "diff", tc.name))
		} else {
			equal(t, tc.name+": edits", len(edits), 0)
		}
		if tc.update {
			same("the update", update, read("diff", "diff", tc.name, "--update"))
		} else {
			equal(t, tc.name+": update diffs", len(update), 0)
		}
		history := h.eventsOfType(out.stdout, "history")
		if tc.history {
			same("the history", history, read("history", "history", tc.name))
		} else {
			equal(t, tc.name+": history events", len(history), 0)
		}
		if tc.name == "alpha" {
			summary := h.one(out.stdout, "result")["summary"].(string)
			parts := strings.Split(summary, "; ")
			equal(t, "the parts of the summary", len(parts), 3)
			equal(t, "its files", parts[0], "alpha has 2 files")
			contains(t, "its edits", parts[1], "alpha differs from its base version at ")
			contains(t, "its update", parts[2], "the update of alpha at ")
		}
	}
	equal(t, "journals", journalCount(t, h), 0)
}
