// The activity log: every command the app ran this session and what the serve child reported,
// newest first, kept in memory only. The transport (src/cli/transport.ts) records each one-shot
// run, whoever started it: a control, the startup checks or a loader. The feed (src/store/feed.ts)
// records what serve reported. An entry holds text made ready to show when it is recorded:
// credentials in a URL are replaced as the CLI replaces them, an inventory is counted rather than
// kept, and long values and long outputs are cut, so the log never holds a secret and stays small.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, LogEvent } from "#/cli/events.gen.ts";

/** How many entries the log keeps; older ones drop off. */
export const LOG_LIMIT = 300;
/** How many lines of each stream an entry keeps; `dropped` counts the rest. */
const LINE_LIMIT = 200;
/** How long a string value of an event may be before it is cut. */
const VALUE_LIMIT = 240;

export type LogTone = "success" | "neutral" | "attention" | "danger";

/** The lines of one stream, and how many more the entry left out. */
export interface Output {
  lines: string[];
  dropped: number;
}

/** How a command ended. */
export interface Ending {
  /** How long it ran, in milliseconds. */
  ms: number;
  /** null when a signal ended the CLI or it could not start. */
  exitCode: number | null;
  cancelled: boolean;
  timedOut: boolean;
  /** Why the CLI could not start; absent when it ran. */
  startError?: string;
  /** The events on stdout, one line each. */
  events: Output;
  /** What it wrote to stderr: its log events as "level: message", and lines that are no event. */
  stderr: Output;
}

/** One run of `agentx`. */
export interface CommandEntry {
  kind: "command";
  id: number;
  /** When it started, as Date.now(). */
  at: number;
  /** The command line as a terminal takes it, without the `--json` the app adds. */
  line: string;
  /** Absent while it runs. */
  ending?: Ending;
}

/** Something the serve child reported. */
export interface ServeEntry {
  kind: "serve";
  id: number;
  at: number;
  tone: LogTone;
  title: string;
  /** What it reported, one line each. */
  details: string[];
}

export type LogEntry = CommandEntry | ServeEntry;

interface ActivityLog {
  /** Newest first, at most LOG_LIMIT entries. */
  entries: LogEntry[];
  /** Whether the commands run from now on also log at debug level (`--verbose`). */
  debug: boolean;
  /** Whether the log's sheet shows. */
  open: boolean;
}

export const useActivityLog = create<ActivityLog>()(() => ({
  debug: false,
  entries: [],
  open: false,
}));

/** Shows or hides the activity log. */
export const openActivityLog = (open = true) => {
  useActivityLog.setState({ open });
};

/** Turns debug lines on or off for the commands run from now on. */
export const setDebugLines = (debug: boolean) => {
  useActivityLog.setState({ debug });
};

/**
 * `text` with the user and password of every URL in it replaced by `***`, as the CLI does before
 * it prints a URL (`redact` in its `internal/source/url.go`): a token someone put in a remote's
 * URL never reaches the log. Everything up to the last `@` before the path goes, as there.
 */
export const redact = (text: string) =>
  text.replaceAll(/(?<scheme>[a-z][a-z0-9+.-]*:\/\/)[^\s/"']*@/giu, "$1***@");

/** One argument as a shell takes it: quoted when it holds anything a shell would read. */
const quote = (arg: string) =>
  /^[\w./:=@%+,#~-]+$/u.test(arg) ? arg : `'${arg.replaceAll("'", "'\\''")}'`;

/** The command line of `args`, credentials replaced. */
export const commandLine = (args: readonly string[]) =>
  ["agentx", ...args.map((arg) => quote(redact(arg)))].join(" ");

/**
 * A value of an event as the log keeps it: strings without credentials and cut at VALUE_LIMIT.
 * An inventory's lists become their counts: it lists every MCP server's command line, URL and
 * headers as the configuration files hold them, which may hold a secret, and it is large.
 */
const kept = (value: unknown, countLists: boolean): unknown => {
  if (typeof value === "string") {
    const text = redact(value);
    return text.length > VALUE_LIMIT
      ? `${text.slice(0, VALUE_LIMIT)}… (${text.length - VALUE_LIMIT} more characters)`
      : text;
  }
  if (Array.isArray(value)) {
    return countLists
      ? `${value.length} ${value.length === 1 ? "item" : "items"}`
      : value.map((item) => kept(item, countLists));
  }
  if (typeof value === "object" && value !== null) {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [key, kept(item, countLists)])
    );
  }
  return value;
};

/** One event as a line: its type, then its other fields as JSON. */
export const eventLine = (event: CliEvent): string => {
  const {
    type,
    schema_version: _,
    ...fields
  }: Record<string, unknown> = {
    ...event,
  };
  const body = JSON.stringify(kept(fields, type === "inventory"));
  return body === "{}" ? String(type) : `${String(type)} ${body}`;
};

/** One log event as a line: "warn: the message". */
export const logLine = (event: LogEvent) =>
  `${event.level}: ${redact(event.message)}`;

const output = (lines: string[]): Output => ({
  dropped: Math.max(0, lines.length - LINE_LIMIT),
  lines: lines.slice(0, LINE_LIMIT),
});

let nextId = 1;

const add = (entry: LogEntry) => {
  useActivityLog.setState((s) => ({
    entries: [entry, ...s.entries].slice(0, LOG_LIMIT),
  }));
};

/** Records a command as it starts, and returns its entry's id. */
export const logCommandStart = (args: readonly string[]): number => {
  const id = nextId;
  nextId += 1;
  add({ at: Date.now(), id, kind: "command", line: commandLine(args) });
  return id;
};

/** What a run gave the transport when it ended. */
export interface Ended {
  exitCode: number | null;
  cancelled: boolean;
  timedOut: boolean;
  startError?: string;
  events: CliEvent[];
  logs: LogEvent[];
  /** The lines that were no JSON object, of either stream. */
  invalid: { stream: "stdout" | "stderr"; line: string }[];
}

/** Records how the command of entry `id` ended. An entry that dropped off the log stays gone. */
export const logCommandEnd = (id: number, ended: Ended) => {
  const invalid = (stream: "stdout" | "stderr") =>
    ended.invalid.filter((l) => l.stream === stream).map((l) => redact(l.line));
  useActivityLog.setState((s) => ({
    entries: s.entries.map((entry) =>
      entry.kind === "command" && entry.id === id
        ? {
            ...entry,
            ending: {
              cancelled: ended.cancelled,
              events: output([
                ...ended.events.map(eventLine),
                ...invalid("stdout"),
              ]),
              exitCode: ended.exitCode,
              ms: Date.now() - entry.at,
              startError:
                ended.startError === undefined
                  ? undefined
                  : redact(ended.startError),
              stderr: output([
                ...ended.logs.map(logLine),
                ...invalid("stderr"),
              ]),
              timedOut: ended.timedOut,
            },
          }
        : entry
    ),
  }));
};

/** Records something the serve child reported. */
export const logServe = (
  tone: LogTone,
  title: string,
  details: string[] = []
) => {
  const id = nextId;
  nextId += 1;
  add({
    at: Date.now(),
    details: details.slice(0, LINE_LIMIT).map(redact),
    id,
    kind: "serve",
    title: redact(title),
    tone,
  });
};

/** What each exit code means (docs/spec/cli-contract.md, Exit codes). */
const MEANINGS: Record<string, string | undefined> = {
  account_repo: "the account repo is unusable",
  git: "git is missing or too old",
  internal: "internal error",
  interrupted: "interrupted",
  locked: "another agentx command held the lock",
  not_found: "not found",
  ok: "succeeded",
  pending_merge: "a pending merge blocks it, or was left by it",
  refused: "refused",
  source: "a source or the account remote was unreachable or denied access",
  usage: "usage error",
} satisfies Record<keyof typeof EXIT_CODES, string>;

const meaningOf = (exitCode: number) =>
  Object.entries(EXIT_CODES)
    .filter(([, code]) => code === exitCode)
    .map(([name]) => MEANINGS[name])[0];

/** How an entry ended, in words, and the tone that goes with it. */
export const statusOf = (entry: LogEntry): { tone: LogTone; text: string } => {
  if (entry.kind === "serve") {
    return { text: "agentx serve", tone: entry.tone };
  }
  const { ending } = entry;
  if (ending === undefined) {
    return { text: "Running", tone: "neutral" };
  }
  if (ending.startError !== undefined) {
    return { text: "Could not start", tone: "danger" };
  }
  if (ending.timedOut) {
    return { text: "Stopped at its deadline", tone: "danger" };
  }
  if (ending.cancelled) {
    return { text: "Cancelled", tone: "attention" };
  }
  if (ending.exitCode === null) {
    return { text: "Ended by a signal", tone: "danger" };
  }
  const meaning = meaningOf(ending.exitCode);
  const text =
    meaning === undefined
      ? `Exit ${ending.exitCode}`
      : `Exit ${ending.exitCode}, ${meaning}`;
  if (ending.exitCode === EXIT_CODES.ok) {
    // A command that succeeded and warned asks for a look.
    const warned = ending.stderr.lines.some((l) => l.startsWith("warn: "));
    return { text, tone: warned ? "attention" : "success" };
  }
  const soft =
    ending.exitCode === EXIT_CODES.locked ||
    ending.exitCode === EXIT_CODES.interrupted;
  return { text, tone: soft ? "attention" : "danger" };
};

/** How long a command ran: "412 ms", "1.8 s", "2 min 5 s". */
export const formatDuration = (ms: number) => {
  if (ms < 1000) {
    return `${Math.round(ms)} ms`;
  }
  if (ms < 60_000) {
    return `${(ms / 1000).toFixed(1)} s`;
  }
  const seconds = Math.round(ms / 1000);
  return `${Math.floor(seconds / 60)} min ${seconds % 60} s`;
};

const block = (title: string, { lines, dropped }: Output) =>
  lines.length === 0
    ? []
    : [
        "",
        `${title}:`,
        ...lines,
        ...(dropped > 0 ? [`… ${dropped} more lines`] : []),
      ];

/** An entry as plain text, to paste into an issue. */
export const entryText = (entry: LogEntry): string => {
  const at = new Date(entry.at).toISOString();
  if (entry.kind === "serve") {
    return [`${at}  agentx serve: ${entry.title}`, ...entry.details].join("\n");
  }
  const { ending } = entry;
  const head = [`$ ${entry.line}`, `Started: ${at} (the app adds --json)`];
  if (ending === undefined) {
    return [...head, "Still running"].join("\n");
  }
  return [
    ...head,
    `Took: ${formatDuration(ending.ms)}`,
    `Ended: ${statusOf(entry).text}`,
    ...(ending.startError === undefined ? [] : [ending.startError]),
    ...block("Events (stdout)", ending.events),
    ...block("Stderr", ending.stderr),
  ].join("\n");
};
