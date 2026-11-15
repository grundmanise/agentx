package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestManifestWebURL reads where a plugin can be seen off its manifest:
// the repository, a string or npm's object with a url, before the
// homepage, each as a web URL, and a value of another type or shape as
// none.
func TestManifestWebURL(t *testing.T) {
	t.Parallel()
	for manifest, want := range map[string]string{
		`{"repository": "https://github.com/acme/formatter", "homepage": "https://acme.dev"}`: "https://github.com/acme/formatter",
		`{"repository": {"type": "git", "url": "git+https://github.com/acme/formatter.git"}}`: "https://github.com/acme/formatter",
		`{"repository": "git@github.com:acme/formatter.git"}`:                                 "https://github.com/acme/formatter",
		`{"repository": "./not-a-url", "homepage": "https://acme.dev/formatter/"}`:            "https://acme.dev/formatter",
		`{"repository": 42, "homepage": "https://acme.dev"}`:                                  "https://acme.dev",
		`{"homepage": "https://me:secret@acme.dev"}`:                                          "https://acme.dev",
		`{"homepage": ["https://acme.dev"]}`:                                                  "",
		`{"homepage": "file:///home/me/formatter"}`:                                           "",
		`{"name": "formatter"}`:                                                               "",
	} {
		var m pluginManifest
		if err := json.Unmarshal([]byte(manifest), &m); err != nil {
			t.Fatalf("%s: %v", manifest, err)
		}
		if got := m.webURL(); got != want {
			t.Errorf("%s: webURL = %q, want %q", manifest, got, want)
		}
	}
}

// TestMarketplaceURLs reads the web URL of each marketplace Claude Code
// knows of from its GitHub repository or git URL, and none for another
// kind of source or a value of another shape; a file that is missing or
// is no JSON object gives none at all.
func TestMarketplaceURLs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "known_marketplaces.json")
	if got := marketplaceURLs(path); len(got) != 0 {
		t.Errorf("a missing file gives %v", got)
	}
	if err := os.WriteFile(path, []byte(`{
		"acme": {"source": {"source": "github", "repo": "acme/plugins"}, "installLocation": "/x", "lastUpdated": "2026-09-01T00:00:00.000Z"},
		"tools": {"source": {"source": "git", "url": "https://me:token@gitlab.com/team/tools.git"}},
		"ssh": {"source": {"source": "git", "url": "git@git.example.com:team/more.git"}},
		"local": {"source": {"source": "directory", "path": "/home/me/marketplace"}},
		"remote": {"source": {"source": "url", "url": "https://example.com/marketplace.json"}},
		"odd": {"source": {"source": "github", "repo": "../../evil"}},
		"broken": {"source": "github"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"acme":  "https://github.com/acme/plugins",
		"tools": "https://gitlab.com/team/tools",
		"ssh":   "https://git.example.com/team/more",
	}
	if got := marketplaceURLs(path); !reflect.DeepEqual(got, want) {
		t.Errorf("marketplaceURLs = %v, want %v", got, want)
	}
	if err := os.WriteFile(path, []byte(`["acme"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := marketplaceURLs(path); len(got) != 0 {
		t.Errorf("a file that is no object gives %v", got)
	}
}
