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

For the desktop app, you also need:

- Node `24` or later – [`.node-version`](.node-version) names it for version managers
- pnpm `12` – the exact version is `packageManager` in the root [`package.json`](package.json)
- Rust, and on Linux the WebKitGTK development packages; see the desktop app's
  [README](apps/desktop/README.md)

Clone the repository:

```sh
git clone https://github.com/grundmanise/agentx.git
cd agentx
```

For the desktop app, install its dependencies from the repository root, then Playwright's Chromium
once for its end-to-end tests:

```sh
pnpm install
pnpm --dir apps/desktop exec playwright install chromium
```

## Find your way around

| Path | What it holds |
| --- | --- |
| [`apps/cli`](apps/cli) | The Go module of the `agentx` command-line tool |
| [`apps/desktop`](apps/desktop) | The desktop app (Tauri and React); see its [README](apps/desktop/README.md) |
| [`GLOSSARY.md`](GLOSSARY.md) | The vocabulary: the terms to use in code, docs, issues and PRs |
| [`docs/adr`](docs/adr) | Architecture decisions |
| [`docs/spec`](docs/spec) | Contracts and specs, such as the CLI's output contract |
| [`docs/help-center`](docs/help-center) | The user documentation, built with Mintlify |
| [`scripts/install.sh`](scripts/install.sh), [`packaging`](packaging) | The install script and the Homebrew formula template |

Read the ADRs and specs that touch the area you're changing before you start.

## Development commands

Run these from the repository root. Targets are named `<action>-<app>`. The CLI targets run against the
Go module in `apps/cli`:

| Target | What it does | Command |
| --- | --- | --- |
| `make fmt-cli` | Formats every Go file in place | `gofmt -w .` |
| `make fmt-check-cli` | Checks that every Go file is formatted, without changing any | `gofmt -l .`, fails when any file is listed |
| `make lint-cli` | Runs the linters, including `go vet`, with the project's configuration | `golangci-lint run ./...` with `apps/cli/.golangci.yml` |
| `make tidy-check-cli` | Checks that `go.mod` and `go.sum` list exactly the dependencies the code uses | `go mod tidy`, fails when `go.mod` or `go.sum` change |
| `make build-cli` | Compiles every package | `go build ./...`, `CGO_ENABLED=0` on Linux and `1` on macOS |
| `make test-cli` | Runs every test with the race detector, never from cache | `go test -race -count=1 ./...` |
| `make check-cli` | Runs all the CLI checks CI runs, in the same order | `fmt-check-cli lint-cli tidy-check-cli build-cli test-cli` |
| `make check-desktop` | Runs all the desktop app checks CI runs: the frontend, then the Tauri shell | `pnpm install --frozen-lockfile`, `pnpm check`, `pnpm test`, `pnpm build`, then `cargo fmt --check` and `cargo clippy --locked -- -D warnings` |
| `make check` | Runs every check CI runs | `check-cli check-desktop` |

To run the tests without the race detector, as CI does on macOS, use `make test-cli RACE=`.

`golangci-lint` needs no install: its version is pinned in `apps/cli/.golangci-lint-version`, and
`make lint-cli` builds that release into the build cache on first use through `go run`, locally and in
CI alike. The build uses the Go version `apps/cli/go.mod` specifies.

`make check-desktop` needs the desktop tools listed in [Set up your machine](#set-up-your-machine),
including Playwright's Chromium. The desktop app's own commands, such as `pnpm fix`, are in its
[README](apps/desktop/README.md).

## Make a change

- **Use the vocabulary.** Name concepts as [`GLOSSARY.md`](GLOSSARY.md) defines them, and avoid the
  synonyms it lists. If a concept you need is missing, raise it in your issue or PR.
- **Respect the ADRs.** If your change contradicts an architecture decision, say so in the PR and explain
  why the decision should be reopened.
- **Test it.** Commands are [cobra](https://github.com/spf13/cobra) commands, built in
  `apps/cli/internal/cli/run.go`. Tests drive `cli.Run` against a temporary home, so they never touch
  your real agent configurations. Add or update tests with every change in behavior.
- **Update the Help Center.** When you add a feature or change how one works, update the matching page
  in [`docs/help-center`](docs/help-center). Keep it clear, structured and concise, in an imperative
  tone.
- **Run `make check`.** It runs what CI runs: the CLI checks and the desktop app checks. A green
  `make check` means a green pull request. It needs the tools of both apps, so to check only the side you
  changed, run `make check-cli` or `make check-desktop`. Run `make fmt-cli` to fix Go formatting, and
  `pnpm fix` in `apps/desktop` to fix the desktop app's formatting and lint.

## Open a pull request

- **Branch name**: name the branch after the change in product terms, such as
  `feat/source-removed-drift`.
- **Title**: use the [Conventional Commits](https://www.conventionalcommits.org/) format, such as
  `feat(skill): check managed skills for upstream updates`. Pull requests are squash
  merged, so the title becomes the commit on `main`, and its type decides where the change appears in
  the [release notes](#release-notes).
- **Description**: in an imperative style, summarize what changed for users and why. Describe the
  implementation only where it's needed to understand the change. For how to write a good commit
  message, read [How to Write a Git Commit Message](https://cbea.ms/git-commit/); where its rules for
  the subject line differ, the Conventional Commits title above wins.
- **Evidence**: if the change affects functionality or behavior, show it before and after with
  screenshots, a recording or, for an execution-based change, the console output. Otherwise, it's
  optional.
- **Writing style**: don't use em dashes (`—`) anywhere; use en dashes (`–`), hyphens (`-`) or another
  suitable punctuation.

Keep each pull request to one change. A maintainer reviews it once CI is green.

### Release notes

The release notes list the commits that change `apps/cli` or `apps/desktop`, by the type of their title:

| Type | Section |
| --- | --- |
| `feat`: a new command, flag or capability | New |
| `change`: a change to how an existing feature behaves or reads | Changes |
| `perf`, `revert` | Changes |
| `fix` | Fixes |
| `docs`, `test`, `refactor`, `style`, `chore`, `ci` | Left out |

A breaking change, marked `!` after the type or with a `BREAKING CHANGE:` footer, is marked
**Breaking:** in its section, and listed under Changes if its type is one that is left out, such as
`refactor!`. The title's description becomes the entry, so write it for someone who uses agentx. Preview the notes with [git-cliff](https://git-cliff.org):
`git cliff v0.1.0..main`.

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

## Releases

Maintainers release through the Release workflow in
[`.github/workflows/release.yml`](.github/workflows/release.yml). Every release builds the CLI for
Linux and macOS on amd64 and arm64, and the desktop app for macOS as one universal app with the CLI
inside it. It publishes the CLI archives, the app's disk image `agentx_desktop_darwin_universal.dmg`
and their checksums to [GitHub Releases](https://github.com/grundmanise/agentx/releases).

- **Nightly**: every night at 03:17 UTC, the workflow releases the head of `main` as a prerelease, such
  as `v0.3.0-nightly.20261001`, when `apps/cli` or `apps/desktop` changed since the last nightly and CI
  passed on it. The
  version is the next release the commits call for: a breaking change bumps the minor version (the
  major from 1.0 on), a `feat` or `change` the minor, anything else the patch. The 14 newest nightlies
  are kept.
- **Stable**: the Release workflow is triggered manually. By default it promotes the newest nightly:
  it tags that nightly's commit, such as `v0.3.0`, and builds it again, so `agentx version` prints the
  release version. The workflow also automatically updates the Homebrew formula.

The install script is served from `https://agentx.wtf/install`, which redirects to
[`scripts/install.sh`](scripts/install.sh) on `main`. The Homebrew formula lives in
[grundmanise/homebrew-tap](https://github.com/grundmanise/homebrew-tap), generated from
[`packaging/homebrew/agentx.rb`](packaging/homebrew/agentx.rb).

### Release secrets

The workflow reads these Actions secrets, under Settings → Secrets and variables → Actions. Without
them a release still publishes, but the step that needs them shows a warning and does less:

| Secret | What it holds | Without it |
| --- | --- | --- |
| `APPLE_CERTIFICATE` | The Developer ID Application certificate and its private key, as a `.p12` file in base64 | The app is signed ad hoc and not notarized. macOS blocks it until the user allows it in System Settings → Privacy & Security |
| `APPLE_CERTIFICATE_PASSWORD` | The password of that `.p12` file | |
| `APPLE_API_ISSUER` | The issuer ID of an App Store Connect API key | The app is signed but not notarized, and macOS blocks it the same way |
| `APPLE_API_KEY` | The key ID of that API key | |
| `APPLE_API_PRIVATE_KEY` | The contents of the key's `.p8` file | |
| `HOMEBREW_TAP_DEPLOY_KEY` | The private half of a deploy key with write access to `grundmanise/homebrew-tap` | Stable releases don't update the Homebrew formula |

To make the Apple secrets, with a membership of the Apple Developer Program:

1. **Certificate.** Only the account holder can make a Developer ID certificate. In Xcode, open
   Settings → Accounts, select the team, click Manage Certificates, then + → Developer ID Application.
   In Keychain Access, find "Developer ID Application: …" under My Certificates, right-click it, choose
   Export and save it as a `.p12` file with a password. Then:

   ```sh
   base64 -i certificate.p12 | pbcopy   # paste as APPLE_CERTIFICATE
   rm certificate.p12
   ```

   The password is `APPLE_CERTIFICATE_PASSWORD`.
2. **API key.** In [App Store Connect](https://appstoreconnect.apple.com/access/integrations/api), open
   Users and Access → Integrations → App Store Connect API, and generate a team key with the Developer
   role. The issuer ID above the table is `APPLE_API_ISSUER` and the key's Key ID is `APPLE_API_KEY`.
   Download the key, which Apple allows only once, and paste the whole `.p8` file, from
   `-----BEGIN PRIVATE KEY-----` to `-----END PRIVATE KEY-----`, as `APPLE_API_PRIVATE_KEY`.

To make the deploy key:

```sh
ssh-keygen -t ed25519 -N "" -C "agentx release" -f homebrew_tap
```

Add `homebrew_tap.pub` as a deploy key of `grundmanise/homebrew-tap`, with **Allow write access**
checked, and the contents of `homebrew_tap` as `HOMEBREW_TAP_DEPLOY_KEY`. Then delete both files.

## License

agentx is licensed under the [Apache License 2.0](LICENSE). By submitting a contribution, you agree that
it's licensed under the same terms, as set out in section 5 of the license.
