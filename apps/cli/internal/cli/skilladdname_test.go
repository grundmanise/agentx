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
// locked or written.
func TestSkillAddPointsAtSkillPlaceForALibrarySkill(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		what    string
		name    string   // the library skill
		args    []string // after skill add
		hint    string
		follows bool // the hint runs as it is printed and places the skill in cursor
	}{
		{
			what: "a skill agentx installed", name: "alpha", args: []string{"alpha", "--to", "cursor"},
			hint: "agentx skill place alpha --to cursor", follows: true,
		},
		{
			what: "several clients and a copy", name: "alpha", args: []string{"alpha", "--to", "cursor", "--to", "windsurf", "--copy"},
			hint: "agentx skill place alpha --to cursor --to windsurf --copy", follows: true,
		},
		{
			what: "no client named", name: "alpha", args: []string{"alpha"},
			hint: "agentx skill place alpha --to <configuration>",
		},
		{
			what: "a skill of the user's own", name: "mine", args: []string{"mine", "--copy"},
			hint: "agentx skill place mine --to <configuration> --copy",
		},
		{
			what: "a name that starts with a dash", name: "-mine", args: []string{"--to", "cursor", "--", "-mine"},
			hint: "agentx skill place --to cursor -- -mine", follows: true,
		},
		{
			what: "a name a shell has to quote", name: "it's mine", args: []string{"it's mine", "--to", "cursor"},
			hint: `agentx skill place 'it'\''s mine' --to cursor`, follows: true,
		},
		{
			what: "a client a shell has to quote", name: "alpha", args: []string{"alpha", "--to", "my client"},
			hint: "agentx skill place alpha --to 'my client'",
		},
	} {
		t.Run(c.what, func(t *testing.T) {
			t.Parallel()
			h, s := placementHarness(t)
			if c.name == "alpha" {
				h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
			} else {
				ownLibrarySkill(t, h, c.name)
			}
			message := c.name + " is already in the library; skill add installs a skill from a source"
			hint := "to place it in more clients, run '" + c.hint + "'"
			before := diskState(t, h)

			text := h.run(append([]string{"skill", "add"}, c.args...)...)
			equal(t, "exit", text.exit, 1)
			equal(t, "stdout", text.stdout, "")
			equal(t, "stderr", text.stderr, "error: "+message+"\nhint: "+hint+"\n")

			out := h.run(append([]string{"--json", "skill", "add"}, c.args...)...)
			equal(t, "exit", out.exit, 1)
			events := h.events(out.stdout)
			if got, want := h.types(events), []string{"error", "result"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("event types = %v, want %v\n%s", got, want, out.stdout)
			}
			equal(t, "error.code", events[0]["code"], "usage")
			equal(t, "error.message", events[0]["message"], message)
			equal(t, "error.hint", events[0]["hint"], hint)
			equal(t, "stderr", out.stderr, "")

			if after := diskState(t, h); !reflect.DeepEqual(after, before) {
				t.Errorf("the refusal changed what is on disk:\nbefore %v\nafter  %v", before, after)
			}
			if c.follows {
				h.mustRun(shellCommands(c.hint)[0][1:]...)
				if _, err := os.Lstat(filepath.Join(h.home, ".cursor", "skills", c.name)); err != nil {
					t.Errorf("the command the hint names placed nothing in cursor: %v", err)
				}
			}
		})
	}
}

// TestSkillAddKeepsItsAnswerForWhatTheLibraryDoesNotHold: a bare name the
// library holds no skill of is refused as today, with the forms a source
// takes, and so is a directory of the library that is not a skill.
func TestSkillAddKeepsItsAnswerForWhatTheLibraryDoesNotHold(t *testing.T) {
	t.Parallel()
	h, s := placementHarness(t)
	h.mustRun("skill", "add", s.url, "--skill", "alpha", "--to", "claude-code")
	if err := os.MkdirAll(filepath.Join(h.library, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.library, "notes", "README.md"), "no SKILL.md here\n")

	for _, name := range []string{"gamma", "notes"} {
		text := h.run("skill", "add", name, "--to", "cursor")
		equal(t, name+": exit", text.exit, 1)
		equal(t, name+": stderr", text.stderr, "error: not a source URL: "+name+"\nhint: accepted forms: "+source.Forms+"\n")

		out := h.run("--json", "skill", "add", name, "--to", "cursor")
		equal(t, name+": exit", out.exit, 1)
		ev := h.one(out.stdout, "error")
		equal(t, name+": error.code", ev["code"], "usage")
		equal(t, name+": error.message", ev["message"], "not a source URL: "+name)
		equal(t, name+": error.hint", ev["hint"], "accepted forms: "+source.Forms)
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

	out := h.run("--json", "skill", "add", id, "--skill", "alpha", "--to", "cursor")
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
