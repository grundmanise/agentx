---
status: accepted
date: 2026-10-05
---

# Adoption restores the files the vercel skills CLI left out

The vercel skills CLI does not copy every file of a skill into the library: no version copies `metadata.json`, versions before 1.4.1 skip `README.md`, and versions before 1.4.5 skip every file or folder whose name starts with `_`. The base an adoption records is the upstream version, all of it, so a directory missing those files differed from its base, and every such skill was adopted `modified`, with `skill diff` showing deletions the user never made and a publish that would send them to the source.

`agentx adopt` therefore writes those files back into the library directory, with the base version's bytes. It writes only a file of the base that one of those versions skips and that the directory lacks, since the lock file does not record which version installed the skill. It never changes a file the directory holds, and it replaces the directory through the mutation journal, as an install does. This ends the rule that adoption writes nothing on disk: the directory still keeps every edit of the user's, and gains only content that was never theirs to remove.

## Considered and rejected

Ignoring those files when a directory is compared with its base: modified is decided again on every read, by every command that compares a directory with its base, and Git has no way to ignore a tracked file that is missing. The exception would spread to the listing, diff, update, publish, removal and the copies, need a per-skill marker that the import commit cannot carry without two machines writing different commits for one version, hide a deletion the user did make, and end at the first update, which writes the files back anyway. Leaving the directory as it is and calling the difference a local modification: it is not one, and it makes every skill that tool installed look edited.
