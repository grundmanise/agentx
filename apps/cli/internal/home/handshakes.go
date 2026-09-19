package home

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
)

// Handshake is what one server exposed the last time it was handshaken,
// kept in handshakes.json by the server's logical id.
type Handshake struct {
	Signature string `json:"signature"`
	Tools     []Tool `json:"tools"`
	At        string `json:"at"` // RFC 3339, UTC
}

// Tool is one tool, prompt, resource or resource template of a server.
type Tool struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // tool, prompt, resource or resource_template
	Name        string `json:"name"`
	Description string `json:"description"`
}

func handshakesPath(dir string) string { return filepath.Join(dir, "handshakes.json") }

// LoadHandshakes reads the handshakes file; a missing file is empty.
func LoadHandshakes(dir string) (map[string]Handshake, error) {
	path := handshakesPath(dir)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Handshake{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]Handshake
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse %s: not a handshakes file", path)
	}
	if m == nil {
		m = map[string]Handshake{}
	}
	return m, nil
}

// SaveHandshakes replaces the entries in fresh under the exclusive lock as a
// journaled mutation. The file is derived state, so the version file is not
// touched: no other command needs to rescan for it.
func SaveHandshakes(dir string, fresh map[string]Handshake) error {
	return mutate(dir, false, func() error {
		stored, err := LoadHandshakes(dir)
		if err != nil {
			stored = map[string]Handshake{} // an unreadable file is replaced
		}
		maps.Copy(stored, fresh)
		b, err := json.MarshalIndent(stored, "", "  ")
		if err != nil {
			return err
		}
		return replaceFile(dir, "handshakes", handshakesPath(dir), append(b, '\n'))
	})
}
