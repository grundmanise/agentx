package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

func newSkillPlaceCommand(inv *invocation) *cobra.Command {
	var to []string
	var asCopy bool
	cmd := &cobra.Command{
		Use:   "place <name>",
		Short: "Place a skill the library already holds into a configuration",
		Long: "Place a skill the library already holds into one or more agent configurations.\n" +
			"Name each configuration with --to. This is the placement half of 'agentx skill\n" +
			"add' on its own, for a skill that is in the library but not yet in a client.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillPlace(cmd.Context(), args[0], to, asCopy)
		},
	}
	cmd.Flags().StringArrayVar(&to, "to", nil, "the configuration to place the skill in; give it again for each")
	cmd.Flags().BoolVar(&asCopy, "copy", false, "place a copy instead of a symlink")
	return cmd
}

// placeTarget is one configuration a placement is made in.
type placeTarget struct {
	id           string
	dir          string // the client's own skills directory
	readsLibrary bool   // the library entry is the placement; a symlink would list the skill twice
}

// ownPlace is where this configuration's placement of the skill called name
// is: the library entry for a client that reads the library, which is its
// placement, and the skill's directory in the client's own skills directory
// for any other. A path the client sees the skill through in another
// client's skills directory is that other client's placement, not this one.
func (t placeTarget) ownPlace(library, name string) string {
	if t.readsLibrary {
		return filepath.Join(library, name)
	}
	return filepath.Join(t.dir, name)
}

// placements is what a command did about one skill's placements, for the
// report that follows the mutation.
type placements struct {
	placed    []placeTarget // the configurations that now see the skill
	copies    []string      // the configurations that hold a copy
	adoptions []string      // the placement paths that were a directory of this version
	skipped   []string      // the placement paths the run left without a placement
}

// placeable is what a placement is made of: the name the skill is placed
// under, the content hash of the version behind it, and how to lay that
// version out again when the placement is a copy. An install stages a copy
// from the files it imported, since the library directory is published by
// the same mutation and is not there yet; placing later copies the library
// directory itself.
type placeable struct {
	name  string
	hash  string
	stage func(dest string) error
}

// detectedTargets is every detected configuration a placement can be made
// in, whatever its enabled state, sorted by id as detection is.
func (inv *invocation) detectedTargets() []placeTarget {
	var targets []placeTarget
	for _, c := range scan.Detect(inv.dirs) {
		t := placeTarget{id: c.Slug(), dir: scan.PlacementDir(c, inv.dirs), readsLibrary: scan.ReadsLibrary(c, inv.dirs)}
		if t.dir == "" && !t.readsLibrary {
			continue // a client the registry gives no skills directory has nowhere to place into
		}
		targets = append(targets, t)
	}
	return targets
}

// placementTargets are the configurations a command places into: every
// enabled one, or exactly those named, whatever their enabled state, since
// naming one is asking for it.
func (inv *invocation) placementTargets(to []string) ([]placeTarget, error) {
	detected := inv.detectedTargets()
	if len(to) == 0 {
		s, err := inv.loadSettings()
		if err != nil {
			return nil, err
		}
		var targets []placeTarget
		for _, t := range detected {
			if !containsString(s.DisabledConfigurations, t.id) {
				targets = append(targets, t)
			}
		}
		return targets, nil
	}
	var targets []placeTarget
	for _, t := range detected {
		if containsString(to, t.id) {
			targets = append(targets, t)
		}
	}
	for _, id := range to {
		if !hasTarget(targets, id) {
			if err := inv.detectedConfiguration(id); err != nil {
				return nil, err
			}
			// Detected, but the registry gives its client no skills directory,
			// so there is nowhere to put a placement. Saying so is better than
			// reporting a placement the command did not make.
			return nil, fail(exitRefused, "configuration "+id+" has no skills directory to place into",
				"place the skill in another configuration")
		}
	}
	return targets, nil
}

func hasTarget(targets []placeTarget, id string) bool {
	for _, t := range targets {
		if t.id == id {
			return true
		}
	}
	return false
}

// targetIDs are the configuration ids of the targets, for filtering what a
// rescan found down to what the command covered.
func targetIDs(targets []placeTarget) []string {
	ids := make([]string, 0, len(targets))
	for _, t := range targets {
		ids = append(ids, t.id)
	}
	return ids
}

// skillPlace places a skill the library already holds into the named
// configurations. It is the placement half of an install on its own: the
// same decisions, the same journal steps and the same refusals, so a
// placement made later is the placement an install would have made.
func (inv *invocation) skillPlace(ctx context.Context, name string, to []string, asCopy bool) error {
	if len(to) == 0 {
		return fail(exitUsage, "skill place needs --to", "name the configuration to place the skill in with --to, once for each")
	}
	if _, ok := librarySkill(inv.dirs.Library, name); !ok {
		return inv.noLibrarySkill(name)
	}
	targets, err := inv.placementTargets(to)
	if err != nil {
		return err
	}
	var done placements
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		// Every input is read again under the lock: the library may have
		// changed between the check above and this mutation.
		lib, ok := librarySkill(inv.dirs.Library, name)
		if !ok {
			return inv.noLibrarySkill(name)
		}
		for _, t := range targets {
			if !t.readsLibrary {
				sweepStaged(t.dir)
			}
		}
		m := home.NewMutation(inv.dirs.Home)
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		done = placements{}
		p := libraryPlaceable(lib)
		for _, t := range targets {
			// A placement this machine cannot make is skipped and counted,
			// not a failure of the run: stagePlacement decides that itself.
			inv.stagePlacement(m, p, t, lib.Path, asCopy, edit.copiesOf(name), &done)
		}
		edit.addCopies(name, done.copies)
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return mutationFailure(err)
	}
	return inv.reportPlaced(ctx, name, targets, done)
}

// libraryPlaceable is the version the library holds now, for a placement
// made after the install: a copy is laid out from the library directory
// itself and validated by its content hash before it is published.
func libraryPlaceable(lib scan.LibrarySkill) placeable {
	return placeable{
		name: lib.Name,
		hash: lib.ContentHash,
		stage: func(dest string) error {
			return copyTreeTo(lib.ResolvedPath, dest)
		},
	}
}

// noLibrarySkill is the answer to a command naming a skill the library does
// not hold. A directory that is not a skill, one with no SKILL.md, is no
// skill of the library either and is left where it is.
func (inv *invocation) noLibrarySkill(name string) error {
	return fail(exitNotFound, fmt.Sprintf("the library holds no skill called %q at %s", name, inv.dirs.Library),
		"run 'agentx skill list' to see what the library holds")
}

// stagePlacement plans one configuration's placement. A configuration whose
// client reads the library needs none: the library entry is the placement,
// and a second entry would make that client list the skill twice.
//
// A configuration recordedCopies names, the ones copy_mode records for the
// skill, gets a copy whether or not this command was asked for one: its
// client may not follow a symlink, and turning agentx's own copy into a
// link would leave copy_mode naming a copy that is not there, which a
// removal would then trust about whatever the user put at that path. A copy
// there that does not hold this version is kept and skipped, see keepCopy.
//
// A placement this machine cannot make — a skills directory owned by
// somebody else, one on a read-only mount, one macOS has not granted
// access to, a path that is a file rather than a directory — is skipped
// with a warning and counted in the result, exactly as a path something
// else holds is. One client agentx cannot reach is not a reason to leave
// the library, the branch and every other placement undone, and a run that
// aborted here would leave a journal no later command could finish.
func (inv *invocation) stagePlacement(m *home.Mutation, p placeable, t placeTarget, libPath string, asCopy bool, recordedCopies []string, done *placements) {
	if t.readsLibrary {
		done.placed = append(done.placed, t)
		return
	}
	recorded := containsString(recordedCopies, t.id)
	asCopy = asCopy || recorded
	placePath := t.ownPlace(inv.dirs.Library, p.name)
	state, err := home.State(placePath)
	if err != nil {
		inv.skipPlacement(done, t, placePath, err)
		return
	}
	var displace, adopt bool
	switch {
	case home.IsAbsent(state):
	case sameTarget(placePath, libPath):
		// A symlink naming the library directory, dangling until this
		// mutation publishes it or not: the placement is already what it
		// should be, unless copies were asked for, and a link holds
		// nothing to keep.
		if !asCopy {
			done.placed = append(done.placed, t)
			return
		}
		displace = true
	case home.IsDir(state) && contentHashAt(placePath) == p.hash:
		// A real directory holding exactly this version: nothing of the
		// user's is lost by replacing it, and with --copy it is the copy.
		// A symlink somewhere else is never adopted however it reads: it
		// is the user's link to the user's directory, and agentx unlinking
		// it would decide for them where their skill lives.
		//
		// Either way the directory becomes agentx's to delete, so either
		// way it is an adoption and is reported as one. With --copy nothing
		// is written and only copy_mode changes, which is exactly why the
		// run has to say so: the user would otherwise never learn that a
		// directory they made by hand is now one a removal takes away. A
		// copy copy_mode already records is agentx's own and no adoption.
		if asCopy {
			if !recorded {
				done.adoptions = append(done.adoptions, placePath)
			}
			done.placed = append(done.placed, t)
			done.copies = append(done.copies, t.id)
			return
		}
		displace, adopt = true, true
	case recorded && home.IsDir(state):
		// agentx's own copy, which does not hold this version: it is
		// kept, since replacing it would take what it holds with it.
		inv.keepCopy(done, t, placePath, p.name)
		return
	default:
		// A link is refused for being a link and not for where it leads: it
		// may well resolve to this very version, and saying it is not this
		// skill would be untrue and would send the user looking at the
		// wrong thing. Anything else is refused for what it holds.
		what := " is not this skill and was left as it is"
		if home.IsLink(state) {
			what = " is a link of your own and was left as it is"
		}
		done.skipped = append(done.skipped, placePath)
		inv.out.warn(placePath + what + "; no placement was made for " + t.id)
		return
	}
	// Everything that can fail runs before the first step of this placement
	// is recorded, so that a placement that cannot be made leaves neither a
	// step in the journal nor content on disk.
	staged, fingerprint, err := inv.placementContent(m, p, t, placePath, asCopy)
	if err != nil {
		inv.skipPlacement(done, t, placePath, err)
		return
	}
	if displace {
		m.Remove(placePath, state)
		if adopt {
			done.adoptions = append(done.adoptions, placePath)
		}
	}
	if asCopy {
		m.Publish(placePath, staged, fingerprint)
		done.copies = append(done.copies, t.id)
	} else {
		m.Link(placePath, libPath)
	}
	done.placed = append(done.placed, t)
}

// placementContent prepares on disk what one placement needs: a directory
// to hold it, which the run creates when it is missing, and for --copy the
// staged copy itself, read back the way a scan reads it. The directory is
// checked for being writable rather than only for existing, since the
// symlink a placement usually is writes nothing until the journal runs it,
// and a directory agentx cannot write would otherwise be found out only
// then, with the journal already on disk.
func (inv *invocation) placementContent(m *home.Mutation, p placeable, t placeTarget, placePath string, asCopy bool) (staged, fingerprint string, err error) {
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return "", "", err
	}
	if err := unix.Access(t.dir, unix.W_OK|unix.X_OK); err != nil {
		return "", "", &fs.PathError{Op: "access", Path: t.dir, Err: err}
	}
	if !asCopy {
		return "", "", nil
	}
	staged = m.Sibling(placePath, "staged")
	if err := inv.stageCopy(staged, p); err != nil {
		os.RemoveAll(staged)
		return "", "", err
	}
	if fingerprint, err = home.Fingerprint(staged); err != nil {
		os.RemoveAll(staged)
		return "", "", err
	}
	return staged, fingerprint, nil
}

// keepCopy skips a copy copy_mode records for the configuration whose
// content is not the version being placed, and says whose copy it is and
// how to replace it with the library version: remove the copy, which a
// removal deletes as agentx's own, and place a copy again, since copy_mode
// recorded one for a client that may not follow a symlink.
//
// It says the copy is different from the library and never why. Nothing
// records what a copy held when it was placed, so an edit of the copy, a
// change to the library directory since and a copy_mode entry imported
// from another machine all look the same from here.
//
// The two commands are one line under the warning, joined by && so they can
// be pasted as they are. A log event carries no hint, so in JSON mode that
// line is part of the message, where a script finds it as a person reading
// the text does.
func (inv *invocation) keepCopy(done *placements, t placeTarget, placePath, name string) {
	done.skipped = append(done.skipped, placePath)
	inv.out.warnWith(t.id+"'s copy of "+name+" is different from the library, so it was left unchanged ("+placePath+")",
		"to replace it with the library version: "+skillCommand("remove", name, "--from", t.id)+" && "+skillCommand("place", name, "--to", t.id, "--copy"))
}

// skillCommand is the agentx skill command a line tells the user to run on
// the skill called name, the name quoted for a shell. A name that starts
// with a dash comes last, after the flags and "--", since agentx would
// otherwise read it as a flag of its own and refuse the command.
func skillCommand(verb, name string, flags ...string) string {
	if strings.HasPrefix(name, "-") {
		return "agentx skill " + verb + " " + strings.Join(flags, " ") + " -- " + shellWord(name)
	}
	return "agentx skill " + verb + " " + shellWord(name) + " " + strings.Join(flags, " ")
}

// skipPlacement leaves one configuration without a placement and says why,
// counting it where the contract counts a placement path something else
// holds: the run still succeeds and the result says how many were skipped.
func (inv *invocation) skipPlacement(done *placements, t placeTarget, placePath string, err error) {
	done.skipped = append(done.skipped, placePath)
	inv.out.warn("cannot place " + placePath + ": " + err.Error() + "; no placement was made for " + t.id)
}

// stageCopy lays a version out at a hidden directory beside the live path it
// will become, then reads it back the way a scan does: a directory whose
// content hash is not the version's never gets published.
func (inv *invocation) stageCopy(staged string, p placeable) error {
	if err := os.MkdirAll(staged, 0o755); err != nil {
		return err
	}
	if err := p.stage(staged); err != nil {
		return err
	}
	if err := home.SyncTree(staged); err != nil {
		return err
	}
	if hash, _ := scan.ContentHashAt(staged); hash != p.hash {
		return fmt.Errorf("the staged copy of %s hashes to %s, not %s", p.name, hash, p.hash)
	}
	return nil
}

// copyTreeTo lays the directory at src out again under dest, entry for
// entry: directories as directories, regular files with their bytes and
// their executable bit, and symlinks as the same symlinks rather than as
// what they point at. The copy is checked against the version's content
// hash before it is published, so a link that meant something else in its
// old place stops the placement instead of being placed wrong.
func copyTreeTo(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if info, err := d.Info(); err == nil && info.Mode()&0o111 != 0 {
				mode = 0o755
			}
			return writeSynced(target, b, mode)
		}
		return nil // a socket or a device is no part of a skill
	})
}

// sameTarget reports whether the symlink at path names the library
// directory, whether or not it resolves: a placement made before the
// library held the skill dangles until an install publishes it, and is
// still the placement that install would make. A relative link is read
// against the directory it sits in, so a placement written that way is
// recognised as the placement it is.
//
// Only what the link names counts, never where following it ends up. A link
// of the user's that happens to resolve into the library is theirs: the
// contract leaves "a symlink of theirs pointing somewhere else however it
// resolves" alone, and this predicate is what a removal deletes by. Judging
// a link by its resolved target would make the user's own link
// indistinguishable from one agentx wrote, and delete it without a word.
func sameTarget(path, libPath string) bool {
	link, err := os.Readlink(path)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(link) {
		link = filepath.Join(filepath.Dir(path), link)
	}
	return filepath.Clean(link) == filepath.Clean(libPath)
}

// reportPlaced reads the configurations this command covered again and
// reports the skill as the machine now has it, the way an install does: the
// placements in the event are what the rescan found, not what the command
// meant to make.
func (inv *invocation) reportPlaced(ctx context.Context, name string, targets []placeTarget, done placements) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	lib, ok := librarySkill(inv.dirs.Library, name)
	if !ok {
		return fail(exitInternal, "the library holds no "+name+" after placing it", "run 'agentx doctor' and check the library it names")
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	ev := sc.librarySkillEventFor(inv, snap, lib, targetIDs(targets))
	inv.out.emit(ev)
	inv.printPlaced(lib, targets, done, ev)
	inv.summary = placeSummary(name, done) + universalClause(ev.Universal)
	return nil
}

// skillContext is what a report about library skills reads once: the
// lineage branches of the account repo, and the copy modes and the sources
// of the settings. Reading them per skill would cost a git process per
// skill, which a run over the whole library may not.
type skillContext struct {
	records map[string]lineage.Record
	modes   map[string][]string
	sources map[string]bool // the canonical URL of every source the settings hold
}

func (inv *invocation) skillContext(ctx context.Context) (skillContext, error) {
	records, err := inv.lineageRecords(ctx)
	if err != nil {
		return skillContext{}, err
	}
	s, err := inv.loadSettings()
	if err != nil {
		return skillContext{}, err
	}
	modes, err := inv.copyModes(s)
	if err != nil {
		return skillContext{}, err
	}
	return skillContext{records: records, modes: modes, sources: sourceURLs(s)}, nil
}

// librarySkillEventFor builds the library_skill event of one library directory from the
// lineage the account repo holds and the placements the rescan found in the
// configurations the command covered. A nil covered reports every placement,
// which is what a listing of the whole library does. The universal clients
// are every one the rescan detected, covered or not: they see the skill
// through the library whatever the command covered.
func (sc skillContext) librarySkillEventFor(inv *invocation, snap scan.Snapshot, lib scan.LibrarySkill, covered []string) librarySkillEvent {
	places := inv.placements(snap, lib, sc.modes)
	if covered != nil {
		places = filterPlacements(places, covered)
	}
	rec, managed := sc.records[lib.Name]
	return skillFromLibrary(lib, rec, managed, sc.sources, places, universalClients(snap))
}

// lineageRecords are the branches of the account repo by skill name, empty
// when this machine has no account repo yet: a listing never creates one.
// Either failure names the account repo, as the check of it does, since
// what git says of refs it cannot read need not, and the hint and a scan's
// warning both send the reader to that repo.
func (inv *invocation) lineageRecords(ctx context.Context) (map[string]lineage.Record, error) {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	if !exists {
		return map[string]lineage.Record{}, nil
	}
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		return nil, accountRepoFailure(fmt.Errorf("account repo %s: %w", gitDir, err))
	}
	return records, nil
}

// filterPlacements keeps the placements of the configurations the command
// covered, which is what a targeted rescan reports on; the library entry of
// a client that reads the library is one of them.
func filterPlacements(places []placementEvent, covered []string) []placementEvent {
	kept := []placementEvent{}
	for _, p := range places {
		if containsString(covered, p.Configuration) {
			kept = append(kept, p)
		}
	}
	return kept
}

// placeSummary is what the result event says a placement run did.
func placeSummary(name string, done placements) string {
	summary := fmt.Sprintf("placed %s in %s", name, plural(len(done.placed), "configuration"))
	for _, what := range []struct {
		n    int
		text string
	}{{len(done.copies), "as " + modeCopy}, {len(done.adoptions), "adopted"}, {len(done.skipped), "skipped"}} {
		if what.n > 0 {
			summary += fmt.Sprintf(", %s %s", plural(what.n, "placement"), what.text)
		}
	}
	return summary
}

// printPlaced writes the confirmation of one placement run and one row per
// configuration it covered, as ownPlacements picks them out of what the
// rescan found. The name is a library directory's, which whoever made it
// chose, so it is sanitised as skill list prints it.
func (inv *invocation) printPlaced(lib scan.LibrarySkill, targets []placeTarget, done placements, ev librarySkillEvent) {
	out := inv.out
	// The configurations placed into, not the rows: a configuration whose
	// placement was skipped is not counted, yet still has a row when it sees
	// the skill through another client's skills directory, as Cursor reads
	// Claude Code's.
	line := "placed " + out.paint(heading, sanitised(lib.Name)) + " in " + out.paint(noteStyle, plural(len(done.placed), "configuration"))
	if n := len(done.skipped); n > 0 {
		line += ", " + out.paint(warnStyle, plural(n, "placement")+" skipped")
	}
	out.done(line)
	inv.printPlacementRows(lib.Name, inv.ownPlacements(lib.Name, targets, ev.Placements))
	inv.printAdoptions(done.adoptions)
	inv.printUniversal(ev.Universal)
}

// availableToUniversal is how a command that placed a skill names the
// universal clients: every one detected, whether or not the command placed
// into it, since it sees the skill through the library whatever the command
// was told. "Always" is the point of the line: neither --to nor a disabled
// configuration keeps the skill from a client that reads the library. The
// list comes last, so the commas that separate the ids cannot be read as
// the ones that separate the clauses of a summary.
const availableToUniversal = "always available to universal clients: "

// printUniversal writes the line that names the universal clients, when the
// machine has any. They are named even when the rows above show one of
// them, so the line always answers the same question: who has this skill
// whatever the placements.
func (inv *invocation) printUniversal(ids []string) {
	if len(ids) == 0 {
		return
	}
	inv.out.print("  ", inv.out.paint(muted, availableToUniversal+strings.Join(ids, ", ")))
}

// universalClause is what the result event of a command that placed a
// skill adds about the universal clients, the same words the text output
// ends with, and nothing when the machine has none.
func universalClause(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return "; " + availableToUniversal + strings.Join(ids, ", ")
}

// printAdoptions writes the line that names the directories a run adopted
// as placements, when it adopted any. Each path carries the skill's name,
// so it is quoted as the rows above it are.
func (inv *invocation) printAdoptions(adoptions []string) {
	if len(adoptions) == 0 {
		return
	}
	paths := make([]string, len(adoptions))
	for i, p := range adoptions {
		paths[i] = quotedPath(p)
	}
	inv.out.print("  ", inv.out.paint(muted, "adopted "+strings.Join(paths, ", ")))
}

// ownPlacements are the placements the text of a command that placed a
// skill shows: one per configuration it covered, the one at that
// configuration's own place. The rescan finds more than that. A client that
// reads another client's skills directory, as Cursor reads Claude Code's,
// sees the skill through that client's placement as well as its own, and
// printing both under it would give one client two rows for one placement.
// That path is the other client's placement and is shown as that client's
// row. The library_skill event is left as the rescan found it, every path
// included, for whoever wants each way a client sees the skill.
//
// A configuration with nothing of the skill at its own place keeps every
// row the rescan found for it, so that a client that sees the skill is
// never left without one. skill place and config enable --place-all report
// on every configuration they were asked for, one whose placement was
// skipped included, and that one can still see the skill through another
// client's skills directory: its row says so, below the warning that says
// why nothing was placed. A copy keepCopy left is one of those: it does not
// hold the library's version, so the rescan does not find it as the skill,
// and nothing of the skill is at that configuration's own place. An
// install reports only on the configurations it
// placed into, each of which the rescan finds holding the placement at its
// own place, so there this is no more than a safety net for a path that
// something changed between the mutation and the rescan.
func (inv *invocation) ownPlacements(name string, targets []placeTarget, places []placementEvent) []placementEvent {
	own := make(map[string]string, len(targets))
	for _, t := range targets {
		own[t.id] = t.ownPlace(inv.dirs.Library, name)
	}
	atOwn := map[string]bool{} // the configurations the rescan found at their own place
	for _, p := range places {
		if p.Path == own[p.Configuration] {
			atOwn[p.Configuration] = true
		}
	}
	rows := []placementEvent{}
	for _, p := range places {
		if !atOwn[p.Configuration] || p.Path == own[p.Configuration] {
			rows = append(rows, p)
		}
	}
	return rows
}

// printPlacementRows writes one indented row per placement, a symlink
// ending in the library directory it points at. Both paths carry the
// skill's name, so they are quoted the way config enable --place-all and
// scan quote a placement's path.
func (inv *invocation) printPlacementRows(name string, places []placementEvent) {
	t := &table{}
	for _, p := range places {
		path := quotedPath(p.Path)
		if p.Kind == modeSymlink {
			path += inv.out.paint(muted, " -> "+quotedPath(inv.libraryPath(name)))
		}
		t.add(c("  "+p.Configuration, label), c(p.Mode, muted), c(path, plain))
	}
	inv.out.render(t, "")
}
