package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// asJSON is v as the JSON a test compares.
func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestBuildReport holds what a report says of an inventory: the machine's
// facts, every list sorted with each entry once, an empty list for nothing,
// and no path, scope, description, drift, candidate or pending merge. A
// skill inside a project is no part of it, and the order the scan found
// things in changes nothing.
func TestBuildReport(t *testing.T) {
	t.Parallel()
	on, off := true, false
	machine := scan.Machine{ID: "0123456789abcdef0123456789abcdef", Label: "studio"}
	full := scan.Inventory{
		InstanceID: "instance", ScanCounter: 7, Machine: machine,
		Configurations: []scan.Configuration{
			{ID: "codex", Client: "codex", Name: "Codex", Path: "/home/you/.codex", Enabled: false, Placement: "library", ReadsLibrary: true},
			{ID: "claude-code", Client: "claude-code", Name: "Claude Code", Path: "/home/you/.claude", Enabled: true, Placement: "symlink"},
		},
		Skills: []scan.Skill{
			{Name: "pdf", Description: "Read a PDF", ContentHash: "bbbb", Occurrences: []scan.Occurrence{
				{Configuration: "codex", Path: "/home/you/.agents/skills/pdf", Kind: scan.KindDirectory, Scope: scan.ScopeUser},
				{Configuration: "claude-code", Path: "/home/you/.claude/skills/pdf", Kind: scan.KindSymlink, Scope: scan.ScopeUser},
				{Configuration: "claude-code", Path: "/home/you/.claude/other/pdf", Kind: scan.KindSymlink, Scope: scan.ScopeUser},
				{Configuration: "claude-code", Path: "/home/you/work/.claude/skills/pdf", Kind: scan.KindDirectory, Scope: scan.ScopeProject},
			}},
			{Name: "lint", Description: "Lint", ContentHash: "aaaa", Occurrences: []scan.Occurrence{
				{Configuration: "claude-code", Path: "/home/you/.claude/plugins/design-kit/skills/lint", Kind: scan.KindDirectory, Scope: scan.ScopeUser, Plugin: "design-kit"},
			}},
			{Name: "local", ContentHash: "cccc", Occurrences: []scan.Occurrence{
				{Configuration: "claude-code", Path: "/home/you/work/.claude/skills/local", Kind: scan.KindDirectory, Scope: scan.ScopeProject},
			}},
		},
		Library: []scan.LibraryEntry{
			{Name: "pdf", Kind: lineage.KindManaged, LogicalID: "skill:1", Source: "https://github.com/example/skills", Subpath: ptr("skills/pdf"),
				UpstreamCommit: "c0ffee", UpstreamTime: "2026-01-02T03:04:05Z", BaseHash: "b0b0", ContentHash: "bbbb", State: stateModified,
				Drift: []string{driftSourceRemoved}, Candidate: &scan.LibraryCandidate{UpstreamCommit: "f00d", ContentHash: "dddd"}, PendingMerge: true,
				Placements: []scan.LibraryPlacement{
					{Configuration: "codex", Path: "/home/you/.agents/skills/pdf", Mode: modeLibrary, Kind: modeLibrary},
					{Configuration: "claude-code", Path: "/home/you/.claude/skills/pdf", Mode: modeSymlink, Kind: scan.KindSymlink},
					{Configuration: "claude-code", Path: "/home/you/.claude/other/pdf", Mode: modeSymlink, Kind: scan.KindSymlink},
				}, Universal: []string{"codex"}},
			{Name: "notes", Kind: lineage.KindManaged, LogicalID: "skill:2", ForkID: "fork-1", Source: "https://example.com/hub.git",
				Upstream: "https://github.com/example/skills", UpstreamSubpath: ptr(""), ContentHash: "eeee", State: stateCurrent,
				Placements: []scan.LibraryPlacement{}, Universal: []string{"codex"}},
		},
		MCPServers: []scan.Server{{Name: "github", Occurrences: []scan.ServerOccurrence{{Configuration: "claude-code", Command: "npx", Args: []string{"--token", "secret"}}}}},
		Plugins: []scan.Plugin{
			{Name: "design-kit", Marketplace: "example", Version: "1.2.0", Configuration: "cursor", Path: "/home/you/.cursor/plugins/design-kit"},
			{Name: "design-kit", Marketplace: "example", Version: "1.2.0", Configuration: "claude-code", Path: "/home/you/.claude/plugins/design-kit", Enabled: &off, Icon: "data:image/png;base64,AAAA", URL: "https://example.com/design-kit"},
			{Name: "audit", Marketplace: "example", Version: "0.1.0", Configuration: "claude-code", Path: "/home/you/.claude/plugins/audit", Enabled: &on},
		},
		Edges:    []scan.Edge{{From: "a", To: "b"}},
		Warnings: []string{"/home/you/.claude/skills/broken: broken symlink, skipped"},
	}
	shuffled := full
	shuffled.Configurations = slices.Clone(full.Configurations)
	shuffled.Skills = slices.Clone(full.Skills)
	shuffled.Library = slices.Clone(full.Library)
	shuffled.Plugins = slices.Clone(full.Plugins)
	for _, list := range []func(){
		func() { slices.Reverse(shuffled.Configurations) }, func() { slices.Reverse(shuffled.Skills) },
		func() { slices.Reverse(shuffled.Library) }, func() { slices.Reverse(shuffled.Plugins) },
	} {
		list()
	}
	const want = `{"schema_version":1,"machine":{"id":"0123456789abcdef0123456789abcdef","label":"studio","os":"linux","agentx":"1.2.3"},` +
		`"configurations":[{"id":"claude-code","name":"Claude Code","enabled":true,"reads_library":false},{"id":"codex","name":"Codex","enabled":false,"reads_library":true}],` +
		`"skills":[` +
		`{"name":"lint","content_hash":"aaaa","occurrences":[{"configuration":"claude-code","kind":"directory","plugin":"design-kit"}]},` +
		`{"name":"pdf","content_hash":"bbbb","occurrences":[{"configuration":"claude-code","kind":"symlink"},{"configuration":"codex","kind":"directory"}]}],` +
		`"library":[` +
		`{"name":"notes","kind":"managed","logical_id":"skill:2","fork_id":"fork-1","source":"https://example.com/hub.git","upstream":"https://github.com/example/skills","upstream_subpath":"",` +
		`"content_hash":"eeee","state":"current","placements":[],"universal":["codex"]},` +
		`{"name":"pdf","kind":"managed","logical_id":"skill:1","source":"https://github.com/example/skills","subpath":"skills/pdf",` +
		`"upstream_commit":"c0ffee","upstream_time":"2026-01-02T03:04:05Z","base_hash":"b0b0","content_hash":"bbbb","state":"modified",` +
		`"placements":[{"configuration":"claude-code","mode":"symlink","kind":"symlink"},{"configuration":"codex","mode":"library","kind":"library"}],"universal":["codex"]}],` +
		`"plugins":[` +
		`{"name":"audit","marketplace":"example","version":"0.1.0","configuration":"claude-code","enabled":true},` +
		`{"name":"design-kit","marketplace":"example","version":"1.2.0","configuration":"claude-code","enabled":false},` +
		`{"name":"design-kit","marketplace":"example","version":"1.2.0","configuration":"cursor"}]}`
	for _, tc := range []struct {
		name      string
		inventory scan.Inventory
		want      string
	}{
		{"a machine with nothing", scan.Inventory{Machine: machine},
			`{"schema_version":1,"machine":{"id":"0123456789abcdef0123456789abcdef","label":"studio","os":"linux","agentx":"1.2.3"},"configurations":[],"skills":[],"library":[],"plugins":[]}`},
		{"every part of an inventory", full, want},
		{"the same inventory in another order", shuffled, want},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			equal(t, "the report", asJSON(t, buildReport(tc.inventory, "linux", "1.2.3")), tc.want)
		})
	}
}

// TestReportOf holds which report branches a view can use, and the warning
// of each one it cannot, which names the branch: a branch whose name is no
// machine id, a tip with no report file, a file too large, a file that is
// no JSON, one with no schema version, one of a later version and one that
// names another machine. A report this version can read is read whatever
// else it carries, and every list it leaves out is empty.
func TestReportOf(t *testing.T) {
	t.Parallel()
	const id = "0123456789abcdef0123456789abcdef"
	branch := gitx.ReportBranch(id)
	for _, tc := range []struct {
		name   string
		branch string
		data   string
		size   int
		found  bool
		want   string // the machine as JSON, "" for a branch that is skipped
		why    string
	}{
		{"a report with a field and a list this version does not know", branch,
			`{"schema_version":1,"machine":{"id":"` + id + `","label":"studio","arch":"arm64"},"hooks":[{"name":"x"}],"skills":[{"name":"pdf","content_hash":"bbbb"}]}`, 0, true,
			`{"machine":{"id":"` + id + `","label":"studio"},"commit":"","changed_at":"","configurations":[],` +
				`"skills":[{"name":"pdf","content_hash":"bbbb","occurrences":[]}],"library":[],"plugins":[]}`, ""},
		{"a branch named after no machine id", "reports/old/laptop", `{"schema_version":1,"machine":{"id":"old/laptop","label":"x"}}`, 0, true,
			"", "reports/old/laptop on the hub is skipped: its name is not that of a machine id"},
		{"a tip with no report file", branch, "", 0, false, "", "it holds no inventory.json"},
		{"a file too large to be a report", branch, "", documentLimit + 1, true, "", "is larger than"},
		{"a file that is no JSON", branch, "not json", 0, true, "", "its inventory.json is not a report"},
		{"a file with no schema version", branch, `{"machine":{"id":"` + id + `"}}`, 0, true, "", "carries no schema_version"},
		{"a report of a later version", branch, `{"schema_version":2,"machine":{"id":"` + id + `"}}`, 0, true,
			"", "is a report of schema version 2, and this agentx reads version 1"},
		{"a report that names another machine", branch, `{"schema_version":1,"machine":{"id":"fedcba9876543210fedcba9876543210"}}`, 0, true,
			"", "names another machine, fedcba9876543210fedcba9876543210"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, why := reportOf(tc.branch, []byte(tc.data), tc.size, tc.found)
			if tc.want != "" {
				equal(t, "the warning", why, "")
				equal(t, "the machine", asJSON(t, m), tc.want)
				return
			}
			contains(t, "the warning", why, tc.why)
			contains(t, "the warning names the branch", why, tc.branch+" on the hub is skipped: ")
		})
	}
}

// reportOnHub is what the report branch of the machine id on the bare
// repository remote holds: its tip and its one file, read with plain git,
// failing the test when its tree holds anything else.
func reportOnHub(t *testing.T, h *harness, remote, id string) (tip string, doc map[string]any, raw string) {
	t.Helper()
	ref := "refs/heads/" + gitx.ReportBranch(id)
	tip = remoteGit(t, h, remote, "rev-parse", ref)
	equal(t, "the files of "+ref, remoteGit(t, h, remote, "ls-tree", "--name-only", tip), gitx.ReportFile)
	raw = remoteGit(t, h, remote, "cat-file", "blob", tip+":"+gitx.ReportFile) + "\n"
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("%s holds no JSON: %v\n%s", ref, err, raw)
	}
	return tip, doc, raw
}

// named is the entry of list, a list of a report or of a fleet event,
// whose key holds value, failing the test when there is none.
func named(t *testing.T, list any, key, value string) map[string]any {
	t.Helper()
	for _, e := range list.([]any) {
		if entry := e.(map[string]any); entry[key] == value {
			return entry
		}
	}
	t.Fatalf("no entry whose %s is %s in %v", key, value, list)
	return nil
}

// asMachine gives h a machine id of its own, as a second computer has:
// every home the harness builds has the same one.
func (h *harness) asMachine(id string) {
	h.t.Helper()
	writeFile(h.t, filepath.Join(h.agentx, "machine.json"), `{"id": "`+id+`"}`+"\n")
}

// TestMachineReportRefusesWithoutWritingAnything: a machine with no hub
// has nowhere to report to, exit code 6. With a hub, a push the hub does
// not take writes no report, each answer of the hub with its own exit
// code: a branch another writer moved and a push a hook declined are exit
// code 6, a push the host denies, which says what to check first, and one
// whose answer names nothing of the branch exit code 3.
func TestMachineReportRefusesWithoutWritingAnything(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	for _, args := range [][]string{{"machine", "report"}, {"machine", "list"}} {
		out := h.run(args...)
		equal(t, strings.Join(args, " ")+" with no hub: exit", out.exit, 6)
		contains(t, strings.Join(args, " ")+" with no hub", out.stderr, "no hub is set")
	}
	remote := newHubRemote(t, h)
	h.setHub(remote)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	const id = "0123456789abcdef0123456789abcdef"
	ref := "refs/heads/" + gitx.ReportBranch(id)
	path := h.env["PATH"]
	for _, tc := range []struct {
		what, push string
		exit       int
		says       string
		check      string // what a denial says to check first
	}{
		{"moved", `printf 'To hub\n!\tHEAD:` + ref + `\t[rejected] (stale info)\nDone\n'; exit 1`, 6,
			"reports/" + id + " on the hub changed since it was read, so nothing was pushed", ""},
		{"declined", `echo "remote: no pushes on Fridays" >&2
	printf 'To hub\n!\tHEAD:` + ref + `\t[remote rejected] (pre-receive hook declined)\nDone\n'; exit 1`, 6,
			"the hub declined reports/" + id + ": pre-receive hook declined: no pushes on Fridays", ""},
		{"denied", `echo "remote: Permission to me/hub.git denied to someone." >&2
	echo "fatal: unable to access 'https://example.com/me/hub.git/': The requested URL returned error: 403" >&2
	exit 128`, 3, "did not let this machine push reports/" + id, "credential helper"},
		{"no answer for the branch", "exit 0", 3, "whether the hub took reports/" + id + " is not known", ""},
	} {
		stubGit(t, h, `#!/bin/sh
case " $* " in
*" push "*)
	`+tc.push+` ;;
esac
exec `+real+` "$@"
`)
		out := h.run("--json", "machine", "report")
		h.env["PATH"] = path
		equal(t, tc.what+": exit", out.exit, tc.exit)
		e := h.one(out.stdout, "error")
		contains(t, tc.what+": message", e["message"].(string), tc.says)
		if tc.check != "" {
			contains(t, tc.what+": what to check first", e["hint"].(string), tc.check)
		}
		equal(t, tc.what+": machine_report events", len(h.eventsOfType(out.stdout, "machine_report")), 0)
	}
	equal(t, "the hub's report branches", remoteGit(t, h, remote, "for-each-ref", "refs/heads/reports/"), "")
	equal(t, "this machine's tracking refs", h.storeGit("for-each-ref", gitx.TrackingPrefix(hubRemoteName(t, h))+"reports/"), "")
}

// TestMachineReportListShow is an inventory report's way through the
// hub, between two computers and the cloud machine.
//
// The first computer reports: one commit on reports/<its id> whose one
// file names the machine, its configurations, every skill its agent
// clients see and its library, with no path of its disk, written with the
// machine's identity and no operation. A second report of the unchanged
// machine pushes nothing, and one after a change is one more commit on the
// first.
//
// The second computer, which has installed nothing, lists the fleet: with
// --json one fleet event holding what the hub holds, sorted by machine id,
// the cloud machine from its snapshot among them, and as text one row a
// machine, itself first from a scan made now, then the others by label,
// the cloud machine last. It shows the first computer by its label and by
// the start of its id, with each skill's origin. A name no machine of the
// hub has is exit code 5.
func TestMachineReportListShow(t *testing.T) {
	t.Parallel()
	const idA, idB = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	a, s := installHarness(t)
	a.mustRun("config", "set", "label", "studio")
	a.mustRun("skill", "add", s.url, "--all", "--to", "claude-code")
	a.mustRun("skill", "fork", "alpha")
	own := filepath.Join(a.home, ".claude", "skills", "own", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(own), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, own, skill("own", "A skill of Claude Code's own directory"))
	remote := newHubRemote(t, a)
	a.setHub(remote)
	a.mustRun("skill", "publish", "alpha")

	out := a.run("--json", "machine", "report")
	equal(t, "report: exit", out.exit, 0)
	equal(t, "report: events", strings.Join(a.types(a.events(out.stdout)), " "), "machine_report result")
	ev := a.one(out.stdout, "machine_report")
	first, doc, raw := reportOnHub(t, a, remote, idA)
	equal(t, "machine_report", fmt.Sprint(ev["machine"], ev["commit"], ev["pushed"]), fmt.Sprint(idA, first, true))
	equal(t, "the report's keys", keysOf(doc), "configurations,library,machine,plugins,schema_version,skills")
	equal(t, "the report's machine", fmt.Sprint(doc["machine"]), fmt.Sprint(map[string]any{"id": idA, "label": "studio", "os": runtime.GOOS, "agentx": cliVersion}))
	if strings.Contains(raw, filepath.Dir(a.home)) || strings.Contains(raw, `"path"`) || strings.Contains(raw, "description") {
		t.Errorf("the report carries a path or a description:\n%s", raw)
	}
	if !strings.HasSuffix(raw, "\n}\n") || !strings.Contains(raw, "\n  \"machine\": {\n") {
		t.Errorf("the report is not indented JSON with a newline after it:\n%s", raw)
	}
	var configurations, skills []string
	for _, c := range doc["configurations"].([]any) {
		configurations = append(configurations, c.(map[string]any)["id"].(string))
	}
	for _, sk := range doc["skills"].([]any) {
		skills = append(skills, sk.(map[string]any)["name"].(string))
	}
	equal(t, "the report's configurations", strings.Join(configurations, " "), "claude-code codex cortex gemini-cli")
	equal(t, "the report's skills", strings.Join(skills, " "), "alpha beta own")
	alpha, beta := named(t, doc["library"], "name", "alpha"), named(t, doc["library"], "name", "beta")
	equal(t, "alpha's logical asset", alpha["logical_id"], scan.SkillLogicalID(alpha["fork_id"].(string), "", nil, ""))
	equal(t, "beta's logical asset", beta["logical_id"], scan.SkillLogicalID("", s.url, ptr("skills/beta"), ""))
	equal(t, "beta's entry", fmt.Sprint(beta["kind"], beta["source"], beta["subpath"], beta["state"], beta["universal"]),
		fmt.Sprint("managed", s.url, "skills/beta", "current", []any{"codex", "gemini-cli"}))
	equal(t, "the commit's message", remoteGit(t, a, remote, "log", "-1", "--format=%B", first), "Report studio\n\nAgentx-Machine: "+idA)
	equal(t, "the commit's parents", remoteGit(t, a, remote, "log", "-1", "--format=%P", first), "")
	tracking := gitx.TrackingPrefix(hubRemoteName(t, a)) + gitx.ReportBranch(idA)
	equal(t, "the remote-tracking ref", a.ref(tracking), first)

	again := a.run("machine", "report")
	equal(t, "report again: exit", again.exit, 0)
	contains(t, "report again", again.stdout, "reports/"+idA+" on the hub holds this report already, at "+short(first)+", so nothing was pushed")
	equal(t, "the tip after the second report", remoteGit(t, a, remote, "rev-parse", "refs/heads/reports/"+idA), first)
	contains(t, "list on the only machine of the hub", a.mustRun("machine", "list").stdout,
		"\nThe hub holds no other machine. A machine shows here once you run agentx machine report on it, or turn on its report_inventory setting.\n")

	writeFile(t, own, skill("own", "Edited"))
	changed := a.run("machine", "report")
	equal(t, "report after a change: exit", changed.exit, 0)
	tip, _, _ := reportOnHub(t, a, remote, idA)
	contains(t, "report after a change", changed.stdout, "wrote this machine's report to reports/"+idA+" on the hub at "+short(tip))
	equal(t, "the second commit's parents", remoteGit(t, a, remote, "log", "-1", "--format=%P", tip), first)

	a.mustRun("skill", "place", "alpha", "--machine", "cloud", "--to", "claude-code")
	cloudTip := remoteGit(t, a, remote, "rev-parse", "refs/heads/machines/cloud")

	b := newHarness(t)
	b.build(t, fixture{dirs: []string{".cursor"}})
	b.asMachine(idB)
	b.env["AGENTX_HOSTNAME"], b.env["TZ"] = "workbench", "UTC"
	b.setHub(remote)
	b.mustRun("machine", "report")

	out = b.run("--json", "--verbose", "machine", "list")
	equal(t, "list --json: exit", out.exit, 0)
	equal(t, "list --json: events", strings.Join(b.types(b.events(out.stdout)), " "), "fleet result")
	machines := b.one(out.stdout, "fleet")["machines"].([]any)
	var ids []string
	for _, m := range machines {
		ids = append(ids, m.(map[string]any)["machine"].(map[string]any)["id"].(string))
	}
	equal(t, "the fleet's machines", strings.Join(ids, " "), idA+" cloud "+idB)
	fromA, cloud := machines[0].(map[string]any), machines[1].(map[string]any)
	equal(t, "the first machine's commit", fromA["commit"], tip)
	committed, err := strconv.ParseInt(strings.TrimSpace(remoteGit(t, a, remote, "log", "-1", "--format=%ct", tip)), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the first machine's time", fromA["changed_at"], time.Unix(committed, 0).UTC().Format(time.RFC3339))
	equal(t, "the first machine's library", asJSON(t, fromA["library"]), asJSON(t, doc["library"]))
	equal(t, "the cloud machine", asJSON(t, cloud["machine"])+" "+fmt.Sprint(cloud["commit"]), `{"id":"cloud","label":"cloud"} `+cloudTip)
	equal(t, "the cloud machine's alpha", named(t, cloud["library"], "name", "alpha")["logical_id"], alpha["logical_id"])
	equal(t, "list: fetches of a fleet that did not move", fetches(b.run("--verbose", "machine", "list").stderr), 0)

	list := b.run("machine", "list")
	equal(t, "list: exit", list.exit, 0)
	equal(t, "list", list.stdout, "3 machines\n"+
		"workbench (this machine)  scanned now               0 skills  Cursor\n"+
		"studio                    changed "+historyDate(fromA["changed_at"].(string))+"  3 skills  Claude Code, Codex, Cortex Code, Gemini CLI\n"+
		"cloud                     changed "+historyDate(cloud["changed_at"].(string))+"  1 skill   Claude Code\n")

	show := b.run("machine", "show", "studio")
	equal(t, "show: exit", show.exit, 0)
	equal(t, "show", show.stdout, "Label        studio\n"+
		"Machine id   "+idA+"\n"+
		"OS           "+runtime.GOOS+"\n"+
		"agentx       "+cliVersion+"\n"+
		"Last change  "+historyDate(fromA["changed_at"].(string))+"\n"+
		"\nClaude Code (claude-code)  enabled  3 skills\n"+
		"  alpha  library           file://"+remote+" (hub), from "+s.url+"/skills/alpha\n"+
		"  beta   library           "+s.url+"/skills/beta\n"+
		"  own    skills directory\n"+
		"\nCodex (codex)  enabled  2 skills\n"+
		"  alpha  library  file://"+remote+" (hub), from "+s.url+"/skills/alpha\n"+
		"  beta   library  "+s.url+"/skills/beta\n"+
		"\nCortex Code (cortex)  enabled  0 skills\n"+
		"\nGemini CLI (gemini-cli)  enabled  2 skills\n"+
		"  alpha  library  file://"+remote+" (hub), from "+s.url+"/skills/alpha\n"+
		"  beta   library  "+s.url+"/skills/beta\n")
	out = b.run("--json", "machine", "show", "0123")
	equal(t, "show by the start of an id: exit", out.exit, 0)
	equal(t, "show by the start of an id", asJSON(t, b.one(out.stdout, "fleet")["machines"]), asJSON(t, []any{fromA}))

	gone := b.run("machine", "show", "laptop")
	equal(t, "show of a machine with no report: exit", gone.exit, 5)
	contains(t, "show of a machine with no report", gone.stderr, "the hub holds no report of a machine called laptop")
}

// TestRemovingTheHubDeletesThisMachinesReport: source remove of the hub
// deletes this machine's report branch there, once the hub is no longer
// set. A hub git cannot reach is removed all the same, exit code 0, with a
// warning that names the branch left there and the git command that
// deletes it.
func TestRemovingTheHubDeletesThisMachinesReport(t *testing.T) {
	t.Parallel()
	const id = "0123456789abcdef0123456789abcdef"
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	remote := newHubRemote(t, h)
	url := "file://" + remote
	h.setHub(remote)
	h.mustRun("machine", "report")
	out := h.run("source", "remove", url)
	equal(t, "remove: exit", out.exit, 0)
	equal(t, "remove", out.stdout, "✓ the hub "+url+" is no longer set; your skills on this machine are as they were\n"+
		"✓ deleted this machine's report, reports/"+id+", from "+url+"\n")
	equal(t, "the hub's report branches", remoteGit(t, h, remote, "for-each-ref", "refs/heads/reports/"), "")

	h.setHub(remote)
	h.mustRun("machine", "report")
	if err := os.Rename(remote, remote+".away"); err != nil {
		t.Fatal(err)
	}
	out = h.run("--json", "source", "remove", url)
	equal(t, "remove of a hub out of reach: exit", out.exit, 0)
	equal(t, "remove of a hub out of reach: events", strings.Join(h.types(h.events(out.stdout)), " "), "source result")
	warnings := h.eventsOfType(out.stderr, "log")
	equal(t, "remove of a hub out of reach: warnings", len(warnings), 1)
	message := warnings[0]["message"].(string)
	contains(t, "the warning", message, url+" still holds reports/"+id+", this machine's report: ")
	if sources, _ := readSettingsFile(t, h)["sources"].([]any); len(sources) != 0 {
		t.Errorf("source remove left the entry: %v", readSettingsFile(t, h)["sources"])
	}
}

// TestMachineListSkipsABranchItCannotUse: a report branch no view can use
// and a cloud machine whose snapshot cannot be read are each one warning
// and fail nothing, and the machines beside them are listed.
func TestMachineListSkipsABranchItCannotUse(t *testing.T) {
	t.Parallel()
	const bad = "fedcba9876543210fedcba9876543210"
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	remote := newHubRemote(t, h)
	h.setHub(remote)
	h.mustRun("machine", "report")
	hub := gitx.New(h.env, false, func(string, ...any) {})
	for branch, file := range map[string][2]string{
		gitx.ReportBranch(bad):      {gitx.ReportFile, `{"schema_version":2,"machine":{"id":"` + bad + `"}}`},
		gitx.MachineBranch("cloud"): {gitx.SnapshotFile, `{"schema_version":4}`},
	} {
		tree, err := hub.FileTree(context.Background(), remote, file[0], []byte(file[1]))
		if err != nil {
			t.Fatal(err)
		}
		commit := remoteGit(t, h, remote, "commit-tree", tree, "-m", "by hand")
		remoteGit(t, h, remote, "update-ref", "refs/heads/"+branch, strings.TrimSpace(commit))
	}

	out := h.run("--json", "machine", "list")
	equal(t, "list: exit", out.exit, 0)
	equal(t, "the fleet", len(h.one(out.stdout, "fleet")["machines"].([]any)), 1)
	var warnings []string
	for _, e := range h.eventsOfType(out.stderr, "log") {
		warnings = append(warnings, e["message"].(string))
	}
	slices.Sort(warnings)
	equal(t, "the warnings", strings.Join(warnings, "\n"),
		"reports/"+bad+" on the hub is skipped: its inventory.json is a report of schema version 2, and this agentx reads version 1; upgrade agentx here to read it\n"+
			"the cloud machine is not listed: machines/cloud is not an agentx snapshot: it carries no settings")
}
