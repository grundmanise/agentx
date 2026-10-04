package cli

import (
	"bytes"
	"encoding/json"
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

// logged is every log line serve has written to stderr so far at level
// whose message starts with prefix, in order. Serve writes each line whole,
// so what it has written so far is whole lines.
func (p *serveProc) logged(level, prefix string) []string {
	p.t.Helper()
	var messages []string
	for _, line := range strings.Split(p.stderr.String(), "\n") {
		if line == "" {
			continue
		}
		var e jsonEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			p.t.Fatalf("not a JSON event: %q: %v", line, err)
		}
		if msg, _ := e["message"].(string); e["type"] == "log" && e["level"] == level && strings.HasPrefix(msg, prefix) {
			messages = append(messages, msg)
		}
	}
	return messages
}

// awaitLogged waits until serve has written at least n log lines at level
// whose message starts with prefix, and returns every one written by then.
// A wait that fails logs what serve wrote instead.
func (p *serveProc) awaitLogged(level, prefix string, n int) []string {
	p.t.Helper()
	var got []string
	defer func() {
		if len(got) < n { // awaitTrue failed the test, and this runs as it ends
			p.t.Logf("stderr:\n%s", p.stderr.String())
		}
	}()
	awaitTrue(p.t, fmt.Sprintf("%d %s lines starting %q", n, level, prefix), func() bool {
		got = p.logged(level, prefix)
		return len(got) >= n
	})
	return got
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
	p.next("snapshot")
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

// TestServeWarnsOnceAboutAnUnfetchedSourceWhileAnotherMoves runs serve on a
// machine with two sources, the second left as a removal cut short leaves
// it, its remote and ref gone, while a commit lands in the first and source
// fetch takes it in. The check's write and the fetch's each set off a
// rescan that rebuilds the source index, twice after the initial build,
// but the second source is no different from one rebuild to the next: the
// index warns once that it has not been fetched, and the check at launch
// warns that it could not fetch it and leaves its last_fetched as it was.
// The account remote, which is never fetched as a shared source, is warned
// about by neither.
func TestServeWarnsOnceAboutAnUnfetchedSourceWhileAnotherMoves(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	one, two := h.fetchSources(t)
	h.accountGit("remote", "remove", source.RemoteName(source.ID(two.url)))
	h.accountGit("update-ref", "-d", source.Ref(source.ID(two.url)))
	err := home.Mutate(h.agentx, nil, func() error {
		settings, err := home.LoadSettings(h.agentx)
		if err != nil {
			return err
		}
		settings.SetSource(home.Source{URL: "https://example.invalid/me/forks", Account: true})
		return home.SaveSettings(h.agentx, settings)
	})
	if err != nil {
		t.Fatal(err)
	}
	backdate(t, h)

	before := mutationVersion(t, h)
	p := h.serve(t, "--json")
	p.next("snapshot")
	h.awaitWrite(t, before) // the check at launch, which fetched one
	one.skill("skills/delta", "delta", "A skill added upstream", nil)
	one.commit("delta")
	h.runBesideServe("source", "fetch", one.url)
	// The rebuild that finds the skill logs its warnings before a search
	// can be answered from it.
	found := []string{one.url + " skills/delta delta"}
	searches := 0
	awaitTrue(t, fmt.Sprintf("a search after one moved finds %q", found), func() bool {
		searches++
		return reflect.DeepEqual(p.search(fmt.Sprintf("s%d", searches), "delta"), found)
	})
	equal(t, "exit", p.close(), 0)
	equal(t, "two's last_fetched", lastFetched(t, h, two.url), backdated)

	warned := warnings(h, p.stderr.String())
	slices.Sort(warned)
	want := []string{
		"source " + two.url + " has not been fetched, its skills cannot be searched",
		"update check: source not fetched: " + two.url,
	}
	if !slices.Equal(warned, want) {
		t.Errorf("the warnings = %q, want one from the index and one from the check: %q", warned, want)
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
	s.commit("thirty skills")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	other := h.newSourceRepo("other", true)
	other.skill("gamma", "gamma", "Never installed", nil)
	other.commit("gamma")
	h.mustRun("source", "add", other.url)

	calls := countingGit(t, h)
	before := mutationVersion(t, h)
	// JSON, as the desktop app runs it, so that the rescans read the
	// library's lineage. The snapshot of thirty skills is a line of about
	// 40 KB, which the harness reads whole: it stops at 64 KB.
	p := h.serve(t, "--json")
	p.next("snapshot")
	h.awaitWrite(t, before)
	p.send(`{"type":"refresh","request_id":"settled"}`)
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
			p.next("snapshot")
			p.send(`{"type":"refresh","request_id":"settled"}`)
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

// TestServeWarnsAboutSourcesAnImportBrought runs serve on a machine whose
// settings name two sources because an import wrote them, and which has
// fetched nothing, so has no account repo to fetch into until source add
// creates one. The check at launch fetches nothing, creates no account
// repo and writes nothing, but warns about each source as not fetched, as
// source fetch refuses it. skill check-updates, which checks only the
// sources a managed skill came from, still has nothing to check there.
func TestServeWarnsAboutSourcesAnImportBrought(t *testing.T) {
	t.Parallel()
	from := newHarness(t)
	from.build(t, fixture{dirs: []string{".claude"}})
	one, two := from.fetchSources(t)
	file := from.exportPath("export.json")
	from.mustRun("export", file)

	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	h.mustRun("import", file, "--yes")
	account := filepath.Join(h.agentx, "account.git")
	if _, err := os.Stat(account); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the import left %s behind, or it cannot be told: %v", account, err)
	}
	version := mutationVersion(t, h)
	calls := countingGit(t, h)
	p := h.serve(t, "--json")
	p.next("snapshot")
	var want []string
	for _, s := range []*sourceRepo{one, two} {
		line := "update check: source not fetched: " + s.url
		p.awaitLogged("warn", line, 1)
		want = append(want, line)
	}
	equal(t, "exit", p.close(), 0)

	warned := p.logged("warn", "update check: ")
	slices.Sort(warned)
	slices.Sort(want)
	if !slices.Equal(warned, want) {
		t.Errorf("the check warnings = %q, want one per source: %q", warned, want)
	}
	for _, call := range calls() {
		if slices.Contains(strings.Fields(call), "fetch") {
			t.Errorf("serve fetched on a machine with no account repo: %s", call)
		}
	}
	if _, err := os.Stat(account); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("serve created %s: %v", account, err)
	}
	equal(t, "the version file", mutationVersion(t, h), version)
	equal(t, "skill check-updates", h.mustRun("skill", "check-updates").stdout,
		"Nothing to check: no managed skill comes from a source added on this machine.\n")
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
