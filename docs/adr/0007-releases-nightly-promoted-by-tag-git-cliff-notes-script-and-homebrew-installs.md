---
status: accepted
date: 2026-09-30
---

# Releases: nightlies promoted by tag, notes by git-cliff, installs by script and Homebrew

The CLI needs binaries people can install, a nightly channel for early testers and a stable channel they can rely on, with short release notes grouped as New, Changes and Fixes. The desktop app ships with the CLI of its own release inside it (ADR 0001), so the same release builds both. The release process must stay small enough for one maintainer to read in full.

Amended 2026-10-30: the release builds the desktop app, Homebrew gets a cask of the app under the product's name and the source formula becomes `agentx-cli`.

## Decision

| Concern | Choice | Why |
|---|---|---|
| Channels | A nightly prerelease of `main`, `vX.Y.Z-nightly.YYYYMMDD`, and stable releases `vX.Y.Z`, all in this repository's GitHub releases | Prereleases never become GitHub's latest release, so `releases/latest` is always the stable one. The nightly version is the release the commits lead up to, so a nightly sorts below that release and above the one before. The desktop app has the same version and the same tags: one release carries the CLI and the app. |
| Promotion | Tag the nightly's commit `vX.Y.Z` and build it again | The version is compiled into the binary, so reusing the nightly's files would print the nightly version. Rebuilding the same commit with the Go version `go.mod` pins gives the code nightly users ran. |
| Version | Computed in the workflow from the types of the commits that change `apps/cli` or `apps/desktop` since the last stable tag | git-cliff's own bump ignores a tag whose commit is outside its path filter, and release tags often sit on such commits. Ten lines of shell are easier to check than that interaction. |
| Release notes | [git-cliff](https://git-cliff.org), configured in `cliff.toml` | It groups conventional commits into sections, limits them to `apps/cli` and `apps/desktop` and runs without an account or a service. The notes are deterministic; an AI summary was considered and deferred until the plain notes prove too coarse. |
| Build and publish | A matrix of `go build` jobs for the CLI, a `tauri build` job per platform for the app, and `gh release create` in `.github/workflows/release.yml` | [GoReleaser](https://goreleaser.com) was considered and rejected: nightlies, monorepo tags and path filters for the notes are in its paid tier, and it doesn't generate a Homebrew formula that builds from source. What it would do here, the archives and a checksum file, is about as long in shell. |
| Desktop app | Built by the release from the same commit, with the CLI the release built unpacked next to it and passed to Tauri as `bundle.externalBin`, so Tauri copies it next to the app's executable. For Apple silicon Macs, a disk image, signed and notarized when the Apple secrets are set. For Linux on amd64 and arm64, a `.deb` and an AppImage, built on Ubuntu 22.04 so they run on distributions from 2022 on. The assets carry no version in their names. Each job checks that the CLI inside the app reports the release version before it uploads | The app runs only the CLI next to its executable (ADR 0001), so the release is the one place that puts it there. `tauri.conf.json` keeps version `0.0.0` and no `externalBin`: the release passes both with `--config`, and `tauri dev` needs no sidecar. An asset name without a version lets `releases/latest/download/<asset>` always get the newest. |
| Install | `https://agentx.wtf/install`, a redirect to `scripts/install.sh` on `main` | It installs a release binary without `sudo`, checks it against the release's checksums and works on any Linux, since the Linux build is static. The domain lets the script move without changing the command in the docs. |
| Homebrew | Two packages in `grundmanise/homebrew-tap`, pushed by each stable release with a deploy key: the cask `agentx`, which installs the notarized app and links the CLI inside it onto `PATH`, and the source formula `agentx-cli`, the CLI alone. The cask depends on arm64 and on the `git` formula, has no `auto_updates` and zaps only the app's own files, never `~/.agentx`. The release updates the cask only when the app was notarized | `brew install grundmanise/tap/agentx` gives the app and the CLI at the same version in one step, the way users expect the product's own name to work. The notarized app passes Gatekeeper, which is what blocked a cask before. The formula stays for the CLI alone, for Linux, which has no casks, and for `brew install --HEAD` of the latest code. |

## Consequences

- A pull request's title decides its place in the release notes, so the types in `CONTRIBUTING.md` carry meaning: `feat` for something new, `change` for a change to an existing feature, `fix` for a bug fix.
- Stable releases need a maintainer to run the workflow; nothing is released on merge.
- A change only in `apps/desktop` produces a nightly, and its commits appear in the notes.
- Every release carries the app, so the release needs the Apple secrets and the WebKitGTK packages, and takes longer than a CLI build. Without the Apple secrets a release still publishes, with an unsigned or unnotarized app and a warning, and a stable release then leaves the cask as it was.
- `brew install grundmanise/tap/agentx` installs the app with its CLI on macOS, and `brew install grundmanise/tap/agentx-cli` the CLI alone on macOS and Linux. Both link `agentx` into Homebrew's `bin`, and Homebrew keeps only the first link. Homebrew 5 removed `conflicts_with formula:` from casks, so the install page says to install one or the other.
- The `.deb` depends on `git (>= 1:2.40)`, the floor the CLI enforces, so `apt` refuses it on Ubuntu 22.04 and Debian 12 until the user installs a newer git; the install page says where to get one. The app would otherwise install and fail its startup check on every launch.
- Updating is running the install script again, `brew upgrade agentx` or `brew upgrade agentx-cli`, or installing the newest app package. The app has no updater yet; an `agentx upgrade` command can reuse the same release files later.
- `agentx-cli` installs take about a minute, since they build from source. The cask installs the prebuilt app.
