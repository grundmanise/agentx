package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// backdated is the last_fetched every fetch test starts from. The
// timestamps have one-second resolution and a test is faster than that, so
// a run's own clock cannot tell which entries it rewrote; an old value can.
const backdated = "2000-01-01T00:00:00Z"

// backdate sets the last_fetched of every source in the settings file to
// backdated, without going through a mutation: the file is replaced whole,
// which is what a settings write does anyway.
func backdate(t *testing.T, h *harness) {
	t.Helper()
	file := readSettingsFile(t, h)
	for _, entry := range file["sources"].([]any) {
		entry.(map[string]any)["last_fetched"] = backdated
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.agentx, "settings.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// lastFetched is the last_fetched the settings file holds for url.
func lastFetched(t *testing.T, h *harness, url string) string {
	t.Helper()
	for _, entry := range readSettingsFile(t, h)["sources"].([]any) {
		e := entry.(map[string]any)
		if e["url"] == url {
			when, _ := e["last_fetched"].(string)
			return when
		}
	}
	t.Fatalf("no source %s in the settings", url)
	return ""
}

// refetched checks that url's entry was rewritten by this run and that what
// it now holds is a timestamp.
func refetched(t *testing.T, h *harness, url string) {
	t.Helper()
	when := lastFetched(t, h, url)
	if when == backdated {
		t.Errorf("last_fetched of %s is still %s", url, backdated)
		return
	}
	if _, err := time.Parse(time.RFC3339, when); err != nil {
		t.Errorf("last_fetched of %s = %q: %v", url, when, err)
	}
}

// fetchSources adds two sources to the harness: one with two skills under
// skills/, two with one at its root level, both fetched. A test moves one
// of them and proves the other stayed where it was.
func (h *harness) fetchSources(t *testing.T) (one, two *sourceRepo) {
	t.Helper()
	one = h.newSourceRepo("one", true)
	one.skill("skills/alpha", "alpha", "The first skill", nil)
	one.skill("skills/beta", "beta", "The second skill", nil)
	one.commit("first version")
	two = h.newSourceRepo("two", true)
	two.skill("gamma", "gamma", "The third skill", nil)
	two.commit("first version")
	for _, s := range []*sourceRepo{one, two} {
		if out := h.run("source", "add", s.url); out.exit != 0 {
			t.Fatalf("source add %s: exit %d\n%s", s.url, out.exit, out.stderr)
		}
	}
	return one, two
}

// sourceRef is the commit the account repo holds for a source.
func (h *harness) sourceRef(url string) string {
	h.t.Helper()
	return h.accountGit("rev-parse", source.Ref(source.ID(url))+"^{commit}")
}

// TestSourceFetchByURLByIDAndAll walks the three ways to ask for a fetch
// over two sources of which only one moved, and checks each time what
// landed: the ref, the settings entry, the events and the listing.
func TestSourceFetchByURLByIDAndAll(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	one, two := h.fetchSources(t)
	first, twoHead := h.sourceRef(one.url), h.sourceRef(two.url)

	// A new skill upstream, in source one alone.
	one.skill("skills/gamma", "gamma", "A skill added upstream", nil)
	moved := one.commit("second version")
	backdate(t, h)
	before := readVersion(t, h)

	// By URL: one source, one progress event, the ref moved and the event
	// says where from.
	out := h.run("--json", "source", "fetch", one.url)
	equal(t, "exit", out.exit, 0)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"progress", "source", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v\n%s", got, want, out.stderr)
	}
	equal(t, "progress.phase", events[0]["phase"], "fetch")
	equal(t, "progress.subject", events[0]["subject"], one.url)
	equal(t, "progress.current", events[0]["current"], float64(1))
	equal(t, "progress.total", events[0]["total"], float64(1))
	src := events[1]
	equal(t, "source.id", src["id"], source.ID(one.url))
	equal(t, "source.url", src["url"], one.url)
	equal(t, "source.commit", src["commit"], moved)
	equal(t, "source.previous_commit", src["previous_commit"], first)
	equal(t, "source.skills", src["skills"], float64(3))
	equal(t, "source.subpath", src["subpath"], nil)
	equal(t, "source.last_fetched", src["last_fetched"], lastFetched(t, h, one.url))
	equal(t, "stderr", out.stderr, "")
	equal(t, "ref of one", h.sourceRef(one.url), moved)
	equal(t, "ref of two", h.sourceRef(two.url), twoHead)
	refetched(t, h, one.url)
	equal(t, "last_fetched of two", lastFetched(t, h, two.url), backdated)
	equal(t, "version", readVersion(t, h), before+1)

	// The new skill is in the listing, which reads the account repo alone.
	listing := h.run("source", "skills", one.url)
	equal(t, "exit", listing.exit, 0)
	contains(t, "source skills", listing.stdout, "3 skills in "+one.url+" at "+moved[:7])
	contains(t, "source skills", listing.stdout, "gamma")

	// By id: nothing moved this time, and the line says so, but the entry
	// is still marked as fetched.
	backdate(t, h)
	before = readVersion(t, h)
	out = h.run("source", "fetch", source.ID(two.url))
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "✓ re-fetched "+two.url+", already at "+twoHead[:7]+": 1 skill\n")
	refetched(t, h, two.url)
	equal(t, "last_fetched of one", lastFetched(t, h, one.url), backdated)
	equal(t, "version", readVersion(t, h), before+1)

	// Several arguments fetch several sources, reported in the order they
	// were named rather than the order the settings hold them in.
	backdate(t, h)
	before = readVersion(t, h)
	out = h.run("--json", "source", "fetch", two.url, source.ID(one.url))
	equal(t, "exit", out.exit, 0)
	events = h.events(out.stdout)
	if got, want := h.types(events), []string{"progress", "progress", "source", "source", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v\n%s", got, want, out.stderr)
	}
	equal(t, "source[0].url", events[2]["url"], two.url)
	equal(t, "source[1].url", events[3]["url"], one.url)
	refetched(t, h, one.url)
	refetched(t, h, two.url)
	equal(t, "version", readVersion(t, h), before+1) // one settings write for the whole run

	// With --all: every source of the machine, reported in settings order
	// whatever order the fetches finished in.
	two.skill("delta", "delta", "A fourth skill", nil)
	twoMoved := two.commit("second version")
	backdate(t, h)
	before = readVersion(t, h)
	out = h.run("--json", "source", "fetch", "--all")
	equal(t, "exit", out.exit, 0)
	events = h.events(out.stdout)
	if got, want := h.types(events), []string{"progress", "progress", "source", "source", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v\n%s", got, want, out.stderr)
	}
	// The progress events stream as the fetches end, so their subjects come
	// in the order the network decided; what each one says about the run is
	// fixed: one step per source, counted 1 to the total.
	var subjects []string
	for i, e := range events[:2] {
		subjects = append(subjects, e["subject"].(string))
		equal(t, "progress.phase", e["phase"], "fetch")
		equal(t, "progress.current", e["current"], float64(i+1))
		equal(t, "progress.total", e["total"], float64(2))
	}
	sort.Strings(subjects)
	if want := []string{one.url, two.url}; !reflect.DeepEqual(subjects, want) {
		t.Errorf("progress subjects = %v, want %v", subjects, want)
	}
	equal(t, "source[0].url", events[2]["url"], one.url) // sorted by URL: one before two
	equal(t, "source[0].commit", events[2]["commit"], moved)
	equal(t, "source[0].previous_commit", events[2]["previous_commit"], nil) // it did not move
	equal(t, "source[1].url", events[3]["url"], two.url)
	equal(t, "source[1].commit", events[3]["commit"], twoMoved)
	equal(t, "source[1].previous_commit", events[3]["previous_commit"], twoHead)
	equal(t, "source[1].skills", events[3]["skills"], float64(2))
	refetched(t, h, one.url)
	refetched(t, h, two.url)
	equal(t, "ref of two", h.sourceRef(two.url), twoMoved)
	equal(t, "version", readVersion(t, h), before+1) // one settings write again
}

// TestSourceFetchWarnsOnAnUnreachableSourceAndFetchesTheRest: one source
// that cannot be reached is a warning and an exit code, never a reason to
// leave the others as they were.
func TestSourceFetchWarnsOnAnUnreachableSourceAndFetchesTheRest(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	one, two := h.fetchSources(t)
	one.skill("skills/gamma", "gamma", "A skill added upstream", nil)
	moved := one.commit("second version")
	if err := os.Rename(two.gitDir, two.gitDir+".gone"); err != nil { // the URL now names nothing
		t.Fatal(err)
	}
	backdate(t, h)
	before := readVersion(t, h)

	out := h.run("--json", "source", "fetch", "--all")
	equal(t, "exit", out.exit, 3)
	events := h.events(out.stdout)
	if got, want := h.types(events), []string{"progress", "progress", "source", "error", "result"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v\n%s", got, want, out.stderr)
	}
	equal(t, "source.url", events[2]["url"], one.url)
	equal(t, "source.commit", events[2]["commit"], moved)
	equal(t, "error.code", events[3]["code"], "source")
	contains(t, "error.message", events[3]["message"].(string), two.url)
	equal(t, "result.ok", events[4]["ok"], false)
	contains(t, "result.summary", events[4]["summary"].(string), two.url)

	logs := h.events(out.stderr)
	if len(logs) != 1 || logs[0]["level"] != "warn" {
		t.Fatalf("stderr = %q, want one warning", out.stderr)
	}
	contains(t, "log.message", logs[0]["message"].(string), two.url)

	// What did fetch landed: the ref, the settings entry and the version.
	equal(t, "ref of one", h.sourceRef(one.url), moved)
	refetched(t, h, one.url)
	equal(t, "last_fetched of two", lastFetched(t, h, two.url), backdated)
	equal(t, "version", readVersion(t, h), before+1)

	// Named alone, the unreachable source is the whole failure, and the
	// text output names it once with a hint.
	text := h.run("source", "fetch", two.url)
	equal(t, "exit", text.exit, 3)
	equal(t, "stdout", text.stdout, "")
	contains(t, "stderr", text.stderr, "warning: "+two.url)
	contains(t, "stderr", text.stderr, "error: could not fetch "+two.url)
	contains(t, "stderr", text.stderr, "hint: ")
}

func TestSourceFetchErrors(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s, v1, _ := h.standardSource(true)
	equal(t, "exit", h.run("source", "add", s.url+"#v1").exit, 0)

	tests := []struct {
		name    string
		args    []string
		exit    int
		code    string
		message string
		hint    string
	}{
		{"neither an argument nor --all", []string{"source", "fetch"}, 1, "usage", "no source to fetch", "--all"},
		{"both an argument and --all", []string{"source", "fetch", "--all", s.url}, 1, "usage", "not both", "--all"},
		{"no such source", []string{"source", "fetch", "owner/repo"}, 5, "not_found", "no source https://github.com/owner/repo", "source add"},
		{"no such id", []string{"source", "fetch", "0123456789abcdef"}, 5, "not_found", "no source with id 0123456789abcdef", "source list"},
		{"another ref than the pin", []string{"source", "fetch", s.url + "#main"}, 1, "usage", `is pinned to "v1", not "main"`, "source add"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := h.run(append([]string{"--json"}, tt.args...)...)
			equal(t, "exit", out.exit, tt.exit)
			events := h.events(out.stdout)
			if len(events) != 2 || events[0]["type"] != "error" {
				t.Fatalf("events = %v", events)
			}
			equal(t, "error.code", events[0]["code"], tt.code)
			contains(t, "error.message", events[0]["message"].(string), tt.message)
			contains(t, "error.hint", events[0]["hint"].(string), tt.hint)
		})
	}

	// The stored pin spelled out is not a mismatch, and a subpath in the
	// argument is ignored: a fetch covers the whole source.
	out := h.run("--json", "source", "fetch", s.url+"/skills/alpha#v1")
	equal(t, "exit", out.exit, 0)
	src, _ := sourceEvents(t, h.events(out.stdout))
	equal(t, "source.commit", src["commit"], v1)
	equal(t, "source.subpath", src["subpath"], nil)
	equal(t, "source.skills", src["skills"], float64(2))
	equal(t, "source.pin", src["pin"], "v1")

	// Naming one source twice fetches it once.
	out = h.run("--json", "source", "fetch", s.url, source.ID(s.url))
	equal(t, "exit", out.exit, 0)
	if got, want := h.types(h.events(out.stdout)), []string{"progress", "source", "result"}; !reflect.DeepEqual(got, want) {
		t.Errorf("event types = %v, want %v", got, want)
	}

	// An account repo that is gone takes every source with it.
	if err := os.RemoveAll(filepath.Join(h.agentx, "account.git")); err != nil {
		t.Fatal(err)
	}
	out = h.run("--json", "source", "fetch", "--all")
	equal(t, "exit", out.exit, 5)
	equal(t, "error.code", h.events(out.stdout)[0]["code"], "not_found")
}

// TestSourceFetchAllWithoutSources: nothing to do is not a failure.
func TestSourceFetchAllWithoutSources(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requireGit(t)
	out := h.run("source", "fetch", "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout", out.stdout, "No sources. Add one with agentx source add <url>.\n")

	out = h.run("--json", "source", "fetch", "--all")
	equal(t, "exit", out.exit, 0)
	if got, want := h.types(h.events(out.stdout)), []string{"result"}; !reflect.DeepEqual(got, want) {
		t.Errorf("event types = %v, want %v", got, want)
	}
	if _, err := os.Stat(filepath.Join(h.agentx, "version")); err == nil {
		t.Error("a run with nothing to fetch wrote the version file")
	}
}

// gatedGit puts a git wrapper alone on the harness PATH that brackets every
// git process with a mark in a log, so a test can compute how many ran at
// once, and holds every fetch at a gate until want of them have arrived.
// Without the gate a test could only observe that two fetches happened to
// overlap; with it, an implementation that fetched one source after another
// never opens the gate and the peak it reports stays one. The function
// returned reads the log and gives the peak and the total.
func gatedGit(t *testing.T, h *harness, want int) func() (peak, total int) {
	t.Helper()
	return gatedGitOn(t, h, want, `*" fetch "*`)
}

// gatedGitOn is gatedGit over the calls a shell case pattern matches, for a
// command whose parallel calls are reads rather than fetches.
func gatedGitOn(t *testing.T, h *harness, want int, pattern string) func() (peak, total int) {
	t.Helper()
	requireGit(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log, gate := filepath.Join(dir, "log"), filepath.Join(dir, "gate")
	if err := os.Mkdir(gate, 0o755); err != nil {
		t.Fatal(err)
	}
	// PATH is set inside the script because the harness PATH holds this
	// wrapper alone, so the shell would find neither ls nor sleep.
	stubGit(t, h, fmt.Sprintf(`#!/bin/sh
PATH=%s
printf '+' >> %s
case " $* " in
%s)
	: > %s/$$
	i=0
	while [ "$(ls %s | wc -l)" -lt %d ] && [ "$i" -lt 200 ]; do
		i=$((i+1))
		sleep 0.05
	done
	;;
esac
%s "$@"
status=$?
printf '-' >> %s
exit $status
`, os.Getenv("PATH"), log, pattern, gate, gate, want, real, log))
	_ = os.Remove(log) // the --version stubGit runs to clear ETXTBSY is not one of them
	return func() (peak, total int) {
		marks, err := os.ReadFile(log)
		if err != nil {
			t.Fatalf("read %s: %v", log, err)
		}
		running := 0
		for _, mark := range marks {
			if mark == '+' {
				running++
				total++
				peak = max(peak, running)
			} else {
				running--
			}
		}
		return peak, total
	}
}

// TestSourceFetchIsParallelAndBounded counts the git processes of a run
// over every source: they overlap, never more than the worker count at a
// time, and their number follows the sources rather than the skills.
func TestSourceFetchIsParallelAndBounded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requireGit(t)
	const sources = 5
	for i := range sources {
		s := h.newSourceRepo(fmt.Sprintf("s%d", i), true)
		for j := range 1 + 4*i { // s4 alone holds seventeen skills
			s.skill(fmt.Sprintf("skills/skill%d", j), fmt.Sprintf("skill%d", j), "One of many", nil)
		}
		s.commit("first version")
		equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	}
	// The wrapper goes on the PATH only now: the adds above must not open
	// the gate the run below has to open for itself.
	counts := gatedGit(t, h, 2)

	// --verbose puts every git command line on stderr from all the workers
	// at once, which is what the race detector reads this run for.
	out := h.run("--verbose", "source", "fetch", "--all")
	equal(t, "exit", out.exit, 0)
	equal(t, "lines", strings.Count(out.stdout, "\n"), sources)
	// Two fetches per source and no more: the blobless fetch of the ref and
	// the batch of SKILL.md blobs, the path source add already has.
	equal(t, "fetches", fetches(out.stderr), 2*sources)
	peak, total := counts()
	if peak < 2 {
		t.Errorf("at most %d git processes ran at once: the sources were fetched one after another", peak)
	}
	if peak > source.Fetchers {
		t.Errorf("%d git processes ran at once, want at most %d", peak, source.Fetchers)
	}
	// One fetch runs eleven git processes: the source ref before it, the
	// blobless fetch onto the staging ref, the for-each-ref that reads what
	// landed there, three to walk the trees, the blob batch, the
	// batch-check that proves the blobs arrived, the cat-file that reads
	// them, the update-ref that publishes the fetch and the one that drops
	// the staging ref. The run adds the version check and the account repo
	// probe. Nothing here is per skill.
	if bound := 12*sources + 4; total > bound {
		t.Errorf("%d git processes for %d sources, want at most %d", total, sources, bound)
	}
}

// TestSourceFetchKeepsTheNetworkOutsideTheLock holds a fetch inside git and
// mutates the machine settings meanwhile. The mutation must go through,
// which it cannot if the fetch holds the lock, and the fetch's own write
// must keep what it found rather than the settings it read before.
func TestSourceFetchKeepsTheNetworkOutsideTheLock(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	one, _ := h.fetchSources(t)
	one.skill("skills/gamma", "gamma", "A skill added upstream", nil)
	moved := one.commit("second version")
	backdate(t, h)

	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	held, release := filepath.Join(dir, "held"), filepath.Join(dir, "release")
	stubGit(t, h, fmt.Sprintf(`#!/bin/sh
PATH=%s
case " $* " in
*" fetch "*)
	: > %s
	i=0
	while [ ! -f %s ] && [ "$i" -lt 200 ]; do
		i=$((i+1))
		sleep 0.05
	done
	;;
esac
exec %s "$@"
`, os.Getenv("PATH"), held, release, real))

	done := make(chan outcome, 1)
	go func() { done <- h.run("source", "fetch", "--all") }()
	waitForFile(t, held)

	set := h.run("config", "set", "label", "set-during-the-fetch")
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	equal(t, "config set exit", set.exit, 0)

	out := <-done
	equal(t, "exit", out.exit, 0)
	equal(t, "ref of one", h.sourceRef(one.url), moved)
	refetched(t, h, one.url)
	// The settings write of the fetch read the file under the lock, so the
	// label written while it fetched is still there.
	equal(t, "label", readSettingsFile(t, h)["label"], "set-during-the-fetch")
}

// waitForFile blocks until path exists, which is how a test waits for the
// stub git of another goroutine to reach a point.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not appear", filepath.Base(path))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSourceFetchAllKeepsTheRefsOfTheSourcesThatFailed: one run over
// several sources has one exit code but not one outcome. A source whose
// SKILL.md blobs do not arrive keeps the commit it had, whatever the rest
// of the run did, and the sources that fetched move. Without that, a run
// over `--all` that fails halfway costs the machine every source at once,
// and it is the one source that succeeded that publishes the damage: its
// settings write bumps `version`, the serve child rebuilds, and the broken
// sources fall out of search.
func TestSourceFetchAllKeepsTheRefsOfTheSourcesThatFailed(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requireGit(t)
	const sources = 3
	var repos []*sourceRepo
	var before []string
	for i := range sources {
		s := h.newSourceRepo(fmt.Sprintf("s%d", i), true)
		s.skill("alpha", "alpha", "The first skill", nil)
		s.commit("first version")
		equal(t, "add", h.run("source", "add", s.url).exit, 0)
		repos = append(repos, s)
		before = append(before, h.accountGit("rev-parse", source.Ref(source.ID(s.url))))
	}
	for _, s := range repos {
		s.skill("beta", "beta", "The second skill", nil)
		s.commit("second version")
	}
	// The blob batch is refused for all but the first source, so one source
	// of the run lands and two fail.
	failObjectFetch(t, h, source.RemoteName(source.ID(repos[1].url)), source.RemoteName(source.ID(repos[2].url)))

	out := h.run("source", "fetch", "--all")
	equal(t, "exit", out.exit, 3)
	equal(t, "lines", strings.Count(out.stdout, "\n"), 1) // only the source that fetched
	for _, s := range repos[1:] {
		contains(t, "stderr", out.stderr, s.url)
	}

	// The source that fetched moved; the two that failed are where they were.
	if got := h.accountGit("rev-parse", source.Ref(source.ID(repos[0].url))); got == before[0] {
		t.Errorf("the source that fetched is still at %s", got[:7])
	}
	for i, s := range repos[1:] {
		equal(t, "ref of "+s.url, h.accountGit("rev-parse", source.Ref(source.ID(s.url))), before[i+1])
	}

	// Nothing staged is left over, and every source still lists whole: the
	// one that fetched at its new commit, the others at their old one.
	var want []string
	for _, s := range repos {
		want = append(want, source.Ref(source.ID(s.url)))
	}
	sort.Strings(want)
	equal(t, "refs under refs/agentx", h.agentxRefs(), strings.Join(want, "\n"))
	for i, s := range repos {
		out := h.run("--json", "source", "skills", s.url)
		equal(t, "source skills "+s.url, out.exit, 0)
		_, skills := sourceEvents(t, h.events(out.stdout))
		if i == 0 {
			equal(t, "skills of the source that fetched", len(skills), 2)
		} else {
			equal(t, "skills of "+s.url, len(skills), 1)
		}
	}
}

// TestSourceFetchAnswersWithTheCauseItFound: a run has one exit code, and
// it is the code the same failure would have on its own. Fetching a source
// whose pin is gone is exit 5, as adding it would be; a run every source of
// which fell over the account repo's own git is exit 8, as `source skills`
// would be; a run whose sources failed for different reasons is the
// source-level 3, since no one code is true of them all. In every case the
// hint answers the code, and every warning names the source it is about.
func TestSourceFetchAnswersWithTheCauseItFound(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requireGit(t)
	path := h.env["PATH"]
	var repos []*sourceRepo
	for i := range 2 {
		s := h.newSourceRepo(fmt.Sprintf("s%d", i), true)
		s.skill("alpha", "alpha", "The first skill", nil)
		s.commit("first version")
		s.tag("v1")
		equal(t, "add", h.run("source", "add", s.url+"#v1").exit, 0)
		repos = append(repos, s)
	}

	// Every source of the run falls over the account repo's own git, which
	// is exit 8 wherever it happens, and each warning names its source.
	failLocalGit(t, h, "ls-tree")
	out := h.run("--json", "source", "fetch", "--all")
	equal(t, "exit", out.exit, 8)
	e := lastError(t, h.events(out.stdout))
	equal(t, "code", e["code"], "account_repo")
	contains(t, "hint", e["hint"].(string), "agentx doctor")
	for _, s := range repos {
		contains(t, "warning names "+s.url, out.stderr, s.url)
	}

	// The pin of the first source is deleted upstream. Adding it again is
	// exit 5, and fetching it answers the same, hint and all.
	h.env["PATH"] = path // the real git again
	repos[0].run("tag", "--delete", "v1")
	equal(t, "source add", h.run("source", "add", repos[0].url+"#v1").exit, 5)
	out = h.run("--json", "source", "fetch", repos[0].url)
	equal(t, "exit", out.exit, 5)
	e = lastError(t, h.events(out.stdout))
	equal(t, "code", e["code"], "not_found")
	contains(t, "hint", e["hint"].(string), "pin a branch, tag or commit")
	contains(t, "warning", out.stderr, repos[0].url)

	// One source of each cause now: no single code is true of the run, so
	// it is the source-level refusal.
	failLocalGit(t, h, "ls-tree")
	out = h.run("--json", "source", "fetch", "--all")
	equal(t, "exit", out.exit, 3)
	equal(t, "code", lastError(t, h.events(out.stdout))["code"], "source")
}

// TestSourceFetchNamesASourceItHasNothingOf covers the state an interrupted
// removal leaves: the settings entry stays, the remote and the ref are
// gone. git answers a fetch through a remote that is not there by naming
// the remote, which is agentx's internal name for the source; the run must
// say what `source skills` says of the same state instead, and say nothing
// about credentials.
func TestSourceFetchNamesASourceItHasNothingOf(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requireGit(t)
	s := h.newSourceRepo("skills", true)
	s.skill("alpha", "alpha", "The first skill", nil)
	s.commit("first version")
	equal(t, "add", h.run("source", "add", s.url).exit, 0)
	id := source.ID(s.url)
	h.accountGit("remote", "remove", source.RemoteName(id))
	h.accountGit("update-ref", "-d", source.Ref(id))

	out := h.run("--json", "source", "fetch", s.url)
	equal(t, "exit", out.exit, 5)
	e := lastError(t, h.events(out.stdout))
	equal(t, "code", e["code"], "not_found")
	contains(t, "hint", e["hint"].(string), "agentx source add "+sourceAddArg(s.url, ""))
	contains(t, "warning", out.stderr, s.url)
	for _, leak := range []string{source.RemoteName(id), "credential"} {
		if strings.Contains(out.stderr+out.stdout, leak) {
			t.Errorf("the refusal leaks %q:\n%s%s", leak, out.stdout, out.stderr)
		}
	}
	// Nor does the run build half a remote out of the entry on its way past:
	// the refspec is not written for a source that has no remote to fetch
	// from. (git's own --filter bookkeeping writes promisor and
	// partialclonefilter for the remote name before it resolves it, which
	// is git's and not agentx's; url and fetch are what make a remote one.)
	for _, key := range []string{"url", "fetch"} {
		if out, err := h.accountGitErr("config", "--get", "remote."+source.RemoteName(id)+"."+key); err == nil {
			t.Errorf("a fetch of a source with no remote wrote %s = %q", key, out)
		}
	}
}

// TestSourceFetchDropsASourceRemovedMidRun: the settings write skips a
// source removed while the run fetched, so the report must skip it too —
// a source event and a re-fetched line would tell a script the source is
// present and fresh when it is gone. The fetch publishes its ref last of
// all, after the removal has deleted it, so that ref is taken away again.
func TestSourceFetchDropsASourceRemovedMidRun(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	s := h.newSourceRepo("skills", true)
	s.skill("alpha", "alpha", "The first skill", nil)
	s.commit("first version")
	equal(t, "add", h.run("source", "add", s.url).exit, 0)
	s.skill("beta", "beta", "The second skill", nil)
	s.commit("second version")
	arm := gatePublish(t, h) // after the add, which publishes a ref of its own

	reached, release := arm()
	done := make(chan outcome, 1)
	go func() { done <- h.run("--json", "source", "fetch", s.url) }()
	reached()
	equal(t, "remove", h.run("source", "remove", s.url).exit, 0)
	release()

	out := <-done
	equal(t, "exit", out.exit, 0)
	equal(t, "stdout has no re-fetched line", strings.Contains(out.stdout, "re-fetched"), false)
	for _, e := range h.events(out.stdout) {
		if e["type"] == "source" {
			t.Errorf("a removed source was reported as fetched: %v", e)
		}
	}
	// Nothing of the source is left in the account repo either.
	equal(t, "refs under refs/agentx", h.agentxRefs(), "")
	equal(t, "sources", len(readSettingsFile(t, h)["sources"].([]any)), 0)
}

// TestSourceFetchRealignsARemoteWithThePin: the settings hold the pin and
// the remote's refspec is derived from it, so a run interrupted between the
// two leaves a remote recording a ref the settings do not name — what a
// `source add <url>#main` killed after the remote was written and before
// the settings were leaves over a source pinned to v1. A fetch answers for
// the pin the settings hold whatever the remote says, and brings the remote
// back in line, so that a remote left behind does not outlive one run.
func TestSourceFetchRealignsARemoteWithThePin(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	requireGit(t)
	s := h.newSourceRepo("skills", true)
	s.skill("alpha", "alpha", "The first skill", nil)
	s.commit("v1 version")
	s.tag("v1")
	s.skill("gamma", "gamma", "Only on the default branch", nil)
	s.commit("main version")
	equal(t, "add", h.run("source", "add", s.url+"#v1").exit, 0)
	id := source.ID(s.url)
	v1 := h.accountGit("rev-parse", source.Ref(id))

	h.accountGit("config", "remote."+source.RemoteName(id)+".fetch", "+main:"+source.StagingRef(id))
	equal(t, "fetch", h.run("source", "fetch", "--all").exit, 0)

	// The pin decided, not the remote, and the remote records it again.
	equal(t, "ref", h.accountGit("rev-parse", source.Ref(id)), v1)
	equal(t, "refspec", h.accountGit("config", "--get", "remote.src-"+id+".fetch"), "+v1:"+source.StagingRef(id))
	out := h.run("--json", "source", "skills", s.url)
	equal(t, "source skills", out.exit, 0)
	_, skills := sourceEvents(t, h.events(out.stdout))
	equal(t, "skills", len(skills), 1) // gamma is on main alone
}
