package cli

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// librarySkillEvent is one skill of the library: what it is called, where
// it came from and where it can be seen from. It is named for the library
// so that nothing has to tell it apart from the skill node of a snapshot,
// which is one content version found anywhere on the machine, or from a
// sourceSkillEvent, which is one installable skill of a source. Its fields
// are the library entry a snapshot lists, so the two always say the same.
type librarySkillEvent struct {
	event
	scan.LibraryEntry
}

// placementEvent is one way a configuration sees the skill: mode is what
// agentx keeps for it, kind is what is on disk now.
type placementEvent = scan.LibraryPlacement

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
//
// The two are told apart by tree id, the one way agentx compares a
// directory with a base version: the directory's tree as git would record
// it, computed in process, against the tree of the import commit. A mode
// is content to git, so a file made executable, a file swapped for a link
// to the same bytes and a link added anywhere are edits like any other;
// the content hash, which reads neither modes nor links, stays the name of
// a version and decides nothing here.
const (
	stateCurrent  = "current"
	stateModified = "modified"
)

// driftSourceRemoved is the drift state of a managed skill whose source
// this machine no longer has: the canonical URL its lineage names is the
// url of no source entry in the machine settings. It sits beside the
// state rather than in it, because the two answer different questions and
// both can be true at once: a skill edited by hand whose source was then
// removed is modified and source removed, and saying only one of them
// would hide the other. The drift states of the placements, displaced and
// missing, hold together with this one and with each other too, as will
// the drift states still to come, which is why drift is a list and state
// stays one word.
const driftSourceRemoved = "source removed"

func newSkillCommand(inv *invocation) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "skill",
		Short:       "Install, place, compare, revert and remove skills, and list what the library holds",
		Annotations: map[string]string{annotationGroup: "true"},
		Args:        cobra.NoArgs,
		RunE:        needSubcommand(inv, "no skill command given", "run 'agentx skill --help' to list commands"),
	}
	cmd.AddCommand(newSkillAddCommand(inv))
	cmd.AddCommand(newSkillPlaceCommand(inv))
	cmd.AddCommand(newSkillRemoveCommand(inv))
	cmd.AddCommand(newSkillDiffCommand(inv))
	cmd.AddCommand(newSkillRevertCommand(inv))
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

// universalClients are the configurations of the scan whose client reads the
// library as one of its own skills directories, reads_library in the
// snapshot, sorted by id as the snapshot sorts them. Every one of them sees
// every skill of the library, enabled or not and whatever --to named, since
// the library entry is its placement: no command that places a skill can
// keep it from them, and a command that places one says who they are. A
// client that is not installed sees nothing and is not named, and a machine
// with none names none.
func universalClients(snap scan.Snapshot) []string {
	ids := []string{}
	for _, c := range snap.Configurations {
		if c.ReadsLibrary {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// skillFromLibrary builds the event of one library skill from its lineage
// record, the canonical URLs of the sources the settings hold, the
// placements a scan found, the universal clients that scan detected and
// what was observed of its directory and of its places.
//
// Every state is derived here, on every read, and nothing is ever written
// for one: the lineage says where the skill came from and the settings say
// which sources this machine has, so removing a source and adding it back
// each change what the next read says, and nothing else. What decides that
// a source is gone is its settings entry and not its ref in the account
// repo: an entry whose ref is missing is a source this machine still has
// and has not fetched, which is what an import leaves.
func skillFromLibrary(lib scan.LibrarySkill, rec lineage.Record, ok bool, sources map[string]bool, places []placementEvent, universal []string, obs observation) librarySkillEvent {
	ev := librarySkillEvent{event: newEvent("library_skill"), LibraryEntry: scan.LibraryEntry{
		Name: lib.Name, Kind: lineage.KindUnmanaged, ContentHash: lib.ContentHash, Placements: places, Universal: universal,
	}}
	if !ok {
		return ev
	}
	ev.Kind = rec.Kind
	if rec.HasImport {
		subpath := rec.Import.Path
		ev.Source, ev.Subpath, ev.UpstreamCommit, ev.BaseHash = rec.Import.Source, &subpath, rec.Import.Commit, rec.Import.Hash
	}
	// A managed skill's base version is the import commit its branch points
	// at, so the directory's tree can be compared with that commit's; a
	// fork's base is the last version merged into it, which a later command
	// reads from its history. A directory that could not be read whole is
	// not known to hold the base, and is not called current.
	if rec.Kind == lineage.KindManaged && rec.HasImport {
		ev.State = stateCurrent
		if !obs.read || !rec.Current(obs.tree) {
			ev.State = stateModified
		}
		// The coordinates stay as the lineage has them: they are still where
		// the skill came from, and they are what adding the source again
		// needs. A fork is not judged this way, since the source that
		// matters to a fork is the account remote it is published to, and
		// its third-party upstream is only where later versions are merged
		// in from.
		ev.Drift = driftOf(obs, !sources[rec.Import.Source])
	}
	return ev
}

// sourceURLs are the canonical URLs of the sources the settings hold, the
// one thing a library skill's drift is judged against besides its lineage.
func sourceURLs(s home.Settings) map[string]bool {
	urls := make(map[string]bool, len(s.Sources))
	for _, src := range s.Sources {
		urls[src.URL] = true
	}
	return urls
}
