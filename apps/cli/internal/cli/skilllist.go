package cli

import (
	"context"
	"strings"
)

// skillList reports the skills of the library: what each one is, where it
// came from and where it is seen from. The lineage comes from the branches
// of the account repo alone, read in one for-each-ref over both namespaces,
// so what a source holds, and whether its ref is in the account repo at
// all, changes nothing here. Whether the source is still added does: a
// managed skill whose source has no entry in the settings is listed as
// source removed, which is read from the settings this command reads
// anyway, never written anywhere.
func (inv *invocation) skillList(ctx context.Context) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	skills, warnings := readLibrary(inv.dirs.Library)
	out := inv.out
	if len(skills) == 0 {
		out.print("No skills in the library. Install one with ", out.paint(label, "agentx skill add <source>"), ".")
		for _, w := range warnings {
			out.warn(w)
		}
		return nil
	}
	out.print(out.paint(heading, plural(len(skills), "skill")))
	t := &table{}
	for _, lib := range skills {
		ev := sc.librarySkillEventFor(inv, snap, lib, nil) // every placement, not only a command's own
		out.emit(ev)
		t.add(row(out, ev)...)
	}
	out.render(t, "")
	for _, w := range warnings {
		out.warn(w)
	}
	return nil
}

// row is one line of the human listing: the name, what agentx knows it as,
// how it stands against its base version and whatever else it has drifted
// by, where it came from and how many placements it has.
//
// The name and the upstream are sanitised: the name of an unmanaged skill
// is the library directory's own, which whoever put it there chose, a
// managed one's came from the source, and the subpath is a directory of the
// source's repository. Without that a directory named across two lines
// would print one skill as two rows. The state, the drift, the kind and the
// count are agentx's own words and are printed as they are. The event above
// carries all of them as they were read.
//
// The drift shares the state's cell, after it, rather than taking a column
// of its own: most skills have none, and a column that is empty on almost
// every row would widen every listing for the sake of a few. Both are said
// in full, so that neither hides the other.
func row(out *writer, ev librarySkillEvent) []cell {
	state := c(strings.Join(append([]string{ev.State}, ev.Drift...), ", "), okStyle)
	if ev.State == stateModified || len(ev.Drift) > 0 {
		state.style = warnStyle
	}
	if ev.State == "" {
		state = c("-", muted)
	}
	upstream := c("(none)", muted)
	if ev.Source != "" {
		where := ev.Source
		if ev.Subpath != nil && *ev.Subpath != "" {
			where += "/" + *ev.Subpath
		}
		upstream = c(sanitised(where), plain)
	}
	return []cell{
		c("  "+sanitised(ev.Name), heading),
		c(ev.Kind, muted),
		state,
		upstream,
		c(plural(len(ev.Placements), "placement"), noteStyle),
	}
}
