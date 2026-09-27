package gitx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// WorkTree is a directory opened as a work tree of a repository over an
// index of its own, in a temporary directory: git reads the directory with
// every rule it applies to a work tree, the directory's own .gitignore and
// .gitattributes files, the repository's info/exclude and the user's
// global ignore file, and writes nothing to the directory or to any index
// but this one. A skill directory is compared, diffed and reverted this
// way against the account repo.
type WorkTree struct {
	r            *Runner
	gitDir       string
	dir          string
	index        string
	excludesFile string
}

// NewWorkTree opens dir, a real directory, as a work tree of the
// repository at gitDir with an empty index. excludesFile is the user's
// core.excludesFile, "" for none, which the isolated environment would
// otherwise lose; git's default place for the file needs no setting. The
// caller loads the index and closes the work tree.
func (r *Runner) NewWorkTree(gitDir, dir, excludesFile string) (*WorkTree, error) {
	tmp, err := os.MkdirTemp("", "agentx-index-")
	if err != nil {
		return nil, err
	}
	return &WorkTree{r: r, gitDir: gitDir, dir: dir, index: filepath.Join(tmp, "index"), excludesFile: excludesFile}, nil
}

// Close removes the index.
func (w *WorkTree) Close() error { return os.RemoveAll(filepath.Dir(w.index)) }

// run runs one git over the work tree, in the isolated environment and in
// the directory itself, since git lists only what is below the directory
// it runs in. Line-ending warnings are kept off stderr.
func (w *WorkTree) run(ctx context.Context, args ...string) (string, error) {
	full := append(isolatedConfig(), "-c", "core.safecrlf=false")
	if w.excludesFile != "" {
		full = append(full, "-c", "core.excludesFile="+w.excludesFile)
	}
	full = append(full, "--git-dir="+w.gitDir, "--work-tree="+w.dir)
	c := call{isolated: true, env: map[string]string{"GIT_INDEX_FILE": w.index}, dir: w.dir}
	return w.r.run(ctx, c, append(full, args...)...)
}

// Load reads treeish, a tree the repository holds or a <commit>:<path>,
// into the index: the version the directory is compared with. A file the
// version holds counts whatever an ignore rule says of it, since the index
// tracks it.
func (w *WorkTree) Load(ctx context.Context, treeish string) error {
	_, err := w.run(ctx, "read-tree", treeish)
	return err
}

// AddAll records in the index what the directory holds that git does not
// ignore, and drops from it what the directory no longer holds.
func (w *WorkTree) AddAll(ctx context.Context) error {
	_, err := w.run(ctx, "add", "-A")
	return err
}

// WriteTree writes the index as a tree and returns its id.
func (w *WorkTree) WriteTree(ctx context.Context) (string, error) {
	out, err := w.run(ctx, "write-tree")
	return strings.TrimSpace(out), err
}

// DiffCached compares base, a tree-ish of the repository, with the index:
// the status of each file that differs, NUL-terminated, and the unified
// diff of all of them. Renames are not looked for.
func (w *WorkTree) DiffCached(ctx context.Context, base string) (status, patch string, err error) {
	if status, err = w.run(ctx, "diff", "--cached", "-z", "--no-renames", "--name-status", base); err != nil {
		return "", "", err
	}
	patch, err = w.run(ctx, "diff", "--cached", "-p", "--no-renames", "--no-color", base)
	return status, patch, err
}

// Ignored lists the files of the directory that git ignores and the index
// does not track, relative to the directory with / as the separator.
func (w *WorkTree) Ignored(ctx context.Context) ([]string, error) {
	out, err := w.run(ctx, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// UserExcludesFile is the core.excludesFile the user's own configuration
// sets, read in the user environment with --path so that git expands a
// leading ~. It is "" when they set none, which git answers with exit
// status 1, and when git cannot read their configuration.
func (r *Runner) UserExcludesFile(ctx context.Context, gitDir string) string {
	out, err := r.User(ctx, gitDir, "config", "--path", "--get", "core.excludesFile")
	if err != nil {
		return ""
	}
	return out
}
