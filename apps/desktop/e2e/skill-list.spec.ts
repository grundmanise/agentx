// The Skills list on a machine with hundreds of skills: only the rows near the part of the list
// that shows are in the document, the selected row comes into view, and each plugin's skills sit
// in a subgroup that starts collapsed.
import type { Page } from "@playwright/test";

import {
  fileReads,
  inventory,
  listedPluginSkill,
  PLUGIN_SKILL_COUNT,
  SKILL_COUNT,
  SKILLS_PER_PLUGIN,
} from "./fixtures/many-skills.ts";
import { adoption } from "./fixtures/skills.ts";
import type { MockCli } from "./harness/cli.ts";
import { expect, test } from "./harness/cli.ts";

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });

/** Opens the Skills screen on the machine with hundreds of skills, at the skill `name`. */
const openAt = async (cli: MockCli, page: Page, name: string) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  // The route names a skill far down the list, as when another screen opens it.
  await page.goto(`/#/skills/${encodeURIComponent(`skill:h-${name}`)}`);
  await expect(page.getByLabel(`${SKILL_COUNT} skills`)).toBeVisible();
};

test("a list of hundreds of skills holds only the rows near what shows, and brings the selected row into view", async ({
  cli,
  page,
}) => {
  const start = listedPluginSkill(300);
  await openAt(cli, page, start.name);
  const opened = row(page, start.name);
  await expect(opened).toHaveAttribute("aria-current", "true");
  await expect(opened).toBeInViewport({ ratio: 1 });
  // The group and each plugin's subgroup say how many rows they have, and each row its place
  // among its plugin's.
  await expect(
    list(page).getByRole("region", { name: `PLUGINS (${PLUGIN_SKILL_COUNT})` })
  ).toBeVisible();
  await expect(
    list(page)
      .getByRole("region", {
        name: `${start.plugin} (${SKILLS_PER_PLUGIN})`,
      })
      .getByRole("listitem")
      .filter({
        has: page.getByRole("button", { exact: true, name: start.name }),
      })
  ).toHaveAttribute("aria-posinset", String(start.posinset));
  const drawn = await list(page).getByRole("listitem").count();
  expect(drawn).toBeLessThan(50);

  // Every plugin's subgroup opened, from the PLUGINS group's header.
  await list(page).evaluate((nav) => {
    nav.scrollTop = 0;
  });
  await list(page).getByRole("button", { name: "Collapse all" }).click();
  await list(page).getByRole("button", { name: "Expand all" }).click();

  // The keys move the selection past the rows that were drawn and from one plugin to the next,
  // one key after the other, and the list follows.
  for (let i = 301; i <= 320; i += 1) {
    // oxlint-disable-next-line no-await-in-loop
    await page.keyboard.press("j");
    // oxlint-disable-next-line no-await-in-loop
    await expect(row(page, listedPluginSkill(i).name)).toHaveAttribute(
      "aria-current",
      "true"
    );
  }
  const last = listedPluginSkill(320).name;
  await expect(row(page, last)).toBeInViewport({ ratio: 1 });
  await expect(pane(page, last)).toBeVisible();
  expect(await list(page).getByRole("listitem").count()).toBeLessThan(50);
});

test("plugins' subgroups start collapsed, but the opened skill's; Collapse all and Expand all", async ({
  cli,
  page,
}) => {
  // The first generated plugin's first skill, so its header is drawn beside it.
  const first = listedPluginSkill(0);
  await openAt(cli, page, first.name);
  const plugin = list(page).getByRole("region", {
    name: `${first.plugin} (${SKILLS_PER_PLUGIN})`,
  });
  const toggle = (name: string, count = SKILLS_PER_PLUGIN) =>
    list(page).getByRole("button", { exact: true, name: `${name} ${count}` });
  await expect(toggle(first.plugin)).toHaveAttribute("aria-expanded", "true");
  await expect(toggle("ACME-TOOLS", 1)).toHaveAttribute(
    "aria-expanded",
    "false"
  );

  // Collapse all collapses every subgroup to its header; the detail pane keeps the selected skill.
  await list(page).getByRole("button", { name: "Collapse all" }).click();
  await expect(toggle(first.plugin)).toHaveAttribute("aria-expanded", "false");
  await expect(plugin.getByRole("listitem")).toHaveCount(0);
  await expect(pane(page, first.name)).toBeVisible();

  // Expand all opens every one.
  await list(page).getByRole("button", { name: "Expand all" }).click();
  await expect(toggle("ACME-TOOLS", 1)).toHaveAttribute(
    "aria-expanded",
    "true"
  );
  await expect(toggle(first.plugin)).toHaveAttribute("aria-expanded", "true");

  // A collapsed subgroup's rows are skipped by the keys.
  await toggle(first.plugin).click();
  await expect(toggle(first.plugin)).toHaveAttribute("aria-expanded", "false");
  const next = listedPluginSkill(SKILLS_PER_PLUGIN).name;
  await page.keyboard.press("j");
  await expect(row(page, next)).toHaveAttribute("aria-current", "true");
  await toggle(first.plugin).click();
  await expect(row(page, first.name)).toBeVisible();
});

test("while the list scrolls, the header of its group and of its plugin stay pinned at the top", async ({
  cli,
  page,
}) => {
  const start = listedPluginSkill(300);
  await openAt(cli, page, start.name);
  const listBox = await list(page).boundingBox();
  const top = listBox?.y ?? 0;
  const group = list(page).getByText("PLUGINS", { exact: true });
  const header = (plugin: string) =>
    list(page).getByRole("button", {
      exact: true,
      name: `${plugin} ${SKILLS_PER_PLUGIN}`,
    });
  /** How far below the top of the list an element is, in px. */
  const below = async (element: ReturnType<typeof header>) => {
    const box = await element.boundingBox();
    return (box?.y ?? 0) - top;
  };
  // Far into PLUGINS, its header sits at the top of the list and the plugin's header under it.
  expect(await below(group)).toBeGreaterThanOrEqual(0);
  expect(await below(group)).toBeLessThan(20);
  expect(await below(header(start.plugin))).toBeGreaterThan(20);
  expect(await below(header(start.plugin))).toBeLessThan(40);
  // The selected row comes into view below them.
  const opened = await row(page, start.name).boundingBox();
  expect((opened?.y ?? 0) - top).toBeGreaterThanOrEqual(50);

  // Moved to the last skill of its plugin, then into the next plugin, opened from its header,
  // and scrolled into it, the next plugin's header takes the pinned place.
  const firstOfNext = Math.ceil(301 / SKILLS_PER_PLUGIN) * SKILLS_PER_PLUGIN;
  const next = listedPluginSkill(firstOfNext).plugin;
  for (let i = 300; i < firstOfNext - 1; i += 1) {
    // oxlint-disable-next-line no-await-in-loop
    await page.keyboard.press("j");
  }
  await expect(
    row(page, listedPluginSkill(firstOfNext - 1).name)
  ).toHaveAttribute("aria-current", "true");
  await header(next).click();
  await page.keyboard.press("j");
  await expect(row(page, listedPluginSkill(firstOfNext).name)).toHaveAttribute(
    "aria-current",
    "true"
  );
  // Scrolled past its header, by more than the header's distance from the top.
  const distance = await below(header(next));
  await list(page).evaluate((nav, by) => {
    nav.scrollBy(0, by);
  }, distance + 100);
  await expect.poll(async () => (await below(header(next))) < 40).toBe(true);
  expect(await below(group)).toBeGreaterThanOrEqual(0);
  expect(await below(group)).toBeLessThan(20);
  expect(await below(header(next))).toBeGreaterThan(20);

  // A click on the pinned plugin's header collapses it, and the plugin stays at the top.
  await header(next).click();
  await expect(header(next)).toHaveAttribute("aria-expanded", "false");
  await expect(header(next)).toBeInViewport();
  expect(await below(header(next))).toBeLessThan(40);
});
