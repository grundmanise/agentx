// Package scan inventories one machine: the agent configurations on it and
// the skills each one can see.
package scan

import (
	"os"
	"sort"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// Client is one agent client the registry knows. A client contributes path
// data only; the scanner does the reading. Add a client by adding one file
// with a value of this interface and listing it in registry.go.
type Client interface {
	// Slug is the stable lowercase id, such as "claude-code"; it names the
	// configuration in settings and output.
	Slug() string
	// Name is the display name, such as "Claude Code".
	Name() string
	// ConfigDir is the user-scope configuration directory. The client is
	// detected when it exists.
	ConfigDir(d home.Dirs) string
	// SkillsDirs are the user-scope skills directories the client reads, its
	// own first, then other clients' directories and the library where the
	// client reads them.
	SkillsDirs(d home.Dirs) []string
	// ReadsLibrary reports whether the client reads the library directly, so
	// a library skill needs no placement in its own directory.
	ReadsLibrary() bool
	// ProjectSkillsDirs are the project-scope skills directories, relative
	// to a project root.
	ProjectSkillsDirs() []string
}

// Detect returns the registered clients whose configuration directory
// exists, sorted by slug.
func Detect(d home.Dirs) []Client {
	var detected []Client
	for _, c := range registry {
		if info, err := os.Stat(c.ConfigDir(d)); err == nil && info.IsDir() {
			detected = append(detected, c)
		}
	}
	sort.Slice(detected, func(i, j int) bool { return detected[i].Slug() < detected[j].Slug() })
	return detected
}
