package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// placeRig is a machine skill place is judged on in process, with plain
// directories and no git: a library holding alpha, and the skills
// directories of Claude Code and Cursor, each holding the library's
// symlink, as an install leaves them. What skill place decides before the
// lock is what planPlace reads and refusal answers, so every arrangement a
// refusal turns on is a case here, and the end-to-end tests keep one run
// per refusal message.
type placeRig struct {
	t                *testing.T
	user             string // the user's HOME
	library, lib     string // the library, and alpha's directory in it
	claude, cursor   string // alpha's place in Claude Code's and Cursor's skills directories
	copies, disabled []string
	flags            []string // the --to and --copy a hint repeats
}

func newPlaceRig(t *testing.T) *placeRig {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &placeRig{t: t, user: filepath.Join(root, "home"), library: filepath.Join(root, "library")}
	r.lib = filepath.Join(r.library, "alpha")
	writeFile(t, mkdirs(t, r.lib, "SKILL.md"), skill("alpha", "The first skill"))
	writeFile(t, filepath.Join(r.lib, "notes.md"), "alpha notes\n")
	writeFile(t, mkdirs(t, filepath.Join(r.lib, "scripts"), "run.sh"), "#!/bin/sh\n")
	chmod(t, filepath.Join(r.lib, "scripts", "run.sh"), 0o755)
	r.claude = filepath.Join(r.user, ".claude", "skills", "alpha")
	r.cursor = filepath.Join(r.user, ".cursor", "skills", "alpha")
	for _, place := range []string{r.claude, r.cursor} {
		link(t, r.lib, mkdirs(t, filepath.Dir(place), "alpha"))
	}
	return r
}

// judge is what skill place of alpha decides before the lock, over Claude
// Code, Codex, which reads the library, and Cursor: the refusal without
// --force and with it, each as its message and hint on two lines, "" where
// the run goes ahead.
func (r *placeRig) judge() (plain, forced string) {
	r.t.Helper()
	detected := []placeTarget{
		{id: "claude-code", dir: filepath.Dir(r.claude)},
		{id: "codex", readsLibrary: true},
		{id: "cursor", dir: filepath.Dir(r.cursor)},
	}
	var asked []string
	for _, t := range detected {
		if !slices.Contains(r.disabled, t.id) {
			asked = append(asked, t.id)
		}
	}
	lib, ok := librarySkill(r.library, "alpha")
	if !ok {
		r.t.Fatal("the library holds no alpha")
	}
	inv := &invocation{dirs: home.Dirs{User: r.user, Library: r.library}}
	plan, err := inv.planPlace(lib, detected, asked, r.copies, false)
	if err != nil {
		r.t.Fatalf("planPlace: %v", err)
	}
	plan.flags = r.flags
	return refusalOf(r.t, plan.refusal(false)), refusalOf(r.t, plan.refusal(true))
}

// refusalOf is a refusal as its message and hint on two lines, "" for none.
func refusalOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var f *failure
	if !errors.As(err, &f) || f.status != exitRefused {
		t.Fatalf("%v is no refusal", err)
	}
	return f.message + "\n" + f.hint
}

// moveLibraryEntry moves alpha's library directory to dir and leaves a
// symlink to it as the library entry.
func (r *placeRig) moveLibraryEntry(dir string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.Rename(r.lib, dir); err != nil {
		r.t.Fatal(err)
	}
	link(r.t, dir, r.lib)
}

// linkSkillsDir replaces Cursor's skills directory with a symlink to dir.
func (r *placeRig) linkSkillsDir(dir string) {
	r.t.Helper()
	remove(r.t, filepath.Dir(r.cursor))
	link(r.t, dir, filepath.Dir(r.cursor))
}

// betaAt writes a skill of the user's, beta, at dir and returns its
// directory.
func betaAt(t *testing.T, dir string) string {
	t.Helper()
	writeFile(t, mkdirs(t, dir, "SKILL.md"), skill("beta", "A skill of my own"))
	return dir
}

// TestPlacePlanRefusals holds skill place to its refusals, decided before
// the lock from what the first read found. Where symlinks make what the
// run changes overlap what it must leave alone, it refuses with or without
// --force: a path it changes that is, lies inside or holds the library or
// what a library entry leads to, a place inside another it changes, a
// library entry that is a symlink, or a symlink anywhere in the library
// directory, the last two only where a displaced directory is to be
// removed. A displaced directory whose content differs from the library's
// is replaced only with --force. Where nothing it changes overlaps
// anything, it goes ahead.
func TestPlacePlanRefusals(t *testing.T) {
	t.Parallel()
	differs := func(r *placeRig) string {
		return r.claude + " is a directory whose content differs from the library's alpha, so nothing was placed\n" +
			"to replace it with the library's version and delete what it holds, run 'agentx skill place alpha --force'; to keep it, move it elsewhere first"
	}
	overlap := func(message string) string { return message + "\n" + overlapHint }
	entryLink := func(r *placeRig, target string) string {
		return r.lib + " is a symlink to " + target + ", not the directory agentx installed, so nothing was placed\n" +
			"replace the link with the directory it leads to, then run 'agentx skill place alpha' again"
	}
	heldLink := func(r *placeRig, at string) string {
		return "the library directory " + r.lib + " holds the symlink " + at + ", which no version agentx installs holds, so nothing was placed\n" +
			"replace the link with the files it leads to, or see what changed with 'agentx skill diff alpha' and get the original back by removing the skill and adding it again, then run 'agentx skill place alpha' again"
	}
	for _, c := range []struct {
		name string
		// arrange sets the machine up and returns the refusal without
		// --force and the one with it, "" where the run goes ahead.
		arrange func(t *testing.T, r *placeRig) (plain, forced string)
	}{
		// A displaced directory, judged against the library's content.
		{"nothing wrong", func(*testing.T, *placeRig) (string, string) { return "", "" }},
		{"a missing placement", func(t *testing.T, r *placeRig) (string, string) {
			remove(t, r.cursor)
			return "", ""
		}},
		{"a directory holding the library's content", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, false)
			return "", ""
		}},
		{"a directory with an edited file", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, false)
			writeFile(t, filepath.Join(r.claude, "notes.md"), "alpha notes, edited in the displaced directory\n")
			return differs(r), ""
		}},
		{"a directory with a file made executable", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, false)
			chmod(t, filepath.Join(r.claude, "notes.md"), 0o755)
			return differs(r), ""
		}},
		{"a directory holding the library's repository", func(t *testing.T, r *placeRig) (string, string) {
			writeFile(t, mkdirs(t, filepath.Join(r.lib, "sub", ".git"), "HEAD"), "ref: refs/heads/main\n")
			displace(t, r.lib, r.claude, false)
			return "", ""
		}},
		{"a directory holding a repository that differs", func(t *testing.T, r *placeRig) (string, string) {
			writeFile(t, mkdirs(t, filepath.Join(r.lib, "sub", ".git"), "HEAD"), "ref: refs/heads/main\n")
			displace(t, r.lib, r.claude, false)
			writeFile(t, filepath.Join(r.claude, "sub", ".git", "HEAD"), "ref: refs/heads/other\n")
			return differs(r), ""
		}},
		{"two directories that differ", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			displace(t, r.lib, r.cursor, true)
			return r.claude + ", " + r.cursor + " are directories whose content differs from the library's alpha, so nothing was placed\n" +
				"to replace them with the library's version and delete what they hold, run 'agentx skill place alpha --force'; to keep them, move them elsewhere first", ""
		}},
		{"a directory that differs, placed with --to and --copy", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			r.flags = placeFlags([]string{"claude-code"}, true)
			return strings.Replace(differs(r), "alpha --force", "alpha --to claude-code --copy --force", 1), ""
		}},
		{"a directory that differs, of a disabled configuration", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.cursor, true)
			r.disabled = []string{"cursor"}
			return "", ""
		}},

		// A path the run changes and the library, or what a library entry
		// leads to.
		{"another skill's entry linked into a directory that differs", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			beta := filepath.Join(r.library, "beta")
			link(t, betaAt(t, filepath.Join(r.claude, "beta")), beta)
			m := overlap(entryOverlap(r.claude, "holds", filepath.Join(r.claude, "beta"), beta))
			return m, m
		}},
		{"another skill's entry linked into a directory holding the library's content", func(t *testing.T, r *placeRig) (string, string) {
			betaAt(t, filepath.Join(r.lib, "beta"))
			displace(t, r.lib, r.claude, false)
			beta := filepath.Join(r.library, "beta")
			link(t, filepath.Join(r.claude, "beta"), beta)
			m := overlap(entryOverlap(r.claude, "holds", filepath.Join(r.claude, "beta"), beta))
			return m, m
		}},
		{"another skill's entry linked to the directory itself", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			beta := filepath.Join(r.library, "beta")
			link(t, r.claude, beta)
			m := overlap(entryOverlap(r.claude, "is", r.claude, beta))
			return m, m
		}},
		{"another skill's entry linked to the skills directory above the directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			beta := filepath.Join(r.library, "beta")
			link(t, filepath.Dir(r.claude), beta)
			m := overlap(entryOverlap(r.claude, "lies inside", filepath.Dir(r.claude), beta))
			return m, m
		}},
		{"another skill's entry linked into the library directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			link(t, betaAt(t, filepath.Join(r.lib, "vendored", "beta")), filepath.Join(r.library, "beta"))
			return differs(r), ""
		}},
		{"a displaced directory inside the library", func(t *testing.T, r *placeRig) (string, string) {
			beta := betaAt(t, filepath.Join(r.library, "beta"))
			copyTree(t, r.lib, filepath.Join(beta, "alpha"))
			r.linkSkillsDir(beta)
			m := overlap(libraryOverlap(r.cursor, "lies inside", r.library))
			return m, m
		}},
		{"a displaced directory the library was moved into", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			moved := filepath.Join(r.claude, "library")
			if err := os.Rename(r.library, moved); err != nil {
				t.Fatal(err)
			}
			link(t, moved, r.library)
			m := overlap(libraryOverlap(r.claude, "holds", r.library))
			return m, m
		}},
		{"a missing place inside the library", func(t *testing.T, r *placeRig) (string, string) {
			r.linkSkillsDir(betaAt(t, filepath.Join(r.library, "beta")))
			m := overlap(libraryOverlap(r.cursor, "lies inside", r.library))
			return m, m
		}},
		{"a missing place inside what a library entry leads to", func(t *testing.T, r *placeRig) (string, string) {
			dev := betaAt(t, filepath.Join(r.user, "dev", "beta"))
			beta := filepath.Join(r.library, "beta")
			link(t, dev, beta)
			r.linkSkillsDir(dev)
			m := overlap(entryOverlap(r.cursor, "lies inside", dev, beta))
			return m, m
		}},
		{"a missing place inside what a library entry leads to, reached through that entry", func(t *testing.T, r *placeRig) (string, string) {
			dev := betaAt(t, filepath.Join(r.user, "dev", "beta"))
			beta := filepath.Join(r.library, "beta")
			link(t, dev, beta)
			r.linkSkillsDir(beta)
			m := overlap(entryOverlap(r.cursor, "lies inside", dev, beta))
			return m, m
		}},
		{"a missing copy inside what a library entry leads to", func(t *testing.T, r *placeRig) (string, string) {
			r.copies = []string{"cursor"}
			dev := betaAt(t, filepath.Join(r.user, "dev", "beta"))
			beta := filepath.Join(r.library, "beta")
			link(t, dev, beta)
			r.linkSkillsDir(dev)
			m := overlap(entryOverlap(r.cursor, "lies inside", dev, beta))
			return m, m
		}},
		{"the library's symlink where a copy belongs, inside what a library entry leads to", func(t *testing.T, r *placeRig) (string, string) {
			r.copies = []string{"cursor"}
			dev := betaAt(t, filepath.Join(r.user, "dev", "beta"))
			link(t, r.lib, filepath.Join(dev, "alpha"))
			beta := filepath.Join(r.library, "beta")
			link(t, dev, beta)
			r.linkSkillsDir(dev)
			m := overlap(entryOverlap(r.cursor, "lies inside", dev, beta))
			return m, m
		}},
		{"a copy inside the library", func(t *testing.T, r *placeRig) (string, string) {
			r.copies = []string{"cursor"}
			beta := betaAt(t, filepath.Join(r.library, "beta"))
			copyTree(t, r.lib, filepath.Join(beta, "alpha"))
			r.linkSkillsDir(beta)
			displace(t, r.lib, r.claude, true)
			return differs(r), ""
		}},
		{"a copy inside what a library entry leads to", func(t *testing.T, r *placeRig) (string, string) {
			r.copies = []string{"cursor"}
			dev := betaAt(t, filepath.Join(r.user, "dev", "beta"))
			link(t, dev, filepath.Join(r.library, "beta"))
			copyTree(t, r.lib, filepath.Join(dev, "alpha"))
			r.linkSkillsDir(dev)
			displace(t, r.lib, r.claude, true)
			return differs(r), ""
		}},
		{"a skills directory linked elsewhere", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.cursor, true)
			elsewhere := filepath.Join(r.user, "dotfiles", "cursor-skills")
			if err := os.MkdirAll(filepath.Dir(elsewhere), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Dir(r.cursor), elsewhere); err != nil {
				t.Fatal(err)
			}
			link(t, elsewhere, filepath.Dir(r.cursor))
			return strings.Replace(differs(r), r.claude, r.cursor, 1), ""
		}},
		{"other skills' entries leading no further than where they lead", func(t *testing.T, r *placeRig) (string, string) {
			// Another skill's entry is followed to where it leads and no
			// further, whatever it holds there: links round in circles, a
			// link back to the library, a directory this machine cannot
			// read, or nothing at all. Nor does a link deep inside another
			// skill's directory stop anything.
			displace(t, r.lib, r.claude, true)
			beta := betaAt(t, filepath.Join(r.library, "beta"))
			link(t, beta, filepath.Join(beta, "loop"))
			link(t, r.user, filepath.Join(beta, "home"))
			dev := betaAt(t, filepath.Join(r.user, "dev", "gamma"))
			link(t, dev, filepath.Join(dev, "self"))
			link(t, r.library, filepath.Join(dev, "library"))
			link(t, filepath.Dir(dev), filepath.Join(dev, "up"))
			link(t, dev, filepath.Join(r.library, "gamma"))
			link(t, filepath.Join(r.user, "gone"), filepath.Join(r.library, "delta"))
			private := betaAt(t, filepath.Join(r.user, "dev", "private"))
			link(t, private, filepath.Join(r.library, "epsilon"))
			chmod(t, private, 0)
			t.Cleanup(func() { _ = os.Chmod(private, 0o755) }) // so the temporary directory can be removed
			return differs(r), ""
		}},
		{"another skill's entry leading elsewhere, with a placement missing", func(t *testing.T, r *placeRig) (string, string) {
			link(t, betaAt(t, filepath.Join(r.user, "dev", "beta")), filepath.Join(r.library, "beta"))
			remove(t, r.cursor)
			return "", ""
		}},

		// A place inside another the run changes.
		{"a client's place inside another client's displaced directory", func(t *testing.T, r *placeRig) (string, string) {
			writeFile(t, mkdirs(t, filepath.Join(r.lib, "sub", "alpha"), "x.md"), "a file of the library's own\n")
			displace(t, r.lib, r.claude, false)
			r.linkSkillsDir(filepath.Join(r.claude, "sub"))
			m := overlap(nestedOverlap(r.cursor, r.claude))
			return m, m
		}},
		{"a copy inside the directory", func(t *testing.T, r *placeRig) (string, string) {
			r.copies = []string{"cursor"}
			displace(t, r.lib, r.cursor, false)
			displace(t, r.lib, r.claude, true)
			if err := os.Rename(r.cursor, filepath.Join(r.claude, "scripts", "alpha")); err != nil {
				t.Fatal(err)
			}
			r.linkSkillsDir(filepath.Join(r.claude, "scripts"))
			m := overlap(nestedOverlap(r.cursor, r.claude))
			return m, m
		}},
		{"an edited copy inside the directory", func(t *testing.T, r *placeRig) (string, string) {
			r.copies = []string{"cursor"}
			displace(t, r.lib, r.cursor, true)
			displace(t, r.lib, r.claude, true)
			if err := os.Rename(r.cursor, filepath.Join(r.claude, "scripts", "alpha")); err != nil {
				t.Fatal(err)
			}
			r.linkSkillsDir(filepath.Join(r.claude, "scripts"))
			m := overlap(nestedOverlap(r.cursor, r.claude))
			return m, m
		}},
		{"a skills directory inside the directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			vendor := filepath.Join(r.claude, "vendor")
			if err := os.Rename(filepath.Dir(r.cursor), vendor); err != nil {
				t.Fatal(err)
			}
			link(t, vendor, filepath.Dir(r.cursor))
			m := overlap(nestedOverlap(r.cursor, r.claude))
			return m, m
		}},
		{"a disabled client's skills directory inside the directory", func(t *testing.T, r *placeRig) (string, string) {
			r.disabled = []string{"cursor"}
			displace(t, r.lib, r.claude, true)
			vendor := filepath.Join(r.claude, "vendor")
			if err := os.Rename(filepath.Dir(r.cursor), vendor); err != nil {
				t.Fatal(err)
			}
			link(t, vendor, filepath.Dir(r.cursor))
			m := overlap(nestedOverlap(r.cursor, r.claude))
			return m, m
		}},

		// A library entry that is a symlink, refused only where a displaced
		// directory is removed.
		{"an entry linked to a directory of the user's, beside a directory that differs", func(t *testing.T, r *placeRig) (string, string) {
			dev := filepath.Join(r.user, "dev-alpha")
			r.moveLibraryEntry(dev)
			displace(t, dev, r.claude, true)
			m := entryLink(r, dev)
			return m, m
		}},
		{"an entry linked beneath the displaced directory", func(t *testing.T, r *placeRig) (string, string) {
			remove(t, r.claude)
			inner := filepath.Join(r.claude, "inner")
			r.moveLibraryEntry(inner)
			writeFile(t, filepath.Join(r.claude, "README.md"), "a file beside the library's content\n")
			m := entryLink(r, inner)
			return m, m
		}},
		{"an entry linked to a directory holding a link into the displaced directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			dev := filepath.Join(r.user, "dev", "alpha")
			r.moveLibraryEntry(dev)
			swapForLink(t, filepath.Join(dev, "scripts"), filepath.Join(r.claude, "scripts"))
			m := entryLink(r, dev)
			return m, m
		}},
		{"an entry linked to a directory of the user's, with a placement missing", func(t *testing.T, r *placeRig) (string, string) {
			r.moveLibraryEntry(filepath.Join(r.user, "dev-alpha"))
			remove(t, r.cursor)
			return "", ""
		}},

		// A symlink in the library directory, refused only where a displaced
		// directory is removed.
		{"a directory of the library linked into the displaced directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			scripts := filepath.Join(r.lib, "scripts")
			swapForLink(t, scripts, filepath.Join(r.claude, "scripts"))
			m := heldLink(r, scripts)
			return m, m
		}},
		{"the library's SKILL.md linked into the displaced directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			file := filepath.Join(r.lib, "SKILL.md")
			swapForLink(t, file, filepath.Join(r.claude, "SKILL.md"))
			m := heldLink(r, file)
			return m, m
		}},
		{"a link to the skills directory above the displaced directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			up := filepath.Join(r.lib, "up")
			link(t, filepath.Dir(r.claude), up)
			m := heldLink(r, up)
			return m, m
		}},
		{"a link to a directory outside holding a link into the displaced directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			writeFile(t, mkdirs(t, filepath.Join(r.claude, "shared"), "data.md"), "only in the displaced directory\n")
			vendor := filepath.Join(r.user, "vendor")
			link(t, filepath.Join(r.claude, "shared"), mkdirs(t, vendor, "shared"))
			at := filepath.Join(r.lib, "vendor")
			link(t, vendor, at)
			m := heldLink(r, at)
			return m, m
		}},
		{"a link the displaced directory holds too, which another skill's entry leads through", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			real := betaAt(t, filepath.Join(r.user, "real-beta"))
			at := filepath.Join(r.lib, "beta-link")
			link(t, real, at)
			link(t, real, filepath.Join(r.claude, "beta-link"))
			link(t, filepath.Join(r.claude, "beta-link"), filepath.Join(r.library, "beta"))
			m := heldLink(r, at)
			return m, m
		}},
		{"a link back into the displaced directory", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			at := filepath.Join(r.lib, "back")
			link(t, filepath.Join(r.claude, "SKILL.md"), at)
			m := heldLink(r, at)
			return m, m
		}},
		{"a link deep in the library directory to a file of the user's", func(t *testing.T, r *placeRig) (string, string) {
			displace(t, r.lib, r.claude, true)
			outside := filepath.Join(r.user, "shared-notes.md")
			writeFile(t, outside, "notes kept outside the skill\n")
			at := filepath.Join(r.lib, "scripts", "shared.md")
			link(t, outside, at)
			m := heldLink(r, at)
			return m, m
		}},
		{"a link in the library directory, with the library's symlink where a copy belongs", func(t *testing.T, r *placeRig) (string, string) {
			r.copies = []string{"cursor"}
			outside := filepath.Join(r.user, "shared-notes.md")
			writeFile(t, outside, "notes kept outside the skill\n")
			link(t, outside, filepath.Join(r.lib, "shared.md"))
			return "", ""
		}},
		{"a link in the library directory, with a placement missing", func(t *testing.T, r *placeRig) (string, string) {
			outside := filepath.Join(r.user, "shared-notes.md")
			writeFile(t, outside, "notes kept outside the skill\n")
			link(t, outside, filepath.Join(r.lib, "shared.md"))
			remove(t, r.cursor)
			return "", ""
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := newPlaceRig(t)
			wantPlain, wantForced := c.arrange(t, r)
			plain, forced := r.judge()
			equal(t, "the refusal", plain, wantPlain)
			equal(t, "the refusal with --force", forced, wantForced)
		})
	}
}

func TestFirstSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, mkdirs(t, filepath.Join(dir, "a", "b"), "file.md"), "a file\n")
	got, err := firstSymlink(dir)
	equal(t, "a directory holding no link", got, "")
	equal(t, "its error", err, error(nil))

	for _, at := range []string{
		filepath.Join(dir, "a", "b", "z-to-a-file"),
		filepath.Join(dir, "a", "m-to-a-directory"),
		filepath.Join(dir, "a", "b", "c-dangling"),
	} {
		target := filepath.Join(dir, "a", "b", "file.md")
		switch {
		case strings.HasSuffix(at, "directory"):
			target = filepath.Join(dir, "a", "b")
		case strings.HasSuffix(at, "dangling"):
			target = filepath.Join(dir, "nowhere")
		}
		link(t, target, at)
	}
	got, err = firstSymlink(dir)
	equal(t, "the first link in the order of the walk", got, filepath.Join(dir, "a", "b", "c-dangling"))
	equal(t, "its error", err, error(nil))

	if _, err := firstSymlink(filepath.Join(dir, "missing")); err == nil {
		t.Error("a directory that is not there holds no link and no error")
	}
}

func TestRelation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outer := filepath.Join(dir, "outer")
	if err := os.MkdirAll(outer, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	writeFile(t, file, "a file\n")
	hard := filepath.Join(dir, "hard")
	if err := os.Link(file, hard); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ a, b, want string }{
		{outer, outer, "is"},
		{file, hard, "is"}, // one file, two names
		{filepath.Join(outer, "x", "y"), outer, "lies inside"},
		{filepath.Join(outer, "x"), outer + string(filepath.Separator), "lies inside"},
		{outer, filepath.Join(outer, "x"), "holds"},
		{outer + "-beside", outer, ""},
		{filepath.Join(dir, "elsewhere"), outer, ""},
	} {
		equal(t, "the relation of "+c.a+" to "+c.b, relation(c.a, c.b), c.want)
	}

	t.Run("a directory spelled in another case", func(t *testing.T) {
		ignoresCase(t)
		equal(t, "the relation", relation(filepath.Join(inAnotherCase(outer), "x"), outer), "lies inside")
		equal(t, "one directory", relation(inAnotherCase(outer), outer), "is")
	})
}

func TestLinkedEntries(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	writeFile(t, mkdirs(t, filepath.Join(real, "alpha"), "SKILL.md"), skill("alpha", "The first skill"))
	dev := betaAt(t, filepath.Join(root, "dev", "beta"))
	link(t, filepath.Join(root, "dev"), filepath.Join(root, "dev-link"))
	link(t, filepath.Join(root, "dev-link", "beta"), filepath.Join(real, "beta"))
	link(t, "gone", filepath.Join(real, "gamma"))
	library := filepath.Join(root, "library")
	link(t, real, library)

	entries, err := linkedEntries(real, library)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.path+" -> "+e.real)
	}
	equal(t, "the linked entries", strings.Join(got, "\n"),
		filepath.Join(library, "beta")+" -> "+dev+"\n"+
			filepath.Join(library, "gamma")+" -> "+filepath.Join(real, "gone"))
}

// TestPlaceSummary is the result's summary of skill place, which the
// end-to-end tests check a part of each: the counts, then what --force
// discarded, then the universal clients.
func TestPlaceSummary(t *testing.T) {
	t.Parallel()
	targets := func(n int) []placeTarget { return make([]placeTarget, n) }
	for _, c := range []struct {
		done      placements
		universal []string
		want      string
	}{
		{placements{placed: targets(1)}, nil, "placed alpha in 1 configuration"},
		{placements{skipped: []string{"/a"}}, nil, "placed alpha in 0 configurations, 1 placement skipped"},
		{placements{placed: targets(6), copies: []string{"github-copilot"}}, []string{"codex", "gemini-cli"},
			"placed alpha in 6 configurations, 1 placement as copy; always available to universal clients: codex, gemini-cli"},
		{placements{placed: targets(3), copies: []string{"a", "b"}, adoptions: []string{"/a"}, skipped: []string{"/b", "/c"}}, nil,
			"placed alpha in 3 configurations, 2 placements as copy, 1 placement adopted, 2 placements skipped"},
		{placements{placed: targets(4), discarded: []string{"/a", "/b"}}, []string{"codex"},
			"placed alpha in 4 configurations; discarded what /a, /b held; always available to universal clients: codex"},
	} {
		equal(t, "the summary", placeSummary("alpha", c.done)+universalClause(c.universal), c.want)
	}
}
