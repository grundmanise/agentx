---
status: accepted
date: 2026-11-23
---

# The app updates itself with Tauri's updater, one manifest per channel

The desktop app had no way to update. The CLI lives inside the app's bundle, so `agentx update` refuses it on purpose, and a user had to download a new package by hand and install it over the old one. Running `agentx update --check` from the app and showing a download link was tried and removed: the app could only tell, not update. The app now updates itself, the CLI inside it included, from the same releases that ADR 0007 describes.

## Decision

| Concern | Choice | Why |
|---|---|---|
| Updater | Tauri's updater plugin, `tauri-plugin-updater`. Each update file is signed with the updater's own minisign key: the public key is `plugins.updater.pubkey` in `tauri.conf.json`, and the private key and its password are the release secrets `TAURI_SIGNING_PRIVATE_KEY` and `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`. `requireSignedVersion` is on: the signature must carry the version the manifest announces | The plugin downloads an update, checks its signature against the public key it was built with and replaces the app, on macOS and on Linux. The signature is separate from Apple's signing, so it covers the Linux packages too, and a release without the Apple secrets still has updates the app can verify. The manifest itself is not signed, so a signed version stops a forged manifest from pairing a new version number with an old, validly signed release. |
| Network | The Rust side makes network calls for the update check and the download, and for nothing else. This is an exception to ADR 0001, where the app opens no network connection; the header comment of the app's update module says so | The updater must replace the bundle the CLI lives in, so the CLI cannot do it. Everything else still goes through the CLI. |
| Channel | Picked from the app's own version: a version with `-nightly.` in it follows the nightlies, any other release follows the stable releases. A local build, version `0.0.0`, has no channel and finds nothing | The same rule as `agentx update`, so the app and its CLI stay on one channel, and a nightly tester keeps getting nightlies. |
| Manifests | The updater reads `latest.json`: the version, the release notes, the publish date and, per platform, the asset to install and its signature. A stable release uploads its manifest to its own release, which `releases/latest/download/latest.json` serves. A nightly writes its manifest to the rolling prerelease `nightly-updates`, which `releases/download/nightly-updates/latest.json` serves. The release workflow builds the manifest with `jq` in its own step, and only when every platform has a signature | Prereleases never become GitHub's latest release, so nightlies need a fixed place of their own. The tag `nightly-updates` has no `v` and no `-nightly.`, so the release workflow, the install script and `agentx update` never take it for a release or a nightly. A step of its own runs again on a re-run of the publish job, so a re-run repairs a missing manifest; a re-run of an older nightly leaves a newer manifest as it is. |
| macOS asset | `agentx_desktop_darwin_arm64.app.tar.gz`, the signed and notarized app that Tauri packs and signs after it builds it, a new asset in `checksums.txt`. The disk image stays for the first install | The updater cannot install from a disk image. It unpacks the archive and replaces the `.app`. |
| Linux assets | The AppImage and the `.deb` the release already publishes, signed as they are. The app updates the one it was installed from: an AppImage replaces itself, and a `.deb` installs through `pkexec`, which asks for the user's password | No new asset. A `.deb` installs system-wide, so it needs the user's permission. |
| When | The app checks after startup, never in the startup sequence, which stays offline, and then once a day while it runs. The app preference Check for updates (ADR 0013), on by default, turns the check off | Startup must not wait for the network or fail without it. A check a day finds a new release within a day, with one request. |
| Install | A quiet status in the title bar, left of the sync status, says "Update to X.Y.Z". A click downloads, verifies and installs the update, with the download on the status's progress rim; then it says "Restart to update", and a click restarts the app. The restart quits through the app's usual exit, which stops the serve child first. Settings has App updates: the Check for updates switch and Check now | The app never restarts on its own, in the middle of the user's work. |
| Homebrew | The cask `agentx` declares `auto_updates true` | The app updates itself, so `brew upgrade` leaves it to the app, and `brew upgrade --greedy` still upgrades it. |

## Consequences

- The private key and its password must be kept safe. A lost key means every installed app must be reinstalled by hand, since the app refuses an update signed with another key. A new key reaches installed apps only in a release signed with the old one.
- Without the signing secrets, a release still publishes, with a warning, but has no manifest. Installed apps are not offered it, and until the next signed stable release, `releases/latest/download/latest.json` finds nothing.
- A local `tauri build` needs no key: `tauri.conf.json` leaves `createUpdaterArtifacts` off, and the release turns it on with `--config` only when the key is set. The release fails when the key is set and a signature is missing.
- A nightly build is offered the newest nightly, and a release the newest stable release. A stable release leaves the nightly manifest as it is.
- The check is one request to GitHub a day, which carries no data about the user beyond what GitHub sees of any download.
- Homebrew's record of the cask's version lags behind the app once the app updates itself, which is what `auto_updates` tells Homebrew to expect.
