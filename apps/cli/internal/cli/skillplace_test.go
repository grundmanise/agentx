package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestSkillAddToOverridesTheTargets places into the configurations --to
// names and no others, a disabled one included, since naming a
// configuration is asking for it. A --to naming no detected configuration
// is refused, and names the ones there are.
func TestSkillAddToOverridesTheTargets(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	unknown := h.run("skill", "add", s.url, "--name", "alpha", "--to", "nowhere")
	equal(t, "exit of an unknown configuration", unknown.exit, 5)
	contains(t, "stderr of an unknown configuration", unknown.stderr, "detected configurations:")
	equal(t, "exit", h.run("config", "disable", "cursor").exit, 0)

	out := h.run("--json", "skill", "add", s.url, "--name", "alpha", "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	if _, err := os.Lstat(filepath.Join(h.home, ".cursor", "skills", "alpha")); err != nil {
		t.Errorf("the configuration --to named has no placement: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(h.home, ".claude", "skills", "alpha")); err == nil {
		t.Error("a configuration --to did not name was placed into")
	}
	for _, p := range placementsOf(t, h.one(out.stdout, "library_skill")) {
		if !strings.Contains(p, "cursor") {
			t.Errorf("placement %q, want the --to configuration alone", p)
		}
	}
}

// TestSkillAddSkipsDisabledConfigurations leaves a disabled configuration
// out of the default targets.
func TestSkillAddSkipsDisabledConfigurations(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("config", "disable", "claude-code").exit, 0)
	equal(t, "exit", h.run("skill", "add", s.url, "--name", "alpha").exit, 0)
	if _, err := os.Lstat(filepath.Join(h.home, ".claude", "skills", "alpha")); err == nil {
		t.Error("a disabled configuration was placed into")
	}
	if _, err := os.Lstat(filepath.Join(h.home, ".cursor", "skills", "alpha")); err != nil {
		t.Errorf("an enabled configuration has no placement: %v", err)
	}
}

// TestSkillAddCopyPlacesCopies makes copies instead of symlinks and records
// the copy mode of the whole install in one settings write.
func TestSkillAddCopyPlacesCopies(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	before := journalCount(t, h)
	out := h.run("--json", "skill", "add", s.url, "--name", "alpha", "--copy")
	equal(t, "exit", out.exit, 0)

	place := filepath.Join(h.home, ".claude", "skills", "alpha")
	info, err := os.Lstat(place)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the placement is not a copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(place, "notes.md")); err != nil {
		t.Errorf("the copy is missing a file: %v", err)
	}
	// One settings write for the whole install, naming every configuration
	// that holds a copy.
	var settings struct {
		CopyMode map[string][]string `json:"copy_mode"`
	}
	b, err := os.ReadFile(filepath.Join(h.agentx, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(settings.CopyMode["alpha"], ",")
	equal(t, "copy_mode", got, "claude-code,cursor")
	equal(t, "journals left behind", journalCount(t, h), before)
	for _, p := range placementsOf(t, h.one(out.stdout, "library_skill")) {
		if strings.HasPrefix(p, "claude-code") && !strings.Contains(p, "copy copy") {
			t.Errorf("placement %q, want it reported as a copy", p)
		}
	}
	contains(t, "summary", out.stdout, "as copy")
}

// TestSkillAddAdoptsAPlacementOfTheSameVersion replaces a real directory
// that already holds exactly this version with a symlink and says so, and
// leaves anything else alone with a warning.
func TestSkillAddAdoptsAPlacementOfTheSameVersion(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	// A directory holding the version already, as a skill installed by hand
	// would be: copy it out of the source's work tree, which holds the
	// version the install takes.
	same := filepath.Join(h.home, ".claude", "skills", "alpha")
	copyTree(t, filepath.Join(s.work, "skills", "alpha"), same)
	// And a directory holding something else.
	other2 := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(other2, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(other2, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")

	out := h.run("--json", "skill", "add", s.url, "--name", "alpha")
	equal(t, "exit", out.exit, 0)
	if info, err := os.Lstat(same); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the directory of this version was not adopted as a symlink: %v", err)
	}
	if info, err := os.Lstat(other2); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("a directory that is not this version was replaced: %v", err)
	}
	contains(t, "stderr", out.stderr, other2)
	contains(t, "the result", out.stdout, "1 placement adopted")
	contains(t, "the result", out.stdout, "1 placement skipped")
}

// TestSkillAddKeepsAPlacementThatIsAlreadyRight leaves a symlink that
// already names the library alone, whether or not it resolved before.
func TestSkillAddKeepsAPlacementThatIsAlreadyRight(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	place := filepath.Join(h.home, ".claude", "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(place), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h.library, "alpha"), place); err != nil { // dangling until the install
		t.Fatal(err)
	}
	out := h.run("--json", "skill", "add", s.url, "--name", "alpha")
	equal(t, "exit", out.exit, 0)
	target, err := os.Readlink(place)
	if err != nil || target != filepath.Join(h.library, "alpha") {
		t.Errorf("the placement is %q: %v", target, err)
	}
	if strings.Contains(out.stderr, "left as it is") {
		t.Errorf("a placement that was already right was reported as skipped:\n%s", out.stderr)
	}
}

// TestSkillAddAdoptsTheLibraryDirectory writes the import branch without
// touching a library directory that already holds exactly this version, and
// refuses one that holds something else.
func TestSkillAddAdoptsTheLibraryDirectory(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--name", "alpha").exit, 0)
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")

	// The same version again: the branch stays where it is and nothing is
	// written over the library.
	out := h.run("--json", "skill", "add", s.url, "--name", "alpha")
	equal(t, "exit", out.exit, 0)
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)
	contains(t, "the result", out.stdout, "adopted alpha")

	// A library directory holding something else is a refusal, not an
	// overwrite: the user's content is never replaced by an install.
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "edited by hand\n")
	mine := h.run("skill", "add", s.url, "--name", "alpha")
	equal(t, "exit", mine.exit, 6)
	contains(t, "stderr", mine.stderr, "the library already holds alpha")
	body, err := os.ReadFile(filepath.Join(h.library, "alpha", "notes.md"))
	if err != nil || string(body) != "edited by hand\n" {
		t.Errorf("the refused install changed the library: %q %v", body, err)
	}
}

// TestSkillAddTreatsADanglingWorktreeLinkAsAbsent replaces a library entry
// that is a symlink into the agentx worktrees directory with nothing behind
// it, which is what a fork whose worktree is gone leaves.
func TestSkillAddTreatsADanglingWorktreeLinkAsAbsent(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	if err := os.MkdirAll(h.library, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(h.agentx, "worktrees", "alpha")
	if err := os.Symlink(gone, filepath.Join(h.library, "alpha")); err != nil {
		t.Fatal(err)
	}
	equal(t, "exit", h.run("skill", "add", s.url, "--name", "alpha").exit, 0)
	info, err := os.Lstat(filepath.Join(h.library, "alpha"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("the library entry is not the installed directory: %v", err)
	}
}

// TestSkillAddRefusesASecondVersion refuses to install another version over
// a managed skill: that is an update, which this command does not do. The
// library directory is gone, so the listing's warning offers the install
// again, which cannot lay out the version the branch names once the source
// moved past it: the refusal names the removal that stops managing that
// version, and following it installs the version the source holds now.
func TestSkillAddRefusesASecondVersion(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--name", "alpha").exit, 0)
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")

	// Move the source on and take the library directory out of the way, so
	// that the branch is the only thing in the way.
	s.skill("skills/alpha", "alpha", "The first skill, moved on", map[string]string{"notes.md": "newer\n"})
	s.commit("a newer version")
	equal(t, "exit", h.run("source", "fetch", s.url).exit, 0)
	if err := os.RemoveAll(filepath.Join(h.library, "alpha")); err != nil {
		t.Fatal(err)
	}
	contains(t, "the listing's warning", h.mustRun("skill", "list").stderr,
		"run 'agentx skill add "+shellWord(s.url)+" --name alpha' to install it again, or 'agentx skill remove alpha' to stop managing it")
	out := h.run("--json", "skill", "add", s.url, "--name", "alpha")
	equal(t, "exit", out.exit, 6)
	e := h.one(out.stdout, "error")
	equal(t, "message", e["message"], "alpha is already managed at another version, which the library no longer holds")
	equal(t, "hint", e["hint"], "run 'agentx skill remove alpha' to stop managing that version, then install again to get the version the source holds now")
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)

	h.mustRun("skill", "remove", "alpha")
	h.mustRun("skill", "add", s.url, "--name", "alpha")
	equal(t, "notes.md", fileBody(t, filepath.Join(h.library, "alpha", "notes.md")), "newer\n")
	if h.accountGit("rev-parse", "refs/heads/managed/alpha") == head {
		t.Error("the import branch still names the version the library no longer held")
	}
	listed := h.mustRun("--json", "skill", "list")
	equal(t, "state", h.librarySkill(listed.stdout, "alpha")["state"], stateCurrent)
	equal(t, "warnings", strings.Join(warnings(h, listed.stderr), "\n"), "")
}

// TestSkillAddNamesTheSkillToInstall refuses a source that holds more than
// one skill without --name, and a --name that names none.
func TestSkillAddNamesTheSkillToInstall(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	many := h.run("skill", "add", s.url)
	equal(t, "exit", many.exit, 1)
	contains(t, "stderr", many.stderr, "name one with --name, or take them all with --all: alpha, beta")

	none := h.run("skill", "add", s.url, "--name", "gamma")
	equal(t, "exit", none.exit, 5)
	contains(t, "stderr", none.stderr, "has no skill called \"gamma\"")

	// A path that holds exactly one skill needs no --name.
	equal(t, "exit", h.run("skill", "add", s.url+"#main").exit, 1) // still the whole repository
	one := h.run("skill", "add", strings.TrimSuffix(s.url, ".git")+".git/skills/beta")
	equal(t, "exit", one.exit, 0)
	if _, err := os.Stat(filepath.Join(h.library, "beta", "SKILL.md")); err != nil {
		t.Errorf("the one skill under the path was not installed: %v", err)
	}
}

// TestSkillAddAddsASourceThatIsNotAdded installs from a URL this machine has
// no source for. The run adds it exactly as source add would, and keeps it
// when the install that follows fails. That it fetches no more than a
// source add and an install would is counted in
// TestSkillAddAllAddsASourceThatIsNotAdded.
func TestSkillAddAddsASourceThatIsNotAdded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s, _, head := h.standardSource(true)

	out := h.run("--json", "skill", "add", s.url, "--name", "alpha")
	equal(t, "exit", out.exit, 0)
	types := h.types(h.events(out.stdout))
	want := []string{"source", "progress", "progress", "progress", "progress", "library_skill", "result"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Errorf("events = %v, want %v\n%s", types, want, out.stdout)
	}
	ev := h.one(out.stdout, "source")
	equal(t, "the source commit", ev["commit"], head)
	fetched, _ := ev["last_fetched"].(string)
	contains(t, "summary", h.one(out.stdout, "result")["summary"].(string), ", source fetched "+fetched)
	list := h.run("--json", "source", "list")
	equal(t, "the source entry", h.one(list.stdout, "source")["url"], s.url)
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("the skill was not installed: %v", err)
	}

	// An install that fails after the add keeps the source it added.
	third := newHarness(t)
	third.build(t, fixture{dirs: []string{".claude"}})
	none := third.run("skill", "add", s.url, "--name", "gamma")
	equal(t, "exit", none.exit, 5)
	contains(t, "stdout", none.stdout, "added "+s.url)
	equal(t, "the source kept", third.one(third.run("--json", "source", "list").stdout, "source")["url"], s.url)
}

// TestSkillAddResolvesAnAddedSource covers what skill add does not add: an
// id names no URL to fetch, and a known source named with another pin is
// the pin change source add is for.
func TestSkillAddResolvesAnAddedSource(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	id := h.run("skill", "add", "0123456789abcdef")
	equal(t, "exit of an unknown id", id.exit, 5)
	contains(t, "stderr", id.stderr, "agentx source list")

	pin := h.run("skill", "add", s.url+"#v1", "--name", "alpha")
	equal(t, "exit of another pin", pin.exit, 1)
	contains(t, "stderr", pin.stderr, `is pinned to "", not "v1"`)
	contains(t, "stderr", pin.stderr, "agentx source add "+s.url+"#v1")
}

// TestSkillAddFetchesOnlyWhenAsked installs from an added source as it was
// last fetched, and fetches it again first with --fetch.
func TestSkillAddFetchesOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	ref := "refs/agentx/sources/" + source.ID(s.url)
	fetchedAt := h.accountGit("rev-parse", ref)
	s.skill("skills/beta", "beta", "The second skill, revised", nil)
	moved := s.commit("beta moves on")

	// Without --fetch: the version the last fetch brought, and no fetch.
	out := h.run("--json", "skill", "add", s.url, "--name", "beta")
	equal(t, "exit", out.exit, 0)
	equal(t, "the source ref", h.accountGit("rev-parse", ref), fetchedAt)
	equal(t, "the source commit", h.one(out.stdout, "source")["commit"], fetchedAt)
	if strings.Contains(h.accountGit("cat-file", "commit", "refs/heads/managed/beta"), moved) {
		t.Error("an install without --fetch read a commit the last fetch did not bring")
	}

	// With --fetch: the source is fetched again at its pin first, and the
	// install reads what that fetch brought.
	out = h.run("--json", "skill", "add", s.url, "--name", "alpha", "--fetch")
	equal(t, "exit", out.exit, 0)
	equal(t, "the source ref", h.accountGit("rev-parse", ref), moved)
	ev := h.one(out.stdout, "source")
	equal(t, "commit", ev["commit"], moved)
	equal(t, "previous_commit", ev["previous_commit"], fetchedAt)
	text := h.run("skill", "add", s.url, "--name", "alpha", "--fetch")
	equal(t, "exit", text.exit, 0)
	contains(t, "stdout", text.stdout, "re-fetched "+s.url+", already at "+moved[:7])
	contains(t, "stdout", text.stdout, ", source fetched ")
}

// placementsOf renders the placements of a skill event as
// "<configuration> <mode> <kind>" lines.
func placementsOf(t *testing.T, ev jsonEvent) []string {
	t.Helper()
	var got []string
	for _, p := range ev["placements"].([]any) {
		p := p.(map[string]any)
		got = append(got, p["configuration"].(string)+" "+p["mode"].(string)+" "+p["kind"].(string))
	}
	return got
}

// journalCount is how many mutation journals are waiting in agentx home.
func journalCount(t *testing.T, h *harness) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(h.agentx, "mutations"))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

// copyTree copies a directory, for a test that puts a skill somewhere by
// hand.
func copyTree(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		src, dst := filepath.Join(from, e.Name()), filepath.Join(to, e.Name())
		if e.IsDir() {
			copyTree(t, src, dst)
			continue
		}
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, b, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSkillAddSkipsAForeignSymlink leaves a placement that is a symlink
// somewhere other than the library where it is, even when what it points at
// holds exactly this version: it is the user's link to the user's
// directory, and unlinking it would decide for them where their skill
// lives. Only a real directory of this version is adopted.
// Both placement modes are covered: a copy replaces what is in its way
// wherever a symlink would only be pointed elsewhere, so --copy is the
// mode with something to lose, and it was the untested half.
func TestSkillAddSkipsAForeignSymlink(t *testing.T) {
	t.Parallel()
	for _, mode := range []struct{ name, flag string }{{"symlink", ""}, {"copy", "--copy"}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			h, s := installHarness(t)
			// A directory of exactly this version, somewhere of the user's
			// own, copied from the source's work tree, with the placement
			// path a symlink to it.
			mine := filepath.Join(h.home, "my-skills", "alpha")
			copyTree(t, filepath.Join(s.work, "skills", "alpha"), mine)
			place := filepath.Join(h.home, ".claude", "skills", "alpha")
			if err := os.MkdirAll(filepath.Dir(place), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(mine, place); err != nil {
				t.Fatal(err)
			}

			args := []string{"--json", "skill", "add", s.url, "--name", "alpha"}
			if mode.flag != "" {
				args = append(args, mode.flag)
			}
			out := h.run(args...)
			equal(t, "exit", out.exit, 0)
			target, err := os.Readlink(place)
			if err != nil || target != mine {
				t.Errorf("the placement is %q: %v", target, err)
			}
			// What the link points at is the user's directory and is left
			// whole: a copy would otherwise write this version over it.
			if _, err := os.Stat(filepath.Join(mine, "SKILL.md")); err != nil {
				t.Errorf("the directory the link points at was disturbed: %v", err)
			}
			// The warning says the link is the user's own. Saying it "is
			// not this skill" would be untrue here: it points at exactly
			// this version.
			contains(t, "stderr", out.stderr, place+" is a link of your own and was left as it is")
			contains(t, "the result", out.stdout, "1 placement skipped")
			if strings.Contains(h.one(out.stdout, "result")["summary"].(string), "adopted") {
				t.Errorf("a foreign symlink was reported as adopted:\n%s", out.stdout)
			}
		})
	}
}
