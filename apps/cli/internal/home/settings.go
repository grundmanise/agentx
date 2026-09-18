package home

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Settings is the machine settings file, settings.json in agentx home. Sources
// and CopyMode are kept as raw JSON: the commands that read them parse them,
// and a write must not lose them.
type Settings struct {
	SchemaVersion          int             `json:"schema_version"`
	Label                  string          `json:"label,omitempty"` // empty until set; the hostname stands in
	AutoPush               bool            `json:"auto_push"`
	AcceptOperations       bool            `json:"accept_operations"`
	DisabledConfigurations []string        `json:"disabled_configurations"`
	Sources                json.RawMessage `json:"sources"`
	CopyMode               json.RawMessage `json:"copy_mode"`
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
	if s.DisabledConfigurations == nil {
		s.DisabledConfigurations = []string{}
	}
	if s.Sources == nil {
		s.Sources = json.RawMessage("[]")
	}
	if s.CopyMode == nil {
		s.CopyMode = json.RawMessage("{}")
	}
	return s, nil
}

// SaveSettings writes the settings file atomically. Call it inside Mutate.
func SaveSettings(dir string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(SettingsPath(dir), append(b, '\n'))
}
