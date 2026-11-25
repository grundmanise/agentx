---
status: accepted
date: 2026-11-24
---

# agentx is for one person and their one hub

[ADR 0017](0017-the-account-remote-is-the-hub-and-the-account-repo-the-store.md) named the hub so that it could also be a repository someone else keeps to distribute skills, such as a company's. That reading pulls the product toward teams: several hubs, a choice of where to publish, and agentx taking part in how a shared repository is changed. None of that is wanted.

## Goals and non-goals

Goal: one person keeps their skills in sync across their own machines and agents, the cloud machine included.

Non-goals:

- Distributing skills to a team or a company. A repository other people share is a source like any other.
- More than one place to publish to.
- Owning anyone's Git workflow for a shared repository. A person who changes a skill there clones the repository and uses its own branches, commit messages and reviews.

The README states the same goal and non-goals.

## Decision

- Each machine has at most one **hub**: the user's own Git repository, set with `agentx source add <url> --hub`. It is the only place agentx publishes forks to, and the only place it keeps what travels between the user's machines: forks, snapshots and any later record of a machine.
- Every other repository, a company's skills included, is a shared source: agentx installs and updates its skills, and never changes how that repository is run.
- The hub keeps its name. For one person it says where their machines meet.

## Considered and rejected

- Several hubs: each publish would need a target, each machine's view of the others would split by hub, and a company hub would replace the user's own.
- Renaming the hub back to the account repo, to say it is one per person: the single-user rule above holds with either name, and the rename touches most of the code and docs.

## Consequences

- ADR 0017's reading that a hub may be kept by someone else to distribute skills is superseded. Its names stand.
- A read-only hub stays possible, for example a machine with read-only credentials, so the app still turns Publish off for one.
- The Help Center and the glossary describe the hub as the user's own.
