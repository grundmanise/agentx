package lineage

import (
	"context"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// A merge of two histories of one fork, as a pull or a publish makes when
// both this machine and the account remote moved on, records which import
// is the fork's base afterwards in its Agentx-Base trailer, since the next
// update from upstream merges with that import as its base. When both
// sides are on the same import, that is it. When they took different
// versions of the same upstream, the newer one is the base, but only when
// the source's own history proves it newer: its upstream commit descends
// from the other's. Anything else keeps this machine's base: a version
// whose history the account repo never fetched, a source that was force
// pushed, two versions on diverged branches of the source, or bases from
// two different sources or directories. A base kept that way costs at most
// a conflict on the next update from upstream; no content is ever lost
// over it, and no time or order of import is ever read to guess.

// chooseBase is the rule, given whether the source history proves the
// remote side's upstream commit a descendant of the local side's. Pure.
func chooseBase(local, remote ForkLineage, remoteNewer bool) string {
	switch {
	case local.Base == remote.Base:
		return local.Base
	case !comparable(local, remote):
		return local.Base
	case remoteNewer:
		return remote.Base
	}
	return local.Base
}

// comparable reports whether two different bases are two versions of one
// upstream skill whose order the source history could prove: both read,
// both from the same source and directory, at different upstream commits.
func comparable(local, remote ForkLineage) bool {
	return local.Base != "" && remote.Base != "" && local.Problem == "" && remote.Problem == "" &&
		local.Import.Source == remote.Import.Source && local.Import.Path == remote.Import.Path &&
		local.Import.Commit != remote.Import.Commit
}

// PickBase is the base a merge of the local and the remote side of one fork
// records, see chooseBase, the order of two comparable versions proved by
// Newer.
func PickBase(ctx context.Context, r *gitx.Runner, gitDir string, local, remote ForkLineage) string {
	if local.Base == remote.Base || !comparable(local, remote) {
		return chooseBase(local, remote, false)
	}
	return chooseBase(local, remote, Newer(ctx, r, gitDir, local.Import, remote.Import))
}

// Newer reports whether the source history proves newer a later version of
// the same upstream skill than older: both from one source and directory,
// and newer's upstream commit a descendant of older's. It is proved in the
// isolated environment, at most two git processes: one rev-list that finds
// whether both upstream commits are in the account repo at all, without
// reaching any remote for one that is not, and, when they are, one
// merge-base --is-ancestor. The same version, and any failure, is no
// proof.
func Newer(ctx context.Context, r *gitx.Runner, gitDir string, older, newer Import) bool {
	a, b := older.Commit, newer.Commit
	if a == "" || b == "" || a == b || older.Source != newer.Source || older.Path != newer.Path {
		return false
	}
	if _, err := r.Isolated(ctx, gitDir, "rev-list", "--missing=print", "--no-walk", a, b); err != nil {
		return false
	}
	_, status, err := r.IsolatedStatus(ctx, gitDir, 1, "merge-base", "--is-ancestor", a, b)
	return err == nil && status == 0
}
