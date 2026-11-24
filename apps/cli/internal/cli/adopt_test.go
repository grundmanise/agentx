package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
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

	out := h.run("--json", "skill", "import", "--all")
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
	contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), "imported alpha from "+s.url)
	// The Vercel CLI's link in the client's folder already leads to the
	// library directory, so it is the placement, kept as it is.
	if link, err := os.Readlink(filepath.Join(h.home, ".claude", "skills", "alpha")); err != nil || filepath.IsAbs(link) {
		t.Errorf("the client's link is %q (%v), want the Vercel CLI's relative link kept", link, err)
	}
	equal(t, "placements", len(installed["placements"].([]any)), 1)

	preview := h.run("--json", "skill", "import")
	equal(t, "exit", preview.exit, 0)
	equal(t, "state afterwards", h.one(preview.stdout, "adoption")["state"], adoptManaged)
	human := h.run("skill", "import")
	equal(t, "preview", human.stdout, "Already managed by agentx (1)\n  alpha\n\n"+
		"Nothing to import: this machine has no unmanaged skill.\n")
	again := h.run("skill", "import", "--all")
	equal(t, "exit", again.exit, 0)
	contains(t, "stdout", again.stdout, "Already imported.")
}

// TestAdoptPreviewWritesNothing: with no flag the command only looks, an
// entry whose directory is gone is no candidate at all, and nor is an
// unmanaged skill no tool recorded.
func TestAdoptPreviewWritesNothing(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha/SKILL.md"},
		"gone":  {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/gone"},
	})
	writeFile(t, mkdirs(t, filepath.Join(h.library, "notes"), "SKILL.md"), skill("notes", "Take notes"))
	before := names(t, h.agentx)

	out := h.run("--json", "skill", "import")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	if after := names(t, h.agentx); after != before {
		t.Errorf("the preview changed agentx home: it held %q and now holds %q", before, after)
	}
	// Nothing was taken, created or signalled: no store, no lock file,
	// no mutation counter and no journals directory.
	for _, gone := range []string{"store.git", "lock", "version", "mutations"} {
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

// TestAdoptPrintsWhatItAdopted pins the lines a run prints: the source it
// added and the skill it adopted, without the commit, the fetch time or the
// write access that source add and skill add print.
func TestAdoptPrintsWhatItAdopted(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha"},
	})

	out := h.run("skill", "import", "alpha")
	equal(t, "exit", out.exit, 0)
	lines := strings.SplitN(out.stdout, "\n", 3)
	equal(t, "the source line", lines[0], "✓ added source "+s.url+": 1 skill")
	equal(t, "the skill line", lines[1], "✓ imported alpha from "+s.url+" under skills/alpha: 1 placement")
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

	out := h.run("--json", "skill", "import", "--all")
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

// TestAdoptRefusesAName refuses, in one home and changing nothing, every
// --skill that cannot be adopted. A name can miss in two ways: no record
// names it and nothing holds it, or a record holds it for a directory this
// machine no longer has. The rest name a skill no record covers, which is
// refused as agentx skill import refuses an unmanaged skill: one agentx manages
// already, a name the library, the store or the account repo
// holds for another skill, directories that differ between two clients, a
// symlink where the directory would be, and what keeps a directory from
// moving into a worktree.
func TestAdoptRefusesAName(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha"},
		"gone":  {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/gone"},
	})
	h.mustRun("skill", "new", "mine")
	h.setAccount(newAccountRemote(t, h))
	// Branches for the names the store and the account repo hold.
	tip := h.ref(lineage.ForkRef("mine"))
	for _, ref := range []string{
		lineage.ManagedRef("shared"), lineage.ManagedRef("dup"), lineage.ManagedRef("lost"),
		lineage.RemoteForkPrefix(accountRemoteName(t, h)) + "published",
	} {
		h.storeGit("update-ref", ref, tip)
	}
	claude, cortex := filepath.Join(h.home, ".claude", "skills"), filepath.Join(h.home, ".snowflake/cortex", "skills")
	for _, dir := range []string{
		filepath.Join(h.library, "shared"), filepath.Join(h.library, "dup"), filepath.Join(claude, "dup"),
		filepath.Join(claude, "lost"), filepath.Join(claude, "published"), filepath.Join(claude, "differs"),
		filepath.Join(h.home, "elsewhere"), filepath.Join(h.library, "cloned"), filepath.Join(h.library, "held"),
		filepath.Join(h.library, "My_Skill"), filepath.Join(claude, "occupied"),
	} {
		writeFile(t, mkdirs(t, dir, "SKILL.md"), skill(filepath.Base(dir), "A skill"))
	}
	writeFile(t, mkdirs(t, filepath.Join(cortex, "differs"), "SKILL.md"), skill("differs", "Another skill"))
	for link, target := range map[string]string{
		filepath.Join(claude, "linked"):     filepath.Join(h.home, "elsewhere"),
		filepath.Join(h.library, "liblink"): filepath.Join(h.home, "elsewhere"),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, mkdirs(t, filepath.Join(h.library, "cloned", ".git"), "HEAD"), "ref: refs/heads/main\n")
	for _, dir := range []string{filepath.Join(h.agentx, "worktrees", "held"), filepath.Join(h.library, "occupied")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A skills directory this machine cannot write, which root can all the
	// same, so the row is left out for root.
	gemini := filepath.Join(h.home, ".gemini", "skills")
	writeFile(t, mkdirs(t, filepath.Join(gemini, "locked"), "SKILL.md"), skill("locked", "A skill"))
	if err := os.Chmod(gemini, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(gemini, 0o755) }) // so the temporary home can be removed
	// A plugin's skill, which only Claude Code's plugin provides: adopt, like
	// every command, reads a bare name as no plugin's.
	plugin := pluginHome(t, h)[0]
	writeFile(t, mkdirs(t, filepath.Join(plugin, "skills", "tidy"), "SKILL.md"), skill("tidy", "Tidy up"))
	refs, worktrees := h.storeGit("for-each-ref"), names(t, filepath.Join(h.agentx, "worktrees"))

	for _, c := range []struct {
		name string
		exit status
		want string
	}{
		{"nowhere", exitNotFound, `no skill called "nowhere" can be imported: no record names it`},
		{"gone", exitNotFound, "this machine no longer holds it"},
		{"shared", exitRefused, "shared is already managed by agentx"},
		{"mine", exitRefused, "mine is already an account skill"},
		{"dup", exitRefused, "the library already holds a skill called dup, a skill of a shared source, so " + filepath.Join(claude, "dup") + " cannot be imported under that name"},
		{"lost", exitRefused, "agentx already manages a skill called lost, so " + filepath.Join(claude, "lost") + " cannot be imported under that name"},
		{"published", exitRefused, "you already published a skill called published, so " + filepath.Join(claude, "published") + " cannot be imported under that name"},
		{"differs", exitRefused, "differs is in several agents' skills folders, with different files: " + filepath.Join(claude, "differs") + " (claude-code), " + filepath.Join(cortex, "differs") + " (cortex)"},
		{"linked", exitRefused, "so there is no folder of linked to import"},
		{"liblink", exitRefused, "liblink is a symlink to " + filepath.Join(h.home, "elsewhere") + ", and agentx imports folders only"},
		{"cloned", exitRefused, "cloned is a Git repository of its own, which agentx cannot import"},
		{"held", exitRefused, filepath.Join(h.agentx, "worktrees", "held") + " is in the way: agentx keeps held there once it is imported"},
		{"occupied", exitRefused, filepath.Join(h.library, "occupied") + " is in the way: importing occupied puts the skill there"},
		{"locked", exitRefused, "locked cannot be imported: agentx cannot write to " + gemini + " (permission denied)"},
		{"My_Skill", exitRefused, "My_Skill is not a valid skill name"},
		{"tidy", exitNotFound, `no skill called "tidy" can be imported: no record names it`},
	} {
		if c.name == "locked" && os.Geteuid() == 0 {
			continue
		}
		out := h.run("--json", "skill", "import", c.name)
		equal(t, c.name+": exit", out.exit, c.exit.exit)
		e := lastError(t, h.events(out.stdout))
		contains(t, c.name+": the error", e["message"].(string), c.want)
		if c.name == "dup" {
			equal(t, "dup: the hint", e["hint"], "rename the folder and import it under the new name, or replace it with 'agentx skill place dup --to claude-code --force', which discards what it holds")
		}
	}
	equal(t, "the refs", h.storeGit("for-each-ref"), refs)
	equal(t, "the worktrees", names(t, filepath.Join(h.agentx, "worktrees")), worktrees)

	// Adopting every skill lists, in the preview's words, each one that
	// only the run finds cannot be adopted, and succeeds.
	all := h.run("--json", "skill", "import", "--all")
	equal(t, "every skill: exit", all.exit, 0)
	for _, row := range []string{
		"published could not be imported: " + filepath.Join(claude, "published") + " has the name of a skill you already published. To import it, rename that folder",
		"differs could not be imported: in several agents' skills folders, with different files: " + filepath.Join(claude, "differs") + " (claude-code), " + filepath.Join(cortex, "differs") + " (cortex). To import it, keep one and remove or rename the others",
		"held could not be imported: " + filepath.Join(h.agentx, "worktrees", "held") + " is in the way: agentx keeps the skill there. To import it, move " + filepath.Join(h.agentx, "worktrees", "held") + " somewhere else",
	} {
		contains(t, "every skill: stderr", all.stderr, row)
	}
}

// TestAdoptTakesOverAnUnmanagedSkill adopts, in one home, three skills
// with their files as they are. One the library holds, which no tool
// recorded, becomes an account skill in its place: its content is the
// first commit of a branch of its own, by the user and with a new fork id,
// its directory moves into the worktree and the library holds a symlink to
// it. One three clients hold the same directory of comes out of the first
// client's skills directory: the library gets its symlink, the two clients
// that do not read the library get a symlink to the library where their
// directory was, and the one that reads it gets none. Neither run reports
// an adoption event. One the Vercel skills CLI recorded as installed from
// no git repository is adopted the same way, the user's edit included, by
// a run that names the tool: it reports the record as adopted, that tool's
// file is left as it was, and the next run finds the skill managed.
func TestAdoptTakesOverAnUnmanagedSkill(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.withIdentity("Ada Lovelace", "ada@example.com")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "notes"), "SKILL.md"), skill("notes", "Take notes"))
	text := h.mustRun("skill", "import", "notes")
	// No account repo is set, so the adoption ends with how to publish it.
	equal(t, "the text output", text.stdout, "✓ imported notes\n"+
		"No account repo is set: run 'agentx source add <url> --account', then 'agentx skill publish notes' to publish it.\n")
	tip := h.ref(lineage.ForkRef("notes"))
	equal(t, "the history", h.storeGit("rev-list", "--parents", lineage.ForkRef("notes")), tip)
	equal(t, "the author", h.storeGit("log", "-1", "--format=%an <%ae>", tip), "Ada Lovelace <ada@example.com>")
	equal(t, "the subject", h.storeGit("log", "-1", "--format=%s", tip), "Adopt notes")
	equal(t, "the tree", h.storeGit("ls-tree", "-r", "--name-only", tip), "notes/SKILL.md")
	if link, _ := os.Readlink(filepath.Join(h.library, "notes")); link != "../../../agentx/worktrees/notes/notes" {
		t.Errorf("the library symlink is %q", link)
	}
	ev := h.listed("notes")
	equal(t, "kind", ev["kind"], lineage.KindManaged)
	equal(t, "the fork id", ev["fork_id"], h.trailer(tip, lineage.TrailerForkID))

	clients := map[string]string{}
	for _, client := range []string{".claude", ".codex", ".snowflake/cortex"} {
		clients[client] = filepath.Join(h.home, client, "skills", "jot")
		writeFile(t, mkdirs(t, clients[client], "SKILL.md"), skill("jot", "Jot things down"))
		writeFile(t, filepath.Join(clients[client], "pad.md"), "a pad\n")
	}
	out := h.run("--json", "skill", "import", "jot")
	equal(t, "from the clients: exit", out.exit, 0)
	equal(t, "from the clients: adoption events", len(h.eventsOfType(out.stdout, "adoption")), 0)
	equal(t, "from the clients: kind", h.librarySkill(out.stdout, "jot")["kind"], lineage.KindManaged)
	equal(t, "from the clients: the summary", h.one(out.stdout, "result")["summary"],
		"imported jot from "+clients[".claude"]+" in 3 configurations; always available to universal clients: codex, gemini-cli")
	equal(t, "from the clients: the tree", h.storeGit("ls-tree", "-r", "--name-only", h.ref(lineage.ForkRef("jot"))), "jot/SKILL.md\njot/pad.md")
	equal(t, "from the clients: the moved file", fileBody(t, filepath.Join(h.agentx, "worktrees", "jot", "jot", "pad.md")), "a pad\n")
	if link, _ := os.Readlink(filepath.Join(h.library, "jot")); link != "../../../agentx/worktrees/jot/jot" {
		t.Errorf("from the clients: the library symlink is %q", link)
	}
	for _, client := range []string{".claude", ".snowflake/cortex"} {
		if target, _ := os.Readlink(clients[client]); target != filepath.Join(h.library, "jot") {
			t.Errorf("from the clients: %s's placement points at %q", client, target)
		}
		equal(t, "what is left in "+client, strings.Join(hiddenEntries(t, filepath.Dir(clients[client])), " "), "")
	}
	if _, err := os.Lstat(clients[".codex"]); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("from the clients: the directory of a client that reads the library is still there: %v", err)
	}

	vercelInstall(t, h, s, "skills/beta", "beta")
	editLibrary(t, h, "beta", "notes.md", "an edit of mine\n")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"beta": {
		Source: "owner/repo", SourceType: "well-known", SourceURL: s.url, SkillPath: "skills/beta/SKILL.md",
	}})
	out = h.run("--json", "skill", "import", "beta", "--from", "skills-cli")
	equal(t, "installed from no git repository: exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	equal(t, "installed from no git repository: the events", strings.Join(h.types(h.events(out.stdout)), ", "), "library_skill, adoption, result")
	equal(t, "installed from no git repository: the adoption", h.one(out.stdout, "adoption")["state"], adoptAdopted)
	equal(t, "installed from no git repository: the summary", h.one(out.stdout, "result")["summary"], "imported beta")
	equal(t, "installed from no git repository: the edit", fileBody(t, filepath.Join(h.agentx, "worktrees", "beta", "beta", "notes.md")), "an edit of mine\n")
	equal(t, "installed from no git repository: the subject", h.storeGit("log", "-1", "--format=%s", h.ref(lineage.ForkRef("beta"))), "Adopt beta")
	again := h.run("--json", "skill", "import", "--all")
	equal(t, "adopted already: exit", again.exit, 0)
	equal(t, "adopted already: the adoption", h.one(again.stdout, "adoption")["state"], adoptManaged)
}

// TestAdoptReplacesAClientsCopyOfALibrarySkill adopts a skill the library
// holds, which an agent client's own skills directory holds too, as a
// tool that installs into both leaves it. The client's directory that holds
// the same files, a system file such as Finder's .DS_Store aside, is a
// copy: it is removed and gets the library's symlink, as a placement, so
// the client no longer sees a directory agentx did not place. One that
// holds other files refuses the adoption, and nothing changes, but under
// --force, which replaces it as well.
func TestAdoptReplacesAClientsCopyOfALibrarySkill(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	library := filepath.Join(h.library, "rex")
	writeFile(t, mkdirs(t, library, "SKILL.md"), skill("rex", "Run things"))
	claude := filepath.Join(h.home, ".claude", "skills", "rex")
	writeFile(t, mkdirs(t, claude, "SKILL.md"), skill("rex", "Run things"))
	writeFile(t, filepath.Join(claude, ".DS_Store"), "finder\n")
	cortex := filepath.Join(h.home, ".snowflake", "cortex", "skills", "rex")
	writeFile(t, mkdirs(t, cortex, "SKILL.md"), skill("rex", "Run other things"))

	out := h.run("skill", "import", "rex")
	equal(t, "other files: exit", out.exit, 6)
	equal(t, "other files: stderr", out.stderr, "error: rex cannot be imported: \""+cortex+"\" contains files that differ from the skill version\n")
	for _, dir := range []string{library, claude, cortex} {
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			t.Errorf("the refused adoption changed %s: %v", dir, err)
		}
	}
	equal(t, "the other files", fileBody(t, filepath.Join(cortex, "SKILL.md")), skill("rex", "Run other things"))

	out = h.run("--json", "skill", "import", "rex", "--force")
	equal(t, "exit", out.exit, 0)
	equal(t, "warnings", out.stderr, "")
	equal(t, "kind", h.librarySkill(out.stdout, "rex")["kind"], lineage.KindManaged)
	contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), "imported rex in 2 configurations")
	if link, _ := os.Readlink(library); link != "../../../agentx/worktrees/rex/rex" {
		t.Errorf("the library symlink is %q", link)
	}
	for _, place := range []string{claude, cortex} {
		if target, _ := os.Readlink(place); target != library {
			t.Errorf("%s points at %q", place, target)
		}
	}
	equal(t, "what is left in .claude", strings.Join(hiddenEntries(t, filepath.Dir(claude)), " "), "")
}

// TestAdoptKeepsAClientsCopyWhereItPlacesCopies adopts a library skill
// that a client holds a copy of, where that client's configuration places
// copies: the copy stays where it is, and copy_mode records it as agentx's
// copy, a placement of the adopted skill. A copy that holds other files
// gets the library's files under --force, and is agentx's copy too.
func TestAdoptKeepsAClientsCopyWhereItPlacesCopies(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	h.mustRun("config", "placement", "claude-code", "copy")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "rex"), "SKILL.md"), skill("rex", "Run things"))
	claude := filepath.Join(h.home, ".claude", "skills", "rex")
	writeFile(t, mkdirs(t, claude, "SKILL.md"), skill("rex", "Run things"))
	writeFile(t, mkdirs(t, filepath.Join(h.library, "max"), "SKILL.md"), skill("max", "Run more"))
	edited := filepath.Join(h.home, ".claude", "skills", "max")
	writeFile(t, mkdirs(t, edited, "SKILL.md"), skill("max", "Run more, edited"))
	writeFile(t, filepath.Join(edited, "notes.md"), "mine\n")

	out := h.run("--json", "skill", "import", "max", "--force")
	equal(t, "--force: exit", out.exit, 0)
	equal(t, "--force: the copy", fileBody(t, filepath.Join(edited, "SKILL.md")), skill("max", "Run more"))
	if _, err := os.Lstat(filepath.Join(edited, "notes.md")); !os.IsNotExist(err) {
		t.Errorf("--force kept the copy's own file: %v", err)
	}

	out = h.run("--json", "skill", "import", "rex")
	equal(t, "exit", out.exit, 0)
	equal(t, "warnings", out.stderr, "")
	contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), "imported rex in 1 configuration")
	if info, err := os.Lstat(claude); err != nil || !info.IsDir() {
		t.Fatalf("claude-code's copy is gone or no directory: %v", err)
	}
	equal(t, "the copy", fileBody(t, filepath.Join(claude, "SKILL.md")), skill("rex", "Run things"))
	s, err := home.LoadSettings(h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	modes, _ := s.CopyModes()
	deep(t, "copy_mode", modes, map[string][]string{"max": {"claude-code"}, "rex": {"claude-code"}})
	mode := ""
	for _, p := range h.librarySkill(out.stdout, "rex")["placements"].([]any) {
		if p := p.(map[string]any); p["configuration"] == "claude-code" {
			mode, _ = p["mode"].(string)
		}
	}
	equal(t, "claude-code's placement", mode, "copy")
}

// TestChooseAdoptions sorts the names of a run into the skills a record
// holds, whatever the record says of them, and the ones none holds, which
// are looked for as unmanaged skills, once each and in name order. With a
// tool named, a name its records do not hold is not found.
func TestChooseAdoptions(t *testing.T) {
	t.Parallel()
	cands := []*candidate{
		{rec: record{name: "alpha"}, state: adoptCandidate},
		{rec: record{name: "beta"}, state: adoptRefused},
	}
	names := []string{"notes", "beta", "alpha", "beta"}
	for _, tc := range []struct {
		name        string
		sel         adoptSelection
		chosen, own string
		exit        int
	}{
		{"by name", adoptSelection{names: names}, "alpha beta", "notes", 0},
		{"by name, of one tool", adoptSelection{tool: toolVercel, names: names}, "", "", exitNotFound.exit},
		{"every record", adoptSelection{all: true}, "alpha beta", "", 0},
	} {
		chosen, own, err := (&invocation{}).chooseAdoptions(cands, nil, tc.sel)
		var got []string
		for _, c := range chosen {
			got = append(got, c.rec.name)
		}
		code := 0
		if f := (*failure)(nil); errors.As(err, &f) {
			code = f.status.exit
		}
		if strings.Join(got, " ") != tc.chosen || strings.Join(own, " ") != tc.own || code != tc.exit {
			t.Errorf("%s: chosen %v, own %v, exit %d, want %q, %q, exit %d", tc.name, got, own, code, tc.chosen, tc.own, tc.exit)
		}
	}
}

// TestAsItIsRefusal judges, in one home, every shape a skill can have on
// this machine when it was installed from somewhere agentx cannot install
// from: a folder in the library or in a client's skills directory is
// adopted as it is, and anything else is refused in a row of the preview
// that says what it is and what to do.
func TestAsItIsRefusal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lib, claude, store := filepath.Join(root, ".agents", "skills"), filepath.Join(root, ".claude", "skills"), filepath.Join(root, "store")
	inv := &invocation{dirs: home.Dirs{User: root, Home: filepath.Join(root, "agentx"), Library: lib, Config: filepath.Join(root, "config")}}
	for _, dir := range []string{
		filepath.Join(lib, "folder"), filepath.Join(claude, "client"), filepath.Join(lib, "Bad_Name"),
		filepath.Join(lib, "cloned"), filepath.Join(root, "work", "linked"), filepath.Join(store, "apart"),
	} {
		writeFile(t, mkdirs(t, dir, "SKILL.md"), skill(filepath.Base(dir), "A skill"))
	}
	writeFile(t, mkdirs(t, filepath.Join(lib, "cloned", ".git"), "HEAD"), "ref: refs/heads/main\n")
	writeFile(t, mkdirs(t, filepath.Join(lib, "bare"), "README.md"), "no skill\n")
	writeFile(t, filepath.Join(lib, "file"), "no folder\n")
	for link, target := range map[string]string{
		filepath.Join(lib, "linked"):       filepath.Join(root, "work", "linked"),
		filepath.Join(claude, "elsewhere"): "../../work/linked",
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	at := func(name string) string { return filepath.Join(lib, name) }
	for _, tc := range []struct {
		rec     record
		managed bool
		reason  string // "" for a skill adopted as it is
	}{
		{rec: record{name: "folder"}},
		{rec: record{name: "client"}},
		{rec: record{name: "linked"}, reason: "a symlink to " + filepath.Join(root, "work", "linked") + ". To import it, move that folder to " + at("linked")},
		{rec: record{name: "elsewhere"}, reason: "a symlink to " + filepath.Join(root, "work", "linked") + ". To import it, move that folder to " + filepath.Join(claude, "elsewhere")},
		{rec: record{name: "file"}, reason: "a file, and a skill is a folder. To import it, put the skill's folder at " + at("file")},
		{rec: record{name: "bare"}, reason: "a folder with no SKILL.md file. To import it, add a SKILL.md to " + at("bare")},
		{rec: record{name: "Bad_Name"}, reason: "not a valid skill name. To import it, rename " + at("Bad_Name")},
		{rec: record{name: "cloned"}, reason: "a Git repository of its own. To import it, move " + filepath.Join(at("cloned"), ".git") + " out of the folder"},
		{rec: record{name: "apart", dir: filepath.Join(store, "apart")}, reason: "only in " + filepath.Join(store, "apart") + ", where no agent looks for skills. To import it, move that folder to " + at("apart")},
		{rec: record{name: "apart", dir: filepath.Join(store, "apart"), tracked: true},
			reason: "a whole repository that Skillshare tracks. Add it with 'agentx source add <url>', then install its skills"},
		{rec: record{name: "folder"}, managed: true,
			reason: "agentx already manages a skill of this name, whose folder " + at("folder") + " is missing. To import this one, first run 'agentx skill remove folder'"},
	} {
		reason, f := inv.asItIsRefusal(tc.rec, tc.managed)
		if reason != tc.reason || (f == nil) != (tc.reason == "") {
			t.Errorf("%s: asItIsRefusal = %q, %v, want %q", tc.rec.name, reason, f, tc.reason)
		}
	}
}

// TestLooseSkillReason says, of an unmanaged skill no tool recorded, why a
// preview lists it as one it cannot adopt, and nothing of one it can: a
// folder of the library or of an agent client's skills directory.
func TestLooseSkillReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		skill  looseSkill
		reason string
	}{
		{"a folder of the library", looseSkill{name: "notes", path: "/lib/notes"}, ""},
		{"a folder of a client", looseSkill{name: "jot", path: "/claude/jot"}, ""},
		{"a symlink in the library", looseSkill{name: "linked", path: "/lib/linked", link: "/work/linked"},
			"a symlink to /work/linked. To import it, move that folder to /lib/linked"},
		{"a name that is no skill name", looseSkill{name: "My_Skill", path: "/lib/My_Skill"},
			"not a valid skill name. To import it, rename /lib/My_Skill"},
		{"a folder that is a Git repository", looseSkill{name: "cloned", path: "/lib/cloned", repo: "/lib/cloned/.git"},
			"a Git repository of its own. To import it, move /lib/cloned/.git out of the folder"},
		{"a client's folder named as a managed skill", looseSkill{name: "pdf", path: "/claude/pdf", managed: true},
			"/claude/pdf has the name of a skill agentx already manages. To import it, rename that folder"},
		{"a client's folder whose library path is taken", looseSkill{name: "pdf", path: "/claude/pdf", blocked: "/lib/pdf"},
			"/lib/pdf is in the way and is not a skill. To import it, move /lib/pdf somewhere else"},
	} {
		if got := tc.skill.reason(); got != tc.reason {
			t.Errorf("%s: reason = %q, want %q", tc.name, got, tc.reason)
		}
	}
}

// TestPreviewSummary is what the result of a preview says it found.
func TestPreviewSummary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		ready, refused, managed, gone int
		want                          string
	}{
		{0, 0, 0, 0, "nothing to import"},
		{2, 1, 1, 1, "3 unmanaged skills, 2 to import, 1 that cannot be imported, 1 already managed, 1 no longer on this machine"},
		{1, 0, 0, 0, "1 unmanaged skill, 1 to import"},
		{0, 0, 2, 0, "no unmanaged skill, 2 already managed"},
	} {
		if got := previewSummary(tc.ready, tc.refused, tc.managed, tc.gone); got != tc.want {
			t.Errorf("previewSummary(%d, %d, %d, %d) = %q, want %q", tc.ready, tc.refused, tc.managed, tc.gone, got, tc.want)
		}
	}
}

// TestAdoptPlacesDiffer compares the directories an adoption found in the
// clients' own skills directories: by the tree git would record, and by
// their bytes where git cannot record everything one of them holds.
func TestAdoptPlacesDiffer(t *testing.T) {
	t.Parallel()
	whole := func(tree, state string) adoptPlace { return adoptPlace{tree: tree, state: state, whole: true} }
	partial := func(tree, state string) adoptPlace { return adoptPlace{tree: tree, state: state} }
	for _, tc := range []struct {
		name   string
		places []adoptPlace
		differ bool
	}{
		{"one directory", []adoptPlace{whole("t1", "s1")}, false},
		{"the same tree", []adoptPlace{whole("t1", "s1"), whole("t1", "s1")}, false},
		{"the same tree and an empty directory git leaves out", []adoptPlace{whole("t1", "s1"), whole("t1", "s2")}, false},
		{"another tree", []adoptPlace{whole("t1", "s1"), whole("t2", "s2")}, true},
		{"the third differs", []adoptPlace{whole("t1", "s1"), whole("t1", "s1"), whole("t2", "s2")}, true},
		{"what git cannot record, the same bytes", []adoptPlace{partial("t1", "s1"), partial("t1", "s1")}, false},
		{"what git cannot record, other bytes", []adoptPlace{whole("t1", "s1"), partial("t1", "s2")}, true},
	} {
		if got := adoptPlacesDiffer(tc.places); got != tc.differ {
			t.Errorf("%s: adoptPlacesDiffer = %v, want %v", tc.name, got, tc.differ)
		}
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
		{"every skill and one", adoptSelection{all: true, names: []string{"alpha"}}, "--all and a skill name"},
		{"one skill, forced", adoptSelection{names: []string{"alpha"}, force: true}, ""},
		{"every skill, forced", adoptSelection{all: true, force: true}, "--force needs a skill name"},
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
	out := h.run("--json", "skill", "import", "alpha", "--all")
	equal(t, "exit", out.exit, exitUsage.exit)
	equal(t, "error.code", lastError(t, h.events(out.stdout))["code"], "usage")

	// The argument names a tool; a skill name there is pointed at --skill.
	out = h.run("--json", "skill", "import", "--from", "zoom;out")
	equal(t, "exit", out.exit, exitUsage.exit)
	contains(t, "the hint", lastError(t, h.events(out.stdout))["hint"].(string), "agentx skill import "+shellWord("zoom;out"))
}

// TestMixedPlacements names the configurations that read one directory
// and place skills differently, by name, the ones that place copies first.
func TestMixedPlacements(t *testing.T) {
	t.Parallel()
	zencoder := placeTarget{id: "zencoder", name: "Zencoder"}
	zenflow := placeTarget{id: "zenflow", name: "Zenflow", copies: true}
	other := placeTarget{id: "other", name: "Other", copies: true}
	equal(t, "one each", mixedPlacements([]placeTarget{zencoder, zenflow}),
		"is also read by Zenflow agent which is set to copy files, while Zencoder uses symlinks")
	equal(t, "two that copy", mixedPlacements([]placeTarget{zenflow, zencoder, other}),
		"is also read by Zenflow and Other agents which are set to copy files, while Zencoder uses symlinks")
}
