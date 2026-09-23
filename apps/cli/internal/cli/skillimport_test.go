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
	// Not parallel, for the reason harness_test.go gives above
	// suiteParallel: two homes, two sources and a version installed every
	// way there is make it the heaviest forker in the suite by an order of
	// magnitude.
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

// TestImportCommitMatchesTheSubpathLiterally installs skills whose
// directory names git would read as a pattern or as pathspec magic. The
// upstream commit is the last one that touched that directory and no
// other: as a pattern, "skills/star*" also matches skills/starry, which
// moves on after it, and ":(icase)odd" means ODD or odd in any case and
// never the directory of that name, so without a literal match the walk
// would not see that skill at all.
func TestImportCommitMatchesTheSubpathLiterally(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("patterns", true)
	s.skill("skills/star*", "star", "Named like a pattern", nil)
	s.skill("skills/starry", "other", "Matched by the pattern", nil)
	s.skill(":(icase)odd", "odd", "Named like pathspec magic", nil)
	touched := s.commit("three skills")
	s.skill("skills/starry", "other", "Matched by the pattern, revised", nil)
	s.skill("ODD", "loud", "Matched by the magic", nil)
	s.commit("the other skills move on")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	// One at a time: a walk over "skills/star*" as a pattern would also
	// print the tree of ":(icase)odd", which git cannot rule out for a
	// pattern.
	for _, name := range []string{"star", "odd"} {
		equal(t, name+": exit", h.run("skill", "add", s.url, "--skill", name).exit, 0)
		contains(t, name+": the import commit", h.accountGit("cat-file", "commit", "refs/heads/managed/"+name), "Agentx-Upstream-Commit: "+touched)
	}
}

// TestBulkImportCommitsDoNotDependOnTheFetchedTip installs a whole source
// on two machines that fetched it at different commits, the later one past
// a commit and a merge that touch none of its skills. Every skill gets the
// same import commit on both, recorded at the last commit that touched it,
// and a skill installed on its own gets the one it got in the bulk install.
func TestBulkImportCommitsDoNotDependOnTheFetchedTip(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("drift", true)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		s.skill("skills/"+name, name, "The "+name+" skill", map[string]string{"notes.md": name + " notes\n"})
	}
	first := s.commitAt("three skills", "1700000000 +0000")
	s.skill("skills/gamma", "gamma", "The gamma skill, revised", nil)
	gamma := s.commitAt("gamma moves on", "1710000000 +0000")
	s.write("README.md", "# skills\n")
	s.commitAt("a readme", "1720000000 +0000")
	equal(t, "exit of source add", h.run("source", "add", s.url).exit, 0)
	equal(t, "exit of skill add --all", h.run("skill", "add", s.url, "--all").exit, 0)
	want := map[string]string{}
	for name, upstream := range map[string]string{"alpha": first, "beta": first, "gamma": gamma} {
		want[name] = h.accountGit("rev-parse", "refs/heads/managed/"+name)
		contains(t, name+"'s import commit", h.accountGit("cat-file", "commit", want[name]), "Agentx-Upstream-Commit: "+upstream)
	}

	// The source moves on without touching a skill: a commit on its branch,
	// and a merge of a side branch whose first parent is that commit.
	s.write("README.md", "# skills, revised\n")
	main := s.commitAt("the readme moves on", "1730000000 +0000")
	s.write("docs/side.md", "side work\n")
	s.run("add", "--all")
	tree := s.run("write-tree")
	side := s.run("commit-tree", tree, "-p", first, "-m", "side work")
	merge := s.run("commit-tree", tree, "-p", main, "-p", side, "-m", "merge the side work")
	s.run("update-ref", "refs/heads/main", merge)

	later := newHarness(t)
	later.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "exit of the later source add", later.run("source", "add", s.url).exit, 0)
	equal(t, "the later fetch", later.accountGit("rev-parse", "refs/agentx/sources/"+source.ID(s.url)), merge)
	equal(t, "exit of the later skill add --all", later.run("skill", "add", s.url, "--all").exit, 0)
	one := newHarness(t)
	one.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "exit of the single install", one.run("skill", "add", s.url, "--skill", "gamma").exit, 0)
	for name, head := range want {
		if got := later.accountGit("rev-parse", "refs/heads/managed/"+name); got != head {
			t.Errorf("%s: the import branch is at %s in a home that fetched %s, %s in one that fetched before it", name, got, short(merge), head)
		}
	}
	equal(t, "gamma installed on its own", one.accountGit("rev-parse", "refs/heads/managed/gamma"), want["gamma"])
}

// TestUpstreamCommitIsTheOneGitLogNames installs a whole source whose
// history merges a change back onto work that predates it, and gives every
// skill the commit "git log -1 --no-renames <tip> -- :(literal)<subpath>"
// names for it alone. A merge that took a skill from its second parent is
// not that skill's change: a machine that fetched the change and one that
// fetched the merge get the same import commit. A merge that combined both
// sides of a skill is its change, and a skill one side changed and took
// back was last changed by taking it back.
func TestUpstreamCommitIsTheOneGitLogNames(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("merges", true)
	names := []string{"alpha", "beta", "gamma", "delta"}
	for _, name := range names {
		s.skill("skills/"+name, name, "The "+name+" skill", map[string]string{"notes.md": name + " notes\n"})
	}
	base := s.commit("four skills")
	commitTree := func(message string, parents ...string) string {
		s.run("add", "--all")
		args := []string{"commit-tree", s.run("write-tree"), "-m", message}
		for _, p := range parents {
			args = append(args, "-p", p)
		}
		return s.run(args...)
	}

	// The work the merge's first parent holds, all of it older than the
	// change to alpha: a readme, gamma's notes, and delta changed and taken
	// back.
	s.write("README.md", "# merges\n")
	s.write("skills/gamma/notes.md", "gamma notes, revised\n")
	s.write("skills/delta/notes.md", "delta notes, for a while\n")
	early := commitTree("a readme, gamma's notes and delta", base)
	s.write("skills/delta/notes.md", "delta notes\n")
	back := commitTree("take delta back", early)

	// The change, on a line of its own: alpha and gamma's description.
	if err := os.Remove(filepath.Join(s.work, "README.md")); err != nil {
		t.Fatal(err)
	}
	s.write("skills/gamma/notes.md", "gamma notes\n")
	s.skill("skills/alpha", "alpha", "The alpha skill, revised", nil)
	s.skill("skills/gamma", "gamma", "The gamma skill, revised", nil)
	change := commitTree("alpha and gamma move on", base)
	s.run("update-ref", "refs/heads/main", change)
	first := newHarness(t)
	first.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "exit of the first source add", first.run("source", "add", s.url).exit, 0)
	equal(t, "exit of the first skill add --all", first.run("skill", "add", s.url, "--all").exit, 0)

	// The change merged back onto the older work, which is the merge's
	// first parent.
	s.write("README.md", "# merges\n")
	s.write("skills/gamma/notes.md", "gamma notes, revised\n")
	merge := commitTree("merge the change back", back, change)
	s.run("update-ref", "refs/heads/main", merge)
	equal(t, "exit of source add", h.run("source", "add", s.url).exit, 0)
	equal(t, "the fetch", h.accountGit("rev-parse", "refs/agentx/sources/"+source.ID(s.url)), merge)
	equal(t, "exit of skill add --all", h.run("skill", "add", s.url, "--all").exit, 0)

	for name, want := range map[string]string{"alpha": change, "beta": base, "gamma": merge, "delta": back} {
		reference := s.run("log", "-1", "--no-renames", "--format=%H", merge, "--", ":(literal)skills/"+name)
		equal(t, name+": git log -1", reference, want)
		contains(t, name+"'s import commit", h.accountGit("cat-file", "commit", "refs/heads/managed/"+name), "Agentx-Upstream-Commit: "+reference)
	}
	for _, name := range []string{"alpha", "beta"} {
		equal(t, name+" fetched at the change and at the merge", h.accountGit("rev-parse", "refs/heads/managed/"+name), first.accountGit("rev-parse", "refs/heads/managed/"+name))
	}
}

// TestUpstreamWalkReadsNoBlob installs a skill whose history renames one of
// its files with a change. Finding the upstream commit compares trees
// alone: the blob the file had before the rename is never asked of the
// source, which a blobless account repo would have to fetch for it, or
// fail, to tell whether the two files are similar.
func TestUpstreamWalkReadsNoBlob(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("renames", true)
	long := strings.Repeat("a line the rename keeps\n", 40)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": long})
	before := s.commit("alpha with notes")
	old := s.run("rev-parse", before+":skills/alpha/notes.md")
	if err := os.Remove(filepath.Join(s.work, "skills", "alpha", "notes.md")); err != nil {
		t.Fatal(err)
	}
	s.write("skills/alpha/guide.md", long+"and one more\n")
	renamed := s.commit("rename the notes, with a change")
	equal(t, "exit of source add", h.run("source", "add", s.url).exit, 0)
	equal(t, "exit of skill add", h.run("skill", "add", s.url).exit, 0)
	contains(t, "the import commit", h.accountGit("cat-file", "commit", "refs/heads/managed/alpha"), "Agentx-Upstream-Commit: "+renamed)
	if strings.Contains(h.accountGit("cat-file", "--batch-all-objects", "--batch-check=%(objectname)"), old) {
		t.Errorf("the blob notes.md had before the rename, %s, was fetched into the account repo", short(old))
	}
}

// TestUpstreamOfASkillAtTheRootIsTheTip installs a skill at the root of a
// source together with one below it. The root skill's upstream commit is
// the tip, since every commit touches the repository; the other one's is
// the last commit that touched its own directory.
func TestUpstreamOfASkillAtTheRootIsTheTip(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("mixed", true)
	s.skill(".", "top", "A skill at the root", nil)
	s.skill("skills/inner", "inner", "A skill below it", nil)
	first := s.commit("two skills")
	s.write("README.md", "# mixed\n")
	tip := s.commit("a readme")
	equal(t, "exit of source add", h.run("source", "add", s.url).exit, 0)
	equal(t, "exit of skill add --all", h.run("skill", "add", s.url, "--all").exit, 0)
	contains(t, "the root skill's import commit", h.accountGit("cat-file", "commit", "refs/heads/managed/top"), "Agentx-Upstream-Commit: "+tip)
	contains(t, "the inner skill's import commit", h.accountGit("cat-file", "commit", "refs/heads/managed/inner"), "Agentx-Upstream-Commit: "+first)
}

// TestUpstreamsAttributesEachSubpath reads a history walk as git prints it
// and gives every subpath the commit git log -1 names for it alone: a merge
// that took the subpath from its second parent sends the walk down that
// parent, a directory whose name another one starts with is not the
// subpath, and a path is the source's own bytes, a newline included.
func TestUpstreamsAttributesEachSubpath(t *testing.T) {
	t.Parallel()
	id := func(c string) string { return strings.Repeat(c, 40) }
	c1, c2, c3, side, merge := id("1"), id("2"), id("3"), id("5"), id("9")
	zero := id("0")
	record := func(commit, epoch string, parents []string, entries ...string) string {
		r := "\x00" + strings.Join(append([]string{commit, epoch}, parents...), " ") + "\x00"
		for i := 0; i < len(entries); i += 2 {
			status := ":" + entries[i]
			if i == 0 {
				status = "\n" + status
			}
			r += status + "\x00" + entries[i+1] + "\x00"
		}
		return r
	}
	tree := func(old, new string) string { return "040000 040000 " + old + " " + new + " M" }
	added := func(new string) string { return "000000 040000 " + zero + " " + new + " A" }
	walk := record(merge, "1740000000", []string{c3, side}, tree(id("b"), id("c")), "skills/beta") +
		record(c3, "1730000000", []string{c2}, tree(id("d"), id("e")), "skills/alphabet") +
		record(side, "1725000000", []string{c1}, tree(id("b"), id("c")), "skills/beta") +
		record(c2, "1720000000", []string{c1}, tree(id("a"), id("f")), "skills/alpha", tree(id("6"), id("7")), "skills/new\nline", added(id("d")), "skills/alphabet") +
		record(c1, "1710000000", nil, added(id("a")), "skills/alpha", added(id("b")), "skills/beta", added(id("6")), "skills/new\nline")
	root := "\x00" + merge + " 1740000000\n"
	got, err := upstreams(merge, []string{"skills/alpha", "", "skills/beta", "skills/new\nline"}, []string{walk, root})
	if err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]upstream{
		"skills/alpha":     {commit: c2, when: "1720000000 +0000"},
		"skills/beta":      {commit: side, when: "1725000000 +0000"},
		"skills/new\nline": {commit: c2, when: "1720000000 +0000"},
		"":                 {commit: merge, when: "1740000000 +0000"},
	} {
		equal(t, "the upstream of "+p, got[p], want)
	}
	if _, err := upstreams(merge, []string{"skills/gamma"}, []string{walk}); err == nil {
		t.Error("a subpath no commit touches has an upstream")
	}
}
