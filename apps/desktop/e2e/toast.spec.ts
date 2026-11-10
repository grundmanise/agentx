import type { Page } from "@playwright/test";

import { expect, test } from "./harness/cli.ts";

declare global {
  interface Window {
    agentxNotify: (
      message: string,
      action?: { label: string; onClick: () => void }
    ) => void;
  }
}

const toastOf = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u }).getByRole("listitem");

/** Opens the app and loads notify() into the page, as the harness module exposes it. */
const withNotify = async (page: Page) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await page.addScriptTag({ type: "module", url: "/e2e/harness/notify.ts" });
  await page.waitForFunction(() => typeof window.agentxNotify === "function");
};

/** Shows a toast with `message`, and an action when `label` is given. */
const show = async (page: Page, message: string, label?: string) => {
  await page.evaluate(
    ([text, action]) => {
      window.agentxNotify(
        text,
        action === undefined
          ? undefined
          : {
              label: action,
              onClick: () => {
                document.title = "clicked";
              },
            }
      );
    },
    [message, label] as const
  );
};

const LONG =
  "There is a newer version of release-notes skill on the account remote. Update it first, then publish your changes. · 2 not published yet";

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

test("a short toast keeps one line at its height", async ({ page }) => {
  await withNotify(page);
  await show(page, "Published notes skill");
  const box = await toastOf(page).boundingBox();
  expect(box?.height).toBe(44);
});

for (const viewport of [
  { height: 800, width: 1280 },
  { height: 700, width: 1100 },
]) {
  test(`a long toast wraps inside a ${viewport.width}x${viewport.height} window, with its action beside the text`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport);
    await withNotify(page);
    await show(page, LONG, "Update");
    const toast = toastOf(page);
    await expect(toast).toContainText("2 not published yet");
    const box = await toast.boundingBox();
    const action = await toast
      .getByRole("button", { name: "Update" })
      .boundingBox();
    const text = await toast.getByText(LONG).boundingBox();
    if (box === null || action === null || text === null) {
      throw new Error("the toast is not laid out");
    }
    // Wrapped: taller than one line, no wider than toast.max-width, centred in the window.
    expect(box.height).toBeGreaterThan(44);
    expect(box.width).toBeLessThanOrEqual(640);
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width);
    expect(Math.abs(box.x + box.width / 2 - viewport.width / 2)).toBeLessThan(
      2
    );
    // The action sits beside the text, inside the toast and the window.
    expect(action.x).toBeGreaterThanOrEqual(text.x + text.width);
    expect(action.x + action.width).toBeLessThanOrEqual(box.x + box.width);
    expect(action.y).toBeGreaterThanOrEqual(box.y);
    expect(action.y + action.height).toBeLessThanOrEqual(box.y + box.height);
    await toast.getByRole("button", { name: "Update" }).click();
    await expect(page).toHaveTitle("clicked");
  });
}
