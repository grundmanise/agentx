package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// TestAdoptSaysWhichRefItPinnedTheSourceTo: two entries of one source
// recording two different refs give the machine one pin, which then governs
// every later install from that source. The run may not choose it in
// silence, and a skill it cannot find must say the search was confined to
// that ref rather than leave the user with a --base the ancestor check
// would refuse.
func TestAdoptSaysWhichRefItPinnedTheSourceTo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", nil)
	s.commit("alpha at v1")
	s.tag("v1")
	vercelInstall(t, h, s, "skills/alpha", "alpha")
	s.skill("skills/beta", "beta", "The second skill", nil)
	s.commit("beta, after v1")
	vercelInstall(t, h, s, "skills/beta", "beta")
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, Ref: "v1", SkillPath: "skills/alpha"},
		"beta": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, Ref: "main", SkillPath: "skills/beta",
			SkillFolderHash: s.tree("skills/beta")},
	})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	contains(t, "stderr", out.stderr, "more than one ref")
	contains(t, "stderr", out.stderr, "v1")
	var refused string
	for _, ev := range h.eventsOfType(out.stdout, "adoption") {
		if ev["name"] == "beta" {
			refused, _ = ev["reason"].(string)
		}
	}
	contains(t, "beta's refusal", refused, "cannot be established")
	if !strings.Contains(refused, "commits of it behind that") {
		t.Errorf("beta's refusal does not say the search was confined to the commit the source was fetched at: %q", refused)
	}
}

// TestAdoptReportsWhatTheDirectoryHoldsWhenItChangesUnderTheLock: the
// directory is edited between the two reads, so the adoption is refused;
// the event's content hash is what the directory holds now, which is what
// the contract says that field is, and it reports no base at all, since no
// base was established for it.
func TestAdoptReportsWhatTheDirectoryHoldsWhenItChangesUnderTheLock(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	s.commit("the only version")
	vercelInstall(t, h, s, "skills/alpha", "alpha")
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha",
	}})
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	// The import commits are written before the lock is taken, so a run held
	// there has read the directory once and not yet again.
	arm := gateGit(t, h, `for arg in "$@"; do case "$arg" in fast-import) gate=1 ;; esac; done`)

	reached, release := arm()
	done := make(chan outcome, 1)
	go func() { done <- h.run("--json", "adopt", "--all") }()
	reached()
	editLibrary(t, h, "alpha", "notes.md", "an edit made while the run was reading\n")
	edited := contentHashAt(filepath.Join(h.library, "alpha"))
	release()

	out := <-done
	equal(t, "exit", out.exit, exitRefused.exit)
	ev := h.one(out.stdout, "adoption")
	equal(t, "state", ev["state"], adoptRefused)
	equal(t, "content_hash", ev["content_hash"], edited)
	if _, reported := ev["upstream_commit"]; reported {
		t.Errorf("a refused entry reports a base version: %v", ev)
	}
	if _, reported := ev["modified"]; reported {
		t.Errorf("a refused entry reports whether it is modified: %v", ev)
	}
}

// TestAdoptSanitisesTheDirectoryItNames adopts two skills whose lock file
// entries name source directories with a C1 control in them, which the
// check on a lock file's subpath lets through. The line that confirms each
// adoption names that directory, and the lock file chose it, so it is
// sanitised there as the preview sanitises it; a directory named with
// nothing else is quoted instead, so that the line still says it is one.
func TestAdoptSanitisesTheDirectoryItNames(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/al\u009bpha", "alpha", "The first skill", nil)
	s.skill("\u009b", "beta", "The second skill", nil)
	v1 := s.commit("two skills")
	vercelInstall(t, h, s, "skills/al\u009bpha", "alpha")
	vercelInstall(t, h, s, "\u009b", "beta")
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/al\u009bpha/SKILL.md",
			SkillFolderHash: s.tree("skills/al\u009bpha")},
		"beta": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "\u009b/SKILL.md",
			SkillFolderHash: s.tree("\u009b")},
	})

	out := h.run("adopt", "--all")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "✓ adopted alpha from "+s.url+" under skills/al pha at "+short(v1)+"\n")
	contains(t, "stdout", out.stdout, "✓ adopted beta from "+s.url+` under "\302\233" at `+short(v1)+"\n")
	for _, r := range out.stdout {
		if unicode.IsControl(r) && r != '\n' {
			t.Fatalf("a control character reached the output: %q in\n%q", r, out.stdout)
		}
	}
}
