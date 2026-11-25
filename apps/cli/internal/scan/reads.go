package scan

import (
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// library names the library as a directory a client reads, in reads.
const library = "library"

// projectLibrary is the project-scope counterpart of the library, relative
// to a project root.
const projectLibrary = ".agents/skills"

// read is one skills directory a client reads besides its own: another
// client's own directory, named by that client's slug, or the library.
// when, if set, says whether the client reads it at user scope on this
// machine; at project scope it is always read.
type read struct {
	from string
	when func(home.Dirs) bool
}

// reads says which clients read which other clients' skills directories
// and which read the library. It is the only place that does: every
// directory list a scan, a placement or a removal works from is composed
// here from each client's own directory.
//
// Cursor reads Claude Code's and Codex's directories. OpenCode reads Claude
// Code's. Copilot CLI reads the library since 1.0.11, except when
// COPILOT_HOME is set (since 1.0.66).
var reads = map[string][]read{
	"cline":          {{from: library}},
	"codex":          {{from: library}},
	"cursor":         {{from: "claude-code"}, {from: "codex"}, {from: library}},
	"gemini-cli":     {{from: library}},
	"github-copilot": {{from: library, when: func(d home.Dirs) bool { return d.CopilotHome == "" }}},
	"opencode":       {{from: "claude-code"}, {from: library}},
}

// SkillsDirs are the user-scope skills directories c reads, each once: its
// own first, then the directories reads names for it, in that order. A
// client that lists the library reads it directly, so a library skill needs
// no placement in its own directory.
func SkillsDirs(c Client, d home.Dirs) []string {
	dirs := []string{c.SkillsDir(d)}
	for _, r := range reads[c.Slug()] {
		if r.when != nil && !r.when(d) {
			continue
		}
		dir := d.Library
		if r.from != library {
			other, _ := Lookup(r.from)
			dir = other.SkillsDir(d)
		}
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// ProjectSkillsDirs are the project-scope skills directories c reads,
// relative to a project root, each once, composed as SkillsDirs composes
// the user-scope ones.
func ProjectSkillsDirs(c Client) []string {
	dirs := []string{c.ProjectSkillsDir()}
	for _, r := range reads[c.Slug()] {
		dir := projectLibrary
		if r.from != library {
			other, _ := Lookup(r.from)
			dir = other.ProjectSkillsDir()
		}
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
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
// once: a client that reaches one directory through two of the directories
// it reads, as Cursor reaches a library skill through Claude Code's link to
// it, loads it once. The library is read first, so the path a library skill
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
