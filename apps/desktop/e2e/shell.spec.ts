import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

const main = (page: Page) => page.getByRole("navigation", { name: "Main" });

test("opens on the Inbox and lists the screens in sidebar order", async ({
  page,
}) => {
  await page.goto("/");
  const nav = main(page);
  await expect(nav.getByRole("link")).toHaveText([
    "Inbox",
    "Skills",
    "Add",
    "Agents",
    "MCP servers",
    "Plugins",
    "Settings",
  ]);
  await expect(nav.getByRole("link", { name: "Inbox" })).toHaveAttribute(
    "aria-current",
    "page"
  );
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();

  // Each link's icon is an inline SVG, hidden from screen readers and from the tab order.
  const icons = nav.locator("svg");
  await expect(icons).toHaveCount(7);
  await expect(nav.getByRole("img")).toHaveCount(0);
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

test("brightens a nav icon with its label on hover and on the active item", async ({
  page,
}) => {
  await page.goto("/");
  const icon = (name: string) =>
    main(page).getByRole("link", { name }).locator("svg");
  // The icons draw in currentColor: shell.nav-text, then text.primary on hover, and
  // shell.nav-active-text on the active item.
  const navText = "rgb(139, 139, 147)";
  const bright = "rgb(237, 237, 239)";
  await expect(icon("Inbox")).toHaveCSS("stroke", bright);
  await expect(icon("Skills")).toHaveCSS("stroke", navText);
  await main(page).getByRole("link", { name: "Skills" }).hover();
  await expect(icon("Skills")).toHaveCSS("stroke", bright);
});

test("moves between screens from the sidebar", async ({ page }) => {
  await page.goto("/");
  await main(page).getByRole("link", { name: "Add" }).click();
  await expect(page.getByRole("heading", { name: "Add skills" })).toBeVisible();
  await expect(main(page).getByRole("link", { name: "Add" })).toHaveAttribute(
    "aria-current",
    "page"
  );
});

test("keeps Skills active on a screen nested under it", async ({ page }) => {
  await page.goto("/#/skills/review/pdf");
  await expect(
    main(page).getByRole("link", { name: "Skills" })
  ).toHaveAttribute("aria-current", "page");
});
