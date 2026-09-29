# Contributing to agentx

Thanks for your interest in agentx. This guide covers how to report a problem, propose a change and get a
pull request merged.

agentx is in early development, so commands, output and file formats still change. Open an issue before
you start on anything larger than a small fix, so we can agree on the approach first.

Everyone taking part is expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Report a bug or request a feature

Open a [GitHub issue](https://github.com/grundmanise/agentx/issues). For a bug, include:

- what you ran and what you expected
- what happened instead, with the full output
- the output of `agentx version` and `agentx doctor`
- your operating system and the agent clients involved

New issues are labelled `needs-triage` until a maintainer looks at them.

Don't report a security vulnerability in a public issue. Report it privately through the repository's
[Security tab](https://github.com/grundmanise/agentx/security) instead.

## Set up your machine

You need:

- Go `1.27` or later – the exact version is the `go` line in [`apps/cli/go.mod`](apps/cli/go.mod)
- `git` 2.40 or later
- `make`
- macOS or Linux

Clone the repository and run the checks:

```sh
git clone https://github.com/grundmanise/agentx.git
cd agentx
make check
```

`golangci-lint` needs no install: `make lint` builds the pinned version on first use. See
[Development](README.md#development) in the README for every Makefile target.

## Find your way around

| Path | What it holds |
| --- | --- |
| [`apps/cli`](apps/cli) | The Go module of the `agentx` command-line tool |
| [`CONTEXT.md`](CONTEXT.md) | The vocabulary: the terms to use in code, docs, issues and PRs |
| [`docs/adr`](docs/adr) | Architecture decisions |
| [`docs/spec`](docs/spec) | Contracts and specs, such as the CLI's output contract |
| [`docs/help-center`](docs/help-center) | The user documentation, built with Mintlify |

Read the ADRs and specs that touch the area you're changing before you start.

## Make a change

- **Use the vocabulary.** Name concepts as [`CONTEXT.md`](CONTEXT.md) defines them, and avoid the
  synonyms it lists. If a concept you need is missing, raise it in your issue or PR.
- **Respect the ADRs.** If your change contradicts an architecture decision, say so in the PR and explain
  why the decision should be reopened.
- **Test it.** Commands are [cobra](https://github.com/spf13/cobra) commands, built in
  `apps/cli/internal/cli/run.go`. Tests drive `cli.Run` against a temporary home, so they never touch
  your real agent configurations. Add or update tests with every change in behavior.
- **Update the Help Center.** When you add a feature or change how one works, update the matching page
  in [`docs/help-center`](docs/help-center). Keep it clear, structured and concise, in an imperative
  tone.
- **Run `make check`.** It runs what CI runs, in the same order. A green `make check` means a green pull
  request. Run `make fmt` to fix formatting.

## Open a pull request

- **Branch name**: name the branch after the change in product terms, such as
  `feat/source-removed-drift`.
- **Title**: use the [Conventional Commits](https://www.conventionalcommits.org/) format, such as
  `feat(skill): check managed skills for upstream updates`. Pull requests are squash
  merged, so the title becomes the commit on `main`.
- **Description**: in an imperative style, summarize what changed for users and why. Describe the
  implementation only where it's needed to understand the change.
- **Visual evidence**: if the change affects functionality, include a screenshot, a recording or, for
  CLI-only changes, the relevant CLI output.
- **Writing style**: don't use em dashes (`—`) anywhere; use en dashes (`–`) or hyphens (`-`).

Keep each pull request to one change. A maintainer reviews it once CI is green.

## License

agentx is licensed under the [Apache License 2.0](LICENSE). By submitting a contribution, you agree that
it's licensed under the same terms, as set out in section 5 of the license.
