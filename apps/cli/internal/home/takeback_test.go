package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoRemovals is the shape of a mutation that is taken back when it stops:
// two directories that go, as config remove's configuration directories
// do, with the settings file written beside them.
type twoRemovals struct {
	dir, root, settings string
	dirs                []string
}

const oldSettings, newSettings = `{"old": true}` + "\n", `{"new": true}` + "\n"

func newTwoRemovals(t *testing.T) (twoRemovals, *Mutation) {
	t.Helper()
	root := t.TempDir()
	r := twoRemovals{dir: filepath.Join(root, "agentx"), root: root, dirs: []string{filepath.Join(root, "one"), filepath.Join(root, "two")}}
	r.settings = filepath.Join(r.dir, "settings.json")
	if err := os.MkdirAll(MutationsDir(r.dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.settings, []byte(oldSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewMutation(r.dir)
	for _, dir := range r.dirs {
		if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "skills", "f"), []byte(dir), 0o644); err != nil {
			t.Fatal(err)
		}
		state, err := State(dir)
		if err != nil {
			t.Fatal(err)
		}
		m.Remove(dir, state)
	}
	if err := m.ReplaceFile(r.settings, []byte(newSettings)); err != nil {
		t.Fatal(err)
	}
	m.TakeBackUnfinished("check one and two")
	return r, m
}

// untouched fails unless both directories hold their file, the settings
// hold what they held, and neither a journal nor anything retained or
// staged is left.
func (r twoRemovals) untouched(t *testing.T) {
	t.Helper()
	for _, dir := range r.dirs {
		if b, err := os.ReadFile(filepath.Join(dir, "skills", "f")); err != nil || string(b) != dir {
			t.Errorf("%s does not hold its file: %q, %v", dir, b, err)
		}
	}
	if b, err := os.ReadFile(r.settings); err != nil || string(b) != oldSettings {
		t.Errorf("the settings hold %q, %v", b, err)
	}
	for _, dir := range []string{r.root, MutationsDir(r.dir)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".agentx-") || dir != r.root {
				t.Errorf("%s is left in %s", e.Name(), dir)
			}
		}
	}
}

// TestApplyTakesBackAMutationWhoseStepRefuses is a second directory that
// changed after it was captured: its step refuses, the first directory,
// already moved, goes back to its path, the journal is gone, and the next
// mutation runs.
func TestApplyTakesBackAMutationWhoseStepRefuses(t *testing.T) {
	t.Parallel()
	r, m := newTwoRemovals(t)
	added := filepath.Join(r.dirs[1], "written-late")
	if err := os.WriteFile(added, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := m.Apply(refs{})
	if !errors.Is(err, ErrTakenBack) || errors.Is(err, ErrRecovery) || !strings.Contains(err.Error(), r.dirs[1]) {
		t.Fatalf("apply = %v, want the change taken back, naming %s", err, r.dirs[1])
	}
	if m.Journaled() {
		t.Error("the mutation still says its journal is on disk")
	}
	if err := os.Remove(added); err != nil {
		t.Errorf("the file written late is gone: %v", err)
	}
	r.untouched(t)
	if err := recoverJournals(r.dir, refs{}); err != nil {
		t.Errorf("the next command's recovery: %v", err)
	}
}

// TestRecoveryTakesBackFromEveryBoundary stops the mutation after each
// thing it does, the two directories and then the settings, and checks
// that recovery puts everything back and never finishes the removal.
// Recovery is then run a second time, which must repeat nothing.
func TestRecoveryTakesBackFromEveryBoundary(t *testing.T) {
	t.Parallel()
	for stop := 0; stop <= 3; stop++ {
		t.Run(fmt.Sprintf("after %d steps", stop), func(t *testing.T) {
			t.Parallel()
			r, m := newTwoRemovals(t)
			if err := m.stopAfter(min(stop, 2), refs{}); err != nil {
				t.Fatal(err)
			}
			if stop == 3 {
				rep := m.j.Replace[0]
				if err := keepBackup(rep); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(rep.Staged, rep.Path); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := recoverJournals(r.dir, refs{}); err != nil {
					t.Fatalf("recovery: %v", err)
				}
				r.untouched(t)
			}
		})
	}
}

// TestRecoveryKeepsBothWhenThePathWasMadeAgain is a directory something
// made again while the one that was there sat retained: recovery cannot
// put it back, so it keeps both, names where the retained one is and says
// what to do with it, which is never to move the journal aside. The other
// directory, whose path is free, goes back all the same, and once the
// retained one is dealt with the next recovery ends the journal.
func TestRecoveryKeepsBothWhenThePathWasMadeAgain(t *testing.T) {
	t.Parallel()
	r, m := newTwoRemovals(t)
	if err := m.stopAfter(2, refs{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(r.dirs[0], 0o755); err != nil {
		t.Fatal(err)
	}
	retained := m.j.Steps[0].Retained
	err := recoverJournals(r.dir, refs{})
	var stranded *StrandedError
	if !errors.Is(err, ErrRecovery) || !errors.As(err, &stranded) || !strings.Contains(err.Error(), retained) {
		t.Fatalf("recovery = %v, want a recovery naming %s", err, retained)
	}
	if hint := stranded.Hint(); !strings.Contains(hint, "delete the kept directory") || strings.Contains(hint, "aside") {
		t.Errorf("hint = %q", hint)
	}
	if _, err := os.Stat(filepath.Join(retained, "skills", "f")); err != nil {
		t.Errorf("the retained directory lost its content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.dirs[1], "skills", "f")); err != nil {
		t.Errorf("the directory whose path was free did not go back: %v", err)
	}
	if err := os.Rename(retained, filepath.Join(r.dirs[0], "kept")); err != nil {
		t.Fatal(err)
	}
	if err := recoverJournals(r.dir, refs{}); err != nil {
		t.Errorf("recovery once the retained directory was dealt with: %v", err)
	}
	if left, _ := Journals(r.dir); len(left) > 0 {
		t.Errorf("the journal is still there: %v", left)
	}
}

// TestTakeBackLeavesSettingsItCannotRestore is the settings file edited
// by hand, or its backup deleted, while the mutation was unfinished: the
// directories go back all the same, the file is left as it is and named,
// and no journal is left to hold later commands up.
func TestTakeBackLeavesSettingsItCannotRestore(t *testing.T) {
	t.Parallel()
	for name, spoil := range map[string]func(t *testing.T, r twoRemovals, m *Mutation) string{
		"edited by hand": func(t *testing.T, r twoRemovals, _ *Mutation) string {
			if err := os.WriteFile(r.settings, []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return "{}\n"
		},
		"backup deleted": func(t *testing.T, r twoRemovals, m *Mutation) string {
			rep := m.j.Replace[0]
			if err := os.Rename(rep.Staged, rep.Path); err != nil {
				t.Fatal(err)
			}
			return newSettings
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, m := newTwoRemovals(t)
			if err := m.stopAfter(2, refs{}); err != nil {
				t.Fatal(err)
			}
			want := spoil(t, r, m)
			var messages []string
			if err := recoverJournals(r.dir, warned{refs{}, &messages}); err != nil {
				t.Fatalf("recovery: %v", err)
			}
			for _, dir := range r.dirs {
				if _, err := os.Stat(filepath.Join(dir, "skills", "f")); err != nil {
					t.Errorf("%s did not go back: %v", dir, err)
				}
			}
			if b, _ := os.ReadFile(r.settings); string(b) != want {
				t.Errorf("the settings hold %q, want them left as %q", b, want)
			}
			if len(messages) != 1 || !strings.Contains(messages[0], r.settings) || !strings.HasSuffix(messages[0], "; check one and two") {
				t.Errorf("warnings = %q, want one naming the settings and ending with the command's note", messages)
			}
			if left, _ := Journals(r.dir); len(left) > 0 {
				t.Errorf("the journal is still there: %v", left)
			}
		})
	}
}

// TestRecoveryOfAnAppliedTakeBackOnlyCleansUp is a process killed while it
// was discarding what it had removed: the journal says applied, one
// retained directory is partly deleted, and the other directory's path
// holds, again, exactly what the removal captured there, as an installer
// that ran since would leave it. Recovery deletes what is retained, all
// of it, and never touches the paths again.
func TestRecoveryOfAnAppliedTakeBackOnlyCleansUp(t *testing.T) {
	t.Parallel()
	r, m := newTwoRemovals(t)
	if err := m.stopAfter(2, refs{}); err != nil {
		t.Fatal(err)
	}
	rep := m.j.Replace[0]
	if err := os.Rename(rep.Staged, rep.Path); err != nil {
		t.Fatal(err)
	}
	m.j.Progress = "applied"
	if err := writeJournal(m.journalPath(), m.j); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(m.j.Steps[1].Retained, "skills", "f")); err != nil {
		t.Fatal(err)
	}
	again := filepath.Join(r.dirs[0], "skills", "f")
	if err := os.MkdirAll(filepath.Dir(again), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(again, []byte(r.dirs[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recoverJournals(r.dir, refs{}); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if _, err := os.Stat(again); err != nil {
		t.Errorf("recovery removed the directory that was made again: %v", err)
	}
	for _, s := range m.j.Steps {
		if _, err := os.Lstat(s.Retained); !os.IsNotExist(err) {
			t.Errorf("%s is left behind: %v", s.Retained, err)
		}
	}
	if b, _ := os.ReadFile(r.settings); string(b) != newSettings {
		t.Errorf("the settings hold %q", b)
	}
	if left, _ := Journals(r.dir); len(left) > 0 {
		t.Errorf("the journal is still there: %v", left)
	}
}
