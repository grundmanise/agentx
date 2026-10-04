package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestSkillHistoryMarksImportsAndForeignCommits forks an edited managed
// skill, records an edit as a publish does and commits one more with git directly
// in the worktree, beside the skill directory too, and lists the fork's
// history: every commit newest first, the one made with git foreign, with
// its author and its whole message, the two agentx wrote carrying the
// machine, and the import at the root, with every file it holds added.
// Paths are relative to the skill's directory, a file beside it starting
// with ../. A managed skill, an unmanaged one and a name the library does
// not hold are refused, each in its own words.
func TestSkillHistoryMarksImportsAndForeignCommits(t *testing.T) {
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
		imported, foreign                        bool
	}{
		{foreign, committed, "By hand", "", "../README.md added,extra.md modified", false, true},
		{committed, created, "alpha: add extra.md (test-host)", machine, "extra.md added", false, false},
		{created, imported, h.accountGit("log", "-1", "--format=%s", created), machine, "SKILL.md modified", false, false},
		{imported, "", h.accountGit("log", "-1", "--format=%s", imported), "", strings.Join(importFiles, ","), true, false},
	} {
		ev := events[i]
		equal(t, "event "+want.subject+": commit", ev["commit"], want.commit)
		var parents []string
		for _, p := range ev["parents"].([]any) {
			parents = append(parents, p.(string))
		}
		equal(t, want.subject+": parents", strings.Join(parents, " "), want.parents)
		equal(t, want.subject+": subject", ev["subject"], want.subject)
		equal(t, want.subject+": import", ev["import"], want.imported)
		equal(t, want.subject+": foreign", ev["foreign"], want.foreign)
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
	equal(t, "the foreign commit's author", events[0]["author"], h.accountGit("log", "-1", "--format=%an <%ae>", foreign))
	equal(t, "the foreign commit's message", events[0]["message"], "By hand\n\nWith a body.")
	equal(t, "the result", h.one(out.stdout, "result")["summary"], "alpha has 4 commits")

	lines := strings.Split(strings.TrimSuffix(h.mustRun("skill", "history", "alpha").stdout, "\n"), "\n")
	equal(t, "text lines", len(lines), 4)
	contains(t, "the foreign line", lines[0], short(foreign)+"  ")
	contains(t, "the foreign line", lines[0], "  By hand  foreign  2 files")
	contains(t, "the commit line", lines[1], "  alpha: add extra.md (test-host)  1 file")
	contains(t, "the import line", lines[3], "  import  "+plural(len(importFiles), "file"))

	for _, c := range []struct {
		name, message string
		exit          int
	}{
		{"beta", "beta is managed, not a fork, so it has no history of its own", 6},
		{"mine", "mine is not a fork, so it has no history", 6},
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
