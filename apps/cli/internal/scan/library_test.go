package scan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// TestReadHashesEachLibrarySkillOnce holds a scan to one hash per library
// skill. A scan that lists the library reads it through the same cache as
// the clients that read it directly, Codex and Gemini CLI among them, and
// as a placement that links into it, so however many ways the scan comes
// across a library skill, its files are read and hashed once. A scan, and
// every rescan of serve, pays that once per skill; reading the library a
// second time, through a scanner of its own, would pay it again for every
// client that reads the library.
//
// It swaps hashSkill to count the hashes, so it does not run beside
// another test of this package that scans.
func TestReadHashesEachLibrarySkillOnce(t *testing.T) {
	tests := []struct {
		name        string
		dirs        []string // the client configuration directories under the home
		links       []string // library skills ~/.claude/skills links to
		description string   // what the case is about
	}{
		{
			name: "no client reads the library", dirs: []string{".claude"},
			description: "only the scan's own read of the library reaches it",
		},
		{
			name: "codex reads the library", dirs: []string{".claude", ".codex"},
			description: "Codex lists the library as one of its skills directories",
		},
		{
			name: "gemini cli reads the library", dirs: []string{".claude", ".gemini"},
			description: "Gemini CLI lists the library as one of its skills directories",
		},
		{
			name: "every way at once", dirs: []string{".claude", ".codex", ".gemini"}, links: []string{"alpha"},
			description: "two clients read the library and a placement links into it, so alpha is reached four ways",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := t.TempDir()
			d := home.Dirs{
				User:    user,
				Home:    filepath.Join(user, ".agentx"),
				Library: filepath.Join(user, ".agents", "skills"),
				Config:  filepath.Join(user, ".config"),
			}
			for _, name := range []string{"alpha", "beta"} {
				dir := filepath.Join(d.Library, name)
				mkdir(t, dir)
				write(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+name+"\ndescription: a library skill\n---\n\n"+name+"\n")
			}
			for _, dir := range tt.dirs {
				mkdir(t, filepath.Join(user, dir))
			}
			for _, name := range tt.links {
				mkdir(t, filepath.Join(user, ".claude", "skills"))
				if err := os.Symlink(filepath.Join(d.Library, name), filepath.Join(user, ".claude", "skills", name)); err != nil {
					t.Fatal(err)
				}
			}

			hashes := countHashes(t)
			lib := Read(Options{Dirs: d, MachineID: "m", Library: true}).Library()
			if len(lib) != 2 || lib[0].Name != "alpha" || lib[1].Name != "beta" {
				t.Fatalf("%s: library = %v, want alpha and beta", tt.description, lib)
			}
			for _, s := range lib {
				if n := hashes[s.ResolvedPath]; n != 1 {
					t.Errorf("%s: %s hashed %d times, want 1", tt.description, s.Name, n)
				}
			}
		})
	}
}

// countHashes counts, until the test ends, how many times each skill
// directory is hashed, by its real path.
func countHashes(t *testing.T) map[string]int {
	t.Helper()
	real := hashSkill
	t.Cleanup(func() { hashSkill = real })
	hashes := map[string]int{}
	hashSkill = func(root string, fm frontmatter, warn func(string)) string {
		hashes[root]++
		return real(root, fm, warn)
	}
	return hashes
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReadsLibraryFollowsLinksEitherWay: a skills directory is the library
// when it is named by the library's path, or leads to the library directory
// through a symlink, either way round, since a skill directory in it is
// then the library's own. A directory of its own is not, and neither is
// one that does not resolve, or any directory but the library's own path
// when the library does not resolve.
func TestReadsLibraryFollowsLinksEitherWay(t *testing.T) {
	t.Parallel()
	link := func(t *testing.T, target, path string) {
		t.Helper()
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name  string
		setup func(t *testing.T, library, skills string) // skills is a client's skills directory
		named bool                                       // the library's path is among the directories too
		want  bool
	}{
		{"the library named by its path", func(t *testing.T, library, skills string) {
			mkdir(t, library)
			mkdir(t, skills)
		}, true, true},
		{"a skills directory linked to the library", func(t *testing.T, library, skills string) {
			mkdir(t, library)
			link(t, library, skills)
		}, false, true},
		{"the library linked to a skills directory", func(t *testing.T, library, skills string) {
			mkdir(t, skills)
			link(t, skills, library)
		}, false, true},
		{"a skills directory of its own", func(t *testing.T, library, skills string) {
			mkdir(t, library)
			mkdir(t, skills)
		}, false, false},
		{"a skills directory that does not exist", func(t *testing.T, library, _ string) {
			mkdir(t, library)
		}, false, false},
		{"a library that does not resolve", func(t *testing.T, _, skills string) {
			mkdir(t, skills)
		}, false, false},
		{"a library that does not resolve, named by its path", func(t *testing.T, _, skills string) {
			mkdir(t, skills)
		}, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			user := t.TempDir()
			library := filepath.Join(user, ".agents", "skills")
			skills := filepath.Join(user, ".claude", "skills")
			mkdir(t, filepath.Dir(library))
			mkdir(t, filepath.Dir(skills))
			tt.setup(t, library, skills)
			dirs := []string{skills}
			if tt.named {
				dirs = append(dirs, library)
			}
			if got := readsLibrary(dirs, library); got != tt.want {
				t.Errorf("readsLibrary(%v, %s) = %t, want %t", dirs, library, got, tt.want)
			}
		})
	}
}
