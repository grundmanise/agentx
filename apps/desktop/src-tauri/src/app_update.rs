// The app's own updates (ADR 0019): Tauri's updater plugin reads the update manifest of the app's
// channel, and on Install downloads the update, checks its signature against the public key in
// `tauri.conf.json` and replaces the app, the CLI inside it included. The new version runs after
// a restart, which the user starts. A nightly follows the nightlies and a release the stable
// releases, from the app's own version; a build that is no release, version 0.0.0, has no
// channel and finds nothing. This is the one place the Rust side reaches the network.

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

/// The update manifest of the channel `version` belongs to, or none for a build that is no
/// release.
pub fn manifest_url(version: &str) -> Option<&'static str> {
    if version == "0.0.0" {
        None
    } else if version.contains("-nightly.") {
        Some(NIGHTLY)
    } else {
        Some(STABLE)
    }
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

/// Reads the channel's manifest, and returns the update it announces when it is newer than this
/// app; Install then installs that one.
#[tauri::command(rename_all = "snake_case")]
pub async fn app_update_check(
    app: AppHandle,
    found: State<'_, Found>,
) -> Result<Option<Available>, String> {
    let Some(url) = manifest_url(&app.package_info().version.to_string()) else {
        return Ok(None);
    };
    let update = app
        .updater_builder()
        .endpoints(vec![url.parse().map_err(|err| format!("{err}"))?])
        .map(|builder| builder.timeout(CHECK_TIMEOUT))
        .and_then(|builder| builder.build())
        .map_err(|err| err.to_string())?
        .check()
        .await
        .map_err(|err| err.to_string())?;
    let available = update.as_ref().map(|update| Available {
        version: update.version.clone(),
        current_version: update.current_version.clone(),
    });
    *found.0.lock().unwrap_or_else(|err| err.into_inner()) = update;
    Ok(available)
}

/// Downloads the update the last check found, checks its signature and installs it in place of
/// the app, sending `app-update-progress` as it downloads. The app runs the new version once it
/// restarts. `version` is the version the title bar shows: when a later check found another one,
/// nothing is installed.
#[tauri::command(rename_all = "snake_case")]
pub async fn app_update_install(
    app: AppHandle,
    found: State<'_, Found>,
    version: String,
) -> Result<(), String> {
    let mut update = found
        .0
        .lock()
        .unwrap_or_else(|err| err.into_inner())
        .clone()
        .ok_or("no update was found to install")?;
    if update.version != version {
        return Err(format!(
            "the update found is {}, not {version}",
            update.version
        ));
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
        .map_err(|err| err.to_string())
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
    fn the_channel_follows_the_version() {
        assert_eq!(manifest_url("0.0.0"), None);
        assert_eq!(manifest_url("0.3.0"), Some(STABLE));
        assert_eq!(manifest_url("1.2.3"), Some(STABLE));
        assert_eq!(manifest_url("0.3.0-nightly.20261001.42"), Some(NIGHTLY));
    }
}
