// Adopting another tool's skills all at once: the Adopt skills section of Settings and the
// Unmanaged group's Import all, both opening the adopt dialog. The dialog lists the skills before
// the run (the ones the inventory lists as refused marked, with why), runs one `skill import --all`
// with a `--from` for each tool it covers, with each row's state and one progress line, then shows
// each row's result and a summary. A run that fails as a whole says why in the dialog.
import type { Locator, Page } from "@playwright/test";

import {
  bothToolsRun,
  bothToolsWarnings,
  inventoryWith,
  MOVED,
  skillsCliNames,
  skillsCliNoneRun,
  skillsCliRun,
  skillsCliWarnings,
  skillshareNames,
  skillshareRun,
} from "./fixtures/adopt-all.ts";
import { fileReads, filesOf, history } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const unmanaged = (page: Page) =>
  list(page).getByRole("region", { name: /^UNMANAGED/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const settingsSection = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Hub" });
const dialog = (page: Page) => page.getByRole("dialog");
/** The rows of the dialog's list of `tool`, each read as "{state}: {name}" then any reason. */
const rows = (where: Locator, tool: string) =>
  where
    .getByRole("region", { name: "Skills" })
    .getByRole("region", { name: tool })
    .getByRole("listitem");
const progress = (where: Locator) => where.getByRole("status");
/** The skills the inventory lists as refused, folded under the list. */
const refused = (where: Locator) =>
  where.getByRole("region", { name: "Can't be imported" });

/**
 * The command lines the screens ran: everything but the launch and the pane's reads,
 * and the read of the store's size Settings runs as it shows.
 */
const ran = (cli: MockCli) =>
  cli.commands.filter(
    (l) =>
      !["version", "doctor", "machine"].includes(l[0] ?? "") &&
      !["skill files", "skill diff", "skill history"].includes(
        l.slice(0, 2).join(" ")
      )
  );

/** Registers the reads the Skills screen runs for the skills it may select. */
const registerReads = (cli: MockCli) => {
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  cli.register(["skill", "history", "release-notes"]);
  cli.register(["skill", "history", "draft"]);
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  for (const name of [...skillsCliNames, ...skillshareNames, "legacy"]) {
    cli.register(["skill", "files", name], { events: filesOf(name) });
  }
  cli.register(["skill", "diff", "review"]);
  cli.register(["skill", "diff", "release-notes"]);
  cli.register(["skill", "diff", "pdf-letters"]);
};

/** Opens the Skills screen on the machine with skills of both tools to adopt. */
const openSkills = async (cli: MockCli, page: Page, path = "/#/skills") => {
  cli.onServeStart({ events: [inventoryWith()] });
  registerReads(cli);
  await page.goto(path);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await expect(list(page).getByRole("region").first()).toBeVisible();
};

/** Opens Settings on that machine, with `managed` adopted already. */
const openSettings = async (
  cli: MockCli,
  page: Page,
  managed: string[] = []
) => {
  cli.onServeStart({ events: [inventoryWith(managed)] });
  await page.goto("/#/settings");
  await expect(settingsSection(page)).toBeVisible();
};

/** The inventory that follows a run that adopted `managed`, and the answer to its refresh. */
const afterRun = async (cli: MockCli, managed: string[]) => {
  await expect.poll(() => cli.requestIds.length).toBeGreaterThan(0);
  await cli.serveEmit([inventoryWith(managed)]);
  await cli.serveEmit([
    {
      ok: true,
      request_id: cli.requestIds.at(-1) ?? "",
      scan_counter: managed.length + 1,
      type: "refresh_complete",
    } satisfies InlineEvent,
  ]);
};

test.describe("Import skills in the Hub card of Settings", () => {
  test("has a button for each tool that has skills to adopt", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    await expect(
      settingsSection(page).getByRole("button", { name: /^Import from/u })
    ).toHaveText(["Import from Skills CLI", "Import from Skillshare"]);
  });

  test("shows only the tool that has skills to adopt", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page, skillshareNames);
    await expect(
      settingsSection(page).getByRole("button", { name: /^Import from/u })
    ).toHaveText(["Import from Skills CLI"]);
  });

  test("keeps its line when no tool has skills to adopt", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page, [...skillsCliNames, ...skillshareNames]);
    const section = settingsSection(page);
    await expect(section).toContainText(
      "Let agentx manage the skills another tool installed."
    );
    await expect(section).toContainText(
      "No skills from the Skills CLI or Skillshare to import"
    );
  });

  test("the dialog lists the tool's skills, the refused one with its reason, and Cancel closes it", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    await settingsSection(page)
      .getByRole("button", { name: "Import from Skills CLI" })
      .click();
    const open = dialog(page);
    await expect(open).toHaveAccessibleName("Import skills from Skills CLI");
    await expect(open.getByRole("paragraph")).toHaveText([
      "Manage the 3 skills you added with Skills CLI in agentx. Skills CLI isn't affected, but any custom edits to these skills in your library will be lost.",
    ]);
    await expect(rows(open, "Skills CLI")).toHaveText([
      "Waiting: commit-style",
      "Waiting: deck-outline",
      "Waiting: scratch",
    ]);
    // The refused skill is folded under the list, its reason shown once opened.
    const fold = refused(open).getByRole("button", {
      name: "1 skill can't be imported",
    });
    await expect(fold).toHaveAttribute("aria-expanded", "false");
    await expect(refused(open).getByRole("listitem")).toHaveCount(0);
    await fold.click();
    await expect(refused(open).getByRole("listitem")).toHaveText([
      "Can't be imported: legacyA symlink to /home/me/work/legacy. To import it, move that folder to /home/me/.agents/skills/legacy.",
    ]);
    await open.getByRole("button", { name: "Cancel" }).click();
    await expect(open).toHaveCount(0);
  });

  test("the dialog for one skill says so", async ({ cli, page }) => {
    await openSettings(cli, page, ["api-docs"]);
    await settingsSection(page)
      .getByRole("button", { name: "Import from Skillshare" })
      .click();
    const open = dialog(page);
    await expect(open).toHaveAccessibleDescription(
      "Manage the skill you added with Skillshare in agentx. Skillshare isn't affected, but any custom edits to this skill in your library will be lost."
    );
    await expect(rows(open, "Skillshare")).toHaveText([
      "Waiting: frontend__ui",
    ]);
  });

  test("Import runs in the dialog: each row's state, the progress, then each result", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    const run = cli.register(
      ["skill", "import", "--from", "skills-cli", "--all"],
      {
        events: skillsCliRun,
        exitCode: 5,
        // The fetch of the source, then deck-outline's first two steps: the fetch is no skill.
        holdAfter: 3,
        logs: skillsCliWarnings.map((message) => ({ level: "warn", message })),
      }
    );
    await settingsSection(page)
      .getByRole("button", { name: "Import from Skills CLI" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { exact: true, name: "Import" }).click();
    await run.started;
    await expect(progress(open)).toHaveText("Importing 1 of 3");
    await expect(rows(open, "Skills CLI")).toHaveText([
      "Waiting: commit-style",
      "Importing: deck-outline",
      "Waiting: scratch",
    ]);
    const adopt = open.getByRole("button", { exact: true, name: "Import" });
    await expect(adopt).toBeDisabled();
    await expect(adopt).toHaveAttribute("aria-busy", "true");
    // Only Cancel, which stops the run, closes it while it runs.
    await page.keyboard.press("Escape");
    await expect(open).toBeVisible();
    // The button behind it, hidden by the dialog, waits too.
    await expect(
      page
        .getByRole("region", {
          exact: true,
          includeHidden: true,
          name: "Hub",
        })
        .getByRole("button", {
          includeHidden: true,
          name: "Import from Skillshare",
        })
    ).toBeDisabled();

    run.release();
    await expect(progress(open)).toHaveText("Imported 2 of 3 skills");
    await expect(rows(open, "Skills CLI")).toHaveText([
      "Not imported: commit-stylehttps://github.com/vercel/skills holds no skill at commit-style now.",
      "Imported: deck-outline",
      "Imported: scratch",
    ]);
    // Each skill the run did not adopt says why once, under its name, as the warning the CLI
    // logged for it did.
    await expect(open.getByRole("paragraph")).toHaveText([
      /^Manage the 3 skills/u,
      "https://github.com/vercel/skills holds no skill at commit-style now.",
    ]);
    await open.getByRole("button", { name: "Done" }).click();
    await expect(open).toHaveCount(0);
    expect(ran(cli)).toEqual([
      ["skill", "import", "--from", "skills-cli", "--all", "--json"],
    ]);
  });

  test("Cancel stops the import that runs and closes the dialog", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    const run = cli.register(
      ["skill", "import", "--from", "skills-cli", "--all"],
      // The fetch alone: no skill has started.
      { events: skillsCliRun, holdAfter: 1 }
    );
    await settingsSection(page)
      .getByRole("button", { name: "Import from Skills CLI" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { exact: true, name: "Import" }).click();
    await run.started;
    await expect(progress(open)).toHaveText("Importing…");
    await open.getByRole("button", { name: "Cancel" }).click();
    await expect(open).toHaveCount(0);
    expect(cli.cancels).toHaveLength(1);
  });

  test("a tool's button goes once its skills are adopted", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    cli.register(["skill", "import", "--from", "skillshare", "--all"], {
      events: skillshareRun,
    });
    await settingsSection(page)
      .getByRole("button", { name: "Import from Skillshare" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { exact: true, name: "Import" }).click();
    await expect(progress(open)).toHaveText("Imported 2 skills");
    await expect(rows(open, "Skillshare")).toHaveText([
      "Imported: api-docs",
      "Imported: frontend__ui",
    ]);
    await open.getByRole("button", { name: "Done" }).click();
    await afterRun(cli, skillshareNames);
    await expect(
      settingsSection(page).getByRole("button", { name: /^Import from/u })
    ).toHaveText(["Import from Skills CLI"]);
  });

  test("a run that adopts none says so in the dialog", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    cli.register(["skill", "import", "--from", "skills-cli", "--all"], {
      events: skillsCliNoneRun,
      exitCode: 5,
    });
    await settingsSection(page)
      .getByRole("button", { name: "Import from Skills CLI" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { exact: true, name: "Import" }).click();
    await expect(progress(open)).toHaveText("No skills were imported");
    await expect(rows(open, "Skills CLI").first()).toHaveText(
      "Not imported: commit-stylehttps://github.com/vercel/skills holds no skill at commit-style now."
    );
  });

  test("a run that fails before any skill says why in the dialog and keeps the rows", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    cli.register(["skill", "import", "--from", "skillshare", "--all"], {
      events: [
        {
          hint: "fix the file, or move it aside",
          message: "the store of Skillshare is not JSON",
          type: "error",
        },
      ],
      exitCode: 10,
    });
    await settingsSection(page)
      .getByRole("button", { name: "Import from Skillshare" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { exact: true, name: "Import" }).click();
    await expect(open.getByRole("alert")).toHaveText(
      "The store of Skillshare is not JSON. Fix the file, or move it aside."
    );
    await expect(rows(open, "Skillshare")).toHaveText([
      "Waiting: api-docs",
      "Waiting: frontend__ui",
    ]);
    await open.getByRole("button", { name: "Done" }).click();
    await expect(open).toHaveCount(0);
  });
});

test.describe("Import all in the Unmanaged header", () => {
  test("covers both tools, grouped, and imports them in one run with one progress", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    const run = cli.register(
      [
        "skill",
        "import",
        "--from",
        "skills-cli",
        "--from",
        "skillshare",
        "--all",
      ],
      {
        events: bothToolsRun(),
        // Skillshare's source and its two skills, then the Skills CLI's source and two of its three.
        holdAfter: 6,
        logs: bothToolsWarnings().map((message) => ({
          level: "warn",
          message,
        })),
      }
    );
    await unmanaged(page).getByRole("button", { name: "Import all" }).click();
    const open = dialog(page);
    await expect(open).toHaveAccessibleName(
      "Import skills from Skills CLI and Skillshare"
    );
    await expect(open).toHaveAccessibleDescription(
      "Manage the 5 skills you added with Skills CLI and Skillshare in agentx. Skills CLI and Skillshare aren't affected, but any custom edits to these skills in your library will be lost."
    );
    await expect(open.getByRole("heading", { level: 3 })).toHaveText([
      "Skills CLI",
      "Skillshare",
    ]);
    await expect(rows(open, "Skillshare")).toHaveText([
      "Waiting: api-docs",
      "Waiting: frontend__ui",
    ]);
    await open.getByRole("button", { exact: true, name: "Import" }).click();
    await run.started;
    // One count over both tools, and each skill's step on the row of its tool.
    await expect(progress(open)).toHaveText("Importing 4 of 5");
    await expect(rows(open, "Skills CLI")).toHaveText([
      "Importing: commit-style",
      "Importing: deck-outline",
      "Waiting: scratch",
    ]);
    await expect(rows(open, "Skillshare")).toHaveText([
      "Importing: api-docs",
      "Importing: frontend__ui",
    ]);
    run.release();
    await expect(progress(open)).toHaveText("Imported 5 skills");
    await expect(rows(open, "Skills CLI")).toHaveText([
      "Imported: commit-style",
      "Imported: deck-outline",
      "Imported: scratch",
    ]);
    await expect(rows(open, "Skillshare")).toHaveText([
      "Imported: api-docs",
      "Imported: frontend__ui",
    ]);
    await expect(
      refused(open).getByRole("button", { name: "1 skill can't be imported" })
    ).toBeVisible();
    await open.getByRole("button", { name: "Done" }).click();
    await afterRun(cli, [...skillsCliNames, ...skillshareNames]);
    await expect(page.getByRole("button", { name: "Import all" })).toHaveCount(
      0
    );
    expect(ran(cli)).toEqual([
      [
        "skill",
        "import",
        "--from",
        "skills-cli",
        "--from",
        "skillshare",
        "--all",
        "--json",
      ],
    ]);
  });

  test("a skill the run could not adopt keeps its note and details on its own detail pane", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page, `/#/skills/${MOVED}`);
    cli.register(
      [
        "skill",
        "import",
        "--from",
        "skills-cli",
        "--from",
        "skillshare",
        "--all",
      ],
      {
        events: bothToolsRun([MOVED]),
        exitCode: 5,
        logs: bothToolsWarnings([MOVED]).map((message) => ({
          level: "warn",
          message,
        })),
      }
    );
    await unmanaged(page).getByRole("button", { name: "Import all" }).click();
    const open = dialog(page);
    await open.getByRole("button", { exact: true, name: "Import" }).click();
    await expect(progress(open)).toHaveText("Imported 4 of 5 skills");
    await open.getByRole("button", { name: "Done" }).click();
    await afterRun(cli, ["deck-outline", "scratch", ...skillshareNames]);
    const article = pane(page, MOVED);
    await expect(
      article.getByRole("note", { name: "Added by Skills CLI" })
    ).toBeVisible();
    const details = article.getByRole("region", { name: "Details" });
    await expect(details.getByRole("term")).toHaveText([
      "Found at",
      "Installed by",
    ]);
  });
});
