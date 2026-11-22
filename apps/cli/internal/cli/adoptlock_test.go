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
// before any path is built from it, and the file is the same afterwards.
// What the library holds under the name does not decide for an entry that
// names a source: a symlink or a directory without a SKILL.md is what that
// tool left there, and adopting replaces it. An entry that names no source
// agentx can install from is adopted as the folder the library holds, and
// refused, in the preview and in a run, when the library holds a symlink.
// The preview lists the unmanaged skills no entry names with them, a
// folder as ready and a symlink as one it cannot adopt, and reports no
// adoption of either.
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
		"aliased":    wellKnown,
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

	// A symlink no entry names, to a skill outside the library.
	writeFile(t, mkdirs(t, filepath.Join(h.home, "work", "shortcut"), "SKILL.md"), skill("shortcut", "A skill kept elsewhere"))
	for link, target := range map[string]string{"aliased": filepath.Join(h.library, "wellknown"), "shortcut": filepath.Join(h.home, "work", "shortcut")} {
		if err := os.Symlink(target, filepath.Join(h.library, link)); err != nil {
			t.Fatal(err)
		}
	}

	out := h.run("--json", "skill", "import")
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
		{"escaping", adoptCandidate, ""},
		{"newline", adoptCandidate, ""},
		{"wellknown", adoptCandidate, ""},
		{"aliased", adoptRefused, "a symlink to " + filepath.Join(h.library, "wellknown") + ". To import it, move that folder to " + filepath.Join(h.library, "aliased")},
	} {
		if states[c.name] != c.state {
			t.Errorf("%s is %q, want %q", c.name, states[c.name], c.state)
		}
		if c.reason != "" && !strings.Contains(reasons[c.name], c.reason) {
			t.Errorf("%s: reason %q, want one about %q", c.name, reasons[c.name], c.reason)
		}
	}
	// An entry the reader can make nothing of, one whose directory is not
	// there, and a skill no entry names are no candidates at all.
	for _, name := range []string{"../escape", "gone", "sourceless", "shortcut"} {
		if _, reported := states[name]; reported {
			t.Errorf("%s was reported as a candidate", name)
		}
	}
	for _, want := range []string{"../escape", "sourceless"} {
		contains(t, "stderr", out.stderr, want)
	}

	// Without --json the skills are grouped by what adopting would do.
	human := h.run("skill", "import")
	equal(t, "exit", human.exit, 0)
	aliased := "a symlink to " + filepath.Join(h.library, "wellknown") + ". To import it, move that folder to " + filepath.Join(h.library, "aliased")
	equal(t, "the preview", human.stdout, "Found 9 unmanaged skills.\n\n"+
		"Ready to import (7)\n  alpha, escaping, linked, newline, notaskill, sourceless, wellknown\n\n"+
		"Cannot import (2)\n"+
		"  aliased   "+aliased+"\n"+
		"  shortcut  a symlink to "+filepath.Join(h.home, "work", "shortcut")+". To import it, move that folder to "+filepath.Join(h.library, "shortcut")+"\n\n"+
		"To import:\n"+
		"  agentx skill import --all   all 7 skills\n"+
		"  agentx skill import <name>  one skill\n")
	contains(t, "the summary", h.one(out.stdout, "result")["summary"].(string), "9 unmanaged skills, 7 to import, 2 that cannot be imported")
	// A run asked for the one it cannot adopt says the same, and what to do.
	refused := h.run("--json", "skill", "import", "aliased")
	equal(t, "a symlink: exit", refused.exit, exitRefused.exit)
	e := lastError(t, h.events(refused.stdout))
	equal(t, "a symlink: the error", e["message"], "aliased is a symlink to "+filepath.Join(h.library, "wellknown")+", and agentx imports folders only")
	equal(t, "a symlink: the hint", e["hint"], "move "+filepath.Join(h.library, "wellknown")+" to "+filepath.Join(h.library, "aliased")+", then run 'agentx skill import aliased' again")
	equal(t, "a symlink: the adoption", h.one(refused.stdout, "adoption")["reason"], aliased)
	// Naming the other tool reads its records alone, and it recorded none.
	other := h.run("skill", "import", "--from", "skillshare")
	equal(t, "exit", other.exit, 0)
	contains(t, "stdout", other.stdout, "Nothing to import: no skill installed by Skillshare is on this machine.")
}

// TestAdoptAllAdoptsFoldersAsTheyAre adopts every unmanaged skill of a
// machine whose folders have no source to install them from. One the lock
// file names with a subpath that leaves the repository, as one an import
// commit cannot record: the subpath never reaches git, so no run can be
// talked into reading outside the source, and the run reports the record
// as adopted, with no import branch. One no tool recorded is adopted, with
// no adoption event. Two more cannot be adopted, as the preview says of
// them: a symlink, and a folder that is a Git repository of its own. The
// run lists them after the skills it adopted, and says once that those are
// not published yet. Run again, with nothing left to adopt, it lists the
// two, each a warning, counts them in the result, and succeeds.
func TestAdoptAllAdoptsFoldersAsTheyAre(t *testing.T) {
	t.Parallel()
	h, s, _, _ := adoptHarness(t)
	lock := h.writeLock(h.lockPath(), map[string]lockEntry{"alpha": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url, SkillPath: "../../../etc/passwd",
	}})
	for _, name := range []string{"cloned", "notes"} {
		writeFile(t, mkdirs(t, filepath.Join(h.library, name), "SKILL.md"), skill(name, "No tool installed it"))
	}
	git := filepath.Join(h.library, "cloned", ".git")
	writeFile(t, mkdirs(t, git, "HEAD"), "ref: refs/heads/main\n")
	work := filepath.Join(h.home, "work", "shortcut")
	writeFile(t, mkdirs(t, work, "SKILL.md"), skill("shortcut", "A skill kept elsewhere"))
	if err := os.Symlink(work, filepath.Join(h.library, "shortcut")); err != nil {
		t.Fatal(err)
	}
	cloned := "a Git repository of its own. To import it, move " + git + " out of the folder"
	shortcut := "a symlink to " + work + ". To import it, move that folder to " + filepath.Join(h.library, "shortcut")

	out := h.run("skill", "import", "--all")
	equal(t, "exit", out.exit, 0)
	h.lockUnchanged(h.lockPath(), lock)
	equal(t, "stdout", out.stdout, "✓ imported alpha\n"+
		"✓ imported notes\n"+
		"Imported skills stay on this machine until you publish them: run 'agentx source add <url> --account', then 'agentx skill publish <name>'.\n\n"+
		"Could not import (2)\n"+
		"  cloned    "+cloned+"\n"+
		"  shortcut  "+shortcut+"\n\n"+
		"Imported 2 skills; 2 could not be imported.\n")
	equal(t, "the notes", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "alpha notes\n")
	if _, err := h.accountGitErr("rev-parse", "--verify", "-q", "refs/heads/managed/alpha"); err == nil {
		t.Error("alpha has an import branch")
	}

	again := h.run("--json", "skill", "import", "--all")
	equal(t, "again: exit", again.exit, 0)
	equal(t, "again: the events", strings.Join(h.types(h.events(again.stdout)), ", "), "adoption, result")
	adoption := h.one(again.stdout, "adoption")
	equal(t, "again: the adoption", adoption["name"].(string)+" "+adoption["state"].(string), "alpha "+adoptManaged)
	equal(t, "again: the summary", h.one(again.stdout, "result")["summary"], "no skills imported; 2 could not be imported")
	for _, warning := range []string{"cloned could not be imported: " + cloned, "shortcut could not be imported: " + shortcut} {
		contains(t, "again: stderr", again.stderr, warning)
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

	out := h.run("--json", "skill", "import", "--all")
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
	out := h.run("--json", "skill", "import", "--all")
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

	out := h.run("--json", "skill", "import", "--all")
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
	out := h.run("skill", "import")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "Nothing to import: this machine has no unmanaged skill.\n")
	out = h.run("skill", "import", "--all")
	equal(t, "exit", out.exit, 0)
	contains(t, "stdout", out.stdout, "Already imported.")
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

	out := h.run("--json", "skill", "import", "beta", "alpha")
	equal(t, "exit", out.exit, exitNotFound.exit)
	contains(t, "stderr", out.stderr, "beta: "+s.url+" holds no skill at skills/moved/beta now")
	var events []string
	for _, ev := range h.eventsOfType(out.stdout, "adoption") {
		events = append(events, ev["name"].(string)+" "+ev["state"].(string))
	}
	equal(t, "the adoption events", strings.Join(events, ", "), "alpha "+adoptAdopted+", beta "+adoptRefused)
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/beta"); err == nil {
		t.Error("beta was imported although the source holds no skill at its subpath")
	}
	if h.accountGit("rev-parse", "--verify", "refs/heads/managed/alpha") == "" {
		t.Error("alpha was not imported although the source holds it")
	}
	list := h.run("--json", "skill", "list")
	kinds := map[string]any{}
	for _, ev := range h.eventsOfType(list.stdout, "library_skill") {
		kinds[ev["name"].(string)] = ev["kind"]
	}
	equal(t, "alpha", kinds["alpha"], "managed")
	equal(t, "beta", kinds["beta"], "unmanaged")
	// Adopting every skill fails the same way: the source no longer holds
	// the skill, which is a failure and no skill that cannot be adopted.
	equal(t, "every skill: exit", h.run("skill", "import", "--all").exit, exitNotFound.exit)
}
