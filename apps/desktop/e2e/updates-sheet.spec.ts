// The What's new sheet: See changes on an update's banner opens it with what
// `skill diff <name> --update` read when the skill was selected, one block per changed file with
// its status, its path and the patch with the diff tints. A hub skill takes one update at a time:
// what another machine published comes before a newer version from upstream. Escape and Close
// close it.
import type { Page } from "@playwright/test";

import { EXIT_CODES } from "#/cli/events.gen.ts";

import {
  fileReads,
  history,
  releaseNotesDiffs,
  reviewDiffs,
} from "./fixtures/skills.ts";
import {
  lettersHubDiffs,
  lettersUpdateDiffs,
  pdfUpdateDiffs,
  updateDiffsOf,
} from "./fixtures/update-diffs.ts";
import { inventoryOf, updatesLibrary } from "./fixtures/updates.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli, Reply } from "./harness/cli.ts";

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

/**
 * Opens the Skills screen on `name`, with serve's first inventory offering every update of
 * `library`, and `diff` as what `skill diff <name> --update` answers. Returns that read.
 */
const open = async (
  cli: MockCli,
  page: Page,
  name: string,
  diff?: Reply,
  library = updatesLibrary
) => {
  cli.onServeStart({ events: [inventoryOf(library)] });
  const reading = cli.register(
    ["skill", "diff", name, "--update"],
    diff ?? { events: updateDiffsOf[name] }
  );
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
  return reading;
};

test.describe("the What's new sheet", () => {
  test("See changes records skill diff --update and shows one block per file with its patch", async ({
    cli,
    page,
  }) => {
    const read = await open(cli, page, "pdf", {
      events: pdfUpdateDiffs,
      holdAfter: 0,
    });
    await read.started;
    await seeChanges(page, "pdf").click();
    await expect(sheet(page)).toBeVisible();
    // It opens as wide as every sheet, the Activity log's 1120.
    const box = await sheet(page).boundingBox();
    expect(Math.round(box?.width ?? 0)).toBe(1120);
    await expect(sheet(page)).toContainText("pdf");
    // The two versions, by date where the CLI dates them.
    await expect(sheet(page)).toContainText("Sep 14 → Sep 28");
    // While it reads: the loading pane.
    await expect(sheet(page).getByText("Reading the changes…")).toBeAttached();
    read.release();

    await expect(block(page, "scripts/convert.py")).toBeVisible();
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
    // A change marks the text that differs, in its removed line and in its added one.
    await expect(patch(page, "SKILL.md").locator("del mark")).toHaveText([
      "and",
    ]);
    await expect(patch(page, "SKILL.md").locator("ins mark")).toHaveText([
      ",",
      "and convert",
    ]);

    // A block is as high as its lines: a short patch leaves no empty pane under them.
    const short = await patch(page, "scripts/convert.py").evaluate((el) => ({
      client: el.clientHeight,
      scroll: el.scrollHeight,
    }));
    expect(short.client).toBe(short.scroll);
    expect(short.client).toBeLessThan(100);

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

  test("lets a long patch take the sheet's height and scroll inside it", async ({
    cli,
    page,
  }) => {
    const lines = Array.from({ length: 80 }, (_, i) => `+Step ${i + 1}.`);
    await open(cli, page, "review", {
      events: [
        {
          name: "review",
          patch: [
            "diff --git a/SKILL.md b/SKILL.md",
            "--- a/SKILL.md",
            "+++ b/SKILL.md",
            "@@ -0,0 +1,80 @@",
            ...lines,
            "",
          ].join("\n"),
          path: "SKILL.md",
          status: "modified",
          type: "diff",
        },
      ],
    });
    await seeChanges(page, "review").click();
    const longPane = patch(page, "SKILL.md");
    await expect(longPane).toBeVisible();
    // The block ends at the sheet's bottom padding, and its lines scroll inside it.
    const gap = await sheet(page).evaluate(
      (el, paneEl) =>
        el.getBoundingClientRect().bottom -
        Number(getComputedStyle(el).borderBottomWidth.replace("px", "")) -
        Number(getComputedStyle(el).paddingBottom.replace("px", "")) -
        (paneEl?.getBoundingClientRect().bottom ?? 0),
      await longPane.elementHandle()
    );
    expect(Math.abs(gap)).toBeLessThan(2);
    const sizes = await longPane.evaluate((el) => ({
      client: el.clientHeight,
      scroll: el.scrollHeight,
    }));
    expect(sizes.scroll).toBeGreaterThan(sizes.client);
  });

  test("keeps an open block at least code.min-height high in a short window, and scrolls", async ({
    cli,
    page,
  }) => {
    await page.setViewportSize({ height: 420, width: 1280 });
    await open(cli, page, "pdf");
    await seeChanges(page, "pdf").click();
    await expect(patch(page, "scripts/convert.py")).toBeAttached();
    const box = await patch(page, "SKILL.md").boundingBox();
    expect(box?.height).toBe(200);
    await patch(page, "scripts/convert.py").scrollIntoViewIfNeeded();
    await expect(patch(page, "scripts/convert.py")).toBeInViewport();
  });

  test("shows a deleted file of an update from the original, by short ids", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf-letters");
    await seeChanges(page, "pdf-letters").click();
    await expect(block(page, "examples.md")).toContainText("deleted");
    await expect(sheet(page)).toContainText("3f2a9c1 → 9b8c7d6");
    await expect(
      patch(page, "examples.md").locator("del", { hasText: "# Examples" })
    ).toBeVisible();
    expect(updateDiffs(cli)).toEqual([
      ["skill", "diff", "pdf-letters", "--update", "--json"],
    ]);
  });

  test("shows what another machine published to the hub, with its patch", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "release-notes");
    await seeChanges(page, "release-notes").click();
    await expect(sheet(page)).toBeVisible();
    await expect(sheet(page)).toContainText("release-notes");
    await expect(sheet(page)).toContainText("Oct 1");
    await expect(block(page, "template.md")).toContainText("added");
    await expect(
      patch(page, "template.md").locator("ins", { hasText: "# Release notes" })
    ).toBeVisible();
    expect(updateDiffs(cli)).toEqual([
      ["skill", "diff", "release-notes", "--update", "--json"],
    ]);
    await page.keyboard.press("Escape");
    await expect(sheet(page)).toHaveCount(0);
  });

  test("a fork behind its hub with a newer upstream: only what the hub holds, which its update takes in first", async ({
    cli,
    page,
  }) => {
    const library = updatesLibrary.map((entry) =>
      entry.name === "pdf-letters"
        ? { ...entry, hub_ahead: { commit: "77e1d0c0ffee" } }
        : entry
    );
    await open(cli, page, "pdf-letters", { events: lettersHubDiffs }, library);
    await seeChanges(page, "pdf-letters").click();
    // One update, under no caption: the hub's version, by its short id.
    await expect(sheet(page)).toContainText("77e1d0c");
    await expect(block(page, "template.md")).toContainText("added");
    await expect(block(page, "examples.md")).toHaveCount(0);
    await expect(sheet(page)).not.toContainText("New version from");
    await expect(sheet(page)).not.toContainText(
      "Published from another machine"
    );
  });

  test("reads the changes again when a hub update moved the base of an update from upstream", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "pdf-letters");
    await seeChanges(page, "pdf-letters").click();
    await expect(block(page, "examples.md")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(sheet(page)).toHaveCount(0);

    // The hub update took in a newer version of the original, and the same candidate stays: its
    // update now changes SKILL.md alone.
    cli.register(["skill", "diff", "pdf-letters", "--update"], {
      events: lettersUpdateDiffs.slice(0, 1),
    });
    const moved = updatesLibrary.map((entry) =>
      entry.name === "pdf-letters"
        ? {
            ...entry,
            base_hash: "h-pdf-between",
            upstream_commit: "5e4d3c2b1a0f9",
          }
        : entry
    );
    await cli.serveEmit([{ ...inventoryOf(moved), scan_counter: 2 }]);
    await seeChanges(page, "pdf-letters").click();
    await expect(block(page, "SKILL.md")).toBeVisible();
    await expect(block(page, "examples.md")).toHaveCount(0);
    expect(updateDiffs(cli)).toHaveLength(2);
  });

  test("says when the changes could not be read, in the sheet", async ({
    cli,
    page,
  }) => {
    await open(cli, page, "review", {
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
  });
});
