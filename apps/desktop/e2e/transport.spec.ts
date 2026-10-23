import type { Page } from "@playwright/test";

import { expect, test } from "./harness/cli.ts";

// Nothing renders CLI data yet, so a harness module calls the transport module and writes what it
// receives into the page: one list item per line, result, serve line or serve exit, as
// "<what> <JSON>".
test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
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

test("a one-shot streams its events as they arrive", async ({ cli, page }) => {
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
        event: { cli_version: "1.2.0", schema_version: 2, type: "version" },
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

test("serve's events arrive in order, with their child id", async ({
  cli,
  page,
}) => {
  expect(
    await page.evaluate(async () => await window.transport.serveStart())
  ).toBe(1);
  await cli.serveEmit([{ type: "inventory" }, { type: "refresh_complete" }]);

  await expect(received(page)).toHaveCount(2);
  expect(await items(page)).toEqual([
    {
      value: {
        childId: 1,
        event: { schema_version: 2, type: "inventory" },
        kind: "event",
      },
      what: "serve",
    },
    {
      value: {
        childId: 1,
        event: { schema_version: 2, type: "refresh_complete" },
        kind: "event",
      },
      what: "serve",
    },
  ]);
  expect(cli.serveStarts).toBe(1);
});

test("a serve request is one line on serve's stdin", async ({ cli, page }) => {
  await page.evaluate(async () => await window.transport.serveStart());
  await page.evaluate(async () => {
    await window.transport.serveRequest({ request_id: "r1", type: "refresh" });
  });

  expect(cli.serveRequests).toHaveLength(1);
  const [line = ""] = cli.serveRequests;
  expect(line).not.toContain("\n");
  const request: unknown = JSON.parse(line);
  expect(request).toEqual({ request_id: "r1", type: "refresh" });
});

test("a serve exit is reported, and later events carry the new child's id", async ({
  cli,
  page,
}) => {
  await page.evaluate(async () => await window.transport.serveStart());
  await cli.serveEmit([{ type: "inventory" }]);
  await cli.serveExit(1, ["first line", "second line"]);
  cli.serveRestart(2);
  await cli.serveEmit([{ type: "inventory" }]);

  await expect(received(page)).toHaveCount(3);
  const [first, exit, next] = await items(page);
  expect(first).toMatchObject({ value: { childId: 1 }, what: "serve" });
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
    window.transport.run(["doctor"]);
  }, url);

  await expect(received(page)).toHaveCount(4);
  const refused = cli.takeRefusals();
  expect(refused).toEqual([
    `"skill add ${url} --tooo x": agentx skill add has no flag --tooo`,
    `"skill addd ${url}": agentx skill has no command "addd"`,
    `"skill list --remote=yes": --remote is a boolean flag and takes no value, but was given "yes"`,
    `"doctor": the test registered no reply`,
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
    ["doctor", "--json"],
  ]);
});
