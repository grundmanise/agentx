import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

// The app in a browser, on serve's WebSocket door (ADR 0016): no Tauri runtime, so no opener
// plugin, file manager, editor or open dialog. This file imports Playwright's own `test`, so no
// mocked Rust shell is installed; the page loads only the harness module that calls the helpers.

declare global {
  interface Window {
    /** Set by e2e/harness/os.tsx: the helpers that reach the operating system. */
    os: {
      openInBrowser: (url: string) => Promise<void>;
      openInEditor: (path: string) => Promise<void>;
      pickSnapshot: () => Promise<string | null>;
      revealInFinder: (path: string) => Promise<void>;
      saveEditor: (editor: { kind: "system" }) => Promise<boolean>;
    };
  }
}

const FILE = "/library/pdf/SKILL.md";

/**
 * Opens the app's own page, with the harness module in place of the app's entry, so the dev
 * server's React preamble and the app's styles load and the app does not launch.
 */
const openHarness = async (page: Page) => {
  await page.route("/", async (route) => {
    const response = await route.fetch();
    const body = await response.text();
    await route.fulfill({
      body: body.replace("/src/main.tsx", "/e2e/harness/os.tsx"),
      response,
    });
  });
  await page.goto("/");
  await page.waitForFunction(() => "os" in window);
};

test("a link opens in a new tab, with no way back to the app", async ({
  context,
  page,
}) => {
  await context.route("https://github.com/**", async (route) => {
    await route.fulfill({ body: "repository", contentType: "text/html" });
  });
  await openHarness(page);

  const popup = page.waitForEvent("popup");
  await page.evaluate(async () => {
    await window.os.openInBrowser("https://github.com/example/skills");
  });
  const tab = await popup;

  expect(tab.url()).toBe("https://github.com/example/skills");
  expect(await tab.evaluate(() => window.opener === null)).toBe(true);
  await expect(page.getByText(/Couldn't open/u)).toHaveCount(0);
});

test("Show in file manager and Edit copy the path, and say why nothing opened", async ({
  context,
  page,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await openHarness(page);
  const clipboard = async () =>
    await page.evaluate(async () => await navigator.clipboard.readText());

  await page.evaluate(async (path) => {
    await window.os.revealInFinder(path);
  }, FILE);
  await expect(
    page.getByText(
      "Copied the path. A browser can't show files in the file manager"
    )
  ).toBeVisible();
  expect(await clipboard()).toBe(FILE);

  await page.evaluate(async () => {
    await navigator.clipboard.writeText("");
  });
  await page.evaluate(async (path) => {
    await window.os.openInEditor(path);
  }, FILE);
  await expect(
    page.getByText("Copied the path. A browser can't open an editor")
  ).toBeVisible();
  expect(await clipboard()).toBe(FILE);
});

test("the open dialog and the editor preference say they need the app", async ({
  page,
}) => {
  await openHarness(page);

  expect(
    await page.evaluate(async () => await window.os.pickSnapshot())
  ).toBeNull();
  await expect(
    page.getByText("Couldn't open the file dialog. Apply a snapshot in the app")
  ).toBeVisible();

  expect(
    await page.evaluate(
      async () => await window.os.saveEditor({ kind: "system" })
    )
  ).toBe(false);
  await expect(
    page.getByText("Couldn't save the editor. A browser can't keep it")
  ).toBeVisible();
});
