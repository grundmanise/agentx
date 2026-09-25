package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cursor reads Claude Code's skills directory as well as its own, so once
// both hold a skill Cursor sees it through two paths. The text of a command
// that placed the skill shows one row per client, the placement at that
// client's own place, while the library_skill event still lists every path
// the rescan found. These tests pin both halves.

// pathsOf renders the placements of a skill event as configuration and path,
// one entry each, in the order the event lists them.
func pathsOf(t *testing.T, ev jsonEvent) []string {
	t.Helper()
	var got []string
	for _, p := range ev["placements"].([]any) {
		p := p.(map[string]any)
		got = append(got, p["configuration"].(string)+" "+p["path"].(string))
	}
	return got
}

// TestSkillPlaceShowsOneRowPerClient places a skill into Cursor on a machine
// where Claude Code already holds it, as the help center says to turn a copy
// back into a symlink. The text shows one row for Cursor, its own placement,
// and no second one for the path it sees through Claude Code's directory,
// which is Claude Code's placement. The event of the same placement still
// lists both paths Cursor sees the skill through.
func TestSkillPlaceShowsOneRowPerClient(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t, ".cursor")
	h.mustRun("skill", "add", s.url, "--skill", "alpha")
	h.mustRun("skill", "remove", "alpha", "--from", "cursor")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")

	out := h.mustRun("skill", "place", "alpha", "--to", "cursor")
	equal(t, "stdout", out.stdout, "✓ placed alpha in 1 configuration\n"+
		"  cursor  symlink  "+cursor+" -> "+lib+"\n"+
		"  always available to universal clients: codex, gemini-cli\n")

	h.mustRun("skill", "remove", "alpha", "--from", "cursor")
	placed := h.mustRun("--json", "skill", "place", "alpha", "--to", "cursor")
	equal(t, "the paths of the event", strings.Join(pathsOf(t, h.one(placed.stdout, "library_skill")), ";"),
		"cursor "+claude+";cursor "+cursor)
}

// TestSkillAddShowsOneRowPerClient installs a skill into Claude Code and
// Cursor. Each gets one row, its own placement, and the line above the rows
// counts the rows, so the two agree, while the event lists the three paths
// the rescan found, Cursor's view of Claude Code's placement among them.
func TestSkillAddShowsOneRowPerClient(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t, ".cursor")
	lib := filepath.Join(h.library, "alpha")
	claude := filepath.Join(h.home, ".claude", "skills", "alpha")
	cursor := filepath.Join(h.home, ".cursor", "skills", "alpha")

	out := h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--to", "cursor")
	header, rows, ok := strings.Cut(out.stdout, "\n")
	if !ok || !strings.HasPrefix(header, "✓ installed alpha from ") {
		t.Fatalf("stdout does not open with the install:\n%s", out.stdout)
	}
	if !strings.HasSuffix(header, ": 2 placements") {
		t.Errorf("the install line does not count the two rows: %q", header)
	}
	equal(t, "rows", rows, "  claude-code  symlink  "+claude+" -> "+lib+"\n"+
		"  cursor       symlink  "+cursor+" -> "+lib+"\n"+
		"  always available to universal clients: codex, gemini-cli\n")

	j, js := universalHarness(t, ".cursor")
	installed := j.mustRun("--json", "skill", "add", js.url, "--skill", "alpha", "--to", "claude-code", "--to", "cursor")
	jclaude := filepath.Join(j.home, ".claude", "skills", "alpha")
	equal(t, "the paths of the event", strings.Join(pathsOf(t, j.one(installed.stdout, "library_skill")), ";"),
		"claude-code "+jclaude+";cursor "+jclaude+";cursor "+filepath.Join(j.home, ".cursor", "skills", "alpha"))
}

// TestSkillListCountsEachClientOnce: after an install into every client the
// listing counts one placement per client, the number the install line
// gave, although Cursor sees the skill through two paths and the listing's
// event names both.
func TestSkillListCountsEachClientOnce(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t, ".cursor")
	installed := h.mustRun("skill", "add", s.url, "--skill", "alpha")
	header, _, _ := strings.Cut(installed.stdout, "\n")
	if !strings.HasSuffix(header, ": 4 placements") {
		t.Errorf("the install line does not count one placement per client: %q", header)
	}

	list := h.mustRun("skill", "list")
	equal(t, "stdout", list.stdout, "1 skill\n  alpha  managed  current  "+s.url+"/skills/alpha  4 placements\n")

	listed := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")
	equal(t, "placements of the event", strings.Join(placementsOf(t, listed), ";"),
		"claude-code symlink symlink;codex library library;cursor symlink symlink;cursor symlink symlink;gemini-cli library library")
}

// TestConfigEnablePlaceAllShowsOneRowPerSkill fills Cursor with a library
// Claude Code already holds. Each skill gets one row, its placement in
// Cursor's own skills directory, while each skill's event still lists the
// path Cursor sees it through in Claude Code's directory as well.
func TestConfigEnablePlaceAllShowsOneRowPerSkill(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t, ".cursor")
	h.mustRun("config", "disable", "cursor")
	h.mustRun("skill", "add", s.url, "--all")

	out := h.mustRun("config", "enable", "cursor", "--place-all")
	cursor := filepath.Join(h.home, ".cursor", "skills")
	equal(t, "stdout", out.stdout, "✓ cursor is now enabled\n"+
		"✓ placed 2 skills in cursor\n"+
		"  alpha  symlink  "+filepath.Join(cursor, "alpha")+" -> "+filepath.Join(h.library, "alpha")+"\n"+
		"  beta   symlink  "+filepath.Join(cursor, "beta")+" -> "+filepath.Join(h.library, "beta")+"\n")

	j, js := universalHarness(t, ".cursor")
	j.mustRun("config", "disable", "cursor")
	j.mustRun("skill", "add", js.url, "--all")
	enabled := j.mustRun("--json", "config", "enable", "cursor", "--place-all")
	for _, ev := range j.eventsOfType(enabled.stdout, "library_skill") {
		name := ev["name"].(string)
		equal(t, "the paths of the event of "+name, strings.Join(pathsOf(t, ev), ";"),
			"cursor "+filepath.Join(j.home, ".claude", "skills", name)+";cursor "+filepath.Join(j.home, ".cursor", "skills", name))
	}
}

// TestPlacingShowsWhereAClientStillSeesASkillItCouldNotPlace: when Cursor's
// own path holds a directory of the user's, placing the skill there is
// skipped, yet Cursor still sees the skill through Claude Code's skills
// directory. With nothing of the skill at its own place, Cursor keeps the
// row of the path it does see it through, so the text never leaves out a
// client that sees the skill: after skill place, and after config enable
// --place-all.
func TestPlacingShowsWhereAClientStillSeesASkillItCouldNotPlace(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t, ".cursor")
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	handMade := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(handMade, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(handMade, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")
	row := "symlink  " + filepath.Join(h.home, ".claude", "skills", "alpha") + " -> " + filepath.Join(h.library, "alpha") + "\n"

	out := h.mustRun("skill", "place", "alpha", "--to", "cursor")
	contains(t, "stderr", out.stderr, handMade+" is not this skill and was left as it is; no placement was made for cursor")
	equal(t, "stdout", out.stdout, "✓ placed alpha in 0 configurations, 1 placement skipped\n"+
		"  cursor  "+row+
		"  always available to universal clients: codex, gemini-cli\n")

	h.mustRun("config", "disable", "cursor")
	all := h.mustRun("config", "enable", "cursor", "--place-all")
	contains(t, "stderr of --place-all", all.stderr, handMade+" is not this skill and was left as it is")
	if !strings.HasSuffix(all.stdout, "1 placement skipped\n  alpha  "+row) {
		t.Errorf("--place-all does not end with the row of the path Cursor sees alpha through:\n%s", all.stdout)
	}
}

// TestPlacingShowsWhereAClientSeesASkillBesideACopyItKept: a copy copy_mode
// records for Cursor that is different from the library is kept and
// skipped, and the rescan does not find it as the skill, since it does not
// hold the library's version, so Cursor has nothing of the skill at its own
// place. Where Claude Code holds the skill, Cursor still sees it through
// Claude Code's skills directory, and that path is Cursor's one row, as for
// a directory of the user's, after skill place and after config enable
// --place-all. Where no other client holds it, Cursor has no row at all.
func TestPlacingShowsWhereAClientSeesASkillBesideACopyItKept(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what   string
		claude bool
	}{
		{"Claude Code holds the skill", true},
		{"no other client holds the skill", false},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := universalHarness(t, ".cursor")
			h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy")
			if c.claude {
				h.mustRun("skill", "place", "alpha", "--to", "claude-code")
			}
			kept := filepath.Join(h.home, ".cursor", "skills", "alpha")
			editCopy(t, kept)
			warning, _ := keptCopyLines(kept, "alpha", "agentx skill remove alpha --from cursor", "agentx skill place alpha --to cursor --copy")
			var cursorRow, alphaRow string
			if c.claude {
				row := "copy  " + filepath.Join(h.home, ".claude", "skills", "alpha") + " -> " + filepath.Join(h.library, "alpha") + "\n"
				cursorRow, alphaRow = "  cursor  "+row, "  alpha  "+row
			}

			out := h.mustRun("skill", "place", "alpha", "--to", "cursor")
			contains(t, "stderr", out.stderr, warning)
			equal(t, "stdout", out.stdout, "✓ placed alpha in 0 configurations, 1 placement skipped\n"+
				cursorRow+
				"  always available to universal clients: codex, gemini-cli\n")

			h.mustRun("config", "disable", "cursor")
			all := h.mustRun("config", "enable", "cursor", "--place-all")
			contains(t, "stderr of --place-all", all.stderr, warning)
			seen := "0 skills"
			if c.claude {
				seen = "1 skill"
			}
			equal(t, "stdout of --place-all", all.stdout, "✓ cursor is now enabled\n"+
				"✓ placed "+seen+" in cursor, 1 placement skipped\n"+
				alphaRow)
		})
	}
}
