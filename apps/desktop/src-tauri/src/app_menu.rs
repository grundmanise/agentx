// The app menu on macOS: the system's default menu, with Settings… (⌘,) and Check for Updates…
// under About, as Mac apps place them. A click sends `app-menu` with the item's id, and the app
// opens Settings or checks for an update (`src/app/app-menu.ts`). Elsewhere the window has no
// menu bar, and ⌘, / Ctrl+, opens Settings from the app itself.

use tauri::menu::{Menu, MenuEvent, MenuItem, MenuItemKind, PredefinedMenuItem};
use tauri::{AppHandle, Emitter, Runtime};

/// The event a menu click sends, with the item's id as its payload.
pub const EVENT: &str = "app-menu";

const SETTINGS: &str = "settings";
const CHECK_UPDATES: &str = "check-updates";

/// The default menu, with Settings… and Check for Updates… after About and its separator.
pub fn build<R: Runtime>(app: &AppHandle<R>) -> tauri::Result<Menu<R>> {
    let menu = Menu::default(app)?;
    if let Some(MenuItemKind::Submenu(app_menu)) = menu.items()?.into_iter().next() {
        app_menu.insert_items(
            &[
                &MenuItem::with_id(app, SETTINGS, "Settings…", true, Some("CmdOrCtrl+,"))?,
                &MenuItem::with_id(app, CHECK_UPDATES, "Check for Updates…", true, None::<&str>)?,
                &PredefinedMenuItem::separator(app)?,
            ],
            2,
        )?;
    }
    Ok(menu)
}

/// Sends a click on one of the app's own items to the app.
pub fn on_event<R: Runtime>(app: &AppHandle<R>, event: MenuEvent) {
    let id = event.id().as_ref();
    if id == SETTINGS || id == CHECK_UPDATES {
        let _ = app.emit(EVENT, id);
    }
}
