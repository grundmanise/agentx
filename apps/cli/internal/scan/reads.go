package scan

import (
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// projectLibrary is the project-scope counterpart of the library, relative
// to a project root.
const projectLibrary = ".agents/skills"

// readsTheLibrary names the clients that read the library besides their
// own skills directory, each with whether it does on this machine. A
// client reads its own directory and, when listed here, the library;
// agentx does not model a client reading another client's directory.
//
// Copilot CLI reads the library since 1.0.11, except when COPILOT_HOME is
// set (since 1.0.66).
var readsTheLibrary = map[string]func(home.Dirs) bool{
	"cline":          always,
	"codex":          always,
	"cursor":         always,
	"gemini-cli":     always,
	"github-copilot": func(d home.Dirs) bool { return d.CopilotHome == "" },
	"opencode":       always,
}

func always(home.Dirs) bool { return true }

// SkillsDirs are the user-scope skills directories c reads: its own, then
// the library when c reads it too. A client that reads the library needs
// no placement of a library skill in its own directory.
func SkillsDirs(c Client, d home.Dirs) []string {
	dirs := []string{c.SkillsDir(d)}
	if reads, ok := readsTheLibrary[c.Slug()]; ok && reads(d) && dirs[0] != d.Library {
		dirs = append(dirs, d.Library)
	}
	return dirs
}

// ProjectSkillsDirs are the project-scope skills directories c reads,
// relative to a project root: its own, then the project's .agents/skills
// when c reads the library.
func ProjectSkillsDirs(c Client) []string {
	dirs := []string{c.ProjectSkillsDir()}
	if _, ok := readsTheLibrary[c.Slug()]; ok && dirs[0] != projectLibrary {
		dirs = append(dirs, projectLibrary)
	}
	return dirs
}

// loaded is one skill directory a client loads, at the path it is
// reported at, and the other paths the client reaches the same directory
// through.
type loaded struct {
	placement
	also []string
}

// loadedOnce are the skills a client finds in dirs, each real directory
// once: a client that reaches one directory through both of the
// directories it reads, as Cursor reaches a library skill through a link
// to it in ~/.cursor/skills, loads it once. The library is read first, so the path a library skill
// is reported at is the library entry, which is the placement of every
// client that reads the library.
func (s *scanner) loadedOnce(dirs []string, library string) []loaded {
	if i := slices.Index(dirs, library); i > 0 {
		dirs = slices.Concat([]string{library}, dirs[:i], dirs[i+1:])
	}
	var found []loaded
	at := map[string]int{}
	for _, dir := range dirs {
		for _, p := range s.skillsIn(dir) {
			if i, ok := at[p.resolved]; ok {
				found[i].also = append(found[i].also, p.path)
				continue
			}
			at[p.resolved] = len(found)
			found = append(found, loaded{placement: p})
		}
	}
	for i := range found {
		slices.Sort(found[i].also)
	}
	return found
}
