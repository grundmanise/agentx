// The app's store of what the CLI reports. Only CLI events change it: the serve child's
// (src/store/feed.ts) and those of one-shot commands (src/store/runner.ts and
// src/store/loaders.ts). Every screen renders from it, and the app never predicts a result: a
// control shows its command as pending, and the store changes only when the CLI answers.
import { create } from "zustand";

import type {
  AdoptionEvent,
  InventoryEvent,
  ReconcileEvent,
  Settings,
  SourceEvent,
  UpdateAvailableEvent,
} from "#/cli/events.gen.ts";

/** Why a command or the serve child failed, as the CLI said it. */
export interface Problem {
  message: string;
  hint?: string;
}

/**
 * Whether the inventory shown is the machine as it is now: "stale" while a restarted serve child
 * has not sent its first inventory yet, "failed" when the last refresh failed.
 */
export type Freshness =
  | { kind: "fresh" }
  | { kind: "stale" }
  | { kind: "failed"; error: string };

/** Which serve child and instance the inventory shown came from, and its scan counter. */
export interface Applied {
  childId: number;
  instanceId: string;
  counter: number;
}

/** How a serve child ended. */
export interface ServeEnd {
  childId: number;
  exitCode: number | null;
  problem: Problem;
  /** Whether the supervisor starts a new child. */
  restarting: boolean;
}

export type Tone = "success" | "attention" | "danger";

/** One line of the activity list: a command's outcome or something serve noticed. */
export interface ActivityEntry {
  id: number;
  /** When it happened, as Date.now(). */
  at: number;
  tone: Tone;
  message: string;
}

/** How many activity entries the store keeps; older ones drop off. */
export const ACTIVITY_LIMIT = 200;

export interface AppState {
  /** The last inventory applied, from the active serve child only. */
  inventory: InventoryEvent | undefined;
  applied: Applied | undefined;
  freshness: Freshness;
  /** Refresh requests sent and not acknowledged yet: request id → the serve child it went to. */
  refreshes: Record<string, number>;
  /** How the last serve child that ended did so. */
  serveEnd: ServeEnd | undefined;
  /** The reconcile events of the active serve child's start. */
  reconciles: ReconcileEvent[];
  /** The update candidates, by skill name, from the last update check. */
  updates: Record<string, UpdateAvailableEvent>;
  settings: Settings | undefined;
  sources: SourceEvent[];
  /** What `adopt` would take over, from its preview. */
  adoption: AdoptionEvent[];
  /** The controls whose command runs: label → run id. */
  pending: Record<string, string>;
  /** Newest first, at most ACTIVITY_LIMIT entries. */
  activity: ActivityEntry[];
}

export const useAppStore = create<AppState>()(() => ({
  activity: [],
  adoption: [],
  applied: undefined,
  freshness: { kind: "stale" },
  inventory: undefined,
  pending: {},
  reconciles: [],
  refreshes: {},
  serveEnd: undefined,
  settings: undefined,
  sources: [],
  updates: {},
}));

let nextActivity = 1;

/** Adds an entry at the top of the activity list. */
export const addActivity = (tone: Tone, message: string) => {
  const entry = { at: Date.now(), id: nextActivity, message, tone };
  nextActivity += 1;
  useAppStore.setState((s) => ({
    activity: [entry, ...s.activity].slice(0, ACTIVITY_LIMIT),
  }));
};
