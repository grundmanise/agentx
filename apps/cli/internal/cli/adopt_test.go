package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	equal(t, "placements", len(installed["placements"].([]any)), 1)

	preview := h.run("--json", "adopt")
	equal(t, "exit", preview.exit, 0)
	equal(t, "state afterwards", h.one(preview.stdout, "adoption")["state"], adoptManaged)
	human := h.run("adopt")
	equal(t, "preview", human.stdout, "Found 1 skill installed by the Vercel skills CLI.\n\n"+
		"Already managed by agentx (1)\n  alpha\n\n"+
		"Nothing to adopt: agentx already manages every one.\n"+
		"To take over an unmanaged skill no tool installed: agentx adopt --skill <name>\n")
	again := h.run("adopt", "--all")
	equal(t, "exit", again.exit, 0)
	contains(t, "stdout", again.stdout, "Already adopted.")
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

// TestAdoptPrintsWhatItAdopted pins the lines a run prints: the source it
// added and the skill it adopted, without the commit, the fetch time or the
// write access that source add and skill add print.
func TestAdoptPrintsWhatItAdopted(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha"},
	})

	out := h.run("adopt", "--skill", "alpha")
	equal(t, "exit", out.exit, 0)
	lines := strings.SplitN(out.stdout, "\n", 3)
	equal(t, "the source line", lines[0], "✓ added source "+s.url+": 1 skill")
	equal(t, "the skill line", lines[1], "✓ adopted alpha from "+s.url+" under skills/alpha: 1 placement")
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

// TestAdoptRefusesAName refuses, in one home and changing nothing, every
// --skill that cannot be adopted. A name can miss in two ways: no record
// names it and nothing holds it, or a record holds it for a directory this
// machine no longer has. The rest name a skill no record covers, which is
// refused as agentx adopt refuses an unmanaged skill: one agentx manages
// already, a name the library, the account repo or the account remote
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
	// Branches for the names the account repo and the account remote hold.
	tip := h.ref(lineage.ForkRef("mine"))
	for _, ref := range []string{
		lineage.ManagedRef("shared"), lineage.ManagedRef("dup"), lineage.ManagedRef("lost"),
		lineage.RemoteForkPrefix(accountRemoteName(t, h)) + "published",
	} {
		h.accountGit("update-ref", ref, tip)
	}
	claude, cursor := filepath.Join(h.home, ".claude", "skills"), filepath.Join(h.home, ".cursor", "skills")
	for _, dir := range []string{
		filepath.Join(h.library, "shared"), filepath.Join(h.library, "dup"), filepath.Join(claude, "dup"),
		filepath.Join(claude, "lost"), filepath.Join(claude, "published"), filepath.Join(claude, "differs"),
		filepath.Join(h.home, "elsewhere"), filepath.Join(h.library, "cloned"), filepath.Join(h.library, "held"),
		filepath.Join(h.library, "My_Skill"), filepath.Join(claude, "occupied"),
	} {
		writeFile(t, mkdirs(t, dir, "SKILL.md"), skill(filepath.Base(dir), "A skill"))
	}
	writeFile(t, mkdirs(t, filepath.Join(cursor, "differs"), "SKILL.md"), skill("differs", "Another skill"))
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
	// A plugin's skill, which only Claude Code's plugin provides.
	plugin := pluginHome(t, h)[0]
	writeFile(t, mkdirs(t, filepath.Join(plugin, "skills", "tidy"), "SKILL.md"), skill("tidy", "Tidy up"))
	refs, worktrees := h.accountGit("for-each-ref"), names(t, filepath.Join(h.agentx, "worktrees"))

	for _, c := range []struct {
		name string
		exit status
		want string
	}{
		{"nowhere", exitNotFound, `no skill called "nowhere" can be adopted: no record names it`},
		{"gone", exitNotFound, "this machine no longer holds it"},
		{"shared", exitRefused, "shared is already managed by agentx"},
		{"mine", exitRefused, "mine is already one of your own skills"},
		{"dup", exitRefused, "the library already holds a skill called dup, a skill of a shared source, so " + filepath.Join(claude, "dup") + " cannot be adopted under that name"},
		{"lost", exitRefused, "the account repo already holds lost as a skill of a shared source"},
		{"published", exitRefused, "your account remote already holds a skill called published"},
		{"differs", exitRefused, "differs is a directory with different content in several configurations: " + filepath.Join(claude, "differs") + " (claude-code), " + filepath.Join(cursor, "differs") + " (cursor)"},
		{"linked", exitRefused, "so there is no directory of linked to adopt"},
		{"liblink", exitRefused, "adopting liblink moves the library directory into its worktree"},
		{"cloned", exitRefused, "adopting it would move that repository into its worktree"},
		{"held", exitRefused, "worktrees/held already exists"},
		{"occupied", exitRefused, "the library already holds " + filepath.Join(h.library, "occupied")},
		{"locked", exitRefused, "cannot adopt " + filepath.Join(gemini, "locked") + ": access " + gemini + ": permission denied"},
		{"My_Skill", exitRefused, "My_Skill is not a valid skill name"},
		{"tidy", exitRefused, "tidy comes with the plugin formatter, whose directory agentx never writes"},
	} {
		if c.name == "locked" && os.Geteuid() == 0 {
			continue
		}
		out := h.run("--json", "adopt", "--skill", c.name)
		equal(t, c.name+": exit", out.exit, c.exit.exit)
		e := lastError(t, h.events(out.stdout))
		contains(t, c.name+": the error", e["message"].(string), c.want)
		if c.name == "dup" {
			equal(t, "dup: the hint", e["hint"], "rename the directory and adopt it under the new name, or replace it with 'agentx skill place dup --to claude-code --force', which discards what it holds")
		}
	}
	equal(t, "the refs", h.accountGit("for-each-ref"), refs)
	equal(t, "the worktrees", names(t, filepath.Join(h.agentx, "worktrees")), worktrees)
}

// TestAdoptTakesOverAnUnmanagedSkill adopts, in one home, three skills no
// record names or that are taken as they are. One the library holds
// becomes one of the user's own in its place: its content is the first
// commit of a branch of its own, by the user and with a new fork id, its
// directory moves into the worktree and the library holds a symlink to it.
// One three clients hold the same directory of comes out of the first
// client's skills directory: the library gets its symlink, the two clients
// that do not read the library get a symlink to the library where their
// directory was, and the one that reads it gets none. One the Vercel
// skills CLI recorded, taken with --as-is, keeps the edit an install from
// its source would replace, and that tool's file is left as it was. No run
// reports an adoption event: no record was adopted.
func TestAdoptTakesOverAnUnmanagedSkill(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.withIdentity("Ada Lovelace", "ada@example.com")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "notes"), "SKILL.md"), skill("notes", "Take notes"))
	text := h.mustRun("adopt", "--skill", "notes")
	// No account remote is set, so the adoption ends with how to publish it.
	equal(t, "the text output", text.stdout, "✓ adopted notes; it is now one of your own skills\n"+
		"No account remote is set: run 'agentx source add <url> --account', then 'agentx skill publish notes' to publish it.\n")
	tip := h.ref(lineage.ForkRef("notes"))
	equal(t, "the history", h.accountGit("rev-list", "--parents", lineage.ForkRef("notes")), tip)
	equal(t, "the author", h.accountGit("log", "-1", "--format=%an <%ae>", tip), "Ada Lovelace <ada@example.com>")
	equal(t, "the subject", h.accountGit("log", "-1", "--format=%s", tip), "Adopt notes")
	equal(t, "the tree", h.accountGit("ls-tree", "-r", "--name-only", tip), "notes/SKILL.md")
	if link, _ := os.Readlink(filepath.Join(h.library, "notes")); link != "../../../agentx/worktrees/notes/notes" {
		t.Errorf("the library symlink is %q", link)
	}
	ev := h.listed("notes")
	equal(t, "kind", ev["kind"], lineage.KindManaged)
	equal(t, "the fork id", ev["fork_id"], h.trailer(tip, lineage.TrailerForkID))

	clients := map[string]string{}
	for _, client := range []string{".claude", ".codex", ".cursor"} {
		clients[client] = filepath.Join(h.home, client, "skills", "jot")
		writeFile(t, mkdirs(t, clients[client], "SKILL.md"), skill("jot", "Jot things down"))
		writeFile(t, filepath.Join(clients[client], "pad.md"), "a pad\n")
	}
	out := h.run("--json", "adopt", "--skill", "jot")
	equal(t, "from the clients: exit", out.exit, 0)
	equal(t, "from the clients: adoption events", len(h.eventsOfType(out.stdout, "adoption")), 0)
	equal(t, "from the clients: kind", h.librarySkill(out.stdout, "jot")["kind"], lineage.KindManaged)
	equal(t, "from the clients: the summary", h.one(out.stdout, "result")["summary"],
		"adopted jot from "+clients[".claude"]+" in 3 configurations; always available to universal clients: codex, gemini-cli")
	equal(t, "from the clients: the tree", h.accountGit("ls-tree", "-r", "--name-only", h.ref(lineage.ForkRef("jot"))), "jot/SKILL.md\njot/pad.md")
	equal(t, "from the clients: the moved file", fileBody(t, filepath.Join(h.agentx, "worktrees", "jot", "jot", "pad.md")), "a pad\n")
	if link, _ := os.Readlink(filepath.Join(h.library, "jot")); link != "../../../agentx/worktrees/jot/jot" {
		t.Errorf("from the clients: the library symlink is %q", link)
	}
	for _, client := range []string{".claude", ".cursor"} {
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
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/beta/SKILL.md",
	}})
	out = h.run("--json", "adopt", "--skill", "beta", "--as-is")
	equal(t, "as it is: exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	equal(t, "as it is: adoption events", len(h.eventsOfType(out.stdout, "adoption")), 0)
	equal(t, "as it is: the summary", h.one(out.stdout, "result")["summary"], "adopted beta; it is now one of your own skills")
	equal(t, "as it is: the edit", fileBody(t, filepath.Join(h.agentx, "worktrees", "beta", "beta", "notes.md")), "an edit of mine\n")
	if _, err := h.accountGitErr("rev-parse", "--verify", "-q", lineage.ManagedRef("beta")); err == nil {
		t.Error("as it is: the skill was installed from its source")
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
		{"every skill and one", adoptSelection{all: true, names: []string{"alpha"}}, "--all and --skill"},
		{"a skill as it is", adoptSelection{names: []string{"alpha"}, asIs: true}, ""},
		{"as it is, with no skill", adoptSelection{asIs: true}, "--as-is needs --skill"},
		{"every skill as it is", adoptSelection{all: true, asIs: true}, "--as-is needs --skill"},
		{"as it is, of one tool", adoptSelection{tool: toolVercel, names: []string{"alpha"}, asIs: true}, "--as-is cannot be given with skills-cli"},
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

	out = h.run("--json", "adopt", "--as-is")
	equal(t, "exit", out.exit, exitUsage.exit)
	equal(t, "the hint", lastError(t, h.events(out.stdout))["hint"], "name the skill to take over as it is with --skill <name>")

	// The argument names a tool; a skill name there is pointed at --skill.
	out = h.run("--json", "adopt", "zoom;out")
	equal(t, "exit", out.exit, exitUsage.exit)
	contains(t, "the hint", lastError(t, h.events(out.stdout))["hint"].(string), "agentx adopt --skill "+shellWord("zoom;out"))
}
