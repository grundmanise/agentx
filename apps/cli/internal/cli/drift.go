package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
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
// its lineage and the settings: whether its directory holds its base
// version, and what the configurations' own places hold. It is read from
// the filesystem, so a snapshot reads it under the lock it reads the
// library under, and every other report reads it with the rest of what it
// reports on.
type observation struct {
	judged   bool     // the skill has a state: a managed skill with a base version, or a fork whose library entry leads into its worktree
	modified bool     // the directory does not hold its base version, see holdsBase, or its branch tip, see holdsTip
	placed   []string // the drift states of the placements, sorted
	pending  bool     // an update left a merge pending for the skill, see pendingMerges
}

// observe reads what a skill's drift is judged from: a managed skill's
// directory against its base version, a fork's skill directory against its
// branch tip, and the places of either. An unmanaged skill has nothing to
// drift from, and nor does a fork whose library entry does not lead into
// its worktree: a directory of its own there, or one the fork has no
// worktree for, is not the fork's checkout, and comparing it with the
// branch would call edits what may be someone else's files.
func (sc skillContext) observe(ctx context.Context, inv *invocation, lib scan.LibrarySkill) observation {
	rec, ok := sc.records[lib.Name]
	switch {
	case !ok:
		return observation{}
	case rec.Kind == lineage.KindManaged && rec.HasImport:
		return observation{judged: true, modified: !inv.holdsBase(ctx, lib, rec), placed: sc.placementDrift(inv, lib), pending: sc.merges[lib.Name]}
	case rec.Kind == lineage.KindFork:
		dir, ok := inv.placedForkDir(lib.Name, lib.ResolvedPath)
		if !ok {
			return observation{}
		}
		return observation{judged: true, modified: !inv.holdsTip(ctx, lib, rec, dir), placed: sc.placementDrift(inv, lib), pending: sc.merges[lib.Name]}
	}
	return observation{}
}

// observeAll reads the observation of every skill of the library ahead of
// the report, for a snapshot, which composes its library entries after it
// released the lock the reads have to happen under.
func (sc *skillContext) observeAll(ctx context.Context, inv *invocation, libs []scan.LibrarySkill) {
	sc.observed = make(map[string]observation, len(libs))
	for _, lib := range libs {
		sc.observed[lib.Name] = sc.observe(ctx, inv, lib)
	}
}

// observationOf is the observation of lib: the one read ahead of the
// report when there was one, and a fresh read otherwise.
func (sc skillContext) observationOf(ctx context.Context, inv *invocation, lib scan.LibrarySkill) observation {
	if obs, ok := sc.observed[lib.Name]; ok {
		return obs
	}
	return sc.observe(ctx, inv, lib)
}

// placementDrift is what the configurations' own places say about a
// managed skill or a fork: the drift word of every place drift asks
// about, see ownPlaces and placeSite.drift, each once, sorted.
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
// for. Drift reads them, and skill place puts back what drift finds there.
//
// Only an enabled configuration is asked about: a disabled one is one the
// user chose not to place into. A path two configurations share, as
// Zencoder and Zenflow share one skills directory, is one place and is
// judged once, as refreshCopies plans it once: it is asked about when
// either configuration is enabled, and it is to hold a copy when copy_mode
// records one for either of them, since a copy placed for one is the copy
// the other reads. Judged for each configuration on its own, the copy
// agentx placed for one would read as a directory displacing the other's
// link, and skill place would plan the one path twice, the second step
// finding the first one's work there and stopping the mutation part way.
// So is a path two configurations spell differently, one of their skills
// directories a symlink to the other's, the link's own spelling in another
// case included: places are told apart as placeKey tells them, and each
// configuration keeps its own spelling in paths.
func ownPlaces(targets []placeTarget, library, name string, disabled, copies []string) []placeSite {
	var places []placeSite
	var keys []placeKey // the key of each of places, in the same order
	for _, t := range targets {
		if t.readsLibrary {
			continue
		}
		path := t.ownPlace(library, name)
		key := keyOf(path)
		i := slices.IndexFunc(keys, key.is)
		if i < 0 {
			i = len(places)
			keys = append(keys, key)
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

// placeKey is how the paths of places are told apart: the path as
// canonicalPath writes it, and the directory it sits in, read once as a
// file, nil when it cannot be read. Drift judges each place once by it, a
// revert or an update refreshes each copy once, and skill place changes
// each path once.
type placeKey struct {
	real string
	dir  fs.FileInfo
}

// keyOf is the placeKey of the entry at path.
func keyOf(path string) placeKey {
	return keyAt(canonicalPath(path))
}

// keyAt is the placeKey of real, a path as canonicalPath writes it.
func keyAt(real string) placeKey {
	k := placeKey{real: real}
	if dir, err := os.Lstat(filepath.Dir(real)); err == nil {
		k.dir = dir
	}
	return k
}

// is reports whether k and o are one place: spelled the same, or the same
// name in one directory, that directory compared as a file. canonicalPath
// spells a directory as the links on the way to it spell it, and on a disk
// that ignores case two links can spell one directory in two cases, which
// the spellings alone would take for two places, and skill place would
// plan the one path twice. The directory is compared, not the entry, so
// that a place nothing is at yet is found to be one as well.
func (k placeKey) is(o placeKey) bool {
	return k.real == o.real ||
		filepath.Base(k.real) == filepath.Base(o.real) && k.dir != nil && o.dir != nil && os.SameFile(k.dir, o.dir)
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
//     not a directory displacing a placement. It earns "", and skill
//     place never replaces it; see isLibraryDirectory.
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

// driftOf is the drift list of a skill: the states of its placements,
// source removed and upstream removed, sorted, or nil when it is in none. A
// fork is never source removed or upstream removed: the source that
// matters to it is the account remote, and nothing marks its upstream.
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
//
// A fork reconciliation would not call restored is named the same way,
// after them: one whose worktree is missing, and one with something of the
// user's in the way, see forkWarnings.
func (sc skillContext) absentWarnings(inv *invocation, libs []scan.LibrarySkill) []string {
	var warnings []string
	for _, name := range sc.absentManaged(libs) {
		what, wayOut := sc.absentNotice(inv, name)
		warnings = append(warnings, what+"; "+wayOut)
	}
	return append(warnings, inv.forkWarnings(sc.records, "")...)
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

// newSkillContext is the context of a report built from what it already
// read: the lineage, the settings and their copy modes. The configurations
// placements can be made in are detected here, once for the whole report.
func newSkillContext(inv *invocation, records map[string]lineage.Record, s home.Settings, modes map[string][]string) skillContext {
	merges, err := inv.pendingMerges()
	if err != nil {
		inv.out.debugf("cannot read the pending merges: %v", err)
	}
	return skillContext{
		records:  records,
		modes:    modes,
		sources:  sourceURLs(s),
		disabled: s.DisabledConfigurations,
		targets:  inv.detectedTargets(),
		merges:   merges,
	}
}
