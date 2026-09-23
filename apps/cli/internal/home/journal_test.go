package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refs is a ref updater over a map, so that the journal's ref steps can be
// exercised without git.
type refs map[string]string

func (r refs) RefValue(gitDir, ref string) (string, error) { return r[gitDir+" "+ref], nil }

func (r refs) UpdateRef(gitDir, ref, newValue, oldValue string) error {
	key := gitDir + " " + ref
	if r[key] != oldValue {
		return errors.New("the ref moved under us")
	}
	r[key] = newValue
	return nil
}

// warned is a ref updater that also listens: it keeps what the journal told
// it, so a test can check that content a recovery kept was named to the
// user rather than left in a hidden directory nobody will ever look in.
type warned struct {
	refs
	messages *[]string
}

func (w warned) Warn(message string) { *w.messages = append(*w.messages, message) }

// stopAfter writes the journal and applies the first n steps, which is what
// a process killed at a durable boundary leaves behind: the journal on disk
// and the live state somewhere in the middle of it.
func (m *Mutation) stopAfter(n int, u RefUpdater) error {
	m.j.Progress = "staged"
	if err := writeJournal(m.journalPath(), m.j); err != nil {
		return err
	}
	for i, s := range m.j.Steps {
		if i >= n {
			return nil
		}
		if _, err := applyStep(s, u); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mutation) steps() int { return len(m.j.Steps) }

// install is the shape of a one-skill install: the import branch, the
// library directory and one symlink placement, with the settings file
// written beside them.
type install struct {
	dir      string // agentx home
	library  string
	place    string // the client's skills directory
	gitDir   string
	settings string
}

func newInstall(t *testing.T) (install, refs) {
	t.Helper()
	root := t.TempDir()
	in := install{
		dir:      filepath.Join(root, "agentx"),
		library:  filepath.Join(root, "library"),
		place:    filepath.Join(root, "client", "skills"),
		gitDir:   filepath.Join(root, "account.git"),
		settings: filepath.Join(root, "agentx", "settings.json"),
	}
	for _, dir := range []string{MutationsDir(in.dir), in.library, in.place} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return in, refs{}
}

// plan builds the mutation of one install over the content given.
func (in install) plan(t *testing.T, content string) *Mutation {
	t.Helper()
	m := NewMutation(in.dir)
	lib := filepath.Join(in.library, "alpha")
	staged := m.Sibling(lib, "staged")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := Fingerprint(staged)
	if err != nil {
		t.Fatal(err)
	}
	// The order is the one skill add records and the contract prescribes:
	// the ref, the library directory, the placements, then the settings
	// write. A plan in any other order would exercise a journal the product
	// never writes.
	m.Ref(in.gitDir, "refs/heads/managed/alpha", "", "c0ffee")
	m.Publish(lib, staged, fingerprint)
	m.Link(filepath.Join(in.place, "alpha"), lib)
	if err := m.ReplaceFile(in.settings, []byte(`{"copy_mode":{}}`+"\n")); err != nil {
		t.Fatal(err)
	}
	return m
}

// done reports whether the install is fully in place.
func (in install) done(t *testing.T, u refs) []string {
	t.Helper()
	var missing []string
	if u[in.gitDir+" refs/heads/managed/alpha"] != "c0ffee" {
		missing = append(missing, "the import branch")
	}
	if b, err := os.ReadFile(filepath.Join(in.library, "alpha", "SKILL.md")); err != nil || len(b) == 0 {
		missing = append(missing, "the library directory")
	}
	if target, err := os.Readlink(filepath.Join(in.place, "alpha")); err != nil || target != filepath.Join(in.library, "alpha") {
		missing = append(missing, "the placement")
	}
	if _, err := os.Stat(in.settings); err != nil {
		missing = append(missing, "the settings file")
	}
	if left, _ := Journals(in.dir); len(left) > 0 {
		missing = append(missing, "the journal is still there")
	}
	return missing
}

// TestInstallRecoversFromEveryBoundary stops an install after each step it
// records, which is where a process killed at a durable boundary leaves it,
// and checks that recovery finishes exactly what is left. Recovery is then
// run a second time, which must repeat nothing.
func TestInstallRecoversFromEveryBoundary(t *testing.T) {
	t.Parallel()
	total := 0
	{
		in, _ := newInstall(t)
		total = in.plan(t, "one\n").steps()
	}
	for stop := 0; stop <= total; stop++ {
		t.Run(fmt.Sprintf("after %d steps", stop), func(t *testing.T) {
			t.Parallel()
			in, u := newInstall(t)
			m := in.plan(t, "one\n")
			if err := m.stopAfter(stop, u); err != nil {
				t.Fatalf("stopping after %d steps: %v", stop, err)
			}
			if err := recoverJournals(in.dir, u); err != nil {
				t.Fatalf("recovery after %d steps: %v", stop, err)
			}
			if missing := in.done(t, u); len(missing) > 0 {
				t.Errorf("after recovery from step %d, %s", stop, strings.Join(missing, ", "))
			}
			if err := recoverJournals(in.dir, u); err != nil {
				t.Errorf("recovery run again: %v", err)
			}
			if missing := in.done(t, u); len(missing) > 0 {
				t.Errorf("recovery run again undid %s", strings.Join(missing, ", "))
			}
		})
	}
}

// TestRecoveryRefusesContentThatChanged is the other half: a live path that
// holds neither what the mutation captured nor what it was to become was
// changed by something else, and recovery keeps both rather than choosing.
func TestRecoveryRefusesContentThatChanged(t *testing.T) {
	t.Parallel()
	in, u := newInstall(t)
	m := in.plan(t, "one\n")
	if err := m.stopAfter(1, u); err != nil { // the branch is written, the library is not
		t.Fatal(err)
	}
	mine := filepath.Join(in.library, "alpha")
	if err := os.MkdirAll(mine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := recoverJournals(in.dir, u)
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("recovery = %v, want it refused", err)
	}
	if b, _ := os.ReadFile(filepath.Join(mine, "SKILL.md")); string(b) != "mine\n" {
		t.Errorf("the directory that changed was replaced: %q", b)
	}
	if left, _ := Journals(in.dir); len(left) != 1 {
		t.Errorf("%d journals left, want the refused one kept", len(left))
	}
}

// TestRemoveRetainsWhatItDisplaces keeps the content a placement held until
// the mutation is through, and drops it only once every live path is where
// it should be.
func TestRemoveRetainsWhatItDisplaces(t *testing.T) {
	t.Parallel()
	in, u := newInstall(t)
	live := filepath.Join(in.place, "alpha")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "SKILL.md"), []byte("displaced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := State(live)
	if err != nil {
		t.Fatal(err)
	}
	if !IsDir(state) {
		t.Fatalf("state = %q, want a directory", state)
	}
	m := NewMutation(in.dir)
	m.Remove(live, state)
	m.Link(live, filepath.Join(in.library, "alpha"))
	if err := m.Apply(u); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Readlink(live); err != nil {
		t.Errorf("the placement is not a symlink: %v", err)
	}
	// The content was retained beside the placement and dropped once the
	// mutation was verified, since it hashed to what the journal captured.
	entries, err := os.ReadDir(in.place)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentx-retained-") {
			t.Errorf("%s was kept although the mutation went through", e.Name())
		}
	}
}

// TestFingerprintFollowsTheContent tells two directories apart by what they
// hold, links and modes included, and never by where they are.
func TestFingerprintFollowsTheContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	build := func(name string, mode os.FileMode, link string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sub", "a"), []byte("content\n"), mode); err != nil {
			t.Fatal(err)
		}
		if link != "" {
			if err := os.Symlink(link, filepath.Join(dir, "l")); err != nil {
				t.Fatal(err)
			}
		}
		fp, err := Fingerprint(dir)
		if err != nil {
			t.Fatal(err)
		}
		return fp
	}
	same := build("one", 0o644, "")
	if other := build("two", 0o644, ""); other != same {
		t.Errorf("two directories with the same content hash differently")
	}
	if executable := build("three", 0o755, ""); executable == same {
		t.Errorf("a mode change went unnoticed")
	}
	if linked := build("four", 0o644, "sub/a"); linked == same {
		t.Errorf("a symlink went unnoticed")
	}
}

// TestRemoveKeepsAndNamesRetainedContentThatChanged is the other half of
// retention: locks coordinate agentx commands, not editors, so the content
// a removal displaced can change while the mutation is unfinished. It is
// then the user's own and is kept — and named, since nothing else ever
// mentions it: the sweep of the next install covers staging directories
// alone, so an unnamed retained directory stays hidden in a client's skills
// directory for good.
func TestRemoveKeepsAndNamesRetainedContentThatChanged(t *testing.T) {
	t.Parallel()
	in, u := newInstall(t)
	live := filepath.Join(in.place, "alpha")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "SKILL.md"), []byte("displaced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := State(live)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMutation(in.dir)
	m.Remove(live, state)
	m.Link(live, filepath.Join(in.library, "alpha"))
	if err := m.stopAfter(1, u); err != nil { // the content is retained, the placement is not made yet
		t.Fatal(err)
	}
	retained := retainedIn(t, in.place)
	if err := os.WriteFile(filepath.Join(retained, "SKILL.md"), []byte("the user edited it\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var messages []string
	if err := recoverJournals(in.dir, warned{u, &messages}); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(retained, "SKILL.md")); err != nil || string(b) != "the user edited it\n" {
		t.Errorf("the content that changed was dropped: %q, %v", b, err)
	}
	named := false
	for _, message := range messages {
		if strings.Contains(message, retained) && strings.Contains(message, live) {
			named = true
		}
	}
	if !named {
		t.Errorf("recovery kept %s and said %v, naming neither it nor %s", retained, messages, live)
	}
}

// TestRecoveryNamesTheLivePathThatChangedAfterAPublish checks what a
// recovery blames. A staging directory is gone after the publish that
// renamed it into place, so a live path that changed afterwards must be
// reported as the live path that changed: naming the staging directory
// sends the reader looking for something that is supposed to be gone.
func TestRecoveryNamesTheLivePathThatChangedAfterAPublish(t *testing.T) {
	t.Parallel()
	in, u := newInstall(t)
	m := in.plan(t, "one\n")
	if err := m.stopAfter(2, u); err != nil { // the ref is written and the library is published
		t.Fatal(err)
	}
	lib := filepath.Join(in.library, "alpha")
	if err := os.WriteFile(filepath.Join(lib, "SKILL.md"), []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := recoverJournals(in.dir, u)
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("recovery = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), lib) {
		t.Errorf("the refusal does not name the live path that changed: %v", err)
	}
	if strings.Contains(err.Error(), ".agentx-staged-") {
		t.Errorf("the refusal blames the staging directory, which the publish renamed away: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(lib, "SKILL.md")); string(b) != "edited by hand\n" {
		t.Errorf("the edit was not kept: %q", b)
	}
}

// retainedIn is the one directory a removal retained in dir.
func retainedIn(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentx-retained-") {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatalf("nothing was retained in %s", dir)
	return ""
}
