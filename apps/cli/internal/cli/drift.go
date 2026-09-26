package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// The drift states a managed skill's placements can be in, beside source
// removed. Both are judged at each configuration's own place, the path a
// placement of the skill is made at, and never from what a scan found
// resolving to the library: a directory that replaced a link does not
// resolve to the library and would read as nothing at all, which is the
// one thing displaced exists to tell apart from missing.
const (
	// driftDisplaced is a placement whose kind on disk is not the one the
	// settings keep for it: a real directory where agentx keeps a symlink,
	// which is what an installer that copies over links leaves, or the
	// library's own link where copy_mode records a copy.
	driftDisplaced = "displaced"
	// driftMissing is an enabled configuration with nothing at its place:
	// the skill is not placed there, whether it never was or the placement
	// went. It says what is not placed and asks for nothing.
	driftMissing = "missing"
)

// observation is what the drift of one library skill is judged from beside
// its lineage and the settings: the tree its directory holds, as git would
// record it, and what the configurations' own places hold. It is read from
// the filesystem, so a snapshot reads it under the lock it reads the
// library under, and every other report reads it with the rest of what it
// reports on.
type observation struct {
	tree   treeid.Tree
	read   bool     // the directory was read whole; one that could not be is not the base version
	placed []string // the drift states of the placements, sorted
}

// observe reads what a managed skill's drift is judged from, and nothing for
// any other: a fork's drift is decided by its own history and an unmanaged
// skill has no base to drift from.
func (sc skillContext) observe(inv *invocation, lib scan.LibrarySkill) observation {
	rec, ok := sc.records[lib.Name]
	if !ok || rec.Kind != lineage.KindManaged || !rec.HasImport {
		return observation{}
	}
	obs := observation{placed: sc.placementDrift(inv, lib)}
	tree, err := treeid.Read(lib.ResolvedPath)
	obs.tree, obs.read = tree, err == nil
	return obs
}

// observeAll reads the observation of every skill of the library ahead of
// the report, for a snapshot, which composes its library entries after it
// released the lock the reads have to happen under.
func (sc *skillContext) observeAll(inv *invocation, libs []scan.LibrarySkill) {
	sc.observed = make(map[string]observation, len(libs))
	for _, lib := range libs {
		sc.observed[lib.Name] = sc.observe(inv, lib)
	}
}

// observationOf is the observation of lib: the one read ahead of the
// report when there was one, and a fresh read otherwise.
func (sc skillContext) observationOf(inv *invocation, lib scan.LibrarySkill) observation {
	if obs, ok := sc.observed[lib.Name]; ok {
		return obs
	}
	return sc.observe(inv, lib)
}

// placementDrift is what the configurations' own places say about a managed
// skill. Only an enabled configuration with a skills directory of its own is
// asked: a universal client's placement is the library entry itself, which
// the skill cannot be missing from while it is in the library, and a
// disabled configuration is one the user chose not to place into.
//
//   - Nothing at the place is missing: the configuration does not see the
//     skill through a placement of its own.
//   - A real directory where the settings keep a symlink is displaced, and
//     so is the library's own link where copy_mode records a copy: the kind
//     on disk is not the mode agentx keeps. A copy whose content differs is
//     still the copy and earns nothing here; see keepCopy.
//   - A link of the user's to somewhere else, and anything else at the
//     place, is theirs: the place is taken, so the skill is not missing,
//     and nothing agentx keeps was displaced.
//
// The rule is literal: a skill placed with --to, or taken out of one
// configuration with --from, is missing from every other enabled one, which
// is what the word is there to say. It is information and never a failure.
func (sc skillContext) placementDrift(inv *invocation, lib scan.LibrarySkill) []string {
	var drift []string
	add := func(word string) {
		if !slices.Contains(drift, word) {
			drift = append(drift, word)
		}
	}
	copies := sc.modes[lib.Name]
	for _, t := range sc.targets {
		if t.readsLibrary || slices.Contains(sc.disabled, t.id) {
			continue
		}
		place := t.ownPlace(inv.dirs.Library, lib.Name)
		info, err := os.Lstat(place)
		copied := slices.Contains(copies, t.id)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			add(driftMissing)
		case err != nil:
			// A place this machine cannot read says nothing either way.
		case info.Mode()&os.ModeSymlink != 0:
			if copied && sameTarget(place, lib.Path) {
				add(driftDisplaced)
			}
		case info.IsDir():
			if !copied {
				add(driftDisplaced)
			}
		}
	}
	sort.Strings(drift)
	return drift
}

// driftOf is the drift list of a managed skill: the states of its
// placements and source removed, sorted, or nil when it is in none.
func driftOf(obs observation, sourceRemoved bool) []string {
	drift := append([]string(nil), obs.placed...)
	if sourceRemoved {
		drift = append(drift, driftSourceRemoved)
	}
	if len(drift) == 0 {
		return nil
	}
	sort.Strings(drift)
	return drift
}

// holdsVersion reports whether the directory at path holds exactly the
// version v, as git would record the two: false for a directory this
// machine cannot read whole.
func holdsVersion(path string, v *imported) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	tree, err := treeid.Read(real)
	return err == nil && lineage.Holds(tree, v.dir, v.tree)
}

// newSkillContext is the context of a report built from what it already
// read: the lineage, the settings and their copy modes. The configurations
// placements can be made in are detected here, once for the whole report.
func newSkillContext(inv *invocation, records map[string]lineage.Record, s home.Settings, modes map[string][]string) skillContext {
	return skillContext{
		records:  records,
		modes:    modes,
		sources:  sourceURLs(s),
		disabled: s.DisabledConfigurations,
		targets:  inv.detectedTargets(),
	}
}
