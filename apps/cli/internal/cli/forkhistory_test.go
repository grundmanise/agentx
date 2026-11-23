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
	for _, p := range strings.Split(h.homeGit("ls-tree", "-r", "--name-only", imported), "\n") {
		importFiles = append(importFiles, strings.TrimPrefix(p, "alpha/")+" added")
	}
	for i, want := range []struct {
		commit, parents, subject, machine, files string
	}{
		{foreign, committed, "By hand", "", "../README.md added,extra.md modified"},
		{committed, created, "alpha: add extra.md (test-host)", machine, "extra.md added"},
		{created, imported, h.homeGit("log", "-1", "--format=%s", created), machine, "SKILL.md modified"},
		{imported, "", h.homeGit("log", "-1", "--format=%s", imported), "", strings.Join(importFiles, ",")},
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
	equal(t, "the foreign commit's author", events[0]["author"], h.homeGit("log", "-1", "--format=%an <%ae>", foreign))
	equal(t, "the foreign commit's message", events[0]["message"], "By hand\n\nWith a body.")
	equal(t, "the result", h.one(out.stdout, "result")["summary"], "alpha has 4 commits")
	for i, want := range []bool{false, false, false, true} {
		equal(t, fmt.Sprintf("event %d: import", i), events[i]["import"] == true, want)
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
		{"beta", "beta is not a skill of the hub, so it has no history of its own", 6},
		{"mine", "mine is not a skill of the hub, so it has no history", 6},
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
