package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
)

// The oldest git agentx runs on: merge-tree --write-tree --merge-base needs it.
const gitFloorMajor, gitFloorMinor = 2, 40

// gitVersion returns the git version when it meets the floor and otherwise
// the exit 2 failure every command reports, with a hint naming the
// distribution package where known.
func (inv *invocation) gitVersion(ctx context.Context) (gitx.Version, error) {
	v, err := inv.git.Version(ctx)
	if err != nil {
		if errors.Is(err, gitx.ErrMissing) {
			return v, fail(exitGit, err.Error(), "install git 2.40 or newer and make sure it is in PATH")
		}
		return v, fail(exitGit, err.Error(), "install git 2.40 or newer")
	}
	if !v.AtLeast(gitFloorMajor, gitFloorMinor) {
		hint := "install git 2.40 or newer"
		if v.Major == 2 && v.Minor == 34 {
			hint = "Ubuntu 22.04 ships git 2.34; install a newer git (for example from the git-core PPA) or upgrade the distribution"
		}
		return v, fail(exitGit, fmt.Sprintf("git %s is older than 2.40", v), hint)
	}
	return v, nil
}

// needsGit reports whether a command runs git, so that startup refuses a
// missing or too-old git before it. version, doctor and help stay available
// to report the problem.
func needsGit(name string) bool {
	switch name {
	case "agentx", "version", "doctor", "help":
		return false
	}
	return true
}
