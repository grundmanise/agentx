// The one call the app makes to the operating system beside running the CLI: showing a file
// where it is, selected in Finder on macOS and in the file manager on Linux, through Tauri's
// opener plugin (ADR 0001). A browser cannot reach the file manager, so there it copies the path
// and says why nothing opened. Screens call this and never the plugin, and say the file manager's
// name with what this module exports, never with a platform check of their own.
import { revealItemInDir } from "@tauri-apps/plugin-opener";

import { notify, notifyFailure } from "#/components/toast.tsx";
import { inApp, isMac } from "#/lib/platform.ts";

/** The file manager's name: "Finder" on macOS, "file manager" on Linux. */
export const FILE_MANAGER = isMac ? "Finder" : "file manager";

/**
 * A control that shows a file there: "Show in Finder" or "Show in file manager", and with `what`,
 * "Show SKILL.md in Finder".
 */
export const showInLabel = (what?: string) =>
  what === undefined
    ? `Show in ${FILE_MANAGER}`
    : `Show ${what} in ${FILE_MANAGER}`;

/**
 * In a browser, which reaches no file manager or editor: copies `absolutePath` and says `why`
 * nothing opened, or says only that when the clipboard refused.
 */
export const copyPathInstead = async (absolutePath: string, why: string) => {
  try {
    await navigator.clipboard.writeText(absolutePath);
    notify(`Copied the path. ${why}`);
  } catch {
    notify(why);
  }
};

const revealInBrowser = async (absolutePath: string) => {
  await copyPathInstead(
    absolutePath,
    `A browser can't show files in ${isMac ? "Finder" : "the file manager"}`
  );
};

/**
 * Shows the file or directory at `absolutePath` in Finder, or says nothing and false when it is
 * not there. In a browser, copies the path instead, and is true.
 */
export const revealIfThere = async (absolutePath: string) => {
  if (!inApp) {
    await revealInBrowser(absolutePath);
    return true;
  }
  try {
    await revealItemInDir(absolutePath);
    return true;
  } catch {
    return false;
  }
};

/**
 * Shows the file or directory at `absolutePath` selected in Finder, or the file manager. In a
 * browser, copies the path instead.
 */
export const revealInFinder = async (absolutePath: string) => {
  if (!inApp) {
    await revealInBrowser(absolutePath);
    return;
  }
  try {
    await revealItemInDir(absolutePath);
  } catch {
    notifyFailure(`"${absolutePath}" doesn't exist`);
  }
};
