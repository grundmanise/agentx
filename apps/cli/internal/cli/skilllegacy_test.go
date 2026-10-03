package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// legacyHarness is a machine with Claude Code and Cursor and a source whose
// skill nc is stored with mode 100664, a file at the top and one in a
// directory below, which git mktree takes and ls-tree reads back as 100644:
// the tree keeps an id of its own that no directory on disk has. canonical
// is the id git writes today for the same files. nc is installed and placed
// in both configurations.
func legacyHarness(t *testing.T) (h *harness, s *sourceRepo, canonical string) {
	t.Helper()
	h, s, ids := legacyHome.copy(t)
	return h, s, ids[0]
}

// legacyHome is the home legacyHarness hands out; its id is the canonical
// tree id of nc.
var legacyHome = &fixtureHome{
	source: "legacy",
	dirs:   []string{".claude", ".cursor"},
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
		h.mustRun("skill", "add", s.url, "--skill", "nc")
		return []string{canonical}
	},
}

// storeInOlderForm makes the import branch of nc what an agentx that reused
// a source's own tree whole wrote for the version it holds: the commit the
// branch holds, author, dates and message and all, over the source's tree
// with its legacy modes. It returns that commit and the one the branch
// held, which is the commit an install of the version writes today.
func (h *harness) storeInOlderForm(t *testing.T, s *sourceRepo) (legacy, canonical string) {
	t.Helper()
	ctx := context.Background()
	r := gitx.New(h.env, false, func(string, ...any) {})
	repo := gitx.AccountRepoPath(h.agentx)
	canonical = h.accountGit("rev-parse", "refs/heads/managed/nc")
	wrapped, err := r.IsolatedInput(ctx, repo, strings.NewReader("040000 tree "+s.tree("skills/nc")+"\tnc\n"), "mktree")
	if err != nil {
		t.Fatal(err)
	}
	tree := "tree " + h.accountGit("rev-parse", canonical+"^{tree}") + "\n"
	body := h.accountGit("cat-file", "commit", canonical) + "\n"
	if !strings.HasPrefix(body, tree) {
		t.Fatalf("the import commit does not start with its tree:\n%s", body)
	}
	body = "tree " + strings.TrimSpace(wrapped) + "\n" + strings.TrimPrefix(body, tree)
	out, err := r.IsolatedInput(ctx, repo, strings.NewReader(body), "hash-object", "-t", "commit", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	legacy = strings.TrimSpace(out)
	h.accountGit("update-ref", "refs/heads/managed/nc", legacy, canonical)
	equal(t, "the state over a branch stored in an older form", h.listed("nc")["state"], stateModified)
	return legacy, canonical
}

// ncShort is the short upstream commit nc's base version is named by.
func ncShort(h *harness) string {
	return h.accountGit("log", "-1", "--format=%(trailers:key=Agentx-Upstream-Commit,valueonly)", "refs/heads/managed/nc")[:7]
}

// TestASkillStoredWithALegacyModeInstallsCurrent: the import writes the
// tree git writes today for a source that stores a legacy mode, so the
// skill lists as current straight after the install, the diff agrees, and
// an edit put back by hand is current again. Over a branch an earlier
// agentx wrote with the source's own tree, an edit is still the one file
// the diff names.
func TestASkillStoredWithALegacyModeInstallsCurrent(t *testing.T) {
	t.Parallel()
	h, s, canonical := legacyHarness(t)
	equal(t, "state after the install", h.listed("nc")["state"], stateCurrent)
	equal(t, "the import tree", h.accountGit("rev-parse", "refs/heads/managed/nc:nc"), canonical)
	short := ncShort(h)
	equal(t, "the diff", h.mustRun("skill", "diff", "nc").stdout, "nc matches its base version at "+short+"\n")

	file := filepath.Join(h.library, "nc", "a.md")
	writeFile(t, file, "an edit\n")
	equal(t, "state after an edit", h.listed("nc")["state"], stateModified)
	writeFile(t, file, "a\n")
	equal(t, "state with the edit put back", h.listed("nc")["state"], stateCurrent)

	h.storeInOlderForm(t, s)
	writeFile(t, file, "another edit\n")
	diffs := h.eventsOfType(h.mustRun("--json", "skill", "diff", "nc").stdout, "diff")
	if len(diffs) != 1 || diffs[0]["path"] != "a.md" {
		t.Errorf("the diff over the older branch = %v, want a.md alone", diffs)
	}
}

// TestInstallingTheSameVersionStoresAnOlderBranchAgain: over a branch an
// earlier agentx stored with the source's legacy modes, a library
// directory that holds every file of its base version lists as modified
// all the same, since no directory is current against that tree. The diff
// says the one difference is where the version is stored and names the
// install that stores it again, which, with a file git ignores in the
// directory, takes moving that file out first. Installing that version is
// the same version installed again, told by its four trailers rather than
// by the commit: the branch moves, from the commit it holds, to the one
// the install writes today, and the skill is current. With the library
// directory gone as well, the install lays it out again.
func TestInstallingTheSameVersionStoresAnOlderBranchAgain(t *testing.T) {
	t.Parallel()
	h, s, _ := legacyHarness(t)
	legacy, tip := h.storeInOlderForm(t, s)

	ignored := filepath.Join(h.library, "nc", ".DS_Store")
	writeFile(t, ignored, "finder\n")
	contains(t, "the diff with a file git ignores", h.mustRun("skill", "diff", "nc").stdout,
		"move what git ignores out of "+filepath.Join(h.library, "nc")+", then run 'agentx skill add")
	remove(t, ignored)

	stored := "nc differs from its base version at " + ncShort(h) + " only in how the account repo stores it;" +
		" run 'agentx skill add " + shellWord(s.url) + " --skill nc' to install that version again while the source still holds it," +
		" which stores it as git writes it today and changes no file, or, once 'agentx skill check' finds a newer version, run 'agentx skill update nc'"
	out := h.mustRun("--json", "skill", "diff", "nc")
	equal(t, "diff events", len(h.eventsOfType(out.stdout, "diff")), 0)
	equal(t, "the diff's result", h.one(out.stdout, "result")["summary"], stored)
	equal(t, "the import branch after the diff", h.accountGit("rev-parse", "refs/heads/managed/nc"), legacy)

	out = h.run("--json", "skill", "add", s.url, "--skill", "nc")
	if out.exit != 0 {
		t.Fatalf("add: exit %d\n%s", out.exit, out.stderr)
	}
	contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), "adopted nc from "+s.url)
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/nc"), tip)
	equal(t, "state", h.listed("nc")["state"], stateCurrent)

	h.storeInOlderForm(t, s)
	remove(t, filepath.Join(h.library, "nc"))
	h.mustRun("skill", "add", s.url, "--skill", "nc")
	equal(t, "the import branch with the directory gone", h.accountGit("rev-parse", "refs/heads/managed/nc"), tip)
	equal(t, "a.md", fileBody(t, filepath.Join(h.library, "nc", "a.md")), "a\n")
	equal(t, "state with the directory gone", h.listed("nc")["state"], stateCurrent)
	equal(t, "journals", journalCount(t, h), 0)
}

// TestSkillRemoveOfAnAbsentSkillJudgesACopyByItsBaseFiles: with the library
// directory gone, a recorded copy is compared with the base version the
// branch holds, and over a branch stored in an older form, which no
// directory is current against, a copy holding every file of that version
// goes without a word. A copy edited where it is still goes with the
// warning.
func TestSkillRemoveOfAnAbsentSkillJudgesACopyByItsBaseFiles(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, edit, warning string
	}{
		{"unchanged", "", ""},
		{"edited", "an edit in the copy\n", "cursor's copy of nc was different from its base version; removing it deleted those changes (%s)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _ := legacyHarness(t)
			h.mustRun("skill", "remove", "nc", "--from", "cursor")
			h.mustRun("skill", "place", "nc", "--to", "cursor", "--copy")
			cursor := filepath.Join(h.home, ".cursor", "skills", "nc")
			if c.edit != "" {
				writeFile(t, filepath.Join(cursor, "a.md"), c.edit)
			}
			h.storeInOlderForm(t, s)
			remove(t, filepath.Join(h.library, "nc"))

			out := h.run("--json", "skill", "remove", "nc")
			if out.exit != 0 {
				t.Fatalf("remove: exit %d\n%s", out.exit, out.stderr)
			}
			want := c.warning
			if want != "" {
				want = fmt.Sprintf(want, cursor)
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), want)
			nothingAt(t, "cursor's copy", cursor)
			equal(t, "the import branch", refValue(t, h, "refs/heads/managed/nc"), "")
		})
	}
}
