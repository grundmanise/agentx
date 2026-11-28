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

// Handshake is what one server declaration exposed the last time it
// answered a handshake, and why its last handshake failed when it did, kept
// in handshakes.json by the declaration's occurrence id. A declaration that
// never answered has no signature and no time. An earlier agentx kept one
// entry per server, by its logical id.
type Handshake struct {
	Signature string            `json:"signature,omitempty"`
	Tools     []Tool            `json:"tools"`
	At        string            `json:"at,omitempty"`    // RFC 3339, UTC
	Error     *HandshakeFailure `json:"error,omitempty"` // absent once the declaration answers again
}

// HandshakeFailure is why the last handshake of a declaration failed.
type HandshakeFailure struct {
	Reason  string `json:"reason"` // one of the mcp reasons
	Message string `json:"message"`
}

// Tool is one tool, prompt, resource or resource template of a server: its
// identity, which stays the same when its definition changes, the
// definition and the hashes that version it.
type Tool struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"` // tool, prompt, resource or resource_template
	Name         string          `json:"name"`
	URI          string          `json:"uri,omitempty"` // a resource's URI, a resource template's URI template
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Hash         string          `json:"hash,omitempty"`        // the whole definition; absent from an entry an earlier agentx stored
	SchemaHash   string          `json:"schema_hash,omitempty"` // the two schemas alone; absent likewise
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
func SaveHandshakes(dir string, u RefUpdater, fresh map[string]Handshake) error {
	return mutate(dir, u, quick(dir), false, func() error {
		stored, err := LoadHandshakes(dir)
		if err != nil {
			stored = map[string]Handshake{} // an unreadable file is replaced
		}
		maps.Copy(stored, fresh)
		b, err := json.MarshalIndent(stored, "", "  ")
		if err != nil {
			return err
		}
		return replaceFile(dir, handshakesPath(dir), append(b, '\n'))
	})
}
