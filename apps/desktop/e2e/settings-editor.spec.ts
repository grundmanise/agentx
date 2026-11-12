// The Editor section of Settings: System default, the editors found on this machine and a custom
// command line, one choice, saved in the app's own preferences and kept across a restart (a page
// reload, which keeps the mocked preferences file).
import type { Page } from "@playwright/test";

import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const FOUND = [
  { id: "vscode", name: "VS Code" },
  { id: "zed", name: "Zed" },
];

const section = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Editor" });
const choices = (page: Page) =>
  section(page).getByRole("radiogroup", { name: "Editor" });
const choice = (page: Page, name: string) =>
  choices(page).getByRole("radio", { exact: true, name });

/** Expects the choices to be `names`, in order. */
const expectChoices = async (page: Page, names: string[]) => {
  const radios = choices(page).getByRole("radio");
  await expect(radios).toHaveCount(names.length);
  await Promise.all(
    names.map(async (name, i) => {
      await expect(radios.nth(i)).toHaveAccessibleName(name);
    })
  );
};
const commandField = (page: Page) =>
  section(page).getByRole("textbox", { name: "Custom command" });

const openSettings = async (page: Page) => {
  await page.goto("/#/settings");
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  await expect(choices(page)).toBeVisible();
};

/** Restarts the app: the page loads again, and the preferences file stays. */
const restart = async (cli: MockCli, page: Page) => {
  cli.restart();
  await page.reload();
  await expect(choices(page)).toBeVisible();
};

test("Editor lists System default, the editors found and Custom command", async ({
  cli,
  page,
}) => {
  cli.answerEditors(FOUND);
  await openSettings(page);
  await expect(section(page)).toContainText(
    "The application that opens a skill when you edit it."
  );
  await expectChoices(page, [
    "System default",
    "VS Code",
    "Zed",
    "Custom command",
  ]);
  await expect(choice(page, "System default")).toBeChecked();
});

test("a detected editor, once chosen, is still chosen after a restart", async ({
  cli,
  page,
}) => {
  cli.answerEditors(FOUND);
  await openSettings(page);
  await choice(page, "Zed").click();
  await expect(choice(page, "Zed")).toBeChecked();
  await expect
    .poll(() => cli.savedPreferences)
    .toEqual({
      editor: { id: "zed", kind: "app", name: "Zed" },
    });

  await restart(cli, page);
  await expect(choice(page, "Zed")).toBeChecked();

  // Gone from the machine since: still listed, so the choice shows.
  cli.answerEditors([FOUND[0]]);
  await restart(cli, page);
  await expectChoices(page, [
    "System default",
    "VS Code",
    "Zed",
    "Custom command",
  ]);
  await expect(choice(page, "Zed")).toBeChecked();
});

test("Custom command shows its field, saves the command on Enter, and keeps it after a restart", async ({
  cli,
  page,
}) => {
  cli.answerEditors(FOUND);
  await openSettings(page);
  await choice(page, "Custom command").click();
  await expect(choice(page, "Custom command")).toBeChecked();
  await expect(commandField(page)).toHaveAttribute(
    "placeholder",
    "code --wait"
  );
  await expect(section(page)).toContainText(
    "The file path is added at the end."
  );

  await commandField(page).fill("subl -n");
  await commandField(page).press("Enter");
  await expect
    .poll(() => cli.savedPreferences)
    .toEqual({ editor: { command: "subl -n", kind: "custom" } });

  await restart(cli, page);
  await expect(choice(page, "Custom command")).toBeChecked();
  await expect(commandField(page)).toHaveValue("subl -n");

  // Back to the system default; the command typed stays in the field for next time.
  await choice(page, "System default").click();
  await expect
    .poll(() => cli.savedPreferences)
    .toEqual({ editor: { kind: "system" } });
  await choice(page, "Custom command").click();
  await expect(commandField(page)).toHaveValue("subl -n");
});

test("with no editor found, the choice is System default or Custom command", async ({
  page,
}) => {
  await openSettings(page);
  await expectChoices(page, ["System default", "Custom command"]);
});
