package cli

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// coordinates are the four fields of a library_skill event that say where
// a managed skill came from, which removing its source must leave as they
// were.
var coordinates = []string{"source", "subpath", "upstream_commit", "base_hash"}

// librarySkill returns the library_skill event of the named skill from a
// JSON listing, failing the test when there is none.
func (h *harness) librarySkill(text, name string) jsonEvent {
	h.t.Helper()
	for _, e := range h.eventsOfType(text, "library_skill") {
		if e["name"] == name {
			return e
		}
	}
	h.t.Fatalf("no library_skill event for %s:\n%s", name, text)
	return nil
}

// snapshotLibrary returns the entry of the named skill in the library of
// the snapshot serve --once emits, the snapshot the desktop app applies.
func (h *harness) snapshotLibrary(name string) map[string]any {
	h.t.Helper()
	out := h.serveOnce("--json")
	if out.exit != 0 {
		h.t.Fatalf("serve --once: exit %d\n%s", out.exit, out.stderr)
	}
	snap := h.one(out.stdout, "snapshot")
	for _, e := range snap["library"].([]any) {
		if entry := e.(map[string]any); entry["name"] == name {
			return entry
		}
	}
	h.t.Fatalf("the snapshot's library holds no %s:\n%s", name, out.stdout)
	return nil
}

// removedHint is the hint a command that names a removed source by its id
// gives: the command that adds the source again, its URL quoted for a
// shell as import quotes the lines it prints.
func removedHint(url string) string {
	return "run 'agentx source add " + sourceAddArg(url, "") + "' to add it again"
}

// drift is the drift a library entry carries, "" when it carries none.
func drift(entry map[string]any) string {
	list, ok := entry["drift"].([]any)
	if !ok {
		return ""
	}
	var words []string
	for _, w := range list {
		words = append(words, w.(string))
	}
	return strings.Join(words, ",")
}

// TestSourceRemovedIsReportedAndClearedByAddingTheSourceAgain is the whole
// life of the state: a managed skill whose source is removed keeps its
// coordinates and is listed, and put in the snapshot, as source removed;
// an install from that source by id is refused with the command that
// brings the source back; and running that command gives back the listing
// the machine had before the removal, byte for byte, with the skill, its
// import branch and its placements never touched. Following the hint lets
// the refused install through: the source is fetched again, and a skill it
// holds installs as it would have before the removal.
func TestSourceRemovedIsReportedAndClearedByAddingTheSourceAgain(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	beforeText := h.mustRun("skill", "list").stdout
	beforeJSON := h.mustRun("--json", "skill", "list").stdout
	before := h.librarySkill(beforeJSON, "alpha")
	if _, ok := before["drift"]; ok {
		t.Fatalf("a skill whose source is added carries drift: %v", before["drift"])
	}
	branch := h.accountGit("rev-parse", "refs/heads/managed/alpha")

	h.mustRun("source", "remove", s.url)

	// The listing says what happened and keeps where the skill came from.
	afterJSON := h.mustRun("--json", "skill", "list").stdout
	after := h.librarySkill(afterJSON, "alpha")
	equal(t, "drift", drift(after), "source removed")
	equal(t, "state", after["state"], "current")
	for _, field := range coordinates {
		equal(t, field, after[field], before[field])
	}
	afterText := h.mustRun("skill", "list").stdout
	contains(t, "skill list", afterText, "current, source removed")
	contains(t, "skill list", afterText, s.url+"/skills/alpha")

	// The snapshot says the same, in the same words: it is what the
	// desktop app shows, and a one-shot listing may not tell it otherwise.
	entry := h.snapshotLibrary("alpha")
	equal(t, "snapshot drift", drift(entry), "source removed")
	for _, field := range coordinates {
		equal(t, "snapshot "+field, entry[field], before[field])
	}
	listed := map[string]any{}
	if err := json.Unmarshal([]byte(strings.SplitN(afterJSON, "\n", 2)[0]), &listed); err != nil {
		t.Fatal(err)
	}
	delete(listed, "type")
	delete(listed, "schema_version")
	if !reflect.DeepEqual(entry, listed) {
		t.Errorf("the snapshot and skill list disagree about alpha:\nsnapshot:  %v\nskill list: %v", entry, listed)
	}

	// An install that names the source by id cannot fetch it, since an id
	// is a hash. The lineage still knows the URL, so the refusal names the
	// command that adds the source again rather than a listing it is gone
	// from.
	id := source.ID(s.url)
	refused := h.run("--json", "skill", "add", id, "--skill", "beta")
	equal(t, "exit", refused.exit, 5)
	e := h.one(refused.stdout, "error")
	equal(t, "code", e["code"], "not_found")
	contains(t, "message", e["message"].(string), s.url)
	equal(t, "hint", e["hint"], removedHint(s.url))
	if got := h.accountGit("rev-parse", "refs/heads/managed/alpha"); got != branch {
		t.Errorf("the refused install moved the import branch from %s to %s", branch, got)
	}
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/beta"); err == nil {
		t.Error("the refused install wrote an import branch for beta")
	}

	// Adding the source again clears the state and changes nothing else:
	// the listing, which carries the library directory's content hash, the
	// coordinates and every placement, is the one from before the removal.
	h.mustRun("source", "add", s.url)
	equal(t, "import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), branch)
	if got := h.mustRun("--json", "skill", "list").stdout; got != beforeJSON {
		t.Errorf("the JSON listing differs from before the removal:\nbefore:\n%safter:\n%s", beforeJSON, got)
	}
	if got := h.mustRun("skill", "list").stdout; got != beforeText {
		t.Errorf("the listing differs from before the removal:\nbefore:\n%safter:\n%s", beforeText, got)
	}
	if got := drift(h.snapshotLibrary("alpha")); got != "" {
		t.Errorf("the snapshot still carries drift %q after the source was added again", got)
	}

	// The install the hint was given for now goes through.
	again := h.run("--json", "skill", "add", id, "--skill", "beta")
	equal(t, "exit after adding the source again", again.exit, 0)
	equal(t, "beta's add event drift", drift(h.librarySkill(again.stdout, "beta")), "")
	h.accountGit("rev-parse", "--verify", "refs/heads/managed/beta")
	list := h.mustRun("--json", "skill", "list").stdout
	for _, name := range []string{"alpha", "beta"} {
		if got := drift(h.librarySkill(list, name)); got != "" {
			t.Errorf("%s carries drift %q once the source is back", name, got)
		}
	}
	equal(t, "alpha's import branch after the install", h.accountGit("rev-parse", "refs/heads/managed/alpha"), branch)
}

// TestSourceRemovedIsReadFromTheSettings takes the source entry out of the
// settings and puts it back behind agentx's back, with no source command
// at all: the remote and the ref stay, and nothing but the entry changes.
// The state follows the entry both ways, so it is read from the settings
// on every read, and nothing that records it, in agentx home or in the
// account repo, is what a command that changes the settings has to keep.
func TestSourceRemovedIsReadFromTheSettings(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	beforeJSON := h.mustRun("--json", "skill", "list").stdout
	before := h.librarySkill(beforeJSON, "alpha")
	equal(t, "snapshot drift before", drift(h.snapshotLibrary("alpha")), "")
	settings, err := home.LoadSettings(h.agentx)
	if err != nil {
		t.Fatal(err)
	}
	entry := settings.Sources[settings.FindSource(s.url)]
	editSettings := func(edit func(*home.Settings)) {
		t.Helper()
		err := home.Mutate(h.agentx, nil, func() error {
			st, err := home.LoadSettings(h.agentx)
			if err != nil {
				return err
			}
			edit(&st)
			return home.SaveSettings(h.agentx, st)
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	editSettings(func(st *home.Settings) { st.RemoveSource(s.url) })
	refs := h.accountGit("for-each-ref")
	contains(t, "account repo refs", refs, source.Ref(source.ID(s.url)))
	entries := entriesOf(t, h.agentx)

	after := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")
	equal(t, "drift", drift(after), "source removed")
	for _, field := range coordinates {
		equal(t, field, after[field], before[field])
	}
	equal(t, "snapshot drift", drift(h.snapshotLibrary("alpha")), "source removed")
	refused := h.run("--json", "skill", "add", source.ID(s.url), "--skill", "beta")
	equal(t, "refused exit", refused.exit, 5)
	equal(t, "refused hint", h.one(refused.stdout, "error")["hint"], removedHint(s.url))

	// Reading the state wrote nothing for it, anywhere.
	equal(t, "account repo refs after the reads", h.accountGit("for-each-ref"), refs)
	if got := entriesOf(t, h.agentx); !reflect.DeepEqual(got, entries) {
		t.Errorf("agentx home after the reads = %v, want %v", got, entries)
	}

	editSettings(func(st *home.Settings) { st.SetSource(entry) })
	if got := h.mustRun("--json", "skill", "list").stdout; got != beforeJSON {
		t.Errorf("the JSON listing differs once the entry is back:\nbefore:\n%safter:\n%s", beforeJSON, got)
	}
	equal(t, "snapshot drift once the entry is back", drift(h.snapshotLibrary("alpha")), "")
}

// TestImportedSettingsDecideSourceRemoved imports settings that do not hold
// the source a managed skill came from, as the settings of another machine
// may not, and then settings that do. Import writes nothing but the
// settings, so the first leaves the skill source removed although no
// source was removed here, and the second clears it although it fetches
// nothing: an entry whose ref the account repo does not hold is a source
// not fetched, not a source removed.
func TestImportedSettingsDecideSourceRemoved(t *testing.T) {
	t.Parallel()
	_, elsewhere := plainExport(t)
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	beforeJSON := h.mustRun("--json", "skill", "list").stdout
	here := h.exportPath("here.json")
	h.mustRun("export", here)

	h.mustRun("import", elsewhere, "--yes")
	equal(t, "drift under settings without the source", drift(h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")), "source removed")
	equal(t, "snapshot drift", drift(h.snapshotLibrary("alpha")), "source removed")

	h.mustRun("source", "remove", s.url) // takes the ref the import left behind, and no entry
	h.mustRun("import", here, "--yes")
	if _, err := h.accountGitErr("rev-parse", "--verify", source.Ref(source.ID(s.url))); err == nil {
		t.Fatal("the account repo holds the source ref; the test proves nothing")
	}
	if got := h.mustRun("--json", "skill", "list").stdout; got != beforeJSON {
		t.Errorf("the JSON listing differs once the settings hold the source again:\nbefore:\n%safter:\n%s", beforeJSON, got)
	}
}

// TestSourceRemovedDoesNotHideAnEdit holds the two states side by side: a
// skill edited by hand whose source is then removed is both, and neither
// word may stand in for the other.
func TestSourceRemovedDoesNotHideAnEdit(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	writeFile(t, filepath.Join(h.library, "alpha", "notes.md"), "edited by hand\n")
	h.mustRun("source", "remove", s.url)

	ev := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")
	equal(t, "state", ev["state"], "modified")
	equal(t, "drift", drift(ev), "source removed")
	contains(t, "skill list", h.mustRun("skill", "list").stdout, "modified, source removed")
}

// TestSourceRemovedIsForManagedSkillsAlone lists a fork and an unmanaged
// skill beside the managed one after the source goes. The source that
// matters to a fork is the account remote, not the third-party upstream it
// merges from, and an unmanaged skill has no source at all.
func TestSourceRemovedIsForManagedSkillsAlone(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.accountGit("update-ref", "refs/heads/skills/beta", h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	copyTree(t, filepath.Join(h.library, "alpha"), filepath.Join(h.library, "beta"))
	copyTree(t, filepath.Join(h.library, "alpha"), filepath.Join(h.library, "gamma"))
	h.mustRun("source", "remove", s.url)

	out := h.mustRun("--json", "skill", "list").stdout
	equal(t, "managed drift", drift(h.librarySkill(out, "alpha")), "source removed")
	for _, name := range []string{"beta", "gamma"} {
		if ev := h.librarySkill(out, name); drift(ev) != "" {
			t.Errorf("%s, a %s, carries drift %q", name, ev["kind"], drift(ev))
		}
	}
}

// TestInstallByURLAddsARemovedSourceBack installs from a removed source by
// URL, which adds the source again on the way, as an install from any URL
// the settings do not hold does: the skills already installed from it are
// no longer source removed.
func TestInstallByURLAddsARemovedSourceBack(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.mustRun("source", "remove", s.url)
	equal(t, "drift after the removal", drift(h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")), "source removed")

	out := h.mustRun("--json", "skill", "add", s.url, "--skill", "beta")
	equal(t, "added source", h.one(out.stdout, "source")["url"], s.url)
	equal(t, "beta's add event drift", drift(h.librarySkill(out.stdout, "beta")), "")
	list := h.mustRun("--json", "skill", "list").stdout
	for _, name := range []string{"alpha", "beta"} {
		if got := drift(h.librarySkill(list, name)); got != "" {
			t.Errorf("%s carries drift %q after its source was added again", name, got)
		}
	}
}

// TestSourceAddAtAnotherPinClearsSourceRemoved adds the source back at a
// pin the removed entry did not have. The canonical URL is the source's
// identity, so it is the same source whatever it is pinned to, and the
// skill's coordinates stay the version it was installed at.
func TestSourceAddAtAnotherPinClearsSourceRemoved(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	before := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")
	h.mustRun("source", "remove", s.url)
	equal(t, "drift after the removal", drift(h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")), "source removed")

	h.mustRun("source", "add", s.url+"#v1")
	after := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")
	equal(t, "drift", drift(after), "")
	for _, field := range coordinates {
		equal(t, field, after[field], before[field])
	}
}

// TestSourceCommandsNameARemovedSource covers the other commands that take
// a source by id. Each refuses an id no source entry has as it always has,
// and for one that a managed skill still records, names the command that
// adds it again; an id nothing records keeps the hint to list the sources,
// and a URL no entry has keeps the hint to add it.
func TestSourceCommandsNameARemovedSource(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.mustRun("source", "remove", s.url)
	id := source.ID(s.url)
	for _, args := range [][]string{
		{"source", "skills", id},
		{"source", "fetch", id},
		{"source", "remove", id},
		{"skill", "add", id},
	} {
		out := h.run(append([]string{"--json"}, args...)...)
		equal(t, strings.Join(args, " ")+" exit", out.exit, 5)
		equal(t, strings.Join(args, " ")+" hint", h.one(out.stdout, "error")["hint"], removedHint(s.url))
	}

	never := "file://" + filepath.Join(filepath.Dir(h.home), "never.git")
	for _, c := range []struct {
		args []string
		hint string
	}{
		{[]string{"source", "skills", "0123456789abcdef"}, "run 'agentx source list' to see the sources"},
		{[]string{"source", "fetch", "0123456789abcdef"}, "run 'agentx source list' to see the sources"},
		{[]string{"source", "skills", never}, "run 'agentx source add " + never + "' to add it"},
		{[]string{"source", "fetch", never}, "run 'agentx source add " + never + "' to add it"},
	} {
		out := h.run(append([]string{"--json"}, c.args...)...)
		equal(t, strings.Join(c.args, " ")+" exit", out.exit, 5)
		equal(t, strings.Join(c.args, " ")+" hint", h.one(out.stdout, "error")["hint"], c.hint)
	}
}

// TestRemovedSourceHintIsQuotedForAShell removes a source whose URL holds a
// character a shell acts on. The hint is a line to paste, so the URL in it
// is quoted as the lines import prints are, and pasting it adds that source
// and runs nothing else.
func TestRemovedSourceHintIsQuotedForAShell(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("a&b", true)
	s.skill("skills/alpha", "alpha", "The first skill", nil)
	s.commit("one skill")
	// The URL and the id as agentx has them: the canonical URL escapes
	// some characters of a path, and the id is the hash of that URL.
	added := h.one(h.mustRun("--json", "source", "add", s.url).stdout, "source")
	url, id := added["url"].(string), added["id"].(string)
	h.mustRun("skill", "add", url, "--skill", "alpha")
	h.mustRun("source", "remove", url)

	out := h.run("--json", "skill", "add", id, "--skill", "alpha")
	equal(t, "exit", out.exit, 5)
	hint := h.one(out.stdout, "error")["hint"].(string)
	equal(t, "hint", hint, removedHint(url))
	contains(t, "hint", hint, "'"+url+"'")
}

// TestAForkDoesNotNameARemovedSource moves a skill's lineage into the fork
// namespace before its source is removed. The source that matters to a
// fork is the account remote it is published to, not the upstream it
// merges later versions from, so only a managed skill's lineage turns the
// refusal of an id into the command that adds the source again.
func TestAForkDoesNotNameARemovedSource(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.accountGit("update-ref", "refs/heads/skills/alpha", h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	h.accountGit("update-ref", "-d", "refs/heads/managed/alpha")
	h.mustRun("source", "remove", s.url)

	out := h.run("--json", "source", "skills", source.ID(s.url))
	equal(t, "exit", out.exit, 5)
	e := h.one(out.stdout, "error")
	equal(t, "hint", e["hint"], "run 'agentx source list' to see the sources")
	if strings.Contains(e["message"].(string), s.url) {
		t.Errorf("the refusal names the fork's upstream: %s", e["message"])
	}
}

// TestUnreadableLineageLeavesTheRefusalOfAnId breaks the refs of the
// account repo after a source whose skill still records it was removed.
// The lineage is read for the hint alone, and the command was not asked
// about the account repo, so the refusal is what it is without the
// lineage: exit code 5 and the hint to list the sources, not exit code 8.
func TestUnreadableLineageLeavesTheRefusalOfAnId(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.mustRun("source", "remove", s.url)
	// rev-parse --is-bare-repository still succeeds on this, so the check
	// of the account repo passes and the lineage read is what fails.
	writeFile(t, filepath.Join(gitx.AccountRepoPath(h.agentx), "packed-refs"), "this is not a packed-refs line\n")
	if _, err := h.accountGitErr("for-each-ref", "refs/heads/"); err == nil {
		t.Fatal("the account repo is still readable; the test proves nothing")
	}

	out := h.run("--json", "source", "skills", source.ID(s.url))
	equal(t, "exit", out.exit, 5)
	e := h.one(out.stdout, "error")
	equal(t, "code", e["code"], "not_found")
	equal(t, "hint", e["hint"], "run 'agentx source list' to see the sources")
}

// TestPlacementEventsCarryTheDrift places a skill, takes it out of one
// configuration and enables that configuration with the whole library,
// before its source is removed and after. Every command that reports on a
// library skill reports the same object skill list does, so each event
// carries the drift the skill is in, and none when it is in none, with the
// coordinates as they were.
func TestPlacementEventsCarryTheDrift(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	before := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")
	report := func(want string) {
		t.Helper()
		for _, args := range [][]string{
			{"skill", "remove", "alpha", "--from", "cursor"},
			{"skill", "place", "alpha", "--to", "cursor"},
		} {
			what := strings.Join(args, " ")
			ev := h.librarySkill(h.mustRun(append([]string{"--json"}, args...)...).stdout, "alpha")
			equal(t, what+" drift", drift(ev), want)
			for _, field := range coordinates {
				equal(t, what+" "+field, ev[field], before[field])
			}
		}
		// Enabling a configuration with --place-all reports every library
		// skill, alpha among them, which is already placed there and so is
		// left as it is and reported all the same.
		h.mustRun("config", "disable", "cursor")
		ev := h.librarySkill(h.mustRun("--json", "config", "enable", "cursor", "--place-all").stdout, "alpha")
		equal(t, "config enable --place-all drift", drift(ev), want)
		for _, field := range coordinates {
			equal(t, "config enable --place-all "+field, ev[field], before[field])
		}
	}
	report("")
	h.mustRun("source", "remove", s.url)
	report("source removed")
}

// TestAdoptingFromARemovedSourceClearsSourceRemoved adopts a skill the
// vercel skills CLI installed from a source this machine removed. Adopting
// adds the source again as source add would, so the managed skill already
// installed from it is no longer source removed, and nothing else of it
// changes.
func TestAdoptingFromARemovedSourceClearsSourceRemoved(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	before := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")
	branch := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	h.mustRun("source", "remove", s.url)
	equal(t, "drift after the removal", drift(h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")), "source removed")

	vercelInstall(t, h, s, "skills/beta", "beta")
	h.writeLock(h.lockPath(), map[string]lockEntry{"beta": {
		Source: "owner/repo", SourceType: "github", SourceURL: s.url,
		SkillPath: "skills/beta", SkillFolderHash: s.tree("skills/beta"),
	}})
	h.mustRun("adopt", "--skill", "beta")

	list := h.mustRun("--json", "skill", "list").stdout
	if after := h.librarySkill(list, "alpha"); !reflect.DeepEqual(after, before) {
		t.Errorf("alpha after the adoption = %v, want %v", after, before)
	}
	equal(t, "beta's drift", drift(h.librarySkill(list, "beta")), "")
	equal(t, "alpha's import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), branch)
}

// besideServe runs a mutation while a serve of the same home is running.
// Every scan of serve holds the shared lock over its reads, and a mutation
// that finds the lock held gives up after 50 ms, as the contract has it, so
// a mutation that lands on a scan is refused with exit 7 and run again, as
// its user would run it again. It fails nothing itself, so a test can run
// it off its own goroutine.
func (h *harness) besideServe(args ...string) outcome {
	out := h.run(args...)
	for start := time.Now(); out.exit == 7 && time.Since(start) < serveDeadline; out = h.run(args...) {
		time.Sleep(10 * time.Millisecond)
	}
	return out
}

// runBesideServe is besideServe for a mutation that has to succeed.
func (h *harness) runBesideServe(args ...string) {
	h.t.Helper()
	if out := h.besideServe(args...); out.exit != 0 {
		h.t.Fatalf("agentx %s: exit %d\n%s%s", strings.Join(args, " "), out.exit, out.stdout, out.stderr)
	}
}

// TestServeSnapshotFollowsTheSource runs serve while the source goes and
// comes back. Each is a mutation of agentx home, and the snapshot that
// follows it is a changed one, since the library it lists changed: serve
// emits it without being asked, and the desktop app applies it.
func TestServeSnapshotFollowsTheSource(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	p := h.serve(t, "--json")
	libraryOf := func(snap jsonEvent) map[string]any {
		t.Helper()
		entries := snap["library"].([]any)
		if len(entries) != 1 {
			t.Fatalf("the snapshot lists %d library skills, want 1: %v", len(entries), entries)
		}
		return entries[0].(map[string]any)
	}
	first := libraryOf(p.next("snapshot"))
	equal(t, "drift at start", drift(first), "")

	h.runBesideServe("source", "remove", s.url)
	p.send(`{"type":"refresh","request_id":"removed"}`)
	removed := libraryOf(p.next("snapshot"))
	equal(t, "drift after the removal", drift(removed), "source removed")
	for _, field := range coordinates {
		equal(t, field, removed[field], first[field])
	}
	equal(t, "request_id", p.next("refresh_complete")["request_id"], "removed")

	h.runBesideServe("source", "add", s.url)
	p.send(`{"type":"refresh","request_id":"added"}`)
	if again := libraryOf(p.next("snapshot")); !reflect.DeepEqual(again, first) {
		t.Errorf("the library entry after the source came back = %v, want %v", again, first)
	}
	equal(t, "request_id", p.next("refresh_complete")["request_id"], "added")
	equal(t, "exit", p.close(), 0)
}
