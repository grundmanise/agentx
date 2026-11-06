// The History sheet of one of your own skills: each entry expands to the files its commit changed
// and collapses again, and Compare with now runs `skill diff <name> --commit <commit>` and shows
// how the skill as it is now differs from that commit, one block per file with the diff tints.
import type { Locator, Page } from "@playwright/test";

import { comparedCommit, comparedDiffs } from "./fixtures/history-compare.ts";
import { adoption, fileReads, history, inventory } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const SUBJECT = "pdf-letters: edit SKILL.md and add examples.md (studio-mac)";
const COMPARE = ["skill", "diff", "pdf-letters", "--commit", comparedCommit];

const sheet = (page: Page) => page.getByRole("dialog", { name: "History" });
const entryButton = (page: Page) =>
  sheet(page).getByRole("button", { exact: true, name: SUBJECT });
/** The sheet's entry of the compared commit. */
const entry = (page: Page) =>
  sheet(page)
    .getByRole("list", { name: "All history" })
    .getByRole("listitem")
    // The `has` locator is matched inside each item, so it names the button alone.
    .filter({
      has: page.getByRole("button", { exact: true, name: SUBJECT }),
    });
const compareButton = (where: Locator) =>
  where.getByRole("button", { exact: true, name: "Compare with now" });
const comparison = (where: Locator) =>
  where.getByRole("region", { name: `${SUBJECT} compared with now` });
/** The command lines of Compare with now, the only `skill diff --commit` the app runs. */
const compares = (cli: MockCli) =>
  cli.commands.filter((l) => l.includes("--commit"));

/** Opens pdf-letters, one of your own skills, and its History sheet. */
const openSheet = async (cli: MockCli, page: Page) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  await page.goto("/#/skills/pdf-letters");
  await page
    .getByRole("region", { name: "Skill details" })
    .getByRole("button", { name: "Show all 5" })
    .click();
  await expect(sheet(page)).toBeVisible();
};

test("an entry expands to its files and Compare with now, and collapses again", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const button = entryButton(page);
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(compareButton(sheet(page))).toHaveCount(0);

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect(
    entry(page)
      .getByRole("list", { name: `Files of ${SUBJECT}` })
      .getByRole("listitem")
  ).toHaveText(["modifiedSKILL.md", "addedexamples.md"]);
  // Only the open entry offers the comparison.
  await expect(compareButton(sheet(page))).toHaveCount(1);
  await expect(compareButton(entry(page))).toBeVisible();

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(
    entry(page).getByRole("list", { name: `Files of ${SUBJECT}` })
  ).toHaveCount(0);
  await expect(compareButton(sheet(page))).toHaveCount(0);
  // Expanding runs nothing.
  expect(compares(cli)).toEqual([]);
});

test("Compare with now runs skill diff --commit and shows one block per file", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const held = cli.register(COMPARE, { events: comparedDiffs, holdAfter: 0 });
  await entryButton(page).click();
  await compareButton(entry(page)).click();
  await held.started;
  // While it runs the button waits.
  await expect(compareButton(entry(page))).toBeDisabled();
  held.release();

  const result = comparison(entry(page));
  await expect(result).toBeVisible();
  expect(compares(cli)).toEqual([[...COMPARE, "--json"]]);
  await expect(compareButton(entry(page))).toHaveCount(0);
  await expect(result).toContainText("3 files changed since this version");

  const blocks = result.getByRole("listitem");
  await expect(blocks).toHaveCount(3);
  await expect(blocks.nth(0)).toContainText("modifiedSKILL.md");
  await expect(blocks.nth(1)).toContainText("deletedexamples.md");
  await expect(blocks.nth(2)).toContainText("addedletter.md");

  // Each patch shows with the diff tints: its lines, the removed ones and the added ones.
  const skillMd = result.getByRole("region", { name: "Changes to SKILL.md" });
  await expect(skillMd.getByRole("deletion")).toHaveText([
    "Read the PDF first.",
  ]);
  await expect(skillMd.getByRole("insertion")).toHaveText([
    "Read the PDF, then draft the letter.",
  ]);
  await expect(skillMd).toContainText("# pdf-letters");
  await expect(skillMd).toContainText("Keep every letter short.");
  // Numbered in the file as it is now, from the hunk's start; a removed line has none.
  const numbers = await skillMd
    .locator(":scope > div > span:first-child")
    .allTextContents();
  expect(numbers).toEqual(["6", "", "7", "8"]);

  const examples = result.getByRole("region", {
    name: "Changes to examples.md",
  });
  await expect(examples.getByRole("deletion")).toHaveText([
    "# Examples",
    "A letter to the bank.",
  ]);
  await expect(examples.getByRole("insertion")).toHaveCount(0);
  const letter = result.getByRole("region", { name: "Changes to letter.md" });
  await expect(letter.getByRole("insertion")).toHaveText(["Dear {name},"]);
  await expect(letter.getByRole("deletion")).toHaveCount(0);

  // The comparison stays with its entry: collapsed and expanded again, it shows without a run.
  await entryButton(page).click();
  await expect(comparison(entry(page))).toHaveCount(0);
  await entryButton(page).click();
  await expect(comparison(entry(page))).toBeVisible();
  expect(compares(cli)).toHaveLength(1);
});

test("a commit that matches the skill as it is now says so", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  cli.register(COMPARE, { events: [] });
  await entryButton(page).click();
  await compareButton(entry(page)).click();
  const result = comparison(entry(page));
  await expect(result).toHaveText("Matches the skill as it is now");
  await expect(result.getByRole("listitem")).toHaveCount(0);
  expect(compares(cli)).toEqual([[...COMPARE, "--json"]]);
});

test("a failed comparison shows the CLI's error under the entry and runs again on Retry", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  cli.register(COMPARE, {
    events: [
      {
        code: "refused",
        hint: "Run agentx skill history pdf-letters to see its commits",
        message:
          "commit 4c2e9a1 holds no skill directory pdf-letters to compare pdf-letters with",
        type: "error",
      },
    ],
    exitCode: 6,
  });
  await entryButton(page).click();
  await compareButton(entry(page)).click();

  const alert = entry(page).getByRole("alert");
  await expect(alert).toContainText(
    "commit 4c2e9a1 holds no skill directory pdf-letters to compare pdf-letters with"
  );
  await expect(alert).toContainText(
    "Run agentx skill history pdf-letters to see its commits"
  );
  await expect(comparison(entry(page))).toHaveCount(0);
  // The entry shows the failure itself: no error toast.
  await expect(page.locator("[data-sonner-toast]")).toHaveCount(0);

  cli.register(COMPARE, { events: comparedDiffs });
  await entry(page).getByRole("button", { exact: true, name: "Retry" }).click();
  await expect(comparison(entry(page)).getByRole("listitem")).toHaveCount(3);
  await expect(entry(page).getByRole("alert")).toHaveCount(0);
  expect(compares(cli)).toEqual([
    [...COMPARE, "--json"],
    [...COMPARE, "--json"],
  ]);
});
