package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestImportCommitIsTheSameEverywhere installs one version of one skill in
// a second home that differs from the first in one way a commit id could
// depend on, and then in every way at once. The import branch must end up
// at the same commit each time, which is what lets two machines that accept
// the same upstream version share one object.
func TestImportCommitIsTheSameEverywhere(t *testing.T) {
	t.Parallel()
	first, s := installHarness(t)
	first.env["TZ"] = "UTC"
	equal(t, "exit", first.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	want := first.accountGit("rev-parse", "refs/heads/managed/alpha")

	for _, c := range []struct {
		name          string
		tz, machineID string
		userConfig    bool
	}{
		{name: "another time zone", tz: "Asia/Kathmandu"}, // a zone with a 45-minute offset
		{name: "a user git configuration", userConfig: true},
		{name: "another machine id", machineID: "fedcba9876543210fedcba9876543210"},
		{name: "all of them at once", tz: "Pacific/Chatham", machineID: "00112233445566778899aabbccddeeff", userConfig: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			second := newHarness(t)
			second.build(t, fixture{dirs: []string{".claude"}})
			if c.tz != "" {
				second.env["TZ"] = c.tz
			}
			if c.machineID != "" {
				writeFile(t, filepath.Join(second.agentx, "machine.json"), `{"id": "`+c.machineID+`"}`+"\n")
			}
			if c.userConfig {
				spoilTheGitConfig(t, second)
			}
			if out := second.run("source", "add", s.url); out.exit != 0 {
				t.Fatalf("source add in the second home: exit %d\n%s", out.exit, out.stderr)
			}
			if out := second.run("skill", "add", s.url, "--skill", "alpha"); out.exit != 0 {
				t.Fatalf("skill add in the second home: exit %d\n%s", out.exit, out.stderr)
			}
			got := second.accountGit("rev-parse", "refs/heads/managed/alpha")
			if got != want {
				t.Errorf("the import branch is at %s with %s and %s without", got, c.name, want)
				t.Logf("first:\n%s", first.accountGit("cat-file", "commit", want))
				t.Logf("second:\n%s", second.accountGit("cat-file", "commit", got))
			}
		})
	}
}

// spoilTheGitConfig gives the home a user git configuration with what most
// often changes a commit: line endings, signing, hooks and an identity. The
// isolated environment must keep all of it out.
func spoilTheGitConfig(t *testing.T, h *harness) {
	t.Helper()
	hooks := filepath.Join(h.home, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(hooks, "prepare-commit-msg"), "#!/bin/sh\necho spoiled >> \"$1\"\n")
	if err := os.Chmod(filepath.Join(hooks, "prepare-commit-msg"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.home, ".gitconfig"), strings.Join([]string{
		"[core]", "\tautocrlf = true", "\thooksPath = " + hooks,
		"[commit]", "\tgpgsign = true",
		"[user]", "\tname = Someone Else", "\temail = else@example.com",
		"[gpg]", "\tprogram = /nonexistent/gpg",
		"",
	}, "\n"))
}

// TestImportCommitReusesTheUpstreamTree checks that an install writes the
// commit over the objects the source already put in the account repo: the
// entry of the import tree is the upstream tree itself.
func TestImportCommitReusesTheUpstreamTree(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "beta").exit, 0)
	tree := h.accountGit("rev-parse", "refs/heads/managed/beta^{tree}")
	entry := h.accountGit("ls-tree", tree)
	equal(t, "the import tree", entry, "040000 tree "+s.tree("skills/beta")+"\tbeta")
}

// TestImportLeavesOutWhatIsNotAFile installs a skill whose directory holds
// a symlink. The import tree has no room for one, so it is left out with a
// warning and the library holds the rest; the content hash and the
// directory then agree, which is what lets the listing call the skill
// current.
func TestImportLeavesOutWhatIsNotAFile(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("linked", true)
	s.skill("tools/linked", "linked", "Holds a symlink", map[string]string{"notes.md": "notes\n"})
	if err := os.Symlink("notes.md", filepath.Join(s.work, "tools", "linked", "alias.md")); err != nil {
		t.Fatal(err)
	}
	s.commit("a skill with a symlink")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)

	out := h.run("skill", "add", s.url, "--skill", "linked")
	equal(t, "exit", out.exit, 0)
	contains(t, "stderr", out.stderr, "tools/linked/alias.md is not a regular file")
	if _, err := os.Lstat(filepath.Join(h.library, "linked", "alias.md")); err == nil {
		t.Error("the library holds the symlink the import left out")
	}
	for _, line := range strings.Split(h.accountGit("ls-tree", "-r", "refs/heads/managed/linked"), "\n") {
		if !strings.HasPrefix(line, "100644 ") && !strings.HasPrefix(line, "100755 ") {
			t.Errorf("the import tree holds %q, want only regular files", line)
		}
	}
	// The content hash the trailer records is the content hash of what was
	// laid down, so the skill is current rather than modified.
	list := h.run("--json", "skill", "list")
	equal(t, "state", h.one(list.stdout, "library_skill")["state"], "current")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLibraryNameAndImportDirectoryDiffer is the distinction between the two
// names a skill has: the library directory takes the frontmatter name, the
// import tree keeps the upstream's own directory name. Conflating them would
// make the commit id depend on frontmatter the content hash already covers.
func TestLibraryNameAndImportDirectoryDiffer(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("named", true)
	s.skill("tools/pdf-tools", "pdf", "Named by its frontmatter", nil)
	s.commit("a skill whose name is not its directory")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "pdf").exit, 0)

	// The library and the branch go by the frontmatter name.
	if _, err := os.Stat(filepath.Join(h.library, "pdf", "SKILL.md")); err != nil {
		t.Errorf("the library does not hold the skill under its frontmatter name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.library, "pdf-tools")); err == nil {
		t.Error("the library holds the upstream directory name as well")
	}
	head := h.accountGit("rev-parse", "refs/heads/managed/pdf")
	// The import tree goes by the upstream's own directory name.
	equal(t, "the import tree", h.accountGit("ls-tree", head), "040000 tree "+s.tree("tools/pdf-tools")+"\tpdf-tools")

	// A skill at the root of a repository with no frontmatter name takes the
	// repository name for both, as the source listing calls it.
	root := h.newSourceRepo("rooted", true)
	root.skill(".", "", "A skill at the root", nil)
	root.commit("a skill at the root")
	h.rewrite(root, "https://github.com/example/rooted") // fetch the local repository under a canonical URL
	equal(t, "exit", h.run("source", "add", "example/rooted").exit, 0)
	equal(t, "exit", h.run("skill", "add", "example/rooted").exit, 0)
	if _, err := os.Stat(filepath.Join(h.library, "rooted", "SKILL.md")); err != nil {
		t.Errorf("a skill at the root is not in the library under the repository name: %v", err)
	}
	rootHead := h.accountGit("rev-parse", "refs/heads/managed/rooted")
	contains(t, "the import tree", h.accountGit("ls-tree", rootHead), "\trooted")
	contains(t, "the import commit", h.accountGit("cat-file", "commit", rootHead), "Agentx-Path: .")
}

// TestStagingIsHiddenFromDiscovery is why the library directory is built in
// a hidden directory beside the one it becomes: no agent client, and no scan
// of agentx's own, ever sees a half-written skill.
func TestStagingIsHiddenFromDiscovery(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	staging := filepath.Join(h.library, ".agentx-staged-1234-1")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(staging, "SKILL.md"), "---\nname: half\ndescription: half written\n---\n")

	list := h.run("--json", "skill", "list")
	equal(t, "exit", list.exit, 0)
	for _, ev := range h.eventsOfType(list.stdout, "library_skill") {
		t.Errorf("a hidden staging directory was listed as %v", ev["name"])
	}
	scan := h.run("--json", "scan")
	equal(t, "exit", scan.exit, 0)
	if strings.Contains(scan.stdout, "half written") {
		t.Errorf("a scan found the hidden staging directory:\n%s", scan.stdout)
	}

	// And an install leaves none of its own behind.
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	entries, err := os.ReadDir(h.library)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentx-staged-") && e.Name() != ".agentx-staged-1234-1" {
			t.Errorf("the install left %s in the library", e.Name())
		}
	}
	// The one left by a run that stopped before its journal existed is swept
	// by the install that follows it.
	if _, err := os.Stat(staging); err == nil {
		t.Error("the stale staging directory was not swept")
	}
}

// TestImportCommitTakesTheUpstreamCommitterDate pins the date an import
// commit carries. It is the upstream commit's committer time, as epoch
// seconds with a zero offset, and not the date the isolated environment
// fixes for everything else agentx writes: the commit is a pure function of
// the version it holds, so two machines that accept the same upstream
// version write the same commit whenever they install it.
//
// The fixture's own commit has to be made at a time of its own for this to
// say anything. Every commit made through the isolated environment carries
// the same fixed date the fallback would use, so a source built that way
// cannot tell the two apart.
func TestImportCommitTakesTheUpstreamCommitterDate(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("dated", true)
	s.skill("tools/dated", "dated", "Committed elsewhere, at another time", nil)
	// An offset that is not zero, so that an import that kept the offset
	// rather than the instant would be caught too.
	const upstream = "1710000000"
	s.commitAt("a version committed elsewhere", upstream+" +0545")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "dated").exit, 0)

	equal(t, "the upstream committer time", s.bare("show", "-s", "--format=%ct", "HEAD"), upstream)
	body := h.accountGit("cat-file", "commit", "refs/heads/managed/dated")
	for _, who := range []string{"author", "committer"} {
		contains(t, "the import commit", body, who+" agentx <agentx@localhost> "+upstream+" +0000")
	}
	if strings.Contains(body, gitx.FixedDate) {
		t.Errorf("the import commit carries the date the isolated environment fixes, not the upstream's:\n%s", body)
	}
}

// TestImportCommitIgnoresUnrelatedUpstreamCommits pins the upstream commit
// an import records: the last commit that touched the skill's directory,
// not the tip the source was fetched at. A commit elsewhere in the source
// and a fetch of it leave an installed skill where it was, so installing it
// into another configuration still works, and a home that fetched the
// source later writes the same import commit for the same version.
func TestImportCommitIgnoresUnrelatedUpstreamCommits(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	touched := s.run("rev-parse", "HEAD") // the fixture's last commit changes skills/alpha
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	body := h.accountGit("cat-file", "commit", head)
	contains(t, "the import commit", body, "Agentx-Upstream-Commit: "+touched)
	contains(t, "the import subject", body, " at "+touched[:12]+" content ")

	// A commit that touches nothing of alpha, at a time of its own, so that
	// dates taken from the tip would show as well.
	s.write("README.md", "# skills, revised\n")
	const later = "1800000000"
	tip := s.commitAt("an unrelated change", later+" +0000")
	equal(t, "exit of source fetch", h.run("source", "fetch", s.url).exit, 0)
	equal(t, "the fetched tip", h.accountGit("rev-parse", "refs/agentx/sources/"+source.ID(s.url)), tip)

	out := h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor")
	equal(t, "exit of the second placement", out.exit, 0)
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)
	if _, err := os.Lstat(filepath.Join(h.home, ".cursor", "skills", "alpha")); err != nil {
		t.Errorf("the second configuration has no placement: %v", err)
	}

	// A home that fetched the source at the new tip writes the same commit.
	second := newHarness(t)
	second.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "exit of source add in the second home", second.run("source", "add", s.url).exit, 0)
	equal(t, "exit of skill add in the second home", second.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	got := second.accountGit("rev-parse", "refs/heads/managed/alpha")
	if got != head {
		t.Errorf("the import branch is at %s in a home that fetched %s, %s in one that fetched before it", got, short(tip), head)
		t.Logf("first:\n%s", body)
		t.Logf("second:\n%s", second.accountGit("cat-file", "commit", got))
	}
	if strings.Contains(second.accountGit("cat-file", "commit", got), later) {
		t.Error("the import commit takes its dates from the tip, not from the commit that touched the skill")
	}
}

// TestImportCommitMatchesTheSubpathLiterally installs a skill whose
// directory name is a pattern git would expand. The upstream commit is
// the last one that touched that directory, not one that touched another
// directory the pattern matches: as a pattern, "skills/star*" also matches
// the files of skills/starry, which moves on after it.
func TestImportCommitMatchesTheSubpathLiterally(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("patterns", true)
	s.skill("skills/star*", "star", "Named like a pattern", nil)
	s.skill("skills/starry", "other", "Matched by the pattern", nil)
	touched := s.commit("two skills")
	s.skill("skills/starry", "other", "Matched by the pattern, revised", nil)
	s.commit("the other skill moves on")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "star").exit, 0)
	contains(t, "the import commit", h.accountGit("cat-file", "commit", "refs/heads/managed/star"), "Agentx-Upstream-Commit: "+touched)
}
