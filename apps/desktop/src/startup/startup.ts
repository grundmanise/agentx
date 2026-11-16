// The launch sequence and the state of the startup layer. On launch the app resolves the CLI, runs
// `version` and `doctor` at the same time, then starts the serve child and waits for its first
// inventory. The layer shows only when a check fails, when a command exits with code 8, and in
// manual mode. Until the checks pass, the panel shows the kept inventory under a frosted
// pane (src/store/launch-cache.ts), or, on a launch with none, that it loads. The
// only commands it runs are `version`, `doctor` and `serve`: it changes nothing on disk.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  DoctorEvent,
  InventoryEvent,
} from "#/cli/events.gen.ts";
import { onRunEnd, resolveCli, run } from "#/cli/transport.ts";
import type { RunResult } from "#/cli/transport.ts";
import { readChecks } from "#/startup/checks.ts";
import type { Findings, Problem } from "#/startup/checks.ts";
import { connect, startServe } from "#/store/feed.ts";
import {
  keepLaunch,
  restoreLaunch,
  stillAsShown,
} from "#/store/launch-cache.ts";
import { useAppStore } from "#/store/store.ts";
import type { ServeEnd } from "#/store/store.ts";

/** How long a check may take before it fails. A launch normally takes well under a second. */
export const CHECK_DEADLINE_MS = 30_000;
const deadline = `${CHECK_DEADLINE_MS / 1000} seconds`;
/**
 * Since when (`Date.now()`) the sequence waits for what it waits for now: first the CLI, `version`
 * and `doctor`, then serve's first inventory. A check's deadline counts from then.
 */
export interface Step {
  startedAt: number;
}

export interface StartupState {
  /** Whether the layer shows. */
  open: boolean;
  /** The checks passed once: the screens show this machine as it is, and commands run. */
  ready: boolean;
  /**
   * The store holds the kept inventory: the screens mount before the checks pass, and no command
   * runs until they do.
   */
  cached: boolean;
  /** "launch" closes the layer once the checks pass; "manual" waits for Open agentx. */
  mode: "launch" | "manual";
  running: boolean;
  /** What the running sequence waits for. */
  step: Step | undefined;
  /** The sequence runs again after a failure. */
  retrying: boolean;
  findings: Findings;
}

/** The startup state. Outside React, getState() reads it and setState() changes it. */
export const useStartup = create<StartupState>()(() => ({
  cached: false,
  findings: {},
  mode: "launch",
  open: false,
  ready: false,
  retrying: false,
  // The launch sequence starts with the app.
  running: true,
  step: undefined,
}));

const update = (patch: Partial<StartupState>) => {
  useStartup.setState(patch);
};

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
  ended: ServeEnd | undefined
): ServeOutcome | undefined => {
  const { applied, inventory, serveEnd } = useAppStore.getState();
  const after = Math.max(childId, (ended?.childId ?? 0) + 1);
  if (
    applied !== undefined &&
    inventory !== undefined &&
    applied.childId >= after
  ) {
    return { inventory };
  }
  if (
    serveEnd !== undefined &&
    serveEnd !== ended &&
    serveEnd.childId >= childId
  ) {
    return { error: serveEnd.problem };
  }
  return undefined;
};

/**
 * Waits for serve child `childId`, or a later one, to write an inventory or to exit, until the
 * deadline. A child that is only slow keeps running: a Retry waits for it again.
 */
const serveOutcome = async (childId: number): Promise<ServeOutcome> => {
  const { serveEnd } = useAppStore.getState();
  const ended = serveEnd?.restarting === true ? serveEnd : undefined;
  let unsubscribe: (() => void) | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const outcomes = new ReadableStream<ServeOutcome>({
    start: (controller) => {
      const check = () => {
        const outcome = outcomeOf(childId, ended);
        if (outcome !== undefined) {
          controller.enqueue(outcome);
        }
      };
      unsubscribe = useAppStore.subscribe(check);
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
    unsubscribe?.();
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

/**
 * The checks passed and the machine is no longer as the kept inventory showed it: a change a
 * control started against that inventory does not run.
 */
let moved = false;

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
  const nextStep = () => {
    if (mine === generation) {
      update({ step: { startedAt: Date.now() } });
    }
  };
  update({ findings, running: true });
  nextStep();
  try {
    const resolution = await resolveCli().catch(() => ({ path: null }));
    found({ cli: { path: resolution.path } });
    if (resolution.path === null) {
      return;
    }

    // Both read the machine and neither waits for the other: doctor's rows count only once the
    // version is known.
    nextStep();
    const versioning = oneShot(["version"]);
    const checking = oneShot(["doctor"]);
    const version = await versioning;
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

    const doctor = await checking;
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

    nextStep();
    let childId: number;
    try {
      childId = await startServe();
    } catch (error) {
      found({ serve: { error: { message: String(error) } } });
      return;
    }
    found({ serve: await serveOutcome(childId) });
  } finally {
    if (mine === generation) {
      const { passed } = readChecks(findings, false);
      const { cached, ready } = useStartup.getState();
      if (passed && cached && !ready) {
        moved = !stillAsShown();
      }
      update({
        // A failure shows the layer; a pass closes it at once, except in manual mode.
        open: !passed || useStartup.getState().mode === "manual",
        ready: useStartup.getState().ready || passed,
        retrying: false,
        running: false,
        step: undefined,
      });
      if (passed) {
        keepLaunch();
      }
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

/** Runs the checks again whenever a serve child or a one-shot exits with code 8. */
const listen = async () => {
  useAppStore.subscribe(({ serveEnd }, previous) => {
    if (
      serveEnd !== previous.serveEnd &&
      serveEnd?.exitCode === EXIT_CODES.account_repo
    ) {
      accountRepoLost();
    }
  });
  await onRunEnd(({ runId, exitCode }) => {
    if (exitCode === EXIT_CODES.account_repo && !ownRuns.has(runId)) {
      accountRepoLost();
    }
  });
};

/**
 * Fills the store with the kept inventory, if there is one, so that the screens mount with the
 * first render. Called once, before that render.
 */
export const showKept = () => {
  if (restoreLaunch()) {
    update({ cached: true });
  }
};

/**
 * Waits for the checks while the screens show the kept inventory: a command a control starts then
 * runs once they pass. Resolves with whether the command may run. A command that changes
 * something runs only on the machine the user saw. It does not run when the serve child's first
 * inventory differs from the kept one: the name it was given may now be another skill's. It does
 * not outlive a failed check either: the startup layer has replaced the control that started it,
 * so it does not run after a Retry.
 */
export const checksPass = async (changes: boolean): Promise<boolean> => {
  const { cached, ready } = useStartup.getState();
  if (ready || !cached) {
    return true;
  }
  const answer = (state: StartupState) => {
    if (state.ready) {
      return !(changes && moved);
    }
    return changes && !state.running ? false : undefined;
  };
  let unsubscribe: (() => void) | undefined;
  const answers = new ReadableStream<boolean>({
    start: (controller) => {
      const check = (state: StartupState) => {
        const may = answer(state);
        if (may !== undefined) {
          controller.enqueue(may);
        }
      };
      unsubscribe = useStartup.subscribe(check);
      check(useStartup.getState());
    },
  });
  try {
    const { value } = await answers.getReader().read();
    return value ?? false;
  } finally {
    unsubscribe?.();
  }
};

let launched = false;

/** Listens to the CLI, then runs the launch sequence: once, when the app starts. */
export const launch = () => {
  if (!launched) {
    launched = true;
    void (async () => {
      await connect();
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
