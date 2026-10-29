package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/skillshare"
)

// storeEntry is one entry of Skillshare's metadata store, written as that
// tool writes it; the fields agentx never reads are left out.
type storeEntry struct {
	Source  string `json:"source"`
	Type    string `json:"type,omitempty"`
	RepoURL string `json:"repo_url,omitempty"`
	Subdir  string `json:"subdir,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Tracked bool   `json:"tracked,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Group   string `json:"group,omitempty"`
}

// skillshareSkills is the folder Skillshare installs into when nothing
// moves it: skills below its base directory under the XDG config home.
func (h *harness) skillshareSkills() string {
	return filepath.Join(h.config, "skillshare", "skills")
}

// writeStore writes Skillshare's store in dir and returns its bytes.
func (h *harness) writeStore(dir string, entries map[string]storeEntry) []byte {
	h.t.Helper()
	return h.writeRecordFile(filepath.Join(dir, skillshare.StoreName), map[string]any{"version": 1, "entries": entries})
}

// skillshareInstall lays a skill out the way Skillshare does: a copy of
// the source's files below its skills folder at key, and, as its default
// merge sync leaves it, a symlink to that copy in the library under the
// flat name.
func skillshareInstall(t *testing.T, h *harness, s *sourceRepo, subpath, key string) {
	t.Helper()
	dir := filepath.Join(h.skillshareSkills(), filepath.FromSlash(key))
	if err := copyTreeTo(filepath.Join(s.work, subpath), dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, filepath.Join(h.library, skillshare.LibraryName(key))); err != nil {
		t.Fatal(err)
	}
}

// TestAdoptInstallsWhatSkillshareInstalled: Skillshare keeps the real
// directory in its own folder and links it into the library, and a grouped
// skill lives below a group folder there. Adopting installs each from its
// source under the name the library already shows it as: the link gives way
// to a real directory, Skillshare's folder and store stay as they are, and
// a record of a tracked repository, a local path or an agent is left alone.
func TestAdoptInstallsWhatSkillshareInstalled(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n"})
	s.skill("skills/beta", "beta", "The second skill", nil)
	s.commit("two skills")
	skillshareInstall(t, h, s, "skills/alpha", "alpha")
	skillshareInstall(t, h, s, "skills/beta", "team/beta")
	for _, dir := range []string{"_tracked", "local"} {
		writeFile(t, mkdirs(t, filepath.Join(h.skillshareSkills(), dir), "SKILL.md"), skill(dir, "Not adoptable"))
	}
	store := h.writeStore(h.skillshareSkills(), map[string]storeEntry{
		"alpha":     {Source: "github.com/owner/repo/skills/alpha", Type: "github-subdir", RepoURL: s.url, Subdir: "skills/alpha"},
		"team/beta": {Source: "github.com/owner/repo/skills/beta", Type: "github-subdir", RepoURL: s.url, Subdir: "skills/beta", Group: "team"},
		"_tracked":  {Source: "github.com/owner/repo", Tracked: true},
		"local":     {Source: "/somewhere/local", Type: "local"},
		"reviewer":  {Source: "github.com/owner/agents/reviewer.md", Type: "github-subdir", Kind: "agent"},
		"../escape": {Source: "github.com/owner/repo/skills/alpha", Type: "github-subdir", RepoURL: s.url, Subdir: "skills/alpha"},
	})
	before := libraryTree(t, h.skillshareSkills())

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	h.lockUnchanged(filepath.Join(h.skillshareSkills(), skillshare.StoreName), store)
	sameTree(t, "Skillshare's folder", libraryTree(t, h.skillshareSkills()), before)
	contains(t, "stderr", out.stderr, "../escape")
	states := map[string]string{}
	reasons := map[string]string{}
	for _, ev := range h.eventsOfType(out.stdout, "adoption") {
		equal(t, ev["name"].(string)+" tool", ev["tool"], toolSkillshare)
		equal(t, ev["name"].(string)+" file", ev["file"], filepath.Join(h.skillshareSkills(), skillshare.StoreName))
		states[ev["name"].(string)] = ev["state"].(string)
		reasons[ev["name"].(string)], _ = ev["reason"].(string)
	}
	for _, c := range []struct{ name, state, reason string }{
		{"alpha", adoptAdopted, ""},
		{"team__beta", adoptAdopted, ""},
		{"_tracked", adoptRefused, "tracks as a whole"},
		{"local", adoptRefused, "not a git repository agentx can fetch"},
	} {
		if states[c.name] != c.state {
			t.Errorf("%s is %q, want %q", c.name, states[c.name], c.state)
		}
		if c.reason != "" && !strings.Contains(reasons[c.name], c.reason) {
			t.Errorf("%s: reason %q, want one about %q", c.name, reasons[c.name], c.reason)
		}
	}
	if _, reported := states["reviewer"]; reported {
		t.Error("an agent was reported as a skill")
	}
	for name, subpath := range map[string]string{"alpha": "skills/alpha", "team__beta": "skills/beta"} {
		path := filepath.Join(h.library, name)
		if info, err := os.Lstat(path); err != nil || !info.IsDir() {
			t.Errorf("%s is not a real directory of the library after adopting: %v", path, err)
		}
		sameTree(t, name, libraryTree(t, path), libraryTree(t, filepath.Join(s.work, subpath)))
		if h.accountGit("rev-parse", "--verify", "refs/heads/managed/"+name) == "" {
			t.Errorf("%s has no import branch", name)
		}
	}
	list := h.run("--json", "skill", "list")
	kinds := map[string]any{}
	for _, ev := range h.eventsOfType(list.stdout, "library_skill") {
		kinds[ev["name"].(string)] = ev["kind"]
	}
	equal(t, "alpha", kinds["alpha"], "managed")
	equal(t, "team__beta", kinds["team__beta"], "managed")
}

// TestAdoptFindsSkillshareWhereItsConfigurationSays: Skillshare's
// configuration can move the folder it installs into, and the store with
// it. The preview reads the record there and reports it under the tool's
// name.
func TestAdoptFindsSkillshareWhereItsConfigurationSays(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	s.skill("skills/alpha", "alpha", "The first skill", nil)
	s.commit("one skill")
	moved := filepath.Join(h.home, "my-skills")
	writeFile(t, mkdirs(t, filepath.Join(h.config, "skillshare"), "config.yaml"), "sources:\n  skills: ~/my-skills\nmode: merge\n")
	if err := copyTreeTo(filepath.Join(s.work, "skills/alpha"), filepath.Join(moved, "alpha")); err != nil {
		t.Fatal(err)
	}
	h.writeStore(moved, map[string]storeEntry{
		"alpha": {Source: "github.com/owner/repo/skills/alpha", Type: "github-subdir", RepoURL: s.url, Subdir: "skills/alpha"},
	})

	out := h.run("--json", "adopt")
	equal(t, "exit", out.exit, 0)
	ev := h.one(out.stdout, "adoption")
	equal(t, "name", ev["name"], "alpha")
	equal(t, "tool", ev["tool"], toolSkillshare)
	equal(t, "state", ev["state"], adoptCandidate)
	equal(t, "file", ev["file"], filepath.Join(moved, skillshare.StoreName))
	contains(t, "stdout", out.stdout, "skillshare")
}
