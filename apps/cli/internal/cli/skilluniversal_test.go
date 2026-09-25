package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// universalHarness is a machine with one source added and nothing installed
// yet, whose clients are the two kinds a placement can be: Claude Code keeps
// a skills directory of its own and gets a symlink, while Codex and Gemini
// CLI are universal clients, which read the library itself and see every
// skill in it. extra adds configuration directories a test needs besides.
func universalHarness(t *testing.T, extra ...string) (*harness, *sourceRepo) {
	t.Helper()
	h := newHarness(t)
	h.build(t, fixture{dirs: append([]string{".claude", ".codex", ".gemini"}, extra...)})
	s, _, _ := h.standardSource(true)
	if out := h.run("source", "add", s.url); out.exit != 0 {
		t.Fatalf("source add: exit %d\n%s", out.exit, out.stderr)
	}
	return h, s
}

// universalOf renders the universal clients a skill event names, joined with
// commas. A missing field fails the test rather than reading as none: an
// event that names no universal client says so with an empty list.
func universalOf(t *testing.T, ev jsonEvent) string {
	t.Helper()
	ids, ok := ev["universal"].([]any)
	if !ok {
		t.Fatalf("the event carries no universal list: %v", ev)
	}
	got := make([]string, 0, len(ids))
	for _, id := range ids {
		got = append(got, id.(string))
	}
	return strings.Join(got, ",")
}

// TestSkillAddNamesTheUniversalClients: an install tells the user which
// clients will see the skill whatever they chose, in the text, the result
// and the library_skill event alike. Codex is named when it is disabled and
// when --to leaves it out, since it reads the library and the skill is in
// the library either way, while the placements stay those of the
// configurations the install covered.
func TestSkillAddNamesTheUniversalClients(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what       string
		disable    string
		to         []string
		placements string
	}{
		{what: "a disabled universal client", disable: "codex",
			placements: "claude-code symlink symlink;gemini-cli library library"},
		{what: "universal clients --to left out", to: []string{"--to", "claude-code"},
			placements: "claude-code symlink symlink"},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			for _, asJSON := range []bool{true, false} {
				h, s := universalHarness(t)
				if c.disable != "" {
					equal(t, "disable", h.run("config", "disable", c.disable).exit, 0)
				}
				args := append([]string{"skill", "add", s.url, "--skill", "alpha"}, c.to...)
				if asJSON {
					args = append([]string{"--json"}, args...)
				}
				out := h.run(args...)
				equal(t, "exit", out.exit, 0)
				nothingAt(t, "a placement in a universal client's own directory", filepath.Join(h.home, ".codex", "skills", "alpha"))
				if !asJSON {
					contains(t, "stdout", out.stdout, "\n  always available to universal clients: codex, gemini-cli\n")
					continue
				}
				ev := h.one(out.stdout, "library_skill")
				equal(t, "universal", universalOf(t, ev), "codex,gemini-cli")
				equal(t, "placements", strings.Join(placementsOf(t, ev), ";"), c.placements)
				summary := h.one(out.stdout, "result")["summary"].(string)
				if !strings.HasSuffix(summary, "; always available to universal clients: codex, gemini-cli") {
					t.Errorf("the summary does not end naming the universal clients: %q", summary)
				}
			}
		})
	}
}

// TestSkillAddNamesEveryUniversalClientOfABatch: each skill of a bulk
// install is available to the same universal clients, and each skill's
// event and block of text says so; the result names them once for the run.
func TestSkillAddNamesEveryUniversalClientOfABatch(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	out := h.run("--json", "skill", "add", s.url, "--all", "--to", "claude-code")
	equal(t, "exit", out.exit, 0)
	events := h.eventsOfType(out.stdout, "library_skill")
	equal(t, "library_skill events", len(events), 2)
	for _, ev := range events {
		equal(t, "universal of "+ev["name"].(string), universalOf(t, ev), "codex,gemini-cli")
	}
	summary := h.one(out.stdout, "result")["summary"].(string)
	equal(t, "universal clauses in the summary", strings.Count(summary, "universal"), 1)

	other, second := universalHarness(t)
	text := other.run("skill", "add", second.url, "--all", "--to", "claude-code")
	equal(t, "exit", text.exit, 0)
	equal(t, "universal lines", strings.Count(text.stdout, "\n  always available to universal clients: codex, gemini-cli\n"), 2)
}

// TestSkillPlaceNamesTheUniversalClients: placing a skill later names the
// universal clients as an install does, Codex included while it is disabled
// and not among the configurations --to names.
func TestSkillPlaceNamesTheUniversalClients(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "gemini-cli").exit, 0)
	equal(t, "disable", h.run("config", "disable", "codex").exit, 0)

	out := h.run("--json", "skill", "place", "alpha", "--to", "claude-code")
	equal(t, "exit", out.exit, 0)
	target, ok := isSymlink(t, filepath.Join(h.home, ".claude", "skills", "alpha"))
	if !ok || target != filepath.Join(h.library, "alpha") {
		t.Errorf("the placement is %q (symlink %v), want a link to the library", target, ok)
	}
	ev := h.one(out.stdout, "library_skill")
	equal(t, "universal", universalOf(t, ev), "codex,gemini-cli")
	equal(t, "placements", strings.Join(placementsOf(t, ev), ";"), "claude-code symlink symlink")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "placed alpha in 1 configuration; always available to universal clients: codex, gemini-cli")

	// The text says the same, once the rows are done. The placement is
	// already right by now, which changes nothing about who sees the skill.
	text := h.run("skill", "place", "alpha", "--to", "claude-code")
	equal(t, "exit", text.exit, 0)
	equal(t, "stdout", text.stdout, "✓ placed alpha in 1 configuration\n"+
		"  claude-code  symlink  "+filepath.Join(h.home, ".claude", "skills", "alpha")+" -> "+filepath.Join(h.library, "alpha")+"\n"+
		"  always available to universal clients: codex, gemini-cli\n")
}

// TestSkillAddNamesNoUniversalClientWhereThereIsNone: a machine with no
// universal client installed has nobody to name. The event says so with an
// empty list, and the text and the result say nothing about it.
func TestSkillAddNamesNoUniversalClientWhereThereIsNone(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s, _, _ := h.standardSource(true)
	equal(t, "source add", h.run("source", "add", s.url).exit, 0)

	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha")
	equal(t, "exit", out.exit, 0)
	equal(t, "universal", universalOf(t, h.one(out.stdout, "library_skill")), "")
	if summary := h.one(out.stdout, "result")["summary"].(string); strings.Contains(summary, "universal") {
		t.Errorf("the summary names universal clients on a machine with none: %q", summary)
	}
	text := h.run("skill", "place", "alpha", "--to", "claude-code")
	equal(t, "exit", text.exit, 0)
	if strings.Contains(text.stdout, "universal") {
		t.Errorf("the text names universal clients on a machine with none:\n%s", text.stdout)
	}
}

// TestSkillRemoveFromUniversalIsTheWholeRemoval: removing a skill from the
// universal clients is removing the library entry they read, which takes
// the skill from every client. It is the removal without --from, by name:
// every placement, the library directory, the import branch, its candidate
// ref and its copy-mode entries go, in one mutation, and the output is the
// output of that removal word for word.
func TestSkillRemoveFromUniversalIsTheWholeRemoval(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--copy").exit, 0)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code")
	head := strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	h.accountGit("update-ref", "refs/agentx/candidate/alpha", head)
	before := mutationVersion(t, h)

	out := h.run("--json", "skill", "remove", "alpha", "--from", "universal")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "the copy placement", filepath.Join(h.home, ".claude", "skills", "alpha"))
	nothingAt(t, "the library directory", filepath.Join(h.library, "alpha"))
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("the import branch is still there")
	}
	equal(t, "refs under refs/agentx", h.agentxRefs(), source.Ref(source.ID(s.url)))
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals left behind", journalCount(t, h), 0)
	equal(t, "library_skill events", len(h.eventsOfType(out.stdout, "library_skill")), 0)
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "removed alpha from the library, 3 placements and its import branch")

	// The text of --from universal, given twice, which is one request, is
	// the text of a removal without --from on a machine in the same state.
	text := func(from ...string) string {
		t.Helper()
		m, ms := universalHarness(t)
		equal(t, "add", m.run("skill", "add", ms.url, "--skill", "alpha", "--copy").exit, 0)
		out := m.run(append([]string{"skill", "remove", "alpha"}, from...)...)
		equal(t, "exit", out.exit, 0)
		return strings.ReplaceAll(out.stdout, m.home, "~")
	}
	named := text("--from", "universal", "--from", "universal")
	equal(t, "the text of --from universal", named, text())
	contains(t, "the text of --from universal", named, "✓ removed alpha from the library: 3 placements\n")
	// Each universal client has a row of its own, as the help center shows.
	for _, row := range []string{"  codex        library  ~/.agents/skills/alpha\n", "  gemini-cli   library  ~/.agents/skills/alpha\n"} {
		contains(t, "the text of --from universal", named, row)
	}
}

// TestSkillRemoveFromUniversalRefusesAFork: --from universal runs the whole
// removal and so answers a fork as that removal does, before anything is
// planned.
func TestSkillRemoveFromUniversalRefusesAFork(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	h.accountGit("update-ref", "refs/heads/skills/alpha", head)
	before := mutationVersion(t, h)

	out := h.run("skill", "remove", "alpha", "--from", "universal")
	equal(t, "exit", out.exit, 6)
	contains(t, "stderr", out.stderr, "alpha is a fork on this machine")
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("a refused removal took the library directory: %v", err)
	}
	equal(t, "the fork branch", strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/skills/alpha")), head)
	equal(t, "mutations", mutationVersion(t, h), before)
}

// TestSkillRemoveFromUniversalStandsAlone: --from universal beside a client
// that is not universal is a usage error, as --all beside --skill is. The
// whole removal already takes the skill from every client, so a list beside
// it means the user expected it to do less, and nothing is removed on a
// guess. Beside a universal client it is that client's refusal instead, see
// TestSkillRemoveRefusesAUniversalClient.
//
// Which clients are universal is read from the machine, so the usage error
// comes after what is read before it: a name the library does not hold is
// exit code 5 first, as it is for any removal.
func TestSkillRemoveFromUniversalStandsAlone(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	before := mutationVersion(t, h)

	for _, from := range [][]string{
		{"--from", "universal", "--from", "claude-code"},
		{"--from", "claude-code", "--from", "universal"},
	} {
		out := h.run(append([]string{"skill", "remove", "alpha"}, from...)...)
		equal(t, "exit", out.exit, 1)
		contains(t, "stderr", out.stderr, "--from universal and --from claude-code cannot both be given")
		contains(t, "the hint", out.stderr, standsAloneHint)
	}
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("a refused removal touched the library: %v", err)
	}
	if _, ok := isSymlink(t, filepath.Join(h.home, ".claude", "skills", "alpha")); !ok {
		t.Error("a refused removal took a placement away")
	}
	equal(t, "mutations", mutationVersion(t, h), before)

	unknown := h.run("skill", "remove", "beta", "--from", "universal", "--from", "claude-code")
	equal(t, "exit for a name the library does not hold", unknown.exit, 5)
}

// standsAloneHint is the hint --from universal beside a client that is not
// universal is refused with.
const standsAloneHint = "hint: --from universal asks for the removal without --from; drop it to remove only the placements you name\n"

// TestSkillRemoveFromUniversalStandsAloneForAFork: the usage error is the
// same for a fork, and its hint holds for one too. It is decided before the
// refs are read, so it does not claim that --from universal takes the skill
// from every client, which for a fork it does not: that removal is refused.
// Dropping --from universal, as the hint says, leads to a removal that
// works.
func TestSkillRemoveFromUniversalStandsAloneForAFork(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	h.accountGit("update-ref", "refs/heads/skills/alpha", head)
	before := mutationVersion(t, h)

	out := h.run("skill", "remove", "alpha", "--from", "universal", "--from", "claude-code")
	equal(t, "exit", out.exit, 1)
	contains(t, "stderr", out.stderr, "error: --from universal and --from claude-code cannot both be given\n"+standsAloneHint)
	if strings.Contains(out.stderr, "every client") {
		t.Errorf("the hint says --from universal takes a fork from every client, which it refuses to:\n%s", out.stderr)
	}
	contains(t, "the library directory", fileBody(t, filepath.Join(h.library, "alpha", "SKILL.md")), "name: alpha")
	if target, ok := isSymlink(t, filepath.Join(h.home, ".claude", "skills", "alpha")); !ok || target != filepath.Join(h.library, "alpha") {
		t.Errorf("the claude-code placement is %q (symlink %v), want the link to the library", target, ok)
	}
	equal(t, "mutations", mutationVersion(t, h), before)

	equal(t, "exit of what the hint leads to", h.run("skill", "remove", "alpha", "--from", "claude-code").exit, 0)
	nothingAt(t, "the claude-code placement", filepath.Join(h.home, ".claude", "skills", "alpha"))
}

// TestSkillRemoveFromASymlinkedClient: --from naming a client that is not
// universal takes that client's placement and nothing else. The universal
// clients keep the skill, since the library keeps it, and the event says
// so.
func TestSkillRemoveFromASymlinkedClient(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")

	out := h.run("--json", "skill", "remove", "alpha", "--from", "claude-code")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "the claude-code placement", filepath.Join(h.home, ".claude", "skills", "alpha"))
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("the library directory was removed by a --from removal: %v", err)
	}
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)
	ev := h.one(out.stdout, "library_skill")
	equal(t, "placements left in claude-code", len(placementsOf(t, ev)), 0)
	equal(t, "universal", universalOf(t, ev), "codex,gemini-cli")
	equal(t, "summary", h.one(out.stdout, "result")["summary"], "removed alpha from claude-code: 1 placement")
}

// TestSkillRemoveRefusesAUniversalClient: a universal client reads the
// library entry itself, so the skill cannot be taken from it alone. --from
// naming one is refused, alone, disabled, among other configurations and
// beside --from universal, and the hint offers the removal that can be
// done, --from universal, with every configuration it would take the skill
// from, in id order: the universal clients and Claude Code, whose symlink or
// recorded copy agentx would delete, and not Windsurf, whose directory is
// the user's and would stay.
//
// Beside --from universal the refusal is this one and not the usage error
// --from universal beside another client gets: that error's hint, to drop
// --from universal, would only lead here.
//
// The refusal is decided before the lock, the journal and any placement:
// it is given while another command holds the lock, and afterwards the
// library, the refs, the placements and the settings are as they were.
func TestSkillRemoveRefusesAUniversalClient(t *testing.T) {
	t.Parallel()
	const codexAlone = "codex reads the library directly, so alpha cannot be removed from it alone"
	cases := []struct {
		what    string
		disable string
		from    []string
		refusal string
	}{
		{what: "alone", from: []string{"codex"}, refusal: codexAlone},
		{what: "disabled", disable: "codex", from: []string{"codex"}, refusal: codexAlone},
		{what: "after a client that is not universal", from: []string{"claude-code", "codex"}, refusal: codexAlone},
		{what: "before a client that is not universal", from: []string{"gemini-cli", "claude-code"},
			refusal: "gemini-cli reads the library directly, so alpha cannot be removed from it alone"},
		{what: "with the other universal client", from: []string{"gemini-cli", "codex"},
			refusal: "codex, gemini-cli read the library directly, so alpha cannot be removed from them alone"},
		{what: "after --from universal", from: []string{"universal", "codex"}, refusal: codexAlone},
		{what: "before --from universal", from: []string{"codex", "universal"}, refusal: codexAlone},
		{what: "with --from universal and a client that is not universal", from: []string{"universal", "codex", "claude-code"},
			refusal: codexAlone},
	}
	for _, mode := range []string{modeSymlink, modeCopy} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			for _, c := range cases {
				t.Run(c.what, func(t *testing.T) {
					t.Parallel()
					h, s := universalHarness(t, ".codeium/windsurf")
					if c.disable != "" {
						equal(t, "disable", h.run("config", "disable", c.disable).exit, 0)
					}
					add := []string{"skill", "add", s.url, "--skill", "alpha", "--to", "claude-code"}
					if mode == modeCopy {
						add = append(add, "--copy")
					}
					equal(t, "add", h.run(add...).exit, 0)
					// A directory of the user's own where Windsurf would have
					// its placement: the whole removal leaves it, so the hint
					// may not say Windsurf loses the skill.
					mine := filepath.Join(h.home, ".codeium", "windsurf", "skills", "alpha")
					if err := os.MkdirAll(mine, 0o755); err != nil {
						t.Fatal(err)
					}
					writeFile(t, filepath.Join(mine, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")
					head := strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/managed/alpha"))
					h.accountGit("update-ref", "refs/agentx/candidate/alpha", head)
					settings := fileBody(t, filepath.Join(h.agentx, "settings.json"))
					before := mutationVersion(t, h)

					args := []string{"skill", "remove", "alpha"}
					for _, id := range c.from {
						args = append(args, "--from", id)
					}
					release := holdLock(t, h)
					out := h.run(args...)
					js := h.run(append([]string{"--json"}, args...)...)
					release()
					equal(t, "exit", out.exit, 6)
					contains(t, "stderr", out.stderr, "error: "+c.refusal+"\n")
					contains(t, "stderr", out.stderr, "hint: take alpha off the machine with 'agentx skill remove alpha --from universal', "+
						"which removes it from claude-code, codex, gemini-cli\n")
					equal(t, "exit of --json", js.exit, 6)
					ev := h.one(js.stdout, "error")
					equal(t, "code", ev["code"], "refused")
					contains(t, "the hint", ev["hint"].(string), "--from universal")

					// Nothing moved: the library, the refs, the placements, the
					// settings, the counter.
					contains(t, "the library directory", fileBody(t, filepath.Join(h.library, "alpha", "SKILL.md")), "name: alpha")
					claude := filepath.Join(h.home, ".claude", "skills", "alpha")
					if mode == modeCopy {
						contains(t, "the claude-code copy", fileBody(t, filepath.Join(claude, "SKILL.md")), "name: alpha")
					} else if target, ok := isSymlink(t, claude); !ok || target != filepath.Join(h.library, "alpha") {
						t.Errorf("the claude-code placement is %q (symlink %v), want the link to the library", target, ok)
					}
					contains(t, "the user's directory", fileBody(t, filepath.Join(mine, "SKILL.md")), "description: mine")
					equal(t, "the import branch", strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/managed/alpha")), head)
					equal(t, "the candidate ref", strings.TrimSpace(h.accountGit("rev-parse", "refs/agentx/candidate/alpha")), head)
					equal(t, "the settings", fileBody(t, filepath.Join(h.agentx, "settings.json")), settings)
					equal(t, "mutations", mutationVersion(t, h), before)
					equal(t, "journals", journalCount(t, h), 0)
				})
			}
		})
	}
}

// TestSkillRemoveRefusalNamesAClientReadingAnotherClientsDirectory: the
// hint of a refused --from names every client the removal it offers takes
// the skill from, and that includes a client that sees the skill through
// another client's skills directory. Cursor reads Claude Code's, so the link
// or the recorded copy agentx made there is how Cursor sees the skill, --to
// having left Cursor out, and the removal takes the skill from Cursor too.
func TestSkillRemoveRefusalNamesAClientReadingAnotherClientsDirectory(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{modeSymlink, modeCopy} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			h, s := universalHarness(t, ".cursor")
			add := []string{"skill", "add", s.url, "--skill", "alpha", "--to", "claude-code"}
			if mode == modeCopy {
				add = append(add, "--copy")
			}
			equal(t, "add", h.run(add...).exit, 0)
			nothingAt(t, "a placement of Cursor's own", filepath.Join(h.home, ".cursor", "skills", "alpha"))
			before := mutationVersion(t, h)

			out := h.run("skill", "remove", "alpha", "--from", "codex")
			equal(t, "exit", out.exit, 6)
			contains(t, "stderr", out.stderr, "hint: take alpha off the machine with 'agentx skill remove alpha --from universal', "+
				"which removes it from claude-code, codex, cursor, gemini-cli\n")
			contains(t, "the library directory", fileBody(t, filepath.Join(h.library, "alpha", "SKILL.md")), "name: alpha")
			equal(t, "mutations", mutationVersion(t, h), before)

			// The hint's removal does what it says: nothing Cursor reads
			// holds the skill any more.
			equal(t, "exit of --from universal", h.run("skill", "remove", "alpha", "--from", "universal").exit, 0)
			nothingAt(t, "the claude-code placement Cursor read", filepath.Join(h.home, ".claude", "skills", "alpha"))
			nothingAt(t, "the library directory", filepath.Join(h.library, "alpha"))
		})
	}
}

// TestSkillRemoveRefusalNamesAClientLinkedThroughTheLibrary: a link of the
// user's that leads into the library entry, straight or by way of another
// link, stays when the skill is taken off the machine, since agentx did not
// make it, but it no longer leads anywhere, so its client loses the skill
// and the hint of a refused --from names it. Claude Code sees alpha here
// only through a chain of the user's links, and Cursor, which reads Claude
// Code's and Codex's directories, through that chain or through a link in
// Codex's own directory.
//
// A link that reaches the skill's files without going through the library
// entry is not counted: when the entry is itself a link to a directory of
// the user's, a link straight to that directory still works once the entry
// is gone. A chain of the user's links that passes through such an entry is
// counted all the same, since the chain stops at the entry the removal
// deletes and not at the directory beyond it.
//
// The removal the hint offers says the same of each link it leaves: that
// it no longer leads to the skill, or, for a link that still works, that its
// client still sees it.
func TestSkillRemoveRefusalNamesAClientLinkedThroughTheLibrary(t *testing.T) {
	t.Parallel()
	const hint = "hint: take alpha off the machine with 'agentx skill remove alpha --from universal', which removes it from "

	t.Run("a chain of links in a client's own directory", func(t *testing.T) {
		t.Parallel()
		h, s := universalHarness(t, ".cursor")
		equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "gemini-cli").exit, 0)
		place, target := userLink(t, h, ".claude", "alpha")
		before := mutationVersion(t, h)

		out := h.run("skill", "remove", "alpha", "--from", "codex")
		equal(t, "exit", out.exit, 6)
		contains(t, "stderr", out.stderr, hint+"claude-code, codex, cursor, gemini-cli\n")
		contains(t, "the library directory", fileBody(t, filepath.Join(h.library, "alpha", "SKILL.md")), "name: alpha")
		equal(t, "mutations", mutationVersion(t, h), before)

		// The hint's removal does what it says: the user's link stays, and
		// leads nowhere any more, which is what the removal says of it too.
		removed := h.run("skill", "remove", "alpha", "--from", "universal")
		equal(t, "exit of --from universal", removed.exit, 0)
		stillTheirs(t, "the user's link", place, target)
		if _, err := os.Stat(place); err == nil {
			t.Errorf("%s still leads to a skill once the library entry is gone", place)
		}
		contains(t, "the warning", removed.stderr, place+" points at "+target+", not at ")
		contains(t, "the warning", removed.stderr, "and was left as it is; it no longer leads to alpha\n")
		if strings.Contains(removed.stderr, "claude-code still sees it") {
			t.Errorf("the removal says Claude Code still sees alpha through a link it left leading nowhere:\n%s", removed.stderr)
		}
	})

	t.Run("a link in a universal client's own directory", func(t *testing.T) {
		t.Parallel()
		h, s := universalHarness(t, ".cursor")
		equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "gemini-cli").exit, 0)
		mine := filepath.Join(h.home, ".codex", "skills", "alpha")
		if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(h.library, "alpha"), mine); err != nil {
			t.Fatal(err)
		}

		out := h.run("skill", "remove", "alpha", "--from", "codex")
		equal(t, "exit", out.exit, 6)
		contains(t, "stderr", out.stderr, hint+"codex, cursor, gemini-cli\n")
	})

	t.Run("a link beside a library entry that is a link", func(t *testing.T) {
		t.Parallel()
		h, _ := universalHarness(t, ".cursor")
		dev := filepath.Join(h.home, "dev", "alpha")
		if err := os.MkdirAll(dev, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dev, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")
		for _, link := range []string{filepath.Join(h.library, "alpha"), filepath.Join(h.home, ".claude", "skills", "alpha")} {
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(dev, link); err != nil {
				t.Fatal(err)
			}
		}

		out := h.run("skill", "remove", "alpha", "--from", "codex")
		equal(t, "exit", out.exit, 6)
		contains(t, "stderr", out.stderr, hint+"codex, gemini-cli\n")

		// And the removal indeed leaves Claude Code, and Cursor through it,
		// seeing the user's directory, and says so.
		removed := h.run("skill", "remove", "alpha", "--from", "universal")
		equal(t, "exit of --from universal", removed.exit, 0)
		contains(t, "the user's directory through their link",
			fileBody(t, filepath.Join(h.home, ".claude", "skills", "alpha", "SKILL.md")), "description: mine")
		contains(t, "the warning", removed.stderr, "and was left as it is; claude-code still sees it\n")
	})

	t.Run("a chain of links through a library entry that is a link", func(t *testing.T) {
		t.Parallel()
		h, _ := universalHarness(t, ".cursor")
		dev := filepath.Join(h.home, "dev", "alpha")
		if err := os.MkdirAll(dev, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dev, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")
		if err := os.MkdirAll(h.library, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dev, filepath.Join(h.library, "alpha")); err != nil {
			t.Fatal(err)
		}
		place, target := userLink(t, h, ".claude", "alpha")

		out := h.run("skill", "remove", "alpha", "--from", "codex")
		equal(t, "exit", out.exit, 6)
		contains(t, "stderr", out.stderr, hint+"claude-code, codex, cursor, gemini-cli\n")

		// The chain went through the library entry, so it breaks there,
		// although the directory the entry named is still the user's.
		removed := h.run("skill", "remove", "alpha", "--from", "universal")
		equal(t, "exit of --from universal", removed.exit, 0)
		stillTheirs(t, "the user's link", place, target)
		if _, err := os.Stat(place); err == nil {
			t.Errorf("%s still leads to a skill once the library entry is gone", place)
		}
		contains(t, "the user's directory", fileBody(t, filepath.Join(dev, "SKILL.md")), "description: mine")
		contains(t, "the warning", removed.stderr, "and was left as it is; it no longer leads to alpha\n")
	})
}

// TestSkillRemoveRefusesAUniversalClientOfAFork: a fork cannot be taken off
// the machine by this command, --from universal included, so the refusal of
// a universal client offers no --from universal: following it would only be
// refused again. The hint says instead that the universal clients see the
// fork until the fork itself is removed, which is not this command, and
// that the other clients can still lose it. Nothing moves.
func TestSkillRemoveRefusesAUniversalClientOfAFork(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)
	head := strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/managed/alpha"))
	h.accountGit("update-ref", "refs/heads/skills/alpha", head)
	before := mutationVersion(t, h)

	for _, from := range [][]string{{"codex"}, {"claude-code", "codex"}, {"universal", "codex"}} {
		args := []string{"skill", "remove", "alpha"}
		for _, id := range from {
			args = append(args, "--from", id)
		}
		out := h.run(args...)
		equal(t, "exit of "+strings.Join(from, " "), out.exit, 6)
		contains(t, "stderr", out.stderr, "error: codex reads the library directly, so alpha cannot be removed from it alone\n")
		contains(t, "stderr", out.stderr, "hint: alpha is a fork on this machine: every universal client sees it through the library entry "+
			"until the fork itself is removed, which is not this command; take it from the other clients with "+
			"'agentx skill remove alpha --from <configuration>'\n")
		for _, offer := range []string{"--from universal", "which removes it from"} {
			if strings.Contains(out.stderr, offer) {
				t.Errorf("the refusal for a fork offers %q, which refuses a fork too:\n%s", offer, out.stderr)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(h.library, "alpha", "SKILL.md")); err != nil {
		t.Errorf("a refused removal took the library directory: %v", err)
	}
	if _, ok := isSymlink(t, filepath.Join(h.home, ".claude", "skills", "alpha")); !ok {
		t.Error("a refused removal took the claude-code placement away")
	}
	equal(t, "the fork branch", strings.TrimSpace(h.accountGit("rev-parse", "refs/heads/skills/alpha")), head)
	equal(t, "mutations", mutationVersion(t, h), before)
}

// TestSkillListAndPlaceAllNameTheUniversalClients: every library_skill event
// names the universal clients, whichever command emits it, the listing and
// enabling a configuration with --place-all included, and a disabled
// universal client is named with the others. The placements of --place-all
// stay those of the configuration it enabled.
func TestSkillListAndPlaceAllNameTheUniversalClients(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t)
	equal(t, "disable codex", h.run("config", "disable", "codex").exit, 0)
	equal(t, "disable claude-code", h.run("config", "disable", "claude-code").exit, 0)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha").exit, 0)

	list := h.run("--json", "skill", "list")
	equal(t, "exit of skill list", list.exit, 0)
	ev := h.one(list.stdout, "library_skill")
	equal(t, "universal of skill list", universalOf(t, ev), "codex,gemini-cli")
	equal(t, "placements of skill list", strings.Join(placementsOf(t, ev), ";"), "codex library library;gemini-cli library library")

	out := h.run("--json", "config", "enable", "claude-code", "--place-all")
	equal(t, "exit of --place-all", out.exit, 0)
	ev = h.one(out.stdout, "library_skill")
	equal(t, "placements of --place-all", strings.Join(placementsOf(t, ev), ";"), "claude-code symlink symlink")
	equal(t, "universal of --place-all", universalOf(t, ev), "codex,gemini-cli")
}

// TestEveryClientReadingTheLibraryIsUniversal: Codex and Gemini CLI are not
// the only universal clients. Every registered client whose skills directory
// is the library is one, Warp among them: an install names it with the
// others, and --from naming it is refused as it is for Codex.
func TestEveryClientReadingTheLibraryIsUniversal(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t, ".warp")
	out := h.run("--json", "skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	equal(t, "exit", out.exit, 0)
	equal(t, "universal", universalOf(t, h.one(out.stdout, "library_skill")), "codex,gemini-cli,warp")

	refused := h.run("skill", "remove", "alpha", "--from", "warp")
	equal(t, "exit of --from warp", refused.exit, 6)
	contains(t, "stderr", refused.stderr, "error: warp reads the library directly, so alpha cannot be removed from it alone\n")
	contains(t, "stderr", refused.stderr, "which removes it from claude-code, codex, gemini-cli, warp\n")
	contains(t, "the library directory", fileBody(t, filepath.Join(h.library, "alpha", "SKILL.md")), "name: alpha")
}

// TestUniversalClientsOfASkillWhoseSourceWasRemoved: removing a source
// changes nothing of who sees the skills installed from it. The universal
// clients still read the library entry, so placing the skill names them
// beside its source removed, and the snapshot entry says what the listing
// says. Removing the skill from one of them is still refused, with the hint
// that offers the whole removal, and --from universal is that removal: it
// takes the import branch too, which was the last record of the removed
// source's URL, so the source's id is from then on refused as any other id
// is, with the hint to list the sources rather than the command that adds
// the source again.
func TestUniversalClientsOfASkillWhoseSourceWasRemoved(t *testing.T) {
	t.Parallel()
	h, s := universalHarness(t, ".cursor")
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	h.mustRun("source", "remove", s.url)

	placed := h.mustRun("--json", "skill", "place", "alpha", "--to", "cursor")
	ev := h.one(placed.stdout, "library_skill")
	equal(t, "drift of skill place", drift(ev), "source removed")
	equal(t, "universal of skill place", universalOf(t, ev), "codex,gemini-cli")
	equal(t, "summary of skill place", h.one(placed.stdout, "result")["summary"],
		"placed alpha in 1 configuration; always available to universal clients: codex, gemini-cli")
	listed := h.librarySkill(h.mustRun("--json", "skill", "list").stdout, "alpha")
	equal(t, "universal of skill list", universalOf(t, listed), "codex,gemini-cli")
	entry := h.snapshotLibrary("alpha")
	equal(t, "drift of the snapshot", drift(entry), "source removed")
	equal(t, "universal of the snapshot", universalOf(t, entry), "codex,gemini-cli")

	branch := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	refused := h.run("skill", "remove", "alpha", "--from", "codex")
	equal(t, "exit of --from codex", refused.exit, 6)
	contains(t, "stderr", refused.stderr, "hint: take alpha off the machine with 'agentx skill remove alpha --from universal', "+
		"which removes it from claude-code, codex, cursor, gemini-cli\n")
	contains(t, "the library directory", fileBody(t, filepath.Join(h.library, "alpha", "SKILL.md")), "name: alpha")
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), branch)

	// While alpha records the source, its id is refused with the command
	// that adds the source again.
	id := source.ID(s.url)
	recorded := h.run("--json", "skill", "add", id, "--skill", "alpha")
	equal(t, "exit while alpha records the source", recorded.exit, 5)
	equal(t, "hint while alpha records the source", h.one(recorded.stdout, "error")["hint"], removedHint(s.url))

	out := h.run("--json", "skill", "remove", "alpha", "--from", "universal")
	equal(t, "exit of --from universal", out.exit, 0)
	equal(t, "summary of --from universal", h.one(out.stdout, "result")["summary"],
		"removed alpha from the library, 4 placements and its import branch")
	nothingAt(t, "the library directory", filepath.Join(h.library, "alpha"))
	if _, err := h.accountGitErr("rev-parse", "--verify", "refs/heads/managed/alpha"); err == nil {
		t.Error("the import branch is still there")
	}

	gone := h.run("--json", "skill", "add", id, "--skill", "alpha")
	equal(t, "exit once nothing records the source", gone.exit, 5)
	e := h.one(gone.stdout, "error")
	equal(t, "message once nothing records the source", e["message"], "no source with id "+id)
	equal(t, "hint once nothing records the source", e["hint"], "run 'agentx source list' to see the sources")
}

// TestNoConfigurationIsCalledUniversal: --from universal names the universal
// clients together, so no client the registry knows may have that id, or
// asking to remove a skill from that one client would take it off the
// machine. The skills CLI's universal pseudo-agent is left out of the
// registry for this, among other reasons.
func TestNoConfigurationIsCalledUniversal(t *testing.T) {
	t.Parallel()
	for _, slug := range scan.Slugs() {
		if slug == fromUniversal {
			t.Errorf("a registered client is called %q, which --from reserves for the universal clients together", slug)
		}
	}
}
