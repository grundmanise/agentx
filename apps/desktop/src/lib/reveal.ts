// The one call the app makes to the operating system beside running the CLI: showing a file
// where it is, selected in Finder on macOS and in the file manager on Linux, through Tauri's
// opener plugin (ADR 0001). Screens call this and never the plugin, and say the file manager's
// name with what this module exports, never with a platform check of their own.
import { revealItemInDir } from "@tauri-apps/plugin-opener";

import { notify } from "#/components/toast.tsx";
import { isMac } from "#/lib/platform.ts";

/** The file manager's name: "Finder" on macOS, "file manager" on Linux. */
export const FILE_MANAGER = isMac ? "Finder" : "file manager";

/** The file manager in a sentence: "in Finder", "in the file manager". */
export const IN_FILE_MANAGER = isMac ? "in Finder" : "in the file manager";

/**
 * A control that shows a file there: "Show in Finder" or "Show in file manager", and with `what`,
 * "Show SKILL.md in Finder".
 */
export const showInLabel = (what?: string) =>
  what === undefined
    ? `Show in ${FILE_MANAGER}`
    : `Show ${what} in ${FILE_MANAGER}`;

/** Shows the file or directory at `absolutePath` selected in Finder, or the file manager. */
export const revealInFinder = async (absolutePath: string) => {
  try {
    await revealItemInDir(absolutePath);
  } catch {
    notify(`Couldn't show ${absolutePath} ${IN_FILE_MANAGER}`);
  }
};
