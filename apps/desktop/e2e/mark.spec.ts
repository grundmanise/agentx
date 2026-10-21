import { expect, test } from "@playwright/test";

// The harness module renders the product mark at both of its sizes on load.
test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
});

test("each product mark size has its own radius", async ({ page }) => {
  await page.addScriptTag({ type: "module", url: "/e2e/harness/marks.tsx" });
  const marks = page
    .getByTestId("harness")
    .getByRole("img", { name: "agentx" });
  await expect(marks).toHaveCount(2);
  // shell.logo-radius and startup.logo-radius in DESIGN.md.
  await expect(marks.nth(0)).toHaveCSS("border-radius", "8px");
  await expect(marks.nth(1)).toHaveCSS("border-radius", "12px");
});
