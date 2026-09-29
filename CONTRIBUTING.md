# Contributing to agentx

Thanks for your interest in agentx. This guide covers how to report a problem, propose a change and get a
pull request merged.

agentx is in early development, so commands, output and file formats still change. Open an issue before
you start on anything larger than a small fix, so we can agree on the approach first.

Everyone taking part is expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Set up your machine

You need:

- Go `1.27` or later – the exact version is the `go` line in [`apps/cli/go.mod`](apps/cli/go.mod)
- `git` 2.40 or later
- `make`
- macOS or Linux

Clone the repository:

```sh
git clone https://github.com/grundmanise/agentx.git
cd agentx
```

## Find your way around

| Path | What it holds |
| --- | --- |
| [`apps/cli`](apps/cli) | The Go module of the `agentx` command-line tool |
| [`CONTEXT.md`](CONTEXT.md) | The vocabulary: the terms to use in code, docs, issues and PRs |
| [`docs/adr`](docs/adr) | Architecture decisions |
| [`docs/spec`](docs/spec) | Contracts and specs, such as the CLI's output contract |
| [`docs/help-center`](docs/help-center) | The user documentation, built with Mintlify |

Read the ADRs and specs that touch the area you're changing before you start.

## Development commands

Run these from the repository root. Each target runs against the Go module in `apps/cli`:

| Target | What it does | Command |
| --- | --- | --- |
| `make fmt` | Formats every Go file in place | `gofmt -w .` |
| `make fmt-check` | Checks that every Go file is formatted, without changing any | `gofmt -l .`, fails when any file is listed |
| `make lint` | Runs the linters, including `go vet`, with the project's configuration | `golangci-lint run ./...` with `apps/cli/.golangci.yml` |
| `make tidy-check` | Checks that `go.mod` and `go.sum` list exactly the dependencies the code uses | `go mod tidy`, fails when `go.mod` or `go.sum` change |
| `make build` | Compiles every package | `go build ./...`, `CGO_ENABLED=0` on Linux and `1` on macOS |
| `make test` | Runs every test with the race detector, never from cache | `go test -race -count=1 ./...` |
| `make check` | Runs all the checks CI runs, in the same order | `fmt-check lint tidy-check build test` |

To run the tests without the race detector, as CI does on macOS, use `make test RACE=`.

`golangci-lint` needs no install: its version is pinned in `apps/cli/.golangci-lint-version`, and
`make lint` builds that release into the build cache on first use through `go run`, locally and in
CI alike. The build uses the Go version `apps/cli/go.mod` specifies.

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
  implementation only where it's needed to understand the change. For how to write a good commit
  message, read [How to Write a Git Commit Message](https://cbea.ms/git-commit/); where its rules for
  the subject line differ, the Conventional Commits title above wins.
- **Visual evidence**: if the change affects functionality, include a screenshot, a recording or, for
  CLI-only changes, the relevant CLI output.
- **Writing style**: don't use em dashes (`—`) anywhere; use en dashes (`–`), hyphens (`-`) or another
  suitable punctuation.

Keep each pull request to one change. A maintainer reviews it once CI is green.

## Open an issue

Search the [existing issues](https://github.com/grundmanise/agentx/issues) first, then open one with a
form:

- [Bug report](https://github.com/grundmanise/agentx/issues/new?template=bug_report.yml): asks what you
  ran, what you expected, what happened instead, the output of `agentx version` and `agentx doctor`,
  your operating system and the agent clients involved.
- [Feature request](https://github.com/grundmanise/agentx/issues/new?template=feature_request.yml):
  asks for the problem you want solved and the change you propose.
- [Something else](https://github.com/grundmanise/agentx/issues/new?template=other.yml): for a
  question, feedback, a documentation fix or anything else constructive.

New issues are labelled `needs-triage` until a maintainer looks at them.

Don't report a security vulnerability in a public issue.
[Report it privately](https://github.com/grundmanise/agentx/security/advisories/new) instead, as the
[security policy](SECURITY.md) describes.

## License

agentx is licensed under the [Apache License 2.0](LICENSE). By submitting a contribution, you agree that
it's licensed under the same terms, as set out in section 5 of the license.
