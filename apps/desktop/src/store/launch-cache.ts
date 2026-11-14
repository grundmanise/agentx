// The kept inventory: the last inventory the app applied, with the slices the store loads at
// start, kept for the next launch. A launch that finds it fills the store from it before the first
// render, so the screens show at once, while the checks run and the serve child scans; what they
// report then replaces it. The browser's localStorage holds it, as one item: the read is
// synchronous, and the app writes no file. What another build of the app kept is dropped, since
// its events may have another shape. It is a cache: nothing reads it but a launch, and the app
// works the same without it.
import type { InventoryEvent } from "#/cli/events.gen.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState } from "#/store/store.ts";

const KEY = "agentx.launch";
/** How long the store is quiet before it is written: a launch changes every slice within moments. */
const WRITE_AFTER_MS = 500;

/** What is kept: the inventory, and what the store loads once the checks pass. */
const SLICES = [
  "adoption",
  "inventory",
  "machine",
  "settings",
  "sources",
] as const satisfies readonly (keyof AppState)[];

type Slices = Pick<AppState, (typeof SLICES)[number]>;

interface Kept extends Slices {
  build: string;
  inventory: InventoryEvent;
}

const slicesOf = (s: AppState): Slices => ({
  adoption: s.adoption,
  inventory: s.inventory,
  machine: s.machine,
  settings: s.settings,
  sources: s.sources,
});

/** Whether this build wrote `item`: it then holds this build's own store. */
const isKept = (item: unknown): item is Kept =>
  typeof item === "object" &&
  item !== null &&
  "build" in item &&
  item.build === import.meta.env.APP_BUILD &&
  "inventory" in item;

const read = (): Kept | undefined => {
  try {
    const item = localStorage.getItem(KEY);
    const kept: unknown = item === null ? undefined : JSON.parse(item);
    if (isKept(kept)) {
      return kept;
    }
  } catch {
    // No storage, or an item that is not JSON: a launch with nothing to show.
  }
  return undefined;
};

/**
 * Fills the store from the kept inventory, and says whether there was one. Called once, before
 * the first render.
 */
export const restoreLaunch = (): boolean => {
  const kept = read();
  if (kept === undefined) {
    return false;
  }
  const { build: _, ...slices } = kept;
  useAppStore.setState(slices);
  return true;
};

const write = () => {
  const { inventory, ...slices } = slicesOf(useAppStore.getState());
  if (inventory === undefined) {
    return;
  }
  const kept: Kept = {
    ...slices,
    build: import.meta.env.APP_BUILD,
    inventory,
  };
  try {
    localStorage.setItem(KEY, JSON.stringify(kept));
  } catch {
    // No storage, or no room in it: the next launch shows nothing rather than an older machine.
    try {
      localStorage.removeItem(KEY);
    } catch {
      // No storage.
    }
  }
};

let keeping = false;

/**
 * Keeps the inventory for the next launch, from now on: written once the store has been quiet
 * for a moment after the inventory or a slice changed. Called once the checks pass, so that only
 * what this run of the app read from the CLI is written.
 */
export const keepLaunch = () => {
  if (keeping) {
    return;
  }
  keeping = true;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const schedule = () => {
    clearTimeout(timer);
    timer = setTimeout(write, WRITE_AFTER_MS);
  };
  useAppStore.subscribe((s, previous) => {
    if (SLICES.some((slice) => s[slice] !== previous[slice])) {
      schedule();
    }
  });
  schedule();
};
