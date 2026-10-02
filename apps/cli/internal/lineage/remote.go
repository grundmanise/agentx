package lineage

import (
	"context"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// RemoteForkPrefix is where the account repo keeps the fork branches the
// remote called remote holds, as a fetch of the remote writes them: one
// remote-tracking branch per fork branch, under the same name.
func RemoteForkPrefix(remote string) string { return gitx.TrackingPrefix(remote) + "skills/" }

// RemoteForkRef is the remote-tracking branch of the fork called name on
// the remote called remote.
func RemoteForkRef(remote, name string) string { return RemoteForkPrefix(remote) + name }

// ListRemote reads, in one git process, the commit every remote-tracking
// fork branch of the remote called remote holds, by fork name: what the
// last fetch of that remote found there. A repo with no such remote, or one
// never fetched, holds none.
func ListRemote(ctx context.Context, r *gitx.Runner, gitDir, remote string) (map[string]string, error) {
	prefix := RemoteForkPrefix(remote)
	out, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)%00%(objectname)", prefix)
	if err != nil {
		return nil, err
	}
	tips := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		ref, commit, ok := strings.Cut(line, "\x00")
		if name, under := strings.CutPrefix(ref, prefix); ok && under && name != "" {
			tips[name] = commit
		}
	}
	return tips, nil
}
