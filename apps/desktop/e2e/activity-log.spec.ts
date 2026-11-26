// The activity log: the last lines of what the app ran, a click's commands and the app's own, and
// of what went wrong with the background scan. A failure keeps its last lines, in the CLI's own
// words. The palette and Settings open it.
import type { Page } from "@playwright/test";

import { settled } from "./harness/animations.ts";
import { expect, serveLogPath, test } from "./harness/cli.ts";

const log = (page: Page) => page.getByRole("dialog", { name: "Activity log" });
/** The lines of the log whose text holds `text`, oldest first. */
const lines = (page: Page, text: string) =>
  log(page)
    .getByRole("list", { name: "Activity log lines" })
    .getByRole("listitem")
    .filter({ hasText: text });
const checkButton = (page: Page) =>
  page.getByRole("button", { exact: true, name: "Check updates" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });

const openFromPalette = async (page: Page) => {
  // ⌘K opens nothing until the startup checks pass.
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "false");
  await page.keyboard.press("Control+K");
  await page
    .getByRole("dialog")
    .getByRole("option")
    .filter({ hasText: "Show activity log" })
    .click();
  await expect(log(page)).toBeVisible();
  await settled(log(page));
};

test("a check that succeeds is one line, and one that fails keeps its last lines, which Copy copies", async ({
  cli,
  context,
  page,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();

  cli.register(["skill", "check-updates"], {
    events: [
      {
        current: 1,
        phase: "fetch",
        subject: "https://github.com/acme/skills",
        total: 1,
        type: "progress",
      },
      { ok: true, type: "result" },
    ],
  });
  await checkButton(page).click();
  await expect(toasts(page).getByText(/up to date/u)).toBeVisible();

  cli.register(["skill", "check-updates"], {
    events: [
      {
        code: "source",
        hint: "check your network",
        message:
          "cannot fetch https://me:s3cret-token@git.example.com/acme/skills?access_token=s3cret-token",
        type: "error",
      },
      { ok: false, type: "result" },
    ],
    exitCode: 3,
    logs: [{ level: "warn", message: "the fetch of acme/skills timed out" }],
  });
  await checkButton(page).click();
  await expect(toasts(page).getByText(/^Couldn't reach/u)).toBeVisible();

  await openFromPalette(page);
  // Oldest first: what the launch ran by itself, the check that passed, then the failed one.
  await expect(lines(page, "agentx doctor")).toHaveCount(1);
  await expect(lines(page, "background scan started")).toHaveCount(1);
  const checks = lines(page, "agentx skill check-updates");
  await expect(checks).toHaveCount(2);
  await expect(checks.nth(1)).toContainText("FAILED exit 3");
  await expect(log(page).getByText("1 failed")).toBeVisible();

  // Under the failed check, its warning, its error and its hint, as the CLI wrote them, with the
  // credentials of a URL replaced.
  await expect(lines(page, "the fetch of acme/skills timed out")).toHaveText([
    "the fetch of acme/skills timed out",
  ]);
  await expect(lines(page, "check your network")).toHaveText([
    "check your network",
  ]);
  await expect(
    lines(
      page,
      "cannot fetch https://***@git.example.com/acme/skills?access_token=***"
    )
  ).toHaveCount(1);
  // The check that passed keeps no output.
  await expect(lines(page, "progress")).toHaveCount(0);

  await log(page).getByRole("button", { exact: true, name: "Copy" }).click();
  const copied = await page.evaluate(
    async () => await navigator.clipboard.readText()
  );
  expect(copied).toContain("agentx skill check-updates  FAILED exit 3");
  expect(copied).toContain("    the fetch of acme/skills timed out");
  expect(copied).not.toContain("s3cret-token");
});

test("Settings opens the log, and Debug output runs later commands and serve with --verbose", async ({
  cli,
  page,
}) => {
  await page.goto("/#/settings");
  await page
    .getByRole("region", { exact: true, name: "Troubleshooting" })
    .getByRole("button", { name: "Show activity log" })
    .click();
  await expect(log(page)).toBeVisible();
  await log(page).getByRole("switch", { name: "Debug output" }).click();
  // Serve follows the switch: the supervisor restarts it to log at debug level.
  expect(cli.serveVerbose).toEqual([true]);
  await page.keyboard.press("Escape");
  await expect(log(page)).toBeHidden();

  // Check updates reads the import preview again, with the flag too.
  cli.register(["skill", "import", "--verbose"]);
  cli.register(["skill", "check-updates", "--verbose"], {
    logs: [
      { level: "debug", message: "git fetch --quiet src-1: exit status 0" },
    ],
  });
  await page.getByRole("link", { name: "Skills" }).click();
  await checkButton(page).click();
  await expect(toasts(page).getByText(/up to date/u)).toBeVisible();

  await openFromPalette(page);
  // The check ran with the flag, and its debug line is under it.
  await expect(lines(page, "agentx skill check-updates --verbose")).toHaveCount(
    1
  );
  await expect(
    lines(page, "git fetch --quiet src-1: exit status 0")
  ).toHaveText(["git fetch --quiet src-1: exit status 0"]);
});

test("a drag selects the text of the lines, so any part of the log can be copied", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await openFromPalette(page);
  const text = lines(page, "agentx version").first().locator("span").nth(1);
  const box = await text.boundingBox();
  if (box === null) {
    throw new Error("the line has no box");
  }
  await page.mouse.move(box.x + 1, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width - 1, box.y + box.height / 2, {
    steps: 5,
  });
  await page.mouse.up();
  await expect
    .poll(
      async () => await page.evaluate(() => window.getSelection()?.toString())
    )
    .toContain("agentx version");
});

test("Open serve.log shows the serve log file in the file manager", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await openFromPalette(page);
  await log(page).getByRole("button", { name: "Open serve.log" }).click();
  await expect.poll(() => cli.reveals).toEqual([[serveLogPath]]);
});

test("the log opens 1120 wide, its left edge drags its width up to 90% of the window, and it keeps that width", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await openFromPalette(page);
  const width = async () => {
    const box = await log(page).boundingBox();
    return Math.round(box?.width ?? 0);
  };
  expect(await width()).toBe(1120);

  // Dragged far to the left, the sheet stops at 90% of the 1280-wide window.
  const handle = log(page).getByRole("separator", { name: "Resize" });
  const box = await handle.boundingBox();
  const y = (box?.y ?? 0) + (box?.height ?? 0) / 2;
  await page.mouse.move(box?.x ?? 0, y);
  await page.mouse.down();
  await page.mouse.move(0, y, { steps: 5 });
  await page.mouse.up();
  expect(await width()).toBe(1152);

  // The sheet keeps its own width, apart from the other sheets', and opens again at it, also after
  // the app restarts.
  const kept = await page.evaluate(() =>
    localStorage.getItem("agentx.sheet-widths")
  );
  expect(kept).toBe(JSON.stringify({ "activity-log": 1152 }));
  await page.keyboard.press("Escape");
  await expect(log(page)).toBeHidden();
  await openFromPalette(page);
  expect(await width()).toBe(1152);
  cli.restart();
  await page.reload();
  await openFromPalette(page);
  expect(await width()).toBe(1152);
});
