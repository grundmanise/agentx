// Rename, for one of your own skills: Rename your skill under AGENTS, on a plugin's skill your fork
// of the same name sits beside, renames the fork and selects it under its new name; Rename in the
// action row with a name another skill has keeps the dialog open with the CLI's words.
import type { Page } from "@playwright/test";

import { sources } from "./fixtures/agents.ts";
import { commitHelperForked, commitHelperRenamed } from "./fixtures/fork.ts";
import { adoption, filesOf } from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

/** The command lines the Rename controls ran. */
const renames = (cli: MockCli) =>
  cli.commands.filter((l) => l.slice(0, 2).join(" ") === "skill rename");

const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const renameDialog = (page: Page, name: string) =>
  page.getByRole("dialog", { name: `Rename ${name}?` });

/** Opens a skill of the machine where your commit-helper sits beside the acme-tools plugin's. */
const openSkill = async (
  cli: MockCli,
  page: Page,
  name: string,
  key = name
) => {
  cli.onServeStart({ events: [commitHelperForked] });
  cli.register(["adopt"], { events: adoption });
  cli.register(["source", "list"], { events: sources });
  for (const n of ["commit-helper", "my-commit-helper", "release-notes"]) {
    cli.register(["skill", "history", n], { events: [] });
    cli.register(["skill", "files", n], { events: filesOf(n) });
    cli.register(["skill", "diff", n], { events: [] });
  }
  cli.register(["skill", "files", "acme-tools:commit-helper"], {
    events: filesOf("commit-helper"),
  });
  await page.goto(`/#/skills/${encodeURIComponent(key)}`);
  await expect(pane(page, name)).toBeVisible();
};

test("Rename your skill on a plugin's skill renames your skill of that name and selects it", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "commit-helper", "skill:h-commit");
  const notice = pane(page, "commit-helper")
    .getByRole("region", { name: "Agents" })
    .getByRole("note", { name: "Skills of the same name" });
  await notice.getByRole("button", { name: "Rename your skill" }).click();
  const dialog = renameDialog(page, "commit-helper");
  const field = dialog.getByRole("textbox", { name: "Name" });
  await expect(field).toHaveValue("commit-helper");
  await expect(field).toBeFocused();
  const submit = dialog.getByRole("button", { name: "Rename" });
  await expect(submit).toBeDisabled();
  cli.register(["skill", "rename", "commit-helper", "my-commit-helper"], {
    events: [{ name: "my-commit-helper", type: "library_skill" }],
  });

  // The name is selected, so typing replaces it.
  await field.pressSequentially("my-commit-helper");
  await submit.click();
  await expect(dialog).toHaveCount(0);
  await expect(
    page
      .getByRole("region", { name: /^Notifications/u })
      .getByText("Renamed commit-helper skill to my-commit-helper", {
        exact: true,
      })
  ).toBeVisible();
  await expect(page).toHaveURL(/#\/skills\/my-commit-helper$/u);
  await cli.serveEmit([commitHelperRenamed]);
  await expect(pane(page, "my-commit-helper")).toBeVisible();
  expect(renames(cli)).toEqual([
    ["skill", "rename", "commit-helper", "my-commit-helper", "--json"],
  ]);
});

test("Rename with a name another skill has: the dialog stays open with the CLI's words", async ({
  cli,
  page,
}) => {
  await openSkill(cli, page, "commit-helper");
  await pane(page, "commit-helper")
    .getByLabel("Actions", { exact: true })
    .getByRole("button", { name: "Rename" })
    .click();
  const dialog = renameDialog(page, "commit-helper");
  cli.register(["skill", "rename", "commit-helper", "release-notes"], {
    events: [
      {
        code: "refused",
        hint: "choose another name with 'agentx skill rename commit-helper <new>'",
        message:
          "the account repo already holds release-notes as one of your own skills",
        type: "error",
      },
    ],
    exitCode: 6,
  });

  const field = dialog.getByRole("textbox", { name: "Name" });
  await field.fill("release-notes");
  await dialog.getByRole("button", { name: "Rename" }).click();
  const alert = dialog.getByRole("alert");
  await expect(alert).toHaveText(
    "The account repo already holds release-notes as one of your own skills. Choose another name."
  );
  await field.fill("changelog");
  await expect(alert).toHaveCount(0);
  expect(renames(cli)).toEqual([
    ["skill", "rename", "commit-helper", "release-notes", "--json"],
  ]);
});
