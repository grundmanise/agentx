// The History sheet of one of your own skills: each entry expands to the files its commit changed
// and collapses again.
import type { Page } from "@playwright/test";

import { adoption, fileReads, history, inventory } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const SUBJECT = "pdf-letters: edit SKILL.md and add examples.md (studio-mac)";

const sheet = (page: Page) => page.getByRole("dialog", { name: "History" });
const entryButton = (page: Page) =>
  sheet(page).getByRole("button", { exact: true, name: SUBJECT });
const filesOf = (page: Page) =>
  sheet(page).getByRole("list", { name: `Files of ${SUBJECT}` });
/** The command lines run, but the pane's own reads of the skill's history and files. */
const others = (cli: MockCli) =>
  cli.commands.filter(
    (l) => !(l[0] === "skill" && (l[1] === "history" || l[1] === "files"))
  );

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

test("an entry expands to its files and collapses again", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const ran = others(cli);
  const button = entryButton(page);
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(filesOf(page)).toHaveCount(0);

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect(filesOf(page).getByRole("listitem")).toHaveText([
    "modifiedSKILL.md",
    "addedexamples.md",
  ]);
  // Only the open entry shows its files.
  await expect(
    sheet(page).getByRole("list", { name: /^Files of /u })
  ).toHaveCount(1);

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(filesOf(page)).toHaveCount(0);
  // The files come with the history: expanding an entry runs nothing.
  expect(others(cli)).toEqual(ran);
});
