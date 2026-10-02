package cli

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

func newSkillCommitCommand(inv *invocation) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "commit [<name>]",
		Short: "Commit the edits of a fork, or of every fork that has some",
		Long: "Commit the edits of the fork called <name> to its branch in the account repo, or,\n" +
			"without a name, those of every fork with edits, one commit each. A fork's edits\n" +
			"stay uncommitted until you commit them, with this command or with git in the\n" +
			"fork's worktree; agentx never commits on its own. Files git ignores are never\n" +
			"committed. The message names the skill and the files it changes, unless -m gives\n" +
			"one. Committing never pushes.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("message") && strings.TrimSpace(message) == "" {
				return fail(exitUsage, "-m needs a message", "say what the edits do, or leave -m out for the message agentx writes")
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return inv.skillCommit(cmd.Context(), name, message)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "the commit message, in place of the one agentx writes; its first line is the subject")
	return cmd
}

// commitEvent is one commit skill commit wrote on a fork's branch: the
// commit, its subject and every file it changes against the tip it was
// written on, each path relative to the skill's directory.
type commitEvent struct {
	event
	Name    string        `json:"name"`
	Commit  string        `json:"commit"`
	Subject string        `json:"subject"`
	Files   []changedFile `json:"files"`
}

// committing is one fork a run of skill commit commits: where it is, what
// its skill directory held when it was judged, and the commit written of
// it, which the branch moves to once the mutation is applied.
type committing struct {
	site     forkSite
	captured string // the skill directory's fingerprint, taken before git read it
	root     string // the root tree of the commit: the tip's, with the skill's directory as git wrote it
	commit   string
	subject  string
	files    []changedFile
}

// skillCommit commits the edits of the fork called name, or with an empty
// name of every fork that has some, one commit each, written by the fork
// commit writer: the skill directory as git records it over a throwaway
// index loaded from the tip, so under every ignore rule a fork has, as the
// tree of a commit whose one parent is the tip, with the message -m gives
// or one naming the skill and the files the commit changes. Every commit
// is written before the lock, and nothing points at it until one journaled
// mutation moves each branch to its commit, with the tip it was written on
// as the value the branch must still hold, and resets the index of each
// worktree to its new tip, so that git status there is clean.
//
// What each skill directory held is captured before git reads it, and read
// again under the lock with the branch: an edit made, or a commit made
// with git in the worktree, meanwhile refuses that fork rather than
// committing something nobody judged or writing over that commit. A commit
// made with git between that read and the branch's move is found by the
// move itself, which then changes nothing at all.
//
// It never amends, never pushes and never runs on its own: a commit made
// with git in a fork's worktree is an ordinary commit of the branch, which
// this command finds nothing left to commit after.
func (inv *invocation) skillCommit(ctx context.Context, name, message string) error {
	if err := inv.finishJournals(ctx); err != nil {
		return err
	}
	gitDir, exists, err := gitx.CheckAccountRepo(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return accountRepoFailure(err)
	}
	records := map[string]lineage.Record{}
	if exists {
		if records, err = lineage.List(ctx, inv.git, gitDir); err != nil {
			return accountRepoFailure(err)
		}
	}
	names, err := inv.commitSelection(name, records)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return inv.reportNothingToCommit(name)
	}
	r := &refusals{verb: "committed", noun: "fork", mixed: "run 'agentx skill list' to see which forks are modified, then commit the rest one at a time"}
	drop := func(n string, f *failure) {
		r.add(n, f)
		if name == "" && len(names) > 1 {
			inv.out.warn(n + ": " + f.message)
		}
	}
	// The writer reads the user's identity and their core.excludesFile in
	// one read of their configuration, before git reads any skill
	// directory, so the ignore rules every judgement uses come with it.
	w, err := inv.newForkWriter(ctx, gitDir)
	if err != nil {
		return err
	}
	var ready []*committing
	for _, n := range names {
		c, err := inv.judgeCommit(ctx, gitDir, records[n])
		var f *failure
		switch {
		case errors.As(err, &f):
			drop(n, f)
		case err != nil:
			return err
		case c != nil:
			ready = append(ready, c)
		}
	}
	// The forks the run sets out to commit are those with edits, and those
	// it could not judge, which may have some.
	selected := len(ready) + len(r.broken)
	if len(ready) == 0 {
		if f := r.failure(selected, 0); f != nil {
			return f
		}
		return inv.reportNothingToCommit(name)
	}
	if err := inv.writeCommits(ctx, w, gitDir, ready, message); err != nil {
		return err
	}
	applied, err := inv.applyCommits(ctx, gitDir, ready, drop)
	if err != nil {
		return err
	}
	if len(applied) > 0 {
		if err := inv.reportCommitted(ctx, applied); err != nil {
			return err
		}
	}
	if f := r.failure(selected, len(applied)); f != nil {
		return f
	}
	return nil
}

// commitSelection is the forks a run of skill commit sets out to judge, by
// name: the one it was given, which has to be a fork, or every fork the
// account repo holds that is placed on this machine. A fork with neither a
// worktree nor a library entry here holds no edit to commit, and is left
// out rather than refused.
func (inv *invocation) commitSelection(name string, records map[string]lineage.Record) ([]string, error) {
	if name == "" {
		var names []string
		for n, rec := range records {
			if rec.Kind == lineage.KindFork && (lexists(inv.worktreeRoot(n)) || lexists(inv.libraryPath(n))) {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		return names, nil
	}
	rec, ok := records[name]
	switch {
	case ok && rec.Kind == lineage.KindFork:
		return []string{name}, nil
	case ok:
		return nil, fail(exitRefused, sanitised(name)+" is managed, not a fork, so it has no history to commit to",
			"fork it first with '"+skillCommand("fork", name)+"'")
	}
	if _, held := librarySkill(inv.dirs.Library, name); held {
		return nil, fail(exitRefused, sanitised(name)+" is not a fork, so it has no history to commit to",
			"fork it first with '"+skillCommand("fork", name)+"'")
	}
	return nil, inv.noLibrarySkill(name)
}

// judgeCommit judges one fork for a commit: its worktree has to be one git
// can work in, its skill directory is captured and compared with its tip,
// and a directory that holds its tip has nothing to commit, nil. A
// repository nested in it that the ignore rules do not cover is refused,
// since git would record it as a link rather than its files. The root tree
// of the commit is the one git wrote of the worktree, the tip's with the
// skill's directory as it is now, so an entry beside it that a commit made
// with git put there stays.
func (inv *invocation) judgeCommit(ctx context.Context, gitDir string, rec lineage.Record) (*committing, error) {
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

// writeCommits writes the commit of every fork of ready, before the lock:
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
		c.files = []changedFile{}
		for _, f := range changes[c.site.rec.Tree+" "+c.root] {
			f.Path = strings.TrimPrefix(f.Path, prefix)
			c.files = append(c.files, f)
		}
		subject, body := commitSubject(c.site.name, w.label, c.files), ""
		if text := strings.TrimSpace(message); text != "" {
			subject, body, _ = strings.Cut(text, "\n")
		}
		c.subject = strings.TrimSpace(subject)
		if c.commit, err = w.commit(ctx, c.root, []string{c.site.rec.Commit}, forkMessage{subject: subject, body: body}); err != nil {
			return accountRepoFailure(err)
		}
	}
	return nil
}

// applyCommits moves, under the lock and in one journaled mutation, the
// branch of every fork of ready to its commit and resets its worktree's
// index to it. Each fork is read again first: its branch has to hold the
// tip its commit was written on, its worktree has to be on the branch and
// its skill directory has to hold what it held when it was judged; a fork
// for which one of them changed is dropped and the rest committed. A
// branch moved after that read, by a commit made with git in the worktree,
// refuses the mutation before it changes anything, and the run commits
// nothing.
func (inv *invocation) applyCommits(ctx context.Context, gitDir string, ready []*committing, drop func(string, *failure)) ([]*committing, error) {
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
				drop(f.name, refuse(exitRefused, sanitised(f.name)+" changed while it was being committed, so it was not committed",
					"run '"+skillCommand("commit", f.name)+"' again to commit it as it is now"))
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
		return nil, fail(exitRefused, sanitised(moved)+" changed while it was being committed, so nothing was committed", "run the command again")
	}
	if err != nil {
		return nil, mutationFailure(err)
	}
	return applied, nil
}

// reportCommitted reports every fork the run committed, by name: its
// commit event, then its library_skill as the machine now has it, read
// from one rescan.
func (inv *invocation) reportCommitted(ctx context.Context, applied []*committing) error {
	sort.Slice(applied, func(i, j int) bool { return applied[i].site.name < applied[j].site.name })
	snap, err := inv.scan(ctx, lockWait, "", false)
	if err != nil {
		return err
	}
	sc, err := inv.skillContext(ctx)
	if err != nil {
		return err
	}
	libs := librarySkills(inv.dirs.Library)
	out := inv.out
	for _, c := range applied {
		out.emit(commitEvent{event: newEvent("commit"), Name: c.site.name, Commit: c.commit, Subject: c.subject, Files: c.files})
		if lib, ok := libs[c.site.name]; ok {
			out.emit(sc.librarySkillEventFor(ctx, inv, snap, lib, nil))
		}
		out.done("committed " + out.paint(heading, sanitised(c.site.name)) + " as " + short(c.commit) + ": " + sanitised(c.subject))
	}
	inv.summary = "committed " + plural(len(applied), "fork")
	return nil
}

// reportNothingToCommit says that the run found no edit to commit: in the
// fork it was given, or in any fork.
func (inv *invocation) reportNothingToCommit(name string) error {
	inv.summary = "nothing to commit"
	if name != "" {
		inv.summary = name + " has nothing to commit"
	}
	inv.out.done(sanitised(inv.summary))
	return nil
}

// commitVerbs is what the generated subject says a commit does to a file.
var commitVerbs = map[string]string{diffAdded: "add", diffModified: "edit", diffDeleted: "delete"}

// commitSubject is the subject of a commit skill commit writes when -m
// gives none: the skill, the first two files the commit changes, by path,
// each with what it does to them, how many more it changes, and the label
// of the machine it was made on, as in "notes: edit SKILL.md, add
// examples.md and 3 more files (laptop)". Paths and the label are
// sanitised, so the subject stays one line.
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
