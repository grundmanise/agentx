package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conversion is the shape of a managed skill turned into a fork in place:
// the fork's branch created and the import branch deleted, the worktree
// added, the library directory moved into it and the library symlink
// written where the directory was. Nothing is staged: the content is the
// user's and only moves.
type conversion struct{ creation }

func newConversion(t *testing.T, name string) (conversion, refs) {
	t.Helper()
	c, u := newCreation(t)
	lib := filepath.Join(c.library, name)
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	for file, content := range map[string]string{"SKILL.md": "edited\n", ".DS_Store": "ignored\n"} {
		if err := os.WriteFile(filepath.Join(lib, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	u[c.gitDir+" refs/heads/managed/"+name] = "import-" + name
	return conversion{c}, u
}

func (c conversion) conversionOf(t *testing.T, name string) *Mutation {
	t.Helper()
	lib := filepath.Join(c.library, name)
	fp, err := Fingerprint(lib)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMutation(c.dir)
	m.Ref(c.gitDir, "refs/heads/skills/"+name, "", "fork-"+name)
	m.Ref(c.gitDir, "refs/heads/managed/"+name, "import-"+name, "")
	m.Worktree(c.gitDir, c.root(name), "skills/"+name)
	m.Move(lib, filepath.Join(c.root(name), "upstream-dir"), fp)
	m.Link(lib, filepath.Join(c.root(name), "upstream-dir"))
	return m
}

// converted reports what of a conversion is not in place.
func (c conversion) converted(u refs, name string) []string {
	var missing []string
	if u[c.gitDir+" refs/heads/skills/"+name] != "fork-"+name {
		missing = append(missing, "the fork's branch")
	}
	if u[c.gitDir+" refs/heads/managed/"+name] != "" {
		missing = append(missing, "the import branch's deletion")
	}
	if !WorktreeAt(c.root(name), "skills/"+name) {
		missing = append(missing, "the worktree")
	}
	dir := filepath.Join(c.root(name), "upstream-dir")
	for file, content := range map[string]string{"SKILL.md": "edited\n", ".DS_Store": "ignored\n"} {
		if b, err := os.ReadFile(filepath.Join(dir, file)); err != nil || string(b) != content {
			missing = append(missing, "the moved "+file)
		}
	}
	if target, err := os.Readlink(filepath.Join(c.library, name)); err != nil || target != dir {
		missing = append(missing, "the library symlink")
	}
	if left, _ := Journals(c.dir); len(left) > 0 {
		missing = append(missing, "the journal is still there")
	}
	return missing
}

// TestConversionRecoversFromEveryBoundary stops a conversion after each
// step and checks that recovery finishes it, twice over without repeating
// anything, and that the skill's directory, which nothing holds a copy of,
// is never lost on the way.
func TestConversionRecoversFromEveryBoundary(t *testing.T) {
	t.Parallel()
	total := 0
	{
		c, _ := newConversion(t, "alpha")
		total = c.conversionOf(t, "alpha").steps()
	}
	for stop := 0; stop <= total; stop++ {
		t.Run(fmt.Sprintf("after %d steps", stop), func(t *testing.T) {
			t.Parallel()
			c, u := newConversion(t, "alpha")
			m := c.conversionOf(t, "alpha")
			if err := m.stopAfter(stop, u); err != nil {
				t.Fatalf("stopping after %d steps: %v", stop, err)
			}
			for _, pass := range []string{"recovery", "recovery run again"} {
				if err := recoverJournals(c.dir, u); err != nil {
					t.Fatalf("%s after %d steps: %v", pass, stop, err)
				}
				if missing := c.converted(u, "alpha"); len(missing) > 0 {
					t.Errorf("after %s from step %d: %s", pass, stop, strings.Join(missing, ", "))
				}
			}
		})
	}
}

// TestRecoveryRefusesAMoveWhoseSourceChanged is the move step's guard: a
// directory edited after the journal captured it is not moved anywhere the
// journal does not describe, and it and the journal are kept as they are.
func TestRecoveryRefusesAMoveWhoseSourceChanged(t *testing.T) {
	t.Parallel()
	c, u := newConversion(t, "alpha")
	m := c.conversionOf(t, "alpha")
	if err := m.stopAfter(2, u); err != nil { // the branch and the worktree
		t.Fatal(err)
	}
	lib := filepath.Join(c.library, "alpha")
	if err := os.WriteFile(filepath.Join(lib, "notes.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := recoverJournals(c.dir, u)
	if !errors.Is(err, ErrRecovery) || !strings.Contains(err.Error(), lib) {
		t.Fatalf("recovery = %v, want it refused naming %s", err, lib)
	}
	if b, err := os.ReadFile(filepath.Join(lib, "notes.md")); err != nil || string(b) != "mine\n" {
		t.Errorf("the edited directory holds %q, %v, want it where it was", b, err)
	}
	if left, _ := Journals(c.dir); len(left) != 1 {
		t.Errorf("%d journals left, want the refused one kept", len(left))
	}
}

// TestMovingADirectoryOnwardSettlesTheFirstMove is a journal that moves one
// directory twice, out to a second place and on to a third or back where it
// was. Once the directory has arrived, the first move is done, and recovery
// must not move it out again from wherever it now is.
func TestMovingADirectoryOnwardSettlesTheFirstMove(t *testing.T) {
	t.Parallel()
	for _, back := range []bool{false, true} {
		for stop := 0; stop <= 2; stop++ {
			t.Run(fmt.Sprintf("back %v after %d steps", back, stop), func(t *testing.T) {
				t.Parallel()
				c, u := newConversion(t, "alpha")
				first, second := filepath.Join(c.library, "alpha"), filepath.Join(c.worktrees, ".agentx-held")
				last := filepath.Join(c.worktrees, "elsewhere")
				if back {
					last = first
				}
				fp, err := Fingerprint(first)
				if err != nil {
					t.Fatal(err)
				}
				m := NewMutation(c.dir)
				m.Move(first, second, fp)
				m.Move(second, last, fp)
				if err := m.stopAfter(stop, u); err != nil {
					t.Fatalf("stopping after %d steps: %v", stop, err)
				}
				if err := recoverJournals(c.dir, u); err != nil {
					t.Fatalf("recovery: %v", err)
				}
				if got, _ := Fingerprint(last); got != fp || exists(second) {
					t.Errorf("after recovery the directory is not at %s alone", last)
				}
			})
		}
	}
}
