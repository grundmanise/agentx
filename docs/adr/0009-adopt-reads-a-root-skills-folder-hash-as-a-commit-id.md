---
status: accepted
date: 2026-10-05
---

# Adoption reads the folder hash of a skill at the root of its source as a commit id

The vercel skills CLI records a git tree id as `skillFolderHash` for a skill in a folder, but for a skill at the root of its repository it records the id of the commit it installed from. It takes the hash from GitHub's trees API, and for the root that API answers with the commit it read rather than the commit's tree. `agentx adopt` therefore accepts the folder hash in two forms: the tree id of the skill's folder, and, for a skill at the root only, the id of a commit in the history of that source. A commit id names one tree as surely as a tree id does, so accepting it keeps the version verified by git's content addressing and does not turn it into a guess.

## Considered and rejected

Asking the user for `--base` whenever a skill at the root differs from its source's current version: every such skill installed by that tool would be refused, although its lock file names the version exactly. Accepting a commit id for a skill in a folder too: that tool never records one there, and a commit whose tree differs from the folder's would say nothing about the folder.
