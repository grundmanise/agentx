// The Rust side only hosts the window for now. The CLI supervisor (finding the CLI, the login
// shell environment, one-shot commands and the serve child) lands here next; it is the only
// thing this process will ever do besides showing the window.
#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .run(tauri::generate_context!())
        .expect("error while running the agentx desktop app");
}
