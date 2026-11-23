package cli

import (
	"context"
	"errors"
	"slices"

	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// The cloud machine writes no report: it has no scan, and a cloud session
// holds a read-only token (ADR 0011). Its snapshot on machines/cloud is its
// whole state, so its entry of the fleet is read from that, here and
// nowhere else: nothing but this file knows that a machine of the fleet
// can come from a snapshot.

// cloudFleetMachine reads the cloud machine's entry of the fleet from the
// snapshot its branch b holds. why is the warning of a snapshot no view
// can use, anything an import would refuse of it, "" when it can; a git
// that could not read it is one too, unless the run was stopped.
func (inv *invocation) cloudFleetMachine(ctx context.Context, b machineBranch) (m fleetMachine, why string, err error) {
	snap, _, err := inv.branchSnapshot(ctx, b)
	var f *failure
	switch {
	case err == nil:
		return cloudFleet(snap), "", nil
	case errors.As(err, &f) && ctx.Err() == nil && !inv.git.StoppedChild():
		return fleetMachine{}, "the " + cloudMachine + " machine is not listed: " + f.message, nil
	}
	return fleetMachine{}, "", err
}

// cloudFleet is the cloud machine as a machine of the fleet, from its
// snapshot. It has an id and a label, both cloud, and no operating system
// or agentx version, which nothing reports.
//
// Its configurations are the snapshot's and every one a skill is placed
// in, named as the registry names its client. Each reads the library when
// its client does on the cloud machine, see cloudUniversal, whatever the
// computer that saved the snapshot found on its own disk, and one the
// snapshot does not list is enabled unless the snapshot's settings disable
// it.
//
// It has no skills, since nothing has scanned it. Its library is the
// snapshot's skills: each with its logical asset when the entry names its
// fork id or its source and subpath, the base version when the entry is
// exact, one placement for each configuration it is placed in, with no
// mode and no kind, and the universal clients among the configurations. No
// entry has a content hash or a state. Pure.
func cloudFleet(snap snapshot) fleetMachine {
	m := fleetMachine{Machine: machineFacts{ID: cloudMachine, Label: cloudMachine}}
	listed := map[string]bool{}
	for _, c := range snap.Configurations {
		listed[c.ID] = true
		m.Configurations = append(m.Configurations, reportConfiguration{ID: c.ID, Name: c.Name, Enabled: c.Enabled, ReadsLibrary: cloudUniversal(c.ID)})
	}
	for _, s := range snap.Skills {
		for _, id := range s.Placements {
			if listed[id] {
				continue
			}
			listed[id] = true
			name := id
			if client, ok := scan.Lookup(id); ok {
				name = client.Name()
			}
			m.Configurations = append(m.Configurations, reportConfiguration{
				ID: id, Name: name, Enabled: !slices.Contains(snap.Settings.DisabledConfigurations, id), ReadsLibrary: cloudUniversal(id),
			})
		}
	}
	var universal []string
	for _, c := range m.Configurations {
		if c.ReadsLibrary {
			universal = append(universal, c.ID)
		}
	}
	for _, s := range snap.Skills {
		e := reportLibrarySkill{
			Name: s.Name, Kind: s.Kind, ForkID: s.ForkID, Source: s.Source, Subpath: s.Subpath,
			Upstream: s.Upstream, UpstreamSubpath: s.UpstreamSubpath, Universal: slices.Clone(universal),
		}
		if s.ForkID != "" || s.Source != "" && s.Subpath != nil {
			e.LogicalID = scan.SkillLogicalID(s.ForkID, s.Source, s.Subpath, "")
		}
		if s.Commit != "" {
			e.UpstreamCommit, e.BaseHash = s.UpstreamCommit, s.BaseHash
		}
		for _, id := range s.Placements {
			e.Placements = append(e.Placements, reportPlacement{Configuration: id})
		}
		m.Library = append(m.Library, e)
	}
	for _, p := range snap.Plugins {
		m.Plugins = append(m.Plugins, reportPlugin(p))
	}
	m.settle()
	return m
}
