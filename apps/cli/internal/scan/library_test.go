package scan

import (
	"os"
	"path/filepath"
	"slices"
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

// TestLinksToLibraryEitherWay: a skills directory links to the library
// when it leads to the library directory through a symlink, either way
// round, since a skill directory in it is then the library's own. The
// library's own path, a directory of its own, one that does not resolve and
// any directory when the library does not resolve do not.
func TestLinksToLibraryEitherWay(t *testing.T) {
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
		want  bool
	}{
		{"a skills directory linked to the library", func(t *testing.T, library, skills string) {
			mkdir(t, library)
			link(t, library, skills)
		}, true},
		{"the library linked to a skills directory", func(t *testing.T, library, skills string) {
			mkdir(t, skills)
			link(t, skills, library)
		}, true},
		{"a skills directory of its own", func(t *testing.T, library, skills string) {
			mkdir(t, library)
			mkdir(t, skills)
		}, false},
		{"a skills directory that does not exist", func(t *testing.T, library, _ string) {
			mkdir(t, library)
		}, false},
		{"a library that does not resolve", func(t *testing.T, _, skills string) {
			mkdir(t, skills)
		}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			user := t.TempDir()
			library := filepath.Join(user, ".agents", "skills")
			skills := filepath.Join(user, ".claude", "skills")
			mkdir(t, filepath.Dir(library))
			mkdir(t, filepath.Dir(skills))
			tt.setup(t, library, skills)
			if got := linksToLibrary(skills, library); got != tt.want {
				t.Errorf("linksToLibrary(%s, %s) = %t, want %t", skills, library, got, tt.want)
			}
			if linksToLibrary(library, library) {
				t.Errorf("the library's own path links to itself")
			}
		})
	}
}

// TestALinkedSkillsDirectoryDoesNotReadTheLibrary: Claude Code with
// ~/.claude/skills made a link to the library reads its own directory, not
// the library; only readsTheLibrary makes a client read it.
func TestALinkedSkillsDirectoryDoesNotReadTheLibrary(t *testing.T) {
	t.Parallel()
	user := t.TempDir()
	d := home.Dirs{User: user, Library: filepath.Join(user, ".agents", "skills")}
	mkdir(t, d.Library)
	mkdir(t, filepath.Join(user, ".claude"))
	if err := os.Symlink(d.Library, filepath.Join(user, ".claude", "skills")); err != nil {
		t.Fatal(err)
	}
	c, _ := Lookup("claude-code")
	if ReadsLibrary(c, d) {
		t.Error("claude-code reads the library through its linked skills directory")
	}
	if !LinksToLibrary(c, d) {
		t.Error("claude-code's skills directory does not link to the library")
	}
	codex, _ := Lookup("codex")
	if !ReadsLibrary(codex, d) || LinksToLibrary(codex, d) {
		t.Errorf("codex: reads %t, links %t, want true, false", ReadsLibrary(codex, d), LinksToLibrary(codex, d))
	}
}

// TestSkillsDirsOfTheClientsThatReadTheLibrary pins which user skills
// directories the clients read, as their docs and sources say, and so
// whether agentx places a skill for them or counts the library entry as
// their placement.
func TestSkillsDirsOfTheClientsThatReadTheLibrary(t *testing.T) {
	t.Parallel()
	d := home.Dirs{User: "/u", Home: "/u/.agentx", Library: "/u/.agents/skills", Config: "/u/.config"}
	withCopilotHome := d
	withCopilotHome.CopilotHome = "/elsewhere/copilot"
	tests := []struct {
		slug  string
		dirs  home.Dirs
		want  []string
		reads bool
	}{
		{"claude-code", d, []string{"/u/.claude/skills"}, false},
		{"codex", d, []string{"/u/.codex/skills", "/u/.agents/skills"}, true},
		{"gemini-cli", d, []string{"/u/.gemini/skills", "/u/.agents/skills"}, true},
		{"cursor", d, []string{"/u/.cursor/skills", "/u/.agents/skills"}, true},
		{"github-copilot", d, []string{"/u/.copilot/skills", "/u/.agents/skills"}, true},
		{"github-copilot", withCopilotHome, []string{"/elsewhere/copilot/skills"}, false},
		{"opencode", d, []string{"/u/.config/opencode/skills", "/u/.agents/skills"}, true},
		{"cline", d, []string{"/u/.cline/skills", "/u/.agents/skills"}, true},
		{"devin", d, []string{"/u/.config/devin/skills", "/u/.agents/skills"}, true},
	}
	for _, tt := range tests {
		c, ok := Lookup(tt.slug)
		if !ok {
			t.Fatalf("no client %s", tt.slug)
		}
		if got := SkillsDirs(c, tt.dirs); !slices.Equal(got, tt.want) {
			t.Errorf("%s with COPILOT_HOME %q: skills dirs = %v, want %v", tt.slug, tt.dirs.CopilotHome, got, tt.want)
		}
		if got := ReadsLibrary(c, tt.dirs); got != tt.reads {
			t.Errorf("%s with COPILOT_HOME %q: reads the library = %v, want %v", tt.slug, tt.dirs.CopilotHome, got, tt.reads)
		}
	}
	c, _ := Lookup("github-copilot")
	if got := c.ConfigDir(withCopilotHome); got != "/elsewhere/copilot" {
		t.Errorf("Copilot's configuration directory with COPILOT_HOME = %s, want /elsewhere/copilot", got)
	}
}

// TestProjectSkillsDirsFollowTheLibrary covers the project-scope
// directories: a client's own project directory, then the project's
// .agents/skills for a client that reads the library.
func TestProjectSkillsDirsFollowTheLibrary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		slug string
		want []string
	}{
		{"claude-code", []string{".claude/skills"}},
		{"codex", []string{".codex/skills", ".agents/skills"}},
		{"cursor", []string{".cursor/skills", ".agents/skills"}},
		{"github-copilot", []string{".github/skills", ".agents/skills"}},
		{"opencode", []string{".agents/skills"}},
		{"devin", []string{".devin/skills", ".agents/skills"}},
	}
	for _, tt := range tests {
		c, ok := Lookup(tt.slug)
		if !ok {
			t.Fatalf("no client %s", tt.slug)
		}
		if got := ProjectSkillsDirs(c); !slices.Equal(got, tt.want) {
			t.Errorf("%s: project skills dirs = %v, want %v", tt.slug, got, tt.want)
		}
	}
}

// TestReadsTheLibraryNamesRegisteredClients guards readsTheLibrary: every
// client it lists is one the registry knows.
func TestReadsTheLibraryNamesRegisteredClients(t *testing.T) {
	t.Parallel()
	for slug := range readsTheLibrary {
		if _, ok := Lookup(slug); !ok {
			t.Errorf("readsTheLibrary lists %s, which the registry does not know", slug)
		}
	}
}
