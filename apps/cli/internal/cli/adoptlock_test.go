package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdoptJudgesEveryUntrustedEntry runs one lock file holding every shape
// a third party's file, or a user's hand, can put in it. The file is read,
// never trusted: an entry that cannot name a library directory is left out
// before any path is built from it, one whose directory is not a skill of
// the library is refused, and the file is the same afterwards.
func TestAdoptJudgesEveryUntrustedEntry(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	entry := func(subpath string) lockEntry {
		return lockEntry{Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: subpath, SkillFolderHash: installed}
	}
	// A symlink in the library, a directory that is no skill, and a name the
	// library could never hold as a directory.
	if err := os.Symlink(filepath.Join(h.library, "alpha"), filepath.Join(h.library, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.library, "notaskill"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.library, "notaskill", "README.md"), "no frontmatter here\n")

	escaping := entry("../../etc/passwd")
	newline := entry("skills/al\npha")
	wellKnown := entry("skills/alpha")
	wellKnown.SourceType = "well-known"
	sourceless := entry("skills/alpha")
	sourceless.Source, sourceless.SourceURL = "", ""
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha":      entry("skills/alpha"),
		"linked":     entry("skills/alpha"),
		"notaskill":  entry("skills/alpha"),
		"escaping":   escaping,
		"newline":    newline,
		"wellknown":  wellKnown,
		"sourceless": sourceless,
		"../escape":  entry("skills/alpha"),
		"gone":       entry("skills/gone"),
	})
	// Only the entries the library holds under a name it can hold become
	// candidates, and each is judged on its own.
	for name, description := range map[string]string{
		"escaping":   "A skill whose entry escapes the repository",
		"newline":    "A skill whose entry hides a newline in its path",
		"wellknown":  "A skill from somewhere agentx cannot fetch",
		"sourceless": "A skill whose entry names no source",
	} {
		if err := os.MkdirAll(filepath.Join(h.library, name), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(h.library, name, "SKILL.md"), skill(name, description))
	}

	out := h.run("--json", "adopt")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)

	states := map[string]string{}
	reasons := map[string]string{}
	for _, ev := range h.eventsOfType(out.stdout, "adoption") {
		states[ev["name"].(string)] = ev["state"].(string)
		if reason, ok := ev["reason"].(string); ok {
			reasons[ev["name"].(string)] = reason
		}
	}
	for _, c := range []struct{ name, state, reason string }{
		{"alpha", adoptCandidate, ""},
		{"linked", adoptRefused, "is not a directory the library holds"},
		{"notaskill", adoptRefused, "holds no SKILL.md"},
		{"escaping", adoptRefused, "which is not a directory of"},
		{"newline", adoptRefused, "which is not a directory of"},
		{"wellknown", adoptRefused, "not a git repository agentx can fetch"},
	} {
		if states[c.name] != c.state {
			t.Errorf("%s is %q, want %q", c.name, states[c.name], c.state)
		}
		if c.reason != "" && !strings.Contains(reasons[c.name], c.reason) {
			t.Errorf("%s: reason %q, want one about %q", c.name, reasons[c.name], c.reason)
		}
	}
	// An entry the reader can make nothing of, and one whose directory is not
	// there, are no candidates at all.
	for _, name := range []string{"../escape", "gone", "sourceless"} {
		if _, reported := states[name]; reported {
			t.Errorf("%s was reported as a candidate", name)
		}
	}
	for _, want := range []string{"../escape", "sourceless"} {
		contains(t, "stderr", out.stderr, want)
	}
}

// TestAdoptRefusesAnEscapingSubpathBeforeItReadsAnything is the same
// refusal on the adopting path: a subpath that leaves the repository never
// reaches git, so no run can be talked into reading outside the source.
func TestAdoptRefusesAnEscapingSubpathBeforeItReadsAnything(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "../../../etc/passwd", SkillFolderHash: installed,
	}})
	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	h.lockUnchanged(h.lockPath(), lock)
	if _, err := os.Stat(filepath.Join(h.agentx, "account.git")); err == nil {
		t.Error("a refused entry made the run create an account repo")
	}
}

// TestAdoptDropsACredentialInTheURL: a token someone put in the lock file's
// URL never reaches the settings, an event, the account repo or a line of
// output, exactly as it does not for 'agentx source add'.
func TestAdoptDropsACredentialInTheURL(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	h.rewrite(s, "https://github.com/owner/repo")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: "https://carol:s3cr3t@github.com/owner/repo",
		SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	equal(t, "the source of the adoption", h.one(out.stdout, "adoption")["source"], "https://github.com/owner/repo")
	settings, err := os.ReadFile(filepath.Join(h.agentx, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	for what, text := range map[string]string{
		"stdout":            out.stdout,
		"stderr":            out.stderr,
		"the settings":      string(settings),
		"the import commit": h.accountGit("cat-file", "commit", "refs/heads/managed/alpha"),
		"the git config":    h.accountGit("config", "--list"),
	} {
		if strings.Contains(text, "s3cr3t") || strings.Contains(text, "carol") {
			t.Errorf("%s carries the credential from the lock file:\n%s", what, text)
		}
	}
	contains(t, "stderr", out.stderr, "was dropped")
}

// TestAdoptRefusesALockFileItCannotRead: a file that is not a lock file is
// not an empty lock file, since a user asking to adopt what it holds may
// not be told that it holds nothing.
func TestAdoptRefusesALockFileItCannotRead(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, content string }{
		{"truncated JSON", `{"version": 3, "skills": {`},
		{"no skills map", `{"version": 3}`},
		{"a null skills map", `{"version": 3, "skills": null}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, _, _, _ := adoptHarness(t)
			if err := os.MkdirAll(filepath.Dir(h.lockPath()), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, h.lockPath(), c.content)
			out := h.run("--json", "adopt", "--all")
			equal(t, "exit", out.exit, exitInternal.exit)
			h.lockUnchanged(h.lockPath(), []byte(c.content))
			contains(t, "the error", lastError(t, h.events(out.stdout))["message"].(string), h.lockPath())
		})
	}
}

// TestAdoptReadsBothLocations: with XDG_STATE_HOME set, the lock file that
// tool writes is the XDG one, and its entry is the one used for a skill
// both files name.
func TestAdoptReadsBothLocations(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	state := filepath.Join(h.home, "state")
	h.env["XDG_STATE_HOME"] = state
	xdg := filepath.Join(state, "skills", ".skill-lock.json")
	stale := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/gone", SourceType: "github", SourceURL: "https://github.com/owner/gone",
		SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})
	live := h.writeLock(xdg, map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), stale)
	h.lockUnchanged(xdg, live)
	ev := h.one(out.stdout, "adoption")
	equal(t, "the lock file used", ev["lock"], xdg)
	equal(t, "the source", ev["source"], s.url)
	contains(t, "stderr", out.stderr, "with different coordinates")
}

// TestAdoptWithNoLockFile says so and exits 0: a machine that never used
// that tool has nothing to adopt, which is not a failure.
func TestAdoptWithNoLockFile(t *testing.T) {
	t.Parallel()
	h, _, _, _ := adoptHarness(t)
	out := h.run("adopt")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "No skill of the vercel skills lock file is in the library")
	out = h.run("adopt", "--all")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "Nothing left to adopt.")
}

// TestAdoptRefusesAFork: a name the account repo holds as a fork has a
// history of its own, and an import branch of the same name would take its
// place.
func TestAdoptRefusesAFork(t *testing.T) {
	t.Parallel()
	h, s, v1, installed := adoptHarness(t)
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	h.accountGit("update-ref", "refs/heads/skills/alpha", v1)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha", SkillFolderHash: installed,
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	h.lockUnchanged(h.lockPath(), lock)
	contains(t, "the reason", h.one(out.stdout, "adoption")["reason"].(string), "is a fork on this machine")
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("a fork was given an import branch")
	}
}

// TestAdoptOneSkillOfSeveral covers a run that adopts what it can and says
// what it could not: the skills that landed are managed and the one that
// could not be established is unmanaged, with the run answering for it.
func TestAdoptOneSkillOfSeveral(t *testing.T) {
	t.Parallel()
	h, s, _, installed := adoptHarness(t)
	s.skill("skills/beta", "beta", "The second skill", nil)
	s.commit("a second skill")
	vercelInstall(t, h, s, "skills/beta", "beta")
	editLibrary(t, h, "beta", "SKILL.md", skill("beta", "edited by hand"))
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha", SkillFolderHash: installed},
		"beta":  {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/beta", SkillFolderHash: ""},
	})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	contains(t, "stderr", out.stderr, "beta: the version beta was installed at cannot be established")
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/beta"); err == nil {
		t.Error("beta was adopted although its version could not be established")
	}
	if h.accountGit("rev-parse", "--verify", "refs/heads/managed/alpha") == "" {
		t.Error("alpha was not adopted although its version was established")
	}
	list := h.run("--json", "skill", "list")
	kinds := map[string]any{}
	for _, ev := range h.eventsOfType(list.stdout, "library_skill") {
		kinds[ev["name"].(string)] = ev["kind"]
	}
	equal(t, "alpha", kinds["alpha"], "managed")
	equal(t, "beta", kinds["beta"], "unmanaged")
}

// TestAdoptRefusesAFolderHashOfAnotherSource: the lock file is a hint and
// never an authority. Its folder hash is looked up in the source the entry
// names and nowhere else, so a tree id that another repository put in the
// account repo establishes nothing here, however readable that object is.
func TestAdoptRefusesAFolderHashOfAnotherSource(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	editLibrary(t, h, "alpha", "notes.md", "an edit of mine\n")
	other := h.newSourceRepo("other", true)
	other.skill("skills/alpha", "alpha", "Another repository's skill of the same name", nil)
	other.commit("elsewhere")
	if out := h.run("source", "add", other.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	elsewhere := other.tree("skills/alpha")
	h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/alpha", SkillFolderHash: elsewhere,
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("a tree of another source was accepted as this skill's upstream version")
	}
	contains(t, "the reason", h.one(out.stdout, "adoption")["reason"].(string), "cannot be established")
}
