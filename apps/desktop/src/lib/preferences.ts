// The app's preferences (ADR 0013), one file the Rust side reads and writes whole: the editor a
// skill opens in, and whether the app checks for its own updates (ADR 0019). A save reads the
// file first and changes only the preference it names, so saving one never resets another. The
// editor (`editor.ts`) and app updates (`app-update.ts`) call this and never the commands.
import { invoke } from "@tauri-apps/api/core";

import type { EditorChoice } from "#/lib/editor.ts";

export interface Preferences {
  editor: EditorChoice;
  /** Whether the app checks for its own updates after it starts and once a day. */
  check_updates: boolean;
}

const DEFAULTS: Preferences = {
  check_updates: true,
  editor: { kind: "system" },
};

/** The preferences as the file holds them, each one missing or unreadable as its default. */
const readSaved = async (): Promise<Preferences> => {
  try {
    const saved = await invoke<Partial<Preferences>>("preferences_read");
    return { ...DEFAULTS, ...saved };
  } catch {
    return DEFAULTS;
  }
};

// The save going on, which the next one and every read wait for, so two saves at once never
// write over each other's change and a read never misses a change being saved.
let saving: Promise<void> = Promise.resolve();

const settled = async (previous: Promise<void>) => {
  try {
    await previous;
  } catch {
    // A failed save leaves the next one to run; its caller heard of the failure.
  }
};

/** The preferences, once the saves going on are done; each missing or unreadable as its default. */
export const readPreferences = async (): Promise<Preferences> => {
  await settled(saving);
  return await readSaved();
};

const save = async (previous: Promise<void>, change: Partial<Preferences>) => {
  await settled(previous);
  const preferences = { ...(await readSaved()), ...change };
  await invoke("preferences_write", { preferences });
};

/** Saves `change` over the preferences as saved. Throws when they could not be saved. */
export const savePreferences = async (change: Partial<Preferences>) => {
  const current = save(saving, change);
  saving = current;
  await current;
};
