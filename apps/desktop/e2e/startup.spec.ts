import type { Page } from "@playwright/test";

import { filesOf } from "./fixtures/skills.ts";
import {
  cliVersion,
  doctorWith,
  expect,
  firstInventory,
  keptReads,
  loadLines,
  test,
} from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

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
const skills = (page: Page) => page.getByRole("heading", { name: "Skills" });

/**
 * The command lines, but the reads of the skill the Skills screen selects, which
 * e2e/skills.spec.ts covers. `skill import` is the store's adoption preview, a read of the launch.
 */
const launchCommands = (cli: MockCli) =>
  cli.commands.filter((l) => l[0] !== "skill" || l[1] === "import");

/**
 * Waits for the launch and the store's reads once the checks pass, so that the commands a test
 * triggers next come after them.
 */
const launched = async (cli: MockCli) => {
  await expect
    .poll(() => launchCommands(cli))
    .toEqual([...launchLines, ...loadLines]);
};

const oldGit = {
  check: "git",
  detail: "git 2.34.1 is older than 2.40",
  hint: "install git 2.40 or newer, then run agentx doctor again",
  status: "fail",
  type: "doctor",
} as const;

/** Opens manual mode, as Settings › Run checks will; until it exists, a harness module does. */
const runChecks = async (page: Page) => {
  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/run-checks.ts",
  });
};

const unusableRepo = {
  check: "store",
  detail: "/home/me/.agentx/store.git: not a git repository",
  hint: "move /home/me/.agentx/store.git aside, then run agentx doctor again",
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

test("a passing launch checks the machine and opens Skills", async ({
  cli,
  page,
}) => {
  const doctor = cli.register(["doctor"], {
    events: doctorWith(),
    holdAfter: 0,
  });
  await page.goto("/");
  await doctor.started;
  // While the checks run, the shell shows Skills selected and its panel is busy.
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "true");
  await expect(page.getByRole("link", { name: "Skills" })).toHaveAttribute(
    "aria-current",
    "page"
  );
  doctor.release();

  await expect(skills(page)).toBeVisible();
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "false");
  await launched(cli);
  expect(cli.serveStarts).toBe(1);
});

/** Stops the page's timers before it loads; the test moves the clock on. */
const pauseTimers = async (page: Page) => {
  await page.clock.install({ time: new Date("2026-10-23T10:00:00Z") });
  await page.clock.pauseAt(new Date("2026-10-23T10:00:01Z"));
};
/** The launch progress in the empty panel; a screen has status lines of its own. */
const progress = (page: Page) => page.getByRole("main").locator("> output");

test("a first launch says at once that it loads, and from 5 seconds how much longer it tries", async ({
  cli,
  page,
}) => {
  const doctor = cli.register(["doctor"], {
    events: doctorWith(),
    holdAfter: 0,
  });
  await pauseTimers(page);
  await page.goto("/");
  await doctor.started;
  await expect(progress(page)).toContainText("Starting the app…");
  await expect(progress(page)).not.toContainText("trying for");

  await page.clock.runFor(5000);
  await expect(progress(page)).toContainText(
    "Taking longer than usual · trying for 25s more"
  );
  await page.clock.runFor(1000);
  await expect(progress(page)).toContainText("trying for 24s more");

  doctor.release();
  await expect(skills(page)).toBeVisible();
  await expect(progress(page)).toHaveCount(0);
});

/** The screen in the main panel: out of reach while it shows the kept inventory. */
const screen = (page: Page) =>
  page.getByRole("main").locator("> div:not([data-launch-veil])");
/** The frosted pane, with the spinner, over the kept inventory while the checks run. */
const veil = (page: Page) =>
  page.getByRole("main").locator("> [data-launch-veil]");

/**
 * Launches with one hub skill, waits until its inventory and its files are kept for the next
 * launch, then restarts the app with `doctor` held. Returns the held doctor and the command lines of the
 * restart, the reads of the skill included.
 */
const relaunch = async (cli: MockCli, page: Page, more: string[] = []) => {
  cli.onServeStart({
    events: [
      {
        ...firstInventory(),
        library: ["notes", ...more].map((n) => skill(n, { fork_id: n })),
      },
    ],
  });
  for (const n of ["notes", ...more]) {
    cli.register(["skill", "files", n], { events: filesOf(n) });
    cli.register(["skill", "history", n], { events: [] });
  }
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "notes" })).toBeVisible();
  await launched(cli);
  await expect
    .poll(
      async () =>
        await page.evaluate(() => localStorage.getItem("agentx.launch"))
    )
    .not.toBeNull();
  // The files read is kept in IndexedDB apart from the inventory, and after it shows.
  await expect.poll(async () => await keptReads(page)).toContain("files:notes");

  const before = cli.commands.length;
  const held = cli.register(["doctor"], {
    events: doctorWith(),
    holdAfter: 0,
  });
  cli.restart();
  await page.reload();
  await held.started;
  return { commands: () => cli.commands.slice(before), doctor: held };
};

test("a later launch shows the kept inventory at once under a frosted pane, and runs its reads once the checks pass", async ({
  cli,
  page,
}) => {
  const { commands, doctor } = await relaunch(cli, page);
  // The skill is there while the checks run, and none of its reads ran.
  await expect(page.getByRole("heading", { name: "notes" })).toBeVisible();
  await expect(veil(page)).toBeVisible();
  await expect(veil(page).getByRole("status")).toContainText(
    "Starting the app…"
  );
  await expect(screen(page)).toHaveAttribute("inert", "");
  // The screen's own layers, such as the list's pinned group headers, stay under the pane.
  await expect(screen(page)).toHaveCSS("isolation", "isolate");
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "true");
  await expect(progress(page)).toHaveCount(0);
  expect(commands()).toEqual(launchLines);

  // The sidebar, the command field and the sync status are out of reach, and ⌘K opens nothing.
  await expect(page.locator("[inert] nav[aria-label='Main']")).toBeVisible();
  await expect(page.locator("header [inert]")).toHaveCount(2);
  await page.keyboard.press("Control+K");
  await expect(page.getByRole("dialog")).toHaveCount(0);

  doctor.release();
  await expect(veil(page)).toHaveCount(0);
  await expect(screen(page)).not.toHaveAttribute("inert");
  await expect(page.locator("[inert]")).toHaveCount(0);
  await expect(page.getByRole("main")).toHaveAttribute("aria-busy", "false");
  await expect
    .poll(() => commands())
    .toEqual(expect.arrayContaining(loadLines));
  // The skill's files show from the read the first launch kept.
  await expect(page.getByRole("region", { name: "Files" })).toContainText(
    "Use this skill to notes."
  );
  expect(commands()).not.toContainEqual(["skill", "files", "notes", "--json"]);
  expect(cli.serveStarts).toBe(2);
});

test("the arrows do not move the selection under the launch pane", async ({
  cli,
  page,
}) => {
  const { doctor } = await relaunch(cli, page, ["todo"]);
  const detail = (name: string) =>
    page.getByRole("heading", { exact: true, level: 1, name });
  await expect(detail("notes")).toBeVisible();
  await page.keyboard.press("ArrowDown");

  // Once the checks pass, notes is still selected, and the arrows work again.
  doctor.release();
  await expect(veil(page)).toHaveCount(0);
  await expect(detail("notes")).toBeVisible();
  await page.keyboard.press("ArrowDown");
  await expect(detail("todo")).toBeVisible();
});

test("a later launch whose check fails shows the layer, and Retry brings the screens back", async ({
  cli,
  page,
}) => {
  const { commands, doctor } = await relaunch(cli, page);
  doctor.expire();
  await expect(title(page)).toHaveText("agentx can't check this machine");

  cli.register(["doctor"], { events: doctorWith() });
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(page.getByRole("heading", { name: "notes" })).toBeVisible();
  await expect(screen(page)).toHaveCSS("opacity", "1");
  await expect
    .poll(() => commands())
    .toEqual(expect.arrayContaining(loadLines));
});

test("a machine with no skills and no agents passes", async ({ cli, page }) => {
  cli.onServeStart({ events: [firstInventory([])] });
  // Skills, the first screen, reads the skill it selects.
  cli.register(["skill", "files", "notes"], { events: filesOf("notes") });
  cli.register(["skill", "history", "notes"], { events: [] });
  await page.goto("/");
  await expect(skills(page)).toBeVisible();
  await launched(cli);

  await runChecks(page);
  await expect(title(page)).toHaveText("You're all set");
  await expect(row(page, "Store opens")).toContainText("No skills yet");
  await expect(row(page, "Watching this machine")).toContainText(
    "No agents found"
  );
});

test("a CLI that is not found stops at check 1", async ({ cli, page }) => {
  cli.resolveTo({ found: null, path: null });
  await page.goto("/");
  await expect(title(page)).toHaveText("Reinstall agentx");
  await expect(
    row(page, "agentx command line").getByText("Failed:", { exact: true })
  ).toBeVisible();
  await expect(
    row(page, "Watching this machine").getByText("Waiting:", { exact: true })
  ).toBeVisible();
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
  await expect(git.getByRole("paragraph")).toHaveText(`Fix: ${oldGit.hint}`);
  await expect(
    row(page, "Home folder is writable").getByText("Waiting:", { exact: true })
  ).toBeVisible();
  expect(cli.commands).toEqual(launchLines);
});

test("an unusable store blocks at check 4", async ({ cli, page }) => {
  cli.register(["doctor"], { events: doctorWith(unusableRepo), exitCode: 8 });
  await page.goto("/");
  await expect(title(page)).toHaveText("agentx can't open its store");
  const store = row(page, "Store opens");
  await expect(store.getByText("Failed:", { exact: true })).toBeVisible();
  await expect(store).toContainText("not a git repository");
  expect(cli.commands).toEqual(launchLines);
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
  await page.goto("/");
  await expect(skills(page)).toBeVisible();
  expect(cli.serveStarts).toBe(1);
  await launched(cli);

  await runChecks(page);
  await expect(title(page)).toHaveText("You're all set");
  const store = row(page, "Store opens");
  await expect(
    store.getByText("Passed with a warning:", { exact: true })
  ).toBeVisible();
  await expect(store).toContainText(lock.detail);
  await expect(store.getByRole("paragraph")).toHaveText(`Fix: ${lock.hint}`);
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
  // Serve keeps running and answering, and says why it does not watch.
  cli.onServeStart({ events: [refused] });
  await page.goto("/");
  await expect(title(page)).toHaveText("agentx can't watch this machine");
  const watching = row(page, "Watching this machine");
  await expect(watching.getByText("Failed:", { exact: true })).toBeVisible();
  await expect(watching).toContainText(refused.message);
  await expect(watching.getByRole("paragraph")).toHaveText(
    `Fix: ${refused.hint}`
  );
  expect(cli.commands).toEqual(launchLines);

  // Retry asks the same child to scan again, and its inventory passes the check.
  await page.getByRole("button", { name: "Retry" }).click();
  await expect.poll(() => cli.requestIds).toHaveLength(1);
  await cli.serveEmit([
    firstInventory(),
    { ok: true, request_id: cli.requestIds[0], type: "refresh_complete" },
  ]);
  await expect(skills(page)).toBeVisible();
  expect(cli.serveStarts).toBe(1);
});

test("a problem serve reports again on Retry shows at once", async ({
  cli,
  page,
}) => {
  const failed = {
    code: "git",
    message: "cannot scan /home/me/.agents/skills: git failed",
    type: "error",
  } as const;
  cli.onServeStart({ events: [failed] });
  await page.goto("/");
  await expect(row(page, "Watching this machine")).toContainText(
    failed.message
  );

  await page.getByRole("button", { name: "Retry" }).click();
  await expect.poll(() => cli.requestIds).toHaveLength(1);
  await cli.serveEmit([
    {
      error: "cannot scan /home/me/.agents/skills: git still failed",
      ok: false,
      request_id: cli.requestIds[0],
      type: "refresh_complete",
    },
  ]);
  await expect(row(page, "Watching this machine")).toContainText(
    "git still failed"
  );
  await expect(page.getByRole("button", { name: "Retry" })).toBeEnabled();
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
  await expect(skills(page)).toBeVisible();
  await expect(layer(page)).toHaveCount(0);
  await expect
    .poll(() => cli.commands)
    .toEqual([...launchLines, ...launchLines, ...loadLines]);
  expect(cli.serveStarts).toBe(1);
});

test("Enter retries", async ({ cli, page }) => {
  cli.register(["doctor"], { events: [oldGit], exitCode: 2 });
  await page.goto("/");
  await expect(title(page)).toHaveText("Update git to continue");
  cli.register(["doctor"], { events: doctorWith() });
  await page.keyboard.press("Enter");
  await expect(skills(page)).toBeVisible();
  await expect
    .poll(() => cli.commands)
    .toEqual([...launchLines, ...launchLines, ...loadLines]);
});

test("Copy report copies doctor's output in the CLI's layout", async ({
  cli,
  page,
}) => {
  cli.register(["doctor"], { events: [oldGit], exitCode: 2 });
  await page.goto("/");
  await expect(title(page)).toHaveText("Update git to continue");
  await page.getByRole("button", { name: "Copy report" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Copied" })
  ).toHaveCount(1);
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
  // Skills, the first screen, reads the skill it selects.
  cli.register(["skill", "files", "notes"], { events: filesOf("notes") });
  cli.register(["skill", "history", "notes"], { events: [] });
  await page.goto("/");
  await expect(skills(page)).toBeVisible();
  await launched(cli);

  await runChecks(page);
  await expect(title(page)).toHaveText("You're all set");
  await expect(rows(page).getByText("Passed:", { exact: true })).toHaveCount(5);
  await expect(row(page, "agentx command line")).toContainText(
    `${cliVersion} at ${bundled}`
  );
  await expect(row(page, "git 2.40 or newer")).toContainText("git 2.43.0");
  await expect(row(page, "Store opens")).toContainText(
    "2 hub skills, 1 from sources"
  );
  await expect(row(page, "Watching this machine")).toContainText(
    "Found 3 agents"
  );
  // It stays until Open agentx.
  await page.waitForTimeout(800);
  await expect(layer(page)).toBeVisible();

  await page.getByRole("button", { name: "Open agentx" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  // The serve child already running answered check 5; none was started again. Settings reads the
  // import preview again, and the store's size.
  await expect
    .poll(() => launchCommands(cli).filter((l) => l[0] !== "machine"))
    .toEqual([
      ...launchLines,
      ...loadLines,
      ...launchLines,
      ["skill", "import", "--json"],
    ]);
  expect(launchCommands(cli)).toContainEqual(["machine", "--json"]);
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

  cli.register(["doctor"], { events: doctorWith() });
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(skills(page)).toBeVisible();
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
  await expect(skills(page)).toBeVisible();
  expect(cli.serveStarts).toBe(1);
});

test("the layer closes the palette, which stays closed after it", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(skills(page)).toBeVisible();
  await page.keyboard.press("Control+K");
  await expect(page.getByRole("dialog")).toBeVisible();

  cli.register(["doctor"], { events: doctorWith(unusableRepo), exitCode: 8 });
  await cli.serveExit(8, [], null);
  await expect(title(page)).toHaveText("agentx can't open its store");
  cli.register(["doctor"], { events: doctorWith() });
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(skills(page)).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
});

test("a serve exit with code 8 after launch brings the checks back", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(skills(page)).toBeVisible();
  await launched(cli);

  cli.register(["doctor"], { events: doctorWith(unusableRepo), exitCode: 8 });
  await cli.serveExit(8, [], null);
  await expect(title(page)).toHaveText("agentx can't open its store");
  expect(cli.commands).toEqual([...launchLines, ...loadLines, ...launchLines]);
});

test("a store error serve reports after launch brings the checks back", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(skills(page)).toBeVisible();
  await launched(cli);

  cli.register(["doctor"], { events: doctorWith(unusableRepo), exitCode: 8 });
  await cli.serveEmit([
    {
      code: "store",
      message: "the store is not a git repository",
      type: "error",
    },
  ]);
  await expect(title(page)).toHaveText("agentx can't open its store");
  expect(cli.commands).toEqual([...launchLines, ...loadLines, ...launchLines]);
});

test("a run exiting with code 8 brings the checks back", async ({
  cli,
  page,
}) => {
  await page.goto("/");
  await expect(skills(page)).toBeVisible();
  await launched(cli);
  await page.addScriptTag({ type: "module", url: "/e2e/harness/transport.ts" });
  await page.waitForFunction(() => "transport" in window);

  cli.register(["doctor"], { events: [unusableRepo], exitCode: 8 });
  cli.register(["scan"], { exitCode: 8 });
  await page.evaluate(() => window.transport.run(["scan"]));
  await expect(title(page)).toHaveText("agentx can't open its store");
  expect(cli.commands).toEqual([
    ...launchLines,
    ...loadLines,
    ["scan", "--json"],
    ...launchLines,
  ]);
});
