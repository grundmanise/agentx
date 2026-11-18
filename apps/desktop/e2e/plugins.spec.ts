import type { Page } from "@playwright/test";

import {
  ACME_ICON,
  emptyInventory,
  inventory,
  moreInventory,
  TEAM_DB,
} from "./fixtures/plugins.ts";
import { filesOf } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli, InlineEvent } from "./harness/cli.ts";

const list = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Plugins list" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const detail = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Plugin details" });

/** The names of the rows in the list, top to bottom. */
const rowNames = async (page: Page) =>
  await list(page)
    .getByRole("listitem")
    .getByRole("button")
    .evaluateAll((buttons) =>
      buttons.map((b) => b.getAttribute("aria-label") ?? "")
    );

/** Opens the Plugins screen on `machine` and waits for its list. */
const openPlugins = async (
  cli: MockCli,
  page: Page,
  machine: InlineEvent = inventory
) => {
  cli.onServeStart({ events: [machine] });
  await page.goto("/#/plugins");
  await expect(
    page.getByRole("heading", { level: 1, name: "Plugins" })
  ).toBeVisible();
  await expect(row(page, "acme-tools").first()).toBeVisible();
};

test("the list groups the plugins by agent, and the first one opens with its details and tabs", async ({
  cli,
  page,
}) => {
  await openPlugins(cli, page);
  // One group per agent, A to Z, each under its logo, name and count.
  await expect(list(page)).toMatchAriaSnapshot(`
    - region "Claude Code":
      - heading "Claude Code 1" [level=2]
      - list:
        - listitem:
          - button "acme-tools"
    - region "Codex":
      - heading "Codex 1" [level=2]
      - list:
        - listitem:
          - button "codex-review"
    - region "Cursor":
      - heading "Cursor 1" [level=2]
      - list:
        - listitem:
          - button "notes-local"
  `);
  await expect(row(page, "acme-tools")).toHaveAccessibleDescription(
    "1.4.0. 2 skills · 1 server · 2 hooks"
  );
  await expect(row(page, "codex-review")).toHaveAccessibleDescription(
    "Disabled. 0.3.1. 1 skill"
  );
  await expect(row(page, "acme-tools")).toHaveAttribute("aria-current", "true");

  // The open plugin: its icon, name and version, the details under them, then a tab for each of
  // its skills, servers and hooks, as tables.
  const pane = detail(page);
  await expect(pane.getByRole("img", { name: "acme-tools" })).toHaveAttribute(
    "src",
    ACME_ICON
  );
  await expect(pane).toMatchAriaSnapshot(`
    - article "acme-tools":
      - img "acme-tools"
      - heading "acme-tools" [level=1]
      - text: 1.4.0
      - term: Agent
      - definition: Claude Code
      - term: Marketplace
      - definition: acme-marketplace
      - term: Path
      - definition:
        - link "/home/me/.claude/plugins/acme-tools"
      - tablist "Contents":
        - tab "Skills 2" [selected]
        - tab "MCP servers 1"
        - tab "Hooks 2"
      - tabpanel "Skills 2":
        - table "Skills":
          - rowgroup:
            - row "Skill Description"
          - rowgroup:
            - row "commit-helper Write commit messages the way the team does"
            - row "release-check Check a release before it ships"
  `);
  await pane.getByRole("tab", { name: "Hooks 2" }).click();
  await expect(pane.getByRole("table", { name: "Hooks" })).toMatchAriaSnapshot(`
    - table "Hooks":
      - rowgroup:
        - row "Hook Event"
      - rowgroup:
        - row "format-on-save PostToolUse"
        - row "load-context.sh SessionStart"
  `);

  // A plugin with no contents has no tabs; a local one says so.
  await row(page, "notes-local").click();
  await expect(pane).toMatchAriaSnapshot(`
    - article "notes-local":
      - heading "notes-local" [level=1]
      - text: no version
      - term: Agent
      - definition: Cursor
      - term: Marketplace
      - definition: local
      - term: Path
      - definition:
        - link "/home/me/.cursor/plugins/local/notes-local"
  `);
  await expect(pane.getByRole("tablist")).toHaveCount(0);
});

test("Search, the agent filter and the sort toggle", async ({ cli, page }) => {
  await openPlugins(cli, page, moreInventory);
  // A to Z by name at first: the plugin turned off sorts with the rest.
  const az = [
    "aaa-off",
    "acme-tools",
    "codex-review",
    "acme-tools",
    "notes-local",
  ];
  expect(await rowNames(page)).toEqual(az);
  const sort = list(page).getByRole("button", { name: /^Sort/u });
  await expect(sort).toHaveText("A-Z");
  await sort.click();
  await expect(sort).toHaveText("Z-A");
  expect(await rowNames(page)).toEqual([
    "acme-tools",
    "aaa-off",
    "codex-review",
    "notes-local",
    "acme-tools",
  ]);
  await sort.click();
  expect(await rowNames(page)).toEqual(az);

  await list(page)
    .getByRole("searchbox", { name: "Filter plugins by name" })
    .fill("ACME");
  expect(await rowNames(page)).toEqual(["acme-tools", "acme-tools"]);
  await list(page).getByRole("searchbox").fill("");

  await list(page).getByRole("button", { exact: true, name: "Filter" }).click();
  await page.getByRole("checkbox", { name: "Codex" }).check();
  await page.keyboard.press("Escape");
  expect(await rowNames(page)).toEqual(["codex-review"]);
  await expect(detail(page).getByRole("heading", { level: 1 })).toHaveText(
    "codex-review"
  );
  await list(page).getByRole("button", { name: "Clear agents" }).click();
  expect(await rowNames(page)).toHaveLength(5);
});

test("the same plugin in another agent opens from Also installed in", async ({
  cli,
  page,
}) => {
  await openPlugins(cli, page, moreInventory);
  await list(page)
    .getByRole("region", { name: "Claude Code" })
    .getByRole("button", { name: "acme-tools" })
    .click();
  await detail(page).getByRole("button", { name: "Cursor 1.3.0" }).click();
  await expect(
    list(page)
      .getByRole("region", { name: "Cursor" })
      .getByRole("button", { name: "acme-tools" })
  ).toHaveAttribute("aria-current", "true");
  await expect(detail(page).getByText("1.3.0", { exact: true })).toBeVisible();
  await expect(
    detail(page).getByRole("button", { name: "Claude Code 1.4.0" })
  ).toBeVisible();
});

test("a machine with no plugin shows the empty state", async ({
  cli,
  page,
}) => {
  cli.onServeStart({ events: [emptyInventory] });
  await page.goto("/#/plugins");
  await expect(
    page.getByText("No plugins found in your agents.")
  ).toBeVisible();
});

test.describe("navigation", () => {
  test("a skill opens in Skills, selected", async ({ cli, page }) => {
    await openPlugins(cli, page);
    // The Skills screen reads the files of the skill it shows, a plugin's by "<plugin>:<name>".
    cli.register(["skill", "files", "acme-tools:release-check"], {
      events: filesOf("release-check"),
    });
    await detail(page)
      .getByRole("table", { name: "Skills" })
      .getByRole("button", { name: "release-check" })
      .click();
    await expect(page).toHaveURL(/#\/skills\/skill%3Ah-release$/u);
    await expect(
      page
        .getByRole("region", { name: "Skill details" })
        .getByRole("heading", { level: 1, name: "release-check" })
    ).toBeVisible();
  });

  test("a server opens MCP servers with the server's row in view", async ({
    cli,
    page,
  }) => {
    await openPlugins(cli, page);
    await detail(page).getByRole("tab", { name: "MCP servers 1" }).click();
    await detail(page)
      .getByRole("table", { name: "MCP servers" })
      .getByRole("button", { name: TEAM_DB })
      .click();
    await expect(page).toHaveURL(/#\/servers\/server%3Ateam-db$/u);
    const table = page.getByRole("table", { name: "MCP servers" });
    await expect(
      table.getByRole("row", { exact: true, name: TEAM_DB })
    ).toBeInViewport();
  });
});
