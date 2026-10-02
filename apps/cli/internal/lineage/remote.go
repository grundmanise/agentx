package lineage

import (
	"context"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// RemoteForkPrefix is where the account repo keeps the fork branches the
// account remote holds, as a fetch of the remote writes them: one
// remote-tracking branch per fork branch, under the same name.
const RemoteForkPrefix = "refs/remotes/" + gitx.RemoteName + "/skills/"

// RemoteForkRef is the remote-tracking branch of the fork called name.
func RemoteForkRef(name string) string { return RemoteForkPrefix + name }

// ListRemote reads, in one git process, the commit every remote-tracking
// fork branch holds, by fork name: what the last fetch of the account
// remote found there. A repo with no remote, or one never fetched, holds
// none.
func ListRemote(ctx context.Context, r *gitx.Runner, gitDir string) (map[string]string, error) {
	out, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)%00%(objectname)", RemoteForkPrefix)
	if err != nil {
		return nil, err
	}
	tips := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		ref, commit, ok := strings.Cut(line, "\x00")
		if name, under := strings.CutPrefix(ref, RemoteForkPrefix); ok && under && name != "" {
			tips[name] = commit
		}
	}
	return tips, nil
}
