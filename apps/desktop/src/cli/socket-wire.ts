// The wire in a browser: JSON-RPC 2.0 over the WebSocket door of `agentx serve --listen`
// (docs/spec/cli-contract.md, The WebSocket door). The page asks its own server where the door is:
// GET /agentx-serve.json answers the url and token of serve's `listening` event, as
// `pnpm dev:browser` does. One connection carries every run and request of the page, and stands
// for the serve child of the Tauri window: each connection has a child id of its own, a closed one
// is a child that ended, and the next one is its restart, with the supervisor's backoff. Serve
// answers `refresh` and `search` with results; they reach the store as the `refresh_complete` and
// `search` events the serve child writes on its stdout, so the store reads both wires alike.
import { SCHEMA_VERSION } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  LogEvent,
  SearchResult,
  ServeRequest,
} from "#/cli/events.gen.ts";
import type {
  Resolution,
  RunLine,
  RunResult,
  ServeExit,
  ServeLine,
  Wire,
} from "#/cli/transport.ts";
import { slot } from "#/lib/slot.ts";

/** The path, on the page's own server, that answers where serve's door is. */
const DOOR_PATH = "/agentx-serve.json";

// The restart backoff of the supervisor (src-tauri/src/supervisor.rs): the first delay, the cap,
// and how long a connection must have stayed open for the next delay to start from the first.
const BACKOFF_FIRST_MS = 500;
const BACKOFF_CAP_MS = 30_000;
const BACKOFF_RESET_MS = 60_000;

/** Where serve's door is, as its `listening` event says. */
interface Door {
  url: string;
  token: string;
}

/** The params of an `event` notification. */
interface EventParams {
  id?: string | number;
  stream: "stdout" | "stderr";
  event?: unknown;
  line?: unknown;
}

/** How serve answered a request, or that the connection closed before it did. */
type Answer =
  | { result: unknown }
  | { error: { code: number; message: string } }
  | { closed: string };

/** One open connection to the door. */
interface Socket {
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

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const isDoor = (value: unknown): value is Door =>
  isObject(value) &&
  typeof value.url === "string" &&
  typeof value.token === "string";

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

/** Where the door is; undefined when the page's server knows of no serve. */
const findDoor = async (): Promise<Door | undefined> => {
  try {
    const response = await fetch(DOOR_PATH, {
      cache: "no-store",
      headers: { Accept: "application/json" },
    });
    if (!response.ok) {
      return undefined;
    }
    const body: unknown = await response.json();
    return isDoor(body) ? body : undefined;
  } catch {
    return undefined;
  }
};

const lineHandlers = new Set<(line: ServeLine) => void>();
const exitHandlers = new Set<(exit: ServeExit) => void>();
const runEndHandlers = new Set<
  (end: { runId: string; exitCode: number | null }) => void
>();

const emitLine = (line: ServeLine) => {
  for (const handle of lineHandlers) {
    handle(line);
  }
};

/** Whether serve was started: its own events are passed on, and a closed connection reopens. */
let started = false;

/** Opens a connection; `onClose` is called when an open one closes. */
const open = async (
  childId: number,
  onClose: (reason: string, openedAt: number) => void
): Promise<Socket> => {
  const door = await findDoor();
  if (door === undefined) {
    throw new Error(
      `no agentx serve answers this page: ${DOOR_PATH} names none, so start the page with pnpm dev:browser`
    );
  }
  const ws = new WebSocket(
    `${door.url}?token=${encodeURIComponent(door.token)}`
  );
  const pending = new Map<
    string | number,
    { answer: (answer: Answer) => void; onEvent?: (p: EventParams) => void }
  >();
  let closed: string | undefined;
  let instanceId = "";
  let released = started;
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

  ws.addEventListener("message", ({ data }) => {
    if (typeof data !== "string") {
      return;
    }
    let message: unknown;
    try {
      message = JSON.parse(data);
    } catch {
      return;
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
  });

  const opened = slot<boolean>();
  ws.addEventListener("open", () => {
    opened.put(true);
  });
  ws.addEventListener("close", () => {
    opened.put(false);
  });
  if ((await opened.take()) !== true) {
    throw new Error(`cannot connect to agentx serve at ${door.url}`);
  }
  const openedAt = Date.now();

  ws.addEventListener("close", ({ code }) => {
    closed = `the connection to agentx serve closed with code ${code}`;
    for (const { answer } of pending.values()) {
      answer({ closed });
    }
    pending.clear();
    onClose(closed, openedAt);
  });

  const send = (message: object) => {
    ws.send(JSON.stringify({ jsonrpc: "2.0", ...message }));
  };
  return {
    childId,
    instanceId: () => instanceId,
    notify: (method, params) => {
      if (closed === undefined) {
        send({ method, params });
      }
    },
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

/** The connection open or opening, if any, and its child id. */
let current: Promise<Socket> | undefined;
let currentChild = 0;
/** The child id of the next connection; the first is 1, as the supervisor's first child is. */
let nextChild = 1;
let delay = BACKOFF_FIRST_MS;
let reopening: ReturnType<typeof setTimeout> | undefined;

/** The connection, opened when there is none. */
const connection = async (): Promise<Socket> => {
  if (current !== undefined) {
    return await current;
  }
  clearTimeout(reopening);
  reopening = undefined;
  const childId = nextChild;
  nextChild += 1;

  /** Reports this connection as a serve child that ended, and opens another after the delay. */
  const restart = (stderr: string[]) => {
    const wait = delay;
    delay = Math.min(delay * 2, BACKOFF_CAP_MS);
    for (const handle of exitHandlers) {
      handle({
        childId,
        exitCode: null,
        restartInMs: wait,
        restarting: true,
        stderr,
      });
    }
    reopening = setTimeout(() => {
      reopening = undefined;
      void (async () => {
        try {
          await connection();
        } catch {
          // A connection that cannot open restarts itself.
        }
      })();
    }, wait);
  };

  const opening = open(childId, (reason, openedAt) => {
    if (currentChild !== childId) {
      return;
    }
    current = undefined;
    if (Date.now() - openedAt >= BACKOFF_RESET_MS) {
      delay = BACKOFF_FIRST_MS;
    }
    if (started) {
      restart([reason]);
    }
  });
  current = opening;
  currentChild = childId;
  try {
    return await opening;
  } catch (error) {
    if (currentChild === childId) {
      current = undefined;
      // While serve is started, a connection that cannot open is a child that could not start,
      // and is tried again.
      if (started) {
        restart([String(error)]);
      }
    }
    throw error;
  }
};

const resolve = async (): Promise<Resolution> => {
  const door = await findDoor();
  return door === undefined
    ? { found: null, path: null }
    : { found: "serve", path: door.url };
};

/** The runs going, by run id: whether one was cancelled, and the connection it was sent on. */
const runs = new Map<string, { cancelled: boolean; socket?: Socket }>();

const run: Wire["run"] = async (runId, args, deadlineMs, onLine) => {
  const going: { cancelled: boolean; socket?: Socket } = { cancelled: false };
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
  for (const handle of runEndHandlers) {
    handle({ exitCode: result.exitCode, runId });
  }
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
const refreshed = async (socket: Socket, requestId: string) => {
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
const searched = async (socket: Socket, requestId: string, query: string) => {
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
  if (!started || current === undefined) {
    throw new Error("agentx serve is not running");
  }
  const socket = await current;
  void (r.type === "refresh"
    ? refreshed(socket, r.request_id)
    : searched(socket, r.request_id, r.query));
};

const start = async (): Promise<number> => {
  const socket = await connection();
  started = true;
  socket.release();
  return socket.childId;
};

/** Adds `handle` to `handlers`, and gives the function that takes it away. */
const subscribe =
  <T>(handlers: Set<T>) =>
  (handle: T) => {
    handlers.add(handle);
    return () => {
      handlers.delete(handle);
    };
  };

export const socketWire: Wire = {
  cancel,
  onRunEnd: subscribe(runEndHandlers),
  resolve,
  run,
  serve: {
    logPath: () => {
      throw new Error("the serve log is not reachable from a browser");
    },
    onEvent: subscribe(lineHandlers),
    onExit: subscribe(exitHandlers),
    request,
    start,
    verbose: () => {
      throw new Error("serve's log level cannot be set from a browser");
    },
  },
};
