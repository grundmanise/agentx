package cli

import "context"

// skillList reports the skills of the library: what each one is, where it
// came from and where it is seen from. The lineage comes from the branches
// of the account repo alone, read in one for-each-ref over both namespaces,
// so what a source holds, and whether the source is still added at all,
// changes nothing here.
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
// how it stands against its base version, where it came from and how many
// placements it has.
func row(out *writer, ev librarySkillEvent) []cell {
	state := c(ev.State, okStyle)
	if ev.State == stateModified {
		state = c(ev.State, warnStyle)
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
		upstream = c(where, plain)
	}
	return []cell{
		c("  "+ev.Name, heading),
		c(ev.Kind, muted),
		state,
		upstream,
		c(plural(len(ev.Placements), "placement"), noteStyle),
	}
}
