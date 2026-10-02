package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
	"github.com/grundmanise/agentx/apps/cli/internal/scan"
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

// forkSite is one fork as a command that works on its worktree finds it:
// its branch as the command read it, and where its worktree, its skill
// directory and its library entry are.
type forkSite struct {
	name     string
	gitDir   string
	rec      lineage.Record // the fork's branch, its tip and that tip's tree
	branch   string         // the branch's short name, skills/<name>, as the worktree's HEAD names it
	dir      string         // the skill's directory in the branch's tree, which never changes
	root     string         // the worktree, worktreeRoot(name)
	skillDir string         // root/dir, the directory the library entry leads to
	libPath  string
}

// forkVersion is the tip of a fork's branch as a version its skill
// directory dir is compared with. git loads the tip's whole tree, since a
// fork's directory is judged in its worktree, see judgeTip. holds is the
// fast path: the directory's in-process tree id, wrapped under dir as the
// branch's tree wraps it, against the tip's tree, which the for-each-ref
// that read the branch read with it, so a fork whose directory holds its
// tip costs no git process. A tip whose tree holds an entry beside the
// skill directory, as a commit made with git may put there, never passes
// it, and git decides.
func forkVersion(rec lineage.Record, dir string) version {
	return version{load: rec.Commit, holds: func(id string) bool { return treeid.Wrap(dir, id) == rec.Tree }}
}

// version is the fork's tip, see forkVersion.
func (f forkSite) version() version { return forkVersion(f.rec, f.dir) }

// forkSiteOf finds the fork whose branch is rec checked out on this
// machine, refusing one whose worktree git cannot run in, see
// worktreeHealth, before anything runs git inside it.
func (inv *invocation) forkSiteOf(ctx context.Context, gitDir string, rec lineage.Record) (forkSite, error) {
	f := inv.forkPlace(gitDir, rec)
	if err := worktreeHealth(f.name, f.root, f.branch); err != nil {
		return f, err
	}
	return f, inv.findForkDir(ctx, &f)
}

// forkPlace is where the fork whose branch is rec is checked out on this
// machine, its skill directory still to be found, see findForkDir.
func (inv *invocation) forkPlace(gitDir string, rec lineage.Record) forkSite {
	return forkSite{
		name: rec.Name, gitDir: gitDir, rec: rec, branch: strings.TrimPrefix(rec.Ref, "refs/heads/"),
		root: inv.worktreeRoot(rec.Name), libPath: inv.libraryPath(rec.Name),
	}
}

// findForkDir fills in the skill directory of f: where its library entry
// leads, which costs no git, or, when the entry leads elsewhere, the one
// directory the tip's tree holds.
func (inv *invocation) findForkDir(ctx context.Context, f *forkSite) error {
	dir, err := inv.forkDir(ctx, f.gitDir, f.rec)
	if err != nil {
		return err
	}
	f.dir, f.skillDir = dir, filepath.Join(f.root, dir)
	return nil
}

// forkDir is the skill's directory in the tree of the fork whose branch is
// rec: the directory its library entry leads to in its worktree, which
// costs no git, or the one directory the tip's tree holds, read with one
// ls-tree. A tip holding no directory or several, beside which nothing
// tells the skill's, is the account repo's to sort out, exit code 8.
func (inv *invocation) forkDir(ctx context.Context, gitDir string, rec lineage.Record) (string, error) {
	if real, err := filepath.EvalSymlinks(inv.libraryPath(rec.Name)); err == nil {
		if dir, ok := inv.placedForkDir(rec.Name, real); ok {
			return dir, nil
		}
	}
	out, err := inv.git.Isolated(ctx, gitDir, "ls-tree", "-z", "-d", "--name-only", rec.Commit)
	if err != nil {
		return "", accountRepoFailure(err)
	}
	var dirs []string
	for _, d := range strings.Split(out, "\x00") {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) != 1 {
		return "", fail(exitAccountRepo, fmt.Sprintf("the branch %s holds %d directories at its root, not the one skill directory a fork's branch holds", rec.Ref, len(dirs)),
			"run 'agentx doctor' and check the account repo it names")
	}
	return dirs[0], nil
}

// placedForkDir is the skill directory of the fork called name when real,
// where its library entry leads, is a directory of the fork's worktree,
// as the library symlink of a placed fork leads, and false otherwise. It
// reads the filesystem alone.
func (inv *invocation) placedForkDir(name, real string) (string, bool) {
	root, err := filepath.EvalSymlinks(inv.worktreeRoot(name))
	if err != nil || filepath.Dir(real) != root {
		return "", false
	}
	return filepath.Base(real), true
}

// worktreeHealth refuses a fork's worktree that git cannot work in, from
// the files alone, before a command journals anything that runs git there:
// one that is gone, one whose pointers no longer meet, as moving agentx
// home leaves them, one where a git command the user ran stopped part way,
// such as a merge with conflicts, whose state resetting the index would
// lose, one whose index a running git holds, and one checked out on
// another branch than the fork's. Each is exit code 6, since nothing is
// wrong with the account repo.
func worktreeHealth(name, root, branch string) error {
	if err := worktreeMissing(name, root); err != nil {
		return err
	}
	where := sanitised(name) + "'s worktree " + quotedPath(root)
	if home.PointersMoved(root) {
		return fail(exitRefused, where+" needs repair", "run '"+skillCommand("place", name)+"' to repair it")
	}
	if command := home.Unfinished(root); command != "" {
		return fail(exitRefused, where+" is in the middle of a git "+command,
			"finish it with git, or run 'git -C "+shellWord(root)+" "+command+" --abort' to give it up, then run the command again")
	}
	if lock := home.IndexLock(root); lock != "" {
		return fail(exitRefused, "git is running in "+where,
			"run the command again once it finishes; if no git is running, remove "+quotedPath(lock))
	}
	if !home.WorktreeAt(root, branch) {
		return fail(exitRefused, where+" is not on its branch "+branch,
			"run 'git -C "+shellWord(root)+" switch "+branch+"' to put it back on its branch")
	}
	return nil
}

// worktreeMissing refuses a fork's worktree that is not there at all, the
// one thing a command that only reads the fork's skill directory cannot
// do without.
func worktreeMissing(name, root string) error {
	switch _, err := os.Lstat(root); {
	case errors.Is(err, fs.ErrNotExist):
		return fail(exitRefused, sanitised(name)+"'s worktree "+quotedPath(root)+" is missing",
			"run '"+skillCommand("place", name)+"' to check it out again")
	case err != nil:
		return fail(exitRefused, err.Error(), "run 'agentx doctor' and check agentx home")
	}
	return nil
}

// holdsTip is drift's verdict on a placed fork: whether its skill
// directory, the one its library entry leads to, holds its branch tip, as
// git status in the worktree would say of it with no file listed. It never
// fails: a directory that cannot be read, or that git cannot compare, is
// modified, and so is one holding a nested repository git would record as
// a link. Under serve, git's verdict is kept until the directory's tree
// id, the tip or the setting changes, see verdict.
func (inv *invocation) holdsTip(ctx context.Context, lib scan.LibrarySkill, rec lineage.Record, dir string) bool {
	tree, err := treeid.Read(lib.ResolvedPath)
	if err != nil {
		return false
	}
	f := forkSite{name: lib.Name, gitDir: gitx.AccountRepoPath(inv.dirs.Home), rec: rec, dir: dir,
		root: filepath.Dir(lib.ResolvedPath), skillDir: lib.ResolvedPath}
	return inv.verdict(lib.Name, "its branch tip", tree, f.version(), func() (bool, error) {
		j, err := inv.judgeTip(ctx, f, tree, false)
		return j.clean, err
	})
}

// forkJudged is how a fork's skill directory compares with a version of
// it, the branch tip as a rule.
type forkJudged struct {
	clean bool // the directory holds the version, as git records it
	// written is the tree git wrote: of the directory, or, for a fork
	// judged against its tip, of its worktree as a whole, which is the
	// root tree a commit of the fork records. It is "" when no git ran or
	// a repository stopped it.
	written string
	ignored []string // the files git ignores in the directory, when asked for
	exposed []string // the nested repositories git would record as links rather than as their files
	// unrecordable is every path of the directory git cannot record, the
	// .git of a nested repository or a named pipe, ignored or not.
	unrecordable []string
}

// judgeFork compares a directory a fork's branch is to hold with v as
// judgeDir compares a managed skill's: the in-process tree id first, then
// git, with the directory as a work tree of the account repo over a
// throwaway index. So the tree it writes follows Git's ignore rules: the
// skill's own .gitignore files, the user's global ignore file, whether at
// git's default place or at the core.excludesFile they set, and the list
// ignore_system_files keeps in the account repo's info/exclude while the
// setting is on. Attributes come from the skill's own .gitattributes files
// alone. That tree is what a commit of the fork records, so a file git
// ignores stays on disk and is never committed.
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
	return inv.judgeForkTree(ctx, gitDir, dir, dir, t, v, v.holds, wantIgnored)
}

// judgeTip compares the skill directory of the fork f, read as t, with its
// tip as git status in the worktree compares it, under the rules judgeFork
// follows: git runs in the skill directory with the worktree as its work
// tree, over a throwaway index loaded from the tip's whole tree. So a
// .gitignore at the worktree's root counts as it counts for git status, an
// entry beside the skill directory, which a commit made with git may have
// put there, stays as the tip holds it, and the tree git writes is the
// root tree a commit of the fork records, which holds the tip when it is
// the tip's tree.
func (inv *invocation) judgeTip(ctx context.Context, f forkSite, t treeid.Tree, wantIgnored bool) (forkJudged, error) {
	return inv.judgeForkTree(ctx, f.gitDir, f.root, f.skillDir, t, f.version(), func(written string) bool { return written == f.rec.Tree }, wantIgnored)
}

// judgeForkTree judges dir, read as t, inside top, the work tree git runs
// over: v's fast path on t first, then git, whose tree holds the version
// when holds says so.
func (inv *invocation) judgeForkTree(ctx context.Context, gitDir, top, dir string, t treeid.Tree, v version, holds func(string) bool, wantIgnored bool) (forkJudged, error) {
	if len(t.Unrecordable) == 0 && !wantIgnored && fastHolds(t, inv.systemFilesIgnored(), v) {
		return forkJudged{clean: true}, nil
	}
	wt, err := inv.openWorkTreeWithin(ctx, gitDir, top, dir)
	if err != nil {
		return forkJudged{}, err
	}
	defer wt.Close()
	if v.load != "" {
		if err := wt.Load(ctx, v.load); err != nil {
			return forkJudged{}, err
		}
	}
	j := forkJudged{unrecordable: t.Unrecordable}
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
	j.clean = holds(j.written)
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

// hasGitComponent reports whether a component of the slash-separated path
// p is .git in any case, as a case-insensitive file system would match it.
func hasGitComponent(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if strings.EqualFold(part, ".git") {
			return true
		}
	}
	return false
}

// underIgnored reports whether p is one of the paths of ignored, or lies
// inside one, whether that path is written with its trailing slash or not.
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
// dest, see forkLayout. What is laid out is held to the tree the commit
// holds, the files git ignores in from are carried in, and the fingerprint
// a publish of dest expects is returned.
func (inv *invocation) stageForkContent(ctx context.Context, gitDir, commit, dir, dest, from string, ignored []string) (string, error) {
	laid, lay, err := inv.forkLayout(ctx, gitDir, commit, dir)
	if err != nil {
		return "", err
	}
	return stageVersion(dest, lay, laid, from, ignored)
}

// forkLayout is the skill directory dir that commit holds, as a version a
// directory is compared with, and lay, which lays it out at a destination
// byte for byte as the account repo stores it, exactly as a managed skill's
// base version is laid out: every file with the mode git records, every
// symlink as a symlink, and nothing else. It is read in one ls-tree of
// commit:dir and one cat-file of its blobs, whatever the commit holds
// beside the directory, as a commit made with git may hold a file there.
func (inv *invocation) forkLayout(ctx context.Context, gitDir, commit, dir string) (version, func(dest string) error, error) {
	entries, err := source.ReadTree(ctx, inv.git, gitDir, commit+":"+dir)
	if err != nil {
		return version{}, nil, err
	}
	base := lineage.Base{Entries: entries}
	bodies, err := source.ReadBlobs(ctx, inv.git, gitDir, baseBlobs(base))
	if err != nil {
		return version{}, nil, err
	}
	target := base.ID()
	laid := version{load: commit + ":" + dir, holds: func(id string) bool { return id == target }}
	return laid, func(dest string) error { return materialise(dest, base, bodies) }, nil
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
