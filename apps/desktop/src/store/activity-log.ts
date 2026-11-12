// The activity log: every command the app ran this session and what the serve child reported,
// newest first, kept in memory only. The transport (src/cli/transport.ts) records each one-shot
// run, whoever started it: a control, the startup checks or a loader. The feed (src/store/feed.ts)
// records what serve reported. An entry holds text made ready to show when it is recorded:
// credentials in a URL are replaced, its user and password as the CLI replaces them and the values
// of its query parameters that name a credential, an inventory is counted rather than kept, a
// file's content and a patch are left out, and long values, long lines and long outputs are cut,
// so the log never holds a secret and stays small.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, LogEvent } from "#/cli/events.gen.ts";

/** How many entries the log keeps; older ones drop off. */
export const LOG_LIMIT = 300;
/** How many lines of each stream an entry keeps; `dropped` counts the rest. */
const LINE_LIMIT = 200;
/** How many of those are the stream's first lines; the others are its last, which say how it ended. */
const HEAD_LINES = 50;
/** How long a line may be before it is cut. */
const LINE_CHARS = 2000;
/** How long a string value of an event may be before it is cut. */
const VALUE_LIMIT = 240;

export type LogTone = "success" | "neutral" | "attention" | "danger";

/**
 * The lines of one stream. A stream longer than LINE_LIMIT keeps its first and its last lines,
 * with one line between them that says how many were left out, which `dropped` counts.
 */
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
  /** Whether it logged a warning, on any line of its stderr, kept or left out. */
  warned: boolean;
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

/** The user and password of a URL: everything from the scheme to the last `@` before the path. */
const USERINFO = /(?<scheme>[a-z][a-z0-9+.-]*:\/\/)[^\s/"']*@/giu;

/**
 * A query parameter whose name says it holds a credential, with its value: a name that contains
 * token, password, passwd, pwd, secret, key, auth, signature, sig, credential or session, such as
 * `access_token`, `api_key`, `private_token`, `X-Amz-Signature` or `auth`.
 */
const SECRET_PARAMETER =
  /(?<name>[?&;][\w.%-]*(?:token|passw(?:or)?d|pwd|secret|key|auth|signature|sig|credential|session)[\w.%-]*=)[^&;#\s"']*/giu;

/**
 * `text` with the credentials of every URL in it replaced by `***`, so that a token someone put in
 * a remote's URL never reaches the log. The user and password go as the CLI drops them before it
 * prints a URL (`redact` in its `internal/source/url.go`), everything up to the last `@` before
 * the path. The CLI leaves a query string as it is; the log is stricter, and also replaces the
 * value of each query parameter whose name says it holds a credential.
 */
export const redact = (text: string) =>
  text.replaceAll(USERINFO, "$1***@").replaceAll(SECRET_PARAMETER, "$1***");

/**
 * `text` cut at `limit` characters, with how many were left out. It is called on text whose
 * credentials are replaced already, so a cut never leaves part of one behind.
 */
const clip = (text: string, limit = LINE_CHARS) =>
  text.length > limit
    ? `${text.slice(0, limit)}… (${text.length - limit} more characters)`
    : text;

/**
 * One argument as a shell takes it: quoted unless every character is one no shell reads. `~` and
 * `#` are quoted too: a shell expands the first and starts a comment at the second.
 */
const quote = (arg: string) =>
  /^[\w./:=@%+,-]+$/u.test(arg) ? arg : `'${arg.replaceAll("'", "'\\''")}'`;

/** The command line of `args`, credentials replaced. */
export const commandLine = (args: readonly string[]) =>
  clip(["agentx", ...args.map((arg) => quote(redact(arg)))].join(" "));

/**
 * The fields of an event that carry what a file on disk holds: a skill file's `content`
 * (`skill_file`) and a file's `patch` (`diff`). The log keeps their length only, since a file may
 * hold a secret anywhere, its first characters included.
 */
const FILE_FIELDS = new Set(["content", "patch"]);

/**
 * A value of an event as the log keeps it: strings without credentials and cut at VALUE_LIMIT.
 * An inventory's lists become their counts: it lists every MCP server's command line, URL and
 * headers as the configuration files hold them, which may hold a secret, and it is large.
 */
const kept = (value: unknown, countLists: boolean): unknown => {
  if (typeof value === "string") {
    return clip(redact(value), VALUE_LIMIT);
  }
  if (Array.isArray(value)) {
    return countLists
      ? `${value.length} ${value.length === 1 ? "item" : "items"}`
      : value.map((item) => kept(item, countLists));
  }
  if (typeof value === "object" && value !== null) {
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        key,
        FILE_FIELDS.has(key) && typeof item === "string"
          ? `${item.length} characters left out`
          : kept(item, countLists),
      ])
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
export const logLine = (event: { level: string; message: string }) =>
  `${event.level}: ${redact(event.message)}`;

/** A stderr line as JSON, when it is a log event. */
const logEventOf = (
  line: string
): { level: string; message: string } | undefined => {
  try {
    const parsed: unknown = JSON.parse(line);
    return typeof parsed === "object" &&
      parsed !== null &&
      "type" in parsed &&
      parsed.type === "log" &&
      "level" in parsed &&
      typeof parsed.level === "string" &&
      "message" in parsed &&
      typeof parsed.message === "string"
      ? { level: parsed.level, message: parsed.message }
      : undefined;
  } catch {
    return undefined;
  }
};

/**
 * One line of the serve log file as the log shows it: a log event as "level: message", anything
 * else as it was written, credentials replaced either way and a long line cut.
 */
export const serveLogLine = (line: string) => {
  const event = logEventOf(line);
  return clip(event === undefined ? redact(line) : logLine(event));
};

/**
 * Why serve stopped, from its last stderr lines: the last one that is no debug line, a log
 * event's message or the line as it was written, credentials replaced. Undefined when there is none.
 */
export const lastStderr = (lines: readonly string[]): string | undefined => {
  const read = lines.map((line) => ({ event: logEventOf(line), line }));
  const [last] = read.filter(({ event }) => event?.level !== "debug").slice(-1);
  return last === undefined
    ? undefined
    : clip(redact(last.event?.message ?? last.line));
};

/**
 * The lines of a stream as an entry keeps them, each cut at LINE_CHARS. A stream longer than
 * LINE_LIMIT keeps its first HEAD_LINES lines and its last ones, which say how the run ended,
 * with a line between them that counts the lines left out.
 */
const output = (all: string[]): Output => {
  const lines = all.map((line) => clip(line));
  if (lines.length <= LINE_LIMIT) {
    return { dropped: 0, lines };
  }
  const dropped = lines.length - LINE_LIMIT;
  return {
    dropped,
    lines: [
      ...lines.slice(0, HEAD_LINES),
      `… ${dropped} lines left out …`,
      ...lines.slice(HEAD_LINES - LINE_LIMIT),
    ],
  };
};

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
                  : clip(redact(ended.startError)),
              stderr: output([
                ...ended.logs.map(logLine),
                ...invalid("stderr"),
              ]),
              timedOut: ended.timedOut,
              // From every log event, before the stream is cut.
              warned: ended.logs.some((l) => l.level === "warn"),
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
    details: output(details.map(redact)).lines,
    id,
    kind: "serve",
    title: clip(redact(title), VALUE_LIMIT),
    tone,
  });
};

/** What each exit code means (docs/spec/cli-contract.md, Exit codes). */
const MEANINGS: Record<string, string | undefined> = {
  account_repo: "The account repo is unusable",
  git: "Git is missing or too old",
  internal: "Internal error",
  interrupted: "Interrupted",
  locked: "Another agentx command was running",
  not_found: "Not found",
  ok: "Succeeded",
  pending_merge: "A pending merge blocks it, or was left by it",
  refused: "Refused",
  source: "A source or the account remote was unreachable or denied access",
  usage: "Usage error",
} satisfies Record<keyof typeof EXIT_CODES, string>;

const meaningOf = (exitCode: number) =>
  Object.entries(EXIT_CODES)
    .filter(([, code]) => code === exitCode)
    .map(([name]) => MEANINGS[name])[0];

/**
 * How an entry ended, in words, and the tone that goes with it: what happened first, then the exit
 * code. The sheet shows no words for the success tone, whose dot and duration say it.
 */
export const statusOf = (entry: LogEntry): { tone: LogTone; text: string } => {
  if (entry.kind === "serve") {
    return { text: "Background", tone: entry.tone };
  }
  const { ending } = entry;
  if (ending === undefined) {
    return { text: "Running…", tone: "neutral" };
  }
  if (ending.startError !== undefined) {
    return { text: "Couldn't start", tone: "danger" };
  }
  if (ending.timedOut) {
    return { text: "Timed out", tone: "danger" };
  }
  if (ending.cancelled) {
    return { text: "Cancelled", tone: "attention" };
  }
  if (ending.exitCode === null) {
    return { text: "Stopped by the system", tone: "danger" };
  }
  if (ending.exitCode === EXIT_CODES.ok) {
    // A command that succeeded and warned asks for a look.
    return ending.warned
      ? { text: "Succeeded with warnings", tone: "attention" }
      : { text: "Succeeded", tone: "success" };
  }
  const text = `${meaningOf(ending.exitCode) ?? "Failed"} · exit ${ending.exitCode}`;
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

const block = (title: string, { lines }: Output) =>
  lines.length === 0 ? [] : ["", `${title}:`, ...lines];

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
    ...block("Output (stdout)", ending.events),
    ...block("Log (stderr)", ending.stderr),
  ].join("\n");
};
