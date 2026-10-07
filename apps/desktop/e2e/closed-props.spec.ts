import { expect, test } from "@playwright/test";

// The types reject className and style on our components
// (src/components/closed-props.test-d.tsx). These check what reaches the page when untyped
// code passes them anyway. Each harness module renders its components on load.
test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();
});

test("Button drops a className forced past the types", async ({ page }) => {
  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/forced-button.tsx",
  });
  const save = page
    .getByTestId("harness")
    .getByRole("button", { name: "Save" });
  await expect(save).toBeVisible();
  await expect(save).not.toHaveClass(/\bmt-2\b/u);
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
