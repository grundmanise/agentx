---
status: accepted
date: 2026-11-23
---

# The account remote is the hub and the account repo the home repo

The repository that hub skills are published to was called the account remote, and the local Git repository in agentx home the account repo. Both names said the repository belongs to the user's account. It need not: a hub can be the user's own, shared by their machines, or kept by someone else to distribute skills, and then it may be read-only for this machine. The local repository holds every skill agentx manages, the skills of shared sources included, and exists on a machine with no remote at all.

The account remote is now the **hub**, and its skills are **hub skills**. The account repo is now the **home repo**, at `~/.agentx/home.git`. Everything named after them follows: the `--hub` flag of `agentx source add`, the `hub` key of a settings entry and of the `source` event, the `home_repo` and `home_repo_bytes` keys of the `machine` event, the `home_repo` doctor check and exit code 8, the `hub` scope of serve's update check, and the names in the code. The [glossary](../../GLOSSARY.md), the [CLI contract](../spec/cli-contract.md) and the Help Center use the new words.

There are no aliases, migrations or redirects: the old flag, keys, check id and path are not read. This is a breaking change.

## Considered and rejected

- Account: names an identity, which the product keeps for the hosted account a machine may later sign in to; a hub kept by someone else is not the user's account.
- Linked remote: says how the repository is attached, not what it is for, and every source is a linked remote.
- Fleet: names the machines, not the repository, and a hub that distributes skills to other people serves no fleet of the user's.
- Workspace: editors already use it for a folder of projects, and it suggests files the user works in rather than a repository they publish to.
- The remote's own name: it differs for every user, so the docs, the help and a flag cannot name it, and `origin` already means something else in Git.

## Consequences

Older ADRs keep the old words: read account remote as hub, your own skills as hub skills, and account repo as home repo.

An existing install is not carried over. Its owner moves `~/.agentx/account.git` to `~/.agentx/home.git` by hand, and runs `agentx source add <url> --hub` again, since a settings entry marked with the old key reads as a shared source.
