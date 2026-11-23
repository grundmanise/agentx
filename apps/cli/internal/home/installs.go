package home

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Install is when one skill last entered the library of this machine and
// when it last left it, kept in installs.json by the skill's logical asset:
// a skill of a source by the source's canonical URL and the skill's subpath
// in it, one of the user's own skills by its fork id, never by its name,
// which a rename changes. Only the last of each is kept: an add overwrites
// added and leaves removed as it was, and a removal the other way round. An
// unmanaged skill has no entry, since nothing but its content says which
// skill it is. The file is a record of facts this machine saw happen, one
// writer for each machine, and nothing reads it back to decide anything.
type Install struct {
	Source  string  `json:"source,omitempty"`  // the canonical URL of the source, for a skill of a source
	Subpath *string `json:"subpath,omitempty"` // the skill's directory in that source, "" for its root
	ForkID  string  `json:"fork_id,omitempty"` // for one of the user's own skills
	Name    string  `json:"name"`              // the name it last had on this machine, for display only
	Added   string  `json:"added,omitempty"`   // RFC 3339 in UTC; absent when this machine never saw it added
	Removed string  `json:"removed,omitempty"` // RFC 3339 in UTC; absent when this machine never saw it removed
}

// SourceInstall is the logical asset of the skill called name at subpath
// in the source with the canonical URL url, for RecordInstalls.
func SourceInstall(name, url, subpath string) Install {
	return Install{Source: url, Subpath: &subpath, Name: name}
}

// ForkInstall is the logical asset of the user's own skill called name
// with the fork id id, for RecordInstalls.
func ForkInstall(name, id string) Install { return Install{ForkID: id, Name: name} }

// asset is the key an entry is kept by; "" for one that names no logical
// asset, which is never recorded.
func (i Install) asset() string {
	switch {
	case i.ForkID != "":
		return "fork\x00" + i.ForkID
	case i.Source != "" && i.Subpath != nil:
		return "source\x00" + i.Source + "\x00" + *i.Subpath
	}
	return ""
}

func installsPath(dir string) string { return filepath.Join(dir, "installs.json") }

// LoadInstalls reads the installs file, its entries in the order they are
// stored; a missing file is empty.
func LoadInstalls(dir string) ([]Install, error) {
	path := installsPath(dir)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Install{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Install
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("parse %s: not an installs file", path)
	}
	if list == nil {
		list = []Install{}
	}
	return list, nil
}

// RecordInstalls stamps at on every skill of added as the time it entered
// the library and on every skill of removed as the time it left it, and
// takes the name each one carries as its name, as a journaled replacement
// of the installs file. Call it under the exclusive lock, once the mutation
// that added or removed the skills is applied. An entry that names no
// logical asset is passed over. The file is derived state, so the version
// file is not touched here: the mutation it follows rewrites it.
func RecordInstalls(dir string, at time.Time, added, removed []Install) error {
	if len(added)+len(removed) == 0 {
		return nil
	}
	stored, err := LoadInstalls(dir)
	if err != nil {
		stored = []Install{} // an unreadable file is replaced
	}
	when := at.UTC().Format(time.RFC3339)
	stamp := func(changes []Install, set func(*Install)) {
		for _, c := range changes {
			key := c.asset()
			if key == "" {
				continue
			}
			i := slices.IndexFunc(stored, func(e Install) bool { return e.asset() == key })
			if i < 0 {
				stored = append(stored, Install{Source: c.Source, Subpath: c.Subpath, ForkID: c.ForkID})
				i = len(stored) - 1
			}
			stored[i].Name = c.Name
			set(&stored[i])
		}
	}
	stamp(added, func(e *Install) { e.Added = when })
	stamp(removed, func(e *Install) { e.Removed = when })
	slices.SortFunc(stored, func(a, b Install) int {
		return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(subpathOf(a), subpathOf(b)), cmp.Compare(a.ForkID, b.ForkID))
	})
	b, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(dir, installsPath(dir), append(b, '\n'))
}

func subpathOf(i Install) string {
	if i.Subpath == nil {
		return ""
	}
	return *i.Subpath
}
