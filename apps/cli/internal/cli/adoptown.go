package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// agentx adopt --skill takes over an unmanaged skill no other tool
// recorded, as one of the user's own. It is a fork in the skill's place
// with nothing to fork from, see forkOf and makeFork: a branch of its own
// whose first commit holds the skill as it is, its directory moved into
// the worktree, and the library symlink to it. The skill is the library's,
// or a directory in an agent client's own skills directory, which the
// library does not hold yet: that one moves out of the client's directory,
// which gets the library's symlink back, as a placement.

// adoptPlace is one directory of a skill in an agent client's own skills
// directory, as an adoption found it.
type adoptPlace struct {
	path    string        // the directory, as the first of targets names it
	targets []placeTarget // the configurations whose own skills directory holds it, in detection order
	state   string        // what it held when it was compared, in the words a journal records
	tree    string        // its tree id, as git would record it
	whole   bool          // git can record everything it holds
}

// ids are the configurations whose own skills directory holds the place.
func (p adoptPlace) ids() []string { return targetIDs(p.targets) }

// linked reports whether the place gets the library's symlink once its
// directory is gone: one of its configurations does not read the library.
// A client that reads it sees the skill there, and a link in its own
// skills directory would list the skill twice.
func (p adoptPlace) linked() bool {
	return slices.ContainsFunc(p.targets, func(t placeTarget) bool { return !t.readsLibrary })
}

// same reports whether p holds what o holds: the tree git would record
// for either, and, where git cannot record everything one of them holds,
// the same bytes.
func (p adoptPlace) same(o adoptPlace) bool {
	return p.tree == o.tree && (p.whole && o.whole || p.state == o.state)
}

// adoptPlacesDiffer reports whether one of places holds anything but what
// the first holds. Pure.
func adoptPlacesDiffer(places []adoptPlace) bool {
	for _, p := range places[1:] {
		if !p.same(places[0]) {
			return true
		}
	}
	return false
}

// planAdoption finds the unmanaged skill called name and runs every
// refusal of its adoption before anything is written, see adoptSourceOf
// and forkOf. missing is the answer when this machine holds no such skill.
func (inv *invocation) planAdoption(ctx context.Context, name string, missing error) (*forking, error) {
	fk, records, err := inv.beginFork(ctx, modeAdopt)
	if err != nil {
		return nil, err
	}
	src, err := inv.adoptSourceOf(fk, name, records, missing)
	if err != nil {
		return nil, err
	}
	if err := inv.forkOf(ctx, fk, src, "", records); err != nil {
		return nil, err
	}
	if err := inv.readAdoptPlaces(fk); err != nil {
		return nil, err
	}
	return fk, nil
}

// adoptSourceOf finds the unmanaged skill called name. The library comes
// first: an entry no branch records is the skill, whatever the clients'
// own directories hold under the name, and one a branch records is managed
// already. Then the detected configurations' own skills directories, see
// adoptSites: the real directories found there are the skill, the first by
// configuration the one that moves, and fk is told of them. A name only a
// symlink or a plugin holds is refused, and any other is missing.
func (inv *invocation) adoptSourceOf(fk *forking, name string, records map[string]lineage.Record, missing error) (forkSource, error) {
	// The name is joined to a path below, so one that is no single visible
	// directory name is no skill before anything is looked up under it.
	if name == "" || strings.HasPrefix(name, ".") || filepath.Base(name) != name {
		return forkSource{}, missing
	}
	shown := sanitised(name)
	if lib, held := librarySkill(inv.dirs.Library, name); held {
		rec, known := records[name]
		if !known {
			return forkSource{kind: lineage.KindUnmanaged, name: name, lib: lib, held: true, dir: lib.ResolvedPath}, nil
		}
		what, own := "a skill of a shared source", rec.Kind == lineage.KindFork
		if own {
			what = "one of your own skills"
		}
		dirs, _ := inv.adoptSites(name)
		// A copy copy_mode records is agentx's own placement of the skill.
		if s, err := inv.loadSettings(); err == nil {
			if modes, err := inv.copyModes(s); err == nil {
				dirs = slices.DeleteFunc(dirs, func(p adoptPlace) bool {
					return slices.ContainsFunc(p.ids(), func(id string) bool { return slices.Contains(modes[name], id) })
				})
			}
		}
		if len(dirs) > 0 {
			// The way out names the configuration a placement of the library's
			// skill can take the directory's place in. One whose client reads
			// the library has that skill already, and one of the user's own is
			// never placed over a directory.
			hint := "rename the directory and adopt it under the new name, or "
			switch i := slices.IndexFunc(dirs[0].targets, func(t placeTarget) bool { return !t.readsLibrary }); {
			case i < 0:
				hint += "remove it"
			case own:
				hint += "remove it and place yours with '" + skillCommand("place", name, "--to", dirs[0].targets[i].id) + "'"
			default:
				hint += "replace it with '" + skillCommand("place", name, "--to", dirs[0].targets[i].id, "--force") + "', which discards what it holds"
			}
			return forkSource{}, fail(exitRefused, fmt.Sprintf("the library already holds a skill called %s, %s, so %s cannot be adopted under that name", shown, what, quotedPath(dirs[0].path)), hint)
		}
		message := shown + " is already managed by agentx"
		if own {
			message = shown + " is already one of your own skills"
		}
		return forkSource{}, fail(exitRefused, message, "run 'agentx skill list' to see it")
	}
	dirs, links := inv.adoptSites(name)
	switch {
	case len(dirs) > 0:
		fk.from, fk.places = dirs[0].path, dirs
		return forkSource{kind: lineage.KindUnmanaged, name: name, dir: dirs[0].path}, nil
	case len(links) > 0:
		target, _ := os.Readlink(links[0])
		return forkSource{}, fail(exitRefused, fmt.Sprintf("%s is a symlink to %s, so there is no directory of %s to adopt", quotedPath(links[0]), quotedPath(target), shown),
			"replace the link with the directory it points to, then run '"+adoptCommand(name)+"' again")
	}
	inventory, err := fk.scanned()
	if err != nil {
		return forkSource{}, err
	}
	switch o, err := pluginOccurrence(inventory, name); {
	case err == nil:
		return forkSource{}, fail(exitRefused, shown+" comes with the plugin "+sanitised(o.Plugin)+", whose directory agentx never writes",
			"fork it with '"+skillCommand("fork", name)+"'")
	case failureOf(err).status != exitNotFound:
		return forkSource{}, err // several plugins provide it, and the refusal names them
	}
	return forkSource{}, missing
}

// adoptSites are what the detected configurations' own skills directories
// hold under name, each path once, see placeKey, in the order the
// configurations were detected, which is by id: dirs, the real directories
// that are a skill, and links, the symlinks. The library entry is neither,
// by whatever path a client reads it. Only the user's own skills
// directories are read, so a project's skill and a plugin's are no part of
// it, and a configuration counts whether or not it is enabled.
func (inv *invocation) adoptSites(name string) (dirs []adoptPlace, links []string) {
	libPath := inv.libraryPath(name)
	libKey := keyOf(libPath)
	type seen struct {
		key placeKey
		dir int // its place in dirs, -1 for a path that is no directory of the skill
	}
	var found []seen
	for _, t := range inv.detectedTargets() {
		if t.dir == "" {
			continue
		}
		path := filepath.Join(t.dir, name)
		key := keyOf(path)
		if key.is(libKey) {
			continue
		}
		if i := slices.IndexFunc(found, func(s seen) bool { return s.key.is(key) }); i >= 0 {
			if d := found[i].dir; d >= 0 {
				dirs[d].targets = append(dirs[d].targets, t)
			}
			continue
		}
		at := -1
		switch info, err := os.Lstat(path); {
		case err != nil:
		case info.Mode()&os.ModeSymlink != 0:
			links = append(links, path)
		case info.IsDir() && holdsSkillFile(path) && !isLibraryDirectory(path, libPath):
			at = len(dirs)
			dirs = append(dirs, adoptPlace{path: path, targets: []placeTarget{t}})
		}
		found = append(found, seen{key, at})
	}
	return dirs, links
}

// adoptTakenHint is the hint of an adoption whose name a branch of the
// account repo already holds, records being its branches. The directory
// can be renamed, and one found in an agent client's own skills directory
// may also be in the way of the skill the branch is, whose library entry
// is gone: one of the user's own is put back, and any other let go of.
func (fk *forking) adoptTakenHint(records map[string]lineage.Record) string {
	rename := "rename the directory and adopt it under the new name"
	rec, taken := nameTaken(records, fk.target, "")
	switch {
	case !taken || len(fk.places) == 0:
		return rename
	case rec.Kind == lineage.KindFork:
		return rename + ", or put your skill back with '" + skillCommand("place", rec.Name) + "'"
	}
	return rename + ", or stop managing the other with '" + skillCommand("remove", rec.Name) + "'"
}

// adoptClash refuses an adoption out of an agent client's own skills
// directory whose name the account remote already held a skill of at its
// last fetch: adopted, it would be another skill of that name, which a
// publish refuses. An adoption of a library entry is not asked, as a fork
// in its place never was.
func (inv *invocation) adoptClash(ctx context.Context, fk *forking) error {
	if len(fk.places) == 0 {
		return nil
	}
	if _, remote, set, err := inv.accountSource(); err != nil || !set || !inv.remoteHolds(ctx, remote, fk.target) {
		return nil
	}
	return fail(exitRefused, "your account remote already holds a skill called "+sanitised(fk.target),
		"install it with '"+accountAddCommand(fk.target)+"' once "+quotedPath(fk.from)+" is moved aside, or rename the directory and adopt it under the new name")
}

// adoptRoom refuses an adoption out of agent clients' own skills
// directories that the library or one of those directories has no room
// for: something at the library path, where the skill's symlink goes, and a
// skills directory this machine cannot write, which the journal would stop
// at with the skill half moved. Any other fork needs neither.
func (inv *invocation) adoptRoom(fk *forking) error {
	if len(fk.places) == 0 {
		return nil
	}
	if libPath := inv.libraryPath(fk.src.name); lexists(libPath) {
		return fail(exitRefused, "the library already holds "+quotedPath(libPath), "move it aside, then "+fk.again())
	}
	for _, p := range fk.places {
		dir := filepath.Dir(p.path)
		if err := unix.Access(dir, unix.W_OK|unix.X_OK); err != nil {
			return fail(exitRefused, "cannot adopt "+quotedPath(p.path)+": "+(&fs.PathError{Op: "access", Path: dir, Err: err}).Error(),
				"make "+quotedPath(dir)+" writable, then "+fk.again())
		}
	}
	return nil
}

// readAdoptPlaces reads what every directory of an adoption out of agent
// clients' own skills directories holds, as planPlace compares a displaced
// directory with the library: the tree git would record, and the bytes for
// what it cannot. The first moves into the worktree and the others are
// removed, so one that holds anything else refuses the adoption, naming
// them all.
func (inv *invocation) readAdoptPlaces(fk *forking) error {
	for i := range fk.places {
		p := &fk.places[i]
		state, err := home.State(p.path)
		if err != nil {
			return libraryFailure(filepath.Dir(p.path), err)
		}
		tree, err := treeid.Read(p.path)
		if err != nil {
			return libraryFailure(filepath.Dir(p.path), err)
		}
		p.state, p.tree, p.whole = state, tree.ID, len(tree.Unrecordable) == 0
	}
	if len(fk.places) == 0 || !adoptPlacesDiffer(fk.places) {
		return nil
	}
	found := make([]string, len(fk.places))
	for i, p := range fk.places {
		found[i] = quotedPath(p.path) + " (" + strings.Join(p.ids(), ", ") + ")"
	}
	return fail(exitRefused, sanitised(fk.src.name)+" is a directory with different content in several configurations: "+strings.Join(found, ", "),
		"keep one, remove or rename the others, then "+fk.again())
}

// adoptPlacesHold reports, under the lock, whether the machine still holds
// what the adoption was planned from: nothing at the library path, and the
// same directories in the same configurations, each holding what it held.
func (inv *invocation) adoptPlacesHold(fk *forking) bool {
	if lexists(inv.libraryPath(fk.src.name)) {
		return false
	}
	now, _ := inv.adoptSites(fk.src.name)
	if len(now) != len(fk.places) {
		return false
	}
	for i, p := range fk.places {
		if now[i].path != p.path || !slices.Equal(now[i].ids(), p.ids()) {
			return false
		}
		if state, err := home.State(p.path); err != nil || state != p.state {
			return false
		}
	}
	return true
}

// stageAdoptPlaces plans, into m, what becomes of the directories of an
// adoption out of agent clients' own skills directories, once the first
// has moved into the worktree: every other one, which holds the same
// content, is removed, retained until the mutation is verified, and each
// gets the library's symlink where a placement belongs, see
// adoptPlace.linked. done records the configurations that see the skill
// there. No other configuration gets a placement. Any other fork has no
// such directory, and nothing is planned for it.
func stageAdoptPlaces(m *home.Mutation, fk *forking, libPath string, done *placements) {
	for i, p := range fk.places {
		if i > 0 {
			m.Remove(p.path, p.state)
		}
		if p.linked() {
			m.Link(p.path, libPath)
		}
		done.placed = append(done.placed, p.targets...)
	}
}

// reportAdoptedOwn reads the machine again and reports the adoption: the
// library_skill event of the skill, which is one of the user's own now,
// and a confirmation, with one row per configuration whose own skills
// directory held it when it came out of one. A symlink of the user's to
// the skill in another configuration's skills directory is left as it is,
// and said to be.
func (inv *invocation) reportAdoptedOwn(ctx context.Context, fk *forking, done placements) error {
	inventory, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	lib, ok := librarySkill(inv.dirs.Library, fk.target)
	if !ok {
		return fail(exitInternal, "the library holds no "+fk.target+" after adopting it", "run 'agentx doctor' and check the library it names")
	}
	out, name := inv.out, sanitised(fk.target)
	ev := sc.librarySkillEventFor(ctx, inv, inventory, lib, nil)
	out.emit(ev)
	if len(fk.places) == 0 {
		inv.summary = "adopted " + name + "; it is now one of your own skills"
		out.done("adopted " + out.paint(heading, name) + "; it is now one of your own skills")
		inv.noAccountRemoteHint(ev)
		return nil
	}
	_, links := inv.adoptSites(fk.target)
	for _, link := range links {
		if !sameTarget(link, lib.Path) {
			out.warn(quotedPath(link) + " is a link of your own and was left as it is")
		}
	}
	rows := inv.ownPlacements(fk.target, done.placed, filterPlacements(ev.Placements, targetIDs(done.placed)))
	out.done("adopted " + out.paint(heading, name) + " from " + quotedPath(fk.from) + ": " + out.paint(noteStyle, plural(len(rows), "placement")))
	inv.printPlacementRows(fk.target, rows)
	inv.printUniversal(ev.Universal)
	inv.noAccountRemoteHint(ev)
	inv.summary = "adopted " + name + " from " + quotedPath(fk.from) + " in " + plural(len(done.placed), "configuration") + universalClause(ev.Universal)
	return nil
}
