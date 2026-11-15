package cli

import (
	"context"
	"fmt"
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// phasePlace is the step config enable --place-all reports per skill, plus
// the rescan every command of this kind ends with.
const phasePlace = "place"

// enableAndPlaceAll is `agentx config enable <id> --place-all`: the
// settings change and every placement it implies are one journaled
// mutation, because they are one thing the user asked for and an
// interrupted run must not leave a configuration enabled with half the
// library in it.
//
// A skill whose placement path holds something agentx did not put there is
// skipped with a warning and counted, exactly as it is during an install:
// what is refused here is a placement, not a skill, and a placement left
// alone has never failed a run.
func (inv *invocation) enableAndPlaceAll(ctx context.Context, id string) error {
	targets, err := inv.placementTargets([]string{id})
	if err != nil {
		return err
	}
	var s home.Settings
	var done placements
	var names []string
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		skills, warnings := readLibrary(inv.dirs.Library)
		for _, w := range warnings {
			inv.out.warn(w)
		}
		for _, t := range targets {
			if !t.readsLibrary {
				sweepStaged(t.dir)
			}
		}
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		edit.enable(id)
		m := home.NewMutation(inv.dirs.Home)
		done, names = placements{}, nil
		for _, lib := range skills {
			names = append(names, lib.Name)
			// The copies made for this skill are recorded under its own name,
			// in the same settings write, as skill place records them: a copy
			// copy_mode does not name is no placement of agentx's, and the
			// rescan and every later command would take it for the user's.
			var one placements
			for _, t := range edit.placing(targets) {
				inv.stagePlacement(m, libraryPlaceable(lib), t, lib.Path, false, edit.copiesOf(lib.Name), &one)
			}
			edit.addCopies(lib.Name, one.copies)
			done.placed = append(done.placed, one.placed...)
			done.copies = append(done.copies, one.copies...)
			done.adoptions = append(done.adoptions, one.adoptions...)
			done.skipped = append(done.skipped, one.skipped...)
			done.discarded = append(done.discarded, one.discarded...)
		}
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
		if err := m.Apply(inv.refs(ctx)); err != nil {
			return err
		}
		s = edit.settings()
		return nil
	})
	if err != nil {
		return mutationFailure(err)
	}
	inv.emitSettings(s)
	return inv.reportPlacedAll(ctx, id, targets, names, done)
}

// addOwnRows adds one row per placement of the skill at the configuration's
// own place, its name, mode and path, as skill add and skill place print
// theirs.
func (inv *invocation) addOwnRows(t *table, name string, targets []placeTarget, places []placementEvent) {
	for _, p := range inv.ownPlacements(name, targets, places) {
		path := quotedPath(p.Path)
		if p.Kind == modeSymlink {
			path += inv.out.paint(muted, " -> "+quotedPath(inv.libraryPath(name)))
		}
		t.add(c("  "+sanitised(name), heading), c(p.Mode, muted), c(path, plain))
	}
}

// reportPlacedAll reads the configuration this command touched again and
// reports every library skill as it now sees it, one skill event each, in
// the order the library lists them.
func (inv *invocation) reportPlacedAll(ctx context.Context, id string, targets []placeTarget, names []string, done placements) error {
	// placementTargets refuses an id it cannot place into, so the run has
	// exactly the one configuration it named by the time it reports.
	readsLibrary := len(targets) > 0 && targets[0].readsLibrary
	total := len(names) + 1
	for i, name := range names {
		inv.progress(phasePlace, name, i+1, total)
	}
	inventory, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	inv.progress(phaseRescan, "", total, total)
	inv.printEnabled(id, true)
	// The account repo and the settings are read once for the whole library,
	// not once per skill: a run over thirty skills may not cost thirty git
	// processes any more than an install of thirty does.
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	skills, _ := readLibrary(inv.dirs.Library)
	placed := 0
	t := &table{}
	for _, lib := range skills {
		ev := sc.librarySkillEventFor(ctx, inv, inventory, lib, []string{id})
		inv.out.emit(ev)
		if len(ev.Placements) > 0 {
			// One skill the configuration now sees, however many ways it sees
			// it: a client that reads another client's skills directory, as
			// Cursor reads Claude Code's, has more paths than placements made.
			placed++
		}
		// One row per skill, its placement at this configuration's own
		// place, as skill add and skill place print theirs; the event above
		// carries every path the rescan found. The name is the library
		// directory's own, which whoever put it there chose, and both paths
		// carry it, so the name is sanitised and the paths quoted, the way
		// skill list and scan print them.
		inv.addOwnRows(t, lib.Name, targets, ev.Placements)
	}
	// A client that reads the library already sees every library skill, so
	// the run made no entry for it and may not say it placed anything. The
	// rows below are still what that configuration sees, each of mode
	// library, which is the answer to what enabling it got the user.
	line := "placed " + inv.out.paint(noteStyle, plural(placed, "skill")) + " in " + inv.out.paint(heading, id)
	if readsLibrary {
		line = inv.out.paint(heading, id) + " reads the library and already sees " +
			inv.out.paint(noteStyle, plural(placed, "skill")) + "; nothing was placed"
	}
	if n := len(done.skipped); n > 0 {
		line += ", " + inv.out.paint(warnStyle, plural(n, "placement")+" skipped")
	}
	inv.out.done(line)
	inv.out.render(t, "")
	inv.summary = id + " is now enabled, " + plural(placed, "skill") + " placed"
	if readsLibrary {
		inv.summary = id + " is now enabled and already sees " + plural(placed, "skill") + " through the library"
	}
	if n := len(done.skipped); n > 0 {
		inv.summary += ", " + plural(n, "placement") + " skipped"
	}
	return nil
}

// converted is what config placement --convert did about the placements
// already in the configuration: the library skills whose placement it
// turned into the mode, in library order, and the placement paths it left
// as they were and counts as skipped.
type converted struct {
	names   []string
	skipped []string
}

// setPlacementAndConvert is `agentx config placement <id> <mode>
// --convert`: the mode and the placements already made in the
// configuration are changed by one journaled mutation, for the reason
// enableAndPlaceAll gives. It places no skill that was not placed there.
//
//   - To copies, every placement that is a symlink to the library entry
//     becomes a copy recorded in copy_mode, staged as skill place stages
//     one, so a directory agentx cannot write is skipped with a warning.
//   - To symlinks, every copy copy_mode records for the configuration that
//     holds exactly the library's version becomes a symlink to the library
//     entry, and copy_mode no longer records it. A recorded copy that is
//     different from the library is kept and still recorded, since
//     replacing it would discard what it holds, and is named in a warning
//     and counted as skipped.
//
// Anything else at a place, the user's directory or link, a recorded copy
// that is gone, nothing at all, is left as it is and not counted: it is no
// placement of agentx's to convert.
func (inv *invocation) setPlacementAndConvert(ctx context.Context, id string, copies bool) error {
	targets, err := inv.placementTargets([]string{id})
	if err != nil {
		return err
	}
	var s home.Settings
	var done converted
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		skills, warnings := readLibrary(inv.dirs.Library)
		for _, w := range warnings {
			inv.out.warn(w)
		}
		for _, t := range targets {
			sweepStaged(t.dir)
		}
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		edit.setPlacement(id, copies)
		m := home.NewMutation(inv.dirs.Home)
		done = converted{}
		for _, lib := range skills {
			for _, t := range edit.placing(targets) {
				if inv.stageConversion(m, edit, lib, t, copies, &done) {
					done.names = append(done.names, lib.Name)
				}
			}
		}
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
		if err := m.Apply(inv.refs(ctx)); err != nil {
			return err
		}
		s = edit.settings()
		return nil
	})
	if err != nil {
		return mutationFailure(err)
	}
	inv.emitSettings(s)
	return inv.reportConverted(ctx, id, copies, targets, done)
}

// stageConversion plans the conversion of one skill's placement in the
// configuration t and records the copy_mode change it implies, reporting
// whether the placement is converted.
func (inv *invocation) stageConversion(m *home.Mutation, edit *settingsEdit, lib scan.LibrarySkill, t placeTarget, copies bool, done *converted) bool {
	placePath := t.ownPlace(inv.dirs.Library, lib.Name)
	if copies {
		if !sameTarget(placePath, lib.Path) {
			return false
		}
		// The symlink is replaced by a copy exactly as skill place replaces
		// it in a configuration that places copies, the library entry being
		// what the link names, a fork's too, and the copy laid out from the
		// directory it leads to.
		var one placements
		inv.stagePlacement(m, libraryPlaceable(lib), t, lib.Path, true, edit.copiesOf(lib.Name), &one)
		edit.addCopies(lib.Name, one.copies)
		done.skipped = append(done.skipped, one.skipped...)
		return len(one.copies) > 0
	}
	if !containsString(edit.copiesOf(lib.Name), t.id) {
		return false
	}
	state, err := home.State(placePath)
	if err != nil || !home.IsDir(state) || isLibraryDirectory(placePath, lib.Path) {
		// Gone, or no longer a directory, which the user put there in its
		// place, or the library's own directory, which is no copy at all.
		return false
	}
	if contentHashAt(placePath) != lib.ContentHash {
		// Nothing records what the copy held when it was placed, so the
		// warning says it is different and not how it came to be.
		done.skipped = append(done.skipped, placePath)
		inv.out.warnWith(t.id+"'s copy of "+lib.Name+" is different from the library, so it was kept as a copy ("+placePath+")",
			"to replace it with a symlink to the library, discarding what it holds: "+
				skillCommand("remove", lib.Name, "--from", t.id)+" && "+skillCommand("place", lib.Name, "--to", t.id))
		return false
	}
	var one placements
	if _, _, err := inv.placementContent(m, libraryPlaceable(lib), t, placePath, false); err != nil {
		inv.skipPlacement(&one, t, placePath, err)
		done.skipped = append(done.skipped, one.skipped...)
		return false
	}
	// The journal retains the copy until the outcome is verified, as a
	// removal of a recorded copy does, so a change made while the run is
	// under way is kept rather than lost.
	m.Remove(placePath, state)
	m.Link(placePath, lib.Path)
	edit.dropCopies(lib.Name, []string{t.id})
	return true
}

// reportConverted reads the configuration again and reports each converted
// skill as it now sees it, one library_skill event and one row each, in
// the order the library lists them, under the line that says what the run
// did. A run that converted nothing has nothing to read again.
func (inv *invocation) reportConverted(ctx context.Context, id string, copies bool, targets []placeTarget, done converted) error {
	tail := ", " + plural(len(done.names), "skill") + " converted"
	if n := len(done.skipped); n > 0 {
		tail += fmt.Sprintf(", %d skipped", n)
	}
	t := &table{}
	if len(done.names) > 0 {
		inventory, err := inv.scan(ctx, lockWait, "", false)
		if err != nil {
			return err
		}
		sc, err := inv.skillContext(ctx)
		if err != nil {
			return err
		}
		skills, _ := readLibrary(inv.dirs.Library)
		for _, lib := range skills {
			if !slices.Contains(done.names, lib.Name) {
				continue
			}
			ev := sc.librarySkillEventFor(ctx, inv, inventory, lib, []string{id})
			inv.out.emit(ev)
			inv.addOwnRows(t, lib.Name, targets, ev.Placements)
		}
	}
	inv.out.done(inv.out.paint(label, id) + " now places " + inv.out.paint(heading, placedWord(copies)) + tail)
	inv.out.render(t, "")
	inv.summary = id + " now places " + placedWord(copies) + tail
	return nil
}
