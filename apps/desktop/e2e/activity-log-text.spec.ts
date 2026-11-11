// What the activity log keeps of a command (src/store/activity-log.ts), as tables over its pure
// functions: the credentials it replaces, the fields it leaves out, how it quotes a command line,
// and what it keeps of a long output. No page: the module runs here, as the app runs it.
import { expect, test } from "@playwright/test";

import type { CliEvent, LogEvent } from "#/cli/events.gen.ts";
import {
  commandLine,
  eventLine,
  logCommandEnd,
  logCommandStart,
  redact,
  statusOf,
  useActivityLog,
} from "#/store/activity-log.ts";

test("a URL's credentials are replaced: its user and password, and its query parameters that name one", () => {
  const cases: [string, string][] = [
    ["https://me:s3cret@host.example/acme", "https://***@host.example/acme"],
    [
      "https://host.example/acme?access_token=abc123&ref=main",
      "https://host.example/acme?access_token=***&ref=main",
    ],
    [
      "cannot fetch https://host.example/r.git?private_token=abc&x=1#main: denied",
      "cannot fetch https://host.example/r.git?private_token=***&x=1#main: denied",
    ],
    ["https://h.example/r?password=p%40ss", "https://h.example/r?password=***"],
    [
      "https://h.example/r?a=1&API_KEY=k&auth=z",
      "https://h.example/r?a=1&API_KEY=***&auth=***",
    ],
    [
      "https://h.example/r?X-Amz-Signature=ff00",
      "https://h.example/r?X-Amz-Signature=***",
    ],
    [
      "https://h.example/r?client_secret=s;sig=1",
      "https://h.example/r?client_secret=***;sig=***",
    ],
    // A parameter that names no credential stays.
    [
      "https://h.example/r?ref=main&page=2",
      "https://h.example/r?ref=main&page=2",
    ],
  ];
  for (const [text, safe] of cases) {
    expect(redact(text)).toBe(safe);
  }
});

test("a command line is quoted for a shell, with its credentials replaced", () => {
  const cases: [string[], string][] = [
    [["skill", "check-updates"], "agentx skill check-updates"],
    [
      ["skill", "publish", "pdf", "-m", "fix it's typo"],
      "agentx skill publish pdf -m 'fix it'\\''s typo'",
    ],
    // A shell expands a tilde and starts a comment at a hash.
    [["import", "~/export.json"], "agentx import '~/export.json'"],
    [
      ["source", "add", "acme/skills#main"],
      "agentx source add 'acme/skills#main'",
    ],
    [["config", "set", "label", "#1"], "agentx config set label '#1'"],
    [
      ["source", "add", "https://host.example/r?access_token=abc123"],
      "agentx source add 'https://host.example/r?access_token=***'",
    ],
  ];
  for (const [args, line] of cases) {
    expect(commandLine(args)).toBe(line);
  }
});

test("an event keeps no file content and no patch, only their length", () => {
  const file = {
    binary: false,
    content: "API_KEY=sk-live-123\nmore",
    name: "pdf",
    path: ".env",
    schema_version: 2,
    size: 24,
    type: "skill_file",
  } satisfies CliEvent;
  expect(eventLine(file)).toBe(
    'skill_file {"binary":false,"content":"24 characters left out","name":"pdf","path":".env","size":24}'
  );
  const diff = {
    name: "pdf",
    patch: "@@ -1 +1 @@\n-token: old\n+token: new\n",
    path: "SKILL.md",
    schema_version: 2,
    status: "modified",
    type: "diff",
  } satisfies CliEvent;
  expect(eventLine(diff)).toBe(
    'diff {"name":"pdf","patch":"36 characters left out","path":"SKILL.md","status":"modified"}'
  );
});

const log = (level: LogEvent["level"], message: string): LogEvent => ({
  level,
  message,
  schema_version: 2,
  type: "log",
});

/** Records one run that ended with `events` and `logs`, and returns its entry. */
const record = (exitCode: number, events: CliEvent[], logs: LogEvent[]) => {
  const id = logCommandStart(["skill", "check-updates"]);
  logCommandEnd(id, {
    cancelled: false,
    events,
    exitCode,
    invalid: [],
    logs,
    timedOut: false,
  });
  const entry = useActivityLog.getState().entries.find((e) => e.id === id);
  if (entry?.kind !== "command" || entry.ending === undefined) {
    throw new Error("the run was not recorded");
  }
  return { ending: entry.ending, entry };
};

test("a long output keeps its first lines and its last, which say how the run ended", () => {
  const progress: CliEvent[] = Array.from({ length: 260 }, (_, i) => ({
    current: i + 1,
    phase: "fetch",
    schema_version: 2,
    total: 260,
    type: "progress",
  }));
  const error = {
    code: "source",
    message: "cannot fetch acme/skills",
    schema_version: 2,
    type: "error",
  } satisfies CliEvent;
  const { ending } = record(3, [...progress, error], []);
  const { lines, dropped } = ending.events;
  // 261 lines: 50 first, the note, 150 last.
  expect(dropped).toBe(61);
  expect(lines).toHaveLength(201);
  expect(lines[0]).toContain('"current":1,');
  expect(lines[49]).toContain('"current":50,');
  expect(lines[50]).toBe("… 61 lines left out …");
  expect(lines[51]).toContain('"current":112,');
  expect(lines.at(-1)).toContain("cannot fetch acme/skills");
});

test("a run that exited 0 asks for a look when it warned, even on a line the entry left out", () => {
  const debug = Array.from({ length: 300 }, (_, i) => log("debug", `git ${i}`));
  const warned = record(
    0,
    [],
    [
      ...debug.slice(0, 100),
      log("warn", "a placement was skipped"),
      ...debug.slice(100),
    ]
  );
  expect(warned.ending.stderr.lines.join("\n")).not.toContain("a placement");
  expect(statusOf(warned.entry).tone).toBe("attention");
  expect(statusOf(record(0, [], debug).entry).tone).toBe("success");
});

test("a line is cut after its credentials are replaced", () => {
  const long = `${"x".repeat(1990)} https://me:s3cret-token@host.example/acme`;
  const { ending } = record(0, [], [log("warn", long)]);
  const [line] = ending.stderr.lines;
  expect(line).toContain("more characters)");
  expect(line).not.toContain("s3cret");
});
