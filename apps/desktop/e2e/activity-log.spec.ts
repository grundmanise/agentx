// The activity log: the last lines of what the app ran, a click's commands and the app's own, and
// of what the background scan reported. A failure keeps its last lines. The palette and Settings
// open it.
import type { Page } from "@playwright/test";

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
  await page.keyboard.press("Control+K");
  await page
    .getByRole("dialog")
    .getByRole("option")
    .filter({ hasText: "Show activity log" })
    .click();
  await expect(log(page)).toBeVisible();
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
  await expect(lines(page, "inventory updated: ")).toHaveCount(1);
  const checks = lines(page, "agentx skill check-updates");
  await expect(checks).toHaveCount(2);
  await expect(checks.nth(1)).toContainText("FAILED exit 3");
  await expect(log(page).getByText("1 failed")).toBeVisible();

  // Under the failed check, its warning and its error, with the credentials of a URL replaced.
  await expect(
    lines(page, "warn: the fetch of acme/skills timed out")
  ).toHaveCount(1);
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
  expect(copied).toContain("    warn: the fetch of acme/skills timed out");
  expect(copied).not.toContain("s3cret-token");
});

test("Settings opens the log, and Debug output runs later commands and serve with --verbose", async ({
  cli,
  page,
}) => {
  await page.goto("/#/settings");
  await page
    .getByRole("region", { exact: true, name: "Activity log" })
    .getByRole("button", { name: "Show activity log" })
    .click();
  await expect(log(page)).toBeVisible();
  await log(page).getByRole("switch", { name: "Debug output" }).click();
  // Serve follows the switch: the supervisor restarts it to log at debug level.
  expect(cli.serveVerbose).toEqual([true]);
  await page.keyboard.press("Escape");
  await expect(log(page)).toBeHidden();

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
    lines(page, "debug: git fetch --quiet src-1: exit status 0")
  ).toHaveCount(1);
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
