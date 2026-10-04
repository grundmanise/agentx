package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// TestSkillAddPointsAtSkillPlaceForALibrarySkill: skill add installs from a
// source, and a bare name is no source. When the library holds a skill of
// exactly that name, one agentx installed or one of the user's own, the
// refusal says so and its hint is the skill place command that puts it in
// more clients, carrying the --to and --copy that were given, which runs as
// it is printed. The exit code is the one any other argument that is no
// source gets, and the refusal is decided before anything is fetched,
// locked or written. How the hint quotes every other name and client is
// TestThePlaceHintRunsAsItIsPrinted.
//
// A bare name the library holds no skill of is refused as it always was,
// with the forms a source takes, and so is a directory of the library that
// is not a skill.
func TestSkillAddPointsAtSkillPlaceForALibrarySkill(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--name", "alpha", "--to", "claude-code")
	ownLibrarySkill(t, h, "-mine")
	for _, c := range []struct {
		name string   // the library skill
		args []string // after skill add
		hint string
	}{
		{name: "alpha", args: []string{"alpha", "--to", "cursor"}, hint: "agentx skill place alpha --to cursor"},
		{name: "-mine", args: []string{"--copy", "--", "-mine"}, hint: "agentx skill place --copy -- -mine"},
	} {
		message := c.name + " is already in the library; skill add installs a skill from a source"
		hint := "to place it in more clients, run '" + c.hint + "'"
		before := diskState(t, h)

		text := h.run(append([]string{"skill", "add"}, c.args...)...)
		equal(t, c.name+": exit", text.exit, 1)
		equal(t, c.name+": stdout", text.stdout, "")
		equal(t, c.name+": stderr", text.stderr, "error: "+message+"\nhint: "+hint+"\n")

		out := h.run(append([]string{"--json", "skill", "add"}, c.args...)...)
		equal(t, c.name+": exit", out.exit, 1)
		events := h.events(out.stdout)
		if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: event types = %v, want %v\n%s", c.name, got, want, out.stdout)
		}
		equal(t, c.name+": error.code", events[0]["code"], "usage")
		equal(t, c.name+": error.message", events[0]["message"], message)
		equal(t, c.name+": error.hint", events[0]["hint"], hint)

		if after := diskState(t, h); !reflect.DeepEqual(after, before) {
			t.Errorf("%s: the refusal changed what is on disk:\nbefore %v\nafter  %v", c.name, before, after)
		}
		h.mustRun(shellCommands(c.hint)[0][1:]...)
		if _, err := os.Lstat(filepath.Join(h.home, ".cursor", "skills", c.name)); err != nil {
			t.Errorf("%s: the command the hint names placed nothing in cursor: %v", c.name, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(h.library, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.library, "notes", "README.md"), "no SKILL.md here\n")
	for _, name := range []string{"gamma", "notes"} {
		text := h.run("skill", "add", name, "--to", "cursor")
		equal(t, name+": exit", text.exit, 1)
		equal(t, name+": stderr", text.stderr, "error: not a source URL: "+name+"\nhint: accepted forms: "+source.Forms+"\n")
	}
}

// TestSkillAddReadsASourceFormAsASource: an argument that is a source form
// stays one even when the library holds a skill of that exact name. A
// source id is the one such form a directory can be named, and skill add
// installs from the source it names.
func TestSkillAddReadsASourceFormAsASource(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	id := source.ID(s.url)
	ownLibrarySkill(t, h, id)

	out := h.run("--json", "skill", "add", id, "--name", "alpha", "--to", "cursor")
	equal(t, "exit", out.exit, 0)
	equal(t, "installed", h.one(out.stdout, "library_skill")["name"], "alpha")
	if _, ok := isSymlink(t, filepath.Join(h.home, ".cursor", "skills", "alpha")); !ok {
		t.Error("the skill of the source was not placed in cursor")
	}
}

// ownLibrarySkill makes a library skill of the user's own called name,
// which no source holds.
func ownLibrarySkill(t *testing.T, h *harness, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(h.library, name), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.library, name, "SKILL.md"), skill("mine", "A skill of my own"))
}

// diskState is every entry under the user's home, agentx home and the
// library, with its type, its size or where it links to, and when it was
// last written, for a test that has to prove a command wrote nothing at
// all: no placement, no settings, no journal, no source and no ref.
func diskState(t *testing.T, h *harness) []string {
	t.Helper()
	var state []string
	for _, root := range []string{h.home, h.agentx, h.library} {
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			entry := fmt.Sprintf("%s %s %s", p, info.Mode(), info.ModTime())
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				target, err := os.Readlink(p)
				if err != nil {
					return err
				}
				entry += " -> " + target
			case !info.IsDir():
				entry += fmt.Sprintf(" %d bytes", info.Size())
			}
			state = append(state, entry)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(state)
	return state
}
