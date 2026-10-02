package cli

import (
	"context"
	"errors"
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
	"github.com/grundmanise/agentx/apps/cli/internal/source"
)

func newSkillForkCommand(inv *invocation) *cobra.Command {
	var newName string
	cmd := &cobra.Command{
		Use:   "fork <name>",
		Short: "Fork a skill, so that its edits get a history of their own",
		Long: "Fork the skill called <name>: a managed skill, an unmanaged one, a skill a\n" +
			"plugin provides, or a fork. The fork gets its own branch, skills/<name>, in the\n" +
			"account repo, whose first commit holds the skill as it is now, edits included,\n" +
			"checked out as a worktree in agentx home, and the library holds a symlink to it.\n" +
			"A managed skill's fork keeps its upstream, so newer versions can be merged in.\n\n" +
			"Without --name the fork takes the skill's place: its library directory moves\n" +
			"into the fork's worktree and every placement stays as it was. With --name <new>\n" +
			"the fork is made beside the skill, under the new name, which it also writes into\n" +
			"SKILL.md, and placed into the configurations the skill is placed in; the skill\n" +
			"stays as it was. A fork, and a plugin's skill, is always forked beside it.\n\n" +
			"Name a plugin's skill as <plugin>:<skill> when several plugins provide one of\n" +
			"that name. Files git ignores are never committed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return inv.skillFork(cmd.Context(), args[0], newName)
		},
	}
	cmd.Flags().StringVar(&newName, "name", "", "fork it beside the skill under this name, instead of in its place")
	return cmd
}

// forkNoteEvent is what one client is known to do with a fork of a
// plugin's skill beside the plugin's own copy, which agentx leaves as it
// is.
type forkNoteEvent struct {
	event
	Name          string `json:"name"`
	Plugin        string `json:"plugin"`
	Configuration string `json:"configuration"`
	Note          string `json:"note"`
}

// forking is one run of skill fork, as it is planned before the lock.
type forking struct {
	src     forkSource
	target  string // the fork's name
	inPlace bool   // the fork takes the source's place: its library directory moves into the worktree
	rename  bool   // the fork is the first step of skill rename, which removes the source next
	gitDir  string
	dir     string // the skill's directory in the fork's branch, which never changes
	// captured is what the source's library directory held before git read
	// it, for a fork in its place: the content the journal moves.
	captured string
	site     forkSite   // a fork's, when the source is one
	judged   siteJudged // how the source fork's directory compared with its tip
	commit   string     // the creation commit
	targets  []placeTarget
	notes    []forkNoteEvent
	// scanned scans the machine once, the first time it is asked, and
	// snap keeps what it found.
	scanned func() (scan.Snapshot, error)
	snap    *scan.Snapshot
}

// skillFork forks the skill arg names, under newName when it is given and
// differs from the skill's own name, and in the skill's place otherwise.
// The fork's creation commit is written by the fork commit writer before
// the lock, and holds the skill as it is now, as git records it under the
// ignore rules every fork follows: for a managed skill on its import
// commit, so that its edits are exactly what the commit changes, for a
// fork on its tip, so that its history is kept, and for an unmanaged
// skill or a plugin's as a branch of its own. It carries a new fork id
// even when it changes nothing, and nothing points at it until one
// journaled mutation creates the branch.
//
// In its place, the mutation deletes the import branch of a managed skill,
// adds the fork's worktree, moves the library directory into it, files git
// ignores and all, and leaves the symlink to it at the library path, so
// every placement, which names the library path, is the fork's. Beside
// the skill, the fork's directory is laid out from the commit in a
// worktree of its own and placed as createFork places a new skill, into
// the configurations the source is placed in, or, for a plugin's skill,
// into the enabled configurations that have the plugin.
func (inv *invocation) skillFork(ctx context.Context, arg, newName string) error {
	fk, err := inv.planFork(ctx, arg, newName, false)
	if err != nil {
		return err
	}
	return inv.makeFork(ctx, fk)
}

// planFork finds the skill arg names and runs every refusal of its fork
// under newName, see checkForking, before anything is written: the part of
// a fork that skill rename runs before either of its steps, saying so in
// rename.
func (inv *invocation) planFork(ctx context.Context, arg, newName string, rename bool) (*forking, error) {
	if err := inv.finishJournals(ctx); err != nil {
		return nil, err
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return nil, accountRepoFailure(err)
	}
	records := map[string]lineage.Record{}
	if exists {
		if records, err = inv.listLineage(ctx, gitDir); err != nil {
			return nil, accountRepoFailure(err)
		}
	}
	fk := &forking{gitDir: gitDir, rename: rename}
	fk.scanned = func() (scan.Snapshot, error) {
		if fk.snap == nil {
			s, err := inv.scan(ctx, lockWait, "", false)
			if err != nil {
				return scan.Snapshot{}, err
			}
			fk.snap = &s
		}
		return *fk.snap, nil
	}
	src, err := inv.forkSourceOf(arg, records, fk.scanned)
	if err != nil {
		return nil, err
	}
	fk.src, fk.target = src, src.name
	if newName != "" {
		fk.target = newName
	}
	fk.inPlace = fk.target == src.name && (src.kind == lineage.KindManaged || src.kind == lineage.KindUnmanaged)
	if err := inv.checkForking(ctx, fk, records, newName != ""); err != nil {
		return nil, err
	}
	return fk, nil
}

// makeFork writes the creation commit of the fork planFork planned and
// applies the fork in one journaled mutation, then reports it.
func (inv *invocation) makeFork(ctx context.Context, fk *forking) error {
	src := fk.src
	gitDir, _, err := gitx.OpenAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	fk.gitDir = gitDir
	if err := home.SyncExclude(gitDir, inv.systemFilesIgnored()); err != nil {
		return accountRepoFailure(err)
	}
	// The writer reads the user's identity and their core.excludesFile in
	// one read of their configuration, before git reads the skill, so the
	// ignore rules the creation commit is written under come with it.
	w, err := inv.newForkWriter(ctx, gitDir)
	if err != nil {
		return err
	}
	root, parents, err := inv.forkContent(ctx, fk)
	if err != nil {
		return err
	}
	if fk.target != src.name {
		if root, err = inv.renameSkill(ctx, gitDir, root, fk.dir, fk.target); err != nil {
			return err
		}
	}
	if fk.commit, err = w.commit(ctx, root, parents, forkMessage{subject: forkSubject(src, fk.target), trailers: lineage.ForkTrailers{ForkID: lineage.NewForkID()}}); err != nil {
		return accountRepoFailure(err)
	}
	if !fk.inPlace {
		s, err := fk.scanned()
		if err != nil {
			return err
		}
		if err := inv.planBeside(fk, s); err != nil {
			return err
		}
	}
	var done placements
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error { return inv.applyFork(ctx, fk, &done) })
	if errors.Is(err, home.ErrMovedBeforeApply) {
		return fail(exitRefused, sanitised(src.name)+" changed while it was being forked, so nothing was changed", "run the command again")
	}
	if err != nil {
		return mutationFailure(err)
	}
	return inv.reportForked(ctx, fk, done)
}

// checkForking runs the refusals of a fork that need no git but the
// account repo's branches, before anything changes: a fork forked under
// its own name, a name agentx cannot give the fork or that a branch
// already holds, a library path or worktree that is taken, a skill whose
// update left a merge pending, and a fork with uncommitted edits, which
// the new fork would leave behind.
func (inv *invocation) checkForking(ctx context.Context, fk *forking, records map[string]lineage.Record, named bool) error {
	src := fk.src
	if src.kind == lineage.KindFork && fk.target == src.name {
		if named {
			return fail(exitRefused, "the fork is already called "+sanitised(src.name), fk.takenHint())
		}
		return fail(exitRefused, sanitised(src.name)+" is already a fork",
			"fork it under a new name with '"+skillCommand("fork", src.name, "--name", "<new>")+"'")
	}
	if refusal := forkNameRefusal(fk.target); refusal != "" {
		hint := "choose a name such as my-skill"
		if !named {
			hint = "fork it under a valid name with '" + skillCommand("fork", src.name, "--name", "<new>") + "'"
		}
		return fail(exitRefused, refusal, hint)
	}
	if err := inv.checkForkName(ctx, records, fk.target, fk.except(), fk.takenHint()); err != nil {
		return err
	}
	if src.kind == lineage.KindManaged && !src.rec.HasImport {
		return fail(exitRefused, fmt.Sprintf("the import branch %s records no version agentx can read", src.rec.Ref),
			"run 'agentx doctor' and check the account repo it names")
	}
	if fk.inPlace {
		if err := inv.inPlaceRoom(fk); err != nil {
			return err
		}
	} else if err := inv.newSkillRoom(inv.libraryPath(fk.target), inv.worktreeRoot(fk.target)); err != nil {
		return err
	}
	switch {
	case src.kind == lineage.KindManaged && inv.mergePending(src.name):
		return pendingMergeRefusal(src.name, "forked")
	case src.kind == lineage.KindFork && inv.mergePending(src.name):
		return forkPendingRefusal(src.name, "forked")
	case src.kind != lineage.KindFork:
		return nil
	}
	// A fork is forked from its tip, so edits nobody committed would stay
	// behind in it: they are refused, as every command that leaves the
	// fork's directory out of what it does refuses them.
	f, err := inv.forkSiteOf(ctx, fk.gitDir, src.rec)
	if err != nil {
		return err
	}
	if fk.judged, err = inv.judgeSite(ctx, f, false); err != nil {
		return err
	}
	if !fk.judged.clean {
		return uncommittedRefusal(src.name, "forked")
	}
	fk.site, fk.dir = f, f.dir
	return nil
}

// takenHint is the hint of a name the fork cannot take because a skill
// already has it, in the words of the command that gave the name.
func (fk *forking) takenHint() string {
	if fk.rename {
		return "choose another name with '" + skillCommand("rename", fk.src.name, "<new>") + "'"
	}
	return "choose another name with --name <new>"
}

// except is the branch the fork's name may already be held by: a managed
// skill's own import branch, when the fork takes its place.
func (fk *forking) except() string {
	if fk.inPlace && fk.src.kind == lineage.KindManaged {
		return fk.src.name
	}
	return ""
}

// inPlaceRoom refuses a fork in the skill's place that the move into its
// worktree cannot make: a library entry that is a symlink of the user's,
// which the move would take for the directory and which leads to files
// that are not the library's to move, a skill directory that is itself a
// Git repository, a worktree path that is taken, and a library on another
// file system than agentx home, where the directory could only be copied,
// not moved.
func (inv *invocation) inPlaceRoom(fk *forking) error {
	name, libPath := fk.src.name, inv.libraryPath(fk.src.name)
	state, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	beside := "fork it beside the skill with '" + skillCommand("fork", name, "--name", "<new>") + "'"
	if target, isLink := home.LinkTarget(state); isLink {
		return fail(exitRefused, fmt.Sprintf("%s is a symlink to %s; forking %s in its place moves the library directory into the fork's worktree and would move the link rather than the files it leads to", quotedPath(libPath), quotedPath(target), sanitised(name)),
			"replace the link with the directory it points to, then run '"+skillCommand("fork", name)+"' again, or "+beside)
	}
	// A skill that is itself a Git repository would take its .git into the
	// worktree with it, ignored or not, and git run in the skill would find
	// that repository rather than the fork's branch.
	if own := filepath.Join(libPath, ".git"); lexists(own) {
		return fail(exitRefused, sanitised(name)+" is itself a Git repository, at "+quotedPath(own)+"; forking it in its place would move that repository into the fork's worktree, where git would take it for the fork",
			"move its .git out of the skill, then run '"+skillCommand("fork", name)+"' again, or "+beside)
	}
	if root := inv.worktreeRoot(name); lexists(root) {
		return fail(exitRefused, quotedPath(root)+" already exists", "move it aside, then run '"+skillCommand("fork", name)+"' again")
	}
	same, err := home.SameDevice(filepath.Dir(libPath), existingAncestor(inv.worktreesDir()))
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	if !same {
		return fail(exitRefused, "the library and agentx home are on different file systems, so "+sanitised(name)+" cannot be moved into its fork's worktree",
			"keep AGENTX_HOME and AGENTX_LIBRARY on one file system, or "+beside)
	}
	return nil
}

// existingAncestor is path, or the nearest of its parents that exists.
func existingAncestor(path string) string {
	for !lexists(path) && filepath.Dir(path) != path {
		path = filepath.Dir(path)
	}
	return path
}

// forkContent judges what the fork's creation commit holds and returns the
// root tree of that commit, before any rename, and its parents. A managed
// skill's directory is judged against its import commit and keeps the
// upstream's directory name; an unmanaged skill's and a plugin's are
// judged against nothing and named after the fork; a fork is forked from
// its tip, which checkForking found clean, its skill directory alone, see
// skillDirAlone. Git's ignore rules
// decide what is recorded, and a repository nested in the directory that
// they do not cover is refused, since git would record it as a link.
func (inv *invocation) forkContent(ctx context.Context, fk *forking) (string, []string, error) {
	src := fk.src
	if src.kind == lineage.KindFork {
		root, err := inv.skillDirAlone(ctx, fk.gitDir, src.rec, fk.dir)
		if err != nil {
			return "", nil, err
		}
		return root, []string{src.rec.Commit}, nil
	}
	if fk.inPlace {
		// What the library directory holds is captured before git reads it,
		// so that an edit made while git reads it shows as a directory that
		// no longer holds what was captured, and refuses the fork.
		state, err := home.State(inv.libraryPath(src.name))
		if err != nil {
			return "", nil, libraryFailure(inv.dirs.Library, err)
		}
		fk.captured = state
	}
	v := version{holds: func(string) bool { return false }} // compared with nothing: a branch of its own
	var parents []string
	fk.dir = fk.target
	if src.kind == lineage.KindManaged {
		v, parents, fk.dir = baseVersion(src.rec), []string{src.rec.Commit}, src.rec.Import.Dir()
	}
	j, err := inv.judgeFork(ctx, fk.gitDir, src.dir, v, false)
	switch {
	case err != nil:
		return "", nil, accountRepoFailure(err)
	case len(j.exposed) > 0:
		return "", nil, nestedRepoRefusal(src.name, j.exposed)
	case j.written == "":
		return src.rec.Tree, parents, nil // a managed skill that holds its base, by the fast path
	}
	root, err := inv.git.ReplaceEntry(ctx, fk.gitDir, "", fk.dir, j.written)
	if err != nil {
		return "", nil, accountRepoFailure(err)
	}
	return root, parents, nil
}

// skillDirAlone is the root tree of rec's tip, a fork's, with the skill
// directory dir as its one entry: the tip's own tree when that is all it
// holds, as it is for a fork agentx alone committed to, and otherwise a
// tree without the entries a commit made with git put beside the skill
// directory, such as a README at the worktree's root. A fork's branch holds
// its skill directory and nothing else, so a fork of a fork starts without
// them, and its creation commit is where they are deleted.
func (inv *invocation) skillDirAlone(ctx context.Context, gitDir string, rec lineage.Record, dir string) (string, error) {
	out, err := inv.git.Isolated(ctx, gitDir, "ls-tree", "-z", rec.Commit)
	if err != nil {
		return "", accountRepoFailure(err)
	}
	var subtree string
	others := 0
	for _, record := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			continue
		}
		if fields := strings.Fields(meta); path == dir && len(fields) == 3 && fields[1] == "tree" {
			subtree = fields[2]
		} else {
			others++
		}
	}
	if subtree == "" {
		return "", fail(exitAccountRepo, fmt.Sprintf("the branch %s holds no directory %s at its root", rec.Ref, dir),
			"run 'agentx doctor' and check the account repo it names")
	}
	if others == 0 {
		return rec.Tree, nil
	}
	root, err := inv.git.ReplaceEntry(ctx, gitDir, "", dir, subtree)
	if err != nil {
		return "", accountRepoFailure(err)
	}
	return root, nil
}

// renameSkill is root with the name in the frontmatter of the SKILL.md in
// its directory dir set to name, see renameFrontmatter: the one edit
// forking under a new name makes, an ordinary one with no record of its
// own.
func (inv *invocation) renameSkill(ctx context.Context, gitDir, root, dir, name string) (string, error) {
	path := dir + "/SKILL.md"
	out, err := inv.git.Isolated(ctx, gitDir, "ls-tree", "-z", root, "--", path)
	if err != nil {
		return "", accountRepoFailure(err)
	}
	meta, _, _ := strings.Cut(strings.TrimSuffix(out, "\x00"), "\t")
	fields := strings.Fields(meta)
	if len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		return "", fail(exitRefused, "the fork holds no SKILL.md file to write the name "+name+" into",
			"make SKILL.md a file the skill's .gitignore does not name, then run the command again")
	}
	bodies, err := source.ReadBlobs(ctx, inv.git, gitDir, []string{fields[2]})
	if err != nil {
		return "", accountRepoFailure(err)
	}
	renamed, err := renameFrontmatter([]byte(bodies[fields[2]]), name)
	if err != nil {
		return "", fail(exitRefused, "cannot write the name "+name+" into SKILL.md: "+err.Error(),
			"make SKILL.md UTF-8 text whose frontmatter is closed by a --- line, then run the command again")
	}
	tree, err := inv.git.EditTree(ctx, gitDir, root, []gitx.TreeEdit{{Path: path, Mode: fields[0], Content: renamed}})
	if err != nil {
		return "", accountRepoFailure(err)
	}
	return tree, nil
}

// forkSubject is the subject of a fork's creation commit, which names
// where the fork came from: a managed skill's upstream, with the version,
// a plugin, or the skill it was forked from.
func forkSubject(src forkSource, target string) string {
	from := sanitised(src.name)
	switch src.kind {
	case lineage.KindManaged:
		at := src.rec.Import.Source
		if src.rec.Import.Path != "" {
			at += "/" + src.rec.Import.Path
		}
		from = sanitised(at) + " at " + short(src.rec.Import.Commit)
	case kindPlugin:
		from = "plugin " + sanitised(src.plugin)
	}
	return "Fork " + target + " from " + from
}

// planBeside picks the configurations a fork made beside its source is
// placed into, and, for a plugin's skill, the note of every client that
// sees both copies. The fork goes where the source is: into every
// configuration that has a placement of the source at its own place,
// copies where copy_mode records them, and for a plugin's skill into every
// enabled configuration that has the plugin. A client that reads the
// library sees the fork whatever it is placed in, and is said to; a
// disabled configuration that has the plugin and does not read the library
// sees no fork, and gets no note.
func (inv *invocation) planBeside(fk *forking, snap scan.Snapshot) error {
	ids := map[string]bool{}
	src := fk.src
	switch {
	case src.kind == kindPlugin:
		for _, id := range pluginConfigurations(snap, src.plugin) {
			ids[id] = true
		}
		enabled, err := inv.placementTargets(nil)
		if err != nil {
			return err
		}
		// A configuration has the fork beside the plugin's copy when the fork
		// is placed into it or its client reads the library; one agentx
		// leaves alone, which has the plugin all the same, gets no note.
		universal := universalClients(snap)
		var sees []string
		for _, t := range enabled {
			if ids[t.id] {
				fk.targets = append(fk.targets, t)
				sees = append(sees, t.id)
			}
		}
		for _, id := range universal {
			if ids[id] && !containsString(sees, id) {
				sees = append(sees, id)
			}
		}
		fk.notes = pluginForkNotes(fk.target, src.plugin, sees, universal)
		return nil
	case src.held:
		s, err := inv.loadSettings()
		if err != nil {
			return err
		}
		modes, err := inv.copyModes(s)
		if err != nil {
			return err
		}
		detected := inv.detectedTargets()
		for _, p := range inv.placements(snap, src.lib, modes) {
			for _, t := range detected {
				if t.id == p.Configuration && p.Path == t.ownPlace(inv.dirs.Library, src.name) {
					ids[t.id] = true
				}
			}
		}
		for _, t := range detected {
			if ids[t.id] {
				fk.targets = append(fk.targets, t)
			}
		}
	}
	return nil
}

// pluginForkNotes are the notes a fork called name of a skill of plugin
// carries: one for each configuration that has the plugin, what its client
// does with the two copies, and one for each client that reads the
// library and does not have the plugin, which sees the fork all the same.
// Sorted by configuration. Pure.
func pluginForkNotes(name, plugin string, withPlugin, universal []string) []forkNoteEvent {
	has := map[string]bool{}
	var notes []forkNoteEvent
	for _, id := range withPlugin {
		has[id] = true
		note := scan.PluginForkNote(id)
		if note == "" {
			note = "which of the two copies wins is not documented"
		}
		notes = append(notes, forkNoteEvent{Configuration: id, Note: note})
	}
	for _, id := range universal {
		if !has[id] {
			notes = append(notes, forkNoteEvent{Configuration: id, Note: "it reads the library, so it sees " + name + " whether or not it has " + plugin})
		}
	}
	for i := range notes {
		notes[i].event, notes[i].Name, notes[i].Plugin = newEvent("fork_note"), name, plugin
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Configuration < notes[j].Configuration })
	return notes
}

// applyFork records and applies the fork under the lock, once everything
// it was planned from is read again: the branches, for a name another
// command took and for a source branch that moved, a merge an update left
// pending since, and the source fork's directory, which has to be as clean
// as it was. A fork in its place goes through forkInPlace; any other is a
// new fork beside its source, created as skill new creates one.
func (inv *invocation) applyFork(ctx context.Context, fk *forking, done *placements) error {
	src := fk.src
	records, err := inv.listLineage(ctx, fk.gitDir)
	if err != nil {
		return accountRepoFailure(err)
	}
	if err := takenRefusal(records, fk.target, fk.except(), fk.takenHint()); err != nil {
		return err
	}
	changed := fail(exitRefused, sanitised(src.name)+" changed while it was being forked, so nothing was changed", "run the command again")
	if src.kind == lineage.KindManaged || src.kind == lineage.KindFork {
		if rec, ok := records[src.name]; !ok || rec.Kind != src.kind || rec.Commit != src.rec.Commit {
			return changed
		}
	}
	switch src.kind {
	case lineage.KindManaged:
		if inv.mergePending(src.name) {
			return pendingMergeRefusal(src.name, "forked")
		}
	case lineage.KindFork:
		if _, err := inv.forkGuards(ctx, fk.site, fk.judged, "forked", true); err != nil {
			return err
		}
	}
	if !fk.inPlace {
		copiesOf := src.name
		if src.kind == kindPlugin {
			copiesOf = "" // a plugin's skill has no copy_mode of agentx's
		}
		return inv.createFork(ctx, fk.gitDir, fk.target, fk.dir, fk.commit, copiesOf, fk.targets, done)
	}
	return inv.forkInPlace(ctx, fk, records[src.name], changed)
}

// forkInPlace records and applies a fork that takes its source's place:
// the fork's branch at the creation commit, the managed skill's import
// branch and upstream-removed marker deleted once everything else is done,
// the fork's worktree, the library directory moved into it, and the
// library symlink to it where the directory was. The update candidate a
// check pinned for a managed skill stays, since the fork can still take
// that version in. Placements and copy_mode are left as they are: they
// name the library path, which leads to the fork. rec is the source's
// branch as read under the lock.
func (inv *invocation) forkInPlace(ctx context.Context, fk *forking, rec lineage.Record, changed error) error {
	name := fk.src.name
	libPath, root := inv.libraryPath(name), inv.worktreeRoot(name)
	live, err := home.State(libPath)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	fp, isDir := home.DirFingerprint(fk.captured)
	if !isDir || live != fk.captured {
		return changed
	}
	if lexists(root) {
		return fail(exitRefused, quotedPath(root)+" already exists", "move it aside, then run '"+skillCommand("fork", name)+"' again")
	}
	if err := os.MkdirAll(inv.worktreesDir(), 0o755); err != nil {
		return libraryFailure(inv.worktreesDir(), err)
	}
	skillDir := filepath.Join(root, fk.dir)
	link, err := inv.forkLink(libPath, skillDir)
	if err != nil {
		return libraryFailure(inv.dirs.Library, err)
	}
	m := home.NewMutation(inv.dirs.Home)
	m.Ref(fk.gitDir, lineage.ForkRef(name), "", fk.commit)
	if fk.src.kind == lineage.KindManaged {
		m.Ref(fk.gitDir, lineage.ManagedRef(name), rec.Commit, "")
		if rec.UpstreamRemoved != "" {
			m.Ref(fk.gitDir, lineage.UpstreamRemovedRef(name), rec.UpstreamRemoved, "")
		}
	}
	m.Worktree(fk.gitDir, root, strings.TrimPrefix(lineage.ForkRef(name), "refs/heads/"))
	m.Move(libPath, skillDir, fp)
	m.Link(libPath, link)
	return m.Apply(inv.refs(ctx))
}

// reportForked reads the machine again and reports the fork: its
// library_skill event, the source's too when it stays beside the fork, the
// note of every client that sees a plugin's copy beside the fork, and a
// confirmation with one row per configuration a fork beside its source
// was placed in.
func (inv *invocation) reportForked(ctx context.Context, fk *forking, done placements) error {
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	libs := librarySkills(inv.dirs.Library)
	lib, ok := libs[fk.target]
	if !ok {
		return fail(exitInternal, "the library holds no "+fk.target+" after forking it", "run 'agentx doctor' and check the library it names")
	}
	out := inv.out
	src, name := fk.src, sanitised(fk.src.name)
	if fk.inPlace {
		out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
		inv.summary = "forked " + name + "; the fork replaces it wherever it was"
		out.done("forked " + out.paint(heading, name) + "; the fork replaces it wherever it was")
		return nil
	}
	ev := sc.librarySkillEventFor(ctx, inv, snap, lib, targetIDs(done.placed))
	out.emit(ev)
	if from, ok := libs[src.name]; ok && src.held {
		out.emit(sc.librarySkillEventFor(ctx, inv, snap, from, nil))
	}
	for _, n := range fk.notes {
		out.emit(n)
		out.print(out.paint(infoStyle, glyphInfo), " ", n.Configuration, ": ", sanitised(n.Note))
	}
	what, stays := name, name+" stays as it was"
	if src.kind == kindPlugin {
		what, stays = name+" from plugin "+sanitised(src.plugin), "the plugin's copy stays as it was"
	}
	rows := inv.ownPlacements(fk.target, done.placed, ev.Placements)
	line := "forked " + out.paint(heading, what) + " as " + out.paint(heading, fk.target) + ": " + out.paint(noteStyle, plural(len(rows), "placement"))
	inv.summary = "forked " + what + " as " + fk.target + " in " + plural(len(done.placed), "configuration")
	if n := len(done.skipped); n > 0 {
		line += ", " + out.paint(warnStyle, plural(n, "placement")+" skipped")
		inv.summary += ", " + plural(n, "placement") + " skipped"
	}
	if fk.rename {
		// The rename removes the source next, and says so itself.
		out.done(line)
	} else {
		out.done(line + "; " + stays)
		inv.summary += "; " + stays
	}
	inv.printPlacementRows(fk.target, rows)
	inv.printUniversal(ev.Universal)
	inv.summary += universalClause(ev.Universal)
	return nil
}
