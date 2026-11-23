// The one way a control runs a command. The control shows its command as pending, the command
// runs, and the outcome is a toast and an activity entry. The caller says when its command only
// reads, with `sideEffect: false`. After any other command, whatever its exit code, the serve
// child is asked to scan again: the screens redraw from the inventory that follows, never from a
// result the app predicts. The command's outcome and the inventory's freshness are separate
// states.
import { CLI_COMMANDS } from "#/cli/commands.gen.ts";
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, LogEvent, ProgressEvent } from "#/cli/events.gen.ts";
import { cancel, run } from "#/cli/transport.ts";
import type { RunLine } from "#/cli/transport.ts";
import {
  notify,
  notifyAttention,
  notifyError,
  notifySuccess,
} from "#/components/toast.tsx";
import type { ErrorToast, ToastAction } from "#/components/toast.tsx";
import { navigateTo } from "#/lib/navigate.ts";
import { skillNamesOf } from "#/lib/problem-text.ts";
import { refresh } from "#/store/feed.ts";
import { addActivity, useAppStore } from "#/store/store.ts";
import type { Problem, Tone } from "#/store/store.ts";

/** The command that `args` name: the longest run of leading words that is one. */
export const commandOf = (args: readonly string[]): string => {
  const end = args.findIndex((arg) => arg.startsWith("-"));
  const words = end === -1 ? args : args.slice(0, end);
  const name = words
    .map((_, i) => words.slice(0, words.length - i).join(" "))
    .find((candidate) => Object.hasOwn(CLI_COMMANDS, candidate));
  return name ?? words[0] ?? "";
};

export interface CommandOptions {
  /** Names the control: it shows as pending while the command runs, and Cancel stops it. */
  label: string;
  /**
   * False for a command that only reads, such as `skill diff` or the preview that `skill import`
   * with no argument is: no scan follows it, and it adds no activity entry. Leave it out for a
   * command that may change something, and for one of unknown kind.
   */
  sideEffect?: boolean;
  /**
   * The toast and activity entry when the command exits 0; none when the events say the control
   * shows the outcome itself. The logs are the command's log events, its warnings among them.
   */
  success?:
    | string
    | ((events: CliEvent[], logs: LogEvent[]) => string | undefined);
  /** An action on the success toast, such as Undo, or what the events and logs say it is, if any. */
  action?:
    | ToastAction
    | ((events: CliEvent[], logs: LogEvent[]) => ToastAction | undefined);
  /**
   * The lines under the success toast, from the command's warnings; by default each warning as
   * the CLI wrote it.
   */
  warnings?: (logs: LogEvent[]) => string[];
  onProgress?: (event: ProgressEvent) => void;
  /** Called with each event on stdout as it arrives, before the command ends. */
  onEvent?: (event: CliEvent) => void;
  /**
   * Called with each log event on stderr as it arrives. stdout and stderr are separate streams,
   * so a log may arrive after an event the CLI wrote later.
   */
  onLog?: (log: LogEvent) => void;
  /** Stops the command when it passes. */
  deadlineMs?: number;
  /**
   * The control shows a failure itself, where it happened: the runner then shows no error toast
   * and no "Stopped", and still logs the activity entry. "Another agentx command is running" and
   * its Retry still show, since the control cannot run the command again.
   */
  ownFailure?: boolean;
  /**
   * With `ownFailure`, the activity entry of a failure the control words itself, in the tone and
   * words of what it shows; undefined leaves the runner's own danger entry.
   */
  ownActivity?: (outcome: Outcome) => ActivityText | undefined;
  /**
   * The control shows a merge the command left pending itself, where it happened: exit 4 then
   * opens no skill, so a run of several commands stays where it is.
   */
  stayOnConflict?: boolean;
  /**
   * The error toast for a failure the control words itself, such as a check that could not reach
   * a source, with an action that answers it; undefined leaves the runner's own.
   */
  failure?: (outcome: Outcome) => ErrorToast | undefined;
  /**
   * Names the error toast keeps the case of when one starts the CLI's message or hint, such as
   * the skill the command is for; the name of every skill the inventory holds is kept anyway.
   */
  keep?: readonly string[];
  /**
   * What Retry runs after "Another agentx command is running", for a command that is one step of
   * a longer sequence; by default it runs this command again.
   */
  retry?: () => void;
}

/** An activity entry's tone and words. */
export interface ActivityText {
  tone: Tone;
  message: string;
}

export interface Outcome {
  /** null when a signal ended the CLI or it could not start. */
  exitCode: number | null;
  events: CliEvent[];
  logs: LogEvent[];
  /** The user stopped it. */
  cancelled: boolean;
  timedOut: boolean;
  /** Why the CLI could not start; absent when it ran. */
  startError?: string;
}

/** What a finished command was, for the slices that reload after it (src/store/loaders.ts). */
export interface Finished extends Outcome {
  args: readonly string[];
  command: string;
  /** The caller said it only reads, with `sideEffect: false`: it changed nothing. */
  read: boolean;
}

const finishedListeners = new Set<(finished: Finished) => void>();

/** Calls `handle` after every command the runner runs. */
export const onFinished = (handle: (finished: Finished) => void) => {
  finishedListeners.add(handle);
};

const setPending = (label: string, runId: string | undefined) => {
  useAppStore.setState(({ pending }) => {
    const { [label]: _, ...rest } = pending;
    return {
      pending: runId === undefined ? rest : { ...rest, [label]: runId },
    };
  });
};

/** Whether the command of the control named `label` runs. */
export const usePending = (label: string) =>
  useAppStore((s) => Object.hasOwn(s.pending, label));

/** Stops the command of the control named `label`; it ends with code 9. */
export const cancelCommand = async (label: string) => {
  const runId = useAppStore.getState().pending[label];
  if (runId !== undefined) {
    await cancel(runId);
  }
};

/** The handlers a control follows a command's lines with as they arrive. */
type Followers = Pick<CommandOptions, "onEvent" | "onLog" | "onProgress">;

const follow = async (
  reader: ReadableStreamDefaultReader<RunLine>,
  followers: Followers
): Promise<void> => {
  const { done, value } = await reader.read();
  if (done) {
    return;
  }
  if (value.kind === "event") {
    followers.onEvent?.(value.event);
    if (value.event.type === "progress") {
      followers.onProgress?.(value.event);
    }
  } else if (value.kind === "log") {
    followers.onLog?.(value.event);
  }
  await follow(reader, followers);
};

const problemOf = (args: readonly string[], outcome: Outcome): Problem => {
  if (outcome.startError !== undefined) {
    return { message: `agentx could not start: ${outcome.startError}` };
  }
  const error = outcome.events.find((e) => e.type === "error");
  if (error !== undefined) {
    return error.hint === undefined
      ? { message: error.message }
      : { hint: error.hint, message: error.message };
  }
  const command = `agentx ${commandOf(args)}`;
  if (outcome.timedOut) {
    return { message: `${command} took too long and was stopped` };
  }
  return {
    message:
      outcome.exitCode === null
        ? `${command} was stopped`
        : `${command} exited with code ${outcome.exitCode}`,
  };
};

/**
 * The error toast of a failure: the control's own wording, or the CLI's problem and warnings. A
 * skill's name that starts the message or the hint keeps its case: the names the control gave,
 * and the name of every skill the inventory holds.
 */
const errorToastOf = (
  options: CommandOptions,
  problem: Problem,
  outcome: Outcome
): ErrorToast => {
  const toast = options.failure?.(outcome) ?? {
    ...problem,
    warnings: outcome.logs
      .filter((l) => l.level === "warn")
      .map((l) => l.message),
  };
  return {
    ...toast,
    keep: [
      ...(toast.keep ?? []),
      ...(options.keep ?? []),
      ...skillNamesOf(useAppStore.getState().inventory),
    ],
  };
};

/**
 * Keeps the `conflict` events of a command, by skill: the merge each left pending. A skill the
 * command emitted with no conflict, such as an update that went through or `--abort`, has none.
 */
const keepConflicts = (events: CliEvent[]) => {
  const conflicts = events.filter((e) => e.type === "conflict");
  const settled = events
    .filter((e) => e.type === "library_skill")
    .filter((e) => !conflicts.some((c) => c.name === e.name));
  if (conflicts.length === 0 && settled.length === 0) {
    return;
  }
  useAppStore.setState((s) => {
    const kept = Object.entries(s.conflicts).filter(
      ([name]) => !settled.some((e) => e.name === name)
    );
    return {
      conflicts: Object.fromEntries([
        ...kept,
        ...conflicts.map((c) => [c.name, c] as const),
      ]),
    };
  });
};

/**
 * The activity entry of a failure: the CLI's problem as a danger entry, or the control's own
 * words for a failure it shows itself (`ownActivity`).
 */
const logFailure = (
  options: CommandOptions,
  outcome: Outcome,
  problem: Problem
) => {
  const own =
    options.ownFailure === true ? options.ownActivity?.(outcome) : undefined;
  if (own === undefined) {
    addActivity("danger", problem.message);
  } else {
    addActivity(own.tone, own.message);
  }
};

/**
 * Shows a command that exited 0: its success text, as a success toast, or, with warnings, as an
 * attention toast with them under it. The activity entry asks for attention rather than reporting
 * plain success when there are warnings: a placement skipped, a copy that differed.
 */
const reportSuccess = (
  options: CommandOptions,
  outcome: Outcome,
  log: boolean
) => {
  const { events, logs } = outcome;
  const text =
    typeof options.success === "function"
      ? options.success(events, logs)
      : options.success;
  if (text === undefined) {
    return;
  }
  const action =
    typeof options.action === "function"
      ? options.action(events, logs)
      : options.action;
  const warned = logs.filter((l) => l.level === "warn");
  const warnings = options.warnings?.(warned) ?? warned.map((l) => l.message);
  if (warnings.length > 0) {
    notifyAttention({ action, message: text, warnings });
  } else {
    notifySuccess(text, action);
  }
  if (log) {
    addActivity(warnings.length > 0 ? "attention" : "success", text);
  }
};

/** Shows the outcome, by exit code (docs/spec/cli-contract.md, Exit codes). */
const report = (
  args: readonly string[],
  options: CommandOptions,
  outcome: Outcome,
  log: boolean,
  again: () => void
) => {
  const { exitCode, events } = outcome;
  if (exitCode === EXIT_CODES.ok) {
    reportSuccess(options, outcome, log);
    return;
  }
  if (exitCode === EXIT_CODES.locked) {
    const text = "Another agentx command is running";
    notify(
      text,
      {
        label: "Retry",
        onClick: again,
      },
      { kind: "warning" }
    );
    if (log) {
      addActivity("attention", text);
    }
    return;
  }
  if (exitCode === EXIT_CODES.interrupted && outcome.cancelled) {
    if (options.ownFailure !== true) {
      notify("Stopped");
    }
    if (log) {
      addActivity("attention", "Stopped");
    }
    return;
  }
  const problem = problemOf(args, outcome);
  if (log) {
    logFailure(options, outcome, problem);
  }
  // Exit 8: the startup layer comes back (src/startup/startup.ts) and says why.
  if (exitCode === EXIT_CODES.store) {
    return;
  }
  // Exit 4: the skill with the pending merge opens, where the merge is finished or cancelled.
  const conflict = events.find((e) => e.type === "conflict");
  if (
    exitCode === EXIT_CODES.pending_merge &&
    conflict !== undefined &&
    options.stayOnConflict !== true
  ) {
    navigateTo(`/skills/${encodeURIComponent(conflict.name)}`);
    return;
  }
  if (options.ownFailure === true) {
    return;
  }
  notifyError(errorToastOf(options, problem, outcome));
};

/**
 * Runs `agentx <args> --json` for the control named `options.label`, and returns how it ended
 * so that the caller can follow up. It never rejects: a CLI that could not start is an outcome.
 * A command that only reads (`options.sideEffect` is false) asks for no scan after it and adds no
 * activity entry.
 */
export const agentx = async (
  args: readonly string[],
  options: CommandOptions
): Promise<Outcome> => {
  const read = options.sideEffect === false;
  const started = run(args, { deadlineMs: options.deadlineMs });
  setPending(options.label, started.runId);
  const following =
    options.onProgress === undefined &&
    options.onEvent === undefined &&
    options.onLog === undefined
      ? undefined
      : follow(started.lines.getReader(), options);
  let outcome: Outcome;
  try {
    const [result] = await Promise.all([started.result, following]);
    outcome = {
      cancelled: result.cancelled,
      events: result.events,
      exitCode: result.exitCode,
      logs: result.logs,
      timedOut: result.timedOut,
    };
  } catch (error) {
    outcome = {
      cancelled: false,
      events: [],
      exitCode: null,
      logs: [],
      startError: String(error),
      timedOut: false,
    };
  } finally {
    if (useAppStore.getState().pending[options.label] === started.runId) {
      setPending(options.label, undefined);
    }
  }
  keepConflicts(outcome.events);
  if (!read) {
    void refresh();
  }
  report(
    args,
    options,
    outcome,
    !read,
    options.retry ??
      (() => {
        void agentx(args, options);
      })
  );
  for (const handle of finishedListeners) {
    handle({ ...outcome, args, command: commandOf(args), read });
  }
  return outcome;
};
