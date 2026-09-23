package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

func newSkillRemoveCommand(inv *invocation) *cobra.Command {
	var from []string
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a skill from one configuration or from the machine",
		Long: "Remove a skill from the configurations --from names, or, without --from, take it\n" +
			"off the machine: every placement, the library directory, the import branch and\n" +
			"what the settings recorded about it.\n\n" +
			"A placement is deleted only when it is a symlink into the library or a copy this\n" +
			"machine recorded. Anything else at a placement path is left where it is and\n" +
			"named in the output: agentx never removes what it did not create.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillRemove(cmd.Context(), args[0], from)
		},
	}
	cmd.Flags().StringArrayVar(&from, "from", nil, "remove only the placement in this configuration; give it again for each")
	return cmd
}

// removalStep is what a removal does at one placement path: delete what
// agentx put there, or leave what it did not and say why.
type removalStep struct {
	id    string // the configuration the placement belongs to
	path  string
	mode  string // symlink, copy, or library for a client that reads the library
	state string // what the path holds now, in the words the journal records
	why   string // why the path was left alone; empty when the step deletes it
}

// removalPlan is one run of agentx skill remove: what it deleted, what it
// left alone and what it found in the account repo, for the report that
// follows the mutation.
type removalPlan struct {
	name    string
	whole   bool     // no --from: the skill comes off the machine
	from    []string // the configurations --from named, in detection order
	deleted []removalStep
	kept    []removalStep
	library []string // the configurations that saw the skill through the library entry
	managed string   // the commit the import branch held; empty when there was none
	dropped []string // the configurations taken out of copy_mode
}

// skillRemove takes a skill out of the configurations --from names, or off
// the machine when --from is absent: every placement, the library
// directory, the import branch, its candidate ref and its copy modes.
//
// The rule the whole command turns on: a placement is deleted only when it
// is a symlink into the library or a copy this machine recorded in
// copy_mode. A directory the user made by hand, a symlink they pointed
// somewhere else, a copy nothing recorded and a file are left exactly where
// they are and named in the output. agentx removes what it created and
// nothing else.
func (inv *invocation) skillRemove(ctx context.Context, name string, from []string) error {
	whole := len(from) == 0
	if _, ok := librarySkill(inv.dirs.Library, name); !ok {
		return inv.noLibrarySkill(name)
	}
	targets, err := inv.removalTargets(name, from)
	if err != nil {
		return err
	}
	gitDir, hasRepo, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	plan := removalPlan{name: name, whole: whole, from: targetIDs(targets)}
	libPath := inv.libraryPath(name)
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		// Every input is read again under the lock, and the refs before
		// anything is staged, so that a fork is refused before the command
		// has planned a single deletion.
		lib, ok := librarySkill(inv.dirs.Library, name)
		if !ok {
			return inv.noLibrarySkill(name)
		}
		libHash := lib.ContentHash
		refs := map[string]string{}
		if hasRepo {
			values, err := inv.lineageRefs(ctx, gitDir, name)
			if err != nil {
				return err
			}
			if whole && values[lineage.ForkRef(name)] != "" {
				return forkOutOfScope(name)
			}
			refs = values
		}
		edit, err := inv.beginSettings()
		if err != nil {
			return err
		}
		m := home.NewMutation(inv.dirs.Home)
		plan.deleted, plan.kept, plan.library, plan.dropped = nil, nil, nil, nil
		for _, t := range targets {
			if t.readsLibrary {
				// There is no placement to delete: the library entry is the
				// occurrence. Removing the entry takes it away from this
				// client too, which is why --from refused this configuration.
				plan.library = append(plan.library, t.id)
				continue
			}
			step, err := inv.planRemoval(t, name, libPath, libHash, edit.copiesOf(name))
			if err != nil {
				m.Discard()
				return err
			}
			switch {
			case step.state == "" && step.why == "": // nothing there at all
				plan.dropped = append(plan.dropped, t.id)
			case step.why != "":
				plan.kept = append(plan.kept, step)
				inv.out.warn(step.why)
			default:
				plan.deleted = append(plan.deleted, step)
				plan.dropped = append(plan.dropped, t.id)
				m.Remove(step.path, step.state)
			}
		}
		if whole {
			state, err := home.State(libPath)
			if err != nil {
				m.Discard()
				return err
			}
			m.Remove(libPath, state)
			// Both carry the value they hold now, so a branch something
			// else moved is refused rather than dropped. A journal whose
			// ref steps only delete applies them last, after the library
			// directory and every placement, so a removal that cannot
			// finish still has the skill's lineage to answer with.
			if commit := refs[lineage.ManagedRef(name)]; commit != "" {
				plan.managed = commit
				m.Ref(gitDir, lineage.ManagedRef(name), commit, "")
			}
			if candidate := refs[lineage.CandidateRef(name)]; candidate != "" {
				m.Ref(gitDir, lineage.CandidateRef(name), candidate, "")
			}
			edit.dropSkill(name)
		} else {
			edit.dropCopies(name, plan.dropped)
		}
		if err := edit.stage(m, inv.dirs.Home); err != nil {
			m.Discard()
			return err
		}
		return m.Apply(inv.refs(ctx))
	})
	if err != nil {
		return mutationFailure(err)
	}
	return inv.reportRemoved(ctx, plan, targets)
}

// removalTargets are the configurations the removal covers: those --from
// names, or every detected one when the skill comes off the machine,
// whatever their enabled state, since a disabled configuration can still
// hold a placement made before it was disabled.
func (inv *invocation) removalTargets(name string, from []string) ([]placeTarget, error) {
	if len(from) == 0 {
		return inv.detectedTargets(), nil
	}
	targets, err := inv.placementTargets(from)
	if err != nil {
		return nil, err
	}
	for _, t := range targets {
		if t.readsLibrary {
			return nil, universalLibrary(t.id, name, inv.libraryPath(name))
		}
	}
	return targets, nil
}

// lineageRefs reads the import branch, the fork branch and the candidate
// ref of one skill in one git process, so that a removal knows what it has
// to take away and what it must refuse before it plans anything.
func (inv *invocation) lineageRefs(ctx context.Context, gitDir, name string) (map[string]string, error) {
	values, err := inv.git.Refs(ctx).RefValues(gitDir,
		[]string{lineage.ManagedRef(name), lineage.ForkRef(name), lineage.CandidateRef(name)})
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	return values, nil
}

// planRemoval decides what the removal does at one configuration's
// placement path. This is the rule of the whole command: a symlink that
// names the library directory goes, a real directory this machine recorded
// as a copy goes, and everything else stays where it is with a line saying
// so. A path holding nothing is neither, and the step it returns carries
// neither a state nor a reason.
func (inv *invocation) planRemoval(t placeTarget, name, libPath, libHash string, copies []string) (removalStep, error) {
	path := filepath.Join(t.dir, name)
	state, err := home.State(path)
	if err != nil {
		return removalStep{}, err
	}
	step := removalStep{id: t.id, path: path}
	switch {
	case home.IsAbsent(state):
		return step, nil
	case sameTarget(path, libPath):
		// A symlink at the library directory, dangling or not: agentx made
		// it, and a link holds nothing of the user's to keep.
		step.mode, step.state = modeSymlink, state
	case home.IsDir(state) && containsString(copies, t.id):
		// A copy this machine recorded in copy_mode. The journal retains
		// what it displaces until the outcome is verified, so a change made
		// while the removal ran is kept rather than lost. A change made
		// before it goes with the copy, which is what removing a copy
		// means, so the run says that it went.
		//
		// What it says is what agentx can see: that the directory no longer
		// holds the library's version. How it came to differ is not
		// recorded anywhere — the user may have edited the copy, replaced
		// it with something of another project, or had a directory of their
		// own adopted here that was never a copy agentx wrote — so the
		// warning does not claim a history it has no record of.
		step.mode, step.state = modeCopy, state
		if libHash != "" && contentHashAt(path) != libHash {
			inv.out.warn(path + " did not hold the library's version of " + name +
				"; removing the copy took what was there with it")
		}
	default:
		step.why = inv.whyKept(path, state, libPath, t.id)
	}
	return step, nil
}

// whyKept is the line a placement agentx did not make is reported with. It
// names the path, since that is what the user has to look at to decide what
// to do with it, and what the path is, since that is why it stayed.
//
// A link is named by what it points at and by the library directory it does
// not point at, and says nothing about where following it ends up: a link
// of the user's may well resolve into the library, and it stays either way.
func (inv *invocation) whyKept(path, state, libPath, id string) string {
	const left = " and was left as it is; "
	switch target, isLink := home.LinkTarget(state); {
	case isLink:
		return path + " points at " + target + ", not at " + libPath + left + id + " still sees it"
	case home.IsDir(state):
		return path + " is a directory agentx did not place there" + left + id + " still sees it"
	}
	// Neither a directory nor a link: nothing a client reads as a skill, so
	// there is nothing to say about what the client still sees.
	return path + " is not a placement agentx made and was left as it is"
}

// universalLibrary answers a request to remove a universal-library
// occurrence for one client. There is no placement to delete: the library
// entry is what that client reads, and taking it away would take the skill
// from every other client that reads the library too.
func universalLibrary(id, name, libPath string) error {
	return fail(exitRefused,
		fmt.Sprintf("%s reads the library directly, so %s has no placement of its own to remove there", id, name),
		"the library entry "+libPath+" is what "+id+" sees: stop "+id+" reading the library in its own settings, "+
			"or take the entry away from every client that reads it with 'agentx skill remove "+name+"'")
}

// forkOutOfScope refuses to take a fork off the machine: a fork's history
// lives in the account repo and removing it is its own command.
func forkOutOfScope(name string) error {
	return fail(exitRefused, name+" is a fork on this machine",
		"take its placements away with 'agentx skill remove "+name+" --from <configuration>'; removing the fork itself is not this command")
}

// reportRemoved reads the configurations this command covered again and
// reports what is there now, the way an install does: the rescan is what
// the report is read from, not what the command meant to do.
func (inv *invocation) reportRemoved(ctx context.Context, plan removalPlan, targets []placeTarget) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	lib, stillThere := librarySkill(inv.dirs.Library, plan.name)
	switch {
	case plan.whole && stillThere:
		return fail(exitInternal, "the library still holds "+plan.name+" at "+inv.libraryPath(plan.name)+" after removing it",
			"run 'agentx doctor' and check the library it names")
	case !plan.whole && stillThere:
		sc, err := inv.skillContext(ctx)
		if err != nil {
			return err
		}
		inv.out.emit(sc.librarySkillEventFor(inv, snap, lib, targetIDs(targets)))
	}
	if plan.whole {
		inv.warnStillSeen(snap, plan, targetIDs(targets))
	}
	inv.printRemoved(plan)
	inv.summary = removeSummary(plan)
	return nil
}

// warnStillSeen names every configuration the removal covered that still
// has a skill of this name once the run is over.
//
// A removal without --from reports each covered configuration as having
// lost the skill, and for a client that reads the library that claim rests
// on the library entry alone: the removal never looks at a directory of
// that client's own, since it deletes only what agentx placed. A leftover
// directory there leaves the client seeing the skill a second after the run
// said it did not — and a whole removal emits no skill event, so nothing
// else would ever correct the claim.
//
// A placement the run already reported as left in place is not named twice:
// its own warning said the configuration still sees it.
func (inv *invocation) warnStillSeen(snap scan.Snapshot, plan removalPlan, covered []string) {
	said := make(map[string]bool, len(plan.kept))
	for _, s := range plan.kept {
		said[s.path] = true
	}
	var left []string
	for _, node := range snap.Skills {
		if node.Name != plan.name {
			continue
		}
		for _, o := range node.Occurrences {
			if o.Plugin != "" || o.Scope != "user" || said[o.Path] || !containsString(covered, o.Configuration) {
				continue // a plugin's skill and a project's are not placements of the library
			}
			left = append(left, o.Configuration+" still sees "+plan.name+" at "+o.Path+", which agentx did not place there")
		}
	}
	sort.Strings(left)
	for _, message := range left {
		inv.out.warn(message)
	}
}

// removedPlacements is how many ways of seeing the skill the removal took
// away: the placements it deleted and, when the skill left the library, the
// library entry every client that reads it directly saw it through.
func (plan removalPlan) removedPlacements() int { return len(plan.deleted) + len(plan.library) }

// printRemoved writes the confirmation and one row per placement removed.
// The name is a library directory's, which whoever made it chose, so it is
// sanitised, and the paths, which carry it, are quoted, as skill place
// prints its rows.
func (inv *invocation) printRemoved(plan removalPlan) {
	out := inv.out
	where := "the library"
	if !plan.whole {
		where = strings.Join(plan.from, ", ")
	}
	line := "removed " + out.paint(heading, sanitised(plan.name)) + " from " + out.paint(heading, where) + ": " +
		out.paint(noteStyle, plural(plan.removedPlacements(), "placement"))
	if n := len(plan.kept); n > 0 {
		line += ", " + out.paint(warnStyle, plural(n, "placement")+" left in place")
	}
	out.done(line)
	t := &table{}
	for _, s := range plan.deleted {
		t.add(c("  "+s.id, label), c(s.mode, muted), c(quotedPath(s.path), plain))
	}
	for _, id := range plan.library {
		t.add(c("  "+id, label), c(modeLibrary, muted), c(quotedPath(inv.libraryPath(plan.name)), plain))
	}
	out.render(t, "")
	if plan.whole {
		gone := quotedPath(inv.libraryPath(plan.name))
		if plan.managed != "" {
			gone += " and " + sanitised(lineage.ManagedRef(plan.name))
		}
		out.print("  ", out.paint(muted, "deleted "+gone))
	}
}

// removeSummary is what the result event says the removal did.
func removeSummary(plan removalPlan) string {
	n := plural(plan.removedPlacements(), "placement")
	var summary string
	switch {
	case plan.whole && plan.managed != "":
		summary = "removed " + plan.name + " from the library, " + n + " and its import branch"
	case plan.whole:
		summary = "removed " + plan.name + " from the library and " + n
	default:
		summary = "removed " + plan.name + " from " + strings.Join(plan.from, ", ") + ": " + n
	}
	if len(plan.kept) > 0 {
		summary += ", " + plural(len(plan.kept), "placement") + " left in place"
	}
	return summary
}
