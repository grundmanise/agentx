---
status: accepted
date: 2026-09-16
---

# Per-machine libraries, one account repo per account cloned on each machine, content moves only through install and update

Each machine owns its library at `~/.agents/skills`. Nothing in a library propagates to another machine on its own. Skill content reaches a machine only through an install or an update from an upstream. Forks and greenfield skills live in one git repository per account, the account repo, one subdirectory per skill. Each machine works in its own clone, with the git dir in `~/.agentx`, and the library holds symlinks into that clone. Which machine has which fork placed is metadata in the local database, not a path in the repository. Edits are committed locally on their own, one commit per skill per debounce window. Publishing is an explicit action that pushes that repository to a per-account remote, after which a fork is an ordinary upstream that other machines install and update from. Auto-push is a setting, off by default. Non-fork managed skills have no git; their base version is cached by content hash and drift is a hash comparison.

## Why

One mechanism, install-from-upstream, covers third-party skills and the user's own forks alike. A fork edited and published on machine A is a new upstream version, and machine B receives it as a regular update, which auto-applies when B's copy is unmodified. Explicit publish costs the near-instant propagation but means every version B receives is one the user released. A third-party upstream change is merged into the fork on whichever machine the user accepts it, then published; once published, no other machine is offered that upstream version again. Upstream versions are committed deterministically, so if a second machine merges the same version before the first publishes, the upstream side merges clean and only the two machines' own edits can conflict. Metadata sync carries no file content, so it stays small and cheap.

A single account repo rather than one repo per skill: a 200-skill library measured 3x the disk, 130x slower `git status` and 200x slower `git fetch` as 200 repos versus one. Git only for forks rather than the whole library: third-party skills need a base version and a hash, not a history, and giving every installed skill a git history would turn a 10 MB library into a repository the user did not ask for. The separate git dir keeps `.git` out of any directory an agent reads and out of every content hash.

## Considered and rejected

Live fleet-wide content sync, where an edit on A appears on B with no install step: needs a content transport in the sync engine, a relay through the editing machine for skills with no upstream, and a merge story for every skill rather than only forks. A git repository per skill: the fetch and status numbers above. Content-addressed file rows in the metadata sync engine with commit reconstruction on each machine: a second content pipeline duplicating what a git remote already does. CRDT per file: files are written whole by external editors, so merging happens at save time anyway.

## Consequences

An unmanaged skill or an unpublished fork cannot be installed on another machine; the user forks and publishes first. A private third-party source the target machine cannot authenticate to fails the install there; content is never relayed. Deleting a fork from the account remote never deletes it on other machines; they keep an unpublished copy marked "source removed". Two machines that independently fork the same skill under the same name cannot both publish; the second adopts the first, renames, or merges into it, and the merge is three-way because both forks share the same deterministic root commit. A fork of a plugin-owned skill replaces the plugin copy only in Gemini CLI; in Claude Code and Codex the plugin copy stays loaded under a namespaced name, so the fork coexists with it until plugin management can disable the original. A machine with no account still has full history and revert for its forks, and signing in later attaches the remote and merges histories.
