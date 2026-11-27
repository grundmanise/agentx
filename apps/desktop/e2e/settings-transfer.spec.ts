// The Save snapshot and Apply snapshot sections of Settings, the last ones of the page: Save
// snapshot runs `snapshot save <name>` (with --force after the overwrite confirm), and Apply
// snapshot runs `snapshot apply <file> --yes` on the file the open dialog returns after its
// confirm, then `source add` for each source of the applied settings this machine has not
// downloaded, one after another and without asking, and shows what was installed, updated and
// skipped.
import type { Page } from "@playwright/test";

import {
  alreadyThere,
  exported,
  imported,
  LIBRARY,
  NEW_HUB,
  nothingNew,
  SNAPSHOT,
  sourcesAfterImport,
  sourcesBefore,
  TEAM,
  UNMANAGED_REASON,
  unreachable,
} from "./fixtures/settings-transfer.ts";
import { expect, firstInventory, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

/** The read of the store and its size, which Settings runs each time it shows. */
const storeLines = [["machine", "--json"]];

/** The command lines the page ran after the launch, the loads and the read of the store. */
const later = (cli: MockCli) =>
  cli.commands.filter(
    (l) =>
      ![...launchLines, ...loadLines, ...storeLines].some(
        (known) => known.join(" ") === l.join(" ")
      )
  );

const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const saveSnapshot = (page: Page) =>
  page.getByRole("form", { name: "Save snapshot" });
const applySnapshot = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Snapshot" });

const openSettings = async (cli: MockCli, page: Page) => {
  cli.onServeStart({
    events: [{ ...firstInventory(), sources: sourcesBefore }],
  });
  await page.goto("/#/settings");
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  await expect(saveSnapshot(page)).toBeVisible();
  await expect(applySnapshot(page)).toBeVisible();
};

test("saves under the name given and toasts the path the CLI wrote", async ({
  cli,
  page,
}) => {
  cli.register(["snapshot", "save", "agentx-snapshot"], { events: exported });
  await openSettings(cli, page);
  const section = saveSnapshot(page);
  await expect(
    page.getByRole("region", { exact: true, name: "Snapshot" })
  ).toContainText(
    "Save your settings and library to back up or move to another machine."
  );
  const name = section.getByRole("textbox", { name: "File name" });
  await expect(name).toHaveValue("agentx-snapshot");
  await section.getByRole("button", { exact: true, name: "Save" }).click();
  await expect(toasts(page)).toContainText(
    `Saved snapshot to ${LIBRARY}/agentx-snapshot.json`
  );
  await expect(
    toasts(page).getByRole("button", { exact: true, name: "Show" })
  ).toBeVisible();
  expect(later(cli)).toEqual([
    ["snapshot", "save", "agentx-snapshot", "--json"],
  ]);

  // A name snapshot save would refuse disables Save.
  await name.fill("snapshot.json");
  await expect(section).toContainText(
    "Enter a file name without an extension or a slash"
  );
  await expect(
    section.getByRole("button", { exact: true, name: "Save" })
  ).toBeDisabled();
  await name.fill("work-laptop");
  await expect(
    section.getByRole("button", { exact: true, name: "Save" })
  ).toBeEnabled();
});

test("a failed save shows the CLI's message and hint under the field, as one paragraph", async ({
  cli,
  page,
}) => {
  cli.register(["snapshot", "save", "agentx-snapshot"], {
    events: [
      {
        code: "internal",
        hint: "check that you can write to the library",
        message: `can't write ${LIBRARY}/agentx-snapshot.json: permission denied`,
        type: "error",
      },
    ],
    exitCode: 10,
  });
  await openSettings(cli, page);
  const section = saveSnapshot(page);
  await section.getByRole("button", { exact: true, name: "Save" }).click();
  // The message and the hint as one sentence-cased paragraph.
  await expect(section.getByRole("alert")).toHaveText(
    `Can't write ${LIBRARY}/agentx-snapshot.json: permission denied. Check that you can write to the library.`
  );
  expect(later(cli)).toEqual([
    ["snapshot", "save", "agentx-snapshot", "--json"],
  ]);
});

test("asks before it replaces a file in the library, then saves with --force", async ({
  cli,
  page,
}) => {
  cli.register(["snapshot", "save", "agentx-snapshot"], {
    events: alreadyThere,
    exitCode: 6,
  });
  cli.register(["snapshot", "save", "agentx-snapshot", "--force"], {
    events: exported,
  });
  await openSettings(cli, page);
  await saveSnapshot(page)
    .getByRole("button", { exact: true, name: "Save" })
    .click();
  const confirm = page.getByRole("alertdialog", {
    name: "Replace agentx-snapshot.json?",
  });
  await expect(confirm).toBeVisible();
  await expect(confirm).toContainText(
    "A file with this name is already in your library."
  );
  expect(later(cli)).toEqual([
    ["snapshot", "save", "agentx-snapshot", "--json"],
  ]);
  await confirm.getByRole("button", { name: "Replace" }).click();
  await expect(toasts(page)).toContainText(
    `Saved snapshot to ${LIBRARY}/agentx-snapshot.json`
  );
  expect(later(cli)).toEqual([
    ["snapshot", "save", "agentx-snapshot", "--json"],
    ["snapshot", "save", "agentx-snapshot", "--force", "--json"],
  ]);
});

test("applies the picked snapshot after its confirm, adds the sources it names without asking and shows what changed", async ({
  cli,
  page,
}) => {
  cli.answersRefreshes = false;
  cli.answerOpenDialog(SNAPSHOT);
  cli.register(["snapshot", "apply", SNAPSHOT, "--yes"], { events: imported });
  const hubAdd = cli.register(["source", "add", NEW_HUB, "--hub"], {
    events: [{ hub: true, id: "s-new-hub", type: "source", url: NEW_HUB }],
    holdAfter: 0,
  });
  cli.register(["source", "add", `${TEAM}#v2`], {
    events: unreachable,
    exitCode: 3,
  });
  await openSettings(cli, page);
  const section = applySnapshot(page);
  await expect(section).toContainText(
    "Load settings and skills from a snapshot."
  );
  const applyButton = section.getByRole("button", { name: "Apply…" });
  await applyButton.click();

  const confirm = page.getByRole("alertdialog", {
    name: "Apply this snapshot?",
  });
  await expect(confirm).toBeVisible();
  await expect(confirm.getByRole("paragraph")).toHaveText([
    "This will replace your current settings, install missing skills and may update existing skills.",
    SNAPSHOT,
  ]);
  expect(cli.openDialogs).toEqual([
    expect.objectContaining({ defaultPath: LIBRARY, multiple: false }),
  ]);

  await confirm.getByRole("button", { name: "Apply" }).click();
  // The inventory after the apply lists the sources the applied settings name, which this machine
  // has not downloaded: none is added before it comes.
  await expect.poll(() => cli.serveRequests.length).toBe(1);
  expect(later(cli)).toEqual([
    ["snapshot", "apply", SNAPSHOT, "--yes", "--json"],
  ]);
  await cli.serveEmit([
    { ...firstInventory(), scan_counter: 2, sources: sourcesAfterImport },
    {
      ok: true,
      request_id: cli.requestIds.at(-1) ?? "",
      scan_counter: 2,
      type: "refresh_complete",
    },
  ]);
  await expect(toasts(page)).toContainText(
    "Applied snapshot agentx-snapshot.json"
  );
  cli.answersRefreshes = true;

  // The sources are added one after another, with no click, and Apply… waits for the last.
  await expect
    .poll(() => later(cli))
    .toEqual([
      ["snapshot", "apply", SNAPSHOT, "--yes", "--json"],
      ["source", "add", NEW_HUB, "--hub", "--json"],
    ]);
  await expect(applyButton).toBeDisabled();
  await expect(applyButton).toHaveAttribute("aria-busy", "true");
  hubAdd.release();
  const result = section.getByRole("region", { name: "Apply result" });

  await expect(result.getByRole("paragraph")).toHaveText([
    "Installed: pdf, review-kit",
    "Updated: docx",
    "Skipped: notes, drafts",
    `Couldn't reach ${TEAM}`,
  ]);
  await expect(applyButton).toBeEnabled();
  expect(later(cli)).toEqual([
    ["snapshot", "apply", SNAPSHOT, "--yes", "--json"],
    ["source", "add", NEW_HUB, "--hub", "--json"],
    ["source", "add", `${TEAM}#v2`, "--json"],
  ]);
  // A skipped skill carries the CLI's reason as its tooltip and description.
  const notes = result.getByText("notes", { exact: true });
  await expect(notes).toHaveAttribute("title", UNMANAGED_REASON);
  await expect(notes).toHaveAccessibleDescription(UNMANAGED_REASON);
});

test("a snapshot that installed, updated and skipped nothing says so", async ({
  cli,
  page,
}) => {
  cli.answerOpenDialog(SNAPSHOT);
  cli.register(["snapshot", "apply", SNAPSHOT, "--yes"], {
    events: nothingNew,
  });
  await openSettings(cli, page);
  const section = applySnapshot(page);
  await section.getByRole("button", { name: "Apply…" }).click();
  await page
    .getByRole("alertdialog", {
      name: "Apply this snapshot?",
    })
    .getByRole("button", { name: "Apply" })
    .click();
  const result = section.getByRole("region", { name: "Apply result" });
  await expect(result.getByRole("paragraph")).toHaveText([
    "No skills were installed or updated.",
  ]);
  expect(later(cli)).toEqual([
    ["snapshot", "apply", SNAPSHOT, "--yes", "--json"],
  ]);
});
