package gitx

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// SnapshotFile is the one file of a machine's branch on the account remote:
// the snapshot, at the root of the branch's tree.
const SnapshotFile = "snapshot.json"

// MachineBranch is the short name of the branch of the account remote that
// holds the snapshot of the machine with the id, such as "machines/cloud".
func MachineBranch(id string) string { return "machines/" + id }

// MachineTracking is where the account repo keeps what the last read of the
// machine's branch on the remote called remote found. The fork refspec, see
// ForkRefspec, never reaches it, so a fetch of the fork branches leaves it
// alone, and a removal of the remote takes it with every other ref under
// the remote's TrackingPrefix.
func MachineTracking(remote, id string) string {
	return TrackingPrefix(remote) + MachineBranch(id)
}

// MachineTip asks the remote called remote which commit its branch of the
// machine with the id holds, "" when it holds none, in the user's
// environment, with no hook of theirs, see networkConfig. It reads one ref
// and nothing else.
func (r *Runner) MachineTip(ctx context.Context, gitDir, remote, id string) (string, error) {
	ref := "refs/heads/" + MachineBranch(id)
	args := append(networkConfig(), "--git-dir="+gitDir, "ls-remote", "--refs", remote, ref)
	out, err := r.run(ctx, call{}, args...)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if commit, name, ok := strings.Cut(line, "\t"); ok && name == ref {
			return commit, nil
		}
	}
	return "", nil
}

// FetchMachine fetches the branch of the machine with the id from the
// remote called remote into its MachineTracking ref, and nothing else: the
// refspec is given on the command line with --refmap=, so that no fetch
// refspec of the remote's or of the user's configuration applies, quiet,
// with no tags, no FETCH_HEAD, no submodules and no lazy fetch of an object
// the account repo lacks. A read-only credential is enough.
func (r *Runner) FetchMachine(ctx context.Context, gitDir, remote, id string) error {
	refspec := "+refs/heads/" + MachineBranch(id) + ":" + MachineTracking(remote, id)
	args := append(networkConfig(), "--git-dir="+gitDir,
		"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--recurse-submodules=no", "--refmap=", remote, refspec)
	_, err := r.run(ctx, call{env: map[string]string{"GIT_NO_LAZY_FETCH": "1"}}, args...)
	return err
}

// SnapshotAt reads the snapshot file of commit, a tip of a machine's
// branch, refusing one larger than limit bytes before it reads it: size is
// its size then, and found false when the commit's tree holds no such file
// at its root.
func (r *Runner) SnapshotAt(ctx context.Context, gitDir, commit string, limit int) (b []byte, size int, found bool, err error) {
	out, err := r.Isolated(ctx, gitDir, "ls-tree", "-l", "-z", commit, "--", SnapshotFile)
	if err != nil {
		return nil, 0, false, err
	}
	meta, name, _ := strings.Cut(strings.TrimRight(out, "\x00"), "\t")
	fields := strings.Fields(meta)
	if name != SnapshotFile || len(fields) != 4 || fields[1] != "blob" {
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

// SnapshotTree writes content as the snapshot file and returns the tree
// that holds it alone, at its root, as a machine's branch holds it.
func (r *Runner) SnapshotTree(ctx context.Context, gitDir string, content []byte) (string, error) {
	blob, err := r.IsolatedInput(ctx, gitDir, strings.NewReader(string(content)), "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	tree, err := r.IsolatedInput(ctx, gitDir, strings.NewReader("100644 blob "+strings.TrimSpace(blob)+"\t"+SnapshotFile+"\n"), "mktree")
	return strings.TrimSpace(tree), err
}

// TreeOf is the tree commit points at.
func (r *Runner) TreeOf(ctx context.Context, gitDir, commit string) (string, error) {
	out, err := r.Isolated(ctx, gitDir, "rev-parse", "--verify", "--quiet", commit+"^{tree}")
	return strings.TrimSpace(out), err
}
