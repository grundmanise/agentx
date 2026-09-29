package cli

import (
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// quotingCase is one skill whose upstream names carry what git quotes in
// its line-based formats: a name starting with a double quote, which git
// C-unquotes when it reads one back, and a name holding a newline, which
// ends the record. A source is any repository a user adds, so these are
// names agentx has to carry through unchanged rather than names it chose.
type quotingCase struct {
	name    string // the subtest
	dir     string // the skill directory in the source
	skill   string // its frontmatter name, which is the library directory
	files   map[string]string
	links   map[string]string // symlinks inside the skill, which the import leaves out
	rewrite bool              // the skill's own tree cannot be reused whole
}

func quotingCases() []quotingCase {
	return []quotingCase{{
		// One quoted name among the files, with a symlink to force the
		// rewrite: the skill's tree is written anew, so the name goes
		// through mktree rather than travelling with the tree that is reused.
		name:    "a quoted file name",
		dir:     "tools/quoted",
		skill:   "quotedfile",
		files:   map[string]string{`"weird".md`: "weird\n"},
		links:   map[string]string{"alias.md": "SKILL.md"},
		rewrite: true,
	}, {
		name:    "a newline in a file name",
		dir:     "tools/newline",
		skill:   "newlinefile",
		files:   map[string]string{"a\nb.md": "two lines in one name\n"},
		links:   map[string]string{"alias.md": "SKILL.md"},
		rewrite: true,
	}, {
		// A name that ends in a newline is the case where trimming a
		// trailing newline off git's answer could eat part of a record.
		// ls-tree -z ends its last record with NUL, so there is nothing to
		// trim, and the name arrives whole.
		name:    "a file name ending in a newline",
		dir:     "tools/trailing",
		skill:   "trailingnl",
		files:   map[string]string{"ends\n": "ends with a newline in its name\n"},
		links:   map[string]string{"alias.md": "SKILL.md"},
		rewrite: true,
	}, {
		// Nothing is left out, so the skill's tree is reused whole and the
		// only name that goes through mktree is the upstream directory's.
		// The root mktree always runs, so the directory name alone is enough.
		name:  "a quoted directory name",
		dir:   `tools/"odd"dir`,
		skill: "oddir",
	}, {
		// A directory below the skill whose name holds a newline, with a
		// symlink to force the rewrite, so the name goes through mktree as
		// one record of a level rather than travelling with a reused tree.
		// The skill's own directory cannot be the one holding it: the
		// subpath is recorded on one line of an import commit trailer, and
		// a directory a trailer cannot carry back is refused at install,
		// which TestInstallRefusesASubpathItCannotRecord covers.
		name:    "a newline in a directory below the skill",
		dir:     "tools/newlinesub",
		skill:   "newlinesub",
		files:   map[string]string{"su\nb/x.md": "x\n"},
		links:   map[string]string{"su\nb/link": "x.md"},
		rewrite: true,
	}, {
		// Two directories rewritten at one level, so their names travel
		// through the batch protocol rather than through one mktree.
		name:  "quoted names in two directories of one batch",
		dir:   "tools/batched",
		skill: "batched",
		files: map[string]string{
			`sub1/"a".md`: "a\n",
			`sub2/"b".md`: "b\n",
		},
		links:   map[string]string{"sub1/link": `"a".md`, "sub2/link": `"b".md`},
		rewrite: true,
	}}
}

// TestImportKeepsNamesGitWouldQuote installs skills whose upstream names
// need quoting and checks that the import tree holds the source's bytes.
// git's mktree C-unquotes any name that starts with a double quote and
// reads one record per line, so a name written back unquoted is silently
// renamed or breaks the input. The import commit is the base version every
// later update, revert and fork merge restores from, so a name lost here
// is lost for good, and the content hash would name a tree the commit does
// not hold.
//
// Every skill is taken in one run: a batch writes the trees of every skill
// together, one mktree per level, which is the mktree a run of one skill
// writes too. A record framed the wrong way then does not cost that one
// skill, it ends the definition every later skill's entries belong to, so
// the whole run fails and the library is left empty.
func TestImportKeepsNamesGitWouldQuote(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.build(t, fixture{dirs: []string{".claude"}})
	s := h.newSourceRepo("quoting", true)
	cases := quotingCases()
	var skills []string
	for _, c := range cases {
		s.skill(c.dir, c.skill, "Holds a name git would quote", c.files)
		for name, target := range c.links {
			link := filepath.Join(s.work, filepath.FromSlash(c.dir), filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
		}
		skills = append(skills, c.skill)
	}
	s.commit("skills whose names need quoting")
	h.mustRun("source", "add", s.url)
	h.mustRun("skill", "add", s.url, "--all")
	slices.Sort(skills)
	equal(t, "the library", strings.Join(installedNames(t, h), ","), strings.Join(skills, ","))

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ref := "refs/heads/managed/" + c.skill
			upstream := s.treeOf(c.dir)

			// The import tree holds exactly one entry, the upstream
			// directory under its own name, quotes, newlines and all.
			root := treeRecords(t, h.accountGit("ls-tree", "-z", ref))
			if len(root) != 1 {
				t.Fatalf("the import tree holds %d entries, want 1: %q", len(root), root)
			}
			if got := entryName(root[0]); got != path.Base(c.dir) {
				t.Errorf("the import tree's entry is named %q, want %q", got, path.Base(c.dir))
			}
			if !c.rewrite {
				// Nothing was left out, so the upstream tree is reused whole.
				if want := "040000 tree " + upstream + "\t" + path.Base(c.dir); root[0] != want {
					t.Errorf("the import tree holds %q, want %q", root[0], want)
				}
			}

			// Below that entry the tree is the source's, name for name,
			// without the entries an import leaves out.
			var want []string
			for _, rec := range treeRecords(t, s.bare("ls-tree", "-r", "-t", "-z", upstream)) {
				if mode := entryMode(rec); mode == "100644" || mode == "100755" || mode == "040000" {
					want = append(want, entryName(rec))
				}
			}
			var got []string
			for _, rec := range treeRecords(t, h.accountGit("ls-tree", "-r", "-t", "-z", ref)) {
				name := entryName(rec)
				if name == path.Base(c.dir) {
					continue // the one entry of the import tree
				}
				got = append(got, strings.TrimPrefix(name, path.Base(c.dir)+"/"))
			}
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Errorf("the import tree holds %q, want %q", got, want)
			}

			// The library on disk holds the same names, so the content hash
			// the commit records is the hash of what was installed.
			for name := range c.files {
				if _, err := os.Lstat(filepath.Join(h.library, c.skill, filepath.FromSlash(name))); err != nil {
					t.Errorf("the library holds no %q: %v", name, err)
				}
			}
		})
	}
}

// treeOf is the tree id of the source directory dir at HEAD, read out of
// its parent's listing rather than with rev-parse, so that a name needing
// quoting is matched on its own bytes.
func (s *sourceRepo) treeOf(dir string) string {
	s.t.Helper()
	parent, base := path.Split(dir)
	for _, rec := range strings.Split(s.bare("ls-tree", "-z", "HEAD:"+strings.TrimSuffix(parent, "/")), "\x00") {
		if rec != "" && entryName(rec) == base {
			fields := strings.Fields(rec)
			return fields[2]
		}
	}
	s.t.Fatalf("the source holds no directory %q", dir)
	return ""
}

// treeRecords splits the NUL-terminated records of an ls-tree -z, which
// quotes nothing.
func treeRecords(t *testing.T, out string) []string {
	t.Helper()
	var records []string
	for _, rec := range strings.Split(out, "\x00") {
		if rec != "" {
			records = append(records, rec)
		}
	}
	return records
}

// entryName is the name of one ls-tree record, which follows the tab, and
// entryMode is the mode that opens it.
func entryName(record string) string {
	_, name, _ := strings.Cut(record, "\t")
	return name
}

func entryMode(record string) string {
	mode, _, _ := strings.Cut(record, " ")
	return mode
}
