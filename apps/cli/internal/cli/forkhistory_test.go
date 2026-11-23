package cli

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillHistoryListsEveryCommit forks an edited managed
// skill, records an edit as a publish does and commits one more with git directly
// in the worktree, beside the skill directory too, and lists the fork's
// history: every commit newest first, the one made with git without a machine, with
// its author and its whole message, the two agentx wrote carrying the
// machine, and the import at the root, with every file it holds added.
// Paths are relative to the skill's directory, a file beside it starting
// with ../. The creation commit alone names where the fork came from, the
// source of the import it was made from. A managed skill, an unmanaged one
// and a name the library does not hold are refused, each in its own words.
func TestSkillHistoryListsEveryCommit(t *testing.T) {
	t.Parallel()
	h, s := installHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha", "--name", "beta")
	writeFile(t, mkdirs(t, filepath.Join(h.library, "mine"), "SKILL.md"), skill("mine", "mine"))
	imported := h.ref(lineage.ManagedRef("alpha"))
	writeFile(t, filepath.Join(h.library, "alpha", "SKILL.md"), skill("alpha", "Edited before the fork"))
	h.mustRun("skill", "fork", "alpha")
	created := h.ref(lineage.ForkRef("alpha"))
	writeFile(t, filepath.Join(h.library, "alpha", "extra.md"), "extra\n")
	h.record("alpha")
	committed := h.ref(lineage.ForkRef("alpha"))
	root := filepath.Join(h.agentx, "worktrees", "alpha")
	writeFile(t, filepath.Join(h.library, "alpha", "extra.md"), "extra, by hand\n")
	writeFile(t, filepath.Join(root, "README.md"), "readme\n")
	gitIn(t, h, root, "add", "-A")
	gitIn(t, h, root, "commit", "-q", "-m", "By hand\n\nWith a body.")
	foreign := h.ref(lineage.ForkRef("alpha"))
	machine, _, err := home.MachineID(h.agentx, h.env, nil)
	if err != nil {
		t.Fatal(err)
	}

	out := h.mustRun("--json", "skill", "history", "alpha")
	events := h.eventsOfType(out.stdout, "history")
	if len(events) != 4 {
		t.Fatalf("%d history events, want 4:\n%s", len(events), out.stdout)
	}
	var importFiles []string
	for _, p := range strings.Split(h.accountGit("ls-tree", "-r", "--name-only", imported), "\n") {
		importFiles = append(importFiles, strings.TrimPrefix(p, "alpha/")+" added")
	}
	for i, want := range []struct {
		commit, parents, subject, machine, files string
	}{
		{foreign, committed, "By hand", "", "../README.md added,extra.md modified"},
		{committed, created, "alpha: add extra.md (test-host)", machine, "extra.md added"},
		{created, imported, h.accountGit("log", "-1", "--format=%s", created), machine, "SKILL.md modified"},
		{imported, "", h.accountGit("log", "-1", "--format=%s", imported), "", strings.Join(importFiles, ",")},
	} {
		ev := events[i]
		equal(t, "event "+want.subject+": commit", ev["commit"], want.commit)
		var parents []string
		for _, p := range ev["parents"].([]any) {
			parents = append(parents, p.(string))
		}
		equal(t, want.subject+": parents", strings.Join(parents, " "), want.parents)
		equal(t, want.subject+": subject", ev["subject"], want.subject)
		if want.machine == "" {
			if _, ok := ev["machine"]; ok {
				t.Errorf("%s: machine %v, want none", want.subject, ev["machine"])
			}
		} else {
			equal(t, want.subject+": machine", ev["machine"], want.machine)
		}
		var files []string
		for _, f := range ev["files"].([]any) {
			files = append(files, f.(map[string]any)["path"].(string)+" "+f.(map[string]any)["status"].(string))
		}
		equal(t, want.subject+": files", strings.Join(files, ","), want.files)
		if _, err := time.Parse(time.RFC3339, ev["time"].(string)); err != nil {
			t.Errorf("%s: time %q is not RFC 3339: %v", want.subject, ev["time"], err)
		}
	}
	for i, ev := range events {
		if _, ok := ev["origin"]; ok != (i == 2) {
			t.Errorf("event %d: origin %v", i, ev["origin"])
		}
	}
	origin := events[2]["origin"].(map[string]any)
	equal(t, "the origin's kind", origin["kind"], "source")
	equal(t, "the origin's source", origin["source"], h.trailer(imported, lineage.TrailerSource))
	equal(t, "the origin's skill", origin["skill"], "alpha")
	equal(t, "the foreign commit's author", events[0]["author"], h.accountGit("log", "-1", "--format=%an <%ae>", foreign))
	equal(t, "the foreign commit's message", events[0]["message"], "By hand\n\nWith a body.")
	equal(t, "the result", h.one(out.stdout, "result")["summary"], "alpha has 4 commits")
	for i, want := range []struct {
		kind      string
		generated bool
	}{
		{historyGit, false},
		{historyEdit, true},
		{historyFork, true},
		{historyUpstreamKind, false},
	} {
		ev := events[i]
		equal(t, ev["subject"].(string)+": kind", ev["kind"], want.kind)
		equal(t, ev["subject"].(string)+": generated", ev["generated"] == true, want.generated)
	}
	// The commits agentx wrote name their kind, so the kind is read, not
	// guessed from the subject.
	for _, i := range []int{1, 2} {
		contains(t, fmt.Sprintf("event %d's message", i), events[i]["message"].(string), lineage.TrailerKind+": "+events[i]["kind"].(string)+"\n")
	}
	upstream := events[2]["upstream"].(map[string]any)
	equal(t, "the fork's upstream commit", upstream["commit"], h.trailer(imported, lineage.TrailerCommit))
	equal(t, "the fork's upstream time", upstream["time"], events[3]["time"])
	for _, i := range []int{0, 1, 3} {
		if _, ok := events[i]["upstream"]; ok {
			t.Errorf("event %d: upstream %v", i, events[i]["upstream"])
		}
	}

	lines := strings.Split(strings.TrimSuffix(h.mustRun("skill", "history", "alpha").stdout, "\n"), "\n")
	equal(t, "text lines", len(lines), 4)
	contains(t, "the foreign line", lines[0], short(foreign)+"  ")
	contains(t, "the foreign line", lines[0], "  By hand  2 files")
	contains(t, "the commit line", lines[1], "  alpha: add extra.md (test-host)  1 file")
	contains(t, "the import line", lines[3], "  "+plural(len(importFiles), "file"))

	for _, c := range []struct {
		name, message string
		exit          int
	}{
		{"beta", "beta is not a skill of the account remote, so it has no history of its own", 6},
		{"mine", "mine is not a skill of the account remote, so it has no history", 6},
		{"nowhere", `the library holds no skill called "nowhere"`, 5},
	} {
		out := h.run("--json", "skill", "history", c.name)
		equal(t, c.name+": exit", out.exit, c.exit)
		contains(t, c.name+": message", h.one(out.stdout, "error")["message"].(string), c.message)
	}
}

// TestParseForkLog reads git log's output for skill history: commits with
// and without files, a root commit, and paths holding the bytes a field
// separator or a line feed would be.
func TestParseForkLog(t *testing.T) {
	t.Parallel()
	commit := func(id, parents, body string) string {
		return "\x01" + id + "\x00" + parents + "\x00" + "2026-10-02T10:00:00+02:00\x00" + "A <a@example.com>\x00" + body + "\x00"
	}
	for _, tc := range []struct {
		name, out string
		want      []forkLogCommit
	}{
		{"none", "", nil},
		{"a merge, an empty commit and a root", commit("m", "a b", "Merge\n") + "\x00\nM\x00d/SKILL.md\x00A\x00d/new.md\x00" +
			commit("e", "r", "Empty\n") + "\x00" +
			commit("r", "", "Import\n\nAgentx-Source: x\n") + "\x00\nA\x00d/SKILL.md\x00",
			[]forkLogCommit{
				{id: "m", parents: []string{"a", "b"}, files: []changedFile{{"d/SKILL.md", diffModified}, {"d/new.md", diffAdded}}},
				{id: "e", parents: []string{"r"}, files: []changedFile{}},
				{id: "r", parents: []string{}, files: []changedFile{{"d/SKILL.md", diffAdded}}},
			}},
		{"paths with odd bytes", commit("c", "p", "Odd\n") + "\x00\nD\x00d/\x01x\x00T\x00d/line\nfeed\x00",
			[]forkLogCommit{{id: "c", parents: []string{"p"}, files: []changedFile{{"d/\x01x", diffDeleted}, {"d/line\nfeed", diffModified}}}}},
	} {
		got, err := parseForkLog(tc.out)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		for i := range got {
			got[i].time, got[i].author, got[i].message = "", "", ""
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %#v, want %#v", tc.name, got, tc.want)
		}
	}
	if _, err := parseForkLog(commit("c", "p", "Cut\n") + "\x00\nM\x00"); err == nil {
		t.Error("a status with no path was read")
	}
}

// TestHistoryOrigin reads where a fork came from off its creation commit:
// the origin its trailers record, of a plugin's skill or another skill,
// else the source and directory of the import commit it was made from,
// with the web URL of a source an https or SSH URL names, and nothing for
// a commit that creates no fork or a creation commit that records neither.
func TestHistoryOrigin(t *testing.T) {
	t.Parallel()
	const forkID = "01234567-89ab-4def-8123-456789abcdef"
	upstream := strings.Repeat("a", 40)
	imported := func(src, path string) *lineage.Commit {
		i := lineage.Import{Source: src, Path: path, Commit: upstream, Hash: strings.Repeat("b", 64)}
		return &lineage.Commit{ID: "i", Message: i.Message()}
	}
	created := lineage.ForkTrailers{ForkID: forkID}
	for _, tc := range []struct {
		name   string
		t      lineage.ForkTrailers
		parent *lineage.Commit
		want   *forkOrigin
	}{
		{"a plugin's skill", lineage.ForkTrailers{ForkID: forkID, Origin: lineage.Origin{Skill: "format", Plugin: "formatter", Marketplace: "acme", URL: "https://github.com/acme/formatter"}}, nil,
			&forkOrigin{Kind: originPlugin, Skill: "format", Plugin: "formatter", Marketplace: "acme", URL: "https://github.com/acme/formatter"}},
		{"another skill", lineage.ForkTrailers{ForkID: forkID, Origin: lineage.Origin{Skill: "alpha"}}, &lineage.Commit{ID: "f", Parents: []string{"i"}, Message: "x\n"},
			&forkOrigin{Kind: originSkill, Skill: "alpha"}},
		{"a source's skill", created, imported("https://github.com/acme/skills", "skills/pdf"),
			&forkOrigin{Kind: originSource, Skill: "pdf", Source: "https://github.com/acme/skills", Path: "skills/pdf", URL: "https://github.com/acme/skills",
				SkillURL: "https://github.com/acme/skills/tree/" + upstream + "/skills/pdf"}},
		{"a skill at an SSH source's root", created, imported("ssh://git@gitlab.com/acme/pdf", ""),
			&forkOrigin{Kind: originSource, Skill: "pdf", Source: "ssh://git@gitlab.com/acme/pdf", URL: "https://gitlab.com/acme/pdf",
				SkillURL: "https://gitlab.com/acme/pdf/-/tree/" + upstream}},
		{"a local source's skill", created, imported("file:///srv/skills", "pdf"),
			&forkOrigin{Kind: originSource, Skill: "pdf", Source: "file:///srv/skills", Path: "pdf"}},
		{"no fork id", lineage.ForkTrailers{Machine: strings.Repeat("0", 32)}, imported("https://github.com/acme/skills", "pdf"), nil},
		{"a creation commit that records none", created, &lineage.Commit{ID: "f", Parents: []string{"i"}, Message: "Fork alpha from beta\n"}, nil},
		{"skill new's", created, nil, nil},
	} {
		if got := historyOrigin(tc.t, tc.parent); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: historyOrigin = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestDescribeHistory tells what each commit agentx writes on a fork's
// branch did to the skill, from its trailers, its parents and its subject:
// the creation of a fork, of skill new's and skill import's skill, a
// rename, an update that merged an import, a merge of another machine's
// edits, and recorded edits, with the machine's label and whether the
// subject is agentx's own. A message the user gave keeps Generated false.
func TestDescribeHistory(t *testing.T) {
	t.Parallel()
	const forkID = "01234567-89ab-4def-8123-456789abcdef"
	machine := lineage.ForkTrailers{Machine: strings.Repeat("0", 32)}
	created := lineage.ForkTrailers{ForkID: forkID, Machine: machine.Machine}
	upstream := strings.Repeat("a", 40)
	imp := lineage.Import{Source: "https://github.com/acme/skills", Path: "pdf", Commit: upstream, Hash: strings.Repeat("b", 64)}
	imported := &forkLogCommit{id: "i", parents: []string{}, time: "2026-09-16T10:00:00+00:00", message: imp.Message()}
	tip := &forkLogCommit{id: "t", parents: []string{"i"}, message: "x\n"}
	wantUpstream := &historyUpstream{Source: imp.Source, Path: "pdf", Commit: upstream, Time: imported.time, URL: imp.Source,
		SkillURL: "https://github.com/acme/skills/tree/" + upstream + "/pdf"}
	for _, tc := range []struct {
		name, subject string
		t             lineage.ForkTrailers
		parents       []string
		first, second *forkLogCommit
		kind          string
		generated     bool
		renamedFrom   string
		upstream      *historyUpstream
	}{
		// A commit that names its kind is told by it, whatever its subject.
		{name: "a fork", subject: "Fork pdf-letters from https://github.com/acme/skills/pdf at aaaaaaa", t: kinded(created, historyFork),
			parents: []string{"i"}, first: imported, kind: historyFork, generated: true, upstream: wantUpstream},
		{name: "a fork of a plugin's skill", subject: "Fork fmt from plugin formatter", t: kinded(created, historyFork), kind: historyFork, generated: true},
		{name: "skill new's", subject: "Create notes", t: kinded(created, historyCreate), kind: historyCreate, generated: true},
		{name: "skill import's", subject: "Adopt notes", t: kinded(created, historyAdopt), kind: historyAdopt, generated: true},
		{name: "a rename", subject: "Rename notes to jot", t: kinded(machine, historyRename), parents: []string{"t"}, first: tip,
			kind: historyRename, generated: true, renamedFrom: "notes"},
		{name: "an update", subject: "pdf-letters: merge upstream aaaaaaa (laptop)", t: kinded(machine, historyUpdate), parents: []string{"t", "i"},
			first: tip, second: imported, kind: historyUpdate, generated: true, upstream: wantUpstream},
		{name: "another machine's edits", subject: "pdf-letters: merge the account remote (studio mac)", t: kinded(machine, historySync), parents: []string{"t", "o"},
			first: tip, kind: historySync, generated: true},
		{name: "recorded edits", subject: "pdf-letters: edit SKILL.md and add examples.md (laptop)", t: kinded(machine, historyEdit), parents: []string{"t"}, first: tip,
			kind: historyEdit, generated: true},
		{name: "a kind this agentx does not know", subject: "Pick from main", t: kinded(machine, "cherry-pick"), parents: []string{"t"}, first: tip,
			kind: "cherry-pick", generated: true},
		// One without, a message the user gave or an older commit, is told by
		// its parents, and its subject is not agentx's.
		{name: "edits with the user's message", subject: "Tighten the examples", t: machine, parents: []string{"t"}, first: tip, kind: historyEdit},
		{name: "an update the user committed", subject: "Take the new examples", t: machine, parents: []string{"t", "i"}, first: tip, second: imported,
			kind: historyUpdate, upstream: wantUpstream},
		{name: "a merge the user committed", subject: "Merge my laptop's edits", t: machine, parents: []string{"t", "o"}, first: tip, kind: historySync},
		{name: "an older rename", subject: "Rename pdf-notes to pdf-letters", t: machine, parents: []string{"t"}, first: tip, kind: historyEdit},
		{name: "an older fork", subject: "Fork pdf-letters from https://github.com/acme/skills/pdf at aaaaaaa", t: created, parents: []string{"i"},
			first: imported, kind: historyEdit},
	} {
		ev := historyEvent{Name: "pdf-letters", Subject: tc.subject, Message: tc.subject, Parents: tc.parents}
		describeHistory(&ev, tc.t, tc.first, tc.second)
		equal(t, tc.name+": kind", ev.Kind, tc.kind)
		equal(t, tc.name+": generated", ev.Generated, tc.generated)
		equal(t, tc.name+": renamed from", ev.RenamedFrom, tc.renamedFrom)
		if !reflect.DeepEqual(ev.Upstream, tc.upstream) {
			t.Errorf("%s: upstream %+v, want %+v", tc.name, ev.Upstream, tc.upstream)
		}
	}
}

// kinded is t with the kind the Agentx-Kind trailer names.
func kinded(t lineage.ForkTrailers, kind string) lineage.ForkTrailers {
	t.Kind = kind
	return t
}
