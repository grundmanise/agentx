// The editor a skill opens in (ADR 0013): the one way a screen reads or saves the Editor
// preference, lists the editors found on this machine, and opens a skill's file. The system's
// default application is the opener plugin's open, scoped to the library and agentx home; an
// editor the app found and a command line the user typed are the Rust side's `editor_open`, which
// runs no shell. The preference is the app's own, in its config directory, never a machine
// setting. Screens call this and never the plugin, and the tests' mocked CLI answers every call.
import { invoke } from "@tauri-apps/api/core";
import { openPath } from "@tauri-apps/plugin-opener";

import { notify } from "#/components/toast.tsx";
import { FILE_MANAGER, revealInFinder } from "#/lib/reveal.ts";

/** The editor a skill opens in, as the app's preferences keep it. */
export type EditorChoice =
  | { kind: "system" }
  /** An editor the app found on this machine, by its id; `name` is shown when it is gone. */
  | { kind: "app"; id: string; name: string }
  /** A command line; the file's path is added as its last argument. */
  | { kind: "custom"; command: string };

/** An editor the app found on this machine. */
export interface DetectedEditor {
  id: string;
  name: string;
}

interface Preferences {
  editor: EditorChoice;
}

const SYSTEM: EditorChoice = { kind: "system" };

/** The editor the user chose, or the system default when none was saved or the read failed. */
export const readEditor = async (): Promise<EditorChoice> => {
  try {
    const preferences = await invoke<Preferences>("preferences_read");
    return preferences.editor;
  } catch {
    return SYSTEM;
  }
};

/** Saves the editor the user chose. False when it could not be saved, which a toast says. */
export const saveEditor = async (editor: EditorChoice): Promise<boolean> => {
  try {
    await invoke("preferences_write", { preferences: { editor } });
    return true;
  } catch {
    notify("Couldn't save the editor");
    return false;
  }
};

/** The editors the app knows that are on this machine; none when they could not be listed. */
export const detectEditors = async (): Promise<DetectedEditor[]> => {
  try {
    return await invoke<DetectedEditor[]>("editor_detect");
  } catch {
    return [];
  }
};

const BLANKS = /\s+/u;

/** How a sentence names `editor`: the app's name, a command's program, or the default app. */
export const editorName = (editor: EditorChoice): string => {
  switch (editor.kind) {
    case "app": {
      return editor.name;
    }
    case "custom": {
      const [program = ""] = editor.command.trim().split(BLANKS);
      const name = program.split("/").at(-1) ?? "";
      return name === "" ? "your command" : name;
    }
    case "system": {
      return "the default app";
    }
    default: {
      return "the default app";
    }
  }
};

const fileName = (path: string) => path.split("/").at(-1) ?? path;

/**
 * Opens the file at `absolutePath` in the editor the user chose. When it cannot be opened, a
 * toast says so and offers to show the file in Finder instead.
 */
export const openInEditor = async (absolutePath: string) => {
  const editor = await readEditor();
  try {
    await (editor.kind === "system"
      ? openPath(absolutePath)
      : invoke("editor_open", { editor, path: absolutePath }));
  } catch {
    notify(`Couldn't open ${fileName(absolutePath)} in ${editorName(editor)}`, {
      label: `Show in ${FILE_MANAGER}`,
      onClick: () => {
        void revealInFinder(absolutePath);
      },
    });
  }
};
