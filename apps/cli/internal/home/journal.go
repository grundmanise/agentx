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
// finished because a live file no longer holds what the journal expects.
var ErrRecovery = errors.New("recovery required")

func MutationsDir(dir string) string { return filepath.Join(dir, "mutations") }

// journal is one mutations/<id>.json: a mutation that replaces live files
// with staged content, written durably before the first live file changes.
type journal struct {
	Kind     string        `json:"kind"`
	Progress string        `json:"progress"` // staged, then applied once every live file is replaced
	Replace  []replacement `json:"replace"`
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

const absent = "absent"

// replaceFile replaces the live file path with data as one journaled
// mutation of kind, under the exclusive lock the caller holds: the content
// is staged next to the journal, the journal is written durably, the staged
// file is renamed over path, and the journal is marked applied and removed.
func replaceFile(dir, kind, path string, data []byte) error {
	var suffix [4]byte
	rand.Read(suffix[:])
	id := fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(suffix[:]))
	staged := filepath.Join(MutationsDir(dir), id+".staged")
	journalPath := filepath.Join(MutationsDir(dir), id+".json")
	if err := writeAtomic(staged, data); err != nil {
		return err
	}
	old, err := hashFile(path)
	if err != nil {
		os.Remove(staged)
		return err
	}
	j := journal{Kind: kind, Progress: "staged", Replace: []replacement{{Path: path, Old: old, New: hash(data), Staged: staged}}}
	if err := writeJournal(journalPath, j); err != nil {
		os.Remove(staged) // no journal, so nothing would recover it
		return err
	}
	return apply(journalPath, j) // from here on a failure leaves the journal for recovery
}

// apply renames every staged file over its live path, marks the journal
// applied and removes it.
func apply(journalPath string, j journal) error {
	for _, r := range j.Replace {
		if err := renameSynced(r.Staged, r.Path); err != nil {
			return err
		}
	}
	j.Progress = "applied"
	if err := writeJournal(journalPath, j); err != nil {
		return err
	}
	return os.Remove(journalPath)
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
func Recover(ctx context.Context, dir string) error {
	lock, err := waitLock(ctx, dir, syscall.LOCK_EX)
	if err != nil {
		return err
	}
	defer lock.Close()
	return recoverJournals(dir)
}

// recoverJournals resumes or finishes each journal from what its live files
// hold, not from the recorded progress, since a process can stop after a
// rename and before recording it. A live file that holds the new content is
// done; one that holds the old content while the staged file exists gets
// the rename; anything else is left as it is, journal included, and named
// in an ErrRecovery. Every step is safe to repeat.
func recoverJournals(dir string) error {
	journals, err := Journals(dir)
	if err != nil {
		return err
	}
	for _, path := range journals {
		if err := recoverJournal(dir, path); err != nil {
			return err
		}
	}
	return nil
}

func recoverJournal(dir, journalPath string) error {
	b, err := os.ReadFile(journalPath)
	if err != nil {
		return err
	}
	var j journal
	if err := json.Unmarshal(b, &j); err != nil || len(j.Replace) == 0 {
		return fmt.Errorf("%w: %s is not a mutation journal", ErrRecovery, journalPath)
	}
	resumed := false
	for _, r := range j.Replace {
		live, err := hashFile(r.Path)
		if err != nil {
			return err
		}
		switch {
		case live == r.New:
		case live == r.Old && exists(r.Staged):
			if err := renameSynced(r.Staged, r.Path); err != nil {
				return err
			}
			resumed = true
		case live == r.Old:
			return fmt.Errorf("%w: the staged content %s of the mutation in %s is missing", ErrRecovery, r.Staged, journalPath)
		default:
			return fmt.Errorf("%w: %s changed while the mutation in %s was unfinished", ErrRecovery, r.Path, journalPath)
		}
	}
	for _, r := range j.Replace {
		os.Remove(r.Staged) // left behind when the live file already held the new content
	}
	if resumed {
		if err := bumpVersion(dir); err != nil {
			return err
		}
	}
	return os.Remove(journalPath)
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
