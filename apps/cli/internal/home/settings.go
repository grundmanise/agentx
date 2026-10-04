package home

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Settings is the machine settings file, settings.json in agentx home.
// CopyMode is kept as raw JSON: the commands that read it parse it, and a
// write must not lose it.
type Settings struct {
	SchemaVersion          int             `json:"schema_version"`
	Label                  string          `json:"label,omitempty"` // empty until set; the hostname stands in
	AcceptOperations       bool            `json:"accept_operations"`
	IgnoreSystemFiles      bool            `json:"ignore_system_files"` // a file without the key reads true; see SystemFiles
	DisabledConfigurations []string        `json:"disabled_configurations"`
	Sources                []Source        `json:"sources"`
	CopyMode               json.RawMessage `json:"copy_mode"`
}

// Source is one entry of the sources list: a source by its canonical URL,
// whether it is the account remote, what git lets this machine do there,
// the ref it is pinned to and when it was last fetched. A source is fetched
// from and pushed to at its canonical URL.
//
// Alias is a second URL of the one repository the canonical URL names:
// another name of it whose fetches map onto the canonical URL, its old URL
// after a rename for one; nothing sets it yet, and it is never pushed to.
type Source struct {
	URL           string `json:"url"`
	Alias         string `json:"alias,omitempty"`
	Account       bool   `json:"account,omitempty"` // the account remote, which holds one branch per fork and is fetched whole; every other source is shared
	Pin           string `json:"pin,omitempty"`
	Access        string `json:"access,omitempty"`         // AccessWritable or AccessReadOnly, absent while unknown
	AccessChecked string `json:"access_checked,omitempty"` // RFC 3339, when access was last checked, whatever the answer
	DefaultBranch string `json:"default_branch,omitempty"` // the branch the remote's HEAD named at the last look; shown, never followed
	LastFetched   string `json:"last_fetched,omitempty"`   // RFC 3339
}

// The access of a source: what git lets this machine do there. Writable
// means a push was accepted, or would have been; read-only means git
// answered with a denial it is known to give for want of rights. Anything
// else, a check that could not be made among them, is unknown, which an
// entry stores as no access at all.
const (
	AccessWritable = "writable"
	AccessReadOnly = "read-only"
	AccessUnknown  = "unknown"
)

// AccessName is the entry's access as it is reported: unknown when the
// entry records none.
func (s Source) AccessName() string {
	if s.Access == "" {
		return AccessUnknown
	}
	return s.Access
}

// Merge is s, an entry an add has just built, completed with what prev,
// the entry the settings held for the same source, knows and the add did
// not find out. The add decides the canonical URL, the pin, which its
// argument names, and the time it fetched; every other field is the
// source's memory and is kept unless the add set it. The access is kept
// or replaced as a pair with the time of its check, so that an add whose
// check could not decide records that it looked and does not bring back
// an answer it no longer has. The account flag is the add's own: source
// add --account sets it, and only the account remote is added that way.
func (s Source) Merge(prev Source) Source {
	if s.Alias == "" {
		s.Alias = prev.Alias
	}
	if s.AccessChecked == "" && s.Access == "" {
		s.Access, s.AccessChecked = prev.Access, prev.AccessChecked
	}
	if s.DefaultBranch == "" {
		s.DefaultBranch = prev.DefaultBranch
	}
	return s
}

// FindSource returns the index of the source with the canonical URL, or -1.
func (s Settings) FindSource(url string) int {
	for i, src := range s.Sources {
		if src.URL == url {
			return i
		}
	}
	return -1
}

// SetSource adds src or replaces the entry with its URL, keeping the list
// sorted by URL.
func (s *Settings) SetSource(src Source) {
	if i := s.FindSource(src.URL); i >= 0 {
		s.Sources[i] = src
	} else {
		s.Sources = append(s.Sources, src)
	}
	sort.Slice(s.Sources, func(i, j int) bool { return s.Sources[i].URL < s.Sources[j].URL })
}

// RemoveSource deletes the entry with the canonical URL, if any.
func (s *Settings) RemoveSource(url string) {
	if i := s.FindSource(url); i >= 0 {
		s.Sources = append(s.Sources[:i], s.Sources[i+1:]...)
	}
}

// CopyModes reads copy_mode: the configurations that hold a copy of a
// skill instead of a symlink, by the skill's library directory name.
func (s Settings) CopyModes() (map[string][]string, error) {
	modes := map[string][]string{}
	if len(s.CopyMode) == 0 {
		return modes, nil
	}
	if err := json.Unmarshal(s.CopyMode, &modes); err != nil {
		return nil, err
	}
	if modes == nil {
		modes = map[string][]string{}
	}
	return modes, nil
}

// SetCopyModes replaces copy_mode. A skill with no copy anywhere leaves no
// entry behind, so the file says what is rather than what once was.
func (s *Settings) SetCopyModes(modes map[string][]string) error {
	for name, configs := range modes {
		if len(configs) == 0 {
			delete(modes, name)
		}
	}
	b, err := json.Marshal(modes)
	if err != nil {
		return err
	}
	s.CopyMode = b
	return nil
}

func SettingsPath(dir string) string { return filepath.Join(dir, "settings.json") }

// SettingsSchemaVersion is the version of the settings file this agentx
// reads and writes. A field added to the file leaves it where it is: a
// reader ignores a key it does not know. It changes when the file changes
// so that an older agentx would misread it.
const SettingsSchemaVersion = 1

// NewerSettingsError is the error of a settings file a later agentx wrote,
// whose schema version this one does not read. Reading it anyway would
// misread what changed, and the next write would lose it.
type NewerSettingsError struct {
	Path    string
	Version int
}

func (e *NewerSettingsError) Error() string {
	return fmt.Sprintf("%s is of settings schema version %d, and this agentx reads version %d", e.Path, e.Version, SettingsSchemaVersion)
}

// LoadSettings reads the settings file whole. A missing file means
// defaults, and so does a missing key. A file of a later schema version is
// a *NewerSettingsError.
func LoadSettings(dir string) (Settings, error) {
	s := Settings{SchemaVersion: SettingsSchemaVersion, IgnoreSystemFiles: true}
	path := SettingsPath(dir)
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return s, err
	default:
		if err := json.Unmarshal(b, &s); err != nil {
			return s, fmt.Errorf("parse %s: %w", path, err)
		}
		if s.SchemaVersion > SettingsSchemaVersion {
			return s, &NewerSettingsError{Path: path, Version: s.SchemaVersion}
		}
	}
	Normalise(&s)
	return s, nil
}

// Normalise gives the settings the shape agentx writes: a list that is
// empty rather than absent, and a copy_mode that is an object. A file
// agentx wrote has it already; a settings object that came from somewhere
// else, as an import's does, is brought to it before it is written, so that
// every settings file on disk reads the same way.
func Normalise(s *Settings) {
	if s.DisabledConfigurations == nil {
		s.DisabledConfigurations = []string{}
	}
	if s.Sources == nil {
		s.Sources = []Source{}
	}
	if s.CopyMode == nil {
		s.CopyMode = json.RawMessage("{}")
	}
}

// MarshalSettings is the bytes of the settings file, for a mutation that
// writes it beside other changes of its own.
func MarshalSettings(s Settings) ([]byte, error) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// SaveSettings replaces the settings file as a journaled mutation. Call it inside Mutate.
func SaveSettings(dir string, s Settings) error {
	b, err := MarshalSettings(s)
	if err != nil {
		return err
	}
	return replaceFile(dir, SettingsPath(dir), b)
}
