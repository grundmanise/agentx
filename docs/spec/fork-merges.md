# Fork updates and the account step

Status: accepted, 2026-09-19; updated 2026-10-02 to plain Git merges. This replaces inferred import ancestry in ADR 0003.

A fork takes in two kinds of change: a newer version of its upstream, and what another machine of the same user published of it to the account remote. Both are Git merges. Nothing is classified, nothing is selected in place of a merge, and no record of a choice is kept beside the history: the branch's commits and their trailers are the whole record.

## Required behavior

- Verify fork identity by `Agentx-Fork-ID`, the permanent id the commit that created the fork records, read along first parents from each tip. A branch of the same name whose history names another id, or names none, is refused rather than merged: two forks of one name are never tangled, and neither is published over the other.
- Verify the canonical source and subpath: an upstream version is merged into a fork only when it holds the skill under the same source and directory as the fork's base.
- Refuse while the fork has uncommitted edits, as `git merge` refuses over a work tree with changes it would overwrite. Ignored files are never edits.
- Follow the [mutation-safety](mutation-safety.md) contract: a branch moves with its old tip as the expected value, and the worktree follows through the mutation journal.
- Never write conflict markers where an agent reads: a merge that conflicts is left pending in a hidden worktree of the account repo, and the fork's worktree and branch stay as they are until it is resolved there and completed, or given up.

## Updating from upstream

An upstream update is always a three-way merge with the accepted import as the explicit base: `merge-tree --write-tree --merge-base=<accepted import> <fork tip> <new import>`, whether or not the fork holds commits of its own. So the fork's own changes are kept, a line it put back to an older upstream text stays so, and what changed upstream alone comes in clean. A clean result is committed with the fork tip and the new import as parents and `Agentx-Base` naming the new import. A conflict becomes a pending merge whose completion commit carries that same base.

## The account step and publishing

The account step of `skill update`, which takes in what another machine published of a fork before the update from upstream, and the merge a publish makes when the account remote holds commits the fork lacks, are a plain Git merge on ordinary ancestry:

- Nothing to do when the remote tip is the local tip or an ancestor of it.
- A fast-forward when the local tip is an ancestor of the remote tip. No commit is written.
- Otherwise `merge-tree --write-tree` of the two tips, with the merge base git finds. A clean result is committed with the local tip and the remote tip as parents and the base below; a conflict becomes a pending merge, resolved in its worktree like any other conflict. A binary file or a symlink both sides changed conflicts whole, as Git merges it: the pending merge holds the local side's version and the other side's as its own stage, with no markers.

Publishing pushes commits only, never forced, and only the fork's own branch: import branches, update candidates, upstream-removed markers and source refs never travel. A push the remote rejects is reported, never retried with force.

## The recorded base

Every merge commit agentx writes on a fork's branch carries `Agentx-Base`, naming the import commit that is the fork's base after that merge, so that the next upstream update merges with the right base. A greenfield skill has no upstream and records none.

For a merge of two histories of one fork:

- When both sides name the same import, that is the base.
- When they name two versions of the same source and subpath, the newer one is the base when the source history the account repo has fetched proves it: `merge-base --is-ancestor` over the two `Agentx-Upstream-Commit` values, after confirming both commits are present without reaching any remote.
- Otherwise the local side's base stays: a version whose history was never fetched, a force-pushed source, versions on diverged branches, or bases of different sources or subpaths.

A wrong base costs at most a conflict on the next upstream update. No content is lost over it, since the merge itself is Git's.

An account step that moves the fork's base to another import also drops the update candidate this machine's last check pinned, unless the same proof shows the candidate newer than the new base. A candidate pinned against the old base can be older than the version the other machine took, and merging it over the new base would take the fork back to that older version. The next check pins an update against the new base.

## Deterministic parentless imports

Every upstream version becomes a commit with no parent whose id is a pure function of the version and its coordinates, identical on every machine. So two machines that took the same upstream version merge clean when neither has edited since, and two that took different versions merge clean where the versions changed different lines and conflict where both changed the same lines, as Git merges any two histories. Two machines that each took the same version share its import commit as a merge base beside the last commit they shared. The two bases have no common ancestor, so Git merges over a virtual base of its own, in which every line where the two bases differ conflicts: each line the version changed, and each line the fork had changed from upstream before it took the version. Until the two machines have synced once since taking the version, a commit on either one that edits such a line, or a line next to one, conflicts on the other's update or publish, even when the other edited nothing: a line put back to the older upstream text conflicts rather than being taken back without a word, and any other edit there conflicts too. Once either machine has merged the other's commits, that merge is their one base, and later edits merge as any others do.

## No inferred ancestry

Ancestry is never inferred: no grafts, no replacement refs, no ordering by import time or committer time, and no fallback when history is missing. What Git's own history does not prove is not assumed.

## Acceptance

- The same upstream version on two machines merges clean. Until the two have synced once, an edit on either machine to a line the version changed or the fork had changed from upstream, or to a line next to one, conflicts on the other's update or publish.
- Different upstream versions merge clean where they changed different lines and conflict where both changed the same lines; the conflict is resolved like any other.
- The recorded `Agentx-Base` follows the base rule above, never a timestamp.
- A rollback of an upstream line on one machine survives an update on the other, or surfaces as a conflict; it is never silently undone.
- An update never takes a fork back to an upstream version older than the one the account step brought it.
- Custom content is never silently lost.
- Source aliases, a cross-subpath candidate, unavailable or force-pushed source history, file modes, binary files and symlinks behave as specified.
- A different fork of the same name on the remote is refused, by its fork id.
