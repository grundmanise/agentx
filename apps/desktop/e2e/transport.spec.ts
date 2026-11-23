import type { Page } from "@playwright/test";

import { expect, firstInventory, loadLines, test } from "./harness/cli.ts";

// Inside the Tauri window every command is a JSON-RPC request to the serve child, over its stdin
// and stdout, which the mocked Rust shell relays. A harness module calls the transport module and
// writes what it receives into the page: one list item per line, result, serve line or serve
// exit, as "<what> <JSON>".
test.beforeEach(async ({ cli, page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  // The launch ran version, doctor and the store's reads; each test reads only its own lines.
  await expect.poll(() => cli.commands.length).toBe(2 + loadLines.length);
  cli.commands.splice(0);
  cli.stdin.splice(0);
  await page.addScriptTag({ type: "module", url: "/e2e/harness/transport.ts" });
  await page.waitForFunction(() => "transport" in window);
});

const received = (page: Page) =>
  page.getByRole("list", { name: "Received" }).getByRole("listitem");

/** What the page received, each item as its kind and its parsed JSON. */
const items = async (page: Page) => {
  const texts = await received(page).allTextContents();
  return texts.map((text) => {
    const space = text.indexOf(" ");
    const value: unknown = JSON.parse(text.slice(space + 1));
    return { value, what: text.slice(0, space) };
  });
};

test("a run streams its events as they arrive", async ({ cli, page }) => {
  const version = cli.register(["version"], {
    events: [
      { cli_version: "1.2.0", type: "version" },
      { ok: true, type: "result" },
    ],
    holdAfter: 1,
  });
  await page.evaluate(() => window.transport.run(["version"]));

  // The first event is there while the run still goes.
  await expect(received(page)).toHaveCount(1);
  expect(await items(page)).toEqual([
    {
      value: {
        event: { cli_version: "1.2.0", schema_version: 4, type: "version" },
        kind: "event",
      },
      what: "line",
    },
  ]);

  version.release();
  await expect(received(page)).toHaveCount(3);
  const [, second, result] = await items(page);
  expect(second?.value).toMatchObject({ event: { type: "result" } });
  expect(result).toMatchObject({
    value: {
      cancelled: false,
      events: [{ type: "version" }, { type: "result" }],
      exitCode: 0,
      timedOut: false,
    },
    what: "result",
  });
  expect(cli.commands).toEqual([["version", "--json"]]);
  // The run was one JSON-RPC request, one line on serve's stdin.
  expect(cli.stdin).toHaveLength(1);
  const [line = ""] = cli.stdin;
  expect(line).not.toContain("\n");
  expect(JSON.parse(line)).toEqual({
    id: expect.any(String),
    jsonrpc: "2.0",
    method: "run",
    params: { args: ["version"], deadline_ms: null },
  });
});

test("a reader may stop reading a run's lines", async ({ cli, page }) => {
  const version = cli.register(["version"], {
    events: [
      { cli_version: "1.2.0", type: "version" },
      { ok: true, type: "result" },
    ],
    holdAfter: 1,
  });
  await page.evaluate(() => window.transport.runAndStopReading(["version"]));
  await expect(received(page)).toHaveCount(1);

  version.release();
  await expect(received(page)).toHaveCount(2);
  const [, result] = await items(page);
  expect(result).toMatchObject({
    value: { events: [{ type: "version" }, { type: "result" }], exitCode: 0 },
    what: "result",
  });
});

test("cancel stops a run", async ({ cli, page }) => {
  const doctor = cli.register(["doctor"], {
    events: [{ check: "git", status: "ok", type: "doctor" }],
    holdAfter: 1,
  });
  await page.evaluate(() => window.transport.run(["doctor"]));
  const runId = await doctor.started;
  await expect(received(page)).toHaveCount(1);

  expect(
    await page.evaluate(async (id) => await window.transport.cancel(id), runId)
  ).toBe(true);
  await expect(received(page)).toHaveCount(2);
  const [, result] = await items(page);
  expect(result).toMatchObject({
    value: { cancelled: true, events: [{ type: "doctor" }] },
    what: "result",
  });
  expect(cli.cancels).toEqual([runId]);
});

test("serve's own lines arrive in order, with their child id", async ({
  cli,
  page,
}) => {
  expect(
    await page.evaluate(async () => await window.transport.serveStart())
  ).toBe(1);
  await cli.serveEmit([{ drift: [], name: "pdf", type: "drift" }]);
  await cli.serveLog([{ level: "warn", message: "careful" }]);
  await cli.serveEmit([{ checked_at: "now", type: "update_check" }]);

  await expect(received(page)).toHaveCount(3);
  expect(await items(page)).toEqual([
    {
      value: {
        childId: 1,
        event: { drift: [], name: "pdf", schema_version: 4, type: "drift" },
        kind: "event",
      },
      what: "serve",
    },
    {
      value: {
        childId: 1,
        event: {
          level: "warn",
          message: "careful",
          schema_version: 4,
          type: "log",
        },
        kind: "log",
      },
      what: "serve",
    },
    {
      value: {
        childId: 1,
        event: { checked_at: "now", schema_version: 4, type: "update_check" },
        kind: "event",
      },
      what: "serve",
    },
  ]);
  expect(cli.serveStarts).toBe(1);
});

test("the first run starts serve, whose lines are passed on, in order, once serve is started", async ({
  cli,
  page,
}) => {
  // A page with the harness module alone: the app does not launch, so nothing started serve.
  await page.route("/", async (route) => {
    await route.fulfill({
      body: '<!doctype html><script type="module" src="/e2e/harness/transport.ts"></script>',
      contentType: "text/html",
    });
  });
  cli.restart();
  await page.goto("/");
  await page.waitForFunction(() => "transport" in window);
  cli.commands.splice(0);

  await page.evaluate(() => window.transport.run(["version"]));
  await expect(received(page)).toHaveCount(2);
  expect(cli.serveStarts).toBe(2);
  await cli.serveLog([{ level: "warn", message: "cannot fetch a source" }]);
  await cli.serveEmit([{ drift: [], name: "pdf", type: "drift" }]);
  // Serve is not started yet: its lines wait.
  await expect(received(page)).toHaveCount(2);

  expect(
    await page.evaluate(async () => await window.transport.serveStart())
  ).toBe(2);
  await expect(received(page)).toHaveCount(5);
  const lines = await items(page);
  expect(lines.slice(2)).toEqual([
    {
      value: {
        childId: 2,
        event: expect.objectContaining(firstInventory()),
        kind: "event",
      },
      what: "serve",
    },
    {
      value: {
        childId: 2,
        event: {
          level: "warn",
          message: "cannot fetch a source",
          schema_version: 4,
          type: "log",
        },
        kind: "log",
      },
      what: "serve",
    },
    {
      value: {
        childId: 2,
        event: { drift: [], name: "pdf", schema_version: 4, type: "drift" },
        kind: "event",
      },
      what: "serve",
    },
  ]);
  expect(cli.serveStarts).toBe(2);
});

test("a refresh is a JSON-RPC request, whose answer arrives as a refresh_complete event", async ({
  cli,
  page,
}) => {
  await page.evaluate(async () => await window.transport.serveStart());
  await page.evaluate(async () => {
    await window.transport.serveRequest({ request_id: "r1", type: "refresh" });
  });

  expect(cli.stdin).toHaveLength(1);
  const [line = ""] = cli.stdin;
  expect(line).not.toContain("\n");
  expect(JSON.parse(line)).toEqual({
    id: "r1",
    jsonrpc: "2.0",
    method: "refresh",
  });
  await cli.serveEmit([
    { ok: true, request_id: "r1", scan_counter: 3, type: "refresh_complete" },
  ]);
  await expect(received(page)).toHaveCount(1);
  expect(await items(page)).toEqual([
    {
      value: {
        childId: 1,
        event: {
          instance_id: "instance-1",
          ok: true,
          request_id: "r1",
          scan_counter: 3,
          schema_version: 4,
          type: "refresh_complete",
        },
        kind: "event",
      },
      what: "serve",
    },
  ]);
});

test("a serve exit is reported, and later events carry the new child's id", async ({
  cli,
  page,
}) => {
  await page.evaluate(async () => await window.transport.serveStart());
  const doctor = cli.register(["doctor"], { holdAfter: 0 });
  await page.evaluate(() => window.transport.run(["doctor"]));
  await doctor.started;
  await cli.serveEmit([{ type: "inventory" }]);
  await cli.serveExit(1, ["first line", "second line"]);
  cli.serveRestart(2);
  await cli.serveEmit([{ type: "inventory" }]);

  await expect(received(page)).toHaveCount(4);
  const all = await items(page);
  const [first] = all;
  const [exit, next] = all.filter((item) => item.what !== "error").slice(1);
  expect(first).toMatchObject({ value: { childId: 1 }, what: "serve" });
  // The run the child had not answered fails with how the child ended.
  expect(all.filter((item) => item.what === "error")).toEqual([
    {
      value:
        "Error: agentx serve exited with code 1: second line, which stopped the command",
      what: "error",
    },
  ]);
  expect(exit).toEqual({
    value: {
      childId: 1,
      exitCode: 1,
      restartInMs: 500,
      restarting: true,
      stderr: ["first line", "second line"],
    },
    what: "exit",
  });
  expect(next).toMatchObject({ value: { childId: 2 }, what: "serve" });
});

test("the mock refuses a command line the CLI would refuse", async ({
  cli,
  page,
}) => {
  const url = "https://github.com/acme/skills";
  await page.evaluate((u) => {
    window.transport.run(["skill", "add", u, "--tooo", "x"]);
    window.transport.run(["skill", "addd", u]);
    window.transport.run(["skill", "list", "--remote=yes"]);
    window.transport.run(["skill", "list"]);
  }, url);

  await expect(received(page)).toHaveCount(4);
  const refused = cli.takeRefusals();
  expect(refused).toEqual([
    `"skill add ${url} --tooo x": agentx skill add has no flag --tooo`,
    `"skill addd ${url}": agentx skill has no command "addd"`,
    `"skill list --remote=yes": --remote is a boolean flag and takes no value, but was given "yes"`,
    `"skill list": the test registered no reply`,
  ]);
  const errors = await items(page);
  expect(errors.map((item) => item.what)).toEqual([
    "error",
    "error",
    "error",
    "error",
  ]);
  expect(cli.commands).toEqual([
    ["skill", "add", url, "--tooo", "x", "--json"],
    ["skill", "addd", url, "--json"],
    ["skill", "list", "--remote=yes", "--json"],
    ["skill", "list", "--json"],
  ]);
});
