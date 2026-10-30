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
// before any path is built from it, one that names no source agentx can
// fetch is refused, and the file is the same afterwards. What the library
// holds under the name does not decide: a symlink or a directory without a
// SKILL.md is what that tool left there, and adopting replaces it.
func TestAdoptJudgesEveryUntrustedEntry(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	entry := func(subpath string) lockEntry {
		return lockEntry{Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: subpath}
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
		{"linked", adoptCandidate, ""},
		{"notaskill", adoptCandidate, ""},
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
	h, s, _, _ := adoptHarness(t)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "../../../etc/passwd",
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
	h, s, _, _ := adoptHarness(t)
	h.rewrite(s, "https://github.com/owner/repo")
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: "https://carol:s3cr3t@github.com/owner/repo",
		SkillPath: "skills/alpha",
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
// not be told that it holds nothing. vercel.TestReadFileRefusesWhatIsNotALockFile
// has every shape of one, and skillshare.TestReadRefusesWhatIsNotAStore the
// same for Skillshare's store, which answers through the same path.
func TestAdoptRefusesALockFileItCannotRead(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const content = `{"version": 3, "skills": {`
	if err := os.MkdirAll(filepath.Dir(h.lockPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, h.lockPath(), content)
	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitInternal.exit)
	h.lockUnchanged(h.lockPath(), []byte(content))
	contains(t, "the error", lastError(t, h.events(out.stdout))["message"].(string), h.lockPath())
}

// TestAdoptReadsBothLocations: with XDG_STATE_HOME set, the lock file that
// tool writes is the XDG one, and its entry is the one used for a skill
// both files name.
func TestAdoptReadsBothLocations(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	state := filepath.Join(h.home, "state")
	h.env["XDG_STATE_HOME"] = state
	xdg := filepath.Join(state, "skills", ".skill-lock.json")
	stale := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/gone", SourceType: "github", SourceURL: "https://github.com/owner/gone", SkillPath: "skills/alpha",
	}})
	live := h.writeLock(xdg, map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha",
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), stale)
	h.lockUnchanged(xdg, live)
	ev := h.one(out.stdout, "adoption")
	equal(t, "the lock file used", ev["file"], xdg)
	equal(t, "the source", ev["source"], s.url)
	contains(t, "stderr", out.stderr, "with different coordinates")
}

// TestAdoptWithNoRecords says so and exits 0: a machine that never used
// another tool has nothing to adopt, which is not a failure.
func TestAdoptWithNoRecords(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	out := h.run("adopt")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "Nothing to adopt")
	out = h.run("adopt", "--all")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "Already adopted.")
}

// TestAdoptRefusesAFork: a name the account repo holds as a fork has a
// history of its own, and an import branch of the same name would take its
// place.
func TestAdoptRefusesAFork(t *testing.T) {
	t.Parallel()
	h, s, v1, _ := adoptHarness(t)
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	h.accountGit("update-ref", "refs/heads/skills/alpha", v1)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha",
	}})

	out := h.run("--json", "adopt", "--all")
	equal(t, "exit", out.exit, exitRefused.exit)
	h.lockUnchanged(h.lockPath(), lock)
	contains(t, "the reason", h.one(out.stdout, "adoption")["reason"].(string), "is a skill of the account remote on this machine")
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("a fork was given an import branch")
	}
}

// TestAdoptOneSkillOfSeveral covers a run that adopts what it can and says
// what it could not: the skill that landed is managed, the one whose
// directory the source no longer holds is left unmanaged, and the run
// answers for it. The run reports one adoption event per skill in name
// order, whichever order the flags named them in.
func TestAdoptOneSkillOfSeveral(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	s.skill("skills/beta", "beta", "The second skill", nil)
	s.commit("a second skill")
	vercelInstall(t, h, s, "skills/beta", "beta")
	h.writeLock(h.lockPath(), map[string]lockEntry{
		"alpha": {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/alpha"},
		"beta":  {Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "skills/moved/beta"},
	})

	out := h.run("--json", "adopt", "--skill", "beta", "--skill", "alpha")
	equal(t, "exit", out.exit, exitNotFound.exit)
	contains(t, "stderr", out.stderr, "beta: "+s.url+" holds no skill at skills/moved/beta now")
	var events []string
	for _, ev := range h.eventsOfType(out.stdout, "adoption") {
		events = append(events, ev["name"].(string)+" "+ev["state"].(string))
	}
	equal(t, "the adoption events", strings.Join(events, ", "), "alpha "+adoptAdopted+", beta "+adoptRefused)
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/beta"); err == nil {
		t.Error("beta was adopted although the source holds no skill at its subpath")
	}
	if h.accountGit("rev-parse", "--verify", "refs/heads/managed/alpha") == "" {
		t.Error("alpha was not adopted although the source holds it")
	}
	list := h.run("--json", "skill", "list")
	kinds := map[string]any{}
	for _, ev := range h.eventsOfType(list.stdout, "library_skill") {
		kinds[ev["name"].(string)] = ev["kind"]
	}
	equal(t, "alpha", kinds["alpha"], "managed")
	equal(t, "beta", kinds["beta"], "unmanaged")
}
