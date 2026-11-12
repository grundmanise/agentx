// What the activity log keeps of a command (src/store/activity-log.ts), as tables over its pure
// functions: the credentials it replaces, the fields it leaves out, how it quotes a command line,
// what it keeps of a failed command, and how many lines it holds. No page: the module runs here,
// as the app runs it.
import { expect, test } from "@playwright/test";

import type { CliEvent, LogEvent } from "#/cli/events.gen.ts";
import {
  commandLine,
  eventLine,
  LINE_LIMIT,
  logCommandEnd,
  logCommandStart,
  redact,
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

/**
 * Records one run that ended with `events` and `logs`, and returns its line and the lines of
 * output under it.
 */
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
  const { lines } = useActivityLog.getState();
  const at = lines.findIndex((line) => line.id === id);
  if (at === -1) {
    throw new Error("the run was not recorded");
  }
  const rest = lines.slice(at + 1);
  const next = rest.findIndex((line) => line.at !== undefined);
  return {
    line: lines[at],
    output: (next === -1 ? rest : rest.slice(0, next)).map((l) => l.text),
  };
};

test("a command that failed keeps its last 5 lines, which say how the run ended", () => {
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
  const failed = record(3, [...progress, error], []);
  expect(failed.line?.tone).toBe("failed");
  expect(failed.line?.note).toMatch(/^FAILED exit 3 · /u);
  expect(failed.output).toHaveLength(5);
  expect(failed.output[0]).toContain('"current":257,');
  expect(failed.output.at(-1)).toContain("cannot fetch acme/skills");
});

test("a command that succeeded keeps its line only, and its debug lines when it wrote any", () => {
  const passed = record(0, [], [log("warn", "a placement was skipped")]);
  expect(passed.line?.tone).toBe("plain");
  expect(passed.output).toEqual([]);

  const debug = Array.from({ length: 60 }, (_, i) => log("debug", `git ${i}`));
  const verbose = record(0, [], debug);
  // Its last 50.
  expect(verbose.output).toHaveLength(50);
  expect(verbose.output[0]).toBe("debug: git 10");
  expect(verbose.output.at(-1)).toBe("debug: git 59");
});

test("a line is cut after its credentials are replaced", () => {
  const long = `${"x".repeat(290)} https://me:s3cret-token@host.example/acme`;
  const { output } = record(3, [], [log("warn", long)]);
  const [line] = output;
  expect(line).toContain("more characters)");
  expect(line).not.toContain("s3cret");
});

test("the log keeps its last lines: a line beyond the limit removes the oldest one", () => {
  const before = useActivityLog.getState();
  const ids = Array.from({ length: LINE_LIMIT + 100 }, () =>
    logCommandStart(["version"])
  );
  const { lines, dropped } = useActivityLog.getState();
  expect(lines).toHaveLength(LINE_LIMIT);
  expect(dropped).toBe(before.dropped + before.lines.length + 100);
  expect(lines[0]?.id).toBe(ids[100]);
  expect(lines.at(-1)?.id).toBe(ids.at(-1));
});
