import type { Page } from "@playwright/test";

import {
  ACCOUNT,
  ACME,
  ACME_ID,
  account,
  acme,
  acmeSkills,
  sharedOnly,
  sources,
  ZETA,
} from "./fixtures/settings-sources.ts";
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

/** The command lines that change something: every line but the listings the Sources section reads. */
const changes = (cli: MockCli) =>
  commandLines(cli).filter((l) => !l.startsWith("source skills "));

const section = (page: Page, name: string) =>
  page.getByRole("region", { exact: true, name });
const accountRemote = (page: Page) => section(page, "Account remote");
const sourcesSection = (page: Page) => section(page, "Sources");
const urlField = (page: Page) =>
  accountRemote(page).getByRole("textbox", { name: "Account remote URL" });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const sourceRow = (page: Page, url: string) =>
  sourcesSection(page)
    .getByRole("listitem")
    .filter({ has: page.getByText(url, { exact: true }) });

/** Opens Settings on a machine whose `source list` reports `listed`. */
const openSettings = async (cli: MockCli, page: Page, listed = sources) => {
  cli.register(["source", "list"], { events: listed });
  cli.register(["source", "skills", ACME_ID], { events: acmeSkills });
  await page.goto("/#/settings");
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  // The store's read at launch and the section's own when it opens have both been answered.
  await expect
    .poll(
      () =>
        cli.commands.filter((l) => l[0] === "source" && l[1] === "list").length
    )
    .toBe(2);
  await expect(
    sourcesSection(page).getByRole("button", { name: "Add source" })
  ).toBeVisible();
};

test("rows: the account remote and each source with what the Sources slice reports", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  const remote = accountRemote(page);
  await expect(remote).toContainText(
    "The Git repository your own skills publish to."
  );
  await expect(remote.getByText(ACCOUNT, { exact: true })).toBeVisible();
  await expect(
    remote.getByText("3 of your skills on it · fetched 2 hours ago")
  ).toBeVisible();
  await expect(remote.getByText("You can write to it")).toBeVisible();
  await expect(remote.getByRole("button", { name: "Change…" })).toBeVisible();
  await expect(remote.getByRole("button", { name: "Detach" })).toBeVisible();
  await expect(urlField(page)).toHaveCount(0);

  // The shared sources, in the order `source list` gives them; the account remote is not one.
  await expect(sourcesSection(page).getByRole("listitem")).toHaveCount(2);
  const acmeRow = sourceRow(page, ACME);
  await expect(acmeRow.getByText("#v2", { exact: true })).toBeVisible();
  await expect(
    acmeRow.getByText("Fetched 3 days ago · 4 skills")
  ).toBeVisible();
  await expect(acmeRow.getByText("Read-only for you")).toBeVisible();
  const zetaRow = sourceRow(page, ZETA);
  await expect(zetaRow.getByText("Not fetched yet")).toBeVisible();
  await expect(zetaRow.getByText("Write access unknown")).toBeVisible();
  await expect(zetaRow.getByText(/^#/u)).toHaveCount(0);
  // The section reads the sources again, and the listing of the one source that was fetched.
  expect(commandLines(cli)).toEqual([`source skills ${ACME_ID}`]);
});

test("set the account remote: Save runs source add --account, and the row comes from the next source list", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page, sharedOnly);
  await expect(urlField(page)).toBeVisible();
  await expect(
    accountRemote(page).getByRole("button", { name: "Save" })
  ).toBeDisabled();
  cli.register(
    ["source", "add", "git@github.com:me/account.git", "--account"],
    {
      events: [
        { current: 1, phase: "fetch", subject: ACCOUNT, type: "progress" },
        { ...account, last_fetched: new Date().toISOString() },
      ],
    }
  );
  // The read after the add waits, so the screen can be seen before it.
  const relisted = cli.register(["source", "list"], {
    events: [...sharedOnly, account],
    holdAfter: 0,
  });
  await urlField(page).fill("git@github.com:me/account.git");
  await urlField(page).press("Enter");
  await expect(
    toasts(page).getByText(`Saved. Reached ${ACCOUNT}`)
  ).toBeVisible();
  // Nothing is predicted: the field stays until `source list` reports the account remote.
  await relisted.started;
  await expect(urlField(page)).toBeVisible();
  await expect(
    accountRemote(page).getByText("You can write to it")
  ).toHaveCount(0);
  relisted.release();
  await expect(
    accountRemote(page).getByText(ACCOUNT, { exact: true })
  ).toBeVisible();
  await expect(
    accountRemote(page).getByText("You can write to it")
  ).toBeVisible();
  await expect(urlField(page)).toHaveCount(0);
  expect(changes(cli)).toEqual([
    "source add git@github.com:me/account.git --account",
  ]);
});

test("set the account remote: exit 3, 1 and 6 show the CLI's message and hint under the field, as one paragraph", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page, sharedOnly);
  cli.register(["source", "add", "me/missing", "--account"], {
    events: [
      {
        code: "source",
        hint: "check the URL, and that your credential helper can read it",
        message:
          "cannot reach the account remote https://github.com/me/missing: repository not found",
        type: "error",
      },
    ],
    exitCode: 3,
  });
  cli.register(["source", "add", "me/account#main", "--account"], {
    events: [
      {
        code: "usage",
        hint: "give the repository's URL alone",
        message:
          "me/account#main names a folder or a ref, and the account remote is a whole repository",
        type: "error",
      },
    ],
    exitCode: 1,
  });
  cli.register(["source", "add", ACME, "--account"], {
    events: [
      {
        code: "refused",
        hint: `remove the source first with 'agentx source remove ${ACME}', or choose another repository`,
        message: `${ACME} is a shared source, and a repository cannot be both a shared source and the account remote`,
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
  // The runner's error toast stays away: the section says what happened.
  await expect(toasts(page).getByRole("listitem")).toHaveCount(0);

  await urlField(page).fill("me/account#main");
  await save.click();
  await expect(alert).toHaveText(
    "me/account#main names a folder or a ref, and the account remote is a whole repository. Give the repository's URL alone."
  );

  await urlField(page).fill(ACME);
  await save.click();
  // The hint names the control in Settings, not the command the CLI names.
  await expect(alert).toHaveText(
    `${ACME} is a shared source, and a repository cannot be both a shared source and the account remote. Remove it under Sources first, or choose another repository.`
  );
  await expect(toasts(page).getByRole("listitem")).toHaveCount(0);
  expect(changes(cli)).toEqual([
    "source add me/missing --account",
    "source add me/account#main --account",
    `source add ${ACME} --account`,
  ]);
});

test("Change… warns before it replaces the account remote", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  const next = "ssh://git@github.com/me/skills";
  await accountRemote(page).getByRole("button", { name: "Change…" }).click();
  await expect(urlField(page)).toHaveValue(ACCOUNT);
  const save = accountRemote(page).getByRole("button", { name: "Save" });
  await expect(save).toBeDisabled();
  await urlField(page).fill(next);
  await save.click();
  const confirm = page.getByRole("dialog", {
    name: "Replace the account remote?",
  });
  await expect(confirm).toContainText(
    "Your skills stay on this machine and show as not published until you publish each one to the new account remote."
  );
  await confirm.getByRole("button", { name: "Cancel" }).click();
  await expect(confirm).toHaveCount(0);
  expect(changes(cli)).toEqual([]);

  cli.register(["source", "add", next, "--account"], {
    events: [{ ...account, url: next }],
  });
  await save.click();
  await confirm.getByRole("button", { name: "Replace" }).click();
  await expect(toasts(page).getByText(`Saved. Reached ${next}`)).toBeVisible();
  await expect(urlField(page)).toHaveCount(0);
  expect(changes(cli)).toEqual([`source add ${next} --account`]);
});

test("Detach warns, runs source remove and shows the field once source list has no account remote", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  await accountRemote(page).getByRole("button", { name: "Detach" }).click();
  const confirm = page.getByRole("dialog", { name: `Detach ${ACCOUNT}?` });
  await expect(confirm).toContainText(
    "Your skills stay on this machine, and nothing on the account remote changes."
  );
  expect(changes(cli)).toEqual([]);
  cli.register(["source", "remove", ACCOUNT], {
    events: [{ id: account.id, type: "source", url: ACCOUNT }],
  });
  cli.register(["source", "list"], { events: sharedOnly });
  await confirm.getByRole("button", { name: "Detach" }).click();
  await expect(
    toasts(page).getByText("The account remote is no longer set")
  ).toBeVisible();
  await expect(urlField(page)).toBeVisible();
  await expect(urlField(page)).toHaveValue("");
  expect(changes(cli)).toEqual([`source remove ${ACCOUNT}`]);
});

test("Remove runs source remove, and Undo adds the source again with its pin", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  await expect(
    sourceRow(page, ACME).getByText("Fetched 3 days ago · 4 skills")
  ).toBeVisible();
  cli.register(["source", "remove", ACME], {
    events: [{ id: ACME_ID, type: "source", url: ACME }],
  });
  const relisted = cli.register(["source", "list"], {
    events: sources.filter((s) => s.url !== ACME),
    holdAfter: 0,
  });
  await sourceRow(page, ACME).getByRole("button", { name: "Remove" }).click();
  const toast = toasts(page).getByRole("listitem");
  await expect(toast).toContainText(
    "Removed acme/skills. Skills from it stay as they are."
  );
  // The row goes when `source list` no longer lists the source, not before.
  await relisted.started;
  await expect(sourceRow(page, ACME)).toHaveCount(1);
  relisted.release();
  await expect(sourceRow(page, ACME)).toHaveCount(0);

  cli.register(["source", "add", `${ACME}#v2`], {
    events: [{ ...acme, skills: 4 }],
  });
  cli.register(["source", "list"], { events: sources });
  await toast.getByRole("button", { name: "Undo" }).click();
  await expect(toasts(page).getByText(`Added ${ACME}: 4 skills`)).toBeVisible();
  await expect(sourceRow(page, ACME).getByText("#v2")).toBeVisible();
  expect(changes(cli)).toEqual([
    `source remove ${ACME}`,
    `source add ${ACME}#v2`,
  ]);
});

test("Undo of an unpinned source adds its URL alone", async ({ cli, page }) => {
  await openSettings(cli, page);
  cli.register(["source", "remove", ZETA], {
    events: [{ id: "7a3b5c9e1f2d4a60", type: "source", url: ZETA }],
  });
  cli.register(["source", "add", ZETA], {
    events: [{ id: "7a3b5c9e1f2d4a60", skills: 0, type: "source", url: ZETA }],
  });
  await sourceRow(page, ZETA).getByRole("button", { name: "Remove" }).click();
  const toast = toasts(page).getByRole("listitem");
  await expect(toast).toContainText(
    "Removed zeta/tools. Skills from it stay as they are."
  );
  await toast.getByRole("button", { name: "Undo" }).click();
  await expect(toasts(page).getByText(`Added ${ZETA}: 0 skills`)).toBeVisible();
  expect(changes(cli)).toEqual([`source remove ${ZETA}`, `source add ${ZETA}`]);
});

test("Add source opens the Add source dialog of the Add skills screen", async ({
  cli,
  page,
}) => {
  await openSettings(cli, page);
  await sourcesSection(page)
    .getByRole("button", { name: "Add source" })
    .click();
  const dialog = page.getByRole("dialog", { name: "Add source" });
  await expect(dialog).toContainText("Add #branch or #tag to pin it");
  const newUrl = "https://github.com/acme/more";
  cli.register(["source", "add", "acme/more"], {
    events: [
      { id: "9f8e7d6c5b4a3210", skills: 2, type: "source", url: newUrl },
    ],
  });
  await dialog
    .getByRole("textbox", { name: "Git URL or owner/name" })
    .fill("acme/more");
  await dialog.getByRole("button", { name: "Add" }).click();
  await expect(dialog).toHaveCount(0);
  await expect(
    toasts(page).getByText(`Added ${newUrl}: 2 skills`)
  ).toBeVisible();
  expect(changes(cli)).toEqual(["source add acme/more"]);
});
