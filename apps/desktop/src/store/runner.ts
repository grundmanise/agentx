// The one way a control runs a command. The control shows its command as pending, the command
// runs, and the outcome is a toast and an activity entry. After any command that may have changed
// something, whatever its exit code, the serve child is asked to scan again: the screens redraw
// from the inventory that follows, never from a result the app predicts. The command's outcome
// and the inventory's freshness are separate states.
import { CLI_COMMANDS } from "#/cli/commands.gen.ts";
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, LogEvent, ProgressEvent } from "#/cli/events.gen.ts";
import { cancel, run } from "#/cli/transport.ts";
import type { RunLine } from "#/cli/transport.ts";
import { notify, notifyError } from "#/components/toast.tsx";
import { navigateTo } from "#/lib/navigate.ts";
import { refresh } from "#/store/feed.ts";
import { addActivity, useAppStore } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The commands that change nothing: no refresh follows them, and they add no activity entry. */
const READS = new Set([
  "version",
  "doctor",
  "machine",
  "config list",
  "config get",
  "source list",
  "source skills",
  "skill list",
  "skill diff",
  "skill history",
]);

/** The command that `args` name: the longest run of leading words that is one. */
export const commandOf = (args: readonly string[]): string => {
  const end = args.findIndex((arg) => arg.startsWith("-"));
  const words = end === -1 ? args : args.slice(0, end);
  const name = words
    .map((_, i) => words.slice(0, words.length - i).join(" "))
    .find((candidate) => Object.hasOwn(CLI_COMMANDS, candidate));
  return name ?? words[0] ?? "";
};

/**
 * Whether `args` change nothing. `adopt` with no flag is a preview, which writes nothing, and
 * `update --check` only asks which version is out.
 */
export const isRead = (args: readonly string[]) => {
  const command = commandOf(args);
  return (
    READS.has(command) ||
    (command === "adopt" && args.length === 1) ||
    (command === "update" && args.includes("--check"))
  );
};

export interface CommandOptions {
  /** Names the control: it shows as pending while the command runs, and Cancel stops it. */
  label: string;
  /** The toast and activity entry when the command exits 0. */
  success?: string | ((events: CliEvent[]) => string);
  onProgress?: (event: ProgressEvent) => void;
  /** Stops the command when it passes. */
  deadlineMs?: number;
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

const followProgress = async (
  reader: ReadableStreamDefaultReader<RunLine>,
  onProgress: (event: ProgressEvent) => void
): Promise<void> => {
  const { done, value } = await reader.read();
  if (done) {
    return;
  }
  if (value.kind === "event" && value.event.type === "progress") {
    onProgress(value.event);
  }
  await followProgress(reader, onProgress);
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

/** Shows the outcome, by exit code (docs/spec/cli-contract.md, Exit codes). */
const report = (
  args: readonly string[],
  options: CommandOptions,
  outcome: Outcome,
  again: () => void
) => {
  const log = !isRead(args);
  const { exitCode, events } = outcome;
  if (exitCode === EXIT_CODES.ok) {
    const text =
      typeof options.success === "function"
        ? options.success(events)
        : options.success;
    if (text !== undefined) {
      notify(text);
      if (log) {
        addActivity("success", text);
      }
    }
    return;
  }
  if (exitCode === EXIT_CODES.locked) {
    const text = "Another agentx command is running";
    notify(text, {
      label: "Retry",
      onClick: again,
    });
    if (log) {
      addActivity("attention", text);
    }
    return;
  }
  if (exitCode === EXIT_CODES.interrupted && outcome.cancelled) {
    notify("Stopped");
    if (log) {
      addActivity("attention", "Stopped");
    }
    return;
  }
  const problem = problemOf(args, outcome);
  if (log) {
    addActivity("danger", problem.message);
  }
  // Exit 8: the startup layer comes back (src/startup/startup.ts) and says why.
  if (exitCode === EXIT_CODES.account_repo) {
    return;
  }
  // Exit 4: the skill with the pending merge opens, where the merge is finished or cancelled.
  const conflict = events.find((e) => e.type === "conflict");
  if (exitCode === EXIT_CODES.pending_merge && conflict !== undefined) {
    navigateTo(`/skills/${encodeURIComponent(conflict.name)}`);
    return;
  }
  notifyError({
    ...problem,
    warnings: outcome.logs
      .filter((l) => l.level === "warn")
      .map((l) => l.message),
  });
};

/**
 * Runs `agentx <args> --json` for the control named `options.label`, and returns how it ended
 * so that the caller can follow up. It never rejects: a CLI that could not start is an outcome.
 */
export const runCommand = async (
  args: readonly string[],
  options: CommandOptions
): Promise<Outcome> => {
  const started = run(args, { deadlineMs: options.deadlineMs });
  setPending(options.label, started.runId);
  const progress =
    options.onProgress === undefined
      ? undefined
      : followProgress(started.lines.getReader(), options.onProgress);
  let outcome: Outcome;
  try {
    const [result] = await Promise.all([started.result, progress]);
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
  const read = isRead(args);
  if (!read) {
    void refresh();
  }
  report(args, options, outcome, () => {
    void runCommand(args, options);
  });
  for (const handle of finishedListeners) {
    handle({ ...outcome, args, command: commandOf(args), read });
  }
  return outcome;
};
