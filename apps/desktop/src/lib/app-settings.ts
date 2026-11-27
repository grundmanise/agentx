// The app's own settings (ADR 0013, ADR 0019): its preferences, one file the Rust side reads and
// writes whole (the editor a skill opens in, whether it opens a skill the app just created,
// whether the app checks for its own updates and whether it takes nightly builds), with the app's
// version and the editors found on this machine. The Rust side's `app_settings` answers all of
// them at once, and that answer is kept here as one value: Settings reads it each time it shows,
// and its Editor and Updates cards render from it. A save changes the value at once and then
// writes the file; one that fails puts the preference back. The editor (`editor.ts`) and app
// updates (`app-update.ts`) call this and never the commands.
import { invoke } from "@tauri-apps/api/core";
import { create } from "zustand";

import type { DetectedEditor, EditorChoice } from "#/lib/editor.ts";

export interface Preferences {
  editor: EditorChoice;
  /** Whether the editor opens a skill once the app creates it. */
  open_after_create: boolean;
  /** Whether the app checks for its own updates after it starts and once a day. */
  check_updates: boolean;
  /** Whether the app takes nightly builds too; unset, a nightly build does and a release not. */
  include_nightlies?: boolean;
}

/** What `app_settings` answers. */
export interface AppSettings {
  preferences: Preferences;
  /** The app's version. */
  version: string;
  /** Whether the app takes nightly builds while the preferences do not say: a nightly build does. */
  nightlies_default: boolean;
  /** The editors the app knows that are on this machine, in the order Settings lists them. */
  editors: DetectedEditor[];
}

const DEFAULTS: Preferences = {
  check_updates: true,
  editor: { kind: "system" },
  open_after_create: false,
};

/** The settings when they cannot be read, as in a browser: the defaults, of a build that is no release. */
const UNREAD: AppSettings = {
  editors: [],
  nightlies_default: false,
  preferences: DEFAULTS,
  version: "0.0.0",
};

const useStore = create<{ settings?: AppSettings }>()(() => ({}));

/** A part of the app's settings, for a component; undefined until they are read. */
export const useAppSettings = <T>(
  select: (settings: AppSettings) => T
): T | undefined =>
  useStore((s) => (s.settings === undefined ? undefined : select(s.settings)));

// The preferences as the file holds them: what the last read found, or the last save wrote.
let saved = DEFAULTS;
// The write going on, which the next one and every read wait for, so two saves at once never
// write over each other's change and a read never misses a change being saved.
let writing: Promise<void> = Promise.resolve();
// How many saves started, so a read that ran while one did keeps the preferences the store holds.
let saves = 0;

const settled = async (previous: Promise<void>) => {
  try {
    await previous;
  } catch {
    // A failed save leaves the next one to run; its caller heard of the failure.
  }
};

const read = async (): Promise<AppSettings> => {
  const before = saves;
  await settled(writing);
  let answer = UNREAD;
  try {
    const { preferences, ...rest } = await invoke<
      Omit<AppSettings, "preferences"> & { preferences: Partial<Preferences> }
    >("app_settings");
    answer = { ...rest, preferences: { ...DEFAULTS, ...preferences } };
  } catch {
    // No Rust side to ask, as in a browser.
  }
  const kept = useStore.getState().settings;
  const settings =
    saves === before || kept === undefined
      ? answer
      : { ...answer, preferences: kept.preferences };
  if (saves === before) {
    saved = answer.preferences;
  }
  useStore.setState({ settings });
  return settings;
};

// The read going on, which a call meanwhile joins.
let reading: Promise<AppSettings> | undefined;

/**
 * Reads the app's settings into the store: one call to the Rust side, one update. Settings calls
 * it each time it shows, so an editor installed since is listed.
 */
export const readAppSettings = async (): Promise<AppSettings> => {
  reading ??= (async () => {
    try {
      return await read();
    } finally {
      reading = undefined;
    }
  })();
  return await reading;
};

/** The app's settings as the store holds them, read first when nothing read them yet. */
export const appSettings = async (): Promise<AppSettings> =>
  useStore.getState().settings ?? (await readAppSettings());

const write = async <K extends keyof Preferences>(
  previous: Promise<void>,
  key: K,
  value: Preferences[K]
) => {
  await settled(previous);
  const preferences = { ...saved, [key]: value };
  await invoke("preferences_write", { preferences });
  saved = preferences;
};

/**
 * Saves one preference: the store holds it at once, and the file once it is written. Throws when
 * it could not be saved, with the store back at what the file holds.
 */
export const savePreference = async <K extends keyof Preferences>(
  key: K,
  value: Preferences[K]
) => {
  const first = await appSettings();
  const settings = useStore.getState().settings ?? first;
  saves += 1;
  useStore.setState({
    settings: {
      ...settings,
      preferences: { ...settings.preferences, [key]: value },
    },
  });
  const current = write(writing, key, value);
  writing = current;
  try {
    await current;
  } catch (error) {
    const now = useStore.getState().settings;
    // A later save of the same preference keeps its own value.
    if (now !== undefined && now.preferences[key] === value) {
      useStore.setState({
        settings: {
          ...now,
          preferences: { ...now.preferences, [key]: saved[key] },
        },
      });
    }
    throw error;
  }
};
