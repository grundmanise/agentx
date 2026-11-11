// The Agents page: every configuration the scan detected, with its counts from the inventory's
// edges, the Enabled switch, which runs `config enable|disable <id>` and offers Place all, and
// removing leftover configurations: from a card's menu and from Clean up, by `config remove`.
import type { Page } from "@playwright/test";

import { inventory, LIBRARY, removals, sources } from "./fixtures/agents.ts";
import { adoption } from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

// The page's own read: which configurations can be removed.
const preview = ["config", "remove", "--json"];

const launchLines = [["version", "--json"], ["doctor", "--json"], preview];

const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the page ran. */
const later = (cli: MockCli) => cli.commands.filter((l) => !isLaunchOrLoad(l));

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const card = (page: Page, name: string) =>
  page.getByRole("article", { exact: true, name });

const openAgents = async (cli: MockCli, page: Page) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  cli.register(["source", "list"], { events: sources });
  cli.register(["config", "remove"], { events: removals });
  await page.goto("/#/agents");
  await expect(page.getByRole("heading", { name: "Agents" })).toBeVisible();
  await expect(card(page, "Claude Code")).toBeVisible();
};

test("lists the direct agents and the Universal card with their counts", async ({
  cli,
  page,
}) => {
  await openAgents(cli, page);
  await expect(
    page.getByRole("region", { name: "Direct agents" }).getByRole("article")
  ).toHaveCount(4);
  await expect(card(page, "Claude Code")).toMatchAriaSnapshot(`
    - article "Claude Code":
      - switch "Enabled" [checked]
      - heading "Claude Code" [level=2]
      - text: /home/me/.claude-code
      - paragraph: 5 skills · 1 server · 1 plugin
  `);
  await expect(card(page, "Cursor")).toContainText(
    "3 skills · 1 server · 0 plugins"
  );
  await expect(card(page, "GitHub Copilot")).toContainText(
    "0 skills · 0 servers · 0 plugins"
  );
  await expect(card(page, "Windsurf").getByRole("switch")).not.toBeChecked();
  await expect(card(page, "Windsurf").getByRole("switch")).toHaveAttribute(
    "title",
    "Disabled · skipped by default"
  );
  await expect(card(page, "Claude Code").getByRole("switch")).toHaveAttribute(
    "title",
    "Enabled · new skills go here by default"
  );

  const universal = card(page, "Universal");
  await expect(universal).toContainText("Read your shared library");
  await expect(universal).toContainText(LIBRARY);
  await expect(universal.getByRole("region")).toHaveCount(2);
  await expect(universal.getByRole("region", { name: "Codex" })).toContainText(
    "7 skills · 1 server"
  );
  await expect(
    universal.getByRole("region", { name: "Gemini CLI" })
  ).toContainText("7 skills · 0 servers");
});

test("turning an agent on runs config enable, and Place all places the library there", async ({
  cli,
  page,
}) => {
  await openAgents(cli, page);
  const toggle = card(page, "Windsurf").getByRole("switch", {
    name: "Enabled",
  });
  const enable = cli.register(["config", "enable", "windsurf"], {
    events: [{ type: "settings" }],
    holdAfter: 0,
  });

  await toggle.click();
  await enable.started;
  await expect(toggle).toHaveAttribute("aria-busy", "true");
  await expect(toggle).toBeDisabled();
  // The switch shows the inventory's state until the next inventory arrives.
  await expect(toggle).not.toBeChecked();
  enable.release();
  await expect(toasts(page).getByText("Windsurf enabled")).toBeVisible();
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  await expect(toggle).toHaveAttribute("aria-busy", "true");
  await cli.serveEmit([
    {
      ...inventory,
      configurations: inventory.configurations.map((c) =>
        c.id === "windsurf" ? { ...c, enabled: true } : c
      ),
      scan_counter: 2,
    },
    {
      ok: true,
      request_id: cli.requestIds.at(-1) ?? "",
      scan_counter: 2,
      type: "refresh_complete",
    },
  ]);
  await expect(toggle).toBeChecked();
  await expect(toggle).not.toHaveAttribute("aria-busy");

  // Seven library skills, none placed in Windsurf.
  const warning =
    "windsurf: /home/me/.windsurf/skills/scratch is a directory of yours and was left as it is";
  cli.register(["config", "enable", "windsurf", "--place-all"], {
    events: [
      { type: "settings" },
      {
        ok: true,
        summary:
          "windsurf is now enabled, 6 skills placed, 1 placement skipped",
        type: "result",
      },
    ],
    logs: [{ level: "warn", message: warning }],
  });
  await toasts(page).getByRole("button", { name: "Place all 7" }).click();
  const toast = toasts(page)
    .getByRole("listitem")
    .filter({ hasText: "Placed 6 skills in Windsurf, 1 skipped" });
  await expect(toast).toBeVisible();
  await expect(toast).toContainText(warning);
  expect(later(cli)).toEqual([
    ["config", "enable", "windsurf", "--json"],
    ["config", "enable", "windsurf", "--place-all", "--json"],
  ]);
});

test("turning an agent off runs config disable", async ({ cli, page }) => {
  await openAgents(cli, page);
  cli.register(["config", "disable", "cursor"], {
    events: [{ type: "settings" }],
  });
  await card(page, "Cursor").getByRole("switch", { name: "Enabled" }).click();
  await expect(
    toasts(page).getByText(
      "Cursor disabled. New skills won't go there by default."
    )
  ).toBeVisible();
  expect(later(cli)).toEqual([["config", "disable", "cursor", "--json"]]);
});

/** The inventory once the configurations `ids` are gone, with the answer to the last refresh. */
const without = (cli: MockCli, ids: string[]) => [
  {
    ...inventory,
    configurations: inventory.configurations.filter((c) => !ids.includes(c.id)),
    scan_counter: 2,
  },
  {
    ok: true,
    request_id: cli.requestIds.at(-1) ?? "",
    scan_counter: 2,
    type: "refresh_complete" as const,
  },
];

const removed = (id: string) => ({
  ...removals.find((e) => e.type === "config_removal" && e.id === id),
  state: "removed" as const,
  type: "config_removal" as const,
});

test("Remove on a card names the directory and runs config remove", async ({
  cli,
  page,
}) => {
  await openAgents(cli, page);
  cli.register(["config", "remove", "github-copilot"], {
    events: [removed("github-copilot"), { type: "settings" }],
  });
  await card(page, "GitHub Copilot")
    .getByRole("button", { name: "More actions for GitHub Copilot" })
    .click();
  await page.getByRole("menuitem", { name: "Remove" }).click();
  const dialog = page.getByRole("dialog", { name: "Remove GitHub Copilot?" });
  await expect(dialog).toContainText(
    "Its configuration directory is deleted from this machine:"
  );
  await expect(dialog).toContainText("/home/me/.github-copilot");

  await dialog.getByRole("button", { name: "Remove" }).click();
  await expect(toasts(page).getByText("Removed GitHub Copilot")).toBeVisible();
  expect(later(cli)).toEqual([
    ["config", "remove", "github-copilot", "--json"],
  ]);
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  await cli.serveEmit(without(cli, ["github-copilot"]));
  await expect(card(page, "GitHub Copilot")).toHaveCount(0);
});

test("Remove on a configuration that holds something says what it holds", async ({
  cli,
  page,
}) => {
  await openAgents(cli, page);
  await card(page, "Claude Code")
    .getByRole("button", { name: "More actions for Claude Code" })
    .click();
  await page.getByRole("menuitem", { name: "Remove" }).click();
  const dialog = page.getByRole("dialog", {
    name: "Claude Code can't be removed",
  });
  await expect(dialog).toContainText("/home/me/.claude-code");
  await expect(dialog.getByRole("list", { name: "What it holds" }))
    .toMatchAriaSnapshot(`
    - list "What it holds":
      - listitem: settings.json
      - listitem: 1 plugin in plugins
      - listitem: the skill notes, which nothing that stays on the machine holds
  `);
  await dialog.getByRole("button", { name: "Close" }).click();
  await expect(dialog).toBeHidden();
  expect(later(cli)).toEqual([]);
});

test("Clean up lists the configurations that can be removed and removes the chosen ones", async ({
  cli,
  page,
}) => {
  await openAgents(cli, page);
  cli.register(["config", "remove", "github-copilot", "windsurf"], {
    events: [
      removed("github-copilot"),
      removed("windsurf"),
      { type: "settings" },
    ],
  });
  await page.getByRole("button", { name: "Clean up" }).click();
  const dialog = page.getByRole("dialog", {
    name: "Remove leftover configurations",
  });
  await expect(dialog).toMatchAriaSnapshot(`
    - dialog "Remove leftover configurations":
      - heading "Remove leftover configurations" [level=2]
      - paragraph: These agent configurations hold only links and copies of skills that stay on this machine. Removing one deletes its directory.
      - list "Configurations that can be removed":
        - listitem:
          - checkbox "GitHub Copilot /home/me/.github-copilot" [checked]
          - text: GitHub Copilot /home/me/.github-copilot
        - listitem:
          - checkbox "Windsurf /home/me/.windsurf" [checked]
          - text: Windsurf /home/me/.windsurf
      - paragraph: 4 other agent configurations hold something of their own and stay.
      - button "Cancel"
      - button "Remove 2"
  `);

  const windsurf = dialog.getByRole("checkbox", { name: /^Windsurf/u });
  await windsurf.click();
  await expect(dialog.getByRole("button", { name: "Remove 1" })).toBeVisible();
  await windsurf.click();
  await dialog.getByRole("button", { name: "Remove 2" }).click();
  await expect(
    toasts(page).getByText("Removed 2 agent configurations")
  ).toBeVisible();
  expect(later(cli)).toEqual([
    ["config", "remove", "github-copilot", "windsurf", "--json"],
  ]);
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  await cli.serveEmit(without(cli, ["github-copilot", "windsurf"]));
  await expect(
    page.getByRole("region", { name: "Direct agents" }).getByRole("article")
  ).toHaveCount(2);
});

test("a removal the CLI refuses says what holds the configuration, in the app's words", async ({
  cli,
  page,
}) => {
  await openAgents(cli, page);
  cli.register(["config", "remove", "github-copilot", "windsurf"], {
    events: [
      {
        code: "refused",
        hint: "'agentx config remove github-copilot' removes the others",
        message: "nothing was removed: windsurf holds notes.md",
        type: "error",
      },
    ],
    exitCode: 6,
  });
  await page.getByRole("button", { name: "Clean up" }).click();
  await page
    .getByRole("dialog", { name: "Remove leftover configurations" })
    .getByRole("button", { name: "Remove 2" })
    .click();
  await expect(toasts(page).getByRole("listitem")).toContainText(
    "Nothing was removed: windsurf holds notes.md. Open Clean up again to remove the others."
  );
});
