// The mocked CLI transport. The tests run against the Vite dev server, which has no Tauri runtime,
// so this fixture stands in for the Rust supervisor under the app's real transport module
// (src/cli/transport.ts): the page's Tauri calls come here, to the test, which answers them with
// the events it registered and records every command line, cancel and serve request.
//
//   import { expect, test } from "./harness/cli.ts";
//   test("…", async ({ cli, page }) => {
//     cli.register(["version"], { events: [{ type: "version", cli_version: "1.2.0" }] });
//     …
//     expect(cli.commands).toEqual([["version", "--json"]]);
//   });
//
// A command line the CLI would refuse (by src/cli/commands.gen.ts), or one with no registered
// reply, fails the test once it ends, unless the test takes it with takeRefusals().
//
// The app checks the machine on launch (src/startup/startup.ts), so the mock starts out with a
// passing launch: `version` and `doctor` replies, and a serve child that writes its first snapshot
// when it starts. A test replaces them with register() and onServeStart().
import { once } from "node:events";

import { test as base } from "@playwright/test";
import type { Page } from "@playwright/test";

import { SCHEMA_VERSION } from "#/cli/events.gen.ts";
import type { CliEventType, EventOf, LogEvent } from "#/cli/events.gen.ts";

import { refusal } from "./command-line.ts";

/** An event with only the fields a test checks. The mock adds schema_version. */
export type InlineEvent = {
  [T in CliEventType]: { type: T } & Partial<Omit<EventOf<T>, "type">>;
}[CliEventType];

export interface Reply {
  /** The events on stdout, in order. */
  events?: InlineEvent[];
  /** The log events on stderr, sent after the events. */
  logs?: Partial<Omit<LogEvent, "type">>[];
  exitCode?: number;
  /** Holds the run after this many events, until release() or a cancel. */
  holdAfter?: number;
}

export interface Resolution {
  path: string | null;
  found: "bundled" | "path" | null;
}

/** What a serve child does when the app starts it: writes these events, then exits if asked to. */
export interface ServeStart {
  events?: InlineEvent[];
  /** Ends the child after its events; the supervisor restarts it unless the code is 2, 6 or 8. */
  exit?: { code: number; stderr?: string[] };
}

/** A configuration as a snapshot lists it. */
export const configuration = (id: string) => ({
  client: id,
  enabled: true,
  id,
  logical_id: id,
  name: id,
  path: `/home/me/.${id}`,
  physical_id: id,
  reads_library: false,
});

/** A serve child's first snapshot, with these configurations. */
export const firstSnapshot = (configurations = ["claude-code", "codex"]) =>
  ({
    configurations: configurations.map(configuration),
    library: [],
    scan_counter: 1,
    type: "snapshot",
  }) satisfies InlineEvent;

/** Doctor's rows on a machine where everything passes. */
export const passingDoctor: InlineEvent[] = [
  { check: "git", detail: "git 2.43.0", status: "ok", type: "doctor" },
  {
    check: "fork_merges",
    detail: "merge-tree works",
    status: "ok",
    type: "doctor",
  },
  {
    check: "commit_identity",
    detail: "commit ids match",
    status: "ok",
    type: "doctor",
  },
  { check: "home", detail: "/home/me/.agentx", status: "ok", type: "doctor" },
  { check: "lock", detail: "free", status: "ok", type: "doctor" },
  {
    check: "mutations",
    detail: "none unfinished",
    status: "ok",
    type: "doctor",
  },
  {
    check: "settings",
    detail: "/home/me/.agentx/settings.json",
    status: "ok",
    type: "doctor",
  },
  {
    check: "account_repo",
    detail: "/home/me/.agentx/account.git",
    status: "ok",
    type: "doctor",
  },
  {
    check: "library",
    detail: "/home/me/.agents/skills",
    status: "ok",
    type: "doctor",
  },
  {
    check: "clients",
    detail: "2 of 12 registered clients detected",
    status: "info",
    type: "doctor",
  },
];

/** The CLI version a passing launch reports. */
export const cliVersion = "1.2.0";

/** Doctor's passing rows with `changes` put in place of the rows of the same check. */
export const doctorWith = (...changes: InlineEvent[]): InlineEvent[] =>
  passingDoctor.map(
    (row) =>
      changes.find(
        (c) =>
          c.type === "doctor" && row.type === "doctor" && c.check === row.check
      ) ?? row
  );

/** A registered reply, and for one that holds, what releases it. */
export interface Registered {
  /** Sends the rest of the events and ends the run. */
  release: () => void;
  /** Resolves with the run id once the app has run the command line. */
  started: Promise<string>;
}

declare global {
  interface Window {
    agentxMockCli: (command: string, args: unknown) => Promise<unknown>;
    agentxEmit: (event: string, payload: unknown) => void;
  }
}

// The page side. It replaces the Tauri runtime's IPC and event plumbing, as mockIPC does with its
// shouldMockEvents option, and passes every other command to the test through the exposed binding.
const installInPage = () => {
  const handlers = new Map<number, (data: unknown) => void>();
  const listeners = new Map<string, number[]>();
  let next = 1;
  const unregister = (id: number) => {
    handlers.delete(id);
  };
  Object.assign(window, {
    __TAURI_EVENT_PLUGIN_INTERNALS__: {
      unregisterListener: (_event: string, id: number) => {
        unregister(id);
      },
    },
    __TAURI_INTERNALS__: {
      invoke: async (command: string, args: Record<string, unknown> = {}) => {
        const event = String(args.event);
        if (command === "plugin:event|listen") {
          const handler = Number(args.handler);
          listeners.set(event, [...(listeners.get(event) ?? []), handler]);
          return handler;
        }
        if (command === "plugin:event|unlisten") {
          const id = Number(args.eventId);
          listeners.set(
            event,
            (listeners.get(event) ?? []).filter((h) => h !== id)
          );
          return null;
        }
        return await window.agentxMockCli(command, args);
      },
      metadata: {
        currentWebview: { label: "main", windowLabel: "main" },
        currentWindow: { label: "main" },
      },
      transformCallback: (
        handler?: (data: unknown) => void,
        onlyOnce = false
      ) => {
        const id = next;
        next += 1;
        handlers.set(id, (data) => {
          if (onlyOnce) {
            unregister(id);
          }
          handler?.(data);
        });
        return id;
      },
      unregisterCallback: unregister,
    },
  });
  let eventId = 1;
  window.agentxEmit = (event, payload) => {
    for (const id of listeners.get(event) ?? []) {
      handlers.get(id)?.({ event, id: eventId, payload });
      eventId += 1;
    }
  };
};

/** The event as the CLI writes it. */
const full = <E extends object>(event: E) => ({
  schema_version: SCHEMA_VERSION,
  ...event,
});

const line = (args: readonly string[]) => args.join(" ");

const field = (args: unknown, key: string): unknown =>
  typeof args === "object" && args !== null
    ? Reflect.get(args, key)
    : undefined;

interface Pending {
  reply: Reply;
  /** Dispatches "started" with the run id, and "resume" with "release" or "cancel". */
  signals: EventTarget;
}

/** The detail of the first `type` event `target` dispatches from now. */
const nextDetail = async (
  target: EventTarget,
  type: string
): Promise<unknown> => {
  const dispatched: unknown[] = await once(target, type);
  const [event] = dispatched;
  return event instanceof CustomEvent ? event.detail : undefined;
};

export class MockCli {
  /** Every command line, as the supervisor would run it (with --json). */
  readonly commands: string[][] = [];
  /** The run id of every cancel. */
  readonly cancels: string[] = [];
  /** Every line written to serve's stdin. */
  readonly serveRequests: string[] = [];
  /** How many times the app started serve. */
  serveStarts = 0;

  private readonly page: Page;
  private readonly replies = new Map<string, Pending>();
  private readonly running = new Map<string, Pending>();
  private readonly problems: string[] = [];
  private resolution: Resolution = {
    found: "bundled",
    path: "/Applications/agentx.app/Contents/MacOS/agentx",
  };
  private childId = 1;
  private serving = false;
  private serveStart: ServeStart = { events: [firstSnapshot()] };

  constructor(page: Page) {
    this.page = page;
    this.register(["version"], {
      events: [{ cli_version: cliVersion, type: "version" }],
    });
    this.register(["doctor"], { events: passingDoctor });
  }

  async install() {
    await this.page.exposeBinding(
      "agentxMockCli",
      async (_source, command: string, args: unknown) =>
        await this.handle(command, args)
    );
    await this.page.addInitScript(installInPage);
  }

  /** What the supervisor reports about the CLI. */
  resolveTo(resolution: Resolution) {
    this.resolution = resolution;
  }

  /**
   * Answers every later run of `args` with `reply`, replacing what was registered for it before.
   * `args` are the arguments the app passes, without --json.
   */
  register(args: readonly string[], reply: Reply = {}): Registered {
    const reason = refusal(args);
    if (reason !== undefined) {
      throw new Error(
        `registered "${line(args)}", which the CLI would refuse: ${reason}`
      );
    }
    const signals = new EventTarget();
    this.replies.set(line(args), { reply, signals });
    return {
      release: () => {
        signals.dispatchEvent(new CustomEvent("resume", { detail: "release" }));
      },
      started: (async () => String(await nextDetail(signals, "started")))(),
    };
  }

  /** The refusals so far, which then no longer fail the test. */
  takeRefusals(): string[] {
    return this.problems.splice(0);
  }

  /** Fails the test on a command line the mock refused or had no reply for. */
  verify() {
    if (this.problems.length > 0) {
      throw new Error(`the mocked CLI refused:\n${this.problems.join("\n")}`);
    }
  }

  /** Emits serve stdout events under the current child id. */
  async serveEmit(events: InlineEvent[]) {
    await this.emit(
      "serve-event",
      events.map((event) => ({
        child_id: this.childId,
        event: full(event),
        kind: "event",
      }))
    );
  }

  /**
   * Ends the current serve child, as the supervisor reports it: with a restart after
   * `restartInMs`, or none when it is null.
   */
  async serveExit(
    exitCode: number | null,
    stderr: string[] = [],
    restartInMs: number | null = 500
  ) {
    this.serving = false;
    await this.emit("serve-exit", [
      {
        child_id: this.childId,
        exit_code: exitCode,
        restart_in_ms: restartInMs,
        restarting: restartInMs !== null,
        stderr,
      },
    ]);
  }

  /** What every later serve child does when the app starts it. */
  onServeStart(start: ServeStart) {
    this.serveStart = start;
  }

  /** Starts a new serve child, as the supervisor's restart does; its id goes on later events. */
  serveRestart(childId: number) {
    this.childId = childId;
    this.serving = true;
  }

  private async emit(event: string, payloads: unknown[]) {
    await this.page.evaluate(
      ([name, all]) => {
        for (const payload of all) {
          window.agentxEmit(name, payload);
        }
      },
      [event, payloads] as const
    );
  }

  private refuse(message: string): never {
    this.problems.push(message);
    throw new Error(message);
  }

  private async handle(command: string, args: unknown): Promise<unknown> {
    switch (command) {
      case "cli_resolve": {
        return {
          found: this.resolution.found,
          path: this.resolution.path,
        };
      }
      case "cli_run": {
        const runArgs = field(args, "args");
        return await this.run(
          Array.isArray(runArgs) ? runArgs.map(String) : [],
          String(field(args, "run_id"))
        );
      }
      case "cli_cancel": {
        const runId = String(field(args, "run_id"));
        this.cancels.push(runId);
        const pending = this.running.get(runId);
        pending?.signals.dispatchEvent(
          new CustomEvent("resume", { detail: "cancel" })
        );
        return pending !== undefined;
      }
      case "serve_start": {
        if (this.serving) {
          return this.childId;
        }
        // A child that ended is followed by a new one, with the next id.
        if (this.serveStarts > 0) {
          this.childId += 1;
        }
        this.serveStarts += 1;
        this.serving = true;
        const { events = [], exit } = this.serveStart;
        await this.serveEmit(events);
        if (exit !== undefined) {
          const restart = [2, 6, 8].includes(exit.code) ? null : 500;
          await this.serveExit(exit.code, exit.stderr, restart);
        }
        return this.childId;
      }
      case "serve_request": {
        if (!this.serving) {
          throw new Error("serve is not running");
        }
        this.serveRequests.push(String(field(args, "line")));
        return null;
      }
      default: {
        return this.refuse(
          `the app called ${command}, which the supervisor does not have`
        );
      }
    }
  }

  private async run(args: string[], runId: string) {
    this.commands.push([...args, "--json"]);
    const reason = refusal(args);
    if (reason !== undefined) {
      this.refuse(`"${line(args)}": ${reason}`);
    }
    const pending = this.replies.get(line(args));
    if (pending === undefined) {
      this.refuse(`"${line(args)}": the test registered no reply`);
    }
    const { reply, signals } = pending;
    const events = (reply.events ?? []).map(full);
    const held = reply.holdAfter ?? events.length;
    const send = async (chunk: object[]) => {
      await this.emit(
        "cli-run",
        chunk.map((event) => ({ event, kind: "event", run_id: runId }))
      );
    };
    // Listen before the first events go out: the test may release the run as soon as it sees them.
    const resumed =
      reply.holdAfter === undefined ? "release" : nextDetail(signals, "resume");
    this.running.set(runId, pending);
    signals.dispatchEvent(new CustomEvent("started", { detail: runId }));
    await send(events.slice(0, held));
    const cancelled = (await resumed) === "cancel";
    this.running.delete(runId);
    const sent = cancelled ? events.slice(0, held) : events;
    if (!cancelled) {
      await send(events.slice(held));
    }
    const logs = (reply.logs ?? []).map((log) => full({ type: "log", ...log }));
    await this.emit(
      "cli-run",
      logs.map((event) => ({ event, kind: "log", run_id: runId }))
    );
    const end = {
      cancelled,
      events: sent,
      // A cancel stops agentx with SIGTERM, on which it reports the stop and exits 9.
      exit_code: cancelled ? 9 : (reply.exitCode ?? 0),
      invalid: [],
      logs,
      run_id: runId,
      timed_out: false,
    };
    await this.emit("cli-run-end", [end]);
    return end;
  }
}

/**
 * Playwright's test with a `cli` fixture: the mocked CLI. It is installed for every test of a file
 * that imports this `test`, before any hook opens the page, so the app never loads without it.
 */
export const test = base.extend<{ cli: MockCli }>({
  cli: [
    async ({ page }, provide) => {
      const cli = new MockCli(page);
      await cli.install();
      await provide(cli);
      cli.verify();
    },
    { auto: true },
  ],
});

export { expect } from "@playwright/test";
