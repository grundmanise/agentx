import { expect, test } from "./harness/cli.ts";

declare global {
  interface Window {
    /** Set by e2e/harness/toast-kinds.ts: shows a failure toast. */
    showFailure?: () => void;
    /** Set by e2e/harness/running-toast.ts: shows the running command's outcome. */
    showOutcome?: () => void;
  }
}

test("stacks toasts, the newest in front, each with its action", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();

  // No UI raises a toast yet, so a harness module calls notify() twice.
  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/two-toasts.ts",
  });

  const toasts = page.locator("[data-sonner-toast]");
  await expect(toasts).toHaveCount(2);
  await expect(toasts.first()).toHaveAttribute("data-front", "true");
  await expect(toasts.first()).toContainText("Removed pdf from Cursor");
  await expect(toasts.nth(1)).toContainText("Added pdf to Cursor");
  await page.getByRole("button", { name: "Undo" }).click();
  await expect(page).toHaveTitle("undone");
});

test("puts a running command's outcome in place of its toast", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();

  await page.addScriptTag({
    type: "module",
    url: "/e2e/harness/running-toast.ts",
  });

  const toasts = page.locator("[data-sonner-toast]");
  await expect(toasts).toHaveCount(1);
  await expect(toasts).toContainText("Checking for updates… 1/2");
  await expect(toasts).toHaveAttribute("data-type", "loading");

  await page.evaluate(() => {
    window.showOutcome?.();
  });
  await expect(toasts).toHaveCount(1);
  await expect(toasts).toContainText("All skills are up to date");
  await expect(toasts).toHaveAttribute("data-type", "success");
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
  await expect(failure).toHaveAttribute("data-front", "true");
});
