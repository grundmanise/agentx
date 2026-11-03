import type { Locator, Page } from "@playwright/test";

import {
  adoption,
  fileReads,
  history,
  inventory,
  releaseNotesDiffs,
  reviewDiffs,
} from "./fixtures/skills.ts";
import { expect, loadLines, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const launchLines = [
  ["version", "--json"],
  ["doctor", "--json"],
];

const isLaunchOrLoad = (line: string[]) =>
  [...launchLines, ...loadLines].some(
    (known) => known.join(" ") === line.join(" ")
  );

/** The command lines the Skills screen ran, but the history reads, which e2e/skills.spec.ts covers. */
const later = (cli: MockCli) =>
  cli.commands.filter((l) => !isLaunchOrLoad(l) && l[1] !== "history");

/** `later`, in a fixed order: the two reads of a modified skill run at once. */
const sortedLater = (cli: MockCli) => {
  const lines = later(cli).map((l) => l.join(" "));
  lines.sort((a, b) => a.localeCompare(b));
  return lines;
};

const list = (page: Page) => page.getByRole("navigation", { name: "Skills" });
const row = (page: Page, name: string) =>
  list(page).getByRole("button", { exact: true, name });
const pane = (page: Page, name: string) =>
  page
    .getByRole("region", { name: "Skill details" })
    .getByRole("article", { name });
const files = (page: Page, name: string) =>
  pane(page, name).getByRole("region", { name: "Files" });
/** The row of one file in the FILES list. */
const fileRow = (where: Locator, path: string) =>
  where.getByRole("button", { exact: true, name: path });
/** The open pane of one file. */
const content = (where: Locator, path: string) =>
  where.getByRole("region", { exact: true, name: path });
const rowNames = async (where: Locator) =>
  await where
    .getByRole("button")
    .evaluateAll((buttons) => buttons.map((b) => b.getAttribute("aria-label")));

/** Opens the Skills screen on the fixture machine at `path`, with every read registered. */
const openSkills = async (cli: MockCli, page: Page, path: string) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["adopt"], { events: adoption });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  cli.register(["skill", "history", "release-notes"], { events: [] });
  cli.register(["skill", "history", "draft"], { events: [] });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
  cli.register(["skill", "diff", "review"], { events: reviewDiffs });
  cli.register(["skill", "diff", "release-notes"], {
    events: releaseNotesDiffs,
  });
  await page.goto(path);
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  // The store harness shows the activity list, which no screen does yet.
  await page.addScriptTag({ type: "module", url: "/e2e/harness/store.tsx" });
  await page.waitForFunction(() => "store" in window);
};

test.afterEach(async ({ cli, page }) => {
  // Reads only: no refresh went to serve, and the activity list has no entry.
  expect(cli.serveRequests.filter((l) => l.includes('"refresh"'))).toEqual([]);
  await expect(
    page.getByRole("list", { name: "Activity" }).getByRole("listitem")
  ).toHaveCount(0);
});

test("an unmodified skill: every file, SKILL.md first and open, the rest by path", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  const section = files(page, "pdf");
  await expect(fileRow(section, "SKILL.md")).toHaveAttribute(
    "aria-expanded",
    "true"
  );
  expect(later(cli)).toEqual([["skill", "files", "pdf", "--json"]]);
  await expect(section).toContainText("Read-only");
  await expect(section).not.toContainText("Compared with");
  expect(await rowNames(section)).toEqual([
    "SKILL.md",
    "assets/logo.png",
    "reference.pdf",
    "reference/forms.md",
  ]);
  await expect(fileRow(section, "SKILL.md")).toContainText("6 lines");
  await expect(fileRow(section, "assets/logo.png")).toContainText("binary");
  await expect(fileRow(section, "reference.pdf")).toContainText(
    "Too large to show (303 KiB)"
  );
  await expect(fileRow(section, "reference/forms.md")).toContainText("4 lines");
  await expect(fileRow(section, "reference/forms.md")).toHaveAttribute(
    "aria-expanded",
    "false"
  );

  // SKILL.md is open and shown as it is: nothing added or removed.
  const skillMd = content(section, "SKILL.md");
  await expect(skillMd).toBeVisible();
  await expect(skillMd).toContainText("name: pdf");
  await expect(skillMd).toContainText("Use this skill to pdf.");
  await expect(skillMd.getByRole("insertion")).toHaveCount(0);
  await expect(skillMd.getByRole("deletion")).toHaveCount(0);

  // Another file opens beside it; the binary and the oversized file show their placeholders.
  await fileRow(section, "reference/forms.md").click();
  await expect(content(section, "reference/forms.md")).toContainText(
    "Fill in every field."
  );
  await expect(skillMd).toBeVisible();
  await fileRow(section, "assets/logo.png").click();
  await expect(content(section, "assets/logo.png")).toContainText(
    "Binary file (8 KiB)"
  );
  await fileRow(section, "reference.pdf").click();
  await expect(content(section, "reference.pdf")).toContainText(
    "Too large to show (303 KiB)"
  );
  await fileRow(section, "SKILL.md").click();
  await expect(skillMd).toBeHidden();

  // The files were read once: selecting the skill again does not read them again.
  await row(page, "gone").click();
  await expect(files(page, "gone")).toBeVisible();
  await row(page, "pdf").click();
  await expect(fileRow(files(page, "pdf"), "SKILL.md")).toBeVisible();
  expect(later(cli)).toEqual([
    ["skill", "files", "pdf", "--json"],
    ["skill", "files", "gone", "--json"],
  ]);
});

test("a modified skill of a source: its edits labelled and tinted, against the version installed", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/review");
  const section = files(page, "review");
  await expect(section).toContainText(
    "Compared with the version you installed"
  );
  expect(sortedLater(cli)).toEqual([
    "skill diff review --json",
    "skill files review --json",
  ]);
  // The deleted file is listed with the others, in path order.
  expect(await rowNames(section)).toEqual([
    "SKILL.md",
    "assets/logo.png",
    "notes.md",
    "old.md",
    "reference.pdf",
    "reference/forms.md",
  ]);
  await expect(fileRow(section, "SKILL.md")).toContainText("edited by you");
  await expect(fileRow(section, "notes.md")).toContainText("added by you");
  await expect(fileRow(section, "old.md")).toContainText("removed by you");
  await expect(fileRow(section, "old.md")).toContainText("2 lines");
  await expect(fileRow(section, "reference/forms.md")).not.toContainText(
    "by you"
  );

  // The edited file: the whole file, with the line it replaced put back as removed.
  const skillMd = content(section, "SKILL.md");
  await expect(skillMd.getByRole("deletion")).toHaveText([
    "Use this skill to review a change.",
  ]);
  await expect(skillMd.getByRole("insertion")).toHaveText([
    "Use this skill to review a change before merging.",
    "Always run the tests first.",
  ]);
  await expect(skillMd).toContainText("name: review");
  // The added file is all insertions; the deleted file is its old content, all deletions.
  await fileRow(section, "notes.md").click();
  await expect(content(section, "notes.md").getByRole("insertion")).toHaveText([
    "My own notes.",
  ]);
  await fileRow(section, "old.md").click();
  await expect(content(section, "old.md").getByRole("deletion")).toHaveText([
    "An old note.",
    "Nobody reads it.",
  ]);
});

test("a modified skill of your own compares with what was last published", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/release-notes");
  const section = files(page, "release-notes");
  await expect(section).toContainText("Compared with what you last published");
  await expect(fileRow(section, "SKILL.md")).toContainText("edited by you");
  await expect(content(section, "SKILL.md").getByRole("insertion")).toHaveText([
    "Use this skill to write release notes from merged changes.",
  ]);
  expect(sortedLater(cli)).toEqual([
    "skill diff release-notes --json",
    "skill files release-notes --json",
  ]);
});

test("a plugin's skill is read as <plugin>:<name>, a client's own as --configuration; no diff", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/skill%3Ah-commit");
  const section = files(page, "commit-helper");
  await expect(fileRow(section, "SKILL.md")).toBeVisible();
  await expect(section).not.toContainText("Compared with");
  expect(later(cli)).toEqual([
    ["skill", "files", "acme-tools:commit-helper", "--json"],
  ]);

  await row(page, "notes").click();
  await expect(fileRow(files(page, "notes"), "SKILL.md")).toBeVisible();
  expect(later(cli)).toEqual([
    ["skill", "files", "acme-tools:commit-helper", "--json"],
    ["skill", "files", "notes", "--configuration", "cursor", "--json"],
  ]);
});

test("files that cannot be read say so, and are read again on the next selection", async ({
  cli,
  page,
}) => {
  await openSkills(cli, page, "/#/skills/pdf");
  cli.register(["skill", "files", "gone"], {
    events: [{ code: "not_found", message: "gone is not here", type: "error" }],
    exitCode: 5,
  });
  await row(page, "gone").click();
  await expect(files(page, "gone")).toContainText(
    "The files could not be read"
  );
  await page.getByRole("button", { name: "Dismiss" }).click();
  await row(page, "pdf").click();
  await row(page, "gone").click();
  await expect(files(page, "gone")).toContainText(
    "The files could not be read"
  );
  expect(later(cli)).toEqual([
    ["skill", "files", "pdf", "--json"],
    ["skill", "files", "gone", "--json"],
    ["skill", "files", "gone", "--json"],
  ]);
});
