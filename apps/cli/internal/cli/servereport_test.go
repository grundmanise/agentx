package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// lineStarting reads the lines of a serve child that prints text up to the
// first that starts with prefix, and returns it.
func (p *serveProc) lineStarting(prefix string) string {
	p.t.Helper()
	deadline := time.After(serveDeadline)
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				p.t.Fatalf("serve ended before a line starting %q", prefix)
			}
			if strings.HasPrefix(line, prefix) {
				return line
			}
		case <-deadline:
			p.t.Fatalf("no line starting %q within %s", prefix, serveDeadline)
		}
	}
}

// TestServeWritesThisMachinesReport runs serve on a machine with a hub,
// with the quiet period it waits before a report cut to a few
// milliseconds. With the machine setting report_inventory off, an
// inventory that changes writes nothing. Turned on while serve runs, the
// setting leads to the first report with no other change: a machine_report
// event with the commit the hub's branch holds, a commit with no parent,
// so nothing was written before it, then the fleet with this machine in
// it, read from the store with no call to the hub. A change after that is
// one more commit on the first. A hub set in place of that one while serve
// runs takes the report with no other change, in a commit with no parent,
// and the hub replaced holds none. A serve that prints text reports too, with
// the library listed: at start, its report line names the commit, and the
// report lists the skill added before it started. With the hub out of
// reach, a change is a warning prefixed "machine report: " and serve runs
// on: once the hub is back, the next hub check, made a few milliseconds
// apart here, writes the report with no other change. The branch holds one
// commit a report pushed.
func TestServeWritesThisMachinesReport(t *testing.T) {
	// Not parallel: it changes the quiet period and the time between two
	// hub checks of every serve of this process.
	quiet, every := reportQuietFor, hubCheckEvery
	reportQuietFor = 20 * time.Millisecond
	t.Cleanup(func() { reportQuietFor, hubCheckEvery = quiet, every })
	const id = "0123456789abcdef0123456789abcdef"
	h := serveHarness(t)
	requireGit(t)
	h.asMachine(id)
	h.addLibrarySkill(t, "commit")
	remote := newHubRemote(t, h)
	h.setHub(remote)

	calls := countingGit(t, h)
	p := h.serve(t, "--json")
	p.next("inventory")
	h.addLibrarySkill(t, "review")
	p.next("inventory")
	pushed := func() jsonEvent {
		t.Helper()
		e, _ := p.nextMatching("machine_report", func(e jsonEvent) bool { return e["type"] == "machine_report" })
		return e
	}

	h.runBesideServe("config", "set", "report_inventory", "true")
	report := pushed()
	first, doc, _ := reportOnHub(t, h, remote, id)
	equal(t, "machine_report", fmt.Sprint(report["machine"], report["commit"], report["pushed"]), fmt.Sprint(id, first, true))
	equal(t, "the first report's parents", remoteGit(t, h, remote, "log", "-1", "--format=%P", first), "")
	named(t, doc["library"], "name", "review")
	lsRemotes := strings.Count(strings.Join(calls(), "\n"), " ls-remote ")
	fleet := p.next("fleet")
	equal(t, "the fleet after the report: this machine's commit", named(t, fleet["machines"], "commit", first)["machine"].(map[string]any)["id"], id)
	equal(t, "calls to the hub for the fleet after the report", strings.Count(strings.Join(calls(), "\n"), " ls-remote ")-lsRemotes, 0)

	h.addLibrarySkill(t, "lint")
	second := pushed()["commit"].(string)
	equal(t, "the second report's parents", remoteGit(t, h, remote, "log", "-1", "--format=%P", second), first)

	replaced := remote
	remote = newHubRemote(t, h)
	h.runBesideServe("source", "add", remote, "--hub")
	moved := pushed()["commit"].(string)
	onHub, _, _ := reportOnHub(t, h, remote, id)
	equal(t, "the report on the hub set in its place", onHub, moved)
	equal(t, "its parents", remoteGit(t, h, remote, "log", "-1", "--format=%P", moved), "")
	equal(t, "the reports the hub replaced holds", remoteGit(t, h, replaced, "for-each-ref", "refs/heads/reports/"), "")
	equal(t, "exit", p.close(), 0)
	equal(t, "warnings of the report", len(p.logged("warn", "machine report: ")), 0)

	h.addLibrarySkill(t, "notes")
	hubCheckEvery = 20 * time.Millisecond
	text := h.serve(t)
	line := text.lineStarting("report: ")
	third, doc, _ := reportOnHub(t, h, remote, id)
	equal(t, "the report line", line, "report: wrote this machine's report to reports/"+id+" on the hub at "+short(third))
	named(t, doc["library"], "name", "notes")
	equal(t, "the line of the fleet after it", text.lineStarting("machines: "), "machines: 1 on the hub")

	away := remote + ".away"
	if err := os.Rename(remote, away); err != nil {
		t.Fatal(err)
	}
	h.addLibrarySkill(t, "format")
	const warning = "warning: machine report: cannot reach the hub file://"
	awaitTrue(t, "a warning of the report that could not be written", func() bool {
		return strings.Contains(text.stderr.String(), warning+remote)
	})
	if err := os.Rename(away, remote); err != nil {
		t.Fatal(err)
	}
	line = text.lineStarting("report: ")
	fourth, doc, _ := reportOnHub(t, h, remote, id)
	equal(t, "the report line once the hub is back", line, "report: wrote this machine's report to reports/"+id+" on the hub at "+short(fourth))
	named(t, doc["library"], "name", "format")
	equal(t, "exit of the serve that prints text", text.close(), 0)
	equal(t, "warnings of the report that could not be written", strings.Count(text.stderr.String(), warning), 1)
	equal(t, "the commits of the report branch", remoteGit(t, h, remote, "rev-list", "--count", fourth), "3")
}

// TestReportWrite: a report that failed is one warning prefixed "machine
// report: " when the reports start failing and none while they go on
// failing, a report the branch held already is nothing, and a failure
// after a report that worked warns again. So does the first failure after
// the machine stopped reporting, which forgets the report its branch held
// too.
func TestReportWrite(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	c := &serveChecks{inv: &invocation{out: &writer{stdout: &stdout, stderr: &stderr}}}
	held := writtenReport{data: []byte("{}\n")}
	for _, step := range []struct {
		name   string
		forget bool // the machine stopped reporting before this report
		w      writtenReport
		err    error
		want   string
	}{
		{"the first failure", false, writtenReport{}, errors.New("cannot reach the hub"), "warning: machine report: cannot reach the hub\n"},
		{"a failure after a failure", false, writtenReport{}, errors.New("cannot reach the hub"), ""},
		{"a failure once the machine reports again", true, writtenReport{}, errors.New("cannot reach the hub"), "warning: machine report: cannot reach the hub\n"},
		{"a report the branch held", false, held, nil, ""},
		{"a failure after a report", false, writtenReport{}, errors.New("the hub rejected it"), "warning: machine report: the hub rejected it\n"},
	} {
		stderr.Reset()
		if step.forget {
			c.forgetReports()
		}
		c.reportWrite(step.w, step.err)
		equal(t, step.name+": stderr", stderr.String(), step.want)
	}
	equal(t, "stdout", stdout.String(), "")
	equal(t, "the report the branch is known to hold after a failure", string(c.reported.data), "")
	c.reportWrite(held, nil)
	c.forgetReports()
	equal(t, "the report the branch is known to hold once the machine no longer reports", string(c.reported.data), "")
}
