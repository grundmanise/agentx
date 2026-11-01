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
const detail = (page: Page) =>
  page.getByRole("region", { name: "Skill details" });
const pane = (page: Page, name: string) =>
  detail(page).getByRole("article", { name });
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
    // The fork made under a new name is listed once; its original carries a marker for it.
    expect(await rowNames(group(page, "ACME/SKILLS (2)"))).toEqual([
      "pdf",
      "review",
    ]);
    await expect(
      row(page, "pdf").getByTitle("Forked as pdf-letters")
    ).toBeAttached();
    await expect(row(page, "pdf")).toHaveAccessibleDescription(
      "Skill · Enabled. Forked as pdf-letters. Read and convert PDF files"
    );
    await expect(list(page).getByTitle(/^Forked as/u)).toHaveCount(1);
    // Selecting the fork highlights exactly one row.
    await row(page, "pdf-letters").click();
    await expect(list(page).locator("[aria-current=true]")).toHaveCount(1);
    await expect(row(page, "pdf-letters")).toHaveAttribute(
      "aria-current",
      "true"
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
    await expect(pdf).toHaveAccessibleDescription(/^Skill · Enabled\./u);
    expect(await names(pdf.getByRole("img"))).toEqual([
      "Claude Code",
      "Codex",
      "Gemini CLI",
    ]);

    const draft = row(page, "draft");
    await expect(
      draft.getByText("Not published", { exact: true })
    ).toBeVisible();
    await expect(draft).toHaveAccessibleDescription(
      "Your skill · Enabled. Not published. A skill you started and never published"
    );
    expect(await names(draft.getByRole("img"))).toEqual([
      "Codex",
      "Gemini CLI",
    ]);

    await expect(
      row(page, "gone").getByText("Removed from source", { exact: true })
    ).toBeVisible();
    await expect(row(page, "review")).toHaveAccessibleDescription(
      /^Edited · Enabled\./u
    );
    await expect(row(page, "release-notes").getByRole("img")).toHaveCount(4);
    await expect(row(page, "commit-helper")).toHaveAccessibleDescription(
      /^Plugin skill · Enabled\./u
    );
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
    expect(
      await names(page.getByRole("region", { name: "Agents" }).getByRole("img"))
    ).toEqual(["Claude Code", "Zed"]);
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
    expect(await rowNames(list(page))).toEqual(["pdf-letters", "pdf"]);
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
    expect(await rowNames(list(page))).toHaveLength(9);
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
    expect(await rowNames(list(page))).toHaveLength(9);
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
      "review",
      "gone",
      "commit-helper",
      "notes",
    ]);

    await agents.getByRole("checkbox", { name: /^Universal/u }).check();
    await expect(chip).toContainText("3 agents");
    expect(await rowNames(list(page))).toHaveLength(9);

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
    await expect(pane(page, "pdf")).toBeVisible();
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
    await expect(pane(page, "pdf-letters")).toBeVisible();
  });
});

test.describe("the detail pane", () => {
  test("a skill of a source: no command runs, and no history is kept", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page, "/#/skills/pdf");
    const article = pane(page, "pdf");
    await expect(article).toMatchAriaSnapshot(`
      - article "pdf":
        - text: Skill · Enabled
        - heading "pdf" [level=1]
        - text: acme/skills
        - button "Forked as pdf-letters"
        - paragraph: Read and convert PDF files
        - region "Agents":
          - heading "Agents" [level=2]
          - switch "Claude Code" [checked]
          - switch "Cursor"
          - switch "Universal" [checked]
        - region "Files":
          - heading "Files" [level=2]
        - region "Details":
          - heading "Details" [level=2]
          - term: Source
          - definition: https://github.com/acme/skills/pdf
          - term: Version
          - definition: 3f2a9c1
          - term: State
          - definition: current
        - region "History":
          - heading "History" [level=2]
          - paragraph: History is kept for your own skills
    `);
    expect(later(cli)).toEqual([]);
    // The fork pill opens the fork.
    await article
      .getByRole("button", { name: "Forked as pdf-letters" })
      .click();
    await expect(pane(page, "pdf-letters")).toBeVisible();
    await expect(page).toHaveURL(/#\/skills\/pdf-letters$/u);
    await expect(list(page).locator("[aria-current=true]")).toHaveCount(1);
  });

  test("one of your own skills: its history, with Show all opening the sheet", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page, "/#/skills/pdf-letters");
    const article = pane(page, "pdf-letters");
    await expect(article.getByRole("list", { name: "History" })).toBeVisible();
    expect(later(cli)).toEqual([["skill", "history", "pdf-letters", "--json"]]);
    await expect(article).toMatchAriaSnapshot(`
      - article "pdf-letters":
        - text: Your skill · Enabled
        - heading "pdf-letters" [level=1]
        - text: from acme/skills
        - paragraph: Read PDFs and write letters from them
        - region "Agents":
          - heading "Agents" [level=2]
          - switch "Claude Code" [checked]
          - switch "Cursor"
          - switch "Universal" [checked]
        - region "Files":
          - heading "Files" [level=2]
        - region "Details":
          - heading "Details" [level=2]
          - term: Forked from
          - definition: https://github.com/acme/skills/pdf
          - term: Based on
          - definition: 3f2a9c1
          - term: Published to
          - definition: git@github.com:me/account.git
          - term: Files
          - definition: /home/me/.agentx/worktrees/pdf-letters/pdf-letters
          - term: Id
          - definition: f0f0f0f
        - region "History":
          - heading "History" [level=2]
          - list "History":
            - listitem: /Tighten the examples\\s+Oct 2(, 2026)?\\s+· 1 file · made with Git/
            - listitem: /pdf-letters:\\sedit SKILL\\.md and add examples\\.md \\(studio-mac\\)\\s+Oct 2(, 2026)?\\s+· 2 files/
            - listitem: /pdf-letters:\\sedit SKILL\\.md \\(studio-mac\\)\\s+Oct 1(, 2026)?\\s+· 1 file/
            - listitem:
              - button "Show all 5"
    `);

    await article.getByRole("button", { name: "Show all 5" }).click();
    const sheet = page.getByRole("dialog", { name: "History" });
    await expect(sheet).toContainText("pdf-letters");
    const entries = sheet
      .getByRole("list", { name: "All history" })
      .getByRole("listitem");
    await expect(entries).toHaveCount(5);
    await expect(entries.last()).toContainText("from the original");
    // A long subject wraps inside the sheet; the sheet never scrolls sideways.
    const long = sheet.getByRole("button", {
      name: "pdf-letters: edit SKILL.md and add examples.md (studio-mac)",
    });
    const line = await long.evaluate((el) => el.getBoundingClientRect().height);
    expect(line).toBeGreaterThan(40);
    const sideways = async () =>
      await sheet.getByRole("list", { name: "All history" }).evaluate((el) => {
        const box = el.parentElement;
        return box === null ? -1 : box.scrollWidth - box.clientWidth;
      });
    expect(await sideways()).toBe(0);
    await long.click();
    await expect(
      sheet.getByRole("list", { name: /^Files of pdf-letters: edit/u })
    ).toBeVisible();
    expect(await sideways()).toBe(0);
    await long.click();
    await sheet
      .getByRole("button", { name: "Import pdf from acme/skills" })
      .click();
    const files = sheet
      .getByRole("list", { name: "Files of Import pdf from acme/skills" })
      .getByRole("listitem");
    await expect(files).toHaveText(["addedSKILL.md", "addedreference.md"]);
    await page.keyboard.press("Escape");
    await expect(sheet).toBeHidden();
    // The history was read once: selecting the skill again does not read it again.
    await row(page, "pdf").click();
    await row(page, "pdf-letters").click();
    await expect(article.getByRole("list", { name: "History" })).toBeVisible();
    expect(later(cli)).toEqual([["skill", "history", "pdf-letters", "--json"]]);

    // The skill changes and its history cannot be read: the entries of the version before
    // are not shown as if they were this version's.
    cli.register(["skill", "history", "pdf-letters"], { exitCode: 1 });
    await cli.serveEmit([
      {
        ...inventory,
        library: inventory.library.map((e) =>
          e.name === "pdf-letters" ? { ...e, state: "modified" } : e
        ),
        scan_counter: 2,
      },
    ]);
    await expect(
      article.getByText("The history could not be read")
    ).toBeVisible();
    await expect(article.getByRole("list", { name: "History" })).toHaveCount(0);
    expect(later(cli)).toEqual([
      ["skill", "history", "pdf-letters", "--json"],
      ["skill", "history", "pdf-letters", "--json"],
    ]);
  });

  test("a plugin's skill and an unmanaged one", async ({ cli, page }) => {
    await openSkills(cli, page, "/#/skills/skill%3Ah-commit");
    await expect(pane(page, "commit-helper")).toMatchAriaSnapshot(`
      - article "commit-helper":
        - text: Plugin skill · Enabled
        - heading "commit-helper" [level=1]
        - text: acme-tools plugin
        - paragraph: Write commit messages the way the team does
        - region "Agents":
          - heading "Agents" [level=2]
          - switch "Claude Code" [checked]
          - switch "Cursor"
          - switch "Universal"
        - region "Files":
          - heading "Files" [level=2]
        - region "Details":
          - heading "Details" [level=2]
          - term: Plugin
          - definition: acme-tools 1.4.0
          - term: Marketplace
          - definition: acme-marketplace
          - term: Agent
          - definition: Claude Code
        - region "History":
          - heading "History" [level=2]
          - paragraph: History is kept for your own skills
    `);

    await row(page, "scratch").click();
    await expect(pane(page, "scratch")).toMatchAriaSnapshot(`
      - article "scratch":
        - text: Unmanaged · Enabled
        - heading "scratch" [level=1]
        - text: skills CLI
        - paragraph: Notes the skills CLI installed
        - region "Agents":
          - heading "Agents" [level=2]
          - switch "Claude Code"
          - switch "Cursor"
          - switch "Universal" [checked]
        - region "Files":
          - heading "Files" [level=2]
        - region "Details":
          - heading "Details" [level=2]
          - term: Found at
          - definition: /home/me/.agents/skills/scratch
          - term: Installed by
          - definition: skills CLI · https://github.com/vercel/skills
        - region "History":
          - heading "History" [level=2]
          - paragraph: History is kept for your own skills
    `);
    expect(later(cli)).toEqual([]);
  });

  test("an upstream-removed skill shows the banner, and a displaced placement its chip", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page, "/#/skills/gone");
    const article = pane(page, "gone");
    await expect(
      article.getByRole("note", { name: "Removed from its source" })
    ).toContainText("It still works but won't get updates.");
    await expect(article.getByText("Removed from source")).toBeVisible();

    await row(page, "release-notes").click();
    const cursor = pane(page, "release-notes").getByRole("switch", {
      exact: true,
      name: "Cursor, displaced",
    });
    await expect(cursor).toBeChecked();
    await expect(cursor).toContainText("displaced");
    await expect(pane(page, "release-notes").getByRole("note")).toHaveCount(0);
  });
});
