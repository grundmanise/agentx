package cli

import (
	"context"
	"fmt"
	"os"
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
		Long: "Remove a skill from the configurations --from names, or, with --from universal or\n" +
			"without --from, take it off the machine: every placement, the library directory,\n" +
			"the import branch and what the settings recorded about it.\n\n" +
			"A universal client, such as Codex or Gemini CLI, reads the library directly and\n" +
			"sees the skill through the library entry, so it cannot lose the skill on its own:\n" +
			"--from naming one is refused, and --from universal removes the skill from every\n" +
			"client.\n\n" +
			"A placement is deleted only when it is a symlink into the library or a copy this\n" +
			"machine recorded. Anything else at a placement path is left where it is and\n" +
			"named in the output: agentx never removes what it did not create.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillRemove(cmd.Context(), args[0], from)
		},
	}
	cmd.Flags().StringArrayVar(&from, "from", nil,
		"remove only the placement in this configuration, or give universal to take the skill off the machine; give it again for each")
	return cmd
}

// fromUniversal is the --from value that names the universal clients
// together. They can only lose a skill together: each one reads the library
// entry itself, so removing the skill from them is removing that entry,
// which takes it from every client and is the removal without --from. No
// configuration can be called this, since the registry leaves the skills
// CLI's universal pseudo-agent out and a test holds it to that.
const fromUniversal = "universal"

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
	whole   bool     // no --from, or --from universal: the skill comes off the machine
	from    []string // the configurations the removal covers, in detection order
	deleted []removalStep
	kept    []removalStep
	library []string // the configurations that saw the skill through the library entry
	managed string   // the commit the import branch held; empty when there was none
	dropped []string // the configurations taken out of copy_mode
}

// skillRemove takes a skill out of the configurations --from names, or off
// the machine when --from is absent or universal: every placement, the
// library directory, the import branch, its candidate ref and its copy
// modes. --from universal is not a second way to do that but the same run,
// down to the refusal of a fork: it only names the removal by what it does
// to the universal clients.
//
// The rule the whole command turns on: a placement is deleted only when it
// is a symlink into the library or a copy this machine recorded in
// copy_mode. A directory the user made by hand, a symlink they pointed
// somewhere else, a copy nothing recorded and a file are left exactly where
// they are and named in the output. agentx removes what it created and
// nothing else.
func (inv *invocation) skillRemove(ctx context.Context, name string, from []string) error {
	if _, ok := librarySkill(inv.dirs.Library, name); !ok {
		return inv.noLibrarySkill(name)
	}
	targets, whole, err := inv.removalTargets(ctx, name, from)
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
		// What the run deletes is known before its first step, so that the
		// warning for a link it leaves can say whether that link still leads
		// anywhere, whichever target the link leads through.
		gone := removalDeletes(targets, name, libPath, edit.copiesOf(name), whole)
		for _, t := range targets {
			if t.readsLibrary {
				// There is no placement to delete: the library entry is the
				// occurrence. Removing the entry takes it away from this
				// client too, which is why --from refused this configuration.
				plan.library = append(plan.library, t.id)
				continue
			}
			step, err := inv.planRemoval(t, name, libPath, libHash, edit.copiesOf(name), gone)
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

// removalTargets are the configurations the removal covers, and whether it
// takes the skill off the machine: those --from names, or, with --from
// universal or without --from, every detected one, whatever their enabled
// state, since a disabled configuration can still hold a placement made
// before it was disabled. --from universal given more than once is one
// request.
//
// Every other --from value is looked up among the detected configurations
// first, since what the request means turns on which clients they are. A
// --from naming a universal client refuses the whole request here, before
// the command takes the lock, writes a journal or plans a single deletion,
// whatever else --from names, universal included: a run cannot half do what
// it cannot do at all, and the refusal changes nothing a later command has
// to recover from.
//
// Only then is --from universal beside a client that is not universal
// refused, the way --all beside --skill is: the whole removal already takes
// the skill from every configuration, so a list next to it can only mean the
// user expected it to do less than it does, and a removal is not a command
// to guess at. The hint says which removal --from universal asks for, not
// what that removal does: it is decided before the refs are read, and for a
// fork that removal is refused, so it takes the skill from no client at
// all. Beside a universal client that answer would be wrong: its hint, to
// drop --from universal, leads straight to the refusal above.
func (inv *invocation) removalTargets(ctx context.Context, name string, from []string) ([]placeTarget, bool, error) {
	var named []string
	for _, id := range from {
		if id != fromUniversal {
			named = append(named, id)
		}
	}
	if len(named) == 0 {
		return inv.detectedTargets(), true, nil
	}
	targets, err := inv.placementTargets(named)
	if err != nil {
		return nil, false, err
	}
	var universal []string
	for _, t := range targets {
		if t.readsLibrary {
			universal = append(universal, t.id)
		}
	}
	if len(universal) > 0 {
		return nil, false, inv.refuseUniversal(ctx, universal, name)
	}
	if len(named) < len(from) {
		return nil, false, fail(exitUsage, "--from universal and --from "+named[0]+" cannot both be given",
			"--from universal asks for the removal without --from; drop it to remove only the placements you name")
	}
	return targets, false, nil
}

// refuseUniversal answers a --from naming the universal clients in named.
// What it offers instead is the removal --from universal asks for, with
// every configuration that removal would take the skill from, unless the
// name is a fork: that removal refuses a fork, so offering it would send
// the user from one refusal to the next.
//
// Whether the name is a fork is read from the account repo without the
// lock, as a listing reads its branches: a read changes nothing, and the
// request is refused whatever it finds.
func (inv *invocation) refuseUniversal(ctx context.Context, named []string, name string) error {
	gitDir, hasRepo, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	if hasRepo {
		values, err := inv.lineageRefs(ctx, gitDir, name)
		if err != nil {
			return err
		}
		if values[lineage.ForkRef(name)] != "" {
			return universalFork(named, name)
		}
	}
	reach, err := inv.wholeRemovalReach(name)
	if err != nil {
		return err
	}
	return universalLibrary(named, name, reach)
}

// wholeRemovalReach names every configuration the whole removal would take
// the skill from, sorted by id as detection is: every detected client that
// sees the skill through a path the removal deletes. Those paths are the
// library entry, which every universal client reads whatever its enabled
// state, and each placement the removal would delete by the rule
// planRemoval deletes by, a symlink into the library or a recorded copy,
// as removalDeletes finds them for the removal itself.
//
// A client is named by the directories it reads, not by whose directory the
// path is in. Cursor reads Claude Code's skills directory, so a link agentx
// made there takes the skill from Cursor as well as from Claude Code, while
// a directory of the user's that the removal would leave takes the skill
// from nobody. A universal client with a directory of its own under the
// name is named all the same: it loses the library skill, and the removal
// says which directory of its own it still sees.
//
// A link of the user's is counted when following it leads through one of
// those paths, the library entry or a placement the removal deletes. The
// removal leaves the link where it is, since agentx did not make it, but
// the link no longer leads anywhere, so its client loses the skill all the
// same. A link that reaches the skill's files some other way is not
// counted: when the library entry is itself a link, to a directory of the
// user's say, a link straight to that directory keeps working once the
// entry is gone.
//
// It changes nothing and takes no lock, since it only words the refusal of
// a request that never takes it.
func (inv *invocation) wholeRemovalReach(name string) ([]string, error) {
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	modes, err := inv.copyModes(s)
	if err != nil {
		return nil, err
	}
	gone := removalDeletes(inv.detectedTargets(), name, inv.libraryPath(name), modes[name], true)
	var ids []string
	for _, c := range scan.Detect(inv.dirs) {
		for _, dir := range c.SkillsDirs(inv.dirs) {
			if leadsInto(filepath.Join(dir, name), gone) {
				ids = append(ids, c.Slug())
				break
			}
		}
	}
	return ids, nil
}

// removalDeletes is every path a removal of name that covers targets
// deletes, keyed as canonicalPath writes them: the placement agentx made in
// each target, by the rule planRemoval deletes by, and the library entry
// when the removal is whole. The hint of a refused --from and the warning
// for a link a removal leaves both turn on it, so that what either says a
// removal takes away is what the removal deletes. A path that cannot be
// read is left out; the removal itself stops on it.
func removalDeletes(targets []placeTarget, name, libPath string, copies []string, whole bool) map[string]bool {
	gone := map[string]bool{}
	if whole {
		gone[canonicalPath(libPath)] = true
	}
	for _, t := range targets {
		if t.readsLibrary {
			continue // the library entry is its placement, and it goes only when the removal is whole
		}
		path := filepath.Join(t.dir, name)
		state, err := home.State(path)
		if err != nil || home.IsAbsent(state) || agentxPlacement(t, path, state, libPath, copies) == "" {
			continue
		}
		gone[canonicalPath(path)] = true
	}
	return gone
}

// maxLinkHops bounds the links leadsInto follows, as an operating system
// bounds those of one path lookup, so that a loop of links ends.
const maxLinkHops = 40

// leadsInto reports whether path is one of the paths in gone, keyed as
// canonicalPath writes them, or leads to one by following links one at a
// time. It stops at the first path in gone rather than at the end of the
// chain: a link that goes through the library entry dies with it, even
// when the entry is itself a link that leads on somewhere else.
func leadsInto(path string, gone map[string]bool) bool {
	for range maxLinkHops {
		path = canonicalPath(path)
		if gone[path] {
			return true
		}
		target, err := os.Readlink(path)
		if err != nil {
			return false // not a link, or nothing there: the chain ends short of anything the removal deletes
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = target
	}
	return false
}

// canonicalPath is path with every link in its parent directories resolved
// and its last element kept as it is, so that two spellings of one entry
// compare equal while the entry, which may itself be a link, is still the
// one named. A parent that does not resolve leaves path as it is, cleaned.
func canonicalPath(path string) string {
	path = filepath.Clean(path)
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return path
	}
	return filepath.Join(dir, filepath.Base(path))
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
// neither a state nor a reason. gone is every path the removal deletes, as
// removalDeletes finds them, which the line for a link it leaves turns on.
func (inv *invocation) planRemoval(t placeTarget, name, libPath, libHash string, copies []string, gone map[string]bool) (removalStep, error) {
	path := filepath.Join(t.dir, name)
	state, err := home.State(path)
	if err != nil {
		return removalStep{}, err
	}
	step := removalStep{id: t.id, path: path}
	if home.IsAbsent(state) {
		return step, nil
	}
	switch agentxPlacement(t, path, state, libPath, copies) {
	case modeSymlink:
		// A symlink at the library directory, dangling or not: agentx made
		// it, and a link holds nothing of the user's to keep.
		step.mode, step.state = modeSymlink, state
	case modeCopy:
		// A copy this machine recorded in copy_mode. The journal retains
		// what it displaces until the outcome is verified, so a change made
		// while the removal ran is kept rather than lost. A change made
		// before it goes with the copy, which is what removing a copy
		// means, so the run says that it went.
		//
		// What it says is what agentx can see: whose copy it was, and that
		// it was different from the library, worded like the warning for a
		// copy a placement leaves unchanged. How it came to differ is not
		// recorded anywhere: the user may have edited the copy, replaced it
		// with something of another project, or had a directory of their own
		// adopted here that was never a copy agentx wrote. So the warning
		// does not claim a history it has no record of.
		step.mode, step.state = modeCopy, state
		if libHash != "" && contentHashAt(path) != libHash {
			inv.out.warn(t.id + "'s copy of " + name +
				" was different from the library; removing it deleted those changes (" + path + ")")
		}
	default:
		step.why = inv.whyKept(path, state, libPath, t.id, name, gone)
	}
	return step, nil
}

// agentxPlacement is the mode of the placement agentx made at path for the
// configuration t, and "" when what is there is not agentx's: a symlink that
// names the library directory, dangling or not, or a real directory the
// settings record as t's copy. It is the whole rule of what a removal may
// delete, kept in one place so that the hint of a refused removal names
// every client that sees the skill through a path the removal it suggests
// would actually delete, whoever's skills directory that path is in.
func agentxPlacement(t placeTarget, path, state, libPath string, copies []string) string {
	switch {
	case sameTarget(path, libPath):
		return modeSymlink
	case home.IsDir(state) && containsString(copies, t.id):
		return modeCopy
	}
	return ""
}

// whyKept is the line a placement agentx did not make is reported with. It
// names the path, since that is what the user has to look at to decide what
// to do with it, and what the path is, since that is why it stayed.
//
// A link is named by what it points at and by the library directory it does
// not point at: a link of the user's may well resolve into the library, and
// it stays either way. What the line says about the client then turns on
// where following the link leads, gone being every path the removal
// deletes. A link whose chain of links goes through one of them stays but
// no longer leads to the skill once the run is over, which is what the line
// says; when the skill comes off the machine, that is every link of the
// user's into the library entry. Saying the client still sees the skill
// there would be false the moment the run ends. A link that reaches the
// skill's files some other way, such as one straight to the directory a
// library entry that is itself a link names, keeps working, and the client
// still sees the skill through it.
func (inv *invocation) whyKept(path, state, libPath, id, name string, gone map[string]bool) string {
	const left = " and was left as it is; "
	switch target, isLink := home.LinkTarget(state); {
	case isLink && leadsInto(path, gone):
		return path + " points at " + target + ", not at " + libPath + left + "it no longer leads to " + name
	case isLink:
		return path + " points at " + target + ", not at " + libPath + left + id + " still sees it"
	case home.IsDir(state):
		return path + " is a directory agentx did not place there" + left + id + " still sees it"
	}
	// Neither a directory nor a link: nothing a client reads as a skill, so
	// there is nothing to say about what the client still sees.
	return path + " is not a placement agentx made and was left as it is"
}

// universalLibrary answers a request to remove a skill from universal
// clients on their own. There is no placement of theirs to delete: the
// library entry is what each of them reads, and taking it away takes the
// skill from every client that sees it, which is the removal --from
// universal asks for by name. The hint offers that removal and names every
// configuration it would take the skill from, reach, so the user sees what
// the request that can be done costs before making it.
func universalLibrary(named []string, name string, reach []string) error {
	return fail(exitRefused, universalRefusal(named, name),
		"take "+name+" off the machine with 'agentx skill remove "+name+" --from "+fromUniversal+"', which removes it from "+
			strings.Join(reach, ", "))
}

// universalRefusal is what a --from naming universal clients is refused
// with, the universal clients it named in id order.
func universalRefusal(named []string, name string) string {
	who, reads, them := named[0], "reads", "it"
	if len(named) > 1 {
		who, reads, them = strings.Join(named, ", "), "read", "them"
	}
	return fmt.Sprintf("%s %s the library directly, so %s cannot be removed from %s alone", who, reads, name, them)
}

// universalFork answers a --from naming universal clients when the name is
// a fork. The universal clients see the fork through the library entry, and
// only removing the fork takes it from them, which this command does not do,
// so the hint offers no --from universal: that removal refuses a fork too.
// What the command can still do is take the fork from the other clients.
func universalFork(named []string, name string) error {
	return fail(exitRefused, universalRefusal(named, name),
		name+" is a fork on this machine: every universal client sees it through the library entry until the fork itself is removed, "+
			"which is not this command; take it from the other clients with 'agentx skill remove "+name+" --from <configuration>'")
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
// A removal that takes the skill off the machine, without --from or with
// --from universal, reports each covered configuration as having lost the
// skill, and for a client that reads the library that claim rests on the
// library entry alone: the removal never looks at a directory of that
// client's own, since it deletes only what agentx placed. A leftover
// directory there leaves the client seeing the skill a second after the run
// said it did not, and a whole removal emits no skill event, so nothing
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
