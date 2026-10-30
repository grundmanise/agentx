package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lockEntry is one entry of the Vercel skills lock file, written as that
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

// lockPath is where the Vercel skills CLI keeps its lock file when
// XDG_STATE_HOME is not set, beside the library it installs into.
func (h *harness) lockPath() string {
	return filepath.Join(h.home, ".agents", ".skill-lock.json")
}

// writeLock writes a lock file the way that tool does and returns its bytes,
// so that a test can prove afterwards that agentx did not touch it.
func (h *harness) writeLock(path string, skills map[string]lockEntry) []byte {
	h.t.Helper()
	return h.writeRecordFile(path, map[string]any{"version": 3, "skills": skills})
}

// writeRecordFile writes another tool's record file as JSON and returns its
// bytes.
func (h *harness) writeRecordFile(path string, content any) []byte {
	h.t.Helper()
	b, err := json.MarshalIndent(content, "", "  ")
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

// lockUnchanged fails the test when another tool's file is not byte for
// byte what it was. agentx reads those files and never writes them, on any
// path.
func (h *harness) lockUnchanged(path string, want []byte) {
	h.t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatalf("the file is gone: %v", err)
	}
	if string(got) != string(want) {
		h.t.Errorf("agentx wrote another tool's file:\n--- before\n%s\n--- after\n%s", want, got)
	}
}

// vercelInstall lays a skill out the way the Vercel skills CLI does: a
// plain directory copy of the source's files in the library, with nothing
// anywhere that says where it came from but the lock file, and in each
// client's folder a relative symlink to that directory.
func vercelInstall(t *testing.T, h *harness, s *sourceRepo, subpath, name string) {
	t.Helper()
	if err := os.MkdirAll(h.library, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyTreeTo(filepath.Join(s.work, subpath), filepath.Join(h.library, name)); err != nil {
		t.Fatal(err)
	}
	client := filepath.Join(h.home, ".claude", "skills")
	if err := os.MkdirAll(client, 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(client, filepath.Join(h.library, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, filepath.Join(client, name)); err != nil {
		t.Fatal(err)
	}
}

// adoptHarness is a machine the Vercel skills CLI installed a skill on: a
// source with two versions of alpha, the library holding the first of them,
// and no agentx state at all. It returns the two commits.
func adoptHarness(t *testing.T) (*harness, *sourceRepo, string, string) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("skills", true)
	ids := s.advance("adoptHarness", func(s *sourceRepo) []string {
		s.skill("skills/alpha", "alpha", "The first skill", map[string]string{"notes.md": "alpha notes\n", "_template.md": "a template\n"})
		s.write("README.md", "# skills\n")
		return []string{s.commit("the version the other tool installed")}
	})
	vercelInstall(t, h, s, "skills/alpha", "alpha")
	revised := s.advance("adoptHarness, revised", func(s *sourceRepo) []string {
		s.skill("skills/alpha", "alpha", "The first skill, revised", map[string]string{"notes.md": "alpha notes, revised\n"})
		return []string{s.commit("a version nobody on this machine has")}
	})
	return h, s, ids[0], revised[0]
}

// editLibrary changes a file of a library directory by hand, the way a user
// does after installing a skill.
func editLibrary(t *testing.T, h *harness, name, file, content string) {
	t.Helper()
	writeFile(t, filepath.Join(h.library, name, file), content)
}

// libraryTree is the content of a directory, path by path.
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

// adoptPhases is the progress a run that adds one source and adopts one
// skill reports: the fetch of the source, then the three steps of an
// install and the rescan that ends it.
const adoptPhases = "fetch,blobs,import,install,rescan"

// TestAdoptInstallsTheVersionTheSourceHoldsNow is the rule the whole
// command turns on. The library holds the version the other tool installed,
// with an edit of the user's on top, and the source has moved on since.
// Adopting installs the skill from its source as skill add does: the
// library directory becomes the version the source holds now, the edit
// included, and the skill is managed and current afterwards. Adopting
// again changes nothing.
func TestAdoptInstallsTheVersionTheSourceHoldsNow(t *testing.T) {
	t.Parallel()
	h, s, _, revised := adoptHarness(t)
	editLibrary(t, h, "alpha", "notes.md", "an edit of mine\n")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha/SKILL.md",
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	sameTree(t, "the library directory", libraryTree(t, filepath.Join(h.library, "alpha")), map[string]string{
		"SKILL.md":     "---\nname: alpha\ndescription: The first skill, revised\n---\n\n# alpha\n",
		"notes.md":     "alpha notes, revised\n",
		"_template.md": "a template\n",
	})
	adoption := h.one(out.stdout, "adoption")
	equal(t, "state", adoption["state"], adoptAdopted)
	equal(t, "tool", adoption["tool"], toolVercel)
	equal(t, "file", adoption["file"], h.lockPath())
	installed := h.one(out.stdout, "library_skill")
	equal(t, "kind", installed["kind"], "managed")
	equal(t, "state", installed["state"], stateCurrent)
	equal(t, "upstream_commit", installed["upstream_commit"], revised)
	if phases := progressPhases(h.eventsOfType(out.stdout, "progress")); strings.Join(phases, ",") != adoptPhases {
		t.Errorf("progress phases = %v, want %s", phases, adoptPhases)
	}
	contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), "adopted alpha from "+s.url)
	// The Vercel CLI's link in the client's folder already leads to the
	// library directory, so it is the placement, kept as it is.
	if link, err := os.Readlink(filepath.Join(h.home, ".claude", "skills", "alpha")); err != nil || filepath.IsAbs(link) {
		t.Errorf("the client's link is %q (%v), want the Vercel CLI's relative link kept", link, err)
	}
	if strings.Contains(out.stderr, "no placement was made") {
		t.Errorf("the Vercel CLI's link was not taken as the placement:\n%s", out.stderr)
	}
	equal(t, "placements", len(installed["placements"].([]any)), 1)

	preview := h.run("--json", "adopt")
	equal(t, "exit", preview.exit, 0)
	equal(t, "state afterwards", h.one(preview.stdout, "adoption")["state"], adoptManaged)
	again := h.run("adopt", "--all")
	equal(t, "exit", again.exit, 0)
	contains(t, "stdout", again.stdout, "Already adopted.")
}

// TestAdoptPreviewWritesNothing: with no flag the command only looks, and
// an entry whose directory is gone is no candidate at all.
func TestAdoptPreviewWritesNothing(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha/SKILL.md"},
		"gone":  {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/gone"},
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
		t.Fatalf("%d adoption events, want one per entry this machine holds:\n%s", len(events), out.stdout)
	}
	ev := events[0]
	equal(t, "name", ev["name"], "alpha")
	equal(t, "tool", ev["tool"], toolVercel)
	equal(t, "state", ev["state"], adoptCandidate)
	equal(t, "source", ev["source"], s.url)
	equal(t, "subpath", ev["subpath"], "skills/alpha")
	equal(t, "file", ev["file"], h.lockPath())
	contains(t, "the reason", ev["reason"].(string), "is not added yet")
	contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), "1 no longer on this machine")
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
// settings entry, remote and all, pinned to the ref the lock file recorded.
func TestAdoptAddsTheSourceItNeeds(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	s.tag("v1")
	h.rewrite(s, "https://github.com/owner/repo")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: "https://github.com/owner/repo",
		SkillPath: "skills/alpha", Ref: "v1",
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	settings, err := os.ReadFile(filepath.Join(h.agentx, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	contains(t, "the settings", string(settings), `"url": "https://github.com/owner/repo"`)
	contains(t, "the settings", string(settings), `"pin": "v1"`)
	sourceEvent := h.one(out.stdout, "source")
	equal(t, "the source added", sourceEvent["url"], "https://github.com/owner/repo")
	equal(t, "the adoption", h.one(out.stdout, "adoption")["state"], adoptAdopted)
	if phases := progressPhases(h.eventsOfType(out.stdout, "progress")); strings.Join(phases, ",") != adoptPhases {
		t.Errorf("progress phases = %v, want %s", phases, adoptPhases)
	}
}

func progressPhases(events []jsonEvent) []string {
	var phases []string
	for _, e := range events {
		phases = append(phases, e["phase"].(string))
	}
	return phases
}

// TestAdoptRefusesAName tells the two ways a --skill can miss apart: a name
// no record holds at all, and one a record holds for a directory this
// machine no longer has.
func TestAdoptRefusesAName(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha"},
		"gone":  {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/gone"},
	})
	for _, c := range []struct{ name, want string }{
		{"nowhere", "no skill called \"nowhere\""},
		{"gone", "this machine no longer holds it"},
	} {
		out := h.run("--json", "adopt", "--skill", c.name)
		equal(t, "exit", out.exit, exitNotFound.exit)
		contains(t, "the error", lastError(t, h.events(out.stdout))["message"].(string), c.want)
	}
}

// TestAdoptSelectionCheck covers the combination of flags that contradicts
// itself, a usage error before anything is read, and the ones that do not.
func TestAdoptSelectionCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		sel  adoptSelection
		says string // "" for a selection that is sound
	}{
		{"a preview", adoptSelection{}, ""},
		{"every skill", adoptSelection{all: true}, ""},
		{"two skills", adoptSelection{names: []string{"alpha", "beta"}}, ""},
		{"every skill and one", adoptSelection{all: true, names: []string{"alpha"}}, "--all and --skill"},
	} {
		err := tc.sel.check()
		if tc.says == "" {
			if err != nil {
				t.Errorf("%s: %v, want none", tc.name, err)
			}
			continue
		}
		var f *failure
		if !errors.As(err, &f) || f.status != exitUsage {
			t.Errorf("%s: %v, want a usage error", tc.name, err)
			continue
		}
		contains(t, tc.name, f.message, tc.says)
	}
}

// TestAdoptFlagsThatAskForTwoThings is that check as the command answers
// it: a usage error before any record is read.
func TestAdoptFlagsThatAskForTwoThings(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("--json", "adopt", "--all", "--skill", "alpha")
	equal(t, "exit", out.exit, exitUsage.exit)
	equal(t, "error.code", lastError(t, h.events(out.stdout))["code"], "usage")
}
