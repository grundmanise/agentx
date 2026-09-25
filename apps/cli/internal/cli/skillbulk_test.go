package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// bulkHarness is a machine with four configurations and one source holding
// three skills, none of them installed: the starting point of the bulk
// tests. Claude Code and Cursor keep their own skills directories; Codex
// and Gemini CLI read the library itself.
func bulkHarness(t *testing.T) (*harness, *sourceRepo) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex", ".gemini"}})
	s := h.newSourceRepo("bulk", true)
	for _, name := range bulkSkills {
		s.skill("skills/"+name, name, "The "+name+" skill", map[string]string{"notes.md": name + " notes\n"})
	}
	s.commit("three skills")
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	return h, s
}

var bulkSkills = []string{"alpha", "beta", "gamma"}

// installedNames are the skills the library holds, sorted.
func installedNames(t *testing.T, h *harness) []string {
	t.Helper()
	entries, err := os.ReadDir(h.library)
	if err != nil {
		return nil // no install has made the library yet
	}
	var names []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names
}

// TestSkillAddAllSelectsEverySkill takes a whole source in one command:
// every skill lands in the library, every one is placed, and one event and
// one branch answer for each.
func TestSkillAddAllSelectsEverySkill(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	out := h.run("--json", "skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "alpha,beta,gamma")

	events := h.eventsOfType(out.stdout, "library_skill")
	if len(events) != 3 {
		t.Fatalf("%d skill events, want one per skill:\n%s", len(events), out.stdout)
	}
	for i, ev := range events {
		equal(t, "name", ev["name"], bulkSkills[i])
		equal(t, "kind", ev["kind"], "managed")
		equal(t, "state", ev["state"], "current")
	}
	for _, name := range bulkSkills {
		if h.accountGit("rev-parse", "refs/heads/managed/"+name) == "" {
			t.Errorf("%s has no import branch", name)
		}
		for _, rel := range []string{".claude/skills/", ".cursor/skills/"} {
			if _, err := os.Readlink(filepath.Join(h.home, rel+name)); err != nil {
				t.Errorf("%s%s: %v", rel, name, err)
			}
		}
	}
	result := h.one(out.stdout, "result")
	equal(t, "ok", result["ok"], true)
	fetched, _ := h.one(out.stdout, "source")["last_fetched"].(string)
	equal(t, "summary", result["summary"], "installed alpha, beta, gamma from "+s.url+" in 4 configurations, source fetched "+fetched+
		"; always available to universal clients: codex, gemini-cli")
}

// TestSkillAddExceptDeselects leaves out what --except names and installs
// the rest, and repeated --skill selects several by name, a name given
// twice installing one skill.
func TestSkillAddExceptDeselects(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	out := h.run("--json", "skill", "add", s.url, "--all", "--except", "beta")
	equal(t, "exit", out.exit, 0)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "alpha,gamma")

	other, second := bulkHarness(t)
	again := other.run("--json", "skill", "add", second.url, "--skill", "gamma", "--skill", "alpha", "--skill", "alpha")
	equal(t, "exit", again.exit, 0)
	// The listing's order decides, not the order the flags were given, so a
	// run installs the same skills in the same order however they are named.
	equal(t, "the skills in the library", strings.Join(installedNames(t, other), ","), "alpha,gamma")
	names := []string{}
	for _, ev := range other.eventsOfType(again.stdout, "library_skill") {
		names = append(names, ev["name"].(string))
	}
	equal(t, "the skill events", strings.Join(names, ","), "alpha,gamma")
}

// TestSkillAddMatchesNamesInAnyCase selects by the frontmatter name in any
// case for --skill and --except alike, falling back to the directory name
// only when the frontmatter has no name. The library directory keeps the
// frontmatter's spelling.
func TestSkillAddMatchesNamesInAnyCase(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	out := h.run("skill", "add", s.url, "--skill", "GAMMA", "--skill", "Alpha", "--skill", "alpha")
	equal(t, "exit of --skill", out.exit, 0)
	equal(t, "the skills --skill installed", strings.Join(installedNames(t, h), ","), "alpha,gamma")

	e, es := bulkHarness(t)
	out = e.run("skill", "add", es.url, "--all", "--except", "BeTa")
	equal(t, "exit of --except", out.exit, 0)
	equal(t, "the skills --except left", strings.Join(installedNames(t, e), ","), "alpha,gamma")

	o := newHarness(t)
	o.build(t, fixture{dirs: []string{".claude"}})
	r := o.newSourceRepo("renamed", true)
	r.skill("skills/on-disk", "fancy", "Named otherwise in its frontmatter", nil)
	r.skill("skills/nameless", "", "No name in its frontmatter", nil)
	r.skill("skills/plain", "plain", "Named after its directory", nil)
	r.commit("three skills")
	equal(t, "exit of source add", o.run("source", "add", r.url).exit, 0)
	for _, args := range [][]string{
		{"--skill", "plain", "--skill", "on-disk"},
		{"--all", "--except", "on-disk"},
	} {
		out := o.run(append([]string{"skill", "add", r.url}, args...)...)
		equal(t, fmt.Sprint(args, ": exit"), out.exit, 5)
		contains(t, fmt.Sprint(args, ": stderr"), out.stderr, `has no skill called "on-disk"`)
	}
	equal(t, "nothing installed by a missing name", len(installedNames(t, o)), 0)
	out = o.run("skill", "add", r.url, "--all", "--except", "FANCY", "--except", "NameLess")
	equal(t, "exit of --except by frontmatter and fallback names", out.exit, 0)
	equal(t, "the skills left", strings.Join(installedNames(t, o), ","), "plain")
}

// TestSkillAddRefusesAContradictorySelection refuses the flag combinations
// that cannot mean anything, and an --except that names no skill of the
// source, which would otherwise install more than was asked for.
func TestSkillAddRefusesAContradictorySelection(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	for _, c := range []struct {
		what string
		args []string
		exit int
		says string
	}{
		{"--all with --skill", []string{"--all", "--skill", "alpha"}, 1, "--all and --skill cannot both be given"},
		{"--except without --all", []string{"--skill", "alpha", "--except", "beta"}, 1, "--except needs --all"},
		{"--except alone", []string{"--except", "beta"}, 1, "--except needs --all"},
		{"--except nothing of the source", []string{"--all", "--except", "delta"}, 5, `has no skill called "delta"`},
		{"--except everything", []string{"--all", "--except", "alpha", "--except", "beta", "--except", "gamma"}, 1, "--except left no skill to install"},
		{"--skill nothing of the source", []string{"--skill", "alpha", "--skill", "delta"}, 5, `has no skill called "delta"`},
	} {
		out := h.run(append([]string{"skill", "add", s.url}, c.args...)...)
		equal(t, c.what+": exit", out.exit, c.exit)
		contains(t, c.what+": stderr", out.stderr, c.says)
	}
	if names := installedNames(t, h); len(names) > 0 {
		t.Errorf("a refused selection installed %v", names)
	}
}

// TestSkillAddAllAddsASourceThatIsNotAdded takes a whole source this
// machine has not added yet: the run adds it once, as source add would, and
// installs every skill of it, --except leaving some out. A selection whose
// flags contradict each other adds nothing.
func TestSkillAddAllAddsASourceThatIsNotAdded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("bulk", true)
	for _, name := range bulkSkills {
		s.skill("skills/"+name, name, "The "+name+" skill", map[string]string{"notes.md": name + " notes\n"})
	}
	s.commit("three skills")

	for _, args := range [][]string{{"--all", "--skill", "alpha"}, {"--except", "beta"}} {
		out := h.run(append([]string{"skill", "add", s.url}, args...)...)
		equal(t, fmt.Sprint(args, ": exit"), out.exit, 1)
		if list := h.run("--json", "source", "list"); len(h.eventsOfType(list.stdout, "source")) != 0 {
			t.Errorf("%v added the source although the flags contradict each other:\n%s", args, list.stdout)
		}
	}

	out := h.run("--json", "skill", "add", s.url, "--all", "--except", "beta")
	equal(t, "exit", out.exit, 0)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "alpha,gamma")
	equal(t, "source events", len(h.eventsOfType(out.stdout, "source")), 1)
	list := h.run("--json", "source", "list")
	if got := h.eventsOfType(list.stdout, "source"); len(got) != 1 || got[0]["url"] != s.url {
		t.Errorf("the sources after the install = %v, want %s once", got, s.url)
	}

	// Fetched once: what a source add and an install from the added source
	// fetch between them, and no more.
	counted := newHarness(t)
	counted.build(t, fixture{dirs: []string{".claude"}})
	auto := counted.run("--verbose", "skill", "add", s.url, "--all", "--except", "beta")
	equal(t, "exit of the counted install", auto.exit, 0)
	other := newHarness(t)
	other.build(t, fixture{dirs: []string{".claude"}})
	added := other.run("--verbose", "source", "add", s.url)
	installed := other.run("--verbose", "skill", "add", s.url, "--all", "--except", "beta")
	equal(t, "exit of the separate add", added.exit+installed.exit, 0)
	equal(t, "fetches", fetches(auto.stderr), fetches(added.stderr)+fetches(installed.stderr))
	if fetches(added.stderr) == 0 || fetches(installed.stderr) == 0 {
		t.Errorf("the counts say nothing: %d fetches to add, %d to install", fetches(added.stderr), fetches(installed.stderr))
	}
}

// TestSkillAddReportsProgressPerStepPerSkill checks the progress events of
// a batch: three per skill and one for the rescan that ends the run,
// counting from one to the total without a gap.
func TestSkillAddReportsProgressPerStepPerSkill(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	out := h.run("--json", "skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, 0)

	var got []string
	for i, ev := range h.eventsOfType(out.stdout, "progress") {
		equal(t, "current", ev["current"], float64(i+1))
		equal(t, "total", ev["total"], float64(3*len(bulkSkills)+1))
		got = append(got, ev["phase"].(string)+" "+fmt.Sprint(ev["subject"]))
	}
	want := []string{
		"blobs alpha", "blobs beta", "blobs gamma",
		"import alpha", "import beta", "import gamma",
		"install alpha", "install beta", "install gamma",
		"rescan <nil>",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("progress = %v,\nwant %v", got, want)
	}
}

// TestSkillAddInstallsTheRestWhenOneSkillIsBroken is the partial outcome: a
// skill the machine cannot take costs only itself. The others land whole,
// the run names what failed and why, and the exit code is the one that
// failure would have had on its own.
func TestSkillAddInstallsTheRestWhenOneSkillIsBroken(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	// beta's name in the library is taken by something else entirely, which
	// an install never overwrites.
	mine := filepath.Join(h.library, "beta")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(mine, "SKILL.md"), "---\nname: beta\ndescription: mine\n---\n\nmine\n")

	out := h.run("--json", "skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, 6)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "alpha,beta,gamma")
	for _, name := range []string{"alpha", "gamma"} {
		if _, err := os.Readlink(filepath.Join(h.home, ".claude", "skills", name)); err != nil {
			t.Errorf("%s did not install although another skill of the batch failed: %v", name, err)
		}
		if h.accountGit("rev-parse", "refs/heads/managed/"+name) == "" {
			t.Errorf("%s has no import branch", name)
		}
	}
	// beta is left exactly as it was, with no branch claiming it.
	if b, err := os.ReadFile(filepath.Join(mine, "SKILL.md")); err != nil || !strings.Contains(string(b), "mine") {
		t.Errorf("the skill that was in the way was changed: %q %v", b, err)
	}
	if ref := h.accountGit("for-each-ref", "--format=%(refname)", "refs/heads/managed/beta"); ref != "" {
		t.Errorf("the skill that failed got a branch: %s", ref)
	}
	// The result names it, so that a script reading nothing else knows.
	result := h.one(out.stdout, "result")
	equal(t, "ok", result["ok"], false)
	contains(t, "the result", result["summary"].(string), "1 of 3 skills could not be installed: beta: the library already holds beta")
	equal(t, "error code", lastError(t, h.events(out.stdout))["code"], "refused")
	contains(t, "stderr", out.stderr, "beta: the library already holds beta")

	// Two skills installed, so the run's progress budget drops the steps the
	// broken one will not take and still ends where it said it would.
	progress := h.eventsOfType(out.stdout, "progress")
	last := progress[len(progress)-1]
	equal(t, "the last step", last["phase"], "rescan")
	equal(t, "current", last["current"], last["total"])
}

// TestSkillAddDropsASkillTheLibraryCannotName refuses a skill whose
// frontmatter name no directory or branch can carry, and installs the rest:
// one bad name may not cost a batch its whole ref transaction.
func TestSkillAddDropsASkillTheLibraryCannotName(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("names", true)
	s.skill("skills/good", "good", "A name anything can hold", nil)
	s.skill("skills/bad", "feature/login", "A name with a separator in it", nil)
	s.skill("skills/worse", "who?", "A name a branch cannot hold", nil)
	s.commit("three names")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)

	out := h.run("--json", "skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, 6)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "good")
	summary := h.one(out.stdout, "result")["summary"].(string)
	// Each refusal names the thing that cannot hold it. A separator is the
	// library's refusal; "who?" is a name a POSIX directory takes happily
	// and a branch does not, so saying the library cannot hold it would be
	// untrue and would send the user looking in the wrong place.
	for _, want := range []string{
		"2 of 3 skills could not be installed",
		`"feature/login" is not a name the library can hold as a directory`,
		`"who?" is not a name the account repo can hold as an import branch`,
	} {
		contains(t, "the result", summary, want)
	}
}

// TestSkillAddKeepsTheBatchEdgeCasesApart puts every collision the contract
// names into one run, each on a different skill: a library directory that
// already holds the version, a library entry that is a dangling link into
// the worktrees directory, a placement path that is a directory of exactly
// this version and one that is something else. Each is decided for its own
// skill and none of them changes what the others do.
func TestSkillAddKeepsTheBatchEdgeCasesApart(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	// alpha is already in the library, installed from the same source in
	// another home, so this run must adopt it rather than write it again.
	other, second := bulkHarness(t)
	if out := other.run("skill", "add", second.url, "--all"); out.exit != 0 {
		t.Fatalf("the other install: exit %d\n%s", out.exit, out.stderr)
	}
	copyTree(t, filepath.Join(other.library, "alpha"), filepath.Join(h.library, "alpha"))
	// beta's library entry is a fork's placement whose worktree is gone.
	if err := os.Symlink(filepath.Join(h.agentx, "worktrees", "beta"), filepath.Join(h.library, "beta")); err != nil {
		t.Fatal(err)
	}
	// gamma's placement in Claude Code already holds exactly this version,
	// and its placement in Cursor holds something else.
	claude := filepath.Join(h.home, ".claude", "skills", "gamma")
	copyTree(t, filepath.Join(other.library, "gamma"), claude)
	cursor := filepath.Join(h.home, ".cursor", "skills", "gamma")
	if err := os.MkdirAll(cursor, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cursor, "SKILL.md"), "---\nname: gamma\ndescription: mine\n---\n\nmine\n")

	out := h.run("--json", "skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "alpha,beta,gamma")

	// Every library entry is now a real directory of this version, the
	// dangling link included, and every branch is written.
	for _, name := range bulkSkills {
		info, err := os.Lstat(filepath.Join(h.library, name))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("the library entry of %s is not the installed directory: %v", name, err)
		}
		if h.accountGit("rev-parse", "refs/heads/managed/"+name) == "" {
			t.Errorf("%s has no import branch", name)
		}
	}
	// The placement that was this very version is a symlink now; the one
	// that was something else is untouched and named in the warning.
	if info, err := os.Lstat(claude); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the placement of this version was not adopted as a symlink: %v", err)
	}
	if info, err := os.Lstat(cursor); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("a placement that is not this skill was replaced: %v", err)
	}
	contains(t, "stderr", out.stderr, cursor)
	summary := h.one(out.stdout, "result")["summary"].(string)
	for _, want := range []string{"1 already in the library", "1 placement adopted", "1 placement skipped"} {
		contains(t, "the result", summary, want)
	}
}

// TestSkillAddRefusesAConcurrentInstallOfOneName runs two installs of two
// different skills that both want the library name alpha. One of them puts
// it there; the other is refused, and leaves nothing of its own behind:
// no half-built library directory, no staged directory and no journal that
// would refuse the next command.
func TestSkillAddRefusesAConcurrentInstallOfOneName(t *testing.T) {
	// Not parallel, for the reason harness_test.go gives above
	// suiteParallel: it races two installs over one home and counts how
	// many won, so a lock another test's fork was holding would decide it.
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	sources := map[string]*sourceRepo{}
	for _, which := range []string{"one", "two"} {
		s := h.newSourceRepo(which, true)
		s.skill("skill", "alpha", "The "+which+" alpha", map[string]string{"notes.md": which + "\n"})
		s.commit(which)
		equal(t, "exit", h.run("source", "add", s.url).exit, 0)
		sources[which] = s
	}

	outcomes := map[string]outcome{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for which, s := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := h.run("skill", "add", s.url)
			mu.Lock()
			defer mu.Unlock()
			outcomes[which] = got
		}()
	}
	wg.Wait()

	won, lost := 0, 0
	for which, got := range outcomes {
		switch got.exit {
		case 0:
			won++
		case 6, 7: // the name was taken, or the lock was
			lost++
		default:
			t.Errorf("the install of %s exited %d:\n%s", which, got.exit, got.stderr)
		}
	}
	equal(t, "installs that succeeded", won, 1)
	equal(t, "installs that were refused", lost, 1)
	assertNothingLeftBehind(t, h)

	// And once the race is over the refusal is the same every time: the
	// loser run again is refused with the collision named, and still leaves
	// nothing behind.
	for _, s := range sources {
		got := h.run("skill", "add", s.url)
		if got.exit == 0 {
			continue // the one that won; installing it again adopts
		}
		equal(t, "the install of the name that is taken", got.exit, 6)
		contains(t, "stderr", got.stderr, "the library already holds alpha")
	}
	assertNothingLeftBehind(t, h)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "alpha")

	// The machine still answers: no journal is in the way of the next
	// command, and the one skill that landed is managed and current.
	list := h.run("--json", "skill", "list")
	equal(t, "exit", list.exit, 0)
	ev := h.one(list.stdout, "library_skill")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "state", ev["state"], "current")
}

// assertNothingLeftBehind checks that no refused run left staged content, a
// journal or a staging ref where a later command would trip over it.
func assertNothingLeftBehind(t *testing.T, h *harness) {
	t.Helper()
	equal(t, "journals left behind", journalCount(t, h), 0)
	for _, dir := range []string{h.library, filepath.Join(h.home, ".claude", "skills"), filepath.Join(h.agentx, "mutations")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".agentx-staged-") || strings.Contains(e.Name(), ".staged") {
				t.Errorf("%s left behind in %s", e.Name(), dir)
			}
		}
	}
	if left := h.agentxRefs(); strings.Contains(left, "importing") {
		t.Errorf("staging refs left behind:\n%s", left)
	}
}

// TestSkillAddCopyRecordsEverySkillInOneWrite makes copies for a whole
// source and records them all in one settings write: one run is one write,
// however many skills it installed.
func TestSkillAddCopyRecordsEverySkillInOneWrite(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	before := journalCount(t, h)
	out := h.run("--json", "skill", "add", s.url, "--all", "--copy")
	equal(t, "exit", out.exit, 0)

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
	for _, name := range bulkSkills {
		equal(t, "copy_mode of "+name, strings.Join(settings.CopyMode[name], ","), "claude-code,cursor")
		place := filepath.Join(h.home, ".claude", "skills", name)
		info, err := os.Lstat(place)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("the placement of %s is not a copy: %v", name, err)
		}
	}
	equal(t, "journals left behind", journalCount(t, h), before)
	contains(t, "the result", h.one(out.stdout, "result")["summary"].(string), "6 placements as copy")
}

// TestSkillAddRefusesTwoSkillsOfOneName installs a source whose two
// directories carry the same frontmatter name: the library holds one
// directory per name, so the second is dropped and named, and the first
// still lands.
func TestSkillAddRefusesTwoSkillsOfOneName(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("twins", true)
	s.skill("skills/first", "twin", "The first of the two", nil)
	s.skill("skills/second", "twin", "The second of the two", map[string]string{"notes.md": "other\n"})
	s.skill("skills/other", "other", "Not a twin", nil)
	s.commit("two skills of one name")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)

	out := h.run("--json", "skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, 6)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "other,twin")
	contains(t, "the result", h.one(out.stdout, "result")["summary"].(string),
		"twin: twin is also the name of the skill under skills/first, which this run installs")

	// Naming it resolves to one skill, as it always has, so the same source
	// installs without a word when the skill is asked for by name. The
	// second machine reads the same repository through a home of its own.
	other := newHarness(t)
	other.build(t, fixture{dirs: []string{".claude"}})
	equal(t, "exit", other.run("source", "add", s.url).exit, 0)
	equal(t, "exit", other.run("skill", "add", s.url, "--skill", "twin").exit, 0)
	equal(t, "the skills in the library", strings.Join(installedNames(t, other), ","), "twin")
}

// TestSkillAddSignalsNothingWhenNothingInstalls leaves the version file,
// which is the change signal agentx home carries, where it was when every
// skill of a run was refused: nothing changed, so nothing watching the home
// has anything to read again.
func TestSkillAddSignalsNothingWhenNothingInstalls(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	for _, name := range bulkSkills {
		mine := filepath.Join(h.library, name)
		if err := os.MkdirAll(mine, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(mine, "SKILL.md"), "---\nname: "+name+"\ndescription: mine\n---\n\nmine\n")
	}
	before, err := os.ReadFile(filepath.Join(h.agentx, "version"))
	if err != nil {
		t.Fatal(err)
	}
	out := h.run("skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "3 of 3 skills could not be installed")
	after, err := os.ReadFile(filepath.Join(h.agentx, "version"))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the change signal", string(after), string(before))
	assertNothingLeftBehind(t, h)
}

// TestSkillAddBatchAndSingleAgreeOnTheCommit installs one skill on its own
// on one machine and the same skill among three on another, and compares
// the branch they end at. The two runs stream different numbers of commits
// through fast-import; if how a run is batched reached a commit at all,
// the ids would part here and the two machines would disagree about what
// "the version you have" means. What such a commit is compared against
// outside agentx altogether is TestWriteAllMatchesCommitTree, which holds
// it up to git commit-tree.
func TestSkillAddBatchAndSingleAgreeOnTheCommit(t *testing.T) {
	t.Parallel()
	alone, s := bulkHarness(t)
	equal(t, "exit", alone.run("skill", "add", s.url, "--skill", "beta").exit, 0)

	// The second machine is a home of its own, with its own library and its
	// own account repo, reading the same source.
	together := newHarness(t)
	together.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex", ".gemini"}})
	equal(t, "exit", together.run("source", "add", s.url).exit, 0)
	equal(t, "exit", together.run("skill", "add", s.url, "--all").exit, 0)

	single := alone.accountGit("rev-parse", "refs/heads/managed/beta")
	batch := together.accountGit("rev-parse", "refs/heads/managed/beta")
	if single == "" {
		t.Fatal("no import branch was written")
	}
	if single != batch {
		t.Errorf("installed alone beta is %s, installed in a batch %s", single, batch)
	}
}

// TestSkillAddDropsASkillTheSourceCannotServe is the one per-skill refusal
// that is not about a name: a file the account repo does not hold and the
// source will not serve. The account repo is a partial clone, so the blob
// is asked for at install time and the source answers without it; the skill
// that needs it is dropped with exit code 5 and the rest of the run lands.
func TestSkillAddDropsASkillTheSourceCannotServe(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("srcmiss", true)
	s.skill("skills/alpha", "alpha", "The alpha skill", map[string]string{"notes.md": "alpha notes\n"})
	s.skill("skills/beta", "beta", "The beta skill", nil)
	s.commit("two skills")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)

	// The add brought the trees and the SKILL.md blobs; alpha's notes.md is
	// still only in the source. Dropping it from the source's history and
	// pruning it leaves the account repo naming an object the source no
	// longer has, which is what a server that answers a batch without
	// sending everything it was asked for looks like from here.
	s.run("rm", "--quiet", "skills/alpha/notes.md")
	s.run("commit", "--quiet", "--amend", "--no-edit")
	s.bare("reflog", "expire", "--expire=now", "--all")
	s.bare("gc", "--prune=now", "--quiet")

	out := h.run("--json", "skill", "add", s.url, "--all")
	equal(t, "exit", out.exit, 5)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "beta")
	if ref := h.accountGit("for-each-ref", "--format=%(refname)", "refs/heads/managed/alpha"); ref != "" {
		t.Errorf("the skill whose blob did not arrive got a branch: %s", ref)
	}
	if h.accountGit("rev-parse", "refs/heads/managed/beta") == "" {
		t.Error("beta has no import branch although its blobs all arrived")
	}
	result := h.one(out.stdout, "result")
	equal(t, "ok", result["ok"], false)
	contains(t, "the result", result["summary"].(string),
		"1 of 2 skills could not be installed: alpha: alpha holds a file the source did not serve: notes.md")
	failed := lastError(t, h.events(out.stdout))
	equal(t, "error code", failed["code"], "not_found")
	equal(t, "hint", failed["hint"], "run 'agentx source fetch "+s.url+"' to fetch it again")
}

// stagedLeftovers names the .agentx-staged-* entries a directory holds,
// which is what a mutation stages beside the live path before its journal
// exists.
func stagedLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var left []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentx-staged-") {
			left = append(left, filepath.Join(dir, e.Name()))
		}
	}
	return left
}

// TestSkillAddLeavesNothingStagedWhenItGivesUpLate refuses a run after
// every skill of it is staged: the copies are on disk beside the live
// paths, the journal that would recover them does not exist yet, and the
// settings the run has to write cannot be read. What a run gives up on it
// takes away with it, so the library and the configurations are left as
// they were and the next command has nothing to sweep.
func TestSkillAddLeavesNothingStagedWhenItGivesUpLate(t *testing.T) {
	t.Parallel()
	h, s := bulkHarness(t)
	// copy_mode maps a skill to the configurations that hold a copy of it.
	// A string where the list belongs is a settings file this run cannot
	// finish, and it is found only once every skill is staged.
	settings := readSettingsFile(t, h)
	settings["copy_mode"] = map[string]any{"alpha": "cursor"}
	b, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.agentx, "settings.json"), string(b))

	out := h.run("skill", "add", s.url, "--all", "--copy")
	equal(t, "exit", out.exit, 10)
	equal(t, "the skills in the library", strings.Join(installedNames(t, h), ","), "")
	for _, dir := range []string{h.library, filepath.Join(h.home, ".claude", "skills"), filepath.Join(h.home, ".cursor", "skills")} {
		if left := stagedLeftovers(t, dir); len(left) > 0 {
			t.Errorf("the run that gave up left %d staged entries in %s: %v", len(left), dir, left)
		}
	}
	if n := journalCount(t, h); n != 0 {
		t.Errorf("%d journals were written although the run never applied one", n)
	}
}
