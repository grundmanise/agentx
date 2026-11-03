import type { Locator, Page } from "@playwright/test";

import {
  DB_COMMAND,
  emptyInventory,
  FILESYSTEM_COMMAND,
  GITHUB_URL,
  inventory,
  LINEAR_URL,
  NOTES_COMMAND,
  SECRET_NAMES,
} from "./fixtures/servers.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const table = (page: Page) => page.getByRole("table", { name: "MCP servers" });
const row = (page: Page, name: string) =>
  table(page).getByRole("row", { exact: true, name });

/** The accessible names of `where`'s elements: an aria-label or an image's alt. */
const names = async (where: Locator) =>
  await where.evaluateAll((elements) =>
    elements.map(
      (el) => el.getAttribute("aria-label") ?? el.getAttribute("alt") ?? ""
    )
  );

/** No environment variable or header name shows, let alone a value. */
const noSecrets = async (page: Page) => {
  await Promise.all(
    SECRET_NAMES.map(async (secret) => {
      await expect(page.getByText(secret)).toHaveCount(0);
    })
  );
};

/** Whether a command line is one the launch or the store's reads run, whichever order they come in. */
const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the screen itself ran. */
const later = (cli: MockCli) => cli.commands.filter((l) => !isLaunchOrLoad(l));

/** Opens the MCP servers screen on the fixture machine and waits for its rows. */
const openServers = async (cli: MockCli, page: Page) => {
  cli.onServeStart({ events: [inventory] });
  await page.goto("/#/servers");
  await expect(
    page.getByRole("heading", { name: "MCP servers" })
  ).toBeVisible();
  await expect(row(page, "github")).toBeVisible();
};

test.describe("rows", () => {
  test("one row per server, sorted by name, with its address, transport, tools and agents", async ({
    cli,
    page,
  }) => {
    await openServers(cli, page);
    expect(await names(table(page).getByRole("row"))).toEqual([
      "",
      "acme-db",
      "filesystem",
      "github",
      "linear",
      "local-notes",
    ]);
    await expect(table(page).getByRole("columnheader")).toHaveText([
      "SERVER",
      "TRANSPORT",
      "TOOLS",
      "AGENTS",
    ]);

    // A remote server declared by two agents: its URL, one pill, serve's tool count, two logos.
    const github = row(page, "github");
    await expect(github).toContainText(GITHUB_URL);
    await expect(
      github.getByText("streamable-http", { exact: true })
    ).toBeVisible();
    await expect(github.getByText("12 tools", { exact: true })).toBeVisible();
    expect(await names(github.getByRole("img"))).toEqual([
      "Claude Code",
      "Cursor",
    ]);

    // A package server in two versions: the command with its arguments, and "2 versions".
    const filesystem = row(page, "filesystem");
    await expect(filesystem).toContainText(FILESYSTEM_COMMAND);
    await expect(filesystem.getByText("stdio", { exact: true })).toBeVisible();
    await expect(filesystem.getByText("2 versions")).toBeVisible();
    // The tool count of the version checked last.
    await expect(
      filesystem.getByText("4 tools", { exact: true })
    ).toBeVisible();
    expect(await names(filesystem.getByRole("img"))).toEqual([
      "Claude Code",
      "Codex",
    ]);
    await expect(github.getByText(/versions/u)).toHaveCount(0);

    // A plugin's server carries the plugin's tag.
    const acmeDb = row(page, "acme-db");
    await expect(acmeDb.getByText("acme-tools plugin")).toBeVisible();
    await expect(acmeDb).toContainText(DB_COMMAND);
    await expect(acmeDb.getByText("2 tools", { exact: true })).toBeVisible();

    // A declaration the agent turned off: its logo says so.
    const linear = row(page, "linear");
    await expect(linear).toContainText(LINEAR_URL);
    await expect(linear.getByText("sse", { exact: true })).toBeVisible();
    await expect(
      linear.getByTitle("Codex turned this server off")
    ).toBeVisible();
    await expect(
      linear.getByTitle("Gemini CLI", { exact: true })
    ).toBeVisible();
    expect(await names(linear.getByRole("img"))).toEqual([
      "Codex",
      "Gemini CLI",
    ]);

    // A server never checked.
    const notes = row(page, "local-notes");
    await expect(notes).toContainText(NOTES_COMMAND);
    await expect(notes.getByText("Not checked")).toBeVisible();

    // The footnote names the latest signature_at.
    await expect(
      page.getByText(/^Tool counts come from the last check, Oct 21/u)
    ).toContainText(
      "Servers you turned off in an agent are never started, and header or environment values are never shown."
    );

    await noSecrets(page);
    expect(later(cli)).toEqual([]);
  });

  test("a machine with no server shows the empty state", async ({
    cli,
    page,
  }) => {
    cli.onServeStart({ events: [emptyInventory] });
    await page.goto("/#/servers");
    await expect(
      page.getByText("No MCP servers found in your agents.")
    ).toBeVisible();
    await expect(table(page)).toHaveCount(0);
    expect(later(cli)).toEqual([]);
  });
});
