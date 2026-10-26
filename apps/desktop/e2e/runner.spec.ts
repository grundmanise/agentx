import type { Page } from "@playwright/test";

import {
  doctorWith,
  expect,
  firstInventory,
  loadLines,
  machineLabel,
  test,
} from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const activity = (page: Page) =>
  page.getByRole("list", { name: "Activity" }).getByRole("listitem");

/** Launches the app, waits for the store's reads, and loads the store harness. */
const launch = async (cli: MockCli, page: Page) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
  await expect.poll(() => cli.commands).toEqual([...launchLines, ...loadLines]);
  await page.addScriptTag({ type: "module", url: "/e2e/harness/store.tsx" });
  await page.waitForFunction(() => "store" in window);
};

const run = async (page: Page, args: string[], success?: string) => {
  await page.evaluate(
    ([a, s]) => {
      window.store.run(
        a,
        s === undefined ? { label: "test" } : { label: "test", success: s }
      );
    },
    [args, success] as const
  );
};

/** The command lines after the launch and the store's reads. */
const later = (cli: MockCli) =>
  cli.commands.slice(launchLines.length + loadLines.length);

test("loads settings, sources and the adoption preview once the checks pass", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  await expect(page.getByRole("complementary")).toContainText(machineLabel);
  // Reads change nothing: no refresh follows them.
  expect(cli.serveRequests).toEqual([]);
});

test("a command that exits 0 shows its toast, logs it and asks serve to scan again", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  cli.register(["skill", "remove", "pdf"]);

  await run(page, ["skill", "remove", "pdf"], "Removed pdf");
  await expect(toasts(page).getByText("Removed pdf")).toBeVisible();
  await expect(activity(page)).toHaveText(["Removed pdf"]);
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  const [line = ""] = cli.serveRequests;
  expect(JSON.parse(line)).toEqual({
    request_id: expect.any(String),
    type: "refresh",
  });
  expect(later(cli)).toEqual([["skill", "remove", "pdf", "--json"]]);
});

test("a read asks for no refresh and logs nothing", async ({ cli, page }) => {
  await launch(cli, page);
  const list = cli.register(["skill", "list"], { exitCode: 0 });

  await run(page, ["skill", "list"]);
  await list.started;
  await expect.poll(() => later(cli)).toEqual([["skill", "list", "--json"]]);
  await expect
    .poll(async () => await page.evaluate(() => window.store.state().pending))
    .toEqual({});
  expect(cli.serveRequests).toEqual([]);
  await expect(activity(page)).toHaveCount(0);
});

test("exit 7 says another command runs, and Retry runs it again", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  cli.register(["skill", "update", "pdf"], {
    events: [
      {
        code: "locked",
        message: "another agentx command holds the lock",
        type: "error",
      },
    ],
    exitCode: 7,
  });

  await run(page, ["skill", "update", "pdf"], "Updated pdf");
  await expect(
    toasts(page).getByText("Another agentx command is running")
  ).toBeVisible();
  await expect(activity(page)).toHaveText([
    "Another agentx command is running",
  ]);
  await expect(activity(page)).toHaveAttribute("data-tone", "attention");

  cli.register(["skill", "update", "pdf"]);
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(toasts(page).getByText("Updated pdf")).toBeVisible();
  expect(later(cli)).toEqual([
    ["skill", "update", "pdf", "--json"],
    ["skill", "update", "pdf", "--json"],
  ]);
});

test("exit 8 brings the startup layer back", async ({ cli, page }) => {
  await launch(cli, page);
  const unusable = {
    check: "account_repo",
    detail: "/home/me/.agentx/account.git: not a git repository",
    hint: "move /home/me/.agentx/account.git aside, then run agentx doctor again",
    status: "fail",
    type: "doctor",
  } as const;
  cli.register(["doctor"], { events: doctorWith(unusable), exitCode: 8 });
  cli.register(["skill", "remove", "pdf"], {
    events: [
      {
        code: "account_repo",
        message: "account repo is unusable",
        type: "error",
      },
    ],
    exitCode: 8,
  });

  await run(page, ["skill", "remove", "pdf"], "Removed pdf");
  await expect(
    page
      .getByRole("region", { name: "Startup checks" })
      .getByRole("heading", { level: 1 })
  ).toHaveText("agentx can't open its account repo");
  expect(later(cli)).toEqual([
    ["skill", "remove", "pdf", "--json"],
    ...launchLines,
  ]);
});

test("exit 4 opens the skill with the pending merge", async ({ cli, page }) => {
  await launch(cli, page);
  cli.register(["skill", "update", "pdf"], {
    events: [
      { name: "pdf", type: "conflict" },
      {
        code: "pending_merge",
        message: "pdf has a pending merge",
        type: "error",
      },
    ],
    exitCode: 4,
  });

  await run(page, ["skill", "update", "pdf"], "Updated pdf");
  await expect(page).toHaveURL(/#\/skills\/pdf$/u);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
});

test("Cancel stops a command, which says Stopped", async ({ cli, page }) => {
  await launch(cli, page);
  const update = cli.register(["skill", "update", "--all"], { holdAfter: 0 });

  await run(page, ["skill", "update", "--all"], "Updated every skill");
  await update.started;
  await page.evaluate(async () => {
    await window.store.cancel("test");
  });
  await expect(
    toasts(page).getByText("Stopped", { exact: true })
  ).toBeVisible();
  expect(cli.cancels).toHaveLength(1);
  // A cancelled command may have changed something: serve scans again all the same.
  await expect.poll(() => cli.serveRequests.length).toBe(1);
});

test("another failure shows the CLI's message, hint and warnings until dismissed", async ({
  cli,
  page,
}) => {
  await page.clock.install();
  await launch(cli, page);
  const error = {
    code: "source",
    hint: "check that the repository exists and that you can read it",
    message: "github.com/acme/skills is unreachable",
    type: "error",
  } as const;
  const warning = "skipped plugin-skill: a plugin owns it";
  cli.register(["source", "add", "github.com/acme/skills"], {
    events: [error],
    exitCode: 3,
    logs: [{ level: "warn", message: warning }],
  });

  await run(
    page,
    ["source", "add", "github.com/acme/skills"],
    "Added acme/skills"
  );
  const toast = toasts(page)
    .getByRole("listitem")
    .filter({ hasText: error.message });
  await expect(toast).toContainText(error.hint);
  await expect(toast).toContainText(warning);
  await expect(activity(page)).toHaveText([error.message]);
  await expect(activity(page)).toHaveAttribute("data-tone", "danger");

  // A toast with a hint does not go on its own.
  await page.clock.fastForward(60_000);
  await expect(toast).toBeVisible();
  await toast.getByRole("button", { name: "Dismiss" }).click();
  await expect(toast).toHaveCount(0);
});

test("a serve restart before the refresh is answered sends it again to the new child", async ({
  cli,
  page,
}) => {
  await launch(cli, page);
  cli.register(["skill", "remove", "pdf"]);

  await run(page, ["skill", "remove", "pdf"], "Removed pdf");
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  await cli.serveExit(null);
  cli.serveRestart(2);
  await cli.serveEmit([firstInventory()]);

  await expect.poll(() => cli.serveRequests.length).toBe(2);
  const [first, second] = cli.requestIds;
  expect(second).not.toBe(first);
});

test("the app requests nothing outside the dev server", async ({
  cli,
  page,
}) => {
  const outside: string[] = [];
  page.on("request", (request) => {
    if (!request.url().startsWith("http://localhost")) {
      outside.push(request.url());
    }
  });
  await launch(cli, page);
  cli.register(["skill", "remove", "pdf"]);
  await run(page, ["skill", "remove", "pdf"], "Removed pdf");
  await expect(activity(page)).toHaveText(["Removed pdf"]);
  expect(outside).toEqual([]);
});
