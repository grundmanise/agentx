package skillshare

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		env    map[string]string
		config string
	}{
		{"the configuration lives in the base directory", map[string]string{}, "/c/skillshare/config.yaml"},
		{"SKILLSHARE_CONFIG moves the configuration alone", map[string]string{"SKILLSHARE_CONFIG": "/elsewhere/cfg.yaml"}, "/elsewhere/cfg.yaml"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Locate(c.env, "/c")
			if got.Base != "/c/skillshare" || got.Config != c.config {
				t.Errorf("Locate = %+v, want base /c/skillshare and config %s", got, c.config)
			}
		})
	}
}

// write puts content at path, creating the directories on the way.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadConfigSkillsDir(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		config string // "" for no configuration file
		want   string // relative to the base directory unless absolute; ~ is the home
	}{
		{"no configuration", "", "skills"},
		{"a configuration that names no folder", "mode: merge\ntargets:\n  claude:\n    path: ~/.claude/skills\n", "skills"},
		{"sources.skills", "sources:\n  skills: ~/my-skills\n", "~/my-skills"},
		{"the legacy source key", "source: /srv/skills\n", "/srv/skills"},
		{"sources.skills wins over source", "source: /old\nsources:\n  skills: /new\n", "/new"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			home := t.TempDir()
			p := Locate(map[string]string{}, filepath.Join(home, ".config"))
			if c.config != "" {
				write(t, p.Config, c.config)
			}
			cfg, err := ReadConfig(p, home)
			if err != nil {
				t.Fatal(err)
			}
			got := cfg.Skills
			want := c.want
			switch {
			case strings.HasPrefix(want, "~/"):
				want = filepath.Join(home, want[2:])
			case !filepath.IsAbs(want):
				want = filepath.Join(p.Base, want)
			}
			if got != want {
				t.Errorf("SkillsDir = %q, want %q", got, want)
			}
		})
	}
}

func TestReadConfigRefusesAConfigurationItCannotRead(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	p := Locate(map[string]string{}, filepath.Join(home, ".config"))
	write(t, p.Config, "sources: [\n")
	if _, err := ReadConfig(p, home); !errors.Is(err, ErrStore) {
		t.Errorf("error = %v, want an ErrStore", err)
	}
}

func TestLibraryName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"pdf":              "pdf",
		"frontend/ui":      "frontend__ui",
		"team/frontend/ui": "team__frontend__ui",
		"already__flat":    "already__flat",
		"_team-skills/ui":  "_team-skills__ui",
	} {
		if got := LibraryName(in); got != want {
			t.Errorf("LibraryName(%q) = %q, want %q", in, got, want)
		}
	}
}

// store writes a store with content in a fresh directory and returns the
// directory.
func store(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, StoreName), content)
	return dir
}

func TestReadWithNoStore(t *testing.T) {
	t.Parallel()
	entries, warnings, err := Read(t.TempDir())
	if err != nil || len(entries) != 0 || len(warnings) != 0 {
		t.Errorf("Read of an empty directory = %v, %v, %v; want nothing at all", entries, warnings, err)
	}
}

func TestReadRefusesWhatIsNotAStore(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, content string }{
		{"not JSON at all", "{"},
		{"a JSON array", `[]`},
		{"no entries map", `{"version": 1}`},
		{"an entries map that is not an object", `{"version": 1, "entries": []}`},
		{"a null entries map", `{"version": 1, "entries": null}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := Read(store(t, c.content))
			if !errors.Is(err, ErrStore) {
				t.Errorf("error = %v, want an ErrStore", err)
			}
		})
	}
}

func TestReadRefusesAFileTooLargeToBeAStore(t *testing.T) {
	t.Parallel()
	dir := store(t, `{"version": 1, "entries": {"a": {"source": "o/r", "x": "`+strings.Repeat("y", maxBytes)+`"}}}`)
	if _, _, err := Read(dir); !errors.Is(err, ErrStore) {
		t.Errorf("error = %v, want an ErrStore", err)
	}
}

// TestReadJudgesEveryEntryOnItsOwn: one bad entry never costs the others,
// an entry is left out only when it is not an object or names no source,
// and a field of the wrong type is dropped with a warning that names the
// field and never a Go type.
func TestReadJudgesEveryEntryOnItsOwn(t *testing.T) {
	t.Parallel()
	dir := store(t, `{
	  "version": 1,
	  "entries": {
	    "pdf": {"source": "github.com/anthropics/skills/skills/pdf", "type": "github-subdir",
	            "repo_url": "https://github.com/anthropics/skills.git", "subdir": "/skills/pdf/", "branch": "main",
	            "commit": "8f14e45fceea167a5a36dedd4bea2543ce848564", "tree_hash": "f88c8710", "version": "8f14e45",
	            "file_hashes": {"SKILL.md": "sha256:0000"}, "installed_at": "2026-10-12T09:41:07Z"},
	    "frontend/ui": {"source": "github.com/team/skills/ui", "type": "github-subdir", "subdir": "ui",
	                    "group": "frontend", "into": "frontend", "repo_url": "https://github.com/team/skills.git"},
	    "_team": {"source": "github.com/team/skills", "tracked": true},
	    "typed": {"source": "github.com/o/r", "branch": 12, "tracked": "yes"},
	    "sourceless": {"type": "github"},
	    "astring": "not an entry",
	    "twice": {"source": "o/r"},
	    "twice": {"source": "other/r"}
	  }
	}`)
	entries, warnings, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	byName := map[string]Entry{}
	for _, e := range entries {
		names = append(names, e.Name)
		byName[e.Name] = e
	}
	if got := strings.Join(names, " "); got != "_team frontend/ui pdf typed" {
		t.Fatalf("entries = %s, want _team frontend/ui pdf typed, sorted by name", got)
	}
	pdf := byName["pdf"]
	for _, c := range []struct{ what, got, want string }{
		{"source", pdf.Source, "github.com/anthropics/skills/skills/pdf"},
		{"type", pdf.Type, "github-subdir"},
		{"repo_url", pdf.RepoURL, "https://github.com/anthropics/skills.git"},
		{"subdir", pdf.Subdir, "skills/pdf"},
		{"branch", pdf.Branch, "main"},
		{"file", pdf.File, filepath.Join(dir, StoreName)},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}
	if !byName["_team"].Tracked {
		t.Error("_team is not read as tracked")
	}
	if typed := byName["typed"]; typed.Branch != "" || typed.Tracked {
		t.Errorf("typed = %+v, want the branch and tracked dropped", typed)
	}
	text := strings.Join(warnings, "\n")
	for _, want := range []string{"sourceless", "astring", "twice", "branch is not text", "tracked is not true or false"} {
		if !strings.Contains(text, want) {
			t.Errorf("no warning about %s:\n%s", want, text)
		}
	}
	for _, internal := range []string{"struct", "Go ", "cannot unmarshal"} {
		if strings.Contains(text, internal) {
			t.Errorf("a warning puts %q in the user's face:\n%s", internal, text)
		}
	}
}

func TestReadWarnsAboutAVersionItDoesNotKnow(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, content, want string }{
		{"a later version", `{"version": 2, "entries": {}}`, "is version 2"},
		{"no version", `{"entries": {}}`, "says no version"},
		{"a version that is not a number", `{"version": "1", "entries": {}}`, "says no version"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			entries, warnings, err := Read(store(t, c.content))
			if err != nil || len(entries) != 0 {
				t.Fatalf("entries = %#v, err = %v", entries, err)
			}
			if !strings.Contains(strings.Join(warnings, "\n"), c.want) {
				t.Errorf("warnings = %v, want one about %q", warnings, c.want)
			}
		})
	}
}
