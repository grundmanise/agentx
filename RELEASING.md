# Releasing agentx

This guide is for maintainers. Contributors don't need it: see [`CONTRIBUTING.md`](CONTRIBUTING.md).

## How releases work

Releases go through the Release workflow in
[`.github/workflows/release.yml`](.github/workflows/release.yml). Every release builds the CLI for
Linux on amd64 and arm64 and for macOS on arm64, and the desktop app with the CLI inside it: for
Apple silicon Macs as the disk image `agentx_desktop_darwin_arm64.dmg`, and for Linux on amd64 and
arm64 as `agentx_desktop_linux_<arch>.deb` and `agentx_desktop_linux_<arch>.AppImage`. It publishes
the CLI archives, the app packages and their checksums to
[GitHub Releases](https://github.com/grundmanise/agentx/releases).

- **Nightly**: once a day, the workflow releases the head of `main` as a prerelease, such
  as `v0.3.0-nightly.20261001`, when `apps/cli` or `apps/desktop` changed since the last nightly and CI
  passed on it. The version is the next release the commits call for: a breaking change bumps the
  minor version (the major from 1.0 on), a `feat` or `change` the minor, anything else the patch. The
  14 newest nightlies are kept. The run is scheduled for 03:17 UTC, but GitHub starts scheduled runs
  late when it is busy, often by several hours.
- **Stable**: run the Release workflow by hand, from the Actions tab, once a nightly has proved good
  enough to promote. By default it promotes the newest nightly: it tags that nightly's commit, such as
  `v0.3.0`, and builds it again, so `agentx version` prints the release version. It also updates the
  Homebrew tap.

The install script is served from `https://agentx.wtf/install`, which redirects to
[`scripts/install.sh`](scripts/install.sh) on `main`.

## Homebrew

[grundmanise/homebrew-tap](https://github.com/grundmanise/homebrew-tap) holds two packages, which
each stable release writes from the templates in [`packaging/homebrew`](packaging/homebrew), in one
commit:

| Package | Template | What it installs |
| --- | --- | --- |
| Cask `agentx` | `agentx.cask.rb` | The app from its disk image, with the CLI inside it linked onto `PATH`. For Apple silicon Macs. Updated only when the release's app was notarized, since Gatekeeper blocks an unnotarized download |
| Formula `agentx-cli` | `agentx-cli.rb` | The CLI alone, built from source. For macOS and Linux |

The two conflict, since both put `agentx` on `PATH`. Edit the templates, not the tap: the next
release overwrites the tap's copies.

To test a tap change before a release, fill in the templates by hand and push them to a branch of the
tap. Then check that branch out in your local copy of the tap, `$(brew --repository grundmanise/tap)`,
and run `brew audit --cask agentx`, `brew install --cask agentx`, `brew install agentx-cli`, which the
cask refuses, and `brew uninstall` of each. Check out `main` again when you are done.

## Secrets

The workflow reads these Actions secrets, under Settings → Secrets and variables → Actions. Without
them a release still publishes, but the step that needs them shows a warning and does less:

| Secret | What it holds | Without it |
| --- | --- | --- |
| `APPLE_CERTIFICATE` | The Developer ID Application certificate and its private key, as a `.p12` file in base64 | The app is signed ad hoc and not notarized. macOS blocks it until the user allows it in System Settings → Privacy & Security |
| `APPLE_CERTIFICATE_PASSWORD` | The password of that `.p12` file | |
| `APPLE_API_ISSUER` | The issuer ID of an App Store Connect API key | The app is signed but not notarized, and macOS blocks it the same way. A stable release leaves the Homebrew cask as it was |
| `APPLE_API_KEY` | The key ID of that API key | |
| `APPLE_API_PRIVATE_KEY` | The contents of the key's `.p8` file | |
| `HOMEBREW_TAP_DEPLOY_KEY` | The private half of a deploy key with write access to `grundmanise/homebrew-tap` | Stable releases don't update the Homebrew cask and formula |

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
