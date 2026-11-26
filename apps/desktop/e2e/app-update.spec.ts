// The app's own updates: the check after the startup checks pass, the title bar's Update to,
// Installing with the download's percent, Restart to update, the Updates card in Settings with the
// version, when the app last checked, Check for updates, Release notes and the Automatic update
// checks switch, and the macOS app menu's Settings… and Check for Updates…. The Rust side's
// updater is the mocked CLI's.
import type { Page } from "@playwright/test";

import { launch } from "./fixtures/palette-publish.ts";
import { expect, test } from "./harness/cli.ts";

const updateButton = (page: Page) =>
  page.getByRole("banner").getByRole("button", {
    name: /^Update to|^Installing|^Restart to update/u,
  });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const section = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Updates" });
const checkSwitch = (page: Page) =>
  section(page).getByRole("switch", { name: "Automatic update checks" });

test("an update the check finds installs from the title bar, then restarts the app", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.offerAppUpdate("0.4.0");
  cli.holdAppUpdateInstall();
  await page.goto("/#/skills");

  await expect(updateButton(page)).toHaveAccessibleName("Update to 0.4.0");
  await updateButton(page).click();
  await expect(updateButton(page)).toHaveAccessibleName("Installing…");
  await expect(updateButton(page)).toHaveAttribute("aria-busy", "true");

  await cli.appUpdateProgress(42, 100);
  await expect(updateButton(page)).toHaveAccessibleName("Installing 42%");

  cli.endAppUpdateInstall();
  await expect(updateButton(page)).toHaveAccessibleName("Restart to update");
  await updateButton(page).click();
  await expect.poll(() => cli.restarts).toBe(1);
});

test("the rim stays drawn while the pill changes size", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.offerAppUpdate("0.4.0");
  cli.holdAppUpdateInstall();
  await page.goto("/#/skills");

  await updateButton(page).click();
  await cli.appUpdateProgress(5, 100);
  await expect(updateButton(page)).toHaveAccessibleName("Installing 5%");
  const canvas = updateButton(page).locator("canvas[data-progress]");
  await expect
    .poll(async () => Number(await canvas.getAttribute("data-progress")))
    .toBeGreaterThan(0.1);
  // Counts the rim's lit pixels each time the pill resizes, before the pill is painted.
  await canvas.evaluate((element: HTMLCanvasElement) => {
    const gl = element.getContext("webgl");
    const counts: number[] = [];
    element.dataset.lit = "";
    const pill = element.parentElement;
    if (gl === null || pill === null) {
      return;
    }
    new ResizeObserver(() => {
      const pixels = new Uint8Array(element.width * element.height * 4);
      gl.readPixels(
        0,
        0,
        element.width,
        element.height,
        gl.RGBA,
        gl.UNSIGNED_BYTE,
        pixels
      );
      let lit = 0;
      for (let i = 3; i < pixels.length; i += 4) {
        lit += pixels[i] === 0 ? 0 : 1;
      }
      counts.push(lit);
      element.dataset.lit = counts.join(" ");
    }).observe(pill);
  });
  await expect(canvas).toHaveAttribute("data-lit", /^[1-9]\d*$/u);

  await cli.appUpdateProgress(42, 100);
  await expect(updateButton(page)).toHaveAccessibleName("Installing 42%");
  // The first count is the observer's own first call; the second, the resize the new label brings.
  await expect(canvas).toHaveAttribute("data-lit", /^[1-9]\d* [1-9]\d*$/u);
});

test("a failed install says so and offers the update again", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.offerAppUpdate("0.4.0");
  cli.holdAppUpdateInstall();
  await page.goto("/#/skills");

  await updateButton(page).click();
  cli.endAppUpdateInstall({ failed: true });
  await expect(toasts(page)).toContainText("Couldn't install agentx 0.4.0");
  await expect(updateButton(page)).toHaveAccessibleName("Update to 0.4.0");
  expect(cli.restarts).toBe(0);
});

test("a newer update found during an install installs nothing and offers the newer one", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.offerAppUpdate("0.4.0");
  cli.holdAppUpdateInstall();
  await page.goto("/#/skills");

  await updateButton(page).click();
  cli.offerAppUpdate("0.5.0");
  cli.endAppUpdateInstall();
  await expect(toasts(page)).toContainText(
    "Update failed. agentx 0.5.0 is available"
  );
  await expect(updateButton(page)).toHaveAccessibleName("Update to 0.5.0");

  await toasts(page).getByRole("button", { name: "Update" }).click();
  await expect(updateButton(page)).toHaveAccessibleName("Restart to update");
});

test("with no update, the title bar shows nothing for it", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  await page.goto("/#/skills");

  await expect.poll(() => cli.appUpdateChecks).toBe(1);
  await expect(updateButton(page)).toHaveCount(0);
});

test("Automatic update checks off: the app does not check, and Check for updates still does", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.savePreferences({ check_updates: false, editor: { kind: "system" } });
  await page.goto("/#/settings");

  await expect(checkSwitch(page)).not.toBeChecked();
  expect(cli.appUpdateChecks).toBe(0);

  await section(page)
    .getByRole("button", { name: "Check for updates" })
    .click();
  await expect(toasts(page)).toContainText("You're running the latest version");
  expect(cli.appUpdateChecks).toBe(1);

  cli.offerAppUpdate("0.4.0");
  await section(page)
    .getByRole("button", { name: "Check for updates" })
    .click();
  await expect(updateButton(page)).toHaveAccessibleName("Update to 0.4.0");
});

test("the switch saves Automatic update checks and keeps the editor", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.savePreferences({
    check_updates: true,
    editor: { command: "code --wait", kind: "custom" },
  });
  await page.goto("/#/settings");

  await expect(checkSwitch(page)).toBeChecked();
  await checkSwitch(page).click();
  await expect(checkSwitch(page)).not.toBeChecked();
  await expect
    .poll(() => cli.savedPreferences)
    .toEqual({
      check_updates: false,
      editor: { command: "code --wait", kind: "custom" },
      open_after_create: false,
    });
});

test("the app menu's Check for Updates… checks at once and says when there is none", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  await page.goto("/#/skills");
  await expect.poll(() => cli.appUpdateChecks).toBe(1);

  await cli.clickAppMenu("check-updates");
  await expect(toasts(page)).toContainText("You're running the latest version");
  expect(cli.appUpdateChecks).toBe(2);
});

test("the app menu's Settings… and ⌘, open Settings", async ({ cli, page }) => {
  launch(cli, "synced");
  await page.goto("/#/skills");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();

  await cli.clickAppMenu("settings");
  await expect(page).toHaveURL(/#\/settings$/u);

  await page.goto("/#/skills");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await page.keyboard.press("ControlOrMeta+Comma");
  await expect(page).toHaveURL(/#\/settings$/u);
});

test("Updates shows the version and when the app last checked, and Release notes opens its release", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  await page.goto("/#/settings");

  await expect(section(page)).toContainText("agentx 0.3.0");
  await expect(section(page)).toContainText("Checked just now");

  await section(page).getByRole("button", { name: "Release notes" }).click();
  await expect
    .poll(() => cli.openedUrls)
    .toEqual(["https://github.com/grundmanise/agentx/releases/tag/v0.3.0"]);
});

test("with Automatic update checks off, Updates says it has not checked yet", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.savePreferences({ check_updates: false, editor: { kind: "system" } });
  await page.goto("/#/settings");

  await expect(section(page)).toContainText("Not checked yet");
});

test("a build that is no release has no Release notes", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.appVersion = "0.0.0";
  await page.goto("/#/settings");

  await expect(section(page)).toContainText("agentx 0.0.0");
  await expect(
    section(page).getByRole("button", { name: "Release notes" })
  ).toHaveCount(0);
});

test("an update a check finds installs from Updates, as from the title bar", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.offerAppUpdate("0.4.0");
  cli.holdAppUpdateInstall();
  await page.goto("/#/settings");

  await expect(section(page)).toContainText("agentx 0.4.0 is available");
  await section(page).getByRole("button", { name: "Update to 0.4.0" }).click();
  await expect(
    section(page).getByRole("button", { name: "Installing…" })
  ).toBeDisabled();

  cli.endAppUpdateInstall();
  await section(page)
    .getByRole("button", { name: "Restart to update" })
    .click();
  await expect.poll(() => cli.restarts).toBe(1);
});

test("a release leaves nightly builds out until Include nightly builds is on, which checks again", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  await page.goto("/#/settings");
  const nightlies = section(page).getByRole("switch", {
    name: "Include nightly builds",
  });

  await expect(nightlies).not.toBeChecked();
  await expect.poll(() => cli.appUpdateNightlies).toEqual([false]);

  cli.offerAppUpdate("0.4.0-nightly.20261126.7");
  await nightlies.click();
  await expect(nightlies).toBeChecked();
  await expect
    .poll(() => cli.savedPreferences)
    .toMatchObject({
      include_nightlies: true,
    });
  await expect.poll(() => cli.appUpdateNightlies).toEqual([false, true]);
  await expect(updateButton(page)).toHaveAccessibleName(
    "Update to 0.4.0-nightly.20261126.7"
  );

  cli.offerAppUpdate(null);
  await nightlies.click();
  await expect.poll(() => cli.appUpdateNightlies).toEqual([false, true, false]);
  await expect(updateButton(page)).toHaveCount(0);
});

test("a nightly build takes nightly builds unless the preference says otherwise", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.appVersion = "0.3.0-nightly.20261125.100";
  await page.goto("/#/settings");

  await expect(
    section(page).getByRole("switch", { name: "Include nightly builds" })
  ).toBeChecked();
  await expect.poll(() => cli.appUpdateNightlies).toEqual([true]);
});
