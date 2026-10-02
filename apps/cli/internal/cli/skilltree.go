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
	"github.com/grundmanise/agentx/apps/cli/internal/treeid"
)

// Every comparison of a skill directory with a version goes through this
// file, whichever command makes it: drift, diff, revert, adoption and the
// copy step. A skill directory is a git work tree: git runs with the
// account repo as its git directory, the skill directory as its work tree
// and a throwaway index loaded from the version, then add -A and
// write-tree, and the directory holds the version when the tree git writes
// is the version's. So git applies the rules it applies to any work tree:
// the skill's own .gitignore and .gitattributes files, the user's global
// ignore file and, while ignore_system_files is on, the list agentx keeps
// in the account repo's info/exclude. A file the version holds always
// counts, since the index tracks it.
//
// The in-process tree id is the fast path: when it equals the version's,
// or does once the list's names are left out while the setting is on and
// no .gitignore could say otherwise, the directory holds the version and
// no git runs.

// version is what a directory is compared with.
type version struct {
	load  string               // the tree-ish git loads into the index: "<commit>:<dir>" or a tree id the account repo holds
	holds func(id string) bool // whether a directory whose tree id is id holds the version
}

// baseVersion is the base version a managed skill's lineage records.
func baseVersion(rec lineage.Record) version {
	return version{load: rec.Commit + ":" + rec.Import.Dir(), holds: rec.HoldsID}
}

// treeVersion is a tree the account repo holds, such as one git wrote of a
// directory.
func treeVersion(tree string) version {
	return version{load: tree, holds: func(id string) bool { return id == tree }}
}

// importedVersion is the version an adoption establishes, once its import
// commit is written.
func importedVersion(v *imported) version {
	return version{load: v.commit + ":" + v.dir, holds: func(id string) bool { return treeid.Wrap(v.dir, id) == v.tree }}
}

// fastHolds decides without git that the directory read as t holds v. A
// directory holding what git cannot record never does, as before git
// decided anything.
func fastHolds(t treeid.Tree, ignoreSystem bool, v version) bool {
	if len(t.Unrecordable) > 0 {
		return false
	}
	if v.holds(t.ID) {
		return true
	}
	return ignoreSystem && !t.Has(".gitignore") && v.holds(t.Without(home.IsSystemFile))
}

// judged is how a directory compares with a version.
type judged struct {
	holds   bool
	written string   // the tree git wrote of the directory; "" when git did not run
	ignored []string // the files git ignores in it, when asked for
}

// judgeDir compares dir, the real path of a directory read as t, with v: the
// fast path first, then git. A caller that would replace the directory
// asks for its ignored files, which a replacement carries over; then only
// a tree id equal to the version's, which leaves nothing to carry, spares
// the git run.
func (inv *invocation) judgeDir(ctx context.Context, gitDir, dir string, t treeid.Tree, v version, wantIgnored bool) (judged, error) {
	switch {
	case len(t.Unrecordable) > 0:
		return judged{}, nil
	case v.holds(t.ID), !wantIgnored && fastHolds(t, inv.systemFilesIgnored(), v):
		return judged{holds: true}, nil
	}
	wt, err := inv.openWorkTree(ctx, gitDir, dir)
	if err != nil {
		return judged{}, err
	}
	defer wt.Close()
	written, err := writeWorkTree(ctx, wt, v)
	if err != nil {
		return judged{}, err
	}
	j := judged{holds: v.holds(written), written: written}
	if wantIgnored {
		j.ignored, err = wt.Ignored(ctx)
	}
	return j, err
}

// openWorkTree opens dir as a work tree of the account repo, with
// info/exclude brought in line with ignore_system_files first.
func (inv *invocation) openWorkTree(ctx context.Context, gitDir, dir string) (*gitx.WorkTree, error) {
	return inv.openWorkTreeWithin(ctx, gitDir, dir, dir)
}

// openWorkTreeWithin is openWorkTree of top with git running in dir, a
// directory inside it, see gitx.NewWorkTreeWithin.
func (inv *invocation) openWorkTreeWithin(ctx context.Context, gitDir, top, dir string) (*gitx.WorkTree, error) {
	if err := home.SyncExclude(gitDir, inv.systemFilesIgnored()); err != nil {
		return nil, err
	}
	return inv.git.NewWorkTreeWithin(gitDir, top, dir, inv.excludesFile(ctx, gitDir))
}

// writeWorkTree loads v into the work tree's index, adds the directory to
// it and writes the tree.
func writeWorkTree(ctx context.Context, wt *gitx.WorkTree, v version) (string, error) {
	if err := wt.Load(ctx, v.load); err != nil {
		return "", err
	}
	if err := wt.AddAll(ctx); err != nil {
		return "", err
	}
	return wt.WriteTree(ctx)
}

// systemFilesIgnored is ignore_system_files as the settings file holds it
// now, on, the default, when the file cannot be read.
func (inv *invocation) systemFilesIgnored() bool {
	s, err := home.LoadSettings(inv.dirs.Home)
	return err != nil || s.IgnoreSystemFiles
}

// excludesFile is the user's core.excludesFile, read once per run, and so
// once per serve process, the first time git opens a skill directory.
func (inv *invocation) excludesFile(ctx context.Context, gitDir string) string {
	inv.excludes.Do(func() { inv.excludesPath = inv.git.UserExcludesFile(ctx, gitDir) })
	return inv.excludesPath
}

// holdsBase is drift's verdict on a managed skill: whether its library
// directory holds its base version. It never fails: a directory that
// cannot be read, or that git cannot compare, is modified. Under serve,
// git's verdict is kept until the directory's tree id, the version or the
// setting changes, see verdict.
func (inv *invocation) holdsBase(ctx context.Context, lib scan.LibrarySkill, rec lineage.Record) bool {
	tree, err := treeid.Read(lib.ResolvedPath)
	if err != nil || len(tree.Unrecordable) > 0 {
		return false
	}
	v := baseVersion(rec)
	return inv.verdict(lib.Name, "its base version", tree, v, func() (bool, error) {
		j, err := inv.judgeDir(ctx, gitx.AccountRepoPath(inv.dirs.Home), lib.ResolvedPath, tree, v, false)
		return j.holds, err
	})
}

// verdict is whether the directory of the library skill called name, read
// as tree, holds v, the version drift compares it with: by the fast path,
// then by git's verdict, which judge reaches. A judge that fails is a
// directory that does not hold it, said at debug level, against naming the
// version. Serve keeps git's verdict by skill, with the tree id, the
// version and the setting it was reached on, so a rescan asks git again
// only about a directory, or a version, that changed since.
func (inv *invocation) verdict(name, against string, tree treeid.Tree, v version, judge func() (bool, error)) bool {
	ignoreSystem := inv.systemFilesIgnored()
	if fastHolds(tree, ignoreSystem, v) {
		return true
	}
	key := verdictKey{id: tree.ID, load: v.load, ignoreSystem: ignoreSystem}
	if kept, ok := inv.verdicts[name]; ok && kept.key == key {
		return kept.holds
	}
	holds, err := judge()
	if err != nil {
		inv.out.debugf("cannot compare %s with %s: %v", name, against, err)
		return false
	}
	if inv.verdicts != nil {
		inv.verdicts[name] = keptVerdict{key: key, holds: holds}
	}
	return holds
}

// keptVerdict is git's verdict on one managed skill or fork, kept by
// serve, and verdictKey what it was reached on.
type keptVerdict struct {
	key   verdictKey
	holds bool
}

type verdictKey struct {
	id           string // the directory's tree id
	load         string // the version it was compared with
	ignoreSystem bool
}

// holdsImported is adoption's verdict on the directory at path against the
// version it is adopted at, reached as a listing reaches its state, so the
// two agree. A directory that cannot be read, or that git cannot compare,
// does not hold it.
func (inv *invocation) holdsImported(ctx context.Context, gitDir, path string, v *imported) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	tree, err := treeid.Read(real)
	if err != nil {
		return false
	}
	j, err := inv.judgeDir(ctx, gitDir, real, tree, importedVersion(v), false)
	if err != nil {
		inv.out.debugf("cannot compare %s with the version it is adopted at: %v", path, err)
	}
	return err == nil && j.holds
}

// stageVersion lays a version out at dest with lay and checks that what it
// laid out is v, then carries the files git ignores in from into it, and
// makes it durable. It returns the fingerprint a publish of dest expects.
func stageVersion(dest string, lay func(dest string) error, v version, from string, ignored []string) (string, error) {
	if err := lay(dest); err != nil {
		return "", err
	}
	tree, err := treeid.Read(dest)
	if err != nil {
		return "", err
	}
	if !v.holds(tree.ID) {
		return "", fmt.Errorf("the version staged at %s holds tree %s, which is not the version meant", dest, tree.ID)
	}
	if err := carryIgnored(from, dest, ignored); err != nil {
		return "", err
	}
	if err := home.SyncTree(dest); err != nil {
		return "", err
	}
	return home.Fingerprint(dest)
}

// carryIgnored copies each of paths, files git ignores in the directory
// from, into to, as git checkout keeps the ignored files of a work tree: a
// file with its bytes and permission bits, a symlink with its target, and a
// directory, as git lists a repository nested in the directory that an
// ignore rule covers, with everything in it. A path the new content in to
// holds already is the new content's, and is not carried, and so is a path
// under a file or a symlink the new content holds where the ignored file's
// directory was: git checkout drops such a file, and carrying it would write
// through the link, perhaps outside the skill. A directory the new content
// holds a directory at too is not the new content's whole: each entry in it
// is carried by the same rule, so only a file the new content holds at the
// same path replaces the local one.
func carryIgnored(from, to string, paths []string) error {
	for _, p := range paths {
		if err := carryEntry(from, to, strings.TrimSuffix(p, "/")); err != nil {
			return err
		}
	}
	return nil
}

// carryEntry carries the slash-separated path p from from into to by
// carryIgnored's rule.
func carryEntry(from, to, p string) error {
	src := filepath.Join(from, filepath.FromSlash(p))
	held, dir, err := heldByNewContent(to, p)
	if err != nil {
		return err
	}
	if !held {
		return carryFile(src, filepath.Join(to, filepath.FromSlash(p)))
	}
	if !dir {
		return nil
	}
	info, err := os.Lstat(src)
	if err != nil || !info.IsDir() {
		return err // a file where the new content holds a directory is dropped
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := carryEntry(from, to, p+"/"+e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// heldByNewContent reports whether the content laid out in to owns the
// slash-separated path p: it holds p itself, or holds something other than
// a directory at one of p's parents. dir says that what it holds at p is a
// directory, reached through directories only. Each component is read
// without following a link, so a symlink in the new content is never
// resolved.
func heldByNewContent(to, p string) (held, dir bool, err error) {
	parts := strings.Split(p, "/")
	at := to
	for i, part := range parts {
		at = filepath.Join(at, part)
		info, err := os.Lstat(at)
		if errors.Is(err, fs.ErrNotExist) {
			return false, false, nil // the rest is the carry's to make
		}
		if err != nil {
			return false, false, err
		}
		if i == len(parts)-1 || !info.IsDir() {
			return true, i == len(parts)-1 && info.IsDir(), nil
		}
	}
	return false, false, nil
}

func carryFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dst, 0o755); err != nil { // writable until it is filled
			return err
		}
		for _, e := range entries {
			if err := carryFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(dst, info.Mode().Perm())
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is neither a file, a directory nor a symlink, and cannot be carried over", src)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := writeSynced(dst, b, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm()) // past the umask
}
