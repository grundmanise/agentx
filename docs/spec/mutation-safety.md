# Preserve edits through interrupted mutations

Status: accepted. Applies to commands that change lineage refs, worktrees, library directories, placements or machine settings.

## Adoption

Record the actual previously installed upstream version as the base. Keep the adopted directory's current content; differences from that base remain local modifications. Never import edited local content as if it came from upstream.

If the prior base cannot be established, leave the directory unmanaged. Offer an explicit base selection as the one way to adopt it anyway. It never silently replaces the directory with the latest upstream version.

## Mutation and recovery

Use the machine's mutation lock and a durable local journal at `~/.agentx/mutations/<id>.json`. This journal is required without an account and is separate from remote operation records.

1. Capture the expected refs and current content of every affected live path, including uncommitted fork edits and copy placements. Stage and validate the proposed content before replacing live content.
2. Persist the expected old state, intended new state, staged and retained-content paths, and recovery progress before changing live state. Sync the journal and its directory where the platform requires it. Keep required objects and recovery content reachable until completion.
3. Revalidate the inputs under the lock before applying. Use expected-old values when changing refs. If live content changed, re-merge or request resolution. A branch tip alone cannot detect an edit made before auto-commit.
4. Apply the recorded steps and persist progress. Retain displaced content until the final state is verified. Locks coordinate agentx processes, not editors; recheck retained content for intervening edits before discarding it. Preserve unexpected content and stop for resolution if it differs from the captured input.
5. Verify refs, live content, placements and settings agree with the reported outcome before completing the journal. A copy is unchanged only when it matches the content previously placed there, normally the library content before this mutation. Preserve a modified copy and report it for resolution, never refresh it by overwrite. Report incomplete placements explicitly.

After a crash or cancellation, recover the journal before further mutations or ordinary reconciliation. Inspect actual refs and paths because a process may have stopped after a write but before recording its progress. Resume or roll back only steps whose preconditions still hold. Otherwise report recovery required and retain both versions for resolution. Do not classify half-applied state as a new unmanaged skill or repair it by overwriting content.

An operation spanning multiple directories and refs is recoverable, not globally atomic. Agents may read across a replacement. Conflict markers stay in a scratch copy; unresolved content never replaces the library. An explicitly requested revert or removal may discard the content the user selected, but must still guard against later edits.

## Verification required by implementations

Exercise process termination at each durable boundary, including after a live write but before its journal update. Recovery must preserve both committed lineage and unexpected edits. Cover adoption followed by update, edits during a pending merge before auto-commit, a modified copy placement, cancellation, and a source path changed immediately before replacement. Re-running recovery must not repeat a completed destructive step.
