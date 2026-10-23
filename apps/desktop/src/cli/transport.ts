// The app's one way to the CLI. It wraps the Tauri commands and events of the Rust supervisor:
// one-shot runs whose lines arrive as they are written, and the one serve child the window keeps
// open. Events are typed by events.gen.ts, which is generated from the CLI of this app's own
// release, so no schema_version is checked here. An event of a type the app does not know is
// passed on as it came.
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type { UnlistenFn } from "@tauri-apps/api/event";

import type { CliEvent, LogEvent, ServeRequest } from "#/cli/events.gen.ts";

/** A line a child wrote that is not a JSON object. */
export interface InvalidLine {
  stream: "stdout" | "stderr";
  line: string;
}

/** One line of a one-shot run: an event on stdout, a log event on stderr, or an invalid line. */
export type RunLine =
  | { kind: "event"; event: CliEvent }
  | { kind: "log"; event: LogEvent }
  | ({ kind: "invalid" } & InvalidLine);

export interface RunResult {
  /** The exit code; null when a signal ended the CLI. */
  exitCode: number | null;
  /** The events on stdout, in order. */
  events: CliEvent[];
  /** The log events on stderr, in order. */
  logs: LogEvent[];
  invalid: InvalidLine[];
  cancelled: boolean;
  timedOut: boolean;
}

export interface Run {
  runId: string;
  /** The lines as they arrive, closed when the run ends. */
  lines: ReadableStream<RunLine>;
  /** How the run ended. It rejects when the CLI could not be started. */
  result: Promise<RunResult>;
}

export interface RunOptions {
  /** Stops the run when it passes; the result then has timedOut. */
  deadlineMs?: number;
}

export interface Resolution {
  /** The CLI's path; null when none was found. */
  path: string | null;
  /** Found next to the app, or, in a debug build, on the login shell's PATH. */
  found: "bundled" | "path" | null;
}

/** A line of the serve child's stdout, with the id of the child that wrote it. */
export type ServeLine = { childId: number } & (
  | { kind: "event"; event: CliEvent }
  | ({ kind: "invalid" } & InvalidLine)
);

export interface ServeExit {
  childId: number;
  /** The exit code; null when a signal ended it or it could not start. */
  exitCode: number | null;
  /** Its last stderr lines. */
  stderr: string[];
  /** Whether the supervisor starts a new child, after restartInMs. */
  restarting: boolean;
  restartInMs: number | null;
}

// What the supervisor sends, as Rust names it.
type WireRunLine = RunLine & { run_id: string };
interface WireRunEnd {
  run_id: string;
  exit_code: number | null;
  events: CliEvent[];
  logs: LogEvent[];
  invalid: InvalidLine[];
  cancelled: boolean;
  timed_out: boolean;
}
type WireServeLine = (
  | { kind: "event"; event: CliEvent }
  | ({ kind: "invalid" } & InvalidLine)
) & { child_id: number };
interface WireServeExit {
  child_id: number;
  exit_code: number | null;
  stderr: string[];
  restarting: boolean;
  restart_in_ms: number | null;
}
interface WireResolution {
  path: string | null;
  found: "bundled" | "path" | null;
}

/** A stream and the functions that feed it. Nothing waits on its reader: it holds every chunk. */
const channel = <T>() => {
  let controller: ReadableStreamDefaultController<T> | undefined;
  // A stream its reader cancelled takes no more chunks and cannot be closed.
  let open = true;
  const stream = new ReadableStream<T>({
    cancel: () => {
      open = false;
    },
    start: (c) => {
      controller = c;
    },
  });
  return {
    close: () => {
      if (open) {
        open = false;
        controller?.close();
      }
    },
    push: (chunk: T) => {
      if (open) {
        controller?.enqueue(chunk);
      }
    },
    stream,
  };
};

/** Where the CLI is, and how it was found. */
export const resolveCli = async (): Promise<Resolution> => {
  const r = await invoke<WireResolution>("cli_resolve");
  return {
    found: r.found,
    path: r.path,
  };
};

/** Each run's subscription to its events, until it ends. The run is sent once it is in place. */
const subscribed = new Map<string, Promise<UnlistenFn[]>>();

/** Runs `agentx <args> --json`. The supervisor adds `--json`. */
export const run = (args: readonly string[], options: RunOptions = {}): Run => {
  const runId = crypto.randomUUID();
  const lines = channel<RunLine>();
  const ends = channel<WireRunEnd>();
  const subscription = Promise.all([
    listen<WireRunLine>("cli-run", ({ payload }) => {
      if (payload.run_id === runId) {
        const { run_id: _, ...line } = payload;
        lines.push(line);
      }
    }),
    listen<WireRunEnd>("cli-run-end", ({ payload }) => {
      if (payload.run_id === runId) {
        ends.push(payload);
      }
    }),
  ]);
  subscribed.set(runId, subscription);
  const start = async (): Promise<RunResult> => {
    const unlisten = await subscription;
    try {
      await invoke("cli_run", {
        args,
        deadline_ms: options.deadlineMs ?? null,
        run_id: runId,
      });
      // The end event follows every line event; the command's own answer can overtake them.
      const { value: end } = await ends.stream.getReader().read();
      if (end === undefined) {
        throw new Error(`run ${runId} ended without its end event`);
      }
      return {
        cancelled: end.cancelled,
        events: end.events,
        exitCode: end.exit_code,
        invalid: end.invalid,
        logs: end.logs,
        timedOut: end.timed_out,
      };
    } finally {
      subscribed.delete(runId);
      for (const stop of unlisten) {
        stop();
      }
      lines.close();
    }
  };
  return { lines: lines.stream, result: start(), runId };
};

/**
 * Stops a one-shot run, even one that has not started yet. False when no such run is going. It is
 * sent after the run itself, so that the supervisor knows the run.
 */
export const cancel = async (runId: string): Promise<boolean> => {
  await subscribed.get(runId);
  return await invoke<boolean>("cli_cancel", { run_id: runId });
};

/** Starts the serve child unless one runs, and gives its child id. */
const startServe = async (): Promise<number> =>
  await invoke<number>("serve_start");

/** Calls `handle` with every line of every serve child's stdout, in order. */
const onServeLine = async (
  handle: (line: ServeLine) => void
): Promise<UnlistenFn> =>
  await listen<WireServeLine>("serve-event", ({ payload }) => {
    const { child_id: childId, ...line } = payload;
    handle({ childId, ...line });
  });

/** Calls `handle` each time a serve child ends. */
const onServeExit = async (
  handle: (exit: ServeExit) => void
): Promise<UnlistenFn> =>
  await listen<WireServeExit>("serve-exit", ({ payload }) => {
    handle({
      childId: payload.child_id,
      exitCode: payload.exit_code,
      restartInMs: payload.restart_in_ms,
      restarting: payload.restarting,
      stderr: payload.stderr,
    });
  });

/** Writes one request to the serve child's stdin. */
const requestServe = async (request: ServeRequest): Promise<void> => {
  await invoke("serve_request", { line: JSON.stringify(request) });
};

export const serve = {
  onEvent: onServeLine,
  onExit: onServeExit,
  request: requestServe,
  start: startServe,
};
