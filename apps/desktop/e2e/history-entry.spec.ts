// The History sheet of one of your own skills: each entry expands to the files its commit changed
// and collapses again, and the commit that created a fork leads to the skill it came from.
import type { Page } from "@playwright/test";

import type { ForkOrigin, InventoryEvent } from "#/cli/events.gen.ts";
import {
  filtersShowing,
  noFilters,
  originRow,
  projectRows,
} from "#/screens/skills/projection.ts";

import {
  ACME,
  adoption,
  fileReads,
  history,
  inventory,
} from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const SUBJECT = "pdf-letters: edit SKILL.md and add examples.md (studio-mac)";

const sheet = (page: Page) => page.getByRole("dialog", { name: "History" });
const entryButton = (page: Page) =>
  sheet(page).getByRole("button", { exact: true, name: SUBJECT });
const filesOf = (page: Page) =>
  sheet(page).getByRole("list", { name: `Files of ${SUBJECT}` });
const details = (page: Page) =>
  page.getByRole("region", { name: "Skill details" });
const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });

/** Starts the app on the fixture machine, with pdf-letters' history and every skill's files. */
const start = (cli: MockCli) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
};

/** Opens pdf-letters, one of your own skills, and its History sheet. */
const openSheet = async (cli: MockCli, page: Page) => {
  start(cli);
  await page.goto("/#/skills/pdf-letters");
  await details(page).getByRole("button", { name: "Show all 5" }).click();
  await expect(sheet(page)).toBeVisible();
};

test("an entry expands to its files and collapses again", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const button = entryButton(page);
  await expect(button).toHaveAttribute("aria-expanded", "false");

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect(filesOf(page).getByRole("listitem")).toHaveText([
    "modifiedSKILL.md",
    "addedexamples.md",
  ]);

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(filesOf(page)).toHaveCount(0);
});

test("a fork's first commit selects the skill it came from, showing every skill when the filter hides it", async ({
  cli,
  page,
}) => {
  start(cli);
  await page.goto("/#/skills/pdf-letters");
  await page.getByRole("button", { name: "Filter" }).click();
  await page
    .getByRole("dialog", { name: "Filters" })
    .getByRole("group", { name: "Show" })
    .getByRole("radio", { name: "Personal 3" })
    .check();
  await page.keyboard.press("Escape");
  await details(page).getByRole("button", { name: "Show all 5" }).click();

  const origin = sheet(page).getByRole("button", {
    name: "Show pdf in Skills",
  });
  await expect(origin).toHaveText("pdf");
  await origin.click();

  await expect(page).toHaveURL(/#\/skills\/pdf$/u);
  await expect(sheet(page)).toBeHidden();
  await expect(
    details(page).getByRole("article", { name: "pdf" })
  ).toBeVisible();
  await expect(
    page.getByRole("button", { exact: true, name: "Filter" })
  ).toBeVisible();
  await expect(
    list(page).getByRole("button", { exact: true, name: "pdf" })
  ).toHaveAttribute("aria-current", "true");
});

test("a fork whose original is not on this machine opens its page", async ({
  cli,
  page,
}) => {
  const url = "https://github.com/ChromeDevTools/chrome-devtools-mcp";
  const subject = "Fork draft from plugin chrome-devtools-mcp";
  start(cli);
  cli.register(["skill", "history", "draft"], {
    events: [
      {
        ...history[3],
        message: subject,
        name: "draft",
        origin: {
          kind: "plugin",
          marketplace: "claude-plugins-official",
          plugin: "chrome-devtools-mcp",
          skill: "a11y-debugging",
          url,
        },
        subject,
      },
    ],
  });
  await page.goto("/#/skills/draft");

  const origin = details(page)
    .getByRole("list", { name: "History" })
    .getByRole("button", { name: `Open ${url}` });
  await expect(origin).toHaveText("chrome-devtools-mcp");
  await origin.click();
  await expect.poll(() => cli.openedUrls).toEqual([url]);
});

test("the skill a fork came from, by the origin its first commit names", () => {
  // The fixture inventory with the fields serve adds to it.
  const whole: InventoryEvent = {
    edges: [],
    instance_id: "instance-1",
    machine: { id: "machine-1", label: "studio-mac" },
    mcp_servers: [],
    schema_version: 2,
    warnings: [],
    ...inventory,
  };
  const rows = projectRows({ adoption, inventory: whole, updates: {} });
  const found = (origin: ForkOrigin) => originRow(origin, rows)?.key;
  const plugin: ForkOrigin = {
    kind: "plugin",
    plugin: "acme-tools",
    skill: "commit-helper",
  };

  // A plugin's skill, by its plugin and directory, in the marketplace when the origin names one.
  expect(found(plugin)).toBe("skill:h-commit");
  expect(found({ ...plugin, marketplace: "acme-marketplace" })).toBe(
    "skill:h-commit"
  );
  expect(found({ ...plugin, marketplace: "other" })).toBeUndefined();
  expect(found({ ...plugin, skill: "debugging" })).toBeUndefined();
  expect(found({ ...plugin, plugin: "other-tools" })).toBeUndefined();
  // A skill of a source, by the source and its path in it.
  const source: ForkOrigin = {
    kind: "source",
    path: "pdf",
    skill: "pdf",
    source: ACME,
  };
  expect(found(source)).toBe("pdf");
  expect(found({ ...source, path: "docs" })).toBeUndefined();
  expect(found({ ...source, source: "https://github.com/acme/other" })).toBe(
    undefined
  );
  // A library skill or another fork, by its name; a plugin's skill is not one.
  expect(found({ kind: "skill", skill: "release-notes" })).toBe(
    "release-notes"
  );
  expect(found({ kind: "skill", skill: "scratch" })).toBe("scratch");
  expect(found({ kind: "skill", skill: "commit-helper" })).toBeUndefined();

  // The filters change only as far as the row needs to be listed.
  const pdf = rows.find((r) => r.name === "pdf") ?? rows[0];
  const yours = { ...noFilters, show: "yours" as const };
  expect(filtersShowing(pdf, noFilters)).toBe(noFilters);
  expect(filtersShowing(pdf, { ...yours, state: "enabled" })).toEqual({
    ...noFilters,
    state: "enabled",
  });
  expect(filtersShowing(pdf, { ...yours, name: "letters" })).toEqual(noFilters);
});
