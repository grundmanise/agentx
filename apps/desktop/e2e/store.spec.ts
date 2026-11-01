import type { Page } from "@playwright/test";

import { expect, firstInventory, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const value = (page: Page, term: string) =>
  page.getByLabel(term, { exact: true });
const staleBar = (page: Page) => page.getByRole("status");

/** Launches the app, waits for the store's reads, and shows the store. */
const launch = async (cli: MockCli, page: Page) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
  await expect.poll(() => cli.commands).toEqual([...launchLines, ...loadLines]);
  await page.addScriptTag({ type: "module", url: "/e2e/harness/store.tsx" });
  await page.waitForFunction(() => "store" in window);
  await expect(value(page, "Counter")).toHaveText("1");
};

/** Sends one refresh request, as after a command, and gives its id. */
const refresh = async (cli: MockCli, page: Page) => {
  const before = cli.serveRequests.length;
  await page.evaluate(async () => {
    await window.store.refresh();
  });
  expect(cli.serveRequests).toHaveLength(before + 1);
  return cli.requestIds.at(-1) ?? "";
};

test("applies a serve child's inventories in order", async ({ cli, page }) => {
  await launch(cli, page);
  await expect(value(page, "Instance")).toHaveText("instance-1");

  await cli.serveEmit([{ ...firstInventory(["codex"]), scan_counter: 2 }]);
  await expect(value(page, "Counter")).toHaveText("2");
  await expect(value(page, "Agents")).toHaveText("1");
  await expect(value(page, "Freshness")).toHaveText("fresh");
});

test("drops an inventory whose counter is lower or equal", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  await cli.serveEmit([{ ...firstInventory(["codex"]), scan_counter: 2 }]);
  await expect(value(page, "Counter")).toHaveText("2");

  await cli.serveEmit([
    { ...firstInventory(["codex", "cursor", "zed"]), scan_counter: 2 },
    { ...firstInventory([]), scan_counter: 1 },
    { ...firstInventory(["codex", "cursor"]), scan_counter: 3 },
  ]);
  // The counter 3 inventory shows: the two before it were dropped, not applied and replaced.
  await expect(value(page, "Counter")).toHaveText("3");
  await expect(value(page, "Agents")).toHaveText("2");
});

test("after a restart, shows the inventory as stale until the new child's first one", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  await cli.serveEmit([{ ...firstInventory(), scan_counter: 4 }]);
  await expect(value(page, "Counter")).toHaveText("4");

  await cli.serveExit(null);
  await expect(staleBar(page)).toHaveText("Reconnecting to agentx…");
  await expect(value(page, "Freshness")).toHaveText("stale");
  // The inventory shown stays while the new child scans.
  await expect(value(page, "Agents")).toHaveText("2");

  cli.serveRestart(2);
  await cli.serveEmit([firstInventory(["codex"])]);
  // A new instance applies its counter 1, lower than the last child's 4.
  await expect(value(page, "Instance")).toHaveText("instance-2");
  await expect(value(page, "Counter")).toHaveText("1");
  await expect(value(page, "Agents")).toHaveText("1");
  await expect(staleBar(page)).toHaveCount(0);
});

test("a restarted child that ends for good before its first line says so", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  await cli.serveExit(null);
  await expect(staleBar(page)).toHaveText("Reconnecting to agentx…");

  cli.serveRestart(2);
  await cli.serveExit(6, ["another agentx serve is running"], null);
  await expect(staleBar(page)).toContainText(
    "Couldn't read this machine: another agentx serve is running"
  );
  await expect(
    staleBar(page).getByRole("button", { name: "Retry" })
  ).toBeVisible();
});

test("ignores an old child's events after a restart", async ({ cli, page }) => {
  await launch(cli, page);
  const sentToOld = await refresh(cli, page);

  await cli.serveExit(null);
  cli.serveRestart(2);
  await cli.serveEmit([firstInventory(["codex"])]);
  // The request the old child did not answer went to the new one, with a new id.
  await expect.poll(() => cli.serveRequests.length).toBe(2);
  const [, sentToNew = ""] = cli.requestIds;
  expect(sentToNew).not.toBe(sentToOld);

  await cli.serveEmit(
    [
      { ...firstInventory(["codex", "cursor", "zed"]), scan_counter: 9 },
      { ok: false, request_id: sentToOld, type: "refresh_complete" },
      { ok: false, request_id: sentToNew, type: "refresh_complete" },
    ],
    1
  );
  // The old child's events are handled before serveEmit returns: none changed the store.
  const after = await page.evaluate(() => window.store.state());
  expect(after.applied).toMatchObject({ counter: 1, instanceId: "instance-2" });
  expect(after.freshness).toEqual({ kind: "fresh" });
  expect(Object.keys(after.refreshes)).toEqual([sentToNew]);

  await cli.serveEmit([
    {
      ok: true,
      request_id: sentToNew,
      scan_counter: 1,
      type: "refresh_complete",
    },
  ]);
  await expect(value(page, "Pending refreshes")).toHaveText("0");
  await expect(value(page, "Instance")).toHaveText("instance-2");
  await expect(value(page, "Agents")).toHaveText("1");
  await expect(value(page, "Freshness")).toHaveText("fresh");
});

test("shows serve's warnings in the activity list, not its debug lines or an old child's", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  const warning =
    "update check: https://github.com/me/skills: git fetch: could not resolve host; not checked: pdf";
  const activity = page
    .getByRole("list", { name: "Activity" })
    .getByRole("listitem");

  await cli.serveLog([
    { level: "debug", message: "update check: 1 source" },
    { level: "warn", message: warning },
  ]);
  await expect(activity).toHaveText([warning]);
  await expect(activity).toHaveAttribute("data-tone", "attention");

  await cli.serveExit(null);
  cli.serveRestart(2);
  await cli.serveEmit([firstInventory(["codex"])]);
  await expect(value(page, "Instance")).toHaveText("instance-2");
  await cli.serveLog([{ level: "warn", message: "reconcile: pdf" }], 1);
  // The old child's warning was handled before serveLog returned: it added nothing.
  await expect(activity).toHaveText([warning]);
});

test("ignores the inventory a one-shot writes, and keeps its outcome", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  cli.register(["scan"], {
    events: [{ ...firstInventory(["codex"]), scan_counter: 7 }],
  });

  await page.evaluate(() => {
    window.store.run(["scan"], { label: "scan", success: "Scanned" });
  });
  await expect(
    page.getByRole("list", { name: "Activity" }).getByRole("listitem")
  ).toHaveText(["Scanned"]);
  await expect(value(page, "Counter")).toHaveText("1");
  await expect(value(page, "Agents")).toHaveText("2");
});

test("a refresh stays pending through a scan that began before it", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  const id = await refresh(cli, page);
  await expect(value(page, "Pending refreshes")).toHaveText("1");

  // The scan that was running when the request came: its inventory does not answer it.
  await cli.serveEmit([{ ...firstInventory(["codex"]), scan_counter: 2 }]);
  await expect(value(page, "Counter")).toHaveText("2");
  expect(
    Object.keys(await page.evaluate(() => window.store.state().refreshes))
  ).toEqual([id]);

  await cli.serveEmit([
    { ...firstInventory(), scan_counter: 3 },
    { ok: true, request_id: id, scan_counter: 3, type: "refresh_complete" },
  ]);
  await expect(value(page, "Pending refreshes")).toHaveText("0");
  await expect(value(page, "Counter")).toHaveText("3");
});

test("accepts an acknowledgement for an unchanged inventory", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  const id = await refresh(cli, page);

  await cli.serveEmit([
    { ok: true, request_id: id, scan_counter: 1, type: "refresh_complete" },
  ]);
  await expect(value(page, "Pending refreshes")).toHaveText("0");
  await expect(value(page, "Freshness")).toHaveText("fresh");
  await expect(value(page, "Counter")).toHaveText("1");
});

test("a failed refresh says so, and Retry asks again", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  const id = await refresh(cli, page);

  await cli.serveEmit([
    {
      error: "skills folder changed during the scan",
      ok: false,
      request_id: id,
      type: "refresh_complete",
    },
  ]);
  await expect(staleBar(page)).toContainText(
    "Couldn't read this machine: skills folder changed during the scan"
  );
  await expect(value(page, "Freshness")).toHaveText("failed");

  await staleBar(page).getByRole("button", { name: "Retry" }).click();
  await expect.poll(() => cli.serveRequests.length).toBe(2);
  const [, retried] = cli.requestIds;
  expect(retried).not.toBe(id);
  // The serve child already runs: Retry starts none.
  expect(cli.serveStarts).toBe(1);

  await cli.serveEmit([
    {
      ok: true,
      request_id: retried,
      scan_counter: 1,
      type: "refresh_complete",
    },
  ]);
  await expect(staleBar(page)).toHaveCount(0);
});
