// The wire inside the Tauri window: the commands and events of the Rust supervisor, which runs
// each one-shot as a process of its own and keeps one serve child open, writing its requests to
// the child's stdin.
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import type { UnlistenFn } from "@tauri-apps/api/event";

import type { CliEvent, LogEvent, ServeRequest } from "#/cli/events.gen.ts";
import type {
  InvalidLine,
  Resolution,
  RunLine,
  RunResult,
  ServeExit,
  ServeLine,
  Wire,
} from "#/cli/transport.ts";
import { slot } from "#/lib/slot.ts";

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

const resolve = async (): Promise<Resolution> => {
  const r = await invoke<WireResolution>("cli_resolve");
  return {
    found: r.found,
    path: r.path,
  };
};

/** Each run's subscription to its events, until it ends. The run is sent once it is in place. */
const subscribed = new Map<string, Promise<UnlistenFn[]>>();

const run: Wire["run"] = async (runId, args, deadlineMs, onLine) => {
  const end = slot<WireRunEnd>();
  const subscribe = async (): Promise<UnlistenFn[]> => {
    const settled = await Promise.allSettled([
      listen<WireRunLine>("cli-run", ({ payload }) => {
        if (payload.run_id === runId) {
          const { run_id: _, ...line } = payload;
          onLine(line);
        }
      }),
      listen<WireRunEnd>("cli-run-end", ({ payload }) => {
        if (payload.run_id === runId) {
          end.put(payload);
        }
      }),
    ]);
    const stops = settled.flatMap((s) =>
      s.status === "fulfilled" ? [s.value] : []
    );
    const failed = settled.find((s) => s.status === "rejected");
    if (failed !== undefined) {
      // One listener may be in place while the other failed: it goes with the run.
      for (const stop of stops) {
        stop();
      }
      throw new Error(`cannot listen to the run: ${String(failed.reason)}`);
    }
    return stops;
  };
  const subscription = subscribe();
  subscribed.set(runId, subscription);
  let unlisten: UnlistenFn[] = [];
  try {
    // Inside the try: a run that cannot be listened to ends as one that did not start.
    unlisten = await subscription;
    await invoke("cli_run", {
      args,
      deadline_ms: deadlineMs ?? null,
      run_id: runId,
    });
    // The end event follows every line event; the command's own answer can overtake them.
    const ended = await end.take();
    if (ended === undefined) {
      throw new Error(`run ${runId} ended without its end event`);
    }
    return {
      cancelled: ended.cancelled,
      events: ended.events,
      exitCode: ended.exit_code,
      invalid: ended.invalid,
      logs: ended.logs,
      timedOut: ended.timed_out,
    } satisfies RunResult;
  } finally {
    subscribed.delete(runId);
    for (const stop of unlisten) {
      stop();
    }
  }
};

/** It is sent after the run itself, so that the supervisor knows the run. */
const cancel = async (runId: string): Promise<boolean> => {
  await subscribed.get(runId);
  return await invoke<boolean>("cli_cancel", { run_id: runId });
};

const onRunEnd: Wire["onRunEnd"] = async (handle) =>
  await listen<WireRunEnd>("cli-run-end", ({ payload }) => {
    handle({ exitCode: payload.exit_code, runId: payload.run_id });
  });

const onServeLine = async (
  handle: (line: ServeLine) => void
): Promise<UnlistenFn> =>
  await listen<WireServeLine>("serve-event", ({ payload }) => {
    const { child_id: childId, ...line } = payload;
    handle({ childId, ...line });
  });

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

export const tauriWire: Wire = {
  cancel,
  onRunEnd,
  resolve,
  run,
  serve: {
    logPath: async () => await invoke<string>("serve_log_path"),
    onEvent: onServeLine,
    onExit: onServeExit,
    request: async (request: ServeRequest) => {
      await invoke("serve_request", { line: JSON.stringify(request) });
    },
    start: async () => await invoke<number>("serve_start"),
    verbose: async (on: boolean) => {
      await invoke("serve_verbose", { on });
    },
  },
};
