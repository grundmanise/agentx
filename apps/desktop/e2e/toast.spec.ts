import { expect, test } from "./harness/cli.ts";

test("shows one toast at a time, with its action", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();

  // No UI raises a toast yet, so a harness module calls notify() twice.
  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/two-toasts.ts",
  });

  await expect(page.getByText("Removed pdf from Cursor")).toBeVisible();
  // The first toast would stay 2.6s on its own; it must go well before that.
  await expect(page.getByText("Added pdf to Cursor")).toHaveCount(0, {
    timeout: 1000,
  });
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(page).toHaveTitle("undone");
});

test("replaces the toast even when timers run late", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible();

  // The harness module delays every timer set while it calls notify() twice.
  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/two-toasts-busy.ts",
  });

  await expect(page.getByText("Removed pdf from Cursor")).toBeVisible();
  await expect(page.getByText("Added pdf to Cursor")).toHaveCount(0, {
    timeout: 1000,
  });
});
