// The History of one of your own skills: each commit said as what it did to the skill, the
// versions of the source's skill agentx stored left out, and in the sheet each entry expanding to
// the files its commit changed and collapsing again.
import type { Page } from "@playwright/test";

import type { HistoryEvent } from "#/cli/events.gen.ts";
import {
  entrySegments,
  entryText,
  isShown,
  metaOf,
} from "#/screens/skills/history.tsx";

import {
  ACME,
  adoption,
  fileReads,
  history,
  inventory,
} from "./fixtures/skills.ts";
import { expect, test } from "./harness/cli.ts";
import type { MockCli } from "./harness/cli.ts";

const EDITS = "Edited SKILL.md, added examples.md and 2 more files";

const sheet = (page: Page) => page.getByRole("dialog", { name: "History" });
const entryButton = (page: Page) =>
  sheet(page).getByRole("button", { exact: true, name: EDITS });
const filesOf = (page: Page) =>
  sheet(page).getByRole("list", { name: `Files of ${EDITS}` });
const details = (page: Page) =>
  page.getByRole("region", { name: "Skill details" });

/** Starts the app on the fixture machine, with pdf-letters' history and every skill's files. */
const start = (cli: MockCli) => {
  cli.onServeStart({ events: [inventory] });
  cli.register(["skill", "import"], { events: adoption });
  cli.register(["skill", "history", "pdf-letters"], { events: history });
  for (const read of fileReads) {
    cli.register(["skill", "files", ...read.args], { events: read.events });
  }
};

/** Opens pdf-letters, one of your own skills, and its History sheet. */
const openSheet = async (cli: MockCli, page: Page) => {
  start(cli);
  await page.goto("/#/skills/pdf-letters");
  await details(page).getByRole("button", { name: "Show all 4" }).click();
  await expect(sheet(page)).toBeVisible();
};

test("every entry says what happened, and the stored upstream version is left out", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const entries = sheet(page)
    .getByRole("list", { name: "All history" })
    .getByRole("listitem");
  await expect(entries).toHaveText([
    /^Tighten the examplesOct 2(?:, 2026)? · 1 file$/u,
    new RegExp(`^${EDITS}Oct 2(?:, 2026)? · this machine · 4 files$`, "u"),
    /^Synced edits from another machineOct 1(?:, 2026)? · 7c41e09b · 1 file$/u,
    /^Forked from acme\/skillsSep 30(?:, 2026)? · this machine · version of Sep 14(?:, 2026)?$/u,
  ]);
});

test("an entry expands to its files and collapses again", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const button = entryButton(page);
  await expect(button).toHaveAttribute("aria-expanded", "false");

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "true");
  await expect(filesOf(page).getByRole("listitem")).toHaveText([
    "modifiedSKILL.md",
    "addedexamples.md",
    "addedletters/formal.md",
    "addedletters/short.md",
  ]);

  await button.click();
  await expect(button).toHaveAttribute("aria-expanded", "false");
  await expect(filesOf(page)).toHaveCount(0);
});

test("a fork's source links to the skill's folder at the forked commit", async ({
  cli,
  page,
}) => {
  await openSheet(cli, page);
  const files = sheet(page).getByRole("button", {
    name: "Show the files of Forked from acme/skills",
  });
  await files.click();
  await expect(files).toHaveAttribute("aria-expanded", "true");

  const links = sheet(page).getByRole("link");
  await expect(links).toHaveText(["acme/skills"]);
  await links.first().click();
  await expect
    .poll(() => cli.openedUrls)
    .toEqual([`${ACME}/tree/3f2a9c1e8d7/pdf`]);
});

/** An event of `kind`, from the fixture's first, with `fields`. */
const of = (fields: Partial<HistoryEvent>): HistoryEvent => ({
  ...history[0],
  ...fields,
});

test("each kind of commit in words", () => {
  const plugin = "https://github.com/ChromeDevTools/chrome-devtools-mcp";
  const version = {
    commit: "9b8c7d6e5f4a",
    path: "pdf",
    skill_url: `${ACME}/tree/9b8c7d6e5f4a/pdf`,
    source: ACME,
    time: "2026-10-03T12:00:00Z",
    url: ACME,
  };
  const cases: [Partial<HistoryEvent>, string][] = [
    [{ kind: "create", subject: "Create notes" }, "Created"],
    [{ kind: "adopt", subject: "Adopt notes" }, "Imported from disk"],
    [
      { kind: "rename", renamed_from: "pdf-notes", subject: "Rename x to y" },
      "Renamed from pdf-notes",
    ],
    [
      {
        kind: "fork",
        origin: {
          kind: "plugin",
          plugin: "chrome-devtools-mcp",
          skill: "a11y",
          url: plugin,
        },
      },
      "Forked from plugin chrome-devtools-mcp",
    ],
    [
      { kind: "fork", origin: { kind: "skill", skill: "alpha" } },
      "Forked from alpha",
    ],
    [{ kind: "fork", subject: "Fork old from new" }, "Forked"],
    [{ kind: "update", upstream: version }, "Updated from acme/skills"],
    [
      {
        files: [{ path: "SKILL.md", status: "modified" }],
        generated: true,
        kind: "edit",
      },
      "Edited SKILL.md",
    ],
    [
      {
        files: [
          { path: "a.md", status: "deleted" },
          { path: "b.md", status: "added" },
        ],
        generated: true,
        kind: "edit",
      },
      "Deleted a.md, added b.md",
    ],
    [{ kind: "edit", subject: "My own words" }, "My own words"],
    [{ kind: "git", subject: "By hand" }, "By hand"],
    [
      { kind: "later", subject: "A kind not known yet" },
      "A kind not known yet",
    ],
  ];
  for (const [fields, text] of cases) {
    expect(entryText(of(fields))).toBe(text);
  }
  // Only an https page is a link.
  expect(
    entrySegments(
      of({ kind: "fork", origin: { kind: "skill", skill: "a", url: plugin } })
    ).map((s) => s.page)
  ).toEqual([undefined, plugin]);
  expect(
    entrySegments(
      of({
        kind: "fork",
        origin: { kind: "skill", skill: "a", url: "http://example.com" },
      })
    ).map((s) => s.page)
  ).toEqual([undefined, undefined]);
  expect(
    entrySegments(of({ kind: "update", upstream: version })).map((s) => s.page)
  ).toEqual([undefined, version.skill_url]);

  expect(isShown(of({ kind: "upstream" }))).toBe(false);
  expect(isShown(of({ kind: "edit" }))).toBe(true);
  // The meta line: this machine by its id, another by the start of its id, and nothing for an unknown one.
  const at = { time: "2026-10-02T14:03:00Z" };
  expect(metaOf(of({ ...at, machine: "m-1" }), "m-1")).toMatch(
    /· this (?:machine|Mac) · 1 file$/u
  );
  expect(
    metaOf(of({ ...at, machine: "7c41e09b5a2d4f6e8b1c3d5e7f9a0b2c" }), "m-1")
  ).toMatch(/· 7c41e09b · 1 file$/u);
  expect(metaOf(of(at), "m-1")).toMatch(/^Oct 2(?:, 2026)? · 1 file$/u);
});
