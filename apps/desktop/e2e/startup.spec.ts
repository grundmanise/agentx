import type { Page } from "@playwright/test";

import {
  cliVersion,
  doctorWith,
  expect,
  firstInventory,
  test,
} from "./harness/cli.ts";

const bundled = "/Applications/agentx.app/Contents/MacOS/agentx";
const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const layer = (page: Page) =>
  page.getByRole("region", { name: "Startup checks" });
const title = (page: Page) => layer(page).getByRole("heading", { level: 1 });
const rows = (page: Page) =>
  page.getByRole("list", { name: "Checks" }).getByRole("listitem");
const row = (page: Page, label: string) =>
  rows(page).filter({ hasText: label });
const inbox = (page: Page) => page.getByRole("heading", { name: "Inbox" });

const oldGit = {
  check: "git",
  detail: "git 2.34.1 is older than 2.40",
  hint: "install git 2.40 or newer, then run agentx doctor again",
  status: "fail",
  type: "doctor",
} as const;

/** Marks the page's root with data-layer-shown once the startup layer has shown, even briefly. */
const watchLayer = async (page: Page) => {
  await page.addInitScript(() => {
    new MutationObserver(() => {
      if (document.querySelector('[aria-label="Startup checks"]') !== null) {
        document.documentElement.dataset.layerShown = "";
      }
    }).observe(document, { childList: true, subtree: true });
  });
};
const layerShown = (page: Page) => page.locator("html[data-layer-shown]");

/** Opens manual mode, as Settings › Run checks will; until it exists, a harness module does. */
const runChecks = async (page: Page) => {
  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/run-checks.ts",
  });
};

const unusableRepo = {
  check: "account_repo",
  detail: "/home/me/.agentx/account.git: not a git repository",
  hint: "move /home/me/.agentx/account.git aside, then run agentx doctor again",
  status: "fail",
  type: "doctor",
} as const;

/** A library entry as an inventory lists it. */
const skill = (name: string, more: { fork_id?: string; source?: string }) => ({
  content_hash: "sha256:0",
  kind: "managed" as const,
  name,
  placements: [],
  universal: [],
  ...more,
});

test.beforeEach(async ({ context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
});

test("a passing launch checks the machine out of sight and opens the Inbox", async ({
  cli,
  page,
}) => {
  const doctor = cli.register(["doctor"], {
    events: doctorWith(),
    holdAfter: 0,
  });
  await watchLayer(page);
  await page.goto("/");
  await doctor.started;
  // While the checks run, the shell shows the Inbox selected, its panel is empty, and no layer shows.
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "true");
  await expect(page.getByRole("link", { name: "Inbox" })).toHaveAttribute(
    "aria-current",
    "page"
  );
  await expect(inbox(page)).toHaveCount(0);
  doctor.release();

  await expect(inbox(page)).toBeVisible();
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "false");
  await expect(layerShown(page)).toHaveCount(0);
  expect(cli.commands).toEqual(launchLines);
  expect(cli.serveStarts).toBe(1);
});

test("a launch that runs over 2 seconds says that the checks run", async ({
  cli,
  page,
}) => {
  const doctor = cli.register(["doctor"], {
    events: doctorWith(),
    holdAfter: 0,
  });
  await watchLayer(page);
  // Stop the page's timers before it loads; the test moves the clock on.
  await page.clock.install({ time: new Date("2026-10-23T10:00:00Z") });
  await page.clock.pauseAt(new Date("2026-10-23T10:00:01Z"));
  await page.goto("/");
  await doctor.started;
  const checking = page
    .getByRole("status")
    .filter({ hasText: "Checking this machine" });
  await page.clock.runFor(1999);
  await expect(checking).toHaveCount(0);
  await page.clock.runFor(1);
  await expect(checking).toBeVisible();

  doctor.release();
  await expect(inbox(page)).toBeVisible();
  await expect(checking).toHaveCount(0);
  await expect(layerShown(page)).toHaveCount(0);
});

test("a CLI that is not found stops at check 1 and runs nothing", async ({
  cli,
  page,
}) => {
  cli.resolveTo({ found: null, path: null });
  await page.goto("/");
  await expect(title(page)).toHaveText("Reinstall agentx");
  await expect(
    row(page, "agentx command line").getByText("Failed:", { exact: true })
  ).toBeVisible();
  await expect(
    row(page, "Watching this machine").getByText("Waiting:", { exact: true })
  ).toBeVisible();
  await expect(layer(page)).toContainText(
    "Nothing on this machine was changed."
  );
  expect(cli.commands).toEqual([]);
  expect(cli.serveStarts).toBe(0);
});

test("an old git blocks with the CLI's hint as the fix", async ({
  cli,
  page,
}) => {
  cli.register(["doctor"], { events: [oldGit], exitCode: 2 });
  await page.goto("/");
  await expect(title(page)).toHaveText("Update git to continue");
  const git = row(page, "git 2.40 or newer");
  await expect(git.getByText("Failed:", { exact: true })).toBeVisible();
  await expect(git).toContainText(oldGit.detail);
  await expect(
    git.getByRole("button", { name: `Copy fix: ${oldGit.hint}` })
  ).toHaveText(oldGit.hint);
  await expect(
    row(page, "Home folder is writable").getByText("Waiting:", { exact: true })
  ).toBeVisible();
  expect(cli.commands).toEqual(launchLines);
  expect(cli.serveStarts).toBe(0);
});

test("an unusable account repo blocks at check 4", async ({ cli, page }) => {
  cli.register(["doctor"], { events: doctorWith(unusableRepo), exitCode: 8 });
  await page.goto("/");
  await expect(title(page)).toHaveText("agentx can't open its account repo");
  const account = row(page, "Account repo opens");
  await expect(account.getByText("Failed:", { exact: true })).toBeVisible();
  await expect(account).toContainText("not a git repository");
  expect(cli.commands).toEqual(launchLines);
  expect(cli.serveStarts).toBe(0);
});

test("a warning does not block, and manual mode shows it", async ({
  cli,
  page,
}) => {
  const lock = {
    check: "lock",
    detail: "held by agentx (pid 4242)",
    hint: "wait for the other agentx command to finish",
    status: "warn",
    type: "doctor",
  } as const;
  cli.register(["doctor"], { events: doctorWith(lock) });
  await watchLayer(page);
  await page.goto("/");
  await expect(inbox(page)).toBeVisible();
  await expect(layerShown(page)).toHaveCount(0);
  expect(cli.serveStarts).toBe(1);

  await runChecks(page);
  await expect(title(page)).toHaveText("You're all set");
  const account = row(page, "Account repo opens");
  await expect(
    account.getByText("Passed with a warning:", { exact: true })
  ).toBeVisible();
  await expect(account).toContainText(lock.detail);
  await expect(
    account.getByRole("button", { name: `Copy fix: ${lock.hint}` })
  ).toBeVisible();
});

test("serve refused fails check 5 with serve's message and hint", async ({
  cli,
  page,
}) => {
  const refused = {
    code: "refused",
    hint: "another agentx serve holds /home/me/.agentx/serve.lock; close it first",
    message: "agentx serve is already running for /home/me/.agentx",
    type: "error",
  } as const;
  cli.onServeStart({ events: [refused], exit: { code: 6 } });
  await page.goto("/");
  await expect(title(page)).toHaveText("agentx can't watch this machine");
  const watching = row(page, "Watching this machine");
  await expect(watching.getByText("Failed:", { exact: true })).toBeVisible();
  await expect(watching).toContainText(refused.message);
  await expect(
    watching.getByRole("button", { name: `Copy fix: ${refused.hint}` })
  ).toBeVisible();
  expect(cli.commands).toEqual(launchLines);
});

test("Retry runs everything again", async ({ cli, page }) => {
  cli.register(["doctor"], { events: [oldGit], exitCode: 2 });
  await page.goto("/");
  await expect(title(page)).toHaveText("Update git to continue");

  const doctor = cli.register(["doctor"], {
    events: doctorWith(),
    holdAfter: 0,
  });
  await page.getByRole("button", { name: "Retry" }).click();
  await doctor.started;
  await expect(page.getByRole("button", { name: "Retry" })).toBeDisabled();
  await expect(title(page)).toHaveText("Checking this machine");
  doctor.release();

  // A pass closes the layer at once.
  await expect(inbox(page)).toBeVisible();
  await expect(layer(page)).toHaveCount(0);
  expect(cli.commands).toEqual([...launchLines, ...launchLines]);
  expect(cli.serveStarts).toBe(1);
});

test("Enter retries", async ({ cli, page }) => {
  cli.register(["doctor"], { events: [oldGit], exitCode: 2 });
  await page.goto("/");
  await expect(title(page)).toHaveText("Update git to continue");
  cli.register(["doctor"], { events: doctorWith() });
  await page.keyboard.press("Enter");
  await expect(inbox(page)).toBeVisible();
  expect(cli.commands).toEqual([...launchLines, ...launchLines]);
});

test("Copy report copies doctor's output in the CLI's layout", async ({
  cli,
  page,
}) => {
  cli.register(["doctor"], { events: [oldGit], exitCode: 2 });
  await page.goto("/");
  await expect(title(page)).toHaveText("Update git to continue");
  await page.getByRole("button", { name: "Copy report" }).click();
  await expect(page.getByText("Report copied")).toBeVisible();
  const text = await page.evaluate(
    async () => await navigator.clipboard.readText()
  );
  expect(text).toBe(
    [
      `✗ git  fail  ${oldGit.detail}`,
      "",
      "1 issue",
      `✗ git  ${oldGit.detail}`,
      `       hint: ${oldGit.hint}`,
      "",
    ].join("\n")
  );
});

test("⌘C on a fix chip copies the CLI's hint", async ({ cli, page }) => {
  cli.register(["doctor"], { events: [oldGit], exitCode: 2 });
  await page.goto("/");
  await expect(title(page)).toHaveText("Update git to continue");
  await page.getByRole("button", { name: `Copy fix: ${oldGit.hint}` }).focus();
  await page.keyboard.press("ControlOrMeta+C");
  await expect(page.getByText("Fix copied")).toBeVisible();
  expect(
    await page.evaluate(async () => await navigator.clipboard.readText())
  ).toBe(oldGit.hint);
});

test("manual mode shows every check and waits for Open agentx, which goes to Settings", async ({
  cli,
  page,
}) => {
  cli.onServeStart({
    events: [
      {
        ...firstInventory(["claude-code", "codex", "cursor"]),
        library: [
          skill("notes", { fork_id: "f1" }),
          skill("review", {
            fork_id: "f2",
            source: "https://github.com/me/skills",
          }),
          skill("pdf", { source: "https://github.com/acme/skills" }),
        ],
      },
    ],
  });
  await page.goto("/");
  await expect(inbox(page)).toBeVisible();

  await runChecks(page);
  await expect(title(page)).toHaveText("You're all set");
  await expect(layer(page)).toContainText("Watching your agents for changes.");
  await expect(rows(page).getByText("Passed:", { exact: true })).toHaveCount(5);
  await expect(row(page, "agentx command line")).toContainText(
    `${cliVersion} at ${bundled}`
  );
  await expect(row(page, "git 2.40 or newer")).toContainText("git 2.43.0");
  await expect(row(page, "Account repo opens")).toContainText(
    "2 of your skills, 1 from sources"
  );
  await expect(row(page, "Watching this machine")).toContainText(
    "Found 3 agents"
  );
  // No command field while the layer shows.
  await expect(
    page.getByRole("button", { name: /Search skills/u })
  ).toHaveCount(0);
  // It stays until Open agentx.
  await page.waitForTimeout(800);
  await expect(layer(page)).toBeVisible();

  await page.getByRole("button", { name: "Open agentx" }).click();
  await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
  // The serve child already running answered check 5; none was started again.
  expect(cli.commands).toEqual([...launchLines, ...launchLines]);
  expect(cli.serveStarts).toBe(1);
});

test("a check that passes its deadline fails, and Retry runs it again", async ({
  cli,
  page,
}) => {
  const doctor = cli.register(["doctor"], { holdAfter: 0 });
  await page.goto("/");
  await doctor.started;
  doctor.expire();
  await expect(title(page)).toHaveText("agentx can't check this machine");
  await expect(row(page, "git 2.40 or newer")).toContainText(
    "agentx doctor did not finish in 30 seconds"
  );
  expect(cli.deadlines).toEqual([30_000, 30_000]);
  expect(cli.serveStarts).toBe(0);

  cli.register(["doctor"], { events: doctorWith() });
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(inbox(page)).toBeVisible();
});

test("a serve child that writes no inventory fails check 5 at the deadline", async ({
  cli,
  page,
}) => {
  cli.onServeStart({ events: [] });
  await page.clock.install();
  await page.goto("/");
  await expect.poll(() => cli.serveStarts).toBe(1);
  await expect
    .poll(async () => {
      await page.clock.runFor(10_000);
      return await layer(page).count();
    })
    .toBe(1);
  await expect(title(page)).toHaveText("agentx can't watch this machine");
  await expect(row(page, "Watching this machine")).toContainText(
    "agentx serve wrote no inventory in 30 seconds"
  );

  // The slow child keeps running; its late inventory answers Retry.
  await cli.serveEmit([firstInventory()]);
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(inbox(page)).toBeVisible();
  expect(cli.serveStarts).toBe(1);
});

test("the layer closes the palette, which stays closed after it", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(inbox(page)).toBeVisible();
  await page.keyboard.press("Control+K");
  await expect(page.getByRole("dialog")).toBeVisible();

  cli.register(["doctor"], { events: doctorWith(unusableRepo), exitCode: 8 });
  await cli.serveExit(8, [], null);
  await expect(title(page)).toHaveText("agentx can't open its account repo");
  cli.register(["doctor"], { events: doctorWith() });
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(inbox(page)).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
});

test("a serve exit with code 8 after launch brings the checks back", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(inbox(page)).toBeVisible();

  cli.register(["doctor"], { events: doctorWith(unusableRepo), exitCode: 8 });
  await cli.serveExit(8, [], null);
  await expect(title(page)).toHaveText("agentx can't open its account repo");
  await expect(inbox(page)).toHaveCount(0);
  expect(cli.commands).toEqual([...launchLines, ...launchLines]);
});

test("a one-shot exiting with code 8 brings the checks back", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(inbox(page)).toBeVisible();
  await page.addScriptTag({ type: "module", url: "/e2e/harness/transport.ts" });
  await page.waitForFunction(() => "transport" in window);

  cli.register(["doctor"], { events: [unusableRepo], exitCode: 8 });
  cli.register(["scan"], { exitCode: 8 });
  await page.evaluate(() => window.transport.run(["scan"]));
  await expect(title(page)).toHaveText("agentx can't open its account repo");
  expect(cli.commands).toEqual([
    ...launchLines,
    ["scan", "--json"],
    ...launchLines,
  ]);
});
