// A read-only hub: Publish and Publish all look disabled, do nothing and say why in a Tooltip;
// the palette offers no Publish all; Rename stays, since a rename changes only this machine.
import type { Page } from "@playwright/test";

import { hub, launch } from "./fixtures/palette-publish.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const READ_ONLY = "me/hub is read-only";

const readOnlyHub = (cli: MockCli) => {
  launch(cli, "edited", { hub: { ...hub, access: "read-only" } });
};

const status = (page: Page) =>
  page.getByRole("banner").getByRole("button", { name: /to publish/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const published = (cli: MockCli) =>
  cli.commands.filter((l) => l[0] === "skill" && l[1] === "publish");

test("Publish all in the title bar says the hub is read-only and publishes nothing", async ({
  cli,
  page,
}) => {
  readOnlyHub(cli);
  await page.goto("/");
  await expect(status(page)).toHaveText("3 to publish");
  await status(page).click();
  const publishAll = page
    .getByRole("dialog", { name: "Sync" })
    .getByRole("button", { name: "Publish all" });
  await expect(publishAll).toHaveAttribute("aria-disabled", "true");
  await publishAll.hover();
  await expect(page.getByRole("tooltip")).toHaveText(READ_ONLY);
  await publishAll.click({ force: true });
  expect(published(cli)).toEqual([]);
});

test("a hub skill's Publish says the hub is read-only; Rename stays", async ({
  cli,
  page,
}) => {
  readOnlyHub(cli);
  await page.goto("/#/skills/changelog");
  const detail = pane(page, "changelog");
  const publish = detail.getByRole("button", { exact: true, name: "Publish" });
  await expect(publish).toHaveAttribute("aria-disabled", "true");
  await publish.hover();
  await expect(page.getByRole("tooltip")).toHaveText(READ_ONLY);
  await publish.click({ force: true });
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    detail.getByRole("button", { exact: true, name: "Rename" })
  ).not.toHaveAttribute("aria-disabled", "true");
});

test("the palette offers no Publish all", async ({ cli, page }) => {
  readOnlyHub(cli);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await expect(status(page)).toHaveText("3 to publish");
  await page.keyboard.press("Control+K");
  const palette = page.getByRole("dialog");
  await expect(palette).toBeVisible();
  await expect(
    palette.getByRole("option", { name: /Publish all/u })
  ).toHaveCount(0);
});
