package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grundmanise/agentx/apps/cli/internal/gitx"
	"github.com/grundmanise/agentx/apps/cli/internal/lineage"
)

// TestForkGuardsRefuseEditsMadeMeanwhile is the refusal every command that
// moves a fork's branch runs under the lock, once it recorded the edits it
// found: none for a fork whose skill directory holds its tip, or holds
// beside it only a file git ignores, which is carried over; exit code 6
// naming the fork once it holds an edit made after the judgement, found by
// the fingerprint read under the lock; and exit code 4 for a merge
// pending, which is asked about first.
func TestForkGuardsRefuseEditsMadeMeanwhile(t *testing.T) {
	t.Parallel()
	h, _ := installHarness(t)
	h.mustRun("skill", "new", "notes")
	ctx := context.Background()
	inv := h.invocation()
	gitDir := gitx.AccountRepoPath(h.agentx)
	records, err := lineage.List(ctx, inv.git, gitDir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := inv.forkSiteOf(ctx, gitDir, records["notes"])
	if err != nil {
		t.Fatal(err)
	}
	guard := func() error {
		t.Helper()
		pre, err := inv.judgeSite(ctx, f, false)
		if err != nil {
			t.Fatal(err)
		}
		_, err = inv.forkGuards(ctx, f, pre, "merged")
		return err
	}
	refused := func(what string, err error, st status, message string) {
		t.Helper()
		var got *failure
		if !errors.As(err, &got) {
			t.Fatalf("%s: %v, want a refusal", what, err)
		}
		equal(t, what+": exit", got.status, st)
		equal(t, what+": message", got.message, message)
	}
	writeFile(t, filepath.Join(f.skillDir, ".DS_Store"), "finder\n")
	if err := guard(); err != nil {
		t.Errorf("a fork holding only an ignored file: %v", err)
	}
	// An edit made after the judgement, found by the fingerprint alone.
	pre, err := inv.judgeSite(ctx, f, true)
	if err != nil || !pre.clean {
		t.Fatalf("the clean fork judged %+v, %v", pre, err)
	}
	equal(t, "the ignored files before the lock", strings.Join(pre.ignored, " "), ".DS_Store")
	// An ignored file made after the judgement is one the caller carries
	// over: the directory is judged again, and its list is the one returned.
	writeFile(t, filepath.Join(f.skillDir, "Thumbs.db"), "explorer\n")
	now, err := inv.forkGuards(ctx, f, pre, "merged")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, "the ignored files under the lock", strings.Join(now.ignored, " "), ".DS_Store Thumbs.db")
	writeFile(t, filepath.Join(f.skillDir, "SKILL.md"), skill("notes", "Edited meanwhile"))
	_, err = inv.forkGuards(ctx, f, now, "merged")
	refused("an edit made after the judgement", err, exitRefused, "notes changed while it was being merged")
	if err := os.MkdirAll(filepath.Join(h.agentx, "merges", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	refused("a merge pending", guard(), exitPendingMerge,
		"notes has a merge pending, so it cannot be merged until the merge is resolved or given up")
}
