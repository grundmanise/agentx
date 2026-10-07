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
