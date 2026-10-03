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

// awaitWrites waits until agentx home has seen n mutations since the
// version file read from, and returns the version it then reads. In these
// tests nothing but serve's update checks mutates agentx home once serve
// runs, and each check that fetched a source ends in exactly one write,
// which bumps the version file once: n writes are n checks done.
func (h *harness) awaitWrites(t *testing.T, from, n int) int {
	t.Helper()
	var v int
	awaitTrue(t, fmt.Sprintf("%d writes after version %d", n, from), func() bool {
		v = mutationVersion(t, h)
		return v >= from+n
	})
	return v
}

// awaitTrue waits until done reports true, which it is asked every few
// milliseconds: what serve does on its timer has no event of its own.
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

// quickChecks is the AGENTX_CHECK_INTERVAL of the tests that wait for
// checks on the timer. A check of real sources takes longer than that, and
// a tick that finds one running is skipped, so the checks run about back to
// back: the interval is what a test waits between them and no more.
const quickChecks = "20ms"

// TestServeRefreshesEverySource runs serve with the check interval
// shortened on a machine with three sources: one a skill was installed
// from, one nothing was installed from, and one two managed skills came
// from, removed after a check pinned a candidate for one of them and an
// upstream-removed marker for the other, which has moved on since. Every
// check fetches the first two, the check at launch and the ones on the
// timer: a new commit to the second moves its source ref, which plain git
// reads back, its last_fetched is written again, and the version bump of
// the check's one write makes serve rebuild its source index, so that a
// search finds the skill the commit added without anything else asking for
// a scan. No check runs git for the removed source, by URL, remote or ref,
// and the skills that came from it keep their candidate and marker, without
// a warning about them.
func TestServeRefreshesEverySource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	one, two := h.fetchSources(t)
	h.mustRun("skill", "add", one.url, "--skill", "alpha")
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
	backdate(t, h)
	first := h.sourceRef(two.url)

	calls := countingGit(t, h)
	h.env["AGENTX_CHECK_INTERVAL"] = quickChecks
	before := mutationVersion(t, h)
	p := h.serve(t, "--json")
	p.next("snapshot")
	// The check at launch fetches both sources and stamps both, the one no
	// skill came from included, in its one write.
	h.awaitWrites(t, before, 1)
	refetched(t, h, one.url)
	refetched(t, h, two.url)
	equal(t, "two's ref after the check at launch", h.sourceRef(two.url), first)
	if got := p.search("s0", "upstream"); len(got) != 0 {
		t.Errorf("results before the commit = %q, want none", got)
	}

	two.skill("delta", "delta", "A skill added upstream", nil)
	moved := two.commit("delta")
	awaitTrue(t, "two's source ref moves to the new commit", func() bool { return h.sourceRef(two.url) == moved })

	// No refresh is asked for, and until the settings are backdated below
	// nothing but the checks writes to agentx home: the rescan the write of
	// the check that moved the ref sets off rebuilds the index on its own.
	want := []string{two.url + " delta delta"}
	searches := 0
	awaitTrue(t, fmt.Sprintf("a search after the ref moved finds %q", want), func() bool {
		searches++
		return reflect.DeepEqual(p.search(fmt.Sprintf("s%d", searches), "upstream"), want)
	})

	// Checks never overlap, so once the ref has moved every write still to
	// come is that of a check that fetched after the commit. The lock keeps
	// them out while the settings go back to an old last_fetched, so that
	// the next write is known to be one of them.
	unlock := holdLock(t, h)
	backdate(t, h)
	unlock()
	awaitTrue(t, "two's last_fetched is written again", func() bool { return lastFetched(t, h, two.url) != backdated })
	refetched(t, h, two.url)
	equal(t, "exit", p.close(), 0)
	if w := warnings(h, p.stderr.String()); len(w) > 0 {
		t.Errorf("serve warned: %q", w)
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

// TestServeWarnsOnceAboutASourceItCannotFetch runs serve with the check
// interval shortened while a source nothing was installed from fails to
// fetch because the branch it follows is gone. Every check logs the
// failure, but only the first warns: the checks after it log it at debug
// level. Once the source can be fetched again, one note says so and nothing
// else follows. The source beside it is fetched every time and never warned
// about. TestSourceFailuresReportEachCauseOnce drives the rest of the
// policy, a change of cause among it, check by check.
func TestServeWarnsOnceAboutASourceItCannotFetch(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	one, two := h.fetchSources(t)
	h.mustRun("skill", "add", one.url, "--skill", "alpha")
	two.bare("symbolic-ref", "HEAD", "refs/heads/gone") // the default branch it follows names nothing

	h.env["AGENTX_CHECK_INTERVAL"] = quickChecks
	p := h.serve(t, "--json", "--verbose")
	p.next("snapshot")
	about := "update check: " + two.url
	// Three checks: one warning, then debug lines.
	p.awaitLogged("debug", about, 2)
	warned := p.logged("warn", about)
	if len(warned) != 1 {
		t.Fatalf("%d warnings about %s over three checks, want 1: %q", len(warned), two.url, warned)
	}
	if strings.Contains(warned[0], "not checked") {
		t.Errorf("the warning names skills left unchecked, and nothing came from %s: %q", two.url, warned[0])
	}

	// The source comes back in one write of its HEAD.
	two.bare("symbolic-ref", "HEAD", "refs/heads/main")
	p.awaitLogged("info", about, 1)
	written := mutationVersion(t, h)
	// Two more checks: the first is reported by the time the second wrote.
	h.awaitWrites(t, written, 2)
	equal(t, "exit", p.close(), 0)

	equal(t, "the notes", strings.Join(p.logged("info", "update check: "), "\n"), about+" can be fetched again")
	if all := p.logged("warn", ""); !slices.Equal(all, warned) {
		t.Errorf("the warnings = %q, want the one about %s", all, two.url)
	}
}

// TestServeWarnsOnceAboutAnUnfetchedSourceWhileAnotherMoves runs serve with
// the check interval shortened on a machine with two sources, the second
// left as a removal cut short leaves it, its remote and ref gone, while a
// commit lands in the first twice over. Each check that moves the first
// source's ref writes, and the rescan that write sets off rebuilds the
// source index, but the second source is no different from one rebuild to
// the next: the index warns once that it has not been fetched, and the
// checks warn once that they could not fetch it. The account remote, which is
// never fetched as a shared source, is warned about by neither.
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

	h.env["AGENTX_CHECK_INTERVAL"] = quickChecks
	p := h.serve(t, "--json")
	p.next("snapshot")
	searches := 0
	for _, name := range []string{"delta", "epsilon"} {
		one.skill("skills/"+name, name, "A skill added upstream", nil)
		one.commit(name)
		// The rebuild that finds the skill logs its warnings before a
		// search can be answered from it.
		want := []string{one.url + " skills/" + name + " " + name}
		awaitTrue(t, fmt.Sprintf("a search after one moved finds %q", want), func() bool {
			searches++
			return reflect.DeepEqual(p.search(fmt.Sprintf("s%d", searches), name), want)
		})
	}
	equal(t, "exit", p.close(), 0)

	warned := warnings(h, p.stderr.String())
	slices.Sort(warned)
	want := []string{
		"source " + two.url + " has not been fetched, its skills cannot be searched",
		"update check: source not fetched: " + two.url,
	}
	if !slices.Equal(warned, want) {
		t.Errorf("the warnings = %q, want one from the index and one from the checks: %q", warned, want)
	}
}

// TestServeRefreshSpawnsBoundedGit counts the git processes serve spawns
// per check on its timer, on a machine with thirty managed skills from one
// source and a second source nothing was installed from: each check
// fetches both, compares every skill and writes once, the rescans its write
// sets off included, and nothing of that is per skill. It also counts the
// fetches of each source: one per check, whether or not a skill came from
// the source, so that no second pass fetches either of them again. The
// count is of this test's own git, through its own wrapper, so it runs in
// parallel with the rest.
func TestServeRefreshSpawnsBoundedGit(t *testing.T) {
	// Not parallel: how many rescans a check's write sets off, and so how
	// many git processes a check counts, depends on how fast serve runs,
	// which tests running beside it change. On a loaded three-core runner
	// the count went over the bound.
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
	h.env["AGENTX_CHECK_INTERVAL"] = quickChecks
	before := mutationVersion(t, h)
	// JSON, as the desktop app runs it, so that the rescans read the
	// library's lineage. The snapshot of thirty skills is a line of about
	// 40 KB, which the harness reads whole: it stops at 64 KB.
	p := h.serve(t, "--json")
	p.next("snapshot")
	// Counting starts once the check at launch has written, after the
	// initial scan and the first index build, which lists every source.
	from := h.awaitWrites(t, before, 1)
	n := len(calls())
	to := h.awaitWrites(t, from, 3)
	perCheck := (len(calls()) - n) / (to - from)
	equal(t, "exit", p.close(), 0)
	t.Logf("git processes per check: %d", perCheck)
	// Eleven per source fetched, a few for the check around the fetches and
	// its write, and the rescan the write sets off with its read of the
	// lineage: about thirty, with room left for a git that fetches in
	// another way. A count per skill would add thirty at least, and go over.
	if perCheck > 40 {
		t.Errorf("a check of two sources and %d skills spawned %d git processes", skills, perCheck)
	}
	// The fetches are counted over the whole run, the check at launch
	// included, against the writes it made, one per check: only the last
	// check can have fetched without writing, cut short by serve's exit. A
	// fetch names the source's remote right after its filter, where the
	// blob batch that follows it names --stdin, and the log is searched as
	// one text, since two gits that start together can share a line of it.
	checks := mutationVersion(t, h) - before
	run := strings.Join(calls(), "\n")
	for _, src := range []*sourceRepo{s, other} {
		lastFetched(t, h, src.url) // the settings hold the URL the remote is named after
		fetch := "--filter=blob:none " + source.RemoteName(source.ID(src.url)) + " "
		if got := strings.Count(run, fetch); got < checks || got > checks+1 {
			t.Errorf("%s was fetched %d times over %d checks, want once per check", src.url, got, checks)
		}
	}
}

// TestServeRefreshesNothingWithoutASource: on a machine whose settings hold
// no source, never added or every one removed since, while a skill
// installed from one stays, the ticks of the check spawn no git at all.
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
			h.env["AGENTX_CHECK_INTERVAL"] = "10ms"
			p := h.serve(t, "--json")
			p.next("snapshot")
			// The scan and the source index it rebuilds have run their git by
			// the time a refresh is acknowledged.
			p.send(`{"type":"refresh","request_id":"settled"}`)
			p.until("settled")
			n := len(calls())
			// Ten ticks. That a tick fires on this timer and runs git once the
			// settings hold a source, TestServeRefreshesEverySource shows.
			time.Sleep(100 * time.Millisecond)
			if later := calls()[n:]; len(later) > 0 {
				t.Errorf("the ticks of a machine with no source spawned git:\n%s", strings.Join(later, "\n"))
			}
			equal(t, "exit", p.close(), 0)
		})
	}
}

// TestServeWarnsOnceAboutSourcesAnImportBrought runs serve with the check
// interval shortened on a machine whose settings name two sources because
// an import wrote them, and which has fetched nothing, so has no account
// repo to fetch into until source add creates one. No check runs git,
// creates the account repo or writes anything, but the first warns about
// each source as not fetched, as source fetch refuses it, and the checks
// after it log the same line at debug level. skill check-updates, which
// checks only the sources a managed skill came from, still has nothing to
// check there.
//
// Then source add adds one of the two, which creates the account repo, and
// a new serve starts. Its checks fetch that source, but the account repo
// brings nothing of the other, which is still not fetched: the new serve
// warns about it once again, since what a serve remembers of a failing
// source is its own, and its checks after that log it at debug level.
func TestServeWarnsOnceAboutSourcesAnImportBrought(t *testing.T) {
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
	h.env["AGENTX_CHECK_INTERVAL"] = quickChecks
	p := h.serve(t, "--json", "--verbose")
	p.next("snapshot")
	// The scan and the source index it rebuilds have run their git by the
	// time a refresh is acknowledged; the checks run none.
	p.send(`{"type":"refresh","request_id":"settled"}`)
	p.until("settled")
	n := len(calls())
	var want []string
	for _, s := range []*sourceRepo{one, two} {
		line := "update check: source not fetched: " + s.url
		p.awaitLogged("debug", line, 2) // two checks after the one at launch
		want = append(want, line)
	}
	later := calls()[n:]
	equal(t, "exit", p.close(), 0)

	warned := p.logged("warn", "update check: ")
	slices.Sort(warned)
	slices.Sort(want)
	if !slices.Equal(warned, want) {
		t.Errorf("the check warnings = %q, want one per source: %q", warned, want)
	}
	if len(later) > 0 {
		t.Errorf("the checks of a machine with no account repo spawned git:\n%s", strings.Join(later, "\n"))
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
		"Nothing to check: no managed skill or fork comes from a source added on this machine.\n")

	h.mustRun("source", "add", one.url)
	backdate(t, h)
	before := mutationVersion(t, h)
	p = h.serve(t, "--json", "--verbose")
	p.next("snapshot")
	h.awaitWrites(t, before, 1) // the check at launch, which fetched one
	line := "update check: source not fetched: " + two.url
	p.awaitLogged("debug", line, 2) // two checks after the one at launch
	equal(t, "exit", p.close(), 0)

	refetched(t, h, one.url)
	equal(t, "two's last_fetched", lastFetched(t, h, two.url), backdated)
	equal(t, "the check warnings of the new serve", strings.Join(p.logged("warn", "update check: "), "\n"), line)
}

// TestSourceFailuresReportEachCauseOnce drives the memory serve keeps of
// the sources its checks could not fetch through a run of checks, in text
// mode with --verbose: a warning for a new failure, a debug line while the
// cause stays, a warning again when it changes, a note when the source
// fetches again, and nothing for a source that left the settings. A cause
// that differs only in how long git tried to connect, in how many bytes
// came before a transfer stalled or in which address of the host hung up
// is the same cause; one that differs in the status a server answered
// with is not. A skill's failure is a warning on every check.
func TestSourceFailuresReportEachCauseOnce(t *testing.T) {
	t.Parallel()
	const one, two = "https://example.com/one", "https://example.com/two"
	unreachable := func(url, message string, skills ...string) checkFailure {
		return checkFailure{source: url, fetch: true, skills: skills, f: refuse(exitSource, url+": "+message, "")}
	}
	timeout := func(ms int) string {
		return fmt.Sprintf("git fetch: fatal: unable to access '%s/': Failed to connect to example.com port 443 after %d ms: Couldn't connect to server", one, ms)
	}
	closed := func(addr string) string {
		return "git fetch: Connection closed by " + addr + " port 22: fatal: Could not read from remote repository."
	}
	stalled := func(ms, got, total int) string {
		return fmt.Sprintf("git fetch: error: RPC failed; curl 28 Operation timed out after %d milliseconds with %d out of %d bytes received", ms, got, total)
	}
	status := func(code int) string {
		return fmt.Sprintf("git fetch: fatal: unable to access '%s/': The requested URL returned error: %d", one, code)
	}
	skill := checkFailure{source: two, skill: true, skills: []string{"pdf"}, f: refuse(exitRefused, "the version holds a file agentx will not lay out", "")}
	refreshed := func(urls ...string) map[string]bool {
		m := map[string]bool{}
		for _, u := range urls {
			m[u] = true
		}
		return m
	}
	checks := []struct {
		rep  checkReport
		want []string
	}{
		{checkReport{failures: []checkFailure{unreachable(one, timeout(2034), "pdf")}, refreshed: refreshed(two)},
			[]string{"warning: update check: " + one + ": " + timeout(2034) + "; not checked: pdf"}},
		{checkReport{failures: []checkFailure{unreachable(one, timeout(17)), skill}, refreshed: refreshed(two)},
			[]string{"debug: update check: " + one + ": " + timeout(17), "warning: update check: pdf: the version holds a file agentx will not lay out"}},
		{checkReport{failures: []checkFailure{unreachable(one, closed("140.82.121.4"))}, refreshed: refreshed(two)},
			[]string{"warning: update check: " + one + ": " + closed("140.82.121.4")}},
		{checkReport{failures: []checkFailure{unreachable(one, closed("2606:50c0:8000::154"))}, refreshed: refreshed(two)},
			[]string{"debug: update check: " + one + ": " + closed("2606:50c0:8000::154")}},
		{checkReport{failures: []checkFailure{unreachable(one, stalled(30000, 0, 0))}, refreshed: refreshed(two)},
			[]string{"warning: update check: " + one + ": " + stalled(30000, 0, 0)}},
		{checkReport{failures: []checkFailure{unreachable(one, stalled(30002, 1234, 5678))}, refreshed: refreshed(two)},
			[]string{"debug: update check: " + one + ": " + stalled(30002, 1234, 5678)}},
		{checkReport{failures: []checkFailure{unreachable(one, status(403))}, refreshed: refreshed(two)},
			[]string{"warning: update check: " + one + ": " + status(403)}},
		{checkReport{failures: []checkFailure{unreachable(one, status(404))}, refreshed: refreshed(two)},
			[]string{"warning: update check: " + one + ": " + status(404)}},
		{checkReport{failures: []checkFailure{unreachable(one, "git fetch: fatal: repository not found"), skill}, refreshed: refreshed(two)},
			[]string{"warning: update check: " + one + ": git fetch: fatal: repository not found", "warning: update check: pdf: the version holds a file agentx will not lay out"}},
		{checkReport{refreshed: refreshed(one, two)},
			[]string{"info: update check: " + one + " can be fetched again"}},
		{checkReport{refreshed: refreshed(one, two)}, nil},
		{checkReport{failures: []checkFailure{unreachable(two, "git fetch: fatal: repository not found")}, refreshed: refreshed(one)},
			[]string{"warning: update check: " + two + ": git fetch: fatal: repository not found"}},
		{checkReport{idle: true}, nil}, // every source removed: two is forgotten, with no note
		{checkReport{failures: []checkFailure{unreachable(two, "git fetch: fatal: repository not found")}},
			[]string{"warning: update check: " + two + ": git fetch: fatal: repository not found"}},
	}
	failing := sourceFailures{}
	for i, c := range checks {
		var stderr bytes.Buffer
		failing.report(&writer{stderr: &stderr, verbose: true}, c.rep)
		var got []string
		if out := strings.TrimSuffix(stderr.String(), "\n"); out != "" {
			got = strings.Split(out, "\n")
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("check %d logged\n%q\nwant\n%q", i+1, got, c.want)
		}
	}
}
