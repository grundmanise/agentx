package gitx

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/grundmanise/agentx/apps/cli/internal/home"
)

// ErrAccountRepo marks a git failure on the account repo, which the exit
// code table calls 8 wherever it surfaces. A mutation journal's ref step
// runs git through a RefUpdater long after the command that wrote the
// journal is gone, so the account repo can have been removed or broken
// meanwhile; the reader of that failure has to be told to look at the
// account repo rather than handed an internal error.
var ErrAccountRepo = errors.New("the account repo is unusable")

// Refs is the ref updater the mutation journal applies its lineage ref
// steps with, and recovers them with after an interrupted install. The
// journal cannot run git itself, so every command that mutates agentx home
// hands it one of these.
//
// It carries the context of the command it was made for: one command, one
// context, and a ref step outlives neither.
func (r *Runner) Refs(ctx context.Context) home.RefUpdater { return refs{r: r, ctx: ctx} }

type refs struct {
	r   *Runner
	ctx context.Context
}

// RefValue reads what ref holds through for-each-ref rather than rev-parse,
// which cannot tell a missing ref from a failure: for-each-ref prints
// nothing and succeeds when the ref does not exist.
func (x refs) RefValue(gitDir, ref string) (string, error) {
	out, err := x.r.Isolated(x.ctx, gitDir, "for-each-ref", "--format=%(objectname)", ref)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrAccountRepo, err)
	}
	return strings.TrimSpace(out), nil
}

// UpdateRef points ref at newValue with its expected old value, so that two
// commands cannot both create it. An empty oldValue is git's way of
// requiring the ref not to exist.
func (x refs) UpdateRef(gitDir, ref, newValue, oldValue string) error {
	if _, err := x.r.Isolated(x.ctx, gitDir, "update-ref", ref, newValue, oldValue); err != nil {
		return fmt.Errorf("%w: %w", ErrAccountRepo, err)
	}
	return nil
}
