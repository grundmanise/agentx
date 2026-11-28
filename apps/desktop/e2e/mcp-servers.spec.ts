import type { Locator, Page } from "@playwright/test";

import {
  DB_COMMAND,
  emptyInventory,
  FILESYSTEM_COMMAND,
  GITHUB_HINT,
  GITHUB_URL,
  handshakeInventory,
  inventory,
  LINEAR_HINT,
  LINEAR_URL,
  NOTES_COMMAND,
  SLACK_URL,
} from "./fixtures/servers.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

declare global {
  interface Window {
    /** What the Tools column and Check tools said each time the page drew them, as a test records them. */
    checkStates?: string[];
  }
}

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const table = (page: Page) => page.getByRole("table", { name: "MCP servers" });
const row = (page: Page, name: string) =>
  table(page).getByRole("row", { exact: true, name });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const checkTools = (page: Page) =>
  page.getByRole("button", { name: "Check tools" });

/** The accessible names of `where`'s elements: an aria-label or an image's alt. */
const names = async (where: Locator) =>
  await where.evaluateAll((elements) =>
    elements.map(
      (el) => el.getAttribute("aria-label") ?? el.getAttribute("alt") ?? ""
    )
  );

/** Whether a command line is one the launch runs. */
const isLaunch = (line: string[]) =>
  launchLines.some((known) => known.join(" ") === line.join(" "));

/** The command lines the screen itself ran. */
const later = (cli: MockCli) => cli.commands.filter((l) => !isLaunch(l));

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
      "slack",
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

    // A plugin's server carries the plugin's tag.
    const acmeDb = row(page, "acme-db");
    await expect(acmeDb.getByText("acme-tools plugin")).toBeVisible();
    await expect(acmeDb).toContainText(DB_COMMAND);
    await expect(acmeDb.getByText("2 tools", { exact: true })).toBeVisible();

    // A client that turned the server off shows no logo: only Gemini CLI has linear on.
    const linear = row(page, "linear");
    await expect(linear).toContainText(LINEAR_URL);
    await expect(linear.getByText("sse", { exact: true })).toBeVisible();
    expect(await names(linear.getByRole("img"))).toEqual(["Gemini CLI"]);

    // A server every client turned off stays, with its tools, and its agents cell says so.
    const slack = row(page, "slack");
    await expect(slack).toContainText(SLACK_URL);
    await expect(slack.getByText("3 tools", { exact: true })).toBeVisible();
    await expect(slack.getByText("Turned off")).toBeVisible();

    // A server never checked.
    const notes = row(page, "local-notes");
    await expect(notes).toContainText(NOTES_COMMAND);
    await expect(notes.getByText("Not checked")).toBeVisible();

    // Check tools says when the last check was: the latest signature_at.
    await expect(checkTools(page)).toHaveAttribute(
      "title",
      /^Last check: Oct 21/u
    );
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
  });
});

test.describe("Check tools", () => {
  test("runs a scan with a handshake and toasts the counts; the tools, the failures and the time of the check come with serve's next inventory, in one change", async ({
    cli,
    page,
  }) => {
    await openServers(cli, page);
    cli.register(["scan", "--handshake"], { events: [handshakeInventory] });
    // What the Tools column says of three servers and when the last check was, as the page draws
    // them from here on.
    await page.evaluate(() => {
      const seen: string[] = [];
      window.checkStates = seen;
      const record = () => {
        const state = [
          ...["filesystem", "github", "linear"].map((name) => {
            const cell = document.querySelector(`tr[aria-label="${name}"]`)
              ?.children[2];
            const said = [...(cell?.children ?? [])].map(
              (el) => el.textContent
            );
            return `${name}: ${said.join(", ")}`;
          }),
          document
            .querySelector('button[title^="Last check"]')
            ?.getAttribute("title"),
        ].join(" | ");
        if (seen.at(-1) !== state) {
          seen.push(state);
        }
      };
      record();
      new MutationObserver(record).observe(document.body, {
        attributes: true,
        characterData: true,
        childList: true,
        subtree: true,
      });
    });

    await checkTools(page).click();
    await expect(
      toasts(page).getByText(
        "Checked 6 servers · 1 need sign-in, 1 unreachable"
      )
    ).toBeVisible();
    expect(later(cli)).toEqual([["scan", "--handshake", "--json"]]);
    expect(cli.deadlines.at(-1)).toBe(120_000);

    // Serve is asked to scan again: the one-shot's inventory is not applied.
    await expect.poll(() => cli.serveRequests.length).toBe(1);
    const [line = ""] = cli.serveRequests;
    expect(JSON.parse(line)).toEqual({
      request_id: expect.any(String),
      type: "refresh",
    });
    await cli.serveEmit([{ ...handshakeInventory, scan_counter: 2 }]);

    // The failure words, whose hover shows the CLI's hint. Both of github's declarations failed
    // the same way: the toast counts the server once, and the row shows the word once.
    const signIn = row(page, "github").getByText("needs sign-in");
    await expect(signIn).toHaveCount(1);
    await expect(signIn).toBeVisible();
    await signIn.hover();
    await expect(signIn).toHaveAttribute("title", GITHUB_HINT);
    const unreachable = row(page, "linear").getByText("unreachable");
    await expect(unreachable).toBeVisible();
    await expect(unreachable).toHaveAttribute("title", LINEAR_HINT);

    expect(await page.evaluate(() => window.checkStates)).toEqual([
      expect.stringMatching(
        /^filesystem: 4 tools \| github: 12 tools \| linear: 5 tools \| Last check: Oct 21/u
      ),
      expect.stringMatching(
        /^filesystem: 6 tools \| github: 12 tools, needs sign-in \| linear: 5 tools, unreachable \| Last check: Oct 22/u
      ),
    ]);
  });

  test("Cancel in the toast stops the check", async ({ cli, page }) => {
    await openServers(cli, page);
    const scan = cli.register(["scan", "--handshake"], {
      events: [handshakeInventory],
      holdAfter: 0,
    });

    await checkTools(page).click();
    await scan.started;
    await expect(checkTools(page)).toHaveAttribute("aria-busy", "true");
    await expect(toasts(page).getByText("Checking tools")).toBeVisible();
    await toasts(page).getByRole("button", { name: "Cancel" }).click();
    await expect(
      toasts(page).getByText("Stopped", { exact: true })
    ).toBeVisible();
    expect(cli.cancels).toHaveLength(1);
    await expect(checkTools(page)).toHaveAttribute("aria-busy", "false");
    expect(later(cli)).toEqual([["scan", "--handshake", "--json"]]);
  });
});
