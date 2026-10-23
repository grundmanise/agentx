// The launch sequence and the state of the startup layer. On launch the app resolves the CLI, runs
// `version`, then `doctor`, then starts the serve child and waits for its first snapshot. The
// layer shows until all of that passes, and comes back whenever a command exits with code 8.
// The only commands it runs are `version`, `doctor` and `serve`: it changes nothing on disk.
import { useSyncExternalStore } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  DoctorEvent,
  ReconcileEvent,
  SnapshotEvent,
} from "#/cli/events.gen.ts";
import { onRunEnd, resolveCli, run, serve } from "#/cli/transport.ts";
import type { RunResult } from "#/cli/transport.ts";
import { readChecks } from "#/startup/checks.ts";
import type { Findings, Problem } from "#/startup/checks.ts";

/** How long a passing launch stays on screen before the app opens. */
const OPEN_AFTER_MS = 600;

export interface StartupState {
  /** Whether the layer shows. */
  open: boolean;
  /** "launch" opens the app once the checks pass; "manual" waits for Open agentx. */
  mode: "launch" | "manual";
  running: boolean;
  /** The sequence runs again after a failure. */
  retrying: boolean;
  findings: Findings;
}

let state: StartupState = {
  findings: {},
  mode: "launch",
  open: true,
  retrying: false,
  // The launch sequence starts with the app.
  running: true,
};
const listeners = new Set<() => void>();

const update = (patch: Partial<StartupState>) => {
  state = { ...state, ...patch };
  for (const listener of listeners) {
    listener();
  }
};

export const useStartup = (): StartupState =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    () => state
  );

/** The reconcile events serve wrote before its first snapshot, kept for the Inbox. */
export const earlyReconciles: ReconcileEvent[] = [];

// What the serve children reported: the last snapshot and the last exit, with the child's id.
let lastSnapshot: { childId: number; event: SnapshotEvent } | undefined;
let lastExit:
  | { childId: number; problem: Problem; restarting: boolean }
  | undefined;
const serveErrors = new Map<number, Problem>();
const serveWaiters = new Set<() => void>();
// The one-shot runs the sequence started: their exit 8 is a check result, not a reason to restart.
const ownRuns = new Set<string>();

const problemOf = (events: CliEvent[], fallback: string): Problem => {
  const error = events.find((e) => e.type === "error");
  if (error === undefined) {
    return { message: fallback };
  }
  return error.hint === undefined
    ? { message: error.message }
    : { hint: error.hint, message: error.message };
};

type ServeOutcome = { snapshot: SnapshotEvent } | { error: Problem };

/**
 * What serve child `childId`, or a later one, has reported: its first snapshot or its exit, if
 * either is in. `ended` is a child that had already ended, with a restart to come, when the wait
 * began: not the answer, since the next child is.
 */
const outcomeOf = (
  childId: number,
  ended: typeof lastExit
): ServeOutcome | undefined => {
  const after = Math.max(childId, (ended?.childId ?? 0) + 1);
  if (lastSnapshot !== undefined && lastSnapshot.childId >= after) {
    return { snapshot: lastSnapshot.event };
  }
  if (
    lastExit !== undefined &&
    lastExit !== ended &&
    lastExit.childId >= childId
  ) {
    return { error: lastExit.problem };
  }
  return undefined;
};

/** Waits for serve child `childId`, or a later one, to write a snapshot or to exit. */
const serveOutcome = async (childId: number): Promise<ServeOutcome> => {
  const ended = lastExit?.restarting === true ? lastExit : undefined;
  const checks: (() => void)[] = [];
  const outcomes = new ReadableStream<ServeOutcome>({
    start: (controller) => {
      const check = () => {
        const outcome = outcomeOf(childId, ended);
        if (outcome !== undefined) {
          serveWaiters.delete(check);
          controller.enqueue(outcome);
        }
      };
      checks.push(check);
      serveWaiters.add(check);
      check();
    },
  });
  try {
    const { value } = await outcomes.getReader().read();
    if (value === undefined) {
      throw new Error("serve's outcome stream closed empty");
    }
    return value;
  } finally {
    for (const check of checks) {
      serveWaiters.delete(check);
    }
  }
};

/** Runs a one-shot as part of the sequence; a CLI that could not start is reported as a problem. */
const oneShot = async (
  args: string[]
): Promise<RunResult | { error: Problem }> => {
  const started = run(args);
  ownRuns.add(started.runId);
  try {
    return await started.result;
  } catch (error) {
    return { error: { message: String(error) } };
  } finally {
    ownRuns.delete(started.runId);
  }
};

// Each sequence gets a number; a later one makes the earlier one's findings stale.
let generation = 0;
let openTimer: ReturnType<typeof setTimeout> | undefined;

const sequence = async () => {
  generation += 1;
  const mine = generation;
  clearTimeout(openTimer);
  let findings: Findings = {};
  const found = (more: Findings) => {
    findings = { ...findings, ...more };
    if (mine === generation) {
      update({ findings });
    }
  };
  update({ findings, running: true });
  try {
    const resolution = await resolveCli().catch(() => ({ path: null }));
    found({ cli: { path: resolution.path } });
    if (resolution.path === null) {
      return;
    }

    const version = await oneShot(["version"]);
    if ("error" in version) {
      found({ version });
      return;
    }
    const cliVersion = version.events.find((e) => e.type === "version");
    if (version.exitCode !== EXIT_CODES.ok || cliVersion === undefined) {
      found({
        version: {
          error: problemOf(
            version.events,
            `agentx version exited with code ${version.exitCode}`
          ),
        },
      });
      return;
    }
    found({ version: { cliVersion: cliVersion.cli_version } });

    const doctor = await oneShot(["doctor"]);
    if ("error" in doctor) {
      found({ doctor: { error: doctor.error, exitCode: null, rows: [] } });
      return;
    }
    const rows = doctor.events.filter(
      (e): e is DoctorEvent => e.type === "doctor"
    );
    // Doctor exits 2 or 8 with a failed row, which the checks show; any other exit but 0 is a
    // doctor that could not check.
    found({
      doctor: {
        exitCode: doctor.exitCode,
        rows,
        ...(doctor.exitCode === EXIT_CODES.ok
          ? {}
          : {
              error: problemOf(
                doctor.events,
                `agentx doctor exited with code ${doctor.exitCode}`
              ),
            }),
      },
    });
    if (readChecks(findings, false).failure !== undefined) {
      return;
    }

    let childId: number;
    try {
      childId = await serve.start();
    } catch (error) {
      found({ serve: { error: { message: String(error) } } });
      return;
    }
    found({ serve: await serveOutcome(childId) });
  } finally {
    if (mine === generation) {
      update({ retrying: false, running: false });
      if (state.mode === "launch" && readChecks(findings, false).passed) {
        openTimer = setTimeout(() => {
          update({ open: false });
        }, OPEN_AFTER_MS);
      }
    }
  }
};

/** A command exited with code 8: the account repo is unusable, so the checks run again. */
const accountRepoLost = () => {
  if (state.running) {
    return;
  }
  update({ mode: "launch", open: true });
  void sequence();
};

/** Subscribes to serve's events and exits and to every one-shot's end. */
const listen = async () => {
  await serve.onEvent((line) => {
    if (line.kind !== "event") {
      return;
    }
    const { event } = line;
    if (event.type === "snapshot") {
      lastSnapshot = { childId: line.childId, event };
    } else if (event.type === "reconcile" && lastSnapshot === undefined) {
      earlyReconciles.push(event);
    } else if (event.type === "error") {
      serveErrors.set(line.childId, problemOf([event], event.message));
    }
    for (const wake of serveWaiters) {
      wake();
    }
  });
  await serve.onExit((exit) => {
    const code = exit.exitCode === null ? "a signal" : `code ${exit.exitCode}`;
    lastExit = {
      childId: exit.childId,
      problem: serveErrors.get(exit.childId) ?? {
        message: exit.stderr.at(-1) ?? `agentx serve stopped with ${code}`,
      },
      restarting: exit.restarting,
    };
    for (const wake of serveWaiters) {
      wake();
    }
    if (exit.exitCode === EXIT_CODES.account_repo) {
      accountRepoLost();
    }
  });
  await onRunEnd(({ runId, exitCode }) => {
    if (exitCode === EXIT_CODES.account_repo && !ownRuns.has(runId)) {
      accountRepoLost();
    }
  });
};

let launched = false;

/** Listens to the CLI, then runs the launch sequence: once, when the app starts. */
export const launch = () => {
  if (!launched) {
    launched = true;
    void (async () => {
      await listen();
      await sequence();
    })();
  }
};

/** Retry: runs the whole sequence again. */
export const retry = () => {
  update({ retrying: true });
  void sequence();
};

/** Opens the layer in manual mode and runs the checks, as Settings › Run checks does. */
export const runChecks = () => {
  update({ mode: "manual", open: true });
  void sequence();
};

/** Closes the layer: Open agentx in manual mode. */
export const closeChecks = () => {
  update({ open: false });
};
