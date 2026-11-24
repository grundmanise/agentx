package cli

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// The fleet is every machine the hub holds something of: each computer
// whose report branch it holds, see report.go, and the cloud machine, when
// it holds its snapshot, see fleetcloud.go. A read of it is one ls-remote
// of both, a fetch of the branches that moved, and the file of each tip.
// It says what each machine held when its branch last changed, and never
// that a machine is on.

// fleetEvent is what the hub holds of every machine.
type fleetEvent struct {
	event
	Machines []fleetMachine `json:"machines"` // sorted by machine id
}

// fleetMachine is one machine as the hub holds it: a computer's report, or
// the cloud machine's snapshot read as one, with the commit that holds it.
type fleetMachine struct {
	Machine   machineFacts `json:"machine"`
	Commit    string       `json:"commit"`     // the tip of the machine's branch: reports/<id>, or machines/cloud
	ChangedAt string       `json:"changed_at"` // when that commit was made, RFC 3339 in UTC
	reportContents
}

// fleet is one read of the hub's machines: the machines whose branch a view
// can use, sorted by id, and a warning for each branch it cannot.
type fleet struct {
	machines []fleetMachine
	warnings []string
}

// readFleet reads the hub's machines, refusing, exit code 6, a machine
// with no hub set. A hub git cannot reach is exit code 3. A branch a view
// cannot use is left out and warned of, and fails nothing: one machine's
// report never hides another's.
func (inv *invocation) readFleet(ctx context.Context) (fleet, error) {
	gitDir, entry, remote, err := inv.hubRemote(ctx)
	if err != nil {
		return fleet{}, err
	}
	f := fleet{machines: []fleetMachine{}}
	cloud := gitx.MachineBranch(cloudMachine)
	held, err := inv.hubBranches(ctx, gitDir, remote, entry.URL, gitx.ReportBranches, cloud)
	if err != nil {
		return fleet{}, err
	}
	for _, branch := range slices.Sorted(maps.Keys(held)) {
		tip := held[branch]
		var m fleetMachine
		var why string
		switch {
		case tip.Committed.IsZero():
			why = branch + " on the hub is skipped: it holds no commit"
		case branch == cloud:
			m, why, err = inv.cloudFleetMachine(ctx, machineBranch{gitDir: gitDir, remote: remote, url: entry.URL, id: cloudMachine, tip: tip.Commit})
		default:
			m, why, err = inv.reportedMachine(ctx, gitDir, branch, tip.Commit)
		}
		if err != nil {
			return fleet{}, err
		}
		if why != "" {
			f.warnings = append(f.warnings, why)
			continue
		}
		m.Commit, m.ChangedAt = tip.Commit, tip.Committed.Format(time.RFC3339)
		f.machines = append(f.machines, m)
	}
	slices.SortStableFunc(f.machines, func(a, b fleetMachine) int { return cmp.Compare(a.Machine.ID, b.Machine.ID) })
	return f, nil
}

// event is the fleet as the fleet event carries it.
func (f fleet) event() fleetEvent {
	return fleetEvent{event: newEvent("fleet"), Machines: f.machines}
}

// reportedMachine reads the machine the report branch, a short name, holds
// at commit. why is the warning of a branch a view cannot use, "" when it
// can, see reportOf; a git that cannot read the commit is one too, unless
// the run was stopped, which is the run's to answer for.
func (inv *invocation) reportedMachine(ctx context.Context, gitDir, branch, commit string) (m fleetMachine, why string, err error) {
	data, size, found, err := inv.git.FileAt(ctx, gitDir, commit, gitx.ReportFile, documentLimit)
	switch {
	case err != nil && (ctx.Err() != nil || inv.git.StoppedChild()):
		return fleetMachine{}, "", storeFailure(err)
	case err != nil:
		return fleetMachine{}, branch + " on the hub is skipped: git cannot read it: " + trimGit(err.Error()), nil
	}
	m, why = reportOf(branch, data, size, found)
	return m, why, nil
}

// reportOf is the machine the report branch, a short name, holds, from
// what a read of its tip found: the report file's bytes, its size, and
// whether the tip holds one. why is the warning of a branch a view cannot
// use, naming it, "" when it can: a branch whose name is no machine id, a
// tip with no report file, a file too large to be one, and a file
// decodeReport does not take. Pure.
func reportOf(branch string, data []byte, size int, found bool) (m fleetMachine, why string) {
	id, _ := gitx.ReportMachine(branch)
	switch {
	case !computerID.MatchString(id):
		why = "its name is not that of a machine id"
	case !found:
		why = "it holds no " + gitx.ReportFile
	case size > documentLimit:
		why = fmt.Sprintf("its %s is larger than %d bytes, which is more than any machine's report comes to", gitx.ReportFile, documentLimit)
	default:
		var r inventoryReport
		if r, why = decodeReport(id, data); why == "" {
			return fleetMachine{Machine: r.Machine, reportContents: r.reportContents}, ""
		}
	}
	return fleetMachine{}, clipped(sanitised(branch)) + " on the hub is skipped: " + why
}

// matchMachines are the machines of the fleet arg names: the one whose id
// it is, else every computer whose id it starts or whose label it is. One
// match is the machine meant, several are a name that does not tell them
// apart, and none is a machine the hub holds nothing of. The cloud machine
// is named by its id alone. Pure.
func matchMachines(arg string, machines []fleetMachine) []fleetMachine {
	var named []fleetMachine
	for _, m := range machines {
		switch id := m.Machine.ID; {
		case id == arg:
			return []fleetMachine{m}
		case id == cloudMachine || arg == cloudMachine:
		case strings.HasPrefix(id, arg) || m.Machine.Label == arg:
			named = append(named, m)
		}
	}
	return named
}

// noMachineGiven refuses an empty machine argument, exit code 1, which a
// command gives before it reads anything.
func noMachineGiven() error {
	return fail(exitUsage, "no machine given", "")
}

// machineNamed is the machine of the fleet arg names, see matchMachines. A
// name of several machines is refused, exit code 6, naming each by its
// label and id, and one of none is exit code 5.
func machineNamed(arg string, machines []fleetMachine) (fleetMachine, error) {
	if arg == "" {
		return fleetMachine{}, noMachineGiven()
	}
	named := matchMachines(arg, machines)
	switch {
	case len(named) == 1:
		return named[0], nil
	case len(named) > 1:
		var candidates []string
		for _, m := range named {
			candidates = append(candidates, sanitised(m.Machine.Label)+" ("+m.Machine.ID+")")
		}
		return fleetMachine{}, fail(exitRefused, clipped(sanitised(arg))+" names more than one machine: "+strings.Join(candidates, ", "), "")
	case arg == cloudMachine:
		return fleetMachine{}, fail(exitNotFound, "the hub holds no "+gitx.MachineBranch(cloudMachine)+", so there is no "+cloudMachine+" machine to show", "")
	}
	return fleetMachine{}, fail(exitNotFound, "the hub holds no report of a machine called "+clipped(sanitised(arg)), "")
}

// machineList is agentx machine list: every machine the hub holds
// something of. With --json it is the fleet event alone, what the hub
// holds and nothing of this machine's disk. The text output is one row a
// machine: this machine first, from a scan made now, so that it shows
// whether it reports or not, then the other computers by label, then the
// cloud machine.
func (inv *invocation) machineList(ctx context.Context) error {
	f, err := inv.readFleet(ctx)
	if err != nil {
		return err
	}
	out := inv.out
	out.emit(f.event())
	inv.summary = plural(len(f.machines), "machine") + " on the hub"
	if !out.json {
		inventory, err := inv.scan(ctx, lockWait, "", false)
		if err != nil {
			return err
		}
		here := buildReport(inventory, runtime.GOOS, cliVersion)
		others := slices.DeleteFunc(slices.Clone(f.machines), func(m fleetMachine) bool { return m.Machine.ID == here.Machine.ID })
		slices.SortStableFunc(others, func(a, b fleetMachine) int {
			return cmp.Or(compareBool(a.Machine.ID == cloudMachine, b.Machine.ID == cloudMachine), cmp.Compare(a.Machine.Label, b.Machine.Label))
		})
		out.print(out.paint(heading, plural(len(others)+1, "machine")))
		t := &table{}
		t.add(machineRow(out, here.Machine, here.reportContents, "", inv.env["TZ"])...)
		for _, m := range others {
			t.add(machineRow(out, m.Machine, m.reportContents, m.ChangedAt, inv.env["TZ"])...)
		}
		out.render(t, "")
		if len(others) == 0 {
			out.print("The hub holds no other machine.")
		}
	}
	for _, w := range f.warnings {
		out.warn(w)
	}
	return nil
}

// machineRow is one line of the machine list: the machine's label, when
// its branch last changed, how many skills its agent clients see, and its
// agent clients by name, from what its branch holds, held. changed is
// fleetMachine.ChangedAt, "" for this machine, whose row is a scan made
// now and says so. The cloud machine's skills are those its snapshot
// lists, since nothing has scanned it. A label and a client's name are
// another machine's words and are sanitised.
func machineRow(out *writer, m machineFacts, held reportContents, changed, tz string) []cell {
	name, when := out.paint(heading, sanitised(m.Label)), c("changed "+changedDate(changed, tz), plain)
	if changed == "" {
		name += " " + out.paint(tagStyle, "(this machine)")
		when = c("scanned now", muted)
	}
	skills := len(held.Skills)
	if m.ID == cloudMachine {
		skills = len(held.Library)
	}
	var agents []string
	for _, conf := range held.Configurations {
		agents = append(agents, sanitised(conf.Name))
	}
	clients := c(strings.Join(agents, ", "), plain)
	if len(agents) == 0 {
		clients = c("no agent clients", muted)
	}
	return []cell{c(name, plain), when, c(plural(skills, "skill"), noteStyle), clients}
}

// changedDate is when a machine's branch last changed as the text output
// prints it: iso, a fleetMachine.ChangedAt, to the minute, as historyDate
// prints a time, in the time zone tz names, or the local one when it names
// none Go can read.
func changedDate(iso, tz string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return sanitised(iso)
	}
	zone := time.Local
	if loc, err := time.LoadLocation(tz); tz != "" && err == nil {
		zone = loc
	}
	return historyDate(t.In(zone).Format(time.RFC3339))
}

// machineShow is agentx machine show: one machine of the fleet, as the hub
// holds it. Nothing of this machine's disk is read, and nothing is
// compared with it.
func (inv *invocation) machineShow(ctx context.Context, arg string) error {
	if arg == "" {
		return noMachineGiven()
	}
	f, err := inv.readFleet(ctx)
	if err != nil {
		return err
	}
	m, err := machineNamed(arg, f.machines)
	if err != nil {
		return err
	}
	out := inv.out
	out.emit(fleetEvent{event: newEvent("fleet"), Machines: []fleetMachine{m}})
	inv.summary = sanitised(m.Machine.Label) + " as the hub holds it at " + short(m.Commit)
	inv.printMachineShown(m)
	for _, w := range f.warnings {
		out.warn(w)
	}
	return nil
}

// printMachineShown writes one machine of the fleet: its facts as a
// key-value table, then one section per agent configuration with the
// skills its client sees, see shownSkills.
func (inv *invocation) printMachineShown(m fleetMachine) {
	out := inv.out
	t := &table{}
	t.add(c("Label", label), c(sanitised(m.Machine.Label), heading))
	t.add(c("Machine id", label), c(sanitised(m.Machine.ID), plain))
	if m.Machine.OS != "" {
		t.add(c("OS", label), c(sanitised(m.Machine.OS), plain))
	}
	if m.Machine.Agentx != "" {
		t.add(c("agentx", label), c(sanitised(m.Machine.Agentx), plain))
	}
	t.add(c("Last change", label), c(changedDate(m.ChangedAt, inv.env["TZ"]), plain))
	out.render(t, "")
	for _, conf := range m.Configurations {
		skills := shownSkills(m.reportContents, conf.ID)
		state := out.paint(okStyle, "enabled")
		if !conf.Enabled {
			state = out.paint(warnStyle, "disabled")
		}
		out.print("")
		out.print(out.paint(heading, sanitised(conf.Name)), " ", out.paint(muted, "("+sanitised(conf.ID)+")"), "  ", state, "  ", out.paint(noteStyle, plural(len(skills), "skill")))
		rows := &table{}
		for _, s := range skills {
			rows.add(s.row()...)
		}
		out.render(rows, "  ")
	}
	if len(m.Configurations) == 0 {
		out.print("")
		out.print(out.paint(muted, "No agent configurations."))
	}
}

// The places a skill a configuration sees comes from, as machine show
// names them.
const (
	fromLibrary   = "library"
	fromPlugin    = "plugin"
	fromDirectory = "skills directory"
)

// shownSkill is one skill a configuration of a machine sees, as machine
// show lists it: its name, where it comes from, the plugin that brings it
// when one does, and its library entry when it is the library's.
type shownSkill struct {
	name, from, plugin string
	library            *reportLibrarySkill
}

// row is the skill's line: its name, where it comes from, then, for a
// library skill, where it is published to, see sourceCell, and a mark when
// its library directory holds edits. Every word but agentx's own is
// another machine's and is sanitised.
func (s shownSkill) row() []cell {
	switch {
	case s.library != nil:
		l := s.library
		cells := []cell{c(sanitised(s.name), heading), c(fromLibrary, muted),
			sourceCell(l.ForkID != "", l.Source, l.Subpath, l.Upstream, l.UpstreamSubpath, l.Source)}
		if l.State == stateModified {
			cells = append(cells, c(stateModified, warnStyle))
		}
		return cells
	case s.plugin != "":
		return []cell{c(sanitised(s.name), heading), c(fromPlugin+" "+sanitised(s.plugin), tagStyle)}
	}
	return []cell{c(sanitised(s.name), heading), c(fromDirectory, muted)}
}

// shownSkills are the skills the configuration id sees on a machine, from
// what its report holds, held: every library skill placed in it or that
// it reads as a universal client, then every skill a plugin of it brings,
// then every other skill of its own skills directories. An occurrence the
// library explains, a placement with the library skill's content or a
// copy of it by its name, is the library skill's row and no row of its
// own. Each list is sorted by name. Pure.
func shownSkills(held reportContents, id string) []shownSkill {
	var library, plugins, own []shownSkill
	hashes, copies := map[string]bool{}, map[string]bool{} // what the library explains: content it holds, and names it keeps a copy of here
	for i := range held.Library {
		l := &held.Library[i]
		seen := slices.Contains(l.Universal, id)
		for _, p := range l.Placements {
			if p.Configuration == id {
				seen = true
				copies[l.Name] = copies[l.Name] || p.Kind == scan.KindCopy
			}
		}
		if seen {
			library = append(library, shownSkill{name: l.Name, from: fromLibrary, library: l})
			hashes[l.ContentHash] = true
		}
	}
	for _, s := range held.Skills {
		for _, o := range s.Occurrences {
			switch {
			case o.Configuration != id:
			case o.Plugin != "":
				plugins = append(plugins, shownSkill{name: s.Name, from: fromPlugin, plugin: o.Plugin})
			case hashes[s.ContentHash], o.Kind == scan.KindCopy && copies[s.Name]:
			default:
				own = append(own, shownSkill{name: s.Name, from: fromDirectory})
			}
		}
	}
	order := func(a, b shownSkill) int { return cmp.Or(cmp.Compare(a.name, b.name), cmp.Compare(a.plugin, b.plugin)) }
	same := func(a, b shownSkill) bool { return a.name == b.name && a.plugin == b.plugin }
	slices.SortStableFunc(plugins, order)
	slices.SortStableFunc(own, order)
	return slices.Concat(library, slices.CompactFunc(plugins, same), slices.CompactFunc(own, same))
}
