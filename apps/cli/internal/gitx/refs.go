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

// RefValues reads what every ref holds in one for-each-ref rather than in
// one rev-parse each: an install of thirty skills reads thirty branches and
// must not cost thirty git processes. for-each-ref is also the only read
// that can tell a missing ref from a failure, printing nothing and
// succeeding when the ref does not exist, where rev-parse --verify --quiet
// exits non-zero for both.
//
// A ref name given as a pattern also matches the refs below it, so only the
// names asked for are kept: refs/heads/managed/a must not answer for
// refs/heads/managed/a/b.
func (x refs) RefValues(gitDir string, names []string) (map[string]string, error) {
	values := map[string]string{}
	if len(names) == 0 {
		return values, nil
	}
	args := append([]string{"for-each-ref", "--format=%(refname)%00%(objectname)"}, names...)
	out, err := x.r.Isolated(x.ctx, gitDir, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAccountRepo, err)
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	for _, line := range strings.Split(out, "\n") {
		ref, id, ok := strings.Cut(strings.TrimSpace(line), "\x00")
		if ok && wanted[ref] {
			values[ref] = id
		}
	}
	return values, nil
}

// UpdateRefs points every ref at its new value with its expected old value,
// so that two commands cannot both claim one name. They go in one
// update-ref --stdin, which is one git process for a whole install and one
// transaction: a batch either moves every ref or none, so a journal is
// never left with half its branches written. An empty old value is written
// as the empty string, which is git's way of requiring the ref not to exist
// and is the same for a repository written with sha1 or with sha256. An
// empty new value is a deletion, which a removal records for the import
// branch and the candidate ref of the skill it takes away; it too carries
// its expected old value, so a branch that moved since the journal was
// written is refused rather than dropped.
//
// The updates are wrapped in start and commit. Without them git commits
// whatever prefix of the stream it managed to read when the input ends, so
// a writer that died part way through would leave some of the batch's refs
// written and the rest not, the one thing the transaction is here to
// prevent. With them a stream that does not reach its commit changes
// nothing, the way the import's fast-import protects itself with --done.
// A removal's deletions ride in that same transaction, so a refused
// deletion takes the whole batch with it.
func (x refs) UpdateRefs(gitDir string, updates []home.RefUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("start\n")
	for _, u := range updates {
		if u.New == "" {
			// git refuses a zero old value on a delete, so a deletion whose
			// expected old value is empty is written without one; a journal
			// never records that, since a ref that holds nothing is already
			// where the step leaves it.
			line := "delete " + u.Ref
			if u.Old != "" {
				line += " " + u.Old
			}
			b.WriteString(line + "\n")
			continue
		}
		old := u.Old
		if old == "" {
			old = `""`
		}
		b.WriteString("update " + u.Ref + " " + u.New + " " + old + "\n")
	}
	b.WriteString("commit\n")
	if _, err := x.r.IsolatedInput(x.ctx, gitDir, strings.NewReader(b.String()), "update-ref", "--stdin"); err != nil {
		return fmt.Errorf("%w: %w", ErrAccountRepo, err)
	}
	return nil
}
