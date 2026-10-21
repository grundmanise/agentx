// The Rust side hosts the window and supervises the CLI: it finds the CLI, reads the login shell's
// environment, runs one-shot commands and keeps the serve child open. It has no file, git or
// network code of its own beyond writing serve's log, and starts no program but the login shell
// and agentx (ADR 0001).

mod logfile;
mod shell;
pub mod supervisor;

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use tauri::{Emitter, Manager, RunEvent, State};

use supervisor::{Config, Resolution, RunEnd, Supervisor};

type Sup<'a> = State<'a, Arc<Supervisor>>;

/// Where the CLI is, with the app's version and whether this is a debug build.
#[tauri::command(rename_all = "snake_case")]
async fn cli_resolve(sup: Sup<'_>) -> Result<Resolution, String> {
    Ok(sup.resolution().await)
}

/// Runs `agentx <args> --json`. Its lines arrive as `cli-run` events and its end as `cli-run-end`,
/// each with `run_id`; the command resolves with the end too.
#[tauri::command(rename_all = "snake_case")]
async fn cli_run(
    sup: Sup<'_>,
    args: Vec<String>,
    deadline_ms: Option<u64>,
    run_id: String,
) -> Result<RunEnd, String> {
    sup.run(run_id, args, deadline_ms.map(Duration::from_millis))
        .await
}

/// Stops a one-shot run. False when no such run is going.
#[tauri::command(rename_all = "snake_case")]
fn cli_cancel(sup: Sup<'_>, run_id: String) -> bool {
    sup.cancel(&run_id)
}

/// Starts the serve child unless one is running, and returns its child id. Its stdout arrives as
/// `serve-event` events and its end as `serve-exit`, each with `child_id`.
#[tauri::command(rename_all = "snake_case")]
async fn serve_start(sup: Sup<'_>) -> Result<u64, String> {
    sup.inner().serve_start().await
}

/// Writes one JSON request to the serve child's stdin.
#[tauri::command(rename_all = "snake_case")]
async fn serve_request(sup: Sup<'_>, line: String) -> Result<(), String> {
    sup.serve_request(&line).await
}

/// Where serve's stderr is written: `~/Library/Logs/agentx` on macOS, `$XDG_STATE_HOME/agentx/logs`
/// elsewhere.
fn log_dir(env: &HashMap<String, String>) -> PathBuf {
    let home = PathBuf::from(env.get("HOME").cloned().unwrap_or_default());
    if cfg!(target_os = "macos") {
        return home.join("Library/Logs/agentx");
    }
    env.get("XDG_STATE_HOME")
        .filter(|dir| !dir.is_empty())
        .map_or_else(|| home.join(".local/state"), PathBuf::from)
        .join("agentx/logs")
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app = tauri::Builder::default()
        .setup(|app| {
            let base_env: HashMap<String, String> = std::env::vars_os()
                .map(|(k, v)| {
                    (
                        k.to_string_lossy().into_owned(),
                        v.to_string_lossy().into_owned(),
                    )
                })
                .collect();
            let handle = app.handle().clone();
            let sup = Supervisor::new(
                Config {
                    exe_dir: std::env::current_exe()
                        .ok()
                        .and_then(|exe| exe.parent().map(PathBuf::from)),
                    log_dir: log_dir(&base_env),
                    base_env,
                    app_version: app.package_info().version.to_string(),
                    debug: cfg!(debug_assertions),
                },
                Arc::new(move |name, payload| {
                    let _ = handle.emit(name, payload);
                }),
            );
            app.manage(sup);
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            cli_resolve,
            cli_run,
            cli_cancel,
            serve_start,
            serve_request
        ])
        .build(tauri::generate_context!())
        .expect("error while building the agentx desktop app");
    app.run(|app, event| {
        if let RunEvent::Exit = event {
            let sup = app.state::<Arc<Supervisor>>().inner().clone();
            tauri::async_runtime::block_on(sup.shutdown());
        }
    });
}
