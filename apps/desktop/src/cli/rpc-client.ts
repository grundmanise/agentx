// JSON-RPC 2.0 with `agentx serve` (docs/spec/cli-contract.md, The WebSocket door), whatever
// carries the messages: a WebSocket in a browser (socket-wire.ts), or the serve child's stdin and
// stdout inside the Tauri window (tauri-wire.ts). Serve answers `refresh` and `search` with
// results; they reach the store as `refresh_complete` and `search` events of the child
// (transport.ts), so the store reads every wire alike.
import { SCHEMA_VERSION } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  LogEvent,
  RefreshMethodResult,
  RunMethodResult,
  SearchMethodResult,
} from "#/cli/events.gen.ts";
import type {
  RunLine,
  RunResult,
  ServeExit,
  ServeLine,
  ServeRequest,
  Wire,
} from "#/cli/transport.ts";
import { slot } from "#/lib/slot.ts";

/** The params of an `event` notification. */
export interface EventParams {
  id?: string | number;
  stream: "stdout" | "stderr";
  event?: unknown;
  line?: unknown;
}

/** How serve answered a request, or that the connection closed before it did. */
export type Answer =
  | { result: unknown }
  | { error: { code: number; message: string } }
  | { closed: string };

/** One connection to serve: a WebSocket, or the serve child's stdin and stdout. */
export interface Peer {
  childId: number;
  /**
   * Sends a request; its `event` notifications go to onEvent until serve answers it. onWritten is
   * called once the request was written, or with why it could not be.
   */
  request: (
    id: string,
    method: string,
    params?: unknown,
    handlers?: {
      onEvent?: (params: EventParams) => void;
      onWritten?: (failure?: string) => void;
    }
  ) => Promise<Answer>;
  notify: (method: string, params: unknown) => void;
  /** The instance id of the last inventory serve sent on it. */
  instanceId: () => string;
  /** Passes on the lines of serve's own held until serve was started, and from then on each one. */
  release: () => void;
}

export const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const isEventParams = (value: unknown): value is EventParams =>
  isObject(value) &&
  (value.stream === "stdout" || value.stream === "stderr") &&
  (value.id === undefined ||
    typeof value.id === "string" ||
    typeof value.id === "number");

/** An object on stdout is an event; one of a type the app does not know is passed on as it came. */
const isCliEvent = (value: unknown): value is CliEvent =>
  isObject(value) && typeof value.type === "string";

/** An object on stderr is a log event, as the supervisor reads it. */
const isLogEvent = (value: unknown): value is LogEvent => isObject(value);

const isRpcError = (
  value: unknown
): value is { code: number; message: string } =>
  isObject(value) &&
  typeof value.code === "number" &&
  typeof value.message === "string";

const isRunAnswer = (value: unknown): value is RunMethodResult =>
  isObject(value) &&
  (value.exit_code === null || typeof value.exit_code === "number") &&
  typeof value.cancelled === "boolean" &&
  typeof value.timed_out === "boolean";

const isRefreshAnswer = (value: unknown): value is RefreshMethodResult =>
  isObject(value) &&
  typeof value.instance_id === "string" &&
  typeof value.scan_counter === "number";

const isSearchAnswer = (value: unknown): value is SearchMethodResult =>
  isObject(value) &&
  typeof value.instance_id === "string" &&
  typeof value.query === "string" &&
  Array.isArray(value.results);

/** An event's line as the supervisor reads a line: an event, a log event or an invalid line. */
const lineOf = (params: EventParams): RunLine | undefined => {
  if (params.stream === "stdout" && isCliEvent(params.event)) {
    return { event: params.event, kind: "event" };
  }
  if (params.stream === "stderr" && isLogEvent(params.event)) {
    return { event: params.event, kind: "log" };
  }
  if (params.event !== undefined) {
    return {
      kind: "invalid",
      line: JSON.stringify(params.event),
      stream: params.stream,
    };
  }
  return typeof params.line === "string"
    ? { kind: "invalid", line: params.line, stream: params.stream }
    : undefined;
};

/** A peer and what its carrier calls: each message serve sent, and the close of the carrier. */
export interface Connected extends Peer {
  receive: (data: unknown) => void;
  /** A line of serve's own that is no JSON-RPC message: a log event on stderr, or an invalid line. */
  own: (line: RunLine) => void;
  close: (reason: string) => void;
}

/**
 * The JSON-RPC side of one connection, with the child id it stands for. `send` writes one message
 * to the carrier, and fails when it cannot. Serve's own lines go to `emitLine` from `release` on:
 * until then they are held, in order, so that neither the inventory serve sends first nor a
 * warning it logs while it starts is missed.
 */
export const peer = (
  childId: number,
  send: (message: object) => void | Promise<void>,
  releasedAtStart: boolean,
  emitLine: (line: ServeLine) => void
): Connected => {
  const pending = new Map<
    string | number,
    { answer: (answer: Answer) => void; onEvent?: (p: EventParams) => void }
  >();
  let closed: string | undefined;
  let instanceId = "";
  let released = releasedAtStart;
  let held: ServeLine[] = [];

  const own = (line: RunLine) => {
    if (line.kind === "event" && line.event.type === "inventory") {
      instanceId = line.event.instance_id;
    }
    if (released) {
      emitLine({ childId, ...line });
    } else if (closed === undefined) {
      held.push({ childId, ...line });
    }
  };

  const receive = (data: unknown) => {
    let message: unknown = data;
    if (typeof data === "string") {
      try {
        message = JSON.parse(data);
      } catch {
        return;
      }
    }
    if (!isObject(message)) {
      return;
    }
    const { id, method, params } = message;
    if (method === "event") {
      if (isEventParams(params)) {
        if (params.id === undefined) {
          const line = lineOf(params);
          if (line !== undefined) {
            own(line);
          }
        } else {
          pending.get(params.id)?.onEvent?.(params);
        }
      }
    } else if (
      method === undefined &&
      (typeof id === "string" || typeof id === "number")
    ) {
      const waiting = pending.get(id);
      pending.delete(id);
      waiting?.answer(
        isRpcError(message.error)
          ? { error: message.error }
          : { result: message.result }
      );
    }
  };

  /** The requests being written. */
  const writing = new Set<Promise<void>>();

  /** Writes a request; a notification sent meanwhile waits for it. */
  const write = async (message: object) => {
    const written = (async () => {
      await send(message);
    })();
    writing.add(written);
    try {
      await written;
    } finally {
      writing.delete(written);
    }
  };

  /**
   * Sends a notification once the requests being written are, so that a cancel follows its run
   * whatever the carrier does with writes at the same time. Nobody waits on it: one that cannot be
   * sent has nothing left to reach.
   */
  const notifyLater = async (message: object) => {
    await Promise.allSettled(writing);
    try {
      await send(message);
    } catch {
      // The carrier is gone, and with it what the message was for.
    }
  };

  return {
    childId,
    close: (reason) => {
      closed = reason;
      // A connection that closed before serve was started never passes its lines on.
      held = [];
      for (const { answer } of pending.values()) {
        answer({ closed: reason });
      }
      pending.clear();
    },
    instanceId: () => instanceId,
    notify: (method, params) => {
      if (closed === undefined) {
        void notifyLater({ method, params });
      }
    },
    own,
    receive,
    release: () => {
      released = true;
      const lines = held;
      held = [];
      for (const line of lines) {
        emitLine(line);
      }
    },
    request: async (id, method, params, handlers = {}) => {
      if (closed !== undefined) {
        handlers.onWritten?.(closed);
        return { closed };
      }
      const answer = slot<Answer>();
      pending.set(id, { answer: answer.put, onEvent: handlers.onEvent });
      try {
        await write(
          params === undefined ? { id, method } : { id, method, params }
        );
      } catch (error) {
        // Nothing was sent, so nothing answers: the request ends as on a connection that closed.
        pending.delete(id);
        const failure = error instanceof Error ? error.message : String(error);
        handlers.onWritten?.(failure);
        return { closed: failure };
      }
      handlers.onWritten?.();
      return (await answer.take()) ?? { closed: "no answer" };
    },
  };
};

/** A run that ended: its id and exit code. */
export interface RunEnd {
  runId: string;
  exitCode: number | null;
}

/** Adds `handle` to `handlers`, and gives the function that takes it away. */
const subscribe =
  <T>(handlers: Set<T>) =>
  (handle: T) => {
    handlers.add(handle);
    return () => {
      handlers.delete(handle);
    };
  };

/** Calls every handler in `handlers` with the value. */
const callAll =
  <T>(handlers: Set<(value: T) => void>) =>
  (value: T) => {
    for (const handle of handlers) {
      handle(value);
    }
  };

/**
 * What a wire tells the transport, whatever carries it: each line of serve's own, each end of a
 * serve child and each end of a run. `on…` adds a handler, `emit…` calls them all.
 */
export const wireEvents = () => {
  const lines = new Set<(line: ServeLine) => void>();
  const exits = new Set<(exit: ServeExit) => void>();
  const runEnds = new Set<(end: RunEnd) => void>();
  return {
    emitExit: callAll(exits),
    emitLine: callAll(lines),
    emitRunEnd: callAll(runEnds),
    onExit: subscribe(exits),
    onLine: subscribe(lines),
    onRunEnd: subscribe(runEnds),
  };
};

/**
 * Runs, cancels, serve's requests and serve's start over the connection `connection` gives, which
 * it opens when needed; `current` gives the connection serve was started on, if any, and
 * `onStart` marks serve as started.
 */
export const rpcCalls = ({
  connection,
  current,
  emitLine,
  onRunEnd,
  onStart,
}: {
  connection: () => Promise<Peer>;
  current: () => Promise<Peer> | undefined;
  emitLine: (line: ServeLine) => void;
  onRunEnd: (end: RunEnd) => void;
  onStart: () => void;
}) => {
  /** The runs going, by run id: whether one was cancelled, and the connection it was sent on. */
  const runs = new Map<string, { cancelled: boolean; socket?: Peer }>();

  const run: Wire["run"] = async (runId, args, deadlineMs, onLine) => {
    const going: { cancelled: boolean; socket?: Peer } = { cancelled: false };
    runs.set(runId, going);
    const result: RunResult = {
      cancelled: false,
      events: [],
      exitCode: null,
      invalid: [],
      logs: [],
      timedOut: false,
    };
    try {
      const socket = await connection();
      if (going.cancelled) {
        // Cancelled before it was sent: it never runs.
        result.cancelled = true;
      } else {
        going.socket = socket;
        const answer = await socket.request(
          runId,
          "run",
          { args, deadline_ms: deadlineMs ?? null },
          {
            onEvent: (params) => {
              const line = lineOf(params);
              if (line === undefined) {
                return;
              }
              onLine(line);
              if (line.kind === "event") {
                result.events.push(line.event);
              } else if (line.kind === "log") {
                result.logs.push(line.event);
              } else {
                result.invalid.push({ line: line.line, stream: line.stream });
              }
            },
          }
        );
        if ("closed" in answer) {
          throw new Error(`${answer.closed}, which stopped the command`);
        }
        if ("error" in answer) {
          throw new Error(answer.error.message);
        }
        if (!isRunAnswer(answer.result)) {
          throw new Error("agentx serve answered the run with no exit code");
        }
        result.exitCode = answer.result.exit_code;
        result.cancelled = answer.result.cancelled;
        result.timedOut = answer.result.timed_out;
      }
    } finally {
      runs.delete(runId);
    }
    onRunEnd({ exitCode: result.exitCode, runId });
    return result;
  };

  const cancel = (runId: string): boolean => {
    const going = runs.get(runId);
    if (going === undefined) {
      return false;
    }
    going.cancelled = true;
    going.socket?.notify("$/cancelRequest", { id: runId });
    return true;
  };

  /** Passes serve's answer to a refresh request on as the `refresh_complete` event of a child. */
  const refreshed = async (
    socket: Peer,
    requestId: string,
    onWritten: (failure?: string) => void
  ) => {
    const answer = await socket.request(requestId, "refresh", undefined, {
      onWritten,
    });
    // A closed connection answers nothing: the next one gets the request again, as a restarted
    // child does.
    if ("closed" in answer) {
      return;
    }
    const event = {
      instance_id: socket.instanceId(),
      request_id: requestId,
      schema_version: SCHEMA_VERSION,
      type: "refresh_complete" as const,
    };
    emitLine({
      childId: socket.childId,
      event:
        "error" in answer
          ? { ...event, error: answer.error.message, ok: false }
          : {
              ...event,
              ok: isRefreshAnswer(answer.result),
              ...(isRefreshAnswer(answer.result)
                ? answer.result
                : { error: "agentx serve answered the refresh with no scan" }),
            },
      kind: "event",
    });
  };

  /** Passes serve's answer to a search request on as the `search` event of a child. */
  const searched = async (
    socket: Peer,
    requestId: string,
    query: string,
    onWritten: (failure?: string) => void
  ) => {
    const answer = await socket.request(
      requestId,
      "search",
      { query },
      { onWritten }
    );
    // Unanswered, as a search the serve child could not answer: the next search asks anew.
    if ("result" in answer && isSearchAnswer(answer.result)) {
      emitLine({
        childId: socket.childId,
        event: {
          ...answer.result,
          request_id: requestId,
          schema_version: SCHEMA_VERSION,
          type: "search",
        },
        kind: "event",
      });
    }
  };

  /**
   * Sends a request to serve, and resolves once it is written; its answer comes as an event. It
   * fails when serve was not started, or when the request could not be written, as to a serve
   * child that ended.
   */
  const request = async (r: ServeRequest): Promise<void> => {
    const started = current();
    if (started === undefined) {
      throw new Error("agentx serve is not running");
    }
    const socket = await started;
    const written = slot<{ failure?: string }>();
    const onWritten = (failure?: string) => {
      written.put(failure === undefined ? {} : { failure });
    };
    void (r.type === "refresh"
      ? refreshed(socket, r.request_id, onWritten)
      : searched(socket, r.request_id, r.query, onWritten));
    const { failure } = (await written.take()) ?? {};
    if (failure !== undefined) {
      throw new Error(failure);
    }
  };

  /** Starts serve: its lines are passed on from now, those it wrote before first. */
  const start = async (): Promise<number> => {
    const socket = await connection();
    onStart();
    socket.release();
    return socket.childId;
  };
  return { cancel, request, run, start };
};
