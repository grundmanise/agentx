package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/interrupt"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// A publish of a managed skill of a shared source, a source that is not
// the account remote, pushes the skill's edits to the branch of the source
// it is installed from: one commit on what the branch holds, whose one
// parent is the branch's tip and which changes the skill's folder alone,
// written with the user's own git identity and no trailer of agentx's. No
// fork branch, fork id or machine id ever reaches a shared source, and
// only a publish that names the skill pushes there: a bare publish covers
// the user's own skills alone. The account remote is never needed, read or
// fetched.
//
// A publish never merges, as for the account remote: when the source
// changed the skill since it was installed, the publish is refused, and an
// update takes the change in first. The push is leased on the commit the
// source's branch was fetched at, so a branch that moved between the fetch
// and the push is refused the same way, never pushed over.
//
// Once the source took the commit, the skill's import branch moves to the
// import commit of the version it published, the one an install of that
// commit writes, so the skill stays managed and reads current.

// sharedPublishState is what a publish of a managed skill of a shared
// source reads before it judges the skill's edits, see
// sharedPublishRefusal.
type sharedPublishState struct {
	name         string
	library      string     // the library directory, for the refusal of a skill it does not hold
	held         bool       // the library holds the skill's directory
	readable     bool       // the import branch records a version agentx can read
	url          string     // the canonical URL of the source the skill was installed from
	added        bool       // the settings hold that source
	pending      bool       // a merge with an update is pending for the skill
	removed      bool       // the last update check found the source no longer holds the skill
	unrecordable []string   // what the skill directory holds that git cannot record
	libPath      string     // the skill's library directory
	ident        gitx.Ident // the user's git identity, sanitised
}

// sharedPublishRefusal refuses, in this order, a publish to a shared
// source of a skill: the library does not hold, exit code 5; whose import
// branch records no version agentx can read, exit code 6; whose source was
// removed from this machine, exit code 5; with a merge pending, exit code
// 4; that the last update check found the source no longer holds, exit
// code 6; whose directory holds what git cannot record, exit code 6; and
// for a user with no git identity, exit code 6, since the commit goes to
// a repository of others under the user's own name, never the machine's.
// Nil is a skill whose edits can be judged. Pure.
func sharedPublishRefusal(st sharedPublishState) *failure {
	again := "run '" + publishCommand(st.name) + "' again"
	switch {
	case !st.held:
		return refuse(exitNotFound, fmt.Sprintf("the library holds no skill called %q at %s", st.name, st.library),
			"run 'agentx skill list' to see what the library holds")
	case !st.readable:
		return refuse(exitRefused, "the import branch "+lineage.ManagedRef(st.name)+" records no version agentx can read",
			"run 'agentx doctor' and check the account repo it names")
	case !st.added:
		return refuse(exitNotFound, st.name+" was installed from "+shownURL(st.url)+", which was removed from this machine, so it cannot be published",
			"run 'agentx source add "+sourceAddArg(st.url, "")+"' to add it again, then publish again").wrap(errSourceRemoved)
	case st.pending:
		return pendingMergeRefusal(st.name, "published")
	case st.removed:
		return refuse(exitRefused, "the last update check found that "+shownURL(st.url)+" no longer holds "+st.name+", so it cannot be published",
			"run '"+checkUpdatesCommand+"' once the source holds it again, then publish again")
	case len(st.unrecordable) > 0:
		paths := make([]string, len(st.unrecordable))
		for i, p := range st.unrecordable {
			paths[i] = quotedPath(st.libPath + "/" + p)
		}
		return refuse(exitRefused, fmt.Sprintf("%s holds %s, which git cannot record, so it cannot be published", st.name, strings.Join(paths, ", ")),
			"move it out of the skill, then "+again)
	case st.ident.Name == "" || st.ident.Email == "":
		return refuse(exitRefused, "git has no user.name and user.email for this machine, and a publish to "+shownURL(st.url)+" is a commit under your own name",
			"set them with git config --global user.name \"Your Name\" and git config --global user.email you@example.com, then "+again)
	}
	return nil
}

// sharedPublish is one publish of a managed skill of a shared source, as
// it goes: what it read and what it found.
type sharedPublish struct {
	name, message string
	gitDir        string
	rec           lineage.Record
	entry         home.Source // the settings entry of the source
	ident         gitx.Ident
	written       string // the tree git wrote of the skill directory, its edits included
	tip           string // the commit the source's branch was fetched at
	branch        string // the branch the skill is published to
	commit        string // the commit the branch holds once published
	imp           string // the import commit of the version published
	run           string // the staging namespace of that import commit
	retried       bool   // the source moved the branch once between the fetch and the push, and was fetched again
}

// publishShared publishes the managed skill called name to the shared
// source it was installed from, see the top of this file. After any
// unfinished journal is finished, the skill is judged from what this
// machine holds, see sharedPublishRefusal; a skill directory that holds
// its base version has nothing to publish and is up to date, with nothing
// fetched. Otherwise the source alone is fetched and checked, as skill
// check-updates checks it, and the skill's version there is compared with
// the one it was installed at: another is moved, refused, and one that is
// already what the directory holds was published before, by a run that
// stopped after the push, and is finished as up to date. Then the commit is
// written and the import commit of the version it holds, before the lock,
// and pushed, leased on the fetched tip; once the source took it, one
// journaled mutation moves the import branch. A publish refused anywhere
// leaves the library and the lineage as they were, and the edits where
// they are.
func (inv *invocation) publishShared(ctx context.Context, name, message string) error {
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	p := &sharedPublish{name: name, message: strings.TrimSpace(message), gitDir: gitx.AccountRepoPath(inv.dirs.Home)}
	records, err := lineage.List(ctx, inv.git, p.gitDir)
	if err != nil {
		return accountRepoFailure(err)
	}
	rec, ok := records[name]
	switch {
	case ok && rec.Kind == lineage.KindFork: // a journal finished above made it one of your own
		return inv.publish(ctx, name, message)
	case !ok:
		return inv.noLibrarySkill(name)
	}
	p.rec = rec
	s, err := inv.loadSettings()
	if err != nil {
		return err
	}
	st := sharedPublishState{name: name, library: inv.dirs.Library, readable: rec.HasImport, url: rec.Import.Source,
		libPath: inv.libraryPath(name), pending: inv.mergePending(name), removed: rec.UpstreamRemoved != ""}
	for _, entry := range sharedSources(s.Sources) {
		if entry.URL == rec.Import.Source {
			p.entry, st.added = entry, true
		}
	}
	lib, held := librarySkill(inv.dirs.Library, name)
	st.held = held
	var tree treeid.Tree
	if held {
		if tree, err = inv.readLibraryTree(lib.Path); err != nil {
			return err
		}
		st.unrecordable = tree.Unrecordable
	}
	// The identity and the user's core.excludesFile come in one read of
	// their configuration, before git reads the skill directory, so the
	// ignore rules the judgement uses come with it.
	config := inv.git.ReadUserConfig(ctx, p.gitDir)
	inv.excludes.Do(func() { inv.excludesPath = config.ExcludesFile })
	p.ident, st.ident = config.Ident.Sanitised(), config.Ident.Sanitised()
	if f := sharedPublishRefusal(st); f != nil {
		return f
	}
	j, err := inv.judgeDir(ctx, p.gitDir, lib.ResolvedPath, tree, baseVersion(rec), false)
	if err != nil {
		return accountRepoFailure(err)
	}
	if j.holds {
		if p.message != "" {
			inv.out.warn(sanitised(name) + " has no edits to record, so -m was not used")
		}
		return inv.reportShared(ctx, p, publishUpToDate, nil)
	}
	p.written = j.written
	if f := inv.judgeShared(ctx, p); f != nil {
		return inv.reportShared(ctx, p, outcomeOf(f), f)
	}
	return inv.pushShared(ctx, p)
}

// outcomeOf is the publish outcome of a refusal of publishShared: moved
// for a source that changed the skill, refused for any other.
func outcomeOf(f *failure) string {
	if errors.Is(f, errSourceMoved) {
		return publishMoved
	}
	return publishRefused
}

// errSourceMoved marks the refusal of a skill whose source changed it since
// it was installed, outcome moved.
var errSourceMoved = errors.New("the source changed the skill")

// judgeShared fetches the source, checks it as skill check-updates does,
// and judges what it holds of the skill against what the skill was
// installed at; then it reads the branch the skill is published to and
// writes the commit and the import commit of the version it holds, see
// writeShared. A skill whose version the source holds already, published
// by a run that stopped after its push, is left with p.commit the source's
// tip and nothing to push.
func (inv *invocation) judgeShared(ctx context.Context, p *sharedPublish) *failure {
	url := p.rec.Import.Source
	rep, err := inv.checkUpdates(ctx, false, url)
	if err != nil {
		return inv.sharedFailure(err)
	}
	for _, cf := range rep.failures {
		if cf.fetch && cf.source == url {
			return cf.f
		}
	}
	if rep.idle { // the source was removed while the check ran
		return refuse(exitNotFound, p.name+" was installed from "+shownURL(url)+", which was removed from this machine, so it cannot be published",
			"run 'agentx source add "+sourceAddArg(url, "")+"' to add it again, then publish again").wrap(errSourceRemoved)
	}
	records, err := lineage.List(ctx, inv.git, p.gitDir)
	if err != nil {
		return inv.sharedFailure(accountRepoFailure(err))
	}
	switch rec := records[p.name]; {
	case rec.Commit != p.rec.Commit:
		return refuse(exitRefused, p.name+" changed while it was being published, so nothing was published",
			"run '"+publishCommand(p.name)+"' again to publish it as it is now")
	case rec.UpstreamRemoved != "":
		return refuse(exitRefused, shownURL(url)+" no longer holds "+p.name+", so it cannot be published",
			"run '"+checkUpdatesCommand+"' once the source holds it again, then publish again")
	}
	src := source.Source{URL: url, Ref: p.entry.Pin}
	out, err := inv.git.Isolated(ctx, p.gitDir, "rev-parse", "--verify", "--end-of-options", source.Ref(src.ID())+"^{commit}")
	if err != nil {
		return inv.sharedFailure(accountRepoFailure(err))
	}
	p.tip = strings.TrimSpace(out)
	subpath := p.rec.Import.Path
	theirs, err := inv.subtree(ctx, p.gitDir, p.tip, subpath)
	if err != nil {
		return inv.sharedFailure(accountRepoFailure(err))
	}
	if theirs == "" {
		return refuse(exitRefused, shownURL(url)+" no longer holds "+p.name+", so it cannot be published",
			"run '"+checkUpdatesCommand+"' to record that, or '"+skillCommand("remove", p.name)+"' to remove it")
	}
	versions, _, err := inv.listVersions(ctx, p.gitDir, src, p.tip, []source.Skill{{Subpath: subpath, Name: p.name, Tree: theirs}})
	if err != nil {
		return inv.sharedFailure(err)
	}
	dir := p.rec.Import.Dir()
	switch imported := versions[0].version().ImportTree(); imported {
	case treeid.Wrap(dir, p.written):
		p.commit = p.tip // published already, by a run that stopped after its push
		return inv.importShared(ctx, p, src, p.tip)
	case p.rec.Tree:
	default:
		return refuse(exitRefused, shownURL(url)+" changed "+p.name+" since it was installed, so it was not published",
			"run '"+skillCommand("update", p.name)+"' to take the change in, then publish again").wrap(errSourceMoved)
	}
	branch, err := source.PushBranch(ctx, inv.git, p.gitDir, src)
	switch {
	case errors.Is(err, source.ErrNotABranch):
		return refuse(exitRefused, p.name+" has no branch of "+shownURL(url)+" to publish to: "+trimGit(err.Error()),
			"add the source at the branch to publish to with 'agentx source add "+sanitised(url)+"#<branch>', then update the skill from it and publish again")
	case err != nil:
		return unreachableSource(url, err)
	}
	p.branch = branch
	return inv.writeShared(ctx, p, src)
}

// sharedFailure is the refusal a publish to a shared source reports for
// err once its publish event is out: the one the run would report, see
// knownFailure, so a lock another command holds is exit code 7 here too,
// and an internal error, exit code 10, for any other.
func (inv *invocation) sharedFailure(err error) *failure {
	if f, ok := knownFailure(inv.dirs.Home, err); ok {
		return f
	}
	return failureOf(err)
}

// unreachableSource is a shared source a publish could not reach, exit
// code 3.
func unreachableSource(url string, err error) *failure {
	return refuse(exitSource, "cannot reach "+shownURL(url)+": "+trimGit(err.Error()),
		"check the URL and that you can reach it with git; a private repository needs a git credential helper (git config credential.helper) or an SSH key")
}

// subtree is the tree commit holds at subpath, the root's for "", and ""
// when it holds no directory there.
func (inv *invocation) subtree(ctx context.Context, gitDir, commit, subpath string) (string, error) {
	if subpath == "" {
		out, err := inv.git.Isolated(ctx, gitDir, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
		return strings.TrimSpace(out), err
	}
	out, err := inv.git.Isolated(ctx, gitDir, "ls-tree", "-z", commit, "--", subpath)
	if err != nil {
		return "", err
	}
	for _, record := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if ok && path == subpath && len(fields) == 3 && fields[1] == "tree" {
			return fields[2], nil
		}
	}
	return "", nil
}

// writeShared writes, before the lock, the commit a publish pushes: the
// source's tip with what changed in the skill since it was installed, its
// base version against the tree git wrote of its directory, applied to the
// skill's folder alone, see gitx.ApplyChanges, with the tip as its one
// parent, the user's identity as author and committer, the message -m
// gives or `Update <folder>`, and no trailer. A commit that would change
// anything but the skill's folder is an internal error, exit code 10. Then
// the import commit of the version it holds, which has to hold what the
// skill directory holds: a file an install of the commit would leave out
// is refused, exit code 6, since the skill would not read as published.
func (inv *invocation) writeShared(ctx context.Context, p *sharedPublish, src source.Source) *failure {
	subpath, dir := p.rec.Import.Path, p.rec.Import.Dir()
	base := p.rec.Commit + ":" + dir
	root, err := inv.git.ApplyChanges(ctx, p.gitDir, p.tip, subpath, base, p.written)
	switch {
	case errors.Is(err, gitx.ErrUnpublishable):
		what := strings.TrimPrefix(err.Error(), gitx.ErrUnpublishable.Error()+": ")
		return refuse(exitRefused, what+", which a publish never pushes, so "+p.name+" was not published",
			"replace it with the file it leads to, or remove it, then run '"+publishCommand(p.name)+"' again")
	case err != nil:
		return inv.sharedFailure(accountRepoFailure(err))
	}
	message := p.message
	if message == "" {
		folder := subpath
		if folder == "" {
			folder = dir
		}
		message = "Update " + folder
	}
	p.commit, err = inv.git.CommitTreeAs(ctx, p.gitDir, p.ident, gitDate(time.Now(), inv.env["TZ"]), root, []string{p.tip}, message+"\n")
	if err != nil {
		return inv.sharedFailure(accountRepoFailure(err))
	}
	out, err := inv.git.Isolated(ctx, p.gitDir, "diff-tree", "-r", "-z", "--no-renames", "--name-only", p.tip, p.commit)
	if err != nil {
		return inv.sharedFailure(accountRepoFailure(err))
	}
	if path := outside(out, subpath); path != "" {
		return refuse(exitInternal, "the commit that publishes "+p.name+" would change "+quotedPath(path)+", outside "+quotedPath(subpath)+", so nothing was published",
			"report this as a bug of agentx")
	}
	return inv.importShared(ctx, p, src, p.commit)
}

// outside is the first path of diff-tree --name-only -z output that is not
// under subpath, "" when every one is or subpath is the root. Pure.
func outside(diffNamesZ, subpath string) string {
	if subpath == "" {
		return ""
	}
	for _, path := range strings.Split(diffNamesZ, "\x00") {
		if path != "" && !strings.HasPrefix(path, subpath+"/") {
			return path
		}
	}
	return ""
}

// importShared writes the import commit of the version commit holds at the
// skill's folder onto a staging ref of a run of its own, as an install of
// that commit would write it, and refuses one whose tree is not the skill
// directory's, see writeShared.
func (inv *invocation) importShared(ctx context.Context, p *sharedPublish, src source.Source, commit string) *failure {
	subpath, dir := p.rec.Import.Path, p.rec.Import.Dir()
	tree, err := inv.subtree(ctx, p.gitDir, commit, subpath)
	if err != nil {
		return inv.sharedFailure(accountRepoFailure(err))
	}
	versions, missing, err := inv.listVersions(ctx, p.gitDir, src, commit, []source.Skill{{Subpath: subpath, Name: p.name, Tree: tree}})
	if err != nil {
		return inv.sharedFailure(err)
	}
	v := versions[0]
	if f := inv.importable(v, nil, src); f != nil {
		return f
	}
	var refused *failure
	ready, err := inv.fillVersions(ctx, p.gitDir, src, versions, missing, func(_ *imported, f *failure) { refused = f })
	switch {
	case err != nil:
		return inv.sharedFailure(err)
	case refused != nil:
		return refused
	case len(ready) == 0:
		return refuse(exitInternal, "the version of "+p.name+" a publish would record cannot be read", "report this as a bug of agentx")
	}
	p.run = lineage.NewRun()
	commits, trees, err := lineage.WriteAll(ctx, inv.git, p.gitDir, p.run, []lineage.Version{v.version()})
	if err != nil {
		return inv.sharedFailure(accountRepoFailure(err))
	}
	p.imp = commits[0]
	if trees[0] == treeid.Wrap(dir, p.written) {
		return nil
	}
	inv.dropImporting(ctx, p.gitDir, p.run, 1)
	p.run = ""
	path := "a file"
	if out, err := inv.git.Isolated(ctx, p.gitDir, "diff-tree", "-r", "-z", "--no-renames", "--name-only", p.written, commits[0]+":"+dir); err == nil {
		if first, _, _ := strings.Cut(out, "\x00"); first != "" {
			path = quotedPath(first)
		}
	}
	return refuse(exitRefused, path+" cannot be published: agentx installs skills without it, so "+p.name+" was not published",
		"move it out of the skill, then run '"+publishCommand(p.name)+"' again")
}

// pushShared pushes the commit judgeShared wrote to the branch, leased on
// the tip the source's branch was fetched at, and reads what the source
// answered, see gitx.ClassifyPush: a branch that moved since is fetched
// and judged again, once, and moved the second time, as a skill the
// source changed is; one the host declined, by a hook, a
// protected branch or a rule of its own, is exit code 6 naming its reason;
// one this machine may not push to is exit code 3, and so is a push git
// could not make and one whose answer is not known, the host's failure or
// no status for the branch. Nothing is ever pushed again with force. A pushed commit
// is then finished, see finishShared. A skill published already pushes
// nothing.
func (inv *invocation) pushShared(ctx context.Context, p *sharedPublish) error {
	if p.branch == "" {
		return inv.finishShared(ctx, p, publishUpToDate)
	}
	url := p.rec.Import.Source
	s, found, stderr, exit, err := inv.git.PushBranch(ctx, p.gitDir, source.RemoteName(source.ID(url)), p.commit, p.branch, p.tip)
	if err != nil {
		inv.dropImporting(ctx, p.gitDir, p.run, 1)
		return inv.reportShared(ctx, p, publishRefused, unreachableSource(url, err))
	}
	again := "run '" + publishCommand(p.name) + "' again"
	class, reason := gitx.ClassifyPush(s, found, stderr, exit)
	var outcome string
	var f *failure
	switch {
	case class == gitx.PushPushed:
		return inv.finishShared(ctx, p, publishPushed)
	case class == gitx.PushUpToDate:
		return inv.finishShared(ctx, p, publishUpToDate)
	case class == gitx.PushMoved && !p.retried:
		// The branch moved between the fetch and the push. The source is
		// fetched and judged again, once: a change to other folders is
		// published on top of, and one to the skill is refused as the
		// first judgement refuses it, with its update candidate pinned, so
		// that the update the refusal names takes it in.
		inv.dropImporting(ctx, p.gitDir, p.run, 1)
		p.retried, p.branch, p.commit, p.imp, p.run = true, "", "", "", ""
		if f := inv.judgeShared(ctx, p); f != nil {
			return inv.reportShared(ctx, p, outcomeOf(f), f)
		}
		return inv.pushShared(ctx, p)
	case class == gitx.PushMoved:
		outcome, f = publishMoved, refuse(exitRefused, shownURL(url)+" moved "+p.branch+" since it was fetched, so "+p.name+" was not published",
			again+", which checks the source first and says what to do when it changed "+p.name)
	case class == gitx.PushDeclined:
		outcome, f = publishDeclined, refuse(exitRefused, shownURL(url)+" declined "+p.branch+": "+sanitised(reason),
			"agentx never forces a push; once "+shownURL(url)+" takes it, "+again)
	case class == gitx.PushDenied:
		outcome, f = publishDenied, refuse(exitSource, shownURL(url)+" did not let this machine push "+p.branch+": "+sanitised(reason),
			deniedHint(gitx.DeniedFix(reason))+", then "+again)
	case found || exit == 0: // the host answered, but not that it took the branch
		outcome, f = publishRefused, refuse(exitSource, "whether "+shownURL(url)+" took "+p.branch+" is not known: "+sanitised(reason),
			again+"; if the source took it, that finishes the publish")
	default:
		outcome, f = publishRefused, unreachableSource(url, errors.New(reason))
	}
	inv.dropImporting(ctx, p.gitDir, p.run, 1)
	return inv.reportShared(ctx, p, outcome, f)
}

// finishShared moves, under the lock, the skill's import branch from the
// import commit it was installed at to the one of the version it
// published, and deletes its update candidate when that is the same
// commit, as a check run after the push pinned it; one journaled mutation
// with each ref's value as read before as its expected old value. The
// source holds the commit already, so the finish waits for a lock another
// command holds, for a while, and an interrupt does not stop it. When it
// still cannot move the import branch, a ref that moved meanwhile, by an
// update or a check, a lock that stayed held or a journal that could not
// be finished, the publish is reported all the same, with a warning that
// says how the import branch catches up: a check and an update take the
// version in, and a publish run again finishes as up to date.
func (inv *invocation) finishShared(ctx context.Context, p *sharedPublish, outcome string) error {
	name := p.name
	undo := interrupt.Uninterruptible(ctx)
	waiting, cancel := context.WithTimeout(undo, takeBackBound(ctx))
	defer cancel()
	journaled := false
	err := home.MutateWaiting(waiting, inv.dirs.Home, inv.refs(undo), func() error {
		candidate, err := inv.git.Refs(undo).RefValues(p.gitDir, []string{lineage.CandidateRef(name)})
		if err != nil {
			return accountRepoFailure(err)
		}
		m := home.NewMutation(inv.dirs.Home)
		m.Ref(p.gitDir, lineage.ManagedRef(name), p.rec.Commit, p.imp)
		if candidate[lineage.CandidateRef(name)] == p.imp {
			m.Ref(p.gitDir, lineage.CandidateRef(name), p.imp, "")
		}
		err = m.Apply(inv.refs(undo))
		journaled = m.Journaled()
		return err
	})
	if !journaled || err == nil {
		// Recovery of a journal that could not be finished needs the
		// import commit the staging ref holds.
		inv.dropImporting(ctx, p.gitDir, p.run, 1)
	}
	switch {
	case errors.Is(err, home.ErrMovedBeforeApply):
		inv.out.warnWith(sanitised(name)+" changed while it was being published, so its import branch was left as it is",
			"run '"+checkUpdatesCommand+"' and '"+skillCommand("update", name)+"' to take the published version in")
	case errors.Is(err, home.ErrLocked), errors.Is(err, home.ErrRecovery):
		inv.out.warnWith(shownURL(p.rec.Import.Source)+" holds what "+sanitised(name)+" published, but its import branch was not moved: "+err.Error(),
			"run '"+publishCommand(name)+"' again to record it")
	case err != nil:
		return mutationFailure(err)
	}
	return inv.reportShared(ctx, p, outcome, nil)
}

// reportShared reports the publish of a skill of a shared source: one
// progress event of phase publish, the publish event, naming the source
// and, once pushed, the branch, and the line, then, for a publish that
// moved the import branch, the library_skill event of the skill as the
// machine now has it. f is the refusal of a publish that did not go
// through, returned once its event is out.
func (inv *invocation) reportShared(ctx context.Context, p *sharedPublish, outcome string, f *failure) error {
	out, name, url := inv.out, sanitised(p.name), p.rec.Import.Source
	published := p.imp != "" && f == nil
	ev := publishEvent{event: newEvent("publish"), Name: p.name, Outcome: outcome, Source: url}
	if published {
		ev.Commit, ev.Branch = p.commit, p.branch
	}
	out.emit(progressEvent{event: newEvent("progress"), Phase: "publish", Subject: p.name, Current: 1, Total: 1})
	out.emit(ev)
	if f != nil {
		return f
	}
	painted := out.paint(heading, name)
	switch {
	case outcome == publishPushed:
		inv.summary = "published " + name
		out.done("published " + painted + " to " + sanitised(p.branch) + " of " + shownURL(url) + " as " + short(p.commit))
	case published:
		inv.summary = name + " is up to date on " + shownURL(url)
		out.print(painted, " is up to date on ", shownURL(url))
	default:
		inv.summary = name + " has no edits to publish"
		out.print(painted, " has no edits to publish")
	}
	if published {
		return inv.reportRecorded(ctx, []string{p.name})
	}
	return nil
}
