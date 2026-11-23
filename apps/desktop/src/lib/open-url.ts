// Opening a web page in the user's browser: the page of a source's repository on the Sources
// screen, or the origin of a fork from its History. In the app, through Tauri's opener plugin,
// whose capability lets the window open https URLs alone; in a browser, in a new tab. Screens call
// this and never the plugin, and the tests' mocked CLI records every call.
import { openUrl } from "@tauri-apps/plugin-opener";

import { notifyFailure } from "#/components/toast.tsx";
import { inApp } from "#/lib/platform.ts";

/** Opens `url`, an https URL, in the default browser, or in a new tab when the app runs in one. */
export const openInBrowser = async (url: string) => {
  if (!inApp) {
    window.open(url, "_blank", "noopener,noreferrer");
    return;
  }
  try {
    await openUrl(url);
  } catch {
    notifyFailure(`Couldn't open ${url}`);
  }
};
