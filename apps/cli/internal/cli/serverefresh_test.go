package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

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
	deadline := time.Now().Add(serveDeadline)
	for {
		if v := mutationVersion(t, h); v >= from+n {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %d writes after version %d within %s", n, from, serveDeadline)
		}
		time.Sleep(5 * time.Millisecond)
	}
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
		time.Sleep(20 * time.Millisecond)
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
func (p *serveProc) awaitLogged(level, prefix string, n int) []string {
	p.t.Helper()
	deadline := time.Now().Add(serveDeadline)
	for {
		if got := p.logged(level, prefix); len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("fewer than %d %s lines starting %q within %s:\n%s", n, level, prefix, serveDeadline, p.stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestServeRefreshesEverySource runs serve with the check interval
// shortened on a machine with two sources: one a skill was installed from,
// and one nothing was installed from. Every check fetches both, the check
// at launch and the ones on the timer: a new commit to the second moves its
// source ref, which plain git reads back, its last_fetched is written
// again, and the version bump of the check's one write makes serve rebuild
// its source index, so that a search finds the skill the commit added
// without anything else asking for a scan.
func TestServeRefreshesEverySource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	one, two := h.fetchSources(t)
	h.mustRun("skill", "add", one.url, "--skill", "alpha")
	backdate(t, h)
	first := h.sourceRef(two.url)

	h.env["AGENTX_CHECK_INTERVAL"] = "200ms"
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
	// Checks never overlap, so once the ref has moved every write still to
	// come is that of a check that fetched after the commit. The lock keeps
	// them out while the settings go back to an old last_fetched, so that
	// the next write is known to be one of them.
	unlock := holdLock(t, h)
	backdate(t, h)
	unlock()
	awaitTrue(t, "two's last_fetched is written again", func() bool { return lastFetched(t, h, two.url) != backdated })
	refetched(t, h, two.url)

	// No refresh is asked for: the rescan the write sets off rebuilds the
	// index on its own.
	want := []string{two.url + " delta delta"}
	deadline := time.Now().Add(serveDeadline)
	for i := 0; ; i++ {
		got := p.search(fmt.Sprintf("s%d", i+1), "upstream")
		if reflect.DeepEqual(got, want) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("results after the ref moved = %q, want %q", got, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
	equal(t, "exit", p.close(), 0)
	if w := warnings(h, p.stderr.String()); len(w) > 0 {
		t.Errorf("serve warned: %q", w)
	}
}

// TestServeWarnsOnceAboutASourceItCannotFetch runs serve with the check
// interval shortened while a source nothing was installed from fails to
// fetch, first because the branch it follows is gone, then because the
// repository is. Every check logs the failure, but only the first check of
// each cause warns: the checks in between log it at debug level. Once the
// source can be fetched again, one note says so and nothing else follows.
// The source beside it is fetched every time and never warned about.
func TestServeWarnsOnceAboutASourceItCannotFetch(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	one, two := h.fetchSources(t)
	h.mustRun("skill", "add", one.url, "--skill", "alpha")
	two.bare("symbolic-ref", "HEAD", "refs/heads/gone") // the default branch it follows names nothing

	h.env["AGENTX_CHECK_INTERVAL"] = "100ms"
	p := h.serve(t, "--json", "--verbose")
	p.next("snapshot")
	about := "update check: " + two.url
	// Three checks with the first cause: one warning, then debug lines.
	p.awaitLogged("debug", about, 2)
	branch := p.logged("warn", about)
	if len(branch) != 1 {
		t.Fatalf("%d warnings about %s over three checks, want 1: %q", len(branch), two.url, branch)
	}
	if strings.Contains(branch[0], "not checked") {
		t.Errorf("the warning names skills left unchecked, and nothing came from %s: %q", two.url, branch[0])
	}

	// The second cause: the repository is gone. One rename makes the
	// change, so no check can see anything in between.
	if err := os.Rename(two.gitDir, two.gitDir+".gone"); err != nil {
		t.Fatal(err)
	}
	gone := p.awaitLogged("warn", about, 2)[1]
	if gone == branch[0] {
		t.Errorf("the second warning repeats the first: %q", gone)
	}
	debugged := len(p.logged("debug", about))
	p.awaitLogged("debug", about, debugged+2) // two more checks with the second cause
	if n := len(p.logged("warn", about)); n != 2 {
		t.Errorf("%d warnings about %s, want one per cause", n, two.url)
	}

	// The source comes back whole: its branch restored while it is still
	// away, then one rename.
	if _, err := two.git.Isolated(context.Background(), two.gitDir+".gone", "symbolic-ref", "HEAD", "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(two.gitDir+".gone", two.gitDir); err != nil {
		t.Fatal(err)
	}
	p.awaitLogged("info", about, 1)
	written := mutationVersion(t, h)
	h.awaitWrites(t, written, 2) // two more checks, which note nothing more
	equal(t, "exit", p.close(), 0)

	equal(t, "the notes", strings.Join(p.logged("info", "update check: "), "\n"), about+" can be fetched again")
	warned := p.logged("warn", "")
	if !slices.Equal(warned, []string{branch[0], gone}) {
		t.Errorf("the warnings = %q, want the two about %s", warned, two.url)
	}
	if !strings.HasPrefix(gone, about+": ") {
		t.Errorf("the warning = %q, want it to name %s first", gone, two.url)
	}
}

// TestServeRefreshSpawnsBoundedGit counts the git processes serve spawns
// per check on its timer, on a machine with a hundred managed skills from
// one source and a second source nothing was installed from: each check
// fetches both, compares every skill and writes once, the rescans its write
// sets off included, and nothing of that is per skill. It also counts the
// fetches of each source: one per check, whether or not a skill came from
// the source, so that no second pass fetches either of them again.
func TestServeRefreshSpawnsBoundedGit(t *testing.T) {
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("many", true)
	const skills = 100
	for i := range skills {
		s.skill(fmt.Sprintf("skills/s%03d", i), fmt.Sprintf("s%03d", i), "Skill number "+fmt.Sprint(i), nil)
	}
	s.commit("a hundred skills")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	other := h.newSourceRepo("other", true)
	other.skill("gamma", "gamma", "Never installed", nil)
	other.commit("gamma")
	h.mustRun("source", "add", other.url)

	calls := countingGit(t, h)
	h.env["AGENTX_CHECK_INTERVAL"] = "100ms"
	before := mutationVersion(t, h)
	// Text mode: the check is the same, and the JSON snapshot of a hundred
	// skills is a longer line than the harness reads.
	p := h.serve(t)
	// Counting starts once the check at launch has written, after the
	// initial scan and the first index build, which lists every source.
	from := h.awaitWrites(t, before, 1)
	n := len(calls())
	to := h.awaitWrites(t, from, 3)
	perCheck := (len(calls()) - n) / (to - from)
	equal(t, "exit", p.close(), 0)
	t.Logf("git processes per check: %d", perCheck)
	// Eleven per source fetched, a few for the check around the fetches and
	// its write, and the rescan the write sets off: under thirty, with
	// room left for a git that fetches in another way. A count per skill
	// would be a hundred at least.
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
			h.env["AGENTX_CHECK_INTERVAL"] = "50ms"
			p := h.serve(t, "--json")
			p.next("snapshot")
			// The scan and the source index it rebuilds have run their git by
			// the time a refresh is acknowledged.
			p.send(`{"type":"refresh","request_id":"settled"}`)
			p.until("settled")
			n := len(calls())
			// Ten ticks. That a tick fires on this timer and runs git once the
			// settings hold a source, TestServeRefreshesEverySource shows.
			time.Sleep(500 * time.Millisecond)
			if later := calls()[n:]; len(later) > 0 {
				t.Errorf("the ticks of a machine with no source spawned git:\n%s", strings.Join(later, "\n"))
			}
			equal(t, "exit", p.close(), 0)
		})
	}
}

// TestServeSkipsARemovedSource runs serve with the check interval shortened
// on a machine where the source two managed skills came from was removed,
// after a check pinned a candidate for one and an upstream-removed marker
// for the other, and has moved on since. The other source stays in the
// settings, so every tick has real fetching to do: it fetches the source
// that stays and never the removed one, by URL, remote or ref, and leaves
// the skills of the removed source as they are, candidate and marker
// included, without a warning about them.
func TestServeSkipsARemovedSource(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	one, two := h.fetchSources(t)
	h.mustRun("skill", "add", one.url, "--all")
	one.skill("skills/alpha", "alpha", "The first skill, revised", nil)
	one.run("rm", "-r", "--quiet", "skills/beta")
	one.commit("alpha revised, beta removed")
	h.mustRun("skill", "check")
	candidate, marker := h.ref(lineage.CandidateRef("alpha")), h.ref(lineage.UpstreamRemovedRef("beta"))
	if candidate == "" || marker == "" {
		t.Fatalf("candidate %q and marker %q before the source went", candidate, marker)
	}
	h.mustRun("source", "remove", one.url)
	// What a fetch of the removed source would find now: another version of
	// alpha, and beta back.
	one.skill("skills/alpha", "alpha", "The first skill, revised again", nil)
	one.skill("skills/beta", "beta", "The second skill, back", nil)
	one.commit("alpha revised again, beta back")

	backdate(t, h)
	calls := countingGit(t, h)
	h.env["AGENTX_CHECK_INTERVAL"] = "100ms"
	before := mutationVersion(t, h)
	p := h.serve(t, "--json")
	p.next("snapshot")
	// The check at launch and two on the timer.
	from := h.awaitWrites(t, before, 1)
	h.awaitWrites(t, from, 2)
	equal(t, "exit", p.close(), 0)

	refetched(t, h, two.url)
	id := source.ID(one.url)
	for _, call := range calls() {
		if strings.Contains(call, id) || strings.Contains(call, one.url) {
			t.Errorf("serve ran git for the removed source: %s", call)
		}
	}
	equal(t, "alpha's candidate", h.ref(lineage.CandidateRef("alpha")), candidate)
	equal(t, "beta's marker", h.ref(lineage.UpstreamRemovedRef("beta")), marker)
	for _, w := range warnings(h, p.stderr.String()) {
		if strings.HasPrefix(w, "update check:") {
			t.Errorf("a check warned: %s", w)
		}
	}
	equal(t, "alpha's drift", drift(h.listed("alpha")), "source removed")
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
