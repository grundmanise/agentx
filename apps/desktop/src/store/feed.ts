// The serve child's events into the store, by the inventory ordering contract
// (docs/spec/inventory-ordering.md). Only the active child counts: a later child replaces it, and
// the events of a child that ended or was replaced are dropped. Within one serve instance an
// inventory applies only when its scan counter goes up; a new instance applies whatever it sends
// first, since counters are never compared across instances. What serve notices goes to the
// activity list: its drift, reconcile and update events, and the warnings it logs on stderr.
import type {
  CliEvent,
  DriftEvent,
  InventoryEvent,
  LogEvent,
  RefreshCompleteEvent,
  SearchEvent,
  SearchResult,
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

/** What a search resolves with: the results, or undefined when it was not answered. */
type Answer = SearchResult[] | undefined;

/**
 * One answer handed from an event to whoever waits for it, as the transport hands a run its end:
 * a stream of one chunk, read once.
 */
const slot = () => {
  let controller: ReadableStreamDefaultController<Answer> | undefined;
  const stream = new ReadableStream<Answer>({
    start: (c) => {
      controller = c;
    },
  });
  return {
    put: (value?: SearchResult[]) => {
      controller?.enqueue(value);
      controller?.close();
      controller = undefined;
    },
    take: async (): Promise<Answer> => {
      const read = await stream.getReader().read();
      return read.value;
    },
  };
};

/** The search request last sent and not answered yet: the one whose answer counts. */
let latestSearch:
  | { requestId: string; answer: ReturnType<typeof slot> }
  | undefined;

/** Gives up on the search request waiting, if any: its answer, should it come, is dropped. */
const dropSearch = () => {
  latestSearch?.answer.put();
  latestSearch = undefined;
};

/**
 * Asks the serve child to search every source for `query` (docs/spec/cli-contract.md, Serve), and
 * resolves with the matching `search` event's results. Only the latest request counts: sending
 * another resolves the one before with undefined, and an answer to an older request id is dropped.
 * A serve child that is not running answers nothing, so the search resolves with undefined.
 */
export const search = async (query: string): Promise<Answer> => {
  dropSearch();
  const requestId = crypto.randomUUID();
  const answer = slot();
  latestSearch = { answer, requestId };
  try {
    await serve.request({ query, request_id: requestId, type: "search" });
  } catch {
    if (latestSearch.requestId === requestId) {
      dropSearch();
    }
  }
  return await answer.take();
};

/** Answers the search request waiting, when the event is its; an older request's answer is dropped. */
const answerSearch = (event: SearchEvent) => {
  if (
    latestSearch !== undefined &&
    latestSearch.requestId === event.request_id
  ) {
    latestSearch.answer.put(event.results);
    latestSearch = undefined;
  }
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
  // A search the child before did not answer is never answered: the new child's index is asked anew.
  dropSearch();
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

/** What a drift change reads in the activity list: "{skill} is now {state}". */
const driftText = (e: DriftEvent) => {
  const now = e.drift.length > 0 ? e.drift.join(", ") : e.state;
  return now === undefined ? `${e.name} changed` : `${e.name} is now ${now}`;
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
  } else if (event.type === "search") {
    answerSearch(event);
  } else if (event.type === "reconcile") {
    set((s) => ({ reconciles: [...s.reconciles, event] }));
    // A folder put back is something agentx did; a folder missing or in the way is a card in the
    // Inbox, which says it in its own words; the other outcomes are noted as the CLI names them.
    if (event.outcome === "repaired") {
      addActivity("success", `Put ${event.name} back`);
    } else if (
      !["restored", "worktree missing", "adopt candidate"].includes(
        event.outcome
      )
    ) {
      addActivity("attention", `${event.name}: ${event.outcome}`);
    }
  } else if (event.type === "update_available") {
    set((s) => ({ updates: { ...s.updates, [event.name]: event } }));
    addActivity("success", `${event.name} has an update`);
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

/**
 * A warning serve logged is something the user should know about, such as a source the update
 * check could not fetch or a fork it could not repair. Debug lines, and whatever level a later
 * CLI adds, stay in the serve log.
 */
const applyLog = (event: LogEvent) => {
  if (event.level === "warn") {
    addActivity("attention", event.message);
  }
};

const onLine = (line: ServeLine) => {
  if (line.childId < active || line.childId <= ended) {
    return;
  }
  activate(line.childId);
  if (line.kind === "event") {
    apply(line.childId, line.event);
  } else if (line.kind === "log") {
    applyLog(line.event);
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
