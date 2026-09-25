package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// refs is a ref updater over a map, so that the journal's ref steps can be
// exercised without git.
type refs map[string]string

func (r refs) RefValues(gitDir string, names []string) (map[string]string, error) {
	values := map[string]string{}
	for _, name := range names {
		if value := r[gitDir+" "+name]; value != "" {
			values[name] = value
		}
	}
	return values, nil
}

// UpdateRefs applies the whole batch or none of it, as git's own
// transaction does: every expected old value is checked before the first
// ref moves.
func (r refs) UpdateRefs(gitDir string, updates []RefUpdate) error {
	for _, u := range updates {
		if r[gitDir+" "+u.Ref] != u.Old {
			return errors.New("the ref moved under us")
		}
	}
	for _, u := range updates {
		r[gitDir+" "+u.Ref] = u.New
	}
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
// and the live state somewhere in the middle of it. The steps are taken in
// the order apply takes them, ref deletions last, so the states this walks
// through are the states a killed process really leaves.
func (m *Mutation) stopAfter(n int, u RefUpdater) error {
	m.j.Progress = "staged"
	if err := writeJournal(m.journalPath(), m.j); err != nil {
		return err
	}
	for i, s := range m.applyOrder() {
		if i >= n {
			return nil
		}
		if _, err := applyStep(s, u); err != nil {
			return err
		}
	}
	return nil
}

// applyOrder is the journal's steps in the order apply runs them.
func (m *Mutation) applyOrder() []step {
	if !refsGoLast(m.j.Steps) {
		return m.j.Steps
	}
	ordered := make([]step, 0, len(m.j.Steps))
	for _, s := range m.j.Steps {
		if s.Kind != stepRef {
			ordered = append(ordered, s)
		}
	}
	for _, s := range m.j.Steps {
		if s.Kind == stepRef {
			ordered = append(ordered, s)
		}
	}
	return ordered
}

func (m *Mutation) steps() int { return len(m.j.Steps) }

// install is the shape of an install: one import branch, one library
// directory and one symlink placement per skill, with the settings file
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

// plan builds the mutation of an install of the skills named: one ref, one
// library directory and one placement each, in the order the contract gives
// them, and the one settings write that ends the run. A batch install is
// one journal of this shape and not one journal per skill.
func (in install) plan(t *testing.T, content string, names ...string) *Mutation {
	t.Helper()
	m := NewMutation(in.dir)
	for _, name := range names {
		lib := filepath.Join(in.library, name)
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
		// The order is the one skill add records and the contract
		// prescribes: the ref, the library directory, the placements, then
		// the settings write. A plan in any other order would exercise a
		// journal the product never writes.
		m.Ref(in.gitDir, "refs/heads/managed/"+name, "", "c0ffee-"+name)
		m.Publish(lib, staged, fingerprint)
		m.Link(filepath.Join(in.place, name), lib)
	}
	if err := m.ReplaceFile(in.settings, []byte(`{"copy_mode":{}}`+"\n")); err != nil {
		t.Fatal(err)
	}
	return m
}

// done reports whether the install is fully in place, skill by skill.
func (in install) done(t *testing.T, u refs, names ...string) []string {
	t.Helper()
	var missing []string
	for _, name := range names {
		if u[in.gitDir+" refs/heads/managed/"+name] != "c0ffee-"+name {
			missing = append(missing, "the import branch of "+name)
		}
		if b, err := os.ReadFile(filepath.Join(in.library, name, "SKILL.md")); err != nil || len(b) == 0 {
			missing = append(missing, "the library directory of "+name)
		}
		if target, err := os.Readlink(filepath.Join(in.place, name)); err != nil || target != filepath.Join(in.library, name) {
			missing = append(missing, "the placement of "+name)
		}
	}
	if _, err := os.Stat(in.settings); err != nil {
		missing = append(missing, "the settings file")
	}
	if left, _ := Journals(in.dir); len(left) > 0 {
		missing = append(missing, "the journal is still there")
	}
	return missing
}

// batch is the install TestInstallRecoversFromEveryBoundary stops in: three
// skills, so that the boundaries it kills at fall in the middle of a batch
// and not only at its ends.
var batch = []string{"alpha", "beta", "gamma"}

// TestInstallRecoversFromEveryBoundary stops an install after each step it
// records, which is where a process killed at a durable boundary leaves it,
// and checks that recovery finishes exactly what is left. Recovery is then
// run a second time, which must repeat nothing.
func TestInstallRecoversFromEveryBoundary(t *testing.T) {
	t.Parallel()
	total := 0
	{
		in, _ := newInstall(t)
		total = in.plan(t, "one\n", batch...).steps()
	}
	for stop := 0; stop <= total; stop++ {
		t.Run(fmt.Sprintf("after %d steps", stop), func(t *testing.T) {
			t.Parallel()
			in, u := newInstall(t)
			m := in.plan(t, "one\n", batch...)
			if err := m.stopAfter(stop, u); err != nil {
				t.Fatalf("stopping after %d steps: %v", stop, err)
			}
			if err := recoverJournals(in.dir, u); err != nil {
				t.Fatalf("recovery after %d steps: %v", stop, err)
			}
			if missing := in.done(t, u, batch...); len(missing) > 0 {
				t.Errorf("after recovery from step %d, %s", stop, strings.Join(missing, ", "))
			}
			if err := recoverJournals(in.dir, u); err != nil {
				t.Errorf("recovery run again: %v", err)
			}
			if missing := in.done(t, u, batch...); len(missing) > 0 {
				t.Errorf("recovery run again undid %s", strings.Join(missing, ", "))
			}
		})
	}
}

// TestBatchRefsMoveTogether checks the transaction a journal's refs are
// applied in: a batch whose second ref is claimed by something else moves
// none of them, so that recovery finds the branches either all written or
// all absent rather than a run's skills split between the two.
func TestBatchRefsMoveTogether(t *testing.T) {
	t.Parallel()
	in, u := newInstall(t)
	u[in.gitDir+" refs/heads/managed/beta"] = "someone else"
	m := in.plan(t, "one\n", batch...)
	err := m.Apply(u)
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("apply = %v, want it refused", err)
	}
	for _, name := range []string{"alpha", "gamma"} {
		if value := u[in.gitDir+" refs/heads/managed/"+name]; value != "" {
			t.Errorf("%s was written to %q although the batch was refused", name, value)
		}
	}
}

// racingRefs lets something else claim one of the refs between the read of
// their values and the write, which is the race the expected old value is
// there for. Every ref of the batch then passes the check applyRefs makes
// before it builds the updates, so the refusal can only come from the
// transaction itself.
type racingRefs struct {
	refs
	gitDir string
	claim  string // the ref a third party takes once the values are read
}

func (r racingRefs) RefValues(gitDir string, names []string) (map[string]string, error) {
	values, err := r.refs.RefValues(gitDir, names)
	r.refs[r.gitDir+" "+r.claim] = "someone else"
	return values, err
}

// TestBatchRefsMoveInOneTransaction is what the check above cannot see: a
// batch every ref of which looked free when the journal read them, one of
// which is claimed before the update lands. The refusal comes from the
// transaction, so it is the transaction that has to move all three refs or
// none of them; a journal that updated one ref at a time would have
// written the first before it reached the one that was taken.
func TestBatchRefsMoveInOneTransaction(t *testing.T) {
	t.Parallel()
	in, u := newInstall(t)
	m := in.plan(t, "one\n", batch...)
	racing := racingRefs{refs: u, gitDir: in.gitDir, claim: "refs/heads/managed/beta"}
	if err := m.Apply(racing); err == nil {
		t.Fatal("apply = nil, want the transaction refused")
	}
	for _, name := range batch {
		value := u[in.gitDir+" refs/heads/managed/"+name]
		if name == "beta" {
			if value != "someone else" {
				t.Errorf("beta = %q, want the value the other command wrote", value)
			}
			continue
		}
		if value != "" {
			t.Errorf("%s was written to %q although the transaction was refused", name, value)
		}
	}
}

// TestJournaledIsTrueOnlyOnceTheJournalIsOnDisk is the line between a
// failure that leaves nothing behind and one recovery has to finish. The
// caller reads it to decide whether it may take away the staging refs
// holding this run's commits: before the journal exists nothing would ever
// recover them, so keeping them would leak them into the account repo for
// good, and after it exists recovery needs them.
func TestJournaledIsTrueOnlyOnceTheJournalIsOnDisk(t *testing.T) {
	t.Parallel()
	in, u := newInstall(t)
	m := in.plan(t, "one\n", "alpha")
	if m.Journaled() {
		t.Error("a mutation that has not been applied reads as journaled")
	}
	// A file where the mutations directory belongs: the journal cannot be
	// written, and what the mutation staged goes with it.
	if err := os.RemoveAll(MutationsDir(in.dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(MutationsDir(in.dir), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(u); err == nil {
		t.Fatal("apply = nil, want the journal write refused")
	}
	if m.Journaled() {
		t.Error("a mutation whose journal could not be written reads as journaled")
	}

	// And the other way round, with a directory to write into.
	again, v := newInstall(t)
	n := again.plan(t, "one\n", "alpha")
	if err := n.Apply(v); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !n.Journaled() {
		t.Error("a mutation that was applied reads as not journaled")
	}
}

// TestRecoveryRefusesContentThatChanged is the other half: a live path that
// holds neither what the mutation captured nor what it was to become was
// changed by something else, and recovery keeps both rather than choosing.
func TestRecoveryRefusesContentThatChanged(t *testing.T) {
	t.Parallel()
	in, u := newInstall(t)
	m := in.plan(t, "one\n", "alpha")
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

// copyPlace is a second client's skills directory, where a copy placement
// lives: a removal has to take a symlink and a directory away in one run.
func (in install) copyPlace(name string) string {
	return filepath.Join(in.place+"-copies", name)
}

// newRemoval is a machine with one skill installed: its import branch, a
// candidate ref a later spec writes, its library directory, one symlink
// placement and one copy placement. It is the state agentx skill remove
// starts from.
func newRemoval(t *testing.T, name string) (install, refs) {
	t.Helper()
	in, u := newInstall(t)
	if err := in.plan(t, "one\n", name).Apply(u); err != nil {
		t.Fatal(err)
	}
	u[in.gitDir+" refs/agentx/candidate/"+name] = "cand-" + name
	copyDir := in.copyPlace(name)
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copyDir, "SKILL.md"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return in, u
}

// removalOf plans the removal of one installed skill: the import branch and
// the candidate ref deleted, the symlink placement unlinked, the copy
// placement taken away with its content retained, the library directory
// taken away, and the settings rewritten last.
func (in install) removalOf(t *testing.T, name string) *Mutation {
	t.Helper()
	m := NewMutation(in.dir)
	m.Ref(in.gitDir, "refs/heads/managed/"+name, "c0ffee-"+name, "")
	m.Ref(in.gitDir, "refs/agentx/candidate/"+name, "cand-"+name, "")
	for _, path := range []string{filepath.Join(in.place, name), in.copyPlace(name), filepath.Join(in.library, name)} {
		state, err := liveState(path)
		if err != nil {
			t.Fatal(err)
		}
		m.Remove(path, state)
	}
	if err := m.ReplaceFile(in.settings, []byte(`{"copy_mode":{}}`+"\n")); err != nil {
		t.Fatal(err)
	}
	return m
}

// removed reports what of the skill is still on the machine.
func (in install) removed(t *testing.T, u refs, name string) []string {
	t.Helper()
	var left []string
	for ref, what := range map[string]string{
		"refs/heads/managed/" + name:    "the import branch",
		"refs/agentx/candidate/" + name: "the candidate ref",
	} {
		if u[in.gitDir+" "+ref] != "" {
			left = append(left, what)
		}
	}
	for path, what := range map[string]string{
		filepath.Join(in.place, name):   "the symlink placement",
		in.copyPlace(name):              "the copy placement",
		filepath.Join(in.library, name): "the library directory",
	} {
		if _, err := os.Lstat(path); err == nil {
			left = append(left, what)
		}
	}
	if journals, _ := Journals(in.dir); len(journals) > 0 {
		left = append(left, "the journal")
	}
	sort.Strings(left)
	return left
}

// TestRemovalKeepsItsRefsWhenAPathStepRefuses is the ordering rule of a
// removal. Deleting a ref is the one step a journal cannot undo and no
// later command can redo: the journal is the only record of the commit the
// branch pointed at, and the hint a refusal prints offers to move that
// journal aside. So a removal's ref deletions run after its path steps,
// once everything that could still refuse has not.
//
// With them first, a refusal left the skill's directory in the library and
// no branch for it, which is a managed skill silently turned unmanaged,
// permanently, the one thing mutation safety says not to make of
// half-applied state.
func TestRemovalKeepsItsRefsWhenAPathStepRefuses(t *testing.T) {
	t.Parallel()
	in, u := newRemoval(t, "alpha")
	m := in.removalOf(t, "alpha")

	// Between the plan and the apply the placement becomes something the
	// removal did not capture, so its path step refuses.
	live := filepath.Join(in.place, "alpha")
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.Apply(u); !errors.Is(err, ErrRecovery) {
		t.Fatalf("apply = %v, want it refused", err)
	}
	for _, ref := range []string{"refs/heads/managed/alpha", "refs/agentx/candidate/alpha"} {
		if u[in.gitDir+" "+ref] == "" {
			t.Errorf("%s was deleted by a removal that could not finish", ref)
		}
	}
	if _, err := os.Stat(filepath.Join(in.library, "alpha")); err != nil {
		t.Errorf("the library directory went before the refusal: %v", err)
	}
}

// TestRemovalRecoversFromEveryBoundary stops a removal after each step it
// records, which is where a process killed at a durable boundary leaves it,
// and checks that recovery finishes exactly what is left. Recovery is then
// run a second time, which must repeat nothing.
func TestRemovalRecoversFromEveryBoundary(t *testing.T) {
	t.Parallel()
	total := 0
	{
		in, _ := newRemoval(t, "alpha")
		total = in.removalOf(t, "alpha").steps()
	}
	for stop := 0; stop <= total; stop++ {
		t.Run(fmt.Sprintf("after %d steps", stop), func(t *testing.T) {
			t.Parallel()
			in, u := newRemoval(t, "alpha")
			m := in.removalOf(t, "alpha")
			if err := m.stopAfter(stop, u); err != nil {
				t.Fatalf("stopping after %d steps: %v", stop, err)
			}
			if err := recoverJournals(in.dir, u); err != nil {
				t.Fatalf("recovery after %d steps: %v", stop, err)
			}
			if left := in.removed(t, u, "alpha"); len(left) > 0 {
				t.Errorf("after recovery from step %d, %s is still there", stop, strings.Join(left, ", "))
			}
			if err := recoverJournals(in.dir, u); err != nil {
				t.Errorf("recovery run again: %v", err)
			}
			if left := in.removed(t, u, "alpha"); len(left) > 0 {
				t.Errorf("recovery run again left %s", strings.Join(left, ", "))
			}
		})
	}
}

// TestRecoveryRefusesARemovalWhoseTargetChanged is the guard that stands
// between a removal and a user's file. A remove step deletes; every other
// path step replaces. So a remove step whose live path no longer holds what
// the journal captured is the one place where applying a step that is out
// of date destroys something nothing else has a copy of, and the check that
// stops it – live != s.Old – has to be tested for a remove step by name,
// not only for the publish of a state file.
//
// Relaxing it just for removals ("live != s.Old && s.Kind != stepRemove")
// leaves the rest of the suite green while the binary deletes whatever the
// user put at the placement path in the window between planning and
// applying. This test fails on that mutant.
func TestRecoveryRefusesARemovalWhoseTargetChanged(t *testing.T) {
	t.Parallel()
	in, u := newRemoval(t, "alpha")
	m := in.removalOf(t, "alpha")
	if err := m.stopAfter(0, u); err != nil { // the journal is on disk, nothing applied
		t.Fatal(err)
	}

	// Between the plan and the apply the user replaces the placement with a
	// file of their own. The journal expects a symlink there.
	live := filepath.Join(in.place, "alpha")
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	const mine = "the only copy of my notes\n"
	if err := os.WriteFile(live, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}

	err := recoverJournals(in.dir, u)
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("recovery = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), live) {
		t.Errorf("the refusal does not name the path that changed: %v", err)
	}
	if b, readErr := os.ReadFile(live); readErr != nil || string(b) != mine {
		t.Errorf("the file the user put at the placement path is %q (%v), want it untouched", b, readErr)
	}
	if left, _ := Journals(in.dir); len(left) != 1 {
		t.Errorf("%d journals left, want the refused one kept for the user to decide on", len(left))
	}
}

// TestRemovalKeepsRetainedContentThatChanged is the rule that matters most
// in a removal: the content a copy placement held is retained until the
// outcome is verified, and dropped only when it still hashes to what the
// journal captured. Content something else wrote meanwhile is kept and left
// for the user, since locks coordinate agentx commands and not editors.
func TestRemovalKeepsRetainedContentThatChanged(t *testing.T) {
	t.Parallel()
	in, u := newRemoval(t, "alpha")
	m := in.removalOf(t, "alpha")
	// Stop once the copy placement has been moved aside, then write into
	// what was retained, as an editor holding the directory open would.
	moved := 0
	for i, s := range m.j.Steps {
		if s.Path == in.copyPlace("alpha") {
			moved = i + 1
		}
	}
	if err := m.stopAfter(moved, u); err != nil {
		t.Fatal(err)
	}
	retained := ""
	for _, s := range m.j.Steps {
		if s.Path == in.copyPlace("alpha") {
			retained = s.Retained
		}
	}
	if retained == "" {
		t.Fatal("the removal retained nothing of the copy placement")
	}
	if err := os.WriteFile(filepath.Join(retained, "notes.md"), []byte("written after the move\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recoverJournals(in.dir, u); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if left := in.removed(t, u, "alpha"); len(left) > 0 {
		t.Errorf("the removal did not finish: %s is still there", strings.Join(left, ", "))
	}
	b, err := os.ReadFile(filepath.Join(retained, "notes.md"))
	if err != nil || string(b) != "written after the move\n" {
		t.Errorf("the retained content that changed was dropped: %q %v", b, err)
	}
}

// TestRemoveKeepsAndNamesRetainedContentThatChanged is the other half of
// retention: locks coordinate agentx commands, not editors, so the content
// a removal displaced can change while the mutation is unfinished. It is
// then the user's own and is kept, and named, since nothing else ever
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
	m := in.plan(t, "one\n", "alpha")
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
