// The serve child's events into the store, by the inventory ordering contract
// (docs/spec/inventory-ordering.md). Only the active child counts: a later child replaces it, and
// the events of a child that ended or was replaced are dropped. Within one serve instance an
// inventory applies only when its scan counter goes up; a new instance applies whatever it sends
// first, since counters are never compared across instances.
import type {
  CliEvent,
  DriftEvent,
  InventoryEvent,
  RefreshCompleteEvent,
} from "#/cli/events.gen.ts";
import { serve } from "#/cli/transport.ts";
import type { ServeExit, ServeLine } from "#/cli/transport.ts";
import { addActivity, useAppStore } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The serve child whose events count; 0 before the first one starts. */
let active = 0;
/** The last serve child that ended. */
let ended = 0;
/** Each child's last `error` event: why it ended, if it does. */
const errors = new Map<number, Problem>();

const set = useAppStore.setState;

/** Sends a refresh request to the active child; one that cannot be sent waits for the next child. */
const send = async (requestId: string) => {
  set((s) => ({ refreshes: { ...s.refreshes, [requestId]: active } }));
  try {
    await serve.request({ request_id: requestId, type: "refresh" });
  } catch {
    // Serve is not running: a restarted child gets the request again.
  }
};

/** Asks the serve child to scan again, as after every command that may have changed something. */
export const refresh = async () => {
  await send(crypto.randomUUID());
};

/**
 * Makes `childId` the active child, unless a later one already is. The inventory shown stays until
 * the new child sends its own, and the requests sent to earlier children go to it with new ids.
 */
const activate = (childId: number) => {
  if (childId <= active) {
    return;
  }
  active = childId;
  const { refreshes } = useAppStore.getState();
  const unanswered = Object.keys(refreshes).filter(
    (id) => (refreshes[id] ?? 0) < childId
  );
  set({
    reconciles: [],
    refreshes: Object.fromEntries(
      Object.entries(refreshes).filter(([id]) => !unanswered.includes(id))
    ),
    updates: {},
  });
  void Promise.all(unanswered.map(refresh));
};

/** Starts the serve child unless one runs, and makes it the active one. */
export const startServe = async (): Promise<number> => {
  const childId = await serve.start();
  activate(childId);
  return childId;
};

/** Retry on the stale bar: starts serve if it ended for good, then asks it to scan again. */
export const retryRefresh = async () => {
  try {
    await startServe();
  } catch (error) {
    set({ freshness: { error: String(error), kind: "failed" } });
    return;
  }
  await refresh();
};

const driftText = (e: DriftEvent) => {
  const now = e.drift.length > 0 ? e.drift.join(", ") : e.state;
  return now === undefined ? `${e.name} changed` : `${e.name}: ${now}`;
};

/** Applies an inventory, unless the same instance already applied one with a counter as high. */
const applyInventory = (childId: number, event: InventoryEvent) => {
  const { applied } = useAppStore.getState();
  const sameInstance =
    applied !== undefined &&
    applied.childId === childId &&
    applied.instanceId === event.instance_id;
  if (sameInstance && event.scan_counter <= applied.counter) {
    return;
  }
  set({
    applied: {
      childId,
      counter: event.scan_counter,
      instanceId: event.instance_id,
    },
    freshness: { kind: "fresh" },
    inventory: event,
  });
};

/** Clears an answered request, even when the inventory did not change. */
const acknowledge = (event: RefreshCompleteEvent) => {
  const { refreshes } = useAppStore.getState();
  if (!Object.hasOwn(refreshes, event.request_id)) {
    return;
  }
  const { [event.request_id]: _, ...rest } = refreshes;
  set({
    freshness: event.ok
      ? { kind: "fresh" }
      : { error: event.error ?? "the scan failed", kind: "failed" },
    refreshes: rest,
  });
};

const apply = (childId: number, event: CliEvent) => {
  if (event.type === "inventory") {
    applyInventory(childId, event);
  } else if (event.type === "refresh_complete") {
    acknowledge(event);
  } else if (event.type === "reconcile") {
    set((s) => ({ reconciles: [...s.reconciles, event] }));
    if (event.outcome !== "restored") {
      addActivity(
        event.outcome === "repaired" ? "success" : "attention",
        `${event.name}: ${event.outcome}`
      );
    }
  } else if (event.type === "update_available") {
    set((s) => ({ updates: { ...s.updates, [event.name]: event } }));
    addActivity("success", `Update available for ${event.name}`);
  } else if (event.type === "drift") {
    addActivity("attention", driftText(event));
  } else if (event.type === "error") {
    errors.set(
      childId,
      event.hint === undefined
        ? { message: event.message }
        : { hint: event.hint, message: event.message }
    );
  }
};

const onLine = (line: ServeLine) => {
  if (line.childId < active || line.childId <= ended) {
    return;
  }
  activate(line.childId);
  if (line.kind === "event") {
    apply(line.childId, line.event);
  }
};

const onExit = (exit: ServeExit) => {
  ended = Math.max(ended, exit.childId);
  const code = exit.exitCode === null ? "a signal" : `code ${exit.exitCode}`;
  const problem = errors.get(exit.childId) ?? {
    message: exit.stderr.at(-1) ?? `agentx serve stopped with ${code}`,
  };
  errors.delete(exit.childId);
  set({
    serveEnd: {
      childId: exit.childId,
      exitCode: exit.exitCode,
      problem,
      restarting: exit.restarting,
    },
  });
  // A restarted child is active only from its first line: one that ends before it writes any
  // still says whether serve comes back.
  if (exit.childId >= active) {
    set({
      freshness: exit.restarting
        ? { kind: "stale" }
        : { error: problem.message, kind: "failed" },
    });
  }
};

let connected = false;

/** Feeds the store from every serve child, from now on. Called once, before serve starts. */
export const connect = async () => {
  if (connected) {
    return;
  }
  connected = true;
  await serve.onEvent(onLine);
  await serve.onExit(onExit);
};
