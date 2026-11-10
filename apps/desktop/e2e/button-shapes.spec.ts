// Every button has a resting shape: a ghost and a danger button have a fill before any hover, and
// the two halves of a split button make one shape, rounded on its outer corners only.
import type { Locator } from "@playwright/test";

import {
  fileReads,
  history,
  releaseNotesDiffs,
  reviewDiffs,
} from "./fixtures/skills.ts";
import { allUpdates, updatesInventory } from "./fixtures/updates.ts";
import { expect, test } from "./harness/cli.ts";
import { moreAddActions, skillsHeader } from "./harness/skills-header.ts";

const TRANSPARENT = "rgba(0, 0, 0, 0)";

/** A computed radius, such as "8px" or "0px", in px. */
const px = (value: string) => Number(value.replace("px", ""));

/** The resting background and the four corner radii of a control, in px. */
const shapeOf = async (control: Locator) => {
  const raw = await control.evaluate((el) => {
    const style = getComputedStyle(el);
    const box = el.getBoundingClientRect();
    return {
      background: style.backgroundColor,
      bottomLeft: style.borderBottomLeftRadius,
      bottomRight: style.borderBottomRightRadius,
      height: box.height,
      left: box.left,
      right: box.right,
      topLeft: style.borderTopLeftRadius,
      topRight: style.borderTopRightRadius,
    };
  });
  return {
    ...raw,
    bottomLeft: px(raw.bottomLeft),
    bottomRight: px(raw.bottomRight),
    topLeft: px(raw.topLeft),
    topRight: px(raw.topRight),
  };
};

test("ghost and danger buttons have a resting fill; the split button's halves make one shape", async ({
  cli,
  page,
}) => {
  cli.onServeStart({ events: [updatesInventory, ...allUpdates] });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  cli.register(["skill", "diff", "review"], { events: reviewDiffs });
  cli.register(["skill", "diff", "release-notes"], {
    events: releaseNotesDiffs,
  });
  await page.goto("/#/skills/pdf");
  const article = page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name: "pdf" });
  await expect(article).toBeVisible();
  // Away from every control, so nothing shows its hover fill.
  await page.mouse.move(1, 1);

  const ghost = skillsHeader(page).getByRole("button", {
    exact: true,
    name: "Check updates",
  });
  const danger = article
    .getByLabel("Actions", { exact: true })
    .getByRole("button", { exact: true, name: "Remove" });
  const [ghostShape, dangerShape] = await Promise.all([
    shapeOf(ghost),
    shapeOf(danger),
  ]);
  expect(ghostShape.background).not.toBe(TRANSPARENT);
  expect(dangerShape.background).not.toBe(TRANSPARENT);
  expect(dangerShape.background).not.toBe(ghostShape.background);

  const action = skillsHeader(page).getByRole("button", {
    exact: true,
    name: "Add",
  });
  const [main, menu] = await Promise.all([
    shapeOf(action),
    shapeOf(moreAddActions(page)),
  ]);
  // One fill, one height, no gap between the halves.
  expect(main.background).toBe(ghostShape.background);
  expect(menu.background).toBe(main.background);
  expect(menu.height).toBe(main.height);
  expect(menu.left).toBeCloseTo(main.right, 0);
  // Rounded on the outer corners only.
  expect(main.topLeft).toBeGreaterThan(0);
  expect(main.bottomLeft).toBeGreaterThan(0);
  expect([main.topRight, main.bottomRight]).toEqual([0, 0]);
  expect(menu.topRight).toBeGreaterThan(0);
  expect(menu.bottomRight).toBeGreaterThan(0);
  expect([menu.topLeft, menu.bottomLeft]).toEqual([0, 0]);
});
