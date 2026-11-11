// The activity log: every command the app ran this session, a click's and the app's own, with how
// each ended and what it wrote, and what the serve child reported. The palette and Settings open it.
import type { Page } from "@playwright/test";

import { expect, serveLogPath, test } from "./harness/cli.ts";

const log = (page: Page) => page.getByRole("dialog", { name: "Activity log" });
const entries = (page: Page) =>
  log(page).getByRole("list", { name: "Activity log entries" });
/** The entry whose first line is `line`: a command line, or what serve reported. */
const entry = (page: Page, line: string) =>
  entries(page).getByRole("listitem").filter({ hasText: line }).first();
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

test("a check that succeeds and one that fails are entries with their outcome, output and copy", async ({
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
  // Newest first: the failed check, the check that passed, then what the launch ran by itself.
  const checks = entries(page)
    .getByRole("button")
    .filter({ hasText: "agentx skill check-updates" });
  await expect(checks).toHaveCount(2);
  await expect(checks.nth(0)).toContainText(
    "Exit 3, a source or the account remote was unreachable or denied access"
  );
  await expect(checks.nth(1)).toContainText("Exit 0, succeeded");
  await expect(entry(page, "agentx doctor")).toContainText("Exit 0, succeeded");
  await expect(entry(page, "Inventory arrived")).toContainText("agentx serve");

  // The passing check opens to the events it emitted.
  await checks.nth(1).click();
  await expect(
    log(page).getByText('progress {"current":1,"phase":"fetch"', {
      exact: false,
    })
  ).toBeVisible();
  await checks.nth(1).click();

  // The failed one opens to its error and its stderr, with the credentials of a URL replaced.
  await checks.nth(0).click();
  const failed = entry(page, "agentx skill check-updates");
  await expect(failed).toContainText(
    "cannot fetch https://***@git.example.com/acme/skills?access_token=***"
  );
  await expect(failed).toContainText(
    "warn: the fetch of acme/skills timed out"
  );

  await failed.getByRole("button", { exact: true, name: "Copy" }).click();
  const copied = await page.evaluate(
    async () => await navigator.clipboard.readText()
  );
  expect(copied).toContain("$ agentx skill check-updates");
  expect(copied).toContain("Ended: Exit 3");
  expect(copied).toContain("warn: the fetch of acme/skills timed out");
  expect(copied).not.toContain("s3cret-token");
});

test("Settings opens the log, and Debug lines runs later commands and serve with --verbose", async ({
  cli,
  page,
}) => {
  await page.goto("/#/settings");
  await page
    .getByRole("region", { exact: true, name: "Activity log" })
    .getByRole("button", { name: "Show activity log" })
    .click();
  await expect(log(page)).toBeVisible();
  await log(page).getByRole("switch", { name: "Debug lines" }).click();
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
  const check = entry(page, "agentx skill check-updates --verbose");
  await check.getByRole("button").click();
  await expect(check).toContainText(
    "debug: git fetch --quiet src-1: exit status 0"
  );
});

test("Serve log shows the end of serve's log file, and shows the file in the file manager", async ({
  cli,
  page,
}) => {
  cli.answerServeLog([
    '{"type":"log","schema_version":2,"level":"debug","message":"fetching https://me:s3cret-token@host.example.com/acme/skills?token=s3cret-token"}',
    '{"type":"log","schema_version":2,"level":"warn","message":"cannot fetch acme/skills"}',
    "panic: runtime error",
  ]);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await openFromPalette(page);
  await log(page)
    .getByRole("button", { name: /^Serve log/u })
    .click();

  // The file's path, then its lines: a log event as its level and message, any other line as written.
  await expect(log(page).getByText(serveLogPath)).toBeVisible();
  await expect(
    log(page).getByText(
      [
        "debug: fetching https://***@host.example.com/acme/skills?token=***",
        "warn: cannot fetch acme/skills",
        "panic: runtime error",
      ].join("\n")
    )
  ).toBeVisible();

  await log(page).getByRole("button", { name: "Show in file manager" }).click();
  expect(cli.reveals).toEqual([[serveLogPath]]);
});
