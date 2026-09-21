package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// countingGit puts a git wrapper alone on the harness PATH that records
// every invocation and hands it to the real git; the function it returns
// lists what has been run so far.
func countingGit(t *testing.T, h *harness) func() []string {
	t.Helper()
	requireGit(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	stubGit(t, h, "#!/bin/sh\necho \"$@\" >> "+log+"\nexec "+real+" \"$@\"\n")
	_ = os.Remove(log) // the --version stubGit runs to clear ETXTBSY is not one of them
	return func() []string {
		b, err := os.ReadFile(log)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	}
}

// searchSources adds two sources to the harness: alpha, whose skills
// include a nested one and a hidden one, and beta, which shares a skill
// name with alpha.
func (h *harness) searchSources(t *testing.T) (alpha, beta *sourceRepo) {
	t.Helper()
	alpha = h.newSourceRepo("alpha", true)
	alpha.skill("commit", "commit", "Write a commit message", nil)
	alpha.skill("review", "review", "Review a pull request", nil)
	alpha.skill("tools/lint", "lint", "Lint the code before a commit", nil)
	alpha.skill(".hidden/secret", "secret", "Never listed", nil)
	alpha.commit("skills")
	beta = h.newSourceRepo("beta", true)
	beta.skill("commit", "commit", "Commit with conventional messages", nil)
	beta.skill("deploy", "deploy", "Deploy to production", nil)
	beta.commit("skills")
	for _, s := range []*sourceRepo{beta, alpha} {
		if out := h.run("source", "add", s.url); out.exit != 0 {
			t.Fatalf("source add %s: exit %d\n%s", s.url, out.exit, out.stderr)
		}
	}
	return alpha, beta
}

// results reduces a search event to "<source> <subpath> <name>" per result.
func results(e jsonEvent) []string {
	var out []string
	for _, r := range e["results"].([]any) {
		m := r.(map[string]any)
		out = append(out, m["source"].(string)+" "+m["subpath"].(string)+" "+m["name"].(string))
	}
	return out
}

// search sends one search request and returns the results of its answer.
func (p *serveProc) search(id, query string) []string {
	p.t.Helper()
	p.send(`{"type":"search","request_id":"` + id + `","query":"` + query + `"}`)
	e := p.next("search")
	equal(p.t, "request_id", e["request_id"], id)
	equal(p.t, "query", e["query"], query)
	return results(e)
}

func TestServeAnswersSearchFromTheSourceIndex(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	alpha, beta := h.searchSources(t)
	p := h.serve(t, "--json")
	snap := p.next("snapshot")

	p.send(`{"type":"search","request_id":"s1","query":"commit"}`)
	e := p.next("search")
	equal(t, "instance_id", e["instance_id"], snap["instance_id"])
	// Sorted by source canonical URL, then name: alpha before beta, and
	// inside alpha the name match before the description match.
	want := []string{
		alpha.url + " commit commit",
		alpha.url + " tools/lint lint",
		beta.url + " commit commit",
	}
	if got := results(e); !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
	first := e["results"].([]any)[0].(map[string]any)
	equal(t, "description", first["description"], "Write a commit message")
	equal(t, "tree", first["tree"], alpha.tree("commit"))

	// The description is matched too, ignoring case.
	if got, want := p.search("s2", "PULL REQUEST"), []string{alpha.url + " review review"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
	// A hidden directory is never listed, and no match is an empty array.
	for _, query := range []string{"secret", "nothing matches this"} {
		p.send(`{"type":"search","request_id":"s3","query":"` + query + `"}`)
		if got := p.next("search")["results"]; !reflect.DeepEqual(got, []any{}) {
			t.Errorf("results for %q = %#v, want []", query, got)
		}
	}

	// A search that is not one is a usage error, and serve keeps running.
	p.send(`{"type":"search","query":"commit"}`)
	e = p.next("error")
	equal(t, "error.code", e["code"], "usage")
	contains(t, "error.message", e["message"].(string), "request_id")
	p.send(`{"type":"search","request_id":"s4"}`)
	e = p.next("error")
	equal(t, "error.code", e["code"], "usage")
	contains(t, "error.message", e["message"].(string), "query")
	contains(t, "error.hint", e["hint"].(string), `"search"`)
	p.send(`{"type":"bogus"}`)
	contains(t, "error.hint", p.next("error")["hint"].(string), "refresh, search")

	equal(t, "exit", p.close(), 0)
	equal(t, "result.ok", p.next("result")["ok"], true)
}

// TestServeAnswersSearchWhileAScanWaits holds the mutation lock, so the
// rescan a refresh asks for cannot begin: the searches sent after it are
// answered at once, ahead of the refresh, and spawn nothing.
func TestServeAnswersSearchWhileAScanWaits(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	alpha, _ := h.searchSources(t)
	calls := countingGit(t, h)
	p := h.serve(t, "--json")
	p.next("snapshot")
	p.search("idle", "review") // the index is built: its first scan has run

	f, err := os.OpenFile(filepath.Join(h.agentx, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	// No scan can finish while the lock is held, so nothing else runs git
	// and what the wrapper records next is the searches alone.
	before := len(calls())
	p.send(`{"type":"refresh","request_id":"r1"}`)
	if got, want := p.search("s1", "review"), []string{alpha.url + " review review"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results = %q, want %q", got, want)
	}
	if got := p.search("s2", "lint"); len(got) != 1 {
		t.Errorf("results = %q, want the lint skill", got)
	}
	if got := calls()[before:]; len(got) > 0 {
		t.Errorf("the searches spawned git:\n%s", strings.Join(got, "\n"))
	}

	// The refresh waited for the lock all along.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	equal(t, "request_id", p.next("refresh_complete")["request_id"], "r1")
	equal(t, "exit", p.close(), 0)
}

func TestServeSearchIsEmptyWithoutSources(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	calls := countingGit(t, h)
	p := h.serve(t, "--json")
	p.next("snapshot")
	p.send(`{"type":"search","request_id":"s1","query":"commit"}`)
	e := p.next("search")
	equal(t, "request_id", e["request_id"], "s1")
	if got := e["results"]; !reflect.DeepEqual(got, []any{}) {
		t.Errorf("results = %#v, want []", got)
	}
	equal(t, "exit", p.close(), 0)
	// The startup gate runs git --version once; a home with no source
	// reaches git no other time, however many scans and searches it serves.
	if got, want := calls(), []string{"--version"}; !reflect.DeepEqual(got, want) {
		t.Errorf("git spawned with %q, want %q", got, want)
	}
	equal(t, "stderr", p.stderr.String(), "")
}

// TestServeReindexesWhenSourcesChange covers the change signal: a source
// added, fetched again or removed while serve runs is a mutation of agentx
// home, and the scan it schedules rebuilds the index.
func TestServeReindexesWhenSourcesChange(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	requireGit(t)
	p := h.serve(t, "--json")
	p.next("snapshot")
	if got := p.search("s0", "commit"); len(got) != 0 {
		t.Errorf("results before any source = %q", got)
	}

	s := h.newSourceRepo("skills", true)
	s.skill("commit", "commit", "Write a commit message", nil)
	s.commit("first")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	// A refresh is satisfied by a scan begun after it, which follows the add.
	p.send(`{"type":"refresh","request_id":"added"}`)
	p.next("refresh_complete")
	if got, want := p.search("s1", "commit"), []string{s.url + " commit commit"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results after the add = %q, want %q", got, want)
	}

	s.skill("release", "release", "Cut a release commit", nil)
	s.commit("second")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	p.send(`{"type":"refresh","request_id":"fetched"}`)
	p.next("refresh_complete")
	want := []string{s.url + " commit commit", s.url + " release release"}
	if got := p.search("s2", "commit"); !reflect.DeepEqual(got, want) {
		t.Errorf("results after the re-fetch = %q, want %q", got, want)
	}

	equal(t, "exit", h.run("source", "remove", s.url).exit, 0)
	p.send(`{"type":"refresh","request_id":"removed"}`)
	p.next("refresh_complete")
	if got := p.search("s3", "commit"); len(got) != 0 {
		t.Errorf("results after the removal = %q", got)
	}
	equal(t, "exit", p.close(), 0)
	equal(t, "stderr", p.stderr.String(), "")
}

// TestServeWarnsAboutAnUnfetchedSource covers a settings entry whose ref
// the account repo does not hold, which a removal cut short leaves behind.
func TestServeWarnsAboutAnUnfetchedSource(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	requireGit(t)
	s := h.newSourceRepo("skills", true)
	s.skill("commit", "commit", "Write a commit message", nil)
	s.commit("first")
	equal(t, "exit", h.run("source", "add", s.url).exit, 0)
	h.accountGit("update-ref", "-d", "refs/agentx/sources/"+source.ID(s.url))

	p := h.serve(t, "--json")
	p.next("snapshot")
	if got := p.search("s1", "commit"); len(got) != 0 {
		t.Errorf("results = %q, want none", got)
	}
	equal(t, "exit", p.close(), 0)
	events := h.events(p.stderr.String())
	if len(events) != 1 {
		t.Fatalf("stderr events = %v, want one warning", events)
	}
	equal(t, "log.level", events[0]["level"], "warn")
	contains(t, "log.message", events[0]["message"].(string), s.url+" has not been fetched")
}

func TestServePrintsSearchLines(t *testing.T) {
	t.Parallel()
	h := serveHarness(t)
	h.searchSources(t)
	p := h.serve(t)
	p.line("snapshot 1: 1 configuration, 0 skills")
	p.send(`{"type":"search","request_id":"s1","query":"commit"}`)
	p.line("search s1: 3 results")
	p.send(`{"type":"search","request_id":"s2","query":"deploy"}`)
	p.line("search s2: 1 result")
	equal(t, "exit", p.close(), 0)
}
