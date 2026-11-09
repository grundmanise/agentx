// The one native dialog the app shows: the open dialog in which the user picks an exported file
// (a snapshot, in the CLI's words) to import, through Tauri's dialog plugin. It returns the path
// the user picked, and the app passes that path to the CLI; the app reads and writes no file
// itself (ADR 0001). Screens call this and never the plugin, and the tests' mocked CLI answers it
// with the path the test picks.
import { open } from "@tauri-apps/plugin-dialog";

import { notify } from "#/components/toast.tsx";

/**
 * Shows the open dialog for a snapshot, starting in `folder` when one is given, and returns the
 * path the user picked, or null when they cancelled.
 */
export const pickSnapshot = async (folder?: string): Promise<string | null> => {
  try {
    const picked = await open({
      defaultPath: folder,
      directory: false,
      filters: [{ extensions: ["json"], name: "agentx export" }],
      multiple: false,
      title: "Import settings",
    });
    return typeof picked === "string" ? picked : null;
  } catch {
    notify("Couldn't open the file dialog");
    return null;
  }
};
