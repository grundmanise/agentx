// The app's own updates: the check after the startup checks pass, the title bar's Update to,
// Installing with the download's percent, Restart to update, and App updates in Settings with its
// Check for updates switch and Check now. The Rust side's updater is the mocked CLI's.
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
  page.getByRole("region", { exact: true, name: "App updates" });
const checkSwitch = (page: Page) =>
  section(page).getByRole("switch", { name: "Check for updates" });

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

test("with no update, the title bar shows nothing for it", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  await page.goto("/#/skills");

  await expect.poll(() => cli.appUpdateChecks).toBe(1);
  await expect(updateButton(page)).toHaveCount(0);
});

test("Check for updates off: the app does not check, and Check now still does", async ({
  cli,
  page,
}) => {
  launch(cli, "synced");
  cli.savePreferences({ check_updates: false, editor: { kind: "system" } });
  await page.goto("/#/settings");

  await expect(checkSwitch(page)).not.toBeChecked();
  expect(cli.appUpdateChecks).toBe(0);

  await section(page).getByRole("button", { name: "Check now" }).click();
  await expect(toasts(page)).toContainText("agentx is up to date");
  expect(cli.appUpdateChecks).toBe(1);

  cli.offerAppUpdate("0.4.0");
  await section(page).getByRole("button", { name: "Check now" }).click();
  await expect(updateButton(page)).toHaveAccessibleName("Update to 0.4.0");
});

test("the switch saves Check for updates and keeps the editor", async ({
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
    });
});
