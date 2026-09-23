package cli

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// librarySkillEvent is one skill of the library: what it is called, where
// it came from and where it can be seen from. It is named for the library
// so that nothing has to tell it apart from the skill node of a snapshot,
// which is one content version found anywhere on the machine, or from a
// sourceSkillEvent, which is one installable skill of a source.
type librarySkillEvent struct {
	event
	Name           string           `json:"name"`
	Kind           string           `json:"kind"`              // managed, fork or unmanaged
	Source         string           `json:"source,omitempty"`  // the canonical URL of the upstream
	Subpath        *string          `json:"subpath,omitempty"` // the directory in the source, "" for its root
	UpstreamCommit string           `json:"upstream_commit,omitempty"`
	BaseHash       string           `json:"base_hash,omitempty"` // the content hash of the base version
	ContentHash    string           `json:"content_hash"`        // what the library holds now
	State          string           `json:"state,omitempty"`     // current or modified, for a managed skill
	Placements     []placementEvent `json:"placements"`
}

// placementEvent is one way a configuration sees the skill: mode is what
// agentx keeps for it, kind is what is on disk now.
type placementEvent struct {
	Configuration string `json:"configuration"`
	Path          string `json:"path"`
	Mode          string `json:"mode"` // symlink, copy or library
	Kind          string `json:"kind"` // symlink, copy, directory or library
}

// The placement modes and the kind of a placement that is the library entry
// itself, for a client that reads the library as its own skills directory.
const (
	modeSymlink = "symlink"
	modeCopy    = "copy"
	modeLibrary = "library"
)

// The two states a managed skill is listed with: its library directory
// holds the version it was installed at, or it was edited since. Nothing
// here decides whether an upstream moved, which a later command does.
const (
	stateCurrent  = "current"
	stateModified = "modified"
)

func newSkillCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "skill",
		Short:       "Install the skills of a source and list what the library holds",
		Annotations: map[string]string{annotationGroup: "true"},
		Args:        cobra.NoArgs,
		RunE:        needSubcommand(inv, "no skill command given", "run 'agentx skill --help' to list commands"),
	}
	cmd.AddCommand(newSkillAddCommand(inv))
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the skills in the library with their upstream and placements",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return inv.skillList(cmd.Context()) },
	})
	return cmd
}

// libraryName refuses a frontmatter name the library cannot hold as a
// directory: one that is empty, that walks out of the library, or that
// discovery would skip because it is hidden. Renaming on install is not
// offered, so a name agentx cannot use is a refusal and not a guess.
var libraryName = regexp.MustCompile(`^[^./\\\x00][^/\\\x00]*$`)

// nameRefusal says why the skill cannot be held under name, in the words
// of the thing that cannot hold it, and "" when it can. Both are checked
// here, before anything is written, because a name only one of them accepts
// would be found out in the middle of the mutation (the journal already on
// disk and the ref step failing), after which every command would recover
// that journal and fail the same way. The name comes out of a source's
// SKILL.md, so it is checked and not trusted, and renaming on install is
// not offered.
//
// The two are not one refusal: a POSIX directory happily holds "who?" or
// "a b", so telling the user their library cannot would be untrue and would
// send them looking in the wrong place. What refuses those is the branch
// the account repo has to record the version on.
func nameRefusal(name string) string {
	switch {
	case !libraryName.MatchString(name), strings.ContainsAny(name, "\n\r"):
		return "is not a name the library can hold as a directory"
	case !refComponent(name):
		return "is not a name the account repo can hold as an import branch"
	}
	return ""
}

// refComponent reports whether git accepts name as one level of a ref name,
// which is what git check-ref-format allows: no control character, space or
// DEL, none of ~^:?*[\, no "..", no "@{", not "@" alone, and no trailing
// "." or ".lock". A leading "." is already out, the library having no room
// for a hidden directory.
func refComponent(name string) bool {
	if name == "" || name == "@" || strings.Contains(name, "..") || strings.Contains(name, "@{") ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f || strings.ContainsRune(`~^:?*[\`, r) {
			return false
		}
	}
	return true
}

// libraryPath is where the skill called name lives in the library.
func (inv *invocation) libraryPath(name string) string {
	return filepath.Join(inv.dirs.Library, name)
}

// placements maps a library skill to what a snapshot found: every
// occurrence that resolves to its library directory, and every copy of its
// content placed under its name, which resolves to itself. Placements are
// sorted by configuration, then path, so two listings of an unchanged
// machine are byte-identical.
func (inv *invocation) placements(snap scan.Snapshot, lib scan.LibrarySkill, copyMode map[string][]string) []placementEvent {
	found := []placementEvent{}
	for _, node := range snap.Skills {
		for _, o := range node.Occurrences {
			if o.Plugin != "" || o.Scope != "user" {
				continue // a plugin's skill and a project's are not placements of the library
			}
			switch {
			case o.ResolvedPath == lib.ResolvedPath:
			case o.Kind == modeCopy && filepath.Base(o.Path) == lib.Name && node.ContentHash == lib.ContentHash:
			default:
				continue
			}
			found = append(found, placementEvent{
				Configuration: o.Configuration,
				Path:          o.Path,
				Mode:          placementMode(o, lib, copyMode, inv.dirs.Library),
				Kind:          placementKind(o, inv.dirs.Library),
			})
		}
	}
	sortPlacements(found)
	return found
}

// placementMode is what agentx keeps for this configuration: the library
// entry for a client that reads the library, a copy where the settings
// record one, a symlink otherwise.
func placementMode(o scan.Occurrence, lib scan.LibrarySkill, copyMode map[string][]string, library string) string {
	if inLibrary(o, library) {
		return modeLibrary
	}
	for _, id := range copyMode[lib.Name] {
		if id == o.Configuration {
			return modeCopy
		}
	}
	return modeSymlink
}

// placementKind is what the placement is on disk: the scan's own kinds, and
// the library entry itself for a client that reads the library directly.
func placementKind(o scan.Occurrence, library string) string {
	if inLibrary(o, library) {
		return modeLibrary
	}
	return o.Kind
}

// inLibrary reports whether the occurrence is the library entry itself,
// which is the placement of every client that reads the library directly.
func inLibrary(o scan.Occurrence, library string) bool {
	return filepath.Dir(o.Path) == filepath.Clean(library)
}

// sortPlacements orders placements by configuration and then path, so that
// two listings of an unchanged machine are byte-identical.
func sortPlacements(p []placementEvent) {
	sort.Slice(p, func(i, j int) bool {
		if p[i].Configuration != p[j].Configuration {
			return p[i].Configuration < p[j].Configuration
		}
		return p[i].Path < p[j].Path
	})
}

// skillFromLibrary builds the event of one library skill from its lineage
// record and the placements a scan found.
func skillFromLibrary(lib scan.LibrarySkill, rec lineage.Record, ok bool, places []placementEvent) librarySkillEvent {
	ev := librarySkillEvent{event: newEvent("library_skill"), Name: lib.Name, Kind: lineage.KindUnmanaged, ContentHash: lib.ContentHash, Placements: places}
	if !ok {
		return ev
	}
	ev.Kind = rec.Kind
	if rec.HasImport {
		subpath := rec.Import.Path
		ev.Source, ev.Subpath, ev.UpstreamCommit, ev.BaseHash = rec.Import.Source, &subpath, rec.Import.Commit, rec.Import.Hash
	}
	// A managed skill's base version is the import commit its branch points
	// at, so the two hashes can be compared; a fork's base is the last
	// version merged into it, which a later command reads from its history.
	if rec.Kind == lineage.KindManaged && rec.HasImport {
		ev.State = stateCurrent
		if lib.ContentHash != rec.Import.Hash {
			ev.State = stateModified
		}
	}
	return ev
}
