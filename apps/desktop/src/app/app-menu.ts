// The app menu's own items on macOS (`src-tauri/src/app_menu.rs`): Settings… (⌘,) opens Settings,
// and Check for Updates… checks at once, as Check for updates in Settings does. A browser has no app menu.
import { listen } from "@tauri-apps/api/event";

import { checkAppUpdateNow } from "#/app/app-update.ts";
import { inApp } from "#/lib/platform.ts";

/** Listens for the app menu's items; `openSettings` opens Settings. Returns what stops it. */
export const listenAppMenu = (openSettings: () => void): (() => void) => {
  if (!inApp) {
    return () => {
      // A browser has no app menu.
    };
  }
  const stop = listen<string>("app-menu", ({ payload }) => {
    if (payload === "settings") {
      openSettings();
    } else if (payload === "check-updates") {
      void checkAppUpdateNow();
    }
  });
  return () => {
    void (async () => {
      (await stop)();
    })();
  };
};
