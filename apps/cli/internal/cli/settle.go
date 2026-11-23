package cli

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// settleMerge readies the merge pending for the skill called name, under
// the lock, for the update that applies it right after: with theirs false,
// it takes every file of the checkout as the user left it, as 'git add -A'
// in the checkout does, once no file that conflicted still holds a
// conflict marker; with theirs true, it drops the edits and takes the
// update whole, the checkout's index and work tree set to MERGE_HEAD's
// tree. Either way the merge is then resolved, and the update applies it
// as it applies one resolved with git, see judgePending and
// judgeForkCompletion. Only the checkout changes: the library directory,
// every placement and every branch stay as they are until the update.
func (inv *invocation) settleMerge(ctx context.Context, name string, theirs bool) error {
	gitDir, _, err := gitx.CheckStore(ctx, inv.git, inv.dirs.Home)
	if err != nil {
		return storeFailure(err)
	}
	err = home.Mutate(inv.dirs.Home, inv.refs(ctx), func() error {
		if !inv.mergePending(name) {
			return refuse(exitRefused, sanitised(name)+" has no merge pending",
				"a merge is left pending by '"+skillCommand("update", name)+"' when your edits conflict with the update; run 'agentx skill list' to see which skills have one")
		}
		path := inv.checkoutPath(name)
		if theirs {
			return inv.takeTheirs(ctx, name, path)
		}
		return inv.takeResolved(ctx, gitDir, name, path)
	})
	if err != nil {
		return mutationFailure(err)
	}
	return nil
}

// takeTheirs sets the checkout at path of the merge pending for the skill
// called name to the update it merges, MERGE_HEAD, in its index and its
// work tree, every edit dropped. A merge already committed in the checkout
// has no MERGE_HEAD, and is refused.
func (inv *invocation) takeTheirs(ctx context.Context, name, path string) error {
	paths, err := inv.gitPaths(ctx, path, "MERGE_HEAD")
	if err != nil {
		return storeFailure(err)
	}
	b, err := os.ReadFile(paths[0])
	if errors.Is(err, fs.ErrNotExist) {
		return refuse(exitRefused, "the merge of "+sanitised(name)+" was already committed in "+quotedPath(path)+", so the update cannot replace it",
			"run '"+skillCommand("update", name)+"' to apply the merge as committed, or '"+skillCommand("update", name, "--abort")+"' to give it up")
	}
	if err != nil {
		return err
	}
	if _, err := inv.git.InCheckout(ctx, path, "read-tree", "--reset", "-u", strings.TrimSpace(string(b))); err != nil {
		return storeFailure(err)
	}
	return nil
}

// takeResolved stages every file of the checkout at path of the merge
// pending for the skill called name as the user left it, once no file
// still unmerged holds a conflict marker git wrote: a marker left in is a
// merge not finished, which would reach every agent the skill is placed
// for. A merge with nothing left unmerged, staged or committed with git,
// is left as it is.
func (inv *invocation) takeResolved(ctx context.Context, gitDir, name, path string) error {
	s, err := inv.readMerge(ctx, path)
	if err != nil {
		return storeFailure(err)
	}
	if len(s.unmerged) == 0 {
		return nil
	}
	size, err := conflictMarkerSize(ctx, inv.git, gitDir, s.unmerged)
	if err != nil {
		return storeFailure(err)
	}
	for _, p := range unmergedPaths(s.unmerged) {
		body, err := os.ReadFile(filepath.Join(path, filepath.FromSlash(p)))
		if errors.Is(err, fs.ErrNotExist) {
			continue // deleted: the deletion is the resolution
		}
		if err != nil {
			return err
		}
		if holdsMarker(string(body), size) {
			file := filepath.Join(path, filepath.FromSlash(p))
			return refuse(exitPendingMerge, "failed to update the skill: "+quotedPath(file)+" still holds a conflict marker",
				"edit the file until no conflict marker is left, then run '"+skillCommand("update", name, "--continue")+"' again")
		}
	}
	if _, err := inv.git.InCheckout(ctx, path, "add", "-A"); err != nil {
		return storeFailure(err)
	}
	return nil
}

// unmergedPaths are the paths of stages as ls-files -u lists them,
// "<mode> <blob> <stage>\t<path>" each, a path's stages together, each
// path once. Pure.
func unmergedPaths(stages []string) []string {
	var paths []string
	for _, entry := range stages {
		_, p, ok := strings.Cut(entry, "\t")
		if ok && (len(paths) == 0 || paths[len(paths)-1] != p) {
			paths = append(paths, p)
		}
	}
	return paths
}

// holdsMarker reports whether a line of body opens or closes a conflict
// git wrote with markers size long: a run of exactly size '<' or '>', alone
// or followed by a space and the side's label. The middle markers, '|' and '=', are left out, since
// a line of '=' of that length is also a Markdown heading's underline. The
// markers are sized so that no line of the versions merged reads as one,
// see markerSize. Pure.
func holdsMarker(body string, size int) bool {
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		for _, c := range []string{"<", ">"} {
			marker := strings.Repeat(c, size)
			if line == marker || strings.HasPrefix(line, marker+" ") {
				return true
			}
		}
	}
	return false
}
