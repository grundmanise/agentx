// Edit in the Skills detail pane: offered for one of your own skills and an edited skill of a
// source. It opens the skill's SKILL.md in the editor chosen in Settings; when the editor cannot
// be opened, a toast offers to show the file in the file manager instead, which is Finder on
// macOS.
import type { Page } from "@playwright/test";

import { adoption, fileReads, inventory } from "./fixtures/skills.ts";
import { expect, libraryPath, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const actions = (page: Page, name: string) =>
  pane(page, name).getByLabel("Actions", { exact: true });
const editButton = (page: Page, name: string) =>
  actions(page, name).getByRole("button", { exact: true, name: "Edit" });

const libraryNames = [
  "draft",
  "gone",
  "pdf",
  "pdf-letters",
  "release-notes",
  "review",
  "scratch",
];

/** Opens a skill: a library skill by its name, any other by its row's key. */
const openSkill = async (
  cli: MockCli,
  page: Page,
  name: string,
  key = name
) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  for (const n of libraryNames) {
    cli.register(["skill", "history", n], { events: [] });
    cli.register(["skill", "diff", n], { events: [] });
  }
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  await page.goto(`/#/skills/${encodeURIComponent(key)}`);
  await expect(pane(page, name)).toBeVisible();
};

test.describe("Edit button", () => {
  for (const name of ["draft", "pdf-letters", "release-notes", "review"]) {
    test(`shows for ${name}`, async ({ cli, page }) => {
      await openSkill(cli, page, name);
      await expect(editButton(page, name)).toBeVisible();
    });
  }
});

// The action row of each skill that is not yours and has no edits.
for (const [name, key, shown] of [
  ["pdf", "pdf", ["Fork", "Remove"]],
  ["gone", "gone", ["Fork", "Remove"]],
  ["scratch", "scratch", ["Fork", "Remove"]],
  ["commit-helper", "skill:h-commit", ["Fork"]],
] as const) {
  test(`the action row of ${name} shows ${shown.join(" and ")}`, async ({
    cli,
    page,
  }) => {
    await openSkill(cli, page, name, key);
    await expect(actions(page, name).getByRole("button")).toHaveText([
      ...shown,
    ]);
  });
}

test("Edit comes first in the action row and opens SKILL.md in the chosen editor", async ({
  cli,
  page,
}) => {
  const vscode = { id: "vscode", kind: "app", name: "VS Code" };
  cli.answerEditors([{ id: "vscode", name: "VS Code" }]);
  cli.savePreferences({ editor: vscode });
  await openSkill(cli, page, "review");
  await expect(actions(page, "review").getByRole("button")).toHaveText([
    "Edit",
    "Fork",
    "Remove",
  ]);
  await editButton(page, "review").click();
  await expect
    .poll(() => cli.editorOpens)
    .toEqual([{ editor: vscode, path: `${libraryPath}/review/SKILL.md` }]);
});

test("an editor that cannot be opened says so in a toast whose Show in file manager shows the file", async ({
  cli,
  page,
}) => {
  cli.savePreferences({ editor: { command: "subl -n", kind: "custom" } });
  cli.failEditorOpens();
  await openSkill(cli, page, "release-notes");
  await editButton(page, "release-notes").click();
  const path = `${libraryPath}/release-notes/SKILL.md`;
  await expect
    .poll(() => cli.editorOpens)
    .toEqual([{ editor: { command: "subl -n", kind: "custom" }, path }]);
  const toast = toasts(page).getByText("Couldn't open SKILL.md in subl");
  await expect(toast).toBeVisible();
  await toasts(page)
    .getByRole("button", { exact: true, name: "Show in file manager" })
    .click();
  await expect.poll(() => cli.reveals).toEqual([[path]]);
});

test("the system default that cannot open the file names the default app", async ({
  cli,
  page,
}) => {
  cli.failEditorOpens();
  await openSkill(cli, page, "draft");
  await editButton(page, "draft").click();
  await expect(
    toasts(page).getByText("Couldn't open SKILL.md in the default app")
  ).toBeVisible();
  await expect(
    toasts(page).getByRole("button", {
      exact: true,
      name: "Show in file manager",
    })
  ).toBeVisible();
});

test("on macOS the file manager is Finder: the toast's action and the FILES pane's button say so", async ({
  cli,
  page,
}) => {
  await cli.runAsMac();
  cli.failEditorOpens();
  await openSkill(cli, page, "draft");
  await expect(
    pane(page, "draft").getByRole("button", {
      exact: true,
      name: "Show SKILL.md in Finder",
    })
  ).toHaveAttribute("title", "Show in Finder");
  await editButton(page, "draft").click();
  await toasts(page)
    .getByRole("button", { exact: true, name: "Show in Finder" })
    .click();
  await expect
    .poll(() => cli.reveals)
    .toEqual([[`${libraryPath}/draft/SKILL.md`]]);
  await expect(page.getByText("⌘K")).toBeVisible();
});
