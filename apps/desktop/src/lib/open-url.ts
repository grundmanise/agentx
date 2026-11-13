// Opening a web page in the user's browser, through Tauri's opener plugin: the page of a source's
// repository on the Sources screen. The capability lets the window open https URLs alone. Screens
// call this and never the plugin, and the tests' mocked CLI records every call.
import { openUrl } from "@tauri-apps/plugin-opener";

import { notify } from "#/components/toast.tsx";

/** Opens `url`, an https URL, in the default browser. */
export const openInBrowser = async (url: string) => {
  try {
    await openUrl(url);
  } catch {
    notify(`Couldn't open ${url}`);
  }
};
