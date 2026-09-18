package scan

import (
	"os"
	"path/filepath"
	"sort"
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
// are skipped; a broken symlink is a warning. A missing directory is empty.
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
				s.warn(path + ": broken symlink, skipped")
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
	s.listings[dir] = found
	return found
}

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
		contentHash: contentHash(dir, fm, s.warn),
	}
	if info.name == "" {
		info.name = filepath.Base(dir)
	}
	s.skills[dir] = info
	return info
}
