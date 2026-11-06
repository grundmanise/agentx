// The one call the app makes to the operating system beside running the CLI: showing a file
// where it is, selected in Finder on macOS and in the file manager elsewhere, through Tauri's
// opener plugin (ADR 0001). Screens call this and never the plugin.
import { revealItemInDir } from "@tauri-apps/plugin-opener";

import { notify } from "#/components/toast.tsx";

/** Shows the file or directory at `absolutePath` selected in Finder. */
export const revealInFinder = async (absolutePath: string) => {
  try {
    await revealItemInDir(absolutePath);
  } catch {
    notify(`Couldn't show ${absolutePath} in Finder`);
  }
};
