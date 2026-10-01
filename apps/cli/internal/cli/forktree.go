package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/source"
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// A fork lives in agentx home: its branch is checked out as a linked
// worktree at worktrees/<name>, whose one entry is the skill's directory,
// so the worktree's .git file sits beside the skill and never in it, and
// no agent client sees a .git entry inside a skill. The directory is named
// after the upstream's own directory for a fork of a third-party skill and
// after the skill for a greenfield one, and never changes. The library
// entry is a symlink to that directory.

// worktreesDir is where the worktrees of the forks placed on this machine
// are.
func (inv *invocation) worktreesDir() string { return filepath.Join(inv.dirs.Home, "worktrees") }

// worktreeRoot is the worktree of the fork called name.
func (inv *invocation) worktreeRoot(name string) string {
	return filepath.Join(inv.worktreesDir(), name)
}

// forkJudged is how a fork's skill directory compares with a version of
// it, the branch tip as a rule.
type forkJudged struct {
	clean   bool     // the directory holds the version, as git records it
	written string   // the tree git wrote of the directory; "" when no git ran or a repository stopped it
	ignored []string // the files git ignores in it, when asked for
	exposed []string // the nested repositories git would record as links rather than as their files
}

// judgeFork compares a fork's skill directory with v as judgeDir compares a
// managed skill's: the in-process tree id first, then git, with the
// directory as a work tree of the account repo over a throwaway index. So
// the tree it writes follows Git's ignore rules: the skill's own .gitignore
// files, the user's global ignore file, whether at git's default place or
// at the core.excludesFile they set, and the list ignore_system_files keeps
// in the account repo's info/exclude while the setting is on. Attributes
// come from the skill's own .gitattributes files alone. That tree is what a
// commit of the fork records, so a file git ignores stays on disk and is
// never committed.
//
// A Git repository nested in the directory is the one thing that differs:
// git would record it as a link to a commit of its own rather than as its
// files, so a repository that is not ignored is reported in exposed, and
// nothing is written. One the ignore rules cover is no part of the fork and
// is left where it is. v.load is "" for a directory compared with nothing,
// as a new branch's first content is.
func (inv *invocation) judgeFork(ctx context.Context, gitDir, dir string, v version, wantIgnored bool) (forkJudged, error) {
	t, err := treeid.Read(dir)
	if err != nil {
		return forkJudged{}, err
	}
	if len(t.Unrecordable) == 0 && !wantIgnored && fastHolds(t, inv.systemFilesIgnored(), v) {
		return forkJudged{clean: true}, nil
	}
	wt, err := inv.openWorkTree(ctx, gitDir, dir)
	if err != nil {
		return forkJudged{}, err
	}
	defer wt.Close()
	if v.load != "" {
		if err := wt.Load(ctx, v.load); err != nil {
			return forkJudged{}, err
		}
	}
	var j forkJudged
	if wantIgnored {
		if j.ignored, err = wt.Ignored(ctx); err != nil {
			return forkJudged{}, err
		}
	}
	if repos := exposedRepos(t.Unrecordable, nil); len(repos) > 0 {
		covered, err := wt.CheckIgnore(ctx, repos)
		if err != nil {
			return forkJudged{}, err
		}
		if j.exposed = exposedRepos(repos, covered); len(j.exposed) > 0 {
			return j, nil
		}
	}
	if err := wt.AddAll(ctx); err != nil {
		return forkJudged{}, err
	}
	if j.written, err = wt.WriteTree(ctx); err != nil {
		return forkJudged{}, err
	}
	j.clean = v.holds(j.written)
	return j, nil
}

// exposedRepos are the paths of unrecordable, the entries of a directory
// git cannot record, that are a nested repository's .git and lie under no
// path of ignored, the ones git's ignore rules cover, a directory written
// with or without its trailing slash.
func exposedRepos(unrecordable, ignored []string) []string {
	var exposed []string
	for _, p := range unrecordable {
		if !hasGitComponent(p) || underIgnored(p, ignored) {
			continue
		}
		exposed = append(exposed, p)
	}
	return exposed
}

func hasGitComponent(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if strings.EqualFold(part, ".git") {
			return true
		}
	}
	return false
}

func underIgnored(p string, ignored []string) bool {
	for _, i := range ignored {
		dir := strings.TrimSuffix(i, "/")
		if p == dir || strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// nestedRepoRefusal refuses to commit a fork whose directory holds a Git
// repository the ignore rules do not cover, naming the first one.
func nestedRepoRefusal(name string, exposed []string) error {
	path := exposed[0]
	return fail(exitRefused, sanitised(name)+" holds a Git repository at "+quotedPath(path)+", which git would record as a link rather than its files",
		"add "+quotedPath(strings.TrimSuffix(path, "/.git"))+" to the skill's .gitignore to keep it local, or remove it")
}

// stageForkContent lays the skill directory dir that commit holds out at
// dest, byte for byte as the account repo stores it, exactly as a managed
// skill's base version is laid out: every file with the mode git records,
// every symlink as a symlink, and nothing else. What is laid out is held to
// the tree the commit holds, the files git ignores in from are carried in,
// and the fingerprint a publish of dest expects is returned.
func (inv *invocation) stageForkContent(ctx context.Context, gitDir, commit, dir, dest, from string, ignored []string) (string, error) {
	base, err := lineage.ReadMerged(ctx, inv.git, gitDir, commit, dir)
	if err != nil {
		return "", err
	}
	bodies, err := source.ReadBlobs(ctx, inv.git, gitDir, baseBlobs(base))
	if err != nil {
		return "", err
	}
	target := base.ID()
	laid := version{load: commit + ":" + dir, holds: func(id string) bool { return id == target }}
	return stageVersion(dest, func(dest string) error { return materialise(dest, base, bodies) }, laid, from, ignored)
}

// libraryLink is what the library symlink of a fork records: the path of
// target, the fork's skill directory in its worktree, relative to libDir,
// the library directory, when the two share an ancestor other than the
// root, as ~/.agents/skills and ~/.agentx do, so that the link survives the
// home directory moving; and target itself when they share nothing but the
// root. Both must be canonical paths, symlinks resolved, since the kernel
// reads a relative link from the directory it really sits in.
func libraryLink(libDir, target string) string {
	first := func(p string) string {
		head, _, _ := strings.Cut(strings.TrimPrefix(filepath.Clean(p), "/"), "/")
		return head
	}
	if !filepath.IsAbs(libDir) || !filepath.IsAbs(target) || first(libDir) == "" || first(libDir) != first(target) {
		return target
	}
	rel, err := filepath.Rel(libDir, target)
	if err != nil {
		return target
	}
	return rel
}
