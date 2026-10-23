package cli

import (
	"context"
	"errors"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

// The actions an import takes for one skill entry of a snapshot, which its
// import_skill event reports, in the order the result counts them.
const (
	importInstalled = "installed" // the machine lacked it: installed, and placed as the snapshot records
	importUpdated   = "updated"   // moved forward to the version the snapshot names, or the latest for an entry that follows
	importPlaced    = "placed"    // at its version already, and placed in a configuration it was missing from
	importUnplaced  = "unplaced"  // at its version already, and taken out of configurations only
	importKept      = "kept"      // left as it is: at that version, or ahead of it, and placed as recorded
	importSkipped   = "skipped"   // not applied, for the reason the event carries
)

// importActions are the actions, in the order the result counts them.
var importActions = []string{importInstalled, importUpdated, importPlaced, importUnplaced, importKept, importSkipped}

// applySnapshot applies doc, a snapshot read and validated whole, to this
// machine; origin names where it was read from, as the question and the
// report name it. It is the one step after reading a snapshot, whatever it
// was read from.
//
// What the machine holds of each entry is read first, and is what each
// import_skill event's state and local_commit report. Then the command asks
// once, see confirmImport, before it writes or fetches anything, and only a
// yes goes on: the settings are replaced, when the snapshot holds any, and
// every skill entry is applied, see applyRun. Each change is made by the
// command that makes it on its own – skill add, skill update, skill place
// and skill remove – each one journaled mutation, so a run stopped part way
// leaves at most one unfinished journal, which the next command finishes as
// it finishes any other, and a second run applies the rest.
func (inv *invocation) applySnapshot(ctx context.Context, origin string, doc snapshot, yes bool, in io.Reader) error {
	here, err := inv.loadSettings()
	if err != nil {
		return err
	}
	account := snapshotAccount(doc, importTarget{account: documentAccount(here)})
	records, err := inv.lineageRecords(ctx)
	if err != nil {
		return err
	}
	remote, err := inv.remoteTips(ctx)
	if err != nil {
		return err
	}
	states := restoreStates(doc.Skills, records, remote, account)
	if doc.Settings != nil || len(doc.Skills) > 0 {
		if err := inv.confirmImport(origin, doc, in, yes); err != nil {
			return err
		}
	}
	if doc.Settings != nil {
		forgetAccess(doc.Settings)
		if err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
			return home.SaveSettings(inv.dirs.Home, *doc.Settings)
		}); err != nil {
			return mutationFailure(err)
		}
		here = *doc.Settings
	}
	inv.emitSettings(here)
	run := &applyRun{inv: inv, account: account}
	if err := run.apply(ctx, states); err != nil {
		return err
	}
	for _, st := range states {
		inv.out.emit(st)
	}
	inv.printImported(ctx, origin, doc, states, here, run.fetched)
	inv.summary = importSummary(origin, doc.Settings != nil, states)
	return nil
}

// applyRun is one import's application of the skill entries of a snapshot,
// one entry after the other, in the order the snapshot holds them:
//
//   - A skill this machine lacks is installed: a skill of a shared source
//     from that source, at the version the entry names or, for one that
//     follows, at the source's current one; one of your own from the
//     account remote, at its branch there, which must hold the commit an
//     exact entry names. It is placed in the configurations the snapshot
//     records.
//   - A skill this machine holds moves forward only: to the version the
//     entry names, or the latest for one that follows, when that descends
//     from the one here, by skill update. It is kept when it is that
//     version, or descends from it. A skill whose histories diverged, that
//     holds edits, whose merge is pending, or that the machine holds as
//     another skill is left as it is, and skipped with the reason.
//   - Then its placements are set to the recorded ones in the
//     configurations detected here, by skill place and skill remove --from.
//     A configuration of the snapshot this machine has not detected is
//     passed over, and so is a client that reads the library, which sees
//     every library skill.
//   - An unmanaged skill has no source to install it from: one the library
//     holds is placed, and any other is skipped.
//
// A skill the snapshot does not list is left as it is, and MCP servers and
// plugins are not applied.
type applyRun struct {
	inv     *invocation
	account string // the account remote the snapshot is read against, see snapshotAccount
	gitDir  string
	records map[string]lineage.Record    // the branches of the account repo by name, before the run
	library map[string]scan.LibrarySkill // the library before the run
	held    map[string]librarySkillEvent // what the library holds of each entry before the run
	targets []placeTarget                // every detected configuration a placement can be made in
	reads   map[string][]string          // the skills directories each detected configuration reads, by id
	sources map[string]*sharedFetch      // the shared sources the run fetched, by canonical URL
	fetched map[string]bool              // the canonical URLs of the sources the run fetched, the account remote included
	// The account remote as the run fetched it: its git remote, the tip of
	// each of your own skills it holds, and why it could not be fetched,
	// which every entry of your own is skipped for.
	remote  string
	tips    map[string]string
	missing *failure
}

// sharedFetch is one shared source as the run fetched it, once, before any
// skill was applied.
type sharedFetch struct {
	src     source.Source
	entry   home.Source
	listing source.Listing
	err     *failure // why it could not be fetched, which every entry of it is skipped for
}

// apply applies every entry of states and fills in what was done: the
// action, the reason of a skip, and the placements after the run.
func (r *applyRun) apply(ctx context.Context, states []importSkillEvent) error {
	if len(states) == 0 {
		return nil
	}
	inv := r.inv
	if err := r.read(ctx, states); err != nil {
		return err
	}
	if err := r.fetch(ctx, states); err != nil {
		return err
	}
	for i := range states {
		if err := r.entry(ctx, &states[i]); err != nil {
			return err
		}
	}
	// The placements each event reports are the ones the machine has now,
	// read by one scan of it, as an export reads them.
	inventory, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	modes, err := inv.copyModes(s)
	if err != nil {
		return err
	}
	library := librarySkills(inv.dirs.Library)
	for i := range states {
		st := &states[i]
		st.Placements = []string{}
		if lib, ok := library[st.Name]; ok {
			st.Placements = placementIDs(inv.placements(inventory, lib, modes))
		}
	}
	return nil
}

// read reads what the machine holds before anything is applied: the
// lineage, the library, and the placements and state of every library
// skill an entry names, from one scan.
func (r *applyRun) read(ctx context.Context, states []importSkillEvent) error {
	inv := r.inv
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	inventory, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	r.gitDir = gitx.AccountRepoPath(inv.dirs.Home)
	r.records = sc.records
	r.library = librarySkills(inv.dirs.Library)
	r.held = map[string]librarySkillEvent{}
	for _, st := range states {
		if lib, ok := r.library[st.Name]; ok {
			r.held[st.Name] = sc.librarySkillEventFor(ctx, inv, inventory, lib, nil)
		}
	}
	r.targets = inv.detectedTargets()
	r.reads = map[string][]string{}
	for _, c := range scan.Detect(inv.dirs) {
		r.reads[c.Slug()] = c.SkillsDirs(inv.dirs)
	}
	return nil
}

// fetch fetches, once each and before any skill is applied, every source an
// entry needs: the shared source of each entry of one that this machine
// lacks, that follows or that is held at another commit, as skill add
// --fetch fetches it, and the account remote for such an entry of your own,
// as skill add with no source fetches it. An entry held at the commit it
// names needs no source, so a second import of an unchanged snapshot
// reaches no remote. A source that cannot be fetched skips its entries; it
// costs no other.
func (r *applyRun) fetch(ctx context.Context, states []importSkillEvent) error {
	r.sources, r.fetched = map[string]*sharedFetch{}, map[string]bool{}
	account := false
	for _, st := range states {
		e := st.snapshotSkill
		if e.Kind == lineage.KindUnmanaged {
			continue
		}
		if rec, ok := r.records[e.Name]; ok && e.Commit != "" && rec.Commit == e.Commit {
			continue
		}
		if ownRecord(e, r.account) {
			account = true
			continue
		}
		if _, ok := r.sources[e.Source]; ok {
			continue
		}
		fx, err := r.fetchShared(ctx, e.Source)
		if err != nil {
			return err
		}
		r.sources[e.Source] = fx
	}
	if account {
		return r.fetchAccount(ctx)
	}
	return nil
}

// fetchShared adds or fetches the shared source at url, at the pin the
// settings hold for it, as skill add --fetch does: a source the settings
// do not hold is added, and one an import of settings wrote, which this
// machine never fetched, is added for the first time.
func (r *applyRun) fetchShared(ctx context.Context, url string) (*sharedFetch, error) {
	inv := r.inv
	s, err := inv.loadSettings()
	if err != nil {
		return nil, err
	}
	fx := &sharedFetch{src: source.Source{URL: url}}
	if i := s.FindSource(url); i >= 0 {
		fx.src.Ref = s.Sources[i].Pin
	}
	err = inv.quietly(func() error {
		var err error
		fx.listing, fx.entry, err = inv.addSource(ctx, fx.src)
		return err
	})
	if f, ok := skipped(ctx, err); ok {
		fx.err = f
		return fx, nil
	} else if err != nil {
		return nil, err
	}
	r.fetched[url] = true
	return fx, nil
}

// fetchAccount fetches the account remote, as skill add with no source does,
// and reads the tip of each of your own skills it holds.
func (r *applyRun) fetchAccount(ctx context.Context) error {
	inv := r.inv
	err := inv.quietly(func() error {
		gitDir, entry, remote, err := inv.accountRemote(ctx)
		if err != nil {
			return err
		}
		if err := inv.fetchRemote(ctx, gitDir, remote, entry.URL); err != nil {
			return err
		}
		if err := home.SyncExclude(gitDir, inv.systemFilesIgnored()); err != nil {
			return accountRepoFailure(err)
		}
		tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
		if err != nil {
			return accountRepoFailure(err)
		}
		r.gitDir, r.remote, r.tips = gitDir, remote, tips
		r.fetched[entry.URL] = true
		return nil
	})
	if f, ok := skipped(ctx, err); ok {
		r.missing = f
		return nil
	}
	return err
}

// entry applies one entry and records what was done. A step that refuses
// skips the entry with the refusal's message as its reason, and the run
// goes on; a stop, or an error that is no refusal, ends the run.
func (r *applyRun) entry(ctx context.Context, st *importSkillEvent) error {
	e := st.snapshotSkill
	var action string
	var err error
	switch {
	case e.Kind == lineage.KindUnmanaged:
		action, err = r.unmanaged(ctx, e)
	case ownRecord(e, r.account):
		action, err = r.ownSkill(ctx, e)
	default:
		action, err = r.sharedSkill(ctx, e)
	}
	if f, ok := skipped(ctx, err); ok {
		st.Action, st.Reason = importSkipped, f.message
		return nil
	} else if err != nil {
		return err
	}
	st.Action = action
	return nil
}

// skipped reports whether err is a refusal that skips one entry, or one
// source, rather than ending the run: any failure but a stop.
func skipped(ctx context.Context, err error) (*failure, bool) {
	var f *failure
	if err == nil || ctx.Err() != nil || !errors.As(err, &f) || f.status == exitInterrupted {
		return nil, false
	}
	return f, true
}

// skip is the refusal that skips an entry for why.
func skip(why string) *failure { return refuse(exitRefused, why, "") }

// unmanaged applies an unmanaged entry: it has no source to install it
// from, so only a skill of that name the library holds, unmanaged too, is
// placed.
func (r *applyRun) unmanaged(ctx context.Context, e snapshotSkill) (string, error) {
	if _, ok := r.library[e.Name]; !ok {
		return "", skip(e.Name + " is unmanaged, so there is no source to install it from")
	}
	if _, managed := r.records[e.Name]; managed {
		return "", skip("the snapshot lists " + e.Name + " as unmanaged, and agentx manages the one on this machine")
	}
	return r.place(ctx, e, importKept)
}

// ownSkill applies an entry of one of your own skills, see applyRun.
func (r *applyRun) ownSkill(ctx context.Context, e snapshotSkill) (string, error) {
	if r.missing != nil {
		return "", r.missing
	}
	tip := r.tips[e.Name]
	rec, ok := r.records[e.Name]
	if !ok || rec.Kind != lineage.KindFork {
		return r.installOwn(ctx, e, tip)
	}
	local := rec.Commit
	if e.Commit != "" && (e.Commit == local || r.ancestor(ctx, e.Commit, local)) {
		return r.place(ctx, e, importKept) // at the version the entry names, or ahead of it
	}
	if e.Commit == "" && (tip == "" || tip == local || r.ancestor(ctx, tip, local)) {
		return r.place(ctx, e, importKept) // the latest version is here already
	}
	if e.Commit != "" && !r.remoteHolds(ctx, e.Commit, tip) {
		return "", skip("the account remote's skills/" + e.Name + " does not hold the version the snapshot names")
	}
	target := e.Commit
	if target == "" {
		target = tip
	}
	if !r.ancestor(ctx, local, target) {
		return "", skip(e.Name + " on this machine and the version the snapshot names have diverged; merge them with '" + skillCommand("update", e.Name) + "'")
	}
	// The update fast-forwards the branch to the account remote's tip, which
	// holds the version the entry names.
	if f := r.movable(e.Name, rec); f != nil {
		return "", f
	}
	if err := r.inv.quietly(func() error { return r.inv.skillUpdate(ctx, e.Name) }); err != nil {
		return "", err
	}
	if now, err := r.branchAt(ctx, lineage.ForkRef(e.Name)); err != nil || now != tip {
		return "", skip(e.Name + " could not be moved forward to the account remote's version; run '" + skillCommand("update", e.Name) + "'")
	}
	return r.place(ctx, e, importUpdated)
}

// installOwn installs one of your own skills this machine lacks from the
// account remote, whose branch of it is at tip, as skill add --name does,
// placing it in the configurations the snapshot records.
func (r *applyRun) installOwn(ctx context.Context, e snapshotSkill, tip string) (string, error) {
	switch {
	case tip == "":
		return "", skip("the account remote holds no skill called " + e.Name)
	case e.Commit != "" && !r.remoteHolds(ctx, e.Commit, tip):
		return "", skip("the account remote's skills/" + e.Name + " does not hold the version the snapshot names")
	}
	targets := r.placeInto(e.Placements)
	err := r.inv.quietly(func() error {
		return r.inv.installOne(ctx, r.gitDir, r.remote, e.Name, targets, nil, false)
	})
	return importInstalled, err
}

// sharedSkill applies an entry of a skill of a shared source, see
// applyRun.
func (r *applyRun) sharedSkill(ctx context.Context, e snapshotSkill) (string, error) {
	inv := r.inv
	rec, ok := r.records[e.Name]
	switch {
	case ok && rec.Kind == lineage.KindFork:
		return "", skip(e.Name + " on this machine is one of your own skills, and the snapshot lists a skill of " + e.Source)
	case ok && (!rec.HasImport || rec.Import.Source != e.Source || rec.Import.Path != subpathOf(e)):
		return "", skip(e.Name + " on this machine comes from another source or folder than the one the snapshot names")
	case ok && e.Commit != "" && rec.Commit == e.Commit:
		return r.place(ctx, e, importKept)
	}
	fx := r.sources[e.Source]
	if fx.err != nil {
		return "", fx.err
	}
	sk, tip, found, err := r.version(ctx, fx, e)
	switch {
	case err != nil:
		return "", err
	case !found && ok && e.Commit == "":
		return r.place(ctx, e, importKept) // its source holds it no longer, so there is nothing newer
	case !found:
		return "", skip(e.Source + underPath(subpathOf(e)) + " does not hold the version of " + e.Name + " the snapshot names")
	case !ok:
		targets := r.placeInto(e.Placements)
		return importInstalled, inv.quietly(func() error {
			return inv.installSkills(ctx, r.gitDir, fx.src, fx.entry.LastFetched, tip, []source.Skill{sk}, targets, false)
		})
	}
	versions, _, err := inv.listVersions(ctx, r.gitDir, fx.src, tip, []source.Skill{sk})
	if err != nil {
		return "", err
	}
	next := versions[0].imp
	switch {
	case next.Commit == rec.Import.Commit:
		return r.place(ctx, e, importKept)
	case lineage.Newer(ctx, inv.git, r.gitDir, next, rec.Import):
		return r.place(ctx, e, importKept) // ahead of the version the snapshot names
	case !lineage.Newer(ctx, inv.git, r.gitDir, rec.Import, next):
		return "", skip(e.Name + " on this machine and the version the snapshot names have diverged, so neither is newer")
	}
	if f := r.movable(e.Name, rec); f != nil {
		return "", f
	}
	commit, err := r.pinCandidate(ctx, fx.src, rec, tip, sk)
	if err != nil {
		return "", err
	}
	if err := inv.quietly(func() error { return inv.skillUpdate(ctx, e.Name) }); err != nil {
		return "", err
	}
	if now, err := r.branchAt(ctx, lineage.ManagedRef(e.Name)); err != nil || now != commit {
		return "", skip(e.Name + " could not be updated to the version the snapshot names; run '" + skillCommand("update", e.Name) + "'")
	}
	return r.place(ctx, e, importUpdated)
}

// version is the skill an entry of a shared source names in fx and the
// commit of the source to read it at: for an exact entry the upstream
// commit it records, which the account repo holds once the source's history
// is fetched; for one that follows the commit the run fetched. found is
// false when the source holds no skill there at that commit.
func (r *applyRun) version(ctx context.Context, fx *sharedFetch, e snapshotSkill) (sk source.Skill, tip string, found bool, err error) {
	subpath := subpathOf(e)
	if e.Commit == "" {
		for _, listed := range fx.listing.Skills {
			if listed.Subpath == subpath {
				listed.Name = e.Name // the library name, whatever the upstream calls it now
				return listed, fx.listing.Commit, true, nil
			}
		}
		return source.Skill{}, "", false, nil
	}
	tip = e.UpstreamCommit
	// tip is an object id, which validEntries checked, so nothing here can
	// be read as an option.
	out, status, err := r.inv.git.IsolatedStatus(ctx, r.gitDir, 128, "rev-parse",
		tip+":"+subpath, tip+":"+path.Join(subpath, "SKILL.md"))
	if err != nil {
		return source.Skill{}, "", false, accountRepoFailure(err)
	}
	ids := strings.Fields(out)
	if status != 0 || len(ids) != 2 {
		return source.Skill{}, "", false, nil
	}
	return source.Skill{Subpath: subpath, Name: e.Name, Tree: ids[0]}, tip, true, nil
}

// subpathOf is the folder of its source an entry of a shared source names,
// "" for the root.
func subpathOf(e snapshotSkill) string {
	if e.Subpath == nil {
		return ""
	}
	return *e.Subpath
}

// movable refuses to move forward a skill that holds what an update would
// merge rather than replace, which an import never does: edits not yet
// published, a merge pending, or, for one of your own, an update from its
// upstream besides.
func (r *applyRun) movable(name string, rec lineage.Record) *failure {
	ev := r.held[name]
	switch {
	case ev.PendingMerge:
		return skip("a merge of " + name + " is pending; finish it with '" + skillCommand("update", name) + "'")
	case ev.State == stateModified:
		return skip(name + " holds edits not yet published, which an import never merges; take the newer version in with '" + skillCommand("update", name) + "'")
	}
	if _, ok := rec.ForkCandidate(); ok {
		return skip(name + " has an update from its upstream as well; take both in with '" + skillCommand("update", name) + "'")
	}
	return nil
}

// pinCandidate writes the import commit of the version of sk at tip and
// pins it as the update candidate of the managed skill rec, as an update
// check pins the version it found, so that skill update applies exactly
// that version. It returns the import commit.
func (r *applyRun) pinCandidate(ctx context.Context, src source.Source, rec lineage.Record, tip string, sk source.Skill) (string, error) {
	inv := r.inv
	commit := ""
	err := inv.quietly(func() error {
		b := newBatch(inv, 1)
		versions, err := inv.readVersions(ctx, b, r.gitDir, src, tip, []source.Skill{sk})
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			return b.abandon()
		}
		run := lineage.NewRun()
		if err := inv.writeImports(ctx, b, r.gitDir, run, versions); err != nil {
			return err
		}
		commit = versions[0].commit
		if rec.CandidateCommit() == commit {
			inv.dropImporting(ctx, r.gitDir, run, len(versions))
			return nil
		}
		journaled := false
		err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
			m := home.NewMutation(inv.dirs.Home)
			m.Ref(r.gitDir, lineage.CandidateRef(rec.Name), rec.CandidateCommit(), commit)
			err := m.Apply(inv.refs(ctx))
			journaled = m.Journaled()
			return err
		})
		if !journaled || err == nil {
			// Recovery of a journal that could not be finished needs the
			// commit the staging ref holds; otherwise the candidate ref holds
			// it by now, or no ref ever will.
			inv.dropImporting(ctx, r.gitDir, run, len(versions))
		}
		if err != nil {
			return mutationFailure(err)
		}
		return nil
	})
	return commit, err
}

// place sets the placements of a skill the library holds to the ones the
// entry records, in the configurations detected here: it takes the skill
// out of each configuration whose own place holds it and the entry does
// not name, with skill remove --from, then places it in each named one
// that lacks it, with skill place --to, see placeInto. A client that reads
// the library is left as it is either way. did is what was done before,
// which a skill kept as it was turns into placed or unplaced when its
// placements changed.
func (r *applyRun) place(ctx context.Context, e snapshotSkill, did string) (string, error) {
	inv := r.inv
	ev, ok := r.held[e.Name]
	if !ok {
		return did, nil
	}
	own := map[string]bool{} // the configurations whose own place holds the skill
	for _, t := range r.targets {
		for _, p := range ev.Placements {
			if p.Configuration == t.id && p.Path == t.ownPlace(inv.dirs.Library, e.Name) {
				own[t.id] = true
			}
		}
	}
	var add, drop []string
	for _, t := range r.placeInto(e.Placements) {
		if !own[t.id] {
			add = append(add, t.id)
		}
	}
	for _, t := range r.targets {
		if !t.readsLibrary && own[t.id] && !slices.Contains(e.Placements, t.id) {
			drop = append(drop, t.id)
		}
	}
	if len(drop) > 0 {
		if err := inv.quietly(func() error { return inv.skillRemove(ctx, e.Name, drop, false) }); err != nil {
			return "", err
		}
	}
	if len(add) > 0 {
		if err := inv.quietly(func() error { return inv.skillPlace(ctx, e.Name, add, false, false) }); err != nil {
			return "", err
		}
	}
	switch {
	case did != importKept:
	case len(add) > 0:
		did = importPlaced
	case len(drop) > 0:
		did = importUnplaced
	}
	return did, nil
}

// placeInto is the configurations a skill is placed in for want, the ones a
// snapshot records: each detected one want names, but a client that reads
// the library, which sees every library skill, and a client that reads the
// skills directory of another one want names, as Cursor reads Claude
// Code's, which sees the skill there.
func (r *applyRun) placeInto(want []string) []placeTarget {
	var into []placeTarget
	for _, t := range r.targets {
		if t.readsLibrary || !slices.Contains(want, t.id) {
			continue
		}
		seen := slices.ContainsFunc(r.targets, func(o placeTarget) bool {
			return o.id != t.id && !o.readsLibrary && slices.Contains(want, o.id) && slices.Contains(r.reads[t.id], o.dir)
		})
		if !seen {
			into = append(into, t)
		}
	}
	return into
}

// ancestor reports whether the commit a is b or one b descends from, as
// git merge-base --is-ancestor says in the account repo. A commit the repo
// does not hold descends from nothing.
func (r *applyRun) ancestor(ctx context.Context, a, b string) bool {
	if a == b {
		return true
	}
	_, status, err := r.inv.git.IsolatedStatus(ctx, r.gitDir, 128, "merge-base", "--is-ancestor", a, b)
	return err == nil && status == 0
}

// remoteHolds reports whether the account remote's branch of a skill, at
// tip, holds commit.
func (r *applyRun) remoteHolds(ctx context.Context, commit, tip string) bool {
	return tip != "" && r.ancestor(ctx, commit, tip)
}

// branchAt is the commit ref points at in the account repo, "" for none.
func (r *applyRun) branchAt(ctx context.Context, ref string) (string, error) {
	out, err := r.inv.git.Isolated(ctx, r.gitDir, "for-each-ref", "--format=%(objectname)", ref)
	return strings.TrimSpace(out), err
}

// quietly runs step, the work of another command inside this one, with
// what that command would print or emit held back, since the import reports
// each skill once, in its own words; its warnings still reach stderr, as
// they would on their own. What the step says the run did is not this
// run's either, so the summary is kept as it was.
func (inv *invocation) quietly(step func() error) error {
	loud, summary := inv.out, inv.summary
	inv.out = &writer{stdout: io.Discard, stderr: loud.stderr, env: loud.env, json: loud.json, verbose: loud.verbose, color: loud.color}
	defer func() { inv.out, inv.summary = loud, summary }()
	return step()
}
