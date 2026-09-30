---
status: accepted
date: 2026-09-30
---

# Releases: nightlies promoted by tag, notes by git-cliff, installs by script and Homebrew

The CLI needs binaries people can install, a nightly channel for early testers and a stable channel they can rely on, with short release notes grouped as New, Changes and Fixes. The release process must stay small enough for one maintainer to read in full.

## Decision

| Concern | Choice | Why |
|---|---|---|
| Channels | A nightly prerelease of `main`, `vX.Y.Z-nightly.YYYYMMDD`, and stable releases `vX.Y.Z`, all in this repository's GitHub releases | Prereleases never become GitHub's latest release, so `releases/latest` is always the stable one. The nightly version is the release the commits lead up to, so a nightly sorts below that release and above the one before. The desktop app will use the same `vX.Y.Z` tags. |
| Promotion | Tag the nightly's commit `vX.Y.Z` and build it again | The version is compiled into the binary, so reusing the nightly's files would print the nightly version. Rebuilding the same commit with the Go version `go.mod` pins gives the code nightly users ran. |
| Version | Computed in the workflow from the types of the commits that change `apps/cli` since the last stable tag | git-cliff's own bump ignores a tag whose commit is outside its path filter, and release tags often sit on such commits. Ten lines of shell are easier to check than that interaction. |
| Release notes | [git-cliff](https://git-cliff.org), configured in `cliff.toml` | It groups conventional commits into sections, limits them to `apps/cli` and runs without an account or a service. The notes are deterministic; an AI summary was considered and deferred until the plain notes prove too coarse. |
| Build and publish | A matrix of `go build` jobs and `gh release create` in `.github/workflows/release.yml` | [GoReleaser](https://goreleaser.com) was considered and rejected for now: nightlies, monorepo tags and path filters for the notes are in its paid tier, and it doesn't generate a Homebrew formula that builds from source. What it would do here, four archives and a checksum file, is about as long in shell. Revisit when the binaries are signed and notarized, or packages such as `.deb` are wanted. |
| Install | `https://agentx.wtf/install`, a redirect to `scripts/install.sh` on `main` | It installs a release binary without `sudo`, checks it against the release's checksums and works on any Linux, since the Linux build is static. The domain lets the script move without changing the command in the docs. |
| Homebrew | A source formula in `grundmanise/homebrew-tap`, pushed by the release with a deploy key | Casks install prebuilt binaries, which macOS Gatekeeper blocks unless they are signed and notarized. A formula that builds from source avoids that, works on Linux too and gives `brew install --HEAD` for the latest code. |

## Consequences

- A pull request's title decides its place in the release notes, so the types in `CONTRIBUTING.md` carry meaning: `feat` for something new, `change` for a change to an existing feature, `fix` for a bug fix.
- Stable releases need a maintainer to run the workflow; nothing is released on merge.
- Updating is running the install script again or `brew upgrade`. An `agentx upgrade` command can reuse the same release files later.
- Homebrew installs take about a minute, since they build from source. Once the binaries are signed and notarized, the tap can switch to a cask of the release binaries.
