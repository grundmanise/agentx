package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
)

// A machine's snapshot can live on the account remote, on the branch
// machines/<machine id>, whose tree holds one file, the snapshot, see
// gitx.SnapshotFile (ADR 0011). Every write is a command the user runs:
// export naming a machine writes this machine's snapshot there, and skill
// place and skill remove naming the cloud machine change one entry of the
// cloud machine's. Each writes one commit on the tip the branch was read
// at and pushes it leased on that tip, never forced, so that a write over
// another writer's change is refused and the writer reads the branch
// again. Import naming a machine reads the branch, fetching that one ref
// and nothing else, which a read-only credential can do. A fetch of the
// account remote's skills never fetches a machine's branch.

// cloudMachine is the machine id of the cloud machine: a fixed name, not
// one derived on a computer, so that every cloud virtual machine reads the
// same branch, whoever runs it.
const cloudMachine = "cloud"

// computerID is the shape of a computer's machine id, see home.MachineID.
var computerID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// machineArg holds the --machine of export and import to a machine id:
// the cloud machine's, or a computer's.
func machineArg(id string) error {
	if id == cloudMachine || computerID.MatchString(id) {
		return nil
	}
	return fail(exitUsage, clipped(sanitised(id))+" is not a machine id",
		"give "+cloudMachine+" for the cloud machine, or the id 'agentx machine' prints on a computer")
}

// cloudOnly holds the --machine of skill place and skill remove to the
// cloud machine. A computer's disk is its state, so its skills are placed
// and removed there, and never through its branch.
func cloudOnly(id string) error {
	if id == cloudMachine {
		return nil
	}
	return fail(exitUsage, "--machine accepts only "+cloudMachine+": a computer's skills are placed on its own disk",
		"run the command on that computer without --machine, or give --machine "+cloudMachine)
}

// machineBranch is a machine's branch on the account remote as one read
// found it: the account repo, the account remote's git remote and URL, the
// machine id, and the commit the branch held, "" when the remote holds no
// such branch. A tip is in the account repo, fetched into its
// gitx.MachineTracking ref.
type machineBranch struct {
	gitDir, remote, url, id string
	tip                     string
}

// name is the branch's short name, as every line names it.
func (b machineBranch) name() string { return gitx.MachineBranch(b.id) }

// readMachineBranch reads the branch of the machine id on the account
// remote, refusing, exit code 6, a machine with no account remote set: one
// ls-remote of that ref, then, when the remote holds it, one fetch of that
// ref alone. A remote git cannot reach is exit code 3.
func (inv *invocation) readMachineBranch(ctx context.Context, id string) (machineBranch, error) {
	gitDir, entry, remote, err := inv.accountRemote(ctx)
	if err != nil {
		return machineBranch{}, err
	}
	b := machineBranch{gitDir: gitDir, remote: remote, url: entry.URL, id: id}
	if b.tip, err = inv.git.MachineTip(ctx, gitDir, remote, id); err != nil {
		return machineBranch{}, unreachableRemote(entry.URL, err)
	}
	if b.tip == "" {
		return b, nil
	}
	if err := inv.git.FetchMachine(ctx, gitDir, remote, id); err != nil {
		return machineBranch{}, unreachableRemote(entry.URL, err)
	}
	return b, nil
}

// branchSnapshot reads the snapshot the tip of b holds and refuses
// anything import would refuse of a file, see decodeSnapshot, and a tip
// whose tree holds no snapshot file, exit code 6. A branch with no tip
// holds no snapshot: found is false.
func (inv *invocation) branchSnapshot(ctx context.Context, b machineBranch) (snap snapshot, found bool, err error) {
	if b.tip == "" {
		return snapshot{}, false, nil
	}
	data, size, held, err := inv.git.SnapshotAt(ctx, b.gitDir, b.tip, documentLimit)
	switch {
	case err != nil:
		return snapshot{}, false, accountRepoFailure(err)
	case !held:
		return snapshot{}, false, fail(exitRefused, b.name()+" on the account remote holds no "+gitx.SnapshotFile,
			"write a snapshot to it with 'agentx export --machine "+b.id+"'")
	case size > documentLimit:
		return snapshot{}, false, fail(exitRefused,
			fmt.Sprintf("%s on the account remote holds a snapshot larger than %d bytes, which is more than any machine's settings and skills come to", b.name(), documentLimit),
			"write a snapshot to it with 'agentx export --machine "+b.id+"'")
	}
	snap, err = decodeSnapshot(b.name(), data)
	return snap, err == nil, err
}

// importMachine is the snapshot import --machine reads, and the name its
// lines give it, the branch's: it is what the rest of the import reads, as
// the snapshot of a file is. A machine whose branch the account remote
// does not hold is exit code 5.
func (inv *invocation) importMachine(ctx context.Context, id string) (snapshot, string, error) {
	b, err := inv.readMachineBranch(ctx, id)
	if err != nil {
		return snapshot{}, "", err
	}
	snap, found, err := inv.branchSnapshot(ctx, b)
	if err != nil {
		return snapshot{}, "", err
	}
	if !found {
		return snapshot{}, "", fail(exitNotFound, "the account remote holds no "+b.name(),
			"write a snapshot there with 'agentx export --machine "+id+"' on the machine it is to come from")
	}
	return snap, b.name(), nil
}

// pushSnapshot writes snap as the one file of a commit on the tip of b and
// pushes it to b's branch, leased on that tip, "" for a branch that must
// not exist yet, and never forced. A snapshot the tip holds already is
// written nowhere: commit is the tip then and pushed false. subject is the
// commit's subject; the writer's identity and machine are those of every
// commit agentx writes, see forkWriter. again is the command the hints of
// a refused push name.
//
// A branch another writer moved since it was read is exit code 6, the hint
// saying to run the command again, which reads it again; a push the host
// declined is exit code 6; one this machine may not make is exit code 3,
// which a read-only credential is, and so is a push git could not make or
// whose answer is not known.
func (inv *invocation) pushSnapshot(ctx context.Context, b machineBranch, snap snapshot, subject, again string) (commit string, pushed bool, err error) {
	snap.SchemaVersion = snapshotSchemaVersion
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", false, fail(exitInternal, err.Error(), "")
	}
	tree, err := inv.git.SnapshotTree(ctx, b.gitDir, append(data, '\n'))
	if err != nil {
		return "", false, accountRepoFailure(err)
	}
	var parents []string
	if b.tip != "" {
		held, err := inv.git.TreeOf(ctx, b.gitDir, b.tip)
		if err != nil {
			return "", false, accountRepoFailure(err)
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
	commit, err = w.commit(ctx, tree, parents, forkMessage{subject: subject})
	if err != nil {
		return "", false, accountRepoFailure(err)
	}
	branch := b.name()
	s, found, stderr, exit, err := inv.git.PushBranch(ctx, b.gitDir, b.remote, commit, branch, b.tip)
	if err != nil {
		return "", false, unreachableRemote(b.url, err)
	}
	class, reason := gitx.ClassifyPush(s, found, stderr, exit)
	run := "run '" + again + "' again"
	switch {
	case class == gitx.PushPushed, class == gitx.PushUpToDate:
		return commit, true, nil
	case class == gitx.PushMoved:
		return "", false, fail(exitRefused, branch+" on the account remote changed since it was read, so nothing was pushed",
			run+": it reads "+branch+" again first; agentx never forces a push")
	case class == gitx.PushDeclined:
		return "", false, fail(exitRefused, "the account remote declined "+branch+": "+sanitised(reason),
			"agentx never forces a push; once the account remote takes it, "+run)
	case class == gitx.PushDenied:
		return "", false, fail(exitSource, "the account remote "+shownURL(b.url)+" did not let this machine push "+branch+": "+sanitised(reason),
			deniedHint(gitx.DeniedFix(reason))+", then "+run)
	case found || exit == 0:
		return "", false, fail(exitSource, "whether the account remote took "+branch+" is not known: "+sanitised(reason), run)
	}
	return "", false, unreachableRemote(b.url, errors.New(reason))
}

// exportToMachine writes this machine's snapshot to the branch of the
// machine id on the account remote: its own as a backup, or another
// machine's to seed it. The account remote is required, and read before
// the scan, so that a machine with none is refused before anything is
// read.
func (inv *invocation) exportToMachine(ctx context.Context, id string) error {
	if _, _, ok, err := inv.accountSource(); err != nil || !ok {
		if err == nil {
			err = noRemoteRefusal()
		}
		return err
	}
	snap, err := inv.snapshot(ctx)
	if err != nil {
		return err
	}
	b, err := inv.readMachineBranch(ctx, id)
	if err != nil {
		return err
	}
	label := cloudMachine
	if snap.Machine != nil {
		label = snap.Machine.Label
	}
	commit, pushed, err := inv.pushSnapshot(ctx, b, snap, "Export "+sanitised(label), "agentx export --machine "+id)
	if err != nil {
		return err
	}
	inv.out.emit(exportEvent{event: newEvent("export"), Machine: id, Commit: commit, Skills: len(snap.Skills)})
	out := inv.out
	where := b.name() + " on the account remote"
	done := "exported the settings and " + plural(len(snap.Skills), "skill") + " to " + where + " at " + short(commit)
	if !pushed {
		done = where + " holds this snapshot already, at " + short(commit) + ", so nothing was pushed"
	}
	out.done(done)
	out.print("  ", out.paint(muted, "the snapshot names each skill's source, version and placements, and holds no skill content"))
	inv.summary = done
	return nil
}

// machineSkillEvent reports what skill place or skill remove naming a
// machine left of one skill's entry in that machine's snapshot.
type machineSkillEvent struct {
	event
	Machine    string   `json:"machine"`    // the machine id, cloud
	Name       string   `json:"name"`       // the skill's name
	Placements []string `json:"placements"` // the configurations the entry places it in now, sorted; [] for an entry with none or none left
	Removed    bool     `json:"removed"`    // the entry is gone from the snapshot
	Commit     string   `json:"commit"`     // the tip of the machine's branch now: the commit pushed, or the one that held this already
}

// placeOnMachine is skill place naming the cloud machine: it commits the
// skill's entry, which follows the latest version, with the configurations
// to names, or every one this machine enables, added to its placements, to
// the cloud machine's snapshot, and changes nothing on this machine's
// disk. The entry is the skill as this machine's library knows it, or, for
// a skill this machine does not hold, as the snapshot already lists it. A
// branch the account remote does not hold yet is born with the settings of
// a fresh home and this machine's account remote, as a snapshot always
// holds its machine's settings.
func (inv *invocation) placeOnMachine(ctx context.Context, id, name string, to []string, asCopy, force bool) error {
	if err := cloudOnly(id); err != nil {
		return err
	}
	if asCopy || force {
		return fail(exitUsage, "--copy and --force change this machine's disk, and --machine changes none",
			"drop them to place the skill on the "+cloudMachine+" machine")
	}
	for _, c := range to {
		if !configurationID(c) {
			return fail(exitUsage, clipped(sanitised(c))+" is not a configuration id", "give a configuration id, such as claude-code")
		}
		// The cloud machine's configurations are not detected here, so an id
		// is held to the registry instead: a typo, or the reserved
		// universal, would otherwise be pushed as a placement no client
		// ever reads.
		if _, ok := scan.Lookup(c); !ok {
			return fail(exitNotFound, fmt.Sprintf("configuration %q is not one agentx knows", c),
				"give the configuration id of an agent client, such as claude-code")
		}
	}
	if why := nameRefusal(name); why != "" {
		return fail(exitUsage, fmt.Sprintf("%q %s", clipped(name), why), "give the name of a skill")
	}
	here, err := inv.loadSettings()
	if err != nil {
		return err
	}
	ids := slices.Clone(to)
	if len(ids) == 0 {
		for _, t := range inv.detectedTargets() {
			if !containsString(here.DisabledConfigurations, t.id) {
				ids = append(ids, t.id)
			}
		}
		if len(ids) == 0 {
			return fail(exitRefused, "no configuration is enabled on this machine, so there is none to place "+name+" in",
				"name the configurations with --to")
		}
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	b, err := inv.readMachineBranch(ctx, id)
	if err != nil {
		return err
	}
	snap, found, err := inv.branchSnapshot(ctx, b)
	if err != nil {
		return err
	}
	if !found {
		account, _ := accountEntry(here) // set, or readMachineBranch had refused
		snap.Settings = home.DefaultSettings()
		snap.Settings.SetSource(account)
		forgetAccess(&snap.Settings)
	}
	entry, err := inv.machineEntry(sc, snap, name)
	if err != nil {
		return err
	}
	placeInSnapshot(&snap, entry, ids)
	placed := snapshotEntry(snap, name)
	command := skillCommand("place", name, append(placeFlags(to, false), "--machine", id)...)
	commit, pushed, err := inv.pushSnapshot(ctx, b, snap, "Place "+name+" in "+strings.Join(placed.Placements, ", "), command)
	if err != nil {
		return err
	}
	inv.out.emit(machineSkillEvent{event: newEvent("machine_skill"), Machine: id, Name: name, Placements: placed.Placements, Commit: commit})
	done := "placed " + name + " on the " + id + " machine in " + strings.Join(placed.Placements, ", ")
	return inv.reportMachine(b, done, commit, pushed)
}

// removeFromMachine is skill remove naming the cloud machine: it takes the
// configurations from names out of the skill's entry in the cloud
// machine's snapshot, or, without from or with --from universal, deletes
// the entry, and changes nothing on this machine's disk. A universal
// client named in from is refused, as on a computer: it sees every skill
// the snapshot lists, so it cannot lose one alone.
func (inv *invocation) removeFromMachine(ctx context.Context, id, name string, from []string, remote bool) error {
	if err := cloudOnly(id); err != nil {
		return err
	}
	if remote {
		return fail(exitUsage, "--remote and --machine cannot both be given",
			"--remote deletes one of your own skills' branches on the account remote; drop it to change the "+cloudMachine+" machine")
	}
	for _, c := range namedConfigurations(from) {
		if !configurationID(c) {
			return fail(exitUsage, clipped(sanitised(c))+" is not a configuration id", "give a configuration id, such as claude-code, or universal")
		}
	}
	if err := cloudRemoval(name, from, cloudUniversal); err != nil {
		return err
	}
	b, err := inv.readMachineBranch(ctx, id)
	if err != nil {
		return err
	}
	snap, _, err := inv.branchSnapshot(ctx, b)
	if err != nil {
		return err
	}
	whole, ok := removeFromSnapshot(&snap, name, namedConfigurations(from))
	if !ok {
		return fail(exitNotFound, "the "+id+" machine has no skill called "+sanitised(name),
			"the "+id+" machine has the skills its snapshot on "+b.name()+" lists")
	}
	subject, done := "Remove "+name, "took "+name+" off the "+id+" machine"
	placements := []string{}
	if !whole {
		named := strings.Join(namedConfigurations(from), ", ")
		subject, done = "Remove "+name+" from "+named, "removed "+name+" from "+named+" on the "+id+" machine"
		placements = snapshotEntry(snap, name).Placements
	}
	command := skillCommand("remove", name, append(fromFlags(from), "--machine", id)...)
	commit, pushed, err := inv.pushSnapshot(ctx, b, snap, subject, command)
	if err != nil {
		return err
	}
	inv.out.emit(machineSkillEvent{event: newEvent("machine_skill"), Machine: id, Name: name, Placements: placements, Removed: whole, Commit: commit})
	return inv.reportMachine(b, done, commit, pushed)
}

// reportMachine prints what a place or remove naming a machine did, done,
// and the commit it pushed, or that the branch held it already.
func (inv *invocation) reportMachine(b machineBranch, done, commit string, pushed bool) error {
	line := done + ": pushed " + short(commit) + " to " + b.name()
	if !pushed {
		line = done + ": " + b.name() + " holds this already, at " + short(commit) + ", so nothing was pushed"
	}
	inv.out.done(line)
	inv.summary = line
	return nil
}

// fromFlags are the --from a run of skill remove was given, as a command a
// hint names repeats them.
func fromFlags(from []string) []string {
	var flags []string
	for _, id := range from {
		flags = append(flags, "--from", shellWord(id))
	}
	return flags
}

// cloudUniversal reports whether the configuration id names a client that
// reads the library on the cloud machine, which sees every skill the
// library holds. The cloud machine's disk is not this one's, so the answer
// is the registry's alone: a client whose skills directories name the
// library, at the default place, of a home made fresh. A symlink or an
// AGENTX_LIBRARY on this machine changes nothing. A client the registry
// does not know is not one.
func cloudUniversal(id string) bool {
	c, ok := scan.Lookup(id)
	if !ok {
		return false
	}
	d := home.Dirs{User: cloudUser, Library: filepath.Join(cloudUser, ".agents", "skills")}
	return slices.Contains(c.SkillsDirs(d), d.Library)
}

// cloudUser is the home cloudUniversal reads the registry in: any path
// will do, since only where the library is beside it matters.
const cloudUser = "/home/cloud"

// cloudRemoval refuses a removal from the cloud machine that cannot be
// made, before anything is read: --from naming a universal client, which
// sees every skill the snapshot lists and cannot lose one alone, and
// --from universal beside a configuration, as on a computer, see
// removalTargets. Pure but for universal.
func cloudRemoval(name string, from []string, universal func(string) bool) error {
	named := namedConfigurations(from)
	var reads []string
	for _, id := range named {
		if universal(id) && !slices.Contains(reads, id) {
			reads = append(reads, id)
		}
	}
	sort.Strings(reads)
	if len(reads) > 0 {
		return fail(exitRefused, universalRefusal(reads, name),
			"take "+name+" off the "+cloudMachine+" machine with '"+skillCommand("remove", name, "--machine", cloudMachine)+"'")
	}
	if len(named) > 0 && len(named) < len(from) {
		return fail(exitUsage, "--from universal and --from "+named[0]+" cannot both be given",
			"--from universal asks for the removal without --from; drop it to remove only the configurations you name")
	}
	return nil
}

// machineEntry is the entry skill place writes for the skill called name:
// one that follows the latest version, naming the skill as this machine's
// library knows it, its source and subpath for a skill of a shared source,
// the account remote and its fork id for one of your own, or, when this
// machine holds no such skill, as snap already lists it. Its placements
// are left to placeInSnapshot. An unmanaged skill, which no machine can
// install, is refused, exit code 6, and one of your own the account remote
// does not hold is warned of, since the cloud machine cannot install it
// until it is published.
func (inv *invocation) machineEntry(sc skillContext, snap snapshot, name string) (snapshotSkill, error) {
	rec, ok := sc.records[name]
	switch {
	case !ok:
		if e := snapshotEntry(snap, name); e != nil {
			return following(*e), nil
		}
		if _, held := librarySkill(inv.dirs.Library, name); held {
			return snapshotSkill{}, fail(exitRefused, name+" is not managed by agentx, so the "+cloudMachine+" machine has nowhere to install it from",
				"make it one of your own with '"+skillCommand("fork", name)+"' and publish it, then place it again")
		}
		return snapshotSkill{}, inv.noLibrarySkill(name)
	case rec.Kind == lineage.KindFork:
		e := snapshotSkill{Name: name, Kind: lineage.KindManaged, Source: sc.account, Placements: []string{}}
		if rec.Fork != nil {
			e.ForkID = rec.Fork.ID
		}
		if sc.publishedRead && sc.published[name] == "" {
			inv.out.warn(name + " is not published to the account remote, so the " + cloudMachine + " machine cannot install it until you run '" + publishCommand(name) + "'")
		}
		return e, nil
	case !rec.HasImport:
		return snapshotSkill{}, fail(exitRefused, sanitised(rec.Ref)+" names no source it was imported from, so the "+cloudMachine+" machine has nowhere to install "+name+" from",
			"install "+name+" again with 'agentx skill add <source>'")
	}
	return snapshotSkill{Name: name, Kind: lineage.KindManaged, Source: rec.Import.Source, Subpath: ptr(rec.Import.Path), Placements: []string{}}, nil
}

// following is e as an entry that follows the latest version: its version
// and its upstream left out, which only an export writes.
func following(e snapshotSkill) snapshotSkill {
	return snapshotSkill{Name: e.Name, Kind: e.Kind, ForkID: e.ForkID, Source: e.Source, Subpath: e.Subpath, Placements: slices.Clone(e.Placements)}
}

// snapshotEntry is the entry of snap for the skill called name, nil when it
// lists none.
func snapshotEntry(snap snapshot, name string) *snapshotSkill {
	for i := range snap.Skills {
		if snap.Skills[i].Name == name {
			return &snap.Skills[i]
		}
	}
	return nil
}

// placeInSnapshot sets the entry of snap for entry's skill to entry, as
// one that follows, with the placements of the entry snap held, if any,
// and ids, each once, sorted. The skills stay sorted by name, as an export
// writes them, and every list a partial snapshot left out is written
// empty. Pure.
func placeInSnapshot(snap *snapshot, entry snapshotSkill, ids []string) {
	entry = following(entry)
	placements := slices.Clone(ids)
	if held := snapshotEntry(*snap, entry.Name); held != nil {
		placements = append(placements, held.Placements...)
	}
	sort.Strings(placements)
	entry.Placements = slices.Compact(placements)
	if held := snapshotEntry(*snap, entry.Name); held != nil {
		*held = entry
	} else {
		snap.Skills = append(snap.Skills, entry)
	}
	sort.SliceStable(snap.Skills, func(i, j int) bool { return snap.Skills[i].Name < snap.Skills[j].Name })
	fillLists(snap)
}

// removeFromSnapshot takes the configurations from out of the entry of snap
// for the skill called name, leaving the entry with what remains, or, with
// no from, deletes the entry; whole says which. ok is false when snap
// lists no such skill. An entry with configurations taken out follows, as
// every entry these commands write does. Pure.
func removeFromSnapshot(snap *snapshot, name string, from []string) (whole, ok bool) {
	held := snapshotEntry(*snap, name)
	if held == nil {
		return len(from) == 0, false
	}
	if len(from) == 0 {
		snap.Skills = slices.DeleteFunc(snap.Skills, func(e snapshotSkill) bool { return e.Name == name })
		fillLists(snap)
		return true, true
	}
	kept := following(*held)
	kept.Placements = slices.DeleteFunc(kept.Placements, func(id string) bool { return slices.Contains(from, id) })
	if !slices.Equal(kept.Placements, held.Placements) {
		*held = kept
	}
	fillLists(snap)
	return false, true
}

// fillLists writes [] for every list a partial snapshot left out, as an
// export writes a list with nothing in it.
func fillLists(snap *snapshot) {
	if snap.SchemaVersion == 0 {
		snap.SchemaVersion = snapshotSchemaVersion
	}
	if snap.Configurations == nil {
		snap.Configurations = []snapshotConfiguration{}
	}
	if snap.Skills == nil {
		snap.Skills = []snapshotSkill{}
	}
	if snap.MCPServers == nil {
		snap.MCPServers = []snapshotServer{}
	}
	if snap.Plugins == nil {
		snap.Plugins = []snapshotPlugin{}
	}
}
