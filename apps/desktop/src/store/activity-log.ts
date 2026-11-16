// The activity log: the last lines of what the app ran and what the background scan (`agentx
// serve`) reported, oldest first, kept in memory only. It holds at most LINE_LIMIT lines, and a
// line beyond that removes the oldest one, so it takes the same memory after a week as after an
// hour. The transport (src/cli/transport.ts) records each one-shot run, whoever started it: a
// control, the startup checks or a loader. The feed (src/store/feed.ts) records what serve
// reported. A command is one line. Only a command that failed keeps output, its last
// FAILURE_LINES lines. A line holds text made ready to show when it is recorded: credentials in a
// URL are replaced, its user and password as the CLI replaces them and the values of its query
// parameters that name a credential, an inventory is counted rather than kept, a file's content
// and a patch are left out, and long values and long lines are cut, so the log never holds a secret.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, LogEvent } from "#/cli/events.gen.ts";

/** How many lines the log keeps; a line beyond that removes the oldest one. */
export const LINE_LIMIT = 500;
/** How many of its last lines a command that failed keeps, and a background scan that stopped. */
const FAILURE_LINES = 5;
/** How many of its last debug lines a command keeps, which it writes while Debug output is on. */
const DEBUG_LINES = 50;
/** How long a line may be before it is cut. */
const LINE_CHARS = 300;
/** How long a string value of an event may be before it is cut. */
const VALUE_LIMIT = 240;

/**
 * How a line shows: "failed" in red, "quiet" muted, which is what the background scan reported
 * and a command's debug lines.
 */
export type LineTone = "plain" | "failed" | "quiet";

export interface LogLine {
  id: number;
  /** When it happened, as Date.now(). Absent on a line of output, which belongs to the line above it. */
  at?: number;
  /** A command line, what the background scan reported, or one line of output. */
  text: string;
  /** At the right: how long a command ran and how it ended, or what a stopped scan does next. */
  note: string;
  tone: LineTone;
}

interface ActivityLog {
  /** Oldest first, at most LINE_LIMIT lines. */
  lines: LogLine[];
  /** How many lines the limit removed since the app opened. */
  dropped: number;
  /** Whether the commands run from now on also log at debug level (`--verbose`). */
  debug: boolean;
  /** Whether the log's sheet shows. */
  open: boolean;
}

export const useActivityLog = create<ActivityLog>()(() => ({
  debug: false,
  dropped: 0,
  lines: [],
  open: false,
}));

/** Shows or hides the activity log. */
export const openActivityLog = (open = true) => {
  useActivityLog.setState({ open });
};

/** Turns debug output on or off for the commands run from now on. */
export const setDebugOutput = (debug: boolean) => {
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

let nextId = 1;

const take = () => {
  const id = nextId;
  nextId += 1;
  return id;
};

/** Lines of output, which go under the line they belong to. */
const under = (texts: readonly string[], tone: LineTone): LogLine[] =>
  texts.map((text) => ({ id: take(), note: "", text: clip(text), tone }));

/**
 * Adds `added` at the end of the log, or in place of line `inPlaceOf` when the log still holds
 * it. The oldest lines beyond LINE_LIMIT go.
 */
const put = (added: LogLine[], inPlaceOf?: number) => {
  useActivityLog.setState((s) => {
    const index =
      inPlaceOf === undefined
        ? -1
        : s.lines.findIndex((line) => line.id === inPlaceOf);
    const lines =
      index === -1
        ? [...s.lines, ...added]
        : [...s.lines.slice(0, index), ...added, ...s.lines.slice(index + 1)];
    const over = Math.max(0, lines.length - LINE_LIMIT);
    return { dropped: s.dropped + over, lines: lines.slice(over) };
  });
};

/** The commands that run: when each started and its command line, by the id of its line. */
const running = new Map<number, { at: number; text: string }>();

/** Records a command as it starts, and returns the id of its line. */
export const logCommandStart = (args: readonly string[]): number => {
  const id = take();
  const start = { at: Date.now(), text: commandLine(args) };
  running.set(id, start);
  put([{ ...start, id, note: "running", tone: "plain" }]);
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

/**
 * How a command failed, in words. Undefined when it did not fail: it exited 0, or the user
 * cancelled it.
 */
const failureOf = (ended: Ended): string | undefined => {
  if (ended.startError !== undefined) {
    return "couldn't start";
  }
  if (ended.timedOut) {
    return "timed out";
  }
  if (ended.cancelled) {
    return undefined;
  }
  if (ended.exitCode === null) {
    return "stopped by the system";
  }
  return ended.exitCode === EXIT_CODES.ok
    ? undefined
    : `exit ${ended.exitCode}`;
};

/**
 * An event of a failed command as the lines that say why it failed: an error as its message, and
 * its hint when it has one, as a terminal shows them. The result, which only repeats that the
 * command failed, gives no line.
 */
const failureEvent = (event: CliEvent): string[] => {
  if (event.type === "result") {
    return [];
  }
  if (event.type !== "error") {
    return [eventLine(event)];
  }
  return [
    `error: ${redact(event.message)}`,
    ...(event.hint === undefined ? [] : [`hint: ${redact(event.hint)}`]),
  ];
};

/**
 * The last lines a failed command wrote, which say why: its warnings and whatever else it wrote
 * to stderr, then its events on stdout, the error among them. For a command that could not start,
 * the reason.
 */
const failureLines = (ended: Ended): string[] => {
  if (ended.startError !== undefined) {
    return [redact(ended.startError)];
  }
  const invalid = (stream: "stdout" | "stderr") =>
    ended.invalid.filter((l) => l.stream === stream).map((l) => redact(l.line));
  return [
    ...ended.logs.filter((l) => l.level !== "debug").map(logLine),
    ...invalid("stderr"),
    ...ended.events.flatMap(failureEvent),
    ...invalid("stdout"),
  ].slice(-FAILURE_LINES);
};

/**
 * Records how the command of line `id` ended: its line says how long it ran, and for a failure
 * how it failed, with its last lines under it. Its debug lines, which it writes while Debug
 * output is on, go under it too. A command whose line the limit removed stays gone, unless it
 * failed: a failure is added again at the end.
 */
export const logCommandEnd = (id: number, ended: Ended) => {
  const start = running.get(id);
  if (start === undefined) {
    return;
  }
  running.delete(id);
  const took = formatDuration(Date.now() - start.at);
  const failure = failureOf(ended);
  const shown = useActivityLog.getState().lines.some((line) => line.id === id);
  if (failure === undefined && !shown) {
    return;
  }
  const ending = ended.cancelled && failure === undefined ? "cancelled" : "";
  const note =
    failure === undefined
      ? [ending, took].filter((part) => part !== "").join(" · ")
      : [
          `FAILED ${failure}`,
          ...(ended.startError === undefined ? [took] : []),
        ].join(" · ");
  const debug = ended.logs
    .filter((l) => l.level === "debug")
    .map(logLine)
    .slice(-DEBUG_LINES);
  put(
    [
      { ...start, id, note, tone: failure === undefined ? "plain" : "failed" },
      ...under(debug, "quiet"),
      ...(failure === undefined ? [] : under(failureLines(ended), "failed")),
    ],
    id
  );
};

/**
 * Records something the background scan reported, as one quiet line. A failure is red, and keeps
 * the last FAILURE_LINES lines of `output` under it.
 */
export const logBackground = (
  text: string,
  {
    failed = false,
    note = "",
    output = [],
  }: {
    failed?: boolean;
    note?: string;
    output?: readonly string[];
  } = {}
) => {
  const tone = failed ? "failed" : "quiet";
  put([
    { at: Date.now(), id: take(), note, text: clip(redact(text)), tone },
    ...under(output.slice(-FAILURE_LINES).map(redact), tone),
  ]);
};

/** The log as plain text, to paste into an issue: a line of output is indented under its line. */
export const logText = (lines: readonly LogLine[]): string =>
  lines
    .map((line) =>
      line.at === undefined
        ? `    ${line.text}`
        : [
            new Date(line.at).toISOString(),
            line.text,
            ...(line.note === "" ? [] : [line.note]),
          ].join("  ")
    )
    .join("\n");
