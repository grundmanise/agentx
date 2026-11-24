package gitx

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// The branches of the hub that are no fork branch, a machine's snapshot
// and a machine's inventory report, are read and written one branch at a
// time: the fork refspec, see ForkRefspec, never reaches them, so each is
// asked for by name, fetched with a refspec of its own and kept in the
// remote-tracking ref of the same short name. A pattern below is a
// branch's short name, such as "machines/cloud", or every branch under a
// prefix, written "reports/*".

// branchMatches reports whether the branch, by its short name, is one the
// patterns name. Pure.
func branchMatches(branch string, patterns []string) bool {
	for _, p := range patterns {
		if prefix, under := strings.CutSuffix(p, "*"); under {
			if len(branch) > len(prefix) && strings.HasPrefix(branch, prefix) {
				return true
			}
		} else if branch == p {
			return true
		}
	}
	return false
}

// BranchTips asks the remote called remote which commit each of its
// branches the patterns name holds, by the branch's short name, in the
// user's environment, with no hook of theirs, see networkConfig. It is one
// ls-remote whatever the number of patterns, which brings no object and
// writes nothing, so that a caller can tell which branches moved before it
// fetches any.
func (r *Runner) BranchTips(ctx context.Context, gitDir, remote string, patterns ...string) (map[string]string, error) {
	args := append(networkConfig(), "--git-dir="+gitDir, "ls-remote", "--refs", remote)
	for _, p := range patterns {
		args = append(args, "refs/heads/"+p)
	}
	out, err := r.run(ctx, call{}, args...)
	if err != nil {
		return nil, err
	}
	return parseBranchTips(out, patterns), nil
}

// TipsAt is BranchTips of the repository at url, asked where no repository
// is, as ProbeRemote asks: it needs no remote set up in the store, and no
// configuration but the user's applies. The check the serve child repeats
// asks so, for the fork branches and every machine's branch in its one
// ls-remote.
func (r *Runner) TipsAt(ctx context.Context, url string, patterns ...string) (map[string]string, error) {
	args := append(networkConfig(), "ls-remote", "--refs", url)
	for _, p := range patterns {
		args = append(args, "refs/heads/"+p)
	}
	out, err := r.run(ctx, call{dir: os.TempDir()}, args...)
	if err != nil {
		return nil, err
	}
	return parseBranchTips(out, patterns), nil
}

// parseBranchTips reads what ls-remote printed, one "<commit>\t<ref>" line
// each, by the branch's short name. A line of another ref, which a pattern
// matching the tail of a ref name lets in, is left out. Pure.
func parseBranchTips(out string, patterns []string) map[string]string {
	tips := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		commit, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if branch, heads := strings.CutPrefix(ref, "refs/heads/"); ok && heads && commit != "" && branchMatches(branch, patterns) {
			tips[branch] = commit
		}
	}
	return tips
}

// FetchBranches fetches the branches, by their short names, from the
// remote called remote, each into the remote-tracking ref of the same
// name, and nothing else: the refspecs are given on the command line with
// --refmap=, so that no fetch refspec of the remote's or of the user's
// configuration applies, quiet, with no tags, no FETCH_HEAD, no submodules
// and no lazy fetch of an object the store lacks. A read-only credential
// is enough. With no branch it runs nothing.
func (r *Runner) FetchBranches(ctx context.Context, gitDir, remote string, branches []string) error {
	if len(branches) == 0 {
		return nil
	}
	args := append(networkConfig(), "--git-dir="+gitDir,
		"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--refmap=", remote)
	for _, b := range branches {
		args = append(args, "+refs/heads/"+b+":"+TrackingPrefix(remote)+b)
	}
	_, err := r.run(ctx, call{env: map[string]string{"GIT_NO_LAZY_FETCH": "1"}}, args...)
	return err
}

// Tracked is what a remote-tracking ref of the store holds: the commit the
// last fetch of its branch brought, and when that commit was committed,
// the zero time for a ref that holds no commit.
type Tracked struct {
	Commit    string
	Committed time.Time
}

// TrackedBranches reads what the store's remote-tracking refs of the
// remote called remote hold of the branches the patterns name, by the
// branch's short name, in one for-each-ref. It reaches no remote.
func (r *Runner) TrackedBranches(ctx context.Context, gitDir, remote string, patterns ...string) (map[string]Tracked, error) {
	args := []string{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(committerdate:unix)"}
	for _, p := range patterns {
		args = append(args, TrackingPrefix(remote)+strings.TrimSuffix(p, "*"))
	}
	out, err := r.Isolated(ctx, gitDir, args...)
	if err != nil {
		return nil, err
	}
	return parseTracked(out, TrackingPrefix(remote), patterns), nil
}

// parseTracked reads what TrackedBranches had for-each-ref print, one
// "<ref>\0<commit>\0<committer time>" line each, the time in seconds since
// the epoch and empty for an object that is no commit. A ref name given as
// a pattern also matches the refs below it, so only the branches the
// patterns name are kept. Pure.
func parseTracked(out, prefix string, patterns []string) map[string]Tracked {
	tracked := map[string]Tracked{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\x00")
		if len(fields) != 3 || fields[1] == "" {
			continue
		}
		branch, under := strings.CutPrefix(fields[0], prefix)
		if !under || !branchMatches(branch, patterns) {
			continue
		}
		t := Tracked{Commit: fields[1]}
		if seconds, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
			t.Committed = time.Unix(seconds, 0).UTC()
		}
		tracked[branch] = t
	}
	return tracked
}

// SetTracked points the remote-tracking ref of the branch, a short name,
// of the remote called remote at commit, as a fetch of the branch would
// once the remote holds that commit: a push of a branch the fork refspec
// does not reach moves no remote-tracking ref itself.
func (r *Runner) SetTracked(ctx context.Context, gitDir, remote, branch, commit string) error {
	_, err := r.Isolated(ctx, gitDir, "update-ref", TrackingPrefix(remote)+branch, commit)
	return err
}

// DropTracked deletes the remote-tracking refs of the branches, short
// names, of the remote called remote, in one transaction: what a fetch
// wrote of a branch the remote no longer holds says nothing about the
// remote. With no branch it runs nothing.
func (r *Runner) DropTracked(ctx context.Context, gitDir, remote string, branches []string) error {
	refs := make([]string, len(branches))
	for i, b := range branches {
		refs[i] = TrackingPrefix(remote) + b
	}
	return r.deleteRefs(ctx, gitDir, refs)
}

// deleteRefs deletes refs, whatever each holds, in one update-ref
// transaction.
func (r *Runner) deleteRefs(ctx context.Context, gitDir string, refs []string) error {
	if len(refs) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("start\n")
	for _, ref := range refs {
		b.WriteString("delete " + ref + "\n")
	}
	b.WriteString("commit\n")
	_, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(b.String()), "update-ref", "--stdin")
	return err
}

// FileAt reads the file called name at the root of commit's tree, the one
// file a machine's branch holds, refusing one larger than limit bytes
// before it reads it: size is its size then, and found false when the
// tree holds no such file at its root.
func (r *Runner) FileAt(ctx context.Context, gitDir, commit, name string, limit int) (b []byte, size int, found bool, err error) {
	out, err := r.Isolated(ctx, gitDir, "ls-tree", "-l", "-z", commit, "--", name)
	if err != nil {
		return nil, 0, false, err
	}
	meta, held, _ := strings.Cut(strings.TrimRight(out, "\x00"), "\t")
	fields := strings.Fields(meta)
	if held != name || len(fields) != 4 || fields[1] != "blob" {
		return nil, 0, false, nil
	}
	size, err = strconv.Atoi(fields[3])
	if err != nil {
		return nil, 0, false, fmt.Errorf("git ls-tree printed %q", out)
	}
	if size > limit {
		return nil, size, true, nil
	}
	text, err := r.run(ctx, call{isolated: true}, isolatedArgs(gitDir, []string{"cat-file", "blob", fields[2]})...)
	return []byte(text), size, true, err
}

// FileTree writes content as the file called name and returns the tree
// that holds it alone, at its root, as a machine's branch holds it.
func (r *Runner) FileTree(ctx context.Context, gitDir, name string, content []byte) (string, error) {
	blob, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(string(content)), "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	tree, err := r.IsolatedInput(ctx, gitDir, strings.NewReader("100644 blob "+strings.TrimSpace(blob)+"\t"+name+"\n"), "mktree")
	return strings.TrimSpace(tree), err
}

// TreeOf is the tree commit points at.
func (r *Runner) TreeOf(ctx context.Context, gitDir, commit string) (string, error) {
	out, err := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", commit+"^{tree}")
	return strings.TrimSpace(out), err
}
