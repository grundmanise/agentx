package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillPlaceAddsAPlacementLater puts a library skill into a
// configuration the install left out, and changes nothing else.
func TestSkillPlaceAddsAPlacementLater(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "codex").exit, 0)
	nothingAt(t, "a placement the install did not make", filepath.Join(h.home, ".cursor", "skills", "alpha"))
	head := h.accountGit("rev-parse", "refs/heads/managed/alpha")
	before := mutationVersion(t, h)

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	place := filepath.Join(h.home, ".cursor", "skills", "alpha")
	target, ok := isSymlink(t, place)
	if !ok || target != filepath.Join(h.library, "alpha") {
		t.Errorf("the placement is %q (symlink %v), want a link to the library", target, ok)
	}
	equal(t, "the import branch", h.accountGit("rev-parse", "refs/heads/managed/alpha"), head)
	equal(t, "mutations", mutationVersion(t, h), before+1)
	equal(t, "journals left behind", journalCount(t, h), 0)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "")

	ev := h.one(out.stdout, "library_skill")
	equal(t, "kind", ev["kind"], "managed")
	equal(t, "placements", strings.Join(placementsOf(t, ev), ";"), "cursor symlink symlink")
	contains(t, "the result", out.stdout, "placed alpha in 1 configuration")
}

// TestSkillPlaceCopyRecordsTheMode places a copy of the library directory
// and records the configuration in copy_mode, as an install with --copy
// does.
func TestSkillPlaceCopyRecordsTheMode(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "codex").exit, 0)

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor", "--copy")
	equal(t, "exit", out.exit, 0)
	place := filepath.Join(h.home, ".cursor", "skills", "alpha")
	info, err := os.Lstat(place)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the placement is not a copy: %v", err)
	}
	contains(t, "the copy", fileBody(t, filepath.Join(place, "notes.md")), "alpha notes")
	// The executable bit of the source survives the copy, so the copy hashes
	// to the version it was made from.
	script, err := os.Stat(filepath.Join(place, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("the copy is missing the script: %v", err)
	}
	if script.Mode()&0o111 == 0 {
		t.Errorf("the copied script is not executable: %v", script.Mode())
	}
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor")
	equal(t, "placements", strings.Join(placementsOf(t, h.one(out.stdout, "library_skill")), ";"), "cursor copy copy")
	contains(t, "the result", out.stdout, "1 placement as copy")
}

// TestPlacingAgainKeepsARecordedCopy: a configuration copy_mode records for
// the skill keeps its copy when the skill is placed there again without
// --copy, since its client may not follow a symlink, and agentx's own copy
// is not reported as adopted. Otherwise copy_mode would name a copy where a
// symlink stands, and a directory the user later put at that path would be
// one a removal deletes.
func TestPlacingAgainKeepsARecordedCopy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what  string
		again func(t *testing.T, h *harness, s *sourceRepo) outcome
	}{
		{"skill place", func(_ *testing.T, h *harness, _ *sourceRepo) outcome {
			return h.run("--json", "skill", "place", "alpha", "--to", "cursor")
		}},
		{"skill add", func(_ *testing.T, h *harness, s *sourceRepo) outcome {
			return h.run("--json", "skill", "add", s.url, "--skill", "alpha", "--to", "cursor")
		}},
		{"config enable --place-all", func(t *testing.T, h *harness, _ *sourceRepo) outcome {
			equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)
			return h.run("--json", "config", "enable", "cursor", "--place-all")
		}},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy").exit, 0)

			out := c.again(t, h, s)
			equal(t, "exit", out.exit, 0)
			place := filepath.Join(h.home, ".cursor", "skills", "alpha")
			info, err := os.Lstat(place)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("the placement is no longer a copy: %v", err)
			}
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor")
			equal(t, "placements", strings.Join(placementsOf(t, h.one(out.stdout, "library_skill")), ";"), "cursor copy copy")
			if strings.Contains(out.stdout, "placement adopted") {
				t.Errorf("agentx's own copy was reported as adopted:\n%s", out.stdout)
			}
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), "")
		})
	}
}

// TestPlacingAgainKeepsAnEditedCopy: a copy copy_mode records for the
// configuration is agentx's copy even when it no longer holds the library's
// version, and placing the skill there again leaves it byte for byte and
// skips it, since replacing it would lose what it holds. The warning says
// whose copy it is, not that it is some other skill, and the line under it
// how to replace it with the library version. In JSON the one log event
// carries both, since a log event has no hint.
func TestPlacingAgainKeepsAnEditedCopy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what  string
		again func(t *testing.T, h *harness, s *sourceRepo, flags ...string) outcome
	}{
		{"skill place", func(_ *testing.T, h *harness, _ *sourceRepo, flags ...string) outcome {
			return h.run(append(flags, "skill", "place", "alpha", "--to", "cursor")...)
		}},
		{"skill add", func(_ *testing.T, h *harness, s *sourceRepo, flags ...string) outcome {
			return h.run(append(flags, "skill", "add", s.url, "--skill", "alpha", "--to", "cursor")...)
		}},
		{"config enable --place-all", func(t *testing.T, h *harness, _ *sourceRepo, flags ...string) outcome {
			equal(t, "disable", h.run("config", "disable", "cursor").exit, 0)
			return h.run(append(flags, "config", "enable", "cursor", "--place-all")...)
		}},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy")
			place := filepath.Join(h.home, ".cursor", "skills", "alpha")
			editCopy(t, place)
			edited := libraryTree(t, place)

			out := c.again(t, h, s, "--json")
			equal(t, "exit", out.exit, 0)
			sameTree(t, "the copy", libraryTree(t, place), edited)
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor")
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), editedCopyWarning(place))
			contains(t, "the result", h.one(out.stdout, "result")["summary"].(string), ", 1 placement skipped")

			text := c.again(t, h, s)
			equal(t, "exit", text.exit, 0)
			sameTree(t, "the copy", libraryTree(t, place), edited)
			warning, wayOn := keptCopyLines(place, "alpha", "agentx skill remove alpha --from cursor", "agentx skill place alpha --to cursor --copy")
			equal(t, "stderr", text.stderr, "warning: "+warning+"\n  "+wayOn+"\n")
			contains(t, "stdout", text.stdout, ", 1 placement skipped")
		})
	}
}

// TestPlacingAgainLeavesAnEditedCopyOfAnotherConfiguration: a directory is
// agentx's copy only in the configuration copy_mode records it for. The same
// edited copy in the skills directory of another configuration is a
// directory agentx did not place there, one a removal keeps, so its warning
// names neither agentx's copy nor a removal that would not delete it.
func TestPlacingAgainLeavesAnEditedCopyOfAnotherConfiguration(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy")
	recorded := filepath.Join(h.home, ".cursor", "skills", "alpha")
	editCopy(t, recorded)
	other := filepath.Join(h.home, ".copilot", "skills", "alpha")
	copyTree(t, recorded, other)
	edited := libraryTree(t, other)

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor", "--to", "github-copilot")
	equal(t, "exit", out.exit, 0)
	sameTree(t, "the other copy", libraryTree(t, other), edited)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor")
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		editedCopyWarning(recorded)+"\n"+other+" is not this skill and was left as it is; no placement was made for github-copilot")
	contains(t, "the result", h.one(out.stdout, "result")["summary"].(string), ", 2 placements skipped")
}

// TestPlacingAgainLeavesWhatReplacedARecordedCopy: copy_mode records a copy
// for the configuration, but the user has since put a link of their own or
// a file at that path. Neither is agentx's copy, which is a real directory,
// and a removal would not delete either, so each keeps the warning of what
// it is and names no removal; copy_mode still records the configuration.
func TestPlacingAgainLeavesWhatReplacedARecordedCopy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what    string
		replace func(t *testing.T, h *harness, place string)
		warning string
	}{
		{"a link", func(t *testing.T, h *harness, place string) {
			mine := filepath.Join(h.home, "my-skills", "alpha")
			copyTree(t, place, mine)
			if err := os.RemoveAll(place); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(mine, place); err != nil {
				t.Fatal(err)
			}
		}, " is a link of your own and was left as it is; no placement was made for cursor"},
		{"a file", func(t *testing.T, _ *harness, place string) {
			if err := os.RemoveAll(place); err != nil {
				t.Fatal(err)
			}
			writeFile(t, place, "mine\n")
		}, " is not this skill and was left as it is; no placement was made for cursor"},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy")
			place := filepath.Join(h.home, ".cursor", "skills", "alpha")
			c.replace(t, h, place)
			before, err := os.Lstat(place)
			if err != nil {
				t.Fatal(err)
			}

			out := h.run("--json", "skill", "place", "alpha", "--to", "cursor")
			equal(t, "exit", out.exit, 0)
			after, err := os.Lstat(place)
			if err != nil || after.Mode() != before.Mode() || after.Size() != before.Size() {
				t.Errorf("what the user put at the placement path changed: %v", err)
			}
			equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "cursor")
			equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"), place+c.warning)
			contains(t, "the result", h.one(out.stdout, "result")["summary"].(string), ", 1 placement skipped")
		})
	}
}

// TestTheHintForAnEditedCopyTakesTheLibraryVersion runs the commands the
// warning for an edited copy names, as a shell reads them, and ends with the
// library's version in place as a copy again, recorded in copy_mode as the
// copy was: its client may not follow a symlink. The copy is kept whichever
// side changed, the copy or the library directory since the copy was made,
// which is why the warning says only that the copy is different from the
// library and never that it was edited. A name that starts with a dash
// comes after the flags and "--" in both commands, where agentx cannot take
// it for a flag, and a name a shell would split is quoted as one word.
func TestTheHintForAnEditedCopyTakesTheLibraryVersion(t *testing.T) {
	t.Parallel()
	addAlpha := func(t *testing.T, h *harness, s *sourceRepo) {
		h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy")
	}
	for _, c := range []struct {
		what    string
		name    string
		install func(t *testing.T, h *harness, s *sourceRepo)
		edit    func(t *testing.T, h *harness, place string)
		remove  string
		again   string
	}{
		{
			what: "the copy edited", name: "alpha", install: addAlpha,
			edit:   func(t *testing.T, _ *harness, place string) { editCopy(t, place) },
			remove: "agentx skill remove alpha --from cursor", again: "agentx skill place alpha --to cursor --copy",
		},
		{
			what: "the library edited", name: "alpha", install: addAlpha,
			edit: func(t *testing.T, h *harness, _ string) {
				editLibrary(t, h, "alpha", "notes.md", "alpha notes, edited in the library\n")
			},
			remove: "agentx skill remove alpha --from cursor", again: "agentx skill place alpha --to cursor --copy",
		},
		{
			what: "a name that starts with a dash", name: "-mine", install: placeOwnCopy("-mine"),
			edit:   func(t *testing.T, _ *harness, place string) { editCopy(t, place) },
			remove: "agentx skill remove --from cursor -- -mine", again: "agentx skill place --to cursor --copy -- -mine",
		},
		{
			what: "a name a shell has to quote", name: "it's mine", install: placeOwnCopy("it's mine"),
			edit:   func(t *testing.T, _ *harness, place string) { editCopy(t, place) },
			remove: `agentx skill remove 'it'\''s mine' --from cursor`, again: `agentx skill place 'it'\''s mine' --to cursor --copy`,
		},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			c.install(t, h, s)
			place := filepath.Join(h.home, ".cursor", "skills", c.name)
			c.edit(t, h, place)

			warned := warnings(h, h.mustRun("--json", "skill", "place", "--to", "cursor", "--", c.name).stderr)
			if len(warned) != 1 {
				t.Fatalf("warnings = %q, want the one for the copy", warned)
			}
			equal(t, "the warning", warned[0], keptCopyWarning(place, c.name, c.remove, c.again))
			commands := hintedCommands(t, warned[0])
			if len(commands) != 2 {
				t.Fatalf("the warning names %d commands, want 2: %s", len(commands), warned[0])
			}
			for _, args := range commands {
				h.mustRun(args...)
			}

			info, err := os.Lstat(place)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				t.Fatalf("the placement is not a copy: %v", err)
			}
			sameTree(t, "the copy", libraryTree(t, place), libraryTree(t, filepath.Join(h.library, c.name)))
			equal(t, "copy_mode", copyModeOf(t, h, c.name), "cursor")
			again := h.mustRun("--json", "skill", "place", "--to", "cursor", "--", c.name)
			equal(t, "warnings placing again", strings.Join(warnings(h, again.stderr), "\n"), "")
			equal(t, "placements", strings.Join(placementsOf(t, h.one(again.stdout, "library_skill")), ";"), "cursor copy copy")
		})
	}
}

// editCopy changes a copy placement by hand, the way a user edits the
// files their client reads: one file rewritten and one added.
func editCopy(t *testing.T, place string) {
	t.Helper()
	writeFile(t, filepath.Join(place, "notes.md"), "alpha notes, edited in the copy\n")
	writeFile(t, filepath.Join(place, "mine.md"), "a file of my own\n")
}

// placeOwnCopy makes a library skill of the user's own called name, which
// no source holds, and places a copy of it in Cursor.
func placeOwnCopy(name string) func(t *testing.T, h *harness, _ *sourceRepo) {
	return func(t *testing.T, h *harness, _ *sourceRepo) {
		if err := os.MkdirAll(filepath.Join(h.library, name), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(h.library, name, "SKILL.md"), skill("mine", "A skill of my own"))
		h.mustRun("skill", "place", "--to", "cursor", "--copy", "--", name)
	}
}

// editedCopyWarning is the message of the log event a copy of alpha that
// copy_mode records for Cursor, and that does not hold the library's
// version, is kept with.
func editedCopyWarning(place string) string {
	return keptCopyWarning(place, "alpha", "agentx skill remove alpha --from cursor", "agentx skill place alpha --to cursor --copy")
}

// keptCopyWarning is the message of the log event a copy of name that
// copy_mode records for Cursor, and that does not hold the library's
// version, is kept with, remove and again being the two commands it names:
// the two lines of the text warning joined by "; ".
func keptCopyWarning(place, name, remove, again string) string {
	warning, wayOn := keptCopyLines(place, name, remove, again)
	return warning + "; " + wayOn
}

// keptCopyLines are the two lines of that warning in text: the warning, and
// the line under it that names the two commands joined by &&.
func keptCopyLines(place, name, remove, again string) (warning, wayOn string) {
	return "cursor's copy of " + name + " is different from the library, so it was left unchanged (" + place + ")",
		wayOnPrefix + remove + " && " + again
}

// wayOnPrefix is what the warning for a kept copy says before the commands
// that replace it.
const wayOnPrefix = "to replace it with the library version: "

// hintedCommands are the agentx commands a warning for a kept copy names
// after wayOnPrefix, read as a POSIX shell reads them, each as the
// arguments to run.
func hintedCommands(t *testing.T, message string) [][]string {
	t.Helper()
	_, line, ok := strings.Cut(message, wayOnPrefix)
	if !ok {
		t.Fatalf("the warning names no commands: %s", message)
	}
	var commands [][]string
	for _, words := range shellCommands(line) {
		if len(words) == 0 || words[0] != "agentx" {
			t.Fatalf("%q is not an agentx command: %s", words, message)
		}
		commands = append(commands, words[1:])
	}
	return commands
}

// shellCommands splits a line of commands joined by && into the words of
// each, as a POSIX shell does for the words shellWord writes: a space ends
// a word, single quotes keep everything up to the next one, a backslash
// keeps the character after it, and && outside quotes ends a command.
func shellCommands(line string) [][]string {
	var commands [][]string
	var words []string
	var word strings.Builder
	inWord, quoted := false, false
	end := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(line); i++ {
		switch ch := line[i]; {
		case quoted:
			if ch == '\'' {
				quoted = false
			} else {
				word.WriteByte(ch)
			}
		case ch == '\'':
			quoted, inWord = true, true
		case ch == '\\' && i+1 < len(line):
			i++
			word.WriteByte(line[i])
			inWord = true
		case ch == ' ':
			end()
		case strings.HasPrefix(line[i:], "&&"):
			end()
			commands = append(commands, words)
			words = nil
			i++
		default:
			word.WriteByte(ch)
			inWord = true
		}
	}
	end()
	return append(commands, words)
}

// warnings are the messages of the warn-level log events of a JSON run's
// stderr.
func warnings(h *harness, stderr string) []string {
	var messages []string
	for _, e := range h.events(stderr) {
		if e["type"] == "log" && e["level"] == "warn" {
			messages = append(messages, e["message"].(string))
		}
	}
	return messages
}

// TestSkillPlaceLeavesWhatItDidNotMake refuses the placement path and keeps
// the user's bytes, exactly as an install does: a directory of their own,
// and a symlink of their own that happens to hold the same version. The
// skill is a copy in another configuration, which makes neither path
// agentx's copy: copy_mode records a copy for one configuration at a time.
func TestSkillPlaceLeavesWhatItDidNotMake(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code", "--copy").exit, 0)
	equal(t, "copy_mode", copyModeOf(t, h, "alpha"), "claude-code")

	handMade := filepath.Join(h.home, ".cursor", "skills", "alpha")
	if err := os.MkdirAll(handMade, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(handMade, "SKILL.md"), "---\nname: alpha\ndescription: mine\n---\n\nmine\n")
	mine := filepath.Join(h.home, "my-skills", "alpha")
	copyTree(t, filepath.Join(h.library, "alpha"), mine)
	foreign := filepath.Join(h.home, ".copilot", "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mine, foreign); err != nil {
		t.Fatal(err)
	}

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor", "--to", "github-copilot")
	equal(t, "exit", out.exit, 0)
	contains(t, "the hand-made directory", fileBody(t, filepath.Join(handMade, "SKILL.md")), "description: mine")
	target, ok := isSymlink(t, foreign)
	if !ok || target != mine {
		t.Errorf("the user's symlink is %q (symlink %v), want %q", target, ok, mine)
	}
	equal(t, "warnings", strings.Join(warnings(h, out.stderr), "\n"),
		handMade+" is not this skill and was left as it is; no placement was made for cursor\n"+
			foreign+" is a link of your own and was left as it is; no placement was made for github-copilot")
	contains(t, "the result", out.stdout, "2 placements skipped")
}

// TestSkillPlaceIsThePlacementAnInstallMakes proves the reuse: installing
// straight into a configuration and placing into it later leave the same
// thing on disk and the same entry in the settings.
func TestSkillPlaceIsThePlacementAnInstallMakes(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor", "--copy").exit, 0)

	later := newHarness(t)
	later.build(t, fixture{dirs: []string{".claude", ".cursor", ".codex"}})
	equal(t, "source add", later.run("source", "add", s.url).exit, 0)
	equal(t, "add", later.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)
	equal(t, "place", later.run("skill", "place", "alpha", "--to", "cursor", "--copy").exit, 0)

	installed := filepath.Join(h.home, ".cursor", "skills", "alpha")
	placed := filepath.Join(later.home, ".cursor", "skills", "alpha")
	for _, rel := range []string{"SKILL.md", "notes.md", filepath.Join("scripts", "run.sh")} {
		equal(t, "the file "+rel+" of the placement", fileBody(t, filepath.Join(placed, rel)), fileBody(t, filepath.Join(installed, rel)))
	}
	equal(t, "copy_mode", copyModeOf(t, later, "alpha"), copyModeOf(t, h, "alpha"))
}

// TestSkillPlaceKeepsAPlacementThatIsAlreadyRight leaves a symlink already
// naming the library where it is and reports it.
func TestSkillPlaceKeepsAPlacementThatIsAlreadyRight(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "cursor").exit, 0)

	out := h.run("--json", "skill", "place", "alpha", "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	target, ok := isSymlink(t, filepath.Join(h.home, ".cursor", "skills", "alpha"))
	if !ok || target != filepath.Join(h.library, "alpha") {
		t.Errorf("the placement is %q (symlink %v)", target, ok)
	}
	if strings.Contains(out.stderr, "left as it is") {
		t.Errorf("a placement that was already right was reported as skipped:\n%s", out.stderr)
	}
	contains(t, "the result", out.stdout, "placed alpha in 1 configuration")
}

// TestSkillPlaceIntoAClientThatReadsTheLibrary makes no second entry: the
// library entry is the placement, and a symlink beside it would make that
// client list the skill twice.
func TestSkillPlaceIntoAClientThatReadsTheLibrary(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)

	out := h.run("--json", "skill", "place", "alpha", "--to", "codex")
	equal(t, "exit", out.exit, 0)
	nothingAt(t, "a placement in the client's own directory", filepath.Join(h.home, ".codex", "skills", "alpha"))
	equal(t, "placements", strings.Join(placementsOf(t, h.one(out.stdout, "library_skill")), ";"), "codex library library")
}

// TestSkillPlaceRefusesWhatItCannotFind answers for a missing --to, a skill
// the library does not hold and a configuration that is not detected.
func TestSkillPlaceRefusesWhatItCannotFind(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	equal(t, "add", h.run("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code").exit, 0)

	noTo := h.run("skill", "place", "alpha")
	equal(t, "exit", noTo.exit, 1)
	contains(t, "stderr", noTo.stderr, "skill place needs --to")

	none := h.run("skill", "place", "gamma", "--to", "cursor")
	equal(t, "exit", none.exit, 5)
	contains(t, "stderr", none.stderr, "the library holds no skill called \"gamma\"")

	nowhere := h.run("skill", "place", "alpha", "--to", "nowhere")
	equal(t, "exit", nowhere.exit, 5)
	contains(t, "stderr", nowhere.stderr, "detected configurations:")
}

// TestSkillPlaceQuotesThePathsOfItsRows places a library directory whoever
// made it named across two lines and with an escape sequence in it, where
// Cursor already holds a copy of it, so the placement adopts that copy.
// The name on the line that confirms the placement is sanitised, and the
// paths of its row and of the line naming what it adopted carry the name,
// so they are quoted: every line stays one line, and each path still names
// a directory on disk.
func TestSkillPlaceQuotesThePathsOfItsRows(t *testing.T) {
	t.Parallel()
	h, _ := placementHarness(t)
	const raw = "two\nrows \x1b[31mRED\x1b[0m"
	dir := filepath.Join(h.library, raw)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: mine\ndescription: made here\n---\n\nmine\n")
	copyTree(t, dir, filepath.Join(h.home, ".cursor", "skills", raw))

	out := h.run("skill", "place", raw, "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	const quoted = `two\nrows \033[31mRED\033[0m"`
	placement := `"` + filepath.Join(h.home, ".cursor", "skills") + string(filepath.Separator) + quoted
	library := `"` + h.library + string(filepath.Separator) + quoted
	equal(t, "stdout", out.stdout, "✓ placed two rows [31mRED [0m in 1 configuration\n"+
		"  cursor  symlink  "+placement+" -> "+library+"\n"+
		"  adopted "+placement+"\n"+
		"  always available to universal clients: codex, gemini-cli\n")
}
