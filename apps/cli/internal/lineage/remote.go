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

// RemoteTip is what a remote-tracking fork branch holds: the commit, and
// that commit's committer time, RFC 3339 in UTC, which is when the machine
// that published it recorded the version; "" when git printed none.
type RemoteTip struct {
	Commit string
	Time   string
}

// ListRemote reads, in one git process, the commit every remote-tracking
// fork branch of the remote called remote holds, by fork name: what the
// last fetch of that remote found there. A repo with no such remote, or one
// never fetched, holds none.
func ListRemote(ctx context.Context, r *gitx.Runner, gitDir, remote string) (map[string]string, error) {
	tips, err := ListRemoteTips(ctx, r, gitDir, remote)
	if err != nil {
		return nil, err
	}
	commits := make(map[string]string, len(tips))
	for name, tip := range tips {
		commits[name] = tip.Commit
	}
	return commits, nil
}

// ListRemoteTips is ListRemote with each tip's committer time, read in the
// same for-each-ref, for a reader that says when what another machine
// published was recorded.
func ListRemoteTips(ctx context.Context, r *gitx.Runner, gitDir, remote string) (map[string]RemoteTip, error) {
	prefix := RemoteForkPrefix(remote)
	out, err := r.Isolated(ctx, gitDir, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(committerdate:unix)", prefix)
	if err != nil {
		return nil, err
	}
	tips := map[string]RemoteTip{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, "\x00", 3)
		if len(fields) != 3 {
			continue
		}
		if name, under := strings.CutPrefix(fields[0], prefix); under && name != "" {
			tips[name] = RemoteTip{Commit: fields[1], Time: UpstreamTime(fields[2])}
		}
	}
	return tips, nil
}
