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
// how its skill directory differs from the tip, or, when commit names one,
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
	v, against := f.version(), "its last commit "+short(rec.Commit)
	if commit != "" {
		c, err := inv.resolveForkCommit(ctx, f, commit)
		if err != nil {
			return err
		}
		v, against = c.version(), "commit "+short(c.id)
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
