package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillUpdateContinueAppliesTheFilesAsLeft: --continue applies a
// pending merge whose files were edited in the checkout and saved, with no
// git: what the user left is staged and laid out in the library, the
// import branch moves to the update and the checkout goes. An update run
// without it still reports the merge unresolved.
func TestSkillUpdateContinueAppliesTheFilesAsLeft(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	candidate := h.ref(lineage.CandidateRef("alpha"))
	writeFile(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir", "notes.md"), "alpha notes, merged by hand\n")

	if out := h.run("skill", "update", "alpha"); out.exit != 4 {
		t.Fatalf("the update without --continue: exit %d, want 4\n%s", out.exit, out.stderr)
	}
	out := h.mustRun("--json", "skill", "update", "alpha", "--continue")
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), "with the merge you resolved")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	equal(t, "the candidate ref", h.ref(lineage.CandidateRef("alpha")), "")
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes, merged by hand\n")
	noCheckout(t, h, "alpha")
}

// TestSkillUpdateContinueRefusesAConflictMarker: --continue refuses a
// merge whose file still holds a conflict marker git wrote, exit 4, naming
// the file, and changes nothing: the file stays unmerged and
// the library as it was. A line of '=' as long as a marker is no marker.
func TestSkillUpdateContinueRefusesAConflictMarker(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	pendingMerge(t, h, s)
	notes := filepath.Join(pendingCheckout(h, "alpha"), "alpha-dir", "notes.md")
	body := fileBody(t, notes)
	writeFile(t, notes, "Title\n=======\n"+body[strings.Index(body, "<<<<<<<"):])
	was := h.unchangedHome()

	out := h.run("--json", "skill", "update", "alpha", "--continue")
	equal(t, "exit", out.exit, 4)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "failed to update the skill: "+quotedPath(notes)+" still holds a conflict marker")
	equal(t, "hint", e["hint"], "edit the file until no conflict marker is left, then run 'agentx skill update alpha --continue' again")
	equal(t, "the unmerged paths", unmerged(t, h, "alpha"), "1 alpha-dir/notes.md\n2 alpha-dir/notes.md\n3 alpha-dir/notes.md")
	equal(t, "the library", onDisk(t, h.library), was.library)
}

// TestSkillUpdateTheirsDropsTheEdits: --theirs drops the edits of a skill
// whose update conflicted and applies the update as published: the
// library directory holds the update's version, the files git ignores
// kept, the skill is current, and the checkout is gone.
func TestSkillUpdateTheirsDropsTheEdits(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	editLibrary(t, h, "alpha", ".DS_Store", "finder data\n")
	pendingMerge(t, h, s)
	candidate := h.ref(lineage.CandidateRef("alpha"))

	h.mustRun("skill", "update", "alpha", "--theirs")
	equal(t, "the import branch", h.ref(lineage.ManagedRef("alpha")), candidate)
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), withFile(secondTree(t, s), ".DS_Store", "finder data\n"))
	noCheckout(t, h, "alpha")
	equal(t, "state", h.listed("alpha")["state"], stateCurrent)
}

// TestSkillUpdateContinueAndTheirsNeedAPendingMerge: with no merge
// pending, --continue and --theirs are refused, exit 6, and nothing is
// updated.
func TestSkillUpdateContinueAndTheirsNeedAPendingMerge(t *testing.T) {
	t.Parallel()
	h, s, _ := updateHarness(t)
	checked(t, h, s)
	for _, flag := range []string{"--continue", "--theirs"} {
		out := h.run("--json", "skill", "update", "alpha", flag)
		equal(t, "exit of "+flag, out.exit, 6)
		equal(t, "message of "+flag, h.one(out.stdout, "error")["message"], "alpha has no merge pending")
	}
	if h.ref(lineage.CandidateRef("alpha")) == "" {
		t.Error("the update was applied")
	}
}

// TestSkillUpdateContinueOfAFork: a fork's merge left pending is applied
// by --continue with the file as the user saved it, as a merge commit on
// the fork's branch.
func TestSkillUpdateContinueOfAFork(t *testing.T) {
	t.Parallel()
	h, s, _ := forkUpdateHarness(t)
	h.mustRun("skill", "fork", "alpha")
	alpha := h.forkDir("alpha", "alpha")
	writeFile(t, filepath.Join(alpha, "notes.md"), forkNotes("seven", "seven, mine"))
	h.record("alpha")
	s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": forkNotes("seven", "seven, upstream")})
	s.commit("second version")
	h.mustRun("skill", "check-updates")
	tip, candidate := h.ref(lineage.ForkRef("alpha")), h.ref(lineage.CandidateRef("alpha"))
	if out := h.run("skill", "update", "alpha"); out.exit != 4 {
		t.Fatalf("the update that should conflict: exit %d\n%s", out.exit, out.stderr)
	}
	resolved := forkNotes("seven", "seven, resolved")
	writeFile(t, filepath.Join(pendingCheckout(h, "alpha"), "alpha", "notes.md"), resolved)

	h.mustRun("skill", "update", "alpha", "--continue")
	equal(t, "the merge's parents", h.parents(h.ref(lineage.ForkRef("alpha"))), tip+"\n"+candidate)
	equal(t, "the notes", fileBody(t, filepath.Join(alpha, "notes.md")), resolved)
	noCheckout(t, h, "alpha")
}

// TestHoldsMarker: a marker that opens or closes a conflict, at the size
// given, and none for a run of another length, a middle marker or a marker
// inside a line.
func TestHoldsMarker(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		body string
		size int
		want bool
	}{
		{"a\n<<<<<<< ours\nb\n", 7, true},
		{"a\r\n>>>>>>>\r\n", 7, true},
		{"a\n<<<<<<< ours\n", 8, false},
		{"<<<<<<<< ours\n", 8, true},
		{"Title\n=======\n|||||||\n", 7, false},
		{"x <<<<<<< y\n", 7, false},
		{"<<<<<<<<\n", 7, false},
	} {
		equal(t, c.body, holdsMarker(c.body, c.size), c.want)
	}
}
