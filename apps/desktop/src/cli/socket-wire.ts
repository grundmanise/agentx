// The wire in a browser: JSON-RPC 2.0 over the WebSocket door of `agentx serve --listen`
// (docs/spec/cli-contract.md, The WebSocket door). The page asks its own server where the door is:
// GET /agentx-serve.json answers the url and token of serve's `listening` event, as
// `pnpm dev:serve` does. One connection carries every run and request of the page, and stands
// for the serve child of the Tauri window: each connection has a child id of its own, a closed one
// is a child that ended, and the next one is its restart, with the supervisor's backoff. The
// JSON-RPC itself is rpc-client.ts.
import { isObject, peer, rpcCalls, wireEvents } from "#/cli/rpc-client.ts";
import type { Peer } from "#/cli/rpc-client.ts";
import type { Resolution, Wire } from "#/cli/transport.ts";
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

const isDoor = (value: unknown): value is Door =>
  isObject(value) &&
  typeof value.url === "string" &&
  typeof value.token === "string";

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

const events = wireEvents();

/** Whether serve was started: its own events are passed on, and a closed connection reopens. */
let started = false;

/** Opens a connection; `onClose` is called when an open one closes. */
const open = async (
  childId: number,
  onClose: (reason: string, openedAt: number) => void
): Promise<Peer> => {
  const door = await findDoor();
  if (door === undefined) {
    throw new Error(
      `no agentx serve answers this page: ${DOOR_PATH} names none, so start the page with pnpm dev:serve`
    );
  }
  const ws = new WebSocket(
    `${door.url}?token=${encodeURIComponent(door.token)}`
  );
  const connected = peer(
    childId,
    (message) => {
      ws.send(JSON.stringify({ jsonrpc: "2.0", ...message }));
    },
    started,
    events.emitLine
  );
  ws.addEventListener("message", ({ data }) => {
    connected.receive(data);
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
    const reason = `the connection to agentx serve closed with code ${code}`;
    connected.close(reason);
    onClose(reason, openedAt);
  });
  return connected;
};

/** The connection open or opening, if any, and its child id. */
let current: Promise<Peer> | undefined;
let currentChild = 0;
/** The child id of the next connection; the first is 1, as the supervisor's first child is. */
let nextChild = 1;
let delay = BACKOFF_FIRST_MS;
let reopening: ReturnType<typeof setTimeout> | undefined;

/** The connection, opened when there is none. */
const connection = async (): Promise<Peer> => {
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
    events.emitExit({
      childId,
      exitCode: null,
      restartInMs: wait,
      restarting: true,
      stderr,
    });
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

const calls = rpcCalls({
  connection,
  current: (): Promise<Peer> | undefined => (started ? current : undefined),
  emitLine: events.emitLine,
  onRunEnd: events.emitRunEnd,
  onStart: () => {
    started = true;
  },
});

export const socketWire: Wire = {
  cancel: calls.cancel,
  onRunEnd: events.onRunEnd,
  resolve,
  run: calls.run,
  serve: {
    logPath: () => {
      throw new Error("the serve log is not reachable from a browser");
    },
    onEvent: events.onLine,
    onExit: events.onExit,
    request: calls.request,
    start: calls.start,
    verbose: () => {
      throw new Error("serve's log level cannot be set from a browser");
    },
  },
};
