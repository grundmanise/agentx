---
status: accepted
date: 2026-11-24
---

# agentx is for one person and their one account repo

[ADR 0017](0017-the-account-remote-is-the-hub-and-the-account-repo-the-store.md) renamed the account remote to the hub, so that it could also be a repository someone else keeps to distribute skills, such as a company's. That reading pulls the product toward teams: several hubs, a choice of where to publish, and agentx taking part in how a shared repository is changed. None of that is wanted.

## Goals and non-goals

Goal: one person keeps their skills in sync across their own machines and agents, the cloud machine included.

Non-goals:

- Distributing skills to a team or a company. A repository other people share is a source like any other.
- More than one place to publish to.
- Owning anyone's Git workflow for a shared repository. A person who changes a skill there clones the repository and uses its own branches, commit messages and reviews.

The README states the same goal and non-goals.

## Decision

- Each machine has at most one **account repo**: the user's own Git repository, set with `agentx source add <url> --account`. It is the only place agentx publishes forks to, and the only place it keeps what travels between the user's machines: forks, snapshots and any later record of a machine.
- Every other repository, a company's skills included, is a shared source: agentx installs and updates its skills, and never changes how that repository is run.
- The hub is renamed back to the **account repo**, and hub skills to **account skills**. It names the repository by whose it is: one per person, the account a machine may later sign in to. Everything ADR 0017 named after the hub follows: the `--account` flag of `agentx source add`, the `account` key of a settings entry and of the `source` event, the `account` scope of serve's update check, the `(account)` mark of `agentx skill list`, the Help Center page, the app's copy and the names in the code. The [glossary](../../GLOSSARY.md), the [CLI contract](../spec/cli-contract.md) and the Help Center use the new words.
- The store keeps its name and path, `~/.agentx/store.git`. It is not the account repo: it is on every machine, holds the skills of shared sources too, and exists with no account repo set.

There are no aliases, migrations or redirects: `--hub`, the `hub` keys and the `hub` scope are not read. This is a breaking change.

## Considered and rejected

- Keep the hub, and allow several: each publish would need a target, each machine's view of the others would split by hub, and a company hub would replace the user's own.
- Keep the hub name with one hub: it still suggests a place many people meet, the reading this ADR rules out.

## Consequences

- ADR 0017's hub naming is superseded. Its store naming stands. Older ADRs keep their words: read account remote and hub as account repo, and hub skills as account skills.
- A read-only account repo stays possible, for example a machine with read-only credentials, so the app still turns Publish off for one.
- An existing install is not carried over. Its owner runs `agentx source add <url> --account` again, since a settings entry marked with the `hub` key reads as a shared source.
