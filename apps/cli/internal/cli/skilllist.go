package cli

import (
	"context"
	"strings"

	"github.com/spf13/cobra"
)

func newSkillListCommand(inv *invocation) *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the skills in the library with their upstream and placements",
		Long: "List the skills in the library with their upstream and placements. With --remote,\n" +
			"fetch the account remote first and list after them the skills it holds that this\n" +
			"machine has not installed, each with the upstream its history records; install\n" +
			"one with 'agentx skill add --name <name>', or all of them with\n" +
			"'agentx skill add --all'.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return inv.skillList(cmd.Context(), remote) },
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "fetch the account remote and list the skills it holds that this machine has not installed")
	return cmd
}

// skillList reports the skills of the library: what each one is, where it
// came from and where it is seen from. The lineage comes from the branches
// of the account repo alone, read in one for-each-ref over both namespaces,
// so what a source holds, and whether its ref is in the account repo at
// all, changes nothing here. Whether the source is still added does: a
// managed skill whose source has no entry in the settings is listed as
// source removed, which is read from the settings this command reads
// anyway, never written anywhere.
//
// A managed skill whose library directory is gone has no row: there is
// nothing to list, compare or place. It is named in a warning of its own
// after the rows, see absentWarnings, so that a branch the account repo
// still holds is never silently left out.
//
// With remote, the account remote is fetched first, outside the lock, and
// every fork it holds that this machine has no branch of is listed after
// the library's skills as installable, see installableForks. Nothing else
// in the listing reaches the network.
func (inv *invocation) skillList(ctx context.Context, remote bool) error {
	inv.forksWarned = true // the warnings after the rows name them
	gitDir, account := "", ""
	if remote {
		dir, entry, name, err := inv.accountRemote(ctx)
		if err != nil {
			return err
		}
		if err := inv.fetchRemote(ctx, dir, name, entry.URL); err != nil {
			return err
		}
		gitDir, account = dir, name
	}
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	skills, warnings := readLibrary(inv.dirs.Library)
	warnings = append(warnings, sc.absentWarnings(inv, skills)...)
	var installable []installableForkEvent
	if remote {
		var skipped []string
		if installable, skipped, err = inv.installableForks(ctx, gitDir, account, sc.records); err != nil {
			return err
		}
		warnings = append(warnings, skipped...)
	}
	out := inv.out
	if len(skills) == 0 {
		out.print("No skills in the library. Install one with ", out.paint(label, "agentx skill add <source>"), ".")
	} else {
		out.print(out.paint(heading, plural(len(skills), "skill")))
		t := &table{}
		for _, lib := range skills {
			ev := sc.librarySkillEventFor(ctx, inv, snap, lib, nil) // every placement, not only a command's own
			out.emit(ev)
			t.add(row(out, ev)...)
		}
		out.render(t, "")
	}
	if remote {
		inv.printInstallable(installable)
	}
	for _, w := range warnings {
		out.warn(w)
	}
	return nil
}

// printInstallable reports the skills the account remote holds that this
// machine has not installed: one installable_fork event each, and a table
// of them after the library's, with the commands that install them.
func (inv *invocation) printInstallable(installable []installableForkEvent) {
	out := inv.out
	if len(installable) == 0 {
		out.print("The account remote holds no skill this machine has not installed.")
		return
	}
	out.print(out.paint(heading, plural(len(installable), "installable skill")+" on the account remote"))
	t := &table{}
	for _, ev := range installable {
		out.emit(ev)
		t.add(installableRow(out, ev)...)
	}
	out.render(t, "")
	out.print("Install one with ", out.paint(label, "agentx skill add --name <name>"), ", or all of them with ", out.paint(label, "agentx skill add --all"), ".")
}

// row is one line of the human listing: the name, what agentx knows it as,
// how it stands against its base version and whatever else it has drifted
// by, where it came from and how many placements it has, one per
// configuration that sees it.
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
//
// An update the last check found is said last in the same cell, as update
// available, and a merge an update left pending after it, as merge
// pending: neither is drift, but both are how the skill stands against its
// upstream, which is what the cell is about.
func row(out *writer, ev librarySkillEvent) []cell {
	words := append([]string{ev.State}, ev.Drift...)
	if ev.Candidate != nil {
		words = append(words, updateAvailable)
	}
	if ev.PendingMerge {
		words = append(words, mergePending)
	}
	state := c(strings.Join(words, ", "), okStyle)
	if ev.State == stateModified || len(ev.Drift) > 0 || ev.Candidate != nil || ev.PendingMerge {
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
		c(plural(placedIn(ev.Placements), "placement"), noteStyle),
	}
}

// placedIn counts the configurations among the placements, each once
// however many paths it sees the skill through. Cursor reads Claude Code's
// skills directory as well as its own, so a skill placed in both is seen
// by Cursor twice, and counting paths would give Cursor a placement nobody
// made for it. Counted this way the column agrees with the rows an install
// prints, one per configuration, and the event above still carries every
// path.
func placedIn(places []placementEvent) int {
	ids := map[string]bool{}
	for _, p := range places {
		ids[p.Configuration] = true
	}
	return len(ids)
}
