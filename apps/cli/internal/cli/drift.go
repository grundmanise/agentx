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
// skill: the drift word of every place drift asks about, see ownPlaces and
// placeSite.drift, each once, sorted.
//
// The rule is literal: a skill placed with --to, or taken out of one
// configuration with --from, is missing from every other enabled one, which
// is what the word is there to say. It is information and never a failure.
func (sc skillContext) placementDrift(inv *invocation, lib scan.LibrarySkill) []string {
	var drift []string
	for _, p := range ownPlaces(sc.targets, inv.dirs.Library, lib.Name, sc.disabled, sc.modes[lib.Name]) {
		if !p.asked() {
			continue
		}
		if word := p.drift(lib.Path); word != "" && !slices.Contains(drift, word) {
			drift = append(drift, word)
		}
	}
	sort.Strings(drift)
	return drift
}

// placeSite is one path a skill's placements are made at, and every
// configuration whose own place it is. A universal client has none: its
// placement is the library entry itself, which the skill cannot be missing
// from while it is in the library.
type placeSite struct {
	path    string        // the place, as the first of targets names it
	targets []placeTarget // the configurations whose own place it is, in detection order
	paths   []string      // the place as each of targets names it, in the same order
	enabled []string      // the ids of those of them the settings do not disable
	copied  bool          // copy_mode records a copy for one of them, so the place is to hold a copy
}

// asked reports whether drift asks what the place holds: whether one of
// the configurations whose own place it is is enabled.
func (p placeSite) asked() bool { return len(p.enabled) > 0 }

// ownPlaces are the places of the skill called name, one per path, in the
// order the configurations were detected, judged against the configurations
// the settings disable and the ones copy_mode records a copy of the skill
// for. Drift reads them, and a repair puts back what drift finds there.
//
// Only an enabled configuration is asked about: a disabled one is one the
// user chose not to place into. A path two configurations share, as
// Zencoder and Zenflow share one skills directory, is one place and is
// judged once, as refreshCopies plans it once: it is asked about when
// either configuration is enabled, and it is to hold a copy when copy_mode
// records one for either of them, since a copy placed for one is the copy
// the other reads. Judged for each configuration on its own, the copy
// agentx placed for one would read as a directory displacing the other's
// link, and a repair would plan the one path twice, the second step
// finding the first one's work there and stopping the mutation part way.
// So is a path two configurations spell differently, one of their skills
// directories a symlink to the other's: places are told apart by
// canonicalPath, and each configuration keeps its own spelling in paths.
func ownPlaces(targets []placeTarget, library, name string, disabled, copies []string) []placeSite {
	var places []placeSite
	at := map[string]int{}
	for _, t := range targets {
		if t.readsLibrary {
			continue
		}
		path := t.ownPlace(library, name)
		key := canonicalPath(path)
		i, seen := at[key]
		if !seen {
			i = len(places)
			at[key] = i
			places = append(places, placeSite{path: path})
		}
		p := &places[i]
		p.targets = append(p.targets, t)
		p.paths = append(p.paths, path)
		if !slices.Contains(disabled, t.id) {
			p.enabled = append(p.enabled, t.id)
		}
		p.copied = p.copied || slices.Contains(copies, t.id)
	}
	return places
}

// drift is what the place holds now, in the words of drift, the library
// directory being libPath:
//
//   - Nothing at the place is missing: the configuration does not see the
//     skill through a placement of its own.
//   - A real directory where the settings keep a symlink is displaced, and
//     so is the library's own link where copy_mode records a copy: the kind
//     on disk is not the mode agentx keeps. A copy whose content differs is
//     still the copy and earns nothing here; see keepCopy.
//   - The library directory itself, which a library entry made a symlink
//     to the place leaves there, is what the client reads: the library,
//     not a directory displacing a placement. It earns "", and a repair
//     never plans it; see isLibraryDirectory.
//   - A link of the user's to somewhere else, and anything else at the
//     place, is theirs: the place is taken, so the skill is not missing,
//     and nothing agentx keeps was displaced. It earns "".
func (p placeSite) drift(libPath string) string {
	info, err := os.Lstat(p.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return driftMissing
	case err != nil:
		// A place this machine cannot read says nothing either way.
	case info.Mode()&os.ModeSymlink != 0:
		if p.copied && sameTarget(p.path, libPath) {
			return driftDisplaced
		}
	case info.IsDir():
		if !p.copied && !isLibraryDirectory(p.path, libPath) {
			return driftDisplaced
		}
	}
	return ""
}

// driftOf is the drift list of a managed skill: the states of its
// placements, source removed and upstream removed, sorted, or nil when it
// is in none.
func driftOf(obs observation, sourceRemoved, upstreamRemoved bool) []string {
	drift := append([]string(nil), obs.placed...)
	if sourceRemoved {
		drift = append(drift, driftSourceRemoved)
	}
	if upstreamRemoved {
		drift = append(drift, driftUpstreamRemoved)
	}
	if len(drift) == 0 {
		return nil
	}
	sort.Strings(drift)
	return drift
}

// absentManaged are the names of the managed skills whose library
// directory is not among libs, sorted: an import branch the account repo
// holds for a skill the library no longer holds, because its directory was
// deleted, or lost its SKILL.md, outside agentx. Such a skill has no entry
// to carry a state or a drift: there is no directory to compare with its
// base, and a configuration cannot be missing what the library does not
// hold. It is named in a warning instead, see absentWarnings. The branches
// are the ones the report already read, so this runs no git.
func (sc skillContext) absentManaged(libs []scan.LibrarySkill) []string {
	held := make(map[string]bool, len(libs))
	for _, lib := range libs {
		held[lib.Name] = true
	}
	var names []string
	for name, rec := range sc.records {
		if rec.Kind == lineage.KindManaged && !held[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// absentWarnings are the warnings that name the managed skills the library
// no longer holds, one per skill, sorted by name, each with the two ways
// out. skill add installs the skill again, but only while the source still
// holds the version the import branch names: it installs the version the
// source holds now, and refuses while the branch names another, pointing
// at the removal. skill remove takes what is left of the skill off the
// machine whatever the source holds. A directory still at the library
// path, one that lost its SKILL.md, has to be moved aside first for an
// install, which refuses to write over it. skill list prints the warnings
// and a snapshot carries them, so the user and the desktop app both learn
// that the skill is gone. Whether the path is taken is read from the
// filesystem; nothing here runs git or reads a source.
func (sc skillContext) absentWarnings(inv *invocation, libs []scan.LibrarySkill) []string {
	var warnings []string
	for _, name := range sc.absentManaged(libs) {
		what, wayOut := sc.absentNotice(inv, name)
		warnings = append(warnings, what+"; "+wayOut)
	}
	return warnings
}

// absentNotice is what is said of one managed skill the library no longer
// holds, as absentWarnings says it: what is wrong, and the two ways out. A
// command that cannot work on such a skill refuses with the same words,
// the ways out as its hint.
func (sc skillContext) absentNotice(inv *invocation, name string) (what, wayOut string) {
	from := "<source>"
	if rec := sc.records[name]; rec.HasImport {
		from = shellWord(rec.Import.Source)
	}
	add := "'agentx skill add " + from + " --skill " + shellWord(name) + "'"
	remove := "'" + skillCommand("remove", name) + "'"
	libPath := quotedPath(inv.libraryPath(name))
	if _, err := os.Lstat(inv.libraryPath(name)); err == nil {
		return name + " is managed in the account repo but " + libPath + " holds no SKILL.md",
			"move " + libPath + " aside, then run " + add + " to install it again, or run " + remove + " to stop managing it"
	}
	return name + " is managed in the account repo but the library holds no skill directory for it",
		"run " + add + " to install it again, or " + remove + " to stop managing it"
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
