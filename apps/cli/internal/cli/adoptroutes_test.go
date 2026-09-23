package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdoptFindsAVersionWhoseDirectoryIsGoneFromTheSource is the case the
// history search exists for. The other tool installed the skill, upstream
// deleted the directory afterwards, and the lock file still holds the tree
// id of the version that was installed. The source's own history holds that
// version, so it is found and the skill is adopted at it: an
// upstream-removed skill is a state agentx knows, not a reason to refuse.
func TestAdoptFindsAVersionWhoseDirectoryIsGoneFromTheSource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	s.write("README.md", "# skills\n")
	v1 := s.commit("the version the other tool installed")
	installed := s.treeAt(v1, "skills/alpha")
	vercelInstall(t, h, s, "skills/alpha", "alpha")
	if err := os.RemoveAll(filepath.Join(s.work, "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	s.commit("upstream removes the skill")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	ev := h.one(out.stdout, "adoption")
	equal(t, "state", ev["state"], adoptAdopted)
	equal(t, "upstream_commit", ev["upstream_commit"], v1)
	equal(t, "modified", ev["modified"], false)
	equal(t, "the import tree", h.accountGit("ls-tree", "refs/heads/managed/alpha^{tree}"), "040000 tree "+installed+"\talpha")
}

// TestAdoptTakesTheDirectoryWhenTheFolderHashNamesNothing covers the fall
// through between the two routes. The lock file carries that tool's own
// folder digest, which looks like an object id and names nothing in the
// source; the directory is untouched and is exactly the version the source
// has. The hash establishing nothing may not suppress the route that does,
// and 'agentx skill add' adopts this same directory.
func TestAdoptTakesTheDirectoryWhenTheFolderHashNamesNothing(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, hash string }{
		{"the vercel CLI's own folder digest", strings.Repeat("7", 64)},
		{"a tree id the source does not have", strings.Repeat("a", 40)},
		{"no folder hash at all", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixture{dirs: []string{".claude"}})
			s := h.newSourceRepo("skills", true)
			s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
			head := s.commit("the only version")
			vercelInstall(t, h, s, "skills/alpha", "alpha")
			lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
				Source: "owner/repo", SourceType: "github", SourceURL: s.url,
				SkillPath: "skills/alpha", SkillFolderHash: c.hash,
			}})

			out := h.run("--json", "adopt", "--all")
			equal(t, "exit", out.exit, 0)
			h.lockUnchanged(h.lockPath(), lock)
			ev := h.one(out.stdout, "adoption")
			equal(t, "state", ev["state"], adoptAdopted)
			equal(t, "modified", ev["modified"], false)
			equal(t, "upstream_commit", ev["upstream_commit"], head)
			equal(t, "the base hash", ev["base_hash"], ev["content_hash"])
			equal(t, "the import tree", h.accountGit("ls-tree", "refs/heads/managed/alpha^{tree}"),
				"040000 tree "+s.tree("skills/alpha")+"\talpha")
		})
	}
}

// TestAdoptDatesAVersionByTheCommitThatTookIt is the cross-machine identity
// the import commit exists to provide. Three machines hold the same
// directory and the same lock entry and fetched the same source at three
// different commits; the version is one version, so all three must record
// the commit at which the directory took that content and write the same
// import commit. A machine that recorded the commit it happened to fetch
// would give one skill two unrelated parentless commits.
func TestAdoptDatesAVersionByTheCommitThatTookIt(t *testing.T) {
	t.Parallel()
	first := newHarness(t)
	first.build(t, fixture{dirs: []string{".claude"}})
	s := first.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	s.write("README.md", "# skills\n")
	c1 := s.commit("alpha arrives")
	installed := s.treeAt(c1, "skills/alpha")

	machines := []*harness{first, newHarness(t), newHarness(t)}
	for _, m := range machines[1:] {
		m.build(t, fixture{dirs: []string{".claude"}})
	}
	for _, m := range machines {
		vercelInstall(t, m, s, "skills/alpha", "alpha")
		m.writeLock(m.lockPath(), map[string]lockEntry{"alpha": {
			Source: "owner/repo", SourceType: "github", SourceURL: s.url,
			SkillPath: "skills/alpha", SkillFolderHash: installed,
		}})
	}
	s.write("README.md", "# skills, with a line more\n")
	c2 := s.commit("a commit that does not touch alpha")
	s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": "alpha notes, revised\n"})
	s.commit("alpha is revised")

	// Each machine fetched the source at a different commit: two pins and
	// the branch the source follows, which is the third commit.
	for i, pin := range []string{"#" + c1, "#" + c2, ""} {
		if out := machines[i].run("source", "add", s.url+pin); out.exit != 0 {
			t.Fatalf("machine %d: source add: exit %d\n%s", i, out.exit, out.stderr)
		}
	}
	var imports []string
	for i, m := range machines {
		out := m.run("--json", "adopt", "--all")
		equal(t, "exit", out.exit, 0)
		ev := m.one(out.stdout, "adoption")
		if ev["upstream_commit"] != c1 {
			t.Errorf("machine %d recorded upstream_commit %v, want %s, the commit alpha took that content at", i, ev["upstream_commit"], c1)
		}
		imports = append(imports, m.accountGit("rev-parse", "refs/heads/managed/alpha"))
	}
	for i, got := range imports {
		if got != imports[0] {
			t.Errorf("machine %d wrote the import commit %s and machine 0 wrote %s: one version, two identities", i, got, imports[0])
			t.Logf("machine %d:\n%s", i, machines[i].accountGit("cat-file", "commit", got))
			t.Logf("machine 0:\n%s", machines[0].accountGit("cat-file", "commit", imports[0]))
		}
	}
}

// TestAdoptRecordsTheCommitAnInstallRecords holds every route to the commit
// an install of the same version records: the last commit that touched the
// skill's directory, reachable from the commit the version was read at. The
// source moved past the version on a commit that does not touch alpha, and
// each machine reads the version its own way: by the directory at either
// commit, by the folder hash, by --base naming the later commit, and by an
// install at that commit. One version, one import commit.
func TestAdoptRecordsTheCommitAnInstallRecords(t *testing.T) {
	t.Parallel()
	first := newHarness(t)
	first.build(t, fixture{dirs: []string{".claude"}})
	s := first.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	c1 := s.commitAt("alpha arrives", "1700000000 +0000")
	installed := s.treeAt(c1, "skills/alpha")
	s.write("README.md", "# skills\n")
	c2 := s.commitAt("a commit that does not touch alpha", "1710000000 +0000")

	machines := []struct {
		name, pin, hash string
		adopt           []string
	}{
		{"the directory, fetched at the commit that took it", c1, "", []string{"--all"}},
		{"the directory, fetched past it", c2, "", []string{"--all"}},
		{"the vercel CLI's own digest, fetched past it", c2, strings.Repeat("7", 64), []string{"--all"}},
		{"the folder hash, fetched past it", c2, installed, []string{"--all"}},
		{"--base naming a commit past it", c2, "", []string{"--skill", "alpha", "--base", c2}},
	}
	var imports []string
	for _, m := range machines {
		h := newHarness(t)
		h.build(t, fixture{dirs: []string{".claude"}})
		vercelInstall(t, h, s, "skills/alpha", "alpha")
		h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
			Source: "owner/repo", SourceType: "github", SourceURL: s.url,
			SkillPath: "skills/alpha", SkillFolderHash: m.hash,
		}})
		if out := h.run("source", "add", s.url+"#"+m.pin); out.exit != 0 {
			t.Fatalf("%s: source add: exit %d\n%s", m.name, out.exit, out.stderr)
		}
		out := h.run(append([]string{"--json", "adopt"}, m.adopt...)...)
		equal(t, m.name+": exit", out.exit, 0)
		ev := h.one(out.stdout, "adoption")
		equal(t, m.name+": upstream_commit", ev["upstream_commit"], c1)
		imports = append(imports, h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	}

	install := newHarness(t)
	install.build(t, fixture{dirs: []string{".claude"}})
	if out := install.run("source", "add", s.url+"#"+c2); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	if out := install.run("skill", "add", s.url+"#"+c2, "--skill", "alpha"); out.exit != 0 {
		t.Fatalf("skill add: exit %d\n%s", out.exit, out.stderr)
	}
	want := install.accountGit("rev-parse", "refs/heads/managed/alpha")
	for i, got := range imports {
		if got != want {
			t.Errorf("%s wrote the import commit %s and an install at %s wrote %s", machines[i].name, got, short(c2), want)
		}
	}
}

// TestAdoptFindsEveryUpstreamCommitInOneWalk adopts twelve skills of one
// source whose directories establish their versions. The commit each one
// records comes out of one walk of the source's history, the install's own,
// and not out of a git process per skill.
func TestAdoptFindsEveryUpstreamCommitInOneWalk(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("twelve", true)
	entries := map[string]lockEntry{}
	for i := range 12 {
		name := fmt.Sprintf("s%02d", i)
		s.skill("skills/"+name, name, "One of twelve", nil)
		entries[name] = lockEntry{Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/" + name}
	}
	took := s.commitAt("twelve skills", "1700000000 +0000")
	s.write("README.md", "# twelve\n")
	s.commitAt("a readme", "1710000000 +0000")
	for name := range entries {
		vercelInstall(t, h, s, "skills/"+name, name)
	}
	h.writeLock(h.lockPath(), entries)
	equal(t, "exit of source add", h.run("source", "add", s.url).exit, 0)

	calls := countingGit(t, h)
	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	all := strings.Join(calls(), "\n")
	if walks := strings.Count(all, " log --full-history "); walks != 1 {
		t.Errorf("%d history walks for twelve skills of one source, want 1:\n%s", walks, all)
	}
	for name := range entries {
		contains(t, "the history walk", all, " :(literal)skills/"+name)
		contains(t, name+"'s import commit", h.accountGit("cat-file", "commit", "refs/heads/managed/"+name), "Agentx-Upstream-Commit: "+took)
	}
}

// TestAdoptFindsAMergedVersionWhereAnInstallDoes: the version the lock file
// recorded reached the source's branch through a merge. The history search
// walks first parents and finds the version at the merge, and the commit
// recorded is still the one an install records: the side branch commit that
// changed the skill's directory, which the merge took unchanged.
func TestAdoptFindsAMergedVersionWhereAnInstallDoes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	base := s.commitAt("alpha arrives", "1700000000 +0000")
	s.write("README.md", "# skills\n")
	main := s.commitAt("the branch moves on", "1710000000 +0000")
	s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": "revised on a side branch\n"})
	s.run("add", "--all")
	tree := s.run("write-tree")
	side := s.run("commit-tree", s.run("rev-parse", base+":"), "-p", base, "-m", "side work")
	side = s.run("commit-tree", tree, "-p", side, "-m", "alpha is revised on the side")
	merge := s.run("commit-tree", tree, "-p", main, "-p", side, "-m", "merge the side work")
	s.run("update-ref", "refs/heads/main", merge)
	vercelInstall(t, h, s, "skills/alpha", "alpha")
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha", SkillFolderHash: s.treeAt(merge, "skills/alpha"),
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "upstream_commit", h.one(out.stdout, "adoption")["upstream_commit"], side)
	adopted := h.accountGit("rev-parse", "refs/heads/managed/alpha")

	install := newHarness(t)
	install.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "exit of skill add", install.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	equal(t, "the import commit an install writes", install.accountGit("rev-parse", "refs/heads/managed/alpha"), adopted)
}

// TestAdoptRecordsABackMergedVersionWhereAnInstallDoes: the change to
// alpha reached the branch through a back-merge whose first parent predates
// it, and the source moved on past that merge. One machine fetched the
// commit that made the change and another the later tip, and both adopt by
// the folder hash; the second one's history search finds the version at the
// back-merge. Both record the commit that changed alpha, which is what an
// install at the later tip records, and all three write one import commit.
func TestAdoptRecordsABackMergedVersionWhereAnInstallDoes(t *testing.T) {
	t.Parallel()
	first := newHarness(t)
	first.build(t, fixture{dirs: []string{".claude"}})
	s := first.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	base := s.commitAt("alpha arrives", "1700000000 +0000")
	s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": "revised\n"})
	change := s.commitAt("alpha is revised", "1710000000 +0000")
	feature := s.run("commit-tree", s.run("rev-parse", base+"^{tree}"), "-p", base, "-m", "work that predates the revision")
	backMerge := s.run("commit-tree", s.run("rev-parse", change+"^{tree}"), "-p", feature, "-p", change, "-m", "merge the revision into the work")
	s.run("update-ref", "refs/heads/main", backMerge)
	s.write("README.md", "# skills\n")
	tip := s.commitAt("a commit that does not touch alpha", "1730000000 +0000")
	revised := s.treeAt(change, "skills/alpha")
	if found := s.run("rev-list", "--first-parent", "-1", tip, "--", ":(literal)skills/alpha"); found != backMerge {
		t.Fatalf("the first-parent line changes alpha last at %s, want the back-merge %s", found, backMerge)
	}

	var imports []string
	for _, pin := range []string{change, tip} {
		h := newHarness(t)
		h.build(t, fixture{dirs: []string{".claude"}})
		vercelInstall(t, h, s, "skills/alpha", "alpha")
		h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
			Source: "owner/repo", SourceType: "github", SourceURL: s.url,
			SkillPath: "skills/alpha", SkillFolderHash: revised,
		}})
		if out := h.run("source", "add", s.url+"#"+pin); out.exit != 0 {
			t.Fatalf("source add at %s: exit %d\n%s", short(pin), out.exit, out.stderr)
		}
		out := h.run("--json", "adopt", "--all")
		equal(t, "exit of adopt at "+short(pin), out.exit, 0)
		equal(t, "upstream_commit at "+short(pin), h.one(out.stdout, "adoption")["upstream_commit"], change)
		imports = append(imports, h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	}

	install := newHarness(t)
	install.build(t, fixture{dirs: []string{".claude"}})
	if out := install.run("source", "add", s.url+"#"+tip); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	if out := install.run("skill", "add", s.url+"#"+tip, "--skill", "alpha"); out.exit != 0 {
		t.Fatalf("skill add: exit %d\n%s", out.exit, out.stderr)
	}
	contains(t, "the import commit an install writes", install.accountGit("cat-file", "commit", "refs/heads/managed/alpha"), "Agentx-Upstream-Commit: "+change)
	want := install.accountGit("rev-parse", "refs/heads/managed/alpha")
	for i, got := range imports {
		if got != want {
			t.Errorf("the adoption at %s wrote the import commit %s and an install at %s wrote %s", short([]string{change, tip}[i]), got, short(tip), want)
		}
	}
}

// TestAdoptBaseTakesABranchOrATag is the escape hatch every refusal names.
// A user who has to choose the version themselves knows it by the name the
// source publishes it under, so --base resolves a branch or a tag against
// the source and not only against what the account repo happens to hold: a
// fetch writes one ref and no tags at all, so nothing else would find it.
func TestAdoptBaseTakesABranchOrATag(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, base string }{
		{"a tag", "v1.2.0"},
		{"a tag by its full ref", "refs/tags/v1.2.0"},
		{"an annotated tag", "release-1"},
		{"a branch", "main"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixture{dirs: []string{".claude"}})
			s := h.newSourceRepo("skills", true)
			s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
			v1 := s.commit("the version the other tool installed")
			s.tag("v1.2.0")
			s.annotatedTag("release-1", "the first release")
			vercelInstall(t, h, s, "skills/alpha", "alpha")
			s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": "revised\n"})
			head := s.commit("a version nobody on this machine has")
			editLibrary(t, h, "alpha", "notes.md", "an edit of mine\n")
			lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
				Source: "owner/repo", SourceType: "github", SourceURL: s.url,
				SkillPath: "skills/alpha", SkillFolderHash: "",
			}})

			out := h.run("--json", "adopt", "--skill", "alpha", "--base", c.base)
			equal(t, "exit", out.exit, 0)
			h.lockUnchanged(h.lockPath(), lock)
			want := v1
			if c.base == "main" {
				want = head
			}
			ev := h.one(out.stdout, "adoption")
			equal(t, "state", ev["state"], adoptAdopted)
			equal(t, "upstream_commit", ev["upstream_commit"], want)
			equal(t, "modified", ev["modified"], true)
		})
	}
}

// TestAdoptBaseNamesNothingOfTheSource: a ref the source does not publish
// is still exit code 5, and the hint says what --base takes rather than
// naming a fetch that could not help.
func TestAdoptBaseNamesNothingOfTheSource(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: "",
	}})
	out := h.run("--json", "adopt", "--skill", "alpha", "--base", "v9.9.9")
	equal(t, "exit", out.exit, exitNotFound.exit)
	err := lastError(t, h.events(out.stdout))
	contains(t, "the error", err["message"].(string), "v9.9.9")
	if strings.Contains(err["hint"].(string), "source fetch") {
		t.Errorf("the hint names a fetch, which brings neither branches nor tags: %q", err["hint"])
	}
}

// TestAdoptRefusesAnEmptyDirectoryAloneAndAdoptsTheRest: a source whose
// skill directory is an empty tree, which git mktree writes and a fetch
// accepts, has no commit that touched it, so there is no upstream commit to
// record. That is the one skill's refusal, as for a directory with no
// regular file, and never the whole run's: the skill beside it is adopted,
// by --all and by --base alike.
func TestAdoptRefusesAnEmptyDirectoryAloneAndAdoptsTheRest(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		args []string
	}{
		{"every skill", []string{"--all"}},
		{"a chosen base", []string{"--skill", "empty", "--base", "main"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixture{dirs: []string{".claude"}})
			s := h.newSourceRepo("skills", true)
			s.skill("skills/alpha", "alpha", "The first skill", nil)
			s.commit("one skill")
			vercelInstall(t, h, s, "skills/alpha", "alpha")
			blank, err := s.git.IsolatedInput(context.Background(), s.gitDir, strings.NewReader(""), "hash-object", "-t", "tree", "-w", "--stdin")
			if err != nil {
				t.Fatal(err)
			}
			empty := strings.TrimSpace(blank)
			skills := s.mktree(s.replaced("HEAD:skills", "empty", empty)...)
			root := s.mktree(s.replaced("HEAD^{tree}", "skills", skills)...)
			s.bare("update-ref", "refs/heads/main", s.bare("commit-tree", root, "-p", "HEAD", "-m", "an empty skill directory"))
			if err := os.MkdirAll(filepath.Join(h.library, "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(h.library, "empty", "SKILL.md"), skill("empty", "A skill its source holds nothing of"))
			h.writeLock(h.lockPath(), map[string]lockEntry{
				"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: ""},
				"empty": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/empty", SkillFolderHash: empty},
			})

			out := h.run(append([]string{"--json", "adopt"}, c.args...)...)
			equal(t, "exit", out.exit, exitRefused.exit)
			states := map[string]any{}
			for _, ev := range h.eventsOfType(out.stdout, "adoption") {
				states[ev["name"].(string)] = ev["state"]
				if ev["name"] == "empty" {
					contains(t, "the reason", ev["reason"].(string), "has no regular file to import")
				}
			}
			want := map[string]any{"empty": adoptRefused}
			if c.args[0] == "--all" {
				want["alpha"] = adoptAdopted
			}
			equal(t, "the adoptions", fmt.Sprint(states), fmt.Sprint(want))
			if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/empty"); err == nil {
				t.Error("a directory with nothing in it was given an import branch")
			}
		})
	}
}
