package cli

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// A publish records the edits of one of your skills, what its skill
// directory holds that its branch does not, as one commit on its branch,
// and pushes the branch to the account remote. Edits stay on this machine
// until they are published: there is no step between. It pushes only the
// skill's own branch, to the branch of the same name, never forced. A
// publish never merges: when the account remote holds commits the skill
// lacks, another machine's, the skill is not pushed and nothing is
// recorded, and an update takes them in, see updateRun.accountStep. A
// branch of the same name that is another skill, by its fork id, is never
// pushed over.

// publishEvent is what one publish did with one skill.
type publishEvent struct {
	event
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Commit  string `json:"commit,omitempty"` // what the remote's branch holds now, when the publish knows it
	Source  string `json:"source"`           // the URL of the source the skill is published to
}

// The outcomes of a publish, one per skill.
const (
	publishPushed   = "pushed"     // the account remote's branch now holds the skill's tip
	publishUpToDate = "up to date" // it held it already, and the skill had no edits to record
	publishRejected = "rejected"   // the account remote refused the push; see the error
	publishRefused  = "refused"    // see the error
	// publishMoved is a skill the account remote holds commits of that it
	// lacks, published from another machine, while it holds commits or
	// edits of its own the remote lacks: the two diverged, and an update
	// has to take them in before it can be published. See the error.
	publishMoved = "moved"
	// publishBehind is a skill whose tip the account remote's branch holds
	// in its history, with commits another machine published on top, and
	// which holds no edits: it has nothing to publish, and is not refused.
	publishBehind = "behind"
)

func newSkillPublishCommand(inv *invocation) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "publish [<name>]",
		Short: "Publish the edits of one of your skills, or of all of them, to the account remote",
		Long: "Publish the skill called <name>, or with no name every skill of your own: record\n" +
			"the edits its directory holds as one commit on its branch, then push the branch\n" +
			"to the account remote. Edits stay on this machine until you publish them. The\n" +
			"commit's message names the skill and the files it changes, unless -m gives one.\n" +
			"Files git ignores are never published. When the account remote holds changes\n" +
			"another machine published, nothing is recorded or pushed: run 'agentx skill\n" +
			"update <name>' to take them in first, then publish again. A push the account\n" +
			"remote rejects is reported, never forced, and a branch of the same name that is\n" +
			"another skill is never pushed over.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("message") && strings.TrimSpace(message) == "" {
				return fail(exitUsage, "-m needs a message", "say what the edits do, or leave -m out for the message agentx writes")
			}
			if err := checkMessage(message); err != nil {
				return err
			}
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return inv.publish(cmd.Context(), name, message)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "the message of the commit that records the edits, in place of the one agentx writes; its first line is the subject")
	return cmd
}

// checkMessage refuses, before anything is looked up, a message -m gives
// that ends in a trailer agentx writes itself, which agentx would not read
// back from the commit, exit code 1. Every commit of a publish carries the
// message as writeCommits splits it.
func checkMessage(message string) error {
	text := strings.TrimSpace(message)
	if text == "" {
		return nil
	}
	subject, body, _ := strings.Cut(text, "\n")
	if _, err := lineage.ForkMessage(subject, body, lineage.ForkTrailers{}); errors.Is(err, lineage.ErrForkTrailer) {
		return trailerInMessage()
	}
	return nil
}

// trailerInMessage is the usage error of a message -m gives that ends in a
// trailer agentx writes itself.
func trailerInMessage() error {
	return fail(exitUsage, "-m may not end in a trailer agentx writes itself: "+lineage.TrailerBase+", "+lineage.TrailerForkID+" or "+lineage.TrailerMachine,
		"leave those lines out of the message, or move them out of its last paragraph")
}

// publishCommand is the command line that publishes the skill called name.
func publishCommand(name string) string { return skillCommand("publish", name) }

// publishing is one skill of a publish: what was found of it and what the
// publish did with it.
type publishing struct {
	name     string
	outcome  string
	commit   string
	edits    *committing // the edits its directory holds, nil when it holds its tip
	judged   bool        // its edits were judged; a skill whose directory cannot be judged is refused
	recorded bool        // the edits were recorded on its branch
	push     bool        // its branch goes in the push
	f        *failure
}

// publish publishes the skill called name, or with no name every skill of
// your own on this machine. A named skill is judged first from what this
// machine holds, see publishable, so a skill a publish cannot cover is
// refused before the account remote is looked up. Then the account remote
// is fetched once, and each skill is judged against its branch there, as
// the fetch left it, see judgePublish. The edits of every skill that goes
// on are recorded, see recordEdits, after the fetch and every refusal, so
// a remote out of reach or one that moved on records nothing. Then every
// skill with something to push goes in one push, see gitx.Push, and a ref
// the remote rejects is reported, exit code 6, never forced. One progress
// event per skill follows, then its publish event, then the library_skill
// event of each skill whose edits were recorded. Last, the old name's
// branch of a skill renamed here goes, see dropRenamed.
func (inv *invocation) publish(ctx context.Context, name, message string) error {
	if name != "" {
		if err := inv.publishable(ctx, name); err != nil {
			return err
		}
	}
	gitDir, entry, account, err := inv.accountRemote(ctx)
	if err != nil {
		return err
	}
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	if err := inv.fetchRemote(ctx, gitDir, account, entry.URL); err != nil {
		return err
	}
	records, err := inv.forkRecords(ctx, gitDir)
	if err != nil {
		return err
	}
	names := remoteSelection(records)
	if name != "" {
		if records[name].Kind != lineage.KindFork {
			// A journal finished above took the skill away.
			return inv.noLibrarySkill(name)
		}
		names = []string{name}
	}
	if len(names) == 0 {
		inv.summary = "no skill to publish"
		inv.out.print("You have no skill of your own to publish. Create one with ", inv.out.paint(label, "agentx skill new <name>"), " or ", inv.out.paint(label, "agentx skill fork <name>"), ".")
		return nil
	}
	remote, err := inv.readRemoteForks(ctx, gitDir, account, records, names)
	if err != nil {
		return err
	}
	// The writer reads the user's identity and their core.excludesFile in
	// one read of their configuration, before git reads any skill
	// directory, so the ignore rules every judgement uses come with it.
	w, err := inv.newForkWriter(ctx, gitDir)
	if err != nil {
		return err
	}
	list := make([]*publishing, 0, len(names))
	byName := map[string]*publishing{}
	var ready []*committing
	edited, allJudged := false, true
	for _, n := range names {
		p, err := inv.judgePublish(ctx, gitDir, records[n], remote)
		if err != nil {
			return err
		}
		edited, allJudged = edited || p.edits != nil, allJudged && p.judged
		if p.push && p.edits != nil {
			ready = append(ready, p.edits)
		}
		list, byName[n] = append(list, p), p
	}
	// A skill whose directory could not be judged may hold edits: its
	// refusal says what is wrong, and -m is not said to be unused while
	// any skill of the run was not judged.
	if message != "" && allJudged && !edited {
		what := "no skill has edits to record"
		if name != "" {
			what = sanitised(name) + " has no edits to record"
		}
		inv.out.warn(what + ", so -m was not used")
	}
	if len(ready) > 0 {
		recorded, err := inv.recordEdits(ctx, w, gitDir, ready, message, publishWords, func(n string, f *failure) {
			p := byName[n]
			p.push, p.outcome, p.f = false, publishRefused, f
		})
		if err != nil {
			return err
		}
		for _, c := range recorded {
			byName[c.site.name].recorded = true
		}
	}
	var branches []string
	for _, p := range list {
		if p.push {
			branches = append(branches, strings.TrimPrefix(lineage.ForkRef(p.name), "refs/heads/"))
		}
	}
	if len(branches) > 0 {
		if err := inv.pushForks(ctx, gitDir, account, entry.URL, list, branches); err != nil {
			return inv.pushFailed(ctx, list, name, err)
		}
	}
	err = inv.reportPublished(ctx, list, entry.URL)
	inv.dropRenamed(ctx, gitDir, account, records, remote, list)
	return err
}

// pushFailed answers a push that failed, err, after edits may have been
// recorded: the library_skill event of each skill whose edits were, and,
// for a push git could not make at all, exit code 3, a hint that the next
// publish pushes them. The publish of the skill called name, or with no
// name every skill of your own.
func (inv *invocation) pushFailed(ctx context.Context, list []*publishing, name string, err error) error {
	var recorded []string
	for _, p := range list {
		if p.recorded {
			recorded = append(recorded, p.name)
		}
	}
	if len(recorded) == 0 {
		return err
	}
	if rerr := inv.reportRecorded(ctx, recorded); rerr != nil {
		return rerr
	}
	again := "agentx skill publish"
	if name != "" {
		again = publishCommand(name)
	}
	f := failureOf(err)
	if f.status == exitSource {
		f.hint += "; the edits were recorded, so run '" + again + "' again once the account remote can be reached"
	}
	return f
}

// publishable refuses, before the account remote is looked up or fetched,
// a named skill a publish cannot cover, from what this machine holds:
// one it does not hold, exit code 5, with the hint to install it when the
// account remote held it at the last fetch; a managed skill of a shared
// source, exit code 6, which agentx cannot publish to yet; and an
// unmanaged skill, exit code 6, which has no source to publish to. One of
// your own skills, a fork branch's, passes.
func (inv *invocation) publishable(ctx context.Context, name string) error {
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	// The records are read without the fork warnings, which the read after
	// finishJournals gives once any journal left is finished.
	records := map[string]lineage.Record{}
	if exists {
		if records, err = lineage.List(ctx, inv.git, gitDir); err != nil {
			return accountRepoFailure(err)
		}
	}
	rec, ok := records[name]
	if ok && rec.Kind == lineage.KindFork {
		return nil
	}
	_, held := librarySkill(inv.dirs.Library, name)
	// published is whether the account remote held a skill of the name at
	// the last fetch, read without fetching: a fork of the name made here
	// would be another skill of the name, which a publish refuses, so the
	// hint offers to install the one published instead.
	published := false
	if _, remote, set, err := inv.accountSource(); err == nil && set {
		published = inv.remoteHolds(ctx, remote, name)
	}
	switch {
	case ok:
		where := "its source"
		if rec.HasImport && rec.Import.Source != "" {
			where = shownURL(rec.Import.Source)
		}
		hint := "to publish your own version of " + sanitised(name) + ", fork it with '" + skillCommand("fork", name) + "' and publish that"
		if published {
			hint += "; or install the one you published in its place with '" + accountAddCommand(name) + "'"
		}
		return fail(exitRefused, "publishing to "+where+" is not supported yet", hint)
	case held:
		hint := "make it one of your own skills with '" + skillCommand("fork", name) + "', then publish it"
		if published {
			hint += "; or move " + quotedPath(inv.libraryPath(name)) + " aside and install the one you published with '" + accountAddCommand(name) + "'"
		}
		return fail(exitRefused, sanitised(name)+" is not managed, so it has no source to publish to", hint)
	case published:
		f := failureOf(inv.noLibrarySkill(name))
		f.hint = "it is one of your skills on the account remote; install it with '" + accountAddCommand(name) + "'"
		return f
	}
	return inv.noLibrarySkill(name)
}

// judgePublish decides what the publish does with the skill whose branch
// is rec, see publish, before anything is written. Its edits are judged
// first, see judgeEdits; a skill whose directory cannot be judged is
// refused. Then its branch on the account remote: a skill the remote
// holds no branch of, or an ancestor of its tip on, is pushed, its edits
// recorded first; one whose tip the remote holds already is pushed when it
// has edits, and otherwise up to date; one whose remote branch is another
// skill is refused, exit code 6; one whose tip the remote branch descends
// from is behind, and not pushed, when it has no edits; and one the remote
// holds commits of that it lacks while it holds commits or edits of its
// own is refused, outcome moved, exit code 6, since a publish never
// merges: what the account remote holds that the skill lacks is an
// update's to take in, see updateRun.accountStep.
func (inv *invocation) judgePublish(ctx context.Context, gitDir string, rec lineage.Record, remote remoteForks) (*publishing, error) {
	n := rec.Name
	p := &publishing{name: n}
	edits, err := inv.judgeEdits(ctx, gitDir, rec)
	var f *failure
	switch {
	case errors.As(err, &f):
		p.outcome, p.f = publishRefused, f
		return p, nil
	case err != nil:
		return nil, err
	}
	p.edits, p.judged = edits, true
	theirs := remote.tips[n]
	switch theirs {
	case "":
		p.push = true
		return p, nil
	case rec.Commit:
		if edits != nil {
			p.push = true
		} else {
			p.outcome, p.commit = publishUpToDate, theirs
		}
		return p, nil
	}
	if f := sameForkRefusal(rec, remote.walked[theirs], "published"); f != nil {
		p.outcome, p.f = publishRefused, f
		return p, nil
	}
	// Is the remote tip in the skill's history, or the skill's tip in the
	// remote's? Neither is a divergence.
	ahead, err := inv.isAncestor(ctx, gitDir, theirs, rec.Commit)
	behind := false
	if err == nil && !ahead {
		behind, err = inv.isAncestor(ctx, gitDir, rec.Commit, theirs)
	}
	switch {
	case err != nil:
		p.outcome, p.f = publishRefused, failureOf(accountRepoFailure(err))
	case ahead: // the account remote holds nothing the skill lacks
		p.push = true
	case behind && edits == nil: // the skill holds nothing the account remote lacks
		p.outcome, p.commit = publishBehind, theirs
	default: // the remote moved on, and the skill holds commits or edits of its own
		p.outcome, p.f = publishMoved, movedRefusal(n)
	}
	return p, nil
}

// movedRefusal refuses to publish the skill called name while the account
// remote holds commits of it that it lacks, published from another
// machine, and it holds commits or edits of its own the remote lacks: a
// publish never merges, and an update takes them in first.
func movedRefusal(name string) *failure {
	return refuse(exitRefused, "the account remote holds changes to "+sanitised(name)+" that this machine lacks, published from another machine",
		"run '"+skillCommand("update", name)+"' to take them in, then publish again")
}

// committing is one skill whose edits a publish records: where it is, what
// its skill directory held when it was judged, and the commit written of
// it, which the branch moves to once the mutation is applied.
type committing struct {
	site     forkSite
	captured string // the skill directory's fingerprint, taken before git read it
	root     string // the root tree of the commit: the tip's, with the skill's directory as git wrote it
	commit   string
}

// judgeEdits judges the edits of the skill whose branch is rec: its
// worktree has to be one git can work in, its skill directory is captured
// and compared with its tip, and a directory that holds its tip has
// nothing to record, nil, as has a skill with neither a worktree nor a
// library entry on this machine. A repository nested in it that the
// ignore rules do not cover is refused, since git would record it as a
// link rather than its files. The root tree of the commit is the one git
// wrote of the worktree, the tip's with the skill's directory as it is
// now, so an entry beside it that a commit made with git put there stays.
func (inv *invocation) judgeEdits(ctx context.Context, gitDir string, rec lineage.Record) (*committing, error) {
	if !inv.forkPlaced(rec.Name) {
		return nil, nil
	}
	f, err := inv.forkSiteOf(ctx, gitDir, rec)
	if err != nil {
		return nil, err
	}
	j, err := inv.judgeSite(ctx, f, false)
	switch {
	case err != nil:
		return nil, err
	case len(j.exposed) > 0:
		return nil, nestedRepoRefusal(f.name, j.exposed)
	case j.clean:
		return nil, nil
	}
	return &committing{site: f, captured: j.captured, root: j.written}, nil
}

// recordEdits records the edits of every skill of ready as one commit
// each, written by the fork commit writer w: the skill directory as git
// records it over a throwaway index loaded from the tip, so under every
// ignore rule a skill has, as the tree of a commit whose one parent is the
// tip, with the message -m gives or one naming the skill and the files the
// commit changes, see commitSubject. Every commit is written before the
// lock, and nothing points at it until one journaled mutation moves each
// branch to its commit, see applyCommits. A skill that changed meanwhile
// is handed to drop and left out, refused in the words of the command
// that records, words; the rest are returned. Recording never pushes.
func (inv *invocation) recordEdits(ctx context.Context, w *forkWriter, gitDir string, ready []*committing, message string, words recordWords, drop func(string, *failure)) ([]*committing, error) {
	if err := inv.writeCommits(ctx, w, gitDir, ready, message); err != nil {
		return nil, err
	}
	return inv.applyCommits(ctx, gitDir, ready, words, drop)
}

// recordWords is how a command that records edits refuses a skill that
// changed meanwhile: what the skill was being, as in "so it was not
// published", and the hint that runs it again for the skill called name.
type recordWords struct {
	what  string
	again func(name string) string
}

// publishWords are a publish's recordWords.
var publishWords = recordWords{what: "published", again: func(name string) string {
	return "run '" + publishCommand(name) + "' again to publish it as it is now"
}}

// writeCommits writes the commit of every skill of ready, before the lock:
// one diff-tree reads what each changes against its tip, which the
// generated subject summarises, and the writer writes each commit with the
// tip as its one parent.
func (inv *invocation) writeCommits(ctx context.Context, w *forkWriter, gitDir string, ready []*committing, message string) error {
	var pairs strings.Builder
	for _, c := range ready {
		pairs.WriteString(c.site.rec.Tree + " " + c.root + "\n")
	}
	out, err := inv.git.IsolatedInput(ctx, gitDir, strings.NewReader(pairs.String()), "diff-tree", "--stdin", "-r", "-z", "--no-renames", "--name-status")
	if err != nil {
		return accountRepoFailure(err)
	}
	changes, err := parsePairDiffs(out)
	if err != nil {
		return accountRepoFailure(err)
	}
	for _, c := range ready {
		prefix := c.site.dir + "/"
		files := []changedFile{}
		for _, f := range changes[c.site.rec.Tree+" "+c.root] {
			f.Path = strings.TrimPrefix(f.Path, prefix)
			files = append(files, f)
		}
		subject, body := commitSubject(c.site.name, w.label, files), ""
		if text := strings.TrimSpace(message); text != "" {
			subject, body, _ = strings.Cut(text, "\n")
		}
		c.commit, err = w.commit(ctx, c.root, []string{c.site.rec.Commit}, forkMessage{subject: subject, body: body})
		switch {
		case err != nil && strings.TrimSpace(message) != "" && errors.Is(err, lineage.ErrForkTrailer):
			// Only a message the user gave can carry a trailer of agentx's;
			// the command refuses one before anything is looked up, see
			// checkMessage, so this is a backstop.
			return trailerInMessage()
		case err != nil:
			return accountRepoFailure(err)
		}
	}
	return nil
}

// applyCommits moves, under the lock and in one journaled mutation, the
// branch of every skill of ready to its commit and resets its worktree's
// index to it, so that git status there is clean. Each skill is read again
// first: its branch has to hold the tip its commit was written on, its
// worktree has to be on the branch and its skill directory has to hold
// what it held when it was judged; a skill for which one of them changed
// is dropped and the rest recorded, rather than record something nobody
// judged. A branch moved after that read, by a commit made with git in the
// worktree, refuses the mutation before it changes anything, and the run
// records nothing.
func (inv *invocation) applyCommits(ctx context.Context, gitDir string, ready []*committing, words recordWords, drop func(string, *failure)) ([]*committing, error) {
	var applied []*committing
	err := home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		refs := make([]string, 0, len(ready))
		for _, c := range ready {
			refs = append(refs, c.site.rec.Ref)
		}
		values, err := inv.git.Refs(ctx).RefValues(gitDir, refs)
		if err != nil {
			return accountRepoFailure(err)
		}
		m := home.NewMutation(inv.dirs.Home)
		for _, c := range ready {
			f := c.site
			if err := worktreeHealth(f.name, f.root, f.branch); err != nil {
				var refusal *failure
				if errors.As(err, &refusal) {
					drop(f.name, refusal)
					continue
				}
				return err
			}
			if live, err := home.State(f.skillDir); err != nil || live != c.captured || values[f.rec.Ref] != f.rec.Commit {
				drop(f.name, refuse(exitRefused, sanitised(f.name)+" changed while its edits were being recorded, so it was not "+words.what, words.again(f.name)))
				continue
			}
			m.Ref(gitDir, f.rec.Ref, f.rec.Commit, c.commit)
			m.Worktree(gitDir, f.root, f.branch)
			applied = append(applied, c)
		}
		return m.Apply(inv.refs(ctx))
	})
	if errors.Is(err, home.ErrMovedBeforeApply) {
		moved := strings.TrimPrefix(home.MovedRef(err), lineage.ForkPrefix)
		return nil, fail(exitRefused, sanitised(moved)+" changed while its edits were being recorded, so nothing was "+words.what, "run the command again")
	}
	if err != nil {
		return nil, mutationFailure(err)
	}
	return applied, nil
}

// commitVerbs is what the generated subject says a commit does to a file.
var commitVerbs = map[string]string{diffAdded: "add", diffModified: "edit", diffDeleted: "delete"}

// commitSubject is the subject of the commit a publish, or an update or a
// fork that records a skill's edits first, records them as when -m gives
// none: the skill, the first two files the commit
// changes, by path, each with what it does to them, how many more it
// changes, and the label of the machine it was made on, as in "notes: edit
// SKILL.md, add examples.md and 3 more files (laptop)". Paths and the
// label are sanitised, so the subject stays one line.
func commitSubject(name, label string, files []changedFile) string {
	var said []string
	for _, f := range files[:min(2, len(files))] {
		said = append(said, commitVerbs[f.Status]+" "+sanitised(f.Path))
	}
	var what string
	switch len(files) {
	case 0:
		what = "edit"
	case 1:
		what = said[0]
	case 2:
		what = said[0] + " and " + said[1]
	default:
		what = said[0] + ", " + said[1] + " and " + plural(len(files)-2, "more file")
	}
	return name + ": " + what + " (" + sanitised(label) + ")"
}

// pushForks pushes branches in one push to the account remote, the git
// remote called remote at url, and reads what it answered for each, see
// gitx.Push, then what its branches hold now. A push git could not make at
// all is exit code 3, as a fetch is.
func (inv *invocation) pushForks(ctx context.Context, gitDir, remote, url string, list []*publishing, branches []string) error {
	statuses, err := inv.git.Push(ctx, gitDir, remote, branches)
	if err != nil {
		return unreachableRemote(url, err)
	}
	answered := map[string]int{}
	for i, s := range statuses {
		answered[strings.TrimPrefix(s.To, "refs/heads/")] = i
	}
	tips, err := lineage.ListRemote(ctx, inv.git, gitDir, remote)
	if err != nil {
		return accountRepoFailure(err)
	}
	for _, p := range list {
		if !p.push {
			continue
		}
		branch := strings.TrimPrefix(lineage.ForkRef(p.name), "refs/heads/")
		i, ok := answered[branch]
		switch {
		case !ok:
			p.outcome, p.f = publishRejected, refuse(exitSource, "git push said nothing of "+branch+", so whether the account remote took it is not known",
				"run '"+publishCommand(p.name)+"' again")
		case statuses[i].Rejected():
			p.outcome, p.f = publishRejected, refuse(exitRefused, "the account remote rejected "+branch+": "+sanitised(statuses[i].Why()),
				"run '"+skillCommand("update", p.name)+"' to take in what it holds, then publish again; agentx never forces a push")
		case statuses[i].Flag == '=':
			p.outcome, p.commit = publishUpToDate, tips[p.name]
		default:
			p.outcome, p.commit = publishPushed, tips[p.name]
		}
	}
	return nil
}

// reportPublished reports every skill of the publish, in name order: one
// progress event each, its publish event, naming url as its source, and
// its line, or for a skill behind the account remote a warning; then the
// library_skill event of each skill whose edits were recorded, as the
// machine now has it, read from one rescan; and the result, which answers
// for every skill that was not published, see refusals.
func (inv *invocation) reportPublished(ctx context.Context, list []*publishing, url string) error {
	out := inv.out
	run := refusals{verb: "published", mixed: "run 'agentx skill list' to see the state of each skill, then publish the rest one at a time"}
	pushed := 0
	var recorded []string
	for i, p := range list {
		out.emit(progressEvent{event: newEvent("progress"), Phase: "publish", Subject: p.name, Current: i + 1, Total: len(list)})
		out.emit(publishEvent{event: newEvent("publish"), Name: p.name, Outcome: p.outcome, Commit: p.commit, Source: url})
		name := out.paint(heading, sanitised(p.name))
		switch p.outcome {
		case publishPushed:
			pushed++
			out.done("published " + name + " as " + short(p.commit))
		case publishUpToDate:
			out.print(name, " is up to date on the account remote")
		case publishBehind:
			out.warnWith(behindMessage(p.name), "run '"+skillCommand("update", p.name)+"' to take them in")
		}
		if p.recorded {
			recorded = append(recorded, p.name)
		}
		if p.f != nil {
			run.add(p.name, p.f)
			if len(list) > 1 {
				out.warn(namedReason(p.name, p.f.message))
			}
		}
	}
	if err := inv.reportRecorded(ctx, recorded); err != nil {
		return err
	}
	switch {
	case len(list) == 1 && pushed == 1:
		inv.summary = "published " + sanitised(list[0].name)
	case len(list) == 1 && list[0].outcome == publishUpToDate:
		inv.summary = sanitised(list[0].name) + " is up to date on the account remote"
	case len(list) == 1 && list[0].outcome == publishBehind:
		inv.summary = behindMessage(list[0].name)
	default:
		inv.summary = "published " + strconv.Itoa(pushed) + " of " + plural(len(list), "skill")
	}
	if f := run.failure(len(list), len(list)-len(run.broken)); f != nil {
		return f
	}
	return nil
}

// reportRecorded emits the library_skill event of every skill of names,
// whose edits the publish recorded, as the machine now has it, after one
// rescan.
func (inv *invocation) reportRecorded(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	libs := librarySkills(inv.dirs.Library)
	for _, n := range names {
		if lib, ok := libs[n]; ok {
			inv.out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
		}
	}
	return nil
}

// behindMessage is the warning for the skill called name, which the
// account remote is ahead of: it has nothing to publish.
func behindMessage(name string) string {
	return sanitised(name) + " has nothing to publish; the account remote holds changes published from another machine"
}
