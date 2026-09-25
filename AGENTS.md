## Agent skills

### Issue tracker

Issues live in the GitHub Issues of this repo. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage labels. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` at the root, ADRs in `docs/adr/`, specs in `docs/spec/`. See `docs/agents/domain.md`.

## Help center

Mintlify is used for the user-facing documentation. Keep the Help Center up to date. Whenever there's a new feature or a change in functionality, update the relevant user-facing documentation in `docs/help-center`. Keep the language clear, structured, and concise. Use an imperative tone.

## PRs and commits

### Titles

PR and commit titles should follow the conventional commit format. We squash merge PRs, so the first commit message becomes the PR title.

### Descriptions

PR and commit descriptions should use an imperative style and provide a clear, concise summary of the changes made. They should focus on how the changes affect end users – what changed and why – rather than on how it was implemented, unless that’s essential for understanding the changes. Never add links to agent sessions to commits or PRs.

### Branches

- Name branches after the change in product terms, such as `feat/source-removed-drift`.

### Writing style

- Never use em dashes (`—`); instead, use en dashes (`–`) or hyphens (`-`).

### Public repository

This repository is public. See `docs/agents/domain.md` for what must never appear in it.
