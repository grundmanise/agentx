// The Agents page: every configuration the scan detected, with its counts from the inventory's
// edges, and the Enabled switch, which runs `config enable|disable <id>` and offers Place all.
import type { Page } from "@playwright/test";

import { inventory, LIBRARY, sources } from "./fixtures/agents.ts";
import { adoption } from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

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
