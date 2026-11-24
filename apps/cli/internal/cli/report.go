package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// A machine's inventory report lives on the hub, on the branch
// reports/<machine id>, whose tree holds one file, see gitx.ReportFile
// (ADR 0018). It is what the machine's last scan found, without what only
// describes that machine's disk, so that every other machine with the hub
// can show which skills this one has and which agent clients see them
// there, while it is off too. A report is written by the machine it
// describes and by no other: one commit on the tip its branch was read at,
// pushed leased on that tip and never forced, as a snapshot is. It is made
// to be read and never applied, and nothing in this file reads a snapshot.

// reportSchemaVersion is the version of the inventory report this agentx
// writes. It is the version of that document alone. A reader takes a
// report of this version or an earlier one, ignores a field it does not
// know, and skips a report of a later version.
const reportSchemaVersion = 1

// inventoryReport is the document a machine's report branch holds. It is a
// pure function of the machine's state: it carries no time, no counter and
// no id of one process, and every list is sorted, so two reports of an
// unchanged machine are byte-identical and the second is written nowhere.
//
// It leaves out every path, the machine settings, the MCP servers, whose
// commands, arguments and URLs can hold a secret, a skill's description
// and content, the ids that only mean something on the machine, an
// occurrence's scope, and what a view decides or another command reports:
// drift, update candidates, pending merges and warnings.
type inventoryReport struct {
	SchemaVersion int          `json:"schema_version"`
	Machine       machineFacts `json:"machine"`
	reportContents
}

// machineFacts name the machine a report describes. A computer's report
// carries all four; the cloud machine, which writes no report, has an id
// and a label alone.
type machineFacts struct {
	ID     string `json:"id"`
	Label  string `json:"label"`            // the effective label: the setting, or the hostname until it is set
	OS     string `json:"os,omitempty"`     // the operating system, as Go names it: darwin, linux or windows
	Agentx string `json:"agentx,omitempty"` // the version of the agentx that wrote the report
}

// reportContents is what a report says the machine holds, the part of it a
// fleet event carries for each machine as it is.
type reportContents struct {
	Configurations []reportConfiguration `json:"configurations"` // sorted by id
	Skills         []reportSkill         `json:"skills"`         // every skill an agent client sees, sorted by name, then content hash
	Library        []reportLibrarySkill  `json:"library"`        // every skill of the library, sorted by name
	Plugins        []reportPlugin        `json:"plugins"`        // sorted by name, marketplace, configuration, then version
}

// reportConfiguration is one agent configuration of the machine.
type reportConfiguration struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	ReadsLibrary bool   `json:"reads_library"`
}

// reportSkill is one content version of a skill some agent client of the
// machine sees: a skill node of the inventory, by its name and content
// hash, with where it is seen from.
type reportSkill struct {
	Name        string             `json:"name"`
	ContentHash string             `json:"content_hash"`
	Occurrences []reportOccurrence `json:"occurrences"` // each once, sorted by configuration, kind, then plugin
}

// reportOccurrence is one way a configuration sees a skill, without its
// path: what is on disk there, and the plugin that brings it when one
// does.
type reportOccurrence struct {
	Configuration string `json:"configuration"`
	Kind          string `json:"kind"` // symlink, copy or directory
	Plugin        string `json:"plugin,omitempty"`
}

// reportLibrarySkill is one skill of the machine's library: its library
// entry in the inventory without its placement paths, its drift, its
// update candidate and its pending merge. The entry of a skill of the
// cloud machine, read from its snapshot, carries no content hash and no
// state, since nothing has read the cloud machine's disk, and a logical id
// only when the snapshot names its fork id or its source and subpath.
type reportLibrarySkill struct {
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`                 // managed or unmanaged
	LogicalID       string            `json:"logical_id,omitempty"` // its logical asset, see scan.SkillLogicalID
	ForkID          string            `json:"fork_id,omitempty"`
	Source          string            `json:"source,omitempty"`
	Subpath         *string           `json:"subpath,omitempty"`
	Upstream        string            `json:"upstream,omitempty"`
	UpstreamSubpath *string           `json:"upstream_subpath,omitempty"`
	UpstreamCommit  string            `json:"upstream_commit,omitempty"`
	UpstreamTime    string            `json:"upstream_time,omitempty"`
	BaseHash        string            `json:"base_hash,omitempty"`
	ContentHash     string            `json:"content_hash,omitempty"` // what the library holds now
	State           string            `json:"state,omitempty"`        // current or modified
	Placements      []reportPlacement `json:"placements"`             // each once, sorted by configuration, mode, then kind
	Universal       []string          `json:"universal"`              // the universal clients, which see the skill whatever its placements, sorted
}

// reportPlacement is one way a configuration sees a library skill, without
// its path. Mode and kind are absent where nothing read them, which is the
// cloud machine's every placement.
type reportPlacement struct {
	Configuration string `json:"configuration"`
	Mode          string `json:"mode,omitempty"` // symlink, copy or library
	Kind          string `json:"kind,omitempty"` // symlink, copy, directory or library
}

// reportPlugin is one plugin installed in one configuration.
type reportPlugin struct {
	Name          string `json:"name"`
	Marketplace   string `json:"marketplace"`
	Version       string `json:"version"`
	Configuration string `json:"configuration"`
	Enabled       *bool  `json:"enabled,omitempty"` // only for a client that records it
}

// buildReport is the report of the machine inventory was scanned on, whose
// library the inventory lists, with the machine's operating system and the
// version of this agentx. Only what the user scope holds is reported: a
// skill inside a project is no part of a machine's report. Pure.
func buildReport(inventory scan.Inventory, os, version string) inventoryReport {
	r := inventoryReport{
		SchemaVersion: reportSchemaVersion,
		Machine:       machineFacts{ID: inventory.Machine.ID, Label: inventory.Machine.Label, OS: os, Agentx: version},
	}
	for _, c := range inventory.Configurations {
		r.Configurations = append(r.Configurations, reportConfiguration{ID: c.ID, Name: c.Name, Enabled: c.Enabled, ReadsLibrary: c.ReadsLibrary})
	}
	seen := map[[2]string]int{} // a skill's name and content hash to its place in r.Skills
	for _, s := range inventory.Skills {
		var occurrences []reportOccurrence
		for _, o := range s.Occurrences {
			if o.Scope == scan.ScopeUser {
				occurrences = append(occurrences, reportOccurrence{Configuration: o.Configuration, Kind: o.Kind, Plugin: o.Plugin})
			}
		}
		if len(occurrences) == 0 {
			continue
		}
		key := [2]string{s.Name, s.ContentHash}
		i, ok := seen[key]
		if !ok {
			i = len(r.Skills)
			seen[key] = i
			r.Skills = append(r.Skills, reportSkill{Name: s.Name, ContentHash: s.ContentHash})
		}
		r.Skills[i].Occurrences = append(r.Skills[i].Occurrences, occurrences...)
	}
	for _, e := range inventory.Library {
		skill := reportLibrarySkill{
			Name: e.Name, Kind: e.Kind, LogicalID: e.LogicalID, ForkID: e.ForkID,
			Source: e.Source, Subpath: e.Subpath, Upstream: e.Upstream, UpstreamSubpath: e.UpstreamSubpath,
			UpstreamCommit: e.UpstreamCommit, UpstreamTime: e.UpstreamTime, BaseHash: e.BaseHash,
			ContentHash: e.ContentHash, State: e.State, Universal: slices.Clone(e.Universal),
		}
		for _, p := range e.Placements {
			skill.Placements = append(skill.Placements, reportPlacement{Configuration: p.Configuration, Mode: p.Mode, Kind: p.Kind})
		}
		r.Library = append(r.Library, skill)
	}
	for _, p := range inventory.Plugins {
		r.Plugins = append(r.Plugins, reportPlugin{Name: p.Name, Marketplace: p.Marketplace, Version: p.Version, Configuration: p.Configuration, Enabled: p.Enabled})
	}
	r.settle()
	return r
}

// settle puts c in the one order and form a report has, whatever order its
// parts were gathered or written in: every list sorted, each entry once,
// and a list with nothing in it empty rather than absent. Pure.
func (c *reportContents) settle() {
	c.Configurations = sortedOnce(c.Configurations, func(a, b reportConfiguration) int {
		return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Name, b.Name), compareBool(a.Enabled, b.Enabled), compareBool(a.ReadsLibrary, b.ReadsLibrary))
	})
	for i := range c.Skills {
		c.Skills[i].Occurrences = sortedOnce(c.Skills[i].Occurrences, func(a, b reportOccurrence) int {
			return cmp.Or(cmp.Compare(a.Configuration, b.Configuration), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Plugin, b.Plugin))
		})
	}
	slices.SortStableFunc(c.Skills, func(a, b reportSkill) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ContentHash, b.ContentHash))
	})
	if c.Skills == nil {
		c.Skills = []reportSkill{}
	}
	for i := range c.Library {
		c.Library[i].Placements = sortedOnce(c.Library[i].Placements, func(a, b reportPlacement) int {
			return cmp.Or(cmp.Compare(a.Configuration, b.Configuration), cmp.Compare(a.Mode, b.Mode), cmp.Compare(a.Kind, b.Kind))
		})
		c.Library[i].Universal = sortedOnce(c.Library[i].Universal, cmp.Compare[string])
	}
	slices.SortStableFunc(c.Library, func(a, b reportLibrarySkill) int { return cmp.Compare(a.Name, b.Name) })
	if c.Library == nil {
		c.Library = []reportLibrarySkill{}
	}
	c.Plugins = sortedOnce(c.Plugins, func(a, b reportPlugin) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Marketplace, b.Marketplace),
			cmp.Compare(a.Configuration, b.Configuration), cmp.Compare(a.Version, b.Version), cmp.Compare(turnedOn(a.Enabled), turnedOn(b.Enabled)))
	})
}

// sortedOnce is list sorted by compare with each entry once, two entries
// compare calls equal being one, and never nil.
func sortedOnce[T any](list []T, compare func(a, b T) int) []T {
	out := slices.Clone(list)
	slices.SortStableFunc(out, compare)
	out = slices.CompactFunc(out, func(a, b T) bool { return compare(a, b) == 0 })
	if out == nil {
		out = []T{}
	}
	return out
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	}
	return 1
}

// turnedOn orders what a client records of a plugin: nothing, off, on.
func turnedOn(enabled *bool) int {
	switch {
	case enabled == nil:
		return 0
	case !*enabled:
		return 1
	}
	return 2
}

// reportDocument is the bytes of the report file of the machine inventory
// was scanned on: indented JSON with a newline after it, as a snapshot is
// written.
func reportDocument(inventory scan.Inventory) ([]byte, error) {
	data, err := json.MarshalIndent(buildReport(inventory, runtime.GOOS, cliVersion), "", "  ")
	if err != nil {
		return nil, fail(exitInternal, err.Error(), "")
	}
	return append(data, '\n'), nil
}

// decodeReport reads data, the report file of the branch of the machine
// id, and says why a view cannot use it, "" when it can: a file that is no
// JSON object, one that carries no schema version, one a later agentx
// wrote in a version this one does not know, and one that names another
// machine than its branch. A field this version does not know is ignored,
// and every list the file left out is read empty. Pure.
func decodeReport(id string, data []byte) (r inventoryReport, why string) {
	if err := json.Unmarshal(data, &r); err != nil {
		return inventoryReport{}, "its " + gitx.ReportFile + " is not a report: " + sanitised(err.Error())
	}
	switch {
	case r.SchemaVersion < 1:
		return inventoryReport{}, "its " + gitx.ReportFile + " carries no schema_version"
	case r.SchemaVersion > reportSchemaVersion:
		return inventoryReport{}, fmt.Sprintf("its %s is a report of schema version %d, and this agentx reads version %d; upgrade agentx here to read it",
			gitx.ReportFile, r.SchemaVersion, reportSchemaVersion)
	case r.Machine.ID != id:
		return inventoryReport{}, "its " + gitx.ReportFile + " names another machine, " + clipped(sanitised(r.Machine.ID))
	}
	r.settle()
	return r, ""
}

// reportBranch is a machine's report branch on the hub as one read found
// it: the store, the hub's git remote and URL, the machine id, and the
// commit the branch held, "" when the hub holds no such branch. A tip is
// in the store, and its remote-tracking ref points at it.
type reportBranch struct {
	gitDir, remote, url, id string
	tip                     string
}

// name is the branch's short name, as every line names it.
func (b reportBranch) name() string { return gitx.ReportBranch(b.id) }

// written is the line that says a report was pushed to the branch, which
// holds commit now.
func (b reportBranch) written(commit string) string {
	return "wrote this machine's report to " + b.name() + " on the hub at " + short(commit)
}

// readReportBranch reads the report branch of the machine id on the hub,
// refusing, exit code 6, a machine with no hub set, see hubBranches.
func (inv *invocation) readReportBranch(ctx context.Context, id string) (reportBranch, error) {
	gitDir, entry, remote, err := inv.hubRemote(ctx)
	if err != nil {
		return reportBranch{}, err
	}
	b := reportBranch{gitDir: gitDir, remote: remote, url: entry.URL, id: id}
	held, err := inv.hubBranches(ctx, gitDir, remote, entry.URL, b.name())
	if err != nil {
		return reportBranch{}, err
	}
	b.tip = held[b.name()].Commit
	return b, nil
}

// hubBranches reads what the hub holds of the branches the patterns name,
// see gitx.BranchTips, in one ls-remote, and brings the store's
// remote-tracking refs in step with it, see heldBranches. A hub git cannot
// reach is exit code 3. Nothing here runs under the lock of agentx home.
func (inv *invocation) hubBranches(ctx context.Context, gitDir, remote, url string, patterns ...string) (map[string]gitx.Tracked, error) {
	tips, err := inv.git.BranchTips(ctx, gitDir, remote, patterns...)
	if err != nil {
		return nil, unreachableRemote(url, err)
	}
	return inv.heldBranches(ctx, gitDir, remote, url, tips, nil, patterns...)
}

// heldBranches brings the store's remote-tracking refs in step with tips,
// what one ls-remote said the hub holds of the branches the patterns name:
// one fetch of the branches whose tip is not the one their remote-tracking
// ref holds, which is none when nothing moved, and the deletion of the
// remote-tracking ref of every report branch the hub no longer holds. What
// it returns is each branch the hub holds, by its short name, with the
// commit now in the store and when it was committed. before, when it is
// not nil, runs ahead of the fetch, and only when there is one: the serve
// child writes the hub's remote there, which a command has done before it
// asks. A fetch that fails is exit code 3.
func (inv *invocation) heldBranches(ctx context.Context, gitDir, remote, url string, tips map[string]string, before func() error, patterns ...string) (map[string]gitx.Tracked, error) {
	tracked, err := inv.git.TrackedBranches(ctx, gitDir, remote, patterns...)
	if err != nil {
		return nil, storeFailure(err)
	}
	var moved, gone []string
	for branch, tip := range tips {
		if tracked[branch].Commit != tip {
			moved = append(moved, branch)
		}
	}
	for branch := range tracked {
		if _, report := gitx.ReportMachine(branch); report && tips[branch] == "" {
			gone = append(gone, branch)
		}
	}
	slices.Sort(moved)
	slices.Sort(gone)
	if len(moved) > 0 && before != nil {
		if err := before(); err != nil {
			return nil, storeFailure(err)
		}
	}
	if err := inv.git.FetchBranches(ctx, gitDir, remote, moved); err != nil {
		return nil, unreachableRemote(url, err)
	}
	if err := inv.git.DropTracked(ctx, gitDir, remote, gone); err != nil {
		return nil, storeFailure(err)
	}
	if len(moved) > 0 {
		if tracked, err = inv.git.TrackedBranches(ctx, gitDir, remote, patterns...); err != nil {
			return nil, storeFailure(err)
		}
	}
	held := make(map[string]gitx.Tracked, len(tips))
	for branch := range tips {
		if t, ok := tracked[branch]; ok {
			held[branch] = t
		}
	}
	return held, nil
}

// knownReport is this machine's report branch on a hub as the store knows
// it, with no call to the hub: the branch's short name, and the commit its
// remote-tracking ref holds, which this machine's last report, or its last
// read of the hub's machines, left there. A tip of "" is a hub this machine
// knows no report of its own on.
type knownReport struct {
	branch, tip string
}

// knownReportOn reads this machine's report branch on the hub whose git
// remote is called remote from the store, see knownReport. A machine with
// no id yet has written no report.
func (inv *invocation) knownReportOn(ctx context.Context, gitDir, remote string) (knownReport, error) {
	id, err := home.KnownMachineID(inv.dirs.Home, inv.env)
	if err != nil || id == "" {
		return knownReport{}, err
	}
	branch := gitx.ReportBranch(id)
	tracked, err := inv.git.TrackedBranches(ctx, gitDir, remote, branch)
	if err != nil {
		return knownReport{}, storeFailure(err)
	}
	return knownReport{branch: branch, tip: tracked[branch].Commit}, nil
}

// dropReport deletes this machine's report r from the repository at url, a
// hub this machine has just disconnected from: only a machine deletes its
// own report, and it does so when it leaves the hub (ADR 0018). The
// settings and the store name the hub no longer, so the branch is deleted
// at its URL, in the one push a branch of the hub is deleted with, see
// gitx.DeleteRemoteBranch, leased on the tip the store knew. A hub this
// machine knows no report of its own on is not called at all.
//
// The machine is disconnected whatever the hub answers: a deletion git
// could not make, or one the repository rejected, is a warning that names
// the branch left there and the git command that deletes it.
func (inv *invocation) dropReport(ctx context.Context, gitDir, url string, r knownReport) {
	if r.tip == "" {
		return
	}
	out := inv.out
	s, err := inv.git.DeleteRemoteBranch(ctx, gitDir, url, r.branch, r.tip)
	var why string
	switch {
	case err != nil:
		why = trimGit(err.Error())
	case s.Rejected():
		why = "its deletion was rejected: " + sanitised(s.Why())
	default:
		out.done("deleted this machine's report, " + out.paint(heading, r.branch) + ", from " + shownURL(url))
		return
	}
	out.warnWith(shownURL(url)+" still holds "+r.branch+", this machine's report: "+why,
		"delete it with 'git push "+shellWord(shownURL(url))+" --delete "+r.branch+"'")
}

// writeReport writes data, a report document, as the one file of a commit
// on the tip of b and pushes it to b's branch, leased on that tip, "" for
// a branch that must not exist yet, and never forced, then points the
// branch's remote-tracking ref at it. A report the tip holds already is
// written nowhere: commit is the tip then and pushed false. The commit is
// no operation: it carries the machine and the writer's identity, as every
// commit agentx writes does, see forkWriter, and nothing else.
//
// A branch another writer moved since it was read is exit code 6: nothing
// is pushed, and the next report reads the branch again. A push the host
// declined is exit code 6; one this machine may not make is exit code 3,
// which a read-only credential is, and so is a push git could not make or
// whose answer is not known.
func (inv *invocation) writeReport(ctx context.Context, b reportBranch, data []byte) (commit string, pushed bool, err error) {
	tree, err := inv.git.FileTree(ctx, b.gitDir, gitx.ReportFile, data)
	if err != nil {
		return "", false, storeFailure(err)
	}
	var parents []string
	if b.tip != "" {
		held, err := inv.git.TreeOf(ctx, b.gitDir, b.tip)
		if err != nil {
			return "", false, storeFailure(err)
		}
		if held == tree {
			return b.tip, false, nil
		}
		parents = []string{b.tip}
	}
	w, err := inv.newForkWriter(ctx, b.gitDir)
	if err != nil {
		return "", false, err
	}
	commit, err = w.commit(ctx, tree, parents, forkMessage{subject: "Report " + sanitised(w.label)})
	if err != nil {
		return "", false, storeFailure(err)
	}
	branch := b.name()
	s, found, stderr, exit, err := inv.git.PushBranch(ctx, b.gitDir, b.remote, commit, branch, b.tip)
	if err != nil {
		return "", false, unreachableRemote(b.url, err)
	}
	class, reason := gitx.ClassifyPush(s, found, stderr, exit)
	const run = "run 'agentx machine report' again"
	switch {
	case class == gitx.PushPushed, class == gitx.PushUpToDate:
		if err := inv.git.SetTracked(ctx, b.gitDir, b.remote, branch, commit); err != nil {
			return "", false, storeFailure(err)
		}
		return commit, true, nil
	case class == gitx.PushMoved:
		return "", false, fail(exitRefused, branch+" on the hub changed since it was read, so nothing was pushed",
			run+": it reads "+branch+" again first")
	case class == gitx.PushDeclined:
		return "", false, fail(exitRefused, "the hub declined "+branch+": "+sanitised(reason),
			"once the hub takes it, "+run)
	case class == gitx.PushDenied:
		return "", false, fail(exitSource, "the hub "+shownURL(b.url)+" did not let this machine push "+branch+": "+sanitised(reason),
			deniedHint(gitx.DeniedFix(reason))+", then "+run)
	case found || exit == 0:
		return "", false, fail(exitSource, "whether the hub took "+branch+" is not known: "+sanitised(reason), run)
	}
	return "", false, unreachableRemote(b.url, errors.New(reason))
}

// machineReportEvent reports what a report of this machine left on the hub:
// one agentx machine report made, or one serve pushed.
type machineReportEvent struct {
	event
	Machine string `json:"machine"` // this machine's id
	Commit  string `json:"commit"`  // the tip of reports/<machine> now: the commit pushed, or the one that held this report already
	Pushed  bool   `json:"pushed"`  // false when the branch held this report already
}

// noReportUnlisted is why a machine whose library could not be listed,
// since git cannot read the store, writes no report, exit code 8: a report
// with an empty library would say the machine holds no skill of its own.
// unlisted is the store's failure.
func noReportUnlisted(unlisted *failure) error {
	return fail(exitStore, "no report was written, since the library could not be listed: "+unlisted.message, unlisted.hint)
}

// machineReport is agentx machine report: it scans this machine and writes
// its report to the hub. The hub is required, and read before the scan, so
// that a machine with none is refused before anything is read. A machine
// whose library cannot be listed writes no report, see noReportUnlisted.
func (inv *invocation) machineReport(ctx context.Context) error {
	if _, _, ok, err := inv.hubSource(); err != nil || !ok {
		if err == nil {
			err = noRemoteRefusal()
		}
		return err
	}
	inventory, unlisted, err := inv.reportedInventory(ctx, lockWait)
	if err != nil {
		return err
	}
	if unlisted != nil {
		return noReportUnlisted(unlisted)
	}
	data, err := reportDocument(inventory)
	if err != nil {
		return err
	}
	b, err := inv.readReportBranch(ctx, inventory.Machine.ID)
	if err != nil {
		return err
	}
	commit, pushed, err := inv.writeReport(ctx, b, data)
	if err != nil {
		return err
	}
	inv.out.emit(machineReportEvent{event: newEvent("machine_report"), Machine: b.id, Commit: commit, Pushed: pushed})
	done := b.written(commit)
	if !pushed {
		done = b.name() + " on the hub holds this report already, at " + short(commit) + ", so nothing was pushed"
	}
	inv.out.done(done)
	inv.summary = done
	return nil
}
