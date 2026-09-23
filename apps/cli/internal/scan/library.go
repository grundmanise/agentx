package scan

import (
	"path/filepath"
	"sort"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// LibrarySkill is one directory of the library: the name agentx knows it by,
// which is the directory's own name and the name of its branch in the
// account repo, and what the directory holds.
type LibrarySkill struct {
	Name         string // the directory name in the library
	Path         string // <library>/<name>
	ResolvedPath string // the real directory, which differs for a symlink into a fork worktree
	ContentHash  string
}

// ReadLibrary lists the skills the library holds, sorted by name, with the
// warnings the reads produced. It reads the library the way a client that
// reads it directly does, so a hidden entry, the staging directory of an
// install included, is not a skill.
func ReadLibrary(dir string) ([]LibrarySkill, []string) {
	s := newScanner()
	skills := s.librarySkills(dir)
	return skills, s.sortedWarnings()
}

// librarySkills lists the skills of the library at dir, sorted by name,
// through this scanner, so that a directory it listed already, as a scan
// lists the library for every client that reads it, is not read again.
func (s *scanner) librarySkills(dir string) []LibrarySkill {
	var skills []LibrarySkill
	for _, p := range s.skillsIn(dir) {
		skills = append(skills, LibrarySkill{
			Name:         filepath.Base(p.path),
			Path:         p.path,
			ResolvedPath: p.resolved,
			ContentHash:  p.info.contentHash,
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills
}

// ContentHashAt is the content hash of the skill directory at dir, read the
// way a scan of the machine reads it, with the warnings that reading it
// produced. It is how an install validates the directory it staged against
// the version it imported.
func ContentHashAt(dir string) (contentHash string, warnings []string) {
	s := newScanner()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	return s.skill(real).contentHash, s.sortedWarnings()
}

// PlacementDir is the directory an explicit placement in this configuration
// goes into: the client's own skills directory, which SkillsDirs lists
// first. A client that reads the library directly needs no placement of its
// own, so ReadsLibrary decides before this is asked for.
func PlacementDir(c Client, d home.Dirs) string {
	dirs := c.SkillsDirs(d)
	if len(dirs) == 0 {
		return ""
	}
	return dirs[0]
}

// ReadsLibrary reports whether the client reads the library as one of its
// own skills directories, in which case the library entry is the placement
// and a second entry would make that client list the skill twice.
func ReadsLibrary(c Client, d home.Dirs) bool {
	for _, dir := range c.SkillsDirs(d) {
		if dir == d.Library {
			return true
		}
	}
	return false
}
