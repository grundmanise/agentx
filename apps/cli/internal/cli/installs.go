package cli

import (
	"context"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// recordInstalls notes in installs.json that the skills of added entered
// the library of this machine and the skills of removed left it, now, see
// home.RecordInstalls. Every command that adds a skill to the library or
// takes one off the machine calls it inside the hold of the lock its
// mutation ran under, once that mutation is applied, so that a date never
// claims a change the machine does not hold. The dates are a record for
// the inventory to report and nothing reads them to decide anything, so a
// file that cannot be written is a warning: the skill was added or
// removed all the same.
func (inv *invocation) recordInstalls(added, removed []home.Install) {
	if err := home.RecordInstalls(inv.dirs.Home, time.Now(), added, removed); err != nil {
		inv.out.warn("could not record when skills were added or removed: " + err.Error())
	}
}

// recordedInstall is the logical asset of the skill rec is the lineage
// of, for recordInstalls: a managed skill's source and subpath, as its
// import commit records them, and a fork's id, once ReadForks or Walk has
// read it. A record that names neither is the zero Install, which
// recordInstalls passes over.
func recordedInstall(rec lineage.Record) home.Install {
	switch {
	case rec.Kind == lineage.KindManaged && rec.HasImport && rec.Import.Source != "":
		return home.SourceInstall(rec.Name, rec.Import.Source, rec.Import.Path)
	case rec.Kind == lineage.KindFork && rec.Fork != nil && rec.Fork.ID != "":
		return home.ForkInstall(rec.Name, rec.Fork.ID)
	}
	return home.Install{}
}

// importedInstall is the logical asset of the managed skill called name
// whose import branch holds commit, read from that commit's trailers with
// one git log, or the zero Install when the commit records no source. A
// removal reads it before its mutation deletes the branch.
func (inv *invocation) importedInstall(ctx context.Context, gitDir, name, commit string) home.Install {
	message, err := inv.git.Isolated(ctx, gitDir, "log", "-1", "--format=%B", commit)
	if err != nil {
		inv.out.debugf("%s: %v", name, err)
		return home.Install{}
	}
	imported, err := lineage.Parse(message)
	if err != nil || imported.Source == "" {
		return home.Install{}
	}
	return home.SourceInstall(name, imported.Source, imported.Path)
}

// forkInstall is the logical asset of the fork called name whose branch is
// at tip, its fork id read by one walk of its history, or the zero Install
// when the history records none.
func (inv *invocation) forkInstall(ctx context.Context, gitDir, name, tip string) home.Install {
	walked, err := lineage.Walk(ctx, inv.git, gitDir, []string{tip})
	if err != nil {
		inv.out.debugf("%s: %v", name, err)
		return home.Install{}
	}
	l := walked[tip]
	return recordedInstall(lineage.Record{Name: name, Kind: lineage.KindFork, Fork: &l})
}
