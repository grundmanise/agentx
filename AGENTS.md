## Agent skills

### Issue tracker

Issues live in the GitHub Issues of this repo. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage labels. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` at the root, ADRs in `docs/adr/`, specs in `docs/spec/`. See `docs/agents/domain.md`.

## Desktop app

Before writing UI in `apps/desktop`, read `apps/desktop/DESIGN.md` and the "Rules for components" in `apps/desktop/README.md`. Style only with the tokens in `apps/desktop/src/styles/tokens.css`, which `DESIGN.md` documents; when a design needs a value no token holds, add the token to `tokens.css` and document it in `DESIGN.md`. Our components take no `className` or `style`: give them a closed, typed prop or variant instead. `pnpm lint` and `pnpm test` reject anything off the design system; fix the code, never the guard. Ultracite (oxlint and oxfmt) lints and formats the frontend: run `pnpm fix` in `apps/desktop` before you commit.

## Help center

Keep the Help Center up to date. Whenever there's a new feature or a change in functionality, update the relevant user-facing documentation in `docs/help-center`. Keep the language clear, structured, and concise. Use an imperative tone.

Mintlify publishes every file under `docs/help-center`, so treat anything you add there as public.

Mintlify spellchecks the pages a PR changes. When it flags a product term, add the term to `docs/help-center/styles/config/vocabularies/agentx/accept.txt`.

## Tests

The CLI's tests drive real `git`, and starting a process is what they spend their time on, several times more on macOS than on Linux. Test each behavior once, at the cheapest level that shows it:

- **End to end** (through the harness, with a real home and git): one happy path per command or flag family, and one case per distinct user-visible refusal or warning, that is, a different message or exit code. Not one case per input that reaches the same branch.
- **Unit tests** for the edge cases of a pure function (names, URLs, paths, selections, overlap rules, refusal order, formatting), as a table over that function.
- **Crash recovery** at every journal boundary is tested once, without git, in `internal/home`. A command's own recovery test checks the steps it journals and stops it at no more than two boundaries.
- **Fixtures:** refusals that change nothing share one home, and a test builds only what it uses.
- **Assertions:** check the fact that matters (the exit code, a distinctive substring, an event field). One test pins an exact output or a long hint; the others check a substring.
- **Parallelism:** call `t.Parallel()` unless the test uses `t.Setenv` or `t.Chdir`, swaps a package variable, or measures time.

Before adding a test, find the one that already covers the path, and extend it instead.

## PRs and commits

### Titles

Follow the conventional commit format. GitHub uses the first commit message as the PR title, and we squash-merge PRs, so the PR title becomes the commit on `main`.

The type decides the release notes section: `feat` for something new, `change` for a change to how an existing feature behaves, `fix` for a bug fix. See "Release notes" in `CONTRIBUTING.md`.

### Descriptions

For PR descriptions, follow the PR skill in `.agents/skills/pr/SKILL.md`.

Write commit descriptions in an imperative style. Focus on how the change affects users, what changed and why, and describe the implementation only where it's needed to understand the change.

### Branches

- Name branches after the change in product terms, such as `feat/source-removed-drift`.

### Writing style

- Never use em dashes (`—`); instead, use en dashes (`–`) or hyphens (`-`).

### Public repository

This repository is public. See `docs/agents/domain.md` for what must never appear in it.
