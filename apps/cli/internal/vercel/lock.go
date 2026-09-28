// Package vercel reads the lock file the vercel skills CLI writes, which is
// how a machine that installed skills with that tool records where each one
// came from. Nothing here writes: the file belongs to another tool, and
// agentx only ever reads it.
package vercel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// LockName is the file the vercel skills CLI keeps its globally installed
// skills in.
const LockName = ".skill-lock.json"

// Version is the lock file schema this reader was written against. A file
// that says something else is still read, entry by entry, with a warning:
// refusing a version that has not been written yet would leave a machine no
// way to adopt what is on it, while the fields a newer file holds are
// either the ones below or ones agentx does not read.
const Version = 3

// maxLockBytes is the largest lock file that is read. The file is a third
// party's and a user's to edit, so it is bounded before it is read whole;
// a lock file of a few hundred skills is a few hundred kilobytes.
const maxLockBytes = 8 << 20

// ErrLock is the error of a file that is not a lock file this can read.
var ErrLock = errors.New("not a skill lock file")

// Entry is one skill of the lock file: the directory it was installed as,
// where it came from and the folder hash the vercel CLI recorded for it.
// The hash is that tool's own and is not a content hash of agentx's; it is
// carried so that a caller can look for a tree of the source that has that
// id, which is what ties the entry to one upstream version.
type Entry struct {
	Name       string // the map key: the library directory the skill was installed into
	Source     string // the normalised source identifier, owner/repo for github
	SourceType string // github, git, gitlab, well-known, ...
	SourceURL  string // the URL the skill was installed from
	Ref        string // the branch or tag it was installed from, "" for the default branch
	Subpath    string // the skill directory inside the source, "" for its root
	FolderHash string // the vercel CLI's folder hash: a git tree id, or its own digest
	Plugin     string // the plugin the skill belongs to, "" for none
	File       string // the lock file this entry was read from
}

// LockPaths are the lock files agentx reads, in the order a later one wins.
// The vercel CLI writes $XDG_STATE_HOME/skills/.skill-lock.json when that
// variable is set and ~/.agents/.skill-lock.json when it is not, so both are
// read and the one that tool would write now comes last: a skill both files
// name is the one the live file records.
func LockPaths(env map[string]string, userHome string) []string {
	state := env["XDG_STATE_HOME"]
	xdg := filepath.Join(state, "skills", LockName)
	if state == "" {
		xdg = filepath.Join(userHome, ".local", "state", "skills", LockName)
	}
	agents := filepath.Join(userHome, ".agents", LockName)
	if state != "" {
		return []string{agents, xdg}
	}
	return []string{xdg, agents}
}

// Read reads every lock file of paths that exists and returns their entries
// by name, sorted by name, with the warnings the reads produced. A later
// path's entry replaces an earlier one's, and a disagreement between the two
// is one warning naming both files. A file that is not there contributes
// nothing; a file that is not a lock file at all is an error, since a caller
// asked to adopt what it holds cannot be told that it holds nothing.
func Read(paths []string) ([]Entry, []string, error) {
	var warnings []string
	byName := map[string]Entry{}
	seen := map[string]bool{} // a path named twice is read once
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		entries, w, err := ReadFile(p)
		warnings = append(warnings, w...)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, warnings, err
		}
		for _, e := range entries {
			if have, ok := byName[e.Name]; ok && !have.sameCoordinates(e) {
				warnings = append(warnings, fmt.Sprintf("%s is in %s and in %s with different coordinates; the entry in %s is the one used",
					e.Name, have.File, e.File, e.File))
			}
			byName[e.Name] = e
		}
	}
	entries := make([]Entry, 0, len(byName))
	for _, e := range byName {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, warnings, nil
}

// sameCoordinates reports whether two entries say the same thing about
// where a skill came from, which is all a reader compares them by: the file
// each was read from differs by definition.
func (e Entry) sameCoordinates(other Entry) bool {
	a, b := e, other
	a.File, b.File = "", ""
	return a == b
}

// ReadFile reads one lock file. A file that is not there is fs.ErrNotExist;
// one that is not JSON, is larger than a lock file may be, or holds no
// skills map is an ErrLock. Everything below that is a warning and an entry
// left out: a lock file is a third party's, and one bad entry may not cost
// the reader every other.
func ReadFile(path string) ([]Entry, []string, error) {
	b, err := readBounded(path)
	if err != nil {
		return nil, nil, err
	}
	// The file is read field by field rather than into a struct: a version
	// written as something other than a number may not cost the reader the
	// skills map beside it, which is the part an adoption needs.
	var file map[string]json.RawMessage
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrLock, path, err)
	}
	var warnings []string
	var version int
	if raw, ok := file["version"]; !ok || json.Unmarshal(raw, &version) != nil {
		warnings = append(warnings, fmt.Sprintf("%s says no version; it is read as version %d", path, Version))
	} else if version != Version {
		warnings = append(warnings, fmt.Sprintf("%s is version %d and this agentx reads version %d; an entry it cannot read is left out", path, version, Version))
	}
	skills, ok := file["skills"]
	if !ok {
		return nil, warnings, fmt.Errorf("%w: %s holds no skills map", ErrLock, path)
	}
	raw, dup, err := objectFields(skills)
	if err != nil {
		return nil, warnings, fmt.Errorf("%w: %s: %v", ErrLock, path, err)
	}
	entries := make([]Entry, 0, len(raw))
	for _, name := range sortedKeys(raw) {
		if dup[name] {
			// The file says two things about one skill and there is no
			// telling which install is the one on disk.
			warnings = append(warnings, fmt.Sprintf("%s names %s twice and is ambiguous about it; it is left out", path, name))
			continue
		}
		e, notes, err := parseEntry(name, raw[name])
		for _, note := range notes {
			warnings = append(warnings, fmt.Sprintf("%s in %s: %s", name, path, note))
		}
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s in %s is left out: %v", name, path, err))
			continue
		}
		e.File = path
		entries = append(entries, e)
	}
	return entries, warnings, nil
}

// readBounded reads path, refusing a file larger than a lock file may be
// before it is in memory.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxLockBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxLockBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrLock, path, maxLockBytes)
	}
	return b, nil
}

// objectFields splits a JSON object into its fields without losing a key
// given twice, which encoding/json resolves silently by keeping the last.
// A lock file that names one skill twice says two things about it, and the
// caller has to know rather than be handed one of them.
func objectFields(raw json.RawMessage) (fields map[string]json.RawMessage, duplicate map[string]bool, err error) {
	duplicate = map[string]bool{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	// The opening brace is read before anything else: JSON null unmarshals
	// into a map without an error and leaves it nil, so a file whose skills
	// are null would otherwise read as a file holding no skill rather than
	// as one holding no skills map at all, which is the one answer a
	// command asked to adopt what a file holds may not give.
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return nil, nil, errors.New("skills is not an object")
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, nil, errors.New("skills is not an object")
		}
		if seen[name] {
			duplicate[name] = true
		}
		seen[name] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, nil, err
		}
	}
	return fields, duplicate, nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseEntry reads one entry of the skills map. What it insists on is what
// an adoption cannot do without: a name, and a source to look it up in. An
// entry is left out for that and for nothing else, so a field written as
// something other than the text a lock file holds there is one field the
// reader cannot use and not a skill it forgets: the field is dropped, the
// note says which one, and the entry is read on.
//
// Every field is read on its own for the same reason, and what a note says
// is the field the user can look at in their file, never the shape of the
// value agentx tried to read it into.
func parseEntry(name string, raw json.RawMessage) (Entry, []string, error) {
	if name == "" {
		return Entry{}, nil, errors.New("it has no name")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Entry{}, nil, errors.New("it is not an object")
	}
	e, notes := Entry{Name: name}, []string(nil)
	for field, into := range map[string]*string{
		"source":          &e.Source,
		"sourceType":      &e.SourceType,
		"sourceUrl":       &e.SourceURL,
		"ref":             &e.Ref,
		"skillPath":       &e.Subpath,
		"skillFolderHash": &e.FolderHash,
		"pluginName":      &e.Plugin,
	} {
		raw, ok := fields[field]
		if !ok || string(raw) == "null" {
			continue
		}
		if err := json.Unmarshal(raw, into); err != nil {
			*into = ""
			notes = append(notes, field+" is not text and is left out of the entry")
		}
	}
	sort.Strings(notes)
	if e.Source == "" && e.SourceURL == "" {
		return Entry{}, notes, errors.New("it names no source")
	}
	e.Subpath = SkillSubpath(e.Subpath)
	return e, notes, nil
}

// SkillSubpath is the skill's directory inside its source, read from the
// skillPath of a lock entry. That field names the SKILL.md as often as the
// directory holding it, and it is written with the separator of whichever
// machine installed the skill, so both are taken back to one directory path
// from the repository root, "" for a skill at the root.
func SkillSubpath(skillPath string) string {
	p := strings.ReplaceAll(skillPath, `\`, "/")
	if base := path.Base(p); strings.EqualFold(base, "SKILL.md") {
		p = path.Dir(p)
	}
	p = strings.Trim(p, "/")
	if p == "" || p == "." {
		return ""
	}
	return p
}
