package cli

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// mktree writes a tree from ls-tree lines into the source and returns its
// id. git mktree takes names a checkout would never write, which is how a
// hostile source gets them into a repository.
func (s *sourceRepo) mktree(lines ...string) string {
	s.t.Helper()
	out, err := s.git.IsolatedInput(context.Background(), s.gitDir, strings.NewReader(strings.Join(lines, "\n")+"\n"), "mktree")
	if err != nil {
		s.t.Fatalf("git mktree: %v", err)
	}
	return strings.TrimSpace(out)
}

// replaced is the ls-tree of treeish with the entry name pointing at tree.
func (s *sourceRepo) replaced(treeish, name, tree string) []string {
	s.t.Helper()
	var lines []string
	for _, line := range strings.Split(s.bare("ls-tree", treeish), "\n") {
		if !strings.HasSuffix(line, "\t"+name) {
			lines = append(lines, line)
		}
	}
	return append(lines, "040000 tree "+tree+"\t"+name)
}

// hostileSource is a source whose skills/evil tree holds a ".." entry
// leading to a tree with a SKILL.md and pwned.txt, beside a plain skill at
// skills/alpha.
func hostileSource(t *testing.T, h *harness) *sourceRepo {
	t.Helper()
	s := h.newSourceRepo("hostile", true)
	s.skill("skills/alpha", "alpha", "A plain skill", nil)
	s.skill("skills/evil", "evil", "Climbs out", nil)
	s.commit("two skills")

	// skills/evil/../../pwned.txt, which from the staging directory in the
	// library is a file beside the library.
	blob, err := s.git.IsolatedInput(context.Background(), s.gitDir, strings.NewReader("pwned\n"), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	// A SKILL.md there too, which a listing that cleaned the path would
	// offer as a skill at skills/.
	inner := s.mktree("100644 blob "+strings.TrimSpace(blob)+"\tpwned.txt", "100644 blob "+s.run("rev-parse", "HEAD:skills/evil/SKILL.md")+"\tSKILL.md")
	up := s.mktree("040000 tree " + inner + "\t..")
	evil := s.mktree(append(strings.Split(s.bare("ls-tree", "HEAD:skills/evil"), "\n"), "040000 tree "+up+"\t..")...)
	skills := s.mktree(s.replaced("HEAD:skills", "evil", evil)...)
	root := s.mktree(s.replaced("HEAD^{tree}", "skills", skills)...)
	commit := s.bare("commit-tree", root, "-p", "HEAD", "-m", "a hostile tree")
	s.bare("update-ref", "refs/heads/main", commit)
	contains(t, "the source tree", s.bare("ls-tree", "-r", "HEAD"), "skills/evil/../../pwned.txt")
	return s
}

// TestSkillAddRefusesAnEntryThatClimbsOut installs from a source whose skill
// tree holds a ".." entry, which a checkout never writes but git mktree and
// a fetch both accept. Laid out by its path, it would write a file outside
// the staging directory and the library; the install refuses the skill
// before anything is written, names the entry, and leaves every other skill
// of the source installable.
func TestSkillAddRefusesAnEntryThatClimbsOut(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := hostileSource(t, h)

	equal(t, "exit of source add", h.run("source", "add", s.url).exit, 0)
	listed := h.run("--json", "source", "skills", s.url)
	equal(t, "exit of source skills", listed.exit, 0)
	var subpaths []string
	for _, ev := range h.eventsOfType(listed.stdout, "source_skill") {
		subpaths = append(subpaths, ev["subpath"].(string))
	}
	equal(t, "the skills listed", strings.Join(subpaths, " "), "skills/alpha skills/evil")
	out := h.run("skill", "add", s.url, "--skill", "evil")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, `the skill "evil" in `+s.url+` under skills/evil holds an entry agentx will not lay out: ".."`)
	sources := filepath.Join(filepath.Dir(h.home), "sources")
	_ = filepath.WalkDir(filepath.Dir(h.home), func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p == sources {
			return filepath.SkipDir
		}
		if err == nil && d.Name() == "pwned.txt" {
			t.Errorf("the install wrote %s", p)
		}
		return nil
	})
	if _, err := os.Lstat(filepath.Join(h.library, "evil")); err == nil {
		t.Error("the library holds the refused skill")
	}

	// The source is refused one skill at a time, not whole.
	equal(t, "exit of the other skill", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
}

// TestStageCopyStaysInside is the second line of defence: whatever path
// reaches the staging write, nothing lands outside the staging directory.
func TestStageCopyStaysInside(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	staged := filepath.Join(root, "library", ".agentx-staged-1-1")
	for _, p := range []string{"../../pwned.txt", "../pwned.txt", ".."} {
		v := &imported{name: "evil", files: []treeFile{{path: p, mode: source.FileMode, body: "pwned\n"}}}
		if err := (&invocation{}).stageCopy(staged, v.placeable()); err == nil {
			t.Errorf("stageCopy wrote %q", p)
		}
	}
	for _, p := range []string{filepath.Join(root, "pwned.txt"), filepath.Join(root, "library", "pwned.txt")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("stageCopy wrote %s", p)
		}
	}
}

// TestSkillAddRefusesASubpathThatClimbsOut names the hostile tree through
// the subpath of the URL rather than through a listing: a segment that
// decodes to skills/evil/../.. would have git resolve it through the
// source's own ".." entries, install the inner tree as a skill and record
// it as the source root. The URL is refused before anything is read.
func TestSkillAddRefusesASubpathThatClimbsOut(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := hostileSource(t, h)
	equal(t, "exit of source add", h.run("source", "add", s.url).exit, 0)

	for _, sub := range []string{"skills%2Fevil%2F..%2F..", "skills/evil%2F..%2F.."} {
		out := h.run("skill", "add", s.url+"/"+sub)
		equal(t, "exit of "+sub, out.exit, 1)
		contains(t, "stderr of "+sub, out.stderr, `the path "skills/evil/../.." inside the repository is not one agentx reads`)
	}
	if _, err := h.accountGitErr("rev-parse", "--verify", "--quiet", "refs/heads/managed/evil"); err == nil {
		t.Error("the account repo holds managed/evil")
	}
	if _, err := os.Lstat(filepath.Join(h.library, "evil")); err == nil {
		t.Error("the library holds the hostile tree")
	}
}
