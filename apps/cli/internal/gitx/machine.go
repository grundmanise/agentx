package gitx

import (
	"context"
	"strings"
)

// SnapshotFile is the one file of a machine's branch on the hub:
// the snapshot, at the root of the branch's tree.
const SnapshotFile = "snapshot.json"

// MachineBranch is the short name of the branch of the hub that
// holds the snapshot of the machine with the id, such as "machines/cloud".
func MachineBranch(id string) string { return "machines/" + id }

// MachineTracking is where the store keeps what the last read of the
// machine's branch on the remote called remote found. The fork refspec, see
// ForkRefspec, never reaches it, so a fetch of the fork branches leaves it
// alone, and a removal of the remote takes it with every other ref under
// the remote's TrackingPrefix.
func MachineTracking(remote, id string) string {
	return TrackingPrefix(remote) + MachineBranch(id)
}

// MachineTip asks the remote called remote which commit its branch of the
// machine with the id holds, "" when it holds none, see BranchTips. It
// reads one ref and nothing else.
func (r *Runner) MachineTip(ctx context.Context, gitDir, remote, id string) (string, error) {
	tips, err := r.BranchTips(ctx, gitDir, remote, MachineBranch(id))
	return tips[MachineBranch(id)], err
}

// FetchMachine fetches the branch of the machine with the id from the
// remote called remote into its MachineTracking ref, and nothing else, see
// FetchBranches.
func (r *Runner) FetchMachine(ctx context.Context, gitDir, remote, id string) error {
	return r.FetchBranches(ctx, gitDir, remote, []string{MachineBranch(id)})
}

// ReportFile is the one file of a machine's report branch on the hub: its
// inventory report, at the root of the branch's tree.
const ReportFile = "inventory.json"

// ReportBranches names every report branch of the hub, as a pattern
// BranchTips and TrackedBranches take.
const ReportBranches = "reports/*"

// ReportBranch is the short name of the branch of the hub that holds the
// inventory report of the machine with the id.
func ReportBranch(id string) string { return "reports/" + id }

// ReportMachine is the machine id the branch, a short name, is the report
// branch of, ok false for a branch that is no report branch. The id is
// whatever follows the prefix: whether it is one a machine has is the
// caller's to judge.
func ReportMachine(branch string) (id string, ok bool) {
	id, ok = strings.CutPrefix(branch, "reports/")
	return id, ok && id != ""
}
