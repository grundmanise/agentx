// Adopting another tool's skills all at once: the Adopt skills section of Settings and the
// Unmanaged group's Adopt all, both opening the adopt dialog. The dialog lists the skills before
// the run (the ones the preview refuses marked, with the reason), runs `adopt <tool> --all` for
// each tool in turn with each row's state and one progress line, then shows each row's result and
// a summary. A run that fails as a whole says why in the dialog.
import type { Locator, Page } from "@playwright/test";

import {
  inventoryWith,
  MOVED,
  nothingToAdopt,
  previewWith,
  skillsCliAllRun,
  skillsCliNames,
  skillsCliNoneRun,
  skillsCliRun,
  skillshareNames,
  skillshareRun,
} from "./fixtures/adopt-all.ts";
import { fileReads, filesOf, history } from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const unmanaged = (page: Page) =>
  list(page).getByRole("region", { name: /^UNMANAGED/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const settingsSection = (page: Page) =>
  page.getByRole("region", { exact: true, name: "Adopt skills" });
const dialog = (page: Page) => page.getByRole("dialog");
/** The rows of the dialog's list of `tool`, each read as "{state}: {name}" then any reason. */
const rows = (where: Locator, tool: string) =>
  where
    .getByRole("region", { name: "Skills" })
    .getByRole("region", { name: tool })
    .getByRole("listitem");
const progress = (where: Locator) => where.getByRole("status");

/** The command lines the screens ran: everything but the launch, the store's and the pane's reads. */
const ran = (cli: MockCli) =>
  cli.commands.filter(
    (l) =>
      !["version", "doctor"].includes(l[0] ?? "") &&
      !loadLines.some((known) => known.join(" ") === l.join(" ")) &&
      !["skill files", "skill diff", "skill history"].includes(
        l.slice(0, 2).join(" ")
      )
  );

/** The activity list's entries, tone and message, read through the store harness. */
const activity = async (page: Page) => {
  if (!(await page.evaluate(() => "store" in window))) {
    await page.addScriptTag({ type: "module", url: "/e2e/harness/store.tsx" });
    await page.waitForFunction(() => "store" in window);
  }
  return await page.evaluate(() =>
    window.store
      .state()
      .activity.map(({ message, tone }) => ({ message, tone }))
  );
};

/** Registers the reads the Skills screen runs for the skills it may select. */
const registerReads = (cli: MockCli) => {
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  cli.register(["skill", "history", "release-notes"]);
  cli.register(["skill", "history", "draft"]);
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  for (const name of [...skillsCliNames, ...skillshareNames, "legacy"]) {
    cli.register(["skill", "files", name], { events: filesOf(name) });
  }
  cli.register(["skill", "diff", "review"]);
  cli.register(["skill", "diff", "release-notes"]);
  cli.register(["skill", "diff", "pdf-letters"]);
};

/** Opens the Skills screen on the machine with skills of both tools to adopt. */
const openSkills = async (
  cli: MockCli,
  page: Page,
  preview = previewWith(),
  path = "/#/skills"
) => {
  cli.onServeStart({ events: [inventoryWith()] });
  cli.register(["adopt"], { events: preview });
  registerReads(cli);
  await page.goto(path);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await expect(list(page).getByRole("region").first()).toBeVisible();
};

/** Opens Settings on that machine. */
const openSettings = async (
  cli: MockCli,
  page: Page,
  preview = previewWith()
) => {
  cli.onServeStart({ events: [inventoryWith()] });
  cli.register(["adopt"], { events: preview });
  await page.goto("/#/settings");
  await expect(settingsSection(page)).toBeVisible();
};

/** The inventory that follows a run that adopted `managed`, and the answer to its refresh. */
const afterRun = async (cli: MockCli, managed: string[]) => {
  await expect.poll(() => cli.requestIds.length).toBeGreaterThan(0);
  await cli.serveEmit([inventoryWith(managed)]);
  await cli.serveEmit([
    {
      ok: true,
      request_id: cli.requestIds.at(-1) ?? "",
      scan_counter: managed.length + 1,
      type: "refresh_complete",
    } satisfies InlineEvent,
  ]);
};

test.describe("the Adopt skills section of Settings", () => {
  test("has a button for each tool that has skills to adopt", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    await expect(settingsSection(page).getByRole("button")).toHaveText([
      "Adopt Skills CLI skills",
      "Adopt Skillshare skills",
    ]);
  });

  test("shows only the tool that has skills to adopt", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page, previewWith(skillshareNames));
    await expect(settingsSection(page).getByRole("button")).toHaveText([
      "Adopt Skills CLI skills",
    ]);
  });

  test("keeps its line when no tool has skills to adopt", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page, nothingToAdopt);
    const section = settingsSection(page);
    await expect(section).toContainText(
      "Let agentx manage the skills another tool installed."
    );
    await expect(section).toContainText(
      "No skills from the Skills CLI or Skillshare to adopt"
    );
  });

  test("the dialog lists the tool's skills, the refused one with its reason, and Cancel closes it", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    await settingsSection(page)
      .getByRole("button", { name: "Adopt Skills CLI skills" })
      .click();
    const open = dialog(page);
    await expect(open).toHaveAccessibleName("Adopt Skills CLI skills");
    await expect(open.getByRole("paragraph")).toHaveText([
      "agentx will import these 3 skills and manage them from now on. Skills CLI keeps its own files. Edits you made to the copies in your library are not kept. 1 skill can't be adopted and is left out.",
      "legacy is a symlink, not a directory.",
    ]);
    await expect(rows(open, "Skills CLI")).toHaveText([
      "Waiting: commit-style",
      "Waiting: deck-outline",
      "Waiting: scratch",
      "Can't be adopted: legacylegacy is a symlink, not a directory.",
    ]);
    await open.getByRole("button", { name: "Cancel" }).click();
    await expect(open).toHaveCount(0);
  });

  test("the dialog for one skill says so", async ({ cli, page }) => {
    await openSettings(cli, page, previewWith(["api-docs"]));
    await settingsSection(page)
      .getByRole("button", { name: "Adopt Skillshare skills" })
      .click();
    const open = dialog(page);
    await expect(open).toHaveAccessibleDescription(
      "agentx will import this skill and manage it from now on. Skillshare keeps its own files. Edits you made to the copies in your library are not kept."
    );
    await expect(rows(open, "Skillshare")).toHaveText([
      "Waiting: frontend__ui",
    ]);
  });

  test("Adopt runs in the dialog: each row's state, the progress, then each result", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    const run = cli.register(["adopt", "skills-cli", "--all"], {
      events: skillsCliRun,
      exitCode: 5,
      // The fetch of the source, then deck-outline's first two steps: the fetch is no skill.
      holdAfter: 3,
    });
    await settingsSection(page)
      .getByRole("button", { name: "Adopt Skills CLI skills" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { name: "Adopt" }).click();
    await run.started;
    await expect(progress(open)).toHaveText("Adopting 1 of 3");
    await expect(rows(open, "Skills CLI")).toHaveText([
      "Waiting: commit-style",
      "Adopting: deck-outline",
      "Waiting: scratch",
      "Can't be adopted: legacylegacy is a symlink, not a directory.",
    ]);
    const adopt = open.getByRole("button", { name: "Adopt" });
    await expect(adopt).toBeDisabled();
    await expect(adopt).toHaveAttribute("aria-busy", "true");
    await expect(open.getByRole("button", { name: "Cancel" })).toBeDisabled();
    // It cannot be dismissed while it runs.
    await page.keyboard.press("Escape");
    await expect(open).toBeVisible();
    // The button behind it, hidden by the dialog, waits too.
    await expect(
      page
        .getByRole("region", {
          exact: true,
          includeHidden: true,
          name: "Adopt skills",
        })
        .getByRole("button", {
          includeHidden: true,
          name: "Adopt Skillshare skills",
        })
    ).toBeDisabled();

    cli.register(["adopt"], {
      events: previewWith(["deck-outline", "scratch"]),
    });
    run.release();
    await expect(progress(open)).toHaveText("Adopted 2 of 3 skills");
    await expect(rows(open, "Skills CLI")).toHaveText([
      "Not adopted: commit-stylehttps://github.com/vercel/skills holds no skill at commit-style now.",
      "Adopted: deck-outline",
      "Adopted: scratch",
      "Can't be adopted: legacylegacy is a symlink, not a directory.",
    ]);
    // The activity list logs the result.
    expect(await activity(page)).toEqual([
      { message: "Adopted 2 of 3 skills", tone: "attention" },
    ]);
    await open.getByRole("button", { name: "Done" }).click();
    await expect(open).toHaveCount(0);
    expect(ran(cli)).toEqual([["adopt", "skills-cli", "--all", "--json"]]);
  });

  test("a tool's button goes once its skills are adopted", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    cli.register(["adopt", "skillshare", "--all"], { events: skillshareRun });
    cli.register(["adopt"], { events: previewWith(skillshareNames) });
    await settingsSection(page)
      .getByRole("button", { name: "Adopt Skillshare skills" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { name: "Adopt" }).click();
    await expect(progress(open)).toHaveText("Adopted 2 skills");
    await expect(rows(open, "Skillshare")).toHaveText([
      "Adopted: api-docs",
      "Adopted: frontend__ui",
    ]);
    expect(await activity(page)).toEqual([
      { message: "Adopted 2 skills", tone: "success" },
    ]);
    await open.getByRole("button", { name: "Done" }).click();
    await expect(settingsSection(page).getByRole("button")).toHaveText([
      "Adopt Skills CLI skills",
    ]);
  });

  test("a run that adopts none says so in the dialog", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    cli.register(["adopt", "skills-cli", "--all"], {
      events: skillsCliNoneRun,
      exitCode: 5,
    });
    await settingsSection(page)
      .getByRole("button", { name: "Adopt Skills CLI skills" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { name: "Adopt" }).click();
    await expect(progress(open)).toHaveText("No skills were adopted");
    await expect(rows(open, "Skills CLI").first()).toHaveText(
      "Not adopted: commit-stylehttps://github.com/vercel/skills holds no skill at commit-style now."
    );
    expect(await activity(page)).toEqual([
      { message: "No skills were adopted", tone: "attention" },
    ]);
  });

  test("a run that fails before any skill says why in the dialog and keeps the rows", async ({
    cli,
    page,
  }) => {
    await openSettings(cli, page);
    cli.register(["adopt", "skillshare", "--all"], {
      events: [
        {
          hint: "fix the file, or move it aside",
          message: "the store of Skillshare is not JSON",
          type: "error",
        },
      ],
      exitCode: 10,
    });
    await settingsSection(page)
      .getByRole("button", { name: "Adopt Skillshare skills" })
      .click();
    const open = dialog(page);
    await open.getByRole("button", { name: "Adopt" }).click();
    await expect(open.getByRole("alert")).toHaveText(
      "The store of Skillshare is not JSON. Fix the file, or move it aside."
    );
    await expect(rows(open, "Skillshare")).toHaveText([
      "Waiting: api-docs",
      "Waiting: frontend__ui",
    ]);
    expect(await activity(page)).toEqual([
      { message: "the store of Skillshare is not JSON", tone: "danger" },
    ]);
    await open.getByRole("button", { name: "Done" }).click();
    await expect(open).toHaveCount(0);
  });
});

test.describe("Adopt all in the Unmanaged header", () => {
  test("covers both tools, grouped, and runs them one after the other with one progress", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page);
    const first = cli.register(["adopt", "skills-cli", "--all"], {
      events: skillsCliAllRun,
      exitCode: 6,
      // The fetch alone: no skill has started.
      holdAfter: 1,
    });
    const second = cli.register(["adopt", "skillshare", "--all"], {
      events: skillshareRun,
      // Its fetch and api-docs' first step.
      holdAfter: 2,
    });
    await unmanaged(page).getByRole("button", { name: "Adopt all" }).click();
    const open = dialog(page);
    await expect(open).toHaveAccessibleName(
      "Adopt skills from Skills CLI and Skillshare"
    );
    await expect(open).toHaveAccessibleDescription(
      "agentx will import these 5 skills and manage them from now on. Skills CLI and Skillshare keep their own files. Edits you made to the copies in your library are not kept. 1 skill can't be adopted and is left out."
    );
    await expect(open.getByRole("heading", { level: 3 })).toHaveText([
      "Skills CLI",
      "Skillshare",
    ]);
    await expect(rows(open, "Skillshare")).toHaveText([
      "Waiting: api-docs",
      "Waiting: frontend__ui",
    ]);
    await open.getByRole("button", { name: "Adopt" }).click();
    await first.started;
    await expect(progress(open)).toHaveText("Adopting…");
    cli.register(["adopt"], {
      events: previewWith([...skillsCliNames, ...skillshareNames]),
    });
    first.release();
    await second.started;
    // Three of the Skills CLI's, then the first of Skillshare's: one count over both.
    await expect(progress(open)).toHaveText("Adopting 4 of 5");
    await expect(rows(open, "Skills CLI").first()).toHaveText(
      "Adopted: commit-style"
    );
    await expect(rows(open, "Skillshare")).toHaveText([
      "Adopting: api-docs",
      "Waiting: frontend__ui",
    ]);
    second.release();
    await expect(progress(open)).toHaveText("Adopted 5 skills");
    // The Skills CLI's run exits 6 for the record the preview refused: still a success.
    expect(await activity(page)).toEqual([
      { message: "Adopted 2 skills", tone: "success" },
      { message: "Adopted 3 skills", tone: "success" },
    ]);
    await open.getByRole("button", { name: "Done" }).click();
    await afterRun(cli, [...skillsCliNames, ...skillshareNames]);
    await expect(page.getByRole("button", { name: "Adopt all" })).toHaveCount(
      0
    );
    expect(ran(cli)).toEqual([
      ["adopt", "skills-cli", "--all", "--json"],
      ["adopt", "skillshare", "--all", "--json"],
    ]);
  });

  test("a skill the run could not adopt keeps its note and details on its own detail pane", async ({
    cli,
    page,
  }) => {
    await openSkills(cli, page, previewWith(), `/#/skills/${MOVED}`);
    cli.register(["adopt", "skills-cli", "--all"], {
      events: skillsCliRun,
      exitCode: 5,
    });
    cli.register(["adopt", "skillshare", "--all"], { events: skillshareRun });
    cli.register(["adopt"], {
      events: previewWith(["deck-outline", "scratch", ...skillshareNames]),
    });
    await unmanaged(page).getByRole("button", { name: "Adopt all" }).click();
    const open = dialog(page);
    await open.getByRole("button", { name: "Adopt" }).click();
    await expect(progress(open)).toHaveText("Adopted 4 of 5 skills");
    await open.getByRole("button", { name: "Done" }).click();
    await afterRun(cli, ["deck-outline", "scratch", ...skillshareNames]);
    const article = pane(page, MOVED);
    await expect(
      article.getByRole("note", { name: "Added by Skills CLI" })
    ).toBeVisible();
    const details = article.getByRole("region", { name: "Details" });
    await expect(details.getByRole("term")).toHaveText([
      "Found at",
      "Installed by",
    ]);
  });
});
