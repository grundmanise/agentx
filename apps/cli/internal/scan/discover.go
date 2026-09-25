package scan

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// skillInfo is what one skill directory yields, cached by its real path so
// a skill seen through several placements is read and hashed once.
type skillInfo struct {
	name, description, contentHash string
}

// placement is one skill found in one skills directory.
type placement struct {
	path     string // as the client sees it
	resolved string // the real directory
	symlink  bool
	info     *skillInfo
}

// scanner reads skills directories with every directory canonicalised once.
type scanner struct {
	canon    map[string]string // path to real path, "" when it does not resolve
	skills   map[string]*skillInfo
	listings map[string][]placement
	warnings map[string]bool
}

func newScanner() *scanner {
	return &scanner{
		canon:    map[string]string{},
		skills:   map[string]*skillInfo{},
		listings: map[string][]placement{},
		warnings: map[string]bool{},
	}
}

func (s *scanner) warn(msg string) { s.warnings[msg] = true }

func (s *scanner) sortedWarnings() []string {
	out := make([]string, 0, len(s.warnings))
	for w := range s.warnings {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// realPath canonicalises path once; a path that does not resolve is "".
func (s *scanner) realPath(path string) string {
	if real, ok := s.canon[path]; ok {
		return real
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = ""
	}
	s.canon[path] = real
	return real
}

// skillsIn lists the skills in one skills directory: every child directory,
// or symlink to one, that holds a SKILL.md. Hidden entries and node_modules
// are skipped; the broken symlinks are one warning naming them. A missing
// directory is empty.
func (s *scanner) skillsIn(dir string) []placement {
	if found, ok := s.listings[dir]; ok {
		return found
	}
	var found []placement
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			s.warn(err.Error() + ", skipped")
		}
		s.listings[dir] = nil
		return nil
	}
	realDir := s.realPath(dir)
	var broken []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		path := filepath.Join(dir, name)
		symlink := e.Type()&os.ModeSymlink != 0
		resolved := filepath.Join(realDir, name)
		if symlink {
			if resolved = s.realPath(path); resolved == "" {
				broken = append(broken, name)
				continue
			}
		} else if !e.IsDir() {
			continue
		}
		if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
			continue
		}
		if info, err := os.Stat(filepath.Join(resolved, "SKILL.md")); err != nil || !info.Mode().IsRegular() {
			continue
		}
		found = append(found, placement{path: path, resolved: resolved, symlink: symlink, info: s.skill(resolved)})
	}
	if len(broken) == 1 {
		s.warn(filepath.Join(dir, broken[0]) + ": broken symlink, skipped")
	} else if len(broken) > 1 {
		s.warn(dir + ": " + strconv.Itoa(len(broken)) + " broken symlinks (" + strings.Join(broken, ", ") + "), skipped")
	}
	s.listings[dir] = found
	return found
}

// pluginSkills lists the skills at a path a plugin names: the path itself
// when it holds a SKILL.md, as a manifest may name each skill, else the
// skills directory it is.
func (s *scanner) pluginSkills(dir string) []placement {
	if info, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil || !info.Mode().IsRegular() {
		return s.skillsIn(dir)
	}
	resolved := s.realPath(dir)
	link, err := os.Lstat(dir)
	symlink := err == nil && link.Mode()&os.ModeSymlink != 0
	return []placement{{path: dir, resolved: resolved, symlink: symlink, info: s.skill(resolved)}}
}

// hashSkill is the one way a scanner content-hashes a skill directory, so
// what a scan costs in hashing is one thing a test can count. Hashing reads
// every file a skill holds, and that is the cost a scan pays once per real
// directory however many ways it reaches it: a library skill is reached
// through every client that reads the library, through each placement that
// links to it and through the scan's own read of the library, and every way
// after the first is answered from the cache skill keeps.
var hashSkill = contentHash

// skill reads and hashes the skill at real path dir, once.
func (s *scanner) skill(dir string) *skillInfo {
	if info, ok := s.skills[dir]; ok {
		return info
	}
	skillMD := filepath.Join(dir, "SKILL.md")
	fm := frontmatter{}
	if b, err := os.ReadFile(skillMD); err != nil {
		s.warn(err.Error() + ", using the directory name")
	} else if fm, err = parseFrontmatter(string(b)); err != nil {
		s.warn(skillMD + ": " + err.Error() + ", using the directory name")
	} else if fm.name == "" {
		s.warn(skillMD + ": frontmatter has no name, using the directory name")
	}
	info := &skillInfo{
		name:        fm.name,
		description: fm.description,
		contentHash: hashSkill(dir, fm, s.warn),
	}
	if info.name == "" {
		info.name = filepath.Base(dir)
	}
	s.skills[dir] = info
	return info
}
