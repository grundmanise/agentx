import { expect, test } from "./harness/cli.ts";

declare global {
  interface Window {
    /** Set by e2e/harness/toast-kinds.ts: shows a failure toast. */
    showFailure?: () => void;
  }
}

test("shows one toast at a time, with its action", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();

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
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();

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

test("shows a report at the top right, with its outcome's icon and tint", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();

  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/toast-kinds.ts",
  });

  const toaster = page.locator("[data-sonner-toaster]");
  await expect(toaster).toHaveAttribute("data-y-position", "top");
  await expect(toaster).toHaveAttribute("data-x-position", "right");
  const success = page.locator("[data-sonner-toast]", {
    hasText: "Added 2 skills",
  });
  await expect(success).toHaveAttribute("data-type", "success");
  await expect(success.locator("[data-icon] svg")).toBeVisible();

  await page.evaluate(() => {
    window.showFailure?.();
  });
  const failure = page.locator("[data-sonner-toast]", {
    hasText: "Couldn't open the file dialog",
  });
  await expect(failure).toHaveAttribute("data-type", "error");
  await expect(success).toHaveCount(0, { timeout: 1000 });
});
