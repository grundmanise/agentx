// The app's store of what the CLI reports. Only CLI events change it: the serve child's
// (src/store/feed.ts) and those of one-shot commands (src/store/runner.ts and
// src/store/loaders.ts). Every screen renders from it, and the app never predicts a result: a
// control shows its command as pending, and the store changes only when the CLI answers.
import { create } from "zustand";

import type {
  AdoptionEvent,
  ConflictEvent,
  DiffEvent,
  HandshakeError,
  HistoryEvent,
  InstallableSkillEvent,
  InventoryEvent,
  MachineEvent,
  ReconcileEvent,
  Settings,
  SkillFileEvent,
  SourceEvent,
  SourceSkillEvent,
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
  /** This machine and where agentx keeps its files, from `machine`. */
  machine: MachineEvent | undefined;
  sources: SourceEvent[];
  /** What `adopt` would take over, from its preview. */
  adoption: AdoptionEvent[];
  /** The controls whose command runs: label → run id. */
  pending: Record<string, string>;
  /** Newest first, at most ACTIVITY_LIMIT entries. */
  activity: ActivityEntry[];
  /** The history of each of your own skills read so far, by name. */
  histories: Record<string, HistoryRead>;
  /** The skills of each source listed so far, by source id, from `source skills <id>`. */
  listings: Record<string, SourceSkillEvent[]>;
  /**
   * Your skills on the account remote that this machine has not installed, from
   * `skill list --remote`; undefined until read.
   */
  installable: InstallableSkillEvent[] | undefined;
  /** The files of each skill read so far, by the row's key. */
  files: Record<string, FilesRead>;
  /**
   * The declarations whose handshake failed in the last Check tools of this session, from that
   * one-shot's inventory, which is not applied. Replaced by the next check; gone on a restart.
   */
  handshakeFailures: HandshakeFailure[];
  /**
   * What each update changes, read so far, by skill name, from `skill diff <name> --update`:
   * the What's new sheet reads it when it opens.
   */
  updateDiffs: Record<string, UpdateDiffRead>;
  /**
   * Each comparison of a commit of one of your own skills with the skill as it is now, from
   * `skill diff <name> --commit <commit>`, by comparisonKey.
   */
  comparisons: Record<string, ComparisonRead>;
  /**
   * The last `conflict` event of each skill, by name, from the commands the runner ran this
   * session: which files a pending merge left to resolve. Gone on a restart, when a skill with a
   * pending merge has none.
   */
  conflicts: Record<string, ConflictEvent>;
}

/** One declaration of a server whose handshake failed: the server, where it is declared, and why. */
export interface HandshakeFailure {
  logicalId: string;
  configuration: string;
  plugin?: string;
  error: HandshakeError;
}

/**
 * One read of `skill history <name>`, kept with the version of the skill it was read for, so
 * that a change to the skill never shows the entries of the version before it.
 */
export type HistoryRead =
  | { version: string; entries: HistoryEvent[]; failed?: undefined }
  | { version: string; entries?: undefined; failed: true };

/**
 * One read of `skill files` for a skill, kept with the version of the skill it was read for, as a
 * HistoryRead is. For a modified skill it also holds the `diff` events of `skill diff <name>`,
 * the files edited since the version the skill is compared with; `diffFailed` when that read
 * failed while the files were read.
 */
export type FilesRead =
  | {
      version: string;
      files: SkillFileEvent[];
      diffs?: DiffEvent[];
      diffFailed?: boolean;
      failed?: undefined;
    }
  | { version: string; files?: undefined; failed: true };

/**
 * One read of `skill diff <name> --update`, kept with the update candidate it was read for, so
 * that a newer update never shows the changes of the one before it.
 */
export type UpdateDiffRead =
  | { candidate: string; diffs: DiffEvent[]; failed?: undefined }
  | { candidate: string; diffs?: undefined; failed: true; problem: Problem };

/**
 * One run of `skill diff <name> --commit <commit>`, kept with the version of the skill it
 * compared, as a HistoryRead is: a change to the skill makes it a comparison with what is no
 * longer there. A failed run keeps the problem, which the entry shows where it was asked for.
 */
export type ComparisonRead =
  | { version: string; diffs: DiffEvent[]; failed?: undefined }
  | { version: string; diffs?: undefined; failed: true; problem: Problem };

/** The key of the comparison of `commit` of the skill `name` in `comparisons`. */
export const comparisonKey = (name: string, commit: string) =>
  `${name}@${commit}`;

export const useAppStore = create<AppState>()(() => ({
  activity: [],
  adoption: [],
  applied: undefined,
  comparisons: {},
  conflicts: {},
  files: {},
  freshness: { kind: "stale" },
  handshakeFailures: [],
  histories: {},
  installable: undefined,
  inventory: undefined,
  listings: {},
  machine: undefined,
  pending: {},
  reconciles: [],
  refreshes: {},
  serveEnd: undefined,
  settings: undefined,
  sources: [],
  updateDiffs: {},
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

/** The records of the adoption preview that `adopt --all` takes over. */
export const adoptionCandidates = (adoption: readonly AdoptionEvent[]) =>
  adoption.filter((record) => record.state === "candidate");

/** The records of the adoption preview the CLI refuses to adopt, each with its reason. */
export const adoptionRefusals = (adoption: readonly AdoptionEvent[]) =>
  adoption.filter((record) => record.state === "refused");
