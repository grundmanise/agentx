package scan

import (
	"path/filepath"
	"slices"
	"sort"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// LibrarySkill is one directory of the library: the name agentx knows it by,
// which is the directory's own name and the name of its branch in the
// store, and what the directory holds.
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
// first. A client whose placement is the library entry needs none of its
// own, see ReadsLibrary and LinksToLibrary, which decide before this is
// asked for.
func PlacementDir(c Client, d home.Dirs) string {
	dirs := SkillsDirs(c, d)
	if len(dirs) == 0 {
		return ""
	}
	return dirs[0]
}

// ReadsLibrary reports whether the client reads the library: its own skills
// directory is the library, or readsTheLibrary names it. A skills directory
// made a symlink to the library does not make a client read it, see
// LinksToLibrary.
func ReadsLibrary(c Client, d home.Dirs) bool {
	return slices.Contains(SkillsDirs(c, d), d.Library)
}

// LinksToLibrary reports whether the client's own skills directory and the
// library are one directory through a symlink, either way round, as
// ~/.claude/skills made a link to ~/.agents/skills is. The client still
// reads its own directory, but a skill's directory there is the library
// entry itself: a command that took it for a placement of its own would
// replace the library entry with a link to itself, or delete it.
func LinksToLibrary(c Client, d home.Dirs) bool {
	return linksToLibrary(c.SkillsDir(d), d.Library)
}

// linksToLibrary reports whether dir leads to the same directory as library
// through a symlink, either way round. A path that does not resolve leads
// nowhere.
func linksToLibrary(dir, library string) bool {
	if dir == "" || dir == library {
		return false
	}
	real, err := filepath.EvalSymlinks(library)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(dir)
	return err == nil && resolved == real
}
