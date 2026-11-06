import type { Locator, Page } from "@playwright/test";

import {
  ACME_ICON,
  emptyInventory,
  inventory,
  TEAM_DB,
} from "./fixtures/plugins.ts";
import { filesOf } from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const list = (page: Page) => page.getByRole("list", { name: "Plugins" });
const card = (page: Page, name: string) =>
  list(page).getByRole("article", { exact: true, name });
const group = (where: Locator, name: string) =>
  where.getByRole("region", { exact: true, name });

/** The accessible names of `where`'s elements: an aria-label or an image's alt. */
const names = async (where: Locator) =>
  await where.evaluateAll((elements) =>
    elements.map(
      (el) => el.getAttribute("aria-label") ?? el.getAttribute("alt") ?? ""
    )
  );

/** Whether a command line is one the launch or the store's reads run, whichever order they come in. */
const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the screens themselves ran. */
const later = (cli: MockCli) => cli.commands.filter((l) => !isLaunchOrLoad(l));

/** Opens the Plugins screen on the fixture machine and waits for its cards. */
const openPlugins = async (cli: MockCli, page: Page) => {
  cli.onServeStart({ events: [inventory] });
  await page.goto("/#/plugins");
  await expect(page.getByRole("heading", { name: "Plugins" })).toBeVisible();
  await expect(card(page, "acme-tools")).toBeVisible();
};

test.afterEach(({ cli }) => {
  // Read-only: no control on this screen runs a command. The one read is the Skills screen's,
  // of the files of the skill a row opened there.
  for (const line of later(cli)) {
    expect(line.slice(0, 2)).toEqual(["skill", "files"]);
  }
});

test.describe("cards", () => {
  test("one card per plugin, with its version, Disabled pill, agent and marketplace, and groups", async ({
    cli,
    page,
  }) => {
    await openPlugins(cli, page);
    expect(await names(list(page).getByRole("article"))).toEqual([
      "acme-tools",
      "codex-review",
      "notes-local",
    ]);

    // Claude Code's plugin: its own icon and the groups SKILLS, SERVERS, HOOKS, in that order.
    const acme = card(page, "acme-tools");
    await expect(acme.getByRole("img", { name: "acme-tools" })).toHaveAttribute(
      "src",
      ACME_ICON
    );
    await expect(acme).toMatchAriaSnapshot(`
      - article "acme-tools":
        - img "acme-tools"
        - text: 1.4.0
        - heading "acme-tools" [level=2]
        - text: Claude Code · acme-marketplace
        - region "SKILLS":
          - heading "SKILLS" [level=3]
          - list:
            - listitem:
              - button "commit-helper"
            - listitem:
              - button "release-check"
        - region "SERVERS":
          - heading "SERVERS" [level=3]
          - list:
            - listitem:
              - button "${TEAM_DB}"
        - region "HOOKS":
          - heading "HOOKS" [level=3]
          - list:
            - listitem: format-on-save PostToolUse
            - listitem: load-context.sh SessionStart
    `);
    await expect(acme.getByText("Disabled")).toHaveCount(0);
    // A server row carries its transport; a hook row opens nothing.
    await expect(group(acme, "SERVERS").getByText("stdio")).toBeVisible();
    await expect(
      group(acme, "HOOKS").getByRole("button", { name: "format-on-save" })
    ).toHaveCount(0);

    // Codex's plugin, turned off: Disabled before the version, a monogram, and one skill.
    const codex = card(page, "codex-review");
    await expect(codex).toMatchAriaSnapshot(`
      - article "codex-review":
        - text: Disabled 0.3.1
        - heading "codex-review" [level=2]
        - text: Codex · openai-plugins
        - region "SKILLS":
          - heading "SKILLS" [level=3]
          - list:
            - listitem:
              - button "review-pr"
    `);
    await expect(codex.getByRole("img")).toHaveCount(0);
    await expect(codex.getByText("C", { exact: true })).toBeVisible();
    await expect(codex.getByRole("region")).toHaveCount(1);

    // Cursor's local plugin: no version, no marketplace, nothing it provides.
    const notes = card(page, "notes-local");
    await expect(notes).toMatchAriaSnapshot(`
      - article "notes-local":
        - text: no version
        - heading "notes-local" [level=2]
        - text: Cursor · local
    `);
    await expect(notes.getByRole("region")).toHaveCount(0);
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
    await expect(list(page)).toHaveCount(0);
  });
});

test.describe("navigation", () => {
  test("a skill row opens the skill in Skills, selected", async ({
    cli,
    page,
  }) => {
    await openPlugins(cli, page);
    // The Skills screen reads the files of the skill it shows, a plugin's by "<plugin>:<name>".
    cli.register(["skill", "files", "acme-tools:release-check"], {
      events: filesOf("release-check"),
    });
    await group(card(page, "acme-tools"), "SKILLS")
      .getByRole("button", { name: "release-check" })
      .click();
    await expect(page).toHaveURL(/#\/skills\/skill%3Ah-release$/u);
    const row = page
      .getByRole("navigation", { name: "Skills" })
      .getByRole("button", { exact: true, name: "release-check" });
    await expect(row).toHaveAttribute("aria-current", "true");
    await expect(
      page
        .getByRole("region", { name: "Skill details" })
        .getByRole("heading", { level: 1, name: "release-check" })
    ).toBeVisible();
  });

  test("a server row opens MCP servers with the server's row in view", async ({
    cli,
    page,
  }) => {
    await openPlugins(cli, page);
    await group(card(page, "acme-tools"), "SERVERS")
      .getByRole("button", { name: TEAM_DB })
      .click();
    await expect(page).toHaveURL(/#\/servers\/server%3Ateam-db$/u);
    const table = page.getByRole("table", { name: "MCP servers" });
    await expect(
      table.getByRole("row", { exact: true, name: TEAM_DB })
    ).toBeInViewport();
    // The row is the last of ten: the screen scrolled to it, past the first.
    await expect(
      table.getByRole("row", { exact: true, name: "alpha-1" })
    ).not.toBeInViewport();
  });
});

test.describe("read-only", () => {
  test("no command runs after startup, and no image is requested outside the dev server", async ({
    cli,
    page,
  }) => {
    const outside: string[] = [];
    page.on("request", (request) => {
      if (!request.url().startsWith("http://localhost:1420/")) {
        outside.push(request.url());
      }
    });
    await openPlugins(cli, page);
    await expect(card(page, "acme-tools").getByRole("img")).toBeVisible();
    expect(later(cli)).toEqual([]);
    expect(outside).toEqual([]);
  });
});
