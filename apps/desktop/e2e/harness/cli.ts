// The mocked CLI transport. The tests run against the Vite dev server, which has no Tauri runtime,
// so this fixture stands in for the Rust supervisor, and for the serve child behind it, under the
// app's real transport module (src/cli/transport.ts): the page's Tauri calls come here, to the
// test. Every command is a JSON-RPC `run` request the app writes to serve's stdin; the mock
// answers it with the events the test registered, as `event` notifications and then the run's
// response, and records every command line, cancel and serve request.
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
// passing launch: `version` and `doctor` replies, and a serve child that writes its first inventory
// when it starts, which the first command does. Once the checks pass the store loads its slices
// (src/store/loaders.ts), so the mock answers `config list`, `source list`, `skill import` and
// `machine` too. A test replaces any of them with register() and onServeStart().
//
// The mock also answers the native open dialog of Tauri's dialog plugin with the path a test gives
// answerOpenDialog(), or with null, a Cancel, until one does, and records every Tauri call in `calls`.
import { once } from "node:events";

import { test as base } from "@playwright/test";
import type { Page } from "@playwright/test";

import { SCHEMA_VERSION } from "#/cli/events.gen.ts";
import type { Configuration, LogEvent } from "#/cli/events.gen.ts";
import type { ServeEvent } from "#/cli/transport.ts";

import { refusal } from "./command-line.ts";

type EventType = ServeEvent["type"];

/**
 * An event with only the fields a test checks. The mock adds schema_version. A `refresh_complete`
 * or `search` event of serve's is the mock's answer to the request of that id.
 */
export type InlineEvent = {
  [T in EventType]: { type: T } & Partial<
    Omit<Extract<ServeEvent, { type: T }>, "type">
  >;
}[EventType];

export interface Reply {
  /** The events on stdout, in order. */
  events?: InlineEvent[];
  /** The log events on stderr, sent after the events. */
  logs?: Partial<Omit<LogEvent, "type">>[];
  exitCode?: number;
  /** Holds the run after this many events, until release(), expire() or a cancel. */
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

/**
 * A configuration as an inventory lists it, with `overrides` in place of the defaults: one that
 * reads the library places through it, any other by symlink.
 */
export const configuration = (
  id: string,
  overrides: Partial<Configuration> = {}
): Configuration => ({
  client: id,
  enabled: true,
  id,
  logical_id: id,
  name: id,
  path: `/home/me/.${id}`,
  physical_id: id,
  placement: overrides.reads_library === true ? "library" : "symlink",
  reads_library: false,
  ...overrides,
});

/** The label of the machine a passing launch reports. */
export const machineLabel = "studio-mac";

/**
 * A serve child's first inventory, with these configurations. Serve adds the fields an inventory
 * must have (serveEmit), its child's instance id among them.
 */
export const firstInventory = (configurations = ["claude-code", "codex"]) =>
  ({
    configurations: configurations.map((id) => configuration(id)),
    library: [],
    scan_counter: 1,
    type: "inventory",
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
    check: "store",
    detail: "/home/me/.agentx/store.git",
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

/** The reads the store runs once the checks pass (src/store/loaders.ts), with --json. */
export const loadLines = [
  ["config", "list", "--json"],
  ["source", "list", "--json"],
  ["skill", "import", "--json"],
  ["machine", "--json"],
];

/** The library on the machine a passing launch reports. */
export const libraryPath = "/home/me/.agents/skills";

/** The serve log file on the machine a passing launch reports. */
export const serveLogPath = "/home/me/.local/state/agentx/logs/serve.log";

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
  /** Ends the run as the supervisor does when its deadline passes. */
  expire: () => void;
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

/** A refresh or search request serve has not answered yet, and the child it was sent to. */
interface Asked {
  method: "refresh" | "search";
  query: string;
  childId: number;
}

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
  /** The run id of every cancel (`$/cancelRequest`). */
  readonly cancels: string[] = [];
  /** The deadline of every run, in order; null for a run with none. */
  readonly deadlines: (number | null)[] = [];
  /** Every line written to serve's stdin: JSON-RPC messages, runs among them. */
  readonly stdin: string[] = [];
  /**
   * Every refresh and search request written to serve's stdin, in order, as the store made it: a
   * JSON object with `type`, `request_id` and, for a search, `query`.
   */
  readonly serveRequests: string[] = [];
  /** The paths of every call to the opener plugin's reveal, in order: what the app showed in Finder. */
  readonly reveals: string[][] = [];
  /** Every URL the app opened in the browser through the opener plugin, in order. */
  readonly openedUrls: string[] = [];
  /** The options of every native open dialog the app showed (the dialog plugin's open), in order. */
  readonly openDialogs: unknown[] = [];
  /** Every Tauri command the page called, in order: the supervisor's and the plugins'. */
  readonly calls: string[] = [];
  /**
   * Every file the app opened in an editor, in order, with the editor: `{ kind: "system" }` for
   * the opener plugin's open, else the editor the saved preferences held when `editor_open` ran.
   */
  readonly editorOpens: { path: string; editor: unknown }[] = [];
  /** The request id of every refresh and search request, in order. */
  get requestIds(): string[] {
    return this.serveRequests.map((written) => {
      const request: unknown = JSON.parse(written);
      return typeof request === "object" &&
        request !== null &&
        "request_id" in request
        ? String(request.request_id)
        : "";
    });
  }

  /** Every value the app set serve's debug level to (`serve_verbose`), in order. */
  readonly serveVerbose: boolean[] = [];

  /** How many times the app started serve. */
  serveStarts = 0;

  private readonly page: Page;
  private readonly replies = new Map<string, Pending>();
  private readonly running = new Map<string, Pending>();
  /** The refresh and search requests serve has not answered, by request id. */
  private readonly asked = new Map<string, Asked>();
  private readonly problems: string[] = [];
  private resolution: Resolution = {
    found: "bundled",
    path: "/Applications/agentx.app/Contents/MacOS/agentx",
  };
  private childId = 1;
  private serving = false;
  /** The start of the serve child going on, which a start meanwhile waits for, as Rust's lock. */
  private starting: Promise<number> | undefined;
  private serveStart: ServeStart = { events: [firstInventory()] };
  private openAnswer: string | null = null;
  private editors: { id: string; name: string }[] = [];
  private failingOpens = false;
  private failingReveals = false;
  private readonly missingPaths = new Set<string>();
  // The app's preferences file, which outlives a page reload as the file outlives a restart.
  private preferences: unknown = { editor: { kind: "system" } };
  // The app update the next checks find, and how an install of it ends.
  private appUpdate: { version: string; current_version: string } | null = null;
  private holdingInstall = false;
  /** How the held install ended, once the test ended it: failed or not. */
  private installEnded: boolean | undefined;
  private readonly installSignals = new EventTarget();
  /** How many times the app checked for its own update. */
  appUpdateChecks = 0;
  /** How many times the app asked to restart, to run the update it installed. */
  restarts = 0;

  constructor(page: Page) {
    this.page = page;
    this.register(["version"], {
      events: [{ cli_version: cliVersion, type: "version" }],
    });
    this.register(["doctor"], { events: passingDoctor });
    this.register(["config", "list"], { events: [{ type: "settings" }] });
    this.register(["source", "list"]);
    this.register(["skill", "import"]);
    this.register(["machine"], {
      events: [
        {
          home: "/home/me/.agentx",
          id: "machine-1",
          label: machineLabel,
          library: libraryPath,
          settings_file: "/home/me/.agentx/settings.json",
          type: "machine",
        },
      ],
    });
  }

  async install() {
    await this.page.exposeBinding(
      "agentxMockCli",
      async (_source, command: string, args: unknown) =>
        await this.handle(command, args)
    );
    await this.page.addInitScript(installInPage);
  }

  /** What every later native open dialog returns: the path the user picks, or null for Cancel. */
  answerOpenDialog(path: string | null) {
    this.openAnswer = path;
  }

  /** The editors every later detection finds on the machine, in the order the app lists them. */
  answerEditors(editors: { id: string; name: string }[]) {
    this.editors = editors;
  }

  /**
   * Runs the app as on macOS: the webview reports a Mac platform from the next page load, which
   * `src/lib/platform.ts` reads. The suite runs as Linux otherwise.
   */
  async runAsMac() {
    await this.page.addInitScript(() => {
      Object.defineProperty(navigator, "platform", { get: () => "MacIntel" });
    });
  }

  /** Makes a later reveal of each of `paths` fail, as for a path that does not exist. */
  missing(...paths: string[]) {
    for (const p of paths) {
      this.missingPaths.add(p);
    }
  }

  /** Whether a reveal of `paths` fails: every one does after `failReveals`, or one of a missing path. */
  private revealFails(paths: unknown) {
    return (
      this.failingReveals ||
      (Array.isArray(paths) ? paths : [paths]).some((p) =>
        this.missingPaths.has(String(p))
      )
    );
  }

  /** Whether every later reveal fails, as when the path no longer exists. */
  failReveals(fail = true) {
    this.failingReveals = fail;
  }

  /** Whether every later open in an editor fails, as when the editor cannot start. */
  failEditorOpens(fail = true) {
    this.failingOpens = fail;
  }

  /** The app's preferences as last saved, or as the test set them with `savePreferences`. */
  get savedPreferences(): unknown {
    return this.preferences;
  }

  /** Sets the app's preferences file, as an earlier run of the app would have left it. */
  savePreferences(preferences: unknown) {
    this.preferences = preferences;
  }

  /** The app update every later check finds, as `version` over `current`; null for none. */
  offerAppUpdate(version: string | null, current = "0.3.0") {
    this.appUpdate =
      version === null ? null : { current_version: current, version };
  }

  /**
   * Holds the next install of the app update until `endAppUpdateInstall`, so a test can send
   * its progress with `appUpdateProgress` meanwhile.
   */
  holdAppUpdateInstall() {
    this.holdingInstall = true;
    this.installEnded = undefined;
  }

  /** Ends the held install: it succeeds, or fails as with a bad signature. */
  endAppUpdateInstall({ failed = false } = {}) {
    this.installEnded = failed;
    this.installSignals.dispatchEvent(new CustomEvent("end"));
  }

  /** Sends the download's progress of the install going on, in bytes. */
  async appUpdateProgress(downloaded: number, total: number) {
    await this.emit("app-update-progress", [{ downloaded, total }]);
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
      expire: () => {
        signals.dispatchEvent(new CustomEvent("resume", { detail: "expire" }));
      },
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

  /**
   * Emits serve stdout events under the current child id, or `childId` for a child that ended, as
   * `event` notifications of serve's own. An inventory gets the fields it must have and the test
   * left out: no items, this machine, and `instance-<child id>`. A `refresh_complete` or `search`
   * event is serve's response to the request of its `request_id` instead, which only a request
   * serve has not answered gets; the child it was sent to answers it once.
   */
  async serveEmit(events: InlineEvent[], childId = this.childId) {
    const messages = events.flatMap((event) => {
      if (event.type === "refresh_complete" || event.type === "search") {
        return this.answer(event, childId);
      }
      return [
        {
          jsonrpc: "2.0",
          method: "event",
          params: {
            event: full(
              event.type === "inventory"
                ? {
                    configurations: [],
                    edges: [],
                    installs: [],
                    instance_id: `instance-${childId}`,
                    library: [],
                    machine: { id: "machine-1", label: machineLabel },
                    mcp_servers: [],
                    plugins: [],
                    skills: [],
                    warnings: [],
                    ...event,
                  }
                : event
            ),
            stream: "stdout",
          },
        },
      ];
    });
    await this.emitMessages(messages, childId);
  }

  /** Serve's response to the refresh or search request an answer event names, if it awaits one. */
  private answer(
    event: Extract<InlineEvent, { type: "refresh_complete" | "search" }>,
    childId: number
  ): object[] {
    const id = event.request_id ?? "";
    const asked = this.asked.get(id);
    if (asked === undefined) {
      return [];
    }
    if (asked.childId === childId) {
      this.asked.delete(id);
    }
    const instanceId = event.instance_id ?? `instance-${childId}`;
    if (event.type === "search") {
      return [
        {
          id,
          jsonrpc: "2.0",
          result: {
            instance_id: instanceId,
            query: event.query ?? asked.query,
            results: event.results ?? [],
          },
        },
      ];
    }
    return [
      event.ok === false
        ? {
            error: { code: -32_000, message: event.error ?? "the scan failed" },
            id,
            jsonrpc: "2.0",
          }
        : {
            id,
            jsonrpc: "2.0",
            result: {
              instance_id: instanceId,
              scan_counter: event.scan_counter ?? 1,
            },
          },
    ];
  }

  /**
   * Emits serve stderr log events under the current child id, or `childId` for a child that
   * ended, as the supervisor reports them: each a `log` line.
   */
  async serveLog(
    logs: Partial<Omit<LogEvent, "type">>[],
    childId = this.childId
  ) {
    await this.emit(
      "serve-event",
      logs.map((log) => ({
        child_id: childId,
        event: full({ type: "log", ...log }),
        kind: "log",
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
    // An ended child answers nothing more.
    for (const [id, asked] of this.asked) {
      if (asked.childId === this.childId) {
        this.asked.delete(id);
      }
    }
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

  /**
   * Forgets the serve child, as closing the app does: after a page reload, the next serve start
   * is a first start, which writes its first inventory again.
   */
  restart() {
    this.serving = false;
  }

  /** Emits JSON-RPC messages the child wrote on stdout, as the supervisor reports them. */
  private async emitMessages(messages: object[], childId = this.childId) {
    await this.emit(
      "serve-event",
      messages.map((event) => ({ child_id: childId, event, kind: "event" }))
    );
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
    this.calls.push(command);
    switch (command) {
      case "cli_resolve": {
        return {
          found: this.resolution.found,
          path: this.resolution.path,
        };
      }
      case "serve_start": {
        if (this.starting !== undefined) {
          return await this.starting;
        }
        if (this.serving) {
          return this.childId;
        }
        this.starting = this.startServe();
        try {
          return await this.starting;
        } finally {
          this.starting = undefined;
        }
      }
      case "serve_request": {
        if (!this.serving) {
          throw new Error("serve is not running");
        }
        if (field(args, "child_id") !== this.childId) {
          throw new Error(
            `serve child ${String(field(args, "child_id"))} has ended`
          );
        }
        const written = String(field(args, "line"));
        this.stdin.push(written);
        this.read(JSON.parse(written));
        return null;
      }
      case "plugin:opener|reveal_item_in_dir": {
        const paths = field(args, "paths");
        this.reveals.push(
          Array.isArray(paths) ? paths.map(String) : [String(paths)]
        );
        if (this.revealFails(paths)) {
          throw new Error("No such file or directory");
        }
        return null;
      }
      case "plugin:opener|open_url": {
        this.openedUrls.push(String(field(args, "url")));
        return null;
      }
      case "plugin:dialog|open": {
        this.openDialogs.push(field(args, "options"));
        return this.openAnswer;
      }
      default: {
        return this.handleShell(command, args);
      }
    }
  }

  /**
   * Starts a serve child, which writes what onServeStart says. A child that ended is followed by
   * a new one, with the next id, as the supervisor's restart is.
   */
  private async startServe(): Promise<number> {
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

  /** The Rust shell's own commands beside the supervisor's: the editor and the app's preferences. */
  private handleShell(command: string, args: unknown): unknown {
    switch (command) {
      case "plugin:opener|open_path":
      case "editor_open": {
        const path = String(field(args, "path"));
        if (command === "editor_open" && field(args, "editor") !== undefined) {
          return this.refuse("editor_open takes the path alone");
        }
        this.editorOpens.push({
          // The Rust side opens with the editor the preferences hold; the call names only the file.
          editor:
            command === "editor_open"
              ? field(this.preferences, "editor")
              : { kind: "system" },
          path,
        });
        if (this.failingOpens) {
          throw new Error(`couldn't open ${path}`);
        }
        return null;
      }
      case "serve_log_path": {
        return serveLogPath;
      }
      case "serve_verbose": {
        this.serveVerbose.push(field(args, "on") === true);
        return null;
      }
      case "editor_detect": {
        return this.editors;
      }
      case "preferences_read": {
        return this.preferences;
      }
      case "preferences_write": {
        this.preferences = field(args, "preferences");
        return null;
      }
      case "app_update_check": {
        this.appUpdateChecks += 1;
        return this.appUpdate;
      }
      case "app_update_install": {
        return this.installAppUpdate(field(args, "version"));
      }
      case "app_restart": {
        this.restarts += 1;
        return null;
      }
      default: {
        return this.refuse(
          `the app called ${command}, which the supervisor does not have`
        );
      }
    }
  }

  private async installAppUpdate(version: unknown) {
    if (this.appUpdate === null) {
      throw new Error("no update was found to install");
    }
    // Held, a test can offer a newer update meanwhile, as a check that ends before the install
    // starts does.
    if (this.holdingInstall && this.installEnded === undefined) {
      await nextDetail(this.installSignals, "end");
    }
    if (this.appUpdate.version !== version) {
      return this.appUpdate.version;
    }
    if (this.installEnded === true) {
      throw new Error("signature does not match");
    }
    return null;
  }

  /** Reads one JSON-RPC message from serve's stdin, as serve does. */
  private read(message: unknown) {
    const method = field(message, "method");
    const id = String(field(message, "id"));
    const params = field(message, "params");
    if (field(message, "jsonrpc") !== "2.0") {
      this.refuse(`serve read a line that is no JSON-RPC 2.0 message`);
    }
    if (method === "run") {
      const runArgs = field(params, "args");
      const deadline = field(params, "deadline_ms");
      this.deadlines.push(typeof deadline === "number" ? deadline : null);
      // Serve answers a run when it ends; it reads the next line meanwhile.
      void this.run(
        Array.isArray(runArgs) ? runArgs.map(String) : [],
        id,
        this.childId
      );
    } else if (method === "$/cancelRequest") {
      const runId = String(field(params, "id"));
      this.cancels.push(runId);
      this.running
        .get(runId)
        ?.signals.dispatchEvent(
          new CustomEvent("resume", { detail: "cancel" })
        );
    } else if (method === "refresh" || method === "search") {
      const given = field(params, "query");
      const query = typeof given === "string" ? given : "";
      this.asked.set(id, { childId: this.childId, method, query });
      this.serveRequests.push(
        JSON.stringify(
          method === "refresh"
            ? { request_id: id, type: method }
            : { query, request_id: id, type: method }
        )
      );
    } else {
      this.refuse(`serve has no method ${String(method)}`);
    }
  }

  private async run(args: string[], runId: string, childId: number) {
    try {
      await this.runReply(args, runId, childId);
    } catch {
      // The page closed while the run went on: nothing reads its answer.
    }
  }

  private async runReply(args: string[], runId: string, childId: number) {
    this.commands.push([...args, "--json"]);
    const notify = async (params: object[]) => {
      await this.emitMessages(
        params.map((p) => ({
          jsonrpc: "2.0",
          method: "event",
          params: { id: runId, ...p },
        })),
        childId
      );
    };
    const pending = this.replies.get(line(args));
    const problem =
      refusal(args) ??
      (pending === undefined ? "the test registered no reply" : undefined);
    if (problem !== undefined || pending === undefined) {
      // Serve would run it, and the CLI refuse it: the run fails, and so does the test.
      const message = `"${line(args)}": ${problem ?? ""}`;
      this.problems.push(message);
      await this.emitMessages(
        [{ error: { code: -32_602, message }, id: runId, jsonrpc: "2.0" }],
        childId
      );
      return;
    }
    const { reply, signals } = pending;
    const events = (reply.events ?? []).map(full);
    const held = reply.holdAfter ?? events.length;
    const send = async (chunk: object[]) => {
      await notify(chunk.map((event) => ({ event, stream: "stdout" })));
    };
    // Listen before the first events go out: the test may release the run as soon as it sees them.
    const resumed =
      reply.holdAfter === undefined ? "release" : nextDetail(signals, "resume");
    this.running.set(runId, pending);
    signals.dispatchEvent(new CustomEvent("started", { detail: runId }));
    await send(events.slice(0, held));
    const resumedWith = await resumed;
    const cancelled = resumedWith === "cancel";
    const timedOut = resumedWith === "expire";
    this.running.delete(runId);
    const stopped = cancelled || timedOut;
    if (!stopped) {
      await send(events.slice(held));
    }
    const logs = (reply.logs ?? []).map((log) => full({ type: "log", ...log }));
    await notify(logs.map((event) => ({ event, stream: "stderr" })));
    await this.emitMessages(
      [
        {
          id: runId,
          jsonrpc: "2.0",
          result: {
            cancelled,
            // A cancel or a deadline stops agentx with SIGTERM, on which it reports the stop and
            // exits 9.
            exit_code: stopped ? 9 : (reply.exitCode ?? 0),
            timed_out: timedOut,
          },
        },
      ],
      childId
    );
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
