package home

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ErrRecovery is returned when an unfinished journal cannot be resumed or
// finished because a live path no longer holds what the journal expects.
var ErrRecovery = errors.New("recovery required")

func MutationsDir(dir string) string { return filepath.Join(dir, "mutations") }

// journal is one mutations/<id>.json: a mutation that replaces live files
// with staged content and, for an install, publishes library directories,
// creates placements and moves lineage refs. It is written durably before
// the first live path changes, so that a process stopped anywhere leaves a
// state a later command can decide from.
type journal struct {
	Progress string        `json:"progress"` // staged, then applied once every live path holds its new state
	Replace  []replacement `json:"replace"`
	Steps    []step        `json:"steps,omitempty"`
}

// replacement is one live file the mutation replaces: Old and New are the
// SHA-256 hex of its content before and after, Old "absent" when there was
// no file, and Staged holds the new content until it is renamed over Path.
type replacement struct {
	Path   string `json:"path"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Staged string `json:"staged"`
}

// step is one change outside the state files of agentx home: a lineage ref,
// a library directory, a copy placement or a symlink placement. Old and New
// are the live state before and after the step, as liveState renders it for
// a path and as an object id (empty for none) for a ref. Every step is
// decided from the live state and is safe to repeat.
type step struct {
	Kind     string `json:"kind"`               // ref, publish, link or remove
	Path     string `json:"path,omitempty"`     // the live path of publish, link and remove
	GitDir   string `json:"git_dir,omitempty"`  // the repository of a ref step
	Ref      string `json:"ref,omitempty"`      // the ref it moves
	Old      string `json:"old"`                // what the step expects to find
	New      string `json:"new"`                // what it leaves behind
	Staged   string `json:"staged,omitempty"`   // the directory publish renames into place
	Retained string `json:"retained,omitempty"` // where remove keeps the content it displaced
}

// The step kinds.
const (
	stepRef     = "ref"     // create or move a lineage ref, with an expected old value
	stepPublish = "publish" // rename a staged directory into place
	stepLink    = "link"    // create a symlink placement
	stepRemove  = "remove"  // take a path out of the way, retaining what it held
)

// The live states a path step compares. A directory carries the
// fingerprint of its content, so that a directory replaced by hand is
// never mistaken for the one the mutation expected.
const (
	absent = "absent"
	other  = "other" // something the mutation does not know how to replace
	linkTo = "link:"
	dirOf  = "dir:"
)

// RefUpdate is one ref a journal moves: to New, and only from Old, which
// is empty for a ref that must not exist yet.
type RefUpdate struct {
	Ref string
	New string
	Old string
}

// Warner hears about what a mutation or a recovery kept and could not give
// back. The journal has no streams of its own, so a command that wants to
// tell its user implements it on the RefUpdater it hands over; one that
// does not is told nothing and loses nothing.
type Warner interface {
	Warn(message string)
}

// warn tells u, when it listens, about something the journal kept.
func warn(u RefUpdater, message string) {
	if w, ok := u.(Warner); ok {
		w.Warn(message)
	}
}

// RefUpdater applies the ref steps of a journal. The journal cannot run git
// itself, so every command that mutates agentx home passes one; recovery
// needs it for the same steps and refuses a journal it cannot finish
// without one.
//
// Both methods take every ref of one repository at once, because a journal
// holds one ref per skill and an install of thirty skills may not cost
// thirty git processes.
type RefUpdater interface {
	// RefValues are the object ids the named refs hold in gitDir, a ref
	// that holds none being absent from the map.
	RefValues(gitDir string, refs []string) (map[string]string, error)
	// UpdateRefs points every ref at its new value, requiring it to hold its
	// old value now, and applies the whole batch or none of it.
	UpdateRefs(gitDir string, updates []RefUpdate) error
}

// Mutation collects one command's changes into one journal: the state files
// it replaces, the library directories it publishes, the placements it
// creates and the lineage refs it moves. Build it under the exclusive lock,
// stage every piece of content, then Apply it: nothing live changes until
// the journal that describes the whole of it is on disk.
type Mutation struct {
	dir    string // agentx home
	id     string
	j      journal
	staged int // how many staged paths were handed out, so each one is its own
	// journaled says the journal reached the disk, which is the line
	// between a failure that leaves nothing and one recovery has to finish.
	journaled bool
}

// NewMutation starts a mutation of agentx home dir. Call it under the
// exclusive lock.
func NewMutation(dir string) *Mutation {
	var suffix [4]byte
	_, _ = rand.Read(suffix[:])
	return &Mutation{dir: dir, id: fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(suffix[:]))}
}

// StagePath returns a fresh path next to the journal for the content of a
// file the mutation replaces.
func (m *Mutation) StagePath() string {
	m.staged++
	return filepath.Join(MutationsDir(m.dir), fmt.Sprintf("%s.staged.%d", m.id, m.staged))
}

// Sibling is where the mutation builds or keeps content that belongs to a
// live path: a hidden directory next to it, so the rename that publishes or
// displaces it stays on one filesystem and no agent client ever sees a
// half-written skill, discovery skipping hidden entries. kind says what it
// holds, staged or retained.
func (m *Mutation) Sibling(live, kind string) string {
	m.staged++
	return filepath.Join(filepath.Dir(live), fmt.Sprintf(".agentx-%s-%s-%d", kind, m.id, m.staged))
}

// ReplaceFile stages data for the live file path.
func (m *Mutation) ReplaceFile(path string, data []byte) error {
	staged := m.StagePath()
	if err := writeAtomic(staged, data); err != nil {
		return err
	}
	old, err := hashFile(path)
	if err != nil {
		os.Remove(staged)
		return err
	}
	m.j.Replace = append(m.j.Replace, replacement{Path: path, Old: old, New: hash(data), Staged: staged})
	return nil
}

// Publish records that the directory staged at staged becomes the live
// directory path, which must be absent when the step runs.
func (m *Mutation) Publish(path, staged, fingerprint string) {
	m.j.Steps = append(m.j.Steps, step{Kind: stepPublish, Path: path, Old: absent, New: dirOf + fingerprint, Staged: staged})
}

// Link records a symlink placement at path pointing at target.
func (m *Mutation) Link(path, target string) {
	m.j.Steps = append(m.j.Steps, step{Kind: stepLink, Path: path, Old: absent, New: linkTo + target})
}

// Remove records that path, which holds old, goes out of the way. Content
// is retained beside it and kept until the mutation is verified; a symlink
// holds nothing to retain and is only unlinked.
func (m *Mutation) Remove(path, old string) {
	s := step{Kind: stepRemove, Path: path, Old: old, New: absent}
	if strings.HasPrefix(old, dirOf) {
		s.Retained = m.Sibling(path, "retained")
	}
	m.j.Steps = append(m.j.Steps, s)
}

// Ref records that ref in gitDir moves from old, empty for a ref that must
// not exist yet, to newValue.
func (m *Mutation) Ref(gitDir, ref, old, newValue string) {
	m.j.Steps = append(m.j.Steps, step{Kind: stepRef, GitDir: gitDir, Ref: ref, Old: old, New: newValue})
}

// Empty reports whether the mutation would change nothing.
func (m *Mutation) Empty() bool { return len(m.j.Replace) == 0 && len(m.j.Steps) == 0 }

// Apply writes the journal durably and then applies it. From the moment the
// journal is on disk a failure leaves it for recovery, which decides from
// the live state what is left to do. Call it under the exclusive lock.
func (m *Mutation) Apply(u RefUpdater) error {
	if m.Empty() {
		return nil
	}
	m.j.Progress = "staged"
	path := m.journalPath()
	if err := writeJournal(path, m.j); err != nil {
		m.Discard() // no journal, so nothing would ever recover this content
		return err
	}
	m.journaled = true
	return apply(path, m.j, u)
}

// Journaled reports whether this mutation's journal reached the disk. It is
// what tells a caller whose work recovery would need (an install's staging
// refs, which hold the commits the journal's ref steps name) whether it
// may take that work away when the run fails. Before the journal exists
// there is nothing to recover and nothing to keep; after it exists there is
// both, whether or not the apply that followed succeeded.
func (m *Mutation) Journaled() bool { return m.journaled }

// Discard removes what the mutation staged, for a command that refuses
// after staging and before Apply: no journal names that content, so the
// next install's sweep is the only thing that would ever remove it, and a
// staging directory in a client directory no later install targets would
// never be removed at all. Apply discards the same way when its journal
// could not be written.
func (m *Mutation) Discard() { m.discardStaged() }

func (m *Mutation) journalPath() string {
	return filepath.Join(MutationsDir(m.dir), m.id+".json")
}

// discardStaged removes what a mutation staged before its journal existed.
func (m *Mutation) discardStaged() {
	for _, r := range m.j.Replace {
		os.Remove(r.Staged)
	}
	for _, s := range m.j.Steps {
		for _, path := range []string{s.Staged, s.Retained} {
			if path != "" {
				os.RemoveAll(path)
			}
		}
	}
}

// replaceFile replaces the live file path with data as one journaled
// mutation, under the exclusive lock the caller holds.
func replaceFile(dir, path string, data []byte) error {
	m := NewMutation(dir)
	if err := m.ReplaceFile(path, data); err != nil {
		return err
	}
	return m.Apply(nil) // a file replacement has no ref step
}

// apply moves the refs, runs the path steps in order and then renames every
// staged file over its live path, marks the journal applied and removes it.
// Retained content is discarded last, once every live path holds its new
// state.
//
// The refs of a journal go together, and first when the journal creates or
// moves them: they are the record of what this machine accepted, they
// depend on no path, and a batch of them is one transaction, so a run
// stopped anywhere leaves either every branch or none and the paths behind
// them to recover. A journal whose ref steps only delete runs them last
// instead; see refsGoLast.
func apply(journalPath string, j journal, u RefUpdater) error {
	last := refsGoLast(j.Steps)
	if !last {
		if _, err := applyRefs(j.Steps, u); err != nil {
			return err
		}
	}
	for _, s := range j.Steps {
		if s.Kind == stepRef {
			continue
		}
		if _, err := applyStep(s, u); err != nil {
			return unfinished(journalPath, s, err)
		}
	}
	for _, r := range j.Replace {
		if err := renameSynced(r.Staged, r.Path); err != nil {
			return unfinished(journalPath, step{Kind: "replace", Path: r.Path}, err)
		}
	}
	if last {
		if _, err := applyRefs(j.Steps, u); err != nil {
			return err
		}
	}
	j.Progress = "applied"
	if err := writeJournal(journalPath, j); err != nil {
		return err
	}
	discardRetained(j, u)
	return os.Remove(journalPath)
}

// unfinished says what a step that could not be applied leaves behind. The
// journal is on disk by now, so every later command recovers it before its
// own work and would hit the same failure: a raw input or output message –
// an unwritable client directory, a read-only mount, a path something else
// took – would then be every command's answer, with nothing in it for the
// reader to do. It becomes a recovery instead, naming the path and the
// journal, which the exit code table calls a refusal and whose hint says
// how to get out of it. A live path that holds what the mutation did not
// put there is already a recovery, and a ref step's failure is the account
// repo's own answer; neither is reworded.
func unfinished(journalPath string, s step, err error) error {
	if err == nil || s.Kind == stepRef || errors.Is(err, ErrRecovery) {
		return err
	}
	return fmt.Errorf("%w: the %s step of the mutation in %s could not be finished at %s: %w",
		ErrRecovery, s.Kind, journalPath, s.Path, err)
}

// applyStep brings one path step's live state to New, doing nothing when it
// is there already, and reports whether it changed anything. A live state
// that is neither Old nor New was changed by something else and is left
// alone. Ref steps are applied by applyRefs, all of them at once.
//
// That comparison is load-bearing for a remove step above all: every other
// step writes something, while remove deletes, so a remove step applied to
// a path that changed since the journal was written destroys whatever the
// user put there and nothing holds a copy of it.
// TestRecoveryRefusesARemovalWhoseTargetChanged is the guard on it.
func applyStep(s step, u RefUpdater) (bool, error) {
	if s.Kind == stepRef {
		return applyRefs([]step{s}, u)
	}
	live, err := liveState(s.Path)
	if err != nil {
		return false, err
	}
	if live == s.New {
		// The step has nothing left to do, so whatever it staged will never
		// be renamed into place. It sits beside the live path, and once the
		// journal is gone nothing names it: only a later install into that
		// same client directory sweeps staging directories, and a removal
		// never does. Two configurations that share one skills directory
		// plan exactly this – two publishes of one path, the second of them
		// already done – so the leak is an ordinary run's, not a crash's.
		if s.Staged != "" {
			os.RemoveAll(s.Staged)
		}
		return false, nil
	}
	if live != s.Old {
		return false, fmt.Errorf("%w: %s holds neither what the mutation expected nor what it was to become", ErrRecovery, s.Path)
	}
	switch s.Kind {
	case stepPublish:
		if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
			return false, err
		}
		return true, renameSynced(s.Staged, s.Path)
	case stepLink:
		if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
			return false, err
		}
		if err := os.Symlink(strings.TrimPrefix(s.New, linkTo), s.Path); err != nil {
			return false, err
		}
		return true, syncDir(filepath.Dir(s.Path))
	case stepRemove:
		if s.Retained == "" {
			if err := os.Remove(s.Path); err != nil {
				return false, err
			}
			return true, syncDir(filepath.Dir(s.Path))
		}
		return true, renameSynced(s.Path, s.Retained)
	}
	return false, fmt.Errorf("%w: unknown step %q in a mutation journal", ErrRecovery, s.Kind)
}

// refsGoLast reports whether this journal's ref steps run after its path
// steps rather than before them. They do when every one of them deletes.
//
// A ref a journal creates or moves is recoverable: the journal holds the
// value, the step is safe to repeat, and a later command finishes it. A ref
// a journal deletes is not. Once it is gone the only record of the commit
// it pointed at is the journal itself, and the journal is exactly what a
// refusal invites the user to move aside to keep what is on disk. Deleting
// first would mean that taking that offer after a removal stopped half way
// left the skill's directory in the library with no branch for it: a
// managed skill turned unmanaged, permanently, which is what mutation
// safety forbids making of half-applied state.
//
// So deletions go last, when everything that could still refuse has not. A
// journal that both creates and deletes refs keeps them first and together,
// since they are one transaction and must not be split; no command writes
// such a journal today.
func refsGoLast(steps []step) bool {
	deletes := false
	for _, s := range steps {
		if s.Kind != stepRef {
			continue
		}
		if s.New != "" {
			return false
		}
		deletes = true
	}
	return deletes
}

// applyRefs moves every lineage ref of a journal with its expected old
// value, so that two commands cannot both create one. A ref that already
// holds the new value is done; one that holds neither value is left alone
// and refuses the recovery. What is left to do is read in one call per
// repository and applied in one, so that a journal of thirty skills is two
// git processes and one transaction rather than sixty processes and thirty
// chances to stop halfway.
func applyRefs(steps []step, u RefUpdater) (bool, error) {
	byDir := map[string][]step{}
	var dirs []string // the repositories in the order the journal names them
	for _, s := range steps {
		if s.Kind != stepRef {
			continue
		}
		if _, seen := byDir[s.GitDir]; !seen {
			dirs = append(dirs, s.GitDir)
		}
		byDir[s.GitDir] = append(byDir[s.GitDir], s)
	}
	if len(dirs) == 0 {
		return false, nil
	}
	if u == nil {
		first := byDir[dirs[0]][0]
		return false, fmt.Errorf("%w: %s in %s needs git to finish; run a command that uses the account repo", ErrRecovery, first.Ref, first.GitDir)
	}
	moved := false
	for _, dir := range dirs {
		refs := make([]string, 0, len(byDir[dir]))
		for _, s := range byDir[dir] {
			refs = append(refs, s.Ref)
		}
		live, err := u.RefValues(dir, refs)
		if err != nil {
			return moved, err
		}
		var updates []RefUpdate
		for _, s := range byDir[dir] {
			switch have := live[s.Ref]; {
			case have == s.New: // done already, by this run or an earlier one
			case have != s.Old:
				return moved, fmt.Errorf("%w: %s holds neither what the mutation expected nor what it was to become", ErrRecovery, s.Ref)
			default:
				updates = append(updates, RefUpdate{Ref: s.Ref, New: s.New, Old: s.Old})
			}
		}
		if len(updates) == 0 {
			continue
		}
		if err := u.UpdateRefs(dir, updates); err != nil {
			return moved, err
		}
		moved = true
	}
	return moved, nil
}

// liveState renders what path holds now, in the words the journal records:
// nothing, a symlink and where it points, a directory and the fingerprint
// of its content, or something a mutation does not replace.
func liveState(path string) (string, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return absent, nil
	case err != nil:
		return "", err
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		return linkTo + target, nil
	case info.IsDir():
		fp, err := Fingerprint(path)
		if err != nil {
			return "", err
		}
		return dirOf + fp, nil
	}
	return other, nil
}

// Fingerprint identifies what a directory holds, so that a mutation can
// tell the content it captured from content something else wrote. It is
// SHA-256 over every entry below path in bytewise order of its relative
// path: the path, NUL, a kind and a length, NUL, then the file's bytes or
// the symlink's target. Symlinks are recorded as they are rather than
// followed, so nothing outside the directory contributes. This is not the
// content hash of the CLI contract, which names a version of a skill; it
// names the bytes on disk, SKILL.md frontmatter and all.
func Fingerprint(path string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			fmt.Fprintf(h, "%s\x00d\x00", rel)
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s\x00l%d\x00%s", rel, len(target), target)
		case d.Type().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			mode := "f"
			if info, err := d.Info(); err == nil && info.Mode()&0o111 != 0 {
				mode = "x"
			}
			fmt.Fprintf(h, "%s\x00%s%d\x00", rel, mode, len(b))
			h.Write(b)
		default:
			fmt.Fprintf(h, "%s\x00?\x00", rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// State is what path holds now, in the words a journal records, for a
// caller that captures what it is about to replace.
func State(path string) (string, error) { return liveState(path) }

// IsAbsent reports whether a state says the path holds nothing.
func IsAbsent(state string) bool { return state == absent }

// IsDir reports whether a state says the path is a real directory.
func IsDir(state string) bool { return strings.HasPrefix(state, dirOf) }

// IsLink reports whether a state says the path is a symlink, wherever it
// points. What it points at says nothing about whether it may be replaced:
// a link is the user's own, and where it leads is beside the point.
func IsLink(state string) bool { return strings.HasPrefix(state, linkTo) }

// LinkTarget is where a state says the symlink points, and false when the
// state is not a symlink. It is the link as it was written, relative or
// absolute, which is what a report about it should name.
func LinkTarget(state string) (string, bool) { return strings.CutPrefix(state, linkTo) }

// IsDangling reports whether path is a symlink that resolves to nothing,
// which is what a placement into a fork worktree that is gone looks like.
func IsDangling(path string) bool {
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	_, err := os.Stat(path)
	return err != nil
}

func writeJournal(path string, j journal) error {
	b, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'))
}

// Journals lists the unfinished journals of agentx home, oldest first.
func Journals(dir string) ([]string, error) {
	entries, err := os.ReadDir(MutationsDir(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries { // sorted by name, and the ids are time-ordered
		if strings.HasSuffix(e.Name(), ".json") {
			paths = append(paths, filepath.Join(MutationsDir(dir), e.Name()))
		}
	}
	return paths, nil
}

// Recover finishes every unfinished journal under the exclusive lock,
// waiting for a mutation in progress until ctx is done. Mutations recover
// under their own lock before their own journal; this is for scans.
func Recover(ctx context.Context, dir string, u RefUpdater) error {
	lock, err := waitLock(ctx, dir, syscall.LOCK_EX)
	if err != nil {
		return err
	}
	defer lock.Close()
	return recoverJournals(dir, u)
}

// recoverJournals resumes or finishes each journal from what its live paths
// hold, not from the recorded progress, since a process can stop after a
// write and before recording it. A path that holds the new state is done;
// one that holds the old state gets the step applied; anything else is left
// as it is, journal included, and named in an ErrRecovery. Every step is
// safe to repeat. Staged content without a journal, left by a process that
// stopped between the two writes, is removed: the lock the caller holds
// rules out a mutation staging now.
func recoverJournals(dir string, u RefUpdater) error {
	journals, err := Journals(dir)
	if err != nil {
		return err
	}
	for _, path := range journals {
		if err := recoverJournal(dir, path, u); err != nil {
			return err
		}
	}
	orphans, err := filepath.Glob(filepath.Join(MutationsDir(dir), "*.staged*"))
	if err != nil {
		return err
	}
	for _, staged := range orphans {
		os.RemoveAll(staged)
	}
	return nil
}

func recoverJournal(dir, journalPath string, u RefUpdater) error {
	b, err := os.ReadFile(journalPath)
	if err != nil {
		return err
	}
	var j journal
	if err := json.Unmarshal(b, &j); err != nil || len(j.Replace)+len(j.Steps) == 0 {
		return fmt.Errorf("%w: %s is not a mutation journal", ErrRecovery, journalPath)
	}
	last := refsGoLast(j.Steps)
	var resumed bool
	if !last {
		moved, err := applyRefs(j.Steps, u)
		if err != nil {
			return err
		}
		resumed = moved
	}
	for _, s := range j.Steps {
		if s.Kind == stepRef {
			continue
		}
		if s.Staged != "" {
			if _, err := os.Lstat(s.Staged); err != nil {
				// The staged directory is gone, so the step can only be
				// finished if the live path already holds it.
				live, liveErr := liveState(s.Path)
				if liveErr != nil {
					return unfinished(journalPath, s, liveErr)
				}
				if live == s.New {
					continue
				}
				// A staging directory is gone for one of two reasons, and
				// the live path says which: it still holds what the step
				// expected, so the content was lost before it was
				// published, or it holds something else, so the step did
				// publish and the live path changed afterwards. Blaming
				// the staging directory for the second sends the reader
				// looking for a directory that is supposed to be gone.
				if live == s.Old {
					return fmt.Errorf("%w: the staged content %s of the mutation in %s is missing", ErrRecovery, s.Staged, journalPath)
				}
				return fmt.Errorf("%w: %s changed after the mutation in %s put the new content there", ErrRecovery, s.Path, journalPath)
			}
		}
		done, err := applyStep(s, u)
		if err != nil {
			return unfinished(journalPath, s, err)
		}
		resumed = resumed || done
	}
	for _, r := range j.Replace {
		live, err := hashFile(r.Path)
		if err != nil {
			return unfinished(journalPath, step{Kind: "replace", Path: r.Path}, err)
		}
		if live == r.New {
			continue
		}
		if live != r.Old {
			return fmt.Errorf("%w: %s changed while the mutation in %s was unfinished", ErrRecovery, r.Path, journalPath)
		}
		if staged, err := hashFile(r.Staged); err != nil {
			return err
		} else if staged != r.New {
			return fmt.Errorf("%w: the staged content %s of the mutation in %s is missing or changed", ErrRecovery, r.Staged, journalPath)
		}
		if err := renameSynced(r.Staged, r.Path); err != nil {
			return unfinished(journalPath, step{Kind: "replace", Path: r.Path}, err)
		}
		resumed = true
	}
	for _, r := range j.Replace {
		os.Remove(r.Staged) // left behind when the live file already held the new content
	}
	if last {
		moved, err := applyRefs(j.Steps, u)
		if err != nil {
			return err
		}
		resumed = resumed || moved
	}
	discardRetained(j, u)
	if resumed {
		if err := bumpVersion(dir); err != nil {
			return err
		}
	}
	return os.Remove(journalPath)
}

// discardRetained drops the content a removal displaced, once every live
// path holds its new state. It is dropped only when it still hashes to what
// the journal captured: locks coordinate agentx commands, not editors, and
// content that changed under us is kept and named, since it is the user's
// and nothing else will ever mention it – the sweep of the next install
// covers staging directories alone, by design.
func discardRetained(j journal, u RefUpdater) {
	for _, s := range j.Steps {
		if s.Kind != stepRemove || s.Retained == "" {
			continue
		}
		fp, err := Fingerprint(s.Retained)
		switch {
		case err == nil && dirOf+fp == s.Old:
			os.RemoveAll(s.Retained)
		case err == nil:
			warn(u, "what "+s.Path+" held changed while the change was unfinished and is kept at "+s.Retained)
		}
	}
}

// hashFile is the SHA-256 hex of the file at path, or absent when there is none.
func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return absent, nil
	}
	if err != nil {
		return "", err
	}
	return hash(b), nil
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
