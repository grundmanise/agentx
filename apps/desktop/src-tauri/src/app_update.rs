// The app's own updates (ADR 0019): Tauri's updater plugin reads the stable releases' update
// manifest, and the nightlies' too when the Include nightly builds preference is on, and on
// Install downloads the newest update it found, checks its signature against the public key in
// `tauri.conf.json` and replaces the app, the CLI inside it included. The new version runs after
// a restart, which the user starts. An update is only ever newer than the app, so turning
// nightlies off leaves a nightly as it is until a newer stable release comes; a build that is no
// release, version 0.0.0, finds nothing. Beside panic_reports.rs, this is the one place the Rust side reaches
// the network.

use std::sync::Mutex;
use std::time::Duration;

use serde::Serialize;
use tauri::{AppHandle, Emitter, State};
use tauri_plugin_updater::{Update, UpdaterExt};

/// The manifest stable releases publish: an asset of the latest release.
const STABLE: &str = "https://github.com/grundmanise/agentx/releases/latest/download/latest.json";
/// The manifest nightlies publish: an asset of the rolling `nightly-updates` prerelease.
const NIGHTLY: &str =
    "https://github.com/grundmanise/agentx/releases/download/nightly-updates/latest.json";

/// How long a check may take, and a download: a stalled server ends in an error the user can retry.
const CHECK_TIMEOUT: Duration = Duration::from_secs(30);
const DOWNLOAD_TIMEOUT: Duration = Duration::from_secs(15 * 60);

/// The event that carries the download's progress while an update installs.
pub const PROGRESS_EVENT: &str = "app-update-progress";

/// The manifests a check reads for `version`: none for a build that is no release, the stable
/// releases' one, and the nightlies' too with `nightlies`.
pub fn manifest_urls(version: &str, nightlies: bool) -> Vec<&'static str> {
    if version == "0.0.0" {
        Vec::new()
    } else if nightlies {
        vec![STABLE, NIGHTLY]
    } else {
        vec![STABLE]
    }
}

/// Whether `version` is a nightly build's, which takes nightly builds unless the Include nightly
/// builds preference says otherwise.
pub fn is_nightly(version: &str) -> bool {
    version.contains("-nightly.")
}

/// The update of the newest version, or none.
fn newest(updates: Vec<Update>) -> Option<Update> {
    updates.into_iter().max_by(|a, b| {
        match (
            semver::Version::parse(&a.version),
            semver::Version::parse(&b.version),
        ) {
            (Ok(a), Ok(b)) => a.cmp(&b),
            _ => a.version.cmp(&b.version),
        }
    })
}

/// Reads the manifest at `url`, and returns the update it announces when it is newer than this
/// app.
async fn check_one(app: &AppHandle, url: &str) -> Result<Option<Update>, String> {
    app.updater_builder()
        .endpoints(vec![url.parse().map_err(|err| format!("{err}"))?])
        .map(|builder| builder.timeout(CHECK_TIMEOUT))
        .and_then(|builder| builder.build())
        .map_err(|err| err.to_string())?
        .check()
        .await
        .map_err(|err| err.to_string())
}

/// The update the last check found, which Install installs.
#[derive(Default)]
pub struct Found(Mutex<Option<Update>>);

/// An update the check found.
#[derive(Clone, Debug, Serialize)]
pub struct Available {
    pub version: String,
    pub current_version: String,
}

/// How much of the update has downloaded, in bytes, out of `total` when the server said.
#[derive(Clone, Debug, Serialize)]
pub struct Progress {
    pub downloaded: u64,
    pub total: Option<u64>,
}

/// Reads the manifests `manifest_urls` names, and returns the newest update they announce that is
/// newer than this app; Install then installs that one. It fails only when no manifest could be
/// read.
#[tauri::command(rename_all = "snake_case")]
pub async fn app_update_check(
    app: AppHandle,
    found: State<'_, Found>,
    nightlies: bool,
) -> Result<Option<Available>, String> {
    let urls = manifest_urls(&app.package_info().version.to_string(), nightlies);
    let mut updates = Vec::new();
    let mut failure = None;
    let mut read = false;
    for url in urls {
        match check_one(&app, url).await {
            Ok(update) => {
                read = true;
                updates.extend(update);
            }
            Err(err) => failure = Some(err),
        }
    }
    if let (false, Some(err)) = (read, failure) {
        return Err(err);
    }
    let update = newest(updates);
    let available = update.as_ref().map(|update| Available {
        version: update.version.clone(),
        current_version: update.current_version.clone(),
    });
    *found.0.lock().unwrap_or_else(|err| err.into_inner()) = update;
    Ok(available)
}

/// Downloads the update the last check found, checks its signature and installs it in place of
/// the app, sending `app-update-progress` as it downloads. The app runs the new version once it
/// restarts. `version` is the version the title bar shows: when a later check found a newer one,
/// nothing is installed and this returns the newer version, which the title bar then offers.
#[tauri::command(rename_all = "snake_case")]
pub async fn app_update_install(
    app: AppHandle,
    found: State<'_, Found>,
    version: String,
) -> Result<Option<String>, String> {
    let mut update = found
        .0
        .lock()
        .unwrap_or_else(|err| err.into_inner())
        .clone()
        .ok_or("no update was found to install")?;
    if update.version != version {
        return Ok(Some(update.version));
    }
    // The update does not take the check's timeout.
    update.timeout = Some(DOWNLOAD_TIMEOUT);
    let mut downloaded: u64 = 0;
    let mut sent_percent = None;
    update
        .download_and_install(
            |chunk, total| {
                downloaded += chunk as u64;
                // One event per percent, not one per chunk.
                let percent = total.map(|total| downloaded * 100 / total.max(1));
                if percent.is_none() || percent != sent_percent {
                    sent_percent = percent;
                    let _ = app.emit(PROGRESS_EVENT, Progress { downloaded, total });
                }
            },
            || {},
        )
        .await
        .map_err(|err| err.to_string())?;
    Ok(None)
}

/// Quits and opens the app again, so the installed update runs. The exit stops the serve child
/// first, as quitting does.
#[tauri::command(rename_all = "snake_case")]
pub fn app_restart(app: AppHandle) {
    app.request_restart();
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_manifests_follow_the_nightlies_preference() {
        assert!(manifest_urls("0.0.0", true).is_empty());
        assert!(manifest_urls("0.0.0", false).is_empty());
        assert_eq!(manifest_urls("0.3.0", false), vec![STABLE]);
        assert_eq!(manifest_urls("0.3.0", true), vec![STABLE, NIGHTLY]);
        assert_eq!(
            manifest_urls("0.3.0-nightly.20261001.42", false),
            vec![STABLE]
        );
        assert_eq!(
            manifest_urls("0.3.0-nightly.20261001.42", true),
            vec![STABLE, NIGHTLY]
        );
    }

    #[test]
    fn a_nightly_build_is_known_by_its_version() {
        assert!(is_nightly("0.3.0-nightly.20261001.42"));
        assert!(!is_nightly("0.3.0"));
        assert!(!is_nightly("0.0.0"));
    }
}
