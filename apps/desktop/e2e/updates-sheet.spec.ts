// The What's new sheet: See changes on an update's banner opens it, it reads
// `skill diff <name> --update` and shows one block per changed file with its status, its path and
// the patch with the diff tints. An update from another machine lists its files with a note and
// runs no diff. Escape and Close close it.
import type { Page } from "@playwright/test";

import { EXIT_CODES } from "#/cli/events.gen.ts";

import {
  fileReads,
  history,
  releaseNotesDiffs,
  reviewDiffs,
} from "./fixtures/skills.ts";
import { lettersUpdateDiffs, pdfUpdateDiffs } from "./fixtures/update-diffs.ts";
import { allUpdates, updatesInventory } from "./fixtures/updates.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

/** The reads of what an update changes, in the order the sheet ran them. */
const updateDiffs = (cli: MockCli) =>
  cli.commands.filter((l) => l[1] === "diff" && l.includes("--update"));

const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const seeChanges = (page: Page, name: string) =>
  pane(page, name).getByRole("button", { exact: true, name: "See changes" });
const sheet = (page: Page) => page.getByRole("dialog", { name: "What's new" });
/** The row that opens or closes one file's block. */
const block = (page: Page, path: string) =>
  sheet(page).getByRole("button", { exact: true, name: path });
/** The patch of one file, in the Code pane. */
const patch = (page: Page, path: string) =>
  sheet(page).getByRole("region", { exact: true, name: `Changes to ${path}` });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });

/** Opens the Skills screen on `name`, with serve's start check having found every update. */
const open = async (cli: MockCli, page: Page, name: string) => {
  cli.onServeStart({ events: [updatesInventory, ...allUpdates] });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  cli.register(["skill", "history", "release-notes"], { events: [] });
  cli.register(["skill", "history", "draft"], { events: [] });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  cli.register(["skill", "diff", "review"], { events: reviewDiffs });
  cli.register(["skill", "diff", "release-notes"], {
    events: releaseNotesDiffs,
  });
  cli.register(["skill", "diff", "pdf-letters"], { events: [] });
  await page.goto(`/#/skills/${encodeURIComponent(name)}`);
  await expect(pane(page, name)).toBeVisible();
};

test.describe("the What's new sheet", () => {
  test("See changes records skill diff --update and shows one block per file with its patch", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf");
    const read = cli.register(["skill", "diff", "pdf", "--update"], {
      events: pdfUpdateDiffs,
      holdAfter: 0,
    });
    await seeChanges(page, "pdf").click();
    await read.started;
    await expect(sheet(page)).toBeVisible();
    await expect(sheet(page)).toContainText("pdf");
    // The two versions, by date where the CLI dates them.
    await expect(sheet(page)).toContainText("Sep 14 → Sep 28");
    await expect(sheet(page)).toContainText("Reading the changes…");
    read.release();

    await expect(block(page, "SKILL.md")).toBeVisible();
    expect(updateDiffs(cli)).toEqual([
      ["skill", "diff", "pdf", "--update", "--json"],
    ]);
    // One block per file, in the CLI's order, each with its status, every one open.
    const rows = await sheet(page)
      .getByRole("button", { expanded: true })
      .evaluateAll((buttons) =>
        buttons.map((b) => b.getAttribute("aria-label"))
      );
    expect(rows).toEqual(["SKILL.md", "scripts/convert.py"]);
    await expect(block(page, "SKILL.md")).toContainText("modified");
    await expect(block(page, "scripts/convert.py")).toContainText("added");
    // The patch with its tints: the removed line struck out, the added lines inserted.
    await expect(
      patch(page, "SKILL.md").locator("del", {
        hasText: "description: Fill and merge PDF forms.",
      })
    ).toBeVisible();
    await expect(
      patch(page, "SKILL.md").locator("ins", {
        hasText: "Run scripts/convert.py to turn a form into text.",
      })
    ).toBeVisible();
    await expect(
      patch(page, "scripts/convert.py").locator("ins", {
        hasText: "import sys",
      })
    ).toBeVisible();

    // A block closes and opens again.
    await block(page, "SKILL.md").click();
    await expect(patch(page, "SKILL.md")).toHaveCount(0);
    await expect(block(page, "SKILL.md")).toHaveAttribute(
      "aria-expanded",
      "false"
    );
    await block(page, "SKILL.md").click();
    await expect(patch(page, "SKILL.md")).toBeVisible();

    // Escape closes it; opened again, it shows the same read without running it again.
    await page.keyboard.press("Escape");
    await expect(sheet(page)).toHaveCount(0);
    await seeChanges(page, "pdf").click();
    await expect(patch(page, "scripts/convert.py")).toBeVisible();
    expect(updateDiffs(cli)).toHaveLength(1);
    // Close closes it too.
    await sheet(page)
      .getByRole("button", { exact: true, name: "Close" })
      .click();
    await expect(sheet(page)).toHaveCount(0);
  });

  test("shows a deleted file of an update from the original, by short ids", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf-letters");
    cli.register(["skill", "diff", "pdf-letters", "--update"], {
      events: lettersUpdateDiffs,
    });
    await seeChanges(page, "pdf-letters").click();
    await expect(block(page, "examples.md")).toContainText("deleted");
    await expect(sheet(page)).toContainText("1a2b3c4 → 9b8c7d6");
    await expect(
      patch(page, "examples.md").locator("del", { hasText: "# Examples" })
    ).toBeVisible();
    expect(updateDiffs(cli)).toEqual([
      ["skill", "diff", "pdf-letters", "--update", "--json"],
    ]);
  });

  test("lists the files of an update from another machine with the note, and runs no diff", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "release-notes");
    await seeChanges(page, "release-notes").click();
    await expect(sheet(page)).toBeVisible();
    await expect(sheet(page)).toContainText("release-notes");
    await expect(sheet(page)).toContainText("Oct 1");
    await expect(sheet(page)).toContainText(
      "agentx shows these changes once you update"
    );
    const files = sheet(page).getByRole("list", { name: "Changed files" });
    await expect(files.getByRole("listitem")).toHaveText(["addedtemplate.md"]);
    await expect(
      sheet(page).getByRole("button", { expanded: true })
    ).toHaveCount(0);
    expect(updateDiffs(cli)).toEqual([]);
    await page.keyboard.press("Escape");
    await expect(sheet(page)).toHaveCount(0);
  });

  test("says when the changes could not be read, in the sheet", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "review");
    cli.register(["skill", "diff", "review", "--update"], {
      events: [
        {
          code: "refused",
          hint: "run agentx skill check-updates",
          message: "no update of review is known",
          type: "error",
        },
      ],
      exitCode: EXIT_CODES.refused,
    });
    await seeChanges(page, "review").click();
    await expect(sheet(page)).toContainText("The changes could not be read");
    await expect(sheet(page)).toContainText("no update of review is known");
    // The sheet says it; no error toast repeats it.
    await expect(toasts(page)).not.toContainText("no update of review");
  });
});
