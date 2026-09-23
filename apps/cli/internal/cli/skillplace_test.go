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
// configuration is asking for it.
func TestSkillAddToOverridesTheTargets(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("config", "disable", "cursor").exit, 0)

	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha", "--to", "cursor")
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
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	if _, err := os.Lstat(filepath.Join(h.home, ".claude", "skills", "alpha")); err == nil {
		t.Error("a disabled configuration was placed into")
	}
	if _, err := os.Lstat(filepath.Join(h.home, ".cursor", "skills", "alpha")); err != nil {
		t.Errorf("an enabled configuration has no placement: %v", err)
	}
}

func TestSkillAddRefusesAnUnknownConfiguration(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	out := h.run("skill", "add", s.url, "--skill", "alpha", "--to", "nowhere")
	equal(t, "exit", out.exit, 5)
	contains(t, "stderr", out.stderr, "detected configurations:")
}

// TestSkillAddCopyPlacesCopies makes copies instead of symlinks and records
// the copy mode of the whole install in one settings write.
func TestSkillAddCopyPlacesCopies(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	before := journalCount(t, h)
	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha", "--copy")
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
	// would be: copy it out of an install of the same source in another home.
	other := newHarness(t)
	other.build(t, fixture{dirs: []string{".claude"}})
	if out := other.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	if out := other.run("skill", "add", s.url, "--skill", "alpha"); out.exit != 0 {
		t.Fatalf("skill add: exit %d\n%s", out.exit, out.stderr)
	}
	same := filepath.Join(h.home, ".claude", "skills", "alpha")
	copyTree(t, filepath.Join(other.library, "alpha"), same)
	// And a directory holding something else.
	other2 := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(other2, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(other2, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")

	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha")
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
	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha")
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
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")

	// The same version again: the branch stays where it is and nothing is
	// written over the library.
	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha")
	equal(t, "exit", out.exit, 0)
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)
	contains(t, "the result", out.stdout, "adopted alpha")

	// A library directory holding something else is a refusal, not an
	// overwrite: the user's content is never replaced by an install.
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "edited by hand\n")
	mine := h.run("skill", "add", s.url, "--skill", "alpha")
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
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	info, err := os.Lstat(filepath.Join(h.library, "alpha"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("the library entry is not the installed directory: %v", err)
	}
}

// TestSkillAddRefusesASecondVersion refuses to install another version over
// a managed skill: that is an update, which this command does not do.
func TestSkillAddRefusesASecondVersion(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")

	// Move the source on and take the library directory out of the way, so
	// that the branch is the only thing in the way.
	s.skill("skills/alpha", "alpha", "The first skill, moved on", map[string]string{"notes.md": "newer\n"})
	s.commit("a newer version")
	equal(t, "exit", h.run("source", "fetch", s.url).exit, 0)
	if err := os.RemoveAll(filepath.Join(h.library, "alpha")); err != nil {
		t.Fatal(err)
	}
	out := h.run("skill", "add", s.url, "--skill", "alpha")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "already managed at another version")
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)
}

// TestSkillAddNamesTheSkillToInstall refuses a source that holds more than
// one skill without --skill, and a --skill that names none.
func TestSkillAddNamesTheSkillToInstall(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	many := h.run("skill", "add", s.url)
	equal(t, "exit", many.exit, 1)
	contains(t, "stderr", many.stderr, "name one with --skill: alpha, beta")

	none := h.run("skill", "add", s.url, "--skill", "gamma")
	equal(t, "exit", none.exit, 5)
	contains(t, "stderr", none.stderr, "has no skill called \"gamma\"")

	// A path that holds exactly one skill needs no --skill.
	equal(t, "exit", h.run("skill", "add", s.url+"#main").exit, 1) // still the whole repository
	one := h.run("skill", "add", strings.TrimSuffix(s.url, ".git")+".git/skills/beta")
	equal(t, "exit", one.exit, 0)
	if _, err := os.Stat(filepath.Join(h.library, "beta", "SKILL.md")); err != nil {
		t.Errorf("the one skill under the path was not installed: %v", err)
	}
}

// TestSkillAddMatchesTheNameInAnyCase names a skill by its frontmatter
// name in any case, and by its directory name only when the frontmatter
// has no name. The library directory keeps the frontmatter's spelling.
func TestSkillAddMatchesTheNameInAnyCase(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	equal(t, "exit", h.run("skill", "add", s.url, "--skill", "ALPHA").exit, 0)
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("--skill ALPHA did not install alpha: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.library, "beta")); !os.IsNotExist(err) {
		t.Errorf("--skill ALPHA installed beta: %v", err)
	}

	o := newHarness(t)
	o.build(t, fixture{dirs: []string{".claude"}})
	r := o.newSourceRepo("renamed", true)
	r.skill("skills/on-disk", "fancy", "Named otherwise in its frontmatter", nil)
	r.skill("skills/nameless", "", "No name in its frontmatter", nil)
	r.commit("two skills")
	equal(t, "exit of source add", o.run("source", "add", r.url).exit, 0)

	byDir := o.run("skill", "add", r.url, "--skill", "on-disk")
	equal(t, "exit of the directory name", byDir.exit, 5)
	contains(t, "stderr", byDir.stderr, `has no skill called "on-disk"`)
	contains(t, "stderr", byDir.stderr, "name one with --skill: fancy, nameless")

	equal(t, "exit of the frontmatter name", o.run("skill", "add", r.url, "--skill", "Fancy").exit, 0)
	if _, err := os.Stat(filepath.Join(o.library, "fancy", "SKILL.md")); err != nil {
		t.Errorf("--skill Fancy did not install fancy: %v", err)
	}
	equal(t, "exit of the fallback name", o.run("skill", "add", r.url, "--skill", "NameLess").exit, 0)
	if _, err := os.Stat(filepath.Join(o.library, "nameless", "SKILL.md")); err != nil {
		t.Errorf("--skill NameLess did not install nameless: %v", err)
	}
}

// TestSkillAddAddsASourceThatIsNotAdded installs from a URL this machine has
// no source for. The run adds it exactly as source add would, fetching it
// once, and keeps it when the install that follows fails.
func TestSkillAddAddsASourceThatIsNotAdded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s, _, head := h.standardSource(true)

	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha")
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

	// Fetched once: the run makes the fetches of a source add and of an
	// install from the added source, and no more.
	counted := newHarness(t)
	counted.build(t, fixture{dirs: []string{".claude"}})
	auto := counted.run("--verbose", "skill", "add", s.url, "--skill", "alpha")
	equal(t, "exit of the counted add", auto.exit, 0)
	other := newHarness(t)
	other.build(t, fixture{dirs: []string{".claude"}})
	added := other.run("--verbose", "source", "add", s.url)
	installed := other.run("--verbose", "skill", "add", s.url, "--skill", "alpha")
	equal(t, "exit of the separate add", added.exit+installed.exit, 0)
	equal(t, "fetches", fetches(auto.stderr), fetches(added.stderr)+fetches(installed.stderr))
	if fetches(installed.stderr) == 0 || fetches(added.stderr) == 0 {
		t.Errorf("the counts say nothing: %d fetches to add, %d to install", fetches(added.stderr), fetches(installed.stderr))
	}

	// An install that fails after the add keeps the source it added.
	third := newHarness(t)
	third.build(t, fixture{dirs: []string{".claude"}})
	none := third.run("skill", "add", s.url, "--skill", "gamma")
	equal(t, "exit", none.exit, 5)
	contains(t, "stdout", none.stdout, "added "+s.url)
	equal(t, "the source kept", third.one(third.run("--json", "source", "list").stdout, "source")["url"], s.url)
}

// TestSkillAddRefusesAUsageErrorBeforeTheSource refuses a selection the
// command cannot take before it adds or fetches anything: a URL this
// machine has not added stays unadded and the settings are not touched.
func TestSkillAddRefusesAUsageErrorBeforeTheSource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s, _, _ := h.standardSource(true)
	settings := filepath.Join(h.agentx, "settings.json")
	before, errBefore := os.ReadFile(settings)

	out := h.run("skill", "add", s.url, "--skill", "alpha", "--skill", "beta")
	equal(t, "exit", out.exit, 1)
	contains(t, "stderr", out.stderr, "installs one skill at a time")
	if strings.Contains(out.stdout, "added") {
		t.Errorf("a usage error added the source:\n%s", out.stdout)
	}
	after, errAfter := os.ReadFile(settings)
	if string(after) != string(before) || os.IsNotExist(errAfter) != os.IsNotExist(errBefore) {
		t.Errorf("a usage error touched the settings:\nbefore %q (%v)\nafter  %q (%v)", before, errBefore, after, errAfter)
	}
	if list := h.run("--json", "source", "list"); len(h.eventsOfType(list.stdout, "source")) > 0 {
		t.Errorf("a usage error added a source:\n%s", list.stdout)
	}
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

	pin := h.run("skill", "add", s.url+"#v1", "--skill", "alpha")
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
	out := h.run("--json", "skill", "add", s.url, "--skill", "beta")
	equal(t, "exit", out.exit, 0)
	equal(t, "the source ref", h.accountGit("rev-parse", ref), fetchedAt)
	equal(t, "the source commit", h.one(out.stdout, "source")["commit"], fetchedAt)
	if strings.Contains(h.accountGit("cat-file", "commit", "refs/heads/managed/beta"), moved) {
		t.Error("an install without --fetch read a commit the last fetch did not bring")
	}

	// With --fetch: the source is fetched again at its pin first, and the
	// install reads what that fetch brought.
	out = h.run("--json", "skill", "add", s.url, "--skill", "alpha", "--fetch")
	equal(t, "exit", out.exit, 0)
	equal(t, "the source ref", h.accountGit("rev-parse", ref), moved)
	ev := h.one(out.stdout, "source")
	equal(t, "commit", ev["commit"], moved)
	equal(t, "previous_commit", ev["previous_commit"], fetchedAt)
	text := h.run("skill", "add", s.url, "--skill", "alpha", "--fetch")
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
