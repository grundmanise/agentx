import type { Locator, Page } from "@playwright/test";

import {
  adoption,
  configurations,
  history,
  inventory,
} from "./fixtures/skills.ts";
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
const filters = (page: Page) => page.getByRole("dialog", { name: "Filters" });

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

test.describe("filters", () => {
  test("the name filter matches a substring, whatever the case", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    await page
      .getByRole("searchbox", { name: "Filter skills by name" })
      .fill("PDF");
    expect(await rowNames(list(page))).toEqual([
      "pdf-letters",
      "pdf",
      "pdf-letters, your skill",
    ]);
    await page.getByRole("searchbox").fill("zzz");
    await expect(list(page).getByText("No skills match")).toBeVisible();
  });

  test("SHOW keeps one kind of skill, and Reset filters clears it", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    await page.getByRole("button", { name: "Filter" }).click();
    await expect(
      filters(page).getByRole("radio", { name: "All skills 9" })
    ).toBeChecked();
    await filters(page).getByRole("radio", { name: "Updates 1" }).check();
    expect(await rowNames(list(page))).toEqual(["review"]);
    await filters(page).getByRole("radio", { name: "Yours 3" }).check();
    expect(await rowNames(list(page))).toEqual([
      "draft",
      "pdf-letters",
      "release-notes",
    ]);
    await filters(page).getByRole("radio", { name: "Unmanaged 2" }).check();
    expect(await rowNames(list(page))).toEqual(["notes", "scratch"]);
    await expect(
      page.getByRole("button", { name: "Filter 1 applied" })
    ).toBeVisible();

    await filters(page).getByRole("button", { name: "Reset filters" }).click();
    await expect(page.getByLabel("9 skills")).toBeVisible();
    expect(await rowNames(list(page))).toHaveLength(10);
    await expect(
      filters(page).getByRole("button", { name: "Reset filters" })
    ).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(filters(page)).toBeHidden();
  });

  test("STATE keeps the skills an agent client sees, or the ones none does", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    await page.getByRole("button", { name: "Filter" }).click();
    const state = filters(page).getByRole("group", { name: "State" });
    await state.getByRole("radio", { name: "Enabled" }).check();
    expect(await rowNames(list(page))).toHaveLength(10);
    // Every library skill is in a universal client here: none is disabled.
    await state.getByRole("radio", { name: "Disabled" }).check();
    await expect(list(page).getByText("No skills match")).toBeVisible();

    // On a machine with no universal client, a skill placed nowhere is disabled.
    await cli.serveEmit([
      {
        ...inventory,
        configurations: configurations.filter((c) => !c.reads_library),
        library: inventory.library.map((e) => ({
          ...e,
          placements: e.placements.filter((p) => p.mode !== "library"),
          universal: [],
        })),
        scan_counter: 2,
      },
    ]);
    expect(await rowNames(list(page))).toEqual(["draft", "scratch"]);
  });

  test("AGENTS keeps the skills any chosen agent client sees, echoed in a chip", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    await page.getByRole("button", { name: "Filter" }).click();
    const agents = filters(page).getByRole("group", { name: "Agents" });
    await expect(agents).toMatchAriaSnapshot(`
      - group "Agents":
        - checkbox "Claude Code"
        - checkbox "Cursor"
        - checkbox "Universal Codex, Gemini CLI"
    `);
    await agents.getByRole("checkbox", { name: "Cursor" }).check();
    expect(await rowNames(list(page))).toEqual([
      "release-notes",
      "review",
      "notes",
    ]);
    const chip = page
      .getByRole("button", { name: "Clear agents" })
      .locator("..");
    await expect(chip).toContainText("Cursor");
    expect(await names(chip.getByRole("img"))).toEqual(["Cursor"]);

    await agents.getByRole("checkbox", { name: "Claude Code" }).check();
    await expect(chip).toContainText("2 agents");
    expect(await rowNames(list(page))).toEqual([
      "pdf-letters",
      "release-notes",
      "pdf",
      "pdf-letters, your skill",
      "review",
      "gone",
      "commit-helper",
      "notes",
    ]);

    await agents.getByRole("checkbox", { name: /^Universal/u }).check();
    await expect(chip).toContainText("3 agents");
    expect(await rowNames(list(page))).toHaveLength(10);

    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Clear agents" }).click();
    await expect(chip).toHaveCount(0);
    await expect(
      page.getByRole("button", { exact: true, name: "Filter" })
    ).toBeVisible();
  });

  test("the sort toggle reverses the order within each group", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    await page.getByRole("button", { name: "Sort Z to A" }).click();
    expect(await rowNames(group(page, "YOUR SKILLS (3)"))).toEqual([
      "release-notes",
      "pdf-letters",
      "draft",
    ]);
    expect(await rowNames(group(page, "ACME/SKILLS (2)"))).toEqual([
      "review",
      "pdf",
      "pdf-letters, your skill",
    ]);
    // The groups keep their order.
    await expect(list(page).getByRole("region").first()).toHaveAccessibleName(
      "YOUR SKILLS (3)"
    );
    await page.getByRole("button", { name: "Sort A to Z" }).click();
    expect(await rowNames(group(page, "YOUR SKILLS (3)"))).toEqual([
      "draft",
      "pdf-letters",
      "release-notes",
    ]);
  });
});

test.describe("keyboard", () => {
  test("j, k and the arrows move the selection in group order", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    await expect(row(page, "draft")).toHaveAttribute("aria-current", "true");
    await page.keyboard.press("j");
    await expect(row(page, "pdf-letters")).toHaveAttribute(
      "aria-current",
      "true"
    );
    await expect(page).toHaveURL(/#\/skills\/pdf-letters$/u);
    await page.keyboard.press("j");
    await expect(row(page, "release-notes")).toHaveAttribute(
      "aria-current",
      "true"
    );
    await page.keyboard.press("ArrowDown");
    // From the last of YOUR SKILLS into the first source.
    await expect(row(page, "pdf")).toHaveAttribute("aria-current", "true");
    await page.keyboard.press("k");
    await expect(row(page, "release-notes")).toHaveAttribute(
      "aria-current",
      "true"
    );
    await page.keyboard.press("ArrowUp");
    await expect(row(page, "pdf-letters")).toHaveAttribute(
      "aria-current",
      "true"
    );
    // Typing in the search field is not moving the selection.
    await page.getByRole("searchbox").pressSequentially("kj");
    await expect(list(page).getByText("No skills match")).toBeVisible();
    await expect(page).toHaveURL(/#\/skills\/pdf-letters$/u);
  });
});
