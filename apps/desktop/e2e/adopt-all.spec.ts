// Adopt all, above the Skills list: one line per tool whose skills the adoption preview offers,
// each running `adopt <tool> --all` with its progress; the toasts of a run that adopted every
// skill, some or none; a skill the run could not adopt staying adoptable with the CLI's reason;
// and no adoption section in Settings.
import type { Page } from "@playwright/test";

import {
  inventoryWith,
  MOVED,
  movedReason,
  nothingToAdopt,
  previewWith,
  skillsCliNames,
  skillsCliNoneRun,
  skillsCliRun,
  skillshareNames,
  skillshareRun,
} from "./fixtures/adopt-all.ts";
import { fileReads, filesOf, history } from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { InlineEvent, MockCli } from "./harness/cli.ts";

const column = (page: Page) =>
  page.getByRole("region", { name: "Skills list" });
const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
/** The Adopt all line of a tool, named by its text. */
const line = (page: Page, text: string) =>
  column(page).getByRole("group", { exact: true, name: text });
const adoptAllButtons = (page: Page) =>
  page.getByRole("button", { name: /^Adopt(?:ing)? /u });
const toasts = (page: Page) =>
  page.getByRole("region", { name: /^Notifications/u });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });

/** The command lines the screen ran: everything but the launch, the store's and the pane's reads. */
const ran = (cli: MockCli) =>
  cli.commands.filter(
    (l) =>
      !["version", "doctor"].includes(l[0] ?? "") &&
      !loadLines.some((known) => known.join(" ") === l.join(" ")) &&
      !["skill files", "skill diff", "skill history"].includes(
        l.slice(0, 2).join(" ")
      )
  );

/** Opens the Skills screen on the machine with skills of both tools to adopt. */
const openSkills = async (
  cli: MockCli,
  page: Page,
  preview = previewWith(),
  path = "/#/skills"
) => {
  cli.onServeStart({ events: [inventoryWith()] });
  cli.register(["adopt"], { events: preview });
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
  await page.goto(path);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await expect(row(page, "pdf")).toBeVisible();
};

/** The toast that says `text`. */
const toast = (page: Page, text: string) =>
  toasts(page).getByRole("listitem").filter({ hasText: text });

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

test("each tool with skills to adopt has its own line, counting only its skills", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page);
  // The Skills CLI's refused record is not counted: Adopt all would not adopt it.
  const skillsCli = line(page, "3 skills added by Skills CLI");
  const skillshare = line(page, "2 skills added by Skillshare");
  await expect(skillsCli).toBeVisible();
  await expect(skillshare).toBeVisible();
  await expect(
    column(page).getByRole("group").filter({ hasText: "added by" })
  ).toHaveCount(2);
  await expect(skillsCli.getByRole("button")).toHaveText("Adopt all");
  await expect(skillshare.getByRole("button")).toHaveText("Adopt all");
  // The lines sit above the list, not among its rows.
  await expect(
    list(page).getByRole("button", { name: "Adopt all" })
  ).toHaveCount(0);
  expect(ran(cli)).toEqual([]);
});

test("a tool with one skill to adopt says so in the singular, and one with none has no line", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, previewWith(["deck-outline", MOVED, "api-docs"]));
  await expect(line(page, "1 skill added by Skills CLI")).toBeVisible();
  await expect(line(page, "1 skill added by Skillshare")).toBeVisible();
});

test("with nothing to adopt there is no line", async ({ cli, page }) => {
  await openSkills(cli, page, nothingToAdopt);
  await expect(row(page, "legacy")).toBeVisible();
  await expect(page.getByText(/added by/u)).toHaveCount(0);
  await expect(adoptAllButtons(page)).toHaveCount(0);
});

test("Adopt all runs adopt skillshare --all with its progress, and the line goes once every skill is adopted", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page);
  const run = cli.register(["adopt", "skillshare", "--all"], {
    events: skillshareRun,
    holdAfter: 1,
  });
  await line(page, "2 skills added by Skillshare")
    .getByRole("button", { name: "Adopt all" })
    .click();
  await run.started;
  const running = line(page, "2 skills added by Skillshare").getByRole(
    "button"
  );
  await expect(running).toHaveText("Adopting 1 of 7");
  await expect(running).toBeDisabled();
  await expect(running).toHaveAttribute("aria-busy", "true");
  // One run at a time: the other line waits.
  await expect(
    line(page, "3 skills added by Skills CLI").getByRole("button", {
      name: "Adopt all",
    })
  ).toBeDisabled();
  cli.register(["adopt"], { events: previewWith(skillshareNames) });
  run.release();
  const adopted = toast(page, "Adopted 2 skills");
  await expect(adopted).toBeVisible();
  // Every skill adopted: a plain toast, with nothing to dismiss.
  await expect(adopted.getByRole("button", { name: "Dismiss" })).toHaveCount(0);
  await afterRun(cli, skillshareNames);
  await expect(page.getByText(/added by Skillshare/u)).toHaveCount(0);
  await expect(
    line(page, "3 skills added by Skills CLI").getByRole("button", {
      name: "Adopt all",
    })
  ).toBeEnabled();
  expect(ran(cli)).toEqual([["adopt", "skillshare", "--all", "--json"]]);
});

test("a skill the run could not adopt stays adoptable, with the CLI's reason in its Details", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, previewWith(), `/#/skills/${MOVED}`);
  const run = cli.register(["adopt", "skills-cli", "--all"], {
    events: skillsCliRun,
    exitCode: 5,
    holdAfter: 0,
  });
  await line(page, "3 skills added by Skills CLI")
    .getByRole("button", { name: "Adopt all" })
    .click();
  await run.started;
  await expect(
    line(page, "3 skills added by Skills CLI").getByRole("button")
  ).toHaveText("Adopting…");
  cli.register(["adopt"], {
    events: previewWith(["deck-outline", "scratch"]),
  });
  run.release();
  // Of the three skills the line offered, one could not be adopted: legacy, which the preview
  // refused already, is not counted. An attention toast, which waits to be dismissed.
  const partial = toast(page, "Adopted 2 skills. 1 could not be adopted");
  await expect(partial).toBeVisible();
  await expect(partial.getByRole("button", { name: "Dismiss" })).toBeVisible();
  await afterRun(cli, ["deck-outline", "scratch"]);
  await expect(line(page, "1 skill added by Skills CLI")).toBeVisible();
  await expect(row(page, MOVED)).toBeVisible();
  const article = pane(page, MOVED);
  const banner = article.getByRole("note", { name: "Added by Skills CLI" });
  await expect(banner.getByRole("button", { name: "Adopt" })).toBeVisible();
  const details = article.getByRole("region", { name: "Details" });
  await expect(details.getByRole("term").last()).toHaveText("Adopt");
  await expect(details.getByRole("definition").last()).toHaveText(
    `Can't adopt: ${movedReason}`
  );
  expect(ran(cli)).toEqual([["adopt", "skills-cli", "--all", "--json"]]);
});

test("a run that adopts nothing says so", async ({ cli, page }) => {
  await openSkills(cli, page);
  cli.register(["adopt", "skills-cli", "--all"], {
    events: skillsCliNoneRun,
    exitCode: 5,
  });
  await line(page, "3 skills added by Skills CLI")
    .getByRole("button", { name: "Adopt all" })
    .click();
  const none = toast(page, "No skills could be adopted");
  await expect(none).toBeVisible();
  await expect(none.getByRole("button", { name: "Dismiss" })).toBeVisible();
  await expect(line(page, "3 skills added by Skills CLI")).toBeVisible();
  expect(ran(cli)).toEqual([["adopt", "skills-cli", "--all", "--json"]]);
});

test("Settings has no adoption section", async ({ cli, page }) => {
  cli.register(["adopt"], { events: previewWith() });
  await page.goto("/#/settings");
  await expect(
    page.getByRole("heading", { level: 1, name: "Settings" })
  ).toBeVisible();
  await expect(
    page.getByRole("main").getByRole("region", { name: /^Command line/u })
  ).toBeVisible();
  await expect(page.getByRole("main")).not.toContainText(/adopt/iu);
  await expect(adoptAllButtons(page)).toHaveCount(0);
});
