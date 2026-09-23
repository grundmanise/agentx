package cli

import (
	"context"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
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
			for _, t := range targets {
				inv.stagePlacement(m, libraryPlaceable(lib), t, lib.Path, false, edit.copiesOf(lib.Name), &done)
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
	// placementTargets refuses an id it cannot place into, so the run has
	// exactly the one configuration it named by the time it reports.
	readsLibrary := len(targets) > 0 && targets[0].readsLibrary
	return inv.reportPlacedAll(ctx, id, names, done, readsLibrary)
}

// reportPlacedAll reads the configuration this command touched again and
// reports every library skill as it now sees it, one skill event each, in
// the order the library lists them.
func (inv *invocation) reportPlacedAll(ctx context.Context, id string, names []string, done placements, readsLibrary bool) error {
	total := len(names) + 1
	for i, name := range names {
		inv.progress(phasePlace, name, i+1, total)
	}
	snap, err := inv.scan(ctx, lockWait, "", false)
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
		ev := sc.librarySkillEventFor(inv, snap, lib, []string{id})
		inv.out.emit(ev)
		if len(ev.Placements) > 0 {
			// One skill the configuration now sees, however many ways it sees
			// it: a client that reads another client's skills directory, as
			// Cursor reads Claude Code's, has more paths than placements made.
			placed++
		}
		for _, p := range ev.Placements {
			path := p.Path
			if p.Kind == modeSymlink {
				path += inv.out.paint(muted, " -> "+inv.libraryPath(lib.Name))
			}
			t.add(c("  "+lib.Name, heading), c(p.Mode, muted), c(path, plain))
		}
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
