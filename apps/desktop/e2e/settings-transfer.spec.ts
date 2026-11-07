// The Another machine and Skills from the Skills CLI sections of Settings: Export runs
// `export <name>` (with --force after the overwrite confirm), Import runs `import <file> --yes` on
// the file the open dialog returns after its confirm, lists each `import_skill` and adds the
// sources this machine has not fetched one after another, and Adopt all runs `adopt --all`.
import type { Page } from "@playwright/test";

import {
  ACCOUNT,
  adoptedAll,
  adoptionPreview,
  alreadyThere,
  exported,
  imported,
  LIBRARY,
  NEW_ACCOUNT,
  SNAPSHOT,
  sourcesAdded,
  sourcesAfterImport,
  sourcesBefore,
  TEAM,
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
const anotherMachine = (page: Page) =>
  page.getByRole("region", { name: "Another machine" });
const skillsCli = (page: Page) =>
  page.getByRole("region", { name: "Skills from the Skills CLI" });

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
  await expect(anotherMachine(page)).toBeVisible();
};

/** The Tauri calls the page made beside the CLI's: only the open dialog may be one. */
const pluginCalls = (cli: MockCli) =>
  cli.calls.filter((c) => c.startsWith("plugin:"));

test("exports under the name given and toasts the path the CLI wrote", async ({
  cli,
  page,
}) => {
  cli.register(["export", "agentx-export"], { events: exported });
  await openSettings(cli, page);
  const section = anotherMachine(page);
  const name = section.getByRole("textbox", { name: "Snapshot name" });
  await expect(name).toHaveValue("agentx-export");
  await expect(section).toContainText(
    "Written to your library as agentx-export.json"
  );
  await section.getByRole("button", { name: "Export settings" }).click();
  await expect(toasts(page)).toContainText(
    `Exported to ${LIBRARY}/agentx-export.json`
  );
  expect(later(cli)).toEqual([["export", "agentx-export", "--json"]]);

  // A name export would refuse runs nothing.
  await name.fill("snapshot.json");
  await expect(section).toContainText(
    "Give a file name without an extension or a slash"
  );
  await expect(
    section.getByRole("button", { name: "Export settings" })
  ).toBeDisabled();
  await name.fill("work-laptop");
  await expect(section).toContainText(
    "Written to your library as work-laptop.json"
  );
  expect(later(cli)).toHaveLength(1);
  expect(pluginCalls(cli)).toEqual([]);
});

test("asks before it replaces a snapshot, then exports with --force", async ({
  cli,
  page,
}) => {
  cli.register(["export", "agentx-export"], {
    events: alreadyThere,
    exitCode: 6,
  });
  cli.register(["export", "agentx-export", "--force"], { events: exported });
  await openSettings(cli, page);
  await anotherMachine(page)
    .getByRole("button", { name: "Export settings" })
    .click();
  const confirm = page.getByRole("dialog", {
    name: "Replace agentx-export.json?",
  });
  await expect(confirm).toBeVisible();
  // The refusal is the dialog's to answer, so no error toast shows.
  await expect(toasts(page)).not.toContainText("is already there");
  expect(later(cli)).toEqual([["export", "agentx-export", "--json"]]);
  await confirm.getByRole("button", { name: "Replace" }).click();
  await expect(toasts(page)).toContainText(
    `Exported to ${LIBRARY}/agentx-export.json`
  );
  expect(later(cli)).toEqual([
    ["export", "agentx-export", "--json"],
    ["export", "agentx-export", "--force", "--json"],
  ]);
});

test("imports the picked file after its confirm, lists each skill and adds the sources not fetched here", async ({
  cli,
  page,
}) => {
  cli.answerOpenDialog(SNAPSHOT);
  cli.register(["import", SNAPSHOT, "--yes"], { events: imported });
  cli.register(["source", "add", NEW_ACCOUNT, "--account"], {
    events: [
      { account: true, id: "s-new-account", type: "source", url: NEW_ACCOUNT },
    ],
  });
  cli.register(["source", "add", `${TEAM}#v2`], {
    events: [{ id: "s-team", pin: "v2", type: "source", url: TEAM }],
  });
  await openSettings(cli, page);
  const section = anotherMachine(page);
  await section.getByRole("button", { name: "Import…" }).click();

  const confirm = page.getByRole("dialog", {
    name: "Replace this machine's settings with the ones in agentx-export.json?",
  });
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText(SNAPSHOT);
  expect(cli.openDialogs).toEqual([
    expect.objectContaining({ defaultPath: LIBRARY, multiple: false }),
  ]);
  // Nothing runs before the user confirms.
  expect(later(cli)).toEqual([]);

  // The imported settings name sources this machine has not fetched.
  cli.register(["source", "list"], { events: sourcesAfterImport });
  await confirm.getByRole("button", { name: "Import" }).click();
  await expect(toasts(page)).toContainText(
    "Imported the settings in agentx-export.json"
  );
  const result = section.getByRole("region", { name: "Import result" });
  await expect(result).toContainText("3 skills in agentx-export.json");
  await expect(
    result.getByRole("list", { name: "Imported skills" }).getByRole("listitem")
  ).toHaveText([
    "pdfinstalledhttps://github.com/vendor/skills/skills/pdf · New to this machine",
    "reviewkepthttps://github.com/me/work-account · Already here",
    "notesskippedNo source · New to this machine" +
      "notes is unmanaged, so there is no source to install it from",
  ]);
  await expect(
    result.getByRole("list", { name: "Sources to add" }).getByRole("listitem")
  ).toHaveText([`${NEW_ACCOUNT} (account remote)`, `${TEAM}#v2`]);
  expect(later(cli)).toContainEqual(["import", SNAPSHOT, "--yes", "--json"]);

  // Each source add reloads the sources; once all are fetched, nothing is left to add.
  cli.register(["source", "list"], { events: sourcesAdded });
  await result.getByRole("button", { name: "Add sources" }).click();
  await expect(
    result.getByRole("list", { name: "Sources to add" })
  ).toBeHidden();
  const adds = later(cli).filter((l) => l[0] === "source" && l[1] === "add");
  expect(adds).toEqual([
    ["source", "add", NEW_ACCOUNT, "--account", "--json"],
    ["source", "add", `${TEAM}#v2`, "--json"],
  ]);
  // The account remote this machine had is not touched by name: the CLI replaced it.
  expect(adds.flat()).not.toContain(ACCOUNT);

  // The app wrote no file and read none: its only call beside the CLI is the open dialog, and
  // the only path it passed on is the one the dialog returned.
  expect(pluginCalls(cli)).toEqual(["plugin:dialog|open"]);
  const paths = later(cli)
    .flat()
    .filter((arg) => arg.startsWith("/"));
  expect(paths).toEqual([SNAPSHOT]);
});

test("a cancelled open dialog runs nothing", async ({ cli, page }) => {
  cli.answerOpenDialog(null);
  await openSettings(cli, page);
  await anotherMachine(page).getByRole("button", { name: "Import…" }).click();
  await expect.poll(() => cli.openDialogs.length).toBe(1);
  await expect(page.getByRole("dialog")).toBeHidden();
  expect(later(cli)).toEqual([]);
});

test("Adopt all runs adopt --all with its progress and lists the refused record", async ({
  cli,
  page,
}) => {
  cli.register(["adopt"], { events: adoptionPreview });
  const run = cli.register(["adopt", "--all"], {
    events: adoptedAll,
    holdAfter: 1,
  });
  await openSettings(cli, page);
  const section = skillsCli(page);
  await expect(section).toContainText("2 can be adopted");
  await expect(section).toContainText("replaces the copy the Skills CLI left");
  await expect(
    section.getByRole("list", { name: "Can't adopt" }).getByRole("listitem")
  ).toHaveText([
    "local-notesit was installed from a local folder, which is no source agentx can read",
  ]);
  await section.getByRole("button", { name: "Adopt all" }).click();
  await expect(
    section.getByRole("button", { name: "Adopting 1 of 8" })
  ).toBeDisabled();
  run.release();
  await expect(toasts(page)).toContainText("Adopted 2 skills");
  expect(later(cli)).toEqual([["adopt", "--all", "--json"]]);
});

test("the Skills CLI section is hidden without candidates", async ({
  cli,
  page,
}) => {
  cli.register(["adopt"], {
    events: adoptionPreview.filter(
      (r) => r.type === "adoption" && r.state !== "candidate"
    ),
  });
  await openSettings(cli, page);
  await expect(
    page.getByRole("heading", { name: "Command line" })
  ).toBeVisible();
  await expect(skillsCli(page)).toHaveCount(0);
});
