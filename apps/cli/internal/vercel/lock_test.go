package vercel

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockPaths(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		state string
		want  []string
	}{
		{
			name:  "without XDG_STATE_HOME the .agents file is the one that tool writes",
			state: "",
			want:  []string{"/u/.local/state/skills/.skill-lock.json", "/u/.agents/.skill-lock.json"},
		},
		{
			name:  "with XDG_STATE_HOME the XDG file is the one that tool writes",
			state: "/state",
			want:  []string{"/u/.agents/.skill-lock.json", "/state/skills/.skill-lock.json"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := LockPaths(map[string]string{"XDG_STATE_HOME": c.state}, "/u")
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("LockPaths = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSkillSubpath(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"skills/react/SKILL.md", "skills/react"},
		{"skills/react/skill.md", "skills/react"},
		{"skills/react", "skills/react"},
		{"skills/react/", "skills/react"},
		{`skills\react\SKILL.md`, "skills/react"},
		{"SKILL.md", ""},
		{"", ""},
		{"/", ""},
		{".", ""},
		{"../../etc/SKILL.md", "../../etc"}, // kept as it stands; the caller refuses a path that leaves the repository
	} {
		if got := SkillSubpath(c.in); got != c.want {
			t.Errorf("SkillSubpath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// write puts content at a fresh temporary path and returns it.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadFileRefusesWhatIsNotALockFile(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, content string }{
		{"not JSON at all", "{"},
		{"a JSON array", `[]`},
		{"no skills map", `{"version": 3}`},
		{"a skills map that is not an object", `{"version": 3, "skills": []}`},
		{"a null skills map", `{"version": 3, "skills": null}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := ReadFile(write(t, LockName, c.content))
			if !errors.Is(err, ErrLock) {
				t.Errorf("error = %v, want an ErrLock", err)
			}
		})
	}
}

func TestReadFileRefusesAFileTooLargeToBeALockFile(t *testing.T) {
	t.Parallel()
	path := write(t, LockName, `{"version": 3, "skills": {"a": {"source": "o/r", "x": "`+strings.Repeat("y", maxLockBytes)+`"}}}`)
	if _, _, err := ReadFile(path); !errors.Is(err, ErrLock) {
		t.Errorf("error = %v, want an ErrLock", err)
	}
}

func TestReadFileLeavesOutOneBadEntryAndKeepsTheRest(t *testing.T) {
	t.Parallel()
	path := write(t, LockName, `{
	  "version": 3,
	  "skills": {
	    "good": {"source": "owner/repo", "sourceType": "github", "sourceUrl": "https://github.com/owner/repo",
	             "skillPath": "skills/good/SKILL.md", "skillFolderHash": "abc"},
	    "sourceless": {"sourceType": "github", "skillFolderHash": "def"},
	    "astring": "not an entry",
	    "twice": {"source": "owner/repo"},
	    "twice": {"source": "other/repo"}
	  }
	}`)
	entries, warnings, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "good" {
		t.Fatalf("entries = %#v, want the one good entry", entries)
	}
	got := entries[0]
	for _, c := range []struct{ what, got, want string }{
		{"source", got.Source, "owner/repo"},
		{"sourceType", got.SourceType, "github"},
		{"sourceUrl", got.SourceURL, "https://github.com/owner/repo"},
		{"subpath", got.Subpath, "skills/good"},
		{"folder hash", got.FolderHash, "abc"},
		{"file", got.File, path},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}
	for _, want := range []string{"sourceless", "astring", "twice"} {
		if !strings.Contains(strings.Join(warnings, "\n"), want) {
			t.Errorf("no warning about %s:\n%s", want, strings.Join(warnings, "\n"))
		}
	}
}

// TestReadFileKeepsAnEntryWithAFieldOfTheWrongType: an entry is left out
// when it is not an object or names no source, and for nothing else. A
// field of another type than a lock file writes is one field the reader
// cannot use, so it is dropped with a warning naming it and the entry is
// read on. The warning is the user's to act on and never a Go type.
func TestReadFileKeepsAnEntryWithAFieldOfTheWrongType(t *testing.T) {
	t.Parallel()
	path := write(t, LockName, `{
	  "version": 3,
	  "skills": {
	    "alpha": {"source": "owner/repo", "sourceType": "github", "skillPath": "skills/alpha",
	              "skillFolderHash": 12345},
	    "notanobject": ["no"]
	  }
	}`)
	entries, warnings, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "alpha" {
		t.Fatalf("entries = %#v, want alpha kept", entries)
	}
	for _, c := range []struct{ what, got, want string }{
		{"source", entries[0].Source, "owner/repo"},
		{"subpath", entries[0].Subpath, "skills/alpha"},
		{"folder hash", entries[0].FolderHash, ""},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.what, c.got, c.want)
		}
	}
	text := strings.Join(warnings, "\n")
	for _, want := range []string{"skillFolderHash", "notanobject"} {
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

func TestReadFileWarnsAboutAVersionItDoesNotKnow(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, content, want string }{
		{"a later version", `{"version": 4, "skills": {}}`, "is version 4"},
		{"no version", `{"skills": {}}`, "says no version"},
		{"a version that is not a number", `{"version": "3", "skills": {}}`, "says no version"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			entries, warnings, err := ReadFile(write(t, LockName, c.content))
			if err != nil || len(entries) != 0 {
				t.Fatalf("entries = %#v, err = %v", entries, err)
			}
			if !strings.Contains(strings.Join(warnings, "\n"), c.want) {
				t.Errorf("warnings = %v, want one about %q", warnings, c.want)
			}
		})
	}
}

func TestReadMergesTheTwoLocationsWithTheLastWinning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	for path, source := range map[string]string{first: "owner/old", second: "owner/new"} {
		if err := os.WriteFile(path, []byte(`{"version": 3, "skills": {"alpha": {"source": "`+source+`"}, "`+filepath.Base(path)+`": {"source": "o/r"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, warnings, err := Read([]string{first, filepath.Join(dir, "gone.json"), second})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %#v, want three", entries)
	}
	for _, e := range entries {
		if e.Name == "alpha" && e.Source != "owner/new" {
			t.Errorf("alpha came from %q, want the later file's owner/new", e.Source)
		}
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "alpha is in "+first+" and in "+second) {
		t.Errorf("warnings = %v, want one naming both files", warnings)
	}
	// Sorted by name, so that two runs over one machine report in one order.
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name > entries[i].Name {
			t.Errorf("entries are not sorted by name: %v", entries)
		}
	}
}
