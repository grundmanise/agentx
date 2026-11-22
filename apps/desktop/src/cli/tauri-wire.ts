// The wire inside the Tauri window: JSON-RPC 2.0 with the serve child the Rust supervisor keeps
// open, over the child's stdin and stdout (docs/spec/cli-contract.md, The WebSocket door, whose
// messages stdio carries too). The supervisor writes each message to the child's stdin as one
// line (`serve_request`), and emits each line the child writes as a `serve-event` and its end as
// a `serve-exit`, each with the child's id. Every command is a `run` request, so the first one
// starts serve. Each child is one connection of rpc-client.ts: the supervisor's restart is a new
// one, and a child that ends fails the requests it did not answer.
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";

import type { LogEvent } from "#/cli/events.gen.ts";
import { isObject, peer, rpcCalls, wireEvents } from "#/cli/rpc-client.ts";
import type { Connected, Peer } from "#/cli/rpc-client.ts";
import type {
  InvalidLine,
  Resolution,
  ServeExit,
  Wire,
} from "#/cli/transport.ts";
import { lastStderr } from "#/store/activity-log.ts";

// What the supervisor sends, as Rust names it. An object the child wrote on stdout is an event,
// which under --json is a JSON-RPC message.
type WireServeLine = (
  | { kind: "event"; event: unknown }
  | { kind: "log"; event: LogEvent }
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

const events = wireEvents();

/** Whether serve was started: a child's own lines are passed on from its first one. */
let started = false;
/**
 * Each child's connection, by child id. One that ended stays, closed, so that a request to it
 * fails at once with how it ended.
 */
const peers = new Map<number, Connected>();
/** The latest child known: the one serve's requests go to. */
let latest = 0;
/** The children that ended, by child id. */
const ended = new Set<number>();

/** Writes one message to the child's stdin, as one line. It fails when no child runs. */
const send = async (message: object): Promise<void> => {
  await invoke("serve_request", {
    line: JSON.stringify({ jsonrpc: "2.0", ...message }),
  });
};

const peerOf = (childId: number): Connected => {
  const known = peers.get(childId);
  if (known !== undefined) {
    return known;
  }
  const created = peer(childId, send, started, events.emitLine);
  peers.set(childId, created);
  latest = Math.max(latest, childId);
  return created;
};

const isRpcMessage = (value: unknown): value is Record<string, unknown> =>
  isObject(value) && value.jsonrpc === "2.0";

/** How a child ended, as the requests it did not answer report it. */
const endOf = (exit: ServeExit): string => {
  const how =
    exit.exitCode === null
      ? "agentx serve was stopped by a signal"
      : `agentx serve exited with code ${exit.exitCode}`;
  const last = lastStderr(exit.stderr);
  return last === undefined ? how : `${how}: ${last}`;
};

const subscribe = async () => {
  await listen<WireServeLine>("serve-event", ({ payload }) => {
    const { child_id: childId, ...line } = payload;
    const connected = peerOf(childId);
    if (line.kind !== "event") {
      connected.own(line);
    } else if (isRpcMessage(line.event)) {
      connected.receive(line.event);
    } else {
      // Under --json serve writes JSON-RPC alone on stdout: anything else is an invalid line.
      connected.own({
        kind: "invalid",
        line: JSON.stringify(line.event),
        stream: "stdout",
      });
    }
  });
  await listen<WireServeExit>("serve-exit", ({ payload }) => {
    const exit: ServeExit = {
      childId: payload.child_id,
      exitCode: payload.exit_code,
      restartInMs: payload.restart_in_ms,
      restarting: payload.restarting,
      stderr: payload.stderr,
    };
    ended.add(exit.childId);
    peerOf(exit.childId).close(endOf(exit));
    events.emitExit(exit);
  });
};

let listening: Promise<void> | undefined;

/** Listens to the supervisor's events, once: before the first child starts. */
const listened = async () => {
  listening ??= subscribe();
  await listening;
};

/**
 * The serve child's connection: the latest child's while it runs, else the one `serve_start`
 * starts and gives the id of. While the supervisor waits to restart a child that ended, it starts
 * the next one at once.
 */
const connection = async (): Promise<Connected> => {
  await listened();
  if (latest > 0 && !ended.has(latest)) {
    return peerOf(latest);
  }
  return peerOf(await invoke<number>("serve_start"));
};

const resolve = async (): Promise<Resolution> => {
  const r = await invoke<WireResolution>("cli_resolve");
  return {
    found: r.found,
    path: r.path,
  };
};

const calls = rpcCalls({
  connection,
  current: (): Promise<Peer> | undefined =>
    started && latest > 0 ? Promise.resolve(peerOf(latest)) : undefined,
  emitLine: events.emitLine,
  onRunEnd: events.emitRunEnd,
  onStart: () => {
    started = true;
  },
});

export const tauriWire: Wire = {
  cancel: calls.cancel,
  onRunEnd: events.onRunEnd,
  resolve,
  run: calls.run,
  serve: {
    logPath: async () => await invoke<string>("serve_log_path"),
    onEvent: async (handle) => {
      await listened();
      return events.onLine(handle);
    },
    onExit: async (handle) => {
      await listened();
      return events.onExit(handle);
    },
    request: calls.request,
    start: calls.start,
    verbose: async (on: boolean) => {
      await invoke("serve_verbose", { on });
    },
  },
};
