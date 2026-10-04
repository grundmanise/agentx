package cli

import (
	"path/filepath"
	"testing"
)

// legacyHome is a machine with Claude Code and a source whose skill nc is
// stored with mode 100664, a file at the top and one in a directory below,
// which git mktree takes and ls-tree reads back as 100644: the tree keeps an
// id of its own that no directory on disk has. nc is installed and placed in
// Claude Code. The one id it returns is the tree git writes today for nc's
// files, which the import must store.
var legacyHome = &fixtureHome{
	source: "legacy",
	dirs:   []string{".claude"},
	build: func(h *harness, s *sourceRepo) []string {
		s.skill("skills/nc", "nc", "Stored with a legacy mode", map[string]string{"a.md": "a\n", "sub/b.md": "b\n"})
		s.commit("nc")
		sub := s.mktree("100664 blob " + s.run("rev-parse", "HEAD:skills/nc/sub/b.md") + "\tb.md")
		nc := s.mktree(
			"100644 blob "+s.run("rev-parse", "HEAD:skills/nc/SKILL.md")+"\tSKILL.md",
			"100664 blob "+s.run("rev-parse", "HEAD:skills/nc/a.md")+"\ta.md",
			"040000 tree "+sub+"\tsub")
		skills := s.mktree(s.replaced("HEAD:skills", "nc", nc)...)
		root := s.mktree(s.replaced("HEAD^{tree}", "skills", skills)...)
		canonical := s.tree("skills/nc")
		s.bare("update-ref", "refs/heads/main", s.bare("commit-tree", root, "-p", "HEAD", "-m", "legacy modes"))
		if s.tree("skills/nc") == canonical {
			s.t.Fatal("the rewritten tree has the canonical id; the fixture proves nothing")
		}
		contains(s.t, "the source listing", s.bare("ls-tree", "HEAD:skills/nc"), "100644 blob "+s.run("rev-parse", "HEAD:skills/nc/a.md")+"\ta.md")
		h.mustRun("source", "add", s.url)
		h.mustRun("skill", "add", s.url, "--name", "nc")
		return []string{canonical}
	},
}

// TestASkillStoredWithALegacyModeInstallsCurrent: the import writes the
// tree git writes today for a source that stores a legacy mode, so the
// skill lists as current straight after the install, the diff agrees, and
// an edit put back by hand is current again.
func TestASkillStoredWithALegacyModeInstallsCurrent(t *testing.T) {
	t.Parallel()
	h, _, ids := legacyHome.copy(t)
	canonical := ids[0]
	equal(t, "state after the install", h.listed("nc")["state"], stateCurrent)
	equal(t, "the import tree", h.accountGit("rev-parse", "refs/heads/managed/nc:nc"), canonical)
	short := h.accountGit("log", "-1", "--format=%(trailers:key=Agentx-Upstream-Commit,valueonly)", "refs/heads/managed/nc")[:7]
	equal(t, "the diff", h.mustRun("skill", "diff", "nc").stdout, "nc matches its base version at "+short+"\n")

	file := filepath.Join(h.library, "nc", "a.md")
	writeFile(t, file, "an edit\n")
	equal(t, "state after an edit", h.listed("nc")["state"], stateModified)
	writeFile(t, file, "a\n")
	equal(t, "state with the edit put back", h.listed("nc")["state"], stateCurrent)
}
