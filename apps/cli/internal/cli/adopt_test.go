package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lockEntry is one entry of the vercel skills lock file, written as that
// tool writes it.
type lockEntry struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl"`
	Ref             string `json:"ref,omitempty"`
	SkillPath       string `json:"skillPath,omitempty"`
	SkillFolderHash string `json:"skillFolderHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
	PluginName      string `json:"pluginName,omitempty"`
}

// lockPath is where the vercel skills CLI keeps its lock file when
// XDG_STATE_HOME is not set, beside the library it installs into.
func (h *harness) lockPath() string {
	return filepath.Join(h.home, ".agents", ".skill-lock.json")
}

// writeLock writes a lock file the way that tool does and returns its bytes,
// so that a test can prove afterwards that agentx did not touch it.
func (h *harness) writeLock(path string, skills map[string]lockEntry) []byte {
	h.t.Helper()
	b, err := json.MarshalIndent(map[string]any{"version": 3, "skills": skills}, "", "  ")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		h.t.Fatal(err)
	}
	return b
}

// lockUnchanged fails the test when the lock file is not byte for byte what
// it was. agentx reads that file and never writes it, on any path.
func (h *harness) lockUnchanged(path string, want []byte) {
	h.t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatalf("the lock file is gone: %v", err)
	}
	if string(got) != string(want) {
		h.t.Errorf("agentx wrote the lock file:\n--- before\n%s\n--- after\n%s", want, got)
	}
}

// vercelInstall lays a skill out in the library the way the vercel skills
// CLI does: a plain directory copy of the source's files, with nothing
// anywhere that says where it came from but the lock file.
func vercelInstall(t *testing.T, h *harness, s *sourceRepo, subpath, name string) {
	t.Helper()
	if err := os.MkdirAll(h.library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyTreeTo(filepath.Join(s.work, subpath), filepath.Join(h.library, name)); err != nil {
		t.Fatal(err)
	}
}

// adoptHarness is a machine the vercel skills CLI installed a skill on: a
// source with two versions of alpha, the library holding the first of them,
// and no agentx state at all.
func adoptHarness(t *testing.T) (*harness, *sourceRepo, string, string) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	s.write("README.md", "# skills\n")
	v1 := s.commit("the version the other tool installed")
	installed := s.treeAt(v1, "skills/alpha")
	vercelInstall(t, h, s, "skills/alpha", "alpha")
	s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": "alpha notes, revised\n"})
	s.commit("a version nobody on this machine has")
	return h, s, v1, installed
}

// editLibrary changes a file of a library directory by hand, the way a user
// does after installing a skill.
func editLibrary(t *testing.T, h *harness, name, file, content string) {
	t.Helper()
	writeFile(t, filepath.Join(h.library, name, file), content)
}

// libraryTree is the content of a library directory, path by path, for a
// test that has to prove adoption changed nothing on disk.
func libraryTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		files[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sameTree(t *testing.T, what string, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s holds %d files, want %d: %v", what, len(got), len(want), got)
		return
	}
	for path, content := range want {
		if got[path] != content {
			t.Errorf("%s: %s is %q, want %q", what, path, got[path], content)
		}
	}
}

// TestAdoptRecordsTheInstalledVersionAndNotTheDirectory is the rule the
// whole command turns on. The library holds the version the other tool
// installed, with an edit of the user's on top, and the source has moved on
// since. Adoption must record the version that was installed as the base,
// leave the directory exactly as it is, and leave the edit out of the
// import commit: an edit that entered the import commit would be upstream
// content from then on, and no later update or revert could tell the two
// apart.
func TestAdoptRecordsTheInstalledVersionAndNotTheDirectory(t *testing.T) {
	t.Parallel()
	h, s, v1, installed := adoptHarness(t)
	editLibrary(t, h, "alpha", "notes.md", "alpha notes, and a line of mine\n")
	editLibrary(t, h, "alpha", "mine.md", "a file the user added\n")
	before := libraryTree(t, filepath.Join(h.library, "alpha"))
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha/SKILL.md", SkillFolderHash: installed,
		InstalledAt: "2026-01-01T00:00:00.000Z", UpdatedAt: "2026-01-01T00:00:00.000Z",
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)

	// The import commit holds the version that was installed, not what is on
	// disk: its tree entry is the source's own tree at that commit.
	entry := h.accountGit("ls-tree", "refs/heads/managed/alpha^{tree}")
	equal(t, "the import tree", entry, "040000 tree "+installed+"\talpha")
	message := h.accountGit("cat-file", "commit", "refs/heads/managed/alpha")
	contains(t, "the import commit", message, "Agentx-Upstream-Commit: "+v1)
	contains(t, "the import commit", message, "Agentx-Path: skills/alpha")
	for _, mine := range []string{"a line of mine", "mine.md", "a file the user added"} {
		if strings.Contains(h.accountGit("ls-tree", "-r", "refs/heads/managed/alpha"), mine) {
			t.Errorf("the import commit's tree holds the user's edit %q", mine)
		}
	}
	if blob := h.accountGit("cat-file", "blob", "refs/heads/managed/alpha:alpha/notes.md"); blob != "alpha notes" {
		t.Errorf("the import commit holds %q as notes.md, want the upstream version", blob)
	}

	// The directory is the user's and was not touched.
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), before)

	ev := h.one(out.stdout, "adoption")
	equal(t, "state", ev["state"], adoptAdopted)
	equal(t, "modified", ev["modified"], true)
	equal(t, "upstream_commit", ev["upstream_commit"], v1)
	if ev["base_hash"] == ev["content_hash"] {
		t.Errorf("the base version and the directory hash are the same: %v", ev)
	}
}

// TestAdoptedSkillIsTheSameImportAnInstallWrites checks that adoption and
// installation agree on what a version is: the same upstream version gives
// the same import commit, whichever command wrote it. Adopting at a version
// the source has moved past is an install of that version in every way but
// the directory it leaves alone.
func TestAdoptedSkillIsTheSameImportAnInstallWrites(t *testing.T) {
	t.Parallel()
	h, s, v1, installed := adoptHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})
	equal(t, "exit", h.run("adopt", "--all").exit, 0)
	adopted := h.accountGit("rev-parse", "refs/heads/managed/alpha")

	// A second machine installs that same version outright.
	second := newHarness(t)
	second.build(t, fixture{dirs: []string{".claude"}})
	if out := second.run("source", "add", s.url+"#"+v1); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	if out := second.run("skill", "add", s.url+"#"+v1, "--skill", "alpha"); out.exit != 0 {
		t.Fatalf("skill add: exit %d\n%s", out.exit, out.stderr)
	}
	installedCommit := second.accountGit("rev-parse", "refs/heads/managed/alpha")
	if adopted != installedCommit {
		t.Errorf("adoption wrote %s and an install of the same version wrote %s", adopted, installedCommit)
		t.Logf("adopted:\n%s", h.accountGit("cat-file", "commit", adopted))
		t.Logf("installed:\n%s", second.accountGit("cat-file", "commit", installedCommit))
	}
}

// TestAdoptLeavesASkillUnmanagedWhenItsVersionCannotBeEstablished is the
// other half of the rule: with nothing to establish the version the
// directory came from, agentx records nothing rather than recording what is
// on disk as if it had come from upstream. The skill stays unmanaged and
// the hint names --base as the one explicit way on.
//
// The directory is edited in every case, which is what makes the last route
// fail too: the same directory unedited is adopted whatever the folder hash
// says, which TestAdoptTakesTheDirectoryWhenTheFolderHashNamesNothing
// covers. So the reason has to name every route that was tried, which is
// what tells a refusal from a route that was never reached.
func TestAdoptLeavesASkillUnmanagedWhenItsVersionCannotBeEstablished(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, hash, tried string }{
		{"no folder hash at all", "", "records no folder hash for it"},
		{"a folder hash of that tool's own algorithm", strings.Repeat("7", 64), "has the folder hash"},
		{"a tree id the source does not have", strings.Repeat("a", 40), "has the folder hash"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, s, _, _ := adoptHarness(t)
			editLibrary(t, h, "alpha", "notes.md", "an edit that must never become upstream content\n")
			lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
				Source: "owner/repo", SourceType: "github", SourceURL: s.url,
				SkillPath: "skills/alpha", SkillFolderHash: c.hash,
			}})

			out := h.run("--json", "adopt", "--all")
			equal(t, "exit", out.exit, exitRefused.exit)
			h.lockUnchanged(h.lockPath(), lock)
			if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
				t.Error("a skill whose version could not be established was given an import branch")
			}
			ev := h.one(out.stdout, "adoption")
			equal(t, "state", ev["state"], adoptRefused)
			reason := ev["reason"].(string)
			contains(t, "the reason", reason, "cannot be established")
			contains(t, "the route the folder hash took", reason, c.tried)
			contains(t, "the route the directory took", reason, "the directory is not the version")
			equal(t, "the error hint", lastError(t, h.events(out.stdout))["hint"],
				"leave it unmanaged, or name the version it came from with 'agentx adopt --skill alpha --base <commit, branch or tag>'")
			// Unmanaged is what a listing must still call it.
			list := h.run("--json", "skill", "list")
			equal(t, "kind", h.one(list.stdout, "library_skill")["kind"], "unmanaged")
		})
	}
}

// TestAdoptTakesTheDirectoryAsTheVersionOnlyWhenItIsOne covers the one
// route on which what is on disk decides: a directory that holds exactly
// the version the source has now is that version, not an edit of one, so
// adopting it records an upstream version after all. The content hash is
// the whole of the check.
func TestAdoptTakesTheDirectoryAsTheVersionOnlyWhenItIsOne(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	head := s.commit("the only version")
	vercelInstall(t, h, s, "skills/alpha", "alpha")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: "",
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
}

// TestAdoptRefusesADirectoryThatIsNotAVersionOfTheSource is the same route
// with one line changed by hand. The directory is no longer the version the
// source has, nothing else says which version it was, and the run must
// leave it alone rather than import the edit.
func TestAdoptRefusesADirectoryThatIsNotAVersionOfTheSource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	s.commit("the only version")
	vercelInstall(t, h, s, "skills/alpha", "alpha")
	editLibrary(t, h, "alpha", "notes.md", "a line of mine\n")
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: "",
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("an edited directory of an unknown version was recorded as an upstream version")
	}
}

// TestAdoptWithAnExplicitBase is the way out the refusal names: the user
// says which version of the source is the base. It is still an upstream
// version, read from the source at the commit they named, and the directory
// they edited is left alone and shows as modified against it.
func TestAdoptWithAnExplicitBase(t *testing.T) {
	t.Parallel()
	h, s, v1, installed := adoptHarness(t)
	editLibrary(t, h, "alpha", "notes.md", "an edit of mine\n")
	before := libraryTree(t, filepath.Join(h.library, "alpha"))
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: "",
	}})

	out := h.run("--json", "adopt", "--skill", "alpha", "--base", v1)
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	ev := h.one(out.stdout, "adoption")
	equal(t, "state", ev["state"], adoptAdopted)
	equal(t, "modified", ev["modified"], true)
	equal(t, "upstream_commit", ev["upstream_commit"], v1)
	equal(t, "the import tree", h.accountGit("ls-tree", "refs/heads/managed/alpha^{tree}"), "040000 tree "+installed+"\talpha")
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), before)
}

// TestAdoptRefusesABaseThatIsNotOfThatSource keeps --base from making an
// import commit that says a version came from a source it did not: an
// object another source put in the account repo is not a version of this
// one, however readable it is.
func TestAdoptRefusesABaseThatIsNotOfThatSource(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	other := h.newSourceRepo("other", true)
	other.skill("skills/alpha", "alpha", "Another repository's skill", nil)
	elsewhere := other.commit("elsewhere")
	if out := h.run("source", "add", other.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: "",
	}})

	out := h.run("--json", "adopt", "--skill", "alpha", "--base", elsewhere)
	equal(t, "exit", out.exit, exitRefused.exit)
	contains(t, "the error", lastError(t, h.events(out.stdout))["message"].(string), "is not in the history of")
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("a commit of another source was recorded as this skill's base")
	}
}

// TestAdoptPreviewWritesNothing is the read-only half of the command: with
// no flag it says what it would do and leaves the machine exactly as it
// found it, agentx home included.
func TestAdoptPreviewWritesNothing(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha/SKILL.md", SkillFolderHash: installed},
		"gone":  {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/gone", SkillFolderHash: installed},
	})
	before := names(t, h.agentx)

	out := h.run("--json", "adopt")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	if after := names(t, h.agentx); after != before {
		t.Errorf("the preview changed agentx home: it held %q and now holds %q", before, after)
	}
	// Nothing was taken, created or signalled: no account repo, no lock file,
	// no mutation counter and no journals directory.
	for _, gone := range []string{"account.git", "lock", "version", "mutations"} {
		if _, err := os.Stat(filepath.Join(h.agentx, gone)); err == nil {
			t.Errorf("the preview created %s in agentx home", gone)
		}
	}
	events := h.eventsOfType(out.stdout, "adoption")
	if len(events) != 1 {
		t.Fatalf("%d adoption events, want one per entry the library holds:\n%s", len(events), out.stdout)
	}
	ev := events[0]
	equal(t, "name", ev["name"], "alpha")
	equal(t, "state", ev["state"], adoptCandidate)
	equal(t, "source", ev["source"], s.url)
	equal(t, "subpath", ev["subpath"], "skills/alpha")
	equal(t, "lock", ev["lock"], h.lockPath())
	contains(t, "the reason", ev["reason"].(string), "is not added yet")
}

// names lists what a directory holds, for a test that has to prove a
// command changed nothing in it.
func names(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, e := range entries {
		found = append(found, e.Name())
	}
	return strings.Join(found, " ")
}

// TestAdoptAddsTheSourceItNeeds: the source the lock file names has never
// been added on this machine, and adopting adds it the way the user would,
// settings entry, remote and all, before it reads anything from it.
func TestAdoptAddsTheSourceItNeeds(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	h.rewrite(s, "https://github.com/owner/repo")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: "https://github.com/owner/repo",
		SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	settings, err := os.ReadFile(filepath.Join(h.agentx, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	contains(t, "the settings", string(settings), `"url": "https://github.com/owner/repo"`)
	sourceEvent := h.one(out.stdout, "source")
	equal(t, "the source added", sourceEvent["url"], "https://github.com/owner/repo")
	equal(t, "the adoption", h.one(out.stdout, "adoption")["state"], adoptAdopted)
	if phases := progressPhases(h.eventsOfType(out.stdout, "progress")); strings.Join(phases, ",") != "fetch,adopt" {
		t.Errorf("progress phases = %v, want a fetch and an adopt", phases)
	}
}

func progressPhases(events []jsonEvent) []string {
	var phases []string
	for _, e := range events {
		phases = append(phases, e["phase"].(string))
	}
	return phases
}

// TestAdoptedSkillsShowAsManaged is what the user sees afterwards: the
// listing calls the skill managed, names its upstream and says the
// directory is modified, which is the whole point of having a base.
func TestAdoptedSkillsShowAsManaged(t *testing.T) {
	t.Parallel()
	h, s, v1, installed := adoptHarness(t)
	editLibrary(t, h, "alpha", "notes.md", "an edit of mine\n")
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})
	equal(t, "exit", h.run("adopt", "--all").exit, 0)

	out := h.run("--json", "skill", "list")
	equal(t, "exit", out.exit, 0)
	ev := h.one(out.stdout, "library_skill")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "state", ev["state"], stateModified)
	equal(t, "source", ev["source"], s.url)
	equal(t, "subpath", ev["subpath"], "skills/alpha")
	equal(t, "upstream_commit", ev["upstream_commit"], v1)
	contains(t, "the text listing", h.run("skill", "list").stdout, "alpha")
}

// TestAdoptAgainChangesNothing: a skill agentx already manages is reported
// as managed and left where it is, so running the command twice is the same
// as running it once.
func TestAdoptAgainChangesNothing(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})
	equal(t, "exit", h.run("adopt", "--all").exit, 0)
	branch := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	version := readVersionFile(t, h)

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "state", h.one(out.stdout, "adoption")["state"], adoptManaged)
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), branch)
	equal(t, "the mutation counter", readVersionFile(t, h), version)
}

func readVersionFile(t *testing.T, h *harness) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.agentx, "version"))
	if err != nil {
		return ""
	}
	return string(b)
}

// TestAdoptRefusesAName tells the two ways a --skill can miss apart: a name
// the lock file does not hold at all, and one it holds for a directory the
// library does not have.
func TestAdoptRefusesAName(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: installed},
		"gone":  {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/gone", SkillFolderHash: installed},
	})
	for _, c := range []struct{ name, want string }{
		{"nowhere", "no skill called \"nowhere\""},
		{"gone", "the library holds no \"gone\""},
	} {
		out := h.run("--json", "adopt", "--skill", c.name)
		equal(t, "exit", out.exit, exitNotFound.exit)
		contains(t, "the error", lastError(t, h.events(out.stdout))["message"].(string), c.want)
	}
}

// TestAdoptFlagsThatAskForTwoThings covers the combinations that contradict
// themselves, each a usage error before anything is read.
func TestAdoptFlagsThatAskForTwoThings(t *testing.T) {
	t.Parallel()
	h, _, _, _ := adoptHarness(t)
	for _, args := range [][]string{
		{"adopt", "--all", "--skill", "alpha"},
		{"adopt", "--all", "--base", "HEAD"},
		{"adopt", "--base", "HEAD"},
		{"adopt", "--skill", "alpha", "--skill", "beta", "--base", "HEAD"},
		{"adopt", "--skill", "alpha", "--base", "--not-a-ref"},
	} {
		if out := h.run(args...); out.exit != exitUsage.exit {
			t.Errorf("%v: exit %d, want %d\n%s", args, out.exit, exitUsage.exit, out.stderr)
		}
	}
}
