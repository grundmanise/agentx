// The Skills list on a machine with hundreds of skills: only the rows near the part of the list
// that shows are in the document, and the selected row comes into view.
import type { Page } from "@playwright/test";

import {
  fileReads,
  inventory,
  PLUGIN_SKILL_COUNT,
  pluginSkillName,
  SKILL_COUNT,
} from "./fixtures/many-skills.ts";
import { adoption } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });

test("a list of hundreds of skills holds only the rows near what shows, and brings the selected row into view", async ({
  cli,
  page,
}) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  // The route names a skill far down the list, as when another screen opens it.
  await page.goto(
    `/#/skills/${encodeURIComponent(`skill:h-${pluginSkillName(400)}`)}`
  );
  await expect(page.getByLabel(`${SKILL_COUNT} skills`)).toBeVisible();
  const opened = row(page, pluginSkillName(400));
  await expect(opened).toHaveAttribute("aria-current", "true");
  await expect(opened).toBeInViewport({ ratio: 1 });
  // The group says how many rows it has, and each row its place among them.
  const plugins = list(page).getByRole("region", {
    name: `PLUGINS (${PLUGIN_SKILL_COUNT})`,
  });
  await expect(
    plugins.getByRole("listitem").filter({
      has: page.getByRole("button", {
        exact: true,
        name: pluginSkillName(400),
      }),
    })
  ).toHaveAttribute("aria-posinset", "401");
  const drawn = await list(page).getByRole("listitem").count();
  expect(drawn).toBeLessThan(50);

  // The keys move the selection past the rows that were drawn, one key after the other, and
  // the list follows.
  for (let n = 401; n <= 420; n += 1) {
    // oxlint-disable-next-line no-await-in-loop
    await page.keyboard.press("j");
    // oxlint-disable-next-line no-await-in-loop
    await expect(row(page, pluginSkillName(n))).toHaveAttribute(
      "aria-current",
      "true"
    );
  }
  await expect(row(page, pluginSkillName(420))).toBeInViewport({ ratio: 1 });
  await expect(pane(page, pluginSkillName(420))).toBeVisible();
  expect(await list(page).getByRole("listitem").count()).toBeLessThan(50);
});
