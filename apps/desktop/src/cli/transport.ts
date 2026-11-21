// The app's one way to the CLI: one-shot runs whose lines arrive as they are written, and the one
// serve child the window keeps open, whose stdout and stderr arrive the same way. Two wires carry
// them (ADR 0016): the Tauri commands and events of the Rust supervisor inside the window
// (tauri-wire.ts), and JSON-RPC over the WebSocket door of `agentx serve --listen` in a browser
// (socket-wire.ts). Events are typed by events.gen.ts, which is generated from the CLI of this
// app's own release, so no schema_version is checked here. An event of a type the app does not
// know is passed on as it came.
import type { CliEvent, LogEvent, ServeRequest } from "#/cli/events.gen.ts";
import { socketWire } from "#/cli/socket-wire.ts";
import { tauriWire } from "#/cli/tauri-wire.ts";
import {
  logCommandEnd,
  logCommandStart,
  useActivityLog,
} from "#/store/activity-log.ts";

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
  /**
   * Found next to the app, or, in a debug build, on the login shell's PATH; in a browser, the CLI
   * of the serve whose door the path is.
   */
  found: "bundled" | "path" | "serve" | null;
}

/**
 * One line of the serve child's output, with the id of the child that wrote it: an event on
 * stdout, a log event on stderr, or an invalid line.
 */
export type ServeLine = { childId: number } & (
  | { kind: "event"; event: CliEvent }
  | { kind: "log"; event: LogEvent }
  | ({ kind: "invalid" } & InvalidLine)
);

export interface ServeExit {
  childId: number;
  /** The exit code; null when a signal ended it or it could not start. */
  exitCode: number | null;
  /** Its last stderr lines, debug log events left out. */
  stderr: string[];
  /** Whether the supervisor starts a new child, after restartInMs. */
  restarting: boolean;
  restartInMs: number | null;
}

type MaybePromise<T> = T | Promise<T>;

/** What the transport sends through: the Rust shell inside the Tauri window, or serve's door. */
export interface Wire {
  resolve: () => Promise<Resolution>;
  /** Runs the command line; each line goes to onLine as it arrives, before the run ends. */
  run: (
    runId: string,
    args: readonly string[],
    deadlineMs: number | undefined,
    onLine: (line: RunLine) => void
  ) => Promise<RunResult>;
  cancel: (runId: string) => MaybePromise<boolean>;
  onRunEnd: (
    handle: (end: { runId: string; exitCode: number | null }) => void
  ) => MaybePromise<() => void>;
  serve: {
    logPath: () => MaybePromise<string>;
    onEvent: (handle: (line: ServeLine) => void) => MaybePromise<() => void>;
    onExit: (handle: (exit: ServeExit) => void) => MaybePromise<() => void>;
    request: (request: ServeRequest) => Promise<void>;
    start: () => Promise<number>;
    verbose: (on: boolean) => MaybePromise<void>;
  };
}

/**
 * Inside the Tauri window, and under the end-to-end tests' mocked Rust shell, the Rust shell; in
 * a browser, serve's WebSocket door.
 */
const wire: Wire = "__TAURI_INTERNALS__" in globalThis ? tauriWire : socketWire;

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
export const resolveCli = async (): Promise<Resolution> => await wire.resolve();

/**
 * The arguments as they are sent: with `--verbose`, before a `--` that ends the flags, while the
 * activity log's Debug output is on.
 */
const sentArgs = (args: readonly string[]): readonly string[] => {
  if (!useActivityLog.getState().debug || args.includes("--verbose")) {
    return args;
  }
  const end = args.indexOf("--");
  const at = end === -1 ? args.length : end;
  return [...args.slice(0, at), "--verbose", ...args.slice(at)];
};

/**
 * Runs `agentx <args> --json`. The wire adds `--json`. Every run is a line of the activity log
 * (src/store/activity-log.ts), from its start to how it ended.
 */
export const run = (
  given: readonly string[],
  options: RunOptions = {}
): Run => {
  const args = sentArgs(given);
  const logged = logCommandStart(args);
  const runId = crypto.randomUUID();
  const lines = channel<RunLine>();
  const start = async (): Promise<RunResult> => {
    try {
      const result = await wire.run(
        runId,
        args,
        options.deadlineMs,
        lines.push
      );
      logCommandEnd(logged, result);
      return result;
    } catch (error) {
      logCommandEnd(logged, {
        cancelled: false,
        events: [],
        exitCode: null,
        invalid: [],
        logs: [],
        startError: String(error),
        timedOut: false,
      });
      throw error;
    } finally {
      lines.close();
    }
  };
  return { lines: lines.stream, result: start(), runId };
};

/** Stops a one-shot run, even one that has not started yet. False when no such run is going. */
export const cancel = async (runId: string): Promise<boolean> =>
  await wire.cancel(runId);

/** Calls `handle` with the run id and exit code each time any one-shot run ends. */
export const onRunEnd = async (
  handle: (end: { runId: string; exitCode: number | null }) => void
): Promise<() => void> => await wire.onRunEnd(handle);

export const serve = {
  /** The path of the serve log file, which holds everything serve wrote to stderr. */
  logPath: async () => await wire.serve.logPath(),
  /** Calls `handle` with every line of every serve child's output, each stream in order. */
  onEvent: async (handle: (line: ServeLine) => void) =>
    await wire.serve.onEvent(handle),
  /** Calls `handle` each time a serve child ends. */
  onExit: async (handle: (exit: ServeExit) => void) =>
    await wire.serve.onExit(handle),
  /** Sends one request to the serve child. */
  request: async (request: ServeRequest) => {
    await wire.serve.request(request);
  },
  /** Starts the serve child unless one runs, and gives its child id. */
  start: async () => await wire.serve.start(),
  /** Whether serve logs at debug level; a change restarts the serve child. */
  verbose: async (on: boolean) => {
    await wire.serve.verbose(on);
  },
};
