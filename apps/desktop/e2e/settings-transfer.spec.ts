// The Export settings and Import settings sections of Settings, the last ones of the page: Export
// settings runs `export <name>` (with --force after the overwrite confirm), and Import settings
// runs `import <file> --yes` on the file the open dialog returns after its confirm, then
// `source add` for each source of the imported settings this machine has not downloaded, one
// after another and without asking, and shows what was installed, updated and skipped.
import type { Page } from "@playwright/test";

import {
  alreadyThere,
  exported,
  imported,
  LIBRARY,
  NEW_ACCOUNT,
  nothingNew,
  SNAPSHOT,
  sourcesAfterImport,
  sourcesBefore,
  TEAM,
  UNMANAGED_REASON,
  unreachable,
} from "./fixtures/settings-transfer.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/**
 * The command lines the page ran after the launch and the loads, without the listings of each
 * shared source the Sources section reads.
 */
const later = (cli: MockCli) =>
  cli.commands.filter(
    (l) =>
      ![...launchLines, ...loadLines].some(
        (known) => known.join(" ") === l.join(" ")
      ) && l.slice(0, 2).join(" ") !== "source skills"
  );

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const exportSettings = (page: Page) =>
  page.getByRole("region", { name: "Export settings" });
const importSettings = (page: Page) =>
  page.getByRole("region", { name: "Import settings" });

const openSettings = async (cli: MockCli, page: Page) => {
  cli.register(["source", "list"], { events: sourcesBefore });
  // The Sources section lists the skills of each shared source, before and after the import.
  for (const id of ["s-vendor", "s-team"]) {
    cli.register(["source", "skills", id], { events: [] });
  }
  await page.goto("/#/settings");
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  await expect(exportSettings(page)).toBeVisible();
  await expect(importSettings(page)).toBeVisible();
};

test("exports under the name given and toasts the path the CLI wrote", async ({
  cli,
  page,
}) => {
  cli.register(["export", "agentx-export"], { events: exported });
  await openSettings(cli, page);
  const section = exportSettings(page);
  await expect(section).toContainText(
    "Saves your settings and the list of your skills to a file. Import it on another machine to transfer them."
  );
  const name = section.getByRole("textbox", { name: "File name" });
  await expect(name).toHaveValue("agentx-export");
  await expect(section).toContainText(
    "Saved to your library as agentx-export.json"
  );
  await section.getByRole("button", { exact: true, name: "Export" }).click();
  await expect(toasts(page)).toContainText(
    `Exported settings to ${LIBRARY}/agentx-export.json`
  );
  expect(later(cli)).toEqual([["export", "agentx-export", "--json"]]);

  // A name export would refuse disables Export.
  await name.fill("snapshot.json");
  await expect(section).toContainText(
    "Enter a file name without an extension or a slash"
  );
  await expect(
    section.getByRole("button", { exact: true, name: "Export" })
  ).toBeDisabled();
  await name.fill("work-laptop");
  await expect(section).toContainText(
    "Saved to your library as work-laptop.json"
  );
});

test("a failed export shows the CLI's message and hint under the field, as one paragraph", async ({
  cli,
  page,
}) => {
  cli.register(["export", "agentx-export"], {
    events: [
      {
        code: "internal",
        hint: "check that you can write to the library",
        message: `can't write ${LIBRARY}/agentx-export.json: permission denied`,
        type: "error",
      },
    ],
    exitCode: 10,
  });
  await openSettings(cli, page);
  const section = exportSettings(page);
  await section.getByRole("button", { exact: true, name: "Export" }).click();
  // The message and the hint as one sentence-cased paragraph.
  await expect(section.getByRole("alert")).toHaveText(
    `Can't write ${LIBRARY}/agentx-export.json: permission denied. Check that you can write to the library.`
  );
  expect(later(cli)).toEqual([["export", "agentx-export", "--json"]]);
});

test("asks before it replaces a file in the library, then exports with --force", async ({
  cli,
  page,
}) => {
  cli.register(["export", "agentx-export"], {
    events: alreadyThere,
    exitCode: 6,
  });
  cli.register(["export", "agentx-export", "--force"], { events: exported });
  await openSettings(cli, page);
  await exportSettings(page)
    .getByRole("button", { exact: true, name: "Export" })
    .click();
  const confirm = page.getByRole("dialog", {
    name: "Replace agentx-export.json?",
  });
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText(
    "A file with this name is already in your library."
  );
  expect(later(cli)).toEqual([["export", "agentx-export", "--json"]]);
  await confirm.getByRole("button", { name: "Replace" }).click();
  await expect(toasts(page)).toContainText(
    `Exported settings to ${LIBRARY}/agentx-export.json`
  );
  expect(later(cli)).toEqual([
    ["export", "agentx-export", "--json"],
    ["export", "agentx-export", "--force", "--json"],
  ]);
});

test("imports the picked file after its confirm, adds the sources it names without asking and shows what changed", async ({
  cli,
  page,
}) => {
  cli.answerOpenDialog(SNAPSHOT);
  cli.register(["import", SNAPSHOT, "--yes"], { events: imported });
  const accountAdd = cli.register(["source", "add", NEW_ACCOUNT, "--account"], {
    events: [
      { account: true, id: "s-new-account", type: "source", url: NEW_ACCOUNT },
    ],
    holdAfter: 0,
  });
  cli.register(["source", "add", `${TEAM}#v2`], {
    events: unreachable,
    exitCode: 3,
  });
  await openSettings(cli, page);
  const section = importSettings(page);
  await expect(section).toContainText(
    "Replaces the settings on this machine with the ones from an exported file."
  );
  const importButton = section.getByRole("button", { name: "Import…" });
  await importButton.click();

  const confirm = page.getByRole("dialog", {
    name: "Replace settings with agentx-export.json?",
  });
  await expect(confirm).toBeVisible();
  await expect(confirm.getByRole("paragraph")).toHaveText([
    "Missing skills will be installed during import, and existing might get updated.",
    SNAPSHOT,
  ]);
  expect(cli.openDialogs).toEqual([
    expect.objectContaining({ defaultPath: LIBRARY, multiple: false }),
  ]);

  // The imported settings name sources this machine has not downloaded.
  cli.register(["source", "list"], { events: sourcesAfterImport });
  await confirm.getByRole("button", { name: "Import" }).click();
  await expect(toasts(page)).toContainText(
    "Imported settings from agentx-export.json"
  );

  // The sources are added one after another, with no click, and Import… waits for the last.
  await expect
    .poll(() => later(cli))
    .toEqual([
      ["import", SNAPSHOT, "--yes", "--json"],
      ["source", "add", NEW_ACCOUNT, "--account", "--json"],
    ]);
  await expect(importButton).toBeDisabled();
  await expect(importButton).toHaveAttribute("aria-busy", "true");
  accountAdd.release();
  const result = section.getByRole("region", { name: "Import result" });

  await expect(result.getByRole("paragraph")).toHaveText([
    "Installed: pdf, review-kit",
    "Updated: docx",
    "Skipped: notes, drafts",
    `Couldn't reach ${TEAM}`,
  ]);
  // Import… waits for the inventory that follows the commands, then is free again.
  await cli.serveEmit(
    cli.requestIds.map((id) => ({
      ok: true,
      request_id: id,
      type: "refresh_complete" as const,
    }))
  );
  await expect(importButton).toBeEnabled();
  expect(later(cli)).toEqual([
    ["import", SNAPSHOT, "--yes", "--json"],
    ["source", "add", NEW_ACCOUNT, "--account", "--json"],
    ["source", "add", `${TEAM}#v2`, "--json"],
  ]);
  // A skipped skill carries the CLI's reason as its tooltip and description.
  const notes = result.getByText("notes", { exact: true });
  await expect(notes).toHaveAttribute("title", UNMANAGED_REASON);
  await expect(notes).toHaveAccessibleDescription(UNMANAGED_REASON);
});

test("an import that installed, updated and skipped nothing says so", async ({
  cli,
  page,
}) => {
  cli.answerOpenDialog(SNAPSHOT);
  cli.register(["import", SNAPSHOT, "--yes"], { events: nothingNew });
  await openSettings(cli, page);
  const section = importSettings(page);
  await section.getByRole("button", { name: "Import…" }).click();
  await page
    .getByRole("dialog", { name: "Replace settings with agentx-export.json?" })
    .getByRole("button", { name: "Import" })
    .click();
  const result = section.getByRole("region", { name: "Import result" });
  await expect(result.getByRole("paragraph")).toHaveText([
    "No skills were installed or updated.",
  ]);
  expect(later(cli)).toEqual([["import", SNAPSHOT, "--yes", "--json"]]);
});
