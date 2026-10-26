// The launch sequence and the state of the startup layer. On launch the app resolves the CLI, runs
// `version`, then `doctor`, then starts the serve child and waits for its first inventory. All of
// that runs out of sight: the screens mount once it passes, and the layer shows only when a check
// fails, when a command exits with code 8, and in manual mode. A launch that runs over 2 seconds
// says what it does in the empty panel. The only commands it runs are `version`, `doctor` and `serve`: it
// changes nothing on disk.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  DoctorEvent,
  ReconcileEvent,
  InventoryEvent,
} from "#/cli/events.gen.ts";
import { onRunEnd, resolveCli, run, serve } from "#/cli/transport.ts";
import type { RunResult } from "#/cli/transport.ts";
import { readChecks } from "#/startup/checks.ts";
import type { Findings, Problem } from "#/startup/checks.ts";

/** How long a check may take before it fails. A launch normally takes well under a second. */
export const CHECK_DEADLINE_MS = 30_000;
const deadline = `${CHECK_DEADLINE_MS / 1000} seconds`;
/** How long the checks run out of sight before the empty panel says that they run. */
const SLOW_AFTER_MS = 2000;

/** What the sequence waits for, and since when (`Date.now()`): a check's deadline counts from then. */
export interface Step {
  /** "machine" while the CLI, `version` and `doctor` run; "inventory" while serve scans. */
  kind: "machine" | "inventory";
  startedAt: number;
}

export interface StartupState {
  /** Whether the layer shows. */
  open: boolean;
  /** The checks passed once: the screens may mount. */
  ready: boolean;
  /** "launch" closes the layer once the checks pass; "manual" waits for Open agentx. */
  mode: "launch" | "manual";
  running: boolean;
  /** The sequence has run for over SLOW_AFTER_MS. */
  slow: boolean;
  /** What the running sequence waits for. */
  step: Step | undefined;
  /** The sequence runs again after a failure. */
  retrying: boolean;
  findings: Findings;
}

/** The startup state. Outside React, getState() reads it and setState() changes it. */
export const useStartup = create<StartupState>()(() => ({
  findings: {},
  mode: "launch",
  open: false,
  ready: false,
  retrying: false,
  // The launch sequence starts with the app.
  running: true,
  slow: false,
  step: undefined,
}));

const update = (patch: Partial<StartupState>) => {
  useStartup.setState(patch);
};

/** The reconcile events serve wrote before its first inventory, kept for the Inbox. */
export const earlyReconciles: ReconcileEvent[] = [];

// What the serve children reported: the last inventory and the last exit, with the child's id.
let lastInventory: { childId: number; event: InventoryEvent } | undefined;
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

type ServeOutcome = { inventory: InventoryEvent } | { error: Problem };

/**
 * What serve child `childId`, or a later one, has reported: its first inventory or its exit, if
 * either is in. `ended` is a child that had already ended, with a restart to come, when the wait
 * began: not the answer, since the next child is.
 */
const outcomeOf = (
  childId: number,
  ended: typeof lastExit
): ServeOutcome | undefined => {
  const after = Math.max(childId, (ended?.childId ?? 0) + 1);
  if (lastInventory !== undefined && lastInventory.childId >= after) {
    return { inventory: lastInventory.event };
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

/**
 * Waits for serve child `childId`, or a later one, to write an inventory or to exit, until the
 * deadline. A child that is only slow keeps running: a Retry waits for it again.
 */
const serveOutcome = async (childId: number): Promise<ServeOutcome> => {
  const ended = lastExit?.restarting === true ? lastExit : undefined;
  const checks: (() => void)[] = [];
  let timer: ReturnType<typeof setTimeout> | undefined;
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
      timer = setTimeout(() => {
        controller.enqueue({
          error: {
            message: `agentx serve wrote no inventory in ${deadline}`,
          },
        });
      }, CHECK_DEADLINE_MS);
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
    clearTimeout(timer);
    for (const check of checks) {
      serveWaiters.delete(check);
    }
  }
};

/**
 * Runs a one-shot as part of the sequence; a CLI that could not start, or did not finish by the
 * deadline, is reported as a problem.
 */
const oneShot = async (
  args: string[]
): Promise<RunResult | { error: Problem }> => {
  const started = run(args, { deadlineMs: CHECK_DEADLINE_MS });
  ownRuns.add(started.runId);
  try {
    const result = await started.result;
    return result.timedOut
      ? {
          error: {
            message: `agentx ${args.join(" ")} did not finish in ${deadline}`,
          },
        }
      : result;
  } catch (error) {
    return { error: { message: String(error) } };
  } finally {
    ownRuns.delete(started.runId);
  }
};

// Each sequence gets a number; a later one makes the earlier one's findings stale.
let generation = 0;

const sequence = async () => {
  generation += 1;
  const mine = generation;
  let findings: Findings = {};
  const found = (more: Findings) => {
    findings = { ...findings, ...more };
    if (mine === generation) {
      update({ findings });
    }
  };
  const stepTo = (kind: Step["kind"]) => {
    if (mine === generation) {
      update({ step: { kind, startedAt: Date.now() } });
    }
  };
  update({ findings, running: true, slow: false });
  stepTo("machine");
  const slowTimer = setTimeout(() => {
    if (mine === generation) {
      update({ slow: true });
    }
  }, SLOW_AFTER_MS);
  try {
    const resolution = await resolveCli().catch(() => ({ path: null }));
    found({ cli: { path: resolution.path } });
    if (resolution.path === null) {
      return;
    }

    stepTo("machine");
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

    stepTo("machine");
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

    stepTo("inventory");
    let childId: number;
    try {
      childId = await serve.start();
    } catch (error) {
      found({ serve: { error: { message: String(error) } } });
      return;
    }
    found({ serve: await serveOutcome(childId) });
  } finally {
    clearTimeout(slowTimer);
    if (mine === generation) {
      const { passed } = readChecks(findings, false);
      update({
        // A failure shows the layer; a pass closes it at once, except in manual mode.
        open: !passed || useStartup.getState().mode === "manual",
        ready: useStartup.getState().ready || passed,
        retrying: false,
        running: false,
        slow: false,
        step: undefined,
      });
    }
  }
};

/** A command exited with code 8: the account repo is unusable, so the checks run again. */
const accountRepoLost = () => {
  if (useStartup.getState().running) {
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
    if (event.type === "inventory") {
      lastInventory = { childId: line.childId, event };
    } else if (event.type === "reconcile" && lastInventory === undefined) {
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
