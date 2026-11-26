package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// awaitWrite waits until agentx home has seen a mutation since the version
// file read from. In these tests nothing but the one update check serve
// runs at launch mutates agentx home once serve runs, and that check, when
// it fetched a source, ends in exactly one write, which bumps the version
// file once: the write is the check done.
func (h *harness) awaitWrite(t *testing.T, from int) {
	t.Helper()
	awaitTrue(t, fmt.Sprintf("a write after version %d", from), func() bool {
		return mutationVersion(t, h) > from
	})
}

// awaitTrue waits until done reports true, which it is asked every few
// milliseconds: what serve does off its loop has no event of its own.
func awaitTrue(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(serveDeadline)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %s", what, serveDeadline)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestServeRefreshesEverySource runs serve on a machine with three
// sources: one a skill was installed from, one nothing was installed from,
// and one two managed skills came from, removed after a check pinned a
// candidate for one of them and an upstream-removed marker for the other,
// which has moved on since. The check serve runs at launch fetches the
// first two, once each and in one write: a commit made to the second
// before serve started moves its source ref, which plain git reads back,
// its last_fetched is written again, and the version bump of the check's
// write makes serve rebuild its source index, so that a search finds the
// skill the commit added without anything else asking for a scan. No check
// runs git for the removed source, by URL, remote or ref, and the skills
// that came from it keep their candidate and marker, without a warning
// about them. No other check follows the one at launch.
func TestServeRefreshesEverySource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	one, two := h.fetchSources(t)
	h.mustRun("skill", "add", one.url, "--name", "alpha")
	gone := h.newSourceRepo("gone", true)
	gone.skill("skills/epsilon", "epsilon", "A skill of a source removed later", nil)
	gone.skill("skills/zeta", "zeta", "Another skill of that source", nil)
	gone.commit("first version")
	h.mustRun("source", "add", gone.url)
	h.mustRun("skill", "add", gone.url, "--all")
	gone.skill("skills/epsilon", "epsilon", "A skill of a source removed later, revised", nil)
	gone.run("rm", "-r", "--quiet", "skills/zeta")
	gone.commit("epsilon revised, zeta removed")
	h.mustRun("skill", "check-updates")
	candidate, marker := h.ref(lineage.CandidateRef("epsilon")), h.ref(lineage.UpstreamRemovedRef("zeta"))
	if candidate == "" || marker == "" {
		t.Fatalf("candidate %q and marker %q before the source went", candidate, marker)
	}
	h.mustRun("source", "remove", gone.url)
	// What a fetch of the removed source would find now: another version of
	// epsilon, and zeta back.
	gone.skill("skills/epsilon", "epsilon", "A skill of a source removed later, revised again", nil)
	gone.skill("skills/zeta", "zeta", "Another skill of that source, back", nil)
	gone.commit("epsilon revised again, zeta back")
	two.skill("delta", "delta", "A skill added upstream", nil)
	moved := two.commit("delta")
	backdate(t, h)

	calls := countingGit(t, h)
	before := mutationVersion(t, h)
	p := h.serve(t, "--json")
	p.passInventories = true // the adds and fetches change the sources the inventory lists
	p.next("inventory")
	// The check at launch fetches both sources and stamps both, the one no
	// skill came from included, in its one write.
	h.awaitWrite(t, before)
	refetched(t, h, one.url)
	refetched(t, h, two.url)
	equal(t, "two's ref after the check at launch", h.sourceRef(two.url), moved)

	// No refresh is asked for, and nothing but the check writes to agentx
	// home: the rescan its write sets off rebuilds the index on its own.
	want := []string{two.url + " delta delta"}
	searches := 0
	awaitTrue(t, fmt.Sprintf("a search after the check finds %q", want), func() bool {
		searches++
		return reflect.DeepEqual(p.search(fmt.Sprintf("s%d", searches), "upstream"), want)
	})
	equal(t, "exit", p.close(), 0)
	if w := warnings(h, p.stderr.String()); len(w) > 0 {
		t.Errorf("serve warned: %q", w)
	}
	equal(t, "writes over the run", mutationVersion(t, h)-before, 1)

	// A fetch names the source's remote right after its filter, where the
	// blob batch that follows it names --stdin, and the log is searched as
	// one text, since two gits that start together can share a line of it.
	run := strings.Join(calls(), "\n")
	for _, src := range []*sourceRepo{one, two} {
		fetch := "--filter=blob:none " + source.RemoteName(source.ID(src.url)) + " "
		equal(t, src.url+": fetches", strings.Count(run, fetch), 1)
	}
	id := source.ID(gone.url)
	for _, call := range calls() {
		if strings.Contains(call, id) || strings.Contains(call, gone.url) {
			t.Errorf("serve ran git for the removed source: %s", call)
		}
	}
	equal(t, "epsilon's candidate", h.ref(lineage.CandidateRef("epsilon")), candidate)
	equal(t, "zeta's marker", h.ref(lineage.UpstreamRemovedRef("zeta")), marker)
	equal(t, "epsilon's drift", drift(h.listed("epsilon")), "source removed")
}

// TestServeFetchesASourceTheStoreLostWhileAnotherMoves runs serve on a
// machine with two sources, the second left as a removal cut short leaves
// it, its remote and ref gone, while a commit lands in the first and source
// fetch takes it in. The check at launch writes the second's remote again
// and fetches it, so its skills can be searched, and nothing is warned
// about. The hub, which is never fetched as a shared source, is warned
// about by neither.
func TestServeFetchesASourceTheStoreLostWhileAnotherMoves(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	one, two := h.fetchSources(t)
	h.storeGit("remote", "remove", source.RemoteName(source.ID(two.url)))
	h.storeGit("update-ref", "-d", source.Ref(source.ID(two.url)))
	err := home.Mutate(h.agentx, nil, func() error {
		settings, err := home.LoadSettings(h.agentx)
		if err != nil {
			return err
		}
		settings.SetSource(home.Source{URL: "https://example.invalid/me/forks", Hub: true})
		return home.SaveSettings(h.agentx, settings)
	})
	if err != nil {
		t.Fatal(err)
	}
	backdate(t, h)

	before := mutationVersion(t, h)
	p := h.serve(t, "--json")
	p.passInventories = true // the adds and fetches change the sources the inventory lists
	p.next("inventory")
	h.awaitWrite(t, before) // the check at launch, which fetched both
	one.skill("skills/delta", "delta", "A skill added upstream", nil)
	one.commit("delta")
	h.runBesideServe("source", "fetch", one.url)
	found := []string{one.url + " skills/delta delta"}
	searches := 0
	awaitTrue(t, fmt.Sprintf("a search after one moved finds %q", found), func() bool {
		searches++
		return reflect.DeepEqual(p.search(fmt.Sprintf("s%d", searches), "delta"), found)
	})
	searches++
	equal(t, "a search for two's skill", strings.Join(p.search(fmt.Sprintf("s%d", searches), "gamma"), "\n"), two.url+" gamma gamma")
	equal(t, "exit", p.close(), 0)
	if lastFetched(t, h, two.url) == backdated {
		t.Errorf("two's last_fetched stayed %s, want the check at launch", backdated)
	}
	if warned := warnings(h, p.stderr.String()); len(warned) != 0 {
		t.Errorf("the warnings = %q, want none", warned)
	}
}

// TestServeRefreshSpawnsBoundedGit counts the git processes a whole serve
// run spawns, on a machine with thirty managed skills from one source and a
// second source nothing was installed from: the reconciliation and initial
// scan, the first index build, the check at launch, which fetches both
// sources, compares every skill and writes once, the rescan its write sets
// off and one more a refresh asks for. None of that is per skill. The count
// is of this test's own git, through its own wrapper.
func TestServeRefreshSpawnsBoundedGit(t *testing.T) {
	// Not parallel: how many rescans the check's write sets off, and so how
	// many git processes the run counts, depends on how fast serve runs,
	// which tests running beside it change.
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("many", true)
	const skills = 30
	for i := range skills {
		s.skill(fmt.Sprintf("skills/s%03d", i), fmt.Sprintf("s%03d", i), "Skill number "+fmt.Sprint(i), nil)
	}
	head := s.commit("thirty skills")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	other := h.newSourceRepo("other", true)
	other.skill("gamma", "gamma", "Never installed", nil)
	other.commit("gamma")
	h.mustRun("source", "add", other.url)

	calls := countingGit(t, h)
	before := mutationVersion(t, h)
	// JSON, as the desktop app runs it, so that the rescans read the
	// library's lineage. The inventory of thirty skills is a line of about
	// 40 KB, which the harness reads whole: it stops at 64 KB.
	p := h.serve(t, "--json")
	p.next("inventory")
	h.awaitWrite(t, before)
	p.until("settled")
	equal(t, "exit", p.close(), 0)
	n := len(calls())
	t.Logf("git processes over the run: %d", n)
	// Eleven per source fetched, a few for the check around the fetches and
	// its write, and a few per scan with its read of the lineage and per
	// index build: about forty, with room left for a rescan more or a git
	// that fetches in another way. A count per skill would add thirty at
	// least, and go over.
	if n > 60 {
		t.Errorf("a serve run over two sources and %d skills spawned %d git processes:\n%s", skills, n, strings.Join(calls(), "\n"))
	}
	// The skills of a source are counted once per commit, however many
	// scans the run makes.
	equal(t, "counts of the source's skills", strings.Count(strings.Join(calls(), "\n"), " ls-tree -r -t -z "+head), 1)
}

// TestServeRefreshesNothingWithoutASource: on a machine whose settings hold
// no source, never added or every one removed since, while a skill
// installed from one stays, the check at launch fetches nothing and writes
// nothing.
func TestServeRefreshesNothingWithoutASource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(h *harness)
	}{
		{"no source added", func(h *harness) {}},
		{"every source removed", func(h *harness) {
			s := h.newSourceRepo("skills", true)
			s.skill("alpha", "alpha", "A skill", nil)
			s.commit("alpha")
			h.mustRun("skill", "add", s.url)
			h.mustRun("source", "remove", s.url)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.build(t, fixture{dirs: []string{".claude"}})
			requireGit(t)
			tc.setup(h)
			calls := countingGit(t, h)
			version := mutationVersion(t, h)
			p := h.serve(t, "--json")
			p.next("inventory")
			p.until("settled")
			// Serve stops only once the check it started at launch has
			// ended. That the check fetches and writes once the settings
			// hold a source, TestServeRefreshesEverySource shows.
			equal(t, "exit", p.close(), 0)
			for _, call := range calls() {
				if slices.Contains(strings.Fields(call), "fetch") {
					t.Errorf("serve fetched on a machine with no source: %s", call)
				}
			}
			equal(t, "the version file", mutationVersion(t, h), version)
		})
	}
}

// TestServeFetchesSourcesSettingsNameWithoutAStore runs serve on a machine
// whose settings name two sources it never fetched, as settings written on
// another machine leave them, with no store. The check at launch creates
// the store, fetches both and warns about neither, so their skills can be
// searched.
func TestServeFetchesSourcesSettingsNameWithoutAStore(t *testing.T) {
	t.Parallel()
	from := newHarness(t)
	from.build(t, fixture{dirs: []string{".claude"}})
	one, two := from.fetchSources(t)
	exported, err := home.LoadSettings(from.agentx)
	if err != nil {
		t.Fatal(err)
	}
	forgetAccess(&exported)

	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	editSettings(t, h, func(s *home.Settings) { *s = exported })
	store := filepath.Join(h.agentx, "store.git")
	if _, err := os.Stat(store); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the settings write left %s behind, or it cannot be told: %v", store, err)
	}
	before := mutationVersion(t, h)
	p := h.serve(t, "--json")
	p.passInventories = true
	p.next("inventory")
	h.awaitWrite(t, before)
	// The two fetches publish their refs one after the other, and a rebuild
	// can fall between them, so the searches wait for an index that holds both.
	want := map[string][]string{"alpha": {one.url + " skills/alpha alpha"}, "gamma": {two.url + " gamma gamma"}}
	searches := 0
	awaitTrue(t, fmt.Sprintf("searches find %q", want), func() bool {
		for query, found := range want {
			searches++
			if !reflect.DeepEqual(p.search(fmt.Sprintf("s%d", searches), query), found) {
				return false
			}
		}
		return true
	})
	equal(t, "exit", p.close(), 0)
	if warned := warnings(h, p.stderr.String()); len(warned) != 0 {
		t.Errorf("the warnings = %q, want none", warned)
	}
	for _, s := range []*sourceRepo{one, two} {
		equal(t, s.url+": the source ref", h.sourceRef(s.url), s.query("rev-parse", "HEAD"))
	}
}

// TestServeChecksTheHubRemoteWhileItRuns runs serve on machine b of
// two that share a hub, with the hub check it repeats
// made a few milliseconds apart. The check at launch fetches and ends in
// an update_check that is ok. Then, while the hub holds what b's
// remote-tracking branches hold, each hub check is one ls-remote, fetches
// nothing and writes nothing, and ends in an update_check that is ok.
// Once machine a publishes alpha, the next hub check fetches, the next
// inventory says what the hub holds of alpha that b lacks, and the check
// ends in an update_check that is ok. With the hub out of
// reach, the checks warn once, however many fail, each ending in an
// update_check that is not ok, until one reaches it again.
func TestServeChecksTheHubRemoteWhileItRuns(t *testing.T) {
	every := hubCheckEvery
	hubCheckEvery = 20 * time.Millisecond
	t.Cleanup(func() { hubCheckEvery = every })
	a, b, _, remote := twoHomes(t)
	url := "file://" + remote
	calls := countingGit(t, b)
	p := b.serve(t, "--json")
	p.next("inventory")
	checked := func() jsonEvent {
		t.Helper()
		e, _ := p.nextMatching("an update_check", func(e jsonEvent) bool { return e["type"] == "update_check" })
		if _, err := time.Parse(time.RFC3339, e["checked_at"].(string)); err != nil {
			t.Errorf("checked_at %q: %v", e["checked_at"], err)
		}
		return e
	}
	equal(t, "the check at launch: ok", checked()["ok"], true)
	fetched := func() int { return strings.Count(strings.Join(calls(), "\n"), " fetch --") }
	atLaunch, version := fetched(), mutationVersion(t, b)
	if atLaunch == 0 {
		t.Error("the check at launch fetched nothing")
	}

	for range 2 {
		equal(t, "an unchanged remote: ok", checked()["ok"], true)
	}
	equal(t, "an unchanged remote: fetches", fetched(), atLaunch)
	equal(t, "an unchanged remote: writes", mutationVersion(t, b), version)
	contains(t, "an unchanged remote: its git", strings.Join(calls(), "\n"), "ls-remote --refs "+url+" refs/heads/skills/*")

	published := a.commitFork("alpha", forkNotes("one", "one, a"))
	a.mustRun("skill", "publish", "alpha")
	ahead, _ := p.nextEntry("alpha", func(e jsonEvent) bool { return e["hub_ahead"] != nil })
	equal(t, "what the hub holds of alpha", ahead["hub_ahead"].(map[string]any)["commit"], published)
	equal(t, "a remote another machine published to: ok", checked()["ok"], true)
	if fetched() == atLaunch {
		t.Error("the check of a remote another machine published to fetched nothing")
	}

	away := remote + ".away"
	if err := os.Rename(remote, away); err != nil {
		t.Fatal(err)
	}
	// A check that began before the rename can still report ok: wait for the first that failed.
	p.nextMatching("a hub check of the remote out of reach", func(e jsonEvent) bool {
		return e["type"] == "update_check" && e["ok"] == false
	})
	for range 2 {
		equal(t, "a hub out of reach: ok", checked()["ok"], false)
	}
	if err := os.Rename(away, remote); err != nil {
		t.Fatal(err)
	}
	p.nextMatching("an update_check that is ok again", func(e jsonEvent) bool {
		return e["type"] == "update_check" && e["ok"] == true
	})
	equal(t, "exit", p.close(), 0)
	var unreached []string
	for _, w := range warnings(b, p.stderr.String()) {
		if strings.HasPrefix(w, "update check: the hub "+url+": cannot reach it") {
			unreached = append(unreached, w)
		}
	}
	equal(t, "warnings of the hub out of reach", len(unreached), 1)
}

// TestReportServeCheck: what serve's check could not check and each note
// is a warning prefixed "update check: ". A check that failed as a whole is
// one warning, and nothing else is reported.
func TestReportServeCheck(t *testing.T) {
	t.Parallel()
	const one = "https://example.com/one"
	for _, tc := range []struct {
		name string
		rep  checkReport
		err  error
		want string
	}{
		{"checked", checkReport{
			failures: []checkFailure{{source: one, fetch: true, skills: []string{"pdf"}, f: refuse(exitSource, one+": git fetch: fatal: repository not found", "")}},
			notes:    []string{"a note"},
		}, nil, strings.Join([]string{
			"warning: update check: " + one + ": git fetch: fatal: repository not found; not checked: pdf",
			"warning: update check: a note",
		}, "\n") + "\n"},
		{"failed", checkReport{notes: []string{"a note"}}, errors.New("boom"), "warning: update check: boom\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			inv := &invocation{out: &writer{stderr: &stderr}}
			inv.reportServeCheck(tc.rep, tc.err)
			equal(t, "stderr", stderr.String(), tc.want)
		})
	}
}
