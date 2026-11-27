// The Rust side hosts the window and supervises the CLI: it finds the CLI, reads the login shell's
// environment and keeps the serve child open, relaying the JSON-RPC messages the app exchanges
// with it over its stdin and stdout; every command the app runs is a `run` request to serve. It
// has no file, git or network code of its own beyond writing serve's log, reading its end back,
// the app's preferences, the updater plugin, which checks for and installs the app's own
// updates (ADR 0019), and the PostHog SDK, which reports a panic of its own while the app's
// usage data setting is on (panic_reports.rs). It starts no program but the login shell, agentx
// and, when the user asks, the editor of their choice (ADR 0001, ADR 0013). Its calls to the
// operating system beside that are the opener plugin, which shows a path selected in Finder (the
// file manager elsewhere) and opens a skill's file in the system's default application when the
// user asks, and the dialog plugin's open dialog, in which the user picks a snapshot to apply: it
// returns the path, and the app passes it to the CLI without reading the file.

#[cfg(target_os = "macos")]
mod app_menu;
mod app_update;
mod editor;
mod logfile;
mod panic_reports;
mod preferences;
mod shell;
pub mod supervisor;

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;

use tauri::{Emitter, Manager, RunEvent, State};

use supervisor::{Config, Resolution, Supervisor};

type Sup<'a> = State<'a, Arc<Supervisor>>;

/// Where the CLI is, and how it was found.
#[tauri::command(rename_all = "snake_case")]
async fn cli_resolve(sup: Sup<'_>) -> Result<Resolution, String> {
    Ok(sup.resolution().await)
}

/// Starts the serve child unless one is running, and returns its child id; it is safe to call
/// before each request. Its stdout and stderr arrive as `serve-event` events and its end as
/// `serve-exit`, each with `child_id`.
#[tauri::command(rename_all = "snake_case")]
async fn serve_start(sup: Sup<'_>) -> Result<u64, String> {
    sup.inner().serve_start().await
}

/// Writes one JSON-RPC message, a request or a notification, to the stdin of serve child
/// `child_id` as one line, and refuses when that child is no longer the one running. Its answers
/// arrive as `serve-event` events.
#[tauri::command(rename_all = "snake_case")]
async fn serve_request(sup: Sup<'_>, child_id: u64, line: String) -> Result<(), String> {
    sup.serve_request(child_id, &line).await
}

/// The path of the serve log file, which holds everything serve wrote to stderr.
#[tauri::command(rename_all = "snake_case")]
async fn serve_log_path(sup: Sup<'_>) -> Result<PathBuf, String> {
    Ok(sup.serve_log_path())
}

/// Whether serve logs at debug level. A change restarts the serve child.
#[tauri::command(rename_all = "snake_case")]
async fn serve_verbose(sup: Sup<'_>, on: bool) -> Result<(), String> {
    sup.serve_verbose(on).await;
    Ok(())
}

/// The app's preferences file, in the app's config directory.
fn preferences_file(app: &tauri::AppHandle) -> Result<PathBuf, String> {
    app.path()
        .app_config_dir()
        .map(|dir| dir.join(preferences::FILE_NAME))
        .map_err(|err| err.to_string())
}

/// The app's preferences, or their defaults when none were saved.
#[tauri::command(rename_all = "snake_case")]
fn preferences_read(app: tauri::AppHandle) -> Result<preferences::Preferences, String> {
    Ok(preferences::read(&preferences_file(&app)?))
}

/// Saves the app's preferences, replacing what was saved. A custom command saved here is a
/// program the app will run, with a skill's file as its last argument, when `editor_open` is
/// called.
#[tauri::command(rename_all = "snake_case")]
fn preferences_write(
    app: tauri::AppHandle,
    preferences: preferences::Preferences,
) -> Result<(), String> {
    preferences::write(&preferences_file(&app)?, &preferences).map_err(|err| err.to_string())
}

/// The editors the app knows that are on this machine, in the order Settings lists them.
#[tauri::command(rename_all = "snake_case")]
async fn editor_detect(sup: Sup<'_>) -> Result<Vec<editor::Detected>, String> {
    Ok(detected(&sup.login_env().await))
}

fn detected(env: &HashMap<String, String>) -> Vec<editor::Detected> {
    let platform = editor::Platform::current();
    let home = PathBuf::from(env.get("HOME").cloned().unwrap_or_default());
    editor::detect(
        platform,
        &home,
        env.get("PATH").map(String::as_str),
        |candidate| editor::is_launcher(platform, candidate),
    )
}

/// Opens the file at `path` with the editor saved in the app's preferences, an editor the app
/// found or a command line; the system's default application is the opener plugin's. The call
/// names only the file, which must be inside the library or agentx home, as the CLI resolves them
/// from the login shell's environment. The program comes from the saved preference, which the
/// webview writes through `preferences_write` when the user picks an editor or types a command, so
/// the editor command is as trusted as the webview itself.
#[tauri::command(rename_all = "snake_case")]
async fn editor_open(app: tauri::AppHandle, sup: Sup<'_>, path: String) -> Result<(), String> {
    let env = sup.login_env().await;
    let file = PathBuf::from(&path);
    if !file.is_file() || !editor::in_scope(&file, &editor::scope_roots(&env)) {
        return Err(format!(
            "{path} is not a file in the library or agentx home"
        ));
    }
    let editor = preferences::read(&preferences_file(&app)?).editor;
    let words = editor::command_for(editor::Platform::current(), &editor, &detected(&env), &file)?;
    editor::launch(&words, &env).await
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
    let builder = tauri::Builder::default();
    #[cfg(target_os = "macos")]
    let builder = builder
        .menu(app_menu::build)
        .on_menu_event(app_menu::on_event);
    let app = builder
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .manage(app_update::Found::default())
        .manage(app_update::Relaunch::default())
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
            serve_start,
            serve_request,
            serve_log_path,
            serve_verbose,
            preferences_read,
            preferences_write,
            editor_detect,
            editor_open,
            app_update::app_update_check,
            app_update::app_update_install,
            app_update::app_restart,
            panic_reports::set_telemetry
        ])
        .build(tauri::generate_context!())
        .expect("error while building the agentx desktop app");
    app.run(|app, event| {
        if let RunEvent::Exit = event {
            let sup = app.state::<Arc<Supervisor>>().inner().clone();
            tauri::async_runtime::block_on(sup.shutdown());
            app_update::relaunch(app);
        }
    });
}
