---
status: accepted, the shared-hub reading superseded by ADR-0020
date: 2026-11-23
---

# The account remote is the hub and the account repo the store

The repository that hub skills are published to was called the account remote, and the local Git repository in agentx home the account repo. Both names said the repository belongs to the user's account. It need not: a hub can be the user's own, shared by their machines, or kept by someone else to distribute skills, and then it may be read-only for this machine. The local repository holds every skill agentx manages, the skills of shared sources included, and exists on a machine with no remote at all.

The account remote is now the **hub**, and its skills are **hub skills**. The account repo is now the **store**, at `~/.agentx/store.git`. Everything named after them follows: the `--hub` flag of `agentx source add`, the `hub` key of a settings entry and of the `source` event, the `store` and `store_bytes` keys of the `machine` event, the `store` doctor check and exit code 8, the `hub` scope of serve's update check, and the names in the code. The [glossary](../../GLOSSARY.md), the [CLI contract](../spec/cli-contract.md) and the Help Center use the new words.

There are no aliases, migrations or redirects: the old flag, keys, check id and path are not read. This is a breaking change.

## Considered and rejected

For the hub:

- Account: names an identity, which the product keeps for the hosted account a machine may later sign in to; a hub kept by someone else is not the user's account.
- Linked remote: says how the repository is attached, not what it is for, and every source is a linked remote.
- Fleet: names the machines, not the repository, and a hub that distributes skills to other people serves no fleet of the user's.
- Workspace: editors already use it for a folder of projects, and it suggests files the user works in rather than a repository they publish to.
- The remote's own name: it differs for every user, so the docs, the help and a flag cannot name it, and `origin` already means something else in Git.

For the store, named for what it is, where agentx keeps every skill's versions, short as a path and a key, and tied to no hub, since it holds the skills of shared sources too and exists on a machine with no hub:

- Home repo: says where it lives, in agentx home, but not what it is for.
- Hub repo: it exists on a machine with no hub, and holds the skills of shared sources too.
- Library repo: the library is the directory agents read skills from, so the glossary drops store from the library's _Avoid_ list and keeps library repo on the store's.
- State repo: state already names a skill's state, the sync status and an operation's state.
- History repo: closest to its main job, but it also holds fetched sources and worktrees.
- Skills repo: reads as the library, or as a source on a Git host.
- Local repo: fits any repository on the machine.
- Cache and vault: cache suggests the repository can be thrown away, when its history exists nowhere else until a publish; vault suggests secrets.

Store has other meanings nearby: Skillshare's metadata store and git's object store, which the docs always name with their owner, and the desktop app's state store, which only its code and comments name, and which users never see.

## Consequences

Older ADRs keep the old words: read account remote as hub, your own skills as hub skills, and account repo as store.

An existing install is not carried over. Its owner moves `~/.agentx/account.git` to `~/.agentx/store.git` by hand, and runs `agentx source add <url> --hub` again, since a settings entry marked with the old key reads as a shared source.
