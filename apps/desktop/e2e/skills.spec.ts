import type { Locator, Page } from "@playwright/test";

import { adoption, history, inventory } from "./fixtures/skills.ts";
import {
  configuration,
  expect,
  firstInventory,
  loadLines,
  test,
} from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const group = (page: Page, name: string) =>
  list(page).getByRole("region", { name });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const rowNames = async (where: Locator) =>
  await where
    .getByRole("button")
    .evaluateAll((buttons) => buttons.map((b) => b.getAttribute("aria-label")));
/** The accessible names of `where`'s elements: an aria-label, an image's alt, or a label's text. */
const names = async (where: Locator) =>
  await where.evaluateAll((elements) =>
    elements.map(
      (el) =>
        el.getAttribute("aria-label") ??
        el.getAttribute("alt") ??
        el.closest("label")?.textContent ??
        ""
    )
  );

/** Whether a command line is one the launch or the store's reads run, whichever order they come in. */
const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the screen itself ran. */
const later = (cli: MockCli) => cli.commands.filter((l) => !isLaunchOrLoad(l));

/** Opens the Skills screen on the fixture machine and waits for its rows. */
const openSkills = async (cli: MockCli, page: Page, path = "/#/skills") => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  cli.register(["skill", "history", "release-notes"], { events: [] });
  cli.register(["skill", "history", "draft"], { events: [] });
  await page.goto(path);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await expect(row(page, "pdf")).toBeVisible();
};

test.afterEach(({ cli }) => {
  // Read-only: nothing on this screen runs a command but the read of a skill's history.
  for (const line of later(cli)) {
    expect(line.slice(0, 2)).toEqual(["skill", "history"]);
  }
});

test.describe("the list", () => {
  test("groups every skill by where it comes from, in order and with counts", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    await expect(page.getByLabel("9 skills")).toHaveText("9");
    expect(await names(list(page).getByRole("region"))).toEqual([
      "YOUR SKILLS (3)",
      "ACME/SKILLS (2)",
      "ZETA/TOOLS (1)",
      "PLUGINS (1)",
      "ON DISK (2)",
    ]);
    expect(await rowNames(group(page, "YOUR SKILLS (3)"))).toEqual([
      "draft",
      "pdf-letters",
      "release-notes",
    ]);
    // The fork made under a new name is also nested under its original.
    expect(await rowNames(group(page, "ACME/SKILLS (2)"))).toEqual([
      "pdf",
      "pdf-letters, your skill",
      "review",
    ]);
    await expect(row(page, "pdf-letters, your skill")).toContainText(
      "your skill"
    );
    expect(await rowNames(group(page, "ON DISK (2)"))).toEqual([
      "notes",
      "scratch",
    ]);
  });

  test("each row shows its glyph, pills, agent clients and description", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    const pdf = row(page, "pdf");
    await expect(pdf).toContainText("Read and convert PDF files");
    await expect(pdf.getByText("Skill · Enabled")).toBeAttached();
    expect(await names(pdf.getByRole("img"))).toEqual([
      "Claude Code",
      "Codex",
      "Gemini CLI",
    ]);

    const draft = row(page, "draft");
    await expect(draft.getByText("Not published")).toBeVisible();
    await expect(draft.getByText("Your skill · Enabled")).toBeAttached();
    expect(await names(draft.getByRole("img"))).toEqual([
      "Codex",
      "Gemini CLI",
    ]);

    await expect(
      row(page, "gone").getByText("Removed from source")
    ).toBeVisible();
    await expect(
      row(page, "review").getByText("Edited · Enabled")
    ).toBeAttached();
    await expect(row(page, "release-notes").getByRole("img")).toHaveCount(4);
    await expect(
      row(page, "commit-helper").getByText("Plugin skill · Enabled")
    ).toBeAttached();
    expect(await names(row(page, "notes").getByRole("img"))).toEqual([
      "Cursor",
    ]);
  });

  test("shows a monogram for an agent client with no bundled logo", async ({
    cli,
    page,
  }) => {
    const outside: string[] = [];
    page.on("request", (request) => {
      if (!request.url().startsWith("http://localhost")) {
        outside.push(request.url());
      }
    });
    cli.onServeStart({
      events: [
        {
          ...firstInventory(),
          configurations: [
            configuration("claude-code", { name: "Claude Code" }),
            configuration("zed", { name: "Zed" }),
          ],
          library: [
            {
              content_hash: "h",
              kind: "unmanaged",
              name: "scratch",
              placements: [
                {
                  configuration: "zed",
                  kind: "symlink",
                  mode: "symlink",
                  path: "/home/me/.zed/skills/scratch",
                },
              ],
              universal: [],
            },
          ],
        },
      ],
    });
    await page.goto("/#/skills");
    await expect(row(page, "scratch")).toBeVisible();
    const logos = row(page, "scratch").getByRole("img");
    expect(await names(logos)).toEqual(["Zed"]);
    await expect(logos.first()).toHaveAttribute("src", /^data:image\/svg/u);
    expect(outside).toEqual([]);
  });
});
