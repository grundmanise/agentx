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
	AutoPush               bool            `json:"auto_push"`
	AcceptOperations       bool            `json:"accept_operations"`
	DisabledConfigurations []string        `json:"disabled_configurations"`
	Sources                []Source        `json:"sources"`
	CopyMode               json.RawMessage `json:"copy_mode"`
}

// Source is one entry of the sources list: a source by its canonical URL,
// the ref it is pinned to and when it was last fetched. Alias is a second
// URL mapped onto the canonical one; nothing sets it yet.
type Source struct {
	URL         string `json:"url"`
	Alias       string `json:"alias,omitempty"`
	Pin         string `json:"pin,omitempty"`
	LastFetched string `json:"last_fetched,omitempty"` // RFC 3339
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

// LoadSettings reads the settings file whole. A missing file means defaults.
func LoadSettings(dir string) (Settings, error) {
	s := Settings{SchemaVersion: 1}
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
