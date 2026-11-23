import type { Page } from "@playwright/test";

import {
  ACCOUNT,
  ACME,
  account,
  sharedOnly,
  sources,
} from "./fixtures/account-remote.ts";
import type { MockCli } from "./harness/cli.ts";
import { expect, loadLines, test } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/** Whether a command line is one the launch or the store's reads run, whichever order they come in. */
const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the page itself ran, as text, without the --json the supervisor adds. */
const commandLines = (cli: MockCli) =>
  cli.commands
    .filter((l) => !isLaunchOrLoad(l))
    .map((l) => l.slice(0, -1).join(" "));

const section = (page: Page, name: string) =>
  page.getByRole("region", { exact: true, name });
const accountRemote = (page: Page) => section(page, "Hub");
const urlField = (page: Page) =>
  accountRemote(page).getByRole("textbox", { name: "Hub URL" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });

/** Opens Settings on a machine whose `source list` reports `listed`. */
const openSettings = async (cli: MockCli, page: Page, listed = sources) => {
  cli.register(["source", "list"], { events: listed });
  await page.goto("/#/settings");
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  // The store's read at launch has been answered.
  await expect
    .poll(
      () =>
        cli.commands.filter((l) => l[0] === "source" && l[1] === "list").length
    )
    .toBe(1);
};

test("the hub with what the Sources slice reports", async ({ cli, page }) => {
  await openSettings(cli, page);
  const remote = accountRemote(page);
  await expect(remote).toContainText(
    "The Git repository your hub skills publish to."
  );
  await expect(remote.getByText(ACCOUNT, { exact: true })).toBeVisible();
  // No line under the URL: the count was skills published there, not skills installed.
  await expect(remote.getByText(/hub skills? on it|fetched/u)).toHaveCount(0);
  // Write access is the norm, so it shows no tag.
  await expect(remote.getByText("Read-only", { exact: true })).toHaveCount(0);
  await expect(remote.getByRole("button", { name: "Change…" })).toBeVisible();
  await expect(remote.getByRole("button", { name: "Unlink" })).toBeVisible();
});

test("a read-only hub has a Read-only tag beside its URL", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page, [
    ...sharedOnly,
    { ...account, access: "read-only" },
  ]);
  await expect(
    accountRemote(page).getByText("Read-only", { exact: true })
  ).toBeVisible();
});

test("set the hub: Save runs source add --hub, and the row comes from the next source list", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page, sharedOnly);
  await expect(urlField(page)).toBeVisible();
  await expect(
    accountRemote(page).getByRole("button", { name: "Save" })
  ).toBeDisabled();
  cli.register(["source", "add", "git@github.com:me/account.git", "--hub"], {
    events: [
      { current: 1, phase: "fetch", subject: ACCOUNT, type: "progress" },
      { ...account, last_fetched: new Date().toISOString() },
    ],
  });
  // The read after the add waits, so the screen can be seen before it.
  const relisted = cli.register(["source", "list"], {
    events: [...sharedOnly, account],
    holdAfter: 0,
  });
  await urlField(page).fill("git@github.com:me/account.git");
  await urlField(page).press("Enter");
  await expect(toasts(page).getByText("Linked hub")).toBeVisible();
  // The field stays until `source list` reports the hub.
  await relisted.started;
  await expect(urlField(page)).toBeVisible();
  relisted.release();
  await expect(
    accountRemote(page).getByText(ACCOUNT, { exact: true })
  ).toBeVisible();
  expect(commandLines(cli)).toEqual([
    "source add git@github.com:me/account.git --hub",
  ]);
});

test("set the hub: exit 3, 1 and 6 show the CLI's message and hint under the field, as one paragraph", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page, sharedOnly);
  cli.register(["source", "add", "me/missing", "--hub"], {
    events: [
      {
        code: "source",
        hint: "check the URL, and that your credential helper can read it",
        message:
          "cannot reach the hub https://github.com/me/missing: repository not found",
        type: "error",
      },
    ],
    exitCode: 3,
  });
  cli.register(["source", "add", "me/account#main", "--hub"], {
    events: [
      {
        code: "usage",
        hint: "give the repository's URL alone",
        message:
          "me/account#main names a folder or a ref, and the hub is a whole repository",
        type: "error",
      },
    ],
    exitCode: 1,
  });
  cli.register(["source", "add", ACME, "--hub"], {
    events: [
      {
        code: "refused",
        hint: `remove the source first with 'agentx source remove ${ACME}', or choose another repository`,
        message: `${ACME} is a shared source, and a repository cannot be both a shared source and the hub`,
        type: "error",
      },
    ],
    exitCode: 6,
  });
  const save = accountRemote(page).getByRole("button", { name: "Save" });
  const alert = accountRemote(page).getByRole("alert");

  await urlField(page).fill("me/missing");
  await save.click();
  // The message and the hint as one sentence-cased paragraph.
  await expect(alert).toHaveText(
    "Couldn't reach me/missing. Check the URL, and that your credential helper can read it."
  );

  await urlField(page).fill("me/account#main");
  await save.click();
  await expect(alert).toHaveText(
    "me/account#main names a folder or a ref, and the hub is a whole repository. Give the repository's URL alone."
  );

  await urlField(page).fill(ACME);
  await save.click();
  // The hint names the control in Settings, not the command the CLI names.
  await expect(alert).toHaveText(
    `${ACME} is a shared source, and a repository cannot be both a shared source and the hub. Remove it under Sources first, or choose another repository.`
  );
  expect(commandLines(cli)).toEqual([
    "source add me/missing --hub",
    "source add me/account#main --hub",
    `source add ${ACME} --hub`,
  ]);
});

test("Change… warns before it replaces the hub", async ({ cli, page }) => {
  await openSettings(cli, page);
  const next = "ssh://git@github.com/me/skills";
  await accountRemote(page).getByRole("button", { name: "Change…" }).click();
  await expect(urlField(page)).toHaveValue(ACCOUNT);
  const save = accountRemote(page).getByRole("button", { name: "Save" });
  await expect(save).toBeDisabled();
  await urlField(page).fill(next);
  await save.click();
  const confirm = page.getByRole("alertdialog", {
    name: "Change your hub?",
  });
  await expect(confirm).toContainText(
    "Skills from the previous hub will stay but show as not published."
  );
  await confirm.getByRole("button", { name: "Cancel" }).click();
  await expect(confirm).toHaveCount(0);

  cli.register(["source", "add", next, "--hub"], {
    events: [{ ...account, url: next }],
  });
  await save.click();
  await confirm.getByRole("button", { name: "Change" }).click();
  await expect(toasts(page).getByText("Linked hub")).toBeVisible();
  await expect(urlField(page)).toHaveCount(0);
  expect(commandLines(cli)).toEqual([`source add ${next} --hub`]);
});

test("Unlink warns, runs source remove and shows the field once source list has no hub", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  await accountRemote(page).getByRole("button", { name: "Unlink" }).click();
  const confirm = page.getByRole("alertdialog", {
    name: "Unlink the hub?",
  });
  await expect(confirm).toContainText(
    `This will disconnect ${ACCOUNT} from the app, but keep all current skills in place.`
  );
  cli.register(["source", "remove", ACCOUNT], {
    events: [{ id: account.id, type: "source", url: ACCOUNT }],
  });
  cli.register(["source", "list"], { events: sharedOnly });
  await confirm.getByRole("button", { name: "Unlink" }).click();
  await expect(
    toasts(page).getByText("The hub is no longer set")
  ).toBeVisible();
  await expect(urlField(page)).toBeVisible();
  await expect(urlField(page)).toHaveValue("");
  expect(commandLines(cli)).toEqual([`source remove ${ACCOUNT}`]);
});
