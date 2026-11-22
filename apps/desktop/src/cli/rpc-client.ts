// JSON-RPC 2.0 with `agentx serve` (docs/spec/cli-contract.md, The WebSocket door), whatever
// carries the messages: a WebSocket in a browser (socket-wire.ts). Serve answers `refresh` and
// `search` with results; they reach the store as the `refresh_complete` and `search` events the
// serve child writes on its stdout, so the store reads every wire alike.
import { SCHEMA_VERSION } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  LogEvent,
  SearchResult,
  ServeRequest,
} from "#/cli/events.gen.ts";
import type { RunLine, RunResult, ServeLine, Wire } from "#/cli/transport.ts";
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
  /** Sends a request; its `event` notifications go to onEvent until serve answers it. */
  request: (
    id: string,
    method: string,
    params?: unknown,
    onEvent?: (params: EventParams) => void
  ) => Promise<Answer>;
  notify: (method: string, params: unknown) => void;
  /** The instance id of the last inventory serve sent on it. */
  instanceId: () => string;
  /** Passes on the inventory held until serve was started, and from then on every event. */
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

const isRunAnswer = (
  value: unknown
): value is {
  exit_code: number | null;
  cancelled: boolean;
  timed_out: boolean;
} =>
  isObject(value) &&
  (value.exit_code === null || typeof value.exit_code === "number") &&
  typeof value.cancelled === "boolean" &&
  typeof value.timed_out === "boolean";

const isRefreshAnswer = (
  value: unknown
): value is { instance_id: string; scan_counter: number } =>
  isObject(value) &&
  typeof value.instance_id === "string" &&
  typeof value.scan_counter === "number";

const isSearchAnswer = (
  value: unknown
): value is { instance_id: string; query: string; results: SearchResult[] } =>
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
  close: (reason: string) => void;
}

/**
 * The JSON-RPC side of one connection, with the child id it stands for. `send` writes one message
 * to the carrier. Serve's own events go to `emitLine` from `release` on: until then only the last
 * inventory is held, which serve sends first.
 */
export const peer = (
  childId: number,
  send: (message: object) => void,
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
  let held: ServeLine | undefined;

  const ownEvent = (params: EventParams) => {
    const line = lineOf(params);
    if (line === undefined) {
      return;
    }
    const inventory =
      line.kind === "event" && line.event.type === "inventory"
        ? line.event
        : undefined;
    if (inventory !== undefined) {
      instanceId = inventory.instance_id;
    }
    if (released) {
      emitLine({ childId, ...line });
    } else if (inventory !== undefined) {
      // Serve sends its last inventory first, which serve's start must not miss.
      held = { childId, ...line };
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
          ownEvent(params);
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

  return {
    childId,
    close: (reason) => {
      closed = reason;
      for (const { answer } of pending.values()) {
        answer({ closed: reason });
      }
      pending.clear();
    },
    instanceId: () => instanceId,
    notify: (method, params) => {
      if (closed === undefined) {
        send({ method, params });
      }
    },
    receive,
    release: () => {
      released = true;
      if (held !== undefined) {
        emitLine(held);
        held = undefined;
      }
    },
    request: async (id, method, params, onEvent) => {
      if (closed !== undefined) {
        return { closed };
      }
      const answer = slot<Answer>();
      pending.set(id, { answer: answer.put, onEvent });
      send(params === undefined ? { id, method } : { id, method, params });
      return (await answer.take()) ?? { closed: "no answer" };
    },
  };
};

/**
 * Runs, cancels and serve's requests over the connection `connection` gives, which it opens when
 * needed; `current` gives the connection serve was started on, if any.
 */
export const rpcCalls = ({
  connection,
  current,
  emitLine,
  onRunEnd,
}: {
  connection: () => Promise<Peer>;
  current: () => Promise<Peer> | undefined;
  emitLine: (line: ServeLine) => void;
  onRunEnd: (end: { runId: string; exitCode: number | null }) => void;
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
          (params) => {
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
  const refreshed = async (socket: Peer, requestId: string) => {
    const answer = await socket.request(requestId, "refresh");
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
  const searched = async (socket: Peer, requestId: string, query: string) => {
    const answer = await socket.request(requestId, "search", { query });
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

  /** Sends a request to serve, and fails when serve was not started, as with no serve child. */
  const request = async (r: ServeRequest): Promise<void> => {
    const started = current();
    if (started === undefined) {
      throw new Error("agentx serve is not running");
    }
    const socket = await started;
    void (r.type === "refresh"
      ? refreshed(socket, r.request_id)
      : searched(socket, r.request_id, r.query));
  };
  return { cancel, request, run };
};
