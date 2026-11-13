import type { Page } from "@playwright/test";

import { expect, test } from "./harness/cli.ts";

const main = (page: Page) => page.getByRole("navigation", { name: "Main" });

test("opens on Skills and lists the screens in sidebar order", async ({
  page,
}) => {
  await page.goto("/");
  const nav = main(page);
  await expect(nav.getByRole("link")).toHaveText([
    "Skills",
    "Sources",
    "Agents",
    "MCP servers",
    "Plugins",
    "Settings",
  ]);
  await expect(nav.getByRole("link", { name: "Skills" })).toHaveAttribute(
    "aria-current",
    "page"
  );
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();

  // Each link's icon is an inline SVG, hidden from screen readers and from the tab order.
  const icons = nav.locator("svg");
  await expect(icons).toHaveCount(6);
  expect(
    await icons.evaluateAll((svgs) =>
      svgs.every(
        (svg) =>
          svg.getAttribute("aria-hidden") === "true" &&
          svg.getAttribute("focusable") === "false"
      )
    )
  ).toBe(true);
});

test("moves between screens from the sidebar, Sources nested under Skills", async ({
  page,
}) => {
  await page.goto("/");
  await main(page).getByRole("link", { name: "Sources" }).click();
  await expect(
    page.getByRole("heading", { level: 1, name: "Sources" })
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/skills\/sources$/u);
  await expect(
    main(page).getByRole("link", { name: "Sources" })
  ).toHaveAttribute("aria-current", "page");
  await expect(
    main(page).getByRole("link", { name: "Skills" })
  ).not.toHaveAttribute("aria-current", "page");
});

test("the Add skills screen's old path opens Sources", async ({ page }) => {
  await page.goto("/#/add");
  await expect(page).toHaveURL(/#\/skills\/sources$/u);
});

test("keeps Skills active on a screen nested under it", async ({ page }) => {
  await page.goto("/#/skills/review/pdf");
  await expect(
    main(page).getByRole("link", { name: "Skills" })
  ).toHaveAttribute("aria-current", "page");
});
