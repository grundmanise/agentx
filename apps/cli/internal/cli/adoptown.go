package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// agentx adopt takes over an unmanaged skill, one no other tool recorded
// or one whose record names nowhere agentx can install it from, as one of
// the user's own. It is a fork in the skill's place with nothing to fork
// from, see forkOf and makeFork: a branch of its own whose first commit
// holds the skill as it is, its directory moved into the worktree, and the
// library symlink to it. The skill is the library's, or a directory in an
// agent client's own skills directory, which the library does not hold
// yet: that one moves out of the client's directory, which gets the
// library's symlink back, as a placement.

// adoptPlace is one directory of a skill in an agent client's own skills
// directory, as an adoption found it.
type adoptPlace struct {
	path    string        // the directory, as the first of targets names it
	targets []placeTarget // the configurations whose own skills directory holds it, in detection order
	state   string        // what it held when it was compared, in the words a journal records
	tree    string        // its tree id, as git would record it
	whole   bool          // git can record everything it holds
	// bare is its tree id without the system files, see home.SystemFiles,
	// and "" when a .gitignore of its own decides what git records.
	bare string
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
	if err := inv.readAdoptCopies(fk); err != nil {
		return nil, err
	}
	return fk, nil
}

// looseSkill is one unmanaged skill no tool recorded, as a preview lists
// it and --all adopts it: an entry of the library no branch records, or a
// folder in an agent client's own skills directory under a name the
// library holds no unmanaged skill of.
type looseSkill struct {
	name string
	path string // its folder: the library entry, or the folder in the client's skills directory
	link string // for a library entry that is a symlink, the folder it leads to
	// For a folder in a client's skills directory: managed says a skill
	// agentx manages has its name, and blocked is what is at the library
	// path, where its symlink would go, and is no skill.
	managed bool
	blocked string
	repo    string // its .git, when the folder is a Git repository of its own
}

// reason is why the skill cannot be adopted, as a row of a preview says
// it, without the skill's name, and "" for one that can: what is read
// without hashing a file or taking the lock. What else can refuse it, see
// checkForking, is found when it is adopted. Pure.
func (l looseSkill) reason() string {
	switch {
	case l.link != "":
		return "a symlink to " + quotedPath(l.link) + ". To adopt it, move that folder to " + quotedPath(l.path)
	case l.managed:
		return managedNameRow(l.path)
	case l.blocked != "":
		return quotedPath(l.blocked) + " is in the way and is not a skill. To adopt it, move " + quotedPath(l.blocked) + " somewhere else"
	case forkNameRefusal(l.name) != "":
		return invalidNameRow(l.path)
	case l.repo != "":
		return ownRepoRow(l.repo)
	}
	return ""
}

// managedNameRow is the row of a skill whose folder, dir, has the name of
// a skill agentx already manages. Pure.
func managedNameRow(dir string) string {
	return quotedPath(dir) + " has the name of a skill agentx already manages. To adopt it, rename that folder"
}

// invalidNameRow is the row of a skill whose folder, dir, has a name agentx
// cannot give a skill. Pure.
func invalidNameRow(dir string) string {
	return "not a valid skill name. To adopt it, rename " + quotedPath(dir)
}

// ownRepoRow is the row of a skill whose folder is a Git repository of its
// own, repo being its .git. Pure.
func ownRepoRow(repo string) string {
	return "a Git repository of its own. To adopt it, move " + quotedPath(repo) + " out of the folder"
}

// ownRepoRefusal refuses to adopt the skill called name, whose folder is a
// Git repository of its own, repo being its .git: moved with the folder,
// git run in the skill would find that repository and not agentx's.
func ownRepoRefusal(name, repo string) *failure {
	return refuse(exitRefused, sanitised(name)+" is a Git repository of its own, which agentx cannot adopt",
		"move "+quotedPath(repo)+" out of the folder, then run '"+adoptCommand(name)+"' again").listed(ownRepoRow(repo))
}

// invalidNameRefusal refuses to adopt the skill whose folder, dir, has the
// name name, which agentx cannot give a skill.
func invalidNameRefusal(name, dir string) *failure {
	return refuse(exitRefused, forkNameRefusal(name),
		"rename "+quotedPath(dir)+" to a valid name, then run 'agentx adopt --skill <new name>'").listed(invalidNameRow(dir))
}

// adoptChanged refuses an adoption of the skill called name that found it
// changed since it was planned.
func adoptChanged(name string) *failure {
	return refuse(exitRefused, sanitised(name)+" changed while it was being adopted, so nothing was changed", "run the command again").
		listed("changed while it was being adopted, so nothing was changed. To adopt it, run the command again")
}

// adoptRootRefusal refuses an adoption of the skill called name that
// something at root, where agentx keeps the skill once it is adopted, is
// in the way of.
func adoptRootRefusal(name, root string) *failure {
	return refuse(exitRefused, quotedPath(root)+" is in the way: agentx keeps "+sanitised(name)+" there once it is adopted",
		"move "+quotedPath(root)+" somewhere else, then run '"+adoptCommand(name)+"' again").
		listed(quotedPath(root) + " is in the way: agentx keeps the skill there. To adopt it, move " + quotedPath(root) + " somewhere else")
}

// adoptNestedRepoRefusal refuses to adopt the skill called name whose
// folder holds a Git repository at exposed, its .git relative to the
// folder, that no ignore rule keeps out: git would record it as a link and
// not as its files.
func adoptNestedRepoRefusal(name, exposed string) *failure {
	dir := strings.TrimSuffix(exposed, "/.git")
	return refuse(exitRefused, sanitised(name)+" holds a Git repository of its own at "+quotedPath(dir)+", which agentx cannot adopt with the skill",
		"add "+quotedPath(dir)+" to the skill's .gitignore to keep it on this machine only, or remove it, then run '"+adoptCommand(name)+"' again").
		listed("holds a Git repository of its own at " + quotedPath(dir) + ". To adopt it, add " + quotedPath(dir) + " to the skill's .gitignore, or remove it")
}

// differentDiskRefusal refuses an adoption of the skill called name out of
// from, a folder on another disk than agentx home, where it could only be
// copied, not moved. One in the library is put right by keeping the two on
// one disk; one in an agent client's skills directory by moving it into the
// library.
func (inv *invocation) differentDiskRefusal(name, from string, inLibrary bool) *failure {
	home := quotedPath(inv.dirs.Home) + ", where agentx keeps the skills it manages"
	if inLibrary {
		library := quotedPath(inv.dirs.Library)
		return refuse(exitRefused, "the library, "+library+", is on a different disk than "+home+", so "+sanitised(name)+" cannot be moved there",
			"keep AGENTX_HOME and AGENTX_LIBRARY on one disk").
			listed("in " + library + ", which is on a different disk than " + home + ". To adopt it, keep AGENTX_HOME and AGENTX_LIBRARY on one disk")
	}
	return refuse(exitRefused, sanitised(name)+" is on a different disk than "+home+", so it cannot be moved there",
		"move "+quotedPath(from)+" into "+quotedPath(inv.dirs.Library)+" yourself, then run '"+adoptCommand(name)+"' again").
		listed("on a different disk than " + home + ". To adopt it, move " + quotedPath(from) + " into " + quotedPath(inv.dirs.Library))
}

// skillFolders are the names of the entries of dir that are a skill, as a
// scan lists a skills directory: every visible entry but node_modules that
// is a directory, or leads to one, holding a SKILL.md. Nothing is hashed.
func skillFolders(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.IsDir() && holdsSkillFile(filepath.Join(dir, name)) {
			names = append(names, name)
		}
	}
	return names
}

// looseSkills finds the unmanaged skills no tool recorded, in name order:
// the entries of the library no branch records, then the folders in the
// detected configurations' own skills directories, see adoptSites, under a
// name the library holds no such entry of. A name a record of cands holds
// is that record's to answer for. A folder copy_mode records is agentx's
// own placement and no skill of its own. It reads the machine and writes
// nothing.
func (inv *invocation) looseSkills(cands []*candidate, records map[string]lineage.Record) ([]looseSkill, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	modes, err := inv.copyModes(s)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, c := range cands {
		seen[c.rec.name] = true
	}
	var loose []looseSkill
	for _, name := range skillFolders(inv.dirs.Library) {
		if _, known := records[name]; known || seen[name] {
			continue
		}
		seen[name] = true
		l := looseSkill{name: name, path: inv.libraryPath(name)}
		if info, err := os.Lstat(l.path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			l.link = linkedFolder(l.path)
		} else {
			l.repo = ownRepo(l.path)
		}
		loose = append(loose, l)
	}
	for _, t := range inv.detectedTargets() {
		if t.dir == "" {
			continue
		}
		for _, name := range skillFolders(t.dir) {
			if seen[name] {
				continue
			}
			seen[name] = true
			dirs, _ := inv.adoptSites(name)
			dirs = slices.DeleteFunc(dirs, func(p adoptPlace) bool {
				return slices.ContainsFunc(p.ids(), func(id string) bool { return slices.Contains(modes[name], id) })
			})
			if len(dirs) == 0 {
				continue
			}
			l := looseSkill{name: name, path: dirs[0].path, repo: ownRepo(dirs[0].path)}
			if _, l.managed = nameTaken(records, name, ""); !l.managed && lexists(inv.libraryPath(name)) {
				l.blocked = inv.libraryPath(name)
			}
			loose = append(loose, l)
		}
	}
	sort.Slice(loose, func(i, j int) bool { return loose[i].name < loose[j].name })
	return loose, nil
}

// ownRepo is the .git of the folder dir when it is a Git repository of
// its own, a directory or a file, and "" when it is none.
func ownRepo(dir string) string {
	if own := filepath.Join(dir, ".git"); lexists(own) {
		return own
	}
	return ""
}

// linkedFolder is where the symlink at link leads, as an absolute path: a
// relative link is read against the directory it sits in.
func linkedFolder(link string) string {
	target, _ := os.Readlink(link)
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	return filepath.Clean(target)
}

// symlinkRefusal refuses to adopt the skill called name, which this
// machine holds as the symlink at link and no folder: the folder it leads
// to is the user's to move. reason is the refusal as a row of a preview
// says it, without the name.
func symlinkRefusal(name, link string) (reason string, f *failure) {
	target, at := quotedPath(linkedFolder(link)), quotedPath(link)
	reason = "a symlink to " + target + ". To adopt it, move that folder to " + at
	return reason, refuse(exitRefused, sanitised(name)+" is a symlink to "+target+", and agentx adopts folders only",
		"move "+target+" to "+at+", then run '"+adoptCommand(name)+"' again").listed(reason)
}

// asItIsRefusal is why the skill of rec, whose record names nowhere agentx
// can install it from, cannot be adopted with its files as they are: nil
// when this machine holds a folder of it to adopt, the library's, or,
// where the library holds nothing under the name, one in an agent client's
// own skills directory. reason is the refusal as a row of a preview says
// it, without the skill's name; the failure is how a run asked to adopt it
// answers. managed says an import branch holds the name, whose skill the
// library no longer holds as a folder. It reads the machine and writes
// nothing, as the preview that asks does; what else can refuse the
// adoption, see checkForking, is found when it runs.
func (inv *invocation) asItIsRefusal(rec record, managed bool) (reason string, f *failure) {
	name, libPath := rec.name, inv.libraryPath(rec.name)
	shown, again := sanitised(name), "run '"+adoptCommand(rec.name)+"' again"
	refused := func(reason, message, hint string) (string, *failure) {
		return reason, refuse(exitRefused, message, hint).listed(reason)
	}
	if managed {
		remove := "'" + skillCommand("remove", name) + "'"
		return refused("agentx already manages a skill of this name, whose folder "+quotedPath(libPath)+" is missing. To adopt this one, first run "+remove,
			shown+" cannot be adopted: agentx already manages a skill of this name, whose folder "+quotedPath(libPath)+" is missing",
			"run "+remove+", then "+again)
	}
	dir := libPath
	switch info, err := os.Lstat(libPath); {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		return symlinkRefusal(name, libPath)
	case err == nil && !info.IsDir():
		return refused("a file, and a skill is a folder. To adopt it, put the skill's folder at "+quotedPath(libPath),
			shown+" is a file, and agentx adopts folders only",
			"put the skill's folder at "+quotedPath(libPath)+", then "+again)
	case err == nil && !holdsSkillFile(libPath):
		return refused("a folder with no SKILL.md file. To adopt it, add a SKILL.md to "+quotedPath(libPath),
			shown+" has no SKILL.md file, so it is not a skill",
			"add a SKILL.md to "+quotedPath(libPath)+", then "+again)
	case err != nil:
		dirs, links := inv.adoptSites(name)
		switch {
		case len(dirs) > 0:
			dir = dirs[0].path
		case len(links) > 0:
			return symlinkRefusal(name, links[0])
		case rec.tracked:
			return refused("a whole repository that Skillshare tracks. Add it with 'agentx source add <url>', then install its skills",
				shown+" is a repository Skillshare tracks as a whole, with skills inside it",
				"add the repository with 'agentx source add <url>' and install its skills with 'agentx skill add'")
		default:
			return refused("only in "+quotedPath(rec.dir)+", where no agent looks for skills. To adopt it, move that folder to "+quotedPath(libPath),
				shown+" is only in "+quotedPath(rec.dir)+", where no agent looks for skills",
				"move "+quotedPath(rec.dir)+" to "+quotedPath(libPath)+", then "+again)
		}
	}
	if forkNameRefusal(name) != "" {
		return invalidNameRow(dir), invalidNameRefusal(name, dir)
	}
	if own := ownRepo(dir); own != "" {
		return ownRepoRow(own), ownRepoRefusal(name, own)
	}
	return "", nil
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
			if err := inv.findAdoptCopies(fk, name); err != nil {
				return forkSource{}, err
			}
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
			hint := "rename the folder and adopt it under the new name, or "
			switch i := slices.IndexFunc(dirs[0].targets, func(t placeTarget) bool { return !t.readsLibrary }); {
			case i < 0:
				hint += "remove it"
			case own:
				hint += "remove it and place yours with '" + skillCommand("place", name, "--to", dirs[0].targets[i].id) + "'"
			default:
				hint += "replace it with '" + skillCommand("place", name, "--to", dirs[0].targets[i].id, "--force") + "', which discards what it holds"
			}
			return forkSource{}, refuse(exitRefused, fmt.Sprintf("the library already holds a skill called %s, %s, so %s cannot be adopted under that name", shown, what, quotedPath(dirs[0].path)), hint).
				listed(managedNameRow(dirs[0].path))
		}
		message, row := shown+" is already managed by agentx", "already managed by agentx"
		if own {
			message, row = shown+" is already one of your own skills", "already one of your own skills"
		}
		return forkSource{}, refuse(exitRefused, message, "run 'agentx skill list' to see it").listed(row + ". To see it, run 'agentx skill list'")
	}
	dirs, links := inv.adoptSites(name)
	switch {
	case len(dirs) > 0:
		fk.from, fk.places = dirs[0].path, dirs
		return forkSource{kind: lineage.KindUnmanaged, name: name, dir: dirs[0].path}, nil
	case len(links) > 0:
		target, _ := os.Readlink(links[0])
		return forkSource{}, refuse(exitRefused, fmt.Sprintf("%s is a symlink to %s, so there is no folder of %s to adopt", quotedPath(links[0]), quotedPath(target), shown),
			"replace the link with the folder it points to, then run '"+adoptCommand(name)+"' again").
			listed("a symlink to " + quotedPath(target) + ". To adopt it, replace " + quotedPath(links[0]) + " with the folder it points to")
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
	rename := "rename the folder and adopt it under the new name"
	rec, taken := nameTaken(records, fk.target, "")
	switch {
	case !taken || len(fk.places) == 0:
		return rename
	case rec.Kind == lineage.KindFork:
		return rename + ", or put your skill back with '" + skillCommand("place", rec.Name) + "'"
	}
	return rename + ", or stop managing the other with '" + skillCommand("remove", rec.Name) + "'"
}

// adoptTaken refuses an adoption whose name the account repo already
// holds a branch of, whatever its case, see nameTaken; nil when it holds
// none. It is takenRefusal in an adoption's words: a branch is no word of
// the user's, and the name is all there is to change.
func (fk *forking) adoptTaken(records map[string]lineage.Record) error {
	rec, taken := nameTaken(records, fk.target, fk.except())
	if !taken {
		return nil
	}
	message := "agentx already manages a skill called " + sanitised(rec.Name)
	if rec.Name != fk.target {
		message += ", whose name differs from " + sanitised(fk.target) + " only by case"
	}
	message += ", so " + quotedPath(fk.from) + " cannot be adopted under that name"
	return refuse(exitRefused, message, fk.adoptTakenHint(records)).listed(managedNameRow(fk.from))
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
	return refuse(exitRefused, "you already published a skill called "+sanitised(fk.target)+", so "+quotedPath(fk.from)+" cannot be adopted under that name",
		"install the published one with '"+accountAddCommand(fk.target)+"' once "+quotedPath(fk.from)+" is moved aside, or rename the folder and adopt it under the new name").
		listed(quotedPath(fk.from) + " has the name of a skill you already published. To adopt it, rename that folder")
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
		return refuse(exitRefused, quotedPath(libPath)+" is in the way: adopting "+sanitised(fk.src.name)+" puts the skill there",
			"move "+quotedPath(libPath)+" somewhere else, then "+fk.again()).
			listed(quotedPath(libPath) + " is in the way. To adopt it, move " + quotedPath(libPath) + " somewhere else")
	}
	for _, p := range fk.places {
		dir := filepath.Dir(p.path)
		if err := unix.Access(dir, unix.W_OK|unix.X_OK); err != nil {
			return refuse(exitRefused, sanitised(fk.src.name)+" cannot be adopted: agentx cannot write to "+quotedPath(dir)+" ("+err.Error()+")",
				"make "+quotedPath(dir)+" writable, then "+fk.again()).
				listed("in " + quotedPath(dir) + ", which agentx cannot write to. To adopt it, make " + quotedPath(dir) + " writable")
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
	for i, p := range fk.places {
		read, err := readAdoptPlace(p)
		if err != nil {
			return libraryFailure(filepath.Dir(p.path), err)
		}
		fk.places[i] = read
	}
	if len(fk.places) == 0 || !adoptPlacesDiffer(fk.places) {
		return nil
	}
	found := make([]string, len(fk.places))
	for i, p := range fk.places {
		found[i] = quotedPath(p.path) + " (" + strings.Join(p.ids(), ", ") + ")"
	}
	return refuse(exitRefused, sanitised(fk.src.name)+" is in several agents' skills folders, with different files: "+strings.Join(found, ", "),
		"keep one of them, remove or rename the others, then "+fk.again()).
		listed("in several agents' skills folders, with different files: " + strings.Join(found, ", ") + ". To adopt it, keep one and remove or rename the others")
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

// findAdoptCopies finds, for the adoption of the library entry called
// name, the real directories the detected configurations' own skills
// directories hold under that name, see adoptSites: what a client copied
// there, or a tool that installed the skill into the library and into
// that client's directory. One is a candidate copy, see readAdoptCopies,
// where every configuration it belongs to places symlinks and agentx can
// write its skills directory; any other is kept as it is.
func (inv *invocation) findAdoptCopies(fk *forking, name string) error {
	dirs, _ := inv.adoptSites(name)
	if len(dirs) == 0 {
		return nil
	}
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	for _, p := range dirs {
		p.targets = withCopies(p.targets, s.CopyConfigurations)
		copies := slices.ContainsFunc(p.targets, func(t placeTarget) bool { return t.copies })
		if copies || unix.Access(filepath.Dir(p.path), unix.W_OK|unix.X_OK) != nil {
			fk.kept = append(fk.kept, p)
			continue
		}
		fk.copies = append(fk.copies, p)
	}
	return nil
}

// readAdoptCopies reads what the library entry an adoption takes holds,
// and what each of its candidate copies holds, see findAdoptCopies, as
// readAdoptPlaces reads a directory: a copy that holds the same files
// stays one, to be replaced by the library's symlink, see copyOf, and any
// other is kept as it is. A copy that cannot be read is kept too: the adoption of
// the library entry never needed it.
func (inv *invocation) readAdoptCopies(fk *forking) error {
	if len(fk.copies) == 0 {
		return nil
	}
	lib, err := readAdoptPlace(adoptPlace{path: fk.from})
	if err != nil {
		return libraryFailure(filepath.Dir(fk.from), err)
	}
	fk.copiesOf = lib.state
	ignoreSystem := inv.systemFilesIgnored()
	same := fk.copies[:0]
	for _, p := range fk.copies {
		if p, err := readAdoptPlace(p); err == nil && p.copyOf(lib, ignoreSystem) {
			same = append(same, p)
			continue
		}
		fk.kept = append(fk.kept, p)
	}
	fk.copies = same
	return nil
}

// copyOf reports whether p holds what lib holds, as same does, or, while
// git ignores system files, all but the system files either holds, such as
// the .DS_Store Finder leaves in a folder it showed. Pure.
func (p adoptPlace) copyOf(lib adoptPlace, ignoreSystem bool) bool {
	return p.same(lib) || ignoreSystem && p.whole && lib.whole && p.bare != "" && p.bare == lib.bare
}

// readAdoptPlace is p with what its directory holds read: its state, its
// tree id and whether git can record all of it.
func readAdoptPlace(p adoptPlace) (adoptPlace, error) {
	state, err := home.State(p.path)
	if err != nil {
		return p, err
	}
	tree, err := treeid.Read(p.path)
	if err != nil {
		return p, err
	}
	p.state, p.tree, p.whole = state, tree.ID, len(tree.Unrecordable) == 0
	if !tree.Has(".gitignore") {
		p.bare = tree.Without(home.IsSystemFile)
	}
	return p, nil
}

// adoptCopiesHold reports, under the lock, whether every copy of an
// adoption still holds what it held when it was compared with the library
// entry.
func adoptCopiesHold(fk *forking) bool {
	for _, p := range fk.copies {
		if state, err := home.State(p.path); err != nil || state != p.state {
			return false
		}
	}
	return true
}

// stageAdoptCopies plans, into m, what becomes of the copies of an
// adoption of a library entry, see readAdoptCopies: each is removed,
// retained until the mutation is verified, and gets the library's symlink
// where a placement belongs, see adoptPlace.linked. done records the
// configurations that see the skill there.
func stageAdoptCopies(m *home.Mutation, fk *forking, libPath string, done *placements) {
	for _, p := range fk.copies {
		m.Remove(p.path, p.state)
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
	for _, p := range fk.kept {
		out.warnWith(quotedPath(p.path)+" was left as it is: agentx could not tell it is a copy of "+name,
			"to replace it with the adopted skill, run '"+skillCommand("place", fk.target, "--to", p.targets[0].id, "--force")+"'")
	}
	if len(fk.places) == 0 && len(fk.copies) == 0 {
		inv.summary = "adopted " + name
		out.done("adopted " + out.paint(heading, name))
		inv.noAccountRemoteHint(ev)
		return nil
	}
	_, links := inv.adoptSites(fk.target)
	for _, link := range links {
		if !sameTarget(link, lib.Path) {
			out.warn(quotedPath(link) + " is a link of your own and was left as it is")
		}
	}
	from := ""
	if len(fk.places) > 0 {
		from = " from " + quotedPath(fk.from)
	}
	rows := inv.ownPlacements(fk.target, done.placed, filterPlacements(ev.Placements, targetIDs(done.placed)))
	out.done("adopted " + out.paint(heading, name) + from + ": " + out.paint(noteStyle, plural(len(rows), "placement")))
	inv.printPlacementRows(fk.target, rows)
	inv.printUniversal(ev.Universal)
	inv.noAccountRemoteHint(ev)
	inv.summary = "adopted " + name + from + " in " + plural(len(done.placed), "configuration") + universalClause(ev.Universal)
	return nil
}
