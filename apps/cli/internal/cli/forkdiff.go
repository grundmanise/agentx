package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// forkCommit is a commit of the account repo a fork's skill directory is
// compared with or put back to, as a user named it: the commit, and the
// tree of the fork's skill directory in it.
type forkCommit struct {
	id  string
	sub string // the tree of <id>:<dir>
}

// version is the skill directory c holds, as a version the fork's skill
// directory is compared with in its worktree: git loads the commit's whole
// tree, and a directory whose in-process tree id is the directory's tree
// holds it with no git at all.
func (c forkCommit) version() version {
	return version{load: c.id, holds: func(id string) bool { return id == c.sub }}
}

// resolveForkCommit finds the commit arg names, an id or anything else git
// reads as one, for the fork f, in one cat-file of the account repo: the
// commit itself and its tree at the fork's skill directory. A name that is
// no commit, or several, is exit code 6, and so is a commit that holds no
// skill directory under the fork's directory name, which the fork's
// content cannot be compared with.
func (inv *invocation) resolveForkCommit(ctx context.Context, f forkSite, arg string) (forkCommit, error) {
	unknown := fail(exitRefused, "the account repo holds no commit "+sanitised(arg),
		"name a commit by its id, as '"+skillCommand("history", f.name)+"' lists them")
	if strings.ContainsAny(arg, "\r\n\x00") {
		return forkCommit{}, unknown
	}
	query := arg + "^{commit}\n" + arg + "^{commit}:" + f.dir + "\n"
	out, err := inv.git.IsolatedInput(ctx, f.gitDir, strings.NewReader(query), "cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return forkCommit{}, accountRepoFailure(err)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 2 {
		return forkCommit{}, accountRepoFailure(fmt.Errorf("git cat-file answered %q for two names", out))
	}
	commit, commitType, _ := strings.Cut(lines[0], " ")
	switch {
	case strings.HasSuffix(lines[0], " ambiguous"):
		return forkCommit{}, fail(exitRefused, sanitised(arg)+" names more than one object of the account repo",
			"give more of the commit id")
	case commitType != "commit":
		return forkCommit{}, unknown
	}
	c := forkCommit{id: commit}
	sub, subType, _ := strings.Cut(lines[1], " ")
	if subType != "tree" {
		return forkCommit{}, fail(exitRefused, "commit "+short(c.id)+" holds no skill directory "+quotedPath(f.dir)+" to compare "+sanitised(f.name)+" with",
			"name a commit of "+sanitised(f.name)+"'s history, as '"+skillCommand("history", f.name)+"' lists them")
	}
	c.sub = sub
	return c, nil
}

// forkDiff shows the unpublished edits of the fork whose branch is rec:
// how its skill directory differs from its last published version, see
// publishedVersion, so edits an update, a fork or a failed push recorded
// stay in the diff until they are published, or, when commit names one,
// from that commit, any commit of the account repo whose tree holds the
// fork's skill directory. Only the skill directory is compared, and every
// path is relative to it.
//
// The directory is compared as state compares it with the tip: its tree
// id in process first, and a directory holding the version costs no git;
// otherwise git runs in the skill directory with the worktree as its work
// tree, over a throwaway index loaded from the commit's whole tree, so
// every ignore rule of the fork applies, a .gitignore at the worktree's
// root included, and a file git ignores is never in the diff. Nothing is
// written to the worktree, its index included, so the diff needs nothing
// of the worktree but its files: it reads a fork whose worktree is in the
// middle of a git merge as well.
//
// What git cannot record, a repository nested in the directory or a named
// pipe, is left out with a warning, unless an ignore rule covers it, and
// counted in the line, as for a managed skill.
func (inv *invocation) forkDiff(ctx context.Context, gitDir string, rec lineage.Record, commit string) error {
	f := inv.forkPlace(gitDir, rec)
	if err := worktreeMissing(f.name, f.root); err != nil {
		return err
	}
	if err := inv.findForkDir(ctx, &f); err != nil {
		return err
	}
	v, at, against := f.version(), commit, ""
	if commit == "" {
		var err error
		if at, against, err = inv.publishedVersion(ctx, gitDir, rec); err != nil {
			return err
		}
	}
	if commit != "" || at != rec.Commit {
		c, err := inv.resolveForkCommit(ctx, f, at)
		if err != nil {
			return err
		}
		v = c.version()
		if commit != "" {
			against = "commit " + short(c.id)
		}
	}
	if !lexists(f.skillDir) {
		return skillDirMissing(f)
	}
	t, err := treeid.Read(f.skillDir)
	if err != nil {
		return libraryFailure(f.root, err)
	}
	subject := diffSubject{plain: f.name, painted: inv.out.paint(heading, sanitised(f.name))}
	if fastHolds(t, inv.systemFilesIgnored(), v) {
		inv.reportDiff(subject, f.name, against, nil, 0)
		return nil
	}
	files, left, err := inv.diffForkDir(ctx, f, t, v)
	if err != nil {
		return accountRepoFailure(err)
	}
	for _, p := range left {
		inv.out.warn(quotedPath(filepath.Join(f.skillDir, filepath.FromSlash(p))) + " cannot be recorded by git and is left out of the diff")
	}
	inv.reportDiff(subject, f.name, against, files, len(left))
	return nil
}

// publishedVersion is the commit skill diff compares the fork whose branch
// is rec with, and how its line names it: the newest commit of its history
// the account remote's branch holds, as the last fetch left it, which is
// its last published version, read with one merge-base; for a renamed
// skill not yet published under its new name, the old name's branch, see
// publishedName; for a skill the account remote no longer holds, the
// version a fetch last found there, see lineage.RemoteRemovedPrefix. A fork
// the account remote holds no branch of, or none that shares its history, never
// published or with no account remote set, is compared with its creation
// commit, the one skill new or skill fork wrote, and one whose history
// carries no fork id with its tip. Either way it is what its state is
// judged against, see unpublished.
func (inv *invocation) publishedVersion(ctx context.Context, gitDir string, rec lineage.Record) (string, string, error) {
	readFork := func() error {
		if rec.Fork != nil {
			return nil
		}
		recs := map[string]lineage.Record{rec.Name: rec}
		if err := lineage.ReadForks(ctx, inv.git, gitDir, recs, inv.forkWalks); err != nil {
			return accountRepoFailure(err)
		}
		rec = recs[rec.Name]
		return nil
	}
	_, remote, ok, err := inv.accountSource()
	if err != nil {
		return "", "", err
	}
	if ok {
		there, err := inv.remoteForkTip(ctx, gitDir, remote, rec.Name)
		if err != nil {
			return "", "", err
		}
		// A renamed skill's history is read only when its own name has no
		// branch there, which costs a published skill nothing.
		if there == "" {
			if err := readFork(); err != nil {
				return "", "", err
			}
			tips := map[string]string{}
			for _, old := range rec.Fork.Renamed {
				if tips[old], err = inv.remoteForkTip(ctx, gitDir, remote, old); err != nil {
					return "", "", err
				}
			}
			there = tips[publishedName(rec.Name, rec.Fork.Renamed, tips)]
		}
		// One the account remote no longer holds, removed or renamed by
		// another machine, is compared with the version a fetch last found
		// there, see lineage.RemoteRemovedPrefix.
		if there == "" {
			there = rec.RemoteRemoved
		}
		if there != "" {
			out, status, err := inv.git.IsolatedStatus(ctx, gitDir, 1, "merge-base", rec.Commit, there)
			if err != nil {
				return "", "", accountRepoFailure(err)
			}
			if base := strings.TrimSpace(out); status == 0 && base != "" {
				return base, "its last published version " + short(base), nil
			}
		}
	}
	if err := readFork(); err != nil {
		return "", "", err
	}
	if created := rec.Fork.Created; created != "" {
		return created, "its creation commit " + short(created), nil
	}
	return rec.Commit, "its last commit " + short(rec.Commit), nil
}

// remoteForkTip is the tip of the account remote's branch of the skill
// called name, the git remote called remote, as last fetched; "" when it
// holds none.
func (inv *invocation) remoteForkTip(ctx context.Context, gitDir, remote, name string) (string, error) {
	ref := lineage.RemoteForkRef(remote, name)
	values, err := inv.git.Refs(ctx).RefValues(gitDir, []string{ref})
	if err != nil {
		return "", accountRepoFailure(err)
	}
	return values[ref], nil
}

// diffForkDir is the diff of the fork f's skill directory, read as t,
// against v, and the paths of the directory git cannot record that no
// ignore rule covers, which the diff leaves out.
func (inv *invocation) diffForkDir(ctx context.Context, f forkSite, t treeid.Tree, v version) ([]fileDiff, []string, error) {
	wt, err := inv.openWorkTreeWithin(ctx, f.gitDir, f.root, f.skillDir)
	if err != nil {
		return nil, nil, err
	}
	defer wt.Close()
	if err := wt.Load(ctx, v.load); err != nil {
		return nil, nil, err
	}
	var left []string
	if len(t.Unrecordable) > 0 {
		covered, err := wt.CheckIgnore(ctx, t.Unrecordable)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range t.Unrecordable {
			if !underIgnored(p, covered) {
				left = append(left, p)
			}
		}
	}
	if err := wt.AddAll(ctx); err != nil {
		return nil, nil, err
	}
	status, patch, err := wt.DiffCachedHere(ctx, v.load)
	if err != nil {
		return nil, nil, err
	}
	files, err := parseDiff(status, patch)
	return files, left, err
}
