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
  SkillFileEvent,
  Source,
  SourceSkillEvent,
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

/** A problem a serve child reported with an `error` event of its own, and kept running. */
export interface ServeError {
  childId: number;
  /** The error event's code, such as `store`. */
  code: string;
  problem: Problem;
}

/** How a serve child ended. */
export interface ServeEnd {
  childId: number;
  exitCode: number | null;
  problem: Problem;
  /** Whether the supervisor starts a new child. */
  restarting: boolean;
}

export interface AppState {
  /** The last inventory applied, from the active serve child only. */
  inventory: InventoryEvent | undefined;
  applied: Applied | undefined;
  freshness: Freshness;
  /** Refresh requests sent and not acknowledged yet: request id → the serve child it went to. */
  refreshes: Record<string, number>;
  /** How the last serve child that ended did so. */
  serveEnd: ServeEnd | undefined;
  /**
   * The last problem a serve child reported, such as a scan that failed or the serve lock another
   * serve holds: serve keeps running and answering, and reports it with an `error` event.
   */
  serveError: ServeError | undefined;
  /**
   * When an update check last ended well (RFC 3339): serve's own, by its `update_check` event, or
   * a `skill check-updates` run, which reached every source or all but some (exit 3). Undefined
   * until one did this session.
   */
  lastChecked: string | undefined;
  /**
   * The store and its size on disk, from `machine`, read when Settings shows Storage: the
   * inventory leaves them out, since every git write changes the size. Undefined before a read.
   */
  store: Pick<MachineEvent, "store" | "store_bytes"> | undefined;
  /** Whether the telemetry question was answered this session: it is not asked again. */
  telemetryAnswered: boolean;
  /** What `skill import` would take over, from its preview. */
  adoption: AdoptionEvent[];
  /** The controls whose command runs: label → run id. */
  pending: Record<string, string>;
  /** The skills of each source listed so far, by source id, from `source skills <id>`. */
  listings: Record<string, SourceSkillEvent[]>;
  /**
   * Your skills on the hub that this machine has not installed, from
   * `skill list --remote`; undefined until read.
   */
  installable: InstallableSkillEvent[] | undefined;
  /**
   * What `skill show` read of each skill so far, by the row's key: its files, its unpublished
   * edits, what its update changes and its history. The detail pane reads a skill when it is
   * selected; the Publish dialog and the What's new sheet show the same read.
   */
  skillReads: Record<string, SkillRead>;
  /**
   * The SKILL.md of each skill of a source read so far, by the Sources card's key, from
   * `source read`: a row of the Sources list reads it when it opens.
   */
  skillMds: Record<string, SkillMdRead>;
  /**
   * The declarations whose handshake failed in the last Check tools of this session, from that
   * one-shot's inventory, which is not applied. Replaced by the next check; gone on a restart.
   */
  handshakeFailures: HandshakeFailure[];
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
 * One read of `skill show` for a skill, kept with the version of the skill it was read for. It
 * stays while a later version is read, and that read replaces it in one write, so every section
 * that shows it changes at once. `edits` are the files edited since the version the skill is
 * compared with, and `update` what its update changes: the `diff` events that say `update`.
 */
export type SkillRead =
  | {
      version: string;
      files: SkillFileEvent[];
      edits: DiffEvent[];
      update: DiffEvent[];
      history: HistoryEvent[];
      failed?: undefined;
    }
  | { version: string; files?: undefined; failed: true; problem: Problem };

/**
 * One read of `source read` for a skill of a source, kept with the version it was read for, its
 * subpath and the tree the source listed it at, so that a fetch that changes the skill never
 * shows the SKILL.md of the version before it.
 */
export type SkillMdRead =
  | { version: string; file: SkillFileEvent; failed?: undefined }
  | { version: string; file?: undefined; failed: true };

export const useAppStore = create<AppState>()(() => ({
  adoption: [],
  applied: undefined,
  conflicts: {},
  freshness: { kind: "stale" },
  handshakeFailures: [],
  installable: undefined,
  inventory: undefined,
  lastChecked: undefined,
  listings: {},
  pending: {},
  refreshes: {},
  serveEnd: undefined,
  serveError: undefined,
  skillMds: {},
  skillReads: {},
  store: undefined,
  telemetryAnswered: false,
}));

const noSources: Source[] = [];

/** The sources of the settings, as the inventory lists them; none before the first inventory. */
export const sourcesOf = (s: AppState): Source[] =>
  s.inventory?.sources ?? noSources;
